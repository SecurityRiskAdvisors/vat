package vat_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"

	"sra/vat"
	"sra/vat/internal/dao"

	"pgregory.net/rapid"
)

// genFloat draws a value that safely round-trips through JSON (avoids
// NaN/Inf, which encoding/json cannot marshal).
func genFloat(t *rapid.T, label string) float64 {
	return float64(rapid.Int32().Draw(t, label))
}

func genOrganization(t *rapid.T, label string) dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentOrganizationsOrganization {
	return dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentOrganizationsOrganization{
		Id:           rapid.String().Draw(t, label+".Id"),
		Name:         rapid.String().Draw(t, label+".Name"),
		Abbreviation: rapid.String().Draw(t, label+".Abbreviation"),
		Description:  rapid.String().Draw(t, label+".Description"),
		Url:          rapid.String().Draw(t, label+".Url"),
		Offset:       rapid.Int().Draw(t, label+".Offset"),
		CreateTime:   genFloat(t, label+".CreateTime"),
		UpdateTime:   genFloat(t, label+".UpdateTime"),
	}
}

func genMetadata(t *rapid.T, label string) []dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentMetadataMetadataKeyValuePair {
	return rapid.SliceOfN(rapid.Custom(func(t *rapid.T) dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentMetadataMetadataKeyValuePair {
		return dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentMetadataMetadataKeyValuePair{
			Key:   rapid.String().Draw(t, "key"),
			Value: rapid.String().Draw(t, "value"),
		}
	}), 0, 3).Draw(t, label)
}

func genTestCase(t *rapid.T, label string) dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentCampaignsCampaignTestCasesTestCase {
	return dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentCampaignsCampaignTestCasesTestCase{
		Id:                 rapid.String().Draw(t, label+".Id"),
		Name:               rapid.String().Draw(t, label+".Name"),
		Description:        rapid.String().Draw(t, label+".Description"),
		Method:             rapid.String().Draw(t, label+".Method"),
		LibraryTestCaseId:  rapid.String().Draw(t, label+".LibraryTestCaseId"),
		MitreId:            rapid.String().Draw(t, label+".MitreId"),
		Deprecated:         rapid.Bool().Draw(t, label+".Deprecated"),
		AttackSuccess:      dao.AttackSuccessState(rapid.SampledFrom(dao.AllAttackSuccessState).Draw(t, label+".AttackSuccess")),
		OutcomeNotes:       rapid.String().Draw(t, label+".OutcomeNotes"),
		OperatorGuidance:   rapid.String().Draw(t, label+".OperatorGuidance"),
		Status:             rapid.String().Draw(t, label+".Status"),
		Offset:             rapid.Int().Draw(t, label+".Offset"),
		DetectionGuidance:  rapid.SliceOf(rapid.String()).Draw(t, label+".DetectionGuidance"),
		PreventionGuidance: rapid.SliceOf(rapid.String()).Draw(t, label+".PreventionGuidance"),
		References:         rapid.SliceOf(rapid.String()).Draw(t, label+".References"),
		UserContext:        rapid.String().Draw(t, label+".UserContext"),
		ImportTime:         genFloat(t, label+".ImportTime"),
		DataVer:            rapid.Int().Draw(t, label+".DataVer"),
		CreateTime:         genFloat(t, label+".CreateTime"),
		UpdateTime:         genFloat(t, label+".UpdateTime"),
		OverrideOutcome:    rapid.Bool().Draw(t, label+".OverrideOutcome"),
	}
}

