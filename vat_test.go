package vat_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sra/vat"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Khan/genqlient/graphql"
	"github.com/google/uuid"
	"pgregory.net/rapid"
)

// MockGraphQLClient is a mock implementation of the graphql.Client interface.
type MockGraphQLClient struct {
	// MockResponses maps operation names to their corresponding mock responses.
	MockResponses map[string]interface{}

	// MockErrors maps operation names to their corresponding errors.
	MockErrors map[string]error
}

// MakeRequest is the mock implementation of the graphql.Client's MakeRequest method.
func (m *MockGraphQLClient) MakeRequest(
	ctx context.Context,
	req *graphql.Request,
	resp *graphql.Response,
) error {
	// Extract the operation name from the request.
	operationName := req.OpName

	// Check if a mock error is defined for the operation.
	if err, ok := m.MockErrors[operationName]; ok {
		return err
	}

	// Check if a mock response is defined for the operation.
	if response, ok := m.MockResponses[operationName]; ok {
		// Populate the provided response object with the mock response.
		// Use type assertion to ensure the response matches the expected type.
		resp.Data = response
		return nil
	}

	// If no response or error is defined, return a default error.
	return errors.New("no mock response or error defined for operation: " + operationName)
}

func TestRestoreAssessment_MissingOrganizations(t *testing.T) {
	// Mock input data
	data := &vat.AssessmentData{
		Organizations: []string{"MissingOrg"},
	}

	// Mock GraphQL client
	// Return an empty organization list
	mockClient := &MockGraphQLClient{
		MockResponses: map[string]interface{}{
			"FindOrganization": &vat.FindOrganizationResponse{
				Organizations: vat.FindOrganizationOrganizationsOrganizationConnection{
					Nodes: []vat.FindOrganizationOrganizationsOrganizationConnectionNodesOrganization{},
				},
			},
		},
	}

	// Call RestoreAssessment
	err := vat.RestoreAssessment(context.Background(), mockClient, "test-db", data, &vat.RestoreOptionalParams{})
	if err == nil || !errors.Is(err, vat.ErrOrgNotFound) {
		t.Errorf("Expected ErrOrgNotFound, got %v", err)
	}
}

func TestRoundtripAssessmentData(t *testing.T) {
	it := os.Getenv("IT")
	if it != "TRUE" {
		t.Skip("no integration test")
	}
	slog.SetLogLoggerLevel(slog.LevelDebug)
	ctx := context.TODO()

	src_hostname := os.Getenv("SOURCE_HOSTNAME")
	dst_hostname := os.Getenv("DEST_HOSTNAME")
	src_creds := os.Getenv("SOURCE_CREDS")
	dst_creds := os.Getenv("DEST_CREDS")

	src_db := os.Getenv("SOURCE_DB")
	src_assessment := os.Getenv("SOURCE_ASSESSMENT")
	dst_db := os.Getenv("DEST_DB")

	s, _ := vat.SetupVectrClient(src_hostname, src_creds, true)
	d, _ := vat.SetupVectrClient(dst_hostname, dst_creds, true)

	o, err := vat.SaveAssessmentData(ctx, s, src_db, src_assessment)
	if err != nil {
		t.Fatalf("got an error back: %s", err)
	}

	sd, err := vat.EncodeToJson(o)
	if err != nil {
		t.Fatalf("could not encode to json: %s", err)
	}

	td, err := vat.DecodeJson(sd)
	if err != nil {
		t.Fatalf("could not decode json: %s", err)
	}

	td.Assessment.Name = fmt.Sprintf("%s ", time.Now().String())

	err = vat.RestoreAssessment(ctx, d, dst_db, td, &vat.RestoreOptionalParams{
		OverrideAssessmentTemplate: false,
		AssessmentName:             td.Assessment.Name,
	})
	if err != nil {
		t.Fatalf("got an error: %s", err)
	}
}

func TestParseLibraryTestcasesByIdsError(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random number of UUIDs
		numUUIDs := rapid.IntRange(0, 10).Draw(t, "numUUIDs")

		// Generate random UUIDs
		uuids := make([]string, numUUIDs)
		for i := 0; i < numUUIDs; i++ {
			uuidgen, err := uuid.FromBytes(rapid.SliceOfN(rapid.Byte(), 16, 16).Draw(t, "uuid"+strconv.Itoa(i)))
			if err != nil {
				t.Fatalf("could not generate uuid: %s", err)
			}
			uuids[i] = uuidgen.String()
		}

		// Create the input string
		input := "The following IDs were not valid: " + strings.Join(uuids, ", ")

		// Call the function
		result, err := vat.ParseLibraryTestcasesByIdsError(input)

		// Validate the result
		if numUUIDs == 0 {
			if err == nil {
				t.Errorf("Expected an error when no UUIDs are present, got nil")
			}
		} else {
			if err != nil {
				t.Errorf("Expected no error when UUIDs are present, got %v", err)
			}
			if len(result) != numUUIDs {
				t.Errorf("Expected %d UUIDs, got %d", numUUIDs, len(result))
			}
			for i, uuid := range uuids {
				if result[i] != uuid {
					t.Errorf("Expected UUID %s at position %d, got %s", uuid, i, result[i])
				}
			}
		}
	})
}

func TestNewVatMetadata(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		version_string := rapid.String().Draw(t, "version")

		ctx := context.WithValue(context.Background(), vat.VERSION, vat.VatContextValue(version_string))

		md := vat.NewVatOpMetadata(ctx)

		if md.Version != version_string {
			t.Errorf("version string did not round trip, want: %s, got: %s", version_string, md.Version)
		}
	})

}
