package tradingpartner_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/filetickets"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/rest"
)

// --- Fakes -------------------------------------------------------------
//
// These specs drive the byte path with in-memory fakes rather than Postgres
// and a live NATS Object Store. That is deliberate: BR-TP41-BR-TP45 are rules
// about ordering, identity and refusal — "were the bytes written before the
// row", "is this ticket spent", "is this name derived from anything the client
// controls" — and every one of them is observable at this boundary. The
// Postgres-backed and over-the-wire behaviour is covered by the existing
// suites, which already prove the repository and adapter honour the same
// domain guards.

// fileDocRepo implements domain.ComplianceDocumentRepository, but only the
// methods the byte path calls; the rest exist to satisfy the interface and
// panic if the file path ever reaches them, which would itself be the bug.
type fileDocRepo struct {
	docs map[string]domain.ComplianceDocument // documentID -> document
	// attachErr forces AttachFile to fail, so a spec can observe what happens
	// to bytes already written when recording them fails (BR-TP43).
	attachErr error
	attached  int
}

func newFileDocRepo(docs ...domain.ComplianceDocument) *fileDocRepo {
	r := &fileDocRepo{docs: map[string]domain.ComplianceDocument{}}
	for _, d := range docs {
		r.docs[d.ID] = d
	}
	return r
}

func (r *fileDocRepo) GetDocument(_ context.Context, _, documentID string) (domain.ComplianceDocument, error) {
	doc, ok := r.docs[documentID]
	if !ok {
		return domain.ComplianceDocument{}, domain.ErrDocumentNotFound
	}
	return doc, nil
}

func (r *fileDocRepo) AttachFile(_ context.Context, _, documentID string, file domain.DocumentFile) (domain.ComplianceDocument, error) {
	r.attached++
	if r.attachErr != nil {
		return domain.ComplianceDocument{}, r.attachErr
	}
	doc, ok := r.docs[documentID]
	if !ok {
		return domain.ComplianceDocument{}, domain.ErrDocumentNotFound
	}
	updated, err := doc.AttachFile(file)
	if err != nil {
		return domain.ComplianceDocument{}, err
	}
	r.docs[documentID] = updated
	return updated, nil
}

func (r *fileDocRepo) AddDocument(context.Context, string, domain.ComplianceDocument) (domain.ComplianceDocument, error) {
	panic("the document byte path must not add documents")
}
func (r *fileDocRepo) ListDocuments(context.Context, string) ([]domain.ComplianceDocument, error) {
	panic("the document byte path must not list documents")
}
func (r *fileDocRepo) ApproveDocument(context.Context, string, string) (domain.ComplianceDocument, error) {
	panic("the document byte path must not review documents")
}
func (r *fileDocRepo) RejectDocument(context.Context, string, string) (domain.ComplianceDocument, error) {
	panic("the document byte path must not review documents")
}
func (r *fileDocRepo) ResubmitDocument(context.Context, string, string) (domain.ComplianceDocument, error) {
	panic("the document byte path must not review documents")
}

// fakeObjectStore records what was stored under which name, which is how the
// specs observe BR-TP42's naming rule and BR-TP43's write order.
type fakeObjectStore struct {
	objects  map[string][]byte
	names    []string
	metadata map[string][2]string // name -> {fileName, contentType}
	putErr   error
}

func newFakeObjectStore() *fakeObjectStore {
	return &fakeObjectStore{objects: map[string][]byte{}, metadata: map[string][2]string{}}
}