func genCampaign(t *rapid.T, label string) dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentCampaignsCampaign {
	numTestCases := rapid.IntRange(0, 3).Draw(t, label+".numTestCases")
	testCases := make([]dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentCampaignsCampaignTestCasesTestCase, numTestCases)
	for i := range testCases {
		testCases[i] = genTestCase(t, label+".TestCase")
	}

	numOrgs := rapid.IntRange(0, 2).Draw(t, label+".numOrgs")
	orgs := make([]dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentCampaignsCampaignOrganizationsOrganization, numOrgs)
	for i := range orgs {
		orgs[i] = dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentCampaignsCampaignOrganizationsOrganization(genOrganization(t, label+".Org"))
	}

	return dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentCampaignsCampaign{
		Id:            rapid.String().Draw(t, label+".Id"),
		Name:          rapid.String().Draw(t, label+".Name"),
		Description:   rapid.String().Draw(t, label+".Description"),
		Icon:          rapid.String().Draw(t, label+".Icon"),
		Organizations: orgs,
		TestCases:     testCases,
		Offset:        rapid.Int().Draw(t, label+".Offset"),
		CreateTime:    genFloat(t, label+".CreateTime"),
		UpdateTime:    genFloat(t, label+".UpdateTime"),
	}
}

func genAssessment(t *rapid.T) dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessment {
	numOrgs := rapid.IntRange(0, 3).Draw(t, "assessment.numOrgs")
	orgs := make([]dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentOrganizationsOrganization, numOrgs)
	for i := range orgs {
		orgs[i] = genOrganization(t, "assessment.Org")
	}

	numCampaigns := rapid.IntRange(0, 3).Draw(t, "assessment.numCampaigns")
	campaigns := make([]dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentCampaignsCampaign, numCampaigns)
	for i := range campaigns {
		campaigns[i] = genCampaign(t, "assessment.Campaign")
	}

	return dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessment{
		Id:               rapid.String().Draw(t, "assessment.Id"),
		Name:             rapid.String().Draw(t, "assessment.Name"),
		Description:      rapid.String().Draw(t, "assessment.Description"),
		GlobalId:         rapid.String().Draw(t, "assessment.GlobalId"),
		Organizations:    orgs,
		Campaigns:        campaigns,
		AssessmentIds:    rapid.SliceOf(rapid.String()).Draw(t, "assessment.AssessmentIds"),
		Metadata:         genMetadata(t, "assessment.Metadata"),
		Offset:           rapid.Int().Draw(t, "assessment.Offset"),
		DefaultTcDataVer: rapid.Int().Draw(t, "assessment.DefaultTcDataVer"),
		ImportTime:       genFloat(t, "assessment.ImportTime"),
		CreateTime:       genFloat(t, "assessment.CreateTime"),
		UpdateTime:       genFloat(t, "assessment.UpdateTime"),
	}
}

func genLibraryTestCase(t *rapid.T, label string) dao.GetLibraryTestCasesLibraryTestcasesByIdsTestCaseConnectionNodesTestCase {
	return dao.GetLibraryTestCasesLibraryTestcasesByIdsTestCaseConnectionNodesTestCase{
		Id:                 rapid.String().Draw(t, label+".Id"),
		Name:               rapid.String().Draw(t, label+".Name"),
		Description:        rapid.String().Draw(t, label+".Description"),
		Method:             rapid.String().Draw(t, label+".Method"),
		LibraryTestCaseId:  rapid.String().Draw(t, label+".LibraryTestCaseId"),
		MitreId:            rapid.String().Draw(t, label+".MitreId"),
		AttackSuccess:      dao.AttackSuccessState(rapid.SampledFrom(dao.AllAttackSuccessState).Draw(t, label+".AttackSuccess")),
		OperatorGuidance:   rapid.String().Draw(t, label+".OperatorGuidance"),
		AutomationCmd:      rapid.String().Draw(t, label+".AutomationCmd"),
		AutomationExecutor: rapid.String().Draw(t, label+".AutomationExecutor"),
		DetectionGuidance:  rapid.SliceOf(rapid.String()).Draw(t, label+".DetectionGuidance"),
		PreventionGuidance: rapid.SliceOf(rapid.String()).Draw(t, label+".PreventionGuidance"),
		References:         rapid.SliceOf(rapid.String()).Draw(t, label+".References"),
		UserContext:        rapid.String().Draw(t, label+".UserContext"),
		ImportTime:         genFloat(t, label+".ImportTime"),
	}
}

