package domain

import "time"

const (
	StreamName = "DICTIONARY"

	SubjectShipArrived   = "DICTIONARY.ship.arrived"
	SubjectShipDeparted  = "DICTIONARY.ship.departed"
	SubjectCargoLoaded   = "DICTIONARY.cargo.loaded"
	SubjectCargoUnloaded = "DICTIONARY.cargo.unloaded"

	// SubjectWildcard matches every shipping event in the DICTIONARY stream.
	SubjectWildcard = "DICTIONARY.>"
)

// StreamSubjects lists the four subjects bound to the DICTIONARY stream.
func StreamSubjects() []string {
	return []string{
		SubjectShipArrived,
		SubjectShipDeparted,
		SubjectCargoLoaded,
		SubjectCargoUnloaded,
	}
}

// ShipEvent is the envelope published on every ship subject. Only the fields
// relevant to the subject are populated; the rest are zero values.
type ShipEvent struct {
	Context    string    `json:"context"`              // fleet / KV-bucket qualifier
	ShipID     string    `json:"shipID"`               // stable machine identifier
	ShipName   string    `json:"shipName,omitempty"`   // carried on .arrived for replay
	Port       string    `json:"port,omitempty"`       // .arrived / .departed
	Cargo      *Cargo    `json:"cargo,omitempty"`      // .loaded / .unloaded
	OccurredAt time.Time `json:"occurredAt"`
}
