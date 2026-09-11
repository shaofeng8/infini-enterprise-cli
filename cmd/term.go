package cmd

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// promptSecret reads a value without echoing it. Refuses to run without a TTY
// so credentials are never read from a pipe by surprise.
func promptSecret(label string) (string, error) {
	if !isTerminal(os.Stdin) {
		return "", fmt.Errorf("cannot prompt for %s without a terminal", label)
	}
	fmt.Fprintf(os.Stderr, "%s: ", label)
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", label, err)
	}
	return strings.TrimSpace(string(raw)), nil
}

func promptLine(label string) (string, error) {
	if !isTerminal(os.Stdin) {
		return "", fmt.Errorf("cannot prompt for %s without a terminal", label)
	}
	fmt.Fprintf(os.Stderr, "%s: ", label)
	var value string
	if _, err := fmt.Fscanln(os.Stdin, &value); err != nil {
		return "", fmt.Errorf("cannot read %s: %w", label, err)
	}
	return strings.TrimSpace(value), nil
}
