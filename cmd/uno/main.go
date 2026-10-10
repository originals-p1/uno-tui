// Command uno is the cross-platform Terminal UNO entry point.
//
// v1 delivers a single binary that can both host a room and join one.
// The full host/join CLI is implemented in Task 7; this skeleton establishes
// the binary shape so the module builds.
package main

import (
	"fmt"
	"os"
)

const version = "0.1.0-dev"

func main() {
	fmt.Fprintf(os.Stderr, "uno %s: CLI not yet wired up (see internal tasks).\n", version)
	os.Exit(2)
}
