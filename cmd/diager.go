package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"sra/vat"

	"filippo.io/age"
	"github.com/spf13/cobra"
)

var diagCmd = &cobra.Command{
	Use:   "diag",
	Short: "Display metadata from a saved assessment file",
	Run: func(cmd *cobra.Command, args []string) {
		// Read the passphrase
		passphrase, err := getPassphrase(passphraseFile)
		if err != nil {
			slog.Error("Failed to read passphrase", "error", err)
			os.Exit(1)
		}

		// Open the encrypted input file
		encryptedFile, err := os.Open(inputFile)
		if err != nil {
			slog.Error("Failed to open input file", "error", err)
			os.Exit(1)
		}
		defer encryptedFile.Close()

		// Set up the age decryption
		identity, err := age.NewScryptIdentity(passphrase)
		if err != nil {
			slog.Error("Failed to create scrypt identity", "error", err)
			os.Exit(1)
		}

		decryptor, err := age.Decrypt(encryptedFile, identity)
		if err != nil {
			slog.Error("Failed to initialize decryption", "error", err)
			os.Exit(1)
		}

		// Set up GZIP decompression
		gzipReader, err := gzip.NewReader(decryptor)
		if err != nil {
			slog.Error("Failed to initialize GZIP decompression", "error", err)
			os.Exit(1)
		}
		defer gzipReader.Close()

		// Read and deserialize the JSON data
		var assessmentData vat.AssessmentData
		if err := json.NewDecoder(gzipReader).Decode(&assessmentData); err != nil {
			slog.Error("Failed to decode JSON data", "error", err)
			os.Exit(1)
		}

		// Extract metadata using the function from vat package
		metadataOutput := vat.ExtractMetadata(&assessmentData)

		// Print the metadata
		fmt.Println(string(metadataOutput))
	},
}

func init() {
	// Add flags to the diag command
	diagCmd.Flags().StringVar(&inputFile, "input-file", "", "Path to the encrypted input file (required)")
	diagCmd.Flags().StringVar(&passphraseFile, "passphrase-file", "", "Path to the file containing the decryption passphrase")

	// Mark flags as required
	diagCmd.MarkFlagRequired("input-file")
}
