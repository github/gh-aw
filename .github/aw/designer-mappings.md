---
description: Quick reference mappings from user requirements to agentic workflow triggers, outputs, tools, and guardrails.
---

# Designer Decision Heuristics

Quick-reference mapping tables for `.github/aw/designer.md`. Load this file during Phase 2–7 of the interview when translating user answers into workflow syntax.

## Trigger Mapping

| User says... | Maps to |
|---|---|
| "when someone opens a PR" | `on: pull_request:` with `types: [opened]` |
| "when a PR is updated" | `on: pull_request:` with `types: [opened, synchronize]` |
| "every morning", "daily" | fuzzy schedule shorthand `on: schedule: daily on weekdays` (compiler expands to cron) |
| "every Monday", "weekly" | fuzzy schedule shorthand `on: schedule: weekly` (compiler expands to cron) |
| "when I say /review" | `on: slash_command:` with `name: review` (or requested command) |
| "when an issue is labeled bug" | `on: issues:` with `types: [labeled]` and label filter guidance |
| "run when label ai-review is added" | `on: label_command:` with `name`/`names`, optional event scoping, and label-as-command semantics |
| "run on PRs from forks" | `on: pull_request:` plus explicit `forks:` allowlist and fork security guardrails |
| "sometimes automatic, sometimes manual" | semi-active pattern: combine `schedule`/event triggers with `workflow_dispatch` |
| "manually", "on demand" | `on: workflow_dispatch:` |
| "maintain this repository long term" | survey the repository first, then choose a bounded `schedule` plus optional `workflow_dispatch` |
| "when a deployment fails" | `on: deployment_status:` |
| "when another workflow finishes" | `on: workflow_run:` |

## Safe Output Mapping

| User says... | Maps to |
|---|---|
| "post a comment" | `add-comment` |
| "create an issue" | `create-issue` |
| "update issue title/body" | `update-issue` |
| "create a Jira issue" | `jira-create-issue` |
| "update Jira issue ENG-123" | `jira-update-issue` |
| "comment on Jira issue ENG-123" | `jira-add-comment` |
| "add a Jira label" | `jira-add-label` |
| "create a Linear issue" | `linear-create-issue` (experimental) |
| "update Linear issue" | `linear-update-issue` (experimental) |
| "comment on Linear issue" | `linear-add-comment` (experimental) |
| "create an Azure DevOps work item" | `ado-create-work-item` (experimental) |
| "update an Azure DevOps work item" | `ado-update-work-item` (experimental) |
| "comment/assign/link Azure DevOps work items" | `ado-comment-on-work-item`, `ado-assign-work-item`, `ado-link-work-items` (experimental) |
| "close the issue" | `close-issue` |
| "assign someone", "remove assignment" | `assign-to-user`, `unassign-from-user` |
| "set issue type/field/milestone" | `set-issue-type`, `set-issue-field`, `assign-milestone` |
| "open a PR", "submit changes" | `create-pull-request` |
| "update PR description/title" | `update-pull-request` |
| "close the PR", "merge the PR" | `close-pull-request`, `merge-pull-request` |
| "mark PR ready" | `mark-pull-request-as-ready-for-review` |
| "sync PR branch with base" | `update-pull-request` with `update-branch: true` |
| "commit a fix to the PR branch" | `push-to-pull-request-branch` |
| "approve / request changes" | `submit-pull-request-review` |
| "dismiss a PR review" | `dismiss-pull-request-review` |
| "inline review comment", "reply to review thread" | `create-pull-request-review-comment`, `reply-to-pull-request-review-comment`, `resolve-pull-request-review-thread` |
| "start or edit discussion", "close discussion" | `create-discussion`, `update-discussion`, `close-discussion` |
| "request reviewer", "hide comment" | `add-reviewer`, `hide-comment` |
| "create/update project", "project status update" | `create-project`, `update-project`, `create-project-status-update` |
| "update release", "upload release asset" | `update-release`, `upload-asset` |
| "trigger another workflow", "dispatch to workflow", "run another workflow" | `dispatch-workflow` |
| "create/auto-fix code scan alert" | `create-code-scanning-alert`, `autofix-code-scanning-alert` |
| "start an agent session", "assign to an agent" | `create-agent-session`, `assign-to-agent` |
| "store persistent memory comment" | `comment-memory` |
| "store durable repository memory", "persist memory in the repository" | `repo-memory` |
| "link a sub-issue" | `link-sub-issue` |
| "add labels", "remove labels" | `add-labels`, `remove-labels` |
| "replace a label with another" | `replace-label` |
| "log completion message", "signal no action needed" | `noop` (auto-enabled; no declaration required in most workflows) |
| "track when tools are missing", "create issues for missing tools" | `missing-tool` (auto-enabled; configure `create-issue: true` to file tracking issues) |
| "track when data is unavailable", "create issues for missing data" | `missing-data` (auto-enabled; configure `create-issue: true` to file tracking issues) |
| "flag when agent can't finish", "report infrastructure failure" | `report-incomplete` (auto-enabled; configure `create-issue: true` to track failures) |
| "surface analysis on the commit/PR checks UI" | `create-check-run` |
| "upload a file as a run artifact" | `upload-artifact` |
| "nothing visible", "just analyze" | no write safe outputs required (noop is still called automatically) |

