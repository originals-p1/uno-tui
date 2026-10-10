package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
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

// parseFlags parses the shared flags for a subcommand, accepting the address
// and flags in any order (readme §2 uses "join <addr> --name …"). A hand-rolled
// scan is used because Go's flag package stops at the first positional arg,
// which would drop flags placed after the address.
func parseFlags(args []string, joined bool) (*cliOpts, error) {
	o := &cliOpts{port: defaultPort}
	var positional []string
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "--port":
			v, err := flagValue(args, &i, "--port")
			if err != nil {
				return nil, err
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("invalid --port %q", v)
			}
			o.port = n
		case strings.HasPrefix(a, "--port="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--port="))
			if err != nil {
				return nil, fmt.Errorf("invalid --port %q", a)
			}
			o.port = n
		case a == "--name":
			v, err := flagValue(args, &i, "--name")
			if err != nil {
				return nil, err
			}
			o.name = v
		case strings.HasPrefix(a, "--name="):
			o.name = strings.TrimPrefix(a, "--name=")
		case a == "--room-key-file":
			v, err := flagValue(args, &i, "--room-key-file")
			if err != nil {
				return nil, err
			}
			o.roomKeyFile = v
		case strings.HasPrefix(a, "--room-key-file="):
			o.roomKeyFile = strings.TrimPrefix(a, "--room-key-file=")
		default:
			if strings.HasPrefix(a, "-") && a != "-" {
				return nil, fmt.Errorf("unknown flag %q", a)
			}
			positional = append(positional, a)
		}
		i++
	}
	if joined {
		if len(positional) == 0 {
			return nil, errors.New("join requires a server address")
		}
		o.addr = positional[0]
	}
	return o, nil
}

// flagValue returns the value for a separated flag, advancing i past it.
func flagValue(args []string, i *int, name string) (string, error) {
	*i++
	if *i >= len(args) {
		return "", fmt.Errorf("%s requires a value", name)
	}
	return args[*i], nil
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
	if o.roomKeyFile == "" {
		fmt.Fprintln(out, "no room key (default); anyone on the network can join. Use --room-key-file to run keyed.")
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
	fmt.Fprintf(out, "connected as host player %q\n", o.name)

	if isTerminal(os.Stdout) {
		if err := runInteractive(local, o.name); err != nil {
			return err
		}
	} else {
		select {
		case <-ctx.Done():
		case <-local.ConnectionLost():
		}
	}
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
	key, err := joinRoomKey(o.roomKeyFile)
	if err != nil {
		return err
	}
	c := client.NewClient(client.Config{
		Address: target, Name: o.name, RoomKey: key,
	})
	if err := c.Connect(target); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer c.Close()
	fmt.Fprintf(out, "connected to %s as player %q\n", target, o.name)

	if isTerminal(os.Stdout) {
		if err := runInteractive(c, o.name); err != nil {
			return err
		}
	} else {
		select {
		case <-ctx.Done():
		case <-c.ConnectionLost():
		}
	}
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
