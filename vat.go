package vat

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vektah/gqlparser/v2/gqlerror"
)

type VatContextKey string
type VatContextValue string

//go:embed LICENSE
var License string

// DefenseToolRef is a defense tool's durable, cross-instance identity as
// recorded in a saved assessment: enough to find-or-create the same tool
// (and its product/layers) in a different VECTR instance during restore,
// and enough to re-associate a test case's tool references (e.g.
// DefenseToolOutcomes) with whichever tool ends up representing it there.
type DefenseToolRef struct {
	Name        string
	Description string
	Active      bool
	Layers      []string // defense layer names attached directly to the tool
	Product     DefenseToolProductRef
}

// DefenseToolProductRef is a defense tool product's durable, cross-instance
// identity. Ref is the matching key restore uses to find/reuse a product on
// the target instance -- ids are per-instance and never compared directly.
type DefenseToolProductRef struct {
	Ref        string
	Name       string
	VendorName string
	Layers     []DefenseLayer
}

// DefenseLayerRef is the defense layer of the associated product
// Since they are getting created in the instance, we'll create with
// all of the appropriate metadata.
type DefenseLayer struct {
	Name        string
	Description string
}

// Key is the composite identity restore uses to decide whether a target
// instance already has this tool: name + product ref + active state. Two
// tools sharing a name but differing in product or active state are
// considered different tools (see restore.go's reconcileDefenseTools).
func (d DefenseToolRef) Key() string {
	return defenseToolKey(d.Name, d.Product.Ref, d.Active)
}

// defenseToolKey is the shared key format used to correlate a DefenseToolRef
// (from a saved assessment) with a BlueTool on a target instance during
// restore -- see reconcileDefenseTools in restore.go.
func defenseToolKey(name, productRef string, active bool) string {
	return fmt.Sprintf("%s\x00%s\x00%t", name, productRef, active)
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
	OrgMap     OrgMapResource
	ToolsMap   ToolsMapResource
	IdToolsMap IdToolsMapResource
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

// isDuplicateGlobalIdError reports whether err is the GraphQL validation
// error VECTR returns when an assessment's globalId already exists in the
// target instance (this happens when restoring/transferring the same source
// assessment into an instance that already holds a copy with that globalId,
// e.g. under a different name). Callers can use this to point the user at
// RestoreOptionalParams.ResetGlobalId / --reset-id instead of surfacing a raw
// GraphQL validation error.
func isDuplicateGlobalIdError(err error) bool {
	gqlErrs, ok := err.(gqlerror.List)
	if !ok {
		return false
	}
	for _, e := range gqlErrs {
		for key, val := range e.Extensions {
			if !strings.Contains(strings.ToLower(key), "globalid") {
				continue
			}
			msgs, ok := val.([]any)
			if !ok {
				continue
			}
			for _, m := range msgs {
				if s, ok := m.(string); ok && strings.Contains(s, "Duplicate globalId") {
					return true
				}
			}
		}
	}
	return false
}
