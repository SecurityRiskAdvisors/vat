package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"sra/vat"
	"sra/vat/internal/util"

	"log/slog"

	"filippo.io/age"
	"github.com/spf13/cobra"
)

var (
	inputFile      string
	passphraseFile string
)

// Create a restore subcommand
var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore an assessment to the VECTR instance",
	Run: func(cmd *cobra.Command, args []string) {
		// Set up a context with signal handling
		ctx, cancel := context.WithCancel(context.WithValue(context.Background(), vat.VERSION, vat.VatContextValue(version)))
		defer cancel()

		// Handle Ctrl-C (SIGINT) and other termination signals
		signalChan := make(chan os.Signal, 1)
		signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
		go func() {
			defer signal.Reset()
			<-signalChan
			slog.Info("\nReceived interrupt signal, shutting down gracefully. Ctrl+C again to force shutdown...")
			cancel()
		}()

		// Read credentials from the file
		credentials, err := os.ReadFile(credentialsFile)
		if err != nil {
			slog.Error("Failed to read credentials file", "error", err)
			os.Exit(1)
		}

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

		// Set up the VECTR client
		client, vectrVersionHandler, err := util.SetupVectrClient(hostname, strings.TrimSpace(string(credentials)), tlsParams)
		if err != nil {
			slog.Error("could not set up connection to vectr", "hostname", hostname, "error", err)
		}

		// get the VECTR version (side effect - check the creds as well)
		vectrVersion, err := vectrVersionHandler.Get(ctx)
		if err != nil {
			if err == util.ErrInvalidAuth {
				slog.Error("could not validate creds", "hostname", hostname, "error", err)
				os.Exit(1)
			}
			slog.Error("could not get vectr version", "hostname", hostname, "error", err)
			os.Exit(1)
		}
		slog.Info("validated credentials and fetched vectr version", "hostname", hostname, "vectr-version", vectrVersion)
		versionContext := context.WithValue(ctx, vat.VECTR_VERSION, vat.VatContextValue(vectrVersion))

		optionalParams := &vat.RestoreOptionalParams{
			AssessmentName:             targetAssessmentName,
			OverrideAssessmentTemplate: overrideAssessmentTemplate,
		}

		// Restore the assessment
		if err := vat.RestoreAssessment(versionContext, client, db, &assessmentData, optionalParams); err != nil {
			slog.Error("Failed to restore assessment", "error", err)
			os.Exit(1)
		}

		slog.Info("Assessment restored successfully")
	},
}

func init() {
	// Add flags to the restore command
	restoreCmd.Flags().StringVar(&db, "db", "", "Database to restore the assessment to (required)")
	restoreCmd.Flags().StringVar(&db, "env", "", "Alias for --db")
	restoreCmd.Flags().StringVar(&hostname, "hostname", "", "Hostname of the VECTR instance (required)")
	restoreCmd.Flags().StringVar(&credentialsFile, "vectr-creds-file", "", "Path to the credentials file (required)")
	restoreCmd.Flags().StringVar(&inputFile, "input-file", "", "Path to the encrypted input file (required)")
	restoreCmd.Flags().StringVar(&passphraseFile, "passphrase-file", "", "Path to the file containing the decryption passphrase")
	restoreCmd.Flags().StringVar(&targetAssessmentName, "target-assessment-name", "", "The assessment name to set in the new instance")
	restoreCmd.Flags().BoolVar(&overrideAssessmentTemplate, "override-template-assessment", false, "Override any set template name in the serialized data and load template test cases anyway")

	// Mark flags as required
	restoreCmd.MarkFlagsOneRequired("db", "env")
	restoreCmd.MarkFlagRequired("hostname")
	restoreCmd.MarkFlagRequired("credentials-file")
	restoreCmd.MarkFlagRequired("input-file")
}
