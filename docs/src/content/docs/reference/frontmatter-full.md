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

# Optional source reference indicating where this workflow was added from. Format:
# owner/repo/path@ref (e.g., githubnext/agentics/workflows/ci-doctor.md@v1.0.0).
# Rendered as a comment in the generated lock file.
# (optional)
source: "example-value"

# Optional tracker identifier to tag all created assets (issues, discussions,
# comments, pull requests). Must be at least 8 characters and contain only
# alphanumeric characters, hyphens, and underscores. This identifier will be
# inserted in the body/description of all created assets to enable searching and
# retrieving assets associated with this workflow.
# (optional)
tracker-id: "example-value"

# Optional array of labels to categorize and organize workflows. Labels can be
# used to filter workflows in status/list commands.
# (optional)
labels: []
  # Array of strings

# Optional metadata field for storing custom key-value pairs compatible with the
# custom agent spec. Key names are limited to 64 characters, and values are
# limited to 1024 characters.
# (optional)
metadata:
  {}

# Optional array of workflow specifications to import (similar to @include
# directives but defined in frontmatter). Format: owner/repo/path@ref (e.g.,
# githubnext/agentics/workflows/shared/common.md@v1.0.0). Can be strings or
# objects with path and inputs. Any markdown files under .github/agents directory
# are treated as custom agent files and only one agent file is allowed per
# workflow.
# (optional)
imports: []

# Workflow triggers that define when the agentic workflow should run. Supports
# standard GitHub Actions trigger events plus special command triggers for
# /commands (required)
# This field supports multiple formats (oneOf):

# Option 1: Simple trigger event name (e.g., 'push', 'issues', 'pull_request',
# 'discussion', 'schedule', 'fork', 'create', 'delete', 'public', 'watch',
# 'workflow_call'), schedule shorthand (e.g., 'daily', 'weekly'), or slash command
# shorthand (e.g., '/my-bot' expands to slash_command + workflow_dispatch)
on: "example-value"

# Option 2: Complex trigger configuration with event-specific filters and options
on:
  # Special slash command trigger for /command workflows (e.g., '/my-bot' in issue
  # comments). Creates conditions to match slash commands automatically. Note: Can
  # be combined with issues/pull_request events if those events only use 'labeled'
  # or 'unlabeled' types.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Null command configuration - defaults to using the workflow filename
  # (without .md extension) as the command name
  slash_command: null

  # Option 2: Command name as a string (shorthand format, e.g., 'customname' for
  # '/customname' triggers). Command names must not start with '/' as the slash is
  # automatically added when matching commands.
  slash_command: "example-value"

  # Option 3: Command configuration object with custom command name
  slash_command:
    # Name of the slash command that triggers the workflow (e.g., '/help',
    # '/analyze'). Used for comment-based workflow activation.
    # (optional)
    # This field supports multiple formats (oneOf):

    # Option 1: Single command name for slash commands (e.g., 'helper-bot' for
    # '/helper-bot' triggers). Command names must not start with '/' as the slash is
    # automatically added when matching commands. Defaults to workflow filename
    # without .md extension if not specified.
    name: "My Workflow"

    # Option 2: Array of command names that trigger this workflow (e.g., ['cmd.add',
    # 'cmd.remove'] for '/cmd.add' and '/cmd.remove' triggers). Each command name must
    # not start with '/'.
    name: []
      # Array items: Command name without leading slash

    # Events where the command should be active. Default is all comment-related events
    # ('*'). Use GitHub Actions event names.
    # (optional)
    # This field supports multiple formats (oneOf):

    # Option 1: Single event name or '*' for all events. Use GitHub Actions event
    # names: 'issues', 'issue_comment', 'pull_request_comment', 'pull_request',
    # 'pull_request_review_comment', 'discussion', 'discussion_comment'.
    events: "*"

    # Option 2: Array of event names where the command should be active (requires at
    # least one). Use GitHub Actions event names.
    events: []
      # Array items: GitHub Actions event name.

  # DEPRECATED: Use 'slash_command' instead. Special command trigger for /command
  # workflows (e.g., '/my-bot' in issue comments). Creates conditions to match slash
  # commands automatically.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Null command configuration - defaults to using the workflow filename
  # (without .md extension) as the command name
  command: null

  # Option 2: Command name as a string (shorthand format, e.g., 'customname' for
  # '/customname' triggers). Command names must not start with '/' as the slash is
  # automatically added when matching commands.
  command: "example-value"

  # Option 3: Command configuration object with custom command name
  command:
    # Name of the slash command that triggers the workflow (e.g., '/deploy', '/test').
    # Used for command-based workflow activation.
    # (optional)
    # This field supports multiple formats (oneOf):

    # Option 1: Custom command name for slash commands (e.g., 'helper-bot' for
    # '/helper-bot' triggers). Command names must not start with '/' as the slash is
    # automatically added when matching commands. Defaults to workflow filename
    # without .md extension if not specified.
    name: "My Workflow"

    # Option 2: Array of command names that trigger this workflow (e.g., ['cmd.add',
    # 'cmd.remove'] for '/cmd.add' and '/cmd.remove' triggers). Each command name must
    # not start with '/'.
    name: []
      # Array items: Command name without leading slash

    # Events where the command should be active. Default is all comment-related events
    # ('*'). Use GitHub Actions event names.
    # (optional)
    # This field supports multiple formats (oneOf):

    # Option 1: Single event name or '*' for all events. Use GitHub Actions event
    # names: 'issues', 'issue_comment', 'pull_request_comment', 'pull_request',
    # 'pull_request_review_comment', 'discussion', 'discussion_comment'.
    events: "*"

    # Option 2: Array of event names where the command should be active (requires at
    # least one). Use GitHub Actions event names.
    events: []
      # Array items: GitHub Actions event name.

  # Push event trigger that runs the workflow when code is pushed to the repository
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: undefined

  # Option 2: undefined

  # Option 3: undefined

  # Pull request event trigger that runs the workflow when pull requests are
  # created, updated, or closed
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: undefined

  # Option 2: undefined

  # Option 3: undefined

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
    # This field supports multiple formats (oneOf):

    # Option 1: Single label name to filter labeled/unlabeled events (e.g., 'bug')
    names: "example-value"

    # Option 2: List of label names to filter labeled/unlabeled events. Only applies
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
  # This field supports multiple formats (oneOf):

  # Option 1: Shorthand schedule string using fuzzy or cron format. Examples:
  # 'daily', 'daily around 14:00', 'daily between 9:00 and 17:00', 'weekly', 'weekly
  # on monday', 'weekly on friday around 5pm', 'hourly', 'every 2h', 'every 10
  # minutes', '0 9 * * 1'. Fuzzy schedules distribute execution times to prevent
  # load spikes. For fixed times, use standard cron syntax. Minimum interval is 5
  # minutes.
  schedule: "example-value"

  # Option 2: Array of schedule objects with cron expressions (standard cron or
  # fuzzy format)
  schedule: []
    # Array items: object

  # Manual workflow dispatch trigger
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Simple workflow dispatch trigger
  workflow_dispatch: null

  # Option 2: object
  workflow_dispatch:
    # Input parameters for manual dispatch
    # (optional)
    inputs:
      {}

  # Workflow run trigger
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: undefined

  # Option 2: undefined

  # Option 3: undefined

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
  # This field supports multiple formats (oneOf):

  # Option 1: Simple create event trigger
  create: null

  # Option 2: object
  create:
    {}

  # Delete event trigger that runs when a Git reference (branch or tag) is deleted
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Simple delete event trigger
  delete: null

  # Option 2: object
  delete:
    {}

  # Deployment event trigger that runs when a deployment is created
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Simple deployment event trigger
  deployment: null

  # Option 2: object
  deployment:
    {}

  # Deployment status event trigger that runs when a deployment status is updated
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Simple deployment status event trigger
  deployment_status: null

  # Option 2: object
  deployment_status:
    {}

  # Fork event trigger that runs when someone forks the repository
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Simple fork event trigger
  fork: null

  # Option 2: object
  fork:
    {}

  # Gollum event trigger that runs when someone creates or updates a Wiki page
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Simple gollum event trigger
  gollum: null

  # Option 2: object
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
  # This field supports multiple formats (oneOf):

  # Option 1: Simple page build event trigger
  page_build: null

  # Option 2: object
  page_build:
    {}

  # Public event trigger that runs when a repository changes from private to public
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Simple public event trigger
  public: null

  # Option 2: object
  public:
    {}

  # Pull request target event trigger that runs in the context of the base
  # repository (secure for fork PRs)
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: undefined

  # Option 2: undefined

  # Option 3: undefined

  # Pull request review event trigger that runs when a pull request review is
  # submitted, edited, or dismissed
  # (optional)
  pull_request_review:
    # Types of pull request review events
    # (optional)
    types: []
      # Array of strings

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
  # This field supports multiple formats (oneOf):

  # Option 1: Simple status event trigger
  status: null

  # Option 2: object
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
  # This field supports multiple formats (oneOf):

  # Option 1: Simple workflow call event trigger
  workflow_call: null

  # Option 2: object
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
  # dates (YYYY-MM-DD HH:MM:SS, June 1 2025, 1st June 2025, 06/01/2025, etc.) or
  # relative time deltas (+25h, +3d, +1d12h30m). Maximum values for time deltas:
  # 12mo, 52w, 365d, 8760h (365 days). Note: Minute unit 'm' is not allowed for
  # stop-after; minimum unit is hours 'h'.
  # (optional)
  stop-after: "example-value"

  # Conditionally skip workflow execution when a GitHub search query has matches.
  # Can be a string (query only, implies max=1) or an object with 'query' and
  # optional 'max' fields.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: GitHub search query string to check before running workflow (implies
  # max=1). If the search returns any results, the workflow will be skipped. Query
  # is automatically scoped to the current repository. Example: 'is:issue is:open
  # label:bug'
  skip-if-match: "example-value"

  # Option 2: Skip-if-match configuration object with query and maximum match count
  skip-if-match:
    # GitHub search query string to check before running workflow. Query is
    # automatically scoped to the current repository.
    query: "example-value"

    # Maximum number of items that must be matched for the workflow to be skipped.
    # Defaults to 1 if not specified.
    # (optional)
    max: 1

  # Conditionally skip workflow execution when a GitHub search query has no matches
  # (or fewer than minimum). Can be a string (query only, implies min=1) or an
  # object with 'query' and optional 'min' fields.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: GitHub search query string to check before running workflow (implies
  # min=1). If the search returns no results, the workflow will be skipped. Query is
  # automatically scoped to the current repository. Example: 'is:pr is:open
  # label:ready-to-deploy'
  skip-if-no-match: "example-value"

  # Option 2: Skip-if-no-match configuration object with query and minimum match
  # count
  skip-if-no-match:
    # GitHub search query string to check before running workflow. Query is
    # automatically scoped to the current repository.
    query: "example-value"

    # Minimum number of items that must be matched for the workflow to proceed.
    # Defaults to 1 if not specified.
    # (optional)
    min: 1

  # Environment name that requires manual approval before the workflow can run. Must
  # match a valid environment configured in the repository settings.
  # (optional)
  manual-approval: "example-value"

  # AI reaction to add/remove on triggering item (one of: +1, -1, laugh, confused,
  # heart, hooray, rocket, eyes, none). Use 'none' to disable reactions. Defaults to
  # 'eyes' if not specified.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: string
  reaction: "+1"

  # Option 2: YAML parses +1 and -1 without quotes as integers. These are converted
  # to +1 and -1 strings respectively.
  reaction: 1

