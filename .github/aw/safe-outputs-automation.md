---
description: Safe-output reference for workflow dispatch, code scanning, checks, agent sessions, and assignment operations.
---

# Safe Outputs: Automation and Orchestration

- `update-discussion:` - Update discussion title, body, or labels

  ```yaml
  safe-outputs:
    update-discussion:
      title: true                     # Optional: enable title updates
      body: true                      # Optional: enable body updates
      labels: true                    # Optional: enable label updates
      allowed-labels: [status, type]  # Optional: restrict to specific labels
      max: 1                          # Optional: max updates (default: 1)
      target: "*"                     # Optional: "triggering" (default), "*", or number
      target-repo: "owner/repo"       # Optional: cross-repository
  ```

- `update-release:` - Update GitHub release descriptions. Agent-generated release notes and release-description changes must use this safe output; never perform those mutations directly with `gh`, the GitHub API, or a GitHub write tool.

  ```yaml
  safe-outputs:
    update-release:
      max: 1                          # Optional: max releases (default: 1, max: 10)
      target-repo: "owner/repo"       # Optional: cross-repository
      github-token: ${{ secrets.GH_AW_UPDATE_RELEASE_TOKEN }}  # Optional: custom token
  ```

  Operation types: `replace`, `append`, `prepend`.
- `upload-asset:` - Publish files to orphaned git branch (recommended for images/charts/screenshots)

  ```yaml
  safe-outputs:
    upload-asset:
      branch: "assets/${{ github.workflow }}"  # Optional: branch name
      max-size: 10240                 # Optional: max file size in KB (default: 10MB)
      allowed-exts: [.png, .jpg, .pdf] # Optional: allowed file extensions
      max: 10                         # Optional: max assets (default: 10)
  ```

  Default allowed extensions are common non-executable types; default max file size is 10MB (10240 KB), configurable via `max-size`. **Use this for images, charts, and screenshots that need embeddable URLs in issues/PRs/discussions.**
- `upload-artifact:` - Upload files as run-scoped GitHub Actions artifacts (recommended for temporary run artifacts and attachment-style outputs)

  ```yaml
  safe-outputs:
    upload-artifact:
      max-uploads: 5                  # Optional: max upload_artifact tool calls (default: 1, max: 20)
      retention-days: 7               # Optional: fixed retention period in days (agent cannot override; 1-90; templatable expression supported)
      skip-archive: false             # Optional: fixed skip-archive flag (templatable expression supported); single-file only
      max-size-bytes: 104857600       # Optional: max bytes per upload (default: 100 MB)
      allowed-paths:                  # Optional: glob patterns restricting uploadable paths
        - "reports/**"
        - "*.json"
      filters:                        # Optional: default include/exclude glob filters
        include: ["*.json", "*.csv"]
        exclude: ["*secret*"]
      defaults:                       # Optional: default values injected when agent omits a field
        if-no-files: "ignore"         # "error" or "ignore" when no files match (default: "error")
  ```

  Artifacts are run-scoped and auto-cleaned when they expire. Agents call `upload_artifact` with a `name` and `path`. `retention-days` and `skip-archive` are fixed at the workflow level (templatable via expressions); the agent cannot override them. **Use this for temporary downloadable artifacts and attachment-style arbitrary data** (e.g. a comment/issue linking to a generated file bundle). Set `skip-archive: true` to serve downloads as direct files without uncompressing. Use `upload-asset` instead when you need stable embeddable URLs (images/charts in GitHub content).
