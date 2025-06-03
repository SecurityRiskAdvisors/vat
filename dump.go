package vat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sra/vat/internal/dao"

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

// todo: I need to pass in a filter object to filter for databases and assessments
// DumpInstance retrieves all assessments from a VECTR instance and returns them as a slice of AssessmentDataEntry.
// It iterates over all databases in the instance, fetching assessments for each database and storing the results.
// The function handles errors gracefully, logging detailed error information when GraphQL errors occur.
//
// Parameters:
// - ctx: A context.Context object used for managing request deadlines, cancellation signals, and other request-scoped values.
// - client: A graphql.Client object used to interact with the VECTR instance's GraphQL API.
//
// Return Values:
// - A slice of AssessmentDataEntry, where each entry contains the database name, assessment name, assessment data, and any error encountered during processing.
// - An error value indicating if there was a failure in dumping the instance or any assessments. If no errors occur, this will be nil.
//
// Errors:
// - ErrDumpInstanceFailure: Returned if there is an error in retrieving databases from the instance.
// - ErrDumpAssessmentFailure: Returned if there is an error in dumping an assessment from a database.
// - GraphQL errors: If a GraphQL error occurs, detailed error information is logged using slog, and the function returns the error wrapped with context.
func DumpInstance(ctx context.Context, client graphql.Client) ([]AssessmentDataEntry, error) {

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
		assessments, err := dao.GetBatchAssessmentsForDb(ctx, client, db.Name)
		if err != nil {
			if gqlObject, ok := gqlErrParse(err); ok {
				slog.Error("detailed error", "error", gqlObject)
			}
			return dumpedAssessments, fmt.Errorf("could not dump assessments for db: %s; %w: %w", db.Name, err, ErrDumpInstanceFailure)
		}
		for _, assessment := range assessments.Assessments.Nodes {
			ae := AssessmentDataEntry{
				Db:             db.Name,
				AssessmentName: assessment.Name,
			}
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
	return dumpedAssessments, overallError
}
