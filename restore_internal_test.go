package vat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"sra/vat/internal/dao"

	"github.com/Khan/genqlient/graphql"
	"pgregory.net/rapid"
)

// stubGraphQLClient is a minimal graphql.Client that returns a fixed,
// zero-value response for a known set of operation names and errors for
// anything else -- enough to drive restoreCampaigns up to the point under
// test without needing a full mutation-response mock.
type stubGraphQLClient struct {
	ops map[string]bool
}

func (s *stubGraphQLClient) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	if !s.ops[req.OpName] {
		return fmt.Errorf("stubGraphQLClient: no stubbed response for operation %q", req.OpName)
	}
	return nil
}

// TestRestoreCampaigns_TestCaseMissingOrganization verifies vat's policy that
// OrgMap is a resource vat itself manages and requires: a test case with no
// organization must fail restoreCampaigns fast with a clear error, rather
// than being silently restored with a blank organization and left for VECTR
// to reject downstream.
func TestRestoreCampaigns_TestCaseMissingOrganization(t *testing.T) {
	client := &stubGraphQLClient{ops: map[string]bool{"CreateCampaigns": true}}

	campaignsToRestore := []dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentCampaignsCampaign{
		{
			Name: "campaign-1",
			TestCases: []dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentCampaignsCampaignTestCasesTestCase{
				{
					Id:            "tc-1",
					Name:          "test case with no organization",
					Organizations: nil,
				},
			},
		},
	}

	err := restoreCampaigns(
		context.Background(),
		client,
		"test-db",
		"assessment-1",
		"assessment-name",
		campaignsToRestore,
		map[string]dao.FindOrganizationOrganizationsOrganizationConnectionNodesOrganization{},
		map[string]string{},
		map[string]DefenseToolRef{},
		&RestoreOptionalParams{},
	)
	if err == nil {
		t.Fatal("expected an error for a test case with no organization, got nil")
	}
	if !strings.Contains(err.Error(), "tc-1") {
		t.Errorf("expected error to identify the offending test case (tc-1), got: %v", err)
	}
}

// TestCreateTemplateData_MissingOrganization verifies the same policy for
// the library-test-case-template path: createTemplateData must return a
// fatal error for a test case with no organization, not silently produce a
// template with a blank organization.
func TestCreateTemplateData_MissingOrganization(t *testing.T) {
	template_test_case := dao.GetLibraryTestCasesLibraryTestcasesByIdsTestCaseConnectionNodesTestCase{
		Name:              "template test case with no organization",
		LibraryTestCaseId: "lib-1",
		Organizations:     nil,
	}

	_, warnings, err := createTemplateData(template_test_case)
	if err == nil {
		t.Fatal("expected a fatal error for a template test case with no organization, got nil")
	}
	if !strings.Contains(err.Error(), "lib-1") {
		t.Errorf("expected error to identify the offending library test case (lib-1), got: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no recoverable warnings alongside the fatal error, got: %v", warnings)
	}
}

// TestGroupedCreateTestCaseWithLibraryIdInput_Batching is a property test:
// for any assignment of source test cases to library test case IDs (with
// repeats allowed, to simulate the same library test case used multiple
// times in a campaign), GenerateInsertsData must split entries into exactly
// maxGroupSize batches, never put the same libraryTestCaseId in a batch
// twice, and account for every added entry (identified by its
// TestCaseData.ClientId) exactly once.
func TestGroupedCreateTestCaseWithLibraryIdInput_Batching(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numLibraryIds := rapid.IntRange(1, 6).Draw(t, "numLibraryIds")

		g := NewGroupedCreateTestCaseWithLibraryIdInput("test-db", "campaign-1")

		maxGroupSize := 0
		total := 0
		clientCounter := 0
		wantClientIds := make(map[string]bool)

		for i := 0; i < numLibraryIds; i++ {
			libId := fmt.Sprintf("lib-%d", i)
			count := rapid.IntRange(0, 5).Draw(t, fmt.Sprintf("count-%d", i))
			if count > maxGroupSize {
				maxGroupSize = count
			}
			for j := 0; j < count; j++ {
				clientId := fmt.Sprintf("src-%d", clientCounter)
				clientCounter++
				total++
				wantClientIds[clientId] = true
				g.Add(dao.CreateTestCaseDataWithLibraryIdInput{
					LibraryTestCaseId: libId,
					TestCaseData:      dao.CreateTestCaseDataInput{ClientId: clientId},
				})
			}
		}

		if got := g.Len(); got != total {
			t.Fatalf("Len() = %d, want %d", got, total)
		}

		batches := g.GenerateInsertsData()
		if total == 0 {
			if batches != nil {
				t.Fatalf("expected nil batches for empty input, got %d", len(batches))
			}
			return
		}
		if len(batches) != maxGroupSize {
			t.Fatalf("got %d batches, want %d (max group size)", len(batches), maxGroupSize)
		}

		seenClientIds := make(map[string]bool)
		totalEntries := 0
		for bi, batch := range batches {
			seenLibIdsInBatch := make(map[string]bool)
			for _, input := range batch.CreateTestCaseInputs {
				if seenLibIdsInBatch[input.LibraryTestCaseId] {
					t.Fatalf("batch %d contains duplicate libraryTestCaseId %q", bi, input.LibraryTestCaseId)
				}
				seenLibIdsInBatch[input.LibraryTestCaseId] = true

				clientId := input.TestCaseData.ClientId
				if seenClientIds[clientId] {
					t.Fatalf("clientId %q appeared in more than one batch", clientId)
				}
				seenClientIds[clientId] = true
				totalEntries++
			}
		}

		if totalEntries != total {
			t.Fatalf("total entries across all batches = %d, want %d", totalEntries, total)
		}
		for clientId := range wantClientIds {
			if !seenClientIds[clientId] {
				t.Fatalf("clientId %q was added but never appeared in any batch", clientId)
			}
		}
	})
}