- `dispatch-workflow:` - Trigger other workflows with inputs

  ```yaml
  safe-outputs:
    dispatch-workflow:
      workflows: [workflow-name]          # Required: list of workflow names to allow
      max: 3                              # Optional: max dispatches (default: 1, max: 50)
      target-repo: org/other-repo         # Optional: cross-repo dispatch target (owner/repo or expression)
      allowed-repos: [org/*]              # Optional: allowlist for cross-repo dispatch targets
      target-ref: main                    # Optional: ref to dispatch against (overrides caller's GITHUB_REF)
      allowed-refs: ["release/*"]         # Optional: glob allowlist for agent-provided per-call ref overrides (default: caller's ref only)
  ```

  Triggers other agentic workflows using workflow_dispatch. Agent output includes `workflow_name` (without .md extension) and optional `inputs` (key-value pairs). Cross-repo dispatch is supported via `target-repo` plus an `allowed-repos` allowlist; cross-repo targets require a token with `actions: write` on the target repository.
- `dispatch-repository:` - Dispatch `repository_dispatch` events to external repositories (experimental)

  ```yaml
  safe-outputs:
    dispatch-repository:
      trigger_ci:                              # Tool name (normalized to MCP tool: trigger_ci)
        description: "Trigger CI in target repo"
        workflow: ci.yml                       # Required: target workflow name (for traceability)
        event_type: ci_trigger                 # Required: repository_dispatch event_type
        repository: org/target-repo           # Required: target repo (or use allowed_repositories)
        # allowed_repositories:               # Alternative: allow multiple target repos
        #   - org/repo1
        #   - org/repo2
        inputs:                               # Optional: input schema for agent
          environment:
            type: string
            description: "Deployment environment"
            required: true
        max: 1                                # Optional: max dispatches (templatable)
        github-token: ${{ secrets.MY_PAT }}   # Optional: override token
        staged: false                         # Optional: preview-only mode
  ```

  Accepts both `dispatch-repository` (dash, canonical) and `dispatch_repository` (underscore, deprecated alias). Each key in the config defines a named MCP tool. Requires a token with `repo` scope since `GITHUB_TOKEN` cannot trigger `repository_dispatch` in external repositories. Use `github-token` or set a PAT as `GH_AW_SAFE_OUTPUTS_TOKEN`.

  **⚠️ Experimental**: Compilation emits a warning when this feature is used.
- `call-workflow:` - Call reusable workflows via workflow_call fan-out (orchestrator pattern)

  ```yaml
  safe-outputs:
    call-workflow:
      workflows: [worker-a, worker-b]     # Required: workflow names (without .md) with workflow_call trigger
      max: 1                              # Optional: max calls per run (default: 1, max: 50)
      github-token: ${{ secrets.TOKEN }}  # Optional: token passed to called workflows
  ```

  Array shorthand: `call-workflow: [worker-a, worker-b]`

  Unlike `dispatch-workflow` (which uses the GitHub Actions API at runtime), `call-workflow` generates static conditional `uses:` jobs at compile time. The agent selects which worker to activate; the compiler validates and wires up all fan-out jobs. Each listed workflow must exist in `.github/workflows/` and declare a `workflow_call` trigger. Use this for orchestrator/dispatcher patterns within the same repository.
- `create-code-scanning-alert:` - Generate SARIF security advisories

  ```yaml
  safe-outputs:
    create-code-scanning-alert:
      max: 50                         # Optional: max findings (default: unlimited)
      driver: "Custom Scanner"        # Optional: SARIF tool.driver.name (default: "GitHub Agentic Workflows Security Scanner")
      github-token: ${{ secrets.MY_TOKEN }}  # Optional: override token for security-events:write
      target-repo: "owner/repo"       # Optional: cross-repository
      allowed-repos: [owner/other]    # Optional: additional repos the agent may target via `repo` field
  ```

  Severity levels: error, warning, info, note.
- `autofix-code-scanning-alert:` - Add autofixes to code scanning alerts

  ```yaml
  safe-outputs:
    autofix-code-scanning-alert:
      max: 10                         # Optional: max autofixes (default: 10)
  ```

  Provides automated fixes for code scanning alerts.
