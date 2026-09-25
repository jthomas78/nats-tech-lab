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
	"fmt"
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

	// ErrOutOfOrder is BR-OD08. An event arrived behind a fold that has
	// already moved past it.
	//
	// A fold's position never moves backwards, so this cannot be repaired by
	// retrying: the same event would be refused again, for ever. The caller
	// terminates the message instead of nak'ing it, and the loss is counted.
	ErrOutOfOrder = errors.New("event is behind the fold")

	// ErrUndecodable is BR-OD09. The event is in the log and this code
	// cannot read it -- an unknown event type, a subject of the wrong
	// shape, or a body that does not fit the type its subject promises.
	//
	// The stream filter is `evt.odometer.>`, which is wider than the subject
	// this code writes. So one `nats pub evt.odometer.oops` puts an event in
	// the log that every fold will choke on, for ever.
	ErrUndecodable = errors.New("event cannot be decoded")
)

// MalformedHistoryError is BR-OD09 with the one fact a reader needs to act:
// WHERE the log stopped being readable. rehydrate() wraps the decode failure
// in it because rehydrate is the only place that holds the sequence when the
// fold stops.
//
// It unwraps to the decode error, so errors.Is(err, ErrUndecodable) and
// Permanent() still hold. This is a better REPORT of the same stop. Whether a
// fold should stop or skip is a separate decision, and this type changes
// neither.
type MalformedHistoryError struct {
	Seq uint64 // stream sequence of the event that could not be decoded
	Err error  // the decode failure; wraps ErrUndecodable
}

func (e *MalformedHistoryError) Error() string {
	return fmt.Sprintf("history is malformed at seq %d: %v", e.Seq, e.Err)
}

func (e *MalformedHistoryError) Unwrap() error { return e.Err }

// Permanent reports whether a failure is one that retrying cannot fix.
//
// This is the difference between a bad EVENT and a bad MOMENT. NATS being
// unreachable is a bad moment: the event was always valid and a retry gets
// it. An event whose type nobody knows is a bad event: the bytes are in the
// log for ever and the tenth delivery reads exactly like the first.
//
// A caller nak's a bad moment and terminates a bad event. Getting this
// backwards is how a demo hangs: BR-OD06..08 already showed that a fold
// which nak's what it can never apply redelivers the same message until
// somebody notices, and with MaxAckPending 1 nothing behind it moves either.
//
// There is deliberately NO MaxDeliver backstop on any consumer in this demo.
// A server that gives up after N tries tells nobody -- the client never hears
// it -- and silent loss is the exact thing BR-OD08 exists to end. A message
// is dropped here by a fold that says why, in a log line, or not at all.
func Permanent(err error) bool {
	return errors.Is(err, ErrUndecodable) || errors.Is(err, ErrOutOfOrder)
}

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

// -------------------------------------------------------------------- fold

// Fold is the position of a projection in the log. It is not a projection.
//
// It holds a sequence and nothing else -- no vehicle, no total. Its one job
// is to answer whether an event that is already in the log may be applied to
// whatever the caller is building, and that answer is BR-OD06..08.
//
// Both KV documents embed one of these, which is why the position and the
// projected state are written together in a single value. A position stored
// apart from the state it describes can disagree with it after a crash.
type Fold struct {
	// LastSeq is the stream sequence of the last event applied. Zero means
	// nothing has been applied yet, which is what a first event must see.
	LastSeq uint64 `json:"lastSeq"`
}

// Next reports whether the event at seq may be applied.
//
//	seq >  LastSeq  ->  true,  nil            BR-OD06  a new fact
//	seq == LastSeq  ->  false, nil            BR-OD07  a redelivery; ack it
//	seq <  LastSeq  ->  false, ErrOutOfOrder  BR-OD08  too late; count it
//
// The gap between the two false answers is the point of the rule. Before
// phase 04.7 both were `return nil`, so an event that arrived too late was
// acked and never applied, and the projection was short for ever with no
// error anywhere. A watermark makes redelivery safe. It does not make
// reordering safe -- it makes reordering silent.
//
// Next decides nothing about ordering itself. It reports what already
// happened, and a caller that runs a pool of workers over one consumer will
// hear about it.
func (f Fold) Next(seq uint64) (bool, error) {
	switch {
	case seq > f.LastSeq:
		return true, nil
	case seq == f.LastSeq:
		return false, nil
	default:
		return false, fmt.Errorf("%w: sequence %d arrived after %d", ErrOutOfOrder, seq, f.LastSeq)
	}
}

// Advance moves the position to seq.
//
// It is a separate call from Next because the caller must write the new
// state and the new position as one value. Folding inside Next would move
// the position even when the write that follows it fails.
func (f Fold) Advance(seq uint64) Fold {
	f.LastSeq = seq
	return f
}
