---
name: Smoke Project
description: Smoke Project - Test project operations
on: 
  workflow_dispatch:
  #schedule: every 12h
  pull_request:
    types: [labeled]
    names: ["smoke"]
  reaction: "eyes"
permissions:
  contents: read
  pull-requests: read
  issues: read
  actions: read
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
    create-pull-request:
      title-prefix: "[smoke-project] "
      if-no-changes: "warn"
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
      project: "https://github.com/orgs/github/projects/24068"
      views:
        - name: "Smoke Test Board"
          layout: board
          filter: "is:open"
      github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
    create-project-status-update:
      max: 1
      project: "https://github.com/orgs/github/projects/24068"
      github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
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

Default status field for any created items: "Todo".
Do the following operations EXACTLY in this order.
Do not re-create draft items but use their returned temporary-ids for the update operations.

## Test Requirements

1. **Add items**: Create items in the project using different content types:

   a. **Draft Issue Creation**:
      Call `update_project` with:
      - `project`: "https://github.com/orgs/github/projects/24068"
      - `content_type`: "draft_issue"
      - `draft_title`: "Test *draft issue* for `smoke-project`"
      - `draft_body`: "Test draft issue for smoke test validation"
      - `temporary_id`: "draft-1"
      - `fields`: `{"Status": "Todo", "Priority": "High"}`

   b. **PR Creation**:
      Call `update_project` with:
        - `project`: "https://github.com/orgs/github/projects/24068"
        - `content_type`: "pull_request"
        - `content_number`: 14477
        - `fields`: `{"Status": "Todo", "Priority": "High"}`

   c. **Issue Creation**:
      Call `update_project` with:
        - `project`: "https://github.com/orgs/github/projects/24068"
        - `content_type`: "issue"
        - `content_number`: 14478
        - `fields`: `{"Status": "Todo", "Priority": "High"}`

2. **Update items**: Update the created items to validate field updates:

   a. **Draft Issue Update**:
      Call `update_project` with the draft issue you created (use the returned temporary-id) to change status to "In Progress":
      - `project`: "https://github.com/orgs/github/projects/24068"
      - `content_type`: "draft_issue"
      - `draft_issue_id`: The temporary-id returned from step 1a (e.g., "aw_abc123def456")
      - `fields`: `{"Status": "In Progress"}`

   b. **Pull Request Update**:
      Call `update_project` to update the pull request item to change status to "In Progress":
      - `project`: "https://github.com/orgs/github/projects/24068"
      - `content_type`: "pull_request"
      - `content_number`: 14477
      - `fields`: `{"Status": "In Progress"}`

    c. **Issue Update**:
      Call `update_project` to update the issue item to change status to "In Progress":
      - `project`: "https://github.com/orgs/github/projects/24068"
      - `content_type`: "issue"
      - `content_number`: 14478
      - `fields`: `{"Status": "In Progress"}`

3. **Project Status Update**:

   a. Create a markdown report summarizing all the operations performed. Keep it short but make it clear what worked and what didn't:
     Example `body`:
     ```md
     ## Run Summary
     - Run: [{workflow_name}]({run_url})
     - List of operations performed:
       - [x] Created *draft issue* update with status "Todo"
       - [ ] ...
     ```

   b. Call `create_project_status_update` with the report from step 3a.
     Required fields:
    - `project`: "https://github.com/orgs/github/projects/24068"
    - `body`: The markdown report created in step 3a
     Optional fields:
    - `status`: "ON_TRACK" | "AT_RISK" | "OFF_TRACK" | "COMPLETE" | "INACTIVE"
    - `start_date`: Optional date in "YYYY-MM-DD" format (if you want to represent the run start)
    - `target_date`: Optional date in "YYYY-MM-DD" format (if you want to represent the run target/end)
