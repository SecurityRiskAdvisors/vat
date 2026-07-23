package vat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"sra/vat/internal/dao"
)

// FormatVersion is the current envelope/manifest wire format version.
//
// Compatibility convention (documented, not tooling-enforced):
//   - Small change (add/remove/rename a field within an existing resource):
//     no version bump. Decoding is permissive by design (unknown fields are
//     ignored, missing fields default to zero values) — this is the normal,
//     expected way a resource evolves over time.
//   - Large change (a change to a resource that would break decoding of
//     existing data, or an entirely new kind of entity such as RTA): do not
//     mutate the existing resource in place. Introduce a new resource name
//     instead (registered in resourceRegistry below), so old decoders that
//     don't know the new name simply skip it and keep working.
//   - FormatVersion itself only changes when the envelope/manifest mechanism
//     changes shape (e.g. how Manifest or the resource dispatch loop work),
//     not for routine resource evolution or the addition of new resources.
//
// Zero-value hazard: a missing field defaulting to Go's zero value is only a
// safe "small change" if that zero value is a legitimate "unset" state for
// every consumer of the field, forever. Fields where "false"/"" and "not
// present" must be distinguishable need a pointer or an explicit sentinel —
// don't rely on bare zero values for those.
const FormatVersion = "1.0"

// Known resource names.
const (
	ResourceAssessment       = "assessment"
	ResourceLibraryTestCases = "librarytestcases"
	ResourceOrgMap           = "orgmap"
)

// ResourceRequirement describes whether a resource must be present for vat
// to be able to proceed at all, or whether the application can carry on
// without it (in a reduced-fidelity way).
type ResourceRequirement bool

const (
	// ResourceRequired means DecodeJson fails outright if the resource is
	// absent from the file — the rest of vat has no meaningful fallback.
	ResourceRequired ResourceRequirement = true
	// ResourceOptional means the resource may be entirely absent from a
	// file; downstream flow (e.g. restore.go) is expected to check for its
	// absence (e.g. an empty LibraryTestCases map) and adjust accordingly.
	ResourceOptional ResourceRequirement = false
)

// ErrMissingRequiredResource is returned by DecodeJson when a resource
// registered as ResourceRequired is absent from the file.
var ErrMissingRequiredResource = errors.New("missing required resource")

// resourceDescriptor is the single registration point for a resource: its
// name, whether it's required, and how to move it between AssessmentData and
// its raw wire payload. EncodeToJson and DecodeJson both drive off the same
// resourceRegistry slice, so there is exactly one place to add a resource —
// no separate encode map, decode switch, and requirements map to keep in
// sync by hand.
//
// Adding a new resource (e.g. "rta"): add one entry here. Vat binaries built
// before that entry exists will slog.Warn and skip the resource rather than
// failing (see DecodeJson).
type resourceDescriptor struct {
	Name     string
	Required ResourceRequirement
	Encode   func(*AssessmentData) (json.RawMessage, error)
	Decode   func(*AssessmentData, json.RawMessage) error
}

var resourceRegistry = []resourceDescriptor{
	{
		Name:     ResourceAssessment,
		Required: ResourceRequired,
		Encode: func(a *AssessmentData) (json.RawMessage, error) {
			return json.Marshal(a.AssessmentResource)
		},
		Decode: func(a *AssessmentData, raw json.RawMessage) error {
			return json.Unmarshal(raw, &a.AssessmentResource)
		},
	},
	{
		Name:     ResourceLibraryTestCases,
		Required: ResourceRequired,
		Encode: func(a *AssessmentData) (json.RawMessage, error) {
			return json.Marshal(a.LibraryTestCases)
		},
		Decode: func(a *AssessmentData, raw json.RawMessage) error {
			return json.Unmarshal(raw, &a.LibraryTestCases)
		},
	},
	{
		Name:     ResourceOrgMap,
		Required: ResourceRequired,
		Encode: func(a *AssessmentData) (json.RawMessage, error) {
			return json.Marshal(a.OrgMap)
		},
		Decode: func(a *AssessmentData, raw json.RawMessage) error {
			return json.Unmarshal(raw, &a.OrgMap)
		},
	},
}

// IsResourceRequired reports whether name is a resource vat cannot function
// without. An unrecognized resource name is always treated as optional: a
// resource this vat build doesn't know about can never be one it requires.
func IsResourceRequired(name string) bool {
	for _, d := range resourceRegistry {
		if d.Name == name {
			return d.Required == ResourceRequired
		}
	}
	return false
}

// ResourceNames returns the names of every resource registered in
// resourceRegistry, in registration order. It exists so callers outside this
// package (notably tests) can enumerate known resources without duplicating
// resourceRegistry's contents by hand.
func ResourceNames() []string {
	names := make([]string, len(resourceRegistry))
	for i, d := range resourceRegistry {
		names[i] = d.Name
	}
	return names
}

// Manifest describes the contents of a serialized vat file. It deliberately
// separates two unrelated "versions" that used to share ambiguous naming:
//   - FormatVersion is a property of the envelope mechanism itself (see the
//     FormatVersion constant above) — the same for every file this vat build
//     writes, regardless of what's inside it.
//   - VatVersion/VectrVersion/Created are this file's own save-time
//     provenance (who saved it, against what VECTR version, when). There is
//     no separate shadow copy of this information; save.go stamps it
//     directly onto AssessmentData.Manifest via NewManifestMetadata, and
//     DecodeJson hands back exactly what was in the file.
type Manifest struct {
	FormatVersion string   `json:"version"`
	VectrVersion  string   `json:"vectr-version"`
	Resources     []string `json:"resources"`
	Created       string   `json:"created"`
	VatVersion    string   `json:"vat-version"`
}

