package domain

import "time"

const (
	StreamName = "DICTIONARY"

	SubjectEntryCreated = "DICTIONARY.entry.created"
	SubjectEntryUpdated = "DICTIONARY.entry.updated"

	// SubjectWildcard matches every dictionary entry event.
	SubjectWildcard = "DICTIONARY.entry.*"
)

// StreamSubjects lists the subjects bound to the DICTIONARY stream.
func StreamSubjects() []string {
	return []string{SubjectEntryCreated, SubjectEntryUpdated}
}

// EntryEvent is the payload published on entry.created / entry.updated.
type EntryEvent struct {
	Entry      DictionaryEntry `json:"entry"`
	OccurredAt time.Time       `json:"occurredAt"`
}