func genLibraryTestCasesResource(t *rapid.T) vat.LibraryTestCasesResource {
	n := rapid.IntRange(0, 3).Draw(t, "libraryTestCases.n")
	m := make(vat.LibraryTestCasesResource, n)
	for i := 0; i < n; i++ {
		k := rapid.String().Draw(t, "libraryTestCases.key")
		m[k] = genLibraryTestCase(t, "libraryTestCases.value")
	}
	return m
}

func genToolsMap(t *rapid.T, label string) map[string]vat.GenericBlueTool {
	n := rapid.IntRange(0, 3).Draw(t, label+".n")
	m := make(map[string]vat.GenericBlueTool, n)
	for i := 0; i < n; i++ {
		k := rapid.String().Draw(t, label+".key")
		m[k] = vat.GenericBlueTool{
			Id:          rapid.String().Draw(t, label+".Id"),
			Name:        rapid.String().Draw(t, label+".Name"),
			ProductName: rapid.String().Draw(t, label+".ProductName"),
		}
	}
	return m
}

func genOrgMap(t *rapid.T) map[string]dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentOrganizationsOrganization {
	n := rapid.IntRange(0, 3).Draw(t, "orgMap.n")
	m := make(map[string]dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentOrganizationsOrganization, n)
	for i := 0; i < n; i++ {
		k := rapid.String().Draw(t, "orgMap.key")
		m[k] = genOrganization(t, "orgMap.value")
	}
	return m
}

func genAssessmentData(t *rapid.T) *vat.AssessmentData {
	return &vat.AssessmentData{
		AssessmentResource: vat.AssessmentResource{
			Assessment:         genAssessment(t),
			TemplateAssessment: rapid.String().Draw(t, "templateAssessment"),
			BundleID:           rapid.String().Draw(t, "bundleID"),
			BundlePrefix:       rapid.String().Draw(t, "bundlePrefix"),
		},
		ToolsMap:         genToolsMap(t, "toolsMap"),
		IdToolsMap:       genToolsMap(t, "idToolsMap"),
		OrgMap:           genOrgMap(t),
		LibraryTestCases: genLibraryTestCasesResource(t),
		Manifest: vat.Manifest{
			VatVersion:   rapid.String().Draw(t, "vatVersion"),
			VectrVersion: rapid.String().Draw(t, "vectrVersion"),
			Created:      rapid.String().Draw(t, "created"),
			// Version and Resources are intentionally left zero: EncodeToJson
			// always recomputes them (from FormatVersion and resourceRegistry
			// respectively), so a generated value here would never be what
			// round-trips back — that's asserted separately, not by DeepEqual
			// against this generator's output.
		},
	}
}

// TestEncodeDecodeRoundTrip is the primary correctness property for the
// envelope: any AssessmentData, encoded then decoded, must come back
// unchanged.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		original := genAssessmentData(t)

		encoded, err := vat.EncodeToJson(original)
		if err != nil {
			t.Fatalf("EncodeToJson failed: %s", err)
		}

		decoded, err := vat.DecodeJson(encoded)
		if err != nil {
			t.Fatalf("DecodeJson failed: %s", err)
		}

		if !reflect.DeepEqual(original.AssessmentResource, decoded.AssessmentResource) {
			t.Errorf("AssessmentResource did not round-trip:\nwant: %+v\ngot:  %+v", original.AssessmentResource, decoded.AssessmentResource)
		}
		if !reflect.DeepEqual(original.LibraryTestCases, decoded.LibraryTestCases) {
			t.Errorf("LibraryTestCases did not round-trip:\nwant: %+v\ngot:  %+v", original.LibraryTestCases, decoded.LibraryTestCases)
		}
		if decoded.Manifest.VatVersion != original.Manifest.VatVersion {
			t.Errorf("vat-version did not round-trip: want %q, got %q", original.Manifest.VatVersion, decoded.Manifest.VatVersion)
		}
		if decoded.Manifest.VectrVersion != original.Manifest.VectrVersion {
			t.Errorf("vectr-version did not round-trip: want %q, got %q", original.Manifest.VectrVersion, decoded.Manifest.VectrVersion)
		}
		if decoded.Manifest.Created != original.Manifest.Created {
			t.Errorf("created did not round-trip: want %q, got %q", original.Manifest.Created, decoded.Manifest.Created)
		}
	})
}

