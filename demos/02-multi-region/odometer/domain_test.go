package main

import "testing"

// BR-OD01 -- km must be greater than 0.
func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		km   float64
		ok   bool
	}{
		{"a real trip is accepted", 12.5, true},
		{"a tiny trip is accepted", 0.1, true},
		{"zero is rejected", 0, false},
		{"a negative trip is rejected", -12.5, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Travelled{Km: c.km}.Validate()
			if c.ok && err != nil {
				t.Fatalf("want accepted, got %v", err)
			}
			if !c.ok && err == nil {
				t.Fatal("want rejected, got accepted")
			}
		})
	}
}

// The odometer only counts up, and one event is one trip.
func TestApply(t *testing.T) {
	o := Odometer{}.Apply(Travelled{Km: 12.5})
	if o.TotalKm != 12.5 || o.Trips != 1 {
		t.Fatalf("after one trip want 12.5/1, got %v/%v", o.TotalKm, o.Trips)
	}

	o = o.Apply(Travelled{Km: 12.5})
	if o.TotalKm != 25 || o.Trips != 2 {
		t.Fatalf("after two trips want 25/2, got %v/%v", o.TotalKm, o.Trips)
	}
}

// This is the whole lab in one test. Apply the SAME trip twice and the total
// doubles. So a total of 25 after one publish of 12.5 is not a rounding
// question -- it is proof the event was captured twice.
func TestDoubleCaptureShowsAsDoubleTotal(t *testing.T) {
	once := Odometer{}.Apply(Travelled{Km: 12.5})
	twice := once.Apply(Travelled{Km: 12.5})

	if once.TotalKm == twice.TotalKm {
		t.Fatal("the total must distinguish one capture from two")
	}
}