# GitHub token permissions for the workflow. Controls what the GITHUB_TOKEN can
# access during execution. Use the principle of least privilege - only grant the
# minimum permissions needed.
# (optional)
# This field supports multiple formats (oneOf):

# Option 1: Simple permissions string: 'read-all' (all read permissions) or
# 'write-all' (all write permissions)
permissions: "read-all"

# Option 2: Detailed permissions object with granular control over specific GitHub
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

  # Permission level for OIDC token requests (read/write/none). Allows workflows to
  # request JWT tokens for cloud provider authentication.
  # (optional)
  id-token: "read"

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

  # Permission level for security events (read/write/none). Controls access to view
  # and manage code scanning alerts and security findings.
  # (optional)
  security-events: "read"

  # Permission level for commit statuses (read/write/none). Controls access to
  # create and update commit status checks.
  # (optional)
  statuses: "read"

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
# This field supports multiple formats (oneOf):

# Option 1: Simple runner label string. Use for standard GitHub-hosted runners
# (e.g., 'ubuntu-latest', 'windows-latest', 'macos-latest') or self-hosted runner
# labels. Most common form for agentic workflows.
runs-on: "example-value"

# Option 2: Array of runner labels for selection with fallbacks. GitHub Actions
# will use the first available runner that matches any label in the array. Useful
# for high-availability setups or when multiple runner types are acceptable.
runs-on: []
  # Array items: string

# Option 3: Runner group configuration for GitHub-hosted runners. Use this form to
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

# Workflow timeout in minutes (GitHub Actions standard field). Defaults to 20
# minutes for agentic workflows. Has sensible defaults and can typically be
# omitted.
# (optional)
timeout-minutes: 1

# Concurrency control to limit concurrent workflow runs (GitHub Actions standard
# field). Supports two forms: simple string for basic group isolation, or object
# with cancel-in-progress option for advanced control. Agentic workflows enhance
# this with automatic per-engine concurrency policies (defaults to single job per
# engine across all workflows) and token-based rate limiting. Default behavior:
# workflows in the same group queue sequentially unless cancel-in-progress is
# true. See https://docs.github.com/en/actions/using-jobs/using-concurrency
# (optional)
# This field supports multiple formats (oneOf):

# Option 1: Simple concurrency group name to prevent multiple runs in the same
# group. Use expressions like '${{ github.workflow }}' for per-workflow isolation
# or '${{ github.ref }}' for per-branch isolation. Agentic workflows automatically
# generate enhanced concurrency policies using 'gh-aw-{engine-id}' as the default
# group to limit concurrent AI workloads across all workflows using the same
# engine.
concurrency: "example-value"

# Option 2: Concurrency configuration object with group isolation and cancellation
# control. Use object form when you need fine-grained control over whether to
# cancel in-progress runs. For agentic workflows, this is useful to prevent
# multiple AI agents from running simultaneously and consuming excessive resources
# or API quotas.
concurrency:
  # Concurrency group name. Workflows in the same group cannot run simultaneously.
  # Supports GitHub Actions expressions for dynamic group names based on branch,
  # workflow, or other context.
  group: "example-value"

  # Whether to cancel in-progress workflows in the same concurrency group when a new
  # one starts. Default: false (queue new runs). Set to true for agentic workflows
  # where only the latest run matters (e.g., PR analysis that becomes stale when new
  # commits are pushed).
  # (optional)
  cancel-in-progress: true

# Environment variables for the workflow
# (optional)
# This field supports multiple formats (oneOf):

# Option 1: object
env:
  {}

# Option 2: string
env: "example-value"

# Feature flags and configuration options for experimental or optional features in
# the workflow. Each feature can be a boolean flag or a string value. The
# 'action-tag' feature (string) specifies the tag or SHA to use when referencing
# actions/setup in compiled workflows (for testing purposes only).
# (optional)
features:
  {}

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
# This field supports multiple formats (oneOf):

# Option 1: Environment name as a string
environment: "example-value"

# Option 2: Environment object with name and optional URL
environment:
  # The name of the environment configured in the repo
  name: "My Workflow"

  # A deployment URL
  # (optional)
  url: "example-value"

# Container to run the job steps in
# (optional)
# This field supports multiple formats (oneOf):

# Option 1: Docker image name (e.g., 'node:18', 'ubuntu:latest')
container: "example-value"

# Option 2: Container configuration object
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
# subdomain. Controls web fetch and search capabilities.
# (optional)
# This field supports multiple formats (oneOf):

# Option 1: Use default network permissions (basic infrastructure: certificates,
# JSON schema, Ubuntu, etc.)
network: "defaults"

# Option 2: Custom network access configuration with ecosystem identifiers and
# specific domains
network:
  # List of allowed domains or ecosystem identifiers (e.g., 'defaults', 'python',
  # 'node', '*.example.com'). Wildcard patterns match any subdomain AND the base
  # domain.
  # (optional)
  allowed: []
    # Array of Domain name or ecosystem identifier. Supports wildcards like
    # '*.example.com' (matches sub.example.com, deep.nested.example.com, and
    # example.com itself) and ecosystem names like 'python', 'node'.

  # List of blocked domains or ecosystem identifiers (e.g., 'python', 'node',
  # 'tracker.example.com'). Blocked domains take precedence over allowed domains.
  # (optional)
  blocked: []
    # Array of Domain name or ecosystem identifier to block. Supports wildcards like
    # '*.example.com' (matches sub.example.com, deep.nested.example.com, and
    # example.com itself) and ecosystem names like 'python', 'node'.

