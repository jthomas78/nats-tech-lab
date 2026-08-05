package accounts

import "testing"

// usageFor sums state.consumer_count across an account's stream_detail —
// the value the Admin UI's Accounts panel now renders as a fourth
// used/limit column alongside Memory/Disk/Streams (previously always
// reported Used: 0, since consumer count wasn't being read from /jsz at
// all).
func TestUsageForSumsConsumerCountAcrossStreams(t *testing.T) {
	a := Account{Name: "acme", JSMaxMem: 100, JSMaxFile: 200, JSMaxStreams: 5, JSMaxConsumers: 10}
	stats := jszAccount{
		Memory: 42,
		Store:  84,
		StreamDetails: []jszStreamDetail{
			{State: jszStreamState{Consumers: 3}},
			{State: jszStreamState{Consumers: 4}},
		},
	}

	got := usageFor(a, stats)

	want := JSUsage{
		Name:      "acme",
		Streams:   JSCounter{Used: 2, Limit: 5},
		Consumers: JSCounter{Used: 7, Limit: 10},
		Mem:       JSCounter{Used: 42, Limit: 100},
		File:      JSCounter{Used: 84, Limit: 200},
	}
	if got != want {
		t.Fatalf("usageFor() = %+v, want %+v", got, want)
	}
}

func TestUsageForZeroStreamsYieldsZeroConsumers(t *testing.T) {
	a := Account{Name: "empty", JSMaxStreams: 5, JSMaxConsumers: 10}

	got := usageFor(a, jszAccount{})

	if got.Streams.Used != 0 || got.Consumers.Used != 0 {
		t.Fatalf("expected zero usage for an account with no live stream_detail, got %+v", got)
	}
}

// FetchAll must be able to tell "SYS has no JetStream at all" apart from
// "this tenant has JetStream but is currently idle" — both would otherwise
// join into an indistinguishable 0/0, which the Admin UI's Accounts panel
// rendered as a misleading "0/0" instead of "N/A" for the SYS row.
func TestJSEnabled(t *testing.T) {
	cases := []struct {
		name string
		a    Account
		want bool
	}{
		{"all limits zero (SYS today)", Account{Name: "sys"}, false},
		{"only mem set", Account{Name: "x", JSMaxMem: 1}, true},
		{"only file set", Account{Name: "x", JSMaxFile: 1}, true},
		{"only streams set", Account{Name: "x", JSMaxStreams: 1}, true},
		{"only consumers set", Account{Name: "x", JSMaxConsumers: 1}, true},
		{"all limits set", Account{Name: "acme", JSMaxMem: 1, JSMaxFile: 1, JSMaxStreams: 1, JSMaxConsumers: 1}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := jsEnabled(c.a); got != c.want {
				t.Fatalf("jsEnabled(%+v) = %v, want %v", c.a, got, c.want)
			}
		})
	}
}