// scriptedGraphQLClient serves a fixed JSON response per operation name, and
// records every operation it's asked to serve -- along with the variables it
// was sent -- enough to drive reconcileDefenseTools through a specific
// branch and assert which create/update mutations it did (or didn't) call,
// and with what input.
type scriptedGraphQLClient struct {
	responses map[string]json.RawMessage
	calls     []string
	variables map[string]json.RawMessage
}

func (s *scriptedGraphQLClient) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	s.calls = append(s.calls, req.OpName)
	if raw, err := json.Marshal(req.Variables); err == nil {
		if s.variables == nil {
			s.variables = make(map[string]json.RawMessage)
		}
		s.variables[req.OpName] = raw
	}
	raw, ok := s.responses[req.OpName]
	if !ok {
		return fmt.Errorf("scriptedGraphQLClient: no stubbed response for operation %q", req.OpName)
	}
	return json.Unmarshal(raw, resp.Data)
}

func (s *scriptedGraphQLClient) called(op string) bool {
	return slices.Contains(s.calls, op)
}

// existingToolRef is the DefenseToolRef a saved assessment would carry for a
// tool that already exists, unmodified, in the target instance below.
var existingToolRef = DefenseToolRef{
	Name:        "Falcon Sensor",
	Description: "EDR agent",
	Active:      true,
	Layers:      []string{"Endpoint"},
	Product: DefenseToolProductRef{
		Ref:        "crowdstrike-falcon",
		Name:       "Falcon",
		VendorName: "CrowdStrike",
	},
}

const existingToolsResponse = `{
	"bluetools": {
		"nodes": [{
			"id": "target-tool-1",
			"name": "Falcon Sensor",
			"description": "EDR agent",
			"active": true,
			"defensiveLayers": [{"id": "target-layer-endpoint", "name": "Endpoint"}],
			"defenseToolProduct": {
				"id": "target-product-1",
				"name": "Falcon",
				"ref": "crowdstrike-falcon",
				"description": "",
				"icon": "",
				"vendor": {"name": "CrowdStrike"}
			},
			"createTime": 1,
			"updateTime": 1
		}]
	}
}`

const emptyProductsResponse = `{"defenseToolProducts": {"nodes": []}}`

// existingProductsResponse mirrors the product already attached to the tool
// in existingToolsResponse (same id/ref/name) -- reconcileDefenseTools
// resolves a ref's product before checking for a matching existing tool, so
// this must be present for CleanMatch/MissingLayers to actually reach that
// tool via a product match rather than trying to create a duplicate one.
const existingProductsResponse = `{"defenseToolProducts": {"nodes": [
	{"id": "target-product-1", "name": "Falcon", "ref": "crowdstrike-falcon"}
]}}`
const emptyLayersResponse = `{"defensivelayers": {"nodes": []}}`
const singleEndpointLayerResponse = `{"defensivelayers": {"nodes": [{"id": "target-layer-endpoint", "name": "Endpoint"}]}}`
const emptyLibraryLayersResponse = `{"libraryDefensivelayers": {"nodes": []}}`

// TestReconcileDefenseTools_CleanMatch verifies that a source tool matching
// an existing target tool on name+product ref+active, with no layers
// beyond what the target already has, is reused as-is: no
// create/update mutation is called, and the resolved id is the existing
// tool's id.
func TestReconcileDefenseTools_CleanMatch(t *testing.T) {
	client := &scriptedGraphQLClient{responses: map[string]json.RawMessage{
		"GetAllDefenseTools":           json.RawMessage(existingToolsResponse),
		"GetAllDefenseToolProducts":    json.RawMessage(existingProductsResponse),
		"GetAllDefensiveLayers":        json.RawMessage(emptyLayersResponse),
		"GetAllLibraryDefensiveLayers": json.RawMessage(emptyLibraryLayersResponse),
	}}

	result, err := reconcileDefenseTools(context.Background(), client, "test-db", map[string]DefenseToolRef{
		existingToolRef.Key(): existingToolRef,
	})
	if err != nil {
		t.Fatalf("reconcileDefenseTools returned an error: %v", err)
	}
	if got := result[existingToolRef.Key()]; got != "target-tool-1" {
		t.Errorf("resolved id = %q, want %q", got, "target-tool-1")
	}
	if client.called("CreateDefenseTool") || client.called("UpdateDefenseTool") {
		t.Errorf("expected no create/update mutation for a clean match, calls: %v", client.calls)
	}
}