## Network Mapping

| User says... | Maps to |
|---|---|
| "calls an external API" | ask for exact FQDN/wildcard, then add to `network.allowed` |
| "reads GitHub data / clones repos" | include `github` in `network.allowed` |
| "uses GitHub Actions artifacts or cache" | include `github-actions` in `network.allowed` |
| "installs npm packages" | include `node` in `network.allowed` |
| "runs pip install" | include `python` in `network.allowed` |
| "builds Go code" | include `go` in `network.allowed` |
| "installs gems / uses Bundler" | include `ruby` in `network.allowed` |
| "runs cargo build" | include `rust` in `network.allowed` |
| "uses NuGet / .NET restore" | include `dotnet` in `network.allowed` |
| "builds with Maven / Gradle" | include `java` in `network.allowed` |
| "uses Docker / pulls container images / pushes to GHCR" | include `containers` in `network.allowed` |
| "runs Playwright browser tests" | include `playwright` in `network.allowed` |
| "runs apt install / yum / apk" | include `linux-distros` in `network.allowed` |
| "uses Terraform / HashiCorp registry" | include `terraform` in `network.allowed` |
| "connects to localhost / loopback / local services" | include `local` in `network.allowed` |
| "no external access" | `network.allowed: [defaults]` (or `[]` if explicitly zero network) |

For less common ecosystems (Swift, PHP, Dart, Haskell, Perl, fonts, Deno, Elixir, Bazel, Clojure, Julia, Kotlin, Lua, node CDNs, OCaml, PowerShell, R, Scala, Zig, dev-tools, Chrome, LaTeX, Lean, python-native) and the full list of **invalid shorthands** (`npm`, `pypi`, `docker`, etc. — see `.github/aw/network.md#invalid-shorthands`), consult `.github/aw/network.md` before generating.

## Tool Mapping

| User says... | Maps to |
|---|---|
| "read GitHub issues/PRs/workflows" | `tools.github` with `mode: gh-proxy` and minimal `toolsets` |
| "use full MCP server/tool definitions" | `tools.github` with `mode: local` |
| "use other MCP servers but keep token cost down" | `tools.cli-proxy: true` (hybrid CLI-proxy mode) |
| "edit files" | `edit` tool (default unless restricted) |
| "run commands/tests" | `bash` tool (default unless restricted) |
| "browse web pages/docs" | `web-fetch` and/or `web-search` |
| "test UI flows" | `playwright` |

## Pattern Heuristics

| User says... | Recommended named pattern |
|---|---|
| "triage issues automatically" | `IssueOps` |
| "run on /commands with human approval loops" | `ChatOps` |
| "run every weekday and keep improving" | `DailyOps` |
| "monitor workflow failures and trends" | `MonitorOps` |
| "process a big backlog in chunks" | `BatchOps` |
| "run manually with input parameters" | `DispatchOps` |
| "keep advancing a feature one chunk at a time" | `Feature Grower` |
| "apply a label-based workflow" | `LabelOps` |
| "operate across multiple repositories" | `MultiRepoOps` |
| "coordinate multiple sub-agents" | `Orchestration` |
| "manage project board items" | `ProjectOps` |
| "research, plan, and assign issues" | `ResearchPlanAssignOps` |
| "self-correcting / retry on failure" | `CorrectionOps` |
| "run in a side/fork repo" | `SideRepoOps` |
| "write a spec before implementing" | `SpecOps` |
| "A/B test workflow variants" | `TrialOps` |
| "process items from a queue" | `WorkQueueOps` |
| "deterministic, no LLM needed" | `DeterministicOps` |
| "manage from a central repo" | `CentralRepoOps` |
| "track work via GitHub Projects" | `Monitoring with Projects` |

## Integration Auth Mapping

When the user names a third-party service or MCP server:

1. Confirm whether native tool, MCP server, or safe-output job is the right integration path.
2. Look up the integration's auth requirements and required scopes before finalizing the design.
3. Provide a concrete setup checklist with:
   - required GitHub Actions secrets (names to create)
   - workflow env variables that consume those secrets
   - minimum token scopes/permissions needed

Output format to use:

```text
Integration auth setup:
- <service-or-mcp>: <purpose>
  - Secrets to create: <SECRET_NAME>, <SECRET_NAME>
  - Workflow env vars: <ENV_VAR>=${{ secrets.<SECRET_NAME> }}
  - Required scopes/permissions: <least-privilege scopes>
```

Never suggest committing plaintext tokens.

## Data Strategy Mapping

| User says... | Maps to |
|---|---|
| "analyze PRs", "review issues", "check status" | add `steps:` that pre-fetch with `gh` + `jq` |
| "read the diff", "look at changed files" | add `steps:` using `gh pr diff` or `gh pr view --json files` |
| "search for patterns across repos" | add `steps:` using `gh search` + `jq` filters |
| "just respond to a comment" | no pre-fetch needed (event payload is enough) |
| "process each item individually" | suggest sub-agent pattern with `model: small` |
| "weekly digest", "compliance report", "license review", "policy audit" | pre-fetch with `gh` + `jq` into `/tmp/gh-aw/data/`; point prompt to those files |
