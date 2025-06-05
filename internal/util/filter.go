package util

import (
	"encoding/csv"
	"io"
)

type Filter struct {
	databaseAssessmentPairs map[string]map[string]bool
}

// NewFilter parses the CSV input and returns a Filter object.
func NewFilter(r io.Reader) (*Filter, error) {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true

	// Initialize map to store database-assessment pairs
	dbAssessmentMap := make(map[string]map[string]bool)

	// Read all records from the CSV
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	// Process each record
	for _, record := range records {
		if len(record) < 2 {
			continue // Skip invalid records
		}
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

// CheckDb returns true if the database should be dumped.
func (f *Filter) CheckDb(db string) bool {
	// Check for wildcard or specific database
	return f.databaseAssessmentPairs["*"] != nil || f.databaseAssessmentPairs[db] != nil
}

// CheckAssessment returns true if the assessment should be dumped for the given database.
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
