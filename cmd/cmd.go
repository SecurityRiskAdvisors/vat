package main

import (
	"os"

	"log/slog"

	"github.com/spf13/cobra"
)

var (
	debug                      bool
	targetAssessmentName       string
	overrideAssessmentTemplate bool
)

// RootCmd is the root command for the CLI
var RootCmd = &cobra.Command{
	Use:   "vat",
	Short: "VECTR Assessment Migrator",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Configure slog based on the debug flag
		if debug {
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
			slog.Debug("Debug mode enabled")
		} else {
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
		}
	},
}

// Initialize the root command and add subcommands
func Execute() {
	// Add global flags
	RootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", false, "Enable debug mode")
	slog.Info("vat started", "version", version)

	// Add subcommands
	RootCmd.AddCommand(saveCmd)     // From saver.go
	RootCmd.AddCommand(restoreCmd)  // From restorer.go
	RootCmd.AddCommand(versionCmd)  // From version.go
	RootCmd.AddCommand(transferCmd) // From transfer.go
	RootCmd.AddCommand(licenseCmd)  // From license.go

	// Execute the root command
	if err := RootCmd.Execute(); err != nil {
		slog.Error("Command execution failed", "error", err)
		os.Exit(1)
	}
}

func main() {
	Execute()
}
