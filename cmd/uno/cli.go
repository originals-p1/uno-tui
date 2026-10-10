package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"

	"terminal-uno/internal/client"
	"terminal-uno/internal/server"
)

// cliOpts are the parsed shared flags for host and join.
type cliOpts struct {
	port        int
	name        string
	roomKeyFile string
	addr        string // join target (may lack port)
}

// parseFlags parses the shared flags for a subcommand. joinAddr is non-empty
// only for the join subcommand.
func parseFlags(args []string, joined bool) (*cliOpts, error) {
	fs := flag.NewFlagSet("uno", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // we render errors ourselves
	port := fs.Int("port", defaultPort, "")
	name := fs.String("name", "", "")
	key := fs.String("room-key-file", "", "")
	_ = fs.Parse(args)
	o := &cliOpts{port: *port, name: *name, roomKeyFile: *key}
	if joined {
		rest := fs.Args()
		if len(rest) == 0 {
			return nil, errors.New("join requires a server address")
		}
		o.addr = rest[0]
	}
	return o, nil
}

// joinAddress adds the default port when the target omits it (readme §7.2).
func joinAddress(hostPort string) (string, error) {
	h := strings.TrimSpace(hostPort)
	if h == "" {
		return "", errors.New("empty address")
	}
	if _, _, err := net.SplitHostPort(h); err == nil {
		return h, nil
	}
	// Treat as bare host, append default port.
	return net.JoinHostPort(h, strconv.Itoa(defaultPort)), nil
}

// runHost parses host mode, starts the server, and connects the local host
// client, then stays alive until ctx is cancelled (graceful shutdown).
func runHost(ctx context.Context, args []string, out, errOut io.Writer) error {
	o, err := parseFlags(args, false)
	if err != nil {
		return err
	}
	if o.name == "" {
		return errors.New("--name is required when hosting")
	}
	key, created, err := hostRoomKey(o.roomKeyFile, out)
	if err != nil {
		return err
	}
	if created {
		fmt.Fprintf(out, "wrote new room key to %s\n", o.roomKeyFile)
	}
	hbt, err := newHostBootstrapToken()
	if err != nil {
		return err
	}
	cfg := server.DefaultConfig()
	cfg.RoomKey = key
	cfg.HostBootstrapToken = hbt
	s := server.NewServer(cfg, server.NewRealClock(), nil)
	defer s.Close()
	ln, err := s.Listen(fmt.Sprintf("0.0.0.0:%d", o.port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	fmt.Fprintf(out, "uno: hosting on 0.0.0.0:%d\n", port)
	fmt.Fprintf(out, "  players join with:  uno join <THIS-IP>:%d --name <name> --room-key-file <key file>\n", port)

	local := client.NewClient(client.Config{
		Address: fmt.Sprintf("127.0.0.1:%d", port),
		Name:    o.name, RoomKey: key, HostBootstrapToken: hbt,
	})
	if err := local.Connect(fmt.Sprintf("127.0.0.1:%d", port)); err != nil {
		return fmt.Errorf("connect to own room: %w", err)
	}
	defer local.Close()
	fmt.Fprintf(out, "connected as host player %q (waiting; Ctrl-C to quit)\n", o.name)

	<-ctx.Done()
	fmt.Fprintln(out, "shutting down")
	return nil
}

// runJoin parses join mode, resolves the key (file or prompt), connects, and
// stays alive until ctx is cancelled.
func runJoin(ctx context.Context, args []string, out, errOut io.Writer) error {
	o, err := parseFlags(args, true)
	if err != nil {
		return err
	}
	if o.name == "" {
		return errors.New("--name is required when joining")
	}
	target, err := joinAddress(o.addr)
	if err != nil {
		return err
	}
	key, prompt, err := joinRoomKey(o.roomKeyFile)
	if err != nil {
		return err
	}
	if prompt {
		if !isTerminal(os.Stdin) {
			return errors.New("no --room-key-file and no terminal for a password prompt; provide the key file")
		}
		key, err = joinRoomKeyPrompt(os.Stderr, os.Stdin)
		if err != nil {
			return err
		}
	}
	c := client.NewClient(client.Config{
		Address: target, Name: o.name, RoomKey: key,
	})
	if err := c.Connect(target); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer c.Close()
	fmt.Fprintf(out, "connected to %s as player %q (Ctrl-C to quit)\n", target, o.name)

	<-ctx.Done()
	fmt.Fprintln(out, "disconnecting")
	return nil
}

// newHostBootstrapToken returns a one-time cryptographic token granting host
// identity; it exists only in this process's memory (readme §7.1).
func newHostBootstrapToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