# Sandbox configuration for AI engines. Controls agent sandbox (AWF or Sandbox
# Runtime) and MCP gateway. The MCP gateway is always enabled and cannot be
# disabled.
# (optional)
# This field supports multiple formats (oneOf):

# Option 1: Legacy string format for sandbox type: 'default' for no sandbox,
# 'sandbox-runtime' or 'srt' for Anthropic Sandbox Runtime, 'awf' for Agent
# Workflow Firewall
sandbox: "default"

# Option 2: Object format for full sandbox configuration with agent and mcp
# options
sandbox:
  # Legacy sandbox type field (use agent instead)
  # (optional)
  type: "default"

  # Agent sandbox type: 'awf' uses AWF (Agent Workflow Firewall), 'srt' uses
  # Anthropic Sandbox Runtime, or false to disable agent sandbox. Defaults to 'awf'
  # if not specified. Note: Disabling the agent sandbox (false) removes firewall
  # protection but keeps the MCP gateway enabled.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Set to false to disable the agent sandbox (firewall). Warning: This
  # removes firewall protection but keeps the MCP gateway enabled. Not allowed in
  # strict mode.
  agent: true

  # Option 2: Sandbox type: 'awf' for Agent Workflow Firewall, 'srt' for Sandbox
  # Runtime
  agent: "awf"

  # Option 3: Custom sandbox runtime configuration
  agent:
    # Agent identifier (replaces 'type' field in new format): 'awf' for Agent Workflow
    # Firewall, 'srt' for Sandbox Runtime
    # (optional)
    id: "awf"

    # Legacy: Sandbox type to use (use 'id' instead)
    # (optional)
    type: "awf"

    # Custom command to replace the default AWF or SRT installation. For AWF: 'docker
    # run my-custom-awf-image'. For SRT: 'docker run my-custom-srt-wrapper'
    # (optional)
    command: "example-value"

    # Additional arguments to append to the command (applies to both AWF and SRT, for
    # standard and custom commands)
    # (optional)
    args: []
      # Array of strings

    # Environment variables to set on the execution step (applies to both AWF and SRT)
    # (optional)
    env:
      {}

    # Container mounts to add when using AWF. Each mount is specified using Docker
    # mount syntax: 'source:destination:mode' where mode can be 'ro' (read-only) or
    # 'rw' (read-write). Example: '/host/path:/container/path:ro'
    # (optional)
    mounts: []
      # Array of Mount specification in format 'source:destination:mode'

    # Custom Sandbox Runtime configuration (only applies when type is 'srt'). Note:
    # Network configuration is controlled by the top-level 'network' field, not here.
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
    # Container image for the MCP gateway executable (required)
    container: "example-value"

    # Optional version/tag for the container image (e.g., 'latest', 'v1.0.0')
    # (optional)
    version: null

    # Optional custom entrypoint for the MCP gateway container. Overrides the
    # container's default entrypoint.
    # (optional)
    entrypoint: "example-value"

    # Arguments for docker run
    # (optional)
    args: []
      # Array of strings

    # Arguments to add after the container image (container entrypoint arguments)
    # (optional)
    entrypointArgs: []
      # Array of strings

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

    # API key for authenticating with the MCP gateway (supports ${{ secrets.* }}
    # syntax)
    # (optional)
    api-key: "example-value"

    # Gateway domain for URL generation (default: 'host.docker.internal' when agent is
    # enabled, 'localhost' when disabled)
    # (optional)
    domain: "localhost"

# ⚠️  EXPERIMENTAL: Plugin configuration for installing plugins before workflow
# execution. Supports array format (list of repos/plugin configs) and object
# format (repos + custom token). Note: Plugin support is experimental and may
# change in future releases.
# (optional)
# This field supports multiple formats (oneOf):

# Option 1: List of plugins to install. Each item can be either a repository slug
# string (e.g., 'org/repo') or an object with id and optional MCP configuration.
plugins: []
  # Array items: undefined

# Option 2: Plugin configuration with custom GitHub token. Repos can be either
# strings or objects with MCP configuration.
plugins:
  # List of plugins to install. Each item can be either a repository slug string or
  # an object with id and optional MCP configuration.
  repos: []

  # Custom GitHub token expression to use for plugin installation. Overrides the
  # default cascading token resolution (GH_AW_PLUGINS_TOKEN -> GH_AW_GITHUB_TOKEN ->
  # GITHUB_TOKEN).
  # (optional)
  github-token: "${{ secrets.GITHUB_TOKEN }}"

# Conditional execution expression
# (optional)
if: "example-value"

# Custom workflow steps
# (optional)
# This field supports multiple formats (oneOf):

# Option 1: object
steps:
  {}

# Option 2: array
steps: []
  # Array items: undefined

# Custom workflow steps to run after AI execution
# (optional)
# This field supports multiple formats (oneOf):

# Option 1: object
post-steps:
  {}

# Option 2: array
post-steps: []
  # Array items: undefined

# AI engine configuration that specifies which AI processor interprets and
# executes the markdown content of the workflow. Defaults to 'copilot'.
# (optional)
# This field supports multiple formats (oneOf):

# Option 1: Simple engine name: 'claude' (default, Claude Code), 'copilot' (GitHub
# Copilot CLI), 'codex' (OpenAI Codex CLI), or 'custom' (user-defined steps)
engine: "claude"

