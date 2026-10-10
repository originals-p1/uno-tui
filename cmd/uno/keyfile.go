package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Room key length in bytes (32 -> 43 Base64URL chars, readme §7.1).
const roomKeyBytes = 32

// generateRoomKey returns a 32-byte cryptographically random key in Base64URL
// (raw, unpadded) form. Never written to logs.
func generateRoomKey() (string, error) {
	b := make([]byte, roomKeyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// readRoomKeyFile loads a room key from disk, trimming surrounding whitespace.
func readRoomKeyFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(string(data))
	if key == "" {
		return "", errors.New("room key file is empty")
	}
	return key, nil
}

// writeRoomKeyFile writes the key owner-only (0600 on Unix; on Windows this
// applies the process ACL) and never logs the key.
func writeRoomKeyFile(path string, key string) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(key + "\n"); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// hostRoomKey resolves the hosting-side key. No key is used by default; a key
// is only required when --room-key-file is given (read it, or create it with
// owner-only permissions when missing). created reports whether a new file was
// written.
func hostRoomKey(path string, out io.Writer) (key string, created bool, err error) {
	if path == "" {
		return "", false, nil // keyless by default
	}
	if k, rerr := readRoomKeyFile(path); rerr == nil {
		return k, false, nil
	} else if !os.IsNotExist(rerr) {
		return "", false, rerr
	}
	k, gerr := generateRoomKey()
	if gerr != nil {
		return "", false, gerr
	}
	if werr := writeRoomKeyFile(path, k); werr != nil {
		return "", false, werr
	}
	return k, true, nil
}

// joinRoomKey resolves the join-side key: empty (keyless) unless a
// --room-key-file was provided.
func joinRoomKey(path string) (key string, err error) {
	if path == "" {
		return "", nil
	}
	return readRoomKeyFile(path)
}
