package vat

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/Khan/genqlient/graphql"
	"github.com/google/uuid"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

type RestoreOptionalParams struct {
	AssessmentName             string // Set desired assessment name to this one, if blank, use existing assessment name
	OverrideAssessmentTemplate bool   // Flag to override using the use of the existing template assessment. Directly import the tests instead (lower fidelty)
}

var ErrOrgNotFound = fmt.Errorf("could not find org(s)")
var ErrMissingTools = fmt.Errorf("could not find tools")
var ErrMissingLibraryAssessment = fmt.Errorf("missing library assessment")
var ErrInvalidAssessmentName = fmt.Errorf("assessment name override is invalid (blank?)")
var ErrAssessmentAlreadyExists = fmt.Errorf("assessment already exists")

// executorMap maps automation executor types (e.g., "powershell") to their corresponding internal representation.
// The read part of the API does not return an ENUM or fixed type, just a generic string. This maps it back
// to the object type
var executorMap map[string]AttackAutomationExecutor = map[string]AttackAutomationExecutor{
	"powershell":        AttackAutomationExecutorPowershell,
	"inline_powershell": AttackAutomationExecutorInlinePowershell,
	"command_prompt":    AttackAutomationExecutorCmd,
	"sh":                AttackAutomationExecutorSh,
	"bash":              AttackAutomationExecutorBash,
	"":                  AttackAutomationExecutorCmd,
}

// outcomeStatusMap maps test case outcome statuses (e.g., "Abandoned") to their corresponding internal representation.
// The read part of the API returns different values than the write part accepts
// This maps the two together
// Note -- it will always require a validation check before use
var outcomeStatusMap map[string]TestCaseStatus = map[string]TestCaseStatus{
	string(TestCaseStatusAbandon):      TestCaseStatusAbandon,
	"Abandoned":                        TestCaseStatusAbandon,
	string(TestCaseStatusNotperformed): TestCaseStatusNotperformed,
	string(TestCaseStatusCompleted):    TestCaseStatusCompleted,
	string(TestCaseStatusInprogress):   TestCaseStatusInprogress,
	string(TestCaseStatusPaused):       TestCaseStatusPaused,
	"Not Performed":                    TestCaseStatusNotperformed,
}

