package vat

import (
	"fmt"
	"testing"

	"sra/vat/internal/dao"

	"pgregory.net/rapid"
)

// TestGroupedCreateTestCaseWithLibraryIdInput_Batching is a property test:
// for any assignment of source test cases to library test case IDs (with
// repeats allowed, to simulate the same library test case used multiple
// times in a campaign), GenerateInsertsData must split entries into exactly
// maxGroupSize batches, never put the same libraryTestCaseId in a batch
// twice, keep each batch's entries/input positionally aligned, and account
// for every added entry exactly once.
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
				g.Add(clientId, dao.CreateTestCaseDataWithLibraryIdInput{LibraryTestCaseId: libId})
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
			if len(batch.entries) != len(batch.input.CreateTestCaseInputs) {
				t.Fatalf("batch %d: entries (%d) and CreateTestCaseInputs (%d) length mismatch", bi, len(batch.entries), len(batch.input.CreateTestCaseInputs))
			}
			seenLibIdsInBatch := make(map[string]bool)
			for i, e := range batch.entries {
				if e.libraryTestCaseId != batch.input.CreateTestCaseInputs[i].LibraryTestCaseId {
					t.Fatalf("batch %d entry %d: libraryTestCaseId %q does not match its own input %q", bi, i, e.libraryTestCaseId, batch.input.CreateTestCaseInputs[i].LibraryTestCaseId)
				}
				if seenLibIdsInBatch[e.libraryTestCaseId] {
					t.Fatalf("batch %d contains duplicate libraryTestCaseId %q", bi, e.libraryTestCaseId)
				}
				seenLibIdsInBatch[e.libraryTestCaseId] = true

				if seenClientIds[e.clientId] {
					t.Fatalf("clientId %q appeared in more than one batch", e.clientId)
				}
				seenClientIds[e.clientId] = true
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

// TestGroupedCreateTestCaseWithLibraryIdInput_ResolveByLibraryIdIsOrderIndependent
// is a property test verifying that resolving a batch's mutation response by
// libraryTestCaseId (as restoreCampaigns does) correctly maps each entry's
// clientId to its newId regardless of what order the response items come
// back in, since the response array's order relative to the request isn't
// guaranteed.
func TestGroupedCreateTestCaseWithLibraryIdInput_ResolveByLibraryIdIsOrderIndependent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 8).Draw(t, "n")

		g := NewGroupedCreateTestCaseWithLibraryIdInput("test-db", "campaign-1")

		type resultItem struct {
			libId string
			newId string
		}
		clientIdByLib := make(map[string]string, n)
		results := make([]resultItem, n)
		for i := 0; i < n; i++ {
			clientId := fmt.Sprintf("src-%d", i)
			libId := fmt.Sprintf("lib-%d", i)
			results[i] = resultItem{libId: libId, newId: fmt.Sprintf("new-%d", i)}
			clientIdByLib[libId] = clientId
			g.Add(clientId, dao.CreateTestCaseDataWithLibraryIdInput{LibraryTestCaseId: libId})
		}

		batches := g.GenerateInsertsData()
		if len(batches) != 1 {
			t.Fatalf("expected exactly 1 batch (all distinct library IDs), got %d", len(batches))
		}
		batch := batches[0]

		// Shuffle the mock response into an arbitrary order relative to the request.
		shuffled := rapid.Permutation(results).Draw(t, "shuffled")

		byLibraryId := make(map[string]*libraryIdInsert, len(batch.entries))
		for _, e := range batch.entries {
			byLibraryId[e.libraryTestCaseId] = e
		}
		for _, r := range shuffled {
			if e, ok := byLibraryId[r.libId]; ok {
				e.newId = r.newId
			}
		}

		testCaseIdMap := make(map[string]string)
		for _, entries := range g.TestCases {
			for _, e := range entries {
				testCaseIdMap[e.clientId] = e.newId
			}
		}

		for _, r := range results {
			clientId := clientIdByLib[r.libId]
			if got := testCaseIdMap[clientId]; got != r.newId {
				t.Errorf("testCaseIdMap[%q] = %q, want %q", clientId, got, r.newId)
			}
		}
	})
}
