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
	slog.InfoContext(ctx, "Starting SaveAssessmentData",
		"db", db,
		"assessment_name", assessment_name)
	data := &AssessmentData{
		AssessmentResource: AssessmentResource{},
		ToolsMap:           map[string]DefenseToolRef{},
		IdToolsMap:         map[string]DefenseToolRef{},
		OrgMap:             make(map[string]dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentOrganizationsOrganization),
		Manifest:           NewManifestMetadata(ctx),
	}

	if data.Manifest.VectrVersion != TAGGED_VECTR_VERSION {
		slog.WarnContext(ctx, "VECTR version mismatch, this version of vat was built for another version of VECTR", "saved-data-version", data.Manifest.VectrVersion, "vat-vectr-version", TAGGED_VECTR_VERSION, "vat-version", data.Manifest.VatVersion)
	}

	assessment, err := dao.GetAllAssessments(ctx, client, db, assessment_name)
	if err != nil {
		if gqlObject, ok := gqlErrParse(err); ok {
			slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
		}
		return nil, fmt.Errorf("could not fetch assessment from instance: %w", err)
	}

	slog.DebugContext(ctx, "Fetched assessments",
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
		data.OrgMap[org.Name] = org
	}

	// check if there is a library assessment (bundle) to use
	completionProgress := map[string]bool{
		"bundle": false,
		"prefix": false,
	}
	for _, metadata := range data.Assessment.Metadata {
		if metadata.Key == "bundle" {
			data.TemplateAssessment = metadata.Value
			completionProgress["bundle"] = true
		}
		if metadata.Key == "prefix" {
			data.BundlePrefix = metadata.Value
			completionProgress["prefix"] = true
		}
		// this isn't the cleanest version, but if I have more keys I can do it that way
		if completionProgress["bundle"] && completionProgress["prefix"] {
			break
		}
	}
	// if we could find a template assessment, then let's get the ID for it as well
	if data.TemplateAssessment != "" {
		var bundle_name string = data.TemplateAssessment
		if data.BundlePrefix != "" {
			bundle_name = fmt.Sprintf("%s - %s", data.BundlePrefix, data.TemplateAssessment)
		}
		bundleIdResponse, err := dao.GetBundleByName(ctx, client, bundle_name)
		if err != nil {
			if gqlObject, ok := gqlErrParse(err); ok {
				slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
			}
			return nil, fmt.Errorf("could not connect to get the bundle id for %s. Env: %s: %w", data.TemplateAssessment, db, err)
		}
		if len(bundleIdResponse.LibraryAssessments.Nodes) > 0 {
			data.BundleID = bundleIdResponse.LibraryAssessments.Nodes[0].Id //there can only be one field due to the graphql query
		}
	}

	data.LibraryTestCases = map[string]dao.GetLibraryTestCasesLibraryTestcasesByIdsTestCaseConnectionNodesTestCase{}

	for _, c := range data.Assessment.Campaigns {
		for _, o := range c.Organizations {
			data.OrgMap[o.Name] = dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentOrganizationsOrganization(o)
		}
		for _, tc := range c.TestCases {
			if tc.LibraryTestCaseId != "" && tc.LibraryTestCaseId != "null" {
				slog.DebugContext(ctx, "Fetching library test case", "test_case_id", tc.LibraryTestCaseId)
				data.LibraryTestCases[tc.LibraryTestCaseId] = dao.GetLibraryTestCasesLibraryTestcasesByIdsTestCaseConnectionNodesTestCase{}
			} else {
				slog.WarnContext(ctx, "Test case missing a library id", "test-case-name", tc.Name)
			}
		}
	}

	ids := slices.Collect(maps.Keys(data.LibraryTestCases))
	if len(ids) > 0 {
		r, err := dao.GetLibraryTestCases(ctx, client, ids)
		if err != nil {
			if gqlObject, ok := gqlErrParse(err); ok {
				slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
			}
			return nil, fmt.Errorf("could not fetch library test cases from: %s: %w", db, err)
		}

		for _, retrived_library_cases := range r.LibraryTestcasesByIds.Nodes {
			data.LibraryTestCases[retrived_library_cases.LibraryTestCaseId] = retrived_library_cases
		}
		// Note: any id placeholdered above (line ~156) that GetLibraryTestCases
		// doesn't return a node for stays as that zero-value placeholder in
		// data.LibraryTestCases -- an empty/ghost entry would silently persist
		// into the save file. Not currently reachable (these ids come from the
		// same live assessment being saved), but if that ever changes, this is
		// the place to reconcile ids against len(r.LibraryTestcasesByIds.Nodes)
		// and warn/error on any that didn't come back.
	}

	slog.DebugContext(ctx, "Fetching defense tools",
		"db", db)
	btr, err := dao.GetAllDefenseTools(ctx, client, db)
	if err != nil {
		if gqlObject, ok := gqlErrParse(err); ok {
			slog.ErrorContext(ctx, "detailed error", "error", gqlObject)
		}
		return nil, fmt.Errorf("could not connect to fetch blue tools for %s: %w", db, err)
	}

	// Index once by id so both loops below always build a DefenseToolRef from
	// the full GetAllDefenseTools node -- tc.BlueTools' own selection lacks
	// description/active/product ref, so resolving through this index (rather
	// than tc.BlueTools' fields directly) keeps every ref fully populated
	// regardless of which loop finds it first.
	bluetoolsById := make(map[string]dao.GetAllDefenseToolsBluetoolsBlueToolConnectionNodesBlueTool, len(btr.Bluetools.Nodes))
	for _, bt := range btr.Bluetools.Nodes {
		bluetoolsById[bt.Id] = bt
	}

	for _, c := range data.Assessment.Campaigns {
		for _, tc := range c.TestCases {
			for _, bt := range tc.BlueTools {
				if _, ok := data.IdToolsMap[bt.Id]; ok {
					continue
				}
				full, ok := bluetoolsById[bt.Id]
				if !ok {
					slog.WarnContext(ctx, "test case references a blue tool not present in GetAllDefenseTools, skipping", "tool-id", bt.Id, "tool-name", bt.Name)
					continue
				}
				ref := toDefenseToolRef(full)
				data.ToolsMap[ref.Key()] = ref
				data.IdToolsMap[bt.Id] = ref
			}
			for _, outcomes := range tc.DefenseToolOutcomes {
				toolId := strconv.Itoa(outcomes.DefenseToolId)
				if _, ok := data.IdToolsMap[toolId]; ok {
					continue
				}
				full, ok := bluetoolsById[toolId]
				if !ok {
					slog.WarnContext(ctx, "defense tool outcome references a tool not present in GetAllDefenseTools, skipping", "tool-id", toolId)
					continue
				}
				ref := toDefenseToolRef(full)
				data.ToolsMap[ref.Key()] = ref
				data.IdToolsMap[toolId] = ref
			}
		}
	}

	slog.InfoContext(ctx, "Finished dumping assessment", "date", data.Manifest.Created, "vat-version", data.Manifest.VatVersion, "assessment-name", data.Assessment.Name, "db", db)

	return data, nil

}

// toDefenseToolRef projects a full GetAllDefenseTools BlueTool node down to
// the durable, cross-instance identity restore needs (see DefenseToolRef's
// doc comment).
func toDefenseToolRef(bt dao.GetAllDefenseToolsBluetoolsBlueToolConnectionNodesBlueTool) DefenseToolRef {
	layers := make([]string, 0, len(bt.DefensiveLayers))
	for _, l := range bt.DefensiveLayers {
		layers = append(layers, l.Name)
	}
	return DefenseToolRef{
		Name:        bt.Name,
		Description: bt.Description,
		Active:      bt.Active,
		Layers:      layers,
		Product: DefenseToolProductRef{
			Ref:        bt.DefenseToolProduct.Ref,
			Name:       bt.DefenseToolProduct.Name,
			VendorName: bt.DefenseToolProduct.Vendor.Name,
		},
	}
}
