---
description: Safe-output reference for update, label, milestone, project, release, and upload operations.
---

# Safe Outputs: Management and Delivery

- `update-issue:` - Update issue title, body, labels, assignees, or milestone (NOT for closing - use close-issue instead)

  ```yaml
  safe-outputs:
    update-issue:
      status: true                    # Optional: allow updating issue status (open/closed)
      target: "*"                     # Optional: target for updates (default: "triggering")
      title: true                     # Optional: allow updating issue title
      body: true                      # Optional: allow updating issue body
      required-labels: [approved]     # Optional: ALL of these labels must be present on the issue for the update to run
      max: 3                          # Optional: maximum number of issues to update (default: 1)
      target-repo: "owner/repo"       # Optional: cross-repository
  ```

  **Note:** `update-issue` can change status between 'open'/'closed', but use `close-issue` to close with a comment. Use `update-issue` for title, body, labels, assignees, or milestone changes without closing.
- `update-pull-request:` - Update PR title or body

  ```yaml
  safe-outputs:
    update-pull-request:
      title: true                     # Optional: enable title updates (default: true)
      body: true                      # Optional: enable body updates (default: true)
      operation: "replace"            # Optional: "replace" (default), "append", "prepend"
      update-branch: false            # Optional: update PR branch with latest base before updates (default: false)
      sync-stack: true                # Optional: allow stacked-PR stack-sync fallback when update-branch is unsupported (default: true)
      max: 1                          # Optional: max updates (default: 1)
      target: "*"                     # Optional: "triggering" (default), "*", or number
      target-repo: "owner/repo"       # Optional: cross-repository
  ```

  Operation types: `replace` (default), `append`, `prepend`.
- `merge-pull-request:` - Merge pull requests under configured policy gates (experimental)

  ```yaml
  safe-outputs:
    merge-pull-request:
      required-labels: [ready-to-merge]   # Optional: ALL listed labels must be present on the PR
      required-title-prefix: "[bot] "     # Optional: only merge PRs with this title prefix
      allowed-branches: ["feature/*"]    # Optional: glob patterns for allowed source branch names
      target: "triggering"                # Optional: "triggering" (default, current PR) or "*" (any PR with pull_request_number)
      target-repo: "owner/repo"           # Optional: cross-repository
      allowed-repos: [owner/other]        # Optional: additional repos the agent can merge in
      max: 1                              # Optional: max merges (default: 1)
  ```

  **⚠️ Experimental**: Compilation emits a warning when this feature is used. The merge is blocked unless all configured gates pass.

- `close-pull-request:` - Safe pull request closing with filtering

  ```yaml
  safe-outputs:
    close-pull-request:
      required-labels: [test, automated]  # Optional: only close PRs with these labels
      required-title-prefix: "[bot]"      # Optional: only close PRs with this title prefix
      allow-body: false                   # Optional: when false, any body the agent emits is dropped (warning logged) and the PR closes without a comment; defaults to true
      target: "triggering"                # Optional: "triggering" (default), "*" (any PR), or explicit PR number
      max: 10                             # Optional: maximum number of PRs to close (default: 1)
      target-repo: "owner/repo"           # Optional: cross-repository
      github-token: ${{ secrets.CUSTOM_TOKEN }}  # Optional: custom token
  ```

- `mark-pull-request-as-ready-for-review:` - Mark draft PRs as ready for review

  ```yaml
  safe-outputs:
    mark-pull-request-as-ready-for-review:
      max: 1                              # Optional: max operations (default: 1)
      target: "*"                         # Optional: "triggering" (default), "*", or number
      required-labels: [automated]        # Optional: only mark PRs with these labels
      required-title-prefix: "[bot]"      # Optional: only mark PRs with this prefix
      target-repo: "owner/repo"           # Optional: cross-repository
  ```

- `add-labels:` - Safe label addition to issues or PRs

  ```yaml
  safe-outputs:
    add-labels:
      allowed: [bug, enhancement, documentation]  # Optional: restrict to specific labels
      blocked: ["~*", "*[bot]"]                   # Optional: blocked label patterns (glob; takes precedence over allowed)
      required-labels: [approved]                 # Optional: ALL of these labels must be present on the issue/PR for the operation to run
      required-title-prefix: "[bot]"              # Optional: issue/PR title must start with this prefix
      issues: true                                # Optional: set false to exclude issues:write permission (default: true)
      pull-requests: true                         # Optional: set false to exclude pull-requests:write permission (default: true)
      max: 5                                      # Optional: maximum number of labels (default: 5)
      target: "*"                                 # Optional: "triggering" (default), "*" (any issue/PR), or number
      target-repo: "owner/repo"                   # Optional: cross-repository
  ```

