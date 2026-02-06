---
description: Test ProjectOps PAT requirements with actual trial repository testing
on:
  workflow_dispatch:
    inputs:
      test_user_projects:
        description: "Test user-owned Projects (requires classic PAT in GH_AW_PROJECT_GITHUB_TOKEN)"
        type: boolean
        default: "true"
      test_org_projects:
        description: "Test org-owned Projects (requires PAT with org access)"
        type: boolean
        default: "true"
      cleanup_trial_repos:
        description: "Delete trial repositories after testing"
        type: boolean
        default: "true"
  schedule: weekly on monday
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
name: Test ProjectOps PAT Requirements
engine: copilot
timeout-minutes: 45
tools:
  bash:
    - "*"
  github:
    mode: remote
    toolsets: [default]
safe-outputs:
  create-issue:
    max: 1
    expires: 1d
    labels: [documentation, projectops, testing, trialops]
---

# Test ProjectOps PAT Requirements with TrialOps

This workflow performs actual integration testing of ProjectOps PAT requirements by:
1. Creating trial repositories
2. Testing different PAT configurations with actual GitHub Projects v2 API calls
3. Verifying documented requirements match real behavior
4. Cleaning up trial repositories after testing

## Prerequisites

This test requires PATs to be configured:
- `GH_AW_PROJECT_GITHUB_TOKEN`: PAT for testing (should have appropriate project scopes)
- The workflow will test if the configured PAT works as documented

## Test Execution

Use `gh aw trial` to test ProjectOps workflows with different PAT configurations in isolated trial repositories:

### Test 1: User-owned Projects with Classic PAT

Test if a classic PAT with `project` scope can successfully manage user-owned Projects v2.

**Steps**:
1. Create a trial repository: `gh-aw-trial-projectops-user`
2. Create a test workflow that uses `update-project` to:
   - Add an issue to a user-owned Project
   - Update project fields
   - Verify the operations succeed
3. Run the workflow with the configured PAT
4. Check if operations succeed as documented

**Expected Result**: Operations should succeed if using classic PAT with `project` scope

### Test 2: Organization-owned Projects with Classic PAT

Test if a classic PAT with `project` + `read:org` can manage org-owned Projects v2.

**Steps**:
1. Create a trial repository in an organization context
2. Create a test workflow that uses `update-project` to:
   - Add an issue to an org-owned Project
   - Update project fields
   - Verify the operations succeed
3. Run with classic PAT having `project` + `read:org` scopes

**Expected Result**: Operations should succeed with proper org permissions

### Test 3: Organization-owned Projects with Fine-grained PAT

Test if a fine-grained PAT with explicit org access + Projects: Read+Write can manage org-owned Projects.

**Steps**:
1. Use the same trial repository
2. Test with fine-grained PAT (if configured separately)
3. Verify org-owned project operations work

**Expected Result**: Should work if org access was explicitly granted

### Test 4: Document Fine-grained PAT Limitation for User Projects

Test and document that fine-grained PATs do NOT work with user-owned Projects.

**Note**: This test documents the limitation but may not be executable if we don't have a fine-grained PAT configured for comparison.

## Implementation

Execute the following tests using bash commands:

**Step 1**: Create a minimal test workflow that attempts ProjectOps operations

**Step 2**: Use `gh aw trial` command to run the workflow in an isolated trial repository

**Step 3**: Analyze the results to determine if the configured PAT works as documented

**Step 4**: Report findings in an issue

The AI agent will:
1. Generate a test workflow dynamically
2. Execute it using the gh-aw trial command with appropriate flags
3. Parse the results to determine PAT permission compatibility
4. Create a summary issue with findings

This provides real integration testing of the documented PAT requirements using actual GitHub Projects v2 API operations.

## Test Results Report

Create an issue summarizing the test results:

**Title**: "ProjectOps PAT TrialOps Test Results - $(date +%Y-%m-%d)"

**Body**:

### Test Summary

**Test Date**: $(date -u +"%Y-%m-%d %H:%M:%S UTC")
**Trial Repository**: [Link to trial repo if not deleted]
**Workflow Run**: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

### Test Configuration

- User-owned Projects Test: Configured via workflow dispatch input
- Org-owned Projects Test: Configured via workflow dispatch input
- Cleanup Trial Repos: Configured via workflow dispatch input

### Results

#### User-owned Projects
- **Classic PAT with project scope**: [PASS/FAIL/NOT_TESTED]
  - Details: [Error messages or success confirmation]
  
- **Fine-grained PAT**: [EXPECTED_TO_FAIL/NOT_TESTED]
  - Details: [Confirmation of documented limitation]

#### Organization-owned Projects
- **Classic PAT with project + read:org**: [PASS/FAIL/NOT_TESTED]
  - Details: [Error messages or success confirmation]
  
- **Fine-grained PAT with org access**: [PASS/FAIL/NOT_TESTED]
  - Details: [Error messages or success confirmation]

### Documentation Validation

- **Documentation Accuracy**: [CONFIRMED/ISSUES_FOUND]
- **Issues Found**: [List any discrepancies between docs and actual behavior]

### Recommendations

[Any updates needed to documentation based on test results]

### Trial Artifacts

- Trial repository: [URL or "Deleted after test"]
- Trial results JSON: [Path to local results file]
- GitHub Actions logs: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

---

**Note**: These tests use actual GitHub Projects v2 API calls with configured PATs. Results may vary based on:
- PAT type and scopes configured
- Organization settings and policies
- Project ownership (user vs org)
- GitHub API rate limits

For manual testing, see the [TrialOps](/gh-aw/guides/trialops/) and [ProjectOps Documentation](/gh-aw/examples/issue-pr-events/projectops/).

