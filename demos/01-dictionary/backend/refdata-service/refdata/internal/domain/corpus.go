package domain

import "errors"

type CorpusStatus string

const (
	CorpusDraft      CorpusStatus = "draft"
	CorpusPublished  CorpusStatus = "published"
	CorpusRolledBack CorpusStatus = "rolled-back"
)

var (
	ErrDraftAlreadyExists      = errors.New("a draft already exists for this context")
	ErrOnlyDraftCanPublish     = errors.New("only a draft corpus version can be published")
	ErrRollbackTargetNotPublic = errors.New("rollback target must be a published corpus version")
	ErrDraftNotFound           = errors.New("no draft corpus version exists for this context")
)

// CorpusVersion is an immutable, flattened corpus snapshot once published.
type CorpusVersion struct {
	Context            string       `json:"context"`
	Version            int          `json:"version"`
	Status             CorpusStatus `json:"status"`
	ParentVersion      *int         `json:"parentVersion,omitempty"`
	BaseContextVersion *int         `json:"baseContextVersion,omitempty"`
	Notes              string       `json:"notes"`
	RolledBackBy       *int         `json:"rolledBackBy,omitempty"`
}

func CanCreateDraft(versions []CorpusVersion) error {
	for _, version := range versions {
		if version.Status == CorpusDraft {
			return ErrDraftAlreadyExists
		}
	}
	return nil
}

func (v CorpusVersion) CanPublish() error {
	if v.Status != CorpusDraft {
		return ErrOnlyDraftCanPublish
	}
	return nil
}

func CanRollbackTo(v CorpusVersion) error {
	if v.Status != CorpusPublished {
		return ErrRollbackTargetNotPublic
	}
	return nil
}

// CorpusDiffChange is deliberately just "what key changed," per the current
// scope for audit/diff visibility (§6.2 — enriched later if ever needed).
type CorpusDiffChange string

const (
	DiffAdded   CorpusDiffChange = "added"
	DiffRemoved CorpusDiffChange = "removed"
	DiffChanged CorpusDiffChange = "changed"
)

// CorpusDiffEntry is one changed key between two corpus versions.
type CorpusDiffEntry struct {
	TypeKey string           `json:"typeKey"`
	Code    string           `json:"code"`
	Change  CorpusDiffChange `json:"change"`
}
