package vat

import (
	_ "embed"
	"encoding/json"
	"sra/vat/internal/dao"

	"github.com/vektah/gqlparser/v2/gqlerror"
)

type VatContextKey string
type VatContextValue string

//go:embed LICENSE
var License string

 // GenericBlueTool represents a tool within the VECTR application, providing a standardized way to manage tool-related data.
 //
 // Fields:
 //   - Id: A unique identifier for the tool.
 //   - Name: The name of the tool.
 //   - ProductName: The product name associated with the tool.
type GenericBlueTool struct {
	Id          string
	Name        string
	ProductName string
}

 // AssessmentData represents the data structure for an assessment within the VECTR application.
 //
 // Fields:
 //   - Assessment: Contains detailed information about the assessment.
 //   - LibraryTestCases: Maps test case IDs to their corresponding library test case details.
 //   - TemplateAssessment: Stores the name of the template assessment used.
 //   - Organizations: Lists the organizations associated with the assessment.
 //   - ToolsMap: Maps tool names to their corresponding `GenericBlueTool` details.
 //   - IdToolsMap: Maps tool IDs to their corresponding `GenericBlueTool` details.
 //   - Metadata: Holds metadata related to the assessment operations.
 //   - OptionalFields: Contains additional fields that are not required for restoration, allowing for backward-compatible updates.
type AssessmentData struct {
	Assessment         dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessment
	LibraryTestCases   map[string]dao.GetLibraryTestCasesLibraryTestcasesByIdsTestCaseConnectionNodesTestCase
	TemplateAssessment string
	Organizations      []string
	ToolsMap           map[string]GenericBlueTool
	IdToolsMap         map[string]GenericBlueTool
	Metadata           *VatMetadata
	OptionalFields     struct { // these fields will never be required on the restore side, so can be added to without changing the major version of the application
		OrgMap map[string]dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentOrganizationsOrganization
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

// DecodeJson is a function that deserializes a JSON-encoded byte slice into an `AssessmentData` struct.
//
// Parameters:
//   - data: A byte slice containing the JSON-encoded representation of the `AssessmentData`.
//
// Returns:
//   - A pointer to an `AssessmentData` struct populated with the deserialized data.
//   - An error if the JSON decoding process fails.
//
// Errors:
//   - Returns an error if the `json.Unmarshal` function fails to deserialize the data.
func DecodeJson(data []byte) (*AssessmentData, error) {
	a := AssessmentData{}

	err := json.Unmarshal(data, &a)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

 // gqlErrParse attempts to parse a GraphQL error into a JSON-compatible object.
 //
 // Parameters:
 //   - err: An error object, expected to be of type `gqlerror.List`.
 //
 // Returns:
 //   - An `any` type representing the JSON-compatible object if parsing is successful.
 //   - A boolean indicating whether the parsing was successful.
 //
 // Errors:
 //   - Returns false if the error is not of type `gqlerror.List`.
 //   - Returns false if the error cannot be marshaled into JSON or unmarshaled back into an object.
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
