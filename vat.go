package vat

import (
	_ "embed"
	"encoding/json"
)

type VersionKey string
type VersionNumber string

const VERSION VersionKey = "VERSION"

//go:embed LICENSE
var License string

type GenericBlueTool struct {
	Id          string
	Name        string
	ProductName string
}

type AssessmentData struct {
	Assessment         GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessment
	LibraryTestCases   map[string]GetLibraryTestCasesLibraryTestcasesByIdsTestCaseConnectionNodesTestCase
	TemplateAssessment string
	Organizations      []string
	ToolsMap           map[string]GenericBlueTool
	IdToolsMap         map[string]GenericBlueTool
	Metadata           *VatMetadata
}

// EncodeToJson is a convienience function converts an `AssessmentData` struct into a JSON-encoded byte slice.
//
// Parameters:
//   - data: A pointer to an `AssessmentData` struct containing the data to be serialized.
//
// Returns:
//   - A byte slice containing the JSON-encoded representation of the `AssessmentData`.
//   - An error if the JSON encoding process fails.
//
// Errors:
//   - Returns an error if the `json.MarshalIndent` function fails to serialize the data.
func EncodeToJson(data *AssessmentData) ([]byte, error) {
	jsonData, err := json.MarshalIndent(data, "", "\t")
	if err != nil {
		return nil, err
	}
	return jsonData, nil
}

func DecodeJson(data []byte) (*AssessmentData, error) {
	a := AssessmentData{}

	err := json.Unmarshal(data, &a)
	if err != nil {
		return nil, err
	}
	return &a, nil
}
