package vat

import (
	"fmt"
	"sra/vat/internal/dao"
	"strings"
	"text/tabwriter"
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
		buffer.WriteString(fmt.Sprintf("Assessment Name: %s\n", data.Assessment.Name))
	} else {
		buffer.WriteString("Assessment Name: <Not Found>\n")
	}

	// Add assessment description if available
	if data.Assessment.Description != "" {
		buffer.WriteString(fmt.Sprintf("Description: %s\n", data.Assessment.Description))
	}

	buffer.WriteString("\n")

	// Assessment Metadata section
	buffer.WriteString("Assessment Metadata:\n")
	buffer.WriteString("-------------------\n")
	writeAssessmentMetadataSection(&buffer, data.Assessment)
	buffer.WriteString("\n")

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
			metadata[k] = "<Not Found>"
		}
	}

	w := tabwriter.NewWriter(buffer, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "VAT Version:\t"+metadata["version"])
	fmt.Fprintln(w, "Operation Date:\t"+metadata["date"])
	fmt.Fprintln(w, "VECTR Version:\t"+metadata["vectr-version"])
	w.Flush()
}

func writeAssessmentMetadataSection(buffer *strings.Builder, assessment dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessment) {
	if len(assessment.Metadata) == 0 {
		buffer.WriteString("No assessment metadata available\n")
		return
	}

	w := tabwriter.NewWriter(buffer, 0, 0, 2, ' ', 0)
	for _, meta := range assessment.Metadata {
		fmt.Fprintln(w, meta.Key+":\t"+meta.Value)
	}
	w.Flush()
}
