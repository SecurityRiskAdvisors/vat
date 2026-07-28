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
// records every operation it's asked to serve -- enough to drive
// reconcileDefenseTools through a specific branch and assert which
// create/update mutations it did (or didn't) call.
type scriptedGraphQLClient struct {
	responses map[string]json.RawMessage
	calls     []string
}

func (s *scriptedGraphQLClient) MakeRequest(_ context.Context, req *graphql.Request, resp *graphql.Response) error {
	s.calls = append(s.calls, req.OpName)
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
const emptyLayersResponse = `{"defensivelayers": {"nodes": []}}`
const singleEndpointLayerResponse = `{"defensivelayers": {"nodes": [{"id": "target-layer-endpoint", "name": "Endpoint"}]}}`

// TestReconcileDefenseTools_CleanMatch verifies that a source tool matching
// an existing target tool on name+product ref+active, with no layers
// beyond what the target already has, is reused as-is: no
// create/update mutation is called, and the resolved id is the existing
// tool's id.
func TestReconcileDefenseTools_CleanMatch(t *testing.T) {
	client := &scriptedGraphQLClient{responses: map[string]json.RawMessage{
		"GetAllDefenseTools":        json.RawMessage(existingToolsResponse),
		"GetAllDefenseToolProducts": json.RawMessage(emptyProductsResponse),
		"GetAllDefensiveLayers":     json.RawMessage(emptyLayersResponse),
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
		"GetAllDefenseTools":        json.RawMessage(existingToolsResponse),
		"GetAllDefenseToolProducts": json.RawMessage(emptyProductsResponse),
		"GetAllDefensiveLayers":     json.RawMessage(emptyLayersResponse),
		"CreateDefenseLayer": json.RawMessage(`{
			"defenseLayer": {"create": {"defenseLayers": [{"id": "target-layer-network", "name": "Network"}]}}
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
	if !client.called("CreateDefenseLayer") {
		t.Error("expected the missing layer to be created")
	}
	if !client.called("UpdateDefenseTool") {
		t.Error("expected the existing tool to be updated with the new layer")
	}
	if client.called("CreateDefenseTool") {
		t.Error("expected no new tool to be created for a name+product+active match")
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
		"GetAllDefenseTools":        json.RawMessage(existingToolsResponse),
		"GetAllDefenseToolProducts": json.RawMessage(emptyProductsResponse),
		"GetAllDefensiveLayers":     json.RawMessage(singleEndpointLayerResponse),
		"FindVendor": json.RawMessage(`{
			"vendors": {"nodes": [{"id": "target-vendor-1", "name": "CrowdStrike"}]}
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