// RestoreAssessment restores an assessment to a VECTR instance by deserializing
// and importing serialized assessment data. It ensures that all required
// organizations, tools, and templates exist in the target instance before
// creating the assessment, campaigns, and test cases.
//
// # Parameters
//
//   - ctx: The context for managing request lifetimes and cancellations.
//   - client: The GraphQL client for interacting with the VECTR instance.
//   - db: The database name in the VECTR instance.
//   - ad: The serialized assessment data to restore, including organizations,
//     tools, campaigns, and test cases.
//   - optionalParams: Optional parameters to customize the restore process,
//     such as overriding the assessment name or skipping template validation.
//
// # Returns
//
//   - error: Returns an error if any step of the restore process fails. The error
//     message provides details about the failure.
//
// # Workflow
//
// 1. **Validate Organizations**:
//   - Checks if all organizations in the serialized data exist in the target
//     VECTR instance.
//   - If any organization is missing, the function returns an error listing
//     the missing organizations.
//
// 2. **Validate Tools**:
//   - Verifies that all tools in the serialized data exist in the target
//     instance.
//   - If any tools are missing, the function returns an error listing the
//     missing tools along with their names and product information.
//
// 3. **Handle Template Assessment**:
//   - If a template assessment is specified in the serialized data:
//   - Checks if the template exists in the target instance.
//   - Returns an error if the template is missing.
//   - If no template is specified or the override flag is set:
//   - Creates template test cases in the target instance using the
//     serialized data.
//
// 4. **Override Assessment Name**:
//   - If `optionalParams.AssessmentName` is provided, it overrides the name
//     of the assessment in the serialized data.
//
// 5. **Create Assessment**:
//   - Creates the assessment in the target instance using the serialized data.
//   - Includes metadata and organization mappings.
//
// 6. **Create Campaigns**:
//   - Creates campaigns associated with the assessment.
//   - Maps campaign metadata and organizations.
//
// 7. **Create Test Cases**:
//   - Creates test cases for each campaign.
//   - Maps test case metadata, tags, targets, sources, defenses, and
//     automation details.
//   - Validates and maps test case outcomes using the `outcomeStatusMap`.
//   - Handles defense tool outcomes by mapping serialized tool IDs to the
//     target instance's tool IDs.
//
// # Error Handling
//
// The function returns detailed errors for the following scenarios:
//   - Missing organizations (`ErrOrgNotFound`).
//   - Missing tools (`ErrMissingTools`).
//   - Missing library assessments (`ErrMissingLibraryAssessment`).
//   - A local asessment already exists (`ErrAssessmentAlreadyExists`).
//   - Invalid or blank assessment name overrides (`ErrInvalidAssessmentName`).
//   - GraphQL API errors during organization, tool, template, assessment,
//     campaign, or test case creation.
//
// # Example Usage
//
//	ctx := context.Background()
//	insecure_connection := false
//	client := vat.SetupVectrClient("vectr.example.org", "<accesskeyid>:<secret>", insecure_connection)
//
//	db := "example_db"
//	serializedData := &AssessmentData{
//	    Organizations: []string{"Org1", "Org2"},
//	    ToolsMap: map[string]GenericBlueTool{
//	        "Tool1": {Name: "Tool1", ProductName: "Product1"},
//	    },
//	    TemplateAssessment: "Template1",
//	    Assessment: GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessment{
//	        Name: "Example Assessment",
//	        Description: "An example assessment",
//	    },
//	}
//
//	err := RestoreAssessment(ctx, client, db, serializedData, &RestoreOptionalParams{
//	    AssessmentName: "Restored Assessment Name",
//	})
//
//	if err != nil {
//	    log.Fatalf("Failed to restore assessment: %v", err)
//	}
func RestoreAssessment(ctx context.Context, client graphql.Client, db string, ad *AssessmentData, optionalParams *RestoreOptionalParams) error {

	// Step 1: Check if the organizations are in the new instance, error if not

	slog.Info("Starting RestoreAssessment",
		"db", db,
		"assessment_name", ad.Assessment.Name,
		"organization_count", len(ad.Organizations),
		"tool_count", len(ad.ToolsMap))
	missing_orgs := []string{}
	org_map := make(map[string]FindOrganizationOrganizationsOrganizationConnectionNodesOrganization)
	for _, o := range ad.Organizations {
		r, err := FindOrganization(ctx, client, o)
		if err != nil {
			if ad.OptionalFields.OrgMap != nil {
				om := ad.OptionalFields.OrgMap[o]
				return fmt.Errorf("could not fetch organization: %s, %s, %s, %s: %w", om.Name, om.Abbreviation, om.Description, om.Url, err)

			} else {
				return fmt.Errorf("could not fetch organization: %s: %w", o, err)
			}
		}
		if len(r.Organizations.Nodes) == 0 {
			missing_orgs = append(missing_orgs, o)
			continue
		}
		org_map[r.Organizations.Nodes[0].Name] = r.Organizations.Nodes[0]
	}
	slog.Debug("Validating organizations",
		"total", len(ad.Organizations),
		"missing_orgs", missing_orgs)
	if len(missing_orgs) > 0 {
		// if the fields exist, then let's print em
		if ad.OptionalFields.OrgMap != nil {
			for _, org := range missing_orgs {
				om := ad.OptionalFields.OrgMap[org]
				slog.Error("missing organization", "name", om.Name, "abbreviation", om.Abbreviation, "desc", om.Description, "url", om.Url)
			}
		}
		return fmt.Errorf("these orgs are missing from your instance: %s: %w", strings.Join(missing_orgs, ","), ErrOrgNotFound)
	}

	// Step 2: Check if all the tools are there, alert with each tool, product info

	instance_tools, err := GetAllDefenseTools(ctx, client, db)
	if err != nil {
		return fmt.Errorf("could not fetch tools: %w", err)
	}

	tool_map := make(map[string]GetAllDefenseToolsBluetoolsBlueToolConnectionNodesBlueTool, len(ad.ToolsMap))

	missing_tools := []GenericBlueTool{}
	slog.Debug("Validating tools",
		"total", len(ad.ToolsMap))
	for name, tool := range ad.ToolsMap {
		found := false
		for _, instance_tool := range instance_tools.Bluetools.Nodes {
			if name == instance_tool.Name {
				found = true
				tool_map[instance_tool.Name] = instance_tool
				break
			}
		}
		if !found {
			missing_tools = append(missing_tools, tool)
		}
	}
	if len(missing_tools) > 0 {
		for _, missing_tool := range missing_tools {
			slog.Error("Missing tool in target database",
				"db", db,
				"tool-name", missing_tool.Name,
				"product (optional)", missing_tool.ProductName,
			)
		}
		return ErrMissingTools

	}

	if optionalParams.AssessmentName != "" {
		slog.Debug("overiding assessment name", "old-assessment-name", ad.Assessment.Name, "new-assessment-name", optionalParams.AssessmentName)
		ad.Assessment.Name = optionalParams.AssessmentName
	}

	lookup_assessments, err := FindExistingAssessment(ctx, client, db, ad.Assessment.Name)
	if err != nil {
		return fmt.Errorf("could not fetch data about assessment %s, error: %w", ad.Assessment.Name, err)
	}
	if len(lookup_assessments.Assessments.Nodes) > 0 {
		return fmt.Errorf("could not add %s into %s: %w", ad.Assessment.Name, db, ErrAssessmentAlreadyExists)
	}

	// Step 3: Check if there is a template name in the seralized data, if so check in the instance (error if not)
	// If the user wants to ignore error, go ahead and import template test cases
	// If no template name, then go ahead and add template test cases in
	instance_library_test_case := make(map[string]GetLibraryTestCasesLibraryTestcasesByIdsTestCaseConnectionNodesTestCase, len(ad.LibraryTestCases))
	if optionalParams.OverrideAssessmentTemplate == false && ad.TemplateAssessment != "" {
		slog.Debug("Validating template assessment in instance",
			"template_assessment", ad.TemplateAssessment,
			"override_template", optionalParams.OverrideAssessmentTemplate)
		prefix := ""
		for _, md := range ad.Assessment.Metadata {
			if md.Key == "prefix" {
				prefix = md.Value + " - "
				break
			}
		}
		t, err := FindLibraryAssessment(ctx, client, prefix+ad.TemplateAssessment)
		if err != nil {
			return fmt.Errorf("could not fetch library assessment for %s: %w", ad.TemplateAssessment, err)
		}
		slog.Debug("checking for library test case content")
		input := CreateTestCaseTemplateInput{
			Overwrite:            true,
			TestCaseTemplateData: []CreateTestCaseTemplateDataInput{},
		}

		ids := slices.Collect(maps.Keys(ad.LibraryTestCases))
		if len(ids) > 0 {
			missing_ids := []string{}
			// first time, we never really need to check the response, if the missing ids remain none,
			// we don't need to do anything
			r, err := GetLibraryTestCases(ctx, client, ids)
			if err != nil {
				gqlerrlist, ok := err.(gqlerror.List)
				if !ok {
					return fmt.Errorf("could not fetch library test cases for %s: %w", ad.TemplateAssessment, err)
				}

				// the error type we expect only has one entry for this path
				if !(len(gqlerrlist) == 1 && gqlerrlist[0].Path.String() == "libraryTestcasesByIds") {
					return fmt.Errorf("could not fetch library test cases for %s: %w", ad.TemplateAssessment, err)
				}
				// there should be an `ids` field in the extensions object
				rawids, ok := gqlerrlist[0].Extensions["ids"]
				if !ok {
					return fmt.Errorf("could not fetch library test cases for %s: %w", ad.TemplateAssessment, err)
				}
				// the `ids` filed should only have one entry
				ids, ok := rawids.([]any)
				if !(ok && len(ids) == 1) {
					return fmt.Errorf("could not fetch library test cases for %s: %w", ad.TemplateAssessment, err)
				}

				id := ids[0].(string)
				if !strings.HasPrefix(id, "The following IDs were not valid") {
					return fmt.Errorf("could not fetch library test cases for %s: %w", ad.TemplateAssessment, err)
				}
				// this is a case where we got an error back for an otherwise valid query, one or more of the ids are not valid
				mids, err := ParseLibraryTestcasesByIdsError(id)
				if err != nil {
					return fmt.Errorf("could not fetch library test cases for %s: %w", ad.TemplateAssessment, err)
				}
				missing_ids = append(missing_ids, mids...)
			}
			if len(missing_ids) > 0 {
				// if there are missing ids, match the missing ones to the serialized ids
				for _, id := range missing_ids {
					input.TestCaseTemplateData = append(input.TestCaseTemplateData, createTemplateData(ad.LibraryTestCases[id]))
					instance_library_test_case[id] = ad.LibraryTestCases[id]
				}

				// create a list of ids to fetch from the instance
				ids_to_fetch := make([]string, 0, len(ad.LibraryTestCases)-len(missing_ids))

				for id, _ := range ad.LibraryTestCases {
					if _, ok := instance_library_test_case[id]; !ok {
						ids_to_fetch = append(ids_to_fetch, id)
					}
				}
				// this should always be greater than 0 since there are missing ids
				// but doesn't hurt to check
				if len(ids_to_fetch) > 0 {
					r, err = GetLibraryTestCases(ctx, client, ids_to_fetch)
					if err != nil {
						// this time, actually just bail out. this should have been a clean insert
						return fmt.Errorf("could not fetch library test cases for %s: %w", ad.TemplateAssessment, err)
					}
					// add in the ones that were found
					for _, ltc := range r.LibraryTestcasesByIds.Nodes {
						instance_library_test_case[ltc.LibraryTestCaseId] = ltc
					}
				} else {
					slog.Error("Data issue, we have missing ids, but could not find them in the map, this is a bug",
						"missing-ids", missing_ids,
						"serialized-ids", slices.Collect(maps.Keys(ad.LibraryTestCases)))
					return fmt.Errorf("could not process data, fatal error")
				}
			} else {
				// this means everything came back
				for _, iltc := range r.LibraryTestcasesByIds.Nodes {
					instance_library_test_case[iltc.LibraryTestCaseId] = iltc
				}
			}

		}
		// if the defined library assessment does not exist, check to see if we have all library test cases
		if len(t.LibraryAssessments.Nodes) == 0 {
			if len(input.TestCaseTemplateData) > 0 {
				return fmt.Errorf("import %s: %w", ad.TemplateAssessment, ErrMissingLibraryAssessment)
			} else {
				slog.Warn("Could not find library assessment, but found all test cases.", "template_assessment", ad.TemplateAssessment)
			}
		}

		// if the defined library assessment does exist, but has missing test cases, add them in
		if len(input.TestCaseTemplateData) > 0 {
			slog.Info("Some test cases are mssing for the assessment, creating them",
				"library-assessment", prefix+ad.TemplateAssessment,
				"template-test-cases", input.TestCaseTemplateData,
			)
			_, err := CreateTemplateTestCases(ctx, client, input)
			if err != nil {
				return fmt.Errorf("could not write extra template test cases, err: %w", err)
			}
		}
	} else {
		slog.Debug("adding template test cases directly")
		// If we are ignoring existing templates or no name was set
		input := CreateTestCaseTemplateInput{
			Overwrite:            true,
			TestCaseTemplateData: []CreateTestCaseTemplateDataInput{},
		}

		if len(ad.LibraryTestCases) > 0 {
			for _, template_test_case := range ad.LibraryTestCases {
				slog.Debug("library test case", "name", template_test_case.Name, "template_id", template_test_case.LibraryTestCaseId)
				input.TestCaseTemplateData = append(input.TestCaseTemplateData, createTemplateData(template_test_case))
				instance_library_test_case[template_test_case.LibraryTestCaseId] = template_test_case
			}

			_, err := CreateTemplateTestCases(ctx, client, input)
			if err != nil {
				return fmt.Errorf("could not write template test cases: %w", err)
			}
			slog.Info("inserted all library test cases", "total", len(input.TestCaseTemplateData))
		} else {
			slog.Info("No library test cases found", "assessment-name", ad.Assessment.Name)
		}

	}

	if ad.Metadata != nil {
		ad.Metadata.LoadData = NewVatOpMetadata(ctx)
	} else {
		ad.Metadata = &VatMetadata{
			LoadData: NewVatOpMetadata(ctx),
		}
	}

	// Step 4: Create the assessment
	slog.Info("Creating assessment",
		"assessment_name", ad.Assessment.Name)
	assessment := &CreateAssessmentInput{
		Db: db,
		AssessmentData: []CreateAssessmentDataInput{
			CreateAssessmentDataInput{
				Name:        ad.Assessment.Name,
				Description: ad.Assessment.Description,
				KillChainId: ad.Assessment.KillChain.Id,
				DataVer:     ad.Assessment.DefaultTcDataVer,
				//OrganizationIds: []string{}, //handle below
				//Metadata: []MetadataKeyValuePairInput{}, // handle below
			},
		},
	}

	for _, o := range ad.Assessment.Organizations {
		assessment.AssessmentData[0].OrganizationIds = append(assessment.AssessmentData[0].OrganizationIds, org_map[o.Name].Id)
	}
	ad.Assessment.Metadata = loadVatMetadata(ad.Assessment.Metadata, ad.Metadata)
	for _, md := range ad.Assessment.Metadata {
		assessment.AssessmentData[0].Metadata = append(assessment.AssessmentData[0].Metadata, MetadataKeyValuePairInput(md))
	}

	a, err := CreateAssessment(ctx, client, *assessment)
	if err != nil {
		return fmt.Errorf("could not create assessment container: %s: %w", assessment.AssessmentData[0].Name, err)
	}
	//a.Assessment.Create.Assessments[0].Id

	// Step 5: Create the campaigns
	campaigns := CreateCampaignInput{
		Db:           db,
		AssessmentId: a.Assessment.Create.Assessments[0].Id,
		CampaignData: []CreateCampaignDataInput{},
	}
	for _, c := range ad.Assessment.Campaigns {
		campaign := CreateCampaignDataInput{
			Name:        c.Name,
			Description: c.Description,
		}
		for _, o := range c.Organizations {
			campaign.OrganizationIds = append(campaign.OrganizationIds, org_map[o.Name].Id)
		}
		for _, md := range c.Metadata {
			campaign.Metadata = append(campaign.Metadata, MetadataKeyValuePairInput(md))
		}
		campaigns.CampaignData = append(campaigns.CampaignData, campaign)
	}
	slog.Info("Creating campaigns",
		"count", len(campaigns.CampaignData),
		"assessment_name", ad.Assessment.Name)
	r, err := CreateCampaigns(ctx, client, campaigns)
	if err != nil {
		return fmt.Errorf("could not create campaigns for %s, suggest deleting the assessment: %w", a.Assessment.Create.Assessments[0].Name, err)
	}
	// Note that this creates a bug where if two campaigns are the same name, it will not work.
	// To be fixed if you'll need to insert each campaign individually so you can map them
	// For now this is fine
	campaign_map := make(map[string]string)
	for _, cdata := range r.Campaign.Create.Campaigns {
		campaign_map[cdata.Name] = cdata.Id
	}

	// Step 6: Create the test cases but need to do a calculation if the highest outcome from the tool doesn't match the test case, set override
	for _, c := range ad.Assessment.Campaigns {
		// there could be a mix of test case types in a campaign, so add both types in
		tc_with_template_name := CreateTestCaseAndTemplateMatchByNameInput{
			Db:                   db,
			CampaignId:           campaign_map[c.Name],
			CreateTestCaseInputs: []CreateTestCaseDataWithTemplateNameInput{},
		}

		tc_no_template := CreateTestCaseWithoutTemplateInput{
			Db:           db,
			CampaignId:   campaign_map[c.Name],
			TestCaseData: []CreateTestCaseDataInput{},
		}

		// have to do this here (maybe make this an object in the future)
		// but basically, I need to check if the outcome is in the map
		// if it is not, throw an error
		for _, serialized_tc := range c.TestCases {
			if _, ok := outcomeStatusMap[serialized_tc.Status]; !ok {
				slog.Error("could not find outcome for this test case", "outcome", serialized_tc.Status, "test-case", serialized_tc.Name, "campaign", c.Name)
				return fmt.Errorf("outcome %s not found", serialized_tc.Status)
			}
			testCaseData := CreateTestCaseDataInput{
				Name:             serialized_tc.Name,
				Description:      serialized_tc.Description,
				Phase:            serialized_tc.Phase.Name,
				Technique:        serialized_tc.MitreId,
				Organization:     serialized_tc.Organizations[0].Name,
				Status:           outcomeStatusMap[serialized_tc.Status],
				DetectionSteps:   serialized_tc.DetectionGuidance,
				PreventionSteps:  serialized_tc.PreventionGuidance,
				OutcomePath:      serialized_tc.Outcome.Path,
				OutcomeNotes:     serialized_tc.OutcomeNotes,
				DetectionTime:    serialized_tc.DetectionTime.CreateTime,
				References:       serialized_tc.References,
				OperatorGuidance: serialized_tc.OperatorGuidance,
				AttackStart:      serialized_tc.AttackStart.CreateTime,
				AttackStop:       serialized_tc.AttackStop.CreateTime,
				DataVer:          serialized_tc.DataVer,
				OverrideOutcome:  serialized_tc.OverrideOutcome,
				//Tags:                  []string{}, //to be handled below
				//Targets:               []string{}, // to be handled below
				//Sources:               []string{},
				//Defenses:              []string{},
				//DetectingDefenseTools: []DefenseToolInput{},          // handle below
				//RedTeamMetadata:       []MetadataKeyValuePairInput{}, //handle below
				//BlueTeamMetadata:      []MetadataKeyValuePairInput{}, // handle below
				//AttackAutomation:      AttackAutomationInput{},       //handle below
				//RedTools:              []RedToolInput{},
				//DefenseToolOutcomes:   []DefenseToolOutcomeInput{},   // handle below
			}
			for _, tag := range serialized_tc.Tags {
				testCaseData.Tags = append(testCaseData.Tags, tag.Name)
			}
			for _, target := range serialized_tc.Targets {
				testCaseData.Targets = append(testCaseData.Targets, target.Name)
			}
			for _, source := range serialized_tc.Sources {
				testCaseData.Sources = append(testCaseData.Sources, source.Name)
			}
			for _, defense := range serialized_tc.DefensiveLayers {
				testCaseData.Defenses = append(testCaseData.Defenses, defense.Name)
			}
			for _, detectingdefensetool := range serialized_tc.BlueTools {
				testCaseData.DetectingDefenseTools = append(testCaseData.DetectingDefenseTools, DefenseToolInput{
					Name: detectingdefensetool.Name,
				})
			}
			for _, md := range serialized_tc.Metadata {
				testCaseData.RedTeamMetadata = append(testCaseData.RedTeamMetadata, MetadataKeyValuePairInput(md))
			}
			if serialized_tc.AutomationCmd != "" {
				testCaseData.AttackAutomation = &AttackAutomationInput{
					Command:         serialized_tc.AutomationCmd,
					Executor:        executorMap[serialized_tc.AutomationExecutor],
					CleanupCommand:  serialized_tc.AutomationCleanup,
					CleanupExecutor: executorMap[serialized_tc.AutomationCleanupExecutor],
				}
				for _, autoArg := range serialized_tc.AutomationArgument {
					testCaseData.AttackAutomation.AttackVariables = append(testCaseData.AttackAutomation.AttackVariables, AttackAutomationVariable{
						InputName:  autoArg.ArgumentKey,
						InputValue: autoArg.ArgumentValue,
						Type:       AutomationVarType(strings.ToUpper(autoArg.ArgumentType)),
					})
				}
			}
			for _, redtool := range serialized_tc.RedTools {
				testCaseData.RedTools = append(testCaseData.RedTools, RedToolInput{
					Name: redtool.Name,
				})
			}

			for _, result := range serialized_tc.DefenseToolOutcomes {
				testCaseData.DefenseToolOutcomes = append(testCaseData.DefenseToolOutcomes, DefenseToolOutcomeInput{
					// take the stringifed integer from the serialized data, look up the tool name from the original data set
					//		and then look up the id in the new instance
					DefenseToolId: tool_map[ad.IdToolsMap[strconv.Itoa(result.DefenseToolId)].Name].Id,
					OutcomeId:     result.OutcomeId,
				})
			}
			// if there is no library test case id, then add with no template
			if serialized_tc.LibraryTestCaseId == "" || serialized_tc.LibraryTestCaseId == "null" {
				tc_no_template.TestCaseData = append(tc_no_template.TestCaseData, testCaseData)
			} else {
				// otherwise, create with template
				tcd := CreateTestCaseDataWithTemplateNameInput{
					TemplateNameDetail: TemplateNamePrefixInput{
						TemplateName: instance_library_test_case[serialized_tc.LibraryTestCaseId].Name,
					},
					TestCaseData: testCaseData,
				}
				tc_with_template_name.CreateTestCaseInputs = append(tc_with_template_name.CreateTestCaseInputs, tcd)

			}
		}
		slog.Info("Creating test cases",
			"campaign_name", c.Name,
			"test_case_count", len(tc_with_template_name.CreateTestCaseInputs),
			"test-case-count-no-template", len(tc_no_template.TestCaseData),
			"assessment_name", ad.Assessment.Name)
		if len(tc_with_template_name.CreateTestCaseInputs) > 0 {
			_, err := CreateTestCases(ctx, client, tc_with_template_name)
			if err != nil {
				return fmt.Errorf("could not write test cases for %s: %w", ad.Assessment.Name, err)
			}
		}
		if len(tc_no_template.TestCaseData) > 0 {
			_, err := CreateTestCasesNoTemplate(ctx, client, tc_no_template)
			if err != nil {
				return fmt.Errorf("could not write test cases for %s: %w", ad.Assessment.Name, err)
			}
		}
	}

	return nil

}

