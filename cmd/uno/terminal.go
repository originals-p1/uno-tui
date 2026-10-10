package main

import (
	"os"

	"golang.org/x/term"
)

// readPassword reads a line from the terminal without echoing it.
func readPassword(f *os.File) ([]byte, error) {
	return term.ReadPassword(int(f.Fd()))
}

// isTerminal reports whether f is an interactive terminal.
func isTerminal(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }
