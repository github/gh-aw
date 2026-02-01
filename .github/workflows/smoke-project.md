---
description: Smoke Project - Test project operations
on: 
  schedule: every 12h
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["smoke-project"]
  reaction: "eyes"
permissions:
  contents: read
  pull-requests: read
  issues: read
  actions: read
name: Smoke Project
engine: copilot
project: "https://github.com/orgs/github-agentic-workflows/projects/1"
imports:
  - shared/gh.md
  - shared/reporting.md
network:
  allowed:
    - defaults
    - node
    - github
tools:
  github:
  bash:
    - "*"
safe-outputs:
    add-comment:
      hide-older-comments: true
      max: 2
    create-issue:
      expires: 2h
      group: true
      close-older-issues: true
    add-labels:
      allowed: [smoke-project]
    remove-labels:
      allowed: [smoke-project]
    update-project:
      max: 20
      views:
        - name: "Smoke Test Board"
          layout: board
          filter: "is:open"
        - name: "Smoke Test Table"
          layout: table
      github-token: ${{ secrets.SMOKE_PROJECT_GITHUB_TOKEN }}
    create-project-status-update:
      max: 5
      github-token: ${{ secrets.SMOKE_PROJECT_GITHUB_TOKEN }}
    messages:
      append-only-comments: true
      footer: "> 🧪 *Project smoke test report by [{workflow_name}]({run_url})*"
      run-started: "🧪 [{workflow_name}]({run_url}) is now testing project operations..."
      run-success: "✅ [{workflow_name}]({run_url}) completed successfully. All project operations validated."
      run-failure: "❌ [{workflow_name}]({run_url}) encountered failures. Check the logs for details."
timeout-minutes: 15
strict: true
---

# Smoke Test: Project Operations Validation

**IMPORTANT: Keep all outputs extremely short and concise. Use single-line responses where possible. No verbose explanations.**

## Test Requirements

1. **Project Operations Testing**: Use project-related safe-output tools to validate multiple project features against the real project configured in the frontmatter. Steps:
   
   a. **Draft Issue Creation**: Call `update_project` with:
      - `content_type`: "draft_issue"
      - `draft_title`: "Smoke Test Draft Issue - Run ${{ github.run_id }}"
      - `draft_body`: "Test draft issue for smoke test validation"
      - `fields`: `{"Status": "Todo", "Priority": "High"}`
   
   b. **Field Creation with New Fields**: Call `update_project` with draft issue including new custom fields:
      - `content_type`: "draft_issue"
      - `draft_title`: "Smoke Test Draft Issue with Custom Fields - Run ${{ github.run_id }}"
      - `fields`: `{"Status": "Todo", "Priority": "High", "Team": "Engineering", "Sprint": "Q1-2026"}`
   
   c. **Field Update**: Call `update_project` again with the same draft issue to update fields:
      - `content_type`: "draft_issue"
      - `draft_title`: "Smoke Test Draft Issue - Run ${{ github.run_id }}"
      - `fields`: `{"Status": "In Progress", "Priority": "Medium"}`
   
   d. **Existing Issue Addition**: Use GitHub MCP to find any open issue from ${{ github.repository }}, then call `update_project` with:
      - `content_type`: "issue"
      - `content_number`: the issue number you found
      - `fields`: `{"Status": "In Review", "Priority": "Low"}`
   
   e. **Existing PR Addition**: Use GitHub MCP to find any open pull request from ${{ github.repository }}, then call `update_project` with:
      - `content_type`: "pull_request"
      - `content_number`: the PR number you found
      - `fields`: `{"Status": "In Progress", "Priority": "High"}`
   
   f. **View Creation**: The workflow automatically creates two views (configured in safe-outputs):
      - "Smoke Test Board" (board layout, filter: "is:open")
      - "Smoke Test Table" (table layout)
   
   g. **Project Status Update**: Call `create_project_status_update` with:
      - `body`: "Smoke test project status - Run ${{ github.run_id }}"
      - `status`: "ON_TRACK"
   
   h. **Verification**: For each operation:
      - Verify the safe-output message is properly formatted in the output file
      - Confirm the project URL auto-populates from frontmatter
      - Check that all field names and values are correctly structured
      - Validate content_type is correctly set for each operation type

2. **Project Scoping Validation**: Test proper scoping behavior with and without top-level project field to ensure operations stay within the correct project scope:
   
   a. **With Top-Level Project (Default Scoping)**: Call `update_project` WITHOUT specifying a project field in the message:
      - `content_type`: "draft_issue"
      - `draft_title`: "Scoping Test - Default Project - Run ${{ github.run_id }}"
      - `fields`: `{"Status": "Todo"}`
      - Verify the message uses the project URL from frontmatter configuration
   
   b. **Explicit Project Override Attempt**: Call `update_project` WITH an explicit different project field to test that scope is enforced:
      - `project`: "https://github.com/orgs/github-agentic-workflows/projects/999"
      - `content_type`: "draft_issue"
      - `draft_title`: "Scoping Test - Override Attempt - Run ${{ github.run_id }}"
      - `fields`: `{"Status": "Todo"}`
      - Verify the message respects the explicit project URL (override should be allowed for flexibility)
   
   c. **Status Update with Default Project**: Call `create_project_status_update` WITHOUT specifying a project field:
      - `body`: "Scoping test status update - Run ${{ github.run_id }}"
      - `status`: "AT_RISK"
      - Verify the status update uses the project URL from frontmatter
   
   d. **Status Update with Explicit Project**: Call `create_project_status_update` WITH an explicit project field:
      - `project`: "https://github.com/orgs/github-agentic-workflows/projects/999"
      - `body`: "Scoping test explicit project - Run ${{ github.run_id }}"
      - `status`: "OFF_TRACK"
      - Verify the message uses the explicitly provided project URL
   
   e. **Scoping Verification**: For all operations:
      - Confirm that when no project field is provided, the top-level project from frontmatter is used
      - Confirm that when an explicit project field is provided, it is used (allowing override)
      - Validate that all project URLs are properly formatted in safe-output messages
      - Ensure no operations stay within the configured project scope

## Output

1. **Create an issue** with a summary of the project smoke test run:
   - Title: "Smoke Test: Project Operations - ${{ github.run_id }}"
   - Body should include:
     - Test results (✅ or ❌ for each test)
     - Overall status: PASS or FAIL
     - Run URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
     - Timestamp

2. Add a **very brief** comment (max 5-10 lines) to the current pull request with:
   - Test results (✅ or ❌ for each test)
   - Overall status: PASS or FAIL

If all tests pass:
- Use the `add_labels` safe-output tool to add the label `smoke-project` to the pull request
- Use the `remove_labels` safe-output tool to remove the label `smoke-project` from the pull request
