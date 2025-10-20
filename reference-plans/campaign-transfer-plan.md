# Campaign Transfer Implementation Plan

Here is a detailed implementation plan with code samples.

## Phase 1: Refactor `restore.go` for Modularity - **COMPLETED**

The goal of this phase was to break up the `RestoreAssessment` function into smaller, reusable pieces without changing its current behavior.

### 1. Create `validateRestorePrerequisites` Function - **COMPLETED**

Moved the organization and tool validation logic into a new internal function. This allows both full assessment and single campaign restores to share the same validation code.

### 2. Create `restoreCampaigns` Function - **COMPLETED**

Moved the campaign and test case creation logic into its own function.

### 3. Update `RestoreAssessment` - **COMPLETED**

Modified `RestoreAssessment` to use the new, smaller functions.
```

## Phase 2: Implement Campaign-Only Restore Logic - **COMPLETED**

Created the new exported function that handles restoring just a single campaign.

### 1. Add `RestoreCampaign` Function - **COMPLETED**

This function orchestrates the campaign-only restore by reusing the validation and campaign creation functions from Phase 1.
```

## Phase 3: Update `cmd/transfer.go` to Use New Feature - **COMPLETED**

Exposed the new functionality through the `transfer` command.

### 1. Add New CLI Flag - **COMPLETED**

Added an optional flag to specify the source campaign name.

### 2. Add Conditional Logic to `transfer` Command - **COMPLETED**

Implemented conditional logic in the command's `Run` function to call `RestoreCampaign` when the new flag is present, or `RestoreAssessment` otherwise.