// TestReconcileDefenseTools_MissingLayers verifies that a source tool
// matching an existing target tool on name+product ref+active, but with a
// defense layer the target tool lacks, creates the missing layer and
// updates the existing tool with the union of its old and new layer ids
// (rather than creating a whole new tool).
func TestReconcileDefenseTools_MissingLayers(t *testing.T) {
	ref := existingToolRef
	ref.Layers = []string{"Endpoint", "Network"}

	client := &scriptedGraphQLClient{responses: map[string]json.RawMessage{
		"GetAllDefenseTools":           json.RawMessage(existingToolsResponse),
		"GetAllDefenseToolProducts":    json.RawMessage(existingProductsResponse),
		"GetAllDefensiveLayers":        json.RawMessage(emptyLayersResponse),
		"GetAllLibraryDefensiveLayers": json.RawMessage(emptyLibraryLayersResponse),
		"CreateLibraryDefenseLayer": json.RawMessage(`{
			"defenseLayer": {"createLibrary": {"defenseLayers": [{"id": "target-library-layer-network", "name": "Network"}]}}
		}`),
		"CloneDefenseLayer": json.RawMessage(`{
			"defenseLayer": {"clone": {"defenseLayers": [{"id": "target-layer-network", "name": "Network"}]}}
		}`),
		"UpdateDefenseTool": json.RawMessage(`{
			"defenseTool": {"update": {"defenseTools": [{
				"id": "target-tool-1",
				"name": "Falcon Sensor",
				"active": true,
				"description": "EDR agent",
				"defenseToolProduct": {"id": "target-product-1", "ref": "crowdstrike-falcon"},
				"defensiveLayers": [
					{"id": "target-layer-endpoint", "name": "Endpoint"},
					{"id": "target-layer-network", "name": "Network"}
				]
			}]}}
		}`),
	}}

	result, err := reconcileDefenseTools(context.Background(), client, "test-db", map[string]DefenseToolRef{
		ref.Key(): ref,
	})
	if err != nil {
		t.Fatalf("reconcileDefenseTools returned an error: %v", err)
	}
	if got := result[ref.Key()]; got != "target-tool-1" {
		t.Errorf("resolved id = %q, want %q", got, "target-tool-1")
	}
	if !client.called("CloneDefenseLayer") {
		t.Error("expected the missing layer to be created")
	}
	if !client.called("UpdateDefenseTool") {
		t.Error("expected the existing tool to be updated with the new layer")
	}
	if client.called("CreateDefenseTool") {
		t.Error("expected no new tool to be created for a name+product+active match")
	}
}

// TestReconcileDefenseTools_ToolMatchViaProductRefMismatch verifies that a
// source tool is still matched against an existing target tool even when the
// source and target product refs differ -- VECTR generates ref as a random
// string independently on every instance, so two instances' refs for
// logically the same product (matched by name) are never expected to be
// equal. Matching must go through the resolved target product id, not a raw
// ref comparison, or this would incorrectly create a duplicate tool.
func TestReconcileDefenseTools_ToolMatchViaProductRefMismatch(t *testing.T) {
	ref := existingToolRef
	ref.Product.Ref = "some-other-ref-from-the-source-instance"

	client := &scriptedGraphQLClient{responses: map[string]json.RawMessage{
		"GetAllDefenseTools":           json.RawMessage(existingToolsResponse),
		"GetAllDefenseToolProducts":    json.RawMessage(existingProductsResponse),
		"GetAllDefensiveLayers":        json.RawMessage(emptyLayersResponse),
		"GetAllLibraryDefensiveLayers": json.RawMessage(emptyLibraryLayersResponse),
	}}

	result, err := reconcileDefenseTools(context.Background(), client, "test-db", map[string]DefenseToolRef{
		ref.Key(): ref,
	})
	if err != nil {
		t.Fatalf("reconcileDefenseTools returned an error: %v", err)
	}
	if got := result[ref.Key()]; got != "target-tool-1" {
		t.Errorf("resolved id = %q, want %q", got, "target-tool-1")
	}
	if client.called("CreateDefenseToolProduct") {
		t.Error("expected the existing product to be reused via name fallback, not recreated")
	}
	if client.called("CreateDefenseTool") {
		t.Error("expected the existing tool to be reused, not duplicated, despite the product ref mismatch")
	}
}