- `remove-labels:` - Safe label removal from issues or PRs

  ```yaml
  safe-outputs:
    remove-labels:
      allowed: [automated, stale]  # Optional: restrict to specific labels
      blocked: ["~*", "*[bot]"]    # Optional: blocked label patterns (glob; takes precedence over allowed)
      required-labels: [approved]  # Optional: ALL of these labels must be present on the issue/PR for the operation to run
      required-title-prefix: "[bot]"  # Optional: issue/PR title must start with this prefix
      max: 5                       # Optional: maximum number of operations (default: 5)
      target: "*"                  # Optional: "triggering" (default), "*" (any issue/PR), or number
      target-repo: "owner/repo"    # Optional: cross-repository
  ```

  When `allowed` is omitted, any labels can be removed.
- `replace-label:` - Atomic label state transition — removes one label and adds another in a single GraphQL request, eliminating the race window of separate remove + add operations

  ```yaml
  safe-outputs:
    replace-label:
      allowed-add: [approved, done]            # Optional: glob patterns for labels that may be added (any allowed if omitted)
      allowed-remove: [in-review, pending]     # Optional: glob patterns for labels that may be removed (any allowed if omitted)
      blocked: ["~*", "*[bot]"]                # Optional: blocked label patterns (glob; applies to both add and remove)
      required-labels: [triage]                # Optional: ALL of these labels must be present on the issue/PR for the operation to run
      required-title-prefix: "[Bug]"           # Optional: issue/PR title must start with this prefix
      max: 5                                   # Optional: maximum number of replacements (default: 5)
      target: "triggering"                     # Optional: "triggering" (default), "*" (any issue/PR), or number
      target-repo: "owner/repo"                # Optional: cross-repository
  ```

  The agent calls `replace_label(label_to_remove, label_to_add)`. If the label to remove is not present on the item, only the add is applied (no failure). Labels that do not yet exist in the repository are auto-created with a deterministic pastel color.
- `add-reviewer:` - Add reviewers to pull requests

  ```yaml
  safe-outputs:
    add-reviewer:
      allowed-reviewers: [user1, copilot]     # Optional: restrict to specific reviewer usernames (any allowed if omitted)
      allowed-team-reviewers: [platform-team] # Optional: restrict to specific team slugs (any allowed if omitted)
      max: 3                                  # Optional: max reviewers (default: 3)
      target: "*"                             # Optional: "triggering" (default), "*", or number
      target-repo: "owner/repo"               # Optional: cross-repository
  ```

  At least one reviewer or team reviewer must be present in agent output. Use `allowed-reviewers: [copilot]` to assign Copilot PR reviewer bot. Requires PAT as `COPILOT_GITHUB_TOKEN`. The legacy `reviewers` / `team-reviewers` field names are deprecated aliases.
- `assign-milestone:` - Assign issues to milestones

  ```yaml
  safe-outputs:
    assign-milestone:
      allowed: [v1.0, v2.0]           # Optional: restrict to specific milestone titles
      auto_create: true               # Optional: auto-create milestones from the allowed list if missing (default: false)
      max: 1                          # Optional: max assignments (default: 1)
      target-repo: "owner/repo"       # Optional: cross-repository
  ```

- `link-sub-issue:` - Safe sub-issue linking

  ```yaml
  safe-outputs:
    link-sub-issue:
      parent-required-labels: [epic]     # Optional: parent must have these labels
      parent-title-prefix: "[Epic]"      # Optional: parent must match this prefix
      sub-required-labels: [task]        # Optional: sub-issue must have these labels
      sub-title-prefix: "[Task]"         # Optional: sub-issue must match this prefix
      max: 5                             # Optional: maximum number of links (default: 5)
      target-repo: "owner/repo"          # Optional: cross-repository
  ```

  Links issues via GitHub's parent-child relationships. Agent output includes `parent_issue_number` and `sub_issue_number`. Use with `create-issue` temporary IDs or existing issue numbers.