- `create-check-run:` - Create GitHub Check Runs to surface agent analysis results in the PR Checks UI

  ```yaml
  safe-outputs:
    create-check-run:
      name: "Security Analysis"       # Optional: check run name (defaults to workflow name)
      target: "triggering"            # Optional: "triggering" (default), "*" (any PR), or explicit PR number
      max: 1                          # Optional: max check runs per workflow run (default: 1)
      output:                         # Optional: static fallback values used when the agent omits the field
        title: "Pending analysis"     # Fallback title (max 256 chars)
        summary: "Awaiting agent output"  # Fallback summary (max 65535 chars)
  ```

  Requires `checks: write` (added automatically). Agents call `create_check_run` with `conclusion` (e.g., `success`, `failure`, `neutral`), `title`, `summary`, and optional `annotations`. Reports structured results (security findings, code quality, test outcomes) directly on commits and PRs.

- `create-agent-session:` - Create GitHub Copilot coding agent sessions

  ```yaml
  safe-outputs:
    create-agent-session:
      base: main                      # Optional: base branch (defaults to current)
      target-repo: "owner/repo"       # Optional: cross-repository
  ```

  Requires PAT as `COPILOT_GITHUB_TOKEN`.
- `assign-to-agent:` - Assign Copilot coding agent to issues

  ```yaml
  safe-outputs:
    assign-to-agent:
      name: "copilot"                 # Optional: agent name
      model: "claude-sonnet-4-5"      # Optional: model override
      custom-agent: "agent-id"        # Optional: custom agent ID
      custom-instructions: "..."      # Optional: additional instructions for the agent
      allowed: [copilot]              # Optional: restrict to specific agent names
      max: 1                          # Optional: max assignments (default: 1)
      target: "*"                     # Optional: "triggering" (default), "*", or number
      target-repo: "owner/repo"       # Optional: where the issue lives (cross-repository)
      pull-request-repo: "owner/repo" # Optional: where PR should be created (if different)
      allowed-pull-request-repos: [owner/repo1]  # Optional: additional repos for PR creation
      base-branch: "develop"          # Optional: target branch for PR (default: repo default)
      ignore-if-error: true           # Optional: continue workflow on assignment error (default: false)
  ```

  Requires PAT with elevated permissions as `GH_AW_AGENT_TOKEN`.
- `assign-to-user:` - Assign users to issues or pull requests

  ```yaml
  safe-outputs:
    assign-to-user:
      allowed: [user1, user2]         # Optional: restrict to specific users
      blocked: [copilot, "*[bot]"]    # Optional: deny specific users or glob patterns
      max: 1                          # Optional: max assignments (default: 1)
      target: "*"                     # Optional: "triggering" (default), "*", or number
      target-repo: "owner/repo"       # Optional: cross-repository
      unassign-first: true            # Optional: unassign all current assignees first (default: false)
  ```

- `unassign-from-user:` - Remove user assignments from issues or PRs

  ```yaml
  safe-outputs:
    unassign-from-user:
      allowed: [user1, user2]         # Optional: restrict to specific users
      blocked: [copilot, "*[bot]"]    # Optional: deny specific users or glob patterns
      max: 1                          # Optional: max unassignments (default: 1)
      target: "*"                     # Optional: "triggering" (default), "*", or number
      target-repo: "owner/repo"       # Optional: cross-repository
  ```

- `hide-comment:` - Hide comments on issues, PRs, or discussions

  ```yaml
  safe-outputs:
    hide-comment:
      max: 5                          # Optional: max comments to hide (default: 5)
      allowed-reasons:                 # Optional: restrict hide reasons
        - spam
        - outdated
        - resolved
      target-repo: "owner/repo"       # Optional: cross-repository
      discussions: true               # Optional: opt-in to discussions:write permission for hiding discussion comments (default: false)
  ```

  Allowed reasons: `spam`, `abuse`, `off_topic`, `outdated`, `resolved`, `low_quality`.
