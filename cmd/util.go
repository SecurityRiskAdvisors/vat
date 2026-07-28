package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"sra/vat"
)

var buffer strings.Builder

// enforceVectrVersionCheck aborts if the live VECTR version is outside the
// range supported by this version of vat. With --ignore-version-check the
// failure is logged as a warning and execution continues.
func enforceVectrVersionCheck(ctx context.Context, vectrVersion string, hostname string) {
	if err := vat.CheckVectrVersionSupported(ctx, vectrVersion); err != nil {
		if ignoreVersionCheck {
			slog.WarnContext(ctx, "ignoring failed VECTR version check", "hostname", hostname, "error", err)
			return
		}
		slog.ErrorContext(ctx, "unsupported VECTR version", "hostname", hostname, "error", err)
		os.Exit(1)
	}
}

// getPassphrase reads the passphrase from a file or interactively via readline.
func getPassphrase(passphraseFile string) (string, error) {
	if passphraseFile != "" {
		// Read the passphrase from the file
		passphrase, err := os.ReadFile(passphraseFile)
		if err != nil {
			return "", fmt.Errorf("failed to read passphrase file: %w", err)
		}
		return strings.TrimSpace(string(passphrase)), nil
	}

	// Read the passphrase interactively
	fmt.Print("Enter decryption passphrase: ")
	reader := bufio.NewReader(os.Stdin)
	passphrase, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read passphrase: %w", err)
	}
	return strings.TrimSpace(passphrase), nil
}
