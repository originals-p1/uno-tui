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
	fmt.Fprintln(out, "（括号内为上次的配置，直接回车即可沿用）")

	cfg := loadWizardConfig()

	defName := cfg.Name
	if defName == "" {
		defName = defaultName() // always resolve, so the hint shows what Enter will use
	}
	name, err := promptDefault(in, out, "你的昵称", defName)
	if err != nil {
		return err
	}
	if name == "" {
		name = defName
	}
	cfg.Name = name

	defMode := cfg.Mode
	if defMode == "" {
		defMode = "host"
	}
	mode, err := promptDefault(in, out, "开房还是加入？[H]ost / [J]oin", defMode)
	if err != nil {
		return err
	}
	isJoin := strings.HasPrefix(strings.ToLower(strings.TrimSpace(mode)), "j")
	cfg.Mode = "host"
	if isJoin {
		cfg.Mode = "join"
	}
	if isJoin {
		return wizardJoin(ctx, &cfg, in, out, errOut)
	}
	return wizardHost(ctx, &cfg, in, out, errOut)
}

// wizardHost collects hosting settings, then launches the server + host client.
func wizardHost(ctx context.Context, cfg *wizardConfig, in io.Reader, out, errOut io.Writer) error {
	defPort := cfg.Port
	if defPort == 0 {
		defPort = defaultPort
	}
	p, err := promptDefault(in, out, "监听端口", strconv.Itoa(defPort))
	if err != nil {
		return err
	}
	if p == "" {
		p = strconv.Itoa(defPort)
	}
	n, aerr := strconv.Atoi(strings.TrimSpace(p))
	if aerr != nil {
		return fmt.Errorf("无效端口 %q", p)
	}
	cfg.Port = n

	defUse, defKey := "n", cfg.KeyFile
	if cfg.UseKey {
		defUse = "y"
		if defKey == "" {
			defKey = "room.key"
		}
	}
	useKey, err := promptDefault(in, out, "使用房间密钥文件吗？[y/N]", defUse)
	if err != nil {
		return err
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(useKey)), "y") {
		cfg.UseKey = true
		cfg.KeyFile, err = promptDefault(in, out, "密钥文件路径", defKey)
		if err != nil {
			return err
		}
		if cfg.KeyFile == "" {
			cfg.KeyFile = "room.key"
		}
	} else {
		cfg.UseKey = false
		cfg.KeyFile = ""
	}

	if err := cfg.save(); err != nil {
		fmt.Fprintf(errOut, "提示: 保存启动配置失败: %v\n", err)
	}
	o := &cliOpts{name: cfg.Name, port: cfg.Port, roomKeyFile: cfg.KeyFile}
	return launchHost(ctx, o, out, errOut)
}

// wizardJoin collects the target address and optional key, then joins a room.
func wizardJoin(ctx context.Context, cfg *wizardConfig, in io.Reader, out, errOut io.Writer) error {
	defAddr := cfg.Addr
	if defAddr == "" {
		defAddr = "localhost"
	}
	addr, err := promptDefault(in, out, "房间地址 HOST[:PORT]", defAddr)
	if err != nil {
		return err
	}
	if addr == "" {
		addr = defAddr
	}
	cfg.Addr = addr
	// Avoid a doubled hint when a key file is remembered.
	keyLabel := "房间密钥文件（没有则直接回车）"
	if cfg.KeyFile != "" {
		keyLabel = "房间密钥文件"
	}
	cfg.KeyFile, err = promptDefault(in, out, keyLabel, cfg.KeyFile)
	if err != nil {
		return err
	}

	if err := cfg.save(); err != nil {
		fmt.Fprintf(errOut, "提示: 保存启动配置失败: %v\n", err)
	}
	o := &cliOpts{name: cfg.Name, addr: cfg.Addr, roomKeyFile: cfg.KeyFile}
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

// promptDefault is prompt with a remembered default: the label shows the
// previous value, and an empty Enter reuses it.
func promptDefault(in io.Reader, out io.Writer, label, def string) (string, error) {
	if def != "" {
		label = fmt.Sprintf("%s（默认 %s）: ", label, def)
	} else {
		label = label + ": "
	}
	v, err := prompt(in, out, label)
	if err != nil {
		return "", err
	}
	if v == "" {
		return def, nil
	}
	return v, nil
}

// readLine reads a single line (without the trailing newline) from in. It
// reuses one buffered reader per input so that look-ahead buffering cannot
// swallow subsequent lines across multiple prompts (which would silently drop
// later answers and fall back to defaults).
var lineBuf *bufio.Reader
var lineBufFrom io.Reader

func readLine(in io.Reader) (string, error) {
	if in == nil {
		return "", io.EOF
	}
	if lineBuf == nil || lineBufFrom != in {
		lineBuf = bufio.NewReader(in)
		lineBufFrom = in
	}
	b, err := lineBuf.ReadString('\n')
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
