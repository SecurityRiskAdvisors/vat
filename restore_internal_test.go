package vat

import (
	"context"
	"fmt"
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
		map[string]dao.GetAllDefenseToolsBluetoolsBlueToolConnectionNodesBlueTool{},
		map[string]GenericBlueTool{},
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
