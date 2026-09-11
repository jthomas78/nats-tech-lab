package main

// The domain. No NATS, no JSON transport concerns, no I/O -- CLAUDE.md's
// hexagonal rule, kept even at this size. A business rule that lives in a
// handler is a business rule nobody can test on its own.

import "errors"

// ErrNonPositiveKm is BR-OD01. A trip of zero or less is not a trip.
//
// It matters more than it looks. Without it a replay of a corrupt event
// silently lowers a total, and a total that can go down is a total nobody
// trusts. The odometer only ever counts up.
var ErrNonPositiveKm = errors.New("km must be greater than 0")

// Travelled is one reported trip.
type Travelled struct {
	Km float64 `json:"km"`
}

// Validate enforces BR-OD01.
func (t Travelled) Validate() error {
	if t.Km <= 0 {
		return ErrNonPositiveKm
	}
	return nil
}

// Odometer is the read model: what we know about one vehicle right now.
//
// It is a projection, not an aggregate. Nothing rehydrates from it and no
// rule is checked against it. The log in the ODOMETER stream is the truth;
// this is the fast answer to "how far has V1 gone".
type Odometer struct {
	TotalKm float64 `json:"totalKm"`
	Trips   int     `json:"trips"`
}

// Apply folds one valid event into the read model. Callers validate first.
func (o Odometer) Apply(t Travelled) Odometer {
	return Odometer{
		TotalKm: o.TotalKm + t.Km,
		Trips:   o.Trips + 1,
	}
}
