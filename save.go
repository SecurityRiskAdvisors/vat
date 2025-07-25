package vat

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sra/vat/internal/dao"
	"strconv"

	"github.com/Khan/genqlient/graphql"
)

var ErrNoAssessmentsFound = fmt.Errorf("no assessments found")
var ErrTooManyAssessmentsFound = fmt.Errorf("more than one assessment matched")

// SaveAssessmentData fetches and processes assessment data from a database.
//
// This function performs the following steps:
//   - Fetches the assessment matching the given name from the specified database.
//   - Validates the number of assessments found, returning an error if none or more than one are found.
//   - Extracts library test cases and defense tools associated with the assessment.
//   - Checks for a template assessment name in the metadata.
//
// Parameters:
//   - ctx: Context for managing request deadlines, cancellations, and other request-scoped values.
//   - client: GraphQL client used to make API calls.
//   - db: Name of the database to query.
//   - assessment_name: Name of the assessment to search for.
//
// Returns:
//   - A pointer to an `AssessmentData` struct containing the matched assessment and associated data.
//   - An error if any step in the process fails.
//
// Errors:
//   - Returns `ErrNoAssessmentsFound` if no assessments are found.
//   - Returns `ErrTooManyAssessmentsFound` if more than one assessment matches the given name.
//   - Returns a wrapped error with additional context if any GraphQL query fails.
func SaveAssessmentData(ctx context.Context, client graphql.Client, db string, assessment_name string) (*AssessmentData, error) {
	slog.Info("Starting SaveAssessmentData",
		"db", db,
		"assessment_name", assessment_name)
	data := &AssessmentData{
		ToolsMap:   map[string]GenericBlueTool{},
		IdToolsMap: map[string]GenericBlueTool{},
		OptionalFields: struct {
			OrgMap map[string]dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentOrganizationsOrganization
		}{
			OrgMap: make(map[string]dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentOrganizationsOrganization),
		},
		Metadata: &VatMetadata{
			SaveData: NewVatOpMetadata(ctx),
		},
	}

	if data.Metadata.SaveData.VectrVersion != TAGGED_VECTR_VERSION {
		slog.Warn("VECTR version mismatch, this version of vat was built for another version of VECTR", "saved-data-version", data.Metadata.SaveData.VectrVersion, "vat-vectr-version", TAGGED_VECTR_VERSION, "vat-version", data.Metadata.SaveData.Version)
	}

	assessment, err := dao.GetAllAssessments(ctx, client, db, assessment_name)
	if err != nil {
		if gqlObject, ok := gqlErrParse(err); ok {
			slog.Error("detailed error", "error", gqlObject)
		}
		return nil, fmt.Errorf("could not fetch assessment from instance: %w", err)
	}

	slog.Debug("Fetched assessments",
		"count", len(assessment.Assessments.Nodes),
		"db", db)
	if len(assessment.Assessments.Nodes) == 0 {
		return nil, ErrNoAssessmentsFound
	}
	if len(assessment.Assessments.Nodes) > 1 {
		return nil, fmt.Errorf("error searching %s, %w", assessment_name, err)
	}

	return saveAssessment(ctx, client, assessment.Assessments.Nodes[0], data, db)
}

