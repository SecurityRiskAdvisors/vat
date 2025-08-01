package vat

import (
	"fmt"
	"strings"
)

// ExtractMetadata takes an AssessmentData object and returns a formatted byte array
// containing the metadata in a tabular format with context.
func ExtractMetadata(data *AssessmentData) []byte {

	if data.Metadata == nil {
		return []byte("No metadata available")
	}
	var buffer strings.Builder

	buffer.WriteString("VECTR Assessment Tool (VAT) Metadata\n")
	buffer.WriteString("===================================\n\n")

	// Add assessment name if available
	if data.Assessment.Name != "" {
		buffer.WriteString(fmt.Sprintf("Assessment Name: %s\n\n", data.Assessment.Name))
	}

	// Save Data section
	buffer.WriteString("Saved VAT Metadata:\n")
	buffer.WriteString("-------------------\n")
	if data.Metadata.SaveData != nil {
		writeMetadataSection(&buffer, data.Metadata.SaveData.serialize())
	} else {
		buffer.WriteString("No save operation data available\n")
	}
	buffer.WriteString("\n")

	// Load Operation Data section
	if data.Metadata.LoadData != nil {
		buffer.WriteString("(Old?) VAT data from a previous transfer:\n")
		buffer.WriteString("-------------------\n")
		writeMetadataSection(&buffer, data.Metadata.LoadData.serialize())
	}

	return []byte(buffer.String())
}

// Helper function to write a metadata section in tabular format
func writeMetadataSection(buffer *strings.Builder, metadata map[string]string) {
	for k, v := range metadata {
		if v == "none_found" || len(v) == 0 {
			metadata[k] = "Not Found"
		}
	}

	buffer.WriteString(fmt.Sprintf("%-20s %s\n", "VAT Version:", metadata["version"]))

	buffer.WriteString(fmt.Sprintf("%-20s %s\n", "Operation Date:", metadata["date"]))

	buffer.WriteString(fmt.Sprintf("%-20s %s\n", "VECTR Version:", metadata["vectr-version"]))
}