// TestReconcileDefenseTools_NoMatch verifies that a source tool with no
// matching target tool (different product ref) creates a new product,
// creates its layer, and creates a new tool -- rather than reusing or
// mutating the unrelated existing tool.
func TestReconcileDefenseTools_NoMatch(t *testing.T) {
	ref := DefenseToolRef{
		Name:        "Falcon Sensor",
		Description: "Next-gen AV",
		Active:      true,
		Layers:      []string{"Endpoint"},
		Product: DefenseToolProductRef{
			Ref:        "crowdstrike-falcon-ngav",
			Name:       "Falcon NGAV",
			VendorName: "CrowdStrike",
		},
	}

	client := &scriptedGraphQLClient{responses: map[string]json.RawMessage{
		"GetAllDefenseTools":           json.RawMessage(existingToolsResponse),
		"GetAllDefenseToolProducts":    json.RawMessage(emptyProductsResponse),
		"GetAllDefensiveLayers":        json.RawMessage(singleEndpointLayerResponse),
		"GetAllLibraryDefensiveLayers": json.RawMessage(emptyLibraryLayersResponse),
		"FindVendor": json.RawMessage(`{
			"libraryVendors": {"nodes": [{"id": "target-vendor-1", "name": "CrowdStrike"}]}
		}`),
		"CreateLibraryDefenseLayer": json.RawMessage(`{
			"defenseLayer": {"createLibrary": {"defenseLayers": [
				{"id": "target-library-layer-placeholder", "name": "NEEDS REVIEW - NO LAYER ASSIGNED"}
			]}}
		}`),
		"CreateDefenseToolProduct": json.RawMessage(`{
			"defenseToolProduct": {"create": {"defenseToolProducts": [
				{"id": "target-product-2", "name": "Falcon NGAV", "ref": "crowdstrike-falcon-ngav"}
			]}}
		}`),
		"CreateDefenseTool": json.RawMessage(`{
			"defenseTool": {"create": {"defenseTools": [{
				"id": "target-tool-2",
				"name": "Falcon Sensor",
				"active": true,
				"description": "Next-gen AV",
				"defenseToolProduct": {"id": "target-product-2", "ref": "crowdstrike-falcon-ngav"},
				"defensiveLayers": [{"id": "target-layer-endpoint", "name": "Endpoint"}]
			}]}}
		}`),
	}}

	result, err := reconcileDefenseTools(context.Background(), client, "test-db", map[string]DefenseToolRef{
		ref.Key(): ref,
	})
	if err != nil {
		t.Fatalf("reconcileDefenseTools returned an error: %v", err)
	}
	if got := result[ref.Key()]; got != "target-tool-2" {
		t.Errorf("resolved id = %q, want %q", got, "target-tool-2")
	}
	if !client.called("CreateDefenseToolProduct") {
		t.Error("expected a new defense tool product to be created")
	}
	if !client.called("CreateDefenseTool") {
		t.Error("expected a new defense tool to be created")
	}
	if client.called("UpdateDefenseTool") {
		t.Error("expected no update to the unrelated existing tool")
	}

	var vars struct {
		Input dao.CreateDefenseToolProductInput `json:"input"`
	}
	if err := json.Unmarshal(client.variables["CreateDefenseToolProduct"], &vars); err != nil {
		t.Fatalf("could not decode CreateDefenseToolProduct variables: %v", err)
	}
	if len(vars.Input.DefenseToolProducts) != 1 {
		t.Fatalf("expected exactly one defense tool product input, got %d", len(vars.Input.DefenseToolProducts))
	}
	if got := vars.Input.DefenseToolProducts[0].VendorId; got == nil || *got != "target-vendor-1" {
		t.Errorf("CreateDefenseToolProduct vendorId = %v, want %q", got, "target-vendor-1")
	}
	if got := vars.Input.DefenseToolProducts[0].DefenseLayerIds; !slices.Equal(got, []string{"target-library-layer-placeholder"}) {
		t.Errorf("CreateDefenseToolProduct defenseLayerIds = %v, want the placeholder layer %v (ref.Product had no layers)", got, []string{"target-library-layer-placeholder"})
	}
}

