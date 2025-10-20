# Campaign Transfer Implementation Plan

Here is a detailed implementation plan with code samples.

## Phase 1: Refactor `restore.go` for Modularity

The goal of this phase is to break up the `RestoreAssessment` function into smaller, reusable pieces without changing its current behavior.

### 1. Create `validateRestorePrerequisites` Function - **COMPLETED**

Move the organization and tool validation logic into a new internal function. This will allow both full assessment and single campaign restores to share the same validation code.

*Example Implementation in `restore.go`:*
```go
// Add this new function
func validateRestorePrerequisites(ctx context.Context, client graphql.Client, db string, ad *AssessmentData) (map[string]dao.FindOrganizationOrganizationsOrganizationConnectionNodesOrganization, map[string]dao.GetAllDefenseToolsBluetoolsBlueToolConnectionNodesBlueTool, error) {
    // ... (Cut and paste the entire logic for Step 1: Organization Check) ...
    // ... (Cut and paste the entire logic for Step 2: Tool Check) ...
    
    // Return the maps and any error
    return org_map, tool_map, nil
}
```

### 2. Create `restoreCampaigns` Function

Move the campaign and test case creation logic into its own function.

*Example Implementation in `restore.go`:*
```go
// Add this new function
func restoreCampaigns(ctx context.Context, client graphql.Client, db string, assessmentId string, assessmentName string, campaignsToRestore []dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentCampaignsCampaignConnectionNodesCampaign, orgMap map[string]dao.FindOrganizationOrganizationsOrganizationConnectionNodesOrganization, toolMap map[string]dao.GetAllDefenseToolsBluetoolsBlueToolConnectionNodesBlueTool, idToolsMap map[string]GenericBlueTool) error {
    // ... (Cut and paste the entire logic from "Step 5: Create the campaigns" and "Step 6: Create the test cases") ...
    // ... (This logic starts with creating the 'campaigns' variable and ends with the "Test cases created" slog message) ...
    return nil
}
```

### 3. Update `RestoreAssessment`

Modify `RestoreAssessment` to use these new, smaller functions.

*Example modification in `restore.go`:*
```go
func RestoreAssessment(ctx context.Context, client graphql.Client, db string, ad *AssessmentData, optionalParams *RestoreOptionalParams) error {
    // ... (initial slog and metadata setup) ...

    org_map, tool_map, err := validateRestorePrerequisites(ctx, client, db, ad)
    if err != nil {
        // ... (error handling logic from old steps 1 & 2) ...
        return err // Simplified for example
    }

    // ... (existing logic for template checks, assessment name override, and existing assessment check) ...

    // Step 4: Create the assessment
    // ... (logic for creating the assessment shell) ...
    a, err := dao.CreateAssessment(ctx, client, *assessment)
    if err != nil {
        // ... (error handling) ...
    }

    // Call the new refactored function, replacing the old logic
    err = restoreCampaigns(ctx, client, db, a.Assessment.Create.Assessments[0].Id, ad.Assessment.Name, ad.Assessment.Campaigns, org_map, tool_map, ad.IdToolsMap)
    if err != nil {
        return err
    }

    return nil
}
```

## Phase 2: Implement Campaign-Only Restore Logic

Now, create the new exported function that handles restoring just a single campaign.

### 1. Add `RestoreCampaign` Function

This function will orchestrate the campaign-only restore by reusing the validation and campaign creation functions from Phase 1.

*Example Implementation in `restore.go`:*
```go
var ErrCampaignNotFound = fmt.Errorf("campaign not found")

func RestoreCampaign(ctx context.Context, client graphql.Client, db string, ad *AssessmentData, sourceCampaignName, targetAssessmentName string) error {
    slog.InfoContext(ctx, "Starting RestoreCampaign", "db", db, "source_campaign", sourceCampaignName, "target_assessment", targetAssessmentName)

    org_map, tool_map, err := validateRestorePrerequisites(ctx, client, db, ad)
    if err != nil {
        return err
    }

    var campaignToRestore dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentCampaignsCampaignConnectionNodesCampaign
    found := false
    for _, c := range ad.Assessment.Campaigns {
        if c.Name == sourceCampaignName {
            campaignToRestore = c
            found = true
            break
        }
    }
    if !found {
        return fmt.Errorf("in assessment data for '%s': %w: %s", ad.Assessment.Name, ErrCampaignNotFound, sourceCampaignName)
    }

    targetAssessment, err := dao.FindExistingAssessment(ctx, client, db, targetAssessmentName)
    if err != nil {
        return fmt.Errorf("could not look up target assessment '%s': %w", targetAssessmentName, err)
    }
    if len(targetAssessment.Assessments.Nodes) == 0 {
        return fmt.Errorf("target assessment '%s' not found in database '%s'", targetAssessmentName, db)
    }
    targetAssessmentId := targetAssessment.Assessments.Nodes[0].Id

    return restoreCampaigns(ctx, client, db, targetAssessmentId, targetAssessmentName, []dao.GetAllAssessmentsAssessmentsAssessmentConnectionNodesAssessmentCampaignsCampaignConnectionNodesCampaign{campaignToRestore}, org_map, tool_map, ad.IdToolsMap)
}
```

## Phase 3: Update `cmd/transfer.go` to Use New Feature

Finally, expose the new functionality through the `transfer` command.

### 1. Add New CLI Flag

Add an optional flag to specify the source campaign name.

*Example modification in `cmd/transfer.go` `init` function:*
```go
// At top of file with other var declarations
var sourceCampaignName string

// In init()
transferCmd.Flags().StringVar(&sourceCampaignName, "source-campaign-name", "", "Name of a specific campaign to transfer. If set, --target-assessment-name must be an existing assessment.")
```

### 2. Add Conditional Logic to `transfer` Command

In the command's `Run` function, check if the new flag is present. If it is, call `RestoreCampaign`; otherwise, execute the original `RestoreAssessment` logic.

*Example modification in `cmd/transfer.go` `Run` function:*
```go
// ... (after fetching assessmentData) ...

if sourceCampaignName == "" {
    // Original full assessment transfer logic
    slog.InfoContext(targetVersionContext, "Transferring assessment data to target instance", "hostname", targetHostname, "db", targetDB)
    optionalParams := &vat.RestoreOptionalParams{
        AssessmentName:             targetAssessmentName,
        OverrideAssessmentTemplate: overrideAssessmentTemplate,
    }
    if err := vat.RestoreAssessment(targetVersionContext, targetClient, targetDB, assessmentData, optionalParams); err != nil {
        slog.ErrorContext(targetVersionContext, "Failed to transfer assessment data to target instance", "error", err)
        os.Exit(1)
    }
} else {
    // New campaign-only transfer logic
    if targetAssessmentName == "" {
        slog.ErrorContext(ctx, "--target-assessment-name is required when using --source-campaign-name")
        os.Exit(1)
    }
    slog.InfoContext(targetVersionContext, "Transferring campaign to target assessment", "source-campaign", sourceCampaignName, "target-assessment", targetAssessmentName)
    if err := vat.RestoreCampaign(targetVersionContext, targetClient, targetDB, assessmentData, sourceCampaignName, targetAssessmentName); err != nil {
        slog.ErrorContext(targetVersionContext, "Failed to transfer campaign to target instance", "error", err)
        os.Exit(1)
    }
}

slog.InfoContext(ctx, "Assessment transferred successfully")
```
