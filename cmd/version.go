package main

import (
	"fmt"
	"sra/vat"

	"github.com/spf13/cobra"
)

// version is the application version, set dynamically during the build process.
var version = "dev" // Default to "dev" if not set during build

// versionCmd is the Cobra command for displaying the application version.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of the application",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Version: %s\n", version)
		fmt.Printf("Built for VECTR version: %s\n", vat.TAGGED_VECTR_VERSION)
	},
}
