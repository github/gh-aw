---
title: Frontmatter Reference
description: Complete JSON Schema-based reference for all GitHub Agentic Workflows frontmatter configuration options with YAML examples.
sidebar:
  order: 201
---

This document provides a comprehensive reference for all available frontmatter configuration options in GitHub Agentic Workflows. The examples below are generated from the JSON Schema and include inline comments describing each field.

> [!NOTE]
> This documentation is automatically generated from the JSON Schema. For a more user-friendly guide, see [Frontmatter](/gh-aw/reference/frontmatter/).

## Schema Description

JSON Schema for validating agentic workflow frontmatter configuration

## Complete Frontmatter Reference

```yaml wrap
---
# Workflow name that appears in the GitHub Actions interface. If not specified,
# defaults to the filename without extension.
# (optional)
name: "My Workflow"

# Optional workflow description that is rendered as a comment in the generated
# GitHub Actions YAML file (.lock.yml)
# (optional)
description: "Description of the workflow"

# Optional statement of the durable outcome the workflow exists to achieve. Unlike
# 'description', which explains what the workflow does, 'intent' explains why the
# workflow exists and should stay implementation-independent. Rendered as a
# comment in the generated GitHub Actions YAML file (.lock.yml).
# (optional)
intent: "example-value"

# Optional emoji to represent the workflow visually in listings and UI surfaces.
# (optional)
emoji: "example-value"

# Optional source reference indicating where this workflow was added from. Format:
# owner/repo/path@ref (e.g., githubnext/agentics/workflows/ci-doctor.md@v1.0.0).
# Rendered as a comment in the generated lock file.
# (optional)
source: "example-value"

# Optional workflow location redirect for updates. Format: workflow spec or GitHub
# URL (e.g., owner/repo/path@ref or
# https://github.com/owner/repo/blob/main/path.md). When present, update follows
# this location and rewrites source.
# (optional)
redirect: "example-value"

# Optional tracker identifier to tag all created assets (issues, discussions,
# comments, pull requests). Must be at least 8 characters and contain only
# alphanumeric characters, hyphens, and underscores. This identifier will be
# inserted in the body/description of all created assets to enable searching and
# retrieving assets associated with this workflow.
# (optional)
tracker-id: "example-value"

# Auditable false-positive suppression annotations for compiler threat-detection
# rules.
# (optional)
threat-detection-suppress: []
  # Array items:
    rule: "example-value"

    reason: "example-value"

    # (optional)
    expires: "example-value"

# Optional array of labels to categorize and organize workflows. Labels can be
# used to filter workflows in status/list commands.
# (optional)
labels: []
  # Array of strings

# Optional list of skill references to install during activation. Supports remote
# repository-wide installs (`owner/repo@<sha>`), remote path-scoped installs
# (`owner/repo/skill/path@<sha>`), and local path references (e.g. `skills/rig` or
# `.github/skills/my-skill`). Remote static references may use a full 40-character
# lowercase commit SHA, or a branch/tag name (resolved to a SHA at compile time).
# Omitting the ref (`owner/repo@`) opts out of pinning and triggers a compile-time
# warning. Local paths are installed with --from-local at runtime and are
# rewritten to a remote repospec by `gh aw add`. GitHub Actions expressions (`${{
# ... }}`) are also accepted and are evaluated at runtime. Entries may also be
# objects to configure per-skill authentication via github-token or github-app.
# (optional)
skills: []

# Optional metadata field for storing custom key-value pairs compatible with the
# custom agent spec. Key names are limited to 64 characters, and values are
# limited to 1024 characters.
# (optional)
metadata:
  # Optional absolute HTTPS URL for human-facing workflow documentation. Preserved
  # in generated lock-file metadata without being fetched during compilation.
  # (optional)
  docs: "https://docs.example.com/automation/repository-health"

# Workflow specifications to import. Supports array form (list of paths) or object
# form with 'aw' (agentic workflow paths) subfield. Path resolution: (1) relative
# paths (e.g., 'shared/file.md') are resolved relative to the workflow's
# directory; (2) paths starting with '.github/' or '/' are resolved from the
# repository root (repo-root-relative); (3) paths matching 'owner/repo/path@ref'
# are fetched from GitHub at compile time (cross-repo).
# (optional)
# Accepted formats:

# Format 1: Array of workflow specifications to import. Three path formats are
# supported: relative paths ('shared/file.md'), repo-root-relative paths
# ('.github/agents/my-agent.md'), and cross-repo paths ('owner/repo/path@ref').
# Any markdown files under .github/agents directory are treated as custom agent
# files and only one agent file is allowed per workflow.
imports: []
  # Array items: undefined

# Format 2: Object form of imports with 'aw' subfield for shared agentic workflow
# paths.
imports:
  # Array of shared agentic workflow specifications to import. Format:
  # owner/repo/path@ref or relative paths.
  # (optional)
  aw: []

# Optional list of additional workflow or action files that should be fetched
# alongside this workflow when running 'gh aw add'. Entries are relative paths
# (from the same directory as this workflow in the source repository) to agentic
# workflow .md files or GitHub Actions .yml/.yaml files. GitHub Actions expression
# syntax (${{) is not allowed in resource paths.
# (optional)
resources: []
  # Array of Relative path to a workflow .md file or action .yml/.yaml file. Must be
  # a static path; GitHub Actions expression syntax (${{) is not allowed.

# If true, inline all imports (including those without inputs) at compilation time
# in the generated lock.yml instead of using runtime-import macros. When enabled,
# the frontmatter hash covers the entire markdown body so any change to the
# content will invalidate the hash.
# (optional)
inlined-imports: true

# Workspace-relative folders to bundle in the activation artifact and restore
# before the agent runs. Useful for activation steps that generate reusable
# prompt, skill, or agent context.
# (optional)
ambient-folders: []
  # Array of strings

# Workflow triggers that define when the agentic workflow should run. Supports
# standard GitHub Actions trigger events plus special command triggers for
# /commands (required)
# Accepted formats:

# Format 1: Simple trigger event name (e.g., 'push', 'issues', 'pull_request',
# 'discussion', 'schedule', 'fork', 'create', 'delete', 'public', 'watch',
# 'workflow_call'), schedule shorthand (e.g., 'daily', 'weekly'), or slash command
# shorthand (e.g., '/my-bot' expands to slash_command + workflow_dispatch)
on: "example-value"

# Format 2: Complex trigger configuration with event-specific filters and options
on:
  # Special slash command trigger for /command workflows (e.g., '/my-bot' in issue
  # comments). Creates conditions to match slash commands automatically. Note: Can
  # be combined with issues/pull_request events if those events only use 'labeled'
  # or 'unlabeled' types.
  # (optional)
  # Accepted formats:

  # Format 1: Null command configuration - defaults to using the workflow filename
  # (without .md extension) as the command name
  slash_command: null

  # Format 2: Command name as a string (shorthand format, e.g., 'customname' for
  # '/customname' triggers). Command names must not start with '/'. Use '*' to match
  # any slash command, or append '*' as a suffix to enable wildcard prefix matching
  # (for example, 'smoke*' matches '/smoke-copilot').
  slash_command: "example-value"

  # Format 3: Command configuration object with custom command name
  slash_command:
    # Name of the slash command that triggers the workflow (e.g., '/help',
    # '/analyze'). Used for comment-based workflow activation.
    # (optional)
    # Accepted formats:

    # Format 1: Single command name for slash commands (e.g., 'helper-bot' for
    # '/helper-bot' triggers). Command names must not start with '/'. Use '*' to match
    # any slash command, or append '*' as a suffix to enable wildcard prefix matching.
    # Defaults to workflow filename without .md extension if not specified.
    name: "My Workflow"

    # Format 2: Array of command names that trigger this workflow (e.g., ['cmd.add',
    # 'cmd.remove'] for '/cmd.add' and '/cmd.remove' triggers). Each command name must
    # not start with '/'.
    name: []
      # Array items: Command name without leading slash. Use '*' to match any slash
      # command, or append '*' as a suffix to enable wildcard prefix matching.

    # Events where the command should be active. Default is all comment-related events
    # ('*'). Use GitHub Actions event names.
    # (optional)
    # Accepted formats:

    # Format 1: Single event name or '*' for all events. Use GitHub Actions event
    # names: 'issues', 'issue_comment', 'pull_request_comment', 'pull_request',
    # 'pull_request_review_comment', 'discussion', 'discussion_comment'.
    events: "*"

    # Format 2: Array of event names where the command should be active (requires at
    # least one). Use GitHub Actions event names.
    events: []
      # Array items: GitHub Actions event name.

    # Slash command trigger compilation strategy. 'inline' (default) compiles direct
    # comment listeners in this workflow. 'centralized' compiles this workflow as
    # workflow_dispatch-centric and routes slash events via the generated central
    # trigger workflow.
    # (optional)
    strategy: "inline"

  # DEPRECATED: Use 'slash_command' instead. Special command trigger for /command
  # workflows (e.g., '/my-bot' in issue comments). Creates conditions to match slash
  # commands automatically.
  # (optional)
  # Accepted formats:

  # Format 1: Null command configuration - defaults to using the workflow filename
  # (without .md extension) as the command name
  command: null

  # Format 2: Command name as a string (shorthand format, e.g., 'customname' for
  # '/customname' triggers). Command names must not start with '/'. Use '*' to match
  # any slash command, or append '*' as a suffix to enable wildcard prefix matching
  # (for example, 'smoke*' matches '/smoke-copilot').
  command: "example-value"

  # Format 3: Command configuration object with custom command name
  command:
    # Name of the slash command that triggers the workflow (e.g., '/deploy', '/test').
    # Used for command-based workflow activation.
    # (optional)
    # Accepted formats:

    # Format 1: Custom command name for slash commands (e.g., 'helper-bot' for
    # '/helper-bot' triggers). Command names must not start with '/'. Use '*' to match
    # any slash command, or append '*' as a suffix to enable wildcard prefix matching.
    # Defaults to workflow filename without .md extension if not specified.
    name: "My Workflow"

    # Format 2: Array of command names that trigger this workflow (e.g., ['cmd.add',
    # 'cmd.remove'] for '/cmd.add' and '/cmd.remove' triggers). Each command name must
    # not start with '/'.
    name: []
      # Array items: Command name without leading slash. Use '*' to match any slash
      # command, or append '*' as a suffix to enable wildcard prefix matching.

    # Events where the command should be active. Default is all comment-related events
    # ('*'). Use GitHub Actions event names.
    # (optional)
    # Accepted formats:

    # Format 1: Single event name or '*' for all events. Use GitHub Actions event
    # names: 'issues', 'issue_comment', 'pull_request_comment', 'pull_request',
    # 'pull_request_review_comment', 'discussion', 'discussion_comment'.
    events: "*"

    # Format 2: Array of event names where the command should be active (requires at
    # least one). Use GitHub Actions event names.
    events: []
      # Array items: GitHub Actions event name.

  # On Label Command trigger: fires when a specific label is added to an issue, pull
  # request, or discussion. The triggering label is automatically removed at
  # workflow start so it can be applied again to re-trigger. Use the 'events' field
  # to restrict which item types (issues, pull_request, discussion) activate the
  # trigger.
  # (optional)
  # Accepted formats:

  # Format 1: Label name as a string (shorthand format). The workflow fires when
  # this label is added to any supported item type (issue, pull request, or
  # discussion).
  label_command: "example-value"

  # Format 2: Label command configuration object with label name(s) and optional
  # event filtering.
  label_command:
    # Label name(s) that trigger the workflow when added to an issue, pull request, or
    # discussion.
    # (optional)
    # Accepted formats:

    # Format 1: Single label name that acts as a command (e.g., 'deploy' triggers the
    # workflow when the 'deploy' label is added).
    name: "My Workflow"

    # Format 2: Array of label names — any of these labels will trigger the workflow.
    name: []
      # Array items: A label name

    # Alternative to 'name': label name(s) that trigger the workflow.
    # (optional)
    # Accepted formats:

    # Format 1: Single label name.
    names: "example-value"

    # Format 2: Array of label names — any of these labels will trigger the workflow.
    names: []
      # Array items: A label name

    # Item types where the label-command trigger should be active. Default is all
    # supported types: issues, pull_request, discussion.
    # (optional)
    # Accepted formats:

    # Format 1: Single item type or '*' for all types.
    events: "*"

    # Format 2: Array of item types where the trigger is active.
    events: []
      # Array items: Item type.

    # Whether to automatically remove the triggering label after the workflow starts.
    # Defaults to true. Set to false to keep the label on the item and skip the
    # label-removal step. When false, the issues:write and discussions:write
    # permissions required for label removal are also omitted.
    # (optional)
    remove_label: true

    # Label command trigger compilation strategy. 'inline' (default) compiles direct
    # labeled listeners in this workflow. 'decentralized' compiles this workflow as
    # workflow_dispatch-centric and routes labeled events via the generated
    # agentic_commands.yml workflow.
    # (optional)
    strategy: "inline"

  # Push event trigger that runs the workflow when code is pushed to the repository
  # (optional)
  push:
    # Branches to filter on
    # (optional)
    branches: []
      # Array of strings

    # Branches to ignore
    # (optional)
    branches-ignore: []
      # Array of strings

    # Paths to filter on
    # (optional)
    paths: []
      # Array of strings

    # Paths to ignore
    # (optional)
    paths-ignore: []
      # Array of strings

    # List of git tag names or patterns to include for push events (supports
    # wildcards)
    # (optional)
    tags: []
      # Array of strings

    # List of git tag names or patterns to exclude from push events (supports
    # wildcards)
    # (optional)
    tags-ignore: []
      # Array of strings

  # Pull request event trigger that runs the workflow when pull requests are
  # created, updated, or closed
  # (optional)
  pull_request:
    # Pull request event types to trigger on. Note: 'converted_to_draft' and
    # 'ready_for_review' represent state transitions (events) rather than states.
    # While technically valid to listen for both, consider if you need to handle both
    # transitions or just one.
    # (optional)
    types: []
      # Array of strings

    # Branches to filter on
    # (optional)
    branches: []
      # Array of strings

    # Branches to ignore
    # (optional)
    branches-ignore: []
      # Array of strings

    # Paths to filter on
    # (optional)
    paths: []
      # Array of strings

    # Paths to ignore
    # (optional)
    paths-ignore: []
      # Array of strings

    # Filter by draft pull request state. Set to false to exclude draft PRs, true to
    # include only drafts, or omit to include both
    # (optional)
    draft: true

    # Maximum number of top stack layers to run on for stacked pull requests. Default
    # is 1 (only the latest/top pull request in the stack). Set to -1 to disable stack
    # protection and run on every pull request in the stack. Value 0 is not allowed.
    # (optional)
    # Accepted formats:

    # Format 1: Disable stack protection; run on every pull request in the stack.
    max-stack: 1

    # Format 2: Run only on the top N pull requests in the stack. Default is 1 (only
    # the latest/top pull request).
    max-stack: 1

    # When true, allows workflow to run on pull requests from forked repositories.
    # Security consideration: fork PRs have limited permissions.
    # (optional)
    # Accepted formats:

    # Format 1: Single fork pattern (e.g., '*' for all forks, 'org/*' for org glob,
    # 'org/repo' for exact match)
    forks: "example-value"

    # Format 2: List of allowed fork repositories with glob support (e.g., 'org/repo',
    # 'org/*', '*' for all forks)
    forks: []
      # Array items: Repository pattern with optional glob support

    # Array of pull request type names that trigger the workflow. Filters workflow
    # execution to specific PR categories.
    # (optional)
    # Accepted formats:

    # Format 1: Single label name to filter labeled/unlabeled events (e.g., 'bug')
    names: "example-value"

    # Format 2: List of label names to filter labeled/unlabeled events. Only applies
    # when 'labeled' or 'unlabeled' is in the types array
    names: []
      # Array items: Label name

  # Issues event trigger that runs when repository issues are created, updated, or
  # managed
  # (optional)
  issues:
    # Types of issue events
    # (optional)
    types: []
      # Array of strings

    # Array of issue type names that trigger the workflow. Filters workflow execution
    # to specific issue categories.
    # (optional)
    # Accepted formats:

    # Format 1: Single label name to filter labeled/unlabeled events (e.g., 'bug')
    names: "example-value"

    # Format 2: List of label names to filter labeled/unlabeled events. Only applies
    # when 'labeled' or 'unlabeled' is in the types array
    names: []
      # Array items: Label name

    # Whether to lock the issue for the agent when the workflow runs (prevents
    # concurrent modifications)
    # (optional)
    lock-for-agent: true

  # Issue comment event trigger
  # (optional)
  issue_comment:
    # Types of issue comment events
    # (optional)
    types: []
      # Array of strings

    # Whether to lock the parent issue for the agent when the workflow runs (prevents
    # concurrent modifications)
    # (optional)
    lock-for-agent: true

  # Discussion event trigger that runs the workflow when repository discussions are
  # created, updated, or managed
  # (optional)
  discussion:
    # Types of discussion events
    # (optional)
    types: []
      # Array of strings

  # Discussion comment event trigger that runs the workflow when comments on
  # discussions are created, updated, or deleted
  # (optional)
  discussion_comment:
    # Types of discussion comment events
    # (optional)
    types: []
      # Array of strings

  # Scheduled trigger events using fuzzy schedules or standard cron expressions.
  # Supports shorthand string notation (e.g., 'daily', 'daily around 2pm') or array
  # of schedule objects. Fuzzy schedules automatically distribute execution times to
  # prevent load spikes.
  # (optional)
  # Accepted formats:

  # Format 1: Shorthand schedule string using fuzzy or cron format. Examples:
  # 'daily', 'daily around 14:00', 'daily between 9:00 and 17:00', 'weekly', 'weekly
  # on monday', 'weekly on friday around 5pm', 'hourly', 'every 2h', 'every 10
  # minutes', '0 9 * * 1'. Fuzzy schedules distribute execution times to prevent
  # load spikes. For fixed times, use standard cron syntax. Minimum interval is 5
  # minutes.
  schedule: "example-value"

  # Format 2: Array of schedule objects with cron expressions (standard cron or
  # fuzzy format)
  schedule: []
    # Array items: object

  # Manual workflow dispatch trigger
  # (optional)
  # Accepted formats:

  # Format 1: Simple workflow dispatch trigger
  workflow_dispatch: null

  # Format 2: object
  workflow_dispatch:
    # Input parameters for manual dispatch
    # (optional)
    inputs:
      {}

  # Workflow run trigger
  # (optional)
  workflow_run:
    # List of workflows to trigger on
    # (optional)
    workflows: []
      # Array of strings

    # Types of workflow run events
    # (optional)
    types: []
      # Array of strings

    # Branches to filter on
    # (optional)
    branches: []
      # Array of strings

    # Branches to ignore
    # (optional)
    branches-ignore: []
      # Array of strings

    # Filter by workflow run conclusion (for example, failure). Compiled into a
    # guarded if: condition.
    # (optional)
    # Accepted formats:

    # Format 1: string
    conclusion: "success"

    # Format 2: array
    conclusion: []
      # Array items: string

  # Release event trigger
  # (optional)
  release:
    # Types of release events
    # (optional)
    types: []
      # Array of strings

  # Pull request review comment event trigger
  # (optional)
  pull_request_review_comment:
    # Types of pull request review comment events
    # (optional)
    types: []
      # Array of strings

  # Branch protection rule event trigger that runs when branch protection rules are
  # changed
  # (optional)
  branch_protection_rule:
    # Types of branch protection rule events
    # (optional)
    types: []
      # Array of strings

  # Check run event trigger that runs when a check run is created, rerequested,
  # completed, or has a requested action
  # (optional)
  check_run:
    # Types of check run events
    # (optional)
    types: []
      # Array of strings

  # Check suite event trigger that runs when check suite activity occurs
  # (optional)
  check_suite:
    # Types of check suite events
    # (optional)
    types: []
      # Array of strings

  # Create event trigger that runs when a Git reference (branch or tag) is created
  # (optional)
  # Accepted formats:

  # Format 1: Simple create event trigger
  create: null

  # Format 2: object
  create:
    {}

  # Delete event trigger that runs when a Git reference (branch or tag) is deleted
  # (optional)
  # Accepted formats:

  # Format 1: Simple delete event trigger
  delete: null

  # Format 2: object
  delete:
    {}

  # Deployment event trigger that runs when a deployment is created
  # (optional)
  # Accepted formats:

  # Format 1: Simple deployment event trigger
  deployment: null

  # Format 2: object
  deployment:
    {}

  # Deployment status event trigger that runs when a deployment status is updated
  # (optional)
  # Accepted formats:

  # Format 1: Simple deployment status event trigger
  deployment_status: null

  # Format 2: object
  deployment_status:
    # Filter to specific deployment states (compiled into if condition). Use a string
    # for one state or an array for multiple states.
    # (optional)
    # Accepted formats:

    # Format 1: string
    state: "error"

    # Format 2: array
    state: []
      # Array items: string

  # Fork event trigger that runs when someone forks the repository
  # (optional)
  # Accepted formats:

  # Format 1: Simple fork event trigger
  fork: null

  # Format 2: object
  fork:
    {}

  # Gollum event trigger that runs when someone creates or updates a Wiki page
  # (optional)
  # Accepted formats:

  # Format 1: Simple gollum event trigger
  gollum: null

  # Format 2: object
  gollum:
    {}

  # Label event trigger that runs when a label is created, edited, or deleted
  # (optional)
  label:
    # Types of label events
    # (optional)
    types: []
      # Array of strings

  # Merge group event trigger that runs when a pull request is added to a merge
  # queue
  # (optional)
  merge_group:
    # Types of merge group events
    # (optional)
    types: []
      # Array of strings

  # Milestone event trigger that runs when a milestone is created, closed, opened,
  # edited, or deleted
  # (optional)
  milestone:
    # Types of milestone events
    # (optional)
    types: []
      # Array of strings

  # Page build event trigger that runs when someone pushes to a GitHub Pages
  # publishing source branch
  # (optional)
  # Accepted formats:

  # Format 1: Simple page build event trigger
  page_build: null

  # Format 2: object
  page_build:
    {}

  # Public event trigger that runs when a repository changes from private to public
  # (optional)
  # Accepted formats:

  # Format 1: Simple public event trigger
  public: null

  # Format 2: object
  public:
    {}

  # Pull request target event trigger that runs in the context of the base
  # repository (secure for fork PRs)
  # (optional)
  pull_request_target:
    # List of pull request target event types to trigger on
    # (optional)
    types: []
      # Array of strings

    # Branches to filter on
    # (optional)
    branches: []
      # Array of strings

    # Branches to ignore
    # (optional)
    branches-ignore: []
      # Array of strings

    # Paths to filter on
    # (optional)
    paths: []
      # Array of strings

    # Paths to ignore
    # (optional)
    paths-ignore: []
      # Array of strings

    # Filter by draft pull request state
    # (optional)
    draft: true

    # When true, allows workflow to run on pull requests from forked repositories with
    # write permissions. Security consideration: use cautiously as fork PRs run with
    # base repository permissions.
    # (optional)
    # Accepted formats:

    # Format 1: Single fork pattern
    forks: "example-value"

    # Format 2: List of allowed fork repositories with glob support
    forks: []
      # Array items: string

  # Pull request review event trigger that runs when a pull request review is
  # submitted, edited, or dismissed
  # (optional)
  pull_request_review:
    # Types of pull request review events
    # (optional)
    types: []
      # Array of strings

    # Maximum number of top stack layers to run on for stacked pull request review
    # events. Default is 1 (only the latest/top pull request in the stack). Set to -1
    # to disable stack protection and run on every pull request review in the stack.
    # Value 0 is not allowed.
    # (optional)
    # Accepted formats:

    # Format 1: Disable stack protection; run on every pull request review in the
    # stack.
    max-stack: 1

    # Format 2: Run only on pull request reviews for the top N pull requests in the
    # stack. Default is 1 (only the latest/top pull request).
    max-stack: 1

  # Registry package event trigger that runs when a package is published or updated
  # (optional)
  registry_package:
    # Types of registry package events
    # (optional)
    types: []
      # Array of strings

  # Repository dispatch event trigger for custom webhook events
  # (optional)
  repository_dispatch:
    # Custom event types to trigger on
    # (optional)
    types: []
      # Array of strings

  # Status event trigger that runs when the status of a Git commit changes
  # (optional)
  # Accepted formats:

  # Format 1: Simple status event trigger
  status: null

  # Format 2: object
  status:
    {}

  # Watch event trigger that runs when someone stars the repository
  # (optional)
  watch:
    # Types of watch events
    # (optional)
    types: []
      # Array of strings

  # Workflow call event trigger that allows this workflow to be called by another
  # workflow
  # (optional)
  # Accepted formats:

  # Format 1: Simple workflow call event trigger
  workflow_call: null

  # Format 2: object
  workflow_call:
    # Input parameters that can be passed to the workflow when it is called
    # (optional)
    inputs:
      {}

    # Secrets that can be passed to the workflow when it is called
    # (optional)
    secrets:
      {}

  # Time when workflow should stop running. Supports multiple formats: absolute
  # dates (YYYY-MM-DD HH:MM:SS, June 1 2025, 1st June 2025, 06/01/2025, etc.),
  # relative time deltas (+25h, +3d, +1d12h30m), or a GitHub Actions expression
  # (e.g. ${{ inputs.stop-after }}) resolved at workflow runtime. Maximum values for
  # time deltas: 12mo, 52w, 365d, 8760h (365 days). Note: Minute unit 'm' is not
  # allowed for stop-after; minimum unit is hours 'h'.
  # (optional)
  stop-after: "example-value"

  # Conditionally skip workflow execution when a GitHub search query has matches.
  # Can be a string (query only, implies max=1) or an object with 'query', optional
  # 'max', and 'scope' fields. Use top-level on.github-token or on.github-app for
  # custom authentication.
  # (optional)
  # Accepted formats:

  # Format 1: GitHub search query string to check before running workflow (implies
  # max=1). If the search returns any results, the workflow will be skipped. Query
  # is automatically scoped to the current repository. Example: 'is:issue is:open
  # label:bug'
  skip-if-match: "example-value"

  # Format 2: Skip-if-match configuration object with query, maximum match count,
  # and optional scope. For custom authentication use the top-level on.github-token
  # or on.github-app fields.
  skip-if-match:
    # GitHub search query string to check before running workflow. Query is
    # automatically scoped to the current repository.
    query: "example-value"

    # Maximum number of items that must be matched for the workflow to be skipped.
    # Defaults to 1 if not specified. Supports integer or GitHub Actions expression
    # (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Scope for the search query. Set to 'none' to disable the automatic
    # 'repo:owner/repo' scoping, enabling org-wide or cross-repo queries.
    # (optional)
    scope: "none"

  # Conditionally skip workflow execution when a GitHub search query has no matches
  # (or fewer than minimum). Can be a string (query only, implies min=1) or an
  # object with 'query', optional 'min', and 'scope' fields. Use top-level
  # on.github-token or on.github-app for custom authentication.
  # (optional)
  # Accepted formats:

  # Format 1: GitHub search query string to check before running workflow (implies
  # min=1). If the search returns no results, the workflow will be skipped. Query is
  # automatically scoped to the current repository. Example: 'is:pr is:open
  # label:ready-to-deploy'
  skip-if-no-match: "example-value"

  # Format 2: Skip-if-no-match configuration object with query, minimum match count,
  # and optional scope. For custom authentication use the top-level on.github-token
  # or on.github-app fields.
  skip-if-no-match:
    # GitHub search query string to check before running workflow. Query is
    # automatically scoped to the current repository.
    query: "example-value"

    # Minimum number of items that must be matched for the workflow to proceed.
    # Defaults to 1 if not specified.
    # (optional)
    min: 1

    # Scope for the search query. Set to 'none' to disable the automatic
    # 'repo:owner/repo' scoping, enabling org-wide or cross-repo queries.
    # (optional)
    scope: "none"

  # Skip workflow execution if any CI checks on the target branch are failing or
  # pending. Accepts true (check all) or an object to filter specific checks by name
  # and optionally specify a branch or allow pending checks.
  # (optional)
  # Accepted formats:

  # Format 1: Bare key with no value — equivalent to true. Skips workflow execution
  # if any CI checks on the target branch are currently failing.
  skip-if-check-failing: null

  # Format 2: Skip workflow execution if any CI checks on the target branch are
  # currently failing. For pull_request events, checks the base branch. For other
  # events, checks the current ref.
  skip-if-check-failing: true

  # Format 3: Skip-if-check-failing configuration object with optional
  # include/exclude filter lists, an optional branch name, and an allow-pending
  # flag.
  skip-if-check-failing:
    # List of check names to evaluate. When specified, only these named checks are
    # considered. If omitted, all checks are evaluated.
    # (optional)
    include: []
      # Array of strings

    # List of check names to ignore. Checks in this list are not considered when
    # determining whether to skip the workflow.
    # (optional)
    exclude: []
      # Array of strings

    # Branch name to check for failing CI checks. When omitted, defaults to the base
    # branch of a pull_request event or the current ref for other events.
    # (optional)
    branch: "example-value"

    # When true, pending or in-progress checks are not treated as failing. By default
    # (false), any check that has not yet completed is treated as failing and will
    # block the workflow.
    # (optional)
    allow-pending: true

  # Skip workflow execution for users with specific repository roles. Useful for
  # workflows that should only run for external contributors or specific permission
  # levels.
  # (optional)
  # Accepted formats:

  # Format 1: Single role to skip workflow for (e.g., 'admin'). If the triggering
  # user has this role, the workflow will be skipped.
  skip-roles: "example-value"

  # Format 2: List of roles to skip workflow for (e.g., ['admin', 'maintainer',
  # 'write']). If the triggering user has any of these roles, the workflow will be
  # skipped.
  skip-roles: []
    # Array items: string

  # Skip workflow execution for specific GitHub users. Useful for preventing
  # workflows from running for specific accounts (e.g., bots, specific team
  # members).
  # (optional)
  # Accepted formats:

  # Format 1: Single GitHub username to skip workflow for (e.g., 'user1'). If the
  # triggering user matches, the workflow will be skipped.
  skip-bots: "example-value"

  # Format 2: List of GitHub usernames to skip workflow for (e.g., ['user1',
  # 'user2']). If the triggering user is in this list, the workflow will be skipped.
  skip-bots: []
    # Array items: string

  # Skip workflow execution when an event-specific payload author_association field
  # (for example: github.event.comment.author_association,
  # github.event.issue.author_association,
  # github.event.pull_request.author_association) matches configured associations
  # for specific events. Keys are event names (for example: issue_comment,
  # pull_request_review_comment, issues, pull_request). Values accept a single
  # string or an array of strings. Association values are case-insensitive in
  # frontmatter.
  # (optional)
  skip-author-associations:
    {}

  # Repository access roles required to trigger agentic workflows. Defaults to
  # ['admin', 'maintainer', 'write'] for security. Use 'all' to allow any
  # authenticated user (⚠️ security consideration).
  # (optional)
  # Accepted formats:

  # Format 1: Single repository permission level that can trigger the workflow. Use
  # 'all' to allow any authenticated user (⚠️ disables permission checking entirely
  # - use with caution)
  roles: "admin"

  # Format 2: List of repository permission levels that can trigger the workflow.
  # Permission checks are automatically applied to potentially unsafe triggers.
  roles: []
    # Array items: Repository permission level: 'admin' (full access),
    # 'maintainer'/'maintain' (repository management), 'write' (push access), 'triage'
    # (issue management), 'read' (read-only access)

  # Allow list of bot identifiers that can trigger the workflow even if they don't
  # meet the required role permissions. When the actor is in this list, the bot must
  # be active (installed) on the repository to trigger the workflow.
  # (optional)
  bots: []
    # Array of Bot identifier/name (e.g., 'dependabot[bot]', 'renovate[bot]',
    # 'github-actions[bot]')

  # Filter workflows triggered by pull_request_target (or other labeled events) to
  # only fire when the triggering label matches one of these names. Generates a
  # job-level if: condition on the pre-activation job so unmatched label events show
  # as Skipped (⊘) rather than Failed (❌).
  # (optional)
  # Accepted formats:

  # Format 1: Single label name that must match the triggering label (e.g.,
  # 'panel-review')
  labels: "example-value"

  # Format 2: List of label names; the workflow fires when the triggering label
  # matches any entry.
  labels: []
    # Array items: A non-empty string value.

  # Allow the bot-posted-menu / user-checks-box pattern: when a workflow posts a
  # checkbox-menu comment as a GitHub App bot and a human maintainer edits it to
  # tick a box (issue_comment:edited where actor ≠ comment.user.login), treat this
  # as safe and skip the confused-deputy check. When false (default), the check
  # applies to all issue_comment events. The Dependabot confused-deputy attack
  # vector (issue_comment:created) is unaffected.
  # (optional)
  allow-bot-authored-trigger-comment: true

  # Environment name that requires manual approval before the workflow can run. Must
  # match a valid environment configured in the repository settings.
  # (optional)
  manual-approval: "example-value"

  # Minimum time after the most recent completed workflow run where the agent job
  # started before another agent run may start. Uses Go duration syntax (for
  # example, '5m', '1h', or '1h30m'), must be at least 5 minutes, and does not
  # support GitHub Actions expressions.
  # (optional)
  cooldown: "example-value"

  # AI reaction to add/remove on triggering item. Scalar form accepts one of: +1,
  # -1, laugh, confused, heart, hooray, rocket, eyes, none. Object form implies
  # enabled reactions and supports optional `issues`, `pull-requests`, and
  # `discussions` fields to control trigger groups independently; use `type` to
  # choose the reaction emoji (defaults to `eyes` when omitted). Use 'none' to
  # disable reactions.
  # (optional)
  # Accepted formats:

  # Format 1: string
  reaction: "+1"

  # Format 2: YAML parses +1 and -1 without quotes as integers. These are converted
  # to +1 and -1 strings respectively.
  reaction: 1

  # Format 3: object
  reaction:
    # Reaction type. Defaults to 'eyes' when omitted.
    # (optional)
    # Accepted formats:

    # Format 1: string
    type: "+1"

    # Format 2: YAML parses +1 and -1 without quotes as integers. These are converted
    # to +1 and -1 strings respectively.
    type: 1

    # Whether reactions are allowed for issue triggers (issues, issue_comment).
    # (optional)
    issues: true

    # Whether reactions are allowed for pull request triggers (pull_request,
    # pull_request_review_comment).
    # (optional)
    pull-requests: true

    # Whether reactions are allowed for discussion and discussion_comment triggers.
    # (optional)
    discussions: true

  # Whether to post status comments (started/completed) on the triggering item.
  # Boolean form enables/disables status comments globally. Object form implies
  # enabled status comments and supports optional `issues`, `pull-requests`, and
  # `discussions` fields to control trigger groups independently. Automatically
  # enabled for slash_command and label_command triggers when not explicitly
  # configured.
  # (optional)
  # Accepted formats:

  # Format 1: boolean
  status-comment: true

  # Format 2: object
  status-comment:
    # Whether status comments are allowed for issue triggers (issues, issue_comment).
    # (optional)
    issues: true

    # Whether status comments are allowed for pull request triggers (pull_request,
    # pull_request_review_comment).
    # (optional)
    pull-requests: true

    # Whether status comments are allowed for discussion and discussion_comment
    # triggers.
    # (optional)
    discussions: true

  # Custom GitHub token for pre-activation reactions, activation status comments,
  # and skip-if search queries. When specified, overrides the default GITHUB_TOKEN
  # for these operations.
  # (optional)
  github-token: "${{ secrets.GITHUB_TOKEN }}"

  # GitHub App configuration for minting a token used in pre-activation reactions,
  # activation status comments, and skip-if search queries. When configured, a
  # single GitHub App installation access token is minted and shared across all
  # these operations instead of using the default GITHUB_TOKEN. Can be defined in a
  # shared agentic workflow and inherited by importing workflows.
  # (optional)
  github-app:
    # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
    # }}').
    # (optional)
    app-id: "example-value"

    # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
    # token.
    # (optional)
    client-id: "example-value"

    # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
    # mint a GitHub App token.
    # (optional)
    private-key: "example-value"

    # If true, skip token minting when client-id/private-key resolve to empty strings
    # at runtime. Defaults to false.
    # (optional)
    ignore-if-missing: true

    # Optional owner of the GitHub App installation (defaults to current repository
    # owner if not specified)
    # (optional)
    owner: "example-value"

    # Optional list of repositories to grant access to (defaults to current repository
    # if not specified)
    # (optional)
    repositories: []
      # Array of strings

    # Optional extra GitHub App-only permissions to merge into the minted token. Takes
    # effect for tools.github.github-app and safe-outputs.github-app; ignored in
    # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
    # scopes (e.g. members, organization-administration) not expressible via standard
    # handler declarations.
    # (optional)
    permissions:
      # Permission level for repository administration (read/none; "write" is rejected
      # by the compiler). GitHub App-only permission for repository administration.
      # (optional)
      administration: "read"

      # Permission level for Codespaces (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      codespaces: "read"

      # Permission level for Codespaces lifecycle administration (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      codespaces-lifecycle-admin: "read"

      # Permission level for Codespaces metadata (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      codespaces-metadata: "read"

      # Permission level for user email addresses (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      email-addresses: "read"

      # Permission level for repository environments (read/none; "write" is rejected by
      # the compiler). GitHub App-only permission.
      # (optional)
      environments: "read"

      # Permission level for git signing (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      git-signing: "read"

      # Permission level for organization members (read/none; "write" is rejected by the
      # compiler). Required for org team membership API calls.
      # (optional)
      members: "read"

      # Permission level for organization administration (read/none; "write" is rejected
      # by the compiler). GitHub App-only permission.
      # (optional)
      organization-administration: "read"

      # Permission level for organization announcement banners (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-announcement-banners: "read"

      # Permission level for organization Codespaces (read/none; "write" is rejected by
      # the compiler). GitHub App-only permission.
      # (optional)
      organization-codespaces: "read"

      # Permission level for organization Copilot (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      organization-copilot: "read"

      # Permission level for organization custom org roles (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-custom-org-roles: "read"

      # Permission level for organization custom properties (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-custom-properties: "read"

      # Permission level for organization custom repository roles (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-custom-repository-roles: "read"

      # Permission level for organization events (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      organization-events: "read"

      # Permission level for organization webhooks (read/none; "write" is rejected by
      # the compiler). GitHub App-only permission.
      # (optional)
      organization-hooks: "read"

      # Permission level for organization members management (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-members: "read"

      # Permission level for organization packages (read/none; "write" is rejected by
      # the compiler). GitHub App-only permission.
      # (optional)
      organization-packages: "read"

      # Permission level for organization personal access token requests (read/none;
      # "write" is rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-personal-access-token-requests: "read"

      # Permission level for organization personal access tokens (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-personal-access-tokens: "read"

      # Permission level for organization plan (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      organization-plan: "read"

      # Permission level for organization self-hosted runners (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-self-hosted-runners: "read"

      # Permission level for organization user blocking (read/none; "write" is rejected
      # by the compiler). GitHub App-only permission.
      # (optional)
      organization-user-blocking: "read"

      # Permission level for repository custom properties (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      repository-custom-properties: "read"

      # Permission level for secret scanning alerts (read/none). Forwarded as
      # permission-secret-scanning-alerts input for actions/create-github-app-token.
      # (optional)
      secret-scanning-alerts: "read"

      # Permission level for repository webhooks (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      repository-hooks: "read"

      # Permission level for single file access (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      single-file: "read"

      # Permission level for team discussions (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      team-discussions: "read"

      # Permission level for Dependabot vulnerability alerts (read/none; "write" is
      # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
      # with a GitHub App, forwarded as permission-vulnerability-alerts input.
      # (optional)
      vulnerability-alerts: "read"

      # Permission level for GitHub Actions workflow files (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      workflows: "read"

  # Explicit additional custom workflow jobs that pre_activation and activation
  # should depend on.
  # (optional)
  needs: []
    # Array of strings

  # When true, restore cache-memory, repo-memory, and comment-memory stores before
  # on.steps in pre_activation. Defaults to false.
  # (optional)
  restore-memory: true

  # Steps to inject into the pre-activation job. These steps run after all built-in
  # checks (membership, stop-time, skip-if, etc.) and their results are exposed as
  # pre-activation outputs. Use 'id' on steps to reference their results via
  # needs.pre_activation.outputs.<id>_result.
  # (optional)
  steps: []
    # Array items:
      # Optional name for the step
      # (optional)
      name: "My Workflow"

      # Optional step ID. When set, the step result is exposed as
      # needs.pre_activation.outputs.<id>_result
      # (optional)
      id: "example-value"

      # Shell command to run
      # (optional)
      run: "example-value"

      # Action to use (e.g., 'actions/checkout@v4')
      # (optional)
      uses: "example-value"

      # Input parameters for the action
      # (optional)
      with:
        {}

      # Environment variables for the step
      # (optional)
      env:
        {}

      # Conditional expression for the step
      # (optional)
      if: "example-value"

      # Whether to continue if the step fails
      # (optional)
      continue-on-error: true

  # Additional permissions for the pre-activation job. Use to declare extra scopes
  # required by on.steps (e.g., issues: read for GitHub API calls in steps).
  # (optional)
  permissions:
    # Permission for GitHub Actions workflows and runs (read: view workflows, write:
    # manage workflows, none: no access)
    # (optional)
    actions: "read"

    # Permission for artifact attestations (read: view attestations, write: create
    # attestations, none: no access)
    # (optional)
    attestations: "read"

    # Permission for repository checks and status checks (read: view checks, write:
    # create/update checks, none: no access)
    # (optional)
    checks: "read"

    # Permission for the GitHub code coverage API (read: view coverage reports, write:
    # upload coverage reports, none: no access). Required by the upload-code-coverage
    # safe output.
    # (optional)
    code-quality: "read"

    # Permission level for Copilot requests (write/none only). Set to write to allow
    # Copilot inference via the GitHub Actions token.
    # (optional)
    copilot-requests: "write"

    # Permission for repository contents (read: view files, write: modify
    # files/branches, none: no access)
    # (optional)
    contents: "read"

    # Permission for repository deployments (read: view deployments, write:
    # create/update deployments, none: no access)
    # (optional)
    deployments: "read"

    # Permission for repository discussions (read: view discussions, write:
    # create/update discussions, none: no access)
    # (optional)
    discussions: "read"

    # Permission for the experimental GitHub Drives service
    # (optional)
    drives: "read"

    # Permission level for OIDC token requests (write/none only - read is not
    # supported). Allows workflows to request JWT tokens for cloud provider
    # authentication.
    # (optional)
    id-token: "write"

    # Permission for repository issues (read: view issues, write: create/update/close
    # issues, none: no access)
    # (optional)
    issues: "read"

    # Permission for GitHub Copilot models (read: access AI models for agentic
    # workflows, none: no access)
    # (optional)
    models: "read"

    # Permission for repository metadata (read: view repository information, write:
    # update repository metadata, none: no access)
    # (optional)
    metadata: "read"

    # Permission level for GitHub Packages (read/write/none). Controls access to
    # publish, modify, or delete packages.
    # (optional)
    packages: "read"

    # Permission level for GitHub Pages (read/write/none). Controls access to deploy
    # and manage GitHub Pages sites.
    # (optional)
    pages: "read"

    # Permission level for pull requests (read/write/none). Controls access to create,
    # edit, review, and manage pull requests.
    # (optional)
    pull-requests: "read"

    # Permission level for repository projects (read/write/none). Controls access to
    # manage repository-level GitHub Projects boards.
    # (optional)
    repository-projects: "read"

    # Permission level for organization projects (read/write/none). Controls access to
    # manage organization-level GitHub Projects boards.
    # (optional)
    organization-projects: "read"

    # Permission level for organization custom org roles (read/write/none). Controls
    # access to custom organization role metadata.
    # (optional)
    organization-custom-org-roles: "read"

    # Permission level for organization custom repository roles (read/write/none).
    # Controls access to custom repository role metadata.
    # (optional)
    organization-custom-repository-roles: "read"

    # Permission level for security events (read/write/none). Controls access to view
    # and manage code scanning alerts and security findings.
    # (optional)
    security-events: "read"

    # Permission level for secret scanning alerts (read/none). GitHub App-only
    # permission forwarded as permission-secret-scanning-alerts input for
    # actions/create-github-app-token.
    # (optional)
    secret-scanning-alerts: "read"

    # Permission level for commit statuses (read/write/none). Controls access to
    # create and update commit status checks.
    # (optional)
    statuses: "read"

    # Permission level for Dependabot vulnerability alerts (read/write/none). Allows
    # workflows to access the Dependabot alerts API via GITHUB_TOKEN instead of
    # requiring a PAT or GitHub App.
    # (optional)
    vulnerability-alerts: "read"

    # Permission shorthand that applies read access to all permission scopes. Can be
    # combined with specific write permissions to override individual scopes. 'write'
    # is not allowed for all.
    # (optional)
    all: "read"

  # Controls the stale lock file check in the activation job. Set to false to
  # disable the check, true (default) to enable frontmatter hash checking, or "full"
  # to check both frontmatter and body hashes. Use "full" when prompt-body edits
  # should also trigger recompilation detection. Useful when the workflow source
  # files are managed outside the default GitHub repo context (e.g. cross-repo org
  # rulesets) and the stale check is not needed (set false), or when comprehensive
  # drift detection is required (set "full").
  # (optional)
  # Accepted formats:

  # Format 1: boolean
  stale-check: true

  # Format 2: string
  stale-check: "full"

  # Controls whether the activation job creates or updates a notification issue
  # when the workflow is compiled with a blocked compile-agentic version. Set to
  # false to suppress only that notification issue while keeping the
  # blocked-version check itself (and its hard failure) active. This is
  # independent of 'check-for-updates' (which disables the whole check) and
  # 'safe-outputs.report-failure-as-issue' (which also gates the notification).
  # (optional)
  report-blocked-version: true

# ⚠️ Experimental. Agent Plugins to install after the agentic engine. Each GitHub
# repository reference must include a ref that the compiler resolves to a commit
# SHA. Using this field emits a compile-time warning. Entries may also be objects
# to configure per-plugin authentication via github-token or github-app, for
# installing plugins from private repositories.
# (optional)
plugins: []

# GitHub token permissions for the workflow. Controls what the GITHUB_TOKEN can
# access during execution. Use the principle of least privilege - only grant the
# minimum permissions needed.
# (optional)
# Accepted formats:

# Format 1: Simple permissions string: 'read-all' (all read permissions),
# 'write-all' (all write permissions), or 'none' (no permissions)
permissions: "read-all"

# Format 2: Detailed permissions object with granular control over specific GitHub
# API scopes
permissions:
  # Permission for GitHub Actions workflows and runs (read: view workflows, write:
  # manage workflows, none: no access)
  # (optional)
  actions: "read"

  # Permission for artifact attestations (read: view attestations, write: create
  # attestations, none: no access)
  # (optional)
  attestations: "read"

  # Permission for repository checks and status checks (read: view checks, write:
  # create/update checks, none: no access)
  # (optional)
  checks: "read"

  # Permission for the GitHub code coverage API (read: view coverage reports, write:
  # upload coverage reports, none: no access). Required by the upload-code-coverage
  # safe output.
  # (optional)
  code-quality: "read"

  # Permission level for Copilot requests (write/none only). Set to write to allow
  # Copilot inference via the GitHub Actions token.
  # (optional)
  copilot-requests: "write"

  # Permission for repository contents (read: view files, write: modify
  # files/branches, none: no access)
  # (optional)
  contents: "read"

  # Permission for repository deployments (read: view deployments, write:
  # create/update deployments, none: no access)
  # (optional)
  deployments: "read"

  # Permission for repository discussions (read: view discussions, write:
  # create/update discussions, none: no access)
  # (optional)
  discussions: "read"

  # Permission for the experimental GitHub Drives service
  # (optional)
  drives: "read"

  # Permission level for OIDC token requests (write/none only - read is not
  # supported). Allows workflows to request JWT tokens for cloud provider
  # authentication.
  # (optional)
  id-token: "write"

  # Permission for repository issues (read: view issues, write: create/update/close
  # issues, none: no access)
  # (optional)
  issues: "read"

  # Permission for GitHub Copilot models (read: access AI models for agentic
  # workflows, none: no access)
  # (optional)
  models: "read"

  # Permission for repository metadata (read: view repository information, write:
  # update repository metadata, none: no access)
  # (optional)
  metadata: "read"

  # Permission level for GitHub Packages (read/write/none). Controls access to
  # publish, modify, or delete packages.
  # (optional)
  packages: "read"

  # Permission level for GitHub Pages (read/write/none). Controls access to deploy
  # and manage GitHub Pages sites.
  # (optional)
  pages: "read"

  # Permission level for pull requests (read/write/none). Controls access to create,
  # edit, review, and manage pull requests.
  # (optional)
  pull-requests: "read"

  # Permission level for repository projects (read/write/none). Controls access to
  # manage repository-level GitHub Projects boards.
  # (optional)
  repository-projects: "read"

  # Permission level for organization projects (read/write/none). Controls access to
  # manage organization-level GitHub Projects boards.
  # (optional)
  organization-projects: "read"

  # Permission level for organization custom org roles (read/write/none). Controls
  # access to custom organization role metadata.
  # (optional)
  organization-custom-org-roles: "read"

  # Permission level for organization custom repository roles (read/write/none).
  # Controls access to custom repository role metadata.
  # (optional)
  organization-custom-repository-roles: "read"

  # Permission level for security events (read/write/none). Controls access to view
  # and manage code scanning alerts and security findings.
  # (optional)
  security-events: "read"

  # Permission level for secret scanning alerts (read/none). GitHub App-only
  # permission forwarded as permission-secret-scanning-alerts input for
  # actions/create-github-app-token.
  # (optional)
  secret-scanning-alerts: "read"

  # Permission level for commit statuses (read/write/none). Controls access to
  # create and update commit status checks.
  # (optional)
  statuses: "read"

  # Permission level for Dependabot vulnerability alerts (read/write/none). Allows
  # workflows to access the Dependabot alerts API via GITHUB_TOKEN instead of
  # requiring a PAT or GitHub App.
  # (optional)
  vulnerability-alerts: "read"

  # Permission shorthand that applies read access to all permission scopes. Can be
  # combined with specific write permissions to override individual scopes. 'write'
  # is not allowed for all.
  # (optional)
  all: "read"

# Custom name for workflow runs that appears in the GitHub Actions interface
# (supports GitHub expressions like ${{ github.event.issue.title }})
# (optional)
run-name: "example-value"

# Groups together all the jobs that run in the workflow
# (optional)
jobs:
  {}

# Runner type for workflow execution (GitHub Actions standard field). Supports
# multiple forms: simple string for single runner label (e.g., 'ubuntu-latest'),
# array for runner selection with fallbacks, or object for GitHub-hosted runner
# groups with specific labels. For agentic workflows, runner selection matters
# when AI workloads require specific compute resources or when using self-hosted
# runners with specialized capabilities. Typically configured at the job level
# instead. See
# https://docs.github.com/en/actions/using-jobs/choosing-the-runner-for-a-job
# (optional)
# Accepted formats:

# Format 1: Simple runner label string. Use for standard GitHub-hosted runners
# (e.g., 'ubuntu-latest', 'windows-latest', 'macos-latest') or self-hosted runner
# labels. Most common form for agentic workflows.
runs-on: "example-value"

# Format 2: Array of runner labels for selection with fallbacks. GitHub Actions
# will use the first available runner that matches any label in the array. Useful
# for high-availability setups or when multiple runner types are acceptable.
runs-on: []
  # Array items: string

# Format 3: Runner group configuration for GitHub-hosted runners. Use this form to
# target specific runner groups (e.g., larger runners with more CPU/memory) or
# self-hosted runner pools with specific label requirements. Agentic workflows may
# benefit from larger runners for complex AI processing tasks.
runs-on:
  # Runner group name for self-hosted runners or GitHub-hosted runner groups
  # (optional)
  group: "example-value"

  # List of runner labels for self-hosted runners or GitHub-hosted runner selection
  # (optional)
  labels: []
    # Array of strings

# Runner for all framework/generated jobs (activation, pre-activation,
# safe-outputs, unlock, APM, etc.). Provides a compile-stable override for
# generated job runners without requiring a safe-outputs section. Supports the
# same string, array, and runner-group object forms as runs-on. Overridden by
# safe-outputs.runs-on when both are set. Defaults to 'ubuntu-slim'. Use this when
# your infrastructure does not provide the default runner or when you need
# consistent runner selection across all jobs.
# (optional)
# Accepted formats:

# Format 1: Simple runner label string. Use for standard GitHub-hosted runners
# (e.g., 'ubuntu-latest', 'windows-latest', 'macos-latest') or self-hosted runner
# labels. Most common form for agentic workflows.
runs-on-slim: "example-value"

# Format 2: Array of runner labels for selection with fallbacks. GitHub Actions
# will use the first available runner that matches any label in the array. Useful
# for high-availability setups or when multiple runner types are acceptable.
runs-on-slim: []
  # Array items: string

# Format 3: Runner group configuration for GitHub-hosted runners. Use this form to
# target specific runner groups (e.g., larger runners with more CPU/memory) or
# self-hosted runner pools with specific label requirements. Agentic workflows may
# benefit from larger runners for complex AI processing tasks.
runs-on-slim:
  # Runner group name for self-hosted runners or GitHub-hosted runner groups
  # (optional)
  group: "example-value"

  # List of runner labels for self-hosted runners or GitHub-hosted runner selection
  # (optional)
  labels: []
    # Array of strings

# Runner topology configuration. Tells gh-aw and AWF what kind of runner
# environment the workflow targets, so they can activate topology-specific
# behaviors automatically (split-filesystem handling, network isolation, sysroot
# images, tool cache redirection). The runner.topology key is the single stable
# contract between gh-aw and AWF for runner environment detection.
# (optional)
runner:
  # Runner execution topology. When set to 'arc-dind', gh-aw emits the topology in
  # the AWF config, redirects the tool cache to a shared volume, and validates that
  # no generated steps require root. AWF then activates split-filesystem handling,
  # network isolation, sysroot image staging, and DinD pre-staging automatically.
  # (optional)
  topology: "arc-dind"

# Agentic execution step timeout in minutes. Defaults to 20 minutes for agentic
# workflows, or the GH_AW_DEFAULT_TIMEOUT_MINUTES GitHub Actions variable. The
# generated jobs are bounded separately by jobs.agent.timeout-minutes (default 60
# minutes or GH_AW_DEFAULT_AGENT_JOB_TIMEOUT_MINUTES) and
# jobs.detection.timeout-minutes (default 10 minutes or
# GH_AW_DEFAULT_DETECTION_JOB_TIMEOUT_MINUTES). Supports GitHub Actions
# expressions (e.g. '${{ inputs.timeout }}') for reusable workflow_call workflows.
# (optional)
# Accepted formats:

# Format 1: integer
timeout-minutes: 1

# Format 2: GitHub Actions expression that resolves to an integer at runtime
timeout-minutes: "example-value"

# Concurrency control to limit concurrent workflow runs (GitHub Actions standard
# field). Supports two forms: simple string for basic group isolation, or object
# with cancel-in-progress option for advanced control. Agentic workflows enhance
# this with automatic per-engine concurrency policies (defaults to single job per
# engine across all workflows) and token-based rate limiting. Default behavior:
# workflows in the same group queue sequentially unless cancel-in-progress is
# true. See https://docs.github.com/en/actions/using-jobs/using-concurrency
# (optional)
# Accepted formats:

# Format 1: Simple concurrency group name to prevent multiple runs in the same
# group. Use expressions like '${{ github.workflow }}' for per-workflow isolation
# or '${{ github.ref }}' for per-branch isolation. Agentic workflows automatically
# generate enhanced concurrency policies using 'gh-aw-{engine-id}' as the default
# group to limit concurrent AI workloads across all workflows using the same
# engine.
concurrency: "example-value"

# Format 2: Concurrency configuration object with group isolation and cancellation
# control. Use object form when you need fine-grained control over whether to
# cancel in-progress runs. For agentic workflows, this is useful to prevent
# multiple AI agents from running simultaneously and consuming excessive resources
# or API quotas.
concurrency:
  # Concurrency group name. Workflows in the same group cannot run simultaneously.
  # Supports GitHub Actions expressions for dynamic group names based on branch,
  # workflow, or other context.
  # (optional)
  group: "example-value"

  # Whether to cancel in-progress workflows in the same concurrency group when a new
  # one starts. Default: false (queue new runs). Set to true for agentic workflows
  # where only the latest run matters (e.g., PR analysis that becomes stale when new
  # commits are pushed).
  # (optional)
  cancel-in-progress: true

  # Pending run queue behavior for this concurrency group. 'single' (default) allows
  # one pending run and replaces older pending runs. 'max' allows up to 100 pending
  # runs in FIFO order.
  # (optional)
  queue: "single"

  # Additional discriminator expression appended to compiler-generated job-level
  # concurrency groups (agent, output jobs). Use this when multiple workflow
  # instances are dispatched concurrently with different inputs (fan-out pattern) to
  # prevent job-level concurrency groups from colliding. For example, '${{
  # inputs.finding_id }}' ensures each dispatched run gets a unique job-level group.
  # Supports GitHub Actions expressions. This field is stripped from the compiled
  # lock file (it is a gh-aw extension, not a GitHub Actions field).
  # (optional)
  job-discriminator: "example-value"

# Environment variables for the workflow
# (optional)
# Accepted formats:

# Format 1: object
env:
  {}

# Format 2: string
env: "example-value"

# Feature flags and configuration options for experimental or optional features in
# the workflow. Each feature can be a boolean flag or a string value. The
# 'action-tag' feature (string) specifies the tag or SHA to use when referencing
# actions/setup in compiled workflows (for testing purposes only). The
# 'intentional-failure' feature (boolean) marks the workflow as one that is
# intentionally expected to fail (e.g., guardrail stress-tests); such workflows
# are excluded from fleet-health and prod-main success-rate rollups.
# (optional)
features:
  # Mark the workflow as one that is intentionally expected to fail (e.g., guardrail
  # stress-tests). Workflows tagged with features.intentional-failure: true are
  # excluded from fleet-health and prod-main success-rate rollups so they do not
  # depress real-regression baselines.
  # (optional)
  intentional-failure: true

  # Internal hidden feature. Replace the agentic step with the deterministic
  # safe-outputs samples replay driver for this workflow, exactly as the hidden `gh
  # aw compile --use-samples` flag does globally. Use it for workflows that are
  # designed around `safe-outputs.*.samples` so a plain `gh aw compile` keeps
  # producing the samples-mode lock file. Threat detection is force-disabled when
  # enabled.
  # (optional)
  samples: true

# Model policy and optional pricing configuration. The policy fields
# (allowed/blocked) are experimental and merged as unions across imports. The
# providers field is optional and supplies pricing data merged by provider/model
# key.
# (optional)
models:
  # Experimental allowlist of model names/patterns. Mapped to AWF
  # apiProxy.allowedModels.
  # (optional)
  allowed: []
    # Array of strings

  # Experimental denylist of model names/patterns. Mapped to AWF
  # apiProxy.disallowedModels.
  # (optional)
  blocked: []
    # Array of strings

  # Provider-keyed map of model pricing data.
  # (optional)
  providers:
    {}

  # Fallback per-token pricing ($/1M tokens) for models not in the built-in pricing
  # table. Required when max-ai-credits is active and the model is self-hosted or
  # unrecognized (e.g. BYOK Ollama). Without this, the AWF API proxy rejects
  # unrecognized models with HTTP 400.
  # (optional)
  default-ai-credits-pricing:
    # Cost per input token in USD.
    # (optional)
    # Accepted formats:

    # Format 1: number
    input: 1

    # Format 2: string
    input: "example-value"

    # Cost per output token in USD.
    # (optional)
    # Accepted formats:

    # Format 1: number
    output: 1

    # Format 2: string
    output: "example-value"

    # Cost per cached-read token in USD.
    # (optional)
    # Accepted formats:

    # Format 1: number
    cache_read: 1

    # Format 2: string
    cache_read: "example-value"

    # Cost per cache-write token in USD.
    # (optional)
    # Accepted formats:

    # Format 1: number
    cache_write: 1

    # Format 2: string
    cache_write: "example-value"

    # Cost per reasoning token in USD.
    # (optional)
    # Accepted formats:

    # Format 1: number
    reasoning: 1

    # Format 2: string
    reasoning: "example-value"

# A/B testing experiments. Each key is an experiment name; the value is either an
# array of two or more variant strings (bare-array form) or an object with a
# 'variants' field plus optional metadata fields (description, metric, weight,
# issue, start_date, end_date, hypothesis, secondary_metrics, guardrail_metrics,
# min_samples, analysis_type, decision). The reserved 'storage' key controls how
# experiment state is persisted: 'repo' (default) commits state to a git branch
# named 'experiments/{sanitizedWorkflowID}' (workflow ID lowercased with hyphens
# removed) for durability; 'cache' uses GitHub Actions cache. At runtime the
# activation job picks a variant and persists the updated counters. Use ${{
# experiments.<name> }} in the workflow prompt to reference the selected variant.
# When multiple experiments are declared, assignments are statistically balanced
# using a least-used counter that round-robins across variants (or weighted when
# 'weight' is provided); ties are broken randomly so no variant is systematically
# favoured on the first run.
# (optional)
experiments:
  # Storage backend for experiment state. 'repo' (default) persists state to a git
  # branch named 'experiments/{sanitizedWorkflowID}' (workflow ID lowercased with
  # hyphens removed, e.g. 'my-workflow' -> 'experiments/myworkflow') for durability
  # across cache evictions. 'cache' uses GitHub Actions cache (legacy behavior).
  # Repo storage is recommended because experiment data is valuable and more durable
  # than cache.
  # (optional)
  storage: "cache"

# Secret values passed to workflow execution. Secrets can be defined as simple
# strings (GitHub Actions expressions) or objects with 'value' and 'description'
# properties. Typically used to provide secrets to MCP servers or custom engines.
# Note: For passing secrets to reusable workflows, use the jobs.<job_id>.secrets
# field instead.
# (optional)
secrets:
  {}

# Environment that the job references (for protected environments and deployments)
# (optional)
# Accepted formats:

# Format 1: Environment name as a string
environment: "example-value"

# Format 2: Environment object with name and optional URL
environment:
  # The name of the environment configured in the repo
  name: "My Workflow"

  # A deployment URL
  # (optional)
  url: "example-value"

# Container to run the job steps in
# (optional)
# Accepted formats:

# Format 1: Docker image name (e.g., 'node:18', 'ubuntu:latest')
container: "example-value"

# Format 2: Container configuration object
container:
  # The Docker image to use as the container
  image: "example-value"

  # Credentials for private registries
  # (optional)
  credentials:
    # Username for Docker registry authentication when pulling private container
    # images.
    # (optional)
    username: "example-value"

    # Password or access token for Docker registry authentication. Should use secrets
    # syntax: ${{ secrets.DOCKER_PASSWORD }}
    # (optional)
    password: "example-value"

  # Environment variables for the container
  # (optional)
  env:
    {}

  # Ports to expose on the container
  # (optional)
  ports: []

  # Volumes for the container
  # (optional)
  volumes: []
    # Array of strings

  # Additional Docker container options
  # (optional)
  options: "example-value"

# Service containers for the job
# (optional)
services:
  {}

# Network access control for AI engines using ecosystem identifiers and domain
# allowlists. Supports wildcard patterns like '*.example.com' to match any
# subdomain. Controls web fetch and search capabilities. IMPORTANT: For workflows
# that build/install/test code, always include the language ecosystem identifier
# alongside 'defaults' — 'defaults' alone only covers basic infrastructure, not
# package registries. Key ecosystem identifiers by runtime: 'dotnet' (.NET/NuGet),
# 'python' (pip/PyPI), 'node' (npm/yarn), 'go' (go modules), 'java'
# (Maven/Gradle), 'ruby' (Bundler), 'rust' (Cargo), 'swift' (Swift PM). Example: a
# .NET project needs network: { allowed: [defaults, dotnet] }.
# (optional)
# Accepted formats:

# Format 1: Use default network permissions (basic infrastructure: certificates,
# JSON schema, Ubuntu, etc.)
network: "defaults"

# Format 2: Custom network access configuration with ecosystem identifiers and
# specific domains
network:
  # List of allowed domains or ecosystem identifiers (e.g., 'defaults', 'python',
  # 'node', '*.example.com'). Wildcard patterns match any subdomain AND the base
  # domain.
  # (optional)
  allowed: []
    # Array of Domain name or ecosystem identifier. Supports wildcards like
    # '*.example.com' (matches sub.example.com, deep.nested.example.com, and
    # example.com itself). Ecosystem identifiers by runtime: 'dotnet' (.NET/NuGet),
    # 'python' (pip/PyPI), 'node' (npm/yarn), 'go' (go modules), 'java'
    # (Maven/Gradle), 'ruby' (RubyGems), 'rust' (Cargo), 'swift' (Swift PM), 'php'
    # (Composer), 'dart' (pub.dev), 'haskell' (Hackage), 'perl' (CPAN), 'containers'
    # (Docker/GHCR), 'github' (GitHub domains), 'terraform' (HashiCorp),
    # 'linux-distros' (apt/yum), 'playwright' (browser testing), 'defaults' (basic
    # infrastructure).

  # When true and the workflow uses workflow_call, expose a network_allowed string
  # input on the compiled lock file. The caller-supplied value is unioned with
  # network.allowed at runtime, supporting ecosystem identifiers (for example
  # 'rust') or comma-separated domains.
  # (optional)
  allowed-input: true

  # List of blocked domains or ecosystem identifiers (e.g., 'python', 'node',
  # 'tracker.example.com'). Blocked domains take precedence over allowed domains.
  # (optional)
  blocked: []
    # Array of Domain name or ecosystem identifier to block. Supports wildcards like
    # '*.example.com' (matches sub.example.com, deep.nested.example.com, and
    # example.com itself) and ecosystem names like 'python', 'node'.

# AWF-owned private-repository executors exposed only through the
# compiler-launched MCP gateway. Omit this field to disable enclaves.
# (optional)
enclaves: []

# Sandbox configuration for AI engines. Controls agent sandbox (AWF) and MCP
# gateway. The MCP gateway is always enabled and cannot be disabled.
# (optional)
# Accepted formats:

# Format 1: String format for sandbox type: 'default' for no sandbox, 'awf' for
# Agent Workflow Firewall. Note: Legacy 'srt' and 'sandbox-runtime' values are
# automatically migrated to 'awf'
sandbox: "default"

# Format 2: Object format for full sandbox configuration with agent and MCP
# gateway options
sandbox:
  # Legacy sandbox type field (use agent instead). Note: Legacy 'srt' and
  # 'sandbox-runtime' values are automatically migrated to 'awf'
  # (optional)
  type: "default"

  # Agent sandbox type: 'awf' uses AWF (Agent Workflow Firewall), or false to
  # disable agent sandbox. Defaults to 'awf' if not specified. Note: Disabling the
  # agent sandbox (false) removes firewall protection but keeps the MCP gateway
  # enabled.
  # (optional)
  # Accepted formats:

  # Format 1: Null (an empty 'agent:' key) leaves the agent sandbox at its default
  # of 'awf'
  agent: null

  # Format 2: Set to false to disable the agent sandbox (firewall). Warning: This
  # removes firewall protection but keeps the MCP gateway enabled. Not allowed in
  # strict mode.
  agent: true

  # Format 3: Sandbox type: 'awf' for Agent Workflow Firewall
  agent: "awf"

  # Format 4: Custom sandbox runtime configuration
  agent:
    # Agent identifier (replaces 'type' field in new format): 'awf' for Agent Workflow
    # Firewall
    # (optional)
    id: "awf"

    # Legacy: Sandbox type to use (use 'id' instead)
    # (optional)
    type: "awf"

    # AWF version override used to install and run the matching firewall version.
    # (optional)
    version: "example-value"

    # AWF platform.type override. Declares the GitHub deployment type so AWF can apply
    # deterministic Copilot auth behavior without relying on host heuristics. Omit to
    # let AWF use its default host heuristic behavior.
    # (optional)
    platform: "github.com"

    # Container mounts to add when using AWF. Each mount is specified using Docker
    # mount syntax: 'source:destination:mode' where mode can be 'ro' (read-only) or
    # 'rw' (read-write). Example: '/host/path:/container/path:ro'
    # (optional)
    mounts: []
      # Array of Mount specification in format 'source:destination:mode'

    # Memory limit for the AWF container (e.g., '4g', '8g'). Passed as --memory-limit
    # to AWF. If not specified, AWF's default memory limit is used.
    # (optional)
    memory: "example-value"

    # Digest-pinned AWF infrastructure images (AWF v0.28.4+). Selects the container
    # image used for each AWF service role instead of AWF's defaults. Every value must
    # be a literal, registry-qualified OCI reference that carries both a tag and an
    # immutable SHA-256 digest ('registry/repository:tag@sha256:<64 lowercase hex
    # chars>'); GitHub Actions expressions and other dynamic values are rejected at
    # compile time. When set, the manifest must cover every image role required by the
    # enabled feature set, AWF fails closed instead of falling back to a default, the
    # same references are predownloaded and recorded in lock metadata unchanged, and
    # the Docker daemon must already be authenticated to the registry.
    # Repository-level '.github/workflows/aw.json' 'container_pins' mappings still
    # redirect non-AWF workflow containers and default AWF predownload references when
    # this manifest is omitted, but they cannot configure AWF runtime roles and never
    # redirect this authoritative manifest.
    # (optional)
    images:
      # Squid forward-proxy image used for domain filtering.
      # (optional)
      squid: "example-value"

      # Agent container image that runs the agentic engine.
      # (optional)
      agent: "example-value"

      # API proxy sidecar image that holds provider credentials.
      # (optional)
      apiProxy: "example-value"

      # CLI proxy sidecar image used by tools.github.mode: gh-proxy,
      # integrity-reactions, or raw --difc-proxy-host AWF arguments.
      # (optional)
      cliProxy: "example-value"

      # Build-tools image used as the chroot sysroot base on runner.topology: arc-dind.
      # (optional)
      buildTools: "example-value"

      # DNS-over-HTTPS proxy image required when legacy-security raw AWF arguments
      # enable --dns-over-https.
      # (optional)
      dohProxy: "example-value"

      # Script enclave image.
      # (optional)
      enclaveScript: "example-value"

      # Agent enclave image.
      # (optional)
      enclaveAgent: "example-value"

      # Shared enclave MCP server image required whenever any enclave is enabled.
      # (optional)
      enclaveMcpServer: "example-value"

      # Docker-in-Docker staging image required when raw AWF arguments enable directory
      # or engine-binary pre-staging.
      # (optional)
      dindStaging: "example-value"

    # Enable or disable model fallback for unresolved model selections. Set to false
    # for BYOK Azure OpenAI deployments to prevent deployment-name rewriting. Supports
    # literal boolean or GitHub Actions expression.
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    model-fallback: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    model-fallback: "example-value"

    # Enable or disable API proxy token steering. Set to false to preserve the
    # explicitly configured provider and model.
    # (optional)
    token-steering: true

    # Host path to an additional CA certificate for API proxy upstream TLS
    # verification. The file is bind-mounted read-only into the API proxy sidecar.
    # Maps to apiProxy.caCert; requires AWF v0.28.10 or later.
    # (optional)
    ca-cert: "example-value"

    # Sandbox runtime profile for the agent container. Each value selects one
    # supported security and topology profile: 'docker' (default) runs the agent under
    # Docker with a rootless AWF and network isolation; 'docker-sudo-iptables' runs
    # Docker with a privileged AWF, legacy iptables networking, and host/service
    # access (required for allow-host-ports and GitHub Actions services:
    # connectivity); 'gvisor' runs the agent under gVisor's runsc runtime with strict
    # network isolation; 'docker-sbx' runs the agent inside a Docker sbx KVM microVM
    # (requires DOCKER_PAT and DOCKER_USERNAME secrets and a KVM-capable runner; the
    # compiler handles the required privileged setup); 'cloud-hypervisor' runs the
    # agent in AWF's preview Cloud Hypervisor microVM runtime on GitHub-hosted Ubuntu
    # x86_64, sized at 2 vCPUs and 4096 MiB. Omitting runtime is equivalent to
    # 'docker'. gvisor, docker-sbx and cloud-hypervisor are incompatible with
    # runner.topology: arc-dind.
    # (optional)
    runtime: "docker"

    # Controls generation of sandbox runtime installation steps. Only valid with
    # runtime: gvisor or runtime: docker-sbx. Defaults to true. Set to false when the
    # runtime is already installed on the runner; docker-sbx credential refresh still
    # runs.
    # (optional)
    runtime-install: true

    # Custom sandbox runtime configuration. Note: Network configuration is controlled
    # by the top-level 'network' field, not here.
    # (optional)
    config:
      # Filesystem access control configuration for the agent within the sandbox.
      # Controls read/write permissions and path restrictions.
      # (optional)
      filesystem:
        # List of paths to deny read access
        # (optional)
        denyRead: []
          # Array of strings

        # List of paths to allow write access
        # (optional)
        allowWrite: []
          # Array of strings

        # List of paths to deny write access
        # (optional)
        denyWrite: []
          # Array of strings

      # Map of command patterns to paths that should ignore violations
      # (optional)
      ignoreViolations:
        {}

      # Enable weaker nested sandbox mode (recommended: true for Docker access)
      # (optional)
      enableWeakerNestedSandbox: true

    # Per-provider API proxy target overrides. Settings are compiled into the AWF
    # config JSON.
    # (optional)
    targets:
      # AWF API proxy target configuration for a single LLM provider.
      # (optional)
      openai:
        # Custom authentication header name to use when forwarding requests to this
        # provider's API. Overrides the provider default ("Authorization" for OpenAI,
        # "x-api-key" for Anthropic). Example: "api-key" for Azure OpenAI gateways.
        # (optional)
        authHeader: "example-value"

      # AWF API proxy target configuration for a single LLM provider.
      # (optional)
      anthropic:
        # Custom authentication header name to use when forwarding requests to this
        # provider's API. Overrides the provider default ("Authorization" for OpenAI,
        # "x-api-key" for Anthropic). Example: "api-key" for Azure OpenAI gateways.
        # (optional)
        authHeader: "example-value"

      # AWF API proxy target configuration for the Copilot BYOK provider. Supports
      # injecting additional headers, body fields, and an optional session ID on
      # upstream requests.
      # (optional)
      copilot:
        # Custom authentication header name to use when forwarding requests to the Copilot
        # API. Example: "api-key" for Azure OpenAI gateways.
        # (optional)
        authHeader: "example-value"

        # Additional non-sensitive HTTP headers to inject on Copilot BYOK upstream
        # requests. Maps to AWF_BYOK_EXTRA_HEADERS. Example: { "x-openrouter-title":
        # "my-workflow" }.
        # (optional)
        extraHeaders:
          {}

        # Additional non-sensitive JSON body fields to inject on Copilot BYOK upstream
        # requests. Maps to AWF_BYOK_EXTRA_BODY_FIELDS. Example: { "custom-field":
        # "custom-value" }.
        # (optional)
        extraBodyFields:
          {}

        # Optional session identifier injected as the x-session-id request header and
        # session_id body field on Copilot BYOK upstream requests. Maps to
        # AWF_PROVIDER_SESSION_ID. Only set this field when your upstream supports it —
        # strict OpenAI-compatible upstreams (e.g. Azure OpenAI) reject the unknown
        # session_id body field with HTTP 400. Example: "${{ github.run_id }}".
        # (optional)
        sessionId: "example-value"

    # Additional host TCP ports the agent may connect to. Requires runtime:
    # docker-sudo-iptables. Ports published by `services:` are reached via
    # --allow-host-service-ports instead; use this only for host daemons not declared
    # there.
    # (optional)
    allow-host-ports: []

  # Legacy custom Sandbox Runtime configuration (use agent.config instead). Note:
  # Network configuration is controlled by the top-level 'network' field, not here.
  # (optional)
  config:
    # Filesystem access control configuration for sandboxed workflows. Controls
    # read/write permissions and path restrictions for file operations.
    # (optional)
    filesystem:
      # Array of path patterns that deny read access in the sandboxed environment. Takes
      # precedence over other read permissions.
      # (optional)
      denyRead: []
        # Array of strings

      # Array of path patterns that allow write access in the sandboxed environment.
      # Paths outside these patterns are read-only.
      # (optional)
      allowWrite: []
        # Array of strings

      # Array of path patterns that deny write access in the sandboxed environment.
      # Takes precedence over other write permissions.
      # (optional)
      denyWrite: []
        # Array of strings

    # When true, log sandbox violations without blocking execution. Useful for
    # debugging and gradual enforcement of sandbox policies.
    # (optional)
    ignoreViolations:
      {}

    # When true, allows nested sandbox processes to run with relaxed restrictions.
    # Required for certain containerized tools that spawn subprocesses.
    # (optional)
    enableWeakerNestedSandbox: true

  # MCP Gateway configuration for routing MCP server calls through a unified HTTP
  # gateway. Requires the 'mcp-gateway' feature flag to be enabled. Per MCP Gateway
  # Specification v1.0.0: Only container-based execution is supported.
  # (optional)
  mcp:
    # Volume mounts for the MCP gateway container. Each mount is specified using
    # Docker mount syntax: 'source:destination:mode' where mode can be 'ro'
    # (read-only) or 'rw' (read-write). Example: '/host/data:/container/data:ro'
    # (optional)
    mounts: []
      # Array of Mount specification in format 'source:destination:mode'

    # Environment variables for MCP gateway
    # (optional)
    env:
      {}

    # Port number for the MCP gateway HTTP server (default: 8080)
    # (optional)
    port: 1

    # Agent/session identifier for authenticating with the MCP gateway (supports ${{
    # secrets.* }} syntax)
    # (optional)
    agent-id: "example-value"

    # Gateway domain for URL generation (default: 'host.docker.internal' when agent is
    # enabled, 'localhost' when disabled)
    # (optional)
    domain: "localhost"

    # Keepalive ping interval in seconds for HTTP MCP backends. Sends periodic pings
    # to prevent session expiry during long-running agent tasks. Set to -1 to disable
    # keepalive pings. Unset or 0 uses the gateway default (1500 seconds = 25
    # minutes).
    # (optional)
    keepalive-interval: 1

# Conditional execution expression
# (optional)
if: "example-value"

# Custom workflow steps
# (optional)
# Accepted formats:

# Format 1: object
steps:
  {}

# Format 2: array
steps: [{"prompt":"Analyze the issue and create a plan"}]
  # Array items: undefined

# Custom workflow steps to run at the very beginning of the agent job, before
# checkout and any other built-in steps. Use pre-steps to mint short-lived tokens
# or perform any setup that must happen before the repository is checked out. Step
# outputs are available via ${{ steps.<id>.outputs.<name> }} and can be referenced
# in checkout.token to avoid masked-value cross-job-boundary issues.
# (optional)
# Accepted formats:

# Format 1: object
pre-steps:
  {}

# Format 2: array
pre-steps: [{"name":"Mint short-lived token","id":"mint","uses":"some-org/token-minting-action@v1","with":{"scope":"target-org/target-repo"}}]
  # Array items: undefined

# Custom workflow steps to run immediately before AI execution, after all
# initialization and setup steps in the agent job.
# (optional)
# Accepted formats:

# Format 1: object
pre-agent-steps:
  {}

# Format 2: array
pre-agent-steps: [{"name":"Prepare final context","run":"echo \"ready\""}]
  # Array items: undefined

# Custom workflow steps to run after AI execution
# (optional)
# Accepted formats:

# Format 1: object
post-steps:
  {}

# Format 2: array
post-steps: [{"name":"Verify Post-Steps Execution","run":"echo \"✅ Post-steps are executing correctly\"\necho \"This step runs after the AI agent completes\"\n"},{"name":"Upload Test Results","if":"always()","uses":"actions/upload-artifact@v4","with":{"name":"post-steps-test-results","path":"/tmp/gh-aw/","retention-days":1,"if-no-files-found":"ignore"}}]
  # Array items: undefined

# AI engine configuration that specifies which AI processor interprets and
# executes the markdown content of the workflow. Defaults to 'copilot'.
# (optional)
# Accepted formats:

# Format 1: Engine name: built-in ('claude', 'codex', 'copilot', 'gemini', 'pi')
# or a named catalog entry
engine: "example-value"

# Format 2: Extended engine configuration object with advanced options for model
# selection, turn limiting, environment variables, and custom steps
engine:
  # AI engine identifier: built-in ('claude', 'codex', 'copilot', 'gemini', 'pi') or
  # a named catalog entry
  id: "example-value"

  # Optional version of the AI engine action (e.g., 'beta', 'stable', 20). Has
  # sensible defaults and can typically be omitted. Numeric values are automatically
  # converted to strings at runtime. GitHub Actions expressions (e.g., '${{
  # inputs.engine-version }}') are accepted and compiled with injection-safe env var
  # handling.
  # (optional)
  version: null

  # Model for this engine, overriding the top-level model. Has sensible defaults and
  # can typically be omitted. Supports full model IDs (e.g.,
  # 'claude-3-5-sonnet-20241022', 'gpt-4'), which is useful when a nested engine
  # (e.g. safe-outputs.threat-detection.engine) needs a different model than the
  # main agent engine.
  # (optional)
  model: "example-value"

  # Optional inference provider override for this engine. Defaults to the engine's
  # native provider (copilot: github, claude: anthropic, codex: openai, pi: github).
  # (optional)
  model-provider: "github"

  # Claude permission mode override. Defaults to acceptEdits (or auto when
  # tools.edit is false).
  # (optional)
  permission-mode: "auto"

  # Maximum number of continuations for multi-run autopilot mode. Default is 1
  # (single run, no autopilot). Values greater than 1 enable --autopilot mode for
  # the copilot engine with --max-autopilot-continues set to this value. Note: Only
  # supported by the copilot engine.
  # (optional)
  max-continuations: 1

  # Agent job concurrency configuration. Defaults to single job per engine across
  # all workflows (group: 'gh-aw-{engine-id}'). Supports full GitHub Actions
  # concurrency syntax.
  # (optional)
  # Accepted formats:

  # Format 1: Simple concurrency group name. Gets converted to GitHub Actions
  # concurrency format with the specified group.
  concurrency: "example-value"

  # Format 2: GitHub Actions concurrency configuration for the agent job. Controls
  # how many agentic workflow runs can run concurrently.
  concurrency:
    # Concurrency group identifier. Use GitHub Actions expressions like ${{
    # github.workflow }} or ${{ github.ref }}. Defaults to 'gh-aw-{engine-id}' if not
    # specified.
    group: "example-value"

    # Whether to cancel in-progress runs of the same concurrency group. Defaults to
    # false for agentic workflow runs.
    # (optional)
    cancel-in-progress: true

    # Pending run queue behavior for this concurrency group. 'single' (default) allows
    # one pending run and replaces older pending runs. 'max' allows up to 100 pending
    # runs in FIFO order.
    # (optional)
    queue: "single"

  # Custom user agent string for GitHub MCP server configuration (codex engine only)
  # (optional)
  user-agent: "example-value"

  # Custom executable path for the AI engine CLI. When specified, the workflow will
  # skip the standard installation steps and use this command instead. The command
  # should be the full path to the executable or a command available in PATH.
  # (optional)
  command: "example-value"

  # Harness configuration for the AI engine. Accepts either a bare filename string
  # (legacy) or an object with sub-keys for fine-grained control.
  # (optional)
  # Accepted formats:

  # Format 1: Custom Node.js harness script filename (legacy form). This replaces
  # the engine's built-in harness wrapper and must end with .js, .cjs, or .mjs.
  harness: "example-value"

  # Format 2: Harness configuration object with optional sub-keys for script
  # override and retry policy.
  harness:
    # Custom Node.js harness script filename. This replaces the engine's built-in
    # harness wrapper and must end with .js, .cjs, or .mjs.
    # (optional)
    use: "example-value"

    # Maximum retry attempts after the initial run (0 = no retries). Accepts a literal
    # integer or a GitHub Actions expression.
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max-retries: 1

    # Format 2: string
    max-retries: "example-value"

    # Delay in ms before the first retry. Accepts a literal integer or a GitHub
    # Actions expression.
    # (optional)
    # Accepted formats:

    # Format 1: integer
    initial-delay-ms: 1

    # Format 2: string
    initial-delay-ms: "example-value"

    # Multiplier applied to the delay after each retry. Accepts a literal integer or a
    # GitHub Actions expression.
    # (optional)
    # Accepted formats:

    # Format 1: integer
    backoff-multiplier: 1

    # Format 2: string
    backoff-multiplier: "example-value"

    # Maximum delay cap in ms. Accepts a literal integer or a GitHub Actions
    # expression.
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max-delay-ms: 1

    # Format 2: string
    max-delay-ms: "example-value"

    # Post-result idle watchdog timeout in seconds. Accepts a literal integer or a
    # GitHub Actions expression.
    # (optional)
    # Accepted formats:

    # Format 1: integer
    watchdog-timeout: 1

    # Format 2: string
    watchdog-timeout: "example-value"

  # Custom environment variables to pass to the AI engine, including secret
  # overrides (e.g., OPENAI_API_KEY: ${{ secrets.CUSTOM_KEY }})
  # (optional)
  env:
    {}

  # Engine-level authentication configuration for AWF API proxy sidecar integration
  # (for example, Azure OpenAI via GitHub OIDC). Values are mapped to AWF_AUTH_*
  # environment variables.
  # (optional)
  auth:
    # Authentication type. Currently only 'github-oidc' is supported.
    type: "github-oidc"

    # OIDC audience to request from GitHub Actions for token exchange.
    # (optional)
    audience: "example-value"

    # Optional Azure tenant ID for token exchange.
    # (optional)
    azure-tenant-id: "example-value"

    # Optional Azure client ID for token exchange.
    # (optional)
    azure-client-id: "example-value"

    # Optional Azure OAuth scope (defaults to
    # https://cognitiveservices.azure.com/.default in AWF sidecar).
    # (optional)
    azure-scope: "example-value"

    # Optional Azure cloud name (for example, public, usgovernment, china).
    # (optional)
    azure-cloud: "example-value"

    # Optional WIF provider discriminator. Recognized values are 'azure', 'anthropic',
    # and 'gcp'.
    # (optional)
    provider: "example-value"

    # Anthropic WIF federation rule ID (e.g., fdrl_...).
    # (optional)
    federation-rule-id: "example-value"

    # Anthropic WIF organization ID (e.g., org_...).
    # (optional)
    organization-id: "example-value"

    # Anthropic WIF service account ID (e.g., svac_...).
    # (optional)
    service-account-id: "example-value"

    # Anthropic WIF workspace ID (e.g., ws_...).
    # (optional)
    workspace-id: "example-value"

    # Google Cloud WIF workload identity provider resource name (e.g.,
    # projects/PROJECT_NUMBER/locations/global/workloadIdentityPools/POOL/providers/PROVIDER).
    # (optional)
    workload-identity-provider: "example-value"

    # Google Cloud service account email to impersonate via WIF (e.g.,
    # my-sa@my-project.iam.gserviceaccount.com).
    # (optional)
    service-account: "example-value"

    # Google Cloud project ID used for Vertex AI / Gemini Enterprise inference.
    # (optional)
    project: "example-value"

    # Google Cloud region for Vertex AI inference (e.g., us-central1). Defaults to
    # us-central1 when omitted.
    # (optional)
    location: "example-value"

  # Additional TOML configuration text that will be appended to the generated
  # config.toml in the action (codex engine only)
  # (optional)
  config: "example-value"

  # Agent identifier to pass to copilot --agent flag (copilot engine only).
  # Specifies which custom agent to use for the workflow.
  # (optional)
  agent: "example-value"

  # Custom API endpoint hostname for the agentic engine. Used for GitHub Enterprise
  # Cloud (GHEC), GitHub Enterprise Server (GHES), or custom AI endpoints. Example:
  # 'api.acme.ghe.com' for GHEC, 'api.enterprise.githubcopilot.com' for GHES, or
  # custom endpoint hostnames.
  # (optional)
  api-target: "example-value"

  # Optional array of command-line arguments to pass to the AI engine CLI. These
  # arguments are injected after all other args but before the prompt.
  # (optional)
  args: []
    # Array of strings

  # When true, disables automatic loading of context and custom instructions by the
  # AI engine. The engine-specific flag depends on the engine: copilot uses
  # --no-custom-instructions (suppresses .github/AGENTS.md and user-level custom
  # instructions), claude uses --bare (suppresses CLAUDE.md memory files), codex
  # uses --no-system-prompt (suppresses the default system prompt), gemini sets
  # GEMINI_SYSTEM_MD=/dev/null (overrides the built-in system prompt with an empty
  # one). Defaults to false.
  # (optional)
  bare: true

  # Engine-level MCP gateway configuration. Settings here apply to the MCP gateway
  # used by this engine.
  # (optional)
  mcp:
    # Session timeout for MCP gateway sessions as a Go duration string (e.g. "30m",
    # "4h", "24h"). Must be at least 5m (no upper bound). Omitted or empty uses the
    # effective gateway default (precedence: this field > MCP_GATEWAY_SESSION_TIMEOUT
    # env var > built-in default 6h). Longer timeouts benefit multi-hour workflows
    # such as large-scale migrations; shorter values free gateway resources sooner.
    # (optional)
    session-timeout: "example-value"

    # Timeout for individual MCP tool calls as a Go duration string (e.g. "30s", "2m",
    # "10m"). Must be between 10s and 600s inclusive. Omitted or empty uses the
    # gateway built-in default (60s). Use a higher value for slow MCP backends such as
    # full-text search over large indexes.
    # (optional)
    tool-timeout: "example-value"

  # Enables the experimental GitHub Copilot SDK integration (copilot engine only).
  # When true, the harness starts a separate headless Copilot CLI sidecar on the
  # configured localhost port and sets COPILOT_SDK_URI on child processes.
  # (optional)
  copilot-sdk: true

  # Custom driver script, command, or inline source for the engine. String values
  # accept a workspace-relative path or bare name. Inline object values are
  # supported for the Copilot engine and materialize runtime files before execution.
  # Supported inline runtimes are node, python, go, and java.
  # (optional)
  # Accepted formats:

  # Format 1: Workspace-relative driver path or bare command name. The copilot
  # engine accepts .js, .cjs, .mjs, .py, .ts, .mts, and .rb drivers; other engines
  # accept .js, .cjs, and .mjs. Bare names without an extension are treated as
  # built-in drivers or executables in PATH.
  driver: "example-value"

  # Format 2: Inline Copilot SDK driver source. Provide exactly one runtime key.
  driver:
    # Inline Node.js driver source written to a generated .cjs file.
    # (optional)
    node: "example-value"

    # Inline Python driver source written to a generated .py file.
    # (optional)
    python: "example-value"

    # Inline Go driver source written to a generated .go file.
    # (optional)
    go: "example-value"

    # Inline Java driver source written to a generated .java file.
    # (optional)
    java: "example-value"

  # Engine-specific plugin names to install before launching the engine. Currently
  # used by the Pi engine: each entry is passed to `pi install <extension>`.
  # (optional)
  extensions: []
    # Array of strings

  # Override the working directory for the engine's spawned process. Accepts a
  # literal path or a GitHub Actions expression (e.g. `${{ github.workspace
  # }}/subdir`). When set, passed as GH_AW_ENGINE_CWD to the engine execution
  # environment.
  # (optional)
  cwd: "example-value"

# Format 3: Inline engine definition: specifies a runtime adapter and optional
# provider settings directly in the workflow frontmatter, without requiring a
# named catalog entry
engine:
  # Runtime adapter reference for the inline engine definition
  runtime:
    # Runtime adapter identifier (e.g. 'codex', 'claude', 'copilot', 'gemini', 'pi')
    id: "example-value"

    # Optional version of the runtime adapter (e.g. '0.105.0', 'beta')
    # (optional)
    version: null

  # Optional provider configuration for the inline engine definition
  # (optional)
  provider:
    # Provider identifier (e.g. 'openai', 'anthropic', 'github', 'google')
    # (optional)
    id: "example-value"

    # Optional specific LLM model to use (e.g. 'gpt-5', 'claude-3-5-sonnet-20241022')
    # (optional)
    model: "example-value"

    # Authentication configuration for the provider
    # (optional)
    auth:
      # Name of the GitHub Actions secret that contains the API key for this provider
      # (optional)
      secret: "example-value"

      # Authentication strategy for the provider (default: api-key when secret is set)
      # (optional)
      strategy: "api-key"

      # OAuth 2.0 token endpoint URL. Required when strategy is
      # 'oauth-client-credentials'.
      # (optional)
      token-url: "example-value"

      # GitHub Actions secret name that holds the OAuth client ID. Required when
      # strategy is 'oauth-client-credentials'.
      # (optional)
      client-id: "example-value"

      # GitHub Actions secret name that holds the OAuth client secret. Required when
      # strategy is 'oauth-client-credentials'.
      # (optional)
      client-secret: "example-value"

      # JSON field name in the token response that contains the access token. Defaults
      # to 'access_token'.
      # (optional)
      token-field: "example-value"

      # HTTP header name to inject the API key or token into (e.g. 'api-key',
      # 'x-api-key'). Required when strategy is not 'bearer'.
      # (optional)
      header-name: "example-value"

    # Request shaping configuration for non-standard provider URL and body
    # transformations
    # (optional)
    request:
      # URL path template with {model} and other variable placeholders (e.g.
      # '/openai/deployments/{model}/chat/completions')
      # (optional)
      path-template: "example-value"

      # Static or template query-parameter values appended to every request
      # (optional)
      query:
        {}

      # Key/value pairs injected into the JSON request body before sending
      # (optional)
      body-inject:
        {}

  # When true, disables automatic loading of context and custom instructions by the
  # AI engine. The engine-specific flag depends on the engine: copilot uses
  # --no-custom-instructions, claude uses --bare, codex uses --no-system-prompt,
  # gemini sets GEMINI_SYSTEM_MD=/dev/null. Defaults to false.
  # (optional)
  bare: true

# Format 4: Engine definition: full declarative metadata for a named engine entry
# (used in builtin engine shared workflow files such as @builtin:engines/*.md)
engine:
  # Unique engine identifier (e.g. 'copilot', 'claude', 'codex', 'gemini', 'pi')
  id: "example-value"

  # Default CLI version applied when a workflow references this engine without
  # specifying engine.version
  # (optional)
  version: null

  # Human-readable display name for the engine
  display-name: "example-value"

  # Human-readable description of the engine
  # (optional)
  description: "Description of the workflow"

  # Marks the engine as experimental so compiled workflows can surface an explicit
  # warning.
  # (optional)
  experimental: true

  # Whether the engine supports MCP. When false, the compiler automatically enables
  # gh-proxy and cli-proxy and rejects attempts to disable either proxy.
  # (optional)
  mcp: true

  # Runtime adapter identifier. Maps to the CodingAgentEngine registered in the
  # engine registry. Defaults to id when omitted.
  # (optional)
  runtime-id: "example-value"

  # Built-in engine used to run threat detection for workflows using this engine.
  # Required for custom engines because the threat detection analyzer only supports
  # built-in engines; defaults to 'copilot' when omitted.
  # (optional)
  detection-engine: "copilot"

  # Optional engine-specific secret bindings for behavior-defined engines.
  # (optional)
  auth: []
    # Array items:
      # Logical authentication role name.
      role: "example-value"

      # GitHub Actions secret name exposed to the engine runtime.
      secret: "example-value"

  # Provider metadata for the engine
  # (optional)
  provider:
    # Provider name (e.g. 'anthropic', 'github', 'google', 'openai')
    # (optional)
    name: "My Workflow"

    # Default authentication configuration for the provider
    # (optional)
    auth:
      # Name of the GitHub Actions secret that contains the API key
      # (optional)
      secret: "example-value"

      # Authentication strategy
      # (optional)
      strategy: "api-key"

      # OAuth 2.0 token endpoint URL
      # (optional)
      token-url: "example-value"

      # GitHub Actions secret name for the OAuth client ID
      # (optional)
      client-id: "example-value"

      # GitHub Actions secret name for the OAuth client secret
      # (optional)
      client-secret: "example-value"

      # JSON field name in the token response containing the access token
      # (optional)
      token-field: "example-value"

      # HTTP header name to inject the API key or token into
      # (optional)
      header-name: "example-value"

    # Request shaping configuration
    # (optional)
    request:
      # URL path template with variable placeholders
      # (optional)
      path-template: "example-value"

      # Static query parameters
      # (optional)
      query:
        {}

      # Key/value pairs injected into the JSON request body
      # (optional)
      body-inject:
        {}

  # Model selection configuration for the engine
  # (optional)
  models:
    # Default model identifier
    # (optional)
    default: "example-value"

    # List of supported model identifiers
    # (optional)
    supported: []
      # Array of strings

  # Additional engine-specific options
  # (optional)
  options:
    {}

  # Declarative custom-engine runtime behavior used to materialize a shared CLI
  # engine from frontmatter.
  # (optional)
  behaviors:
    # Secret resolution mode for the engine (for example 'universal-llm-consumer').
    # (optional)
    secret-strategy: "example-value"

    # Secret or env var keys the engine accepts through engine.env.
    # (optional)
    supported-env-var-keys: []
      # Array of strings

    # (optional)
    capabilities:
      # (optional)
      tools-allowlist: true

      # (optional)
      max-turns: true

      # (optional)
      web-search: true

      # (optional)
      max-continuations: true

      # (optional)
      native-agent-file: true

      # (optional)
      bare-mode: true

      # (optional)
      bash-command-allowlist: true

    # (optional)
    manifest:
      # (optional)
      files: []
        # Array of strings

      # (optional)
      path-prefixes: []
        # Array of strings

    # Declarative default network requirements for the engine. 'defaults' are always
    # allowed; the domain in 'provider-domains' matching the model's provider prefix
    # is added on top.
    # (optional)
    network:
      # Domains always required by the engine CLI (for example package registries and
      # telemetry endpoints).
      # (optional)
      defaults: []
        # Array of strings

      # Maps a model provider prefix (the part before '/' in 'provider/model') to the
      # API domain required for that provider.
      # (optional)
      provider-domains:
        {}

      # Provider key used to resolve a provider domain when the model carries no
      # 'provider/' prefix.
      # (optional)
      default-provider: "example-value"

    # (optional)
    installation:
      # (optional)
      package-manager: "example-value"

      # (optional)
      package-name: "example-value"

      # (optional)
      version: "example-value"

      # (optional)
      step-name: "example-value"

      # (optional)
      binary-name: "example-value"

      # (optional)
      include-node-setup: true

      # (optional)
      post-install-scripts: true

      # (optional)
      cooldown: true

      # (optional)
      verify-command: "example-value"

      # (optional)
      verify-step-name: "example-value"

      # (optional)
      docs-url: "example-value"

    # ⚠️ Experimental. Declares Agent Plugins (https://agent-plugins.org) support for
    # the engine. Plugins are checked out at their pinned commit SHA, then staged in
    # 'directory' and/or installed with the engine CLI using 'install-args'.
    # (optional)
    plugins:
      # Folder the engine scans for plugins. Workspace-relative (for example
      # '.kiro/powers') or home-relative (for example '~/.cursor/plugins/local').
      # (optional)
      directory: "example-value"

      # CLI executable used with 'install-args'. Defaults to the execution command name.
      # (optional)
      command-name: "example-value"

      # CLI arguments placed before the local plugin path (for example ['plugin',
      # 'install']).
      # (optional)
      install-args: []
        # Array of strings

    # (optional)
    config-file:
      # (optional)
      path: "example-value"

      # (optional)
      step-name: "example-value"

      # (optional)
      content: "example-value"

      # (optional)
      merge-strategy: "example-value"

    # (optional)
    execution:
      # (optional)
      command-name: "example-value"

      # (optional)
      args: []
        # Array of strings

      # (optional)
      step-name: "example-value"

      # (optional)
      model-env-var: "example-value"

      # (optional)
      model-env-provider-prefix: "example-value"

      # (optional)
      model-flag: "example-value"

      # (optional)
      mcp-config-env-var: "example-value"

      # (optional)
      mcp-config-flag: "example-value"

      # (optional)
      write-timestamp: true

      # (optional)
      provider-env-mode: "example-value"

      # Additional static environment variables injected into the execution step. Values
      # are rendered verbatim and must not contain secrets.
      # (optional)
      env:
        {}

    # (optional)
    mcp:
      # (optional)
      config-path: "example-value"

      # JavaScript source of a Node.js script that converts the MCP gateway's raw output
      # configuration into the format expected by this engine. When set, the script is
      # written to ${RUNNER_TEMP}/gh-aw/actions/<engine-id>_mcp_config_adapter.cjs
      # before the MCP gateway starts, and start_mcp_gateway.cjs executes it (instead of
      # a built-in per-engine converter) once the gateway has produced its output.
      # (optional)
      config-adapter: "example-value"

    # JavaScript source of a Node.js harness that spawns the engine CLI. When set, the
    # script is written to ${RUNNER_TEMP}/gh-aw/actions/<engine-id>_harness.cjs before
    # execution and the engine is launched as: node <harness-path> <command-name>
    # [args...]. The harness reads process.env.GH_AW_PROMPT for the prompt-file path
    # and, when the AWF firewall is active, process.env.AWF_REFLECT_ENABLED is set to
    # '1' so the harness can read /reflect data to dynamically configure the engine
    # CLI.
    # (optional)
    harness-script: "example-value"

    # JavaScript source of a log-parser function for the engine. When set, the script
    # is written to ${RUNNER_TEMP}/gh-aw/actions/<engine-id>_log_parser.cjs and used
    # in the post-agent log-parsing step. The script must define a
    # parseLog(logContent) function (not export it) that returns {markdown,
    # logEntries, mcpFailures, maxTurnsHit}. A createEngineLogParser wrapper from
    # log_parser_shared.cjs is automatically appended so the author only provides the
    # parsing function; the wrapper handles exports and bootstrap. This enables
    # behavior-defined engines (e.g. crush, opencode, goose, aider) to produce
    # normalized events files like built-in engine parsers.
    # (optional)
    log-parser: "example-value"

# Format 5: MCP gateway configuration for shared workflows. Declares engine.mcp
# settings (tool-timeout, session-timeout) that consumers inherit during import
# without specifying an engine identifier. The engine is always inherited from the
# importing workflow.
engine:
  # Engine-level MCP gateway configuration. Settings here apply to the MCP gateway
  # used by this engine.
  mcp:
    # Session timeout for MCP gateway sessions as a Go duration string (e.g. "30m",
    # "4h", "24h"). Must be at least 5m (no upper bound). Omitted or empty uses the
    # effective gateway default (precedence: this field > MCP_GATEWAY_SESSION_TIMEOUT
    # env var > built-in default 6h).
    # (optional)
    session-timeout: "example-value"

    # Timeout for individual MCP tool calls as a Go duration string (e.g. "30s", "2m",
    # "10m"). Must be between 10s and 600s inclusive. Omitted or empty uses the
    # gateway built-in default (60s). Use a higher value for slow MCP backends such as
    # full-text search over large indexes.
    # (optional)
    tool-timeout: "example-value"

# Format 6: Engine object with only a model preference (no engine.id). Allows
# workflow authors to express a model-size hint (e.g. 'small', 'large') without
# committing to a specific engine. The runtime selects an appropriate engine using
# its default, and the model preference is applied to it.
engine:
  # Model preference or size category (e.g. 'small', 'large', 'gpt-4.1'). Applied to
  # the default engine when engine.id is not specified. Overrides the top-level
  # 'model' field.
  model: "example-value"

# AWF turn cap (`max_turns`) applied consistently across all agentic engines.
# Supports GitHub Actions expressions (e.g. '${{ inputs.max-turns }}') for
# reusable workflow_call workflows.
# (optional)
# Accepted formats:

# Format 1: integer
max-turns: 1

# Format 2: GitHub Actions expression that resolves to an integer at runtime
max-turns: "example-value"

# Copilot SDK safeguard threshold for repeated tool denials before stopping
# inference. Defaults to 5 when omitted. Supports GitHub Actions expressions (for
# example, '${{ inputs.max-tool-denials }}'). Supported only with engine 'copilot'
# and engine.copilot-sdk: true.
# (optional)
# Accepted formats:

# Format 1: integer
max-tool-denials: 1

# Format 2: GitHub Actions expression that resolves to an integer at runtime
max-tool-denials: "example-value"

# Per-run AI Credits budget control for firewall cost enforcement. Enabled by
# default at 1000 (1k) when omitted. Set to -1 to disable both budget enforcement
# and token steering. Supports GitHub Actions expressions.
# (optional)
# Accepted formats:

# Format 1: Configuration object

# Format 2: integer
max-ai-credits: 1

# Format 3: string
max-ai-credits: "example-value"

# Maximum number of consecutive AWF cache misses allowed before the API proxy
# blocks further requests. Maps to `apiProxy.maxCacheMisses`. Precedence:
# frontmatter value → `GH_AW_DEFAULT_MAX_TURN_CACHE_MISSES` env override →
# built-in default `5`.
# (optional)
max-turn-cache-misses: 1

# 24-hour AI Credits guardrail for runs triggered by the same user. Omit the field
# to leave the guardrail disabled. Supports GitHub Actions expressions.
# (optional)
# Accepted formats:

# Format 1: Configuration object

# Format 2: integer
max-daily-ai-credits: 1

# Format 3: string
max-daily-ai-credits: "example-value"

# Format 4: Object form: specify a 'value' (the credit limit) and an optional
# 'github-app' to mint a dedicated token for the guardrail API calls.
max-daily-ai-credits:
  # The maximum AI Credits budget (positive integer, K/M suffix, or expression). To
  # disable the guardrail, use the scalar form `max-daily-ai-credits: -1` instead.
  # Accepted formats:

  # Format 1: integer
  value: 1

  # Format 2: Positive integer as a string (supports optional K/M suffix) or a
  # GitHub Actions expression.
  value: "example-value"

  # Optional GitHub App to mint the token used to compute the daily AI Credits
  # total. Avoids depleting API credits from other apps.
  # (optional)
  github-app:
    # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
    # }}').
    # (optional)
    app-id: "example-value"

    # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
    # token.
    # (optional)
    client-id: "example-value"

    # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
    # mint a GitHub App token.
    # (optional)
    private-key: "example-value"

    # If true, skip token minting when client-id/private-key resolve to empty strings
    # at runtime. Defaults to false.
    # (optional)
    ignore-if-missing: true

    # Optional owner of the GitHub App installation (defaults to current repository
    # owner if not specified)
    # (optional)
    owner: "example-value"

    # Optional list of repositories to grant access to (defaults to current repository
    # if not specified)
    # (optional)
    repositories: []
      # Array of strings

    # Optional extra GitHub App-only permissions to merge into the minted token. Takes
    # effect for tools.github.github-app and safe-outputs.github-app; ignored in
    # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
    # scopes (e.g. members, organization-administration) not expressible via standard
    # handler declarations.
    # (optional)
    permissions:
      # Permission level for repository administration (read/none; "write" is rejected
      # by the compiler). GitHub App-only permission for repository administration.
      # (optional)
      administration: "read"

      # Permission level for Codespaces (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      codespaces: "read"

      # Permission level for Codespaces lifecycle administration (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      codespaces-lifecycle-admin: "read"

      # Permission level for Codespaces metadata (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      codespaces-metadata: "read"

      # Permission level for user email addresses (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      email-addresses: "read"

      # Permission level for repository environments (read/none; "write" is rejected by
      # the compiler). GitHub App-only permission.
      # (optional)
      environments: "read"

      # Permission level for git signing (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      git-signing: "read"

      # Permission level for organization members (read/none; "write" is rejected by the
      # compiler). Required for org team membership API calls.
      # (optional)
      members: "read"

      # Permission level for organization administration (read/none; "write" is rejected
      # by the compiler). GitHub App-only permission.
      # (optional)
      organization-administration: "read"

      # Permission level for organization announcement banners (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-announcement-banners: "read"

      # Permission level for organization Codespaces (read/none; "write" is rejected by
      # the compiler). GitHub App-only permission.
      # (optional)
      organization-codespaces: "read"

      # Permission level for organization Copilot (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      organization-copilot: "read"

      # Permission level for organization custom org roles (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-custom-org-roles: "read"

      # Permission level for organization custom properties (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-custom-properties: "read"

      # Permission level for organization custom repository roles (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-custom-repository-roles: "read"

      # Permission level for organization events (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      organization-events: "read"

      # Permission level for organization webhooks (read/none; "write" is rejected by
      # the compiler). GitHub App-only permission.
      # (optional)
      organization-hooks: "read"

      # Permission level for organization members management (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-members: "read"

      # Permission level for organization packages (read/none; "write" is rejected by
      # the compiler). GitHub App-only permission.
      # (optional)
      organization-packages: "read"

      # Permission level for organization personal access token requests (read/none;
      # "write" is rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-personal-access-token-requests: "read"

      # Permission level for organization personal access tokens (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-personal-access-tokens: "read"

      # Permission level for organization plan (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      organization-plan: "read"

      # Permission level for organization self-hosted runners (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-self-hosted-runners: "read"

      # Permission level for organization user blocking (read/none; "write" is rejected
      # by the compiler). GitHub App-only permission.
      # (optional)
      organization-user-blocking: "read"

      # Permission level for repository custom properties (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      repository-custom-properties: "read"

      # Permission level for secret scanning alerts (read/none). Forwarded as
      # permission-secret-scanning-alerts input for actions/create-github-app-token.
      # (optional)
      secret-scanning-alerts: "read"

      # Permission level for repository webhooks (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      repository-hooks: "read"

      # Permission level for single file access (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      single-file: "read"

      # Permission level for team discussions (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      team-discussions: "read"

      # Permission level for Dependabot vulnerability alerts (read/none; "write" is
      # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
      # with a GitHub App, forwarded as permission-vulnerability-alerts input.
      # (optional)
      vulnerability-alerts: "read"

      # Permission level for GitHub Actions workflow files (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      workflows: "read"

# MCP server definitions
# (optional)
mcp-servers:
  {}

# Tools and MCP (Model Context Protocol) servers available to the AI engine for
# GitHub API access, browser automation, file editing, and more
# (optional)
tools:
  # GitHub API tools for repository operations (issues, pull requests, content
  # management)
  # (optional)
  # Accepted formats:

  # Format 1: Empty GitHub tool configuration (enables all read-only GitHub API
  # functions)
  github: null

  # Format 2: Boolean to explicitly enable (true) or disable (false) the GitHub MCP
  # server. When set to false, the GitHub MCP server is not mounted.
  github: true

  # Format 3: Simple GitHub tool configuration (enables all GitHub API functions)
  github: "example-value"

  # Format 4: GitHub tools object configuration with restricted function access
  github:
    # List of allowed GitHub API functions. Each entry can be a string tool name
    # (e.g., 'issue_read') or an object with per-tool limits (e.g., {name:
    # 'issue_read', max-calls: 1})
    # (optional)
    allowed: []

    # GitHub access mode. Prefer 'gh-proxy' for better performance (uses
    # pre-authenticated gh CLI prompt guidance). Legacy MCP transport values 'local'
    # and 'remote' are accepted for backward compatibility and use GitHub MCP server
    # prompt guidance.
    # (optional)
    mode: "gh-proxy"

    # GitHub MCP transport type: 'local' (Docker-based, default) or 'remote' (hosted
    # at api.githubcopilot.com)
    # (optional)
    type: "local"

    # Optional version specification for the GitHub MCP server (used with 'local'
    # type). Can be a string (e.g., 'v1.0.0', 'latest') or number (e.g., 20, 3.11).
    # Numeric values are automatically converted to strings at runtime.
    # (optional)
    version: null

    # Optional additional arguments to append to the generated MCP server command
    # (used with 'local' type)
    # (optional)
    args: []
      # Array of strings

    # Enable read-only mode to restrict GitHub MCP server to read-only operations only
    # (optional)
    read-only: true

    # Enable lockdown mode to limit content surfaced from public repositories (only
    # items authored by users with push access). Default: false
    # (optional)
    lockdown: true

    # Controls DIFC proxy injection for pre-agent gh CLI steps when guard policies
    # (min-integrity) are configured. Default: true (enabled). Set to false to disable
    # proxy injection and rely solely on MCP gateway-level filtering.
    # (optional)
    integrity-proxy: true

    # Optional custom GitHub token (e.g., '${{ secrets.CUSTOM_PAT }}'). For 'remote'
    # type, defaults to GH_AW_GITHUB_TOKEN if not specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # GitHub MCP server toolset name(s) to enable. Accepts a single toolset name
    # (string) or an array of toolset names.
    # (optional)
    # Accepted formats:

    # Format 1: A single GitHub MCP server toolset name (shorthand for a one-element
    # array)
    toolsets: "all"

    # Format 2: Array of GitHub MCP server toolset names to enable specific groups of
    # GitHub API functionalities
    toolsets: []
      # Array items: A GitHub MCP server toolset name that enables a specific group of
      # GitHub API functionalities.

    # Volume mounts for the containerized GitHub MCP server (format:
    # 'host:container:mode' where mode is 'ro' for read-only or 'rw' for read-write).
    # Applies to local mode only. Example: '/data:/data:ro'
    # (optional)
    mounts: []
      # Array of Mount specification in format 'host:container:mode'

    # Guard policy: repository access configuration. Restricts which repositories the
    # agent can access. Use 'all' to allow all repos, 'public' for public repositories
    # only, '${{ github.repository }}' for the current repository, or an array of
    # repository patterns (e.g., 'owner/repo', 'owner/*', 'owner/prefix*').
    # (optional)
    # Accepted formats:

    # Format 1: Allow access to all repositories ('all'), only public repositories
    # ('public'), or the current repository ('${{ github.repository }}')
    allowed-repos: "all"

    # Format 2: Allow access to specific repositories using patterns (e.g.,
    # 'owner/repo', 'owner/*', 'owner/prefix*')
    allowed-repos: []
      # Array items: Repository pattern in the format 'owner/repo', 'owner/*' (all repos
      # under owner), or 'owner/prefix*' (repos with name prefix)

    # Guard policy: minimum required integrity level for repository access. Restricts
    # the agent to users with at least the specified permission level.
    # (optional)
    min-integrity: "none"

    # Guard policy: GitHub usernames whose content is unconditionally blocked. Items
    # from these users receive 'blocked' integrity (below 'none') and are always
    # denied, even when 'min-integrity' is 'none'. Cannot be overridden by
    # 'approval-labels'. Requires 'min-integrity' to be set. Accepts an array of
    # usernames, a comma-separated string, a newline-separated string, or a GitHub
    # Actions expression (e.g. '${{ vars.BLOCKED_USERS }}').
    # (optional)
    # Accepted formats:

    # Format 1: Array of GitHub usernames to block
    blocked-users: []
      # Array items: GitHub username to block

    # Format 2: Comma- or newline-separated list of usernames, or a GitHub Actions
    # expression resolving to such a list (e.g. '${{ vars.BLOCKED_USERS }}')
    blocked-users: "example-value"

    # Guard policy: GitHub usernames whose content is elevated to 'approved' integrity
    # regardless of author_association. Allows specific external contributors to
    # bypass 'min-integrity' checks without lowering the global policy. Precedence:
    # blocked-users > trusted-users > approval-labels > author_association. Requires
    # 'min-integrity' to be set. Accepts an array of usernames, a comma-separated
    # string, a newline-separated string, or a GitHub Actions expression (e.g. '${{
    # vars.TRUSTED_USERS }}').
    # (optional)
    # Accepted formats:

    # Format 1: Array of GitHub usernames to trust
    trusted-users: []
      # Array items: GitHub username to elevate to approved integrity

    # Format 2: Comma- or newline-separated list of usernames, or a GitHub Actions
    # expression resolving to such a list (e.g. '${{ vars.TRUSTED_USERS }}')
    trusted-users: "example-value"

    # Guard policy: GitHub label names that promote a content item's effective
    # integrity to 'approved' when present. Enables human-review gates where a
    # maintainer labels an item to allow it through. Uses max(base, approved) so it
    # never lowers integrity. Does not override 'blocked-users'. Requires
    # 'min-integrity' to be set. Accepts an array of label names, a comma-separated
    # string, a newline-separated string, or a GitHub Actions expression (e.g. '${{
    # vars.APPROVAL_LABELS }}').
    # (optional)
    # Accepted formats:

    # Format 1: Array of GitHub label names
    approval-labels: []
      # Array items: GitHub label name

    # Format 2: Comma- or newline-separated list of label names, or a GitHub Actions
    # expression resolving to such a list (e.g. '${{ vars.APPROVAL_LABELS }}')
    approval-labels: "example-value"

    # Guard policy: GitHub reaction types that promote a content item's integrity to
    # 'approved' when added by maintainers. Only enforced in proxy mode (DIFC/CLI
    # proxy); ignored in MCP gateway mode because reaction authors cannot be
    # identified. Optional; defaults to ["THUMBS_UP", "HEART"] when the
    # integrity-reactions feature flag is enabled. Requires 'min-integrity' to be set
    # and MCPG >= v0.2.18.
    # (optional)
    endorsement-reactions: []
      # Array of GitHub ReactionContent enum value

    # Guard policy: GitHub reaction types that demote content integrity when added by
    # maintainers. Only enforced in proxy mode (DIFC/CLI proxy); ignored in MCP
    # gateway mode because reaction authors cannot be identified. Optional; defaults
    # to ["THUMBS_DOWN", "CONFUSED"] when the integrity-reactions feature flag is
    # enabled. Disapproval overrides endorsement (safe default). Requires
    # 'min-integrity' to be set and MCPG >= v0.2.18.
    # (optional)
    disapproval-reactions: []
      # Array of GitHub ReactionContent enum value

    # Guard policy: integrity level assigned when a disapproval reaction is present.
    # Optional, defaults to 'none'. Requires the 'integrity-reactions' feature flag
    # and MCPG >= v0.2.18.
    # (optional)
    disapproval-integrity: "none"

    # Guard policy: minimum integrity level required for an endorser (reactor) to
    # promote content. Optional, defaults to 'approved'. Requires the
    # 'integrity-reactions' feature flag and MCPG >= v0.2.18.
    # (optional)
    endorser-min-integrity: "unapproved"

    # GitHub App configuration for token minting. When configured, a GitHub App
    # installation access token is minted at workflow start and used instead of the
    # default token. This token overrides any custom github-token setting and provides
    # fine-grained permissions matching the agent job requirements.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

    # Opts out of cross-visibility protections for private-to-public data flows. Set
    # to 'allow' for a blanket opt-out (disables forcePublicRepos and default
    # sink-visibility enforcement), or provide an array of MCP server IDs to exempt
    # only those servers from default sink-visibility enforcement. Incompatible with
    # strict mode when set to 'allow'. See MCP Gateway Specification Section 10.9.
    # (optional)
    # Accepted formats:

    # Format 1: Blanket opt-out: disables forcePublicRepos and sink-visibility
    # enforcement for all servers. Not allowed in strict mode.
    private-to-public-flows: "allow"

    # Format 2: Selective exemption: disables default sink-visibility enforcement for
    # the listed MCP server IDs only. Compatible with strict mode.
    private-to-public-flows: []
      # Array items: MCP server ID to exempt from sink-visibility enforcement

    # Comma-separated list of GitHub MCP server feature flags to enable. Forwarded as
    # GITHUB_FEATURES (Docker/local) or X-MCP-Features (remote). When omitted,
    # 'fields_param' is enabled by default for server v1.6.0 and later. Set to an
    # empty string to disable all feature flags.
    # (optional)
    features: "example-value"

  # Linear tools provided by Linear's official hosted MCP server
  # (optional)
  # Accepted formats:

  # Format 1: Enable Linear using the well-known LINEAR_API_KEY secret
  linear: null

  # Format 2: object
  linear:
    # Optional Linear API key or OAuth access token secret reference. Defaults to ${{
    # secrets.LINEAR_API_KEY }}.
    # (optional)
    token: "${{ secrets.LINEAR_API_KEY }}"

    # Linear MCP toolset name(s) to enable. Toolsets are expanded to gateway-enforced
    # allowed tools.
    # (optional)
    # Accepted formats:

    # Format 1: A Linear MCP toolset name that enables a related group of read-only
    # Linear tools.
    toolsets: "all"

    # Format 2: Array of Linear MCP toolset names
    toolsets: ["issues","projects"]
      # Array items: A Linear MCP toolset name that enables a related group of read-only
      # Linear tools.

    # List of allowed Linear MCP tool names or wildcard patterns. When toolsets are
    # set, every pattern must match a tool in those toolsets.
    # (optional)
    allowed: ["*"]
      # Array of strings

    # Whether failure to connect to Linear should fail MCP gateway startup. Defaults
    # to true.
    # (optional)
    required: true
  # Jira tools provided by Atlassian's remote Rovo MCP service. This integration
  # supports non-interactive CI/CD authentication only.
  # (optional)
  # Accepted formats:

  # Format 1: Disable the Jira integration, including a Jira configuration inherited
  # from an import.
  jira: false

  # Format 2: object
  jira:
    # Atlassian Rovo MCP endpoint. Defaults to the official hosted endpoint.
    # (optional)
    url: "example-value"

    # Approved read-only Jira MCP tools the agent may call. Required; all-tools access
    # and write-capable tools are not supported.
    allowed: []
      # Array of strings

    # Non-interactive authentication for GitHub Actions.
    # Accepted formats:

    # Format 1: object
    auth:
      type: null

      # Atlassian service account API key, sent with the HTTP bearer scheme.
      token: "example-value"

    # Format 2: object
    auth:
      type: null

      # Atlassian account email stored as a GitHub Actions secret.
      email: "example-value"

      # Atlassian API token stored as a GitHub Actions secret.
      token: "example-value"

  # Bash shell command execution tool. Supports wildcards: '*' (all commands),
  # 'command *' (command with any args, e.g., 'date *', 'echo *'). Default safe
  # commands: echo, ls, pwd, cat, head, tail, grep, wc, sort, uniq, date.
  # (optional)
  # Accepted formats:

  # Format 1: Enable bash tool with all shell commands allowed (security
  # consideration: use restricted list in production)
  bash: null

  # Format 2: Enable bash tool - true allows all commands (equivalent to ['*']),
  # false disables the tool
  bash: true

  # Format 3: List of allowed commands and patterns. Wildcards: '*' allows all
  # commands, 'command *' allows command with any args (e.g., 'date *', 'echo *').
  bash: []
    # Array items: Command or pattern: 'echo' (exact match), 'echo *' (command with
    # any args)

  # Web content fetching tool for downloading web pages and API responses (subject
  # to network permissions)
  # (optional)
  # Accepted formats:

  # Format 1: Enable web fetch tool with default configuration
  web-fetch: null

  # Format 2: Web fetch tool configuration object
  web-fetch:
    {}

  # Web search tool for performing internet searches and retrieving search results
  # (subject to network permissions)
  # (optional)
  # Accepted formats:

  # Format 1: Enable web search tool with default configuration
  web-search: null

  # Format 2: Web search tool configuration object
  web-search:
    {}

  # File editing tool for reading, creating, and modifying files in the repository
  # (optional)
  # Accepted formats:

  # Format 1: Enable edit tool
  edit: null

  # Format 2: Boolean to explicitly enable (true) or disable (false) the edit tool.
  edit: true

  # Format 3: Edit tool configuration object
  edit:
    {}

  # Playwright CLI browser automation tool for web scraping, testing, and UI
  # interactions
  # (optional)
  # Accepted formats:

  # Format 1: Enable Playwright tool with default settings
  playwright: null

  # Format 2: Playwright CLI configuration
  playwright:
    # Optional @playwright/cli npm package version pin. Omit to use the default
    # version.
    # (optional)
    version: null

    # Removed: legacy MCP-mode argument list. Accepted here only so the compiler can
    # reject 'mode: mcp' configurations with actionable migration guidance instead of
    # an unhelpful schema error.
    # (optional)
    args: []
      # Array of strings

    # Integration mode. Only 'cli' is supported. The compiler rejects the removed
    # 'mcp' value with migration guidance. Must be a literal value; GitHub Actions
    # expressions are rejected.
    # (optional)
    # Accepted formats:

    # Format 1: string
    mode: "cli"

    # Format 2: Not allowed at runtime: mode must be the literal 'cli' value, not a
    # GitHub Actions expression.
    mode: "example-value"

    # Browsers to provision before the agent starts. Defaults to Chromium. Chrome and
    # Chrome for Testing are accepted as aliases for Chromium.
    # (optional)
    browsers: []
      # Array of strings

  # GitHub Agentic Workflows MCP server for workflow introspection and analysis.
  # Provides tools for checking status, compiling workflows, downloading logs, and
  # auditing runs.
  # (optional)
  # Accepted formats:

  # Format 1: Enable agentic-workflows tool with default settings
  agentic-workflows: true

  # Format 2: Enable agentic-workflows tool with default settings (same as true)
  agentic-workflows: null

  # Cache memory MCP configuration for persistent memory storage
  # (optional)
  # Accepted formats:

  # Format 1: Enable cache-memory with default settings
  cache-memory: true

  # Format 2: Enable cache-memory with default settings (same as true)
  cache-memory: null

  # Format 3: Cache-memory configuration object
  cache-memory:
    # Custom cache key for memory MCP data (restore keys are auto-generated by
    # splitting on '-')
    # (optional)
    key: "example-value"

    # Optional description for the cache that will be shown in the agent prompt
    # (optional)
    description: "Description of the workflow"

    # Number of days to retain uploaded artifacts (1-90 days, default: repository
    # setting)
    # (optional)
    retention-days: 1

    # If true, only restore the cache without saving it back. Uses
    # actions/cache/restore instead of actions/cache. No artifact upload step will be
    # generated.
    # (optional)
    restore-only: true

    # Cache restore key scope: 'workflow' (default, only restores from same workflow)
    # or 'repo' (restores from any workflow in the repository). Use 'repo' with
    # caution as it allows cross-workflow cache sharing.
    # (optional)
    scope: "workflow"

    # List of allowed file extensions (e.g., [".json", ".txt"]). Default: [".json",
    # ".jsonl", ".txt", ".md", ".csv"]
    # (optional)
    allowed-extensions: []
      # Array of strings

    # Custom domain validation hook for this cache-memory entry
    # (optional)
    validation:
      # JavaScript validator body that runs over the complete cache-memory directory
      # before persistence. Throw, return false, or exit nonzero to reject the update.
      script: "example-value"

      # Maximum validator runtime in minutes (default: 1, max: 5)
      # (optional)
      timeout-minutes: 1

  # Format 4: Array of cache-memory configurations for multiple caches
  cache-memory: [{"id":"default","key":"memory-default"},{"id":"session","key":"memory-session"}]
    # Array items: object

  # Private-preview GitHub Drives configuration. Do not configure unless GitHub has
  # explicitly enrolled the repository in the private preview.
  # (optional)
  # Accepted formats:

  # Format 1: Default configuration for enrolled preview repositories
  drive-memory: true

  # Format 2: Default configuration for enrolled preview repositories (same as true)
  drive-memory: null

  # Format 3: Configuration for an enrolled preview repository
  drive-memory:
    # GitHub Drive name (default: 'default')
    # (optional)
    drive-name: "example-value"

    # Optional description shown in the agent prompt
    # (optional)
    description: "Description of the workflow"

    # Drive size used when creating the drive, as a number with an optional K, M, G,
    # or T suffix (case-insensitive; normalized to upper case; suggested: '100M';
    # ignored for an existing drive)
    # (optional)
    disk-size: "example-value"

    # Eagerly fetch existing drive contents after mounting (default: false)
    # (optional)
    prefetch: true

    # Mount the drive without committing changes
    # (optional)
    restore-only: true

    # List of allowed file extensions. By default, all extensions are allowed.
    # (optional)
    allowed-extensions: []
      # Array of strings

    # (optional)
    validation:
      # JavaScript validator body that runs over the drive-memory directory before
      # persistence
      script: "example-value"

      # Maximum validator runtime in minutes
      # (optional)
      timeout-minutes: 1

  # Format 4: Configurations for multiple drives in an enrolled preview repository
  drive-memory: []
    # Array items: object

  # Comment memory configuration for managed comment persistence
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for persisting memory in a managed issue/PR comment.
  # Memory is materialized to files for agent editing and synchronized back after
  # execution.
  comment-memory:
    # Maximum number of comment_memory updates to process (default: 1). Supports
    # integer or GitHub Actions expression.
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target for comment memory: 'triggering' (default), '*' (current issue/PR), or
    # explicit issue/PR number
    # (optional)
    target: "example-value"

    # Target repository in format 'owner/repo' for cross-repository memory storage.
    # (optional)
    target-repo: "example-value"

    # Additional repositories in format 'owner/repo' allowed for comment memory
    # operations.
    # (optional)
    allowed-repos: []
      # Array of strings

    # Default memory identifier when output items omit memory_id.
    # (optional)
    memory-id: "example-value"

    # Controls whether AI-generated footer is added to the managed comment. Defaults
    # to true.
    # (optional)
    footer: true

    # GitHub token to use for comment-memory operations. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Enable issue-intent metadata support (rationale/confidence/suggest) for this
    # output type.
    # (optional)
    issue-intent: true

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

  # Format 2: Enable (true) or disable (false) comment-memory.
  comment-memory: true

  # Format 3: Explicitly disable comment-memory
  comment-memory: null

  # Timeout in seconds for tool/MCP server operations. Applies to all tools and MCP
  # servers if supported by the engine. Default: 60 seconds (for both Claude and
  # Codex). Supports GitHub Actions expressions for reusable workflow_call
  # workflows.
  # (optional)
  # Accepted formats:

  # Format 1: integer
  timeout: 1

  # Format 2: GitHub Actions expression (e.g. '${{ inputs.tool-timeout }}')
  timeout: "example-value"

  # Timeout in seconds for MCP server startup. Applies to MCP server initialization
  # if supported by the engine. Default: 120 seconds. Supports GitHub Actions
  # expressions for reusable workflow_call workflows.
  # (optional)
  # Accepted formats:

  # Format 1: integer
  startup-timeout: 1

  # Format 2: GitHub Actions expression (e.g. '${{ inputs.startup-timeout }}')
  startup-timeout: "example-value"

  # When true, each user-facing MCP server is mounted as a standalone CLI tool on
  # PATH. The agent can then call MCP servers via shell commands (e.g. 'github
  # issue_read --method get ...'). CLI-mounted servers remain in the MCP gateway
  # config so their containers can start, and are removed only from the agent's
  # final config during convert_gateway_config_*.sh processing. Default: false.
  # (optional)
  cli-proxy: true

  # Repo memory configuration for git-based persistent storage
  # (optional)
  # Accepted formats:

  # Format 1: Enable repo-memory with default settings
  repo-memory: true

  # Format 2: Enable repo-memory with default settings (same as true)
  repo-memory: null

  # Format 3: Repo-memory configuration object
  repo-memory:
    # Branch prefix for memory storage (default: 'memory'). Must be 4-32 characters,
    # alphanumeric with hyphens/underscores, and cannot be 'copilot'. Branch will be
    # named {branch-prefix}/{id}
    # (optional)
    branch-prefix: "example-value"

    # Target repository for memory storage (default: current repository). Format:
    # owner/repo
    # (optional)
    target-repo: "example-value"

    # Git branch name for memory storage (default: {branch-prefix}/default or
    # memory/default if branch-prefix not set)
    # (optional)
    branch-name: "example-value"

    # Glob patterns for files to include in repository memory. Supports wildcards
    # (e.g., '**/*.md', 'docs/**/*.json') to filter cached files.
    # (optional)
    # Accepted formats:

    # Format 1: Single file glob pattern for allowed files
    file-glob: "example-value"

    # Format 2: Array of file glob patterns for allowed files
    file-glob: []
      # Array items: string

    # Maximum size per file in bytes (default: 102400 = 100KB)
    # (optional)
    max-file-size: 1

    # Maximum file count per commit (default: 100)
    # (optional)
    max-file-count: 1

    # Maximum total patch size in bytes (default: 10240 = 10KB, max: 1048576 = 1MB).
    # The total size of the git diff must not exceed this value.
    # (optional)
    max-patch-size: 1

    # Optional description for the memory that will be shown in the agent prompt
    # (optional)
    description: "Description of the workflow"

    # Create orphaned branch if it doesn't exist (default: true)
    # (optional)
    create-orphan: true

    # Use the GitHub Wiki git repository instead of the regular repository. When
    # enabled, files are stored in and read from the wiki, and the agent will be
    # instructed to follow GitHub Wiki markdown syntax (default: false)
    # (optional)
    wiki: true

    # List of allowed file extensions (e.g., [".json", ".txt"]). Default: [".json",
    # ".jsonl", ".txt", ".md", ".csv"]
    # (optional)
    allowed-extensions: []
      # Array of strings

    # When true, all .json files are pretty-printed (2-space indent) before being
    # committed, making them human-readable in the repository (default: false)
    # (optional)
    format-json: true

    # Custom domain validation hook for this repo-memory entry
    # (optional)
    validation:
      # JavaScript validator body that runs over the complete repo-memory directory
      # after optional JSON formatting and before persistence. Throw, return false, or
      # exit nonzero to reject the update.
      script: "example-value"

      # Maximum validator runtime in minutes (default: 1, max: 5)
      # (optional)
      timeout-minutes: 1

  # Format 4: Array of repo-memory configurations for multiple memory locations
  repo-memory: [{"id":"default","branch-name":"memory/default"},{"id":"session","branch-name":"memory/session"}]
    # Array items: object

# ⚠️ Experimental. Top-level Language Server Protocol (LSP) configuration for
# Copilot CLI. Each key is a language identifier and each value defines the server
# command, args, and file extension mappings. Using this field emits a
# compile-time warning.
# (optional)
lsp:
  {}

# Cache configuration for workflow (uses actions/cache syntax)
# (optional)
# Accepted formats:

# Format 1: Single cache configuration
cache:
  # An explicit key for restoring and saving the cache
  key: "example-value"

  # File path or directory to cache for faster workflow execution. Can be a single
  # path or an array of paths to cache multiple locations.
  # Accepted formats:

  # Format 1: A single path to cache
  path: "example-value"

  # Format 2: Multiple paths to cache
  path: []
    # Array items: string

  # Optional list of fallback cache key patterns to use if exact cache key is not
  # found. Enables partial cache restoration for better performance.
  # (optional)
  # Accepted formats:

  # Format 1: A single restore key
  restore-keys: "example-value"

  # Format 2: Multiple restore keys
  restore-keys: []
    # Array items: string

  # The chunk size used to split up large files during upload, in bytes
  # (optional)
  upload-chunk-size: 1

  # Fail the workflow if cache entry is not found
  # (optional)
  fail-on-cache-miss: true

  # If true, only checks if cache entry exists and skips download
  # (optional)
  lookup-only: true

  # Optional custom name for the cache step (overrides auto-generated name)
  # (optional)
  name: "My Workflow"

# Format 2: Multiple cache configurations
cache: []
  # Array items: object

# Safe output processing configuration that automatically creates GitHub issues,
# comments, and pull requests from AI workflow output without requiring write
# permissions in the main job
# (optional)
safe-outputs:
  # ⚠️ Experimental. Create a run-scoped issue and let the agent read user-authored
  # issue comments containing the keyword 'steer'. The issue is reused for agent
  # failure reporting. Requires top-level issues: read permission.
  # (optional)
  steer: true

  # URL sanitization policy for safe outputs. "allowed-only" sanitizes all
  # non-allowed URLs everywhere. "allowed-or-code-region" preserves URLs inside
  # fenced and inline code regions while sanitizing prose.
  # (optional)
  urls: "allowed-only"

  # List of allowed domains for URL redaction in safe output handlers. Supports
  # domain set identifiers (e.g., "python", "node", "default-safe-outputs",
  # "copilot") like network.allowed. These domains are unioned with network.allowed
  # when computing the final allowed domain set. localhost and github.com are always
  # included.
  # (optional)
  allowed-domains: []
    # Array of strings

  # Structured data configuration for body-based safe outputs. Set false (or omit)
  # to disable data, true to allow any object data, provide an inline schema object
  # to enforce shape, or provide a GitHub Actions expression string that resolves to
  # one of those forms at runtime.
  # (optional)
  # Accepted formats:

  # Format 1: boolean
  data: true

  # Format 2: object
  data:
    {}

  # Format 3: GitHub Actions expression resolving to false, true, or a JSON object
  # schema at runtime
  data: "example-value"

  # List of allowed repositories for GitHub references (e.g., #123 or
  # owner/repo#456). Use 'repo' to allow current repository. References to other
  # repositories will be escaped with backticks. If not specified, all references
  # are allowed.
  # (optional)
  allowed-github-references: []
    # Array of strings

  # Enable AI agents to create GitHub issues from workflow output. Supports title
  # prefixes, automatic labeling, assignees, and cross-repository creation. Does not
  # require 'issues: write' permission.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for automatically creating GitHub issues from AI
  # workflow output. The main job does not need 'issues: write' permission.
  create-issue:
    # Optional prefix to add to the beginning of the issue title (e.g., '[ai] ' or
    # '[analysis] ')
    # (optional)
    title-prefix: "example-value"

    # Require create_issue tool calls to include a temporary_id.
    # (optional)
    require-temporary-id: true

    # Optional list of labels to automatically attach to created issues (e.g.,
    # ['automation', 'ai-generated'])
    # (optional)
    labels: []
      # Array of strings

    # Optional list of allowed labels that can be used when creating issues. If
    # omitted, any labels are allowed (including creating new ones). When specified,
    # the agent can only use labels from this list.
    # (optional)
    allowed-labels: []
      # Array of strings

    # Optional list of issue field names that can be modified by create-issue field
    # updates. If omitted or empty, any issue field may be set. Use ['*'] to
    # explicitly allow all.
    # (optional)
    allowed-fields: []
      # Array of strings

    # GitHub usernames to assign the created issue to. Can be a single username string
    # or array of usernames. Use 'copilot' to assign to GitHub Copilot.
    # (optional)
    # Accepted formats:

    # Format 1: Single GitHub username to assign the created issue to (e.g., 'user1'
    # or 'copilot'). Use 'copilot' to assign to GitHub Copilot using the @copilot
    # special value.
    assignees: "example-value"

    # Format 2: List of GitHub usernames to assign the created issue to (e.g.,
    # ['user1', 'user2', 'copilot']). Use 'copilot' to assign to GitHub Copilot using
    # the @copilot special value.
    assignees: []
      # Array items: string

    # Maximum number of issues to create (default: 1) Supports integer or GitHub
    # Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Title-based deduplication for create-issue. Set to true for exact title
    # matching, or provide a non-negative integer (0–100) to deduplicate by
    # Levenshtein edit distance (e.g., 1 allows one-character differences). Accepts a
    # GitHub Actions expression that resolves to a boolean or integer at runtime.
    # Applies within-run and against open/recently-closed repository issues.
    # (optional)
    deduplicate-by-title: null

    # Target repository in format 'owner/repo' for cross-repository issue creation.
    # Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that issues can be
    # created in. When specified, the agent can use a 'repo' field in the output to
    # specify which repository to create the issue in. The target repository (current
    # or target-repo) is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # Time until the issue expires and should be automatically closed. Supports
    # integer (days), relative time format, or false to disable expiration. Minimum
    # duration: 2 hours. When set, a maintenance workflow will be generated.
    # (optional)
    # Accepted formats:

    # Format 1: Number of days until expires
    expires: 1

    # Format 2: Relative time (e.g., '2h', '7d', '2w', '1m', '1y'); minimum 2h for
    # hour values
    expires: "example-value"

    # Format 3: Set to false to explicitly disable expiration
    expires: false

    # If true, group issues as sub-issues under a parent issue. The workflow ID is
    # used as the group identifier. Parent issues are automatically created and
    # managed, with a maximum of 64 sub-issues per parent.
    # (optional)
    group: true

    # When true, automatically close older issues with the same workflow-id marker as
    # 'not planned' with a comment linking to the new issue. Searches for issues
    # containing the workflow-id marker in their body. Maximum 10 issues will be
    # closed. Only runs if issue creation succeeds.
    # (optional)
    close-older-issues: true

    # Optional explicit deduplication key for close-older matching. When set, a `<!--
    # gh-aw-close-key: <value> -->` marker is embedded in the issue body and used as
    # the primary key for searching and filtering older issues instead of the
    # workflow-id markers. This gives deterministic isolation across caller workflows
    # and is stable across workflow renames. The value is normalized to identifier
    # style (lowercase alphanumeric, dashes, underscores).
    # (optional)
    close-older-key: "example-value"

    # When true, if an open issue with the same close-older-key (or workflow-id marker
    # when no key is set) was already created today (UTC), post the new content as a
    # comment on that existing issue instead of creating a new one. Groups multiple
    # same-day runs into a single issue. Works best when combined with
    # close-older-issues: true.
    # (optional)
    group-by-day: true

    # Controls whether AI-generated footer is added to the issue. When false, the
    # visible footer content is omitted but XML markers (workflow-id, tracker-id,
    # metadata) are still included for searchability. Defaults to true.
    # (optional)
    footer: true

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Enable issue-intent metadata support (rationale/confidence/suggest) for this
    # output type.
    # (optional)
    issue-intent: true

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # When true, strip backticks from recognized issue-closing keywords (e.g. `Closes
    # #1` → Closes #1) in body fields for this output type.
    # (optional)
    normalize-closing-keywords: true

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable issue creation with default configuration
  create-issue: null

  # Enable creation of GitHub Copilot coding agent tasks from workflow output.
  # Allows workflows to spawn new agent sessions for follow-up work.
  # (optional)
  # Accepted formats:

  # Format 1: DEPRECATED: Use 'create-agent-session' instead. Configuration for
  # creating GitHub Copilot coding agent sessions from agentic workflow output using
  # gh agent-task CLI. The main job does not need write permissions.
  create-agent-task:
    # Base branch for the agent session pull request. Defaults to the current branch
    # or repository default branch.
    # (optional)
    base: "example-value"

    # Maximum number of agent sessions to create (default: 1) Supports integer or
    # GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target repository in format 'owner/repo' for cross-repository agent session
    # creation. Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that agent sessions can
    # be created in. When specified, the agent can use a 'repo' field in the output to
    # specify which repository to create the agent session in. The target repository
    # (current or target-repo) is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Enable issue-intent metadata support (rationale/confidence/suggest) for this
    # output type.
    # (optional)
    issue-intent: true

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable agent session creation with default configuration
  create-agent-task: null

  # Enable creation of GitHub Copilot coding agent sessions from workflow output.
  # Allows workflows to start interactive agent conversations.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for creating GitHub Copilot coding agent sessions from
  # agentic workflow output using gh agent-task CLI. The main job does not need
  # write permissions.
  create-agent-session:
    # Base branch for the agent session pull request. Defaults to the current branch
    # or repository default branch.
    # (optional)
    base: "example-value"

    # Maximum number of agent sessions to create (default: 1) Supports integer or
    # GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target repository in format 'owner/repo' for cross-repository agent session
    # creation. Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that agent sessions can
    # be created in. When specified, the agent can use a 'repo' field in the output to
    # specify which repository to create the agent session in. The target repository
    # (current or target-repo) is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable agent session creation with default configuration
  create-agent-session: null

  # Enable AI agents to add items to GitHub Projects, update custom fields, and
  # manage project structure. Use this for organizing work into projects with status
  # tracking, priority management, and custom metadata.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for managing GitHub Projects boards. Enable agents to
  # add issues and pull requests to projects, update custom field values (status,
  # priority, effort, dates), create project fields and views. By default it is
  # update-only: if the project does not exist, the job fails with instructions to
  # create it. To allow workflows to create missing projects, explicitly opt in via
  # agent output field create_if_missing=true. Requires a Personal Access Token
  # (PAT) or GitHub App token with Projects permissions (default GITHUB_TOKEN cannot
  # be used). Agent output includes: project (full URL or temporary project ID like
  # aw_XXXXXXXXXXXX or #aw_XXXXXXXXXXXX from create_project), content_type
  # (issue|pull_request|draft_issue), content_number, fields, create_if_missing. For
  # specialized operations, agent can also provide: operation
  # (create_fields|create_view), field_definitions (array of field configs when
  # operation=create_fields), view (view config object when operation=create_view).
  update-project:
    # Maximum number of project operations to perform (default: 10). Each operation
    # may add a project item, or update its fields. Supports integer or GitHub Actions
    # expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # Target project URL for update-project operations. This is required in the
    # configuration for documentation purposes. Agent messages MUST explicitly include
    # the project field in their output - the configured value is not used as a
    # fallback. Must be a valid GitHub Projects v2 URL.
    project: "example-value"

    # Default repository in format 'owner/repo' for cross-repository content
    # resolution. When specified, the agent can use 'target_repo' in agent output to
    # resolve issues or PRs from this repository. Wildcards ('*') are not allowed.
    # Supports GitHub Actions expression syntax (e.g., '${{ vars.TARGET_REPO }}').
    # (optional)
    # Accepted formats:

    # Format 1: string
    target-repo: "example-value"

    # Format 2: GitHub Actions expression that resolves to owner/repo at runtime
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' allowed for
    # cross-repository content resolution via 'target_repo'. The target-repo (or
    # current repo) is always implicitly allowed. Supports wildcard patterns (e.g.,
    # 'org/*', '*/repo', '*') and GitHub Actions expression syntax for individual
    # entries.
    # (optional)
    allowed-repos: []

    # Optional array of project views to create. Each view must have a name and
    # layout. Views are created during project setup.
    # (optional)
    views: []
      # Array items:
        # The name of the view (e.g., 'Sprint Board', 'Roadmap')
        name: "My Workflow"

        # The layout type of the view
        layout: "table"

        # Optional filter query for the view (e.g., 'is:issue is:open', 'label:bug')
        # (optional)
        filter: "example-value"

        # Optional array of field IDs that should be visible in the view (table/board
        # only, not applicable to roadmap)
        # (optional)
        visible-fields: []

        # Optional human description for the view. Not supported by the GitHub Views API
        # and may be ignored.
        # (optional)
        description: "Description of the workflow"

    # Optional array of project custom fields to create up-front.
    # (optional)
    field-definitions: []
      # Array items:
        # The field name to create (e.g., 'status', 'priority')
        name: "My Workflow"

        # The GitHub Projects v2 custom field type
        data-type: "DATE"

        # Options for SINGLE_SELECT fields. GitHub does not support adding options later.
        # (optional)
        options: []
          # Array of strings

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable project management with default configuration (max=10)
  update-project: null

  # Enable AI agents to create new GitHub Projects for organizing and tracking work
  # across issues and pull requests.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for creating new GitHub Projects boards. Enables agents
  # to create new project boards with optional custom fields, views, and an initial
  # item. Requires a Personal Access Token (PAT) or GitHub App token with Projects
  # write permission (default GITHUB_TOKEN cannot be used). Agent output includes:
  # title (project name), owner (org/user login, uses default if omitted),
  # owner_type ('org' or 'user'), optional item_url (issue to add as first item),
  # and optional field_definitions. Returns a temporary project ID for use in
  # subsequent update_project operations.
  create-project:
    # Maximum number of create operations to perform (default: 1). Supports integer or
    # GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # GitHub token to use for this specific output type. Must have Projects write
    # permission. Overrides global github-token if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # Optional default target owner (organization or user login, e.g., 'myorg' or
    # 'username') for the new project. If specified, the agent can omit the owner
    # field in the tool call and this default will be used. The agent can still
    # override by providing an owner in the tool call.
    # (optional)
    target-owner: "example-value"

    # Optional prefix for auto-generated project titles (default: 'Project'). When the
    # agent doesn't provide a title, the project title is auto-generated as
    # '<title-prefix>: <issue-title>' or '<title-prefix> #<issue-number>' based on the
    # issue context.
    # (optional)
    title-prefix: "example-value"

    # Optional array of project views to create automatically after project creation.
    # Each view must have a name and layout. Views are created immediately after the
    # project is created.
    # (optional)
    views: []
      # Array items:
        # The name of the view (e.g., 'Sprint Board', 'Roadmap')
        name: "My Workflow"

        # The layout type of the view
        layout: "table"

        # Optional filter query for the view (e.g., 'is:issue is:open', 'label:bug')
        # (optional)
        filter: "example-value"

        # Optional array of field IDs that should be visible in the view (table/board
        # only, not applicable to roadmap)
        # (optional)
        visible-fields: []

        # Optional human description for the view. Not supported by the GitHub Views API
        # and may be ignored.
        # (optional)
        description: "Description of the workflow"

    # Optional array of project custom fields to create automatically after project
    # creation.
    # (optional)
    field-definitions: []
      # Array items:
        # The field name to create (e.g., 'Priority', 'Classification')
        name: "My Workflow"

        # The GitHub Projects v2 custom field type
        data-type: "DATE"

        # Options for SINGLE_SELECT fields. GitHub does not support adding options later.
        # (optional)
        options: []
          # Array of strings

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable project creation with default configuration (max=1)
  create-project: null

  # Enable AI agents to post status updates to GitHub Projects for progress tracking
  # and stakeholder communication.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for posting status updates to GitHub Projects. Status
  # updates provide stakeholder communication about project progress, health, and
  # timeline. Each update appears in the project's Updates tab and creates a
  # historical record. Requires a Personal Access Token (PAT) or GitHub App token
  # with Projects read & write permission (default GITHUB_TOKEN cannot be used).
  # Typically used by scheduled workflows or orchestrators to post regular progress
  # summaries with status indicators (on-track, at-risk, off-track, complete,
  # inactive), dates, and progress details.
  create-project-status-update:
    # Maximum number of status updates to create (default: 1). Typically 1 per
    # orchestrator run. Supports integer or GitHub Actions expression (e.g. '${{
    # inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified. Must have Projects: Read+Write permission.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # Target project URL for status update operations. This is required in the
    # configuration for documentation purposes. Agent messages MUST explicitly include
    # the project field in their output - the configured value is not used as a
    # fallback. Must be a valid GitHub Projects v2 URL.
    project: "example-value"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable project status updates with default configuration (max=1)
  create-project-status-update: null

  # Enable AI agents to create GitHub Discussions from workflow output. Supports
  # categorization, labeling, and automatic closure of older discussions. Does not
  # require 'discussions: write' permission.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for creating GitHub discussions from agentic workflow
  # output
  create-discussion:
    # Optional prefix for the discussion title
    # (optional)
    title-prefix: "example-value"

    # Optional discussion category. Can be a category ID (string or numeric value),
    # category name, or category slug/route. If not specified, uses the first
    # available category. Matched first against category IDs, then against category
    # names, then against category slugs. Numeric values are automatically converted
    # to strings at runtime.
    # (optional)
    category: null

    # Minimum required length of the discussion body content (before footer/metadata)
    # in characters. If a create_discussion message body is shorter than this value,
    # the safe-outputs job fails.
    # (optional)
    min-body-length: 1

    # Optional list of labels to attach to created discussions. Also used for matching
    # when close-older-discussions is enabled - discussions must have ALL specified
    # labels (AND logic).
    # (optional)
    labels: []
      # Array of strings

    # Optional list of allowed labels that can be used when creating discussions. If
    # omitted, any labels are allowed (including creating new ones). When specified,
    # the agent can only use labels from this list.
    # (optional)
    allowed-labels: []
      # Array of strings

    # Maximum number of discussions to create (default: 1) Supports integer or GitHub
    # Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target repository in format 'owner/repo' for cross-repository discussion
    # creation. Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that discussions can be
    # created in. When specified, the agent can use a 'repo' field in the output to
    # specify which repository to create the discussion in. The target repository
    # (current or target-repo) is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # When true, automatically close older discussions matching the same title prefix
    # or labels as 'outdated' with a comment linking to the new discussion. Requires
    # title-prefix or labels to be set. Maximum 10 discussions will be closed. Only
    # runs if discussion creation succeeds. When fallback-to-issue is enabled and
    # discussion creation fails, older issues will be closed instead.
    # (optional)
    close-older-discussions: true

    # Optional explicit deduplication key for close-older matching. When set, a `<!--
    # gh-aw-close-key: <value> -->` marker is embedded in the discussion body and used
    # as the primary key for searching and filtering older discussions instead of the
    # workflow-id markers. This gives deterministic isolation across caller workflows
    # and is stable across workflow renames. The value is normalized to identifier
    # style (lowercase alphanumeric, dashes, underscores).
    # (optional)
    close-older-key: "example-value"

    # Required category for matching when close-older-discussions is enabled. Only
    # discussions in this category will be considered when searching for older
    # discussions to close.
    # (optional)
    required-category: "example-value"

    # When true (default), fallback to creating an issue if discussion creation fails
    # due to permissions. The fallback issue will include a note indicating it was
    # intended to be a discussion. If close-older-discussions is enabled, the
    # close-older-issues logic will be applied to the fallback issue.
    # (optional)
    fallback-to-issue: true

    # Controls whether AI-generated footer is added to the discussion. When false, the
    # visible footer content is omitted but XML markers (workflow-id, tracker-id,
    # metadata) are still included for searchability. Defaults to true.
    # (optional)
    footer: true

    # Time until the discussion expires and should be automatically closed. Supports
    # integer (days), relative time format like '2h' (2 hours), '7d' (7 days), '2w' (2
    # weeks), '1m' (1 month), '1y' (1 year), or false to disable expiration. Minimum
    # duration: 2 hours. When set, a maintenance workflow will be generated. Defaults
    # to 7 days if not specified.
    # (optional)
    # Accepted formats:

    # Format 1: Number of days until expires
    expires: 1

    # Format 2: Relative time (e.g., '2h', '7d', '2w', '1m', '1y'); minimum 2h for
    # hour values
    expires: "example-value"

    # Format 3: Set to false to explicitly disable expiration
    expires: false

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable discussion creation with default configuration
  create-discussion: null

  # Enable AI agents to close GitHub Discussions based on workflow analysis or
  # conditions.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for closing GitHub discussions with comment and
  # resolution from agentic workflow output
  close-discussion:
    # Only close discussions that have all of these labels
    # (optional)
    required-labels: []
      # Array of strings

    # Only close discussions with this title prefix
    # (optional)
    required-title-prefix: "example-value"

    # Only close discussions in this category
    # (optional)
    required-category: "example-value"

    # Target for closing: 'triggering' (default, current discussion), or '*' (any
    # discussion with discussion_number field)
    # (optional)
    target: "example-value"

    # Maximum number of discussions to close (default: 1) Supports integer or GitHub
    # Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target repository in format 'owner/repo' for cross-repository operations. Takes
    # precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that discussions can be
    # closed in. The target repository is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

  # Format 2: Enable discussion closing with default configuration
  close-discussion: null

  # Enable AI agents to edit and update existing GitHub Discussion content, titles,
  # and metadata.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for updating GitHub discussions from agentic workflow
  # output
  update-discussion:
    # Target for updates: 'triggering' (default), '*' (any discussion), or explicit
    # discussion number
    # (optional)
    target: "example-value"

    # Allow updating discussion title - presence of key indicates field can be updated
    # (optional)
    title: null

    # Allow updating discussion body - presence of key indicates field can be updated
    # (optional)
    body: null

    # Allow updating discussion labels - presence of key indicates field can be
    # updated
    # (optional)
    labels: null

    # Optional list of allowed labels. If omitted, any labels are allowed (including
    # creating new ones).
    # (optional)
    allowed-labels: []
      # Array of strings

    # Maximum number of discussions to update (default: 1) Supports integer or GitHub
    # Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target repository in format 'owner/repo' for cross-repository discussion
    # updates. Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that discussions can be
    # updated in. The target repository is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # Controls whether AI-generated footer is added when updating the discussion body.
    # When false, the visible footer content is omitted. Defaults to true. Only
    # applies when 'body' is enabled.
    # (optional)
    footer: true

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable discussion updating with default configuration
  update-discussion: null

  # Enable AI agents to close GitHub issues based on workflow analysis, resolution
  # detection, or automated triage.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for closing GitHub issues with comment from agentic
  # workflow output
  close-issue:
    # Only close issues that have all of these labels
    # (optional)
    required-labels: []
      # Array of strings

    # Only close issues with this title prefix
    # (optional)
    required-title-prefix: "example-value"

    # Target for closing: 'triggering' (default, current issue), or '*' (any issue
    # with issue_number field)
    # (optional)
    target: "example-value"

    # Maximum number of issues to close (default: 1) Supports integer or GitHub
    # Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target repository in format 'owner/repo' for cross-repository operations. Takes
    # precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that issues can be closed
    # in. When specified, the agent can use a 'repo' field in the output to specify
    # which repository to close the issue in. The target repository (current or
    # target-repo) is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Enable issue-intent metadata support (rationale/confidence/suggest) for this
    # output type.
    # (optional)
    issue-intent: true

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # Reason for closing the issue. Scalar: fixed reason (default: completed). Array:
    # agent selects from the listed subset at runtime.
    # (optional)
    # Accepted formats:

    # Format 1: Fixed reason for closing the issue; the agent cannot override this
    # value.
    state-reason: "completed"

    # Format 2: Restricted subset of allowed reasons; the agent may choose from this
    # list at runtime.
    state-reason: []
      # Array items: string

  # Format 2: Enable issue closing with default configuration
  close-issue: null

  # Enable AI agents to close pull requests based on workflow analysis or automated
  # review decisions.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for closing GitHub pull requests without merging, with
  # comment from agentic workflow output
  close-pull-request:
    # Only close pull requests that have any of these labels
    # (optional)
    required-labels: []
      # Array of strings

    # Only close pull requests with this title prefix
    # (optional)
    required-title-prefix: "example-value"

    # Target for closing: 'triggering' (default, current PR), or '*' (any PR with
    # pull_request_number field)
    # (optional)
    target: "example-value"

    # Maximum number of pull requests to close (default: 1) Supports integer or GitHub
    # Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target repository in format 'owner/repo' for cross-repository operations. Takes
    # precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that pull requests can be
    # closed in. The target repository is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable pull request closing with default configuration
  close-pull-request: null

  # Enable AI agents to mark draft pull requests as ready for review when criteria
  # are met.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for marking draft pull requests as ready for review,
  # with comment from agentic workflow output
  mark-pull-request-as-ready-for-review:
    # Only mark pull requests that have any of these labels
    # (optional)
    required-labels: []
      # Array of strings

    # Only mark pull requests with this title prefix
    # (optional)
    required-title-prefix: "example-value"

    # Target for marking: 'triggering' (default, current PR), or '*' (any PR with
    # pull_request_number field)
    # (optional)
    target: "example-value"

    # Maximum number of pull requests to mark as ready (default: 1) Supports integer
    # or GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target repository in format 'owner/repo' for cross-repository operations. Takes
    # precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that pull requests can be
    # marked as ready in. The target repository is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable marking pull requests as ready for review with default
  # configuration
  mark-pull-request-as-ready-for-review: null

  # Enable AI agents to approve pending workflow runs in the action required state.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for approving workflow runs that are awaiting required
  # approval.
  approve-workflow-run:
    # Maximum number of workflow runs to approve (default: 1). Supports an integer or
    # GitHub Actions expression.
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Repositories in format 'owner/repo' whose pull requests may have their workflow
    # runs approved, in addition to the current repository which is always allowed.
    # Use this to allow fork pull requests. Supports wildcard patterns such as
    # 'org/*'.
    # (optional)
    allowed-repos: []
      # Array of strings

    # Post a comment on the pull request associated with the approved workflow run
    # announcing that the run has started. Defaults to true; set to false to disable.
    # (optional)
    comment: true

    # Additional pull request numbers whose pending workflow runs may be approved. The
    # triggering pull request is always allowed. Supports a list of strings or a
    # GitHub Actions expression that resolves to a list.
    # (optional)
    # Accepted formats:

    # Format 1: array
    allowed-pull-requests: []
      # Array items: string

    # Format 2: GitHub Actions expression that resolves to a list of pull request
    # numbers at runtime
    allowed-pull-requests: "example-value"

    # Required workflow filename patterns allowed for approval. Patterns support
    # wildcards and treat .yml and .yaml as equivalent.
    allowed-workflows: []
      # Array of strings

    # Protected files prevent workflow run approval when modified by the associated
    # pull request. Use exclude to remove filenames or path prefixes from the default
    # protected set.
    # (optional)
    protected-files:
      # (optional)
      exclude: []
        # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, preview the approval without making the GitHub API call.
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable workflow run approval with default configuration
  approve-workflow-run: null

  # (optional)
  # Accepted formats:

  # Format 1: Configuration for creating Jira Cloud issues through the privileged
  # safe-output execution path.
  jira-create-issue:
    # Maximum number of Jira issues to create (default: 1).
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: string
    max: "example-value"

    # Preview Jira issue creation without sending an HTTP mutation.
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: string
    staged: "example-value"

  # Format 2: Enable Jira issue creation with default configuration.
  jira-create-issue: null

  # (optional)
  # Accepted formats:

  # Format 1: Configuration for updating Jira Cloud issue summaries or descriptions
  # through the privileged safe-output execution path.
  jira-update-issue:
    # Maximum number of Jira issues to update (default: 1).
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: string
    max: "example-value"

    # Preview Jira issue updates without sending an HTTP mutation.
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: string
    staged: "example-value"

  # Format 2: Enable Jira issue updates with default configuration.
  jira-update-issue: null

  # (optional)
  # Accepted formats:

  # Format 1: Configuration for commenting on Jira Cloud issues through the
  # privileged safe-output execution path.
  jira-add-comment:
    # Maximum number of Jira comments to add (default: 1).
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: string
    max: "example-value"

    # Preview Jira comments without sending an HTTP mutation.
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: string
    staged: "example-value"

  # Format 2: Enable Jira comments with default configuration.
  jira-add-comment: null

  # (optional)
  # Accepted formats:

  # Format 1: Configuration for adding labels to Jira Cloud issues through the
  # privileged safe-output execution path.
  jira-add-label:
    # Maximum number of Jira labels to add (default: 1).
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: string
    max: "example-value"

    # Preview Jira label additions without sending an HTTP mutation.
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: string
    staged: "example-value"

  # Format 2: Enable Jira label additions with default configuration.
  jira-add-label: null

  # Experimental. Create Linear issues through the isolated safe_outputs job.
  # (optional)
  linear-create-issue:
    # Trusted Linear team model UUID.
    team-id: "example-value"

    # Optional trusted Linear project identifier from a project URL or model UUID.
    # (optional)
    project-id: "example-value"

    # Maximum number of Linear issues to create (default: 1).
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: string
    max: "example-value"

    # A boolean value that may also be specified as a GitHub Actions expression string
    # that resolves to a boolean at runtime (e.g. '${{ inputs.my-flag }}').
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

  # Experimental. Add comments to one trusted Linear issue through the isolated
  # safe_outputs job.
  # (optional)
  linear-add-comment:
    # Trusted Linear issue UUID or shorthand identifier such as ENG-123.
    target: "example-value"

    # Maximum number of Linear comments to add (default: 1).
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: string
    max: "example-value"

    # A boolean value that may also be specified as a GitHub Actions expression string
    # that resolves to a boolean at runtime (e.g. '${{ inputs.my-flag }}').
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

  # Experimental. Update explicitly enabled fields on one trusted Linear issue
  # through the isolated safe_outputs job.
  # (optional)
  linear-update-issue:
    # Trusted Linear issue UUID or shorthand identifier such as ENG-123.
    target: "example-value"

    # Allow the agent to update the Linear issue title.
    # (optional)
    title: null

    # Allow the agent to replace the Linear issue description.
    # (optional)
    body: null

    # Maximum number of updates to apply (default: 1).
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: string
    max: "example-value"

    # A boolean value that may also be specified as a GitHub Actions expression string
    # that resolves to a boolean at runtime (e.g. '${{ inputs.my-flag }}').
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

  # Enable AI agents to add comments to GitHub issues, pull requests, or
  # discussions. Supports templating, cross-repository commenting, and automatic
  # mentions.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for automatically creating GitHub issue or pull request
  # comments from AI workflow output. The main job does not need write permissions.
  add-comment:
    # Maximum number of comments to create (default: 1) Supports integer or GitHub
    # Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target for comments: 'triggering' (default), '*' (any issue), or explicit issue
    # number
    # (optional)
    target: "example-value"

    # Target repository in format 'owner/repo' for cross-repository comments. Takes
    # precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that comments can be
    # created in. When specified, the agent can use a 'repo' field in the output to
    # specify which repository to create the comment in. The target repository
    # (current or target-repo) is always implicitly allowed. Accepts an array or a
    # GitHub Actions expression resolving to a comma-separated list (e.g. '${{
    # inputs[\'allowed-repos\'] }}').
    # (optional)
    # Accepted formats:

    # Format 1: Array of repository slugs in 'owner/repo' format
    allowed-repos: []
      # Array items: string

    # Format 2: GitHub Actions expression resolving to a comma-separated list of
    # repository slugs (e.g. '${{ inputs[\'allowed-repos\'] }}')
    allowed-repos: "example-value"

    # When true, minimizes/hides all previous comments from the same agentic workflow
    # (identified by tracker-id) before creating the new comment. Supports literal
    # boolean, GitHub Actions expression (e.g. '${{ inputs.hide-older-comments }}'),
    # or object form for advanced matching. Default: false.
    # (optional)
    # Accepted formats:

    # Format 1: A boolean value that may also be specified as a GitHub Actions
    # expression string that resolves to a boolean at runtime (e.g. '${{
    # inputs.my-flag }}').

    # Format 2: object
    hide-older-comments:
      # Enable or disable hide-older-comments when using object form. Defaults to true
      # when omitted.
      # (optional)
      # Accepted formats:

      # Format 1: boolean
      enabled: true

      # Format 2: GitHub Actions expression that resolves to a boolean at runtime
      enabled: "example-value"

      # Additional workflow-id values to fully match when selecting older comments to
      # hide.
      # (optional)
      match: []
        # Array of strings

    # Trusted allowlist of issue or pull request comment IDs the agent may update when
    # target is '*'.
    # (optional)
    allows-comment-ids: []
      # Array of strings

    # Exact workflow IDs whose older comments are eligible to be hidden.
    # (optional)
    hide-older-comments-match: []
      # Array of strings

    # List of allowed reasons for hiding older comments when hide-older-comments is
    # enabled. Default: all reasons allowed (spam, abuse, off_topic, outdated,
    # resolved, low_quality).
    # (optional)
    allowed-reasons: []
      # Array of strings

    # Controls whether the workflow requests discussions:write permission for
    # add-comment. Default: true (includes discussions:write). Set to false if your
    # GitHub App lacks Discussions permission to prevent 422 errors during token
    # generation.
    # (optional)
    discussions: true

    # Controls whether the workflow requests issues:write permission for add-comment.
    # Default: true (includes issues:write). Set to false to disable issue commenting
    # permissions.
    # (optional)
    issues: true

    # Controls whether the workflow requests pull-requests:write permission for
    # add-comment. Default: true (includes pull-requests:write). Set to false to
    # disable pull request commenting permissions.
    # (optional)
    pull-requests: true

    # Controls whether AI-generated footer is added to the comment. When false, the
    # visible footer content is omitted but XML markers (workflow-id, metadata) are
    # still included for searchability. Defaults to true.
    # (optional)
    footer: true

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # Conjunctive label constraint: ALL of these labels must be present on the
    # issue/PR for the operation to proceed.
    # (optional)
    required-labels: []
      # Array of strings

    # Title prefix constraint: the issue/PR title must start with this prefix for the
    # operation to proceed.
    # (optional)
    required-title-prefix: "example-value"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # When true, strip backticks from recognized issue-closing keywords (e.g. `Closes
    # #1` → Closes #1) in body fields for this output type.
    # (optional)
    normalize-closing-keywords: true

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable issue comment creation with default configuration
  add-comment: null

  # Enable AI agents to create GitHub pull requests from workflow-generated code
  # changes, patches, or analysis results.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for creating GitHub pull requests from agentic workflow
  # output. Supports creating multiple PRs in a single run when max > 1.
  create-pull-request:
    # Maximum number of pull requests to create (default: 1). Each PR requires
    # distinct changes on a separate branch. Supports integer or GitHub Actions
    # expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Optional prefix to prepend to the pull request branch name (e.g. "signed/").
    # Applied before the agent-specified or auto-generated branch name.
    # (optional)
    branch-prefix: "example-value"

    # Require create_pull_request tool calls to include a temporary_id.
    # (optional)
    require-temporary-id: true

    # Optional prefix for the pull request title
    # (optional)
    title-prefix: "example-value"

    # Optional list of labels to attach to the pull request. Accepts an array of label
    # names or a GitHub Actions expression resolving to a comma-separated list (e.g.
    # '${{ inputs.labels }}').
    # (optional)
    # Accepted formats:

    # Format 1: Array of label names
    labels: []
      # Array items: string

    # Format 2: GitHub Actions expression resolving to a comma-separated list of label
    # names (e.g. '${{ inputs.labels }}')
    labels: "example-value"

    # Optional list of allowed labels that can be used when creating pull requests. If
    # omitted, any labels are allowed (including creating new ones). When specified,
    # the agent can only use labels from this list.
    # (optional)
    allowed-labels: []
      # Array of strings

    # Optional reviewer(s) to assign to the pull request. Accepts either a single
    # string or an array of usernames. Use 'copilot' to request a code review from
    # GitHub Copilot.
    # (optional)
    # Accepted formats:

    # Format 1: Single reviewer username to assign to the pull request. Use 'copilot'
    # to request a code review from GitHub Copilot using the
    # copilot-pull-request-reviewer[bot].
    reviewers: "example-value"

    # Format 2: List of reviewer usernames to assign to the pull request. Use
    # 'copilot' to request a code review from GitHub Copilot using the
    # copilot-pull-request-reviewer[bot].
    reviewers: []
      # Array items: string

    # Optional team reviewer(s) to assign to the pull request. Accepts either a single
    # string or an array of team slugs.
    # (optional)
    # Accepted formats:

    # Format 1: Single team slug to assign as a reviewer to the pull request.
    team-reviewers: "example-value"

    # Format 2: List of team slugs to assign as reviewers to the pull request.
    team-reviewers: []
      # Array items: string

    # Optional assignee(s) for a fallback issue created when pull request creation
    # cannot proceed, including protected-files fallback-to-issue and pull request
    # creation or push failures. Accepts either a single string or an array of
    # usernames.
    # (optional)
    # Accepted formats:

    # Format 1: Single username to assign to a fallback issue created when pull
    # request creation cannot proceed, including protected-files fallback-to-issue and
    # pull request creation or push failures.
    assignees: "example-value"

    # Format 2: List of usernames to assign to a fallback issue created when pull
    # request creation cannot proceed, including protected-files fallback-to-issue and
    # pull request creation or push failures.
    assignees: []
      # Array items: string

    # Optional labels to apply to fallback issues created when pull request creation
    # cannot proceed. When omitted, fallback issues reuse pull request labels. A
    # managed label is always added for triage.
    # (optional)
    fallback-labels: []
      # Array of strings

    # Whether to create pull request as draft (defaults to true). Accepts a boolean or
    # a GitHub Actions expression.
    # (optional)
    draft: null

    # Behavior when no changes to push: 'warn' (default - log warning but succeed),
    # 'error' (fail the action), or 'ignore' (silent success)
    # (optional)
    if-no-changes: "warn"

    # When true, allows creating a pull request without any initial changes or git
    # patch. This is useful for preparing a feature branch that an agent can push
    # changes to later. The branch will be created from the base branch without
    # applying any patch. Defaults to false.
    # (optional)
    allow-empty: true

    # Target repository in format 'owner/repo' for cross-repository pull request
    # creation, or '*' to let the agent choose the target repository at runtime
    # (requires checkout: configs with path: for each possible target). Takes
    # precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # Optional fork or head repository in format 'owner/repo' where the pull request
    # branch is pushed. When set, the pull request is still created in target-repo but
    # uses an owner-qualified head.
    # (optional)
    head-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that pull requests can be
    # created in. When specified, the agent can use a 'repo' field in the output to
    # specify which repository to create the pull request in. The target repository
    # (current or target-repo) is always implicitly allowed. Accepts an array or a
    # GitHub Actions expression resolving to a comma-separated list (e.g. '${{
    # inputs[\'allowed-repos\'] }}').
    # (optional)
    # Accepted formats:

    # Format 1: Array of repository slugs in 'owner/repo' format
    allowed-repos: []
      # Array items: string

    # Format 2: GitHub Actions expression resolving to a comma-separated list of
    # repository slugs (e.g. '${{ inputs[\'allowed-repos\'] }}')
    allowed-repos: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # Optional GitHub token used only for branch writes to head-repo. Use this when
    # the upstream PR repository and the fork head repository require different
    # credentials.
    # (optional)
    head-github-token: "example-value"

    # Optional GitHub App credentials to mint an ephemeral token for head-repo branch
    # writes at runtime. When configured, the minted token takes precedence over
    # head-github-token. The app installation must have contents: write on head-repo.
    # (optional)
    head-github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

    # Time until the pull request expires and should be automatically closed (only for
    # same-repo PRs without target-repo). Supports integer (days) or relative time
    # format. Minimum duration: 2 hours.
    # (optional)
    # Accepted formats:

    # Format 1: Number of days until expires
    expires: 1

    # Format 2: Relative time (e.g., '2h', '7d', '2w', '1m', '1y'); minimum 2h for
    # hour values
    expires: "example-value"

    # Enable auto-merge for the pull request. Accepts true/false, an explicit merge
    # method (squash, merge, rebase), or a GitHub Actions expression resolving to any
    # of those values. When enabled, the PR will be automatically merged once all
    # required checks pass and required approvals are met. Defaults to false.
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    auto-merge: true

    # Format 2: string
    auto-merge: "squash"

    # Format 3: GitHub Actions expression resolving to false, true, or a merge method
    # string
    auto-merge: "example-value"

    # Base branch for the pull request. Defaults to the workflow's branch
    # (github.ref_name) if not specified. Useful for cross-repository PRs targeting
    # non-default branches (e.g., 'vnext', 'release/v1.0').
    # (optional)
    base-branch: "example-value"

    # Optional list of allowed source branch patterns (glob syntax, e.g. 'feature/*',
    # 'release/*'). When configured, the effective create_pull_request branch must
    # match one of these patterns. Accepts an array or a GitHub Actions expression
    # resolving to a comma-separated list (e.g. '${{ inputs[\'allowed-branches\']
    # }}').
    # (optional)
    # Accepted formats:

    # Format 1: Array of source branch patterns (glob syntax supported)
    allowed-branches: []
      # Array items: string

    # Format 2: GitHub Actions expression resolving to a comma-separated list of
    # source branch patterns (e.g. '${{ inputs[\'allowed-branches\'] }}')
    allowed-branches: "example-value"

    # Optional list of allowed base branch patterns (glob syntax, e.g. 'main',
    # 'release/*'). When configured, the agent may provide a `base` field in
    # create_pull_request output to override base-branch for a single run, but only if
    # it matches one of these patterns. Accepts an array or a GitHub Actions
    # expression resolving to a comma-separated list (e.g. '${{
    # inputs[\'allowed-base-branches\'] }}').
    # (optional)
    # Accepted formats:

    # Format 1: Array of base branch patterns (glob syntax supported)
    allowed-base-branches: []
      # Array items: string

    # Format 2: GitHub Actions expression resolving to a comma-separated list of base
    # branch patterns (e.g. '${{ inputs[\'allowed-base-branches\'] }}')
    allowed-base-branches: "example-value"

    # Controls whether stacked pull requests (pull requests whose base branch is
    # another pull request branch created by the same run) are allowed. Defaults to
    # true. Set to false on GitHub Enterprise Server or other instances without
    # stacked pull request support: create_pull_request outputs requesting a
    # non-default base branch are then rejected with an explanatory error.
    # (optional)
    stacked: true

    # Maximum allowed size for git patches in kilobytes (KB) for create-pull-request
    # only. Overrides safe-outputs max-patch-size for this output type. Defaults to
    # 4096 KB (4 MB) when unset.
    # (optional)
    max-patch-size: 1

    # Maximum allowed number of unique files in a create-pull-request patch. Overrides
    # safe-outputs max-patch-files for this output type. Defaults to 100 when unset.
    # (optional)
    max-patch-files: 1

    # Controls whether AI-generated footer is added to the pull request. When false,
    # the visible footer content is omitted but XML markers (workflow-id, tracker-id,
    # metadata) are still included for searchability. Defaults to true.
    # (optional)
    footer: true

    # Controls the fallback behavior when pull request creation fails. When true
    # (default), an issue is created as a fallback with the patch content. When false,
    # no issue is created and the workflow fails with an error. Setting to false also
    # removes the issues:write permission requirement.
    # (optional)
    fallback-as-issue: true

    # When true (default), automatically appends a closing keyword ("Fixes #N") to the
    # PR description when the workflow is triggered from an issue and no closing
    # keyword is already present. This causes GitHub to auto-close the triggering
    # issue when the PR is merged. Set to false to prevent this behavior, e.g., for
    # partial-work PRs or multi-PR workflows. Accepts a boolean or a GitHub Actions
    # expression.
    # (optional)
    auto-close-issue: null

    # When true, automatically close older open pull requests with the same
    # workflow-id marker (or close-older-key when set) with a comment linking to the
    # new PR. Maximum 10 PRs will be closed. Only runs if PR creation succeeds.
    # (optional)
    close-older-pull-requests: null

    # Optional explicit deduplication key for close-older-pull-requests matching. When
    # set, a `<!-- gh-aw-close-key: <value> -->` marker is embedded in the PR body and
    # used as the primary key for searching and filtering older PRs instead of the
    # workflow-id markers. The value is normalized to identifier style (lowercase
    # alphanumeric, dashes, underscores).
    # (optional)
    close-older-key: "example-value"

    # Token used to push an empty commit after PR creation to trigger CI events. Works
    # around the GITHUB_TOKEN limitation where pushes don't trigger workflow runs.
    # Defaults to the magic secret GH_AW_CI_TRIGGER_TOKEN if set in the repository.
    # Use a secret expression (e.g. '${{ secrets.CI_TOKEN }}') for a custom token, or
    # 'app' for GitHub App auth.
    # (optional)
    github-token-for-extra-empty-commit: "example-value"

    # Controls protected-file protection. String form: request-review (default),
    # blocked, allowed, or fallback-to-issue — or a GitHub Actions expression for
    # reusable workflows. Object form: { policy, exclude } to customize the
    # protected-file set.
    # (optional)
    # Accepted formats:

    # Format 1: Controls protected-file protection. request-review (default): create
    # the PR but prepend a caution block and submit a REQUEST_CHANGES review for
    # manual scrutiny. blocked: hard-block any patch that modifies package manifests
    # (e.g. package.json, go.mod), engine instruction files (e.g. AGENTS.md,
    # CLAUDE.md) or .github/ files. allowed: allow all changes. fallback-to-issue:
    # push the branch but create a review issue instead of a PR, so a human can review
    # the manifest changes before merging.
    protected-files: "blocked"

    # Format 2: GitHub Actions expression that resolves to 'blocked', 'allowed',
    # 'fallback-to-issue', or 'request-review' at runtime. Use in reusable
    # workflow_call workflows to parameterize the policy per caller.
    protected-files: "example-value"

    # Format 3: Object form for granular control over the protected-file set. Use the
    # exclude list to remove specific files from the default protection while keeping
    # the rest.
    protected-files:
      # (optional)
      # Accepted formats:

      # Format 1: Protection policy. request-review (default): create the PR but prepend
      # a caution block and submit a REQUEST_CHANGES review. blocked: hard-block any
      # patch that modifies protected files. allowed: allow all changes.
      # fallback-to-issue: push the branch but create a review issue instead of a PR.
      policy: "blocked"

      # Format 2: GitHub Actions expression that resolves to 'blocked', 'allowed',
      # 'fallback-to-issue', or 'request-review' at runtime.
      policy: "example-value"

      # List of filenames or path prefixes to remove from the default protected-file
      # set. Items are matched by basename (e.g. "AGENTS.md") or path prefix (e.g.
      # ".agents/"). Use this to allow the agent to modify specific files that are
      # otherwise blocked by default.
      # (optional)
      exclude: []
        # Array of strings

    # Exclusive allowlist of glob patterns. When set, every file in the patch must
    # match at least one pattern — files outside the list are always refused,
    # including normal source files. This is a restriction, not an exception: setting
    # allowed-files: [".github/workflows/*"] blocks all other files. To allow multiple
    # sets of files, list all patterns explicitly. Acts independently of the
    # protected-files policy; both checks must pass. To modify a protected file, it
    # must both match allowed-files and be permitted by protected-files (e.g.
    # protected-files: allowed). Supports * (any characters except /) and ** (any
    # characters including /).
    # (optional)
    allowed-files: []
      # Array of strings

    # When true, the random salt suffix is not appended to the agent-specified branch
    # name. Invalid characters are still replaced for security, and casing is always
    # preserved regardless of this setting. Useful when the target repository enforces
    # branch naming conventions (e.g. Jira keys in uppercase such as
    # 'bugfix/BR-329-red'). Defaults to false.
    # (optional)
    preserve-branch-name: true

    # When true (and preserve-branch-name is true), allows the handler to force-delete
    # an existing remote branch ref and recreate it from the agent's local HEAD. When
    # false (default), if the agent-specified branch already exists on the remote with
    # preserve-branch-name enabled, the handler falls back (e.g. opens an issue)
    # rather than overwriting the remote ref. Useful for long-lived reusable branches
    # whose previous PR was merged.
    # (optional)
    recreate-ref: true

    # List of glob patterns for files to exclude from the patch. Each pattern is
    # passed to `git format-patch` as a `:(exclude)<pattern>` magic pathspec, so
    # matching files are stripped by git at generation time and will not appear in the
    # commit. Excluded files are also not subject to `allowed-files` or
    # `protected-files` checks. Supports * (any characters except /) and ** (any
    # characters including /).
    # (optional)
    excluded-files: []
      # Array of strings

    # Transport format for packaging changes. "bundle" (default) uses git bundle. "am"
    # uses git format-patch/git am. Accepts a GitHub Actions expression for reusable
    # workflows.
    # (optional)
    # Accepted formats:

    # Format 1: Transport format for packaging changes. "bundle" (default) uses git
    # bundle, which preserves merge commit topology, per-commit authorship, and
    # merge-resolution-only content. "am" uses git format-patch/git am.
    patch-format: "am"

    # Format 2: GitHub Actions expression that resolves to 'am' or 'bundle' at
    # runtime. Use in reusable workflow_call workflows to parameterize the transport
    # format per caller.
    patch-format: "example-value"

    # When true (default), signed commits are required and pushes use GitHub's
    # createCommitOnBranch GraphQL mutation so GitHub signs them. Set to false to use
    # git push directly for repositories that do not require signed commits; this also
    # allows pushing merge commits that GraphQL cannot represent.
    # (optional)
    signed-commits: true

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # When true, adds workflows: write to the GitHub App token permissions. Required
    # when allowed-files targets .github/workflows/ paths. Requires
    # safe-outputs.github-app to be configured because the workflows permission is a
    # GitHub App-only permission and cannot be granted via GITHUB_TOKEN.
    # (optional)
    allow-workflows: true

    # When true, strip backticks from recognized issue-closing keywords (e.g. `Closes
    # #1` → Closes #1) in body fields for this output type.
    # (optional)
    normalize-closing-keywords: true

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable pull request creation with default configuration
  create-pull-request: null

  # Enable AI agents to add review comments to specific lines in pull request diffs
  # during code review workflows.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for creating GitHub pull request review comments from
  # agentic workflow output
  create-pull-request-review-comment:
    # Maximum number of review comments to create (default: 10) Supports integer or
    # GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Side of the diff for comments: 'LEFT' or 'RIGHT' (default: 'RIGHT')
    # (optional)
    side: "LEFT"

    # Target for review comments: 'triggering' (default, only on triggering PR), '*'
    # (any PR, requires pull_request_number in agent output), or explicit PR number
    # (optional)
    target: "example-value"

    # Target repository in format 'owner/repo' for cross-repository PR review
    # comments. Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that PR review comments
    # can be created in. When specified, the agent can use a 'repo' field in the
    # output to specify which repository to create the review comment in. The target
    # repository (current or target-repo) is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # All of these labels must be present on the target item for this operation to
    # proceed
    # (optional)
    required-labels: []
      # Array of strings

    # The target item's title must start with this prefix for this operation to
    # proceed
    # (optional)
    required-title-prefix: "example-value"

    # Pin the review to this specific commit SHA. When set, the review is attributed
    # to this commit instead of the current PR head. GitHub will mark the review as
    # outdated if the PR head has since moved.
    # (optional)
    commit-id: "example-value"

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable PR review comment creation with default configuration
  create-pull-request-review-comment: null

  # Enable AI agents to submit consolidated pull request reviews with a status
  # decision. Works with create-pull-request-review-comment to batch inline comments
  # into a single review.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for submitting a consolidated PR review with a status
  # decision (APPROVE, REQUEST_CHANGES, COMMENT). All
  # create-pull-request-review-comment outputs are collected and submitted as part
  # of this review.
  submit-pull-request-review:
    # Maximum number of reviews to submit (default: 1) Supports integer or GitHub
    # Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Controls when AI-generated footer is added to the review body. Accepts boolean
    # (true/false) or string ('always', 'none', 'if-body'). The 'if-body' mode is
    # useful for clean approval reviews without body text. Defaults to 'always'.
    # (optional)
    # Accepted formats:

    # Format 1: Controls whether AI-generated footer is added to the review body. true
    # maps to 'always', false maps to 'none'.
    footer: true

    # Format 2: Controls when AI-generated footer is added to the review body:
    # 'always' (default), 'none' (never), or 'if-body' (only when review has body
    # text).
    footer: "always"

    # Target PR for the review: 'triggering' (default, current PR), '*' (any PR,
    # requires pull_request_number in agent output), or explicit PR number (e.g. ${{
    # github.event.inputs.pr_number }}). Required when workflow is not triggered by a
    # pull request (e.g. workflow_dispatch).
    # (optional)
    target: "example-value"

    # Target repository in format 'owner/repo' for cross-repository PR review
    # submission. Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that PR reviews can be
    # submitted in. When specified, the agent can use a 'repo' field in the output to
    # specify which repository to submit the review in. The target repository (current
    # or target-repo) is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # Optional list of allowed review event types. If omitted, all event types
    # (APPROVE, COMMENT, REQUEST_CHANGES) are allowed. Use this to restrict the agent
    # to specific event types, e.g. [COMMENT, REQUEST_CHANGES] to prevent approvals.
    # (optional)
    allowed-events: []
      # Array of strings

    # When true, after posting a replacement review this workflow dismisses older
    # REQUEST_CHANGES reviews previously posted by the same workflow on the same pull
    # request. This is best-effort and requires workflow markers in prior review
    # bodies.
    # (optional)
    supersede-older-reviews: true

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # All of these labels must be present on the target item for this operation to
    # proceed
    # (optional)
    required-labels: []
      # Array of strings

    # The target item's title must start with this prefix for this operation to
    # proceed
    # (optional)
    required-title-prefix: "example-value"

    # Pin the review to this specific commit SHA. When set, the review is attributed
    # to this commit instead of the current PR head. GitHub will mark the review as
    # outdated if the PR head has since moved.
    # (optional)
    commit-id: "example-value"

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable PR review submission with default configuration
  submit-pull-request-review: null

  # Enable AI agents to dismiss pull request reviews authored by the current
  # workflow actor.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for dismissing pull request reviews previously submitted
  # by the current workflow actor.
  dismiss-pull-request-review:
    # Maximum number of review dismissals to perform (default: 10) Supports integer or
    # GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target PR for review dismissal: 'triggering' (default, current PR), '*' (any PR,
    # requires pull_request_number in agent output), or explicit PR number.
    # (optional)
    target: "example-value"

    # Target repository in format 'owner/repo' for cross-repository review dismissal.
    # Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' where review dismissals
    # are allowed. The target repository (current or target-repo) is always implicitly
    # allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # All of these labels must be present on the target item for this operation to
    # proceed
    # (optional)
    required-labels: []
      # Array of strings

    # The target item's title must start with this prefix for this operation to
    # proceed
    # (optional)
    required-title-prefix: "example-value"

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable pull request review dismissal with default configuration
  dismiss-pull-request-review: null

  # Enable AI agents to dismiss pull request reviews authored by the current
  # workflow actor.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for dismissing pull request reviews previously submitted
  # by the current workflow actor.
  dismiss-review:
    # Maximum number of review dismissals to perform (default: 10) Supports integer or
    # GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target PR for review dismissal: 'triggering' (default, current PR), '*' (any PR,
    # requires pull_request_number in agent output), or explicit PR number.
    # (optional)
    target: "example-value"

    # Target repository in format 'owner/repo' for cross-repository review dismissal.
    # Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' where review dismissals
    # are allowed. The target repository (current or target-repo) is always implicitly
    # allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # All of these labels must be present on the target item for this operation to
    # proceed
    # (optional)
    required-labels: []
      # Array of strings

    # The target item's title must start with this prefix for this operation to
    # proceed
    # (optional)
    required-title-prefix: "example-value"

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable pull request review dismissal with default configuration
  dismiss-review: null

  # Enable AI agents to reply to existing review comments on pull requests.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for replying to existing pull request review comments
  reply-to-pull-request-review-comment:
    # Maximum number of replies to create (default: 10) Supports integer or GitHub
    # Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target for replies: 'triggering' (default), '*' (any PR), or explicit PR number
    # (optional)
    target: "example-value"

    # Target repository in format 'owner/repo' for cross-repository operations
    # (optional)
    target-repo: "example-value"

    # List of additional repositories that replies can target
    # (optional)
    allowed-repos: []
      # Array of strings

    # Controls whether AI-generated footer is added to the reply body. When false, the
    # footer is omitted. Defaults to true.
    # (optional)
    footer: true

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # All of these labels must be present on the target item for this operation to
    # proceed
    # (optional)
    required-labels: []
      # Array of strings

    # The target item's title must start with this prefix for this operation to
    # proceed
    # (optional)
    required-title-prefix: "example-value"

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable with default configuration
  reply-to-pull-request-review-comment: null

  # Enable AI agents to resolve review threads on pull requests after addressing
  # feedback. Supports cross-repository resolution via target, target-repo, and
  # allowed-repos.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for resolving review threads on pull requests. By
  # default, resolution is scoped to the triggering PR only. When target,
  # target-repo, or allowed-repos are specified, cross-repository thread resolution
  # is supported.
  resolve-pull-request-review-thread:
    # Maximum number of review threads to resolve (default: 10) Supports integer or
    # GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target PR for thread resolution: 'triggering' (default, current PR), '*' (any
    # PR, requires pull_request_number in agent output), or explicit PR number (e.g.
    # ${{ github.event.inputs.pr_number }}). Required when workflow is not triggered
    # by a pull request (e.g. workflow_dispatch).
    # (optional)
    target: "example-value"

    # Target repository in format 'owner/repo' for cross-repository PR review thread
    # resolution. Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that PR review threads
    # can be resolved in. When specified, the agent can use a 'repo' field in the
    # output to specify which repository to resolve threads in. The target repository
    # (current or target-repo) is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # All of these labels must be present on the target item for this operation to
    # proceed
    # (optional)
    required-labels: []
      # Array of strings

    # The target item's title must start with this prefix for this operation to
    # proceed
    # (optional)
    required-title-prefix: "example-value"

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable review thread resolution with default configuration
  resolve-pull-request-review-thread: null

  # Enable AI agents to create GitHub Advanced Security code scanning alerts for
  # detected vulnerabilities or security issues.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for creating repository security advisories (SARIF
  # format) from agentic workflow output
  create-code-scanning-alert:
    # Maximum number of security findings to include (default: unlimited) Supports
    # integer or GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Driver name for SARIF tool.driver.name field (default: 'GitHub Agentic Workflows
    # Security Scanner')
    # (optional)
    driver: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # Target repository in format 'owner/repo' for cross-repository code scanning
    # alert creation. Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that code scanning alerts
    # can be created in. When specified, the agent can use a 'repo' field in the
    # output to specify which repository to create the alert in. The target repository
    # (current or target-repo) is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable code scanning alert creation with default configuration
  # (unlimited findings)
  create-code-scanning-alert: null

  # Enable AI agents to create autofixes for code scanning alerts using the GitHub
  # REST API.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for creating autofixes for code scanning alerts
  autofix-code-scanning-alert:
    # Maximum number of autofixes to create (default: 10) Supports integer or GitHub
    # Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable code scanning autofix creation with default configuration (max:
  # 10)
  autofix-code-scanning-alert: null

  # Enable AI agents to create GitHub Check Runs that surface analysis results in
  # the PR checks UI. Requires checks: write permission.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for creating GitHub Check Runs to surface agent analysis
  # results on commits and pull requests
  create-check-run:
    # Check run name shown in the GitHub Checks UI (e.g., 'Security Analysis'). If
    # omitted, defaults to the workflow name.
    # (optional)
    name: "My Workflow"

    # Target pull request for check run attachment: 'triggering', '*' (any PR), or
    # explicit PR number
    # (optional)
    target: "example-value"

    # Maximum number of check runs to create per workflow run (default: 1). Supports
    # integer or GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App credentials for minting an installation access token scoped to
    # checks:write for this handler. When set, a short-lived token is minted before
    # the handler runs and revoked afterwards.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

    # Optional static fallback values for the check run output fields. Used when the
    # agent does not provide the corresponding field.
    # (optional)
    output:
      # Fallback title for the check run output (max 256 characters). Used when the
      # agent does not supply a title.
      # (optional)
      title: "example-value"

      # Fallback summary for the check run output (max 65535 characters, GitHub API
      # limit). Used when the agent does not supply a summary.
      # (optional)
      summary: "example-value"

  # Format 2: Enable check run creation with default configuration (max: 1)
  create-check-run: null

  # Enable AI agents to add labels to GitHub issues or pull requests based on
  # workflow analysis or classification.
  # (optional)
  # Accepted formats:

  # Format 1: Null configuration allows any labels. Labels must already exist in the
  # repository unless 'create-if-missing' is enabled.
  add-labels: null

  # Format 2: Configuration for adding labels to issues/PRs from agentic workflow
  # output. Labels must already exist in the repository unless 'create-if-missing'
  # is enabled.
  add-labels:
    # Optional list of allowed labels that can be added. Labels must already exist in
    # the repository unless 'create-if-missing' is enabled. If omitted, any labels are
    # allowed.
    # (optional)
    allowed: []
      # Array of strings

    # Optional list of blocked label patterns (supports glob patterns like '~*',
    # '*[bot]'). Labels matching these patterns will be rejected. Applied before
    # allowed list filtering for security.
    # (optional)
    blocked: []
      # Array of strings

    # Optional maximum number of labels to add (default: 3) Supports integer or GitHub
    # Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # When false, excludes issues:write from the minted GitHub App token for
    # add-labels. Default (omitted or true) includes issues:write.
    # (optional)
    issues: true

    # When false, excludes pull-requests:write from the minted GitHub App token for
    # add-labels. Default (omitted or true) includes pull-requests:write.
    # (optional)
    pull-requests: true

    # Target for labels: 'triggering' (default), '*' (any issue/PR), or explicit
    # issue/PR number
    # (optional)
    target: "example-value"

    # Target repository in format 'owner/repo' for cross-repository label addition.
    # Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # List of additional repositories in format 'owner/repo' that labels can be added
    # to. When specified, the agent can use a 'repo' field in the output to specify
    # which repository to add labels to. The target repository (current or
    # target-repo) is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # Conjunctive label constraint: ALL of these labels must be present on the
    # issue/PR for the operation to proceed.
    # (optional)
    required-labels: []
      # Array of strings

    # Title prefix constraint: the issue/PR title must start with this prefix for the
    # operation to proceed.
    # (optional)
    required-title-prefix: "example-value"

    # Enable issue-intent metadata support (rationale/confidence/suggest) for this
    # output type.
    # (optional)
    issue-intent: true

    # When true, automatically creates labels that don't already exist in the target
    # repository. Default (omitted or false) rejects labels that don't already exist.
    # (optional)
    create-if-missing: true

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Enable AI agents to remove labels from GitHub issues or pull requests.
  # (optional)
  # Accepted formats:

  # Format 1: Null configuration allows any labels to be removed.
  remove-labels: null

  # Format 2: Configuration for removing labels from issues/PRs from agentic
  # workflow output.
  remove-labels:
    # Optional list of allowed labels that can be removed. If omitted, any labels can
    # be removed.
    # (optional)
    allowed: []
      # Array of strings

    # Optional list of blocked label patterns (supports glob patterns like '~*',
    # '*[bot]'). Labels matching these patterns will be rejected. Applied before
    # allowed list filtering for security.
    # (optional)
    blocked: []
      # Array of strings

    # Optional maximum number of labels to remove (default: 3) Supports integer or
    # GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # When false, excludes issues:write for remove-labels from both the safe_outputs
    # job permissions and any minted GitHub App token. Default (omitted or true)
    # includes issues:write.
    # (optional)
    issues: true

    # When false, excludes pull-requests:write for remove-labels from both the
    # safe_outputs job permissions and any minted GitHub App token. Default (omitted
    # or true) includes pull-requests:write.
    # (optional)
    pull-requests: true

    # Target for labels: 'triggering' (default), '*' (any issue/PR), or explicit
    # issue/PR number
    # (optional)
    target: "example-value"

    # Target repository in format 'owner/repo' for cross-repository label removal.
    # Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # List of additional repositories in format 'owner/repo' that labels can be
    # removed from. When specified, the agent can use a 'repo' field in the output to
    # specify which repository to remove labels from. The target repository (current
    # or target-repo) is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # Conjunctive label constraint: ALL of these labels must be present on the
    # issue/PR for the operation to proceed.
    # (optional)
    required-labels: []
      # Array of strings

    # Title prefix constraint: the issue/PR title must start with this prefix for the
    # operation to proceed.
    # (optional)
    required-title-prefix: "example-value"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Enable AI agents to request reviews from users or teams on pull requests based
  # on code changes or expertise matching.
  # (optional)
  # Accepted formats:

  # Format 1: Null configuration allows any reviewers
  add-reviewer: null

  # Format 2: Configuration for adding reviewers to pull requests from agentic
  # workflow output
  add-reviewer:
    # Optional list of allowed reviewer usernames. If omitted, any reviewers are
    # allowed.
    # (optional)
    allowed-reviewers: []
      # Array of strings

    # Optional allowed team reviewer or list of allowed team reviewers. If omitted,
    # any team reviewers are allowed.
    # (optional)
    # Accepted formats:

    # Format 1: string
    allowed-team-reviewers: "example-value"

    # Format 2: array
    allowed-team-reviewers: []
      # Array items: string

    # Optional maximum number of reviewers to add (default: 3) Supports integer or
    # GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target for reviewers: 'triggering' (default), '*' (any PR), or explicit PR
    # number
    # (optional)
    target: "example-value"

    # Target repository in format 'owner/repo' for cross-repository reviewer addition.
    # Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that reviewers can be
    # added in. The target repository is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # All of these labels must be present on the target item for this operation to
    # proceed
    # (optional)
    required-labels: []
      # Array of strings

    # The target item's title must start with this prefix for this operation to
    # proceed
    # (optional)
    required-title-prefix: "example-value"

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Enable AI agents to assign GitHub milestones to issues or pull requests based on
  # workflow analysis or project planning.
  # (optional)
  # Accepted formats:

  # Format 1: Null configuration allows assigning any milestones
  assign-milestone: null

  # Format 2: Configuration for assigning issues to milestones from agentic workflow
  # output
  assign-milestone:
    # Optional list of allowed milestone titles that can be assigned. If omitted, any
    # milestones are allowed.
    # (optional)
    allowed: []
      # Array of strings

    # Automatically create missing milestones from the allowed list.
    # (optional)
    auto_create: true

    # Optional maximum number of milestone assignments (default: 1) Supports integer
    # or GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target for milestone assignment: 'triggering' (default, current issue/PR), '*'
    # (any issue/PR), or explicit number.
    # (optional)
    target: "example-value"

    # Target repository in format 'owner/repo' for cross-repository milestone
    # assignment. Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that milestone
    # assignments can target. The target repository is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # All of these labels must be present on the target item for this operation to
    # proceed
    # (optional)
    required-labels: []
      # Array of strings

    # The target item's title must start with this prefix for this operation to
    # proceed
    # (optional)
    required-title-prefix: "example-value"

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Enable AI agents to assign issues or pull requests to GitHub Copilot (@copilot)
  # for automated handling.
  # (optional)
  # Accepted formats:

  # Format 1: Null configuration uses default agent (copilot)
  assign-to-agent: null

  # Format 2: Configuration for assigning GitHub Copilot coding agent to issues from
  # agentic workflow output
  assign-to-agent:
    # Default agent name to assign (default: 'copilot')
    # (optional)
    name: "My Workflow"

    # Default AI model to use for the agent (e.g., 'auto', 'claude-sonnet-4.5',
    # 'claude-opus-4.5', 'claude-opus-4.6', 'gpt-5.1-codex-max', 'gpt-5.2-codex').
    # Defaults to 'auto' if not specified.
    # (optional)
    model: "example-value"

    # Optional reasoning effort to forward without model-specific capability checks.
    # Supports none, minimal, low, medium, high, xhigh, and GitHub Actions
    # expressions. Invalid runtime values are ignored with a warning.
    # (optional)
    reasoning-effort: "example-value"

    # Default custom agent ID to use when assigning custom agents. This is used for
    # specialized agent configurations beyond the standard Copilot agent.
    # (optional)
    custom-agent: "example-value"

    # Default custom instructions to provide to the agent. These instructions will
    # guide the agent's behavior when working on the task.
    # (optional)
    custom-instructions: "example-value"

    # Optional list of allowed agent names. If specified, only these agents can be
    # assigned. When configured, existing agent assignees not in the list are removed
    # while regular user assignees are preserved.
    # (optional)
    allowed: []
      # Array of strings

    # Optional maximum number of agent assignments (default: 1) Supports integer or
    # GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target issue/PR to assign agents to. Use 'triggering' (default) for the
    # triggering issue/PR, '*' to require explicit issue_number/pull_number, or a
    # specific issue/PR number. With 'triggering', auto-resolves from
    # github.event.issue.number or github.event.pull_request.number.
    # (optional)
    target: null

    # Target repository in format 'owner/repo' for cross-repository agent assignment.
    # Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that agents can be
    # assigned in. The target repository is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # Target repository where the pull request should be created, in format
    # 'owner/repo'. If omitted, the PR will be created in the same repository as the
    # issue (specified by target-repo or the workflow's repository). This allows
    # issues and code to live in different repositories.
    # (optional)
    pull-request-repo: "example-value"

    # List of additional repositories that pull requests can be created in beyond
    # pull-request-repo. Each entry should be in 'owner/repo' format. The repository
    # specified by pull-request-repo is automatically allowed without needing to be
    # listed here.
    # (optional)
    allowed-pull-request-repos: []
      # Array of strings

    # If true, the workflow continues gracefully when agent assignment fails (e.g.,
    # due to missing token or insufficient permissions), logging a warning instead of
    # failing. Default is false. Useful for workflows that should not fail when agent
    # assignment is optional.
    # (optional)
    ignore-if-error: true

    # Base branch for pull request creation in the target repository. Defaults to the
    # target repo's default branch. Only relevant when pull-request-repo is
    # configured.
    # (optional)
    base-branch: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Enable issue-intent metadata support (rationale/confidence/suggest) for this
    # output type.
    # (optional)
    issue-intent: true

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Enable AI agents to assign issues or pull requests to specific GitHub users
  # based on workflow logic or expertise matching.
  # (optional)
  # Accepted formats:

  # Format 1: Enable user assignment with default configuration
  assign-to-user: null

  # Format 2: Configuration for assigning users to issues from agentic workflow
  # output
  assign-to-user:
    # Optional list of allowed usernames. If specified, only these users can be
    # assigned.
    # (optional)
    allowed: []
      # Array of strings

    # Optional list of blocked usernames or patterns (e.g., 'copilot', '*[bot]').
    # Users matching these patterns cannot be assigned. Supports exact matches and
    # glob patterns.
    # (optional)
    blocked: []
      # Array of strings

    # Optional maximum number of user assignments (default: 1) Supports integer or
    # GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target issue to assign users to. Use 'triggering' (default) for the triggering
    # issue, '*' to allow any issue, or a specific issue number.
    # (optional)
    target: null

    # Target repository in format 'owner/repo' for cross-repository user assignment.
    # Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # If true, unassign all current assignees before assigning new ones. Useful for
    # reassigning issues from one user to another (default: false).
    # (optional)
    unassign-first: true

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # List of allowed repositories in format 'owner/repo' for cross-repository user
    # assignment operations. Use with 'repo' field in tool calls.
    # (optional)
    allowed-repos: []
      # Array of strings

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Enable issue-intent metadata support (rationale/confidence/suggest) for this
    # output type.
    # (optional)
    issue-intent: true

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # All of these labels must be present on the target item for this operation to
    # proceed
    # (optional)
    required-labels: []
      # Array of strings

    # The target item's title must start with this prefix for this operation to
    # proceed
    # (optional)
    required-title-prefix: "example-value"

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Enable AI agents to unassign users from issues or pull requests. Useful for
  # reassigning work or removing users from issues.
  # (optional)
  # Accepted formats:

  # Format 1: Enable user unassignment with default configuration
  unassign-from-user: null

  # Format 2: Configuration for removing assignees from issues in agentic workflow
  # output
  unassign-from-user:
    # Optional list of allowed usernames. If specified, only these users can be
    # unassigned.
    # (optional)
    allowed: []
      # Array of strings

    # Optional list of blocked usernames or patterns (e.g., 'copilot', '*[bot]').
    # Users matching these patterns cannot be unassigned. Supports exact matches and
    # glob patterns.
    # (optional)
    blocked: []
      # Array of strings

    # Optional maximum number of unassignment operations (default: 1) Supports integer
    # or GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target issue to unassign users from. Use 'triggering' (default) for the
    # triggering issue, '*' to allow any issue, or a specific issue number.
    # (optional)
    target: null

    # Target repository in format 'owner/repo' for cross-repository user unassignment.
    # Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of allowed repositories in format 'owner/repo' for cross-repository
    # unassignment operations. Use with 'repo' field in tool calls.
    # (optional)
    allowed-repos: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # All of these labels must be present on the target item for this operation to
    # proceed
    # (optional)
    required-labels: []
      # Array of strings

    # The target item's title must start with this prefix for this operation to
    # proceed
    # (optional)
    required-title-prefix: "example-value"

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Enable AI agents to create hierarchical relationships between issues using
  # GitHub's sub-issue (tasklist) feature.
  # (optional)
  # Accepted formats:

  # Format 1: Enable sub-issue linking with default configuration
  link-sub-issue: null

  # Format 2: Configuration for linking issues as sub-issues from agentic workflow
  # output
  link-sub-issue:
    # Maximum number of sub-issue links to create (default: 5) Supports integer or
    # GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target for sub-issue linking: 'triggering' (default, current issue), '*' (any
    # issue), or explicit issue number.
    # (optional)
    target: "example-value"

    # Optional list of labels that parent issues must have to be eligible for linking
    # (optional)
    parent-required-labels: []
      # Array of strings

    # Optional title prefix that parent issues must have to be eligible for linking
    # (optional)
    parent-title-prefix: "example-value"

    # Optional list of labels that sub-issues must have to be eligible for linking
    # (optional)
    sub-required-labels: []
      # Array of strings

    # Optional title prefix that sub-issues must have to be eligible for linking
    # (optional)
    sub-title-prefix: "example-value"

    # Target repository in format 'owner/repo' for cross-repository sub-issue linking.
    # Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that sub-issue linking
    # can target. The target repository is always implicitly allowed. Accepts an array
    # or a GitHub Actions expression resolving to a comma-separated list (e.g. '${{
    # inputs[\'allowed-repos\'] }}').
    # (optional)
    # Accepted formats:

    # Format 1: Array of repository slugs in 'owner/repo' format
    allowed-repos: []
      # Array items: string

    # Format 2: GitHub Actions expression resolving to a comma-separated list of
    # repository slugs (e.g. '${{ inputs[\'allowed-repos\'] }}')
    allowed-repos: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Enable AI agents to edit and update existing GitHub issue content, titles,
  # labels, assignees, and metadata.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for updating GitHub issues from agentic workflow output
  update-issue:
    # Allow updating issue status (open/closed) - presence of key indicates field can
    # be updated
    # (optional)
    status: null

    # Target for updates: 'triggering' (default), '*' (any issue), or explicit issue
    # number
    # (optional)
    target: "example-value"

    # Allow updating issue title - presence of key indicates field can be updated
    # (optional)
    title: null

    # Allow updating issue body. Set to true to enable body updates, false to disable.
    # For backward compatibility, null (body:) also enables body updates.
    # (optional)
    body: null

    # Controls whether AI-generated footer is added when updating the issue body. When
    # false, the visible footer content is omitted but XML markers are still included.
    # Defaults to true. Only applies when 'body' is enabled.
    # (optional)
    footer: true

    # Maximum number of issues to update (default: 1) Supports integer or GitHub
    # Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target repository in format 'owner/repo' for cross-repository issue updates.
    # Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # Required prefix for issue title. Only issues with this title prefix can be
    # updated.
    # (optional)
    title-prefix: "example-value"

    # List of additional repositories in format 'owner/repo' that issues can be
    # updated in. When specified, the agent can use a 'repo' field in the output to
    # specify which repository to update the issue in. The target repository (current
    # or target-repo) is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # All of these labels must be present on the target item for this operation to
    # proceed
    # (optional)
    required-labels: []
      # Array of strings

    # The target item's title must start with this prefix for this operation to
    # proceed
    # (optional)
    required-title-prefix: "example-value"

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable issue updating with default configuration
  update-issue: null

  # Enable AI agents to edit and update existing pull request content, titles,
  # labels, reviewers, and metadata.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for updating GitHub pull requests from agentic workflow
  # output. Both title and body updates are enabled by default.
  update-pull-request:
    # Target for updates: 'triggering' (default), '*' (any PR), or explicit PR number
    # (optional)
    target: "example-value"

    # Allow updating pull request title - defaults to true, set to false to disable
    # (optional)
    title: true

    # Allow updating pull request body - defaults to true, set to false to disable
    # (optional)
    body: true

    # When true, update the pull request branch with the latest base branch changes
    # before applying other updates. Defaults to false.
    # (optional)
    update-branch: true

    # When true, allow stacked-PR stack-sync fallback when update-branch is
    # unsupported. Defaults to true. Set to false to disable stacked-PR sync fallback.
    # (optional)
    sync-stack: true

    # Default operation for body updates: 'append' (add to end), 'prepend' (add to
    # start), 'replace' (overwrite completely), or 'replace-island' (update a
    # run-specific section). Defaults to 'replace' if not specified.
    # (optional)
    operation: "append"

    # Controls whether AI-generated footer is added when updating the pull request
    # body. When false, the visible footer content is omitted but XML markers are
    # still included. Defaults to true. Only applies when 'body' is enabled.
    # (optional)
    footer: true

    # Maximum number of pull requests to update (default: 1) Supports integer or
    # GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target repository in format 'owner/repo' for cross-repository pull request
    # updates. Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that pull requests can be
    # updated in. The target repository is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # All of these labels must be present on the target item for this operation to
    # proceed
    # (optional)
    required-labels: []
      # Array of strings

    # The target item's title must start with this prefix for this operation to
    # proceed
    # (optional)
    required-title-prefix: "example-value"

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable pull request updating with default configuration (title and
  # body updates enabled)
  update-pull-request: null

  # Enable AI agents to merge pull requests under configured policy gates.
  # (optional)
  # Accepted formats:

  # Format 1: Enable pull request merge with default policy configuration
  merge-pull-request: null

  # Format 2: Configuration for controlled pull request merges. The merge is blocked
  # unless all configured gates pass.
  merge-pull-request:
    # Maximum number of pull request merges to perform per run (default: 1). Supports
    # integer or GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # List of labels that must ALL be present on the pull request before merge is
    # allowed.
    # (optional)
    required-labels: []
      # Array of strings

    # Glob patterns for allowed source branch names (pull request head ref). The PR's
    # branch must match at least one pattern.
    # (optional)
    allowed-branches: []
      # Array of strings

    # Target for merging: 'triggering' (default, current PR), or '*' (any PR with
    # pull_request_number field)
    # (optional)
    target: "example-value"

    # Target repository in format 'owner/repo' for cross-repository operations. Takes
    # precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that pull requests can be
    # merged in. The target repository is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, evaluate merge gates and emit preview results without executing the
    # merge API call.
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # The target item's title must start with this prefix for this operation to
    # proceed
    # (optional)
    required-title-prefix: "example-value"

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Enable AI agents to push commits directly to pull request branches for automated
  # fixes or improvements.
  # (optional)
  # Accepted formats:

  # Format 1: Use default configuration (branch: 'triggering', if-no-changes:
  # 'warn')
  push-to-pull-request-branch: null

  # Format 2: Configuration for pushing changes to a specific branch from agentic
  # workflow output. Supports pushing to multiple PRs in a single run when max > 1.
  push-to-pull-request-branch:
    # Maximum number of push operations to perform (default: 1). Each push targets a
    # different pull request branch. Supports integer or GitHub Actions expression
    # (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # The branch to push changes to (defaults to 'triggering')
    # (optional)
    branch: "example-value"

    # Base branch of the target repository for incremental patch computation. Defaults
    # to the local checkout branch or the repository default branch.
    # (optional)
    base-branch: "example-value"

    # Target for push operations: 'triggering' (default), '*' (any pull request), or
    # explicit pull request number
    # (optional)
    target: "example-value"

    # Required prefix for pull request title. Only pull requests with this prefix will
    # be accepted.
    # (optional)
    required-title-prefix: "example-value"

    # Required labels for pull request validation. Only pull requests with all these
    # labels will be accepted. Accepts an array of label names or a GitHub Actions
    # expression resolving to a comma-separated list of labels (e.g. '${{
    # inputs[\'required-labels\'] }}').
    # (optional)
    # Accepted formats:

    # Format 1: Array of label names
    required-labels: []
      # Array items: string

    # Format 2: GitHub Actions expression resolving to a comma-separated list of label
    # names (e.g. '${{ inputs[\'required-labels\'] }}')
    required-labels: "example-value"

    # Behavior when no changes to push: 'warn' (default - log warning but succeed),
    # 'error' (fail the action), or 'ignore' (silent success)
    # (optional)
    if-no-changes: "warn"

    # When true, treat deleted/missing pull request branch errors as a skipped push
    # instead of a hard failure. Useful when the PR branch may be deleted before safe
    # outputs run.
    # (optional)
    ignore-missing-branch-failure: true

    # Optional suffix to append to generated commit titles (e.g., ' [skip ci]' to
    # prevent triggering CI on the commit)
    # (optional)
    commit-title-suffix: "example-value"

    # Maximum allowed size for git patches in kilobytes (KB) for
    # push-to-pull-request-branch only. Overrides safe-outputs max-patch-size for this
    # output type. Defaults to 4096 KB (4 MB) when unset.
    # (optional)
    max-patch-size: 1

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # Token used to push an empty commit after pushing changes to trigger CI events.
    # Works around the GITHUB_TOKEN limitation where pushes don't trigger workflow
    # runs. Defaults to the magic secret GH_AW_CI_TRIGGER_TOKEN if set in the
    # repository. Use a secret expression (e.g. '${{ secrets.CI_TOKEN }}') for a
    # custom token, or 'app' for GitHub App auth.
    # (optional)
    github-token-for-extra-empty-commit: "example-value"

    # When true (default), if pushing to the PR branch fails due to a
    # non-fast-forward/diverged branch, create a fallback pull request that targets
    # the original PR branch. Set to false to disable this behavior and avoid
    # requiring pull-requests: write permission.
    # (optional)
    fallback-as-pull-request: true

    # When true (default), signed commits are required and pushes use GitHub's
    # createCommitOnBranch GraphQL mutation so GitHub signs them. Set to false to use
    # git push directly for repositories that do not require signed commits; this also
    # allows pushing merge commits that GraphQL cannot represent.
    # (optional)
    signed-commits: true

    # Target repository in format 'owner/repo' for cross-repository push to pull
    # request branch, or '*' to let the agent choose the target repository at runtime
    # (requires checkout: configs with path: for each possible target). Takes
    # precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # Optional head repository in format 'owner/repo' that is allowed for fork-backed
    # pull request branch updates. When set, the pull request head repository must
    # match exactly.
    # (optional)
    head-repo: "example-value"

    # Optional GitHub token used only for branch writes to head-repo. Use this when
    # the upstream pull request repository and the fork head repository require
    # different credentials.
    # (optional)
    head-github-token: "example-value"

    # Optional GitHub App credentials to mint an ephemeral token for head-repo branch
    # writes at runtime. When configured, the minted token takes precedence over
    # head-github-token. The app installation must have contents: write on head-repo.
    # (optional)
    head-github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

    # List of additional repositories in format 'owner/repo' that push to pull request
    # branch can target. When specified, the agent can use a 'repo' field in the
    # output to specify which repository to push to. The target repository (current or
    # target-repo) is always implicitly allowed. Accepts an array or a GitHub Actions
    # expression resolving to a comma-separated list (e.g. '${{
    # inputs[\'allowed-repos\'] }}').
    # (optional)
    # Accepted formats:

    # Format 1: Array of repository slugs in 'owner/repo' format
    allowed-repos: []
      # Array items: string

    # Format 2: GitHub Actions expression resolving to a comma-separated list of
    # repository slugs (e.g. '${{ inputs[\'allowed-repos\'] }}')
    allowed-repos: "example-value"

    # Controls protected-file protection. String form: request-review (default),
    # blocked, allowed, or fallback-to-issue — or a GitHub Actions expression for
    # reusable workflows. The legacy request_review spelling is also accepted. Object
    # form: { policy, exclude } to customize the protected-file set.
    # (optional)
    # Accepted formats:

    # Format 1: Controls protected-file protection. request-review (default): create
    # the PR and submit a REQUEST_CHANGES review for manual scrutiny. blocked:
    # hard-block any patch that modifies package manifests (e.g. package.json,
    # go.mod), engine instruction files (e.g. AGENTS.md, CLAUDE.md) or .github/ files.
    # allowed: allow all changes. fallback-to-issue: create a review issue instead of
    # pushing to the PR branch, so a human can review the changes before applying. The
    # legacy request_review spelling is also accepted.
    protected-files: "blocked"

    # Format 2: GitHub Actions expression that resolves to 'blocked', 'allowed',
    # 'fallback-to-issue', 'request-review', or the legacy 'request_review' at
    # runtime. Use in reusable workflow_call workflows to parameterize the policy per
    # caller.
    protected-files: "example-value"

    # Format 3: Object form for granular control over the protected-file set. Use the
    # exclude list to remove specific files from the default protection while keeping
    # the rest.
    protected-files:
      # (optional)
      # Accepted formats:

      # Format 1: Protection policy. request-review (default): create the PR and submit
      # a REQUEST_CHANGES review. blocked: hard-block any patch that modifies protected
      # files. allowed: allow all changes. fallback-to-issue: create a review issue
      # instead of pushing. The legacy request_review spelling is also accepted.
      policy: "blocked"

      # Format 2: GitHub Actions expression that resolves to 'blocked', 'allowed',
      # 'fallback-to-issue', 'request-review', or the legacy 'request_review' at
      # runtime.
      policy: "example-value"

      # List of filenames or path prefixes to remove from the default protected-file
      # set. Items are matched by basename (e.g. "AGENTS.md") or path prefix (e.g.
      # ".agents/"). Use this to allow the agent to modify specific files that are
      # otherwise blocked by default.
      # (optional)
      exclude: []
        # Array of strings

    # Exclusive allowlist of glob patterns. When set, every file in the patch must
    # match at least one pattern — files outside the list are always refused,
    # including normal source files. This is a restriction, not an exception: setting
    # allowed-files: [".github/workflows/*"] blocks all other files. To allow multiple
    # sets of files, list all patterns explicitly. Acts independently of the
    # protected-files policy; both checks must pass. To modify a protected file, it
    # must both match allowed-files and be permitted by protected-files (e.g.
    # protected-files: allowed). Supports * (any characters except /) and ** (any
    # characters including /).
    # (optional)
    allowed-files: []
      # Array of strings

    # List of glob patterns for files to exclude from the patch. Each pattern is
    # passed to `git format-patch` as a `:(exclude)<pattern>` magic pathspec, so
    # matching files are stripped by git at generation time and will not appear in the
    # commit. Excluded files are also not subject to `allowed-files` or
    # `protected-files` checks. Supports * (any characters except /) and ** (any
    # characters including /).
    # (optional)
    excluded-files: []
      # Array of strings

    # Transport format for packaging changes. "bundle" (default) uses git bundle. "am"
    # uses git format-patch/git am. Accepts a GitHub Actions expression for reusable
    # workflows.
    # (optional)
    # Accepted formats:

    # Format 1: Transport format for packaging changes. "bundle" (default) uses git
    # bundle, which preserves merge commit topology, per-commit authorship, and
    # merge-resolution-only content. "am" uses git format-patch/git am.
    patch-format: "am"

    # Format 2: GitHub Actions expression that resolves to 'am' or 'bundle' at
    # runtime. Use in reusable workflow_call workflows to parameterize the transport
    # format per caller.
    patch-format: "example-value"

    # When true, adds workflows: write to the GitHub App token permissions. Required
    # when allowed-files targets .github/workflows/ paths. Requires
    # safe-outputs.github-app to be configured because the workflows permission is a
    # GitHub App-only permission and cannot be granted via GITHUB_TOKEN.
    # (optional)
    allow-workflows: true

    # When false, skips the branch protection API pre-flight check before pushing. Set
    # to false to avoid requiring administration: read permission. The GitHub platform
    # will still enforce branch protection at push time. Default is true (check
    # enabled).
    # (optional)
    check-branch-protection: true

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Enable AI agents to minimize (hide) comments on issues or pull requests based on
  # relevance, spam detection, or moderation rules.
  # (optional)
  # Accepted formats:

  # Format 1: Enable comment hiding with default configuration
  hide-comment: null

  # Format 2: Configuration for hiding comments on GitHub issues, pull requests, or
  # discussions from agentic workflow output
  hide-comment:
    # Maximum number of comments to hide (default: 5) Supports integer or GitHub
    # Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target for comment hiding: 'triggering' (default, current issue/PR), '*' (any
    # item), or explicit number.
    # (optional)
    target: "example-value"

    # Target repository in format 'owner/repo' for cross-repository comment hiding.
    # Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that comment hiding can
    # target. The target repository is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # List of allowed reasons for hiding comments. Default: all reasons allowed (spam,
    # abuse, off_topic, outdated, resolved, low_quality).
    # (optional)
    allowed-reasons: []
      # Array of strings

    # Controls whether the workflow requests discussions:write permission for
    # hide-comment. Default: false (excludes discussions:write). Set to true if you
    # need to hide comments on discussions.
    # (optional)
    discussions: true

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # All of these labels must be present on the target item for this operation to
    # proceed
    # (optional)
    required-labels: []
      # Array of strings

    # The target item's title must start with this prefix for this operation to
    # proceed
    # (optional)
    required-title-prefix: "example-value"

  # Enable AI agents to set or clear the type of GitHub issues. Use an empty string
  # to clear the current type.
  # (optional)
  # Accepted formats:

  # Format 1: Null configuration allows setting any issue type
  set-issue-type: null

  # Format 2: Configuration for setting the type of GitHub issues from agentic
  # workflow output
  set-issue-type:
    # Optional list of allowed issue type names (e.g. 'Bug', 'Feature'). If omitted,
    # any type is allowed. Empty string is always allowed to clear the type.
    # (optional)
    allowed: []
      # Array of strings

    # Optional maximum number of set-issue-type operations (default: 5). Supports
    # integer or GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target for issue type: 'triggering' (default), '*' (any issue), or explicit
    # issue number
    # (optional)
    target: "example-value"

    # Target repository in format 'owner/repo' for cross-repository issue type
    # setting. Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' where issue types can be
    # set. When specified, the agent can use a 'repo' field in the output to specify
    # which repository to target. The target repository (current or target-repo) is
    # always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # Optional GitHub token used only for branch writes to head-repo. Use this when
    # the upstream pull request repository and the fork head repository require
    # different credentials.
    # (optional)
    head-github-token: "example-value"

    # Enable issue-intent metadata support (rationale/confidence/suggest) for this
    # output type.
    # (optional)
    issue-intent: true

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # All of these labels must be present on the target item for this operation to
    # proceed
    # (optional)
    required-labels: []
      # Array of strings

    # The target item's title must start with this prefix for this operation to
    # proceed
    # (optional)
    required-title-prefix: "example-value"

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Enable AI agents to set one issue field value by field name and value.
  # (optional)
  # Accepted formats:

  # Format 1: Null configuration enables set-issue-field with defaults.
  set-issue-field: null

  # Format 2: Configuration for setting one issue field value by field name and
  # value.
  set-issue-field:
    # Optional maximum number of set-issue-field operations (default: 5). Supports
    # integer or GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target for issue field updates: 'triggering' (default), '*' (any issue), or
    # explicit issue number
    # (optional)
    target: "example-value"

    # Optional list of issue field names that can be modified by set-issue-field. If
    # omitted or empty, any issue field may be set. Use ['*'] to explicitly allow all.
    # (optional)
    allowed-fields: []
      # Array of strings

    # Target repository in format 'owner/repo' for cross-repository issue field
    # updates. Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' where issue fields can be
    # updated. When specified, the agent can use a 'repo' field in the output to
    # specify which repository to target. The target repository (current or
    # target-repo) is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # Enable issue-intent metadata support (rationale/confidence/suggest) for this
    # output type.
    # (optional)
    issue-intent: true

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # All of these labels must be present on the target item for this operation to
    # proceed
    # (optional)
    required-labels: []
      # Array of strings

    # The target item's title must start with this prefix for this operation to
    # proceed
    # (optional)
    required-title-prefix: "example-value"

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Dispatch workflow_dispatch events to other workflows. Used by orchestrators to
  # delegate work to worker workflows with controlled maximum dispatch count.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for dispatching workflow_dispatch events to other
  # workflows. Orchestrators use this to delegate work to worker workflows.
  dispatch-workflow:
    # List of workflow names (without .md extension) to allow dispatching. Each
    # workflow must exist in .github/workflows/.
    workflows: []
      # Array of strings

    # Maximum number of workflow dispatch operations per run (default: 1, max: 50)
    # Supports integer or GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # GitHub token to use for dispatching workflows. Overrides global github-token if
    # specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # Target repository in format 'owner/repo' for cross-repository workflow dispatch.
    # When specified, the workflow will be dispatched to the target repository instead
    # of the current one.
    # (optional)
    target-repo: "example-value"

    # List of repositories in format 'owner/repo' that cross-repository workflow
    # dispatch may target. Supports arrays and GitHub Actions expressions resolving to
    # a comma-separated list (e.g. '${{ inputs['allowed-repos'] }}').
    # (optional)
    # Accepted formats:

    # Format 1: array
    allowed-repos: []
      # Array items: string

    # Format 2: GitHub Actions expression resolving to a comma-separated list of
    # repository slugs (e.g. '${{ inputs['allowed-repos'] }}')
    allowed-repos: "example-value"

    # List of allowed ref glob patterns for per-call dispatch_workflow message.ref
    # overrides. Supports arrays and GitHub Actions expressions resolving to a
    # comma-separated list (e.g. '${{ inputs['allowed-refs'] }}').
    # (optional)
    # Accepted formats:

    # Format 1: array
    allowed-refs: []
      # Array items: string

    # Format 2: GitHub Actions expression resolving to a comma-separated list of ref
    # glob patterns (e.g. '${{ inputs['allowed-refs'] }}')
    allowed-refs: "example-value"

    # Git ref (branch, tag, or SHA) to use when dispatching the workflow. For
    # workflow_call relay scenarios this is auto-injected by the compiler from
    # needs.activation.outputs.target_ref. Overrides the caller's GITHUB_REF.
    # (optional)
    target-ref: "example-value"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Shorthand array format: list of workflow names (without .md extension)
  # to allow dispatching
  dispatch-workflow: []
    # Array items: string

  # Dispatch repository_dispatch events to external repositories. Each sub-key
  # defines a named dispatch tool with its own event_type, target repository, input
  # schema, and execution limits. Note: nested keys use snake_case (event_type,
  # allowed_repositories) rather than the kebab-case used by other safe-output
  # fields.
  # (optional)
  dispatch-repository:
    # Configuration for a single repository dispatch tool
    trigger-ci:
      # Human-readable description of what this dispatch tool does
      # (optional)
      description: "Description of the workflow"

      # Target workflow name (for traceability and inclusion in client_payload)
      workflow: "example-value"

      # The repository_dispatch event_type string sent to the target repository
      event_type: "example-value"

      # Target repository in 'owner/repo' format. Dispatches to this repository when no
      # 'allowed_repositories' list is given.
      # (optional)
      repository: "example-value"

      # List of allowed target repositories (owner/repo). Supports glob patterns like
      # 'org/*'.
      # (optional)
      allowed_repositories: []
        # Array of strings

      # Input schema for the dispatch tool. Inputs are validated and compiled into
      # client_payload.
      # (optional)
      inputs:
        repo-name:
          # Input type
          # (optional)
          type: "string"

          # Input description
          # (optional)
          description: "Description of the workflow"

          # Whether this input is required
          # (optional)
          required: true

          # Default value for this input
          # (optional)
          default: null

          # Allowed options for 'choice' type inputs
          # (optional)
          options: []
            # Array of strings

      # Maximum number of dispatch executions for this tool per run (default: 1, max:
      # 50). Supports integer or GitHub Actions expression.
      # (optional)
      # Accepted formats:

      # Format 1: integer
      max: 1

      # Format 2: GitHub Actions expression that resolves to an integer at runtime
      max: "example-value"

      # GitHub token to use for dispatching. Overrides global github-token if specified.
      # (optional)
      github-token: "${{ secrets.GITHUB_TOKEN }}"

      # When true, emit step summary messages instead of making GitHub API calls
      # (preview mode)
      # (optional)
      # Accepted formats:

      # Format 1: boolean
      staged: true

      # Format 2: GitHub Actions expression that resolves to a boolean at runtime
      staged: "example-value"

      # GitHub App authentication. Mints a short-lived installation access token via
      # actions/create-github-app-token. Mutually exclusive with github-token.
      # (optional)
      github-app:
        # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
        # }}').
        # (optional)
        app-id: "example-value"

        # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
        # token.
        # (optional)
        client-id: "example-value"

        # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
        # mint a GitHub App token.
        # (optional)
        private-key: "example-value"

        # If true, skip token minting when client-id/private-key resolve to empty strings
        # at runtime. Defaults to false.
        # (optional)
        ignore-if-missing: true

        # Optional owner of the GitHub App installation (defaults to current repository
        # owner if not specified)
        # (optional)
        owner: "example-value"

        # Optional list of repositories to grant access to (defaults to current repository
        # if not specified)
        # (optional)
        repositories: []
          # Array of strings

        # Optional extra GitHub App-only permissions to merge into the minted token. Takes
        # effect for tools.github.github-app and safe-outputs.github-app; ignored in
        # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
        # scopes (e.g. members, organization-administration) not expressible via standard
        # handler declarations.
        # (optional)
        permissions:
          # Permission level for repository administration (read/none; "write" is rejected
          # by the compiler). GitHub App-only permission for repository administration.
          # (optional)
          administration: "read"

          # Permission level for Codespaces (read/none; "write" is rejected by the
          # compiler). GitHub App-only permission.
          # (optional)
          codespaces: "read"

          # Permission level for Codespaces lifecycle administration (read/none; "write" is
          # rejected by the compiler). GitHub App-only permission.
          # (optional)
          codespaces-lifecycle-admin: "read"

          # Permission level for Codespaces metadata (read/none; "write" is rejected by the
          # compiler). GitHub App-only permission.
          # (optional)
          codespaces-metadata: "read"

          # Permission level for user email addresses (read/none; "write" is rejected by the
          # compiler). GitHub App-only permission.
          # (optional)
          email-addresses: "read"

          # Permission level for repository environments (read/none; "write" is rejected by
          # the compiler). GitHub App-only permission.
          # (optional)
          environments: "read"

          # Permission level for git signing (read/none; "write" is rejected by the
          # compiler). GitHub App-only permission.
          # (optional)
          git-signing: "read"

          # Permission level for organization members (read/none; "write" is rejected by the
          # compiler). Required for org team membership API calls.
          # (optional)
          members: "read"

          # Permission level for organization administration (read/none; "write" is rejected
          # by the compiler). GitHub App-only permission.
          # (optional)
          organization-administration: "read"

          # Permission level for organization announcement banners (read/none; "write" is
          # rejected by the compiler). GitHub App-only permission.
          # (optional)
          organization-announcement-banners: "read"

          # Permission level for organization Codespaces (read/none; "write" is rejected by
          # the compiler). GitHub App-only permission.
          # (optional)
          organization-codespaces: "read"

          # Permission level for organization Copilot (read/none; "write" is rejected by the
          # compiler). GitHub App-only permission.
          # (optional)
          organization-copilot: "read"

          # Permission level for organization custom org roles (read/none; "write" is
          # rejected by the compiler). GitHub App-only permission.
          # (optional)
          organization-custom-org-roles: "read"

          # Permission level for organization custom properties (read/none; "write" is
          # rejected by the compiler). GitHub App-only permission.
          # (optional)
          organization-custom-properties: "read"

          # Permission level for organization custom repository roles (read/none; "write" is
          # rejected by the compiler). GitHub App-only permission.
          # (optional)
          organization-custom-repository-roles: "read"

          # Permission level for organization events (read/none; "write" is rejected by the
          # compiler). GitHub App-only permission.
          # (optional)
          organization-events: "read"

          # Permission level for organization webhooks (read/none; "write" is rejected by
          # the compiler). GitHub App-only permission.
          # (optional)
          organization-hooks: "read"

          # Permission level for organization members management (read/none; "write" is
          # rejected by the compiler). GitHub App-only permission.
          # (optional)
          organization-members: "read"

          # Permission level for organization packages (read/none; "write" is rejected by
          # the compiler). GitHub App-only permission.
          # (optional)
          organization-packages: "read"

          # Permission level for organization personal access token requests (read/none;
          # "write" is rejected by the compiler). GitHub App-only permission.
          # (optional)
          organization-personal-access-token-requests: "read"

          # Permission level for organization personal access tokens (read/none; "write" is
          # rejected by the compiler). GitHub App-only permission.
          # (optional)
          organization-personal-access-tokens: "read"

          # Permission level for organization plan (read/none; "write" is rejected by the
          # compiler). GitHub App-only permission.
          # (optional)
          organization-plan: "read"

          # Permission level for organization self-hosted runners (read/none; "write" is
          # rejected by the compiler). GitHub App-only permission.
          # (optional)
          organization-self-hosted-runners: "read"

          # Permission level for organization user blocking (read/none; "write" is rejected
          # by the compiler). GitHub App-only permission.
          # (optional)
          organization-user-blocking: "read"

          # Permission level for repository custom properties (read/none; "write" is
          # rejected by the compiler). GitHub App-only permission.
          # (optional)
          repository-custom-properties: "read"

          # Permission level for secret scanning alerts (read/none). Forwarded as
          # permission-secret-scanning-alerts input for actions/create-github-app-token.
          # (optional)
          secret-scanning-alerts: "read"

          # Permission level for repository webhooks (read/none; "write" is rejected by the
          # compiler). GitHub App-only permission.
          # (optional)
          repository-hooks: "read"

          # Permission level for single file access (read/none; "write" is rejected by the
          # compiler). GitHub App-only permission.
          # (optional)
          single-file: "read"

          # Permission level for team discussions (read/none; "write" is rejected by the
          # compiler). GitHub App-only permission.
          # (optional)
          team-discussions: "read"

          # Permission level for Dependabot vulnerability alerts (read/none; "write" is
          # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
          # with a GitHub App, forwarded as permission-vulnerability-alerts input.
          # (optional)
          vulnerability-alerts: "read"

          # Permission level for GitHub Actions workflow files (read/none; "write" is
          # rejected by the compiler). GitHub App-only permission.
          # (optional)
          workflows: "read"

  # Deprecated alias for dispatch-repository.
  # (optional)
  dispatch_repository:
    # Configuration for a single repository dispatch tool
    trigger-ci:
      # Human-readable description of what this dispatch tool does
      # (optional)
      description: "Description of the workflow"

      # Target workflow name (for traceability and inclusion in client_payload)
      workflow: "example-value"

      # The repository_dispatch event_type string sent to the target repository
      event_type: "example-value"

      # Target repository in 'owner/repo' format. Dispatches to this repository when no
      # 'allowed_repositories' list is given.
      # (optional)
      repository: "example-value"

      # List of allowed target repositories (owner/repo). Supports glob patterns like
      # 'org/*'.
      # (optional)
      allowed_repositories: []
        # Array of strings

      # Input schema for the dispatch tool. Inputs are validated and compiled into
      # client_payload.
      # (optional)
      inputs:
        repo-name:
          # Input type
          # (optional)
          type: "string"

          # Input description
          # (optional)
          description: "Description of the workflow"

          # Whether this input is required
          # (optional)
          required: true

          # Default value for this input
          # (optional)
          default: null

          # Allowed options for 'choice' type inputs
          # (optional)
          options: []
            # Array of strings

      # Maximum number of dispatch executions for this tool per run (default: 1, max:
      # 50). Supports integer or GitHub Actions expression.
      # (optional)
      # Accepted formats:

      # Format 1: integer
      max: 1

      # Format 2: GitHub Actions expression that resolves to an integer at runtime
      max: "example-value"

      # GitHub token to use for dispatching. Overrides global github-token if specified.
      # (optional)
      github-token: "${{ secrets.GITHUB_TOKEN }}"

      # When true, emit step summary messages instead of making GitHub API calls
      # (preview mode)
      # (optional)
      # Accepted formats:

      # Format 1: boolean
      staged: true

      # Format 2: GitHub Actions expression that resolves to a boolean at runtime
      staged: "example-value"

      # GitHub App authentication. Mints a short-lived installation access token via
      # actions/create-github-app-token. Mutually exclusive with github-token.
      # (optional)
      github-app:
        # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
        # }}').
        # (optional)
        app-id: "example-value"

        # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
        # token.
        # (optional)
        client-id: "example-value"

        # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
        # mint a GitHub App token.
        # (optional)
        private-key: "example-value"

        # If true, skip token minting when client-id/private-key resolve to empty strings
        # at runtime. Defaults to false.
        # (optional)
        ignore-if-missing: true

        # Optional owner of the GitHub App installation (defaults to current repository
        # owner if not specified)
        # (optional)
        owner: "example-value"

        # Optional list of repositories to grant access to (defaults to current repository
        # if not specified)
        # (optional)
        repositories: []
          # Array of strings

        # Optional extra GitHub App-only permissions to merge into the minted token. Takes
        # effect for tools.github.github-app and safe-outputs.github-app; ignored in
        # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
        # scopes (e.g. members, organization-administration) not expressible via standard
        # handler declarations.
        # (optional)
        permissions:
          # Permission level for repository administration (read/none; "write" is rejected
          # by the compiler). GitHub App-only permission for repository administration.
          # (optional)
          administration: "read"

          # Permission level for Codespaces (read/none; "write" is rejected by the
          # compiler). GitHub App-only permission.
          # (optional)
          codespaces: "read"

          # Permission level for Codespaces lifecycle administration (read/none; "write" is
          # rejected by the compiler). GitHub App-only permission.
          # (optional)
          codespaces-lifecycle-admin: "read"

          # Permission level for Codespaces metadata (read/none; "write" is rejected by the
          # compiler). GitHub App-only permission.
          # (optional)
          codespaces-metadata: "read"

          # Permission level for user email addresses (read/none; "write" is rejected by the
          # compiler). GitHub App-only permission.
          # (optional)
          email-addresses: "read"

          # Permission level for repository environments (read/none; "write" is rejected by
          # the compiler). GitHub App-only permission.
          # (optional)
          environments: "read"

          # Permission level for git signing (read/none; "write" is rejected by the
          # compiler). GitHub App-only permission.
          # (optional)
          git-signing: "read"

          # Permission level for organization members (read/none; "write" is rejected by the
          # compiler). Required for org team membership API calls.
          # (optional)
          members: "read"

          # Permission level for organization administration (read/none; "write" is rejected
          # by the compiler). GitHub App-only permission.
          # (optional)
          organization-administration: "read"

          # Permission level for organization announcement banners (read/none; "write" is
          # rejected by the compiler). GitHub App-only permission.
          # (optional)
          organization-announcement-banners: "read"

          # Permission level for organization Codespaces (read/none; "write" is rejected by
          # the compiler). GitHub App-only permission.
          # (optional)
          organization-codespaces: "read"

          # Permission level for organization Copilot (read/none; "write" is rejected by the
          # compiler). GitHub App-only permission.
          # (optional)
          organization-copilot: "read"

          # Permission level for organization custom org roles (read/none; "write" is
          # rejected by the compiler). GitHub App-only permission.
          # (optional)
          organization-custom-org-roles: "read"

          # Permission level for organization custom properties (read/none; "write" is
          # rejected by the compiler). GitHub App-only permission.
          # (optional)
          organization-custom-properties: "read"

          # Permission level for organization custom repository roles (read/none; "write" is
          # rejected by the compiler). GitHub App-only permission.
          # (optional)
          organization-custom-repository-roles: "read"

          # Permission level for organization events (read/none; "write" is rejected by the
          # compiler). GitHub App-only permission.
          # (optional)
          organization-events: "read"

          # Permission level for organization webhooks (read/none; "write" is rejected by
          # the compiler). GitHub App-only permission.
          # (optional)
          organization-hooks: "read"

          # Permission level for organization members management (read/none; "write" is
          # rejected by the compiler). GitHub App-only permission.
          # (optional)
          organization-members: "read"

          # Permission level for organization packages (read/none; "write" is rejected by
          # the compiler). GitHub App-only permission.
          # (optional)
          organization-packages: "read"

          # Permission level for organization personal access token requests (read/none;
          # "write" is rejected by the compiler). GitHub App-only permission.
          # (optional)
          organization-personal-access-token-requests: "read"

          # Permission level for organization personal access tokens (read/none; "write" is
          # rejected by the compiler). GitHub App-only permission.
          # (optional)
          organization-personal-access-tokens: "read"

          # Permission level for organization plan (read/none; "write" is rejected by the
          # compiler). GitHub App-only permission.
          # (optional)
          organization-plan: "read"

          # Permission level for organization self-hosted runners (read/none; "write" is
          # rejected by the compiler). GitHub App-only permission.
          # (optional)
          organization-self-hosted-runners: "read"

          # Permission level for organization user blocking (read/none; "write" is rejected
          # by the compiler). GitHub App-only permission.
          # (optional)
          organization-user-blocking: "read"

          # Permission level for repository custom properties (read/none; "write" is
          # rejected by the compiler). GitHub App-only permission.
          # (optional)
          repository-custom-properties: "read"

          # Permission level for secret scanning alerts (read/none). Forwarded as
          # permission-secret-scanning-alerts input for actions/create-github-app-token.
          # (optional)
          secret-scanning-alerts: "read"

          # Permission level for repository webhooks (read/none; "write" is rejected by the
          # compiler). GitHub App-only permission.
          # (optional)
          repository-hooks: "read"

          # Permission level for single file access (read/none; "write" is rejected by the
          # compiler). GitHub App-only permission.
          # (optional)
          single-file: "read"

          # Permission level for team discussions (read/none; "write" is rejected by the
          # compiler). GitHub App-only permission.
          # (optional)
          team-discussions: "read"

          # Permission level for Dependabot vulnerability alerts (read/none; "write" is
          # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
          # with a GitHub App, forwarded as permission-vulnerability-alerts input.
          # (optional)
          vulnerability-alerts: "read"

          # Permission level for GitHub Actions workflow files (read/none; "write" is
          # rejected by the compiler). GitHub App-only permission.
          # (optional)
          workflows: "read"

  # Call reusable workflows via workflow_call fan-out. The compiler generates static
  # conditional jobs; the agent selects which worker to activate. Use this for
  # orchestrator/dispatcher patterns within the same repository.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for calling reusable workflows via workflow_call
  # fan-out. The compiler generates conditional `uses:` jobs at compile time; the
  # agent selects which worker to activate at runtime.
  call-workflow:
    # List of workflow names (without .md extension) to allow calling. Each workflow
    # must exist in .github/workflows/ and declare a workflow_call trigger.
    workflows: []
      # Array of strings

    # Maximum number of workflow_call fan-out operations per run (default: 1, max:
    # 50). Supports integer or GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # GitHub token passed to called workflows. Overrides global github-token if
    # specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Shorthand array format: list of workflow names (without .md extension)
  # to allow calling
  call-workflow: []
    # Array items: string

  # Enable AI agents to report when required MCP tools are unavailable. Used for
  # workflow diagnostics and tool discovery.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for reporting missing tools from agentic workflow output
  missing-tool:
    # Maximum number of missing tool reports (default: unlimited) Supports integer or
    # GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Whether to create or update GitHub issues when tools are missing (default:
    # true). Supports literal boolean or GitHub Actions expression (e.g. '${{
    # inputs.create-issue }}').
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    create-issue: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    create-issue: "example-value"

    # Prefix for issue titles when creating issues for missing tools (default:
    # '[missing tool]')
    # (optional)
    title-prefix: "example-value"

    # Labels to add to created issues for missing tools
    # (optional)
    labels: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable missing tool reporting with default configuration
  missing-tool: null

  # Format 3: Explicitly disable missing tool reporting (false). Missing tool
  # reporting is enabled by default when safe-outputs is configured.
  missing-tool: true

  # Enable AI agents to report when required data or context is missing. Used for
  # workflow troubleshooting and data validation.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for reporting missing data required to achieve workflow
  # goals. Encourages AI agents to be truthful about data gaps instead of
  # hallucinating information.
  missing-data:
    # Maximum number of missing data reports (default: unlimited) Supports integer or
    # GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Whether to create or update GitHub issues when data is missing (default: true).
    # Supports literal boolean or GitHub Actions expression (e.g. '${{
    # inputs.create-missing-data-issue }}').
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    create-issue: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    create-issue: "example-value"

    # Prefix for issue titles when creating issues for missing data (default:
    # '[missing data]')
    # (optional)
    title-prefix: "example-value"

    # Labels to add to created issues for missing data
    # (optional)
    labels: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable missing data reporting with default configuration
  missing-data: null

  # Format 3: Explicitly disable missing data reporting (false). Missing data
  # reporting is enabled by default when safe-outputs is configured.
  missing-data: true

  # Enable AI agents to explicitly indicate no action is needed. Used for workflow
  # control flow and conditional logic.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for no-op safe output (logging only, no GitHub API
  # calls). Always available as a fallback to ensure human-visible artifacts.
  noop:
    # Maximum number of noop messages (default: 1) Supports integer or GitHub Actions
    # expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # Controls whether noop runs are reported as issue comments (default: true). Set
    # to false to disable posting to the no-op runs issue.
    # (optional)
    report-as-issue: true

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable noop output with default configuration (max: 1)
  noop: null

  # Format 3: Explicitly disable noop output (false). Noop is enabled by default
  # when safe-outputs is configured.
  noop: true

  # Enable AI agents to publish files (images, charts, reports) to an orphaned git
  # branch for persistent storage and web access.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for publishing assets to an orphaned git branch
  upload-asset:
    # Branch name (default: 'assets/${{ github.workflow }}')
    # (optional)
    branch: "example-value"

    # Maximum file size in KB (default: 10240 = 10MB)
    # (optional)
    max-size: 1

    # Allowed file extensions (default: common non-executable types)
    # (optional)
    allowed-exts: []
      # Array of strings

    # Maximum number of assets to upload (default: 10) Supports integer or GitHub
    # Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable asset publishing with default configuration
  upload-asset: null

  # Enable AI agents to upload files as run-scoped GitHub Actions artifacts. Returns
  # a temporary artifact ID rather than a raw download URL, keeping authorization
  # centralized.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for uploading files as run-scoped GitHub Actions
  # artifacts
  upload-artifact:
    # Maximum number of upload_artifact tool calls allowed per run (default: 1)
    # (optional)
    max-uploads: 1

    # Artifact retention period in days (fixed; the agent cannot override this value).
    # Supports integer or GitHub Actions expression (e.g. '${{ inputs.retention-days
    # }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    retention-days: 1

    # Format 2: string
    retention-days: "example-value"

    # Upload files directly without zip archiving (fixed; the agent cannot override
    # this value). Only valid for single-file uploads. Supports boolean or GitHub
    # Actions expression (e.g. '${{ inputs.skip-archive }}').
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    skip-archive: true

    # Format 2: string
    skip-archive: "example-value"

    # Maximum total upload size in bytes per slot (default: 104857600 = 100 MB)
    # (optional)
    max-size-bytes: 1

    # Glob patterns restricting which paths relative to the staging directory the
    # model may upload
    # (optional)
    allowed-paths: []
      # Array of strings

    # Default include/exclude glob filters applied on top of allowed-paths
    # (optional)
    filters:
      # Glob patterns for files to include
      # (optional)
      include: []
        # Array of strings

      # Glob patterns for files to exclude
      # (optional)
      exclude: []
        # Array of strings

    # Default values injected when the model omits a field
    # (optional)
    defaults:
      # Behavior when no files match: 'error' (default) or 'ignore'
      # (optional)
      if-no-files: "error"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub Actions artifact
    # uploads (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable artifact uploads with default configuration
  upload-artifact: null

  # ⚠️ Experimental. Enable AI agents to upload a code coverage report (e.g. a
  # Cobertura XML file) to GitHub's code coverage API via
  # actions/upload-code-coverage. Using this field emits a compile-time warning. The
  # agent stages the report file and calls the tool with file/language/label; a
  # separate job performs the actual upload with dedicated code-quality: write
  # permissions.
  # (optional)
  # Accepted formats:

  # Format 1: ⚠️ Experimental. Configuration for uploading a code coverage report
  # via actions/upload-code-coverage
  upload-code-coverage:
    # Fixed fail-on-error input passed to actions/upload-code-coverage (fixed; the
    # agent cannot override this value). When true (default), the upload job fails if
    # the upload or processing fails.
    # (optional)
    fail-on-error: true

    # Fixed wait-for-processing-timeout in seconds passed to
    # actions/upload-code-coverage (fixed; the agent cannot override this value). Set
    # to 0 to disable waiting. Default: 160.
    # (optional)
    wait-for-processing-timeout: 1

    # Maximum number of upload-code-coverage tool calls allowed per run (default: 1).
    # Supports integer or GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified. Must have code-quality: write permission.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, skip the actions/upload-code-coverage call and emit step summary
    # messages instead (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable code coverage report uploads with default configuration
  upload-code-coverage: null

  # Enable AI agents to edit and update GitHub release content, including release
  # notes, assets, and metadata.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for updating GitHub release descriptions
  update-release:
    # Maximum number of releases to update (default: 1) Supports integer or GitHub
    # Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target repository for cross-repo release updates (format: owner/repo). If not
    # specified, updates releases in the workflow's repository.
    # (optional)
    target-repo: "example-value"

    # Controls whether AI-generated footer is added when updating the release body.
    # When false, the visible footer content is omitted. Defaults to true.
    # (optional)
    footer: true

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

  # Format 2: Enable release updates with default configuration
  update-release: null

  # When true, emit step summary messages instead of making GitHub API calls
  # (preview mode)
  # (optional)
  # Accepted formats:

  # Format 1: boolean
  staged: true

  # Format 2: GitHub Actions expression that resolves to a boolean at runtime
  staged: "example-value"

  # Environment variables to pass to safe output jobs
  # (optional)
  env:
    {}

  # GitHub token to use for safe output jobs. Typically a secret reference like ${{
  # secrets.GITHUB_TOKEN }} or ${{ secrets.CUSTOM_PAT }}
  # (optional)
  github-token: "${{ secrets.GITHUB_TOKEN }}"

  # Linear personal API key expression used only by the trusted safe_outputs job.
  # (optional)
  linear-token: "example-value"

  # GitHub App credentials for minting installation access tokens. When configured,
  # a token will be generated using the app credentials and used for all safe output
  # operations.
  # (optional)
  github-app:
    # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
    # }}').
    # (optional)
    app-id: "example-value"

    # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
    # token.
    # (optional)
    client-id: "example-value"

    # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
    # mint a GitHub App token.
    # (optional)
    private-key: "example-value"

    # If true, skip token minting when client-id/private-key resolve to empty strings
    # at runtime. Defaults to false.
    # (optional)
    ignore-if-missing: true

    # Optional owner of the GitHub App installation (defaults to current repository
    # owner if not specified)
    # (optional)
    owner: "example-value"

    # Optional list of repositories to grant access to (defaults to current repository
    # if not specified)
    # (optional)
    repositories: []
      # Array of strings

    # Optional extra GitHub App-only permissions to merge into the minted token. Takes
    # effect for tools.github.github-app and safe-outputs.github-app; ignored in
    # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
    # scopes (e.g. members, organization-administration) not expressible via standard
    # handler declarations.
    # (optional)
    permissions:
      # Permission level for repository administration (read/none; "write" is rejected
      # by the compiler). GitHub App-only permission for repository administration.
      # (optional)
      administration: "read"

      # Permission level for Codespaces (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      codespaces: "read"

      # Permission level for Codespaces lifecycle administration (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      codespaces-lifecycle-admin: "read"

      # Permission level for Codespaces metadata (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      codespaces-metadata: "read"

      # Permission level for user email addresses (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      email-addresses: "read"

      # Permission level for repository environments (read/none; "write" is rejected by
      # the compiler). GitHub App-only permission.
      # (optional)
      environments: "read"

      # Permission level for git signing (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      git-signing: "read"

      # Permission level for organization members (read/none; "write" is rejected by the
      # compiler). Required for org team membership API calls.
      # (optional)
      members: "read"

      # Permission level for organization administration (read/none; "write" is rejected
      # by the compiler). GitHub App-only permission.
      # (optional)
      organization-administration: "read"

      # Permission level for organization announcement banners (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-announcement-banners: "read"

      # Permission level for organization Codespaces (read/none; "write" is rejected by
      # the compiler). GitHub App-only permission.
      # (optional)
      organization-codespaces: "read"

      # Permission level for organization Copilot (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      organization-copilot: "read"

      # Permission level for organization custom org roles (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-custom-org-roles: "read"

      # Permission level for organization custom properties (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-custom-properties: "read"

      # Permission level for organization custom repository roles (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-custom-repository-roles: "read"

      # Permission level for organization events (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      organization-events: "read"

      # Permission level for organization webhooks (read/none; "write" is rejected by
      # the compiler). GitHub App-only permission.
      # (optional)
      organization-hooks: "read"

      # Permission level for organization members management (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-members: "read"

      # Permission level for organization packages (read/none; "write" is rejected by
      # the compiler). GitHub App-only permission.
      # (optional)
      organization-packages: "read"

      # Permission level for organization personal access token requests (read/none;
      # "write" is rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-personal-access-token-requests: "read"

      # Permission level for organization personal access tokens (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-personal-access-tokens: "read"

      # Permission level for organization plan (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      organization-plan: "read"

      # Permission level for organization self-hosted runners (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-self-hosted-runners: "read"

      # Permission level for organization user blocking (read/none; "write" is rejected
      # by the compiler). GitHub App-only permission.
      # (optional)
      organization-user-blocking: "read"

      # Permission level for repository custom properties (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      repository-custom-properties: "read"

      # Permission level for secret scanning alerts (read/none). Forwarded as
      # permission-secret-scanning-alerts input for actions/create-github-app-token.
      # (optional)
      secret-scanning-alerts: "read"

      # Permission level for repository webhooks (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      repository-hooks: "read"

      # Permission level for single file access (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      single-file: "read"

      # Permission level for team discussions (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      team-discussions: "read"

      # Permission level for Dependabot vulnerability alerts (read/none; "write" is
      # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
      # with a GitHub App, forwarded as permission-vulnerability-alerts input.
      # (optional)
      vulnerability-alerts: "read"

      # Permission level for GitHub Actions workflow files (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      workflows: "read"

  # Maximum allowed size for git patches in kilobytes (KB). Defaults to 4096 KB (4
  # MB). If patch exceeds this size, the job will fail.
  # (optional)
  max-patch-size: 1

  # Maximum allowed number of unique files in a create-pull-request patch. Defaults
  # to 100. The check counts unique file paths (deduplicated across multi-commit
  # patches), so it reflects how many distinct files the agent is pushing in this
  # iteration.
  # (optional)
  max-patch-files: 1

  # Enable AI agents to report detected security threats, policy violations, or
  # suspicious patterns for security review.
  # (optional)
  # Accepted formats:

  # Format 1: Enable or disable threat detection for safe outputs (defaults to true
  # when safe-outputs are configured)
  threat-detection: true

  # Format 2: GitHub Actions expression that resolves to a boolean at runtime,
  # enabling or disabling threat detection based on workflow inputs (e.g. '${{
  # inputs.enable-threat-detection }}')
  threat-detection: "example-value"

  # Format 3: Threat detection configuration object
  threat-detection:
    # Whether threat detection is enabled. Accepts a boolean literal or a GitHub
    # Actions expression (e.g. '${{ inputs.enable-threat-detection }}').
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    enabled: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    enabled: "example-value"

    # Additional custom prompt instructions to append to threat detection analysis
    # (optional)
    prompt: "example-value"

    # AI engine configuration specifically for threat detection (overrides main
    # workflow engine). Set to false to disable AI-based threat detection. Supports
    # same format as main engine field when not false.
    # (optional)
    # Accepted formats:

    # Format 1: Disable AI engine for threat detection (only run custom steps)
    engine: true

    # Format 2: Configuration object

    # Extended engine configuration for threat detection.
    # (optional)
    # Accepted formats:

    # Format 1: Engine name: built-in ('claude', 'codex', 'copilot', 'gemini', 'pi')
    # or a named catalog entry
    engine-config: "example-value"

    # Format 2: Extended engine configuration object with advanced options for model
    # selection, turn limiting, environment variables, and custom steps
    engine-config:
      # AI engine identifier: built-in ('claude', 'codex', 'copilot', 'gemini', 'pi') or
      # a named catalog entry
      id: "example-value"

      # Optional version of the AI engine action (e.g., 'beta', 'stable', 20). Has
      # sensible defaults and can typically be omitted. Numeric values are automatically
      # converted to strings at runtime. GitHub Actions expressions (e.g., '${{
      # inputs.engine-version }}') are accepted and compiled with injection-safe env var
      # handling.
      # (optional)
      version: null

      # Model for this engine, overriding the top-level model. Has sensible defaults and
      # can typically be omitted. Supports full model IDs (e.g.,
      # 'claude-3-5-sonnet-20241022', 'gpt-4'), which is useful when a nested engine
      # (e.g. safe-outputs.threat-detection.engine) needs a different model than the
      # main agent engine.
      # (optional)
      model: "example-value"

      # Optional inference provider override for this engine. Defaults to the engine's
      # native provider (copilot: github, claude: anthropic, codex: openai, pi: github).
      # (optional)
      model-provider: "github"

      # Claude permission mode override. Defaults to acceptEdits (or auto when
      # tools.edit is false).
      # (optional)
      permission-mode: "auto"

      # Maximum number of continuations for multi-run autopilot mode. Default is 1
      # (single run, no autopilot). Values greater than 1 enable --autopilot mode for
      # the copilot engine with --max-autopilot-continues set to this value. Note: Only
      # supported by the copilot engine.
      # (optional)
      max-continuations: 1

      # Agent job concurrency configuration. Defaults to single job per engine across
      # all workflows (group: 'gh-aw-{engine-id}'). Supports full GitHub Actions
      # concurrency syntax.
      # (optional)
      # Accepted formats:

      # Format 1: Simple concurrency group name. Gets converted to GitHub Actions
      # concurrency format with the specified group.
      concurrency: "example-value"

      # Format 2: GitHub Actions concurrency configuration for the agent job. Controls
      # how many agentic workflow runs can run concurrently.
      concurrency:
        # Concurrency group identifier. Use GitHub Actions expressions like ${{
        # github.workflow }} or ${{ github.ref }}. Defaults to 'gh-aw-{engine-id}' if not
        # specified.
        group: "example-value"

        # Whether to cancel in-progress runs of the same concurrency group. Defaults to
        # false for agentic workflow runs.
        # (optional)
        cancel-in-progress: true

        # Pending run queue behavior for this concurrency group. 'single' (default) allows
        # one pending run and replaces older pending runs. 'max' allows up to 100 pending
        # runs in FIFO order.
        # (optional)
        queue: "single"

      # Custom user agent string for GitHub MCP server configuration (codex engine only)
      # (optional)
      user-agent: "example-value"

      # Custom executable path for the AI engine CLI. When specified, the workflow will
      # skip the standard installation steps and use this command instead. The command
      # should be the full path to the executable or a command available in PATH.
      # (optional)
      command: "example-value"

      # Harness configuration for the AI engine. Accepts either a bare filename string
      # (legacy) or an object with sub-keys for fine-grained control.
      # (optional)
      # Accepted formats:

      # Format 1: Custom Node.js harness script filename (legacy form). This replaces
      # the engine's built-in harness wrapper and must end with .js, .cjs, or .mjs.
      harness: "example-value"

      # Format 2: Harness configuration object with optional sub-keys for script
      # override and retry policy.
      harness:
        # Custom Node.js harness script filename. This replaces the engine's built-in
        # harness wrapper and must end with .js, .cjs, or .mjs.
        # (optional)
        use: "example-value"

        # Maximum retry attempts after the initial run (0 = no retries). Accepts a literal
        # integer or a GitHub Actions expression.
        # (optional)
        # Accepted formats:

        # Format 1: integer
        max-retries: 1

        # Format 2: string
        max-retries: "example-value"

        # Delay in ms before the first retry. Accepts a literal integer or a GitHub
        # Actions expression.
        # (optional)
        # Accepted formats:

        # Format 1: integer
        initial-delay-ms: 1

        # Format 2: string
        initial-delay-ms: "example-value"

        # Multiplier applied to the delay after each retry. Accepts a literal integer or a
        # GitHub Actions expression.
        # (optional)
        # Accepted formats:

        # Format 1: integer
        backoff-multiplier: 1

        # Format 2: string
        backoff-multiplier: "example-value"

        # Maximum delay cap in ms. Accepts a literal integer or a GitHub Actions
        # expression.
        # (optional)
        # Accepted formats:

        # Format 1: integer
        max-delay-ms: 1

        # Format 2: string
        max-delay-ms: "example-value"

        # Post-result idle watchdog timeout in seconds. Accepts a literal integer or a
        # GitHub Actions expression.
        # (optional)
        # Accepted formats:

        # Format 1: integer
        watchdog-timeout: 1

        # Format 2: string
        watchdog-timeout: "example-value"

      # Custom environment variables to pass to the AI engine, including secret
      # overrides (e.g., OPENAI_API_KEY: ${{ secrets.CUSTOM_KEY }})
      # (optional)
      env:
        {}

      # Engine-level authentication configuration for AWF API proxy sidecar integration
      # (for example, Azure OpenAI via GitHub OIDC). Values are mapped to AWF_AUTH_*
      # environment variables.
      # (optional)
      auth:
        # Authentication type. Currently only 'github-oidc' is supported.
        type: "github-oidc"

        # OIDC audience to request from GitHub Actions for token exchange.
        # (optional)
        audience: "example-value"

        # Optional Azure tenant ID for token exchange.
        # (optional)
        azure-tenant-id: "example-value"

        # Optional Azure client ID for token exchange.
        # (optional)
        azure-client-id: "example-value"

        # Optional Azure OAuth scope (defaults to
        # https://cognitiveservices.azure.com/.default in AWF sidecar).
        # (optional)
        azure-scope: "example-value"

        # Optional Azure cloud name (for example, public, usgovernment, china).
        # (optional)
        azure-cloud: "example-value"

        # Optional WIF provider discriminator. Recognized values are 'azure', 'anthropic',
        # and 'gcp'.
        # (optional)
        provider: "example-value"

        # Anthropic WIF federation rule ID (e.g., fdrl_...).
        # (optional)
        federation-rule-id: "example-value"

        # Anthropic WIF organization ID (e.g., org_...).
        # (optional)
        organization-id: "example-value"

        # Anthropic WIF service account ID (e.g., svac_...).
        # (optional)
        service-account-id: "example-value"

        # Anthropic WIF workspace ID (e.g., ws_...).
        # (optional)
        workspace-id: "example-value"

        # Google Cloud WIF workload identity provider resource name (e.g.,
        # projects/PROJECT_NUMBER/locations/global/workloadIdentityPools/POOL/providers/PROVIDER).
        # (optional)
        workload-identity-provider: "example-value"

        # Google Cloud service account email to impersonate via WIF (e.g.,
        # my-sa@my-project.iam.gserviceaccount.com).
        # (optional)
        service-account: "example-value"

        # Google Cloud project ID used for Vertex AI / Gemini Enterprise inference.
        # (optional)
        project: "example-value"

        # Google Cloud region for Vertex AI inference (e.g., us-central1). Defaults to
        # us-central1 when omitted.
        # (optional)
        location: "example-value"

      # Additional TOML configuration text that will be appended to the generated
      # config.toml in the action (codex engine only)
      # (optional)
      config: "example-value"

      # Agent identifier to pass to copilot --agent flag (copilot engine only).
      # Specifies which custom agent to use for the workflow.
      # (optional)
      agent: "example-value"

      # Custom API endpoint hostname for the agentic engine. Used for GitHub Enterprise
      # Cloud (GHEC), GitHub Enterprise Server (GHES), or custom AI endpoints. Example:
      # 'api.acme.ghe.com' for GHEC, 'api.enterprise.githubcopilot.com' for GHES, or
      # custom endpoint hostnames.
      # (optional)
      api-target: "example-value"

      # Optional array of command-line arguments to pass to the AI engine CLI. These
      # arguments are injected after all other args but before the prompt.
      # (optional)
      args: []
        # Array of strings

      # When true, disables automatic loading of context and custom instructions by the
      # AI engine. The engine-specific flag depends on the engine: copilot uses
      # --no-custom-instructions (suppresses .github/AGENTS.md and user-level custom
      # instructions), claude uses --bare (suppresses CLAUDE.md memory files), codex
      # uses --no-system-prompt (suppresses the default system prompt), gemini sets
      # GEMINI_SYSTEM_MD=/dev/null (overrides the built-in system prompt with an empty
      # one). Defaults to false.
      # (optional)
      bare: true

      # Engine-level MCP gateway configuration. Settings here apply to the MCP gateway
      # used by this engine.
      # (optional)
      mcp:
        # Session timeout for MCP gateway sessions as a Go duration string (e.g. "30m",
        # "4h", "24h"). Must be at least 5m (no upper bound). Omitted or empty uses the
        # effective gateway default (precedence: this field > MCP_GATEWAY_SESSION_TIMEOUT
        # env var > built-in default 6h). Longer timeouts benefit multi-hour workflows
        # such as large-scale migrations; shorter values free gateway resources sooner.
        # (optional)
        session-timeout: "example-value"

        # Timeout for individual MCP tool calls as a Go duration string (e.g. "30s", "2m",
        # "10m"). Must be between 10s and 600s inclusive. Omitted or empty uses the
        # gateway built-in default (60s). Use a higher value for slow MCP backends such as
        # full-text search over large indexes.
        # (optional)
        tool-timeout: "example-value"

      # Enables the experimental GitHub Copilot SDK integration (copilot engine only).
      # When true, the harness starts a separate headless Copilot CLI sidecar on the
      # configured localhost port and sets COPILOT_SDK_URI on child processes.
      # (optional)
      copilot-sdk: true

      # Custom driver script, command, or inline source for the engine. String values
      # accept a workspace-relative path or bare name. Inline object values are
      # supported for the Copilot engine and materialize runtime files before execution.
      # Supported inline runtimes are node, python, go, and java.
      # (optional)
      # Accepted formats:

      # Format 1: Workspace-relative driver path or bare command name. The copilot
      # engine accepts .js, .cjs, .mjs, .py, .ts, .mts, and .rb drivers; other engines
      # accept .js, .cjs, and .mjs. Bare names without an extension are treated as
      # built-in drivers or executables in PATH.
      driver: "example-value"

      # Format 2: Inline Copilot SDK driver source. Provide exactly one runtime key.
      driver:
        # Inline Node.js driver source written to a generated .cjs file.
        # (optional)
        node: "example-value"

        # Inline Python driver source written to a generated .py file.
        # (optional)
        python: "example-value"

        # Inline Go driver source written to a generated .go file.
        # (optional)
        go: "example-value"

        # Inline Java driver source written to a generated .java file.
        # (optional)
        java: "example-value"

      # Engine-specific plugin names to install before launching the engine. Currently
      # used by the Pi engine: each entry is passed to `pi install <extension>`.
      # (optional)
      extensions: []
        # Array of strings

      # Override the working directory for the engine's spawned process. Accepts a
      # literal path or a GitHub Actions expression (e.g. `${{ github.workspace
      # }}/subdir`). When set, passed as GH_AW_ENGINE_CWD to the engine execution
      # environment.
      # (optional)
      cwd: "example-value"

    # Format 3: Inline engine definition: specifies a runtime adapter and optional
    # provider settings directly in the workflow frontmatter, without requiring a
    # named catalog entry
    engine-config:
      # Runtime adapter reference for the inline engine definition
      runtime:
        # Runtime adapter identifier (e.g. 'codex', 'claude', 'copilot', 'gemini', 'pi')
        id: "example-value"

        # Optional version of the runtime adapter (e.g. '0.105.0', 'beta')
        # (optional)
        version: null

      # Optional provider configuration for the inline engine definition
      # (optional)
      provider:
        # Provider identifier (e.g. 'openai', 'anthropic', 'github', 'google')
        # (optional)
        id: "example-value"

        # Optional specific LLM model to use (e.g. 'gpt-5', 'claude-3-5-sonnet-20241022')
        # (optional)
        model: "example-value"

        # Authentication configuration for the provider
        # (optional)
        auth:
          # Name of the GitHub Actions secret that contains the API key for this provider
          # (optional)
          secret: "example-value"

          # Authentication strategy for the provider (default: api-key when secret is set)
          # (optional)
          strategy: "api-key"

          # OAuth 2.0 token endpoint URL. Required when strategy is
          # 'oauth-client-credentials'.
          # (optional)
          token-url: "example-value"

          # GitHub Actions secret name that holds the OAuth client ID. Required when
          # strategy is 'oauth-client-credentials'.
          # (optional)
          client-id: "example-value"

          # GitHub Actions secret name that holds the OAuth client secret. Required when
          # strategy is 'oauth-client-credentials'.
          # (optional)
          client-secret: "example-value"

          # JSON field name in the token response that contains the access token. Defaults
          # to 'access_token'.
          # (optional)
          token-field: "example-value"

          # HTTP header name to inject the API key or token into (e.g. 'api-key',
          # 'x-api-key'). Required when strategy is not 'bearer'.
          # (optional)
          header-name: "example-value"

        # Request shaping configuration for non-standard provider URL and body
        # transformations
        # (optional)
        request:
          # URL path template with {model} and other variable placeholders (e.g.
          # '/openai/deployments/{model}/chat/completions')
          # (optional)
          path-template: "example-value"

          # Static or template query-parameter values appended to every request
          # (optional)
          query:
            {}

          # Key/value pairs injected into the JSON request body before sending
          # (optional)
          body-inject:
            {}

      # When true, disables automatic loading of context and custom instructions by the
      # AI engine. The engine-specific flag depends on the engine: copilot uses
      # --no-custom-instructions, claude uses --bare, codex uses --no-system-prompt,
      # gemini sets GEMINI_SYSTEM_MD=/dev/null. Defaults to false.
      # (optional)
      bare: true

    # Format 4: Engine definition: full declarative metadata for a named engine entry
    # (used in builtin engine shared workflow files such as @builtin:engines/*.md)
    engine-config:
      # Unique engine identifier (e.g. 'copilot', 'claude', 'codex', 'gemini', 'pi')
      id: "example-value"

      # Default CLI version applied when a workflow references this engine without
      # specifying engine.version
      # (optional)
      version: null

      # Human-readable display name for the engine
      display-name: "example-value"

      # Human-readable description of the engine
      # (optional)
      description: "Description of the workflow"

      # Marks the engine as experimental so compiled workflows can surface an explicit
      # warning.
      # (optional)
      experimental: true

      # Whether the engine supports MCP. When false, the compiler automatically enables
      # gh-proxy and cli-proxy and rejects attempts to disable either proxy.
      # (optional)
      mcp: true

      # Runtime adapter identifier. Maps to the CodingAgentEngine registered in the
      # engine registry. Defaults to id when omitted.
      # (optional)
      runtime-id: "example-value"

      # Built-in engine used to run threat detection for workflows using this engine.
      # Required for custom engines because the threat detection analyzer only supports
      # built-in engines; defaults to 'copilot' when omitted.
      # (optional)
      detection-engine: "copilot"

      # Optional engine-specific secret bindings for behavior-defined engines.
      # (optional)
      auth: []
        # Array items:
          # Logical authentication role name.
          role: "example-value"

          # GitHub Actions secret name exposed to the engine runtime.
          secret: "example-value"

      # Provider metadata for the engine
      # (optional)
      provider:
        # Provider name (e.g. 'anthropic', 'github', 'google', 'openai')
        # (optional)
        name: "My Workflow"

        # Default authentication configuration for the provider
        # (optional)
        auth:
          # Name of the GitHub Actions secret that contains the API key
          # (optional)
          secret: "example-value"

          # Authentication strategy
          # (optional)
          strategy: "api-key"

          # OAuth 2.0 token endpoint URL
          # (optional)
          token-url: "example-value"

          # GitHub Actions secret name for the OAuth client ID
          # (optional)
          client-id: "example-value"

          # GitHub Actions secret name for the OAuth client secret
          # (optional)
          client-secret: "example-value"

          # JSON field name in the token response containing the access token
          # (optional)
          token-field: "example-value"

          # HTTP header name to inject the API key or token into
          # (optional)
          header-name: "example-value"

        # Request shaping configuration
        # (optional)
        request:
          # URL path template with variable placeholders
          # (optional)
          path-template: "example-value"

          # Static query parameters
          # (optional)
          query:
            {}

          # Key/value pairs injected into the JSON request body
          # (optional)
          body-inject:
            {}

      # Model selection configuration for the engine
      # (optional)
      models:
        # Default model identifier
        # (optional)
        default: "example-value"

        # List of supported model identifiers
        # (optional)
        supported: []
          # Array of strings

      # Additional engine-specific options
      # (optional)
      options:
        {}

      # Declarative custom-engine runtime behavior used to materialize a shared CLI
      # engine from frontmatter.
      # (optional)
      behaviors:
        # Secret resolution mode for the engine (for example 'universal-llm-consumer').
        # (optional)
        secret-strategy: "example-value"

        # Secret or env var keys the engine accepts through engine.env.
        # (optional)
        supported-env-var-keys: []
          # Array of strings

        # (optional)
        capabilities:
          # (optional)
          tools-allowlist: true

          # (optional)
          max-turns: true

          # (optional)
          web-search: true

          # (optional)
          max-continuations: true

          # (optional)
          native-agent-file: true

          # (optional)
          bare-mode: true

          # (optional)
          bash-command-allowlist: true

        # (optional)
        manifest:
          # (optional)
          files: []
            # Array of strings

          # (optional)
          path-prefixes: []
            # Array of strings

        # Declarative default network requirements for the engine. 'defaults' are always
        # allowed; the domain in 'provider-domains' matching the model's provider prefix
        # is added on top.
        # (optional)
        network:
          # Domains always required by the engine CLI (for example package registries and
          # telemetry endpoints).
          # (optional)
          defaults: []
            # Array of strings

          # Maps a model provider prefix (the part before '/' in 'provider/model') to the
          # API domain required for that provider.
          # (optional)
          provider-domains:
            {}

          # Provider key used to resolve a provider domain when the model carries no
          # 'provider/' prefix.
          # (optional)
          default-provider: "example-value"

        # (optional)
        installation:
          # (optional)
          package-manager: "example-value"

          # (optional)
          package-name: "example-value"

          # (optional)
          version: "example-value"

          # (optional)
          step-name: "example-value"

          # (optional)
          binary-name: "example-value"

          # (optional)
          include-node-setup: true

          # (optional)
          post-install-scripts: true

          # (optional)
          cooldown: true

          # (optional)
          verify-command: "example-value"

          # (optional)
          verify-step-name: "example-value"

          # (optional)
          docs-url: "example-value"

        # ⚠️ Experimental. Declares Agent Plugins (https://agent-plugins.org) support for
        # the engine. Plugins are checked out at their pinned commit SHA, then staged in
        # 'directory' and/or installed with the engine CLI using 'install-args'.
        # (optional)
        plugins:
          # Folder the engine scans for plugins. Workspace-relative (for example
          # '.kiro/powers') or home-relative (for example '~/.cursor/plugins/local').
          # (optional)
          directory: "example-value"

          # CLI executable used with 'install-args'. Defaults to the execution command name.
          # (optional)
          command-name: "example-value"

          # CLI arguments placed before the local plugin path (for example ['plugin',
          # 'install']).
          # (optional)
          install-args: []
            # Array of strings

        # (optional)
        config-file:
          # (optional)
          path: "example-value"

          # (optional)
          step-name: "example-value"

          # (optional)
          content: "example-value"

          # (optional)
          merge-strategy: "example-value"

        # (optional)
        execution:
          # (optional)
          command-name: "example-value"

          # (optional)
          args: []
            # Array of strings

          # (optional)
          step-name: "example-value"

          # (optional)
          model-env-var: "example-value"

          # (optional)
          model-env-provider-prefix: "example-value"

          # (optional)
          model-flag: "example-value"

          # (optional)
          mcp-config-env-var: "example-value"

          # (optional)
          mcp-config-flag: "example-value"

          # (optional)
          write-timestamp: true

          # (optional)
          provider-env-mode: "example-value"

          # Additional static environment variables injected into the execution step. Values
          # are rendered verbatim and must not contain secrets.
          # (optional)
          env:
            {}

        # (optional)
        mcp:
          # (optional)
          config-path: "example-value"

          # JavaScript source of a Node.js script that converts the MCP gateway's raw output
          # configuration into the format expected by this engine. When set, the script is
          # written to ${RUNNER_TEMP}/gh-aw/actions/<engine-id>_mcp_config_adapter.cjs
          # before the MCP gateway starts, and start_mcp_gateway.cjs executes it (instead of
          # a built-in per-engine converter) once the gateway has produced its output.
          # (optional)
          config-adapter: "example-value"

        # JavaScript source of a Node.js harness that spawns the engine CLI. When set, the
        # script is written to ${RUNNER_TEMP}/gh-aw/actions/<engine-id>_harness.cjs before
        # execution and the engine is launched as: node <harness-path> <command-name>
        # [args...]. The harness reads process.env.GH_AW_PROMPT for the prompt-file path
        # and, when the AWF firewall is active, process.env.AWF_REFLECT_ENABLED is set to
        # '1' so the harness can read /reflect data to dynamically configure the engine
        # CLI.
        # (optional)
        harness-script: "example-value"

        # JavaScript source of a log-parser function for the engine. When set, the script
        # is written to ${RUNNER_TEMP}/gh-aw/actions/<engine-id>_log_parser.cjs and used
        # in the post-agent log-parsing step. The script must define a
        # parseLog(logContent) function (not export it) that returns {markdown,
        # logEntries, mcpFailures, maxTurnsHit}. A createEngineLogParser wrapper from
        # log_parser_shared.cjs is automatically appended so the author only provides the
        # parsing function; the wrapper handles exports and bootstrap. This enables
        # behavior-defined engines (e.g. crush, opencode, goose, aider) to produce
        # normalized events files like built-in engine parsers.
        # (optional)
        log-parser: "example-value"

    # Format 5: MCP gateway configuration for shared workflows. Declares engine.mcp
    # settings (tool-timeout, session-timeout) that consumers inherit during import
    # without specifying an engine identifier. The engine is always inherited from the
    # importing workflow.
    engine-config:
      # Engine-level MCP gateway configuration. Settings here apply to the MCP gateway
      # used by this engine.
      mcp:
        # Session timeout for MCP gateway sessions as a Go duration string (e.g. "30m",
        # "4h", "24h"). Must be at least 5m (no upper bound). Omitted or empty uses the
        # effective gateway default (precedence: this field > MCP_GATEWAY_SESSION_TIMEOUT
        # env var > built-in default 6h).
        # (optional)
        session-timeout: "example-value"

        # Timeout for individual MCP tool calls as a Go duration string (e.g. "30s", "2m",
        # "10m"). Must be between 10s and 600s inclusive. Omitted or empty uses the
        # gateway built-in default (60s). Use a higher value for slow MCP backends such as
        # full-text search over large indexes.
        # (optional)
        tool-timeout: "example-value"

    # Format 6: Engine object with only a model preference (no engine.id). Allows
    # workflow authors to express a model-size hint (e.g. 'small', 'large') without
    # committing to a specific engine. The runtime selects an appropriate engine using
    # its default, and the model preference is applied to it.
    engine-config:
      # Model preference or size category (e.g. 'small', 'large', 'gpt-4.1'). Applied to
      # the default engine when engine.id is not specified. Overrides the top-level
      # 'model' field.
      model: "example-value"

    # Model override for threat detection engine execution.
    # (optional)
    model: "example-value"

    # Per-attempt timeout for threat detection engine execution as a Go duration (for
    # example '90s', '10m', '1h30m'). Set to 0 to disable timeout enforcement in
    # threat-detect.
    # (optional)
    # Accepted formats:

    # Format 1: string
    engine-timeout: "example-value"

    # Format 2: integer
    engine-timeout: 1

    # Detector-only per-attempt max-turns override passed to threat-detect. When
    # omitted, threat-detect falls back to GH_AW_MAX_TURNS (if set) and then to its
    # own built-in default.
    # (optional)
    max-turns: 1

    # Detector-only retry count passed to threat-detect for clean exits without a
    # verdict.
    # (optional)
    retries: 1

    # Array of extra job steps to run before engine execution
    # (optional)
    steps: []

    # Array of extra job steps to run after engine execution
    # (optional)
    post-steps: []

    # Per-run AI Credits budget for threat-detection engine execution. Accepts numeric
    # values only (expressions are not supported). When unset, gh-aw emits a runtime
    # default expression `${{ vars.GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS || '400'
    # }}`.
    # (optional)
    # Accepted formats:

    # Format 1: Configuration object

    # Format 2: integer
    max-ai-credits: 1

    # Format 3: string
    max-ai-credits: "example-value"

    # Runner specification for the detection job. Overrides agent.runs-on for the
    # detection job only. Supports string, array, or runner-group object forms.
    # Defaults to agent.runs-on.
    # (optional)
    # Accepted formats:

    # Format 1: Simple runner label string. Use for standard GitHub-hosted runners
    # (e.g., 'ubuntu-latest', 'windows-latest', 'macos-latest') or self-hosted runner
    # labels. Most common form for agentic workflows.
    runs-on: "example-value"

    # Format 2: Array of runner labels for selection with fallbacks. GitHub Actions
    # will use the first available runner that matches any label in the array. Useful
    # for high-availability setups or when multiple runner types are acceptable.
    runs-on: []
      # Array items: string

    # Format 3: Runner group configuration for GitHub-hosted runners. Use this form to
    # target specific runner groups (e.g., larger runners with more CPU/memory) or
    # self-hosted runner pools with specific label requirements. Agentic workflows may
    # benefit from larger runners for complex AI processing tasks.
    runs-on:
      # Runner group name for self-hosted runners or GitHub-hosted runner groups
      # (optional)
      group: "example-value"

      # List of runner labels for self-hosted runners or GitHub-hosted runner selection
      # (optional)
      labels: []
        # Array of strings

    # GitHub Actions environment override for the detection job.
    # (optional)
    environment: "example-value"

    # When true (default), detection failures produce warnings and allow safe outputs
    # to proceed with a caution notice and 'needs-review' label. When false, detection
    # failures block safe outputs entirely. Accepts a boolean literal or a GitHub
    # Actions expression.
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    continue-on-error: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    continue-on-error: "example-value"

  # Custom safe-output jobs that can be executed based on agentic workflow output.
  # Job names containing dashes will be automatically normalized to underscores
  # (e.g., 'send-notification' becomes 'send_notification').
  # (optional)
  jobs:
    {}

  # Inline JavaScript script handlers that run inside the consolidated safe-outputs
  # job handler loop. Unlike 'jobs' (which create separate GitHub Actions jobs),
  # scripts execute in-process alongside the built-in handlers. Users write only the
  # body of the main function — the compiler wraps it with 'async function
  # main(config = {}) { ... }' and 'module.exports = { main };' automatically.
  # Script names containing dashes will be automatically normalized to underscores
  # (e.g., 'post-slack-message' becomes 'post_slack_message').
  # (optional)
  scripts:
    {}

  # Custom message templates for safe-output footer and notification messages.
  # Available placeholders: {workflow_name} (workflow name), {run_url} (GitHub
  # Actions run URL), {triggering_number} (issue/PR/discussion number),
  # {workflow_source} (owner/repo/path@ref), {workflow_source_url} (GitHub URL to
  # source), {operation} (safe-output operation name for staged mode).
  # (optional)
  messages:
    # Custom footer message template for AI-generated content. Available placeholders:
    # {workflow_name}, {run_url}, {triggering_number}, {workflow_source},
    # {workflow_source_url}. Example: '> Generated by [{workflow_name}]({run_url})'
    # (optional)
    footer: "example-value"

    # Custom installation instructions template appended to the footer. Available
    # placeholders: {workflow_source}, {workflow_source_url}. Example: '> Install: `gh
    # aw add {workflow_source}`'
    # (optional)
    footer-install: "example-value"

    # Custom footer message template for workflow recompile issues. Available
    # placeholders: {workflow_name}, {run_url}, {repository}. Example: '> Workflow
    # sync report by [{workflow_name}]({run_url}) for {repository}'
    # (optional)
    footer-workflow-recompile: "example-value"

    # Custom footer message template for comments on workflow recompile issues.
    # Available placeholders: {workflow_name}, {run_url}, {repository}. Example: '>
    # Update from [{workflow_name}]({run_url}) for {repository}'
    # (optional)
    footer-workflow-recompile-comment: "example-value"

    # Custom title template for staged mode preview. Available placeholders:
    # {operation}. Example: '🎭 Preview: {operation}'
    # (optional)
    staged-title: "example-value"

    # Custom description template for staged mode preview. Available placeholders:
    # {operation}. Example: 'The following {operation} would occur if staged mode was
    # disabled:'
    # (optional)
    staged-description: "example-value"

    # Custom message template for workflow activation comment. Available placeholders:
    # {workflow_name}, {run_url}, {event_type}. Default: 'Agentic
    # [{workflow_name}]({run_url}) triggered by this {event_type}.'
    # (optional)
    run-started: "example-value"

    # Custom message template for successful workflow completion. Available
    # placeholders: {workflow_name}, {run_url}. Default: '✅ Agentic
    # [{workflow_name}]({run_url}) completed successfully.'
    # (optional)
    run-success: "example-value"

    # Custom message template for failed workflow. Available placeholders:
    # {workflow_name}, {run_url}, {status}. Default: '❌ Agentic
    # [{workflow_name}]({run_url}) {status} and wasn't able to produce a result.'
    # (optional)
    run-failure: "example-value"

    # Custom message template for detection job failure. Available placeholders:
    # {workflow_name}, {run_url}. Default: '⚠️ Security scanning failed for
    # [{workflow_name}]({run_url}). Review the logs for details.'
    # (optional)
    detection-failure: "example-value"

    # Custom footer template for agent failure tracking issues. Available
    # placeholders: {workflow_name}, {run_url}. Default: '> Agent failure tracked by
    # [{workflow_name}]({run_url})'
    # (optional)
    agent-failure-issue: "example-value"

    # Custom footer template for comments on agent failure tracking issues. Available
    # placeholders: {workflow_name}, {run_url}. Default: '> Agent failure update from
    # [{workflow_name}]({run_url})'
    # (optional)
    agent-failure-comment: "example-value"

    # Custom message template for pull request creation link appended to the
    # activation comment. Available placeholders: {item_number}, {item_url}. Default:
    # 'Pull request created: [#{item_number}]({item_url})'
    # (optional)
    pull-request-created: "example-value"

    # Custom message template for issue creation link appended to the activation
    # comment. Available placeholders: {item_number}, {item_url}. Default: 'Issue
    # created: [#{item_number}]({item_url})'
    # (optional)
    issue-created: "example-value"

    # Custom message template for commit push link appended to the activation comment.
    # Available placeholders: {commit_sha}, {short_sha}, {commit_url}. Default:
    # 'Commit pushed: [`{short_sha}`]({commit_url})'
    # (optional)
    commit-pushed: "example-value"

    # Custom header text prepended to every message body generated by safe outputs
    # (issues, comments, pull requests, discussions). Applied after any
    # threat-detection caution alert and before the agent-generated content. Available
    # placeholders: {workflow_name}, {run_url}.
    # (optional)
    body-header: "example-value"

    # AI authorship disclosure header prepended to every message body. Set to true for
    # built-in default text, or provide a custom template string. Insertion order from
    # top to bottom: threat-detection caution alert (if any) → disclosure-header →
    # body-header → agent-generated content. Available placeholders: {workflow_name},
    # {run_url}.
    # (optional)
    # Accepted formats:

    # Format 1: Set to true to prepend the built-in AI authorship disclosure header to
    # every message body (issues, comments, pull requests, discussions). Set to false
    # to disable disclosure-header. The default text states the content was
    # automatically generated and was not written or reviewed by the account owner
    # personally.
    disclosure-header: true

    # Format 2: Custom AI authorship disclosure header prepended to every message
    # body. Available placeholders: {workflow_name}, {run_url}.
    disclosure-header: "example-value"

    # When enabled, workflow completion notifier creates a new comment instead of
    # editing the activation comment. Creates an append-only timeline of workflow
    # runs. Default: false
    # (optional)
    append-only-comments: true

  # Configuration for @mention filtering in safe outputs. Controls whether and how
  # @mentions in AI-generated content are allowed or escaped.
  # (optional)
  # Accepted formats:

  # Format 1: Simple boolean mode: false = always escape mentions, true = always
  # allow mentions (error in strict mode)
  mentions: true

  # Format 2: Advanced configuration for @mention filtering with fine-grained
  # control
  mentions:
    # Allow mentions of repository collaborators (users with repository access,
    # excluding bots). Default: true
    # (optional)
    allowed-collaborators: true

    # Allow mentions inferred from event context (issue/PR authors, assignees,
    # commenters). Default: true
    # (optional)
    allow-context: true

    # List of user/bot names always allowed to be mentioned. Bots are not allowed by
    # default unless listed here.
    # (optional)
    allowed: []
      # Array of strings

    # List of team slugs whose members are always allowed to be mentioned. Accepts
    # 'team-slug' (resolved against the current org) or 'org/team-slug' format. Team
    # members are fetched from the GitHub API at runtime; bots are excluded.
    # IMPORTANT: requires read:org scope — not available with the default
    # GITHUB_TOKEN. Use a classic PAT with read:org, a fine-grained PAT with
    # Members:Read, or a GitHub App with the Members:Read permission. Without the
    # required scope, team lookups fail with a warning and those members are skipped.
    # (optional)
    allowed-teams: []
      # Array of strings

    # Maximum number of mentions allowed per message. Default: 50 Supports integer or
    # GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

  # Global footer control for all safe outputs. When false, omits visible
  # AI-generated footer content from all created/updated entities (issues, PRs,
  # discussions, releases) while still including XML markers for searchability.
  # Individual safe-output types (create-issue, update-issue, etc.) can override
  # this by specifying their own footer field. Defaults to true.
  # (optional)
  footer: true

  # When set to false or "false", disables all activation and fallback comments
  # entirely (run-started, run-success, run-failure, PR/issue creation links).
  # Supports templatable boolean values including GitHub Actions expressions (e.g.
  # ${{ inputs.activation-comments }}). Default: true
  # (optional)
  activation-comments: null

  # When true, creates a parent '[aw] Failed runs' issue that tracks all workflow
  # failures as sub-issues. Helps organize failure tracking but may be unnecessary
  # in smaller repositories. Defaults to false.
  # (optional)
  group-reports: true

  # (optional)
  # Accepted formats:

  # Format 1: A boolean value that may also be specified as a GitHub Actions
  # expression string that resolves to a boolean at runtime (e.g. '${{
  # inputs.my-flag }}').

  # Format 2: List of failure categories that should trigger issue creation.
  # Categories can be prefixed with '!' to exclude them (e.g.,
  # '!inference_access_error'). If only non-prefixed categories are specified, only
  # those categories trigger issues. If only prefixed (excluded) categories are
  # specified, all categories except those trigger issues. If both are specified,
  # categories must match included AND not match excluded. Common categories:
  # agent_failure, timed_out, missing_safe_outputs, report_incomplete, missing_tool,
  # missing_data, inference_access_error, mcp_policy_error,
  # ai_credits_rate_limit_error, max_ai_credits_exceeded.
  report-failure-as-issue: []
    # Array items: string

  # Repository to create failure tracking issues in, in the format 'owner/repo'.
  # Useful when the current repository has issues disabled. Defaults to the current
  # repository.
  # (optional)
  failure-issue-repo: "example-value"

  # Controls whether to report failed non-builtin jobs as issues (default: true).
  # Set to false to disable.
  # (optional)
  report-failed-jobs: true

  # Maximum number of bot trigger references (e.g. 'fixes #123', 'closes #456')
  # allowed in output before all of them are neutralized. Default: 10. Supports
  # integer or GitHub Actions expression (e.g. '${{ inputs.max-bot-mentions }}').
  # (optional)
  # Accepted formats:

  # Format 1: integer
  max-bot-mentions: 1

  # Format 2: GitHub Actions expression that resolves to an integer at runtime
  max-bot-mentions: "example-value"

  # Override the id-token permission for the safe-outputs job. Use 'write' to
  # force-enable the id-token: write permission (required for OIDC authentication
  # with cloud providers). Use 'none' to suppress automatic detection and prevent
  # adding id-token: write even when vault/OIDC actions are detected in steps. By
  # default, the compiler auto-detects known OIDC/vault actions
  # (aws-actions/configure-aws-credentials, azure/login, google-github-actions/auth,
  # hashicorp/vault-action, cyberark/conjur-action) and adds id-token: write
  # automatically.
  # (optional)
  id-token: "write"

  # Concurrency group for the safe-outputs job. When set, the safe-outputs job will
  # use this concurrency group with cancel-in-progress: false. Supports GitHub
  # Actions expressions.
  # (optional)
  concurrency-group: "example-value"

  # Timeout for the safe_outputs job in minutes. Defaults to 45 minutes. Increase
  # for workflows with many sequential safe output operations (e.g.
  # push_to_pull_request_branch against large repositories).
  # (optional)
  timeout-minutes: 1

  # Explicit additional custom workflow jobs that the consolidated safe_outputs job
  # should depend on.
  # (optional)
  needs: []
    # Array of strings

  # Override the GitHub deployment environment for the safe-outputs job. When set,
  # this environment is used instead of the top-level environment: field. When not
  # set, the top-level environment: field is propagated automatically so that
  # environment-scoped secrets are accessible in the safe-outputs job.
  # (optional)
  # Accepted formats:

  # Format 1: Environment name as a string
  environment: "example-value"

  # Format 2: Environment object with name and optional URL
  environment:
    # The name of the environment configured in the repo
    name: "My Workflow"

    # A deployment URL
    # (optional)
    url: "example-value"

  # Runner specification for all safe-outputs jobs (activation, create-issue,
  # add-comment, etc.). Supports string, array, or runner-group object forms.
  # Defaults to 'ubuntu-slim'. See
  # https://github.blog/changelog/2025-10-28-1-vcpu-linux-runner-now-available-in-github-actions-in-public-preview/
  # (optional)
  # Accepted formats:

  # Format 1: Simple runner label string. Use for standard GitHub-hosted runners
  # (e.g., 'ubuntu-latest', 'windows-latest', 'macos-latest') or self-hosted runner
  # labels. Most common form for agentic workflows.
  runs-on: "example-value"

  # Format 2: Array of runner labels for selection with fallbacks. GitHub Actions
  # will use the first available runner that matches any label in the array. Useful
  # for high-availability setups or when multiple runner types are acceptable.
  runs-on: []
    # Array items: string

  # Format 3: Runner group configuration for GitHub-hosted runners. Use this form to
  # target specific runner groups (e.g., larger runners with more CPU/memory) or
  # self-hosted runner pools with specific label requirements. Agentic workflows may
  # benefit from larger runners for complex AI processing tasks.
  runs-on:
    # Runner group name for self-hosted runners or GitHub-hosted runner groups
    # (optional)
    group: "example-value"

    # List of runner labels for self-hosted runners or GitHub-hosted runner selection
    # (optional)
    labels: []
      # Array of strings

  # Custom steps to inject into all safe-output jobs. These steps run after checking
  # out the repository and setting up the action, and before any safe-output code
  # executes.
  # (optional)
  steps: []

  # Custom GitHub Actions to mount as once-callable MCP tools. Each action is
  # resolved at compile time to derive its input schema from action.yml, and a
  # guarded `uses:` step is injected in the safe_outputs job. Action names
  # containing dashes will be automatically normalized to underscores (e.g.,
  # 'add-smoked-label' becomes 'add_smoked_label').
  # (optional)
  actions:
    {}

  # Enable AI agents to signal that a task could not be completed due to
  # infrastructure or tool failures (e.g., MCP crash, missing auth, inaccessible
  # repository). Activates failure handling even when the agent exits 0.
  # (optional)
  # Accepted formats:

  # Format 1: Configuration for report_incomplete safe output
  report-incomplete:
    # Maximum number of report_incomplete signals (default: 5). Supports integer or
    # GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Whether to create or update GitHub issues when the task was incomplete (default:
    # true). Supports literal boolean or GitHub Actions expression (e.g. '${{
    # inputs.create-incomplete-issue }}').
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    create-issue: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    create-issue: "example-value"

    # Prefix for issue titles when creating issues for incomplete runs (default:
    # '[incomplete]')
    # (optional)
    title-prefix: "example-value"

    # Labels to add to created issues for incomplete runs
    # (optional)
    labels: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler. Used by the hidden `gh aw compile
    # --use-samples` flag to replace the agentic step with a deterministic replay
    # through the safe-outputs MCP server. Each entry should conform to the
    # corresponding MCP tool inputSchema; recognized sidecar keys (currently `patch`
    # for create-pull-request and push-to-pull-request-branch) are stripped before
    # schema validation and consumed by the replay driver.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

  # Format 2: Enable report_incomplete with default configuration
  report-incomplete: null

  # Format 3: Explicitly disable report_incomplete (false). report_incomplete is
  # enabled by default when safe-outputs is configured.
  report-incomplete: true

  # Enable AI agents to replace one label with another on GitHub issues or pull
  # requests in a single atomic operation. Ideal for maintaining label-based state
  # machines (e.g. transitioning issues through workflow states).
  # (optional)
  # Accepted formats:

  # Format 1: Null configuration allows any labels to be replaced. Labels will be
  # created if they don't already exist in the repository.
  replace-label: null

  # Format 2: Configuration for replacing one label with another on issues/PRs in a
  # single atomic operation. Enables clear state transitions (e.g. 'in-progress' →
  # 'done').
  replace-label:
    # Optional list of allowed label state transitions. Each entry specifies a (from,
    # to) pair that is permitted. When specified, the agent may ONLY perform the
    # listed transitions, regardless of allowed-add/allowed-remove. Useful for
    # enforcing a strict state machine (e.g. only 'in-review' → 'approved' is
    # allowed).
    # (optional)
    allowed-transitions: []
      # Array items:
        # The label that must currently be on the item and will be removed.
        from: "example-value"

        # The label that will be added.
        to: "example-value"

    # Optional list of allowed label patterns that can be added (supports glob
    # patterns like 'state-*'). Labels will be created if they don't already exist in
    # the repository. If omitted, any labels are allowed.
    # (optional)
    allowed-add: []
      # Array of strings

    # Optional list of allowed label patterns that can be removed (supports glob
    # patterns like 'state-*'). If omitted, any labels can be removed.
    # (optional)
    allowed-remove: []
      # Array of strings

    # Optional list of blocked label patterns (supports glob patterns like '~*',
    # '*[bot]'). Applied to both label_to_add and label_to_remove. Rejected before
    # allowed list filtering.
    # (optional)
    blocked: []
      # Array of strings

    # Optional maximum number of label replacements (default: 5). Supports integer or
    # GitHub Actions expression (e.g. '${{ inputs.max }}').
    # (optional)
    # Accepted formats:

    # Format 1: integer
    max: 1

    # Format 2: GitHub Actions expression that resolves to an integer at runtime
    max: "example-value"

    # Target for the operation: 'triggering' (default), '*' (any issue/PR), or
    # explicit issue/PR number
    # (optional)
    target: "example-value"

    # Target repository in format 'owner/repo' for cross-repository label replacement.
    # Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # List of additional repositories in format 'owner/repo' that label replacements
    # can target. When specified, the agent can use a 'repo' field in the output to
    # specify which repository to update labels on.
    # (optional)
    allowed-repos: []
      # Array of strings

    # Conjunctive label constraint: ALL of these labels must be present on the
    # issue/PR for the operation to proceed.
    # (optional)
    required-labels: []
      # Array of strings

    # Title prefix constraint: the issue/PR title must start with this prefix for the
    # operation to proceed.
    # (optional)
    required-title-prefix: "example-value"

    # When true, emit step summary messages instead of making GitHub API calls for
    # this specific output type (preview mode)
    # (optional)
    # Accepted formats:

    # Format 1: boolean
    staged: true

    # Format 2: GitHub Actions expression that resolves to a boolean at runtime
    staged: "example-value"

    # Internal hidden feature. Optional list of declarative sample payloads that
    # exercise this safe-output handler.
    # (optional)
    # Accepted formats:

    # Format 1: array
    samples: []
      # Array items: object

    # Format 2: object
    samples:
      {}

    # GitHub App authentication. Mints a short-lived installation access token via
    # actions/create-github-app-token. Mutually exclusive with github-token.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
      # token.
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

      # Optional extra GitHub App-only permissions to merge into the minted token. Takes
      # effect for tools.github.github-app and safe-outputs.github-app; ignored in
      # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
      # scopes (e.g. members, organization-administration) not expressible via standard
      # handler declarations.
      # (optional)
      permissions:
        # Permission level for repository administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission for repository administration.
        # (optional)
        administration: "read"

        # Permission level for Codespaces (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces: "read"

        # Permission level for Codespaces lifecycle administration (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        codespaces-lifecycle-admin: "read"

        # Permission level for Codespaces metadata (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        codespaces-metadata: "read"

        # Permission level for user email addresses (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        email-addresses: "read"

        # Permission level for repository environments (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        environments: "read"

        # Permission level for git signing (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        git-signing: "read"

        # Permission level for organization members (read/none; "write" is rejected by the
        # compiler). Required for org team membership API calls.
        # (optional)
        members: "read"

        # Permission level for organization administration (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-administration: "read"

        # Permission level for organization announcement banners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-announcement-banners: "read"

        # Permission level for organization Codespaces (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-codespaces: "read"

        # Permission level for organization Copilot (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-copilot: "read"

        # Permission level for organization custom org roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-org-roles: "read"

        # Permission level for organization custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-properties: "read"

        # Permission level for organization custom repository roles (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-custom-repository-roles: "read"

        # Permission level for organization events (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-events: "read"

        # Permission level for organization webhooks (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-hooks: "read"

        # Permission level for organization members management (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-members: "read"

        # Permission level for organization packages (read/none; "write" is rejected by
        # the compiler). GitHub App-only permission.
        # (optional)
        organization-packages: "read"

        # Permission level for organization personal access token requests (read/none;
        # "write" is rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-token-requests: "read"

        # Permission level for organization personal access tokens (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-personal-access-tokens: "read"

        # Permission level for organization plan (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        organization-plan: "read"

        # Permission level for organization self-hosted runners (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        organization-self-hosted-runners: "read"

        # Permission level for organization user blocking (read/none; "write" is rejected
        # by the compiler). GitHub App-only permission.
        # (optional)
        organization-user-blocking: "read"

        # Permission level for repository custom properties (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        repository-custom-properties: "read"

        # Permission level for secret scanning alerts (read/none). Forwarded as
        # permission-secret-scanning-alerts input for actions/create-github-app-token.
        # (optional)
        secret-scanning-alerts: "read"

        # Permission level for repository webhooks (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        repository-hooks: "read"

        # Permission level for single file access (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        single-file: "read"

        # Permission level for team discussions (read/none; "write" is rejected by the
        # compiler). GitHub App-only permission.
        # (optional)
        team-discussions: "read"

        # Permission level for Dependabot vulnerability alerts (read/none; "write" is
        # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
        # with a GitHub App, forwarded as permission-vulnerability-alerts input.
        # (optional)
        vulnerability-alerts: "read"

        # Permission level for GitHub Actions workflow files (read/none; "write" is
        # rejected by the compiler). GitHub App-only permission.
        # (optional)
        workflows: "read"

# Configuration for secret redaction behavior in workflow outputs and artifacts
# (optional)
secret-masking:
  # Additional secret redaction steps to inject after the built-in secret redaction.
  # Use this to mask secrets in generated files using custom patterns.
  # (optional)
  steps: []

# Optional observability output settings for workflow runs.
# (optional)
observability:
  # OTLP (OpenTelemetry Protocol) trace export configuration.
  # (optional)
  otlp:
    # OTLP endpoint configuration. Accepts a plain URL string (backward-compat), a
    # single {url, headers} object, or an array of {url, headers} objects for
    # multi-endpoint concurrent fan-out. Encoded as GH_AW_OTLP_ENDPOINTS (JSON array).
    # (optional)
    # Accepted formats:

    # Format 1: OTLP collector endpoint URL (e.g. 'https://traces.example.com:4317').
    # Supports GitHub Actions expressions such as ${{ secrets.OTLP_ENDPOINT }}. When a
    # static URL is provided, its hostname is automatically added to the network
    # firewall allowlist.
    endpoint: "example-value"

    # Format 2: A single OTLP endpoint with a URL and optional per-endpoint headers.
    endpoint:
      # OTLP collector endpoint URL (e.g. 'https://traces.example.com:4317'). Supports
      # GitHub Actions expressions such as ${{ secrets.OTLP_ENDPOINT }}. When a static
      # URL is provided, its hostname is automatically added to the network firewall
      # allowlist.
      url: "example-value"

      # (optional)
      # Accepted formats:

      # Format 1: Map of HTTP header names to values. Values support GitHub Actions
      # expressions such as ${{ secrets.TOKEN }}.
      headers:
        {}

      # Format 2: Deprecated: use the map form instead. Comma-separated list of
      # key=value HTTP headers (e.g. 'Authorization=Bearer <token>'). Supports GitHub
      # Actions expressions such as ${{ secrets.OTLP_HEADERS }}.
      headers: "example-value"

    # Format 3: Multiple OTLP collector endpoints to export traces to concurrently.
    # Each entry has its own URL and optional per-endpoint headers.
    endpoint: []
      # Array items: A single OTLP endpoint with a URL and optional per-endpoint
      # headers.

    # HTTP headers for the backward-compat string endpoint form. Only used when
    # endpoint is a plain string; object/array endpoint entries carry their own
    # per-endpoint headers.
    # (optional)
    # Accepted formats:

    # Format 1: Map of HTTP header names to values to include with every OTLP export
    # request. Values support GitHub Actions expressions such as ${{ secrets.TOKEN }}.
    # Injected as the OTEL_EXPORTER_OTLP_HEADERS environment variable.
    headers:
      {}

    # Format 2: Deprecated: use the map form instead. Comma-separated list of
    # key=value HTTP headers to include with every OTLP export request (e.g.
    # 'Authorization=Bearer <token>'). Supports GitHub Actions expressions such as ${{
    # secrets.OTLP_HEADERS }}. Injected as the OTEL_EXPORTER_OTLP_HEADERS environment
    # variable.
    headers: "example-value"

    # How to handle missing OTLP endpoint/header values at runtime (for example from
    # unset secrets). 'error' fails workflow startup (default), 'warn' logs a warning
    # and skips MCP gateway OTLP configuration, and 'ignore' skips MCP gateway OTLP
    # configuration without warning. This affects MCP gateway setup only;
    # workflow-level OTEL_* environment variables are still injected.
    # (optional)
    if-missing: "error"

    # Additional custom span attributes to attach to OTLP spans emitted by this
    # workflow. Values may be static strings or template expressions resolved from
    # existing span attributes, such as '{{ gh-aw.episode.id }}' or '{{ github.actor
    # }}'. Values are exported to tracing backends and masked in runner logs.
    # (optional)
    attributes:
      {}

    # Additional OTEL_RESOURCE_ATTRIBUTES entries to append to the standard
    # gh-aw/GitHub resource attributes. Values may be static strings or GitHub Actions
    # expressions such as '${{ github.repository }}'. Do not use secrets.* or vars.*
    # expressions here: resource attributes are exported to external tracing backends
    # and are not treated as secret values.
    # (optional)
    resource-attributes:
      {}

    # Optional runtime authentication for OTLP export. Supports GitHub App credentials
    # (client-id/app-id + private-key) for token minting, or implicit GitHub OIDC mode
    # when the github-app object is present without credentials.
    # (optional)
    github-app:
      # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
      # }}').
      # (optional)
      app-id: "example-value"

      # GitHub App client ID (e.g., '${{ vars.APP_ID }}').
      # (optional)
      client-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}').
      # (optional)
      private-key: "example-value"

      # If true, skip token minting when client-id/private-key resolve to empty strings
      # at runtime. Defaults to false.
      # (optional)
      ignore-if-missing: true

      # Optional OIDC audience passed to core.getIDToken(audience) when using
      # credential-less OIDC mode (github-app present without app-id/private-key). Leave
      # omitted to use the default audience.
      # (optional)
      audience: "example-value"

    # Exchange a GitHub Actions OIDC token for a cloud access token before OTLP
    # export. Google Workload Identity Federation is currently supported.
    # (optional)
    workload-identity:
      # Cloud workload identity provider.
      provider: "google"

      # Google Workload Identity Provider resource name (e.g.
      # projects/PROJECT_NUMBER/locations/global/workloadIdentityPools/POOL/providers/PROVIDER).
      # The GitHub OIDC audience (https://iam.googleapis.com/...) and Google STS
      # audience (//iam.googleapis.com/...) are derived from it; either fully-qualified
      # form is also accepted.
      audience: "example-value"

      # Optional Google service account email to impersonate after STS token exchange.
      # (optional)
      service-account: "example-value"

# Rate limiting configuration to restrict how frequently users can trigger the
# workflow. Helps prevent abuse and resource exhaustion from programmatically
# triggered events.
# (optional)
user-rate-limit:
  # Maximum number of workflow runs allowed per user within the time window.
  # Required field. Supports integer or GitHub Actions expression (e.g. '${{
  # inputs.max }}').
  # Accepted formats:

  # Format 1: integer
  max-runs-per-window: 1

  # Format 2: GitHub Actions expression that resolves to an integer at runtime
  max-runs-per-window: "example-value"

  # Time window in minutes for rate limiting. Defaults to 60 (1 hour). Maximum: 180
  # (3 hours).
  # (optional)
  window: 1

  # Optional list of event types to apply rate limiting to. If not specified, rate
  # limiting is inferred from the workflow triggers; if no supported programmatic
  # triggers are found, it falls back to all supported programmatic events.
  # (optional)
  events: []
    # Array of strings

  # Optional list of roles that are exempt from rate limiting. Defaults to ['admin',
  # 'maintain', 'write'] if not specified. Users with any of these roles will not be
  # subject to rate limiting checks. To apply rate limiting to all users, set to an
  # empty array: []
  # (optional)
  ignored-roles: []
    # Array of strings

# Enable strict mode validation for enhanced security and compliance. Strict mode
# enforces: (1) Write Permissions - refuses contents:write, issues:write,
# pull-requests:write; requires safe-outputs instead, (2) Network Configuration -
# requires explicit network configuration with no standalone wildcard '*' in
# allowed domains (patterns like '*.example.com' are allowed), (3) Action Pinning
# - enforces actions pinned to commit SHAs instead of tags/branches, (4) MCP
# Network - requires network configuration for custom MCP servers with containers,
# (5) Deprecated Fields - refuses deprecated frontmatter fields. Can be enabled
# per-workflow via 'strict: true' in frontmatter, or disabled via 'strict: false'.
# CLI flag takes precedence over frontmatter (gh aw compile --strict enforces
# strict mode). Defaults to true. See:
# https://github.github.com/gh-aw/reference/frontmatter/#strict-mode-strict
# (optional)
strict: true

# Mark the workflow as private, preventing it from being added to other
# repositories via 'gh aw add'. A workflow with private: true is not meant to be
# shared outside its repository.
# (optional)
private: true

# Control whether the compile-agentic version update check runs in the activation
# job. When true (default), the activation job downloads config.json from the
# gh-aw repository and verifies the compiled version is not blocked and meets the
# minimum supported version. Set to false to disable the check (not allowed in
# strict mode). See:
# https://github.github.com/gh-aw/reference/frontmatter/#check-for-updates
# (optional)
check-for-updates: true

# Optional list of environment variable names to unconditionally exclude from the
# AWF agent container via --exclude-env. Use when an env var is set from a source
# the compiler cannot auto-detect as credential-bearing (e.g. a workflow_dispatch
# input carrying a token). Names are deduplicated and merged with those
# auto-detected from secrets.* and needs.*.outputs.* references.
# (optional)
excluded-env: []
  # Array of Environment variable name to exclude from the agent container.

# MCP Scripts configuration for defining custom lightweight MCP tools as
# JavaScript, shell scripts, or Python scripts. Tools are mounted in an MCP server
# and have access to secrets specified by the user. Only one of 'script'
# (JavaScript), 'run' (shell), or 'py' (Python) must be specified per tool.
# (optional)
mcp-scripts:
  {}

# Runtime environment version overrides. Allows customizing runtime versions
# (e.g., Node.js, Python) or defining new runtimes. Runtimes from imported shared
# workflows are also merged.
# (optional)
runtimes:
  # Runtime configuration object identified by runtime ID (e.g., 'node', 'python',
  # 'go')
  node:
    # Runtime version as a string (e.g., '22', '3.12', 'latest') or number (e.g., 22,
    # 3.12). Numeric values are automatically converted to strings at runtime.
    # (optional)
    version: null

    # GitHub Actions repository for setting up the runtime (e.g.,
    # 'actions/setup-node', 'custom/setup-runtime'). Overrides the default setup
    # action.
    # (optional)
    action-repo: "example-value"

    # Version of the setup action to use (e.g., 'v4', 'v5'). Overrides the default
    # action version.
    # (optional)
    action-version: "example-value"

    # Optional GitHub Actions if condition to control when the runtime setup step
    # runs. Supports standard GitHub Actions expression syntax. Useful for
    # conditionally installing runtimes based on file presence (e.g.,
    # "hashFiles('go.mod') != ''" to install Go only when go.mod exists).
    # (optional)
    if: "example-value"

    # Enable a default 3-day dependency cooldown for installs associated with this
    # runtime. Set to false to disable.
    # (optional)
    cooldown: true

    # Allow npm pre/post install scripts to execute during package installation. A
    # supply chain security warning is emitted at compile time; in strict mode this is
    # an error. See:
    # https://github.github.com/gh-aw/reference/frontmatter/#run-install-scripts
    # (optional)
    run-install-scripts: true

# Checkout configuration for the agent job. Controls how actions/checkout is
# invoked. Can be a single checkout configuration, an array for multiple
# checkouts, or false to disable the default checkout step entirely (dev-mode
# checkouts are unaffected).
# (optional)
# Accepted formats:

# Format 1: Single checkout configuration for the default workspace
checkout:
  # Repository to checkout in owner/repo format. Defaults to the current repository.
  # (optional)
  repository: "example-value"

  # Branch, tag, or SHA to checkout. Defaults to the ref that triggered the
  # workflow.
  # (optional)
  ref: "example-value"

  # Relative path within GITHUB_WORKSPACE to place the checkout. Defaults to the
  # workspace root.
  # (optional)
  path: "example-value"

  # Number of commits to fetch. 0 fetches all history. 1 (default) is a shallow
  # clone. When multiple configs target the same path, the deepest value is used.
  # (optional)
  fetch-depth: 1

  # Enable sparse-checkout with newline-separated patterns. When multiple configs
  # target the same path, patterns are merged.
  # (optional)
  sparse-checkout: "example-value"

  # Controls submodule checkout. Use "recursive" for all submodules, "true" for
  # immediate submodules, or "false" to skip.
  # (optional)
  # Accepted formats:

  # Format 1: string
  submodules: "recursive"

  # Format 2: boolean
  submodules: true

  # Whether to download Git LFS objects. Defaults to false.
  # (optional)
  lfs: true

  # Deprecated: Use github-token instead. GitHub token for authentication.
  # Credentials are always removed after checkout (persist-credentials: false is
  # enforced).
  # (optional)
  token: "example-value"

  # GitHub token for authentication. Use ${{ secrets.MY_TOKEN }} to reference a
  # secret. Mutually exclusive with github-app (and deprecated app). Credentials are
  # always removed after checkout (persist-credentials: false is enforced).
  # (optional)
  github-token: "${{ secrets.GITHUB_TOKEN }}"

  # GitHub App authentication. Mints a short-lived installation access token via
  # actions/create-github-app-token. Mutually exclusive with github-token.
  # (optional)
  github-app:
    # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
    # }}').
    # (optional)
    app-id: "example-value"

    # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
    # token.
    # (optional)
    client-id: "example-value"

    # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
    # mint a GitHub App token.
    # (optional)
    private-key: "example-value"

    # If true, skip token minting when client-id/private-key resolve to empty strings
    # at runtime. Defaults to false.
    # (optional)
    ignore-if-missing: true

    # Optional owner of the GitHub App installation (defaults to current repository
    # owner if not specified)
    # (optional)
    owner: "example-value"

    # Optional list of repositories to grant access to (defaults to current repository
    # if not specified)
    # (optional)
    repositories: []
      # Array of strings

    # Optional extra GitHub App-only permissions to merge into the minted token. Takes
    # effect for tools.github.github-app and safe-outputs.github-app; ignored in
    # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
    # scopes (e.g. members, organization-administration) not expressible via standard
    # handler declarations.
    # (optional)
    permissions:
      # Permission level for repository administration (read/none; "write" is rejected
      # by the compiler). GitHub App-only permission for repository administration.
      # (optional)
      administration: "read"

      # Permission level for Codespaces (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      codespaces: "read"

      # Permission level for Codespaces lifecycle administration (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      codespaces-lifecycle-admin: "read"

      # Permission level for Codespaces metadata (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      codespaces-metadata: "read"

      # Permission level for user email addresses (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      email-addresses: "read"

      # Permission level for repository environments (read/none; "write" is rejected by
      # the compiler). GitHub App-only permission.
      # (optional)
      environments: "read"

      # Permission level for git signing (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      git-signing: "read"

      # Permission level for organization members (read/none; "write" is rejected by the
      # compiler). Required for org team membership API calls.
      # (optional)
      members: "read"

      # Permission level for organization administration (read/none; "write" is rejected
      # by the compiler). GitHub App-only permission.
      # (optional)
      organization-administration: "read"

      # Permission level for organization announcement banners (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-announcement-banners: "read"

      # Permission level for organization Codespaces (read/none; "write" is rejected by
      # the compiler). GitHub App-only permission.
      # (optional)
      organization-codespaces: "read"

      # Permission level for organization Copilot (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      organization-copilot: "read"

      # Permission level for organization custom org roles (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-custom-org-roles: "read"

      # Permission level for organization custom properties (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-custom-properties: "read"

      # Permission level for organization custom repository roles (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-custom-repository-roles: "read"

      # Permission level for organization events (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      organization-events: "read"

      # Permission level for organization webhooks (read/none; "write" is rejected by
      # the compiler). GitHub App-only permission.
      # (optional)
      organization-hooks: "read"

      # Permission level for organization members management (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-members: "read"

      # Permission level for organization packages (read/none; "write" is rejected by
      # the compiler). GitHub App-only permission.
      # (optional)
      organization-packages: "read"

      # Permission level for organization personal access token requests (read/none;
      # "write" is rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-personal-access-token-requests: "read"

      # Permission level for organization personal access tokens (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-personal-access-tokens: "read"

      # Permission level for organization plan (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      organization-plan: "read"

      # Permission level for organization self-hosted runners (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-self-hosted-runners: "read"

      # Permission level for organization user blocking (read/none; "write" is rejected
      # by the compiler). GitHub App-only permission.
      # (optional)
      organization-user-blocking: "read"

      # Permission level for repository custom properties (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      repository-custom-properties: "read"

      # Permission level for secret scanning alerts (read/none). Forwarded as
      # permission-secret-scanning-alerts input for actions/create-github-app-token.
      # (optional)
      secret-scanning-alerts: "read"

      # Permission level for repository webhooks (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      repository-hooks: "read"

      # Permission level for single file access (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      single-file: "read"

      # Permission level for team discussions (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      team-discussions: "read"

      # Permission level for Dependabot vulnerability alerts (read/none; "write" is
      # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
      # with a GitHub App, forwarded as permission-vulnerability-alerts input.
      # (optional)
      vulnerability-alerts: "read"

      # Permission level for GitHub Actions workflow files (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      workflows: "read"

  # GitHub App authentication used only by safe_outputs checkout/fetch/push
  # operations for this checkout target. Does not change activation/agent checkout
  # authentication.
  # (optional)
  safe-outputs-github-app:
    # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
    # }}').
    # (optional)
    app-id: "example-value"

    # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
    # token.
    # (optional)
    client-id: "example-value"

    # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
    # mint a GitHub App token.
    # (optional)
    private-key: "example-value"

    # If true, skip token minting when client-id/private-key resolve to empty strings
    # at runtime. Defaults to false.
    # (optional)
    ignore-if-missing: true

    # Optional owner of the GitHub App installation (defaults to current repository
    # owner if not specified)
    # (optional)
    owner: "example-value"

    # Optional list of repositories to grant access to (defaults to current repository
    # if not specified)
    # (optional)
    repositories: []
      # Array of strings

    # Optional extra GitHub App-only permissions to merge into the minted token. Takes
    # effect for tools.github.github-app and safe-outputs.github-app; ignored in
    # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
    # scopes (e.g. members, organization-administration) not expressible via standard
    # handler declarations.
    # (optional)
    permissions:
      # Permission level for repository administration (read/none; "write" is rejected
      # by the compiler). GitHub App-only permission for repository administration.
      # (optional)
      administration: "read"

      # Permission level for Codespaces (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      codespaces: "read"

      # Permission level for Codespaces lifecycle administration (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      codespaces-lifecycle-admin: "read"

      # Permission level for Codespaces metadata (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      codespaces-metadata: "read"

      # Permission level for user email addresses (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      email-addresses: "read"

      # Permission level for repository environments (read/none; "write" is rejected by
      # the compiler). GitHub App-only permission.
      # (optional)
      environments: "read"

      # Permission level for git signing (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      git-signing: "read"

      # Permission level for organization members (read/none; "write" is rejected by the
      # compiler). Required for org team membership API calls.
      # (optional)
      members: "read"

      # Permission level for organization administration (read/none; "write" is rejected
      # by the compiler). GitHub App-only permission.
      # (optional)
      organization-administration: "read"

      # Permission level for organization announcement banners (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-announcement-banners: "read"

      # Permission level for organization Codespaces (read/none; "write" is rejected by
      # the compiler). GitHub App-only permission.
      # (optional)
      organization-codespaces: "read"

      # Permission level for organization Copilot (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      organization-copilot: "read"

      # Permission level for organization custom org roles (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-custom-org-roles: "read"

      # Permission level for organization custom properties (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-custom-properties: "read"

      # Permission level for organization custom repository roles (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-custom-repository-roles: "read"

      # Permission level for organization events (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      organization-events: "read"

      # Permission level for organization webhooks (read/none; "write" is rejected by
      # the compiler). GitHub App-only permission.
      # (optional)
      organization-hooks: "read"

      # Permission level for organization members management (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-members: "read"

      # Permission level for organization packages (read/none; "write" is rejected by
      # the compiler). GitHub App-only permission.
      # (optional)
      organization-packages: "read"

      # Permission level for organization personal access token requests (read/none;
      # "write" is rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-personal-access-token-requests: "read"

      # Permission level for organization personal access tokens (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-personal-access-tokens: "read"

      # Permission level for organization plan (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      organization-plan: "read"

      # Permission level for organization self-hosted runners (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      organization-self-hosted-runners: "read"

      # Permission level for organization user blocking (read/none; "write" is rejected
      # by the compiler). GitHub App-only permission.
      # (optional)
      organization-user-blocking: "read"

      # Permission level for repository custom properties (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      repository-custom-properties: "read"

      # Permission level for secret scanning alerts (read/none). Forwarded as
      # permission-secret-scanning-alerts input for actions/create-github-app-token.
      # (optional)
      secret-scanning-alerts: "read"

      # Permission level for repository webhooks (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      repository-hooks: "read"

      # Permission level for single file access (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      single-file: "read"

      # Permission level for team discussions (read/none; "write" is rejected by the
      # compiler). GitHub App-only permission.
      # (optional)
      team-discussions: "read"

      # Permission level for Dependabot vulnerability alerts (read/none; "write" is
      # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
      # with a GitHub App, forwarded as permission-vulnerability-alerts input.
      # (optional)
      vulnerability-alerts: "read"

      # Permission level for GitHub Actions workflow files (read/none; "write" is
      # rejected by the compiler). GitHub App-only permission.
      # (optional)
      workflows: "read"

  # Marks this checkout as the logical current repository for the workflow. When set
  # to true, the AI agent will treat this repository as its primary working target.
  # Only one checkout may have current set to true. Useful for central-repo
  # workflows targeting a different repository.
  # (optional)
  current: true

  # Additional Git refs to fetch after the checkout. Supported values: "*" (all
  # branches), "refs/pulls/open/*" (all open pull-request refs), branch names (e.g.
  # "main"), or glob patterns (e.g. "feature/*").
  # (optional)
  # Accepted formats:

  # Format 1: A single additional ref pattern to fetch after checkout.
  fetch: "example-value"

  # Format 2: Additional Git refs to fetch after checkout. A git fetch step is
  # emitted after the actions/checkout step.
  fetch: []
    # Array items: string

  # When true, clones the repository's wiki git instead of the regular repository.
  # The effective repository becomes "{repository}.wiki" (e.g. "owner/repo.wiki").
  # Defaults to false.
  # (optional)
  wiki: true

  # When true, persist credentials during checkout, then immediately run a
  # post-checkout cleanup step that removes credentials from root and submodule git
  # configs. Useful for submodule-safe cleanup behavior.
  # (optional)
  force-clean-git-credentials: true

# Format 2: Multiple checkout configurations
checkout: []
  # Array items: Configuration for a single actions/checkout step

# Format 3: Set to false to disable the default checkout step. The agent job will
# not check out any repository (dev-mode checkouts are unaffected).
checkout: false

# Top-level GitHub App configuration used as a fallback for all nested github-app
# token minting operations (on, safe-outputs, checkout, tools.github,
# dependencies). When a nested section does not define its own github-app, this
# top-level configuration is used automatically.
# (optional)
github-app:
  # Deprecated alias for client-id. GitHub App ID/client ID (e.g., '${{ vars.APP_ID
  # }}').
  # (optional)
  app-id: "example-value"

  # GitHub App client ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App
  # token.
  # (optional)
  client-id: "example-value"

  # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
  # mint a GitHub App token.
  # (optional)
  private-key: "example-value"

  # If true, skip token minting when client-id/private-key resolve to empty strings
  # at runtime. Defaults to false.
  # (optional)
  ignore-if-missing: true

  # Optional owner of the GitHub App installation (defaults to current repository
  # owner if not specified)
  # (optional)
  owner: "example-value"

  # Optional list of repositories to grant access to (defaults to current repository
  # if not specified)
  # (optional)
  repositories: []
    # Array of strings

  # Optional extra GitHub App-only permissions to merge into the minted token. Takes
  # effect for tools.github.github-app and safe-outputs.github-app; ignored in
  # on.github-app and the top-level github-app fallback. Use to add GitHub App-only
  # scopes (e.g. members, organization-administration) not expressible via standard
  # handler declarations.
  # (optional)
  permissions:
    # Permission level for repository administration (read/none; "write" is rejected
    # by the compiler). GitHub App-only permission for repository administration.
    # (optional)
    administration: "read"

    # Permission level for Codespaces (read/none; "write" is rejected by the
    # compiler). GitHub App-only permission.
    # (optional)
    codespaces: "read"

    # Permission level for Codespaces lifecycle administration (read/none; "write" is
    # rejected by the compiler). GitHub App-only permission.
    # (optional)
    codespaces-lifecycle-admin: "read"

    # Permission level for Codespaces metadata (read/none; "write" is rejected by the
    # compiler). GitHub App-only permission.
    # (optional)
    codespaces-metadata: "read"

    # Permission level for user email addresses (read/none; "write" is rejected by the
    # compiler). GitHub App-only permission.
    # (optional)
    email-addresses: "read"

    # Permission level for repository environments (read/none; "write" is rejected by
    # the compiler). GitHub App-only permission.
    # (optional)
    environments: "read"

    # Permission level for git signing (read/none; "write" is rejected by the
    # compiler). GitHub App-only permission.
    # (optional)
    git-signing: "read"

    # Permission level for organization members (read/none; "write" is rejected by the
    # compiler). Required for org team membership API calls.
    # (optional)
    members: "read"

    # Permission level for organization administration (read/none; "write" is rejected
    # by the compiler). GitHub App-only permission.
    # (optional)
    organization-administration: "read"

    # Permission level for organization announcement banners (read/none; "write" is
    # rejected by the compiler). GitHub App-only permission.
    # (optional)
    organization-announcement-banners: "read"

    # Permission level for organization Codespaces (read/none; "write" is rejected by
    # the compiler). GitHub App-only permission.
    # (optional)
    organization-codespaces: "read"

    # Permission level for organization Copilot (read/none; "write" is rejected by the
    # compiler). GitHub App-only permission.
    # (optional)
    organization-copilot: "read"

    # Permission level for organization custom org roles (read/none; "write" is
    # rejected by the compiler). GitHub App-only permission.
    # (optional)
    organization-custom-org-roles: "read"

    # Permission level for organization custom properties (read/none; "write" is
    # rejected by the compiler). GitHub App-only permission.
    # (optional)
    organization-custom-properties: "read"

    # Permission level for organization custom repository roles (read/none; "write" is
    # rejected by the compiler). GitHub App-only permission.
    # (optional)
    organization-custom-repository-roles: "read"

    # Permission level for organization events (read/none; "write" is rejected by the
    # compiler). GitHub App-only permission.
    # (optional)
    organization-events: "read"

    # Permission level for organization webhooks (read/none; "write" is rejected by
    # the compiler). GitHub App-only permission.
    # (optional)
    organization-hooks: "read"

    # Permission level for organization members management (read/none; "write" is
    # rejected by the compiler). GitHub App-only permission.
    # (optional)
    organization-members: "read"

    # Permission level for organization packages (read/none; "write" is rejected by
    # the compiler). GitHub App-only permission.
    # (optional)
    organization-packages: "read"

    # Permission level for organization personal access token requests (read/none;
    # "write" is rejected by the compiler). GitHub App-only permission.
    # (optional)
    organization-personal-access-token-requests: "read"

    # Permission level for organization personal access tokens (read/none; "write" is
    # rejected by the compiler). GitHub App-only permission.
    # (optional)
    organization-personal-access-tokens: "read"

    # Permission level for organization plan (read/none; "write" is rejected by the
    # compiler). GitHub App-only permission.
    # (optional)
    organization-plan: "read"

    # Permission level for organization self-hosted runners (read/none; "write" is
    # rejected by the compiler). GitHub App-only permission.
    # (optional)
    organization-self-hosted-runners: "read"

    # Permission level for organization user blocking (read/none; "write" is rejected
    # by the compiler). GitHub App-only permission.
    # (optional)
    organization-user-blocking: "read"

    # Permission level for repository custom properties (read/none; "write" is
    # rejected by the compiler). GitHub App-only permission.
    # (optional)
    repository-custom-properties: "read"

    # Permission level for secret scanning alerts (read/none). Forwarded as
    # permission-secret-scanning-alerts input for actions/create-github-app-token.
    # (optional)
    secret-scanning-alerts: "read"

    # Permission level for repository webhooks (read/none; "write" is rejected by the
    # compiler). GitHub App-only permission.
    # (optional)
    repository-hooks: "read"

    # Permission level for single file access (read/none; "write" is rejected by the
    # compiler). GitHub App-only permission.
    # (optional)
    single-file: "read"

    # Permission level for team discussions (read/none; "write" is rejected by the
    # compiler). GitHub App-only permission.
    # (optional)
    team-discussions: "read"

    # Permission level for Dependabot vulnerability alerts (read/none; "write" is
    # rejected by the compiler). Also available as a GITHUB_TOKEN scope. When used
    # with a GitHub App, forwarded as permission-vulnerability-alerts input.
    # (optional)
    vulnerability-alerts: "read"

    # Permission level for GitHub Actions workflow files (read/none; "write" is
    # rejected by the compiler). GitHub App-only permission.
    # (optional)
    workflows: "read"

# Schema for validating 'with' input values when this workflow is imported by
# another workflow using the 'uses'/'with' syntax. Defines the expected
# parameters, their types, and whether they are required. Scalar inputs are
# accessible via '${{ github.aw.import-inputs.<name> }}' expressions. Object
# inputs (type: object) allow one-level deep sub-fields accessible via '${{
# github.aw.import-inputs.<name>.<subkey> }}' expressions.
# (optional)
import-schema:
  {}

# Default LLM model, overridden by nested engine.model. Sets the default model
# used by the agentic engine for this workflow. Acts as a fallback when an engine
# instance does not specify its own 'model'; a nested 'engine.model' (e.g.
# safe-outputs.threat-detection.engine.model) takes precedence over this field for
# that engine instance. Supports full model IDs (e.g.
# 'claude-3-5-sonnet-20241022', 'gpt-5.4') and model aliases (e.g. 'small',
# 'large').
# (optional)
model: "example-value"

# ⚠️ Experimental. Deterministic graders for workflow-run observations. Built-in
# graders use execution artifacts, custom graders use inline scripts, and the
# operational-value grader uses a repository evaluator.
# (optional)
graders:
  {}

# ⚠️ Experimental. BinEval binary evaluation questions to run after safe-outputs
# and before the conclusion job. Can be a plain list of questions (shorthand) or
# an object with questions, model, and runs-on fields.
# (optional)
# Accepted formats:

# Format 1: Shorthand form: plain list of evaluation question objects.
evals: []
  # Array items: object

# Format 2: Extended form: object with questions list and optional configuration.
evals:
  # List of binary evaluation question objects.
  # (optional)
  questions: []
    # Array items:
      # Unique identifier for the evaluation question.
      id: "example-value"

      # The binary (YES/NO) question to evaluate.
      question: "example-value"

      # Optional model override for this specific question. Overrides the top-level
      # model setting. Use a model alias such as "small" or a full model ID.
      # (optional)
      model: "example-value"

  # Default model to use for all evaluation questions. Use a model alias such as
  # "small" or a full model ID. Per-question model fields override this setting.
  # (optional)
  model: "example-value"

  # Runner type for workflow execution (GitHub Actions standard field). Supports
  # multiple forms: simple string for single runner label (e.g., 'ubuntu-latest'),
  # array for runner selection with fallbacks, or object for GitHub-hosted runner
  # groups with specific labels. For agentic workflows, runner selection matters
  # when AI workloads require specific compute resources or when using self-hosted
  # runners with specialized capabilities. Typically configured at the job level
  # instead. See
  # https://docs.github.com/en/actions/using-jobs/choosing-the-runner-for-a-job
  # (optional)
  # Accepted formats:

  # Format 1: Simple runner label string. Use for standard GitHub-hosted runners
  # (e.g., 'ubuntu-latest', 'windows-latest', 'macos-latest') or self-hosted runner
  # labels. Most common form for agentic workflows.
  runs-on: "example-value"

  # Format 2: Array of runner labels for selection with fallbacks. GitHub Actions
  # will use the first available runner that matches any label in the array. Useful
  # for high-availability setups or when multiple runner types are acceptable.
  runs-on: []
    # Array items: string

  # Format 3: Runner group configuration for GitHub-hosted runners. Use this form to
  # target specific runner groups (e.g., larger runners with more CPU/memory) or
  # self-hosted runner pools with specific label requirements. Agentic workflows may
  # benefit from larger runners for complex AI processing tasks.
  runs-on:
    # Runner group name for self-hosted runners or GitHub-hosted runner groups
    # (optional)
    group: "example-value"

    # List of runner labels for self-hosted runners or GitHub-hosted runner selection
    # (optional)
    labels: []
      # Array of strings
---
```

## Additional Information

- Fields marked with `(optional)` are not required
- Fields with multiple options show all possible formats
- See the [Frontmatter guide](/gh-aw/reference/frontmatter/) for detailed explanations and examples
- See individual reference pages for specific topics like [Triggers](/gh-aw/reference/triggers/), [Tools](/gh-aw/reference/tools/), and [Safe Outputs](/gh-aw/reference/safe-outputs/)
