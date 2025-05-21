package main

import (
	"context"
	"encoding/json"
	"os"
	"sra/vat"

	"log/slog"

	"github.com/Khan/genqlient/graphql"
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
			slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{AddSource: true, Level: slog.LevelDebug})))
			slog.Debug("Debug mode enabled")
		} else {
			slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{AddSource: true, Level: slog.LevelInfo})))
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

func validateCreds(ctx context.Context, client graphql.Client, h string) bool {
	// validate connection
	r, err := vat.Introspect(ctx, client)
	if err != nil {
		var e any
		b, jerr := json.Marshal(r)
		if jerr != nil {
			slog.Error("Could not validate creds", "hostname", h, "error", err, "parsing-error", jerr)
			return false
		}
		jerr = json.Unmarshal(b, &e)
		if jerr != nil {
			slog.Error("Could not validate creds", "hostname", h, "error", err, "parsing-error", jerr)
			return false
		}
		slog.Error("could not validate creds", "hostname", h, "error", err, "detailed-error", e)
		return false

	}
	if r.Schema.QueryType.Name != vat.INTROSPECTION_QUERYTYPE {
		slog.Error("Validation attempt failed, response did not validate, please open a bug", "response", r, "hostname", h)
		return false
	}
	return true
}
