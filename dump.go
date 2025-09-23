package vat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sra/vat/internal/dao"
	"sra/vat/internal/util"

	"github.com/Khan/genqlient/graphql"
)

// An object to store data for the results per assessment
type AssessmentDataEntry struct {
	Db             string
	AssessmentName string
	Ad             *AssessmentData
	Err            error
}

var ErrDumpInstanceFailure = errors.New("error in dump an instance")
var ErrDumpAssessmentFailure = errors.New("error in dumping an assessment")

// DumpInstance retrieves and processes assessment data from a VECTR instance.
//
// This function performs the following steps:
//   - Fetches all databases from the VECTR instance.
//   - Iterates over each database to check if it should be dumped based on the provided filter.
//   - Fetches assessments for each eligible database.
//   - Validates assessments against the filter criteria.
//   - Processes each assessment to populate the `AssessmentDataEntry` struct.
//
// Parameters:
//   - ctx: Context for managing request deadlines, cancellations, and other request-scoped values.
//   - client: GraphQL client used to make API calls.
//   - filter: Filter object to determine which databases and assessments should be dumped.
//
// Returns:
//   - A slice of `AssessmentDataEntry` structs containing:
//   - Database name.
//   - Assessment name.
//   - Serialized assessment data.
//   - Any error encountered during processing.
//   - An error if any step in the process fails.
//
// Errors:
//   - Returns `ErrDumpInstanceFailure` if fetching databases fails.
//   - Returns `ErrDumpAssessmentFailure` if processing any assessment fails.
//   - Returns a wrapped error with additional context if any GraphQL query fails.
func DumpInstance(ctx context.Context, client graphql.Client, filter *util.Filter) ([]AssessmentDataEntry, error) {

	dbs, err := dao.GetAllDatabases(ctx, client)
	if err != nil {
		if gqlObject, ok := gqlErrParse(err); ok {
			slog.Error("detailed error", "error", gqlObject)
		}
		return nil, fmt.Errorf("could not get databases for instance: %w: %w", err, ErrDumpInstanceFailure)
	}
	// now process each assessment
	dumpedAssessments := make([]AssessmentDataEntry, 0, len(dbs.Databases))
	var overallError error
	for _, db := range dbs.Databases {
		// Check if the database should be dumped
		if filter.CheckDb(db.Name) {
			assessments, err := dao.GetBatchAssessmentsForDb(ctx, client, db.Name)
			if err != nil {
				if gqlObject, ok := gqlErrParse(err); ok {
					slog.Error("detailed error", "error", gqlObject)
				}
				return dumpedAssessments, fmt.Errorf("could not dump assessments for db: %s; %w: %w", db.Name, err, ErrDumpInstanceFailure)
			}
			for _, assessment := range assessments.Assessments.Nodes {
				// Check if the assessment should be dumped
				if filter.CheckAssessment(db.Name, assessment.Name) {
					ae := AssessmentDataEntry{
						Db:             db.Name,
						AssessmentName: assessment.Name,
					}
					data := &AssessmentData{
						ToolsMap:   map[string]GenericBlueTool{},
						IdToolsMap: map[string]GenericBlueTool{},
						OptionalFields: struct {
							OrgMap       map[string]dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentOrganizationsOrganization
							BundleID     string
							BundlePrefix string
						}{
							OrgMap: make(map[string]dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentOrganizationsOrganization),
						},
						Metadata: &VatMetadata{
							SaveData: NewVatOpMetadata(ctx),
						},
					}
					ad, err := saveAssessment(ctx, client, assessment, data, db.Name)
					if err != nil {
						if gqlObject, ok := gqlErrParse(err); ok {
							slog.Error("Could not dump assessment", "error", gqlObject, "db", db.Name, "assessment", assessment.Name)
						}
						ae.Err = fmt.Errorf("could not dump assessment, db: %s, assessment-name: %s, %w", db.Name, assessment.Name, err)
						overallError = ErrDumpAssessmentFailure
						dumpedAssessments = append(dumpedAssessments, ae)
						// don't return here, just keep processing the data
						continue
					}
					ae.Ad = ad
					dumpedAssessments = append(dumpedAssessments, ae)
				}
			}
		}
	}
	return dumpedAssessments, overallError
}