// NewManifestMetadata stamps save-time provenance (vat/VECTR versions from
// ctx, plus the current time) for a Manifest about to be attached to
// AssessmentData ahead of a save. FormatVersion and Resources are left unset
// here — EncodeToJson fills those in from the FormatVersion constant and
// resourceRegistry at encode time.
func NewManifestMetadata(ctx context.Context) Manifest {
	version, vectrVersion := versionsFromContext(ctx)
	return Manifest{
		VatVersion:   version,
		VectrVersion: vectrVersion,
		Created:      time.Now().Format(time.RFC3339),
	}
}

// asMap renders m's provenance fields as the unprefixed {version, date,
// vectr-version} shape used for display (see diag.go).
func (m Manifest) asMap() map[string]string {
	return map[string]string{
		"version":       orDefault(m.VatVersion, "none_found"),
		"date":          orDefault(m.Created, "none_found"),
		"vectr-version": orDefault(m.VectrVersion, "none_found"),
	}
}

// asPrefixedMap renders m's provenance fields prefixed (e.g.
// "vat-save-version"), for writing into VECTR's own generic metadata
// key/value pairs.
func (m Manifest) asPrefixedMap(prefix string) map[string]string {
	r := make(map[string]string, 3)
	for k, val := range m.asMap() {
		r[prefix+"-"+k] = val
	}
	return r
}

// envelope is the on-disk wrapper: a manifest describing what's present, and
// the raw per-resource payloads keyed by resource name.
type envelope struct {
	Manifest Manifest                   `json:"manifest"`
	Data     map[string]json.RawMessage `json:"data"`
}

// AssessmentResource is the "assessment" resource. It is used both as the
// in-memory representation (embedded into AssessmentData) and directly as
// the wire payload for the "assessment" entry in the envelope.
type AssessmentResource struct {
	Assessment         dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessment
	TemplateAssessment string
	ToolsMap           map[string]GenericBlueTool
	IdToolsMap         map[string]GenericBlueTool
	BundleID           string
	BundlePrefix       string
}

// LibraryTestCasesResource is the "librarytestcases" resource: library test
// cases referenced by an assessment's campaigns, keyed by library test case
// id.
type LibraryTestCasesResource map[string]dao.GetLibraryTestCasesLibraryTestcasesByIdsTestCaseConnectionNodesTestCase

type OrgMapResource map[string]dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentOrganizationsOrganization

// EncodeToJson serializes an AssessmentData into the manifest+resource
// envelope wire format.
func EncodeToJson(data *AssessmentData) ([]byte, error) {
	// data.Manifest already carries save-time provenance (VatVersion,
	// VectrVersion, Created) stamped by NewManifestMetadata at save time;
	// only the format version and the resource list are recomputed here,
	// from resourceRegistry, since those describe this encoding operation
	// itself rather than when the assessment was originally saved.
	manifest := data.Manifest
	manifest.FormatVersion = FormatVersion
	manifest.Resources = make([]string, 0, len(resourceRegistry))

	env := envelope{
		Manifest: manifest,
		Data:     make(map[string]json.RawMessage, len(resourceRegistry)),
	}

	for _, d := range resourceRegistry {
		payload, err := d.Encode(data)
		if err != nil {
			return nil, fmt.Errorf("could not marshal %s resource: %w", d.Name, err)
		}
		env.Manifest.Resources = append(env.Manifest.Resources, d.Name)
		env.Data[d.Name] = payload
	}

	jsonData, err := json.MarshalIndent(env, "", "\t")
	if err != nil {
		return nil, err
	}
	return jsonData, nil
}

// DecodeJson deserializes the manifest+resource envelope wire format into an
// AssessmentData. Vat 1.0's old flat (pre-envelope) format is not a
// supported input: a file with no manifest is a hard error, not a silent
// fallback.
func DecodeJson(raw []byte) (*AssessmentData, error) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}

	if env.Manifest.FormatVersion == "" || len(env.Manifest.Resources) == 0 {
		return nil, fmt.Errorf("missing or empty manifest: this file is not in the vat 2.0+ envelope format")
	}

	a := &AssessmentData{
		Manifest: env.Manifest,
	}

	registryByName := make(map[string]resourceDescriptor, len(resourceRegistry))
	for _, d := range resourceRegistry {
		registryByName[d.Name] = d
	}

	decoded := make(map[string]bool, len(env.Manifest.Resources))
	for _, name := range env.Manifest.Resources {
		payload, ok := env.Data[name]
		if !ok {
			slog.Warn("resource listed in manifest but missing from data, skipping", "resource", name)
			continue
		}
		d, known := registryByName[name]
		if !known {
			slog.Warn("skipping unknown resource, this vat version does not understand it", "resource", name)
			continue
		}
		if err := d.Decode(a, payload); err != nil {
			return nil, fmt.Errorf("could not decode %s resource: %w", name, err)
		}
		decoded[name] = true
	}

	var err error
	for _, d := range resourceRegistry {
		if d.Required == ResourceRequired && !decoded[d.Name] {
			err = errors.Join(err, fmt.Errorf("resource %q: %w", d.Name, ErrMissingRequiredResource))
		}
	}
	if err != nil {
		return nil, err
	}

	return a, nil
}