func loadVatMetadata(md []GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentMetadataMetadataKeyValuePair, vatMetadata *VatMetadata) []GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentMetadataMetadataKeyValuePair {
	for k, v := range vatMetadata.Serialize() {
		md = append(md, GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentMetadataMetadataKeyValuePair{
			Key:   k,
			Value: v,
		})
	}
	return md
}

func createTemplateData(template_test_case GetLibraryTestCasesLibraryTestcasesByIdsTestCaseConnectionNodesTestCase) CreateTestCaseTemplateDataInput {
	ttc := CreateTestCaseTemplateDataInput{
		LibraryTestCaseId: template_test_case.LibraryTestCaseId,
		Name:              template_test_case.Name,
		Description:       template_test_case.Description,
		Phase:             template_test_case.Phase.Name,
		Technique:         template_test_case.MitreId,
		// Tags:              []string{}, //handle below
		Organization: template_test_case.Organizations[0].Name,
		// Defenses:          []string{}, //handle below
		DetectionSteps:  template_test_case.DetectionGuidance,
		PreventionSteps: template_test_case.PreventionGuidance,
		References:      template_test_case.References,
		// RedTools:          []RedToolInput{}, //handle below
		OperatorGuidance: template_test_case.OperatorGuidance,
		// RedTeamMetadata:   []MetadataKeyValuePairInput{}, //handle below
		// BlueTeamMetadata:  []MetadataKeyValuePairInput{}, //handle below
		// AttackAutomation:  &AttackAutomationInput{},      //handle below
		// TemplatePrefix:    "",                            //handle below
	}
	for _, tag := range template_test_case.Tags {
		ttc.Tags = append(ttc.Tags, tag.Name)
	}

	for _, defense := range template_test_case.DefensiveLayers {
		ttc.Defenses = append(ttc.Defenses, defense.Name)
	}
	for _, redtool := range template_test_case.RedTools {
		ttc.RedTools = append(ttc.RedTools, RedToolInput{Name: redtool.Name})
	}
	for _, md := range template_test_case.Metadata {
		ttc.BlueTeamMetadata = append(ttc.BlueTeamMetadata, MetadataKeyValuePairInput{
			Key:   md.Key,
			Value: md.Value,
		})
	}
	if template_test_case.AutomationCmd != "" {
		ttc.AttackAutomation = &AttackAutomationInput{
			Command:         template_test_case.AutomationCmd,
			Executor:        executorMap[template_test_case.AutomationExecutor],
			CleanupCommand:  template_test_case.AutomationCleanup,
			CleanupExecutor: executorMap[template_test_case.AutomationCleanupExecutor],
		}
		for _, autoArg := range template_test_case.AutomationArgument {
			ttc.AttackAutomation.AttackVariables = append(ttc.AttackAutomation.AttackVariables, AttackAutomationVariable{
				InputName:  autoArg.ArgumentKey,
				InputValue: autoArg.ArgumentValue,
				Type:       AutomationVarType(strings.ToUpper(autoArg.ArgumentType)),
			})

		}
	}
	// check for the prefix
	for _, md := range template_test_case.Metadata {
		if md.Key == "prefix" {
			ttc.TemplatePrefix = md.Value
			// There is a bug in the template test case create where if there is a prefix it will keep adding,
			// it onto the name, you gotta remove it to insert it.
			// #VECTRBUG
			ttc.Name = strings.TrimPrefix(template_test_case.Name, ttc.TemplatePrefix+" - ")
			break
		}
	}
	return ttc
}

func ParseLibraryTestcasesByIdsError(e string) ([]string, error) {
	// Define the prefix to look for
	prefix := "The following IDs were not valid: "
	if !strings.HasPrefix(e, prefix) {
		return nil, fmt.Errorf("input string does not start with the expected prefix")
	}

	// Remove the prefix to get the IDs part
	idsPart := strings.TrimPrefix(e, prefix)
	ids := strings.Split(idsPart, ", ")

	var uuids []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		_, err := uuid.Parse(id)
		if err != nil {
			return nil, fmt.Errorf("could not parse %s: %w", id, err)
		}
		uuids = append(uuids, id)
	}

	if len(uuids) == 0 {
		return nil, fmt.Errorf("no valid UUIDs found in the input string")
	}

	return uuids, nil
}