- `set-issue-type:` - Set the type of an issue (requires organization-defined issue types)

  ```yaml
  safe-outputs:
    set-issue-type:
      allowed: [Bug, Feature, Enhancement]  # Optional: restrict to specific issue type names
      target: "triggering"                  # Optional: "triggering" (default), "*", or number
      max: 5                                # Optional: max operations (default: 5)
      target-repo: "owner/repo"             # Optional: cross-repository
  ```

  Set `allowed` to an empty string `""` to allow clearing the issue type. When `allowed` is omitted, any type name is accepted.
- `set-issue-field:` - Set a single issue field value by name/value (avoids the broader update-issue path)

  ```yaml
  safe-outputs:
    set-issue-field:
      allowed-fields: [Priority, Iteration]  # Optional: restrict which issue fields the agent may set (omit/empty = any field; ["*"] explicitly allows all)
      target: "triggering"                    # Optional: "triggering" (default), "*", or number
      max: 5                                  # Optional: max operations (default: 5)
      target-repo: "owner/repo"               # Optional: cross-repository
      allowed-repos: [owner/other]            # Optional: additional repos agent can target
  ```

  Agent calls `set_issue_field` with `value` plus either `field_name` (preferred) or `field_node_id`. `issue_number` is optional and defaults to the triggering issue.
- `approve-workflow-run:` - Approve a pending workflow run in the "action required" state

  ```yaml
  safe-outputs:
    approve-workflow-run:
      allowed-workflows: [ci.yml]     # Required: workflow filenames eligible for approval (no paths)
      allowed-repos: [org/fork]       # Optional: fork repositories allowed for approval (default: current repository only)
      allowed-pull-requests: ["123"]  # Optional: restrict to specific PR numbers
      protected-files: blocked        # Optional: "blocked" (default), "fallback-to-issue", or "allowed"
      github-token: ${{ secrets.APPROVE_WORKFLOW_RUN_TOKEN }}  # Required: external token/app (github.token cannot approve runs requiring approval)
  ```

  Requires `actions: write` (added automatically) plus an external `github-token` or `github-app` — the default `github.token` is not permitted to approve workflow runs requiring approval.
- `noop:` - Log completion message for transparency (auto-enabled)

  ```yaml
  safe-outputs:
    noop:
      report-as-issue: false          # Optional: report noop as issue (default: true)
  ```

  Fallback ensuring workflows never complete silently. Agents emit human-visible messages even when no other action is required (e.g., "Analysis complete - no issues found").
- `missing-tool:` - Report missing tools or functionality (auto-enabled)

  ```yaml
  safe-outputs:
    missing-tool:
      create-issue: true              # Optional: create issues for missing tools (default: false when this block is set; auto-enabled as true only when `missing-tool` is omitted)
      report-as-failure: true         # Optional: classify the run as an agent failure (default: true)
      title-prefix: "[missing tool]"  # Optional: prefix for issue titles
      labels: [tool-request]          # Optional: labels for created issues
  ```

  Lets agents report tools or functionality they need but lack; tracks feature requests. When `create-issue` is true, reports create or update GitHub issues.
- `missing-data:` - Report missing data required to complete tasks (auto-enabled)

  ```yaml
  safe-outputs:
    missing-data:
      create-issue: true              # Optional: create issues for missing data (default: false when this block is set; auto-enabled as true only when `missing-data` is omitted)
      report-as-failure: true         # Optional: classify the run as an agent failure (default: true)
      title-prefix: "[missing data]"  # Optional: prefix for issue titles
      labels: [data-request]          # Optional: labels for created issues
  ```

  Lets agents report when required data or information is unavailable. When `create-issue` is true, reports create or update GitHub issues for tracking.

- `report-incomplete:` - Signal that the task could not be completed due to an infrastructure or tool failure (auto-enabled)

  ```yaml
  safe-outputs:
    report-incomplete:
      create-issue: true              # Optional: create issues for incomplete tasks (default: true)
      title-prefix: "[incomplete]"    # Optional: prefix for issue titles
      labels: [agent-failure]         # Optional: labels for created issues
  ```