// TestDecodeSkipsUnknownResources is the forward-compat property: a manifest
// listing a resource name this vat build doesn't recognize (simulating a
// future resource, e.g. "rta", written by a newer vat) must not prevent
// decoding of the resources it does recognize, and must not error.
func TestDecodeSkipsUnknownResources(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		original := genAssessmentData(t)
		encoded, err := vat.EncodeToJson(original)
		if err != nil {
			t.Fatalf("EncodeToJson failed: %s", err)
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &raw); err != nil {
			t.Fatalf("could not unmarshal envelope: %s", err)
		}

		var manifest vat.Manifest
		if err := json.Unmarshal(raw["manifest"], &manifest); err != nil {
			t.Fatalf("could not unmarshal manifest: %s", err)
		}

		unknownName := rapid.StringMatching(`unknown-[a-z]{3,10}`).Draw(t, "unknownResourceName")
		manifest.Resources = append(manifest.Resources, unknownName)

		var data map[string]json.RawMessage
		if err := json.Unmarshal(raw["data"], &data); err != nil {
			t.Fatalf("could not unmarshal data: %s", err)
		}
		data[unknownName] = json.RawMessage(`{"future":"field"}`)

		manifestRaw, err := json.Marshal(manifest)
		if err != nil {
			t.Fatalf("could not marshal manifest: %s", err)
		}
		dataRaw, err := json.Marshal(data)
		if err != nil {
			t.Fatalf("could not marshal data: %s", err)
		}
		raw["manifest"] = manifestRaw
		raw["data"] = dataRaw

		mutated, err := json.Marshal(raw)
		if err != nil {
			t.Fatalf("could not marshal mutated envelope: %s", err)
		}

		decoded, err := vat.DecodeJson(mutated)
		if err != nil {
			t.Fatalf("DecodeJson failed on envelope with an unknown resource: %s", err)
		}
		if !reflect.DeepEqual(original.AssessmentResource, decoded.AssessmentResource) {
			t.Errorf("known resource AssessmentResource was disturbed by an unknown resource")
		}
		if !reflect.DeepEqual(original.LibraryTestCases, decoded.LibraryTestCases) {
			t.Errorf("known resource LibraryTestCases was disturbed by an unknown resource")
		}
	})
}

