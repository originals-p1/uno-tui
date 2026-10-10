package main

import (
	"bufio"
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

// runWizard is the guided-startup entry point: it interactively collects the
// player name, the mode (host/join) and the relevant settings on stdin, then
// launches the game. It is the recommended way to start uno.
func runWizard(ctx context.Context, in io.Reader, out, errOut io.Writer) error {
	fmt.Fprintf(out, "uno %s — Terminal UNO\n\n", version)
	fmt.Fprintln(out, "（下面的选项直接回车使用默认值）")

	name, err := prompt(in, out, "你的昵称（默认取自系统用户名）: ")
	if err != nil {
		return err
	}
	if name == "" {
		name = defaultName()
	}
	o := &cliOpts{name: name, port: defaultPort}

	mode, err := prompt(in, out, "开房还是加入？[H]ost / [J]oin（默认 Host）: ")
	if err != nil {
		return err
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(mode)), "j") {
		return wizardJoin(ctx, o, in, out, errOut)
	}
	return wizardHost(ctx, o, in, out, errOut)
}

// wizardHost collects hosting settings, then launches the server + host client.
func wizardHost(ctx context.Context, o *cliOpts, in io.Reader, out, errOut io.Writer) error {
	p, err := prompt(in, out, "监听端口（默认 7777）: ")
	if err != nil {
		return err
	}
	if p != "" {
		n, aerr := strconv.Atoi(p)
		if aerr != nil {
			return fmt.Errorf("无效端口 %q", p)
		}
		o.port = n
	}
	useKey, err := prompt(in, out, "使用房间密钥文件吗？[y/N]（默认 N，无密钥）:")
	if err != nil {
		return err
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(useKey)), "y") {
		o.roomKeyFile, err = prompt(in, out, "密钥文件路径（默认 room.key）: ")
		if err != nil {
			return err
		}
		if o.roomKeyFile == "" {
			o.roomKeyFile = "room.key"
		}
	}
	return launchHost(ctx, o, out, errOut)
}

// wizardJoin collects the target address and optional key, then joins a room.
func wizardJoin(ctx context.Context, o *cliOpts, in io.Reader, out, errOut io.Writer) error {
	addr, err := prompt(in, out, "房间地址 HOST[:PORT]（默认端口 7777）: ")
	if err != nil {
		return err
	}
	if addr == "" {
		addr = "localhost"
	}
	o.addr = addr
	o.roomKeyFile, err = prompt(in, out, "房间密钥文件（没有则直接回车）: ")
	if err != nil {
		return err
	}
	return launchJoin(ctx, o, out, errOut)
}

// prompt prints label and reads one trimmed line from in.
func prompt(in io.Reader, out io.Writer, label string) (string, error) {
	fmt.Fprint(out, label)
	line, err := readLine(in)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// readLine reads a single line (without the trailing newline) from in.
func readLine(in io.Reader) (string, error) {
	if in == nil {
		return "", io.EOF
	}
	b, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	if err == io.EOF && strings.TrimSpace(b) == "" {
		return "", nil
	}
	return strings.TrimRight(b, "\r\n"), nil
}

// defaultName returns a sensible default nickname from the environment.
func defaultName() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u := os.Getenv("USERNAME"); u != "" {
		return u
	}
	return "玩家"
}

// runHost is the advanced direct mode: it parses the (now optional) flags,
// prompting for any missing name, and launches the host.
func runHost(ctx context.Context, in io.Reader, args []string, out, errOut io.Writer) error {
	o, err := parseFlags(args, false)
	if err != nil {
		return err
	}
	if o.name == "" {
		o.name, err = prompt(in, out, "你的昵称: ")
		if err != nil {
			return err
		}
	}
	return launchHost(ctx, o, out, errOut)
}

// launchHost starts the server and connects the local host client, then stays
// alive until ctx is cancelled (graceful shutdown).
func launchHost(ctx context.Context, o *cliOpts, out, errOut io.Writer) error {
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

// runJoin is the advanced direct mode: it parses the (now optional) flags,
// prompting for any missing name, and joins the target room.
func runJoin(ctx context.Context, in io.Reader, args []string, out, errOut io.Writer) error {
	o, err := parseFlags(args, true)
	if err != nil {
		return err
	}
	if o.name == "" {
		o.name, err = prompt(in, out, "你的昵称: ")
		if err != nil {
			return err
		}
	}
	return launchJoin(ctx, o, out, errOut)
}

// launchJoin resolves the target and key, connects, and stays alive until ctx
// is cancelled.
func launchJoin(ctx context.Context, o *cliOpts, out, errOut io.Writer) error {
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
