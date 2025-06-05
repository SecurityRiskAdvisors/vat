package util_test

import (
	"sra/vat/internal/util"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func TestCheckDbProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {

		nonIncludedDb := "nonexistent_db"

		// Generate a random set of database names
		dbNames := rapid.SliceOf(rapid.StringMatching(`^[a-zA-Z0-9_]+$`)).Draw(t, "dbNames")

		// Create a CSV string with these database names
		var csvData strings.Builder
		for _, db := range dbNames {
			// unlikely, but check make sure
			if db == nonIncludedDb {
				continue
			}
			csvData.WriteString(`"` + db + `","*"` + "\n")
		}

		// Test without wildcard
		filterWithoutWildcard, err := util.NewFilter(strings.NewReader(csvData.String()))
		if err != nil {
			t.Fatalf("Failed to create filter without wildcard: %v", err)
		}

		// Check that each database name returns true
		for _, db := range dbNames {
			if !filterWithoutWildcard.CheckDb(db) {
				t.Errorf("Expected CheckDb to return true for database: %s", db)
			}
		}

		// Check that a non-included database returns false
		if filterWithoutWildcard.CheckDb(nonIncludedDb) {
			t.Errorf("Expected CheckDb to return false for non-included database: %s", nonIncludedDb)
		}

		// Add wildcard entry
		csvData.WriteString(`"*","*"` + "\n")

		// Test with wildcard
		filterWithWildcard, err := util.NewFilter(strings.NewReader(csvData.String()))
		if err != nil {
			t.Fatalf("Failed to create filter with wildcard: %v", err)
		}

		// Check that wildcard returns true for any database
		if !filterWithWildcard.CheckDb(nonIncludedDb) {
			t.Errorf("Expected CheckDb to return true for wildcard database: %s", nonIncludedDb)
		}
	})
}

func TestCheckAssessment(t *testing.T) {
	// Define test cases
	testCases := []struct {
		name           string
		csvData        string
		db             string
		assessment     string
		expectedResult bool
	}{
		// Positive cases
		{
			name:           "Specific database and assessment",
			csvData:        `"db1","assessment1"` + "\n" + `"db2","assessment2"` + "\n",
			db:             "db1",
			assessment:     "assessment1",
			expectedResult: true,
		},
		{
			name:           "Wildcard assessment in specific database",
			csvData:        `"db1","*"` + "\n",
			db:             "db1",
			assessment:     "any_assessment",
			expectedResult: true,
		},
		{
			name:           "Wildcard database and assessment",
			csvData:        `"*","*"` + "\n",
			db:             "any_db",
			assessment:     "any_assessment",
			expectedResult: true,
		},
		{
			name:           "Wildcard database with specific assessment",
			csvData:        `"*","assessment1"` + "\n",
			db:             "any_db",
			assessment:     "assessment1",
			expectedResult: true,
		},
		{
			name:           "Specific database with wildcard assessment",
			csvData:        `"db1","*"` + "\n",
			db:             "db1",
			assessment:     "nonexistent_assessment",
			expectedResult: true,
		},
		// Negative cases
		{
			name:           "Non-included database and assessment",
			csvData:        `"db1","assessment1"` + "\n" + `"db2","assessment2"` + "\n",
			db:             "db3",
			assessment:     "assessment3",
			expectedResult: false,
		},
		{
			name:           "Wildcard assessment in non-included database",
			csvData:        `"db1","*"` + "\n",
			db:             "db2",
			assessment:     "any_assessment",
			expectedResult: false,
		},
		{
			name:           "Specific assessment not in wildcard database",
			csvData:        `"*","assessment1"` + "\n",
			db:             "any_db",
			assessment:     "assessment2",
			expectedResult: false,
		},
		{
			name:           "Specific database with non-included assessment",
			csvData:        `"db1","assessment1"` + "\n",
			db:             "db1",
			assessment:     "nonexistent_assessment",
			expectedResult: false,
		},
		{
			name:           "Wildcard database with non-included assessment",
			csvData:        `"*","assessment1"` + "\n",
			db:             "any_db",
			assessment:     "nonexistent_assessment",
			expectedResult: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a Filter object
			filter, err := util.NewFilter(strings.NewReader(tc.csvData))
			if err != nil {
				t.Fatalf("Failed to create filter: %v", err)
			}

			// Check the assessment
			result := filter.CheckAssessment(tc.db, tc.assessment)
			if result != tc.expectedResult {
				t.Errorf("Expected %v, got %v for %s: database: %s, assessment: %s", tc.expectedResult, result, tc.name, tc.db, tc.assessment)
			}
		})
	}
}
