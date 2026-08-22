// Specs for the compliance-document byte path (Phase 38c-ii).
//
// These exist because the transfer is the one place in this app where a NATS
// call and an HTTP call have to agree, and the agreement is easy to break
// invisibly. Three things in particular:
//
//   1. The ticket must be minted over NATS *before* the fetch, and must be the
//      only authorization the fetch carries (BR-TP41). A refactor that put the
//      partner or tenant into the HTTP request instead would still work in the
//      happy path while quietly moving the tenancy decision outside the NATS
//      account boundary that enforces it.
//   2. The filename must be percent-encoded in the header. A non-ASCII
//      filename in a raw header value is not merely wrong, it throws in the
//      browser — and every filename in the dev data happens to be ASCII.
//   3. The HTTP status must survive onto the thrown error. The component
//      branches on it to tell 409 (supersede) from 413 (too large) from 403
//      (ticket spent) — three different instructions to the operator.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const request = vi.fn()

vi.mock('./nats/useTenantConnection.js', () => ({
  useTenantConnection: () => ({ request }),
}))
vi.mock('./nats/useRefdataAdminConnection.js', () => ({
  useRefdataAdminConnection: () => ({ request: vi.fn() }),
}))

const { uploadComplianceDocumentFile, downloadComplianceDocumentFile, registerGitCertificateWithFile } = await import('./api.js')

