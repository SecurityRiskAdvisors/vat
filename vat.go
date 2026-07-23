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

// AssessmentData is the in-memory model for a single assessment restore/save
// operation. It composes the individually-versioned resources (see
// format.go) with the file's own manifest.
//
// AssessmentResource is embedded so existing code can keep referencing
// ad.Assessment, ad.ToolsMap, ad.IdToolsMap, ad.TemplateAssessment,
// ad.OrgMap, ad.BundleID, ad.BundlePrefix directly via Go's field promotion.
//
// There is deliberately no restore-time field here (e.g. "RestoreInfo"):
// that information (which vat/VECTR version is doing the current restore) is
// an artifact of a single RestoreAssessment call, not a property of the data
// being restored — nothing ever re-serializes AssessmentData after a
// restore, so it lives as a local variable in restore.go instead of a
// stored field (see VatOpMetadata's doc comment in metadata.go).
type AssessmentData struct {
	AssessmentResource
	LibraryTestCases LibraryTestCasesResource
	// OrgMap is the sole source of the organization names referenced by this
	// assessment (name -> full Organization object) — there is no separate
	// flat name list. Callers that just need names use
	// slices.Collect(maps.Keys(OrgMap)).
	OrgMap OrgMapResource
	// Manifest is save-time provenance and part of the wire file itself —
	// see Manifest's doc comment. Stamped via NewManifestMetadata at save
	// time; handed back as-is by DecodeJson.
	Manifest Manifest
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
