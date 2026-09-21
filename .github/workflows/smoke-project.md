---
private: true
emoji: "🧪"
name: Smoke Project
description: Smoke Project - Test project operations
on:
  schedule: every 2 days
  slash_command:
    name: smoke-project
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  #schedule: every 12h
  pull_request:
    types: [labeled]
    names: ["water"]
  reaction: "eyes"
  status-comment: true
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
    steer: true
    allowed-domains: [default-safe-outputs]
    add-comment:
      hide-older-comments: true
      max: 2
    create-pull-request:
      title-prefix: "[smoke-project] "
      if-no-changes: "warn"
      labels: [ai-generated]
      expires: 2h
    create-issue:
      expires: 2h
      labels: [ai-generated, automation, testing]
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
      footer: "> 🧪 *Project smoke test report by [{workflow_name}]({run_url})*{ai_credits_suffix}{history_link}"
      run-started: "🧪 [{workflow_name}]({run_url}) is now testing project operations..."
      run-success: "✅ [{workflow_name}]({run_url}) completed successfully. All project operations validated."
      run-failure: "❌ [{workflow_name}]({run_url}) encountered failures. Check the logs for details."
timeout-minutes: 15
strict: true
experiments:
  prompt_style_test:
    variants: [detailed, concise, step_by_step]
    description: "Test whether reducing prompt verbosity preserves project-operation success rate"
    hypothesis: "H0: no change in run_success_rate. H1: concise/step_by_step achieve ≥90% success rate with ≥30% fewer prompt tokens"
    metric: run_success_rate
    secondary_metrics: [run_duration_ms, prompt_token_count, ops_completed_count]
    analysis_type: proportion_test
    guardrail_metrics:
      - name: empty_output_rate
        threshold: "<=0.05"
      - name: missing_ops_rate
        threshold: "<=0.10"
    min_samples: 20
    weight: [34, 33, 33]
    start_date: "2026-06-06"
    tags: [smoke-test, prompt-engineering, verbosity]
    notify:
      issue: 37302
imports:
  - shared/otlp.md
  - shared/token-telemetry-check.md
  - shared/reporting.md
features:
  gh-aw-detection: false
evals:
  - id: prompt_style_test_goal_met
    question: Does the agent output show that the objective for experiment prompt_style_test was successfully completed?

---

# Smoke Test: Project Operations Validation

Default status field for any created items: "Todo".
Do not re-create draft items but use their returned temporary-ids for the update operations.

## Test Requirements

{{#if experiments.prompt_style_test == "concise" }}
Run the following project operations against `https://github.com/orgs/github/projects/24068`:

1. Add a draft issue titled "Test *draft issue* for `smoke-project`" (temporary_id: draft-1, Status: Todo, Priority: High)
2. Add PR github/gh-aw#14477 (Status: Todo, Priority: High)
3. Add issue github/gh-aw#14478 (Status: Todo, Priority: High)
4. Update the draft issue from step 1 to Status: In Progress
5. Update PR github/gh-aw#14477 to Status: In Progress
6. Update issue github/gh-aw#14478 to Status: In Progress
7. Post a project status update with a markdown checklist of all operations performed.
{{else if experiments.prompt_style_test == "step_by_step" }}
Execute each step in order:

1. `update_project` — draft_issue, title="Test *draft issue* for `smoke-project`", temporary_id=draft-1, Status=Todo, Priority=High
2. `update_project` — pull_request github/gh-aw#14477, Status=Todo, Priority=High
3. `update_project` — issue github/gh-aw#14478, Status=Todo, Priority=High
4. `update_project` — draft_issue using temporary-id from step 1, Status=In Progress
5. `update_project` — pull_request github/gh-aw#14477, Status=In Progress
6. `update_project` — issue github/gh-aw#14478, Status=In Progress
7. `create_project_status_update` — post a markdown pass/fail checklist covering all 6 operations above
{{else}}
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
      - `draft_issue_id`: The temporary-id returned from step 1a (e.g., "aw_abc123")
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
{{/if}}