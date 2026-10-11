package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGenerateRoomKey_Shape(t *testing.T) {
	k, err := generateRoomKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	b, derr := base64.RawURLEncoding.DecodeString(k)
	if derr != nil || len(b) != roomKeyBytes {
		t.Fatalf("key not a valid 32-byte Base64URL value: %q", k)
	}
}

func TestReadWriteRoomKeyFile_RoundTripAndPerms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "room.key")
	key := "abc123_-XYZ"
	if err := writeRoomKeyFile(path, key); err != nil {
		t.Fatalf("write: %v", err)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if p := fi.Mode().Perm(); p != 0o600 {
			t.Fatalf("file perms = %o, want 0600", p)
		}
	}
	got, err := readRoomKeyFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != key {
		t.Fatalf("read key = %q, want %q", got, key)
	}
}

func TestReadRoomKeyFile_Empty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.key")
	if err := os.WriteFile(path, []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readRoomKeyFile(path); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("want empty-key error, got %v", err)
	}
}

func TestHostRoomKey_CreateThenReuse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "room.key")
	var out bytes.Buffer
	key, created, err := hostRoomKey(path, &out)
	if err != nil {
		t.Fatalf("hostRoomKey create: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true")
	}
	// Second call reuses the existing file, never logs the key.
	out.Reset()
	key2, created2, err := hostRoomKey(path, &out)
	if err != nil {
		t.Fatalf("hostRoomKey reuse: %v", err)
	}
	if created2 {
		t.Fatalf("expected created=false on reuse")
	}
	if key2 != key {
		t.Fatalf("reused key = %q, want %q", key2, key)
	}
	if strings.Contains(out.String(), key) {
		t.Fatalf("key leaked to output: %q", out.String())
	}
}

func TestHostRoomKey_NoFileIsKeyless(t *testing.T) {
	var out bytes.Buffer
	key, created, err := hostRoomKey("", &out)
	if err != nil {
		t.Fatalf("hostRoomKey nofile: %v", err)
	}
	if created {
		t.Fatalf("no file created when none requested")
	}
	if key != "" {
		t.Fatalf("no key expected by default, got %q", key)
	}
	if strings.Contains(out.String(), key) && key != "" {
		t.Fatalf("key leaked to output")
	}
}

func TestJoinRoomKey_NoFileIsKeyless(t *testing.T) {
	key, err := joinRoomKey("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "" {
		t.Fatalf("want empty key with no file, got %q", key)
	}
}

func TestJoinRoomKey_FromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k")
	_ = writeRoomKeyFile(path, "sekret")
	key, err := joinRoomKey(path)
	if err != nil {
		t.Fatalf("joinRoomKey: %v", err)
	}
	if key != "sekret" {
		t.Fatalf("key=%q", key)
	}
}