describe('compliance document file transfer', () => {
  let fetchMock

  beforeEach(() => {
    request.mockReset()
    request.mockResolvedValue({ ticket: 'tok-123', maxBytes: 10485760 })
    fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  const okJson = (body) => ({ ok: true, status: 200, json: async () => body })
  const okBlob = (blob) => ({ ok: true, status: 200, blob: async () => blob })
  const failure = (status, body) => ({
    ok: false,
    status,
    json: async () => body,
  })

  const fileLike = (name, type, size) => ({ name, type, size })

  describe('upload', () => {
    it('registers a GIT certificate and spends the ticket returned by that same call', async () => {
      request.mockResolvedValue({ ticket: 'git-ticket', maxBytes: 10485760, document: { id: 'doc-new' } })
      fetchMock.mockResolvedValue(okJson({ id: 'doc-new', status: 'FOR_REVIEW' }))
      const file = fileLike('renewal.pdf', 'application/pdf', 10)

      const result = await registerGitCertificateWithFile('acme', 'tp-1', {
        reference: 'renewal.pdf', goodsTypes: ['GENERAL_FREIGHT'], coverageCents: 500000,
      }, file)

      expect(request).toHaveBeenCalledTimes(1)
      expect(request).toHaveBeenCalledWith('api.acme.organizations.document.git-register.v1', {
        id: 'tp-1', reference: 'renewal.pdf', goodsTypes: ['GENERAL_FREIGHT'], coverageCents: 500000,
      })
      expect(request.mock.invocationCallOrder[0]).toBeLessThan(fetchMock.mock.invocationCallOrder[0])
      expect(fetchMock.mock.calls[0][1].headers['X-Document-Ticket']).toBe('git-ticket')
      expect(fetchMock.mock.calls[0][1].body).toBe(file)
      expect(result.status).toBe('FOR_REVIEW')
    })

    it('mints an upload ticket over NATS before sending any bytes', async () => {
      fetchMock.mockResolvedValue(okJson({ id: 'doc-1' }))

      await uploadComplianceDocumentFile('acme', 'tp-1', 'doc-1', fileLike('cert.pdf', 'application/pdf', 10))

      expect(request).toHaveBeenCalledTimes(1)
      expect(request.mock.calls[0][0]).toBe('api.acme.organizations.document.upload-ticket.v1')
      expect(request.mock.calls[0][1]).toEqual({ id: 'tp-1', documentId: 'doc-1' })

      // Ordering, not just presence: the ticket has to exist before the POST.
      expect(request.mock.invocationCallOrder[0]).toBeLessThan(fetchMock.mock.invocationCallOrder[0])
    })

    it('carries the ticket as the only authorization, and the file as the raw body', async () => {
      fetchMock.mockResolvedValue(okJson({ id: 'doc-1' }))
      const file = fileLike('cert.pdf', 'application/pdf', 10)

      await uploadComplianceDocumentFile('acme', 'tp-1', 'doc-1', file)

      const [url, init] = fetchMock.mock.calls[0]
      expect(url).toBe('/files/documents')
      expect(init.method).toBe('POST')
      expect(init.headers['X-Document-Ticket']).toBe('tok-123')
      expect(init.body).toBe(file)
      // Neither tenant nor partner nor document is sent over HTTP — all three
      // are read off the ticket server-side (BR-TP41).
      expect(JSON.stringify(init)).not.toContain('tp-1')
      expect(url).not.toContain('acme')
    })

    it('percent-encodes a non-ASCII filename', async () => {
      fetchMock.mockResolvedValue(okJson({ id: 'doc-1' }))

      await uploadComplianceDocumentFile('acme', 'tp-1', 'doc-1', fileLike('GIT — cover (2026).pdf', 'application/pdf', 10))

      expect(fetchMock.mock.calls[0][1].headers['X-Document-File-Name'])
        .toBe('GIT%20%E2%80%94%20cover%20(2026).pdf')
    })

    it('falls back to a generic content type when the browser reports none', async () => {
      fetchMock.mockResolvedValue(okJson({ id: 'doc-1' }))

      await uploadComplianceDocumentFile('acme', 'tp-1', 'doc-1', fileLike('scan.xyz', '', 10))

      expect(fetchMock.mock.calls[0][1].headers['Content-Type']).toBe('application/octet-stream')
    })

    it('throws with the status attached so the caller can distinguish recoveries', async () => {
      fetchMock.mockResolvedValue(failure(409, { error: 'compliance document already has a file attached' }))

      await expect(
        uploadComplianceDocumentFile('acme', 'tp-1', 'doc-1', fileLike('cert.pdf', 'application/pdf', 10)),
      ).rejects.toMatchObject({
        status: 409,
        message: 'compliance document already has a file attached',
      })
    })

    it('still throws usefully when the failure body is not JSON', async () => {
      // A proxy's own 413 page, for instance — nginx answers before the
      // service is reached, so there is no error envelope to read.
      fetchMock.mockResolvedValue({
        ok: false,
        status: 413,
        json: async () => {
          throw new Error('not json')
        },
      })

      await expect(
        uploadComplianceDocumentFile('acme', 'tp-1', 'doc-1', fileLike('big.pdf', 'application/pdf', 10)),
      ).rejects.toMatchObject({ status: 413 })
    })
  })

  describe('download', () => {
    it('mints a download ticket and returns the blob', async () => {
      const blob = { size: 5 }
      fetchMock.mockResolvedValue(okBlob(blob))

      const result = await downloadComplianceDocumentFile('acme', 'tp-1', 'doc-1')

      expect(request.mock.calls[0][0]).toBe('api.acme.organizations.document.download-ticket.v1')
      expect(result).toBe(blob)
    })

    it('sends the ticket in a header, never in the URL', async () => {
      fetchMock.mockResolvedValue(okBlob({ size: 5 }))

      await downloadComplianceDocumentFile('acme', 'tp-1', 'doc-1')

      const [url, init] = fetchMock.mock.calls[0]
      expect(url).toBe('/files/documents')
      expect(url).not.toContain('tok-123')
      expect(init.headers['X-Document-Ticket']).toBe('tok-123')
    })

    it('propagates a refusal with its status', async () => {
      fetchMock.mockResolvedValue(failure(404, { error: 'compliance document has no file attached' }))

      await expect(downloadComplianceDocumentFile('acme', 'tp-1', 'doc-1'))
        .rejects.toMatchObject({ status: 404 })
    })
  })

  it('refuses to build a subject from a context containing a dot', async () => {
    // Same guard as every other tp call: a dotted context would shift every
    // later token and silently address the wrong context.
    await expect(
      uploadComplianceDocumentFile('acme.north', 'tp-1', 'doc-1', fileLike('cert.pdf', 'application/pdf', 10)),
    ).rejects.toThrow(/invalid context/)
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
