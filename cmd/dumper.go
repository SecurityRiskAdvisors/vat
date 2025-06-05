package main

import (
	"compress/gzip"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"sra/vat"
	"sra/vat/internal/util"

	"filippo.io/age"
	"github.com/spf13/cobra"
)

var (
	filterFile string
	outputDir  string
)

// Create a dump subcommand
var dumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Dump all assessments from the VECTR instance",
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

		// Set up the VECTR client
		client, vectrVersionHandler := util.SetupVectrClient(hostname, strings.TrimSpace(string(credentials)), insecure)

		// Get the VECTR version (side effect - check the creds as well)
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

		// Set up the filter
		var filter *util.Filter
		if filterFile != "" {
			file, err := os.Open(filterFile)
			if err != nil {
				slog.Error("Failed to open filter file", "error", err)
				os.Exit(1)
			}
			defer file.Close()

			filter, err = util.NewFilter(file)
			if err != nil {
				slog.Error("Failed to parse filter file", "error", err)
				os.Exit(1)
			}
		} else {
			r := strings.NewReader(`"*","*"` + "\n")
			filter, err = util.NewFilter(r)
			if err != nil {
				slog.Error("Failed to parse filter file", "error", err)
				os.Exit(1)
			}
		}

		// Call DumpInstance with the filter
		dumpedData, err := vat.DumpInstance(versionContext, client, filter)
		if err != nil {
			// if there is an assessment failure, then keep going, we'll handle it as the assessment level
			if err != vat.ErrDumpAssessmentFailure || errors.Is(err, vat.ErrDumpAssessmentFailure) {
				slog.Error("Failed to dump instance", "error", err)
				os.Exit(1)
			} else {
				slog.Error("There was an assessment error, will come up later", "error", err)
			}
		}

		// Ensure the output directory exists
		if err := os.MkdirAll(outputDir, os.ModePerm); err != nil {
			slog.Error("Failed to create output directory", "error", err)
			os.Exit(1)
		}

		// Process each assessment
		for _, entry := range dumpedData {
			if entry.Err != nil {
				slog.Error("Error dumping assessment", "db", entry.Db, "assessment", entry.AssessmentName, "error", entry.Err)
				continue
			}
			subdir := filepath.Join(outputDir, entry.Db)
			if err := os.MkdirAll(subdir, os.ModePerm); err != nil {
				slog.Error("Failed to create the subdir", "error", err, "subdir", subdir)
				os.Exit(1)
			}

			// Serialize the assessment data to JSON
			jsonData, err := vat.EncodeToJson(entry.Ad)
			if err != nil {
				slog.Error("Failed to encode assessment data to JSON", "assessment", entry.AssessmentName, "error", err)
				continue
			}

			// Generate a secure random passphrase
			passphrase, err := generateRandomPassphrase()
			if err != nil {
				slog.Error("Failed to generate random passphrase", "assessment", entry.AssessmentName, "error", err)
				continue
			}

			// Create the output file paths
			outputFilePath := filepath.Join(subdir, entry.AssessmentName+".json.gz")
			passphraseFilePath := filepath.Join(subdir, entry.AssessmentName+"_passphrase.txt")

			// Write the passphrase to a file
			if err := os.WriteFile(passphraseFilePath, []byte(passphrase), 0600); err != nil {
				slog.Error("Failed to write passphrase file", "assessment", entry.AssessmentName, "error", err)
				continue
			}

			// Create the output file
			outputFileHandle, err := os.Create(outputFilePath)
			if err != nil {
				slog.Error("Failed to create output file", "assessment", entry.AssessmentName, "error", err)
				continue
			}
			defer outputFileHandle.Close()

			// Encrypt the data using the age package
			recipient, err := age.NewScryptRecipient(passphrase)
			if err != nil {
				slog.Error("Failed to create scrypt recipient", "assessment", entry.AssessmentName, "error", err)
				continue
			}

			encryptor, err := age.Encrypt(outputFileHandle, recipient)
			if err != nil {
				slog.Error("Failed to initialize encryption", "assessment", entry.AssessmentName, "error", err)
				continue
			}
			defer encryptor.Close()

			// Compress the JSON data using GZIP
			gzipWriter := gzip.NewWriter(encryptor)
			defer gzipWriter.Close()

			_, err = gzipWriter.Write(jsonData)
			if err != nil {
				slog.Error("Failed to write compressed data", "assessment", entry.AssessmentName, "error", err)
				continue
			}

			slog.Info("Assessment dumped successfully", "assessment", entry.AssessmentName, "output-file", outputFilePath, "passphrase-file", passphraseFilePath)
		}
	},
}

func init() {
	// Add flags to the dump command
	dumpCmd.Flags().StringVar(&hostname, "hostname", "", "Hostname of the VECTR instance (required)")
	dumpCmd.Flags().StringVar(&credentialsFile, "vectr-creds-file", "", "Path to the VECTR credentials file (required)")
	dumpCmd.Flags().StringVar(&outputDir, "output-dir", "", "Directory to output the assessment files (required)")
	dumpCmd.Flags().BoolVarP(&insecure, "insecure", "k", false, "Allow insecure connections to the instance (e.g., ignore TLS certificate errors)")

	dumpCmd.Flags().StringVar(&filterFile, "filter-file", "", "Path to the filter file (optional)")
	dumpCmd.MarkFlagRequired("hostname")
	dumpCmd.MarkFlagRequired("credentials-file")
	dumpCmd.MarkFlagRequired("output-dir")
}
