package util

import (
	"encoding/csv"
	"fmt"
	"io"
)

type Filter struct {
	databaseAssessmentPairs map[string]map[string]bool
}

// NewFilter parses CSV input to create a Filter object.
//
// This function performs the following steps:
//   - Reads all records from the provided CSV reader.
//   - Initializes a map to store database-assessment pairs.
//   - Processes each record to populate the map, ensuring each database has a map of assessments.
//
// Parameters:
//   - r: An io.Reader providing CSV input data.
//
// Returns:
//   - A pointer to a `Filter` struct containing:
//     - A map of database names to assessment names, indicating which assessments should be dumped.
//   - An error if reading the CSV input fails.
//
// Errors:
//   - Returns an error if the CSV input cannot be read.
func NewFilter(r io.Reader) (*Filter, error) {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = 2
	reader.LazyQuotes = false

	// Initialize map to store database-assessment pairs
	dbAssessmentMap := make(map[string]map[string]bool)

	// Read all records from the CSV
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("could not create the filter: %w", err)
	}

	// Process each record
	for _, record := range records {
		db := record[0]
		assessment := record[1]

		// Initialize map for assessments if not already done
		if _, exists := dbAssessmentMap[db]; !exists {
			dbAssessmentMap[db] = make(map[string]bool)
		}

		// Add to map
		dbAssessmentMap[db][assessment] = true
	}

	return &Filter{
		databaseAssessmentPairs: dbAssessmentMap,
	}, nil
}

// CheckDb determines if a database should be included in the dump process.
//
// This method checks if the specified database is present in the filter's map of database-assessment pairs.
// It also considers a wildcard entry ("*") that indicates all databases should be included.
//
// Parameters:
//   - db: The name of the database to check.
//
// Returns:
//   - true if the database should be dumped.
//   - false if the database is not present in the filter and no wildcard entry exists.
//
// Logic for false cases:
//   - Returns false if the database is not explicitly listed in the filter and there is no wildcard entry ("*") indicating all databases should be included.
func (f *Filter) CheckDb(db string) bool {
	// Check for wildcard or specific database
	return f.databaseAssessmentPairs["*"] != nil || f.databaseAssessmentPairs[db] != nil
}

// CheckAssessment determines if an assessment should be included in the dump process for a given database.
//
// This method checks if the specified assessment is present in the filter's map for the given database.
// It considers wildcard entries ("*") for both databases and assessments, allowing for flexible inclusion criteria.
//
// Parameters:
//   - db: The name of the database to check.
//   - assessment: The name of the assessment to check.
//
// Returns:
//   - true if the assessment should be dumped for the given database.
//   - false if the assessment is not present in the filter for the specified database and no applicable wildcard entries exist.
//
// Logic for false cases:
//   - Returns false if the assessment is not explicitly listed for the given database and there is no wildcard entry ("*") for either the database or the assessment.
func (f *Filter) CheckAssessment(db, assessment string) bool {
	// Check for wildcard for both (why but whatever)
	if f.databaseAssessmentPairs["*"] != nil && f.databaseAssessmentPairs["*"]["*"] {
		return true
	}

	// If the db has a wildcard, check all databases for this assessment
	if f.CheckDb("*") {
		for _, filterAssessment := range f.databaseAssessmentPairs {
			if filterAssessment[assessment] {
				return true
			}
		}
		return false
	}

	if f.databaseAssessmentPairs[db] != nil && (f.databaseAssessmentPairs[db]["*"] || f.databaseAssessmentPairs[db][assessment]) {
		return true
	}
	return false
}
