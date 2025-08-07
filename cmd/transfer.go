package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"sra/vat"
	"sra/vat/internal/util"

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
		ctx, cancel := context.WithCancel(context.WithValue(context.Background(), vat.VERSION, vat.VatContextValue(version)))
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
		sourceClient, sourceVectrVersionHandler := util.SetupVectrClient(sourceHostname, strings.TrimSpace(string(sourceCredentials)), insecure, tlsParams)
		// get the VECTR version (side effect - check the creds as well)
		sourceVectrVersion, err := sourceVectrVersionHandler.Get(ctx)
		if err != nil {
			if err == util.ErrInvalidAuth {
				slog.Error("could not validate source creds", "src-hostname", hostname, "error", err)
				os.Exit(1)
			}
			slog.Error("could not get srouce vectr version", "src-hostname", hostname, "error", err)
			os.Exit(1)
		}
		slog.Info("validated credentials and fetched vectr version from source", "src-hostname", hostname, "src-vectr-version", sourceVectrVersion)
		sourceVersionContext := context.WithValue(ctx, vat.VECTR_VERSION, vat.VatContextValue(sourceVectrVersion))

		// Set up the target VECTR client
		targetClient, targetVectrVersionHandler := util.SetupVectrClient(targetHostname, strings.TrimSpace(string(targetCredentials)), insecure, tlsParams)
		// get the VECTR version (side effect - check the creds as well)
		targetVectrVersion, err := targetVectrVersionHandler.Get(ctx)
		if err != nil {
			if err == util.ErrInvalidAuth {
				slog.Error("could not validate creds", "hostname", hostname, "error", err)
				os.Exit(1)
			}
			slog.Error("could not get vectr version", "hostname", hostname, "error", err)
			os.Exit(1)
		}
		slog.Info("validated credentials and fetched vectr version", "hostname", hostname, "vectr-version", targetVectrVersion)
		targetVersionContext := context.WithValue(ctx, vat.VECTR_VERSION, vat.VatContextValue(targetVectrVersion))

		// Fetch the assessment data from the source instance
		slog.Info("Fetching assessment data from source instance", "hostname", sourceHostname, "db", sourceDB)
		assessmentData, err := vat.SaveAssessmentData(sourceVersionContext, sourceClient, sourceDB, assessmentName)
		if err != nil {
			slog.Error("Failed to fetch assessment data from source instance", "error", err)
			os.Exit(1)
		}

		optionalParams := &vat.RestoreOptionalParams{
			AssessmentName:             targetAssessmentName,
			OverrideAssessmentTemplate: overrideAssessmentTemplate,
		}
		// Transfer the assessment data to the target instance
		slog.Info("Transferring assessment data to target instance", "hostname", targetHostname, "db", targetDB)
		if err := vat.RestoreAssessment(targetVersionContext, targetClient, targetDB, assessmentData, optionalParams); err != nil {
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
	transferCmd.Flags().StringVar(&sourceDB, "source-env", "", "Alias for --source-db")
	transferCmd.Flags().StringVar(&targetHostname, "target-hostname", "", "Hostname of the target VECTR instance (required)")
	transferCmd.Flags().StringVar(&targetCredentialsFile, "target-vectr-creds-file", "", "Path to the target credentials file (required)")
	transferCmd.Flags().StringVar(&targetDB, "target-db", "", "Database name in the target VECTR instance (required)")
	transferCmd.Flags().StringVar(&targetDB, "target-env", "", "Alias for --target-db")
	transferCmd.Flags().StringVar(&assessmentName, "assessment-name", "", "Name of the assessment to transfer (required)")
	transferCmd.Flags().StringVar(&targetAssessmentName, "target-assessment-name", "", "The assessment name to set in the new instance")
	transferCmd.Flags().BoolVar(&overrideAssessmentTemplate, "override-template-assessment", false, "Ignore the template name in the serialized data and load template test cases anyway")

	// Mark flags as required
	transferCmd.MarkFlagRequired("source-hostname")
	transferCmd.MarkFlagRequired("source-credentials-file")
	transferCmd.MarkFlagsOneRequired("source-db", "source-env")
	transferCmd.MarkFlagRequired("target-hostname")
	transferCmd.MarkFlagRequired("target-credentials-file")
	transferCmd.MarkFlagsOneRequired("target-db", "target-env")
	transferCmd.MarkFlagRequired("assessment-name")
}
