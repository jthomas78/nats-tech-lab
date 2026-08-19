package accounts

import (
	"net/http"

	"github.com/nats-io/jwt/v2"
)

// Edge health states for topologyEdge.Status — BR-AC22, BR-AC24, BR-AC25.
const (
	topologyMatched        = "matched"
	topologyNoExport       = "no-export"
	topologyTokenRequired  = "token-required"
	topologyUnknownAccount = "unknown-account"
)

// topologyEdge is one account's import of another account's export — the
// unit the Admin UI's Topology panel draws as a single directed line.
// FromAccount is the exporter (data/service origin), ToAccount the importer
// (consumer), matching the direction data actually flows in NATS's
// export/import model regardless of which account initiated the request
// (a service import still flows request→response, but the subject
// ownership — and so the line's direction here — is the exporter's).
//
// Status reports whether the import is actually satisfiable, not just
// declared (BR-AC22/BR-AC24/BR-AC25) — an import with no matching export is
// reported, never silently omitted, since that's precisely the state a
// diagram that only ever showed declared imports could never surface.
type topologyEdge struct {
	FromAccount  string `json:"fromAccount"`
	ToAccount    string `json:"toAccount"`
	Subject      string `json:"subject"`
	LocalSubject string `json:"localSubject,omitempty"`
	Type         string `json:"type"` // "service" | "stream"
	Status       string `json:"status"`
}

// unconsumedExport is an export that no known account currently imports —
// BR-AC23. Reported separately from topologyEdge rather than as a graph
// edge, since it has no importer endpoint to draw a line to.
type unconsumedExport struct {
	Account string `json:"account"`
	Subject string `json:"subject"`
	Type    string `json:"type"`
}

type topologyResponse struct {
	Edges             []topologyEdge     `json:"edges"`
	UnconsumedExports []unconsumedExport `json:"unconsumedExports"`
}

// matchExport implements the subject+type containment check behind
// BR-AC22: imp is satisfiable by exports[i] when the types agree and imp's
// subject is contained in the export's subject. jwt.Subject.IsContainedIn
// is the same wildcard-aware containment nsc itself uses to validate an
// import against an export (see nats-io/jwt/v2's own Exports.
// HasExportContainingSubject, which this mirrors plus the type check that
// helper omits) — not a bespoke matcher. Returns -1 if nothing matches.
func matchExport(imp *jwt.Import, exports jwt.Exports) int {
	for i, exp := range exports {
		if exp == nil || exp.Type != imp.Type {
			continue
		}
		if imp.Subject.IsContainedIn(exp.Subject) {
			return i
		}
	}
	return -1
}

// importStatus classifies one import against its exporter's live claims.
// exporterRecognized is whether imp.Account is a known account at all
// (BR-AC25 — an import naming an account outside this deployment is
// reported as unknown-account, not dropped). exporterClaims may be nil even
// when the exporter is recognized, if its own claims lookup failed
// elsewhere; that's treated the same as no-export (BR-AC22) rather than
// invented a third "can't tell" state, since either way no export can be
// confirmed to satisfy the import. matchedIdx is the satisfying export's
// index in exporterClaims.Exports, or -1, so the caller can mark that
// export consumed for BR-AC23.
func importStatus(imp *jwt.Import, exporterClaims *jwt.AccountClaims, exporterRecognized bool) (status string, matchedIdx int) {
	if !exporterRecognized {
		return topologyUnknownAccount, -1
	}
	if exporterClaims == nil {
		return topologyNoExport, -1
	}
	idx := matchExport(imp, exporterClaims.Exports)
	if idx == -1 {
		return topologyNoExport, -1
	}
	if exporterClaims.Exports[idx].TokenReq && imp.Token == "" {
		return topologyTokenRequired, idx
	}
	return topologyMatched, idx
}

// listTopology reads every account's *live* resolver JWT (not the
// bootstrap-time convention baked into tenantImports — see provisioner.go)
// via Provisioner.LookupAccountClaims, so the graph reflects reality even if
// an account's imports were hand-edited or diverge from the standard tenant
// shape in the future. Accounts with no resolver JWT (shouldn't happen for
// an active account, but SYS/PLATFORM/tenants all have one) are skipped
// with a warning rather than failing the whole response — one account's
// lookup failure shouldn't blank the diagram for every other account.
// @Summary      Cross-account export/import topology
// @Description  Every declared export/import edge across all accounts, read from resolver JWTs, plus exports no known account currently imports (BR-AC23).
// @Tags         accounts
// @Produce      json
// @Success      200  {object}  topologyResponse
// @Failure      500  {object}  errorResponse
// @Router       /api/accounts/topology [get]
func (h *Handlers) listTopology(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accs, err := h.Store.List(ctx)
	if err != nil {
		h.Log.Error("list accounts for topology", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	nameByPub := make(map[string]string, len(accs))
	for _, a := range accs {
		nameByPub[a.PublicKey] = a.Name
	}

	claimsByPub := make(map[string]*jwt.AccountClaims, len(accs))
	for _, a := range accs {
		claims, err := h.Provisioner.LookupAccountClaims(ctx, a.PublicKey)
		if err != nil {
			h.Log.Warn("topology: lookup account claims", "account", a.Name, "err", err)
			continue
		}
		claimsByPub[a.PublicKey] = claims
	}

	// consumed[exporterPub][exportIndex] — every export any import actually
	// matched, across all accounts, so the second pass below (BR-AC23) can
	// tell which of an exporter's own declared exports nobody imports.
	consumed := make(map[string]map[int]bool, len(claimsByPub))

	edges := make([]topologyEdge, 0)
	for _, a := range accs {
		claims, ok := claimsByPub[a.PublicKey]
		if !ok {
			continue
		}
		for _, imp := range claims.Imports {
			fromName, exporterRecognized := nameByPub[imp.Account]
			if !exporterRecognized {
				fromName = imp.Account // exporter outside this deployment's known accounts — show the raw pubkey rather than drop the edge
			}
			status, matchedIdx := importStatus(imp, claimsByPub[imp.Account], exporterRecognized)
			if matchedIdx != -1 {
				if consumed[imp.Account] == nil {
					consumed[imp.Account] = make(map[int]bool)
				}
				consumed[imp.Account][matchedIdx] = true
			}
			edges = append(edges, topologyEdge{
				FromAccount:  fromName,
				ToAccount:    a.Name,
				Subject:      string(imp.Subject),
				LocalSubject: string(imp.LocalSubject),
				Type:         imp.Type.String(),
				Status:       status,
			})
		}
	}

	unconsumed := make([]unconsumedExport, 0)
	for _, a := range accs {
		claims, ok := claimsByPub[a.PublicKey]
		if !ok {
			continue
		}
		for i, exp := range claims.Exports {
			if consumed[a.PublicKey][i] {
				continue
			}
			unconsumed = append(unconsumed, unconsumedExport{
				Account: a.Name,
				Subject: string(exp.Subject),
				Type:    exp.Type.String(),
			})
		}
	}

	writeJSON(w, http.StatusOK, topologyResponse{Edges: edges, UnconsumedExports: unconsumed})
}
