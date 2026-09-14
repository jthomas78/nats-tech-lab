package main

// The domain. No NATS, no JSON transport, no I/O.
//
// Everything in this file is a rule or a fold. A rule that lives in a handler
// is a rule nobody can test on its own, and this demo's whole claim is that
// the write side checks rules the read side never sees.
//
// Two types here look similar and are not:
//
//   Vehicle  -- the AGGREGATE. The write side rehydrates it to decide whether
//               a command is allowed. It holds only what a rule reads.
//   Odometer -- the READ MODEL. Nothing is decided from it. It holds what a
//               query wants to print.
//
// Vehicle deliberately has no TotalKm. No rule reads a total, so carrying one
// in the aggregate would blur the split this demo exists to show.

import (
	"errors"
	"time"
)

// Business rules. One error per rule, so a test names the rule it proves.
var (
	// ErrNonPositiveKm is BR-OD01. A trip of zero or less is not a trip.
	//
	// It matters more than it looks. Without it a replay of a corrupt event
	// silently lowers a total, and a total that can go down is a total
	// nobody trusts. The odometer only ever counts up.
	ErrNonPositiveKm = errors.New("km must be greater than 0")

	// ErrNotRegistered is BR-OD02 and BR-OD05. An unknown vehicle cannot
	// travel and cannot be retired.
	ErrNotRegistered = errors.New("vehicle is not registered")

	// ErrAlreadyRegistered is BR-OD03.
	ErrAlreadyRegistered = errors.New("vehicle is already registered")

	// ErrRetired is BR-OD04. A retired vehicle refuses further trips, and
	// cannot be retired a second time.
	ErrRetired = errors.New("vehicle is retired")
)

// Status is the vehicle lifecycle. Three values, and the zero value means
// "we have never seen this vehicle" -- which is what a replay of an empty
// stream must produce.
type Status string

const (
	StatusUnknown    Status = ""
	StatusRegistered Status = "registered"
	StatusRetired    Status = "retired"
)

// ---------------------------------------------------------------- commands

// RegisterVehicle puts a vehicle into service.
type RegisterVehicle struct {
	Plate string
}

// RecordTrip reports one completed trip.
type RecordTrip struct {
	Km float64
}

// RetireVehicle takes a vehicle out of service for good.
type RetireVehicle struct {
	Reason string
}

// ------------------------------------------------------------------ events

// Event is one fact that already happened. It can be refused before it is
// written and never after, so an Apply never returns an error.
type Event interface {
	// EventType is the last token of the subject it is published on.
	EventType() string

	applyToVehicle(Vehicle) Vehicle
	applyToOdometer(Odometer, time.Time) Odometer
}

// Registered — the vehicle entered service.
type Registered struct {
	Plate string `json:"plate"`
}

// Travelled — the vehicle completed one trip.
type Travelled struct {
	Km float64 `json:"km"`
}

// Retired — the vehicle left service.
type Retired struct {
	Reason string `json:"reason"`
}

func (Registered) EventType() string { return "registered" }
func (Travelled) EventType() string  { return "travelled" }
func (Retired) EventType() string    { return "retired" }

// --------------------------------------------------------------- aggregate

// Vehicle is the write-side aggregate: the state a command is judged against.
type Vehicle struct {
	Status Status `json:"status"`
	Plate  string `json:"plate"`
}

// Register enforces BR-OD03.
//
// A retired vehicle counts as already registered. It has been registered once
// already, and "twice" is what BR-OD03 forbids.
func (v Vehicle) Register(c RegisterVehicle) (Event, error) {
	if v.Status != StatusUnknown {
		return nil, ErrAlreadyRegistered
	}
	return Registered{Plate: c.Plate}, nil
}

// Travel enforces BR-OD01, BR-OD02 and BR-OD04.
//
// Order matters for the message the caller sees. A retired vehicle is told it
// is retired, not that it is unregistered -- the two need different fixes.
func (v Vehicle) Travel(c RecordTrip) (Event, error) {
	switch v.Status {
	case StatusUnknown:
		return nil, ErrNotRegistered
	case StatusRetired:
		return nil, ErrRetired
	}
	if c.Km <= 0 {
		return nil, ErrNonPositiveKm
	}
	return Travelled{Km: c.Km}, nil
}

// Retire enforces BR-OD05.
func (v Vehicle) Retire(c RetireVehicle) (Event, error) {
	switch v.Status {
	case StatusUnknown:
		return nil, ErrNotRegistered
	case StatusRetired:
		return nil, ErrRetired
	}
	return Retired{Reason: c.Reason}, nil
}

// Apply folds one event into the aggregate. This is rehydration, one step at
// a time -- the same call whether the event came from a snapshot's tail or
// from sequence 1.
func (v Vehicle) Apply(e Event) Vehicle { return e.applyToVehicle(v) }

func (e Registered) applyToVehicle(v Vehicle) Vehicle {
	return Vehicle{Status: StatusRegistered, Plate: e.Plate}
}

// A trip changes nothing the write side decides on. That is not a gap: it is
// why a snapshot helps so much here, and why the read model is a separate
// type.
func (Travelled) applyToVehicle(v Vehicle) Vehicle { return v }

func (Retired) applyToVehicle(v Vehicle) Vehicle {
	v.Status = StatusRetired
	return v
}

// -------------------------------------------------------------- read model

// Odometer is the read model: what we know about one vehicle right now.
//
// It is a projection, not an aggregate. Nothing rehydrates from it and no
// rule is checked against it. The log in the ODOMETER stream is the truth;
// this is the fast answer to "how far has V1 gone".
type Odometer struct {
	Status     Status    `json:"status"`
	Plate      string    `json:"plate"`
	TotalKm    float64   `json:"totalKm"`
	Trips      int       `json:"trips"`
	LastTripAt time.Time `json:"lastTripAt"`
}

// Apply folds one event into the read model. `at` is the event's stream
// timestamp, not the wall clock -- a re-projection must produce the same
// answer as the first projection did.
func (o Odometer) Apply(e Event, at time.Time) Odometer { return e.applyToOdometer(o, at) }

func (e Registered) applyToOdometer(o Odometer, _ time.Time) Odometer {
	return Odometer{Status: StatusRegistered, Plate: e.Plate}
}

func (e Travelled) applyToOdometer(o Odometer, at time.Time) Odometer {
	o.TotalKm += e.Km
	o.Trips++
	o.LastTripAt = at
	return o
}

func (Retired) applyToOdometer(o Odometer, _ time.Time) Odometer {
	o.Status = StatusRetired
	return o
}