- `create-project:` - Create a new GitHub Project board with optional fields and views

  ```yaml
  safe-outputs:
    create-project:
      max: 1                          # Optional: max projects (default: 1)
      # github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}  # Optional: override default PAT (NOT GITHUB_TOKEN)
      target-owner: "org-or-user"     # Optional: owner for created projects
      title-prefix: "[ai] "           # Optional: prefix for project titles
  ```

  Optionally specify custom fields, project views, and an initial item. Requires PAT/App token with Projects permissions (`GH_AW_PROJECT_GITHUB_TOKEN`); `GITHUB_TOKEN` cannot access Projects v2 API. No cross-repository support.
- `update-project:` - Add items to GitHub Projects, update custom fields, manage project structure

  ```yaml
  safe-outputs:
    update-project:
      max: 20                         # Optional: max project operations (default: 10)
      project: "https://github.com/orgs/myorg/projects/42"  # REQUIRED in agent output (full URL)
      # github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}  # Optional here if GH_AW_PROJECT_GITHUB_TOKEN is set; PAT with projects:write (NOT GITHUB_TOKEN) is still required
  ```

  **⚠️**: Agent must include full project URL (not just number) in every call. Requires PAT/App token with Projects access (same as `create-project:`). Not supported for cross-repository operations.

  **Three calling modes:**

  **Mode 1: Add/update existing issues or PRs**

  ```json
  {
    "type": "update_project",
    "project": "https://github.com/orgs/myorg/projects/42",
    "content_type": "issue",
    "content_number": 123,
    "fields": {"Status": "In Progress", "Priority": "High"}
  }
  ```

  - `content_type`: "issue" or "pull_request"
  - `content_number`: The issue or PR number to add/update
  - `fields`: Custom field values to set on the item (optional)

  **Mode 2: Create draft issues in the project**

  ```json
  {
    "type": "update_project",
    "project": "https://github.com/orgs/myorg/projects/42",
    "content_type": "draft_issue",
    "draft_title": "Follow-up: investigate performance",
    "draft_body": "Check memory usage under load",
    "temporary_id": "aw_abc123def456",
    "fields": {"Status": "Backlog"}
  }
  ```

  - `content_type`: "draft_issue"
  - `draft_title`: Title of the draft issue (required when creating new)
  - `draft_body`: Description in markdown (optional)
  - `temporary_id`: Unique ID for this draft (format: `aw_` + 3-8 alphanumeric chars) for referencing in future updates (optional)
  - `draft_issue_id`: Reference an existing draft by its temporary_id to update it (optional)
  - `fields`: Custom field values (optional)

  **Mode 3: Create custom fields or views** (with `operation` field)

  ```json
  {
    "type": "update_project",
    "project": "https://github.com/orgs/myorg/projects/42",
    "operation": "create_fields",
    "field_definitions": [
      {"name": "Priority", "data_type": "SINGLE_SELECT", "options": ["High", "Medium", "Low"]},
      {"name": "Due Date", "data_type": "DATE"}
    ]
  }
  ```

  - `operation`: "create_fields" or "create_view"
  - `field_definitions`: Array of field definitions (for create_fields)
  - `view`: View configuration object with `name`, `layout` (table/board/roadmap), optional `filter` and `visible_fields` (for create_view)

  Not supported for cross-repository operations.
- `create-project-status-update:` - Post status updates to GitHub Projects for progress tracking

  ```yaml
  safe-outputs:
    create-project-status-update:
      max: 1                          # Optional: max status updates (default: 1)
      project: "https://github.com/orgs/myorg/projects/42"  # REQUIRED in agent output (full URL)
      github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}  # REQUIRED: PAT with projects:write (NOT GITHUB_TOKEN)
  ```

  Requires same PAT/App token as `update-project`. Agent must include full project URL in every call.

  **Agent output fields:**
  - `project`: Full project URL (required) - MUST be explicitly included in output
  - `status`: ON_TRACK, AT_RISK, OFF_TRACK, COMPLETE, or INACTIVE (optional, defaults to ON_TRACK)
  - `start_date`: Project start date in YYYY-MM-DD format (optional)
  - `target_date`: Project end date in YYYY-MM-DD format (optional)
  - `body`: Status summary in markdown (required)

  Not supported for cross-repository operations.
