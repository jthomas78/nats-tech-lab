package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// What `cqrs pool` does when you type it (plan 04.8.5, decision D6).
//
// One subcommand, four jobs. Which one runs is decided HERE, as a pure
// function over the flag names that were typed, so it can be spec'd. The
// alternative -- an if-chain in main.go reading flag values -- cannot tell
// `-workers 4` (the default, typed on purpose) from `-workers` never
// mentioned, and the difference is the whole dispatch.
//
// The bare command must start NOTHING. A reader asking what the log holds has
// not asked to fold it, and a report that quietly created a durable consumer
// would change the thing it was asked to describe.

type poolAction int

const (
	// poolReport is the default, and the safe one.
	poolReport poolAction = iota
	poolSeed
	poolRemove
	poolRun
)

// ErrPoolFlagClash is a command line with no sensible reading. It is refused
// rather than resolved: guessing at `-seed -rm` would delete a log the reader
// was in the middle of building.
var ErrPoolFlagClash = errors.New("these pool flags cannot be used together")

// poolRunFlags are the flags that mean "run the pool".
//
// Any one of them is enough, and the rest keep today's defaults (D12). A
// reader who types `-drain` has asked for a drain, not for a lecture about
// -workers.
func poolRunFlags() []string {
	return []string{"workers", "max-pending", "ack-wait", "kill-at", "drain"}
}

// poolActionFor reads the flag NAMES that were typed, as
// flag.FlagSet.Visit reports them.
func poolActionFor(typed map[string]bool) (poolAction, error) {
	run := []string{}
	for _, f := range poolRunFlags() {
		if typed[f] {
			run = append(run, "-"+f)
		}
	}
	seed, rm := typed["seed"], typed["rm"]

	switch {
	case seed && rm:
		return poolReport, fmt.Errorf("%w: -seed and -rm", ErrPoolFlagClash)

	// A seed that also started a pool would have the pool folding a log
	// that was still being written, and the run would be measuring the
	// seed. A removal that also started one is the same mistake backwards.
	case (seed || rm) && len(run) > 0:
		name := "-seed"
		if rm {
			name = "-rm"
		}
		sort.Strings(run)
		return poolReport, fmt.Errorf("%w: %s and %s", ErrPoolFlagClash, name, strings.Join(run, " "))

	case rm:
		return poolRemove, nil
	case seed:
		return poolSeed, nil
	case len(run) > 0:
		return poolRun, nil
	default:
		return poolReport, nil
	}
}

// poolReportLines is what the bare command prints.
//
// Returned as lines rather than printed, so the standing rule set by the user
// 2026-09-16 -- a count is never shown without its bytes -- is something a
// spec can hold this function to instead of something a reviewer has to spot.
func poolReportLines(st PoolState) []string {
	sizes := make([]string, 0, len(st.Sizes))
	for _, n := range st.Sizes {
		sizes = append(sizes, fmt.Sprintf("%d", n))
	}

	if !st.Exists {
		return []string{
			fmt.Sprintf("%-12s not seeded", st.Stream),
			fmt.Sprintf("%-12s cqrs pool -seed %d", "build it", DefaultPoolSize),
			fmt.Sprintf("%-12s %s", "sizes", strings.Join(sizes, " / ")),
		}
	}

	out := []string{
		fmt.Sprintf("%-12s %d events · %s", st.Stream, st.Events, humanBytes(st.Bytes)),
		fmt.Sprintf("%-12s %s", "subject", st.Subject),
		fmt.Sprintf("%-12s %s — the correct fold, %d vehicles", "truth", st.TruthKV, len(st.Vehicles)),
		fmt.Sprintf("%-12s %s", "sizes", strings.Join(sizes, " / ")),
	}
	if st.ElapsedMs > 0 {
		out = append(out, fmt.Sprintf("%-12s %.0f ms", "seeded in", st.ElapsedMs))
	}
	return out
}

// printPoolState prints the report.
func printPoolState(st PoolState) {
	for _, line := range poolReportLines(st) {
		fmt.Println(line)
	}
}