// TestDecodeRejectsMissingManifest pins the clean-break decision: vat 1.0's
// old flat (pre-envelope) AssessmentData JSON is not a supported input and
// must fail loudly, not silently fall back.
func TestDecodeRejectsMissingManifest(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{"legacy flat format", `{"Assessment":{"name":"foo"},"LibraryTestCases":{}}`},
		{"empty object", `{}`},
		{"manifest with no resources", `{"manifest":{"version":"1.0","resources":[]},"data":{}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := vat.DecodeJson([]byte(tc.json))
			if err == nil {
				t.Fatalf("expected an error decoding %s, got nil", tc.name)
			}
		})
	}
}

// TestResourceRequirements pins the known required/optional classification of
// today's resources, so a future accidental flip is caught here rather than
// surfacing as a confusing failure deep in restore.go.
//
// It is driven off vat.ResourceNames() rather than a fixed list: adding a new
// resource to resourceRegistry (format.go) makes it show up here
// automatically, and this test fails loudly — pointing back at itself —
// until its classification is added to expectedRequired below. That failure
// is the intended mechanism for "don't forget to classify the new resource";
// nothing else in the test suite reminds you to update this map.
func TestResourceRequirements(t *testing.T) {
	expectedRequired := map[string]bool{
		vat.ResourceAssessment:       true,
		vat.ResourceLibraryTestCases: true,
		vat.ResourceOrgMap:           true,
		vat.ResourceToolsMap:         true,
		vat.ResourceIdToolsMap:       true,
	}

	names := vat.ResourceNames()
	for _, name := range names {
		want, ok := expectedRequired[name]
		if !ok {
			t.Fatalf("resource %q is registered in resourceRegistry but has no expected "+
				"requiredness pinned in TestResourceRequirements' expectedRequired map "+
				"(format_test.go) — add it there, and if it's optional, "+
				"TestDecodeMissingOptionalResourceSucceeds will pick it up automatically", name)
		}
		if got := vat.IsResourceRequired(name); got != want {
			t.Errorf("expected %q required=%v, got %v", name, want, got)
		}
	}
	for name := range expectedRequired {
		if !slices.Contains(names, name) {
			t.Errorf("expectedRequired references %q but it is no longer a registered resource", name)
		}
	}

	if vat.IsResourceRequired("some-unknown-resource") {
		t.Errorf("expected an unrecognized resource name to be treated as optional")
	}
}

// TestDecodeMissingRequiredResourceFails is the flow-driving property: if a
// required resource (assessment) is absent from a file, DecodeJson must fail
// with ErrMissingRequiredResource rather than returning a zero-value
// AssessmentData that would fail confusingly deep inside RestoreAssessment.
func TestDecodeMissingRequiredResourceFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		original := genAssessmentData(t)
		encoded, err := vat.EncodeToJson(original)
		if err != nil {
			t.Fatalf("EncodeToJson failed: %s", err)
		}

		var env map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &env); err != nil {
			t.Fatalf("could not unmarshal envelope: %s", err)
		}
		var manifest vat.Manifest
		if err := json.Unmarshal(env["manifest"], &manifest); err != nil {
			t.Fatalf("could not unmarshal manifest: %s", err)
		}
		var data map[string]json.RawMessage
		if err := json.Unmarshal(env["data"], &data); err != nil {
			t.Fatalf("could not unmarshal data: %s", err)
		}

		// Drop the required "assessment" resource entirely, keep the
		// optional "librarytestcases" resource intact.
		manifest.Resources = []string{vat.ResourceLibraryTestCases}
		delete(data, vat.ResourceAssessment)

		manifestRaw, err := json.Marshal(manifest)
		if err != nil {
			t.Fatalf("could not marshal manifest: %s", err)
		}
		dataRaw, err := json.Marshal(data)
		if err != nil {
			t.Fatalf("could not marshal data: %s", err)
		}
		env["manifest"] = manifestRaw
		env["data"] = dataRaw

		mutated, err := json.Marshal(env)
		if err != nil {
			t.Fatalf("could not marshal mutated envelope: %s", err)
		}

		_, err = vat.DecodeJson(mutated)
		if err == nil {
			t.Fatalf("expected an error decoding a file missing the required %q resource, got nil", vat.ResourceAssessment)
		}
		if !errors.Is(err, vat.ErrMissingRequiredResource) {
			t.Errorf("expected errors.Is(err, vat.ErrMissingRequiredResource), got: %s", err)
		}
	})
}

// TestDecodeMissingOptionalResourceSucceeds mirrors the above for an
// optional resource: dropping any one optional resource entirely must not
// fail decoding, and the remaining resources must still round-trip
// correctly.
//
// Which resource (if any) is optional is discovered from vat.ResourceNames()
// rather than hardcoded, since that's a property of the current registry,
// not of this test. Today every registered resource is required, so this
// test has nothing to exercise and skips itself; it starts running again
// the moment a resource is registered as ResourceOptional, with no edit
// needed here.
func TestDecodeMissingOptionalResourceSucceeds(t *testing.T) {
	var optional string
	for _, name := range vat.ResourceNames() {
		if !vat.IsResourceRequired(name) {
			optional = name
			break
		}
	}
	if optional == "" {
		t.Skip("no optional resources are currently registered in resourceRegistry")
	}

	rapid.Check(t, func(t *rapid.T) {
		original := genAssessmentData(t)
		encoded, err := vat.EncodeToJson(original)
		if err != nil {
			t.Fatalf("EncodeToJson failed: %s", err)
		}

		var env map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &env); err != nil {
			t.Fatalf("could not unmarshal envelope: %s", err)
		}
		var manifest vat.Manifest
		if err := json.Unmarshal(env["manifest"], &manifest); err != nil {
			t.Fatalf("could not unmarshal manifest: %s", err)
		}
		var data map[string]json.RawMessage
		if err := json.Unmarshal(env["data"], &data); err != nil {
			t.Fatalf("could not unmarshal data: %s", err)
		}

		manifest.Resources = slices.DeleteFunc(manifest.Resources, func(r string) bool {
			return r == optional
		})
		delete(data, optional)

		manifestRaw, err := json.Marshal(manifest)
		if err != nil {
			t.Fatalf("could not marshal manifest: %s", err)
		}
		dataRaw, err := json.Marshal(data)
		if err != nil {
			t.Fatalf("could not marshal data: %s", err)
		}
		env["manifest"] = manifestRaw
		env["data"] = dataRaw

		mutated, err := json.Marshal(env)
		if err != nil {
			t.Fatalf("could not marshal mutated envelope: %s", err)
		}

		if _, err := vat.DecodeJson(mutated); err != nil {
			t.Fatalf("did not expect an error decoding a file missing the optional %q resource: %s", optional, err)
		}
		// Future improvement: also assert the decoded AssessmentData reflects
		// the absent resource as its zero value and leaves other resources
		// undisturbed, as the old hardcoded version of this test did for
		// "librarytestcases". Doing that generically would need a way to map
		// a resource name to the AssessmentData field(s) it backs.
	})
}

// TestAssessmentDataFieldsAreAccountedFor is the guardrail against silently
// adding a field to AssessmentData without deciding whether it's a resource.
// format.go's resourceRegistry is a single source of truth for
// encode/decode/requiredness, but Go has no compile-time check that every
// AssessmentData field is registered there — nothing stops someone from
// adding a field and forgetting to wire it up. This test enumerates
// AssessmentData's fields via reflection and fails if it finds one that
// isn't explicitly classified below, forcing a conscious decision at the
// point a field is added rather than a silent gap discovered later.
func TestAssessmentDataFieldsAreAccountedFor(t *testing.T) {
	// Fields backed by an entry in format.go's resourceRegistry (i.e. they
	// round-trip through the wire envelope as a named resource).
	resourceBackedFields := map[string]bool{
		"AssessmentResource": true, // backs vat.ResourceAssessment
		"LibraryTestCases":   true, // backs vat.ResourceLibraryTestCases
		"OrgMap":             true, // backs vat.ResourceOrgMap
		"ToolsMap":           true, // backs vat.ResourceToolsMap
		"IdToolsMap":         true, // backs vat.ResourceIdToolsMap
	}
	// Fields that are part of the wire file but travel via the envelope's
	// manifest, not through resourceRegistry's per-resource dispatch.
	manifestFields := map[string]bool{
		"Manifest": true,
	}
	// Fields that are deliberately runtime-only and never appear in the
	// wire file. Empty today: restore-time bookkeeping (e.g. what used to
	// be "RestoreInfo") is a local variable in restore.go, not a stored
	// field — nothing re-serializes AssessmentData after a restore, so it
	// never needs to live here. Kept as a category for a future field that
	// might genuinely need runtime-only storage.
	runtimeOnlyFields := map[string]bool{}

	typ := reflect.TypeOf(vat.AssessmentData{})
	seen := make(map[string]bool, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		seen[name] = true
		if resourceBackedFields[name] || manifestFields[name] || runtimeOnlyFields[name] {
			continue
		}
		t.Errorf("AssessmentData has a new field %q that isn't accounted for here: "+
			"either register it as a resource in resourceRegistry (format.go) and add it "+
			"to resourceBackedFields above, or confirm it's intentionally runtime-only and "+
			"add it to runtimeOnlyFields above (or manifestFields, if it travels via the "+
			"envelope's manifest instead)", name)
	}

	// Guard the other direction too, so the allowlists above don't rot into
	// stale entries for fields that no longer exist.
	for _, allowlist := range []map[string]bool{resourceBackedFields, manifestFields, runtimeOnlyFields} {
		for name := range allowlist {
			if !seen[name] {
				t.Errorf("an allowlist lists %q but AssessmentData no longer has that field", name)
			}
		}
	}
}
