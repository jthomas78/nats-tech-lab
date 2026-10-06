// The T4 playground's control service. See ../PLAYGROUND-PLAN.md.
//
// Order of work, step 3: only the pure parts exist so far — process identity
// (proc.go), the meta summary (summary.go), the transition timer
// (transitions.go) and the gateway arrows (gateways.go). `serve` arrives in
// step 4.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "playground: `serve` is not built yet (PLAYGROUND-PLAN.md, order of work step 4)")
	os.Exit(2)
}
