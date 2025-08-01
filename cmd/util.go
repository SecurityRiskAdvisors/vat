package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

var buffer strings.Builder

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