// TestReconcileDefenseTools_ToolWithNoLayersGetsPlaceholder verifies that a
// new tool whose ref carries zero defense layers -- the state a prior VECTR
// migration could leave a tool in, even though VECTR's own create/update API
// rejects an empty defenseLayerIds list -- is still created, with
// PLACEHOLDER_DEFENSE_LAYER_NAME resolved (cloned from a library layer) and
// attached in place of the missing data, rather than failing the restore.
func TestReconcileDefenseTools_ToolWithNoLayersGetsPlaceholder(t *testing.T) {
	ref := DefenseToolRef{
		Name:   "Falcon Sensor",
		Active: true,
		Product: DefenseToolProductRef{
			Ref:  "crowdstrike-falcon-ngav",
			Name: "Falcon NGAV",
		},
	}

	client := &scriptedGraphQLClient{responses: map[string]json.RawMessage{
		"GetAllDefenseTools":           json.RawMessage(existingToolsResponse),
		"GetAllDefenseToolProducts":    json.RawMessage(emptyProductsResponse),
		"GetAllDefensiveLayers":        json.RawMessage(emptyLayersResponse),
		"GetAllLibraryDefensiveLayers": json.RawMessage(emptyLibraryLayersResponse),
		"CreateLibraryDefenseLayer": json.RawMessage(`{
			"defenseLayer": {"createLibrary": {"defenseLayers": [
				{"id": "target-library-layer-placeholder", "name": "NEEDS REVIEW - NO LAYER ASSIGNED"}
			]}}
		}`),
		"CloneDefenseLayer": json.RawMessage(`{
			"defenseLayer": {"clone": {"defenseLayers": [
				{"id": "target-layer-placeholder", "name": "NEEDS REVIEW - NO LAYER ASSIGNED"}
			]}}
		}`),
		"CreateDefenseToolProduct": json.RawMessage(`{
			"defenseToolProduct": {"create": {"defenseToolProducts": [
				{"id": "target-product-2", "name": "Falcon NGAV", "ref": "crowdstrike-falcon-ngav"}
			]}}
		}`),
		"CreateDefenseTool": json.RawMessage(`{
			"defenseTool": {"create": {"defenseTools": [{
				"id": "target-tool-2",
				"name": "Falcon Sensor",
				"active": true,
				"defenseToolProduct": {"id": "target-product-2", "ref": "crowdstrike-falcon-ngav"},
				"defensiveLayers": [{"id": "target-layer-placeholder", "name": "NEEDS REVIEW - NO LAYER ASSIGNED"}]
			}]}}
		}`),
	}}

	result, err := reconcileDefenseTools(context.Background(), client, "test-db", map[string]DefenseToolRef{
		ref.Key(): ref,
	})
	if err != nil {
		t.Fatalf("reconcileDefenseTools returned an error: %v", err)
	}
	if got := result[ref.Key()]; got != "target-tool-2" {
		t.Errorf("resolved id = %q, want %q", got, "target-tool-2")
	}
	if !client.called("CloneDefenseLayer") {
		t.Error("expected the placeholder defense layer to be cloned into the db")
	}

	var vars struct {
		Input dao.CreateDefenseToolInput `json:"input"`
	}
	if err := json.Unmarshal(client.variables["CreateDefenseTool"], &vars); err != nil {
		t.Fatalf("could not decode CreateDefenseTool variables: %v", err)
	}
	if len(vars.Input.CreateDefenseToolData) != 1 {
		t.Fatalf("expected exactly one defense tool input, got %d", len(vars.Input.CreateDefenseToolData))
	}
	if got := vars.Input.CreateDefenseToolData[0].DefenseLayerIds; !slices.Equal(got, []string{"target-layer-placeholder"}) {
		t.Errorf("CreateDefenseTool defenseLayerIds = %v, want the placeholder layer %v (ref had no layers)", got, []string{"target-layer-placeholder"})
	}
}