func TestParseFlags_DefaultPort(t *testing.T) {
	o, err := parseFlags([]string{"--name", "Alice"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if o.port != defaultPort {
		t.Fatalf("default port = %d, want %d", o.port, defaultPort)
	}
	if o.name != "Alice" {
		t.Fatalf("name = %q", o.name)
	}
}

func TestParseFlags_JoinFlagsAfterAddress(t *testing.T) {
	// Regression: "join <addr> --name X --room-key-file Y" must parse the
	// flags even though they follow the positional address.
	o, err := parseFlags([]string{"192.168.126.130", "--name", "pppp", "--room-key-file", "k.key"}, true)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if o.addr != "192.168.126.130" {
		t.Fatalf("addr = %q", o.addr)
	}
	if o.name != "pppp" {
		t.Fatalf("name = %q", o.name)
	}
	if o.roomKeyFile != "k.key" {
		t.Fatalf("roomKeyFile = %q", o.roomKeyFile)
	}
}

func TestParseFlags_FlagsBeforeAddress(t *testing.T) {
	o, err := parseFlags([]string{"--name", "Bob", "1.2.3.4", "--port", "9000"}, true)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if o.addr != "1.2.3.4" || o.name != "Bob" || o.port != 9000 {
		t.Fatalf("unexpected parse: %+v", o)
	}
}

func TestJoinAddress_DefaultPort(t *testing.T) {
	cases := map[string]string{
		"1.2.3.4":     "1.2.3.4:7777",
		"10.147.0.2":  "10.147.0.2:7777",
		"a:9000":      "a:9000",
		"[::1]:1234":  "[::1]:1234",
		"example.com": "example.com:7777",
	}
	for in, want := range cases {
		got, err := joinAddress(in)
		if err != nil {
			t.Fatalf("joinAddress(%s): %v", in, err)
		}
		if got != want {
			t.Fatalf("joinAddress(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestRunHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := run(context.Background(), strings.NewReader(""), []string{"--help"}, &out, &errOut); err != nil {
		t.Fatalf("help: %v", err)
	}
	if !strings.Contains(out.String(), "uno host") {
		t.Fatalf("help missing host usage: %q", out.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	err := run(context.Background(), strings.NewReader(""), []string{"frobnicate"}, &out, &errOut)
	if err == nil {
		t.Fatalf("expected error for unknown command")
	}
}

// TestRunHost_PromptsName verifies that a missing --name is prompted for
// interactively (guided behavior) rather than rejected outright.
func TestRunHost_PromptsName(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "k")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &lockedBuf{}
	done := make(chan error, 1)
	go func() {
		// Name is NOT passed as a flag; it must be read from stdin.
		done <- runHost(ctx, strings.NewReader("Host\n"), []string{"--port", "0", "--room-key-file", keyFile}, out, out)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), "connected as host player") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(out.String(), "connected as host player") {
		t.Fatalf("host never connected using prompted name; output=%q", out.String())
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runHost returned error on graceful shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("runHost did not shut down, output=%q", out.String())
	}
}

func TestPrompt_ReadsAndTrims(t *testing.T) {
	var out bytes.Buffer
	got, err := prompt(strings.NewReader("  Alice  \n"), &out, "昵称: ")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if got != "Alice" {
		t.Fatalf("prompt = %q, want Alice", got)
	}
	if !strings.Contains(out.String(), "昵称:") {
		t.Fatalf("prompt label missing: %q", out.String())
	}
}

func TestReadLine_Basic(t *testing.T) {
	line, err := readLine(strings.NewReader("hello\n"))
	if err != nil || line != "hello" {
		t.Fatalf("readLine = %q, %v", line, err)
	}
}

// TestRunWizard_HostPath boots through the guided wizard (name + host + port 0
// + no key) and confirms it connects, then shuts down cleanly. The wizard's
// persisted config is redirected to a temp file so tests never write beside
// the source.
func TestRunWizard_HostPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(envUnoConfig, filepath.Join(dir, "uno-config.json"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &lockedBuf{}
	done := make(chan error, 1)
	answers := "Alice\nH\n0\nn\n"
	go func() {
		done <- run(ctx, strings.NewReader(answers), nil, out, &lockedBuf{})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), "connected as host player") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(out.String(), "connected as host player") {
		t.Fatalf("wizard host never connected; output=%q", out.String())
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("wizard returned error on graceful shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("wizard did not shut down, output=%q", out.String())
	}
}

// TestWizardConfig_Roundtrip verifies the wizard config persists a user's
// answers (never any key value) and reloads them as next-launch defaults.
func TestWizardConfig_Roundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "uno-config.json")
	t.Setenv(envUnoConfig, path)

	c := wizardConfig{Name: "Alice", Mode: "host", Port: 8888, UseKey: true, KeyFile: "room.key"}
	if err := c.save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := loadWizardConfig()
	if got.Name != "Alice" || got.Mode != "host" || got.Port != 8888 || !got.UseKey || got.KeyFile != "room.key" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file not written: %v", err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "KeyFile") && !strings.Contains(string(b), "room.key") {
		t.Fatal("config must only store the key-file path, never key material")
	}
}

// TestRunWizard_RemembersDefaults seeds a saved config and runs the wizard with
// nothing but Enter presses, asserting it reuses the stored settings and only
// asks the default questions.
func TestRunWizard_RemembersDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(envUnoConfig, filepath.Join(dir, "uno-config.json"))

	c := wizardConfig{Name: "Bob", Mode: "host", Port: 0, UseKey: false}
	if err := c.save(); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &lockedBuf{}
	done := make(chan error, 1)
	// All Enter: each prompt falls back to the remembered default.
	go func() {
		done <- run(ctx, strings.NewReader("\n\n\n\n"), nil, out, &lockedBuf{})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), "connected as host player") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(out.String(), "connected as host player") {
		t.Fatalf("Enter-through host never connected; output=%q", out.String())
	}
	// The wizard should have shown Bob as the remembered name default.
	if !strings.Contains(out.String(), "默认 Bob") {
		t.Fatalf("remembered name default not shown; output=%q", out.String())
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("wizard returned error on graceful shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("wizard did not shut down, output=%q", out.String())
	}
}

// TestRunHost_GracefulShutdown boots a host, lets it connect, then cancels the
// context and expects a clean return (server and client torn down).
func TestRunHost_GracefulShutdown(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "k")
	ctx, cancel := context.WithCancel(context.Background())
	out := &lockedBuf{}
	done := make(chan error, 1)
	go func() {
		done <- runHost(ctx, strings.NewReader(""), []string{"--port", "0", "--name", "Host", "--room-key-file", keyFile}, out, out)
	}()
	// Wait until the host reports it is connected.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), "connected as host") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(out.String(), "connected as host") {
		t.Fatalf("host never connected; output=%q", out.String())
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runHost returned error on graceful shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("runHost did not shut down, output=%q", out.String())
	}
}

// lockedBuf is a minimal goroutine-safe bytes.Buffer for tests.
type lockedBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