# Option 2: Extended engine configuration object with advanced options for model
# selection, turn limiting, environment variables, and custom steps
engine:
  # AI engine identifier: 'claude' (Claude Code), 'codex' (OpenAI Codex CLI),
  # 'copilot' (GitHub Copilot CLI), or 'custom' (user-defined GitHub Actions steps)
  id: "claude"

  # Optional version of the AI engine action (e.g., 'beta', 'stable', 20). Has
  # sensible defaults and can typically be omitted. Numeric values are automatically
  # converted to strings at runtime.
  # (optional)
  version: null

  # Optional specific LLM model to use (e.g., 'claude-3-5-sonnet-20241022',
  # 'gpt-4'). Has sensible defaults and can typically be omitted.
  # (optional)
  model: "example-value"

  # Maximum number of chat iterations per run. Helps prevent runaway loops and
  # control costs. Has sensible defaults and can typically be omitted. Note: Only
  # supported by the claude engine.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Maximum number of chat iterations per run as an integer value
  max-turns: 1

  # Option 2: Maximum number of chat iterations per run as a string value
  max-turns: "example-value"

  # Agent job concurrency configuration. Defaults to single job per engine across
  # all workflows (group: 'gh-aw-{engine-id}'). Supports full GitHub Actions
  # concurrency syntax.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Simple concurrency group name. Gets converted to GitHub Actions
  # concurrency format with the specified group.
  concurrency: "example-value"

  # Option 2: GitHub Actions concurrency configuration for the agent job. Controls
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

  # Custom user agent string for GitHub MCP server configuration (codex engine only)
  # (optional)
  user-agent: "example-value"

  # Custom executable path for the AI engine CLI. When specified, the workflow will
  # skip the standard installation steps and use this command instead. The command
  # should be the full path to the executable or a command available in PATH.
  # (optional)
  command: "example-value"

  # Custom environment variables to pass to the AI engine, including secret
  # overrides (e.g., OPENAI_API_KEY: ${{ secrets.CUSTOM_KEY }})
  # (optional)
  env:
    {}

  # Custom GitHub Actions steps for 'custom' engine. Define your own deterministic
  # workflow steps instead of using AI processing.
  # (optional)
  steps: []
    # Array items:

  # Custom error patterns for validating agent logs
  # (optional)
  error_patterns: []
    # Array items:
      # Unique identifier for this error pattern
      # (optional)
      id: "example-value"

      # Ecma script regular expression pattern to match log lines
      pattern: "example-value"

      # Capture group index (1-based) that contains the error level. Use 0 to infer from
      # pattern content.
      # (optional)
      level_group: 1

      # Capture group index (1-based) that contains the error message. Use 0 to use the
      # entire match.
      # (optional)
      message_group: 1

      # Human-readable description of what this pattern matches
      # (optional)
      description: "Description of the workflow"

  # Additional TOML configuration text that will be appended to the generated
  # config.toml in the action (codex engine only)
  # (optional)
  config: "example-value"

  # Agent identifier to pass to copilot --agent flag (copilot engine only).
  # Specifies which custom agent to use for the workflow.
  # (optional)
  agent: "example-value"

  # Optional array of command-line arguments to pass to the AI engine CLI. These
  # arguments are injected after all other args but before the prompt.
  # (optional)
  args: []
    # Array of strings

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
  # This field supports multiple formats (oneOf):

  # Option 1: Empty GitHub tool configuration (enables all read-only GitHub API
  # functions)
  github: null

  # Option 2: Boolean to explicitly enable (true) or disable (false) the GitHub MCP
  # server. When set to false, the GitHub MCP server is not mounted.
  github: true

  # Option 3: Simple GitHub tool configuration (enables all GitHub API functions)
  github: "example-value"

  # Option 4: GitHub tools object configuration with restricted function access
  github:
    # List of allowed GitHub API functions (e.g., 'create_issue', 'update_issue',
    # 'add_comment')
    # (optional)
    allowed: []
      # Array of strings

    # MCP server mode: 'local' (Docker-based, default) or 'remote' (hosted at
    # api.githubcopilot.com)
    # (optional)
    mode: "local"

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

    # Optional custom GitHub token (e.g., '${{ secrets.CUSTOM_PAT }}'). For 'remote'
    # type, defaults to GH_AW_GITHUB_TOKEN if not specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # Array of GitHub MCP server toolset names to enable specific groups of GitHub API
    # functionalities
    # (optional)
    toolsets: []
      # Array of Toolset name

    # Volume mounts for the containerized GitHub MCP server (format:
    # 'host:container:mode' where mode is 'ro' for read-only or 'rw' for read-write).
    # Applies to local mode only. Example: '/data:/data:ro'
    # (optional)
    mounts: []
      # Array of Mount specification in format 'host:container:mode'

    # GitHub App configuration for token minting. When configured, a GitHub App
    # installation access token is minted at workflow start and used instead of the
    # default token. This token overrides any custom github-token setting and provides
    # fine-grained permissions matching the agent job requirements.
    # (optional)
    app:
      # GitHub App ID (e.g., '${{ vars.APP_ID }}'). Required to mint a GitHub App token.
      app-id: "example-value"

      # GitHub App private key (e.g., '${{ secrets.APP_PRIVATE_KEY }}'). Required to
      # mint a GitHub App token.
      private-key: "example-value"

      # Optional owner of the GitHub App installation (defaults to current repository
      # owner if not specified)
      # (optional)
      owner: "example-value"

      # Optional list of repositories to grant access to (defaults to current repository
      # if not specified)
      # (optional)
      repositories: []
        # Array of strings

  # Bash shell command execution tool. Supports wildcards: '*' (all commands),
  # 'command *' (command with any args, e.g., 'date *', 'echo *'). Default safe
  # commands: echo, ls, pwd, cat, head, tail, grep, wc, sort, uniq, date.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Enable bash tool with all shell commands allowed (security
  # consideration: use restricted list in production)
  bash: null

  # Option 2: Enable bash tool - true allows all commands (equivalent to ['*']),
  # false disables the tool
  bash: true

  # Option 3: List of allowed commands and patterns. Wildcards: '*' allows all
  # commands, 'command *' allows command with any args (e.g., 'date *', 'echo *').
  bash: []
    # Array items: Command or pattern: 'echo' (exact match), 'echo *' (command with
    # any args)

  # Web content fetching tool for downloading web pages and API responses (subject
  # to network permissions)
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Enable web fetch tool with default configuration
  web-fetch: null

  # Option 2: Web fetch tool configuration object
  web-fetch:
    {}

  # Web search tool for performing internet searches and retrieving search results
  # (subject to network permissions)
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Enable web search tool with default configuration
  web-search: null

  # Option 2: Web search tool configuration object
  web-search:
    {}

  # File editing tool for reading, creating, and modifying files in the repository
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Enable edit tool
  edit: null

  # Option 2: Edit tool configuration object
  edit:
    {}

  # Playwright browser automation tool for web scraping, testing, and UI
  # interactions in containerized browsers
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Enable Playwright tool with default settings (localhost access only
  # for security)
  playwright: null

  # Option 2: Playwright tool configuration with custom version and domain
  # restrictions
  playwright:
    # Optional Playwright container version (e.g., 'v1.41.0', 1.41, 20). Numeric
    # values are automatically converted to strings at runtime.
    # (optional)
    version: null

    # Domains allowed for Playwright browser network access. Defaults to localhost
    # only for security.
    # (optional)
    # This field supports multiple formats (oneOf):

    # Option 1: List of allowed domains or patterns (e.g., ['github.com',
    # '*.example.com'])
    allowed_domains: []
      # Array items: string

    # Option 2: Single allowed domain (e.g., 'github.com')
    allowed_domains: "example-value"

    # Optional additional arguments to append to the generated MCP server command
    # (optional)
    args: []
      # Array of strings

  # GitHub Agentic Workflows MCP server for workflow introspection and analysis.
  # Provides tools for checking status, compiling workflows, downloading logs, and
  # auditing runs.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Enable agentic-workflows tool with default settings
  agentic-workflows: true

  # Option 2: Enable agentic-workflows tool with default settings (same as true)
  agentic-workflows: null

  # Cache memory MCP configuration for persistent memory storage
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Enable cache-memory with default settings
  cache-memory: true

  # Option 2: Enable cache-memory with default settings (same as true)
  cache-memory: null

  # Option 3: Cache-memory configuration object
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

  # Option 4: Array of cache-memory configurations for multiple caches
  cache-memory: []
    # Array items: object

  # Timeout in seconds for tool/MCP server operations. Applies to all tools and MCP
  # servers if supported by the engine. Default varies by engine (Claude: 60s,
  # Codex: 120s).
  # (optional)
  timeout: 1

  # Timeout in seconds for MCP server startup. Applies to MCP server initialization
  # if supported by the engine. Default: 120 seconds.
  # (optional)
  startup-timeout: 1

  # Serena MCP server for AI-powered code intelligence with language service
  # integration
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Enable Serena with default settings
  serena: null

  # Option 2: Short syntax: array of language identifiers to enable (e.g., ["go",
  # "typescript"])
  serena: []
    # Array items: string

  # Option 3: Serena configuration with custom version and language-specific
  # settings
  serena:
    # Optional Serena MCP version. Numeric values are automatically converted to
    # strings at runtime.
    # (optional)
    version: null

    # Serena execution mode: 'docker' (default, runs in container) or 'local' (runs
    # locally with uvx and HTTP transport)
    # (optional)
    mode: "docker"

    # Optional additional arguments to append to the generated MCP server command
    # (optional)
    args: []
      # Array of strings

    # Language-specific configuration for Serena language services
    # (optional)
    languages:
      # Configuration for Go language support in Serena code analysis. Enables
      # Go-specific parsing, linting, and security checks.
      # (optional)
      # This field supports multiple formats (oneOf):

      # Option 1: Enable Go language service with default version
      go: null

      # Option 2: object
      go:
        # Go version (e.g., "1.21", 1.21)
        # (optional)
        version: null

        # Path to go.mod file for Go version detection (e.g., "go.mod", "backend/go.mod")
        # (optional)
        go-mod-file: "example-value"

        # Version of gopls to install (e.g., "latest", "v0.14.2")
        # (optional)
        gopls-version: "example-value"

      # Configuration for TypeScript language support in Serena code analysis. Enables
      # TypeScript-specific parsing, linting, and type checking.
      # (optional)
      # This field supports multiple formats (oneOf):

      # Option 1: Enable TypeScript language service with default version
      typescript: null

      # Option 2: object
      typescript:
        # Node.js version for TypeScript (e.g., "22", 22)
        # (optional)
        version: null

      # Configuration for Python language support in Serena code analysis. Enables
      # Python-specific parsing, linting, and security checks.
      # (optional)
      # This field supports multiple formats (oneOf):

      # Option 1: Enable Python language service with default version
      python: null

      # Option 2: object
      python:
        # Python version (e.g., "3.12", 3.12)
        # (optional)
        version: null

      # Configuration for Java language support in Serena code analysis. Enables
      # Java-specific parsing, linting, and security checks.
      # (optional)
      # This field supports multiple formats (oneOf):

      # Option 1: Enable Java language service with default version
      java: null

      # Option 2: object
      java:
        # Java version (e.g., "21", 21)
        # (optional)
        version: null

      # Configuration for Rust language support in Serena code analysis. Enables
      # Rust-specific parsing, linting, and security checks.
      # (optional)
      # This field supports multiple formats (oneOf):

      # Option 1: Enable Rust language service with default version
      rust: null

      # Option 2: object
      rust:
        # Rust version (e.g., "stable", "1.75")
        # (optional)
        version: null

      # Configuration for C# language support in Serena code analysis. Enables
      # C#-specific parsing, linting, and security checks.
      # (optional)
      # This field supports multiple formats (oneOf):

      # Option 1: Enable C# language service with default version
      csharp: null

      # Option 2: object
      csharp:
        # .NET version for C# (e.g., "8.0", 8.0)
        # (optional)
        version: null

  # Repo memory configuration for git-based persistent storage
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Enable repo-memory with default settings
  repo-memory: true

  # Option 2: Enable repo-memory with default settings (same as true)
  repo-memory: null

  # Option 3: Repo-memory configuration object
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
    # This field supports multiple formats (oneOf):

    # Option 1: Single file glob pattern for allowed files
    file-glob: "example-value"

    # Option 2: Array of file glob patterns for allowed files
    file-glob: []
      # Array items: string

    # Maximum size per file in bytes (default: 10240 = 10KB)
    # (optional)
    max-file-size: 1

    # Maximum file count per commit (default: 100)
    # (optional)
    max-file-count: 1

    # Optional description for the memory that will be shown in the agent prompt
    # (optional)
    description: "Description of the workflow"

    # Create orphaned branch if it doesn't exist (default: true)
    # (optional)
    create-orphan: true

  # Option 4: Array of repo-memory configurations for multiple memory locations
  repo-memory: []
    # Array items: object

# Command name for the workflow
# (optional)
command: "example-value"

# Cache configuration for workflow (uses actions/cache syntax)
# (optional)
# This field supports multiple formats (oneOf):

# Option 1: Single cache configuration
cache:
  # An explicit key for restoring and saving the cache
  key: "example-value"

  # File path or directory to cache for faster workflow execution. Can be a single
  # path or an array of paths to cache multiple locations.
  # This field supports multiple formats (oneOf):

  # Option 1: A single path to cache
  path: "example-value"

  # Option 2: Multiple paths to cache
  path: []
    # Array items: string

  # Optional list of fallback cache key patterns to use if exact cache key is not
  # found. Enables partial cache restoration for better performance.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: A single restore key
  restore-keys: "example-value"

  # Option 2: Multiple restore keys
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

# Option 2: Multiple cache configurations
cache: []
  # Array items: object

# Safe output processing configuration that automatically creates GitHub issues,
# comments, and pull requests from AI workflow output without requiring write
# permissions in the main job
# (optional)
safe-outputs:
  # List of allowed domains for URI filtering in AI workflow output. URLs from other
  # domains will be replaced with '(redacted)' for security.
  # (optional)
  allowed-domains: []
    # Array of strings

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
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for automatically creating GitHub issues from AI
  # workflow output. The main job does not need 'issues: write' permission.
  create-issue:
    # Optional prefix to add to the beginning of the issue title (e.g., '[ai] ' or
    # '[analysis] ')
    # (optional)
    title-prefix: "example-value"

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

    # GitHub usernames to assign the created issue to. Can be a single username string
    # or array of usernames. Use 'copilot' to assign to GitHub Copilot.
    # (optional)
    # This field supports multiple formats (oneOf):

    # Option 1: Single GitHub username to assign the created issue to (e.g., 'user1'
    # or 'copilot'). Use 'copilot' to assign to GitHub Copilot using the @copilot
    # special value.
    assignees: "example-value"

    # Option 2: List of GitHub usernames to assign the created issue to (e.g.,
    # ['user1', 'user2', 'copilot']). Use 'copilot' to assign to GitHub Copilot using
    # the @copilot special value.
    assignees: []
      # Array items: string

    # Maximum number of issues to create (default: 1)
    # (optional)
    max: 1

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
    # This field supports multiple formats (oneOf):

    # Option 1: Number of days until expires
    expires: 1

    # Option 2: Relative time (e.g., '2h', '7d', '2w', '1m', '1y'); minimum 2h for
    # hour values
    expires: "example-value"

    # Option 3: Set to false to explicitly disable expiration
    expires: true

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

    # Controls whether AI-generated footer is added to the issue. When false, the
    # visible footer content is omitted but XML markers (workflow-id, tracker-id,
    # metadata) are still included for searchability. Defaults to true.
    # (optional)
    footer: true

  # Option 2: Enable issue creation with default configuration
  create-issue: null

  # Enable creation of GitHub Copilot agent tasks from workflow output. Allows
  # workflows to spawn new agent sessions for follow-up work.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: DEPRECATED: Use 'create-agent-session' instead. Configuration for
  # creating GitHub Copilot agent sessions from agentic workflow output using gh
  # agent-task CLI. The main job does not need write permissions.
  create-agent-task:
    # Base branch for the agent session pull request. Defaults to the current branch
    # or repository default branch.
    # (optional)
    base: "example-value"

    # Maximum number of agent sessions to create (default: 1)
    # (optional)
    max: 1

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

  # Option 2: Enable agent session creation with default configuration
  create-agent-task: null

  # Enable creation of GitHub Copilot agent sessions from workflow output. Allows
  # workflows to start interactive agent conversations.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for creating GitHub Copilot agent sessions from agentic
  # workflow output using gh agent-task CLI. The main job does not need write
  # permissions.
  create-agent-session:
    # Base branch for the agent session pull request. Defaults to the current branch
    # or repository default branch.
    # (optional)
    base: "example-value"

    # Maximum number of agent sessions to create (default: 1)
    # (optional)
    max: 1

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

  # Option 2: Enable agent session creation with default configuration
  create-agent-session: null

  # Enable AI agents to add items to GitHub Projects, update custom fields, and
  # manage project structure. Use this for organizing work into projects with status
  # tracking, priority management, and custom metadata.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for managing GitHub Projects boards. Enable agents to
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
    # may add a project item, or update its fields.
    # (optional)
    max: 1

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # Target project URL for update-project operations. This is required in the
    # configuration for documentation purposes. Agent messages MUST explicitly include
    # the project field in their output - the configured value is not used as a
    # fallback. Must be a valid GitHub Projects v2 URL.
    project: "example-value"

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

  # Option 2: Enable project management with default configuration (max=10)
  update-project: null

  # Enable AI agents to create new GitHub Projects for organizing and tracking work
  # across issues and pull requests.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for creating new GitHub Projects boards. Enables agents
  # to create new project boards with optional custom fields, views, and an initial
  # item. Requires a Personal Access Token (PAT) or GitHub App token with Projects
  # write permission (default GITHUB_TOKEN cannot be used). Agent output includes:
  # title (project name), owner (org/user login, uses default if omitted),
  # owner_type ('org' or 'user'), optional item_url (issue to add as first item),
  # and optional field_definitions. Returns a temporary project ID for use in
  # subsequent update_project operations.
  create-project:
    # Maximum number of create operations to perform (default: 1).
    # (optional)
    max: 1

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

  # Option 2: Enable project creation with default configuration (max=1)
  create-project: null

  # Option 3: Alternative null value syntax

  # Enable AI agents to post status updates to GitHub Projects for progress tracking
  # and stakeholder communication.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for posting status updates to GitHub Projects. Status
  # updates provide stakeholder communication about project progress, health, and
  # timeline. Each update appears in the project's Updates tab and creates a
  # historical record. Requires a Personal Access Token (PAT) or GitHub App token
  # with Projects read & write permission (default GITHUB_TOKEN cannot be used).
  # Typically used by scheduled workflows or orchestrators to post regular progress
  # summaries with status indicators (on-track, at-risk, off-track, complete,
  # inactive), dates, and progress details.
  create-project-status-update:
    # Maximum number of status updates to create (default: 1). Typically 1 per
    # orchestrator run.
    # (optional)
    max: 1

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified. Must have Projects: Read+Write permission.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # Target project URL for status update operations. This is required in the
    # configuration for documentation purposes. Agent messages MUST explicitly include
    # the project field in their output - the configured value is not used as a
    # fallback. Must be a valid GitHub Projects v2 URL.
    project: "example-value"

  # Option 2: Enable project status updates with default configuration (max=1)
  create-project-status-update: null

  # Enable AI agents to create GitHub Discussions from workflow output. Supports
  # categorization, labeling, and automatic closure of older discussions. Does not
  # require 'discussions: write' permission.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for creating GitHub discussions from agentic workflow
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

    # Maximum number of discussions to create (default: 1)
    # (optional)
    max: 1

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
    # This field supports multiple formats (oneOf):

    # Option 1: Number of days until expires
    expires: 1

    # Option 2: Relative time (e.g., '2h', '7d', '2w', '1m', '1y'); minimum 2h for
    # hour values
    expires: "example-value"

    # Option 3: Set to false to explicitly disable expiration
    expires: true

  # Option 2: Enable discussion creation with default configuration
  create-discussion: null

  # Enable AI agents to close GitHub Discussions based on workflow analysis or
  # conditions.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for closing GitHub discussions with comment and
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

    # Maximum number of discussions to close (default: 1)
    # (optional)
    max: 1

    # Target repository in format 'owner/repo' for cross-repository operations. Takes
    # precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

  # Option 2: Enable discussion closing with default configuration
  close-discussion: null

  # Enable AI agents to edit and update existing GitHub Discussion content, titles,
  # and metadata.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for updating GitHub discussions from agentic workflow
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

    # Maximum number of discussions to update (default: 1)
    # (optional)
    max: 1

    # Target repository in format 'owner/repo' for cross-repository discussion
    # updates. Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # Controls whether AI-generated footer is added when updating the discussion body.
    # When false, the visible footer content is omitted. Defaults to true. Only
    # applies when 'body' is enabled.
    # (optional)
    footer: true

  # Option 2: Enable discussion updating with default configuration
  update-discussion: null

  # Enable AI agents to close GitHub issues based on workflow analysis, resolution
  # detection, or automated triage.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for closing GitHub issues with comment from agentic
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

    # Maximum number of issues to close (default: 1)
    # (optional)
    max: 1

    # Target repository in format 'owner/repo' for cross-repository operations. Takes
    # precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

  # Option 2: Enable issue closing with default configuration
  close-issue: null

  # Enable AI agents to close pull requests based on workflow analysis or automated
  # review decisions.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for closing GitHub pull requests without merging, with
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

    # Maximum number of pull requests to close (default: 1)
    # (optional)
    max: 1

    # Target repository in format 'owner/repo' for cross-repository operations. Takes
    # precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

  # Option 2: Enable pull request closing with default configuration
  close-pull-request: null

  # Enable AI agents to mark draft pull requests as ready for review when criteria
  # are met.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for marking draft pull requests as ready for review,
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

    # Maximum number of pull requests to mark as ready (default: 1)
    # (optional)
    max: 1

    # Target repository in format 'owner/repo' for cross-repository operations. Takes
    # precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

  # Option 2: Enable marking pull requests as ready for review with default
  # configuration
  mark-pull-request-as-ready-for-review: null

  # Enable AI agents to add comments to GitHub issues, pull requests, or
  # discussions. Supports templating, cross-repository commenting, and automatic
  # mentions.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for automatically creating GitHub issue or pull request
  # comments from AI workflow output. The main job does not need write permissions.
  add-comment:
    # Maximum number of comments to create (default: 1)
    # (optional)
    max: 1

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
    # (current or target-repo) is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # When true, minimizes/hides all previous comments from the same agentic workflow
    # (identified by tracker-id) before creating the new comment. Default: false.
    # (optional)
    hide-older-comments: true

    # List of allowed reasons for hiding older comments when hide-older-comments is
    # enabled. Default: all reasons allowed (spam, abuse, off_topic, outdated,
    # resolved).
    # (optional)
    allowed-reasons: []
      # Array of strings

  # Option 2: Enable issue comment creation with default configuration
  add-comment: null

  # Enable AI agents to create GitHub pull requests from workflow-generated code
  # changes, patches, or analysis results.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for creating GitHub pull requests from agentic workflow
  # output. Note: The max parameter is not supported for pull requests - workflows
  # are always limited to creating 1 pull request per run. This design decision
  # prevents workflow runs from creating excessive PRs and maintains repository
  # integrity.
  create-pull-request:
    # Optional prefix for the pull request title
    # (optional)
    title-prefix: "example-value"

    # Optional list of labels to attach to the pull request
    # (optional)
    labels: []
      # Array of strings

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
    # This field supports multiple formats (oneOf):

    # Option 1: Single reviewer username to assign to the pull request. Use 'copilot'
    # to request a code review from GitHub Copilot using the
    # copilot-pull-request-reviewer[bot].
    reviewers: "example-value"

    # Option 2: List of reviewer usernames to assign to the pull request. Use
    # 'copilot' to request a code review from GitHub Copilot using the
    # copilot-pull-request-reviewer[bot].
    reviewers: []
      # Array items: string

    # Whether to create pull request as draft (defaults to true)
    # (optional)
    draft: true

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
    # creation. Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of additional repositories in format 'owner/repo' that pull requests can be
    # created in. When specified, the agent can use a 'repo' field in the output to
    # specify which repository to create the pull request in. The target repository
    # (current or target-repo) is always implicitly allowed.
    # (optional)
    allowed-repos: []
      # Array of strings

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # Time until the pull request expires and should be automatically closed (only for
    # same-repo PRs without target-repo). Supports integer (days) or relative time
    # format. Minimum duration: 2 hours.
    # (optional)
    # This field supports multiple formats (oneOf):

    # Option 1: Number of days until expires
    expires: 1

    # Option 2: Relative time (e.g., '2h', '7d', '2w', '1m', '1y'); minimum 2h for
    # hour values
    expires: "example-value"

    # Enable auto-merge for the pull request. When enabled, the PR will be
    # automatically merged once all required checks pass and required approvals are
    # met. Defaults to false.
    # (optional)
    auto-merge: true

    # Base branch for the pull request. Defaults to the workflow's branch
    # (github.ref_name) if not specified. Useful for cross-repository PRs targeting
    # non-default branches (e.g., 'vnext', 'release/v1.0').
    # (optional)
    base-branch: "example-value"

    # Controls whether AI-generated footer is added to the pull request. When false,
    # the visible footer content is omitted but XML markers (workflow-id, tracker-id,
    # metadata) are still included for searchability. Defaults to true.
    # (optional)
    footer: true

  # Option 2: Enable pull request creation with default configuration
  create-pull-request: null

  # Enable AI agents to add review comments to specific lines in pull request diffs
  # during code review workflows.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for creating GitHub pull request review comments from
  # agentic workflow output
  create-pull-request-review-comment:
    # Maximum number of review comments to create (default: 10)
    # (optional)
    max: 1

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

  # Option 2: Enable PR review comment creation with default configuration
  create-pull-request-review-comment: null

  # Enable AI agents to create GitHub Advanced Security code scanning alerts for
  # detected vulnerabilities or security issues.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for creating repository security advisories (SARIF
  # format) from agentic workflow output
  create-code-scanning-alert:
    # Maximum number of security findings to include (default: unlimited)
    # (optional)
    max: 1

    # Driver name for SARIF tool.driver.name field (default: 'GitHub Agentic Workflows
    # Security Scanner')
    # (optional)
    driver: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

  # Option 2: Enable code scanning alert creation with default configuration
  # (unlimited findings)
  create-code-scanning-alert: null

  # Enable AI agents to create autofixes for code scanning alerts using the GitHub
  # REST API.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for creating autofixes for code scanning alerts
  autofix-code-scanning-alert:
    # Maximum number of autofixes to create (default: 10)
    # (optional)
    max: 1

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

  # Option 2: Enable code scanning autofix creation with default configuration (max:
  # 10)
  autofix-code-scanning-alert: null

  # Enable AI agents to add labels to GitHub issues or pull requests based on
  # workflow analysis or classification.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Null configuration allows any labels. Labels will be created if they
  # don't already exist in the repository.
  add-labels: null

  # Option 2: Configuration for adding labels to issues/PRs from agentic workflow
  # output. Labels will be created if they don't already exist in the repository.
  add-labels:
    # Optional list of allowed labels that can be added. Labels will be created if
    # they don't already exist in the repository. If omitted, any labels are allowed
    # (including creating new ones).
    # (optional)
    allowed: []
      # Array of strings

    # Optional maximum number of labels to add (default: 3)
    # (optional)
    max: 1

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

  # Enable AI agents to remove labels from GitHub issues or pull requests.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Null configuration allows any labels to be removed.
  remove-labels: null

  # Option 2: Configuration for removing labels from issues/PRs from agentic
  # workflow output.
  remove-labels:
    # Optional list of allowed labels that can be removed. If omitted, any labels can
    # be removed.
    # (optional)
    allowed: []
      # Array of strings

    # Optional maximum number of labels to remove (default: 3)
    # (optional)
    max: 1

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

  # Enable AI agents to request reviews from users or teams on pull requests based
  # on code changes or expertise matching.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Null configuration allows any reviewers
  add-reviewer: null

  # Option 2: Configuration for adding reviewers to pull requests from agentic
  # workflow output
  add-reviewer:
    # Optional list of allowed reviewers. If omitted, any reviewers are allowed.
    # (optional)
    reviewers: []
      # Array of strings

    # Optional maximum number of reviewers to add (default: 3)
    # (optional)
    max: 1

    # Target for reviewers: 'triggering' (default), '*' (any PR), or explicit PR
    # number
    # (optional)
    target: "example-value"

    # Target repository in format 'owner/repo' for cross-repository reviewer addition.
    # Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

  # Enable AI agents to assign GitHub milestones to issues or pull requests based on
  # workflow analysis or project planning.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Null configuration allows assigning any milestones
  assign-milestone: null

  # Option 2: Configuration for assigning issues to milestones from agentic workflow
  # output
  assign-milestone:
    # Optional list of allowed milestone titles that can be assigned. If omitted, any
    # milestones are allowed.
    # (optional)
    allowed: []
      # Array of strings

    # Optional maximum number of milestone assignments (default: 1)
    # (optional)
    max: 1

    # Target repository in format 'owner/repo' for cross-repository milestone
    # assignment. Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

  # Enable AI agents to assign issues or pull requests to GitHub Copilot (@copilot)
  # for automated handling.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Null configuration uses default agent (copilot)
  assign-to-agent: null

  # Option 2: Configuration for assigning GitHub Copilot agents to issues from
  # agentic workflow output
  assign-to-agent:
    # Default agent name to assign (default: 'copilot')
    # (optional)
    name: "My Workflow"

    # Optional list of allowed agent names. If specified, only these agents can be
    # assigned. When configured, existing agent assignees not in the list are removed
    # while regular user assignees are preserved.
    # (optional)
    allowed: []
      # Array of strings

    # Optional maximum number of agent assignments (default: 1)
    # (optional)
    max: 1

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

    # If true, the workflow continues gracefully when agent assignment fails (e.g.,
    # due to missing token or insufficient permissions), logging a warning instead of
    # failing. Default is false. Useful for workflows that should not fail when agent
    # assignment is optional.
    # (optional)
    ignore-if-error: true

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

  # Enable AI agents to assign issues or pull requests to specific GitHub users
  # based on workflow logic or expertise matching.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Enable user assignment with default configuration
  assign-to-user: null

  # Option 2: Configuration for assigning users to issues from agentic workflow
  # output
  assign-to-user:
    # Optional list of allowed usernames. If specified, only these users can be
    # assigned.
    # (optional)
    allowed: []
      # Array of strings

    # Optional maximum number of user assignments (default: 1)
    # (optional)
    max: 1

    # Target issue to assign users to. Use 'triggering' (default) for the triggering
    # issue, '*' to allow any issue, or a specific issue number.
    # (optional)
    target: null

    # Target repository in format 'owner/repo' for cross-repository user assignment.
    # Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

  # Enable AI agents to create hierarchical relationships between issues using
  # GitHub's sub-issue (tasklist) feature.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Enable sub-issue linking with default configuration
  link-sub-issue: null

  # Option 2: Configuration for linking issues as sub-issues from agentic workflow
  # output
  link-sub-issue:
    # Maximum number of sub-issue links to create (default: 5)
    # (optional)
    max: 1

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

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

  # Enable AI agents to edit and update existing GitHub issue content, titles,
  # labels, assignees, and metadata.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for updating GitHub issues from agentic workflow output
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

    # Maximum number of issues to update (default: 1)
    # (optional)
    max: 1

    # Target repository in format 'owner/repo' for cross-repository issue updates.
    # Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

  # Option 2: Enable issue updating with default configuration
  update-issue: null

  # Enable AI agents to edit and update existing pull request content, titles,
  # labels, reviewers, and metadata.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for updating GitHub pull requests from agentic workflow
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

    # Default operation for body updates: 'append' (add to end), 'prepend' (add to
    # start), or 'replace' (overwrite completely). Defaults to 'replace' if not
    # specified.
    # (optional)
    operation: "append"

    # Maximum number of pull requests to update (default: 1)
    # (optional)
    max: 1

    # Target repository in format 'owner/repo' for cross-repository pull request
    # updates. Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

  # Option 2: Enable pull request updating with default configuration (title and
  # body updates enabled)
  update-pull-request: null

  # Enable AI agents to push commits directly to pull request branches for automated
  # fixes or improvements.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Use default configuration (branch: 'triggering', if-no-changes:
  # 'warn')
  push-to-pull-request-branch: null

  # Option 2: Configuration for pushing changes to a specific branch from agentic
  # workflow output
  push-to-pull-request-branch:
    # The branch to push changes to (defaults to 'triggering')
    # (optional)
    branch: "example-value"

    # Target for push operations: 'triggering' (default), '*' (any pull request), or
    # explicit pull request number
    # (optional)
    target: "example-value"

    # Required prefix for pull request title. Only pull requests with this prefix will
    # be accepted.
    # (optional)
    title-prefix: "example-value"

    # Required labels for pull request validation. Only pull requests with all these
    # labels will be accepted.
    # (optional)
    labels: []
      # Array of strings

    # Behavior when no changes to push: 'warn' (default - log warning but succeed),
    # 'error' (fail the action), or 'ignore' (silent success)
    # (optional)
    if-no-changes: "warn"

    # Optional suffix to append to generated commit titles (e.g., ' [skip ci]' to
    # prevent triggering CI on the commit)
    # (optional)
    commit-title-suffix: "example-value"

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

  # Enable AI agents to minimize (hide) comments on issues or pull requests based on
  # relevance, spam detection, or moderation rules.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Enable comment hiding with default configuration
  hide-comment: null

  # Option 2: Configuration for hiding comments on GitHub issues, pull requests, or
  # discussions from agentic workflow output
  hide-comment:
    # Maximum number of comments to hide (default: 5)
    # (optional)
    max: 1

    # Target repository in format 'owner/repo' for cross-repository comment hiding.
    # Takes precedence over trial target repo settings.
    # (optional)
    target-repo: "example-value"

    # List of allowed reasons for hiding comments. Default: all reasons allowed (spam,
    # abuse, off_topic, outdated, resolved).
    # (optional)
    allowed-reasons: []
      # Array of strings

  # Dispatch workflow_dispatch events to other workflows. Used by orchestrators to
  # delegate work to worker workflows with controlled maximum dispatch count.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for dispatching workflow_dispatch events to other
  # workflows. Orchestrators use this to delegate work to worker workflows.
  dispatch-workflow:
    # List of workflow names (without .md extension) to allow dispatching. Each
    # workflow must exist in .github/workflows/.
    workflows: []
      # Array of strings

    # Maximum number of workflow dispatch operations per run (default: 1, max: 50)
    # (optional)
    max: 1

    # GitHub token to use for dispatching workflows. Overrides global github-token if
    # specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

  # Option 2: Shorthand array format: list of workflow names (without .md extension)
  # to allow dispatching
  dispatch-workflow: []
    # Array items: string

  # Enable AI agents to report when required MCP tools are unavailable. Used for
  # workflow diagnostics and tool discovery.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for reporting missing tools from agentic workflow output
  missing-tool:
    # Maximum number of missing tool reports (default: unlimited)
    # (optional)
    max: 1

    # Whether to create or update GitHub issues when tools are missing (default: true)
    # (optional)
    create-issue: true

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

  # Option 2: Enable missing tool reporting with default configuration
  missing-tool: null

  # Option 3: Explicitly disable missing tool reporting (false). Missing tool
  # reporting is enabled by default when safe-outputs is configured.
  missing-tool: true

  # Enable AI agents to report when required data or context is missing. Used for
  # workflow troubleshooting and data validation.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for reporting missing data required to achieve workflow
  # goals. Encourages AI agents to be truthful about data gaps instead of
  # hallucinating information.
  missing-data:
    # Maximum number of missing data reports (default: unlimited)
    # (optional)
    max: 1

    # Whether to create or update GitHub issues when data is missing (default: true)
    # (optional)
    create-issue: true

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

  # Option 2: Enable missing data reporting with default configuration
  missing-data: null

  # Option 3: Explicitly disable missing data reporting (false). Missing data
  # reporting is enabled by default when safe-outputs is configured.
  missing-data: true

  # Enable AI agents to explicitly indicate no action is needed. Used for workflow
  # control flow and conditional logic.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for no-op safe output (logging only, no GitHub API
  # calls). Always available as a fallback to ensure human-visible artifacts.
  noop:
    # Maximum number of noop messages (default: 1)
    # (optional)
    max: 1

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

    # Controls whether noop runs are reported as issue comments (default: true). Set
    # to false to disable posting to the no-op runs issue.
    # (optional)
    report-as-issue: true

  # Option 2: Enable noop output with default configuration (max: 1)
  noop: null

  # Option 3: Explicitly disable noop output (false). Noop is enabled by default
  # when safe-outputs is configured.
  noop: true

  # Enable AI agents to publish files (images, charts, reports) to an orphaned git
  # branch for persistent storage and web access.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for publishing assets to an orphaned git branch
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

    # Maximum number of assets to upload (default: 10)
    # (optional)
    max: 1

    # GitHub token to use for this specific output type. Overrides global github-token
    # if specified.
    # (optional)
    github-token: "${{ secrets.GITHUB_TOKEN }}"

  # Option 2: Enable asset publishing with default configuration
  upload-asset: null

  # Enable AI agents to edit and update GitHub release content, including release
  # notes, assets, and metadata.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Configuration for updating GitHub release descriptions
  update-release:
    # Maximum number of releases to update (default: 1)
    # (optional)
    max: 1

    # Target repository for cross-repo release updates (format: owner/repo). If not
    # specified, updates releases in the workflow's repository.
    # (optional)
    target-repo: "example-value"

    # Controls whether AI-generated footer is added when updating the release body.
    # When false, the visible footer content is omitted. Defaults to true.
    # (optional)
    footer: true

  # Option 2: Enable release updates with default configuration
  update-release: null

  # If true, emit step summary messages instead of making GitHub API calls (preview
  # mode)
  # (optional)
  staged: true

  # Environment variables to pass to safe output jobs
  # (optional)
  env:
    {}

  # GitHub token to use for safe output jobs. Typically a secret reference like ${{
  # secrets.GITHUB_TOKEN }} or ${{ secrets.CUSTOM_PAT }}
  # (optional)
  github-token: "${{ secrets.GITHUB_TOKEN }}"

  # GitHub App credentials for minting installation access tokens. When configured,
  # a token will be generated using the app credentials and used for all safe output
  # operations.
  # (optional)
  app:
    # GitHub App ID. Should reference a variable (e.g., ${{ vars.APP_ID }}).
    app-id: "example-value"

    # GitHub App private key. Should reference a secret (e.g., ${{
    # secrets.APP_PRIVATE_KEY }}).
    private-key: "example-value"

    # Optional: The owner of the GitHub App installation. If empty, defaults to the
    # current repository owner.
    # (optional)
    owner: "example-value"

    # Optional: Comma or newline-separated list of repositories to grant access to. If
    # owner is set and repositories is empty, access will be scoped to all
    # repositories in the provided repository owner's installation. If owner and
    # repositories are empty, access will be scoped to only the current repository.
    # (optional)
    repositories: []
      # Array of strings

  # Maximum allowed size for git patches in kilobytes (KB). Defaults to 1024 KB (1
  # MB). If patch exceeds this size, the job will fail.
  # (optional)
  max-patch-size: 1

  # Enable AI agents to report detected security threats, policy violations, or
  # suspicious patterns for security review.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Enable or disable threat detection for safe outputs (defaults to true
  # when safe-outputs are configured)
  threat-detection: true

  # Option 2: Threat detection configuration object
  threat-detection:
    # Whether threat detection is enabled
    # (optional)
    enabled: true

    # Additional custom prompt instructions to append to threat detection analysis
    # (optional)
    prompt: "example-value"

    # AI engine configuration specifically for threat detection (overrides main
    # workflow engine). Set to false to disable AI-based threat detection. Supports
    # same format as main engine field when not false.
    # (optional)
    # This field supports multiple formats (oneOf):

    # Option 1: Disable AI engine for threat detection (only run custom steps)
    engine: true

    # Option 2: undefined

    # Array of extra job steps to run after detection
    # (optional)
    steps: []

  # Custom safe-output jobs that can be executed based on agentic workflow output.
  # Job names containing dashes will be automatically normalized to underscores
  # (e.g., 'send-notification' becomes 'send_notification').
  # (optional)
  jobs:
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

    # When enabled, workflow completion notifier creates a new comment instead of
    # editing the activation comment. Creates an append-only timeline of workflow
    # runs. Default: false
    # (optional)
    append-only-comments: true

  # Configuration for @mention filtering in safe outputs. Controls whether and how
  # @mentions in AI-generated content are allowed or escaped.
  # (optional)
  # This field supports multiple formats (oneOf):

  # Option 1: Simple boolean mode: false = always escape mentions, true = always
  # allow mentions (error in strict mode)
  mentions: true

  # Option 2: Advanced configuration for @mention filtering with fine-grained
  # control
  mentions:
    # Allow mentions of repository team members (collaborators with any permission
    # level, excluding bots). Default: true
    # (optional)
    allow-team-members: true

    # Allow mentions inferred from event context (issue/PR authors, assignees,
    # commenters). Default: true
    # (optional)
    allow-context: true

    # List of user/bot names always allowed to be mentioned. Bots are not allowed by
    # default unless listed here.
    # (optional)
    allowed: []
      # Array of strings

    # Maximum number of mentions allowed per message. Default: 50
    # (optional)
    max: 1

  # Global footer control for all safe outputs. When false, omits visible
  # AI-generated footer content from all created/updated entities (issues, PRs,
  # discussions, releases) while still including XML markers for searchability.
  # Individual safe-output types (create-issue, update-issue, etc.) can override
  # this by specifying their own footer field. Defaults to true.
  # (optional)
  footer: true

  # Runner specification for all safe-outputs jobs (activation, create-issue,
  # add-comment, etc.). Single runner label (e.g., 'ubuntu-slim', 'ubuntu-latest',
  # 'windows-latest', 'self-hosted'). Defaults to 'ubuntu-slim'. See
  # https://github.blog/changelog/2025-10-28-1-vcpu-linux-runner-now-available-in-github-actions-in-public-preview/
  # (optional)
  runs-on: "example-value"

# Configuration for secret redaction behavior in workflow outputs and artifacts
# (optional)
secret-masking:
  # Additional secret redaction steps to inject after the built-in secret redaction.
  # Use this to mask secrets in generated files using custom patterns.
  # (optional)
  steps: []

# Repository access roles required to trigger agentic workflows. Defaults to
# ['admin', 'maintainer', 'write'] for security. Use 'all' to allow any
# authenticated user (⚠️ security consideration).
# (optional)
# This field supports multiple formats (oneOf):

# Option 1: Allow any authenticated user to trigger the workflow (⚠️ disables
# permission checking entirely - use with caution)
roles: "all"

# Option 2: List of repository permission levels that can trigger the workflow.
# Permission checks are automatically applied to potentially unsafe triggers.
roles: []
  # Array items: Repository permission level: 'admin' (full access),
  # 'maintainer'/'maintain' (repository management), 'write' (push access), 'triage'
  # (issue management)

# Allow list of bot identifiers that can trigger the workflow even if they don't
# meet the required role permissions. When the actor is in this list, the bot must
# be active (installed) on the repository to trigger the workflow.
# (optional)
bots: []
  # Array of Bot identifier/name (e.g., 'dependabot[bot]', 'renovate[bot]',
  # 'github-actions[bot]')

# Rate limiting configuration to restrict how frequently users can trigger the
# workflow. Helps prevent abuse and resource exhaustion from programmatically
# triggered events.
# (optional)
rate-limit:
  # Maximum number of workflow runs allowed per user within the time window.
  # Required field.
  max: 1

  # Time window in minutes for rate limiting. Defaults to 60 (1 hour). Maximum: 180
  # (3 hours).
  # (optional)
  window: 1

  # Optional list of event types to apply rate limiting to. If not specified, rate
  # limiting applies to all programmatically triggered events (e.g.,
  # workflow_dispatch, issue_comment, pull_request_review).
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

# Safe inputs configuration for defining custom lightweight MCP tools as
# JavaScript, shell scripts, or Python scripts. Tools are mounted in an MCP server
# and have access to secrets specified by the user. Only one of 'script'
# (JavaScript), 'run' (shell), or 'py' (Python) must be specified per tool.
# (optional)
safe-inputs:
  {}

# Runtime environment version overrides. Allows customizing runtime versions
# (e.g., Node.js, Python) or defining new runtimes. Runtimes from imported shared
# workflows are also merged.
# (optional)
runtimes:
  {}

# GitHub token expression to use for all steps that require GitHub authentication.
# Typically a secret reference like ${{ secrets.GITHUB_TOKEN }} or ${{
# secrets.CUSTOM_PAT }}. If not specified, defaults to ${{
# secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}. This value can be
# overridden by safe-outputs github-token or individual safe-output github-token
# fields.
# (optional)
github-token: "${{ secrets.GITHUB_TOKEN }}"
---
```

## Additional Information

- Fields marked with `(optional)` are not required
- Fields with multiple options show all possible formats
- See the [Frontmatter guide](/gh-aw/reference/frontmatter/) for detailed explanations and examples
- See individual reference pages for specific topics like [Triggers](/gh-aw/reference/triggers/), [Tools](/gh-aw/reference/tools/), and [Safe Outputs](/gh-aw/reference/safe-outputs/)
