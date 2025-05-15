package main

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"sra/vat"

	"log/slog"

	"github.com/spf13/cobra"
)

var (
	sourceHostname        string
	sourceCredentialsFile string
	sourceDB              string
	targetHostname        string
	targetCredentialsFile string
	targetDB              string
)

// Create a transfer subcommand
var transferCmd = &cobra.Command{
	Use:   "transfer",
	Short: "Transfer an assessment from one VECTR instance to another",
	Run: func(cmd *cobra.Command, args []string) {
		// Set up a context with signal handling
		ctx, cancel := context.WithCancel(context.WithValue(context.Background(), "version", version))
		defer cancel()

		// Handle Ctrl-C (SIGINT) and other termination signals
		signalChan := make(chan os.Signal, 1)
		signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-signalChan
			slog.Info("Received interrupt signal, shutting down gracefully...")
			cancel()
		}()

		// Read source credentials
		sourceCredentials, err := os.ReadFile(sourceCredentialsFile)
		if err != nil {
			slog.Error("Failed to read source credentials file", "error", err)
			os.Exit(1)
		}

		// Read target credentials
		targetCredentials, err := os.ReadFile(targetCredentialsFile)
		if err != nil {
			slog.Error("Failed to read target credentials file", "error", err)
			os.Exit(1)
		}

		// Set up the source VECTR client
		sourceClient := vat.SetupVectrClient(sourceHostname, strings.TrimSpace(string(sourceCredentials)), insecure)

		// Fetch the assessment data from the source instance
		slog.Info("Fetching assessment data from source instance", "hostname", sourceHostname, "db", sourceDB)
		assessmentData, err := vat.SaveAssessmentData(ctx, sourceClient, sourceDB, assessmentName)
		if err != nil {
			slog.Error("Failed to fetch assessment data from source instance", "error", err)
			os.Exit(1)
		}

		// Set up the target VECTR client
		targetClient := vat.SetupVectrClient(targetHostname, strings.TrimSpace(string(targetCredentials)), insecure)

		optionalParams := &vat.RestoreOptionalParams{
			AssessmentName:             targetAssessmentName,
			OverrideAssessmentTemplate: overrideAssessmentTemplate,
		}
		// Transfer the assessment data to the target instance
		slog.Info("Transferring assessment data to target instance", "hostname", targetHostname, "db", targetDB)
		if err := vat.RestoreAssessment(ctx, targetClient, targetDB, assessmentData, optionalParams); err != nil {
			slog.Error("Failed to transfer assessment data to target instance", "error", err)
			os.Exit(1)
		}

		slog.Info("Assessment transferred successfully")
	},
}

func init() {
	// Add flags to the transfer command
	transferCmd.Flags().StringVar(&sourceHostname, "source-hostname", "", "Hostname of the source VECTR instance (required)")
	transferCmd.Flags().StringVar(&sourceCredentialsFile, "source-vectr-creds-file", "", "Path to the source credentials file (required)")
	transferCmd.Flags().StringVar(&sourceDB, "source-db", "", "Database name in the source VECTR instance (required)")
	transferCmd.Flags().StringVar(&targetHostname, "target-hostname", "", "Hostname of the target VECTR instance (required)")
	transferCmd.Flags().StringVar(&targetCredentialsFile, "target-vectr-creds-file", "", "Path to the target credentials file (required)")
	transferCmd.Flags().StringVar(&targetDB, "target-db", "", "Database name in the target VECTR instance (required)")
	transferCmd.Flags().StringVar(&assessmentName, "assessment-name", "", "Name of the assessment to transfer (required)")
	transferCmd.Flags().StringVar(&targetAssessmentName, "target-assessment-name", "", "The assessment name to set in the new instance")
	transferCmd.Flags().BoolVarP(&insecure, "insecure", "k", false, "Allow insecure connections to the instances (e.g., ignore TLS certificate errors)")
	transferCmd.Flags().BoolVar(&overrideAssessmentTemplate, "override-template-assessment", false, "Ignore the template name in the serialized data and load template test cases anyway")

	// Mark flags as required
	transferCmd.MarkFlagRequired("source-hostname")
	transferCmd.MarkFlagRequired("source-credentials-file")
	transferCmd.MarkFlagRequired("source-db")
	transferCmd.MarkFlagRequired("target-hostname")
	transferCmd.MarkFlagRequired("target-credentials-file")
	transferCmd.MarkFlagRequired("target-db")
	transferCmd.MarkFlagRequired("assessment-name")
}