- `push-to-pull-request-branch:` - Push changes to PR branch

  ```yaml
  safe-outputs:
    push-to-pull-request-branch:
      target: "*"                     # Optional: "triggering" (default), "*", or number
      title-prefix: "[bot] "          # Optional: require title prefix
      required-labels: [automated]    # Optional: require all labels
      base-branch: "main"             # Optional: base branch for incremental patch computation (defaults to resolving from checkout/repo default branch)
      target-repo: "owner/repo"       # Optional: cross-repository push target
      head-repo: "fork-owner/repo"    # Optional: head (fork) repository for cross-repository pushes; defaults to target-repo
      head-github-token: ${{ secrets.HEAD_REPO_PAT }}  # Optional: token for branch writes to head-repo when it differs from target-repo
      allowed-repos: [owner/other]    # Optional: additional repos the agent can target
      if-no-changes: "warn"           # Optional: "warn" (default), "error", or "ignore"
      ignore-missing-branch-failure: false  # Optional: treat deleted PR branches as skipped pushes (default: false)
      commit-title-suffix: "[auto]"   # Optional: suffix appended to commit title
      staged: true                    # Optional: preview mode (default: follows global staged)
      github-token-for-extra-empty-commit: ${{ secrets.MY_CI_PAT }}  # Optional: PAT or "app" to trigger CI on pushed commits
      fallback-as-pull-request: true  # Optional: when push fails (e.g. diverged branch), open a fallback PR targeting the original branch (default: true)
      patch-format: "bundle"          # Optional: "bundle" (default, supports merge commits) or "am"; auto-falls back to "bundle" when the incremental range contains a merge commit
      signed-commits: true            # Optional: when true (default), push via createCommitOnBranch GraphQL so GitHub signs commits; set false to push merge commits via plain git push
      allow-workflows: false          # Optional: add workflows:write permission for .github/workflows/ paths (requires github-app)
      check-branch-protection: true   # Optional: when true (default), pre-flight check branch protection; set false to skip and avoid administration:read permission
      allowed-files:                  # Recommended: always restrict to specific paths or extensions to limit agent scope
        - "src/**"
      excluded-files:                 # Optional: glob patterns to strip from the patch entirely
        - "**/*.lock"
      protected-files: request-review # Optional: "request-review" (default), "blocked", "fallback-to-issue", or "allowed"
      max-patch-size: 2048            # Optional: per-output cap on git patch size in KB (overrides global; default: 4096 KB, max: 10240)
  ```

  Cross-repository pushes are supported via `target-repo` (and `head-repo`/`head-github-token` for fork-backed PRs) plus an `allowed-repos` allowlist. To trigger CI on pushed commits, use `github-token-for-extra-empty-commit` or set the magic secret `GH_AW_CI_TRIGGER_TOKEN`.

  **File Restrictions**: Same as `create-pull-request`: **always specify `allowed-files`** scoped to specific file extensions or paths to limit the agent's reach. `excluded-files` strips files before all checks, and `protected-files` controls handling of sensitive files. Object form supported: `protected-files: { policy: fallback-to-issue, exclude: [AGENTS.md] }`.

  `push-to-pull-request-branch` now uses the same `request-review` default as `create-pull-request`. Protected-file changes are still surfaced in the PR as a `REQUEST_CHANGES` review, while `blocked` remains available when a workflow requires a hard failure. `CHANGELOG.md` is excluded from the PR handlers' default protected-file set so routine release updates do not require an explicit exception; other shared handlers continue to protect it.

  **Compile-time warnings for `target: "*"`**: When `target: "*"` is set, the compiler emits warnings if:
  1. The checkout configuration does not include a wildcard fetch pattern — add `fetch: ["*"]` with `fetch-depth: 0` so the agent can access all PR branches at runtime
  2. No constraints are provided — add `title-prefix` or `required-labels` to restrict which PRs can receive pushes

  Example with all recommended settings:

  ```yaml
  checkout:
    fetch: ["*"]
    fetch-depth: 0
  safe-outputs:
    push-to-pull-request-branch:
      target: "*"
      required-title-prefix: "[bot] "   # restrict to PRs with this title prefix
