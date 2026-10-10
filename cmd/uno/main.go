// Command uno is the cross-platform Terminal UNO entry point (readme §7).
//
// It provides two modes:
//
//	uno host --port 7777 --name Alice --room-key-file ./room.key
//	uno join 192.168.1.10:7777 --name Bob --room-key-file ./room.key
//
// The room key is never accepted as a plaintext CLI flag, never logged, and
// only ever kept on disk in files the OS marks owner-only.
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

const version = "0.1.0-dev"

// defaultPort is applied when a join address omits a port (readme §7.2).
const defaultPort = 7777

func main() {
	code := 0
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "uno:", err)
		code = 1
	}
	os.Exit(code)
}

// run parses args and dispatches to host/join. It is the testable core that
// never calls os.Exit.
func run(ctx context.Context, args []string, out, errOut io.Writer) error {
	if len(args) == 0 {
		printUsage(out)
		return nil
	}
	switch args[0] {
	case "-h", "--help", "help":
		printUsage(out)
		return nil
	case "host":
		return runHost(ctx, args[1:], out, errOut)
	case "join":
		return runJoin(ctx, args[1:], out, errOut)
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
  uno host [flags]                Host a room and join it as a player
  uno join <HOST[:PORT]> [flags]  Join a room (default port %d)
  uno help                        Show this help

Flags (both modes):
  --name string          Player name (required)
  --port int             Listen port for host (default %d)
  --room-key-file path   Room key file; host creates it if missing,
                         join reads it (otherwise prompted, no echo)
`, version, defaultPort, defaultPort)
}
