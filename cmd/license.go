package main

import (
	"fmt"
	"sra/vat"

	"github.com/spf13/cobra"
)

// license is the license of this binary
var license string = vat.License

// licenseCmd is the Cobra command for displaying the application version.
var licenseCmd = &cobra.Command{
	Use:   "license",
	Short: "Print the license of the application",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(license)
	},
}
