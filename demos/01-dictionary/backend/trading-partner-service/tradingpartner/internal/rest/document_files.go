package rest

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/application/commands"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/domain"
	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/trading-partner-service/tradingpartner/internal/filetickets"
)

// This file is Phase 38c-ii's HTTP ingress for compliance document bytes — a
// scoped, deliberate partial reversal of Phase 33.5's REST retirement, and the
// only business-adjacent HTTP this service serves.
//
// Why HTTP at all, when everything else here is NATS request/reply: the
// command surface is JSON micro request/reply, which is neither a streaming
// transport nor able to exceed the server's max_payload (unset in this lab's
// nats.conf, so the 1 MiB default). Document bytes need a transport that
// streams. Note this makes the service a *mandatory* byte proxy in both
// directions — Object Store has no presigned-URL equivalent, unlike S3. That
// is a permanent property of the choice, not a gap to close later.
//
// Why these two routes are not a reopening of the fourteen Phase 33.5 deleted:
// they carry bytes and nothing else. No business decision, no state machine,
// no lifecycle transition is reachable here — every one of those still needs
// an authenticated NATS connection. BR-TP17's allowlist test is extended by
// exactly these two entries, and stays closed to everything else.
//
// Transport shape is raw body + headers, deliberately not multipart/form-data:
// multipart would add a parser and a temp-file spill path to move a single
// file, and it buries the one field that matters (the bytes) inside a format
// whose only advantage is carrying several fields at once. Here the body *is*
// the file, so it streams straight into the object store.

const (
	// ticketHeader carries BR-TP41's capability token. A header, not a query
	// parameter, on both routes: a URL is logged by proxies, kept in history
	// and pasted into bug reports, and a ticket — however short-lived — is a
	// credential that grants access to a compliance document.
	ticketHeader = "X-Document-Ticket"

	// fileNameHeader carries the client's original filename, percent-encoded
	// because HTTP header values are ASCII and real filenames are not.
	fileNameHeader = "X-Document-File-Name"
)

// DocumentFileRoutes is the "METHOD /pattern" list MountDocumentFiles
// registers, kept beside it so BR-TP17's allowlist test names exactly what
// 38c-ii added.
var DocumentFileRoutes = []string{"POST /files/documents", "GET /files/documents"}

// MountDocumentFiles registers the byte-transfer ingress. Returns the routes
// it registered, matching Mount's contract.
func MountDocumentFiles(mux *http.ServeMux, files *commands.DocumentFileHandler, log *slog.Logger) []string {
	h := &documentFileHandler{files: files, log: log}
	mux.HandleFunc("POST /files/documents", h.upload)
	mux.HandleFunc("GET /files/documents", h.download)
	return DocumentFileRoutes
}

type documentFileHandler struct {
	files *commands.DocumentFileHandler
	log   *slog.Logger
}

func (h *documentFileHandler) upload(w http.ResponseWriter, r *http.Request) {
	ticket := r.Header.Get(ticketHeader)
	if ticket == "" {
		writeFileError(w, commands.ErrTicketRequired)
		return
	}

	fileName, err := url.QueryUnescape(r.Header.Get(fileNameHeader))
	if err != nil || fileName == "" {
		writeFileError(w, domain.ErrFileNameRequired)
		return
	}

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		writeFileError(w, domain.ErrContentTypeRequired)
		return
	}

	// BR-TP44 at the transport edge: stop reading rather than stream an
	// unbounded body into memory or the bucket. The +1 leaves the handler's
	// own size check able to tell "exactly at the limit" from "over it".
	body := http.MaxBytesReader(w, r.Body, domain.MaxDocumentFileBytes+1)
	defer body.Close() //nolint:errcheck

	doc, err := h.files.Upload(r.Context(), ticket, fileName, contentType, body)
	if err != nil {
		// A MaxBytesReader trip surfaces as a read error from the object
		// store's copy, not as one of the domain sentinels, so it is mapped
		// explicitly rather than falling through to a 500.
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeFileError(w, domain.ErrFileTooLarge)
			return
		}
		h.logUnexpected("document file upload", err)
		writeFileError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(doc); err != nil {
		h.logUnexpected("encode upload response", err)
	}
}

func (h *documentFileHandler) download(w http.ResponseWriter, r *http.Request) {
	ticket := r.Header.Get(ticketHeader)
	if ticket == "" {
		writeFileError(w, commands.ErrTicketRequired)
		return
	}

	doc, body, err := h.files.Download(r.Context(), ticket)
	if err != nil {
		h.logUnexpected("document file download", err)
		writeFileError(w, err)
		return
	}
	defer body.Close() //nolint:errcheck

	// BR-TP45: headers come from the projection, so a download presents the
	// name and type the operator uploaded rather than the object's key.
	w.Header().Set("Content-Type", doc.File.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(doc.File.SizeBytes, 10))
	// RFC 5987 form only. A bare filename= parameter cannot carry a non-ASCII
	// name, and the filename here is arbitrary operator input.
	w.Header().Set("Content-Disposition", `attachment; filename*=UTF-8''`+url.PathEscape(doc.File.FileName))

	if _, err := io.Copy(w, body); err != nil {
		// Headers are already sent, so there is no status code left to change
		// — the truncated response is the only signal the client gets.
		h.logUnexpected("stream document file", err)
	}
}

func (h *documentFileHandler) logUnexpected(msg string, err error) {
	if h.log == nil {
		return
	}
	h.log.Error(msg, "err", err)
}

// writeFileError maps domain and ticket errors onto status codes. The mapping
// is explicit rather than a default-500 with a message, because the browser
// branches on the status: 409 is "supersede and re-upload", 413 is "choose a
// smaller file", 403 is "your ticket expired, mint another and retry" — three
// different next actions that a single generic failure would collapse.
func writeFileError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, commands.ErrTicketRequired),
		errors.Is(err, domain.ErrFileNameRequired),
		errors.Is(err, domain.ErrContentTypeRequired),
		errors.Is(err, domain.ErrFileEmpty):
		status = http.StatusBadRequest
	case errors.Is(err, filetickets.ErrUnknownTicket),
		errors.Is(err, filetickets.ErrWrongDirection):
		status = http.StatusForbidden
	case errors.Is(err, domain.ErrDocumentNotFound),
		errors.Is(err, domain.ErrDocumentFileMissing):
		status = http.StatusNotFound
	case errors.Is(err, domain.ErrDocumentFileAlreadyAttached),
		errors.Is(err, domain.ErrDocumentSuperseded):
		status = http.StatusConflict
	case errors.Is(err, domain.ErrFileTooLarge):
		status = http.StatusRequestEntityTooLarge
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	//nolint:errcheck // nothing useful remains once the status is written
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
