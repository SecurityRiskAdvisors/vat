package main

import (
	"os"

	"log/slog"

	"sra/vat/internal/util"

	"github.com/spf13/cobra"
)

var (
	debug                      bool
	insecure                   bool
	targetAssessmentName       string
	overrideAssessmentTemplate bool
	clientCertFile             string
	clientKeyFile              string
	caCertFiles                []string
	tlsParams                  *util.CustomTlsParams
	sourceCampaignName         string
)

// RootCmd is the root command for the CLI
var RootCmd = &cobra.Command{
	Use:   "vat",
	Short: "VECTR Assessment Migrator",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Configure slog based on the debug flag
		if debug {
			slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{AddSource: true, Level: slog.LevelDebug})))
			slog.Debug("Debug mode enabled")
		} else {
			slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{AddSource: true, Level: slog.LevelInfo})))
		}

		if (len(clientCertFile) > 0) != (len(clientKeyFile) > 0) {
			slog.Error("Both --client-cert-file and --client-key-file must be provided together")
			os.Exit(1)
		}

		var err error

		if len(clientCertFile) > 0 || len(clientKeyFile) > 0 || len(caCertFiles) > 0 || insecure {
			tlsParams = &util.CustomTlsParams{
				InsecureConnect: insecure, // just set this here since we are creating the object
			}
		}

		if len(clientCertFile) > 0 {
			tlsParams.ClientCertFile, err = os.ReadFile(clientCertFile)
			if err != nil {
				slog.Error("Failed to read client certificate file", "file", clientCertFile, "error", err)
				os.Exit(1)
			}
		}

		if len(clientKeyFile) > 0 {
			tlsParams.ClientKeyFile, err = os.ReadFile(clientKeyFile)
			if err != nil {
				slog.Error("Failed to read client key file", "file", clientKeyFile, "error", err)
				os.Exit(1)
			}
		}

		if len(caCertFiles) > 0 {
			tlsParams.CaCertFiles = make([][]byte, len(caCertFiles))
			for i, caFile := range caCertFiles {
				tlsParams.CaCertFiles[i], err = os.ReadFile(caFile)
				if err != nil {
					slog.Error("Failed to read CA certificate file", "file", caFile, "error", err)
					os.Exit(1)
				}
			}
		}
	},
}

// Initialize the root command and add subcommands
func Execute() {
	// Add global flags
	RootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", false, "Enable debug mode")
	RootCmd.PersistentFlags().BoolVarP(&insecure, "insecure", "k", false, "Allow insecure server connections when using TLS")
	RootCmd.PersistentFlags().StringVar(&clientCertFile, "client-cert-file", "", "Path to the client certificate file")
	RootCmd.PersistentFlags().StringVar(&clientKeyFile, "client-key-file", "", "Path to the client key file")
	RootCmd.PersistentFlags().StringSliceVar(&caCertFiles, "ca-cert", []string{}, "Path to a CA certificate file (can be used multiple times)")
	slog.Info("vat started", "version", version)

	// Add subcommands
	RootCmd.AddCommand(saveCmd)     // From saver.go
	RootCmd.AddCommand(restoreCmd)  // From restorer.go
	RootCmd.AddCommand(versionCmd)  // From version.go
	RootCmd.AddCommand(transferCmd) // From transfer.go
	RootCmd.AddCommand(licenseCmd)  // From license.go
	RootCmd.AddCommand(dumpCmd)     // From dumper.go
	RootCmd.AddCommand(diagCmd)     // From diag.go

	// Execute the root command
	if err := RootCmd.Execute(); err != nil {
		slog.Error("Command execution failed", "error", err)
		os.Exit(1)
	}
}

func main() {
	Execute()
}
