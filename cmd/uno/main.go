// Command uno is the cross-platform Terminal UNO entry point (readme §7).
//
// Running it with no arguments starts a guided, interactive wizard that asks
// for the player name, host-or-join, and the relevant settings before
// launching (recommended). Advanced users can still use the direct modes:
//
//	uno host --port 7777 --room-key-file ./room.key
//	uno join 192.168.1.10:7777 --room-key-file ./room.key
//
// Any missing setting (including --name) is asked interactively. The room key
// is never accepted as a plaintext CLI flag, never logged, and only ever kept
// on disk in files the OS marks owner-only.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

const version = "1.9.0"

// defaultPort is applied when a join address omits a port (readme §7.2).
const defaultPort = 7777

func main() {
	code := 0
	if err := run(context.Background(), os.Stdin, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "uno:", err)
		code = 1
	}
	os.Exit(code)
}

// run dispatches to the guided-startup wizard (no args) or the advanced
// host/join subcommands. It is the testable core that never calls os.Exit.
func run(ctx context.Context, in io.Reader, args []string, out, errOut io.Writer) error {
	if len(args) == 0 {
		// Guided, interactive startup (recommended).
		return runWizard(ctx, in, out, errOut)
	}
	switch args[0] {
	case "-h", "--help", "help":
		printUsage(out)
		return nil
	case "host":
		return runHost(ctx, in, args[1:], out, errOut)
	case "join":
		return runJoin(ctx, in, args[1:], out, errOut)
	default:
		fmt.Fprintf(errOut, "uno: unknown command %q\n\n", args[0])
		printUsage(out)
		return errors.New("unknown command")
	}
}

// signalContext returns a context cancelled by SIGINT/SIGTERM.
func signalContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, func() { signal.Stop(ch); cancel() }
}

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `uno %s — Terminal UNO

Usage:
  uno                 Guided, interactive startup (recommended)
  uno host [flags]    Host a room and join it as a player
  uno join <ADDR> [flags]  Join a room (default port %d)
  uno help            Show this help

In guided mode (just run "uno") you are asked for the player name, host-or-join,
the port or address, and whether to use a room key — no need to remember flags.
Any setting you leave empty uses its default; in host/join mode, a missing
--name is also asked interactively.

Optional flags (host/join):
  --name string          Player name (asked if omitted)
  --port int             Listen port for host (default %d)
  --room-key-file path   Enable keyed entry: host creates/reads it,
                         join reads it. Default is keyless.
`, version, defaultPort, defaultPort)
}
