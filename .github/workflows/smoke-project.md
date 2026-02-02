---
description: Smoke Project - Test project operations
on: 
  schedule: every 12h
  workflow_dispatch:
  pull_request:
    types: [labeled]
    names: ["smoke"]
  reaction: "eyes"
permissions:
  contents: read
  pull-requests: read
  issues: read
  actions: read
name: Smoke Project
engine: codex
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
      target-repo: github-agentic-workflows/demo-repository 
    create-issue:
      expires: 2h
      group: true
      close-older-issues: true
      target-repo: github-agentic-workflows/demo-repository  
    add-labels:
      allowed: [smoke-project]
      target-repo: github-agentic-workflows/demo-repository  
    remove-labels:
      allowed: [smoke-project]
      target-repo: github-agentic-workflows/demo-repository  
    update-project:
      max: 20
      project: "https://github.com/orgs/github-agentic-workflows/projects/1"
      views:
        - name: "Smoke Test Board"
          layout: board
          filter: "is:open"
        - name: "Smoke Test Table"
          layout: table
      github-token: ${{ secrets.SMOKE_PROJECT_GITHUB_TOKEN }}
    create-project-status-update:
      max: 1
      project: "https://github.com/orgs/github-agentic-workflows/projects/1"
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
      - `project`: "https://github.com/orgs/github-agentic-workflows/projects/1" (explicit project URL required)
      - `content_type`: "draft_issue"
      - `draft_title`: "Smoke Test Draft Issue - Run ${{ github.run_id }}"
      - `draft_body`: "Test draft issue for smoke test validation"
      - `fields`: `{"Status": "Todo", "Priority": "High"}`

   b. **Field Creation with New Fields**: Call `update_project` with draft issue including new custom fields:
      - `project`: "https://github.com/orgs/github-agentic-workflows/projects/1" (explicit project URL required)
      - `content_type`: "draft_issue"
      - `draft_title`: "Smoke Test Draft Issue with Custom Fields - Run ${{ github.run_id }}"
      - `fields`: `{"Status": "Todo", "Priority": "High", "Team": "Engineering", "Sprint": "Q1-2026"}`

   c. **Field Update**: Call `update_project` again with the same draft issue to update fields:
      - `project`: "https://github.com/orgs/github-agentic-workflows/projects/1" (explicit project URL required)
      - `content_type`: "draft_issue"
      - `draft_title`: "Smoke Test Draft Issue - Run ${{ github.run_id }}"
      - `fields`: `{"Status": "In Progress", "Priority": "Medium"}`

   d. **Existing Issue Addition**: Use GitHub MCP to find any open issue from ${{ github.repository }}, then call `update_project` with:
      - `project`: "https://github.com/orgs/github-agentic-workflows/projects/1" (explicit project URL required)
      - `content_type`: "issue"
      - `content_number`: the issue number you found
      - `fields`: `{"Status": "In Review", "Priority": "Low"}`
   
   e. **Existing PR Addition**: Use GitHub MCP to find any open pull request from ${{ github.repository }}, then call `update_project` with:
      - `project`: "https://github.com/orgs/github-agentic-workflows/projects/1" (explicit project URL required)
      - `content_type`: "pull_request"
      - `content_number`: the PR number you found
      - `fields`: `{"Status": "In Progress", "Priority": "High"}`
   
   f. **View Creation**: The workflow automatically creates two views (configured in safe-outputs):
      - "Smoke Test Board" (board layout, filter: "is:open")
      - "Smoke Test Table" (table layout)
   
   g. **Project Status Update**: Call `create_project_status_update` with:
      - `project`: "https://github.com/orgs/github-agentic-workflows/projects/1" (explicit project URL required)
      - `body`: "Smoke test project status - Run ${{ github.run_id }}"
      - `status`: "ON_TRACK"
   
   h. **Verification**: For each operation:
      - Verify the safe-output message is properly formatted in the output file
      - Confirm the project URL is explicitly included in each message
      - Check that all field names and values are correctly structured
      - Validate content_type is correctly set for each operation type

2. **Project URL Requirement Testing**: Test that the `project` field is required in all messages:
   
   a. **Explicit Project URL (Required)**: Call `update_project` WITH the project field:
      - `project`: "https://github.com/orgs/github-agentic-workflows/projects/1"
      - `content_type`: "draft_issue"
      - `draft_title`: "Test - Explicit Project URL - Run ${{ github.run_id }}"
      - `fields`: `{"Status": "Todo"}`
      - Verify the operation succeeds with the explicitly provided project URL
   
   b. **Different Project URL**: Call `update_project` WITH a different explicit project field to test flexibility:
      - `project`: "https://github.com/orgs/github-agentic-workflows/projects/1"
      - `content_type`: "draft_issue"
      - `draft_title`: "Test - Different Project - Run ${{ github.run_id }}"
      - `fields`: `{"Status": "Todo"}`
      - Verify the message uses the explicitly provided project URL
   
   c. **Status Update with Explicit Project**: Call `create_project_status_update` WITH the project field:
      - `project`: "https://github.com/orgs/github-agentic-workflows/projects/1"
      - `body`: "Test status update with explicit project - Run ${{ github.run_id }}"
      - `status`: "ON_TRACK"
      - Verify the status update uses the explicitly provided project URL
   
   d. **URL Format Verification**: For all operations:
      - Confirm that every message includes an explicit project URL
      - Validate that all project URLs are properly formatted in safe-output messages
      - Ensure operations target the correct project scope

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