// TestReconcileDefenseTools_PlaceholderLayerAlreadyExistsIsReused verifies
// that when the placeholder db-scoped layer already exists on the target
// (e.g. a prior restore already created it for another layer-less tool), a
// second layer-less tool resolves to that same layer instead of cloning a
// duplicate.
func TestReconcileDefenseTools_PlaceholderLayerAlreadyExistsIsReused(t *testing.T) {
	ref := DefenseToolRef{
		Name:   "Falcon Sensor",
		Active: true,
		Product: DefenseToolProductRef{
			Ref:  "crowdstrike-falcon-ngav",
			Name: "Falcon NGAV",
		},
	}

	client := &scriptedGraphQLClient{responses: map[string]json.RawMessage{
		"GetAllDefenseTools":        json.RawMessage(existingToolsResponse),
		"GetAllDefenseToolProducts": json.RawMessage(emptyProductsResponse),
		"GetAllDefensiveLayers": json.RawMessage(`{"defensivelayers": {"nodes": [
			{"id": "target-layer-placeholder", "name": "NEEDS REVIEW - NO LAYER ASSIGNED"}
		]}}`),
		"GetAllLibraryDefensiveLayers": json.RawMessage(`{"libraryDefensivelayers": {"nodes": [
			{"id": "target-library-layer-placeholder", "name": "NEEDS REVIEW - NO LAYER ASSIGNED"}
		]}}`),
		"CreateDefenseToolProduct": json.RawMessage(`{
			"defenseToolProduct": {"create": {"defenseToolProducts": [
				{"id": "target-product-2", "name": "Falcon NGAV", "ref": "crowdstrike-falcon-ngav"}
			]}}
		}`),
		"CreateDefenseTool": json.RawMessage(`{
			"defenseTool": {"create": {"defenseTools": [{
				"id": "target-tool-2",
				"name": "Falcon Sensor",
				"active": true,
				"defenseToolProduct": {"id": "target-product-2", "ref": "crowdstrike-falcon-ngav"},
				"defensiveLayers": [{"id": "target-layer-placeholder", "name": "NEEDS REVIEW - NO LAYER ASSIGNED"}]
			}]}}
		}`),
	}}

	result, err := reconcileDefenseTools(context.Background(), client, "test-db", map[string]DefenseToolRef{
		ref.Key(): ref,
	})
	if err != nil {
		t.Fatalf("reconcileDefenseTools returned an error: %v", err)
	}
	if got := result[ref.Key()]; got != "target-tool-2" {
		t.Errorf("resolved id = %q, want %q", got, "target-tool-2")
	}
	if client.called("CreateLibraryDefenseLayer") || client.called("CloneDefenseLayer") {
		t.Errorf("expected the already-existing placeholder layer to be reused, not recreated, calls: %v", client.calls)
	}

	var vars struct {
		Input dao.CreateDefenseToolInput `json:"input"`
	}
	if err := json.Unmarshal(client.variables["CreateDefenseTool"], &vars); err != nil {
		t.Fatalf("could not decode CreateDefenseTool variables: %v", err)
	}
	if len(vars.Input.CreateDefenseToolData) != 1 {
		t.Fatalf("expected exactly one defense tool input, got %d", len(vars.Input.CreateDefenseToolData))
	}
	if got := vars.Input.CreateDefenseToolData[0].DefenseLayerIds; !slices.Equal(got, []string{"target-layer-placeholder"}) {
		t.Errorf("CreateDefenseTool defenseLayerIds = %v, want the existing placeholder layer %v", got, []string{"target-layer-placeholder"})
	}
}

// TestReconcileDefenseTools_NewProductWithLibraryLayers verifies that
// creating a new defense tool product resolves its library defense layers
// (creating any that don't already exist in the target instance) and passes
// their ids as defenseLayerIds on the product create -- distinct from, and
// never mixed with, the tool's own db-scoped layer ids.
func TestReconcileDefenseTools_NewProductWithLibraryLayers(t *testing.T) {
	ref := DefenseToolRef{
		Name:        "Falcon Sensor",
		Description: "Next-gen AV",
		Active:      true,
		Layers:      []string{"Endpoint"},
		Product: DefenseToolProductRef{
			Ref:        "crowdstrike-falcon-ngav",
			Name:       "Falcon NGAV",
			VendorName: "",
			Layers: []DefenseLayer{
				{Name: "Prevention", Description: "Preventative controls"},
			},
		},
	}

	client := &scriptedGraphQLClient{responses: map[string]json.RawMessage{
		"GetAllDefenseTools":           json.RawMessage(existingToolsResponse),
		"GetAllDefenseToolProducts":    json.RawMessage(emptyProductsResponse),
		"GetAllDefensiveLayers":        json.RawMessage(singleEndpointLayerResponse),
		"GetAllLibraryDefensiveLayers": json.RawMessage(emptyLibraryLayersResponse),
		"CreateLibraryDefenseLayer": json.RawMessage(`{
			"defenseLayer": {"createLibrary": {"defenseLayers": [{"id": "target-library-layer-prevention", "name": "Prevention"}]}}
		}`),
		"CreateDefenseToolProduct": json.RawMessage(`{
			"defenseToolProduct": {"create": {"defenseToolProducts": [
				{"id": "target-product-2", "name": "Falcon NGAV", "ref": "crowdstrike-falcon-ngav"}
			]}}
		}`),
		"CreateDefenseTool": json.RawMessage(`{
			"defenseTool": {"create": {"defenseTools": [{
				"id": "target-tool-2",
				"name": "Falcon Sensor",
				"active": true,
				"description": "Next-gen AV",
				"defenseToolProduct": {"id": "target-product-2", "ref": "crowdstrike-falcon-ngav"},
				"defensiveLayers": [{"id": "target-layer-endpoint", "name": "Endpoint"}]
			}]}}
		}`),
	}}

	result, err := reconcileDefenseTools(context.Background(), client, "test-db", map[string]DefenseToolRef{
		ref.Key(): ref,
	})
	if err != nil {
		t.Fatalf("reconcileDefenseTools returned an error: %v", err)
	}
	if got := result[ref.Key()]; got != "target-tool-2" {
		t.Errorf("resolved id = %q, want %q", got, "target-tool-2")
	}
	if !client.called("CreateLibraryDefenseLayer") {
		t.Error("expected the missing library defense layer to be created")
	}

	var productVars struct {
		Input dao.CreateDefenseToolProductInput `json:"input"`
	}
	if err := json.Unmarshal(client.variables["CreateDefenseToolProduct"], &productVars); err != nil {
		t.Fatalf("could not decode CreateDefenseToolProduct variables: %v", err)
	}
	if len(productVars.Input.DefenseToolProducts) != 1 {
		t.Fatalf("expected exactly one defense tool product input, got %d", len(productVars.Input.DefenseToolProducts))
	}
	if got := productVars.Input.DefenseToolProducts[0].DefenseLayerIds; !slices.Equal(got, []string{"target-library-layer-prevention"}) {
		t.Errorf("CreateDefenseToolProduct defenseLayerIds = %v, want %v", got, []string{"target-library-layer-prevention"})
	}

	var toolVars struct {
		Input dao.CreateDefenseToolInput `json:"input"`
	}
	if err := json.Unmarshal(client.variables["CreateDefenseTool"], &toolVars); err != nil {
		t.Fatalf("could not decode CreateDefenseTool variables: %v", err)
	}
	if len(toolVars.Input.CreateDefenseToolData) != 1 {
		t.Fatalf("expected exactly one defense tool input, got %d", len(toolVars.Input.CreateDefenseToolData))
	}
	if got := toolVars.Input.CreateDefenseToolData[0].DefenseLayerIds; slices.Contains(got, "target-library-layer-prevention") {
		t.Errorf("expected the tool's own defenseLayerIds to never include the product's library layer id, got %v", got)
	}
}