// saveAssessment processes the assessment data and fetches associated library test cases and defense tools.
//
// This function performs the following steps:
//   - Processes the assessment object to populate the `AssessmentData` struct.
//   - Extracts library test cases using their IDs and fetches them via the `GetLibraryTestCases` function.
//   - Fetches all defense tools for the given database using the `GetAllDefenseTools` function.
//   - Populates the `ToolsMap` and `IdToolsMap` with defense tool information.
//
// Parameters:
//   - ctx: The context for managing request deadlines, cancellations, and other request-scoped values.
//   - client: The GraphQL client used to make API calls.
//   - assessment: The assessment object containing campaigns and test cases.
//   - data: The `AssessmentData` struct to be populated.
//   - db: The name of the database to query.
//
// Returns:
//   - A pointer to an `AssessmentData` struct containing:
//   - The processed assessment.
//   - A collection of library test cases associated with the assessment.
//   - A collection of defense tools.
//   - The template assessment name (if available in the metadata).
//   - An error if any step in the process fails.
//
// Errors:
//   - Returns a wrapped error with additional context if any GraphQL query fails.
func saveAssessment(ctx context.Context, client graphql.Client, assessment dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessment, data *AssessmentData, db string) (*AssessmentData, error) {

	data.Assessment = assessment

	for _, org := range data.Assessment.Organizations {
		data.OptionalFields.OrgMap[org.Name] = org
	}

	// check if there is a library assessment (bundle) to use
	for _, metadata := range data.Assessment.Metadata {
		if metadata.Key == "bundle" {
			data.TemplateAssessment = metadata.Value
			break
		}
	}

	data.LibraryTestCases = map[string]dao.GetLibraryTestCasesLibraryTestcasesByIdsTestCaseConnectionNodesTestCase{}

	for _, c := range data.Assessment.Campaigns {
		for _, o := range c.Organizations {
			data.OptionalFields.OrgMap[o.Name] = dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentOrganizationsOrganization(o)
		}
		for _, tc := range c.TestCases {
			if tc.LibraryTestCaseId != "" && tc.LibraryTestCaseId != "null" {
				slog.Debug("Fetching library test case", "test_case_id", tc.LibraryTestCaseId)
				data.LibraryTestCases[tc.LibraryTestCaseId] = dao.GetLibraryTestCasesLibraryTestcasesByIdsTestCaseConnectionNodesTestCase{}
			} else {
				slog.Warn("Test case missing a library id", "test-case-name", tc.Name)
			}
		}
	}

	ids := slices.Collect(maps.Keys(data.LibraryTestCases))
	if len(ids) > 0 {
		r, err := dao.GetLibraryTestCases(ctx, client, ids)
		if err != nil {
			if gqlObject, ok := gqlErrParse(err); ok {
				slog.Error("detailed error", "error", gqlObject)
			}
			return nil, fmt.Errorf("could not fetch library test cases from: %s: %w", db, err)
		}

		for _, retrived_library_cases := range r.LibraryTestcasesByIds.Nodes {
			data.LibraryTestCases[retrived_library_cases.LibraryTestCaseId] = retrived_library_cases
		}
	}

	slog.Info("Fetching defense tools",
		"db", db)
	btr, err := dao.GetAllDefenseTools(ctx, client, db)
	if err != nil {
		if gqlObject, ok := gqlErrParse(err); ok {
			slog.Error("detailed error", "error", gqlObject)
		}
		return nil, fmt.Errorf("could not connect to fetch blue tools for %s: %w", db, err)
	}

	for _, c := range data.Assessment.Campaigns {
		for _, tc := range c.TestCases {
			for _, bt := range tc.BlueTools {
				if _, ok := data.ToolsMap[bt.Name]; !ok {
					gbt := GenericBlueTool{
						Id:          bt.Id,
						Name:        bt.Name,
						ProductName: bt.DefenseToolProduct.Name,
					}
					data.ToolsMap[bt.Name] = gbt
					data.IdToolsMap[bt.Id] = gbt
				}
			}
			for _, outcomes := range tc.DefenseToolOutcomes {
				for _, bt := range btr.Bluetools.Nodes {
					if strconv.Itoa(outcomes.DefenseToolId) == bt.Id {
						if _, ok := data.ToolsMap[bt.Name]; !ok {
							gbt := GenericBlueTool{
								Id:          bt.Id,
								Name:        bt.Name,
								ProductName: bt.DefenseToolProduct.Name,
							}
							data.ToolsMap[bt.Name] = gbt
							data.IdToolsMap[bt.Id] = gbt
							break
						}

					}
				}
			}

		}
	}

	// get a unique list of the orgs
	data.Organizations = slices.Collect(maps.Keys(data.OptionalFields.OrgMap))
	slog.Info("Writing vat header", "date", data.Metadata.SaveData.Date, "vat-version", data.Metadata.SaveData.Version)

	return data, nil

}