func (s *fakeObjectStore) Put(_ context.Context, name, fileName, contentType string, r io.Reader) (int64, error) {
	if s.putErr != nil {
		return 0, s.putErr
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	s.objects[name] = data
	s.names = append(s.names, name)
	s.metadata[name] = [2]string{fileName, contentType}
	return int64(len(data)), nil
}

func (s *fakeObjectStore) Get(_ context.Context, name string) (io.ReadCloser, error) {
	data, ok := s.objects[name]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// fakeResolver maps every tenant to one store, and records which tenant was
// asked for — the point of BR-TP41 is that this comes from the grant.
type fakeResolver struct {
	store   *fakeObjectStore
	askedBy []string
	err     error
}

func (f *fakeResolver) DocumentStore(tenant string) (commands.DocumentObjectStore, error) {
	f.askedBy = append(f.askedBy, tenant)
	if f.err != nil {
		return nil, f.err
	}
	return f.store, nil
}

// --- Specs -------------------------------------------------------------

var _ = Describe("Compliance document files (Phase 38c-ii)", func() {
	const (
		tenant     = "acme"
		contextKey = "acme"
		partnerID  = "tp-1"
		documentID = "doc-1"
	)

	pendingDoc := func() domain.ComplianceDocument {
		return domain.ComplianceDocument{
			ID:        documentID,
			Type:      domain.DocumentTypeGoodsInTransit,
			Status:    domain.DocumentStatusPending,
			Reference: documentID,
		}
	}

	newHandler := func(repo *fileDocRepo) (*commands.DocumentFileHandler, *fakeObjectStore, *fakeResolver, *filetickets.Store) {
		store := newFakeObjectStore()
		resolver := &fakeResolver{store: store}
		tickets := filetickets.NewStore(filetickets.DefaultTTL)
		return commands.NewDocumentFileHandler(repo, tickets, resolver), store, resolver, tickets
	}

	Context("BR-TP42: the object name is composed only of service-controlled values", func() {
		It("names an object {context}.transporter.{partnerID}.{docType}.{documentID}", func() {
			Expect(domain.DocumentObjectName("acme", "tp-1", domain.DocumentTypeGoodsInTransit, "doc-1")).
				To(Equal("acme.transporter.tp-1.GOODS_IN_TRANSIT.doc-1"))
		})

		It("does not include the uploaded filename, so two uploads of the same name cannot collide", func() {
			repo := newFileDocRepo(pendingDoc())
			h, store, _, _ := newHandler(repo)

			token, err := h.MintUploadTicket(context.Background(), tenant, contextKey, partnerID, documentID)
			Expect(err).NotTo(HaveOccurred())

			_, err = h.Upload(context.Background(), token, "../../etc/passwd", "application/pdf", strings.NewReader("bytes"))
			Expect(err).NotTo(HaveOccurred())

			Expect(store.names).To(ConsistOf("acme.transporter.tp-1.GOODS_IN_TRANSIT.doc-1"))
			// The hostile filename is retained as metadata — it is data, and it
			// never had a chance to become identity.
			Expect(store.metadata["acme.transporter.tp-1.GOODS_IN_TRANSIT.doc-1"][0]).To(Equal("../../etc/passwd"))
		})
	})

	Context("BR-TP41: transfer is authorized by a single-use, short-lived, direction-bound ticket", func() {
		It("takes tenant and context from the mint call, never from the redeeming caller", func() {
			repo := newFileDocRepo(pendingDoc())
			h, _, resolver, _ := newHandler(repo)

			token, err := h.MintUploadTicket(context.Background(), "globex", "globex-north", partnerID, documentID)
			Expect(err).NotTo(HaveOccurred())

			doc, err := h.Upload(context.Background(), token, "cert.pdf", "application/pdf", strings.NewReader("bytes"))
			Expect(err).NotTo(HaveOccurred())

			// The bucket asked for is the one named in the grant, and the
			// object name carries the grant's context — the Upload call itself
			// supplied neither.
			Expect(resolver.askedBy).To(ConsistOf("globex"))
			Expect(doc.File.ObjectName).To(HavePrefix("globex-north."))
		})

		It("refuses a second use of the same ticket", func() {
			repo := newFileDocRepo(pendingDoc())
			h, _, _, _ := newHandler(repo)

			token, err := h.MintUploadTicket(context.Background(), tenant, contextKey, partnerID, documentID)
			Expect(err).NotTo(HaveOccurred())
			_, err = h.Upload(context.Background(), token, "cert.pdf", "application/pdf", strings.NewReader("bytes"))
			Expect(err).NotTo(HaveOccurred())

			_, err = h.Upload(context.Background(), token, "cert.pdf", "application/pdf", strings.NewReader("bytes"))
			Expect(errors.Is(err, filetickets.ErrUnknownTicket)).To(BeTrue())
		})

		It("refuses an upload ticket spent on a download, and consumes it either way", func() {
			tickets := filetickets.NewStore(filetickets.DefaultTTL)
			token, err := tickets.Mint(filetickets.Grant{Tenant: tenant, Direction: filetickets.DirectionUpload})
			Expect(err).NotTo(HaveOccurred())

			_, err = tickets.Redeem(token, filetickets.DirectionDownload)
			Expect(errors.Is(err, filetickets.ErrWrongDirection)).To(BeTrue())

			// Burned by the failed attempt: a mismatched redemption must not
			// leave the token available to try against the other route.
			_, err = tickets.Redeem(token, filetickets.DirectionUpload)
			Expect(errors.Is(err, filetickets.ErrUnknownTicket)).To(BeTrue())
		})

		It("refuses an expired ticket", func() {
			tickets := filetickets.NewStore(time.Nanosecond)
			token, err := tickets.Mint(filetickets.Grant{Tenant: tenant, Direction: filetickets.DirectionUpload})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() error {
				_, err := tickets.Redeem(token, filetickets.DirectionUpload)
				return err
			}).Should(MatchError(filetickets.ErrUnknownTicket))
		})

		It("refuses an unknown token", func() {
			tickets := filetickets.NewStore(filetickets.DefaultTTL)
			_, err := tickets.Redeem("not-a-real-token", filetickets.DirectionUpload)
			Expect(errors.Is(err, filetickets.ErrUnknownTicket)).To(BeTrue())
		})

		It("refuses to mint an upload ticket for a document that already has a file", func() {
			doc := pendingDoc()
			doc.File = &domain.DocumentFile{FileName: "old.pdf", ContentType: "application/pdf", SizeBytes: 10, ObjectName: "x"}
			h, _, _, _ := newHandler(newFileDocRepo(doc))

			_, err := h.MintUploadTicket(context.Background(), tenant, contextKey, partnerID, documentID)
			Expect(errors.Is(err, domain.ErrDocumentFileAlreadyAttached)).To(BeTrue())
		})

		It("refuses to mint a download ticket for a document that has no file", func() {
			h, _, _, _ := newHandler(newFileDocRepo(pendingDoc()))

			_, err := h.MintDownloadTicket(context.Background(), tenant, contextKey, partnerID, documentID)
			Expect(errors.Is(err, domain.ErrDocumentFileMissing)).To(BeTrue())
		})

		It("refuses to mint any ticket for an unknown document", func() {
			h, _, _, _ := newHandler(newFileDocRepo())

			_, err := h.MintUploadTicket(context.Background(), tenant, contextKey, partnerID, "nope")
			Expect(errors.Is(err, domain.ErrDocumentNotFound)).To(BeTrue())
		})
	})

	Context("BR-TP43: bytes are written before they are recorded, and are write-once", func() {
		It("leaves an orphan object rather than a dangling reference when recording fails", func() {
			repo := newFileDocRepo(pendingDoc())
			repo.attachErr = errors.New("postgres is down")
			h, store, _, _ := newHandler(repo)

			token, err := h.MintUploadTicket(context.Background(), tenant, contextKey, partnerID, documentID)
			Expect(err).NotTo(HaveOccurred())

			_, err = h.Upload(context.Background(), token, "cert.pdf", "application/pdf", strings.NewReader("bytes"))
			Expect(err).To(MatchError(ContainSubstring("postgres is down")))

			// The object exists and nothing references it. This is the
			// deliberate asymmetry: an orphan is addressable by name and
			// invisible to readers, whereas a recorded file whose bytes were
			// never written is a promise the log cannot keep.
			Expect(store.objects).To(HaveLen(1))
			Expect(repo.docs[documentID].File).To(BeNil())
		})

		It("does not write bytes at all when the store cannot be resolved", func() {
			repo := newFileDocRepo(pendingDoc())
			h, store, resolver, _ := newHandler(repo)
			resolver.err = errors.New("tenant is not connected")

			token, err := h.MintUploadTicket(context.Background(), tenant, contextKey, partnerID, documentID)
			Expect(err).NotTo(HaveOccurred())

			_, err = h.Upload(context.Background(), token, "cert.pdf", "application/pdf", strings.NewReader("bytes"))
			Expect(err).To(HaveOccurred())
			Expect(store.objects).To(BeEmpty())
			Expect(repo.attached).To(BeZero())
		})

		It("rejects a second upload against a document that already has a file", func() {
			doc := pendingDoc()
			doc.File = &domain.DocumentFile{FileName: "old.pdf", ContentType: "application/pdf", SizeBytes: 3, ObjectName: "x"}

			_, err := doc.AttachFile(domain.DocumentFile{FileName: "new.pdf", ContentType: "application/pdf", SizeBytes: 3, ObjectName: "y"})
			Expect(errors.Is(err, domain.ErrDocumentFileAlreadyAttached)).To(BeTrue())
		})

		It("rejects attaching a file to a superseded document", func() {
			doc := pendingDoc()
			doc.Status = domain.DocumentStatusSuperseded

			_, err := doc.AttachFile(domain.DocumentFile{FileName: "cert.pdf", ContentType: "application/pdf", SizeBytes: 3, ObjectName: "y"})
			Expect(errors.Is(err, domain.ErrDocumentSuperseded)).To(BeTrue())
		})
	})

	Context("BR-TP44: explicit size limits, enforced on real bytes", func() {
		It("accepts a file exactly at the limit", func() {
			doc := pendingDoc()
			_, err := doc.AttachFile(domain.DocumentFile{
				FileName: "cert.pdf", ContentType: "application/pdf",
				SizeBytes: domain.MaxDocumentFileBytes, ObjectName: "y",
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects a file one byte over the limit", func() {
			doc := pendingDoc()
			_, err := doc.AttachFile(domain.DocumentFile{
				FileName: "cert.pdf", ContentType: "application/pdf",
				SizeBytes: domain.MaxDocumentFileBytes + 1, ObjectName: "y",
			})
			Expect(errors.Is(err, domain.ErrFileTooLarge)).To(BeTrue())
		})

		It("rejects an empty upload", func() {
			repo := newFileDocRepo(pendingDoc())
			h, _, _, _ := newHandler(repo)

			token, err := h.MintUploadTicket(context.Background(), tenant, contextKey, partnerID, documentID)
			Expect(err).NotTo(HaveOccurred())

			_, err = h.Upload(context.Background(), token, "cert.pdf", "application/pdf", strings.NewReader(""))
			Expect(errors.Is(err, domain.ErrFileEmpty)).To(BeTrue())
		})

		It("rejects an oversized stream even when nothing declared its length", func() {
			repo := newFileDocRepo(pendingDoc())
			h, _, _, _ := newHandler(repo)

			token, err := h.MintUploadTicket(context.Background(), tenant, contextKey, partnerID, documentID)
			Expect(err).NotTo(HaveOccurred())

			oversized := strings.NewReader(strings.Repeat("x", int(domain.MaxDocumentFileBytes)+1))
			_, err = h.Upload(context.Background(), token, "cert.pdf", "application/pdf", oversized)
			Expect(errors.Is(err, domain.ErrFileTooLarge)).To(BeTrue())
			// The document is not updated: the size check runs before the row
			// is touched, so an over-limit upload never becomes a file.
			Expect(repo.docs[documentID].File).To(BeNil())
		})
	})

	Context("BR-TP45: file metadata is recorded and replayed on download", func() {
		It("records name, content type, size and object name", func() {
			repo := newFileDocRepo(pendingDoc())
			h, _, _, _ := newHandler(repo)

			token, err := h.MintUploadTicket(context.Background(), tenant, contextKey, partnerID, documentID)
			Expect(err).NotTo(HaveOccurred())

			doc, err := h.Upload(context.Background(), token, "git-cert.pdf", "application/pdf", strings.NewReader("hello"))
			Expect(err).NotTo(HaveOccurred())
			Expect(doc.File).NotTo(BeNil())
			Expect(doc.File.FileName).To(Equal("git-cert.pdf"))
			Expect(doc.File.ContentType).To(Equal("application/pdf"))
			Expect(doc.File.SizeBytes).To(Equal(int64(5)))
			Expect(doc.File.ObjectName).To(Equal("acme.transporter.tp-1.GOODS_IN_TRANSIT.doc-1"))
			Expect(doc.File.UploadedAt).To(BeNumerically(">", 0))
		})

		It("requires a filename and a content type", func() {
			repo := newFileDocRepo(pendingDoc())
			h, _, _, _ := newHandler(repo)

			token, err := h.MintUploadTicket(context.Background(), tenant, contextKey, partnerID, documentID)
			Expect(err).NotTo(HaveOccurred())
			_, err = h.Upload(context.Background(), token, "", "application/pdf", strings.NewReader("x"))
			Expect(errors.Is(err, domain.ErrFileNameRequired)).To(BeTrue())

			token, err = h.MintUploadTicket(context.Background(), tenant, contextKey, partnerID, documentID)
			Expect(err).NotTo(HaveOccurred())
			_, err = h.Upload(context.Background(), token, "cert.pdf", "", strings.NewReader("x"))
			Expect(errors.Is(err, domain.ErrContentTypeRequired)).To(BeTrue())
		})

		It("returns the stored bytes and the document on download", func() {
			repo := newFileDocRepo(pendingDoc())
			h, _, _, _ := newHandler(repo)

			upload, err := h.MintUploadTicket(context.Background(), tenant, contextKey, partnerID, documentID)
			Expect(err).NotTo(HaveOccurred())
			_, err = h.Upload(context.Background(), upload, "git-cert.pdf", "application/pdf", strings.NewReader("hello"))
			Expect(err).NotTo(HaveOccurred())

			download, err := h.MintDownloadTicket(context.Background(), tenant, contextKey, partnerID, documentID)
			Expect(err).NotTo(HaveOccurred())

			doc, body, err := h.Download(context.Background(), download)
			Expect(err).NotTo(HaveOccurred())
			defer body.Close() //nolint:errcheck

			Expect(doc.File.FileName).To(Equal("git-cert.pdf"))
			Expect(io.ReadAll(body)).To(Equal([]byte("hello")))
		})
	})

	Context("BR-TP40: the HTTP ingress maps each refusal to its own status code", func() {
		// The browser branches on these codes to decide what to tell the
		// operator — supersede, pick a smaller file, or retry with a fresh
		// ticket. Collapsing them into one failure would make all three
		// indistinguishable, the same defect BR-TP39 fixed for conflicts.
		var (
			server *httptest.Server
			h      *commands.DocumentFileHandler
			repo   *fileDocRepo
		)

		BeforeEach(func() {
			repo = newFileDocRepo(pendingDoc())
			h, _, _, _ = newHandler(repo)
			mux := http.NewServeMux()
			rest.Mount(mux, h, nil)
			server = httptest.NewServer(mux)
			DeferCleanup(server.Close)
		})

		post := func(ticket, fileName, contentType, body string) *http.Response {
			req, err := http.NewRequest(http.MethodPost, server.URL+"/files/documents", strings.NewReader(body))
			Expect(err).NotTo(HaveOccurred())
			if ticket != "" {
				req.Header.Set("X-Document-Ticket", ticket)
			}
			if fileName != "" {
				req.Header.Set("X-Document-File-Name", fileName)
			}
			if contentType != "" {
				req.Header.Set("Content-Type", contentType)
			}
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(resp.Body.Close)
			return resp
		}

		It("answers 400 when no ticket is presented", func() {
			Expect(post("", "cert.pdf", "application/pdf", "x").StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("answers 403 for an unknown or spent ticket", func() {
			Expect(post("bogus", "cert.pdf", "application/pdf", "x").StatusCode).To(Equal(http.StatusForbidden))
		})

		It("answers 400 when the filename header is missing", func() {
			token, err := h.MintUploadTicket(context.Background(), tenant, contextKey, partnerID, documentID)
			Expect(err).NotTo(HaveOccurred())
			Expect(post(token, "", "application/pdf", "x").StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("answers 413 for a body over the cap", func() {
			token, err := h.MintUploadTicket(context.Background(), tenant, contextKey, partnerID, documentID)
			Expect(err).NotTo(HaveOccurred())
			oversized := strings.Repeat("x", int(domain.MaxDocumentFileBytes)+64)
			Expect(post(token, "cert.pdf", "application/pdf", oversized).StatusCode).
				To(Equal(http.StatusRequestEntityTooLarge))
		})

		It("answers 409 when the document already has a file", func() {
			token, err := h.MintUploadTicket(context.Background(), tenant, contextKey, partnerID, documentID)
			Expect(err).NotTo(HaveOccurred())
			Expect(post(token, "cert.pdf", "application/pdf", "x").StatusCode).To(Equal(http.StatusOK))

			// Mint bypasses the guard only because the fake repo is mutated
			// in place; the redeem-time guard is what must catch this.
			second, err := h.MintUploadTicket(context.Background(), tenant, contextKey, partnerID, documentID)
			if err != nil {
				// Mint refused first, which is also correct (BR-TP41) — then
				// there is nothing left to assert at the HTTP layer.
				Expect(errors.Is(err, domain.ErrDocumentFileAlreadyAttached)).To(BeTrue())
				return
			}
			Expect(post(second, "cert.pdf", "application/pdf", "x").StatusCode).To(Equal(http.StatusConflict))
		})

		It("percent-decodes a non-ASCII filename and returns it on download", func() {
			token, err := h.MintUploadTicket(context.Background(), tenant, contextKey, partnerID, documentID)
			Expect(err).NotTo(HaveOccurred())
			// "GIT — cover (2026).pdf" as the browser would encode it.
			Expect(post(token, "GIT%20%E2%80%94%20cover%20%282026%29.pdf", "application/pdf", "x").StatusCode).
				To(Equal(http.StatusOK))
			Expect(repo.docs[documentID].File.FileName).To(Equal("GIT — cover (2026).pdf"))

			download, err := h.MintDownloadTicket(context.Background(), tenant, contextKey, partnerID, documentID)
			Expect(err).NotTo(HaveOccurred())
			req, err := http.NewRequest(http.MethodGet, server.URL+"/files/documents", nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("X-Document-Ticket", download)
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(resp.Body.Close)

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Header.Get("Content-Type")).To(Equal("application/pdf"))
			Expect(resp.Header.Get("Content-Disposition")).To(ContainSubstring("filename*=UTF-8''"))
			Expect(io.ReadAll(resp.Body)).To(Equal([]byte("x")))
		})
	})
})