// TestReconcileDefenseTools_NoDuplicateToolWithinOneRun verifies that two
// source refs that collapse onto a single target tool identity produce one
// created tool, not two. They're distinct keys in toolsToReconcile (their
// product refs differ) but their product names match case-insensitively, so
// resolveOrCreateDefenseToolProduct resolves both to the same target product
// -- which makes their name+product+active identity on the target identical.
// The tool created for whichever ref is visited first has to be visible to the
// second, even though the toolsByKey snapshot predates it.
func TestReconcileDefenseTools_NoDuplicateToolWithinOneRun(t *testing.T) {
	refA := DefenseToolRef{
		Name:   "Falcon Sensor",
		Active: true,
		Layers: []string{"Endpoint"},
		Product: DefenseToolProductRef{
			Ref:  "source-ref-a",
			Name: "Falcon NGAV",
		},
	}
	refB := refA
	refB.Product.Ref = "source-ref-b"
	refB.Product.Name = "falcon ngav" // same product, differs only in case
	if refA.Key() == refB.Key() {
		t.Fatal("test setup is wrong: the two refs must be distinct keys in toolsToReconcile")
	}

	client := &scriptedGraphQLClient{responses: map[string]json.RawMessage{
		// No tools in the target yet, so the first ref visited must create one.
		"GetAllDefenseTools": json.RawMessage(`{"bluetools": {"nodes": []}}`),
		"GetAllDefenseToolProducts": json.RawMessage(`{"defenseToolProducts": {"nodes": [
			{"id": "target-product-1", "name": "Falcon NGAV", "ref": "target-side-ref"}
		]}}`),
		"GetAllDefensiveLayers":        json.RawMessage(singleEndpointLayerResponse),
		"GetAllLibraryDefensiveLayers": json.RawMessage(emptyLibraryLayersResponse),
		"CreateDefenseTool": json.RawMessage(`{
			"defenseTool": {"create": {"defenseTools": [{
				"id": "target-tool-new",
				"name": "Falcon Sensor",
				"active": true,
				"description": "",
				"defenseToolProduct": {"id": "target-product-1", "ref": "target-side-ref"},
				"defensiveLayers": [{"id": "target-layer-endpoint", "name": "Endpoint"}]
			}]}}
		}`),
	}}

	result, err := reconcileDefenseTools(context.Background(), client, "test-db", map[string]DefenseToolRef{
		refA.Key(): refA,
		refB.Key(): refB,
	})
	if err != nil {
		t.Fatalf("reconcileDefenseTools returned an error: %v", err)
	}

	creates := 0
	for _, op := range client.calls {
		if op == "CreateDefenseTool" {
			creates++
		}
	}
	if creates != 1 {
		t.Errorf("CreateDefenseTool called %d times, want 1 -- the second ref should have reused the tool created for the first", creates)
	}
	if client.called("CreateDefenseToolProduct") {
		t.Error("expected both refs to resolve to the existing product via the name fallback")
	}
	for _, ref := range []DefenseToolRef{refA, refB} {
		if got := result[ref.Key()]; got != "target-tool-new" {
			t.Errorf("resolved id for product ref %q = %q, want %q", ref.Product.Ref, got, "target-tool-new")
		}
	}
}

