package vat

import (
	_ "embed"
	"encoding/json"

	"github.com/vektah/gqlparser/v2/gqlerror"
)

type VatContextKey string
type VatContextValue string

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
	OptionalFields     struct { // these fields will never be required on the restore side, so can be added to without changing the major version of the application
		OrgMap map[string]GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentOrganizationsOrganization
	}
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

func gqlErrParse(err error) (any, bool) {

	// we don't actually need the object
	// just make sure it maps
	if _, ok := err.(gqlerror.List); !ok {
		return nil, false
	}
	b, e := json.Marshal(err)
	if e != nil {
		return nil, false
	}

	var a any
	e = json.Unmarshal(b, &a)
	if e != nil {
		return nil, false
	}
	return a, true
}
