package postgres

import (
	"context"

	"github.com/jthomas78/nats-tech-lab/demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain"
)

// Sources answers "how did each entry get here", read out of the audit trail
// rather than out of the entries table (decision 80, BR-AS43).
//
// Three things in the query are the rule rather than the implementation:
//
//   - DISTINCT ON with ORDER BY id ASC takes the FIRST row per entry, which
//     is the creating write. The latest actor answers a different question,
//     and answering it here would relabel an announced plugin as curated the
//     moment an operator disabled it.
//   - outcome = accepted, because a refused write created nothing. An entry
//     whose first *attempt* was refused was still created by whoever's write
//     went through.
//   - scope = registry, because the trust table shares this table and a
//     publisher-key write is not the creation of any entry.
//
// Ids with no accepted row simply do not appear; the caller maps that to
// SourceUnknown rather than guessing.
func (s *Store) Sources(ctx context.Context) (map[string]domain.Registration, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT ON (entry_id) entry_id, actor
		   FROM registry.audit
		  WHERE outcome = $1 AND scope = $2
		  ORDER BY entry_id, id ASC`,
		domain.AuditAccepted, auditScopeRegistry)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]domain.Registration{}
	for rows.Next() {
		var id, actor string
		if err := rows.Scan(&id, &actor); err != nil {
			return nil, err
		}
		out[id] = domain.RegistrationOf(actor)
	}
	return out, rows.Err()
}

// auditScopeRegistry is the default the column was added with — the counter a
// plugin write moves. Named here rather than spelled inline so the two scopes
// read as a pair beside auditScopePublisher.
const auditScopeRegistry = "registry"