// TestReconcileDefenseTools_ProductMatchByNameFallback verifies that when a
// source product's ref doesn't match any existing product (VECTR derives ref
// itself on create, so it isn't stable across restores of the same source
// data), reconcileDefenseTools falls back to a case-insensitive name match
// instead of creating a duplicate product.
func TestReconcileDefenseTools_ProductMatchByNameFallback(t *testing.T) {
	ref := DefenseToolRef{
		Name:        "Falcon Sensor",
		Description: "Next-gen AV",
		Active:      true,
		Layers:      []string{"Endpoint"},
		Product: DefenseToolProductRef{
			Ref:        "new-ref-from-this-restore",
			Name:       "falcon ngav", // differs in case from the existing product's name
			VendorName: "",
		},
	}

	existingProductsWithDifferentRef := `{"defenseToolProducts": {"nodes": [
		{"id": "existing-product-99", "name": "Falcon NGAV", "ref": "old-ref-from-a-prior-restore"}
	]}}`

	client := &scriptedGraphQLClient{responses: map[string]json.RawMessage{
		"GetAllDefenseTools":           json.RawMessage(existingToolsResponse),
		"GetAllDefenseToolProducts":    json.RawMessage(existingProductsWithDifferentRef),
		"GetAllDefensiveLayers":        json.RawMessage(singleEndpointLayerResponse),
		"GetAllLibraryDefensiveLayers": json.RawMessage(emptyLibraryLayersResponse),
		"CreateDefenseTool": json.RawMessage(`{
			"defenseTool": {"create": {"defenseTools": [{
				"id": "target-tool-2",
				"name": "Falcon Sensor",
				"active": true,
				"description": "Next-gen AV",
				"defenseToolProduct": {"id": "existing-product-99", "ref": "old-ref-from-a-prior-restore"},
				"defensiveLayers": [{"id": "target-layer-endpoint", "name": "Endpoint"}]
			}]}}
		}`),
	}}

	result, err := reconcileDefenseTools(context.Background(), client, "test-db", map[string]DefenseToolRef{
		ref.Key(): ref,
	})
	if err != nil {
		t.Fatalf("reconcileDefenseTools returned an error: %v", err)
	}
	if got := result[ref.Key()]; got != "target-tool-2" {
		t.Errorf("resolved id = %q, want %q", got, "target-tool-2")
	}
	if client.called("CreateDefenseToolProduct") {
		t.Error("expected the existing product to be reused via name fallback, not recreated")
	}

	var toolVars struct {
		Input dao.CreateDefenseToolInput `json:"input"`
	}
	if err := json.Unmarshal(client.variables["CreateDefenseTool"], &toolVars); err != nil {
		t.Fatalf("could not decode CreateDefenseTool variables: %v", err)
	}
	if len(toolVars.Input.CreateDefenseToolData) != 1 {
		t.Fatalf("expected exactly one defense tool input, got %d", len(toolVars.Input.CreateDefenseToolData))
	}
	if got := toolVars.Input.CreateDefenseToolData[0].DefenseToolProductId; got != "existing-product-99" {
		t.Errorf("CreateDefenseTool defenseToolProductId = %q, want %q", got, "existing-product-99")
	}
}

// TestReconcileDefenseTools_BlankDataRejected verifies that a DefenseToolRef
// with any blank identity field (tool name, product ref, product name, or a
// layer name) is rejected with ErrIncompleteDefenseToolData before any
// GraphQL call is made -- covering both a source instance with genuinely
// incomplete data and a legacy/corrupted serialized file.
func TestReconcileDefenseTools_BlankDataRejected(t *testing.T) {
	base := DefenseToolRef{
		Name:   "Falcon Sensor",
		Active: true,
		Layers: []string{"Endpoint"},
		Product: DefenseToolProductRef{
			Ref:  "crowdstrike-falcon",
			Name: "Falcon",
		},
	}

	cases := map[string]DefenseToolRef{
		"blank tool name": func() DefenseToolRef { r := base; r.Name = ""; return r }(),
		"blank product ref": func() DefenseToolRef {
			r := base
			r.Product.Ref = ""
			return r
		}(),
		"blank product name": func() DefenseToolRef {
			r := base
			r.Product.Name = ""
			return r
		}(),
		"blank layer name": func() DefenseToolRef {
			r := base
			r.Layers = []string{"Endpoint", ""}
			return r
		}(),
		"whitespace-only tool name": func() DefenseToolRef { r := base; r.Name = "   "; return r }(),
	}

	for name, ref := range cases {
		t.Run(name, func(t *testing.T) {
			client := &scriptedGraphQLClient{responses: map[string]json.RawMessage{}}

			_, err := reconcileDefenseTools(context.Background(), client, "test-db", map[string]DefenseToolRef{
				ref.Key(): ref,
			})
			if err == nil {
				t.Fatal("expected an error for blank defense tool data, got nil")
			}
			if !errors.Is(err, ErrIncompleteDefenseToolData) {
				t.Errorf("expected err to wrap ErrIncompleteDefenseToolData, got: %v", err)
			}
			if len(client.calls) != 0 {
				t.Errorf("expected no GraphQL calls before the blank-data check, got: %v", client.calls)
			}
		})
	}
}
