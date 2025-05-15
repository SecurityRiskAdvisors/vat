package main

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"sra/vat"

	"filippo.io/age"
	"github.com/spf13/cobra"
)

var (
	db              string
	assessmentName  string
	hostname        string
	credentialsFile string
	outputFile      string
	insecure        bool
)

var saveCmd = &cobra.Command{
	Use:   "save",
	Short: "Save an assessment from the VECTR instance",
	Run: func(cmd *cobra.Command, args []string) {
		// Set up a context with signal handling
		ctx, cancel := context.WithCancel(context.WithValue(context.Background(), vat.VERSION, vat.VersionNumber(version)))
		defer cancel()

		// Handle Ctrl-C (SIGINT) and other termination signals
		signalChan := make(chan os.Signal, 1)
		signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
		go func() {
			defer signal.Reset()
			<-signalChan
			fmt.Println("\nReceived interrupt signal, shutting down gracefully...")
			cancel()
		}()

		// Read credentials from the file
		credentials, err := os.ReadFile(credentialsFile)
		if err != nil {
			log.Fatalf("Failed to read VECTR credentials file: %v", err)
		}

		// Set up the VECTR client
		client := vat.SetupVectrClient(hostname, strings.TrimSpace(string(credentials)), insecure)

		// Call SaveAssessmentData
		data, err := vat.SaveAssessmentData(ctx, client, db, assessmentName)
		if err != nil {
			log.Fatalf("Failed to save assessment: %v", err)
		}

		// Serialize the data to JSON
		jsonData, err := vat.EncodeToJson(data)
		if err != nil {
			log.Fatalf("Failed to encode assessment data to JSON: %v", err)
		}

		// Generate a secure random passphrase
		passphrase, err := generateRandomPassphrase() // 32 bytes = 256 bits
		if err != nil {
			log.Fatalf("Failed to generate random passphrase: %v", err)
		}

		// Output the passphrase to stdout
		fmt.Printf("Encryption passphrase (save this securely!): %s\n", passphrase)

		// Create the output file
		outputFileHandle, err := os.Create(outputFile)
		if err != nil {
			log.Fatalf("Failed to create output file: %v", err)
		}
		defer outputFileHandle.Close()

		// Encrypt the data using the age package
		recipient, err := age.NewScryptRecipient(passphrase)
		if err != nil {
			log.Fatalf("Failed to create scrypt recipient: %v", err)
		}

		encryptor, err := age.Encrypt(outputFileHandle, recipient)
		if err != nil {
			log.Fatalf("Failed to initialize encryption: %v", err)
		}
		defer encryptor.Close()

		// Compress the JSON data using GZIP
		gzipWriter := gzip.NewWriter(encryptor)
		defer gzipWriter.Close()

		_, err = gzipWriter.Write(jsonData)
		if err != nil {
			log.Fatalf("Failed to write compressed data: %v", err)
		}

		fmt.Printf("Assessment data saved, compressed, and encrypted to %s\n", outputFile)
		fmt.Println("Next steps:")
		fmt.Printf("1. Export or save a copy of the template assessment: %s. Instructions here: https://docs.vectr.io/user/data-import/#vectr-import-export-json\n", data.TemplateAssessment)
		fmt.Printf("2. Save the live-data passsword (securely!): %s\n", passphrase)
		fmt.Printf("3. Provide %s, the template assessment (%s) and the passphrase for the file to the client along with a copy of this program.\n", outputFile, data.TemplateAssessment)
		fmt.Println("4. You can then restore the saved assessment data into the client env.")

	},
}

func init() {
	// Add flags to the save command
	saveCmd.Flags().StringVar(&hostname, "hostname", "", "Hostname of the VECTR instance (required)")
	saveCmd.Flags().StringVar(&db, "db", "", "Database to pull the assessment from (required)")
	saveCmd.Flags().StringVar(&assessmentName, "assessment-name", "", "Name of the assessment to save (required)")
	saveCmd.Flags().StringVar(&credentialsFile, "vectr-creds-file", "", "Path to the VECTR credentials file (required)")
	saveCmd.Flags().StringVar(&outputFile, "output-file", "", "Path to the output file (required)")
	saveCmd.Flags().BoolVarP(&insecure, "insecure", "k", false, "Allow insecure connections to the instance (e.g., ignore TLS certificate errors)")

	// Mark flags as required
	saveCmd.MarkFlagRequired("db")
	saveCmd.MarkFlagRequired("assessment-name")
	saveCmd.MarkFlagRequired("hostname")
	saveCmd.MarkFlagRequired("credentials-file")
	saveCmd.MarkFlagRequired("output-file")
}

// generateRandomPassphrase generates a secure random passphrase of the specified length in bytes.
// The passphrase is returned as a hexadecimal string.
func generateRandomPassphrase() (string, error) {
	length := 32 // enforce 32 bytes for the password
	bytes := make([]byte, length)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
