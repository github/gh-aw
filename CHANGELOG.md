# Changelog

All notable changes to this project will be documented in this file.

## v0.40.1 - 2026-02-03

### Bug Fixes

#### Handle 502 Bad Gateway errors in assign_to_agent handler by treating them as success. The cloud gateway may return 502 errors during agent assignment, but the assignment typically succeeds despite the error. The handler now logs 502 errors for troubleshooting but does not fail the workflow.

#### Add discussion interaction to smoke workflows and serialize the discussion

flag in safe-outputs handler config.

Smoke workflows now select a random discussion and post thematic comments to
validate discussion comment functionality. The compiler now emits the
`"discussion": true` flag in `GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG` when a
workflow requests discussion output, and lock files include `discussions: write`
permission where applicable.

#### Add discussion interaction to smoke workflows; compiler now serializes the `discussion` flag into the safe-outputs handler config so workflows can post comments to discussions. Lock files include `discussions: write` where applicable.

Smoke workflows pick a random discussion and post a thematic comment (copilot: playful, claude: comic-book, codex: mystical oracle, opencode: space mission). This is a non-breaking tooling/workflow change.

#### Add discussion interaction to smoke workflows; deprecate the `discussion` flag and

add a codemod to remove it. Smoke workflows now query discussions and post
comments to both discussions and PRs to validate discussion functionality.

The compiler no longer emits a `discussion` boolean flag in compiled handler
configs; the `add_comment` handler auto-detects target type or accepts a
`discussion_number` parameter. A codemod `add-comment-discussion-removal` is
available via `gh aw fix --write` to remove the deprecated field from workflows.

#### Add GitHub App token minting for the GitHub MCP server tooling and workflows.

#### Add expires support to `safe-outputs.create-pull-request` so PRs can mark, describe, and auto-close expired runs.

#### Add safe-inputs gh CLI testing to smoke workflows; updates `shared/gh.md` to remove the `network.allowed` restriction and validate GitHub CLI access using `GITHUB_TOKEN`.

This changeset accompanies the PR that adds `safeinputs-gh` testing to all smoke workflows (smoke-copilot.md, smoke-claude.md, smoke-codex.md, smoke-opencode.md) and adjusts `shared/gh.md` accordingly.

#### Add safe-inputs gh CLI testing to smoke workflows.

This patch adds validation to the smoke workflows to exercise the GitHub CLI
integration via the `safeinputs-gh` tool. It also updates `shared/gh.md`
to remove the `network.allowed` restriction so the `safeinputs-gh` tool can
query PRs using the provided `GITHUB_TOKEN`.

#### Add step summaries for safe-output processing results.

Safe-output handlers now generate collapsible step summaries for each processed
message, providing visibility into what was created or updated during workflow
execution. Body previews are truncated at 500 characters to avoid bloat. The
feature is implemented for both regular safe-outputs and project-based
safe-outputs via a shared helper module.

#### Add built-in pattern detection and extensive tests for secret redaction in compiled logs.

This change adds built-in regex patterns for common credential types (GitHub, Azure, Google, AWS, OpenAI, Anthropic) to `redact_secrets.cjs` and includes comprehensive tests covering these patterns and combinations with custom secrets.

#### Added a stub `send_slack_message` safe-output job and workflow configuration so the smoke Copilot run can exercise Slack tooling without actually sending messages.

#### Add a `Build and Test gh-aw` task to every smoke workflow so each run verifies `make build` and `make test` before succeeding.

#### Aggregate validation errors so compilation reports all issues together and add the `--fail-fast` flag to preserve the legacy behavior when needed.

#### Update the workflow compiler so prompt steps emit separate `[ ... ]` tests joined with `&&` and recompile affected workflows to avoid deprecated `-a` usage.

#### Added a build-only smoke workflow task plus wider timeouts and explicit Go runtimes so the smoke jobs can reliably run `make build` before continuing.

#### Consolidate shell escaping utilities into `shell.go` and remove the duplicate helpers in `mcp_utilities.go` so the generator and tests use a single source of truth.

#### Convert the safe-outputs MCP server from stdio transport to HTTP transport. This change

follows the safe-inputs pattern and includes:

- HTTP server implementation and startup scripts for safe-outputs
- Updated MCP configuration rendering to use HTTP transport and Authorization header
- Added environment variables and startup steps for the safe-outputs server
- Tests and TOML rendering updated to match HTTP transport

This is an internal implementation change; there are no user-facing CLI breaking
changes.

#### Convert safe-outputs MCP server to HTTP transport and update generated

startup steps and MCP configuration to use HTTP (Authorization header,
port, and URL). This is an internal implementation change that moves the
safe-outputs MCP server from stdio to an HTTP transport and updates the
workflow generation to start and configure the HTTP server.

Changes include:
- New HTTP server JavaScript and startup scripts for safe-outputs
- Updated MCP config rendering to use `type: http`, `url`, and `headers`
- Workflow step outputs for `port` and `api_key` and changes to env vars

No public CLI behavior or user-facing flags were changed; this is an
internal/backend change and therefore marked as a `patch`.

#### Convert the `safe-outputs` MCP server from stdio/container transport to an HTTP-based transport and update generated workflow steps to start the HTTP server before the agent. This migrates to a stateful HTTP service, removes stdio/container fields from the generated MCP configuration, and exposes `GH_AW_SAFE_OUTPUTS_PORT` and `GH_AW_SAFE_OUTPUTS_API_KEY` for MCP gateway resolution.

This is an internal/tooling change and does not change public CLI APIs.

#### Document the `remove-labels` safe output type and add examples and a table-of-contents entry.

#### Document the two-file agentic workflow structure (separate `.github/agentics/<id>.md` prompt file and `.github/workflows/<id>.md` + runtime import) in the templates and docs, and teach the compiler to validate dispatch-workflow references while dynamically building the dispatch tools from compiled/yml files.

#### Documented the `remove-labels` safe output type: added reference documentation, examples, and a table-of-contents entry.

#### Documented the `remove-labels` safe output type, added examples and a table-of-contents entry.

#### Enable append-only comments for the `smoke-copilot` workflow.

The workflow now posts new status comments for each run instead of editing
the original activation comment. This adds `append-only-comments: true`
to the messages configuration so timeline updates create discrete comments.

Files changed: schema and `.github/workflows/smoke-copilot.md` (compiled lock updated).

#### Escape single quotes and backslashes when embedding JSON into shell environment

variables to prevent shell injection. This fixes a code-scanning finding
(`go/unsafe-quoting`) by properly escaping backslashes and single quotes
before inserting JSON into a single-quoted shell string.

Files changed:
- `pkg/workflow/update_project_job.go` (apply POSIX-compatible escaping)

This is an internal security fix and does not change the public CLI API.

#### Add more diagnostic logging around safe-outputs MCP server initialization and extend the startup timeout from 10s to 60s to reduce CI flakes.

#### Claude steps now use `--output-format stream-json` so the execution log stays newline-delimited JSON, matching the parser expectation.

#### Handle GitHub Actions PR creation permission errors by setting an `error_message` output

and adding an auto-filed issue handler with guidance when Actions cannot create or
approve pull requests in the repository.

This patch documents the change: the create-pull-request flow now emits a helpful
`error_message` output when permissions block PR creation, and the conclusion job
can use that to file or update an issue with next steps and links to documentation.

#### Sanitize the PATH export used by AWF firewall agents by sourcing a new `sanitize_path.sh` helper so empty elements and stray colons are removed before updating PATH.

#### Add more informative progress logging to the MCP gateway health check script so retries show elapsed time and total attempts.

#### Mirror runner-provided environment variables (Java, Android, browsers, package managers, and language toolchains) into the AWF agent container so workflows keep access to their expected tool paths.

#### Mirror essential GitHub Actions runner environment variables into the agent container so workflows retain access to tool paths.

#### Add the audited essential and common binaries (cat, curl, date, find, gh, grep, jq, yq, cp, cut, diff, head, ls, mkdir, rm, sed, sort, tail, wc, which) as read-only mounts inside the AWF agent container so workflows can rely on the expected utilities.

#### Move safe-output storage from `/tmp` to `/opt` and update the agent intake and secret-redaction

scripts to read from the new path `/opt/gh-aw/safeoutputs/outputs.jsonl`. This keeps the file writable
by the MCP server while making it read-only inside the agent container.

#### Pin workflows to the latest `actions/checkout` (v6.0.2) and `actions/download-artifact` (v7.0.0) releases and regenerate the lockfiles for the new SHAs.

#### Ensure the compound Copilot command is quoted before being passed to AWF/SRT so it runs inside the firewall container.

#### Quote the compound Copilot command passed to AWF/SRT so it runs inside the firewall container.

#### Refactored `ParseWorkflowFile`, added helper functions, and simplified network permissions.

#### Removed the redundant `workdir` field from generated MCP server configurations so the JSON/TOML configs match expectations.

#### Removed MCP gateway JSON validation from `gh aw compile --validate` to simplify MCP configuration rendering.

#### Removed the stray `workdir` field from generated MCP server configurations for agentic workflows so the output matches the expected schema.

#### Add retry logic to the GitHub Copilot CLI installer by moving the

installation steps into a dedicated shell script and invoking it from
workflows. This prevents intermittent download failures during setup.

The change includes creating `actions/setup/sh/install_copilot_cli.sh`,
updating `pkg/workflow/copilot_srt.go` to call the script, and
recompiling workflows that now reference the retry-enabled installer.

#### Use runtime-import macros for the main workflow markdown so the lock file can stay small and workflows remain editable without recompiling; frontmatter imports stay inlined and the compiler/runtime-import helper now track the original markdown path, clean expressions, and cache recursive imports while the updated tests verify the new behavior.

#### Migrate the `safe-outputs` MCP server from stdio transport to an HTTP transport and

update the generated workflow steps to start the HTTP server before the agent.

This change adds the HTTP server implementation and startup scripts, replaces the
stdio-based MCP server configuration with an HTTP-based configuration, and updates
environment variables and MCP host resolution to support the new transport.

#### Add diagnostic logging and widen the safe-outputs MCP server startup timeout to 60 seconds to tame CI flakiness.

#### Added a build-only verification step, explicit Go runtimes, and longer timeouts to every smoke workflow so `make build` can finish reliably.

#### Added build-and-test verification steps to each smoke workflow so failures surface early.

#### Added a build-only verification step and extended timeouts across the smoke workflows so the new task can finish reliably.

#### Added a build-only verification step plus longer timeouts and explicit Go runtimes to every smoke workflow so `make build` can finish reliably.

#### Added a build-only verification step and generous timeouts to every smoke workflow so the compiler validates `make build` reliably.

#### Added a build-only verification step, extended smoke workflow timeouts, and ensured each smoke frontmatter declares the Go runtime so `make build` can complete reliably.

#### Sort safe output tool messages by their temporary ID dependencies before dispatching them so single-pass handlers can resolve every reference without multiple retries.

#### Split the temporary ID helpers into their own `temporary_id.cjs` module and adjust the associated tests.

#### Document the new two-file agentic workflow structure (separating `.github/agentics/` prompts from `.github/workflows/` frontmatter) and update runtime imports so the noop safe output is kept out of the handler manager config.

#### Moved the update-project and create-project-status-update safe-output configs into the unified handler manager so workflows no longer run a separate project handler step for those types.

#### Bump the pinned versions for `actions/checkout` and `actions/download-artifact` to match the regenerated workflow locks.

#### Updated the AWF firewall to v0.11.2 and switched the AWF agent container to act.

#### Updated the AWF firewall to v0.11.2 and switched the agent container to act.

#### Update the default versions for Claude Code, Codex, GitHub MCP Server, Playwright MCP, and MCP Gateway to the latest releases.

#### Updated the embedded agentic tooling stack:

- Claude Code → 2.1.19
- Copilot CLI → 0.0.394
- Codex → 0.91.0
- Playwright MCP → 0.0.58 / Browser → v1.58.0
- Sandbox runtime → 0.0.32

Recompiled workflows and refreshed constants tests to match the new expectations.

#### Update the OpenAI Codex CLI from 0.89.0 to 0.91.0, regen the compiled workflows, and note the reduced sub-agent limit.

#### Update the default Copilot, MCP server, Playwright, and Gateway versions after their 2026-01-26 releases.

#### Bump MCP Gateway to v0.0.76: update `DefaultMCPGatewayVersion` and

recompiled workflow lock files to reference `ghcr.io/github/gh-aw-mcpg:v0.0.76`.
This is a non-breaking dependency bump.

#### Bump the MCP gateway reference to `v0.0.78` and regenerate all compiled workflow locks so they pull the latest container release.

#### Update `smoke-claude` workflow to import the shared `go-make` workflow and

expose `safeinputs-go` and `safeinputs-make` tools for running Go and Make
commands used by CI and local testing. This is an internal tooling update and
does not change public APIs.

The workflow now validates the `safeinputs-make` tool by running `make build`.

#### Update the `smoke-claude` workflow to import the shared `go-make` workflow and expose `safeinputs-go` and `safeinputs-make` tools for running Go and Make commands used by CI and local testing.

This is an internal/tooling workflow update and does not change public APIs.

#### Switch Claude CLI log capture to use `--debug-file /tmp/gh-aw/agent-stdio.log` directly instead of shell redirection.


## v0.36.0 - 2026-01-08

### Features

#### Migrate terminology from "agent task" to "agent session".

This change updates the CLI, JSON schemas, codemods, docs, and tests to use
the new "agent session" terminology. A codemod (`gh aw fix`) is included to
automatically migrate workflows; the old `create-agent-task` key remains
supported with a deprecation warning to preserve backward compatibility.


### Bug Fixes

#### Add domain blocklist support via `--block-domains` flag.

This change adds support for specifying blocked domains in workflow frontmatter and passes the `--block-domains` flag to Copilot/Claude/Codex engines during compilation. Includes parser updates, unit and integration tests, and documentation updates.

#### Add domain blocklist support via the `--block-domains` flag and the

`blocked` frontmatter field. This enables specifying domains or ecosystem
identifiers to block in workflows and ensures the flag is only added when
blocked domains are present.

Supported engines: Copilot, Claude, Codex.

Ref: github/gh-aw#9063

#### Use `awf logs summary` to generate the CI firewall report and print it to the GitHub Actions step summary.

- Adds `continue-on-error: true` to the "Firewall summary" step so CI does not fail when generating reports.
- Recompiles workflow lock files and merges `main` to pick up latest changes.
- Fixes github/gh-aw#9041

#### Bump gh-aw-firewall (AWF) default binary version to v0.8.2.

Updated the `DefaultFirewallVersion` constant, corresponding test expectations, updated documentation, and recompiled workflow lock files.

#### Bump Codex CLI default version to 0.78.0.

This updates the repository to reference `@openai/codex@0.78.0` (used by workflows),
and aligns the `DefaultCodexVersion` constant and related tests/docs with the new
version. Changes include security hardening, reliability fixes, and UX improvements.

Files affected in the PR: constants, tests, docs, and recompiled workflow lock files.

Fixes: github/gh-aw#9159

#### Copy Copilot session state files (`~/.copilot/session-state/*.jsonl`) to

`/tmp/gh-aw/sandbox/agent/logs/` before secret redaction so they are included
in workflow artifacts and available for debugging.

#### Fix template injection vulnerabilities in the workflow compiler by moving

user-controlled inputs into environment variables and securing MCP lockdown
handling. This change updates the way safe-inputs and MCP lockdown values are
passed to runtime steps (moved to `env:` blocks) and simplifies lockdown value
conversion. Affects several workflows and related MCP renderer/server code.

Fixes: github/gh-aw#9124

#### Increase markdown header levels by 1 for Copilot `conversation.md` outputs

before writing them to GitHub Actions step summaries. This change adds a
JavaScript transformer (used in the Copilot log parser), associated tests,
and integration wiring. This is an internal tooling change and includes
comprehensive tests; it does not introduce breaking changes.

#### Move action setup and compiler paths from `/tmp/gh-aw` to `/opt/gh-aw` so agent access is read-only; updates setup action, compiler constants, tests, and AWF mounts.

#### Move action setup and compiler paths from `/tmp/gh-aw` to `/opt/gh-aw` so agent access is

read-only; updates setup action, compiler constants, tests, and AWF mounts.

This is an internal/tooling change and does not change the public API.

#### Support protocol-specific domain filtering for `network.allowed` entries.

This change adds validation and compiler integration so `http://` and
`https://` prefixes (including wildcards) are accepted for protocol-specific
domain restrictions. It also preserves protocol prefixes through compilation,
adds unit and integration tests, and updates the documentation.

Fixes github/gh-aw#9040

#### Rewrite MCP server URLs using `localhost` or `127.0.0.1` to

`host.docker.internal` when the firewall is enabled so agents running
inside firewall containers can reach host MCP servers.

This change adds `RewriteLocalhostToDocker` to `MCPConfigRenderer` and
propagates sandbox configuration through MCP renderers. The rewriting
is skipped when `sandbox.agent.disabled: true` so existing localhost
URLs are preserved when explicitly disabled.

#### Support protocol-specific domain filtering for `network.allowed` entries: validation and compiler integration for `http://` and `https://` prefixes, tests, and documentation updates.

Fixes github/gh-aw#9040

#### Update Copilot CLI to `0.0.375` and Codex to `0.79.0`.

This patch updates the bundled CLI version constants, test expectations, and
regenerates workflow lock files. It also adds support for generating
conversation markdown via the Copilot `--share` flag (used when available),
fixes a path double-slash bug for `conversation.md`, and addresses an
async/await bug in the log parser.

These changes are backward-compatible and affect tooling, tests, and
workflow compilation only.

#### Update Copilot CLI to `0.0.375` and Codex to `0.79.0`; add Copilot `--share` conversation.md support, fix double-slash path and async/await bug, update tests and recompile workflows.

#### Upgrade actions/upload-artifact to v6.0.0 across workflows and recompiled lock files.

This adds the `v6.0.0` pin to `.github/aw/actions-lock.json` and updates compiled
workflows to reference `actions/upload-artifact@v6.0.0` (replacing v5.0.0 references).

This is an internal tooling change (workflow lock files) and does not affect runtime code.

#### Use `awf logs summary` to generate CI firewall reports and print them to the GitHub Actions step summary. Adds `continue-on-error: true` to the "Firewall summary" step so CI does not fail when generating reports. Recompiled workflow lock files and merged `main` to pick up latest changes.

Fixes github/gh-aw#9041

---

Crafted by Changeset Generator


### Migration Guide

`````markdown
The following breaking changes require code updates:

### Migrate terminology from "agent task" to "agent session".

If your workflows use the old `create-agent-task` frontmatter key, update them:

Before:

```yaml
create-agent-task: true
```

After:

```yaml
create-agent-session: true
```

Run `gh aw fix --write` to apply automatic updates across your repository.
`````

## Unreleased

### Breaking Changes

#### Terminology Change: "Agent Task" → "Agent Session"

The terminology for creating Copilot agent work items has been updated from "agent task" to "agent session" to better reflect their purpose and avoid confusion with other task concepts.

**Configuration Changes:**
- `create-agent-task` → `create-agent-session` in safe-outputs configuration
- `GITHUB_AW_AGENT_TASK_BASE` → `GITHUB_AW_AGENT_SESSION_BASE` environment variable
- Output names: `task_number`/`task_url` → `session_number`/`session_url`

**Migration:**
Run `gh aw fix` to automatically update your workflow files to use the new terminology. The deprecated `create-agent-task` key is still supported during the transition period with a deprecation warning.

**Note:** The external `gh agent-task` CLI command name remains unchanged as it is maintained separately.

## v0.35.1 - 2026-01-06

Maintenance release with dependency updates and minor improvements.

## v0.35.0 - 2026-01-06

### Features

#### Rename firewall terminology from "denied" to "blocked" across code, JSON fields,

interfaces, JavaScript variables, table headers, and documentation. This updates
struct fields, JSON tags, method names, and user-facing text to use "blocked".


### Bug Fixes

#### Add `allowed-github-references` safe-output field to restrict and escape unauthorized GitHub-style markdown references (e.g. `#123`, `owner/repo#456`). Includes backend parsing, JS sanitizer, schema validation, and tests.

#### Add `allowed-github-references` safe-output configuration to restrict which

GitHub-style markdown references (e.g. `#123` or `owner/repo#456`) are
allowed when rendering safe outputs. Unauthorized references are escaped with
backticks. This change adds backend parsing, a JS sanitizer, schema
validation, and comprehensive tests.

#### Bump AWF (gh-aw-firewall) to `v0.8.1`.

Updated the embedded default firewall version, adjusted tests and documentation, and recompiled workflow lock files to use the new AWF version.

This is an internal dependency/tooling update (non-breaking).

#### Update the default GitHub MCP Server Docker image to `ghcr.io/github/github-mcp-server:v0.27.0`.

This updates the `DefaultGitHubMCPServerVersion` constant in `pkg/constants/constants.go`,
adjusts hardcoded version strings in tests and documentation, and recompiles workflow lock
files so workflows use the new MCP server image. The upstream release includes improvements
to `get_file_contents` (better error handling and default-branch fallback), `push_files`
(non-initialized repo support), and fixes for `get_job_logs`.

Note: The upstream `experiments` toolset was removed (experimental); this is unlikely to
affect production workflows.


### Migration Guide

`````markdown
The following breaking changes require code updates:

### Rename firewall terminology from "denied" to "blocked" across code, JSON fields,

Update JSON payloads and workflow outputs using these before/after examples:

Before:

```json
{
  "firewall_log": {
    "denied_requests": 3,
    "denied_domains": ["blocked.example.com:443"],
    "requests_by_domain": {
      "blocked.example.com:443": {"allowed": 0, "denied": 2}
    }
  }
}
```

After:

```json
{
  "firewall_log": {
    "blocked_requests": 3,
    "blocked_domains": ["blocked.example.com:443"],
    "requests_by_domain": {
      "blocked.example.com:443": {"allowed": 0, "blocked": 2}
    }
  }
}
```

Update Go types and interfaces (examples):

Before:

```go
type FirewallLog struct {
    DeniedDomains []string `json:"denied_domains"`
}

func (f *FirewallLog) GetDeniedDomains() []string { ... }
```

After:

```go
type FirewallLog struct {
    BlockedDomains []string `json:"blocked_domains"`
}

func (f *FirewallLog) GetBlockedDomains() []string { ... }
```

Update JavaScript variables and table headers (examples):

Before: `deniedRequests`, `deniedDomains`, table header `| Domain | Allowed | Denied |`

After: `blockedRequests`, `blockedDomains`, table header `| Domain | Allowed | Blocked |`

This is a breaking change for any code that relied on the previous field names or
JSON tags; update integrations to use the new `blocked_*` fields and `blocked` in
per-domain stats.
`````

## v0.34.5 - 2026-01-05

### Bug Fixes

#### Cleaned `add_labels.cjs` to make the implementation more concise and maintainable, and added a comprehensive test file `add_labels.test.cjs` covering 13 test cases.

No behavior changes expected; tests added to validate functionality.


## v0.34.4 - 2026-01-04

### Bug Fixes

#### Migrate detection job artifacts to the unified `/tmp/gh-aw/artifacts` path and add validations.

- Update artifact download paths used by detection jobs to `/tmp/gh-aw/artifacts`.
- Fail fast when `prompt.txt` or `agent_output.json` are missing.
- Fail when `aw.patch` is expected but not present.

This is an internal tooling fix and non-breaking (patch).

#### Use the unified `agent-artifacts` artifact for downloads and remove duplicate

artifact downloads. Updated tests to expect `agent-artifacts` and removed
dead/unused artifact upload helpers.

This is an internal fix that consolidates artifact downloads used by
safe_outputs and threat detection jobs.


## v0.34.3 - 2026-01-04

### Bug Fixes

#### Skip dynamic resolution warnings for actions pinned to full SHAs

Actions pinned to full 40-character commit SHAs should not emit dynamic
resolution warnings. This change updates `GetActionPinWithData()` to
detect SHA-based versions, suppress dynamic resolution warnings for
SHA-pinned actions, and preserve known SHA->version annotations when
available. Tests were added to cover SHA-pinned behavior.


## v0.34.2 - 2026-01-04

Maintenance release with dependency updates and minor improvements.

## v0.34.1 - 2026-01-03

### Bug Fixes

#### Auto-generated changeset for pull request #8700.

This is a patch-level changeset (internal/tooling/documentation).

#### Convert PR-related safe outputs and the `hide-comment` safe output to the

handler-manager architecture used by other safe outputs (e.g. `create-issue`).

This is an internal refactor: handlers now use the handler factory pattern,
enforce max counts, return result objects, and are managed by the handler
manager. TypeScript, linting, and Go formatting were applied.

#### Converted PR-related safe outputs and `hide-comment` to the handler manager architecture. Internal refactor only; no user-facing API changes.

Ahoy! This changeset was generated for PR #8683 by the Changeset Generator.

#### Refactor the threat detection result parsing step by moving the inline JavaScript into a dedicated

CommonJS module `actions/setup/js/parse_threat_detection_results.cjs` and update the compiler to require it.
Updated tests to use the require-based pattern and recompiled workflow lock files.


## v0.34.0 - 2026-01-02

### Features

#### Add standalone `awmg` CLI for MCP server aggregation. The new CLI provides a

lightweight MCP gateway and utilities to start and manage MCP servers for local
integration and testing.

This is a non-breaking tooling addition.


### Bug Fixes

#### Add mount for `/home/runner/.copilot` so the Copilot CLI inside the AWF container can

access its MCP configuration. This fixes smoke-test failures where MCP tools were
unavailable (playwright, safeinputs, github).

Fixes: github/gh-aw#8157

#### Add importable tools: `agentic-workflows`, `serena`, and `playwright`.

These tool definitions were added to the parser schema so they can be configured
in shared workflow files and merged into consuming workflows during compilation.
Includes tests and necessary schema updates.

#### Auto-detect GitHub MCP lockdown based on repository visibility.

When the GitHub tool is enabled and `lockdown` is not specified, the
compiler inserts a detection step that sets `lockdown: true` for public
repositories and `false` for private/internal repositories. The detection
defaults to lockdown on API failure for safety.

#### Document that MCP server capability configuration already uses v1.2.0 simplified API.

Both `pkg/cli/mcp_server.go` and `pkg/awmg/gateway.go` already use the modern
`ServerOptions.Capabilities` pattern from go-sdk v1.2.0, eliminating verbose
capability construction code.

No code changes required - this changeset documents the completion of issue #7711.

#### Configure jsweep workflow to use Node.js v20 and compile JavaScript to CommonJS.

This change documents that `jsweep.md` pins `runtimes.node.version: "20"` and
updates `actions/setup/js/tsconfig.json` to emit CommonJS (`module: commonjs`) and
target ES2020 (`target: es2020`) for the JavaScript files in `actions/setup/js/`.

#### Enable MCP gateway for smoke-copilot-no-firewall workflow

Enables the MCP gateway (`awmg`) so MCP server calls are routed through a centralized
HTTP proxy for the `smoke-copilot-no-firewall` workflow. Adds `features.mcp-gateway: true`
and a `sandbox.mcp` block with the gateway command and port.

This is an internal workflow/configuration change (patch).

---
summary: "Enable MCP gateway (awmg) in smoke-copilot-no-firewall workflow"

#### Refactor docker image download inline script to an external shell script at `actions/setup/sh/download_docker_images.sh`.

The generated workflow now calls `bash /tmp/gh-aw/actions/download_docker_images.sh` with image arguments instead of embedding the pull-and-retry function inline. No behavior changes.

#### Extract the "Setup threat detection" inline script into a reusable

`actions/setup/js/setup_threat_detection.cjs` module and update the
workflow compiler to require the module instead of embedding the
full script. Tests were updated to assert the require pattern.

#### Normalize artifact names to comply with upload-artifact@v5 and fix download path resolution.

Artifact names no longer include file extensions and use consistent delimiters (e.g., `prompt.txt` → `prompt`, `safe_output.jsonl` → `safe-output`). Updated download path logic accounts for `actions/download-artifact` extracting into `{download-path}/{artifact-name}/` subdirectories. Backward-compatible flattening preserves CLI behavior for older runs.

#### Fix compile timestamp handling and improve MCP gateway health check logging

Fixes handling of lock file timestamps in the compile command and enhances
gateway health check logging and validation order to check gateway readiness
before validating configuration files. Also includes minor workflow prompt
simplifications and safeinputs routing fixes when the sandbox agent is disabled.

This is an internal tooling and workflow change (patch).

#### Ensure safe-inputs MCP server start step receives tool secrets via an

`env:` block so the MCP server process inherits the correct environment.
Removes redundant `export` statements in the start script that attempted
to export variables that were not present in the step environment.

Fixes passing of secrets like `GH_AW_GH_TOKEN` to the MCP server process.

#### Fix SC2155: Separate export declaration from command substitution in workflows

Split variable assignment from `export PATH=...$(...)` into a separate
assignment and `export` so that the exit status of the command substitution
is not masked. This resolves 31 shellcheck SC2155 warnings related to PATH
setup in generated workflows and keeps `claude_engine.go` and
`codex_engine.go` consistent by using the `pathSetup` variable pattern.

Fixes: github/gh-aw#7897

#### Improve visibility when safe output messages are not handled

Fixed an issue where safe output messages (like create_issue) were silently skipped when no handler was loaded, with only a debug log that isn't visible by default. Now these cases produce clear warnings to help users identify configuration issues.

Changes:
- Convert debug logging to warning when message handlers are missing
- Add detailed warning explaining the issue and suggesting fixes
- Track skipped messages separately in processing summary
- Add test coverage for missing handler scenario

This ensures users are notified when their safe output messages aren't being processed, making it easier to diagnose configuration issues.

#### Migrate safe output handlers to a centralized handler config object and remove handler-specific environment variables.

All eight safe-output handlers (create_issue, add_comment, create_discussion, close_issue, close_discussion, add_labels, update_issue, update_discussion) were refactored to accept a single handler config object instead of reading many individual environment variables. This reduces the number of handler-specific env vars from 30+ down to 3 global env vars and centralizes configuration for easier testing and maintenance.

Files changed: multiple JavaScript safe output handlers under `actions/setup/js/` and related Go compiler cleanup in `pkg/workflow/`.

Benefits: explicit data flow, fewer environment variables, testable handlers, and simpler configuration.

#### Cleaned and modernized `check_permissions_utils.cjs` and improved test coverage.

This change modernizes JavaScript patterns (optional chaining, nullish coalescing,
array shorthand), simplifies error handling, and expands unit tests to reach
full coverage for the module.

#### Mount Copilot MCP config directory into AWF container so Copilot-based workflows can access MCP servers.

This exposes the Copilot config directory at `/home/runner/.copilot` to the AWF container with read-write
permissions, allowing the Copilot CLI to read and write MCP configuration and runtime state.

Fixes: github/gh-aw#8157

#### Pass MCP environment variables through to the MCP gateway (awmg) so the gateway process has access to the same secrets and env vars configured in the "Setup MCPs" step. This centralizes env var collection and updates gateway step generation and tests.

Files changed (PR #8677):
- pkg/workflow/mcp_servers.go
- pkg/workflow/gateway.go
- pkg/workflow/gateway_test.go

#### Refactor multi-secret validation into a shared shell script and simplify generator.

Replaced duplicated inline validation logic in compiled workflows with
`actions/setup/sh/validate_multi_secret.sh`, updated `pkg/workflow/agentic_engine.go`
to invoke the script, and adjusted tests and documentation accordingly.

This reduces repeated validation code across compiled workflows and centralizes
validation logic for easier maintenance and testing.

#### Refactor system prompts to be file-based under `actions/setup/md/` and

update runtime to read prompts from `/tmp/gh-aw/prompts/` instead of
embedding them in the Go binary. This is an internal refactor that
moves prompt content to runtime-managed markdown files and updates the
setup script and prompt generation logic accordingly.

#### Refactor safe output handlers to the handler factory pattern.

All safe output handlers were updated to export `main(config)` which returns a
message handler function. This is an internal refactor to improve handler
composition, state management, and testability. No user-facing CLI behavior
changes are expected.

#### Removed redundant syncing of JavaScript and shell scripts from

`actions/setup/` into `pkg/workflow/{js,sh}` and converted inline
JavaScript to a `require()`-based runtime-loading pattern. This reduces
binary size, eliminates duplicated generated files, consolidates setup
script copying into `actions/setup/setup.sh`, and updates workflow
script loading and tests to the new runtime behavior.

See PR #7654 for details.

#### Remove redundant JS/shell script syncing from `actions/setup` to `pkg/workflow`.

Scripts previously copied into `pkg/workflow/js` and `pkg/workflow/sh` are no longer required because `actions/setup/index.js` bundles them. This changeset documents the build-system and packaging cleanup (removed sync targets, deleted generated files, and adjusted embed directives).

#### Add compile step with security tools to pre-download Docker images and

store compile output for inspection. This ensures security scanning Docker
images (zizmor, poutine) are cached before the workflow analysis phase,
reducing runtime delays during scans.

#### Refactor safe outputs into a centralized handler manager using a

factory pattern. Introduces a safe output handler manager and begins
refactoring individual handlers to the factory-based interface. This is
an internal refactor (WIP) that reorganizes handler initialization and
message dispatching; tests and workflow recompilation are still pending.

#### Add validation for safe-inputs MCP server dependencies

Improves reliability of safe-inputs MCP server startup by adding comprehensive dependency validation:
- Validates all 12 required dependency files exist before starting the server
- Fails fast with clear error messages if files are missing
- Changes warnings to errors in setup script to prevent silent failures
- Adds proper shell script error handling (shebang, set -e, cd || exit patterns)

This prevents cryptic Node.js module errors when dependencies are missing and makes debugging easier.

#### Standardize safe output references to singular "upload-asset" across schemas,

parsing, and processing logic. Includes a codemod to migrate existing workflows
and updates to tests and documentation. This is a non-breaking internal
standardization and tooling change.

#### Track unresolved temporary IDs in safe outputs and perform synthetic

updates once those IDs are resolved. This ensures outputs (issues,
discussions, comments) created with unresolved temporary IDs are
updated to contain final values after resolution.

This is an internal fix to the safe output processing logic and does
not introduce any breaking API changes.

#### Update GitHub Copilot CLI default version to `0.0.374`.

This updates the `DefaultCopilotVersion` constant, adjusts test expectations,
updates documentation references, and recompiles workflow lock files to
reference `0.0.374`. No functional or breaking changes are included — this is
a cosmetic/help-output change only.

PR: #8685

#### Update GitHub Copilot CLI from `0.0.372` to `0.0.373`.

This updates the `DefaultCopilotVersion` constant, adjusts related tests, and
recompiles workflow lock files to use the new CLI version.

#### Fail-fast validation for safe-inputs MCP server startup and stricter error handling in setup scripts.

Validates that all required JavaScript dependency files for the safe-inputs MCP server are present before starting the server, lists missing files and directory contents when validation fails, and changes setup scripts to treat missing files as errors and exit immediately.

This prevents the server from starting with missing dependencies and producing opaque Node.js MODULE_NOT_FOUND crashes.

#### Warn when safe output messages are skipped due to missing handlers

When safe output messages (for example, `create_issue`) are sent but no
handler is loaded, they were silently skipped with only a debug log.
This change converts those debug logs to warnings, records skipped
messages in the processing results, and improves the processing summary
to separately report skipped vs failed messages.


## Unreleased

### Breaking Changes

#### Remove `githubActionsStep` as top-level workflow property

The `githubActionsStep` field has been removed from the top-level properties in the workflow schema. This field was dead code that was never referenced by user workflows or parser/compiler code. It has been moved to the internal `$defs` section where it continues to be used for schema validation purposes.

**Impact**: This is a breaking change only if workflows explicitly set `githubActionsStep:` as a top-level field (which was never a documented or intended use case). No production workflows were found using this field.

**Migration**: If any workflows are using `githubActionsStep` as a top-level field, remove it from the frontmatter as it was never functional.

Related to: #8374

## v0.33.12 - 2025-12-22

### Bug Fixes

#### Add standalone `awmg` CLI for MCP server aggregation. The new CLI provides a

lightweight MCP gateway and utilities to start and manage MCP servers for local
integration and testing.

This is a non-breaking tooling addition.

#### Clean and modernize `pkg/workflow/js/safe_outputs_tools_loader.cjs` by refactoring

internal functions (`loadTools`, `attachHandlers`, `registerDynamicTools`) to use
modern JavaScript patterns (optional chaining, nullish coalescing, handler map)
and reduce nesting and complexity. No behavioral changes.

#### Standardize safe output references to singular "upload-asset" across schemas,

parsing, and processing logic. Includes a codemod to migrate existing workflows
and updates to tests and documentation. This is a non-breaking internal
standardization and tooling change.


## v0.33.11 - 2025-12-22

Maintenance release with dependency updates and minor improvements.

## v0.33.10 - 2025-12-22

### Bug Fixes

#### Fix: Ensure `create-pull-request` and `push-to-pull-request-branch` safe outputs

are applied correctly by downloading the patch artifact, checking out the
repository, configuring git, and using the appropriate token when available.

This is an internal tooling fix for action workflows; it does not change the
public CLI API.

--
PR: #7167

#### Optimize safe output checkout to use shallow fetch and targeted branch fetching

Safe output jobs for `create-pull-request` and `push-to-pull-request-branch` used
full repository checkouts (`fetch-depth: 0`). This change documents the optimization
to use shallow clones (`fetch-depth: 1`) and explicit branch fetches to reduce
network transfer and clone time for large repositories.

#### Optimize safe output jobs to use shallow repository checkouts and targeted

branch fetching for `create-pull-request` and
`push-to-pull-request-branch` safe output jobs. This reduces network transfer and
clone time for large repositories by using `fetch-depth: 1` and fetching only the
required branch.


## v0.33.9 - 2025-12-21

Maintenance release with dependency updates and minor improvements.

## v0.33.8 - 2025-12-20

Maintenance release with dependency updates and minor improvements.

## v0.33.7 - 2025-12-19

Maintenance release with dependency updates and minor improvements.

## v0.33.6 - 2025-12-19

### Bug Fixes

#### Update github.com/goccy/go-yaml from v1.19.0 to v1.19.1

This patch update includes:
- Fixed decoding of integer keys of map type
- Added support for line comment for flow sequence or flow map

No breaking changes or behavior modifications.


## v0.33.5 - 2025-12-19

Maintenance release with dependency updates and minor improvements.

## v0.33.4 - 2025-12-18

### Bug Fixes

#### Add a second pass for JavaScript formatting that runs `terser` on `.cjs` files after `prettier` to reduce file size while preserving readability and TypeScript compatibility.

This change adds the `terser` dependency and integrates it into the `format:cjs` pipeline (prettier → terser → prettier). Files that are TypeScript-checked or contain top-level/dynamic `await` are excluded from terser processing to avoid breaking behavior.

This is an internal tooling change only (formatting/minification) and does not change runtime behavior or public APIs.

Summary of impact:
- 14,784 lines removed across 65 files (~49% reduction) due to minification and formatting
- TypeScript type checking preserved
- All tests remain passing

#### Upgrade google/jsonschema-go to v0.4.0 with critical bug fixes and new features

This upgrade brings several improvements:
- PropertyOrder feature for deterministic property ordering
- Fixed nullable types for slices and pointers 
- Full Draft-07 support
- JSON marshal consistency fixes

Updated tests to handle v0.4.0's new `Types []string` field for nullable types (nullable slices now use `["null", "array"]` instead of a single `"array"` type string).


## v0.33.3 - 2025-12-18

Maintenance release with dependency updates and minor improvements.

## v0.33.2 - 2025-12-17

### Bug Fixes

#### Bump the default Claude Code CLI version from 2.0.70 to 2.0.71.

Workflows that install and reference the Claude Code CLI now use v2.0.71.

#### Replace insecure 'curl | sudo bash' Copilot installer usage with the official `install.sh` downloaded to a temporary file, executed, and removed. Tests updated to assert secure installer usage. Fixes github/gh-aw#6674

---

This changeset was generated for PR #6691.

#### Bump default CLI versions: Claude Code to 2.0.70 and Codex to 0.73.0. Regenerated workflow lock files.

This change updates the default CLI version constants and includes the regenerated
workflow lock files produced by `make recompile`.

Fixes github/gh-aw#6587

#### Update the GitHub MCP Server Docker image to `v0.25.0`.

- Bump `DefaultGitHubMCPServerVersion` to `v0.25.0` in `pkg/constants/constants.go`.
- Recompiled workflow `.lock.yml` files to reference the new image version.
- Updated tests and action pins that referenced the previous version.

This is a non-breaking patch release that updates the MCP server image and related test/lockfile references.

#### Use the official Copilot CLI `install.sh` script instead of piping a

downloaded script directly into `sudo bash`. The new pattern downloads the
installer to a temporary file, executes it, and removes the temporary file to
reduce supply-chain risk. Tests were updated to assert the secure install
pattern. Fixes github/gh-aw#6674


## v0.33.1 - 2025-12-16

### Bug Fixes

#### Add a `mentions` configuration to `safe-outputs` to control how `@mentions`

are filtered in AI-generated content. The option supports both boolean and
object forms for fine-grained control (e.g., `allow-team-members`,
`allow-context`, an explicit `allowed` list, and a `max` per-message limit).

This change is non-breaking and preserves existing behavior when the setting
is unspecified.

#### Refactor: Extracted shared context validation helpers into

`update_context_helpers.cjs` and updated `update_issue.cjs` and
`update_pull_request.cjs` to import and use the shared helpers. This reduces
duplication and improves maintainability. Fixes github/gh-aw#6563

#### Prevent false-positive download annotations by gating the patch download step

behind `needs.agent.outputs.has_patch == 'true'`. When a run uses only
safe-outputs and no code patch is produced, the artifact `aw.patch` will not
exist; the conditional avoids attempting to download it and thus removes
erroneous GitHub Actions error annotations.

Files changed:
- `pkg/workflow/artifacts.go` - added conditional support for artifact steps
- `pkg/workflow/threat_detection.go` - download step now has an `if` check
- `pkg/workflow/threat_detection_test.go` - added test for conditional step

Fixes: false-positive `Unable to download artifact(s): Artifact not found`
issues when no patch is present.

#### Add lazy mention resolution with collaborator filtering, assignee support, and a 50-mention limit.

This change introduces a dedicated `resolve_mentions` module that lazily
resolves @-mentions, caches recent collaborators for optimistic resolution,
filters out bots, and adds assignees to known aliases. It also updates
workflows to include author/assignee mentions where appropriate.

#### Move mention filtering from incoming text processing to the agent output collector.

This is an internal refactor and bugfix: sanitizers were modularized, mention
resolution was moved into the output collector, and a bug that prevented known
authors from being preserved in mentions was fixed. Tests were updated.

#### Remove mention neutralization/sanitization code from `compute_text.cjs`.

Switched from `sanitizeContent` to `sanitizeIncomingText`, removed the
dead `knownAuthors` collection and the `isPayloadUserBot` import. Tests
were updated to match the simplified sanitization behavior.


## v0.33.0 - 2025-12-15

### Features

#### Remove legacy support for the `GH_AW_COPILOT_TOKEN` secret name.

This change removes the legacy fallback to `GH_AW_COPILOT_TOKEN`. The effective token lookup chain is now:

- `COPILOT_GITHUB_TOKEN` (recommended)
- `GH_AW_GITHUB_TOKEN` (legacy)

If you were relying on `GH_AW_COPILOT_TOKEN`, update your repository secrets and workflows to use `COPILOT_GITHUB_TOKEN` or `GH_AW_GITHUB_TOKEN`.

## Migration

Run the following to remove the old secret and add the new one:

```bash
gh secret remove GH_AW_COPILOT_TOKEN -a actions
gh secret set COPILOT_GITHUB_TOKEN -a actions --body "YOUR_PAT"
```text

This follows the precedent set when `COPILOT_CLI_TOKEN` was removed in v0.26+. All workflow lock files have been regenerated to reflect this new token chain.


### Bug Fixes

#### Add structured types for frontmatter configuration parsing and fix integer rendering in YAML outputs.

This change introduces typed frontmatter parsing (`FrontmatterConfig`) to reduce runtime type
assertions and improve error messages. It also fixes integer marshaling so integer fields
(for example `retention-days` and `fetch-depth`) are preserved as integers in compiled YAML.

#### Add `GH_DEBUG=1` to the shared `gh` safe-input tool configuration so

that `gh` commands executed via the `safeinputs-gh` tool run with
verbose debugging enabled.

This is an internal/tooling change that affects workflow execution
verbosity only.

#### Add `hide-older-comments` boolean and `allowed-reasons` array fields to `add-comment` and `hide-comment` safe outputs; includes parsing, JavaScript hiding logic, tests, and documentation updates.

This change adds support for hiding older comments from the same workflow (identified by workflow-id) and allows restricting which hide reasons are permitted via the `allowed-reasons` field. Backwards compatible: if `allowed-reasons` is omitted, all reasons are allowed.

#### Introduce a typed `WorkflowStep` struct and helper methods for safer,

type-checked manipulation of GitHub Actions steps. Replace ad-hoc
`map[string]any` handling in step-related code with the new type where
possible, add conversion helpers, and add tests. Also fix
`ContinueOnError` to accept both boolean and string values.

Fixes github/gh-aw#6053

#### Bump Claude Code CLI from 2.0.65 to 2.0.67.

Updated the `DefaultClaudeCodeVersion` constant, test fixtures, documentation reference, and regenerated workflow lock files to install `@anthropic-ai/claude-code@2.0.67`.

#### Bump Claude Code to `2.0.69` and Codex to `0.72.0`.

This updates the pinned CLI tool versions used by the project and reflects
the workflow recompilation performed in the associated pull request.

#### Display agent response text in action logs as a conversation-style summary.

This change updates the action log rendering so agent replies are shown
inline with tool calls, making logs read like a chat conversation. Agent
responses are prefixed with "Agent:", tool calls use ✓/✗, shell commands
are shown as `$ command`, and long outputs are truncated to keep logs
concise.

This is an internal, non-breaking improvement to log formatting.

#### Fix firewall logs not printing due to incorrect directory path. The firewall

log parser was reading from a sanitized workflow-specific directory but the
logs are written to a fixed sandbox path. This change documents the bugfix
that updates the parser to read from `/tmp/gh-aw/sandbox/firewall/logs/` and
removes the unnecessary workflow name sanitization.

#### Fix shellcheck violations in `pkg/workflow/sh/start_safe_inputs_server.sh` that caused

issues in compiled workflow lock files. Changes include quoting variables, using compound
redirection, and replacing `ps | grep` with `pgrep`. Recompiled workflows were updated
to propagate the fixes to lock files.

#### Adds `hide-older-comments` boolean and `allowed-reasons` array fields to

`add-comment` and `hide-comment` safe outputs; includes parsing, JavaScript
hiding logic, tests, and documentation updates.

This is a patch-level change: non-breaking additions, tests and docs only.

#### Adds `hide-older-comments` boolean and `allowed-reasons` array fields to the

`add-comment` and `hide-comment` safe outputs; includes parsing, JavaScript
hiding logic, tests, and documentation updates.

#### Add a JavaScript helper that removes duplicate titles from safe output descriptions and register it in the bundler.

The helper `removeDuplicateTitleFromDescription` is used by create/update scripts for issues, discussions, and pull requests to avoid repeating the title in the description body.

#### Rename the `command` trigger to `slash_command` with a deprecation path.

The `slash_command` frontmatter field was added (same validation as the old `command`), and the `command` field remains supported but is marked as deprecated and emits a compile-time warning. Schema, compiler, docs, and workflows were updated to prefer `slash_command` while keeping backward compatibility.

#### Update default CLI versions to latest patch releases:

- Copilot: `0.0.368` → `0.0.369`
- Codex: `0.69.0` → `0.71.0`
- Playwright MCP: `0.0.51` → `0.0.52`

Bumped version constants and recompiled workflow lock files.

Fixes github/gh-aw#6187

#### Replace the npm-based GitHub Copilot CLI installation with the

official installer script and add support for mounting the installed
binary into AWF runs.

This removes the Node.js npm dependency for AWF mode and documents
the new `--mount /usr/local/bin/copilot:/usr/local/bin/copilot:ro`
usage for workflows that run Copilot inside AWF.


## v0.32.2 - 2025-12-10

### Bug Fixes

#### Add actions directory structure and Go-based build tooling; initial actions and Makefile targets.

This documents the changes introduced by PR #5953: create an `actions/` directory, add `actions-build`, `actions-validate`, and `actions-clean` targets, initial actions `setup-safe-inputs` and `setup-safe-outputs`, and supporting Go CLI commands for building and validating actions.

Fixes github/gh-aw#5948

#### Add a new `bots` frontmatter field that allows listing GitHub Apps/bots authorized to trigger a workflow.

This change documents the schema and implementation work: schema update, Go parsing and env passing, JavaScript validation, and tests.

#### Added support for passing workflow inputs to `gh aw run` via the new `--raw-field` (`-f`) flag. This accepts `key=value` pairs and forwards them to `gh workflow run` as `-f key=value` arguments. The implementation validates input formatting and provides clear error messages for malformed inputs.

#### Add human-friendly schedule format parser, schema updates, docs, and tests.

This change introduces a deterministic parser that converts simplified
natural-language schedule expressions into valid GitHub Actions cron
syntax, updates the workflow schema to accept shorthand and array formats,
adds fuzz and unit tests, and enhances documentation with usage examples.

Non-breaking: this is an internal feature addition and documentation update.

#### Fix Copilot log parsing so empty JSON arrays are handled correctly and the

Agent Log Summary no longer shows "Log format not recognized as Copilot JSON
array or JSONL". Includes a regression test and updated compiled workflows.

#### Fix push-to-pull-request-branch summary to link commit URL instead of branch URL

#### Remove support for the `COPILOT_CLI_TOKEN` environment variable.

This change removes `COPILOT_CLI_TOKEN` from the Copilot engine secrets list, token
precedence logic, and trial support. Documentation and tests were updated. Workflows
that currently rely on `COPILOT_CLI_TOKEN` must migrate to `COPILOT_GITHUB_TOKEN`.

Migration example:

```bash
gh secret set COPILOT_GITHUB_TOKEN -a actions --body "(your-github-pat)"
```text


## v0.32.1 - 2025-12-09

### Bug Fixes

#### Adds generated asset links (issues, PRs, comments, etc.) to the conclusion job's

workflow completion comment so created assets appear as direct links with GitHub
rich previews.

> 🏴‍☠️ Ahoy! This treasure was crafted by [Changeset Generator](https://github.com/github/gh-aw/actions/runs/20064257954)

#### Convert the safe outputs MCP server to run as a Node process (follow safe inputs pattern). Refactor bootstrap, write modules as individual `.cjs` files, add tests, fix log directory and environment variables, improve ingestion logging, and remove premature config cleanup so ingestion can validate outputs correctly.

#### Update CLI default versions for agent runtimes and MCP bundles:

- Claude Code: 2.0.61 → 2.0.62
- Codex: 0.65.0 → 0.66.0
- Playwright MCP: 0.0.50 → 0.0.51

All workflows recompiled and binary rebuilt as part of the PR validation.


## v0.32.0 - 2025-12-08

### Features

#### Add a changeset for pull request #5782. The PR description was empty, so this defaults to a patch-level changeset for internal/tooling/documentation changes.


### Bug Fixes

#### Add support for configuring default agent and detection models via GitHub Actions

variables. This exposes the following variables for workflows:

- `GH_AW_MODEL_AGENT_COPILOT`, `GH_AW_MODEL_AGENT_CLAUDE`, `GH_AW_MODEL_AGENT_CODEX`
- `GH_AW_MODEL_DETECTION_COPILOT`, `GH_AW_MODEL_DETECTION_CLAUDE`, `GH_AW_MODEL_DETECTION_CODEX`

These variables provide configurable defaults for agent execution and threat
detection models without changing workflow frontmatter.

#### Add MCP tools list to action logs. `generatePlainTextSummary()` now writes a formatted

list of available MCP tools to action logs (via `core.info()`), improving visibility
when reviewing execution logs. Tests were added and workflows were recompiled.

#### Add a new workflow `smoke-copilot-safe-inputs` that uses the `gh` CLI safe-input tool

instead of the GitHub MCP server and sets messages to emoji-only.

#### Auto-generated changeset for pull request #5729 — no description provided.

This is a default `patch` changeset because the PR description is empty; internal or tooling changes are considered patch-level.

#### Bump Claude Code CLI from `2.0.60` to `2.0.61`.

Updated:
- `pkg/constants/constants.go` (version constant)
- Regenerated workflow `.lock.yml` files with new version reference
- Updated test expectations to match new CLI version

This is a patch release with no breaking changes detected.

#### Add support for configuring default agent and detection models through GitHub Actions variables.

This change introduces environment variables for agent execution and threat detection (e.g.
`GH_AW_MODEL_AGENT_COPILOT`, `GH_AW_MODEL_DETECTION_COPILOT`, etc.), updates workflow
YAML generation to inject those variables, and ensures explicit frontmatter configuration
still takes precedence. No breaking CLI changes.

#### Consolidate duplicate MCP server implementations by using a single shared core

implementation and removing the duplicate `mcp_server.cjs` code.

This is an internal refactor that reduces duplicated code and simplifies
maintenance for MCP server transports (HTTP and stdio).

#### Defer cache-memory saves until after threat detection validates agent output.

The agent job now uploads cache-memory artifacts and the new `update_cache_memory`
job saves those artifacts to the Actions cache only after threat detection passes.

This fixes a race where cache memories could be saved before detection validated
the agent's output.

Fixes github/gh-aw#5763

#### Detect and report detection job failures in the conclusion job. Adds support for a

`safe-outputs.messages.detection-failure` message, updates `notify_comment` and
message rendering, and includes tests covering detection failure scenarios.

#### Expose the safe-inputs MCP HTTP server via Docker's `host.docker.internal` and add `host.docker.internal` to the Copilot firewall allowlist so containerized services can access host-hosted safe-inputs.

This is a patch-level change (internal/tooling) and does not introduce breaking changes.

#### Fix `gh.md` to explicitly reference the `safeinputs-gh` tool name instead of the

ambiguous "gh" safe-input tool; update prompt text and examples to use
`safeinputs-gh` consistently.

This patch updates documentation and smoke workflow prompts to avoid
confusion between the `gh` CLI and the MCP tool named `safeinputs-gh`.

#### Add missing agent bootstrap `safe_inputs_bootstrap.cjs` support.

The pull request fixes a bug where the embedded safe inputs bootstrap script
was not exposed via a getter and therefore not written to the
`/tmp/gh-aw/safe-inputs/` directory. This change adds the getter and the
file-writing step so workflows depending on `safe_inputs_bootstrap.cjs` can
load it correctly.

#### Fix safe-inputs MCP config for Copilot CLI: convert `type: stdio` to `type: local` when generating Copilot fields; fix server startup JS to avoid calling `.catch()` on undefined; update tests to assert behavior for Copilot and Claude.

This is a non-breaking bugfix that ensures the Copilot CLI receives a compatible MCP `type` and that the generated server entrypoint handles errors correctly.

#### Fix safe-inputs MCP config for Copilot CLI: convert `type: stdio` to `type: local` when generating Copilot fields; fix server startup JS to avoid calling `.catch()` on undefined; update tests to assert behavior for Copilot and Claude.

#### Fix JavaScript export/invocation bug in placeholder substitution.

Updated the JS substitution helper to export a named async function and
adjusted the generated call site in the compiler to invoke that function.
Recompiled workflows and verified generated scripts pass Node.js syntax
validation.

#### Implement a local MCP HTTP transport layer and remove the `@modelcontextprotocol/sdk` dependency.

Adds `mcp_logger.cjs`, `mcp_server.cjs`, and `mcp_http_transport.cjs` plus unit and integration tests. Internal refactor and tooling change only; no public CLI breaking changes.

#### Move `get_me` out of the default GitHub MCP toolsets and into the `users` toolset.

Workflows that rely on the `get_me` tool must now opt in by adding `toolsets: [users]` under the `github` tools configuration.

This change updates the toolset mappings and documentation; tests were adjusted to ensure `get_me` is not available by default.

#### Refactor PR description updates: extract helper module, add `replace-island` mode for

idempotent PR description sections, make footer messages customizable via workflow
frontmatter, and add tests.

This change introduces a new helper `update_pr_description_helpers.cjs`, a
`replace-island` operation mode that updates workflow-run-scoped islands in PR
descriptions, and customizable footer messages via `messages.footer` in the
workflow frontmatter. Tests were added for the helper and integration scenarios.

#### Refactor safe-inputs MCP server bootstrap to remove duplicated startup logic and centralize

config loading, tool handler resolution, and secure config cleanup.

Adds a shared `safe_inputs_bootstrap.cjs` module and updates stdio/HTTP transports to use it.

Fixes github/gh-aw#5786

#### Fix gh.md to explicitly reference the `safeinputs-gh` tool name instead of the ambiguous "gh" safe-input tool.

This updates prompt text, examples, and documentation comments to consistently use `safeinputs-gh` and prevent confusion with the `gh` CLI.

#### Replaced unsafe `envsubst` usage with JavaScript-based safe placeholder

substitution and fixed a JavaScript export/invocation bug in the
placeholder substitution code. Workflows were recompiled and validated.

#### Document required environment variable names for safe-inputs `tools.json` and

delete the file immediately after loading to avoid leaving secrets on disk.

The `tools.json` file now contains only environment variable names (e.g.
`"GH_TOKEN": "GH_TOKEN"`) and the server removes the file after reading it.

#### Add firewall health endpoint test and display the list of available tools

to the `smoke-copilot` workflow. This adds non-breaking test checks that
curl the (redacted) firewall health endpoint and prints the HTTP status
code, and ensures the workflow displays the available tools for debugging.

These are test and workflow changes only and do not modify the CLI API.

#### Update Claude Code to 2.0.60 and Playwright MCP to 0.0.50. Add the new `--disable-slash-commands` flag to Claude Code CLI

invocations so workflows can opt out of slash command behavior in Claude Code sessions.

Fixes github/gh-aw#5669

<!-- This changeset was generated automatically for PR #5672 -->

#### Update `github.com/spf13/cobra` dependency from v1.10.1 to v1.10.2.

This patch updates the Cobra dependency to v1.10.2 which migrates its
internal YAML dependency from the deprecated `gopkg.in/yaml.v3` package to
`go.yaml.in/yaml/v3`. The change is internal to the dependency and is
transparent to consumers of `gh-aw`.

No code changes were required in this repository. Run the usual validation
steps: `make test`, `make lint`, and `make agent-finish`.

#### Update GitHub MCP Server from v0.24.0 to v0.24.1. This updates the default MCP server

version and recompiles workflow lock files to pick up the bugfix that includes empty
properties in the `get_me` schema for OpenAI compatibility.

Changes:
- Updated `pkg/constants/constants.go` to set `DefaultGitHubMCPServerVersion` to `v0.24.1`.
- Recompiled workflow lock files to use `v0.24.1`.

Fixes github/gh-aw#5877


## v0.31.10 - 2025-12-05

Maintenance release with dependency updates and minor improvements.

## v0.31.9 - 2025-12-04

Maintenance release with dependency updates and minor improvements.

## v0.31.8 - 2025-12-04

Maintenance release with dependency updates and minor improvements.

## v0.31.7 - 2025-12-04

Maintenance release with dependency updates and minor improvements.

## v0.31.6 - 2025-12-04

### Bug Fixes

#### Expand the "default" GitHub MCP toolset into individual, action-friendly

toolsets (exclude `users`) and add support for the `action-friendly`
keyword. This ensures generated workflows expand `default` into the
`context,repos,issues,pull_requests` toolsets which are compatible with
GitHub Actions tokens.

#### Replace GitHub MCP with the shared `gh` CLI tool in the `dev.md` workflow, list the last 3 issues, reduce permissions to `issues: read`, and enable safe-inputs by default. Includes documentation and test updates.

This is an internal/tooling and documentation change.


## v0.31.5 - 2025-12-03

### Bug Fixes

#### Convert `.prompt.md` templates to `.agent.md` format and move them

to `.github/agents/`. Update the CLI, tests, workflows, and Makefile
to reference the new agent files and remove the old prompt files.

This change is internal/tooling only and does not change the public API.

#### Fix template injection warnings by moving GitHub expressions into environment

variables and documenting safe cases.

Moved `needs.release.outputs.release_id` and `github.server_url` into `env` to
avoid template-injection scanner false positives while keeping behavior
unchanged. Documented that other flagged expressions use trusted GitHub
context and require no change.

Fixes: github/gh-aw#5299


## v0.31.4 - 2025-12-03

### Bug Fixes

#### Add test workflows for create-agent-task safe output

#### Added new shared agentic workflow: Another one

#### Add `assign-to-user` safe output type and supporting files (schemas, Go structs, JS implementation, tests, and docs).

This change adds a new safe output `assign-to-user` analogous to `assign-to-agent`, including parser schema, job builder, JavaScript runner script, and tests. It is an internal addition and does not change public CLI APIs.

#### Add --log-dir to Copilot sandbox args and use the sandbox folder

structure for firewall logs so logs are written to the expected
locations for parsing and analysis.

This fixes firewall mode where Copilot logs were not being written to
/tmp/gh-aw/sandbox/agent/logs/ and updates firewall log collection
paths and artifact naming.

#### Add the missing `--log-dir` argument to the Copilot sandbox (firewall) mode so logs are written to the expected

location (`/tmp/gh-aw/.agent/logs/`) for parsing and analysis.

Files changed:
- `pkg/workflow/copilot_engine.go` (added `--log-dir`)
- `pkg/workflow/firewall_args_test.go` (test added to verify presence)

#### Add opt-in file logging to safe outputs MCP server via GH_AW_MCP_LOG_DIR environment variable

#### Add support for loading MCP tool handlers from external files. This change

updates the JavaScript MCP server core and its schema to allow a tool's
`handler` field to point to a file whose default export (or module export)
is used as the handler function. Supports sync/async handlers, ES module
default exports, shell-script handlers, path traversal protection,
serialization fallback for circular structures, and extensive logging. Tests
for handler loading were added and workflows were recompiled.

#### Add "p" element to safe HTML allow list in text sanitization

#### Add debugging logs to Playwright tool configuration and execution

#### Add stop-after workflow trigger field for deadline enforcement

#### Add `imports` field to workflow_dispatch and workflow_run triggers for dynamic workflow composition

#### Add support for `allowed-repos` in `create-issue` and `create-discussion`

safe-outputs. Agent outputs may now include an optional `repo` field to
target a repository from the configured `allowed-repos`. Temporary IDs
are now resolved to `(repo, number)` pairs while remaining backward
compatible with the legacy single-repo format.

This is an internal enhancement to expand safe-outputs to support
creating issues/discussions across multiple repositories.

#### Bump default gh-aw-firewall (AWF) binary version from v0.5.0 to v0.5.1.

This updates the `DefaultFirewallVersion` constant and recompiles workflow lock files that depend on the firewall binary.

#### Bump default gh-aw-firewall (AWF) binary version from v0.5.0 to v0.5.1. Updates the `DefaultFirewallVersion` constant and recompiled workflow lock files that depend on the firewall binary.

#### Bump default gh-aw-firewall (AWF) binary version from v0.5.0 to v0.5.1. This updates the `DefaultFirewallVersion` constant and recompiled workflow lock files that depend on the firewall binary.

#### Update create-issue with sub-issues support

#### Fixed JSON encoding in mcp-config.json to properly handle special characters in HTTP MCP headers

#### Fixed import path in safe-outputs/noop.md by adding missing shared/ prefix

#### Fix bug where not all workspace files were uploaded as squid logs.

Signed-off-by: Mossaka

#### Fixed trailing whitespace in GitHub custom agent format examples

#### Add HTTP MCP header secret support for copilot engine

#### Update Playwright MCP server configuration to use npx launcher

#### Refactor the create-issue Copilot assignment to run in a separate step

Created a dedicated output (`issues_to_assign_copilot`) and separate
post-step that uses the agent token (`GH_AW_AGENT_TOKEN`) to assign
the agent to newly-created issues. This change isolates Copilot
assignment from regular post-steps and improves permission separation.

#### Refactor safe output type validation into a data-driven validator engine.

Moves validation logic into `safe_output_type_validator.cjs`, generates
validation configuration from Go as a single source of truth, and updates
the JavaScript collector to use the new validator. Adds tests and keeps
the generated `validation.json` filtered and indented to reduce merge
conflicts.

#### Updated Agent Workflow Firewall (AWF) to version 0.5.0

#### Update awf to v0.6.0, add the `--proxy-logs-dir` flag to direct firewall proxy

logs to `/tmp/gh-aw/sandbox/firewall/logs`, and remove the post-run `find`
step that searched for agent log directories.

This is an internal/tooling change (updated dependency and script behavior).

#### Update the changeset generator workflow to use the `codex` engine with the

`gpt-5-mini` model. Add `strict: false` and remove the `firewall: true` network
setting to accommodate the codex engine's network behavior.

This is an internal tooling change.

#### Update Claude Code CLI to v2.0.54

#### Updated go.uber.org/multierr indirect dependency from v1.9.0 to v1.11.0

#### Update Playwright server configuration defaults to use NPX launcher instead of Docker

#### Update documentation with current datetime format in system prompt

#### Add validation to ensure safe output messages are correctly processed when MCP server starts


## v0.31.3 - 2025-11-25

### Bug Fixes

#### Add automated changeset generation workflow that creates changeset files when pull requests are marked as ready for review

#### Added support for importing safe-outputs configurations from shared workflow files

#### Update DEVGUIDE.md with agent-finish command


## v0.31.2 - 2025-11-25

Maintenance release with dependency updates and minor improvements.

## v0.31.1 - 2025-11-25

### Bug Fixes

#### Changed AWF mount mode from read-only to read-write for workspace directories

#### Fixed typo in `gh aw mcp inspect` command help text (interacts → interact)

#### Optimize prompt files loading


## v0.31.0 - 2025-11-25

### Features

#### Change strict mode default from false to true

**⚠️ Breaking Change**: Strict mode is now enabled by default for all agentic workflows.

**What changed:**
- Workflows without an explicit `strict:` field now default to `strict: true` (previously `false`)
- The JSON schema default for the `strict` field is now `true`
- Compiler applies schema default when `strict:` is not specified in frontmatter

**Migration guide:**
- **No action needed** if your workflows already comply with strict mode requirements
- **Add `strict: false`** to workflows that need to opt out of strict mode validation
- **Review security** implications before disabling strict mode in production workflows

**Strict mode enforces:**
1. No write permissions (`contents:write`, `issues:write`, `pull-requests:write`) - use safe-outputs instead
2. Network configuration - requires network permissions (defaults to 'defaults' mode if not specified)
3. No wildcard `*` in network.allowed domains
4. Network configuration required for custom MCP servers with containers
5. GitHub Actions pinned to commit SHAs (not tags/branches)
6. No deprecated frontmatter fields

**Examples:**
```yaml
# Default behavior (strict mode enabled)
on: issues
permissions:
  contents: read

# Explicitly disable strict mode
on: issues
permissions:
  contents: write  # Would fail in strict mode
strict: false      # Opt out
```text

**CLI behavior update:**
- `gh aw compile` now uses strict mode by default when `strict:` is not specified in frontmatter (due to schema default)
- `gh aw compile --strict` still overrides frontmatter settings to force strict mode on all workflows
- To opt out of strict mode, add `strict: false` to your workflow frontmatter

See [Strict Mode Documentation](https://github.github.com/gh-aw/reference/frontmatter/#strict-mode-strict) for details.


## v0.30.5 - 2025-11-24

### Bug Fixes

#### Add built-in Serena MCP support with language service integration and Go configuration options

#### Refactor safe output validation: centralize config defaults and eliminate duplicated validation code

#### Extract duplicate safe-output validation logic into shared helpers

Extracted ~120 lines of identical validation logic from `add_labels.cjs` and `add_reviewer.cjs` into a new `safe_output_helpers.cjs` module. The new module provides three reusable functions: `parseAllowedItems()`, `parseMaxCount()`, and `resolveTarget()`, reducing code duplication and improving maintainability.

#### Refactor safe_outputs_mcp_server.cjs: Extract utility functions with bundling and size logging

Extracted 7 utility functions from the monolithic safe_outputs_mcp_server.cjs file into separate, well-tested modules with automatic bundling. Added 50 comprehensive unit tests, detailed size logging during bundling, and fixed MCP server script generation bug. All functionality preserved with no breaking changes.

#### Refactor patch generation from workflow step to MCP server

Moves git patch generation from a dedicated workflow step to the safe-outputs MCP server, where it executes when `create_pull_request` or `push_to_pull_request_branch` tools are called. This provides immediate error feedback when no changes exist, rather than discovering it later in processing jobs.


## v0.30.4 - 2025-11-22

Maintenance release with dependency updates and minor improvements.

## v0.30.3 - 2025-11-22

### Bug Fixes

#### Add shared safe-output-app workflow for repository-level GitHub App authentication


## v0.30.2 - 2025-11-21

### Bug Fixes

#### Fix aw.patch generation logic to handle local commits

Fixed a bug where patch generation only captured commits from explicitly named branches. When an LLM makes commits directly to the currently checked out branch during action execution, those commits are now properly captured in the patch file. Added HEAD-based patch generation as a fallback strategy and extensive logging throughout the patch generation process.


## v0.30.1 - 2025-11-20

### Bug Fixes

#### Update CLI tool versions to latest releases: Claude Code 2.0.44, GitHub MCP Server v0.21.0

#### Add noop safe output for transparent workflow completion

Agents need to emit human-visible artifacts even when no actions are required (e.g., "No issues found"). The noop safe output provides a fallback mechanism ensuring workflows never complete silently.

#### Move noop processing step from separate job into conclusion job

#### Update smoke test workflows to use add-comment and add comprehensive capability testing


## v0.30.0 - 2025-11-18

### Features

#### Strict mode now refuses deprecated schema fields instead of only warning


### Bug Fixes

#### Add actionable hints to strict mode validation errors

Strict mode validation errors now include security rationale and safe alternatives. Error messages explain WHY restrictions exist and HOW to achieve goals safely, with links to documentation and suggestions for safe-outputs alternatives.

#### Add update tool to MCP server

Exposes the `gh aw update` command through the MCP protocol with essential update flags (workflows, major, force).

#### Use GitHub API for lock file timestamp checks instead of repository checkout

#### Prevent workflow_run triggers from executing in forked repositories

#### Standardize MCP command arguments to workflow-id-or-file


### Migration Guide

`````markdown
The following breaking changes require code updates:

### Strict mode now refuses deprecated schema fields instead of only warning

If you are using `--strict` mode and have workflows with deprecated fields, you will need to update them before compilation succeeds.

For example, if you have:

```yaml
timeout_minutes: 30
```text

Update to the recommended replacement:

```yaml
timeout-minutes: 30
```text

Check the error messages when running `gh aw compile --strict` for specific replacement suggestions for each deprecated field. Non-strict mode continues to work with deprecated fields (showing warnings only).
`````

## v0.29.1 - 2025-11-15

Maintenance release with dependency updates and minor improvements.

## v0.29.0 - 2025-11-15

### Features

#### Add new feature description


## v0.28.7 - 2025-11-14

### Bug Fixes

#### Update GitHub MCP Server version to v0.20.2


## v0.28.6 - 2025-11-07

Maintenance release with dependency updates and minor improvements.

## v0.28.5 - 2025-11-06

### Bug Fixes

#### Use JavaScript for prompt variable interpolation instead of shell expansion

The compiler now uses `actions/github-script` to interpolate GitHub Actions expressions in prompts, replacing the previous shell expansion approach. This improves security by using literal shell variables and adds a dedicated JavaScript interpolation step after prompt creation.


## v0.28.4 - 2025-11-06

Maintenance release with dependency updates and minor improvements.

## v0.28.3 - 2025-11-06

### Bug Fixes

#### Fix SC2086: Quote ${GITHUB_WORKSPACE} in generated workflow steps


## v0.28.2 - 2025-11-05

### Bug Fixes

#### Add Daily Ops Pattern Documentation

#### Finalize env_var support for Codex MCP - all tests passing

#### Fix agent file validation to handle relative paths correctly

#### Update CLI versions

#### Use fallback tokens for resolution


## v0.28.1 - 2025-11-05

### Bug Fixes

#### Add changeset generator agent workflow for automatic changeset creation

#### Add --no-gitattributes flag to add command and update gitattributes by default

#### Improve error handling in CLI


## v0.28.0 - 2025-11-04

### Features

#### Replace engine.custom-agent field with imports-based agent files

#### Remove safe output "min" field


### Bug Fixes

#### Add `jqschema` utility script to agent context

#### Add comprehensive tests for console renderSlice function (+56.2% function coverage, +0.1% overall)

#### Convert .prompt.md files to custom agent format

#### Convert shell script extraction prompt to custom agent format

#### Fix copilot-session-insights workflow error handling for missing gh agent-task extension

#### Implement JavaScript bundler with on-demand bundling and caching for embedded sources

#### Refactor SanitizeIdentifier to use unified SanitizeName

#### Refactor SanitizeWorkflowName to use unified SanitizeName

#### Remove redundant agent version capture step, use installation version

#### Add unified SanitizeName function with configurable options


## v0.27.0 - 2025-10-31

### Features

#### Add top-level `agent` field to engine configuration for copilot, claude, and codex

#### Remove permissions over-provisioning validation


### Bug Fixes

#### Add comprehensive tests for frontmatter extraction utilities (+0.1% coverage)

#### Add JSON formatting to linting workflow

#### Add --mcp flag to init command for Copilot Agent MCP configuration

#### Add schema descriptions for runs-on and concurrency fields

#### Download full PR data with all fields for clustering analysis

#### Fix changeset automatic branch resolution failure

#### Fix github.workflow expression validation in comparison operators

#### Add GitHub tool/toolset validator for allowed tools configuration

#### Prettify permissions validation error messages

#### Refactor action pins to embedded JSON with on-demand unmarshaling and sorted output

#### Refactor duplicate MCP config loading logic into shared helper function

#### Add permissions support to shared workflows with validation

#### Set ubuntu-slim as default image for safe outputs workflows


## v0.26.0 - 2025-10-29

### Features

#### Update GitHub MCP Server to v0.20.0 with consolidated tool API


### Bug Fixes

#### Add diagnostic logging to patch generation process

#### Add 10KB output size guardrail to MCP server logs command

#### Add JSON schema examples for safe-outputs configuration

#### Add JSON schema examples for permissions, engine, and network properties

#### Add pull_request trigger with "smoke" label filter to smoke test workflows

#### Add comprehensive tests for utility functions (+0.1% coverage)

#### Add JSON schema examples for workflow properties (name, description, source, imports, on, timeout_minutes, strict, if, steps, post-steps, env, concurrency, run-name, cache)

#### Consolidate duplicate formatNumber() implementations into single shared console.FormatNumber() function

#### Extract duplicate shortenCommand() to shared utility function

#### Fix template injection vulnerability in git config user.name

#### Fix prompt-clustering-analysis workflow to download logs

#### Parallelized git ls-remote test for 70% faster execution

#### Pin gh-aw-firewall version to default when not explicitly specified

#### Add pinned actions manifest to lock file headers

#### Reduce log parsing memory allocations by 23%

#### Refactor: Extract duplicate Copilot token handling logic

#### Replace persist-credentials with explicit git re-authentication

#### Update CLI versions: Claude Code 2.0.28, Copilot 0.0.352, GitHub MCP Server v0.20.1

#### Update documentation for features from 2025-10-28


## v0.25.2 - 2025-10-26

### Bug Fixes

#### Improve create-agentic-workflow prompt with writing style guidelines and user engagement tips


## v0.25.1 - 2025-10-26

### Bug Fixes

#### Centralize git utility functions in git.go

#### Consolidate duplicate sanitizeWorkflowName into pkg/workflow/strings.go

#### Fix: Disable container validation by default, require --validate flag

#### Increase max body size to 65000 for issues, comments, PRs, and discussions

#### Update scheduled reporting workflows to use create-discussion for Audits

#### Add workflow run URL formatting guidelines to reporting shared workflow


## v0.25.0 - 2025-10-26

### Features

#### Add pip and golang dependency manifest generation support

#### Update logs command to show combined errors/warnings table with cleaner formatting

#### Remove per-tool Squid proxy - unify network filtering at workflow level


### Bug Fixes

#### Add configuration examples for `jobs`, `mcp-servers`, and `command` frontmatter fields

#### Add copy button for dictation instructions in documentation

#### Add discussions toolset to GitHub tool in plan.md workflow

#### Add firewall analysis section to audit agent reports

#### Add firewall version of changeset-generator workflow

#### Add configurable log-level field to firewall configuration

#### Add firewall agentic workflow demonstrating network permission enforcement

#### Add github-token secret validation

#### Add firewall version of research workflow


#### Fix audit command to cache downloads and prevent duplicate artifact fetches

#### Cache compiled JSON schemas to improve compilation speed

#### Review and centralize network configuration under top level field

#### Clarify branch name requirement for push pull request tool

#### Configure network firewall, edit, and bash tools for scheduled Copilot workflows

#### Consolidate generic validation functions into validation.go

#### Add daily firewall logs collector and reporter workflow

#### Document AWF firewall feature and reference gh-aw-firewall repository

#### Expand GitHub toolsets documentation in agent instructions

#### Extend action SHA pinning to custom steps and imported jobs

#### Extend action SHA pinning to custom steps and imported jobs

#### Extend secret redaction to .md, .mdx, .yml, .jsonl files

#### Extract extraction functions from compiler.go to frontmatter_extraction.go

#### Refactor: Extract duplicate GitHub MCP remote config rendering into shared helper

#### Extract job building logic from compiler.go to compiler_jobs.go

#### Extract YAML generation logic from compiler.go to compiler_yaml.go

#### Fixed permission denied error when moving Copilot logs in firewall mode

#### Fixed silent failure when agentic workflow hits max-turns limit - now raises clear error message

#### Fix nested quoting in awf compiler shell command generation

#### Fix npx command parsing to support --yes and -y flags

#### Fix relative time precision loss in logs command date filtering

#### Fix push to pull request failure by ensuring safe-outputs directory creation

#### Fixed test expectation for safe outputs MCP server name

#### Add support for silently ignoring description and applyTo fields in frontmatter

#### Merge engine_helpers.go into engine_shared_helpers.go and rename to engine_helpers.go

#### Merge network allowed domains from imported workflow files

#### Move generateGitConfigurationSteps from git.go to yaml_generation.go

#### Move validation functions from compiler.go to validation.go

#### Add --parse support for firewall logs in logs and audit commands

#### Add firewall log parser and step summary generation

#### Pin all GitHub Actions to specific commit SHAs in generated workflows for enhanced security and reproducibility

#### Reduced compile command verbose output noisiness by converting internal messages to logger calls

#### Rename safe-outputs MCP server identifier to "safeoutputs", update file paths, and improve user-facing text

#### Rename "Upload Squid Logs" step to "Upload Firewall Logs"

#### Replace always() with !cancelled() in safe-output job conditions

#### Update cli-version-checker workflow to use copilot engine

#### Update CLI versions: Claude Code 2.0.25, Copilot 0.0.349, GitHub MCP Server v0.19.1


## v0.24.0 - 2025-10-23

### Features

#### Add support for `bash: true` as shortcut for `bash: "*"` and `bash: false` to disable bash tool

#### Add support for feature flags in frontmatter via "features" field

#### Remove "defaults" section from main JSON schema

#### Remove GITHUB_TOKEN fallback for Copilot operations

This is a breaking change. The default `secrets.GITHUB_TOKEN` fallback has been removed from Copilot-related operations (create-agent-task, assigning Copilot to issues, and adding Copilot as PR reviewer) because it lacks the required permissions, causing silent failures.

Users must now configure a Personal Access Token (PAT) as either `COPILOT_GITHUB_TOKEN` or `GH_AW_GITHUB_TOKEN` secret to use these features. Enhanced error messages now guide users to proper configuration when authentication or permission errors occur.

**Note**: As of v0.26+, the `GH_AW_COPILOT_TOKEN` secret is no longer supported. Use `COPILOT_GITHUB_TOKEN` instead.


### Bug Fixes

#### Add safe output messages design system documentation

#### Remove bloat from MCP Server documentation

#### Update duplicate finder workflow to create individual issues per pattern (max 3)

#### Fix heredoc delimiter collision causing workflow compilation failures

#### Fixed $ref resolution in schema documentation generator for frontmatter-full.md

#### Add internal compiler check for secret redaction step ordering

#### Refactor duplicate MCP code patterns for improved maintainability

#### Remove deprecated "claude" top-level field from workflow schema


## v0.23.2 - 2025-10-22

### Bug Fixes

#### Add commit-changes-analyzer agentic workflow

#### Add agentic workflow for enhancing Go files with debug logging

#### Add jsDoc type checking support to JavaScript .cjs files

#### Add reporting instructions for HTML details/summary formatting in report workflows

#### Add documentation page for GitHub Actions status badges

#### Consolidate permission parsing logic and add support for "all: read" syntax

#### Fix discussion comment replies to use threading instead of creating new top-level comments

#### Refactor duplicate MCP configuration rendering logic into shared helper

#### Remove js-yaml dependency in badge generator script

#### Add semantic function refactoring workflow for Go code analysis

#### Improved clarity and conciseness of imports reference documentation


## v0.23.1 - 2025-10-21

### Bug Fixes

#### Fix tidy workflow: Add search_pull_requests permission to GitHub MCP tools

#### Update CLI versions: Claude Code 2.0.24, GitHub Copilot CLI 0.0.347


## v0.23.0 - 2025-10-21

### Features

#### Fix add_comment to auto-detect discussion context and use GraphQL API

**Enhancement**: The `discussion: true` configuration option is now optional with automatic context detection. The add_comment safe-output automatically detects discussion contexts from event types (`discussion` or `discussion_comment`) and uses the appropriate GraphQL API without requiring explicit configuration. The `discussion: true` field remains supported for backward compatibility and can still be used when manual specification is preferred. Existing workflows using this field continue to work without modification.


### Bug Fixes

#### Add agent column to logs output and update table header style

#### Add --add-dir /tmp/gh-aw/agent/ to copilot engine default arguments

#### Add environment variables for comment referencing created items

#### Add continuation field to logs JSON output when timeout is reached

#### Add create-agent-task safe output for GitHub Copilot agent tasks

#### Add run summary caching to logs command for faster reprocessing

#### Add X-MCP-Toolsets header support for remote GitHub MCP configuration

#### Add --parse option to audit command

#### Add success handling to update_reaction job

#### Add command position validation for command triggers to prevent accidental execution

#### Fix copilot assignment for issue assignees and PR reviewers

#### Fix Copilot log parser: accumulate token usage and improve JSON block detection

#### Fix reaction condition to include discussion and discussion_comment events

#### Fixed logs command not returning the requested number of workflow runs when no workflow name is specified

#### Fix logs command parameter naming confusion in pagination function

#### Add automatic file writing for large MCP tool outputs exceeding 16,000 tokens with compact schema descriptions

#### Add progress indicator to workflow runs fetching spinner

#### Reduce error patterns from 68 to 25 (63% reduction)

#### Refactor: Eliminate duplicate safe output env logic in workflow helpers

#### Update GitHub MCP Server to v0.19.0 and add "default" toolset support


## v0.22.10 - 2025-10-19

### Bug Fixes

#### Add reviewers field to create-pull-request safe output

#### Add schema documentation generator for frontmatter reference

#### Extract duplicate step formatting code from Copilot and Codex engines into shared helper function

#### Fix copilot PR search logic in audit workflows

#### Fixed missing GH_TOKEN in workflow install step causing gh-aw extension installation failures

#### Fix jqschema.sh: ensure /tmp/gh-aw directory exists before writing

#### Fixed logs command missing tool detection functionality

#### Limit copilot activity analysis historical data rebuild to max 1 week and add gh CLI data pre-fetch

#### Reduce bloat in research-planning.md documentation (44% reduction)

#### Reduced bloat in triggers.md documentation (21% reduction)

#### Add strongly typed Permissions struct for GitHub Actions permissions

#### Update GitHub Copilot CLI to v0.0.345

#### Update GitHub Copilot CLI to v0.0.346


## v0.22.9 - 2025-10-18

### Bug Fixes

#### Add assignees support to create_issue safe-output job

#### Add installation instructions and usage tips for ffmpeg


## v0.22.8 - 2025-10-18

### Bug Fixes

#### Add support for top-level `github-token` configuration in workflow frontmatter

#### Updated compiler to upload prompt as an artifact after prompt generation

#### Add default concurrency pattern for non-special-case workflows

#### Fixed Copilot CLI log parser to extract and display tools from new debug format

#### Fixed HTTP MCP header escaping for copilot engine JSON config to properly escape secret expressions

#### Fix push_to_pull_request_branch to use agent_output.json artifact instead of direct JSON output

#### Fix upload_assets job to read agent output from file instead of JSON string

#### Place workflow run logs in workflow-logs subdirectory for cleaner organization

#### Refactor duplicate MCP config builders to use shared helpers

#### Update Claude Code to 2.0.22 and Copilot CLI to 0.0.344, add --disable-builtin-mcps flag

#### Verified Codex 0.47.0 upgrade completion - no changes needed


## v0.22.7 - 2025-10-17

### Bug Fixes

#### Fixed GH_AW_AGENT_OUTPUT file path handling in safe output scripts


## v0.22.6 - 2025-10-17

### Bug Fixes

#### Updated copilot engine to use --allow-all-paths flag instead of --add-dir / for edit tool support

#### Enable reaction comments for all workflows, not just command-triggered ones

#### Fixed post-steps indentation in generated YAML workflows to match GitHub Actions schema requirements

#### Fixed empty GH_AW_AGENT_OUTPUT in safe output jobs by downloading agent_output.json artifact instead of relying on job outputs

#### Fix Windows path separator issue in workflow resolution

#### Refactored prompt-step generator methods to eliminate duplicate code by introducing shared helper functions, reducing code by 33% while maintaining identical functionality

#### Refactor: Eliminate duplicate code and organize validation into npm.go and pip.go

#### Separate default list of GitHub tools for local and remote servers

#### Sort mermaid graph nodes alphabetically for stable code generation

#### Remove bloat from packaging-imports.md guide (56% reduction)

#### Add update_reaction job to update activation comments when agent fails without producing output

#### Update Claude Code to 2.0.21 and GitHub Copilot CLI to 0.0.343


## v0.22.5 - 2025-10-16

### Bug Fixes

#### Update add_reaction job to always create new comments and add comment-repo output

#### Fix version display in release binaries to show actual release tag instead of "dev"


## v0.22.4 - 2025-10-16

### Bug Fixes

#### Add generic timeout field for tools configuration. Allows configuring operation timeouts (in seconds) for tool/MCP communications in agentic engines. Supports Claude, Codex, and Copilot engines with a unified 60-second default timeout.

#### Add GH_AW_GITHUB_TOKEN secret check for GitHub remote mode in mcp inspect

#### Add GH_AW_ASSETS_BRANCH normalization for upload-assets safe output

#### Reduced bloat in cache-memory documentation (56% reduction)

#### Extract duplicate custom engine step handling into shared helper functions

#### Refactor prompt-step generation to eliminate code duplication by introducing shared helper functions


## v0.22.3 - 2025-10-16

### Bug Fixes

#### Add mcp-inspect tool to mcp-server command with automatic secret validation

#### Add yq to default bash tools

#### Merge check_membership and stop-time jobs into unified pre-activation job

#### Update CLI versions: Claude Code 2.0.15→2.0.19, GitHub Copilot CLI 0.0.340→0.0.342


## v0.22.2 - 2025-10-16

### Bug Fixes

#### Update add command to resolve agentic workflow file from .github/workflows folder

#### Use HTML details/summary for threat detection prompt in step summary

#### Fixed skipped tests in compiler_test.go for MCP format migration

#### Add HTTP MCP header secret support for Copilot engine


## v0.22.1 - 2025-10-15

### Bug Fixes

#### Add Mermaid graph generation to compiled workflow lock file headers

#### Fixed safe outputs MCP server to return stringified JSON results for Copilot CLI compatibility

#### Add strict mode validation for bash tool wildcards and update documentation


## v0.22.0 - 2025-10-15

### Features

#### Add builtin "agentic-workflows" tool for workflow introspection and analysis

Adds a new builtin tool that enables AI agents to analyze GitHub Actions workflow traces and improve workflows based on execution history. The tool exposes the `gh aw mcp-server` command as an MCP server, providing agents with four powerful capabilities:

- **status** - Check compilation status and GitHub Actions state of all workflows
- **compile** - Programmatically compile markdown workflows to YAML
- **logs** - Download and analyze workflow run logs with filtering options
- **audit** - Investigate specific workflow run failures with detailed diagnostics

When enabled in a workflow's frontmatter, the tool automatically installs the gh-aw extension and configures the MCP server for all supported engines (Claude, Copilot, Custom, Codex). This enables continuous workflow improvement driven by AI analysis of actual execution data.

#### Add container image and runtime package validation to --validate flag

Enhances the `--validate` flag to perform additional validation checks beyond GitHub Actions schema validation:

- **Container images**: Validates Docker container images used in MCP configurations are accessible
- **npm packages**: Validates packages referenced with `npx` exist on the npm registry
- **Python packages**: Validates packages referenced with `pip`, `pip3`, `uv`, or `uvx` exist on PyPI

The validator provides early detection of non-existent Docker images, typos in package names, and missing dependencies during compilation, giving immediate feedback to workflow authors before runtime failures occur.

#### Add secret validation steps to agentic engines (Claude, Copilot, Codex)

Added secret validation steps to all agentic engines to fail early with helpful error messages when required API secrets are missing. This includes new helper functions `GenerateSecretValidationStep()` and `GenerateMultiSecretValidationStep()` for single and multi-secret validation with fallback logic.

#### Clear terminal in watch mode before recompiling

Adds automatic terminal clearing when files are modified in `--watch` mode, improving readability by removing cluttered output from previous compilations. The new `ClearScreen()` function uses ANSI escape sequences and only clears when stdout is a TTY, ensuring compatibility with pipes, redirects, and CI/CD environments.


### Bug Fixes

#### Add "Downloading container images" step to predownload Docker images used in MCP configs

#### Add shared agentic workflow for Microsoft Fabric RTI MCP server

Adds a new shared MCP workflow configuration for the Microsoft Fabric Real-Time Intelligence (RTI) MCP Server, enabling AI agents to interact with Fabric RTI services for data querying and analysis. The configuration provides access to Eventhouse (Kusto) queries and Eventstreams management capabilities.

#### Add if condition to custom safe output jobs to check agent output

Custom safe output jobs now automatically include an `if` condition that checks whether the safe output type (job ID) is present in the agent output, matching the behavior of built-in safe output jobs. When users provide a custom `if` condition, it's combined with the safe output type check using AND logic.

#### Add documentation for init command to CLI docs

#### Clarify that edit: tool is required for writing to files

Updated documentation in instruction files to explicitly state that the `edit:` tool is required when workflows need to write to files in the repository. This helps users understand they must include this tool in their workflow configuration to enable file writing capabilities.

#### Treat container image validation as warning instead of error

Container image validation failures during `compile --validate` are now treated as warnings instead of errors. This prevents compilation from failing due to local Docker authentication issues or private registry access problems, while still informing users about potential container validation issues.

#### Fix patch generation to handle underscored safe-output type names

The patch generation script now correctly searches for underscored type names (`push_to_pull_request_branch`, `create_pull_request`) to match the format used by the safe-outputs MCP server. This fixes a mismatch that was causing the `push_to_pull_request_branch` safe-output job to fail when looking for the patch file.

#### Update q.md workflow to use MCP server tools instead of CLI commands

The Q agentic workflow was incorrectly referencing the `gh aw` CLI command, which won't work because the agent doesn't have GitHub token access. Updated all references to explicitly use the gh-aw MCP server's `compile` tool instead.

#### Pretty print unauthorized expressions error message with line breaks

When compilation fails due to unauthorized expressions, the error message now displays each expression on its own line with bullet points, making it much easier to read and identify which expressions are valid. Previously, all expressions were displayed in a single long line that was difficult to scan.

#### Use --allow-all-tools flag for bash wildcards in Copilot engine

#### Optimize changeset-generator workflow for token efficiency

#### Reduce bloat in CLI commands documentation by 72%

Dramatically reduced bloat in CLI commands documentation from 803 lines to 224 lines while preserving all essential information. Removed excessive command examples, redundant explanations, duplicate information, and verbose descriptions to improve documentation clarity and scannability.

#### Reduced bloat in packaging-imports.md documentation (58% reduction)

#### Remove detection of missing tools using error patterns

Removed fragile error pattern matching logic that attempted to detect missing tools from log parsing infrastructure. This detection is now the exclusive responsibility of coding agents. Cleaned up 569 lines of code across Claude, Copilot, and Codex engine implementations while maintaining all error pattern functionality for legitimate use cases (counting and categorizing errors/warnings).

#### Update tool call rendering to include duration and token size

Enhanced log parsers for Claude, Codex, and Copilot to display execution duration and approximate token counts for tool calls. Adds helper functions for token estimation (using chars/token formula) and human-readable duration formatting to provide better visibility into tool execution performance and resource usage.

#### Add Playwright, upload-assets, and documentation build pipeline to unbloat-docs workflow

#### Update Claude Code to version 2.0.15


## v0.21.0 - 2025-10-14

### Features

#### Add support for discussion and discussion_comment events in command trigger

The command trigger now recognizes GitHub Discussions events, allowing agentic workflows to respond to `/mention` commands in discussions just like they do for issues and pull requests. This includes support for both `discussion` (when a discussion is created or edited) and `discussion_comment` (when a comment on a discussion is created or edited) events.

#### Add discussion support to add_reaction_and_edit_comment.cjs

The workflow script now supports GitHub Discussions events (`discussion` and `discussion_comment`), enabling agentic workflows to add reactions and comments to discussions. This extends the existing functionality that previously only supported issues and pull requests. The implementation uses GraphQL API for all discussion operations and includes comprehensive test coverage.

#### Add entrypointArgs field to container-type MCP configuration

This adds a new `entrypointArgs` field that allows specifying arguments to be added after the container image in Docker run commands. This provides greater flexibility when configuring containerized MCP servers, following the standard Docker CLI pattern where arguments can be placed before the image (via `args`) or after the image (via `entrypointArgs`).

#### Extract and display premium model information and request consumption from Copilot CLI logs

Enhanced the Copilot log parser to extract and display premium request information from agent stdio logs. Users can now see which AI model was used, whether it requires a premium subscription, any cost multipliers that apply, and how many premium requests were consumed. This information is now surfaced directly in the GitHub Actions step summary, making it easily accessible without needing to download and manually parse log files.

#### Add --json flag to logs command for structured JSON output

Reorganized the logs command to support both JSON and console output formats using the same structured data collection approach. The implementation follows the architecture pattern established by the audit command, with structured data types (LogsData, LogsSummary, RunData) and separate rendering functions for JSON and console output. The MCP server logs tool now also supports the --json flag with jq filtering capabilities.

#### Add support for multiple cache-memory configurations with array notation and optional descriptions

Implemented support for multiple cache-memory configurations with a simplified, unified array-based structure. This feature allows workflows to define multiple caches using array notation, each with a unique ID and optional description. The implementation maintains full backward compatibility with existing single-cache configurations (boolean, nil, or object notation).

Key features:
- Unified array structure for all cache configurations
- Support for multiple caches with explicit IDs
- Optional description field for each cache
- Backward compatibility with existing workflows
- Smart path handling for single cache with ID "default"
- Duplicate ID validation at compile time
- Import support for shared workflows

#### Reorganize audit command with structured output and JSON support

Added `--json` flag to the audit command for machine-readable output. Enhanced audit reports with comprehensive information including per-job durations, file sizes with descriptions, and improved error/warning categorization. Updated MCP server integration to use JSON output for programmatic access.

Key improvements:
- New `--json` flag for structured JSON output
- Per-job duration tracking from GitHub API
- Enhanced file information with sizes and intelligent descriptions
- Better error and warning categorization
- Dual rendering: human-readable console tables or machine-readable JSON
- MCP server now returns structured JSON instead of console-formatted text

#### Update status command JSON output structure

The status command with --json flag now:
- Replaces `agent` field with `engine_id` for clarity
- Removes `frontmatter` and `prompt` fields
- Adds `on` field from workflow frontmatter to show trigger configuration

#### Add workflow run logs download and extraction to audit/logs commands

The `gh aw logs` and `gh aw audit` commands now automatically download and extract GitHub Actions workflow run logs in addition to artifacts, providing complete audit trail information by including the actual console output from workflow executions. The implementation includes security protection against zip slip vulnerability and graceful error handling for missing or expired logs.


### Bug Fixes

#### Add Datadog MCP shared workflow configuration

Adds a new shared Datadog MCP server configuration at `.github/workflows/shared/mcp/datadog.md` that enables agentic workflows to interact with Datadog's observability and monitoring platform. The configuration provides 10 tools for comprehensive Datadog access including monitors, dashboards, metrics, logs, events, and incidents with container-based deployment and multi-region support.

#### Add JSON schema helper for MCP tool outputs

Implements a reusable `GenerateOutputSchema[T]()` helper function that generates JSON schemas from Go structs using `github.com/google/jsonschema-go`. Enhanced MCP tool documentation by inlining schema information in tool descriptions for better LLM discoverability. Added comprehensive unit and integration tests for schema generation.

#### Add Sentry MCP Integration for Agentic Workflows (Read-Only)

Adds comprehensive Sentry MCP integration to enable agentic workflows to interact with Sentry for application monitoring and debugging. The integration provides 14 read-only Sentry tools including organization/project management, release management, issue/event analysis, AI-powered search, and documentation access. Configuration is available as a shared MCP setup at `.github/workflows/shared/mcp/sentry.md` that can be imported into any workflow for safe, non-destructive monitoring operations.

#### Add SST OpenCode shared agentic workflow and smoke test

Added support for SST OpenCode as a custom agentic engine with:
- Shared workflow configuration at `.github/workflows/shared/opencode.md`
- Smoke test workflow for validation
- Test workflow example
- Documentation for customizing environment variables (agent version and AI model)
- Simplified 2-step workflow (Install and Run) with direct prompt reading

#### Apply struct-based rendering to status command

Refactored the `status` command to use the struct tag-based console rendering system, following the guidelines in `.github/instructions/console-rendering.instructions.md`. The change reduces code duplication by eliminating manual table construction and improves maintainability by defining column headers once in struct tags. JSON output continues to work exactly as before.

#### Remove bloat from coding-development.md documentation

Cleaned up the coding and development workflows documentation by eliminating repetitive bullet structures and converting 12 bullet points to concise prose descriptions. This change preserves all essential information while reducing the file size by 35% and improving readability.

#### Extract shared engine installation and permission error helpers

Refactors engine-specific implementations to eliminate ~165 lines of duplicated code by extracting shared installation scaffolding and permission error handling into reusable helper functions. Creates `BuildStandardNpmEngineInstallSteps()` and permission error detection helpers, maintaining backward compatibility with no breaking changes.

#### Fix logs command to fetch all runs when date filters are specified

The `logs` command's `--count` parameter was limiting the number of logs downloaded, not the number of matching logs returned after filtering. This caused incomplete results when using date filters like `--start-date -24h`.

Modified the algorithm to always limit downloads inline based on remaining count needed, ensuring the count parameter correctly limits the final output after applying all filters. Also increased the default count from 20 to 100 for better coverage and updated documentation to clarify the behavior.

#### Fix threat detection CLI overflow by using file access instead of inlining agent output

The threat detection job was passing the entire agent output to the detection agent via environment variables, which could cause CLI argument overflow errors when the agent output was large. Modified the threat detection system to use a file-based approach where the agent reads the output file directly using bash tools (cat, head, tail, wc, grep, ls, jq) instead of inlining the full content into the prompt.

#### Fix: Add setup-python dependency for uv tool in workflow compilation

The workflow compiler now correctly adds the required `setup-python` step when the `uv` tool is detected via MCP server configurations. Previously, the runtime detection system would skip all runtime setup when ANY setup action existed in custom steps, causing workflows using `uv` or `uvx` commands to fail.

The fix refactors runtime detection to:
- Always run runtime detection and process all sources
- Automatically inject Python as a dependency when uv is detected
- Selectively filter out only runtimes that already have setup actions, rather than skipping all detection

#### Add GitHub Actions workflow commands error pattern detector

Adds support for detecting common GitHub Actions workflow command error syntax (::error, ::warning, ::notice) across all agentic engines. This improves error detection for GitHub Actions workflows by recognizing standard workflow command formats.

#### Merge "Create prompt" and "Print prompt to step summary" workflow steps

Consolidates the prompt generation workflow by moving the "Print prompt to step summary" step to appear immediately after prompt creation, making the workflow more logical and easier to understand. The functionality remains identical - this is purely a reorganization for better code structure.

#### Fix Copilot MCP configuration tools field population

Updates the `renderGitHubCopilotMCPConfig` function to correctly populate the "tools" field in MCP configuration based on allowed tools from the configuration. Adds helper function `getGitHubAllowedTools` to extract allowed tools and defaults to `["*"]` when no allowed list is specified.

#### Refactor: Extract duplicate safe-output environment setup logic into helper functions

Extracted duplicated safe-output environment setup code from multiple workflow engines and job builders into reusable helper functions in `pkg/workflow/safe_output_helpers.go`. This eliminates ~123 lines of duplicated code across 4 engine implementations and 5 safe-output job builders, improving maintainability and consistency while maintaining 100% backward compatibility.

#### Remove workflow cancellation API calls from compiler

The compiler no longer uses the GitHub Actions cancellation API. Workflow cancellation is now handled through job dependencies and `if` conditions, resulting in a cleaner architecture. This removes the need for `actions: write` permission in the `add_reaction` job and eliminates 125 lines of legacy code.

#### Rename check-membership job to check_membership with constant

Refactored the check-membership job name to use underscores (check_membership) for consistency with Go naming conventions. Introduced CheckMembershipJobName constant in constants.go to centralize the job name and eliminate hardcoded strings throughout the codebase. Updated all references including step IDs, job dependencies, step outputs, tests, and recompiled all workflow files.

#### Add rocket reaction to Q workflow

Changed the Q agentic workflow optimizer to use a rocket emoji (🚀) reaction instead of the default "eyes" (👀) reaction when triggered via `/q` comments. The rocket emoji better represents Q's mission as a workflow optimizer and performance enhancer.

#### Replace channel_id input with GH_AW_SLACK_CHANNEL_ID environment variable in Slack shared workflow

Updates the Slack shared workflow to use a required environment variable `GH_AW_SLACK_CHANNEL_ID` instead of accepting the channel ID as a `channel_id` input parameter. This simplifies the interface and aligns with best practices for configuration management. Workflows using the Slack shared workflow will need to set `GH_AW_SLACK_CHANNEL_ID` as an environment variable or repository variable instead of passing `channel_id` as an input.

#### Refactor logs command to use struct-based console rendering system

Updated the logs command to use the same struct-based rendering approach as the audit command, improving code maintainability and consistency. All data structures now use unified types for both console and JSON output with proper struct tags.

#### Add temporary folder usage instructions to agentic workflow prompts

Agentic workflows now include explicit instructions for AI agents to use `/tmp/gh-aw/agent/` for temporary files instead of the root `/tmp/` directory. This improves file organization and prevents conflicts between workflow runs.

#### Update GitHub Copilot CLI to version 0.0.340 and implement ${} syntax for MCP environment variables

This update upgrades the GitHub Copilot CLI from version 0.0.339 to 0.0.340 and implements the breaking change for MCP server environment variable configuration. The safe-outputs MCP server now uses the new `${VAR}` syntax for environment variable references instead of direct variable names.


## v0.20.0 - 2025-10-12

### Features

#### Add --json flag to status command and jq filtering to MCP server

Adds new command-line flags to the status command:
- `--json` flag renders the entire output as JSON
- Optional `jq` parameter allows filtering JSON output through jq tool

The jq filtering functionality has been refactored into dedicated files (jq.go) with comprehensive test coverage.


### Bug Fixes

#### Fix content truncation message priority in sanitizeContent function

Fixed a bug where the `sanitizeContent` function was applying truncation checks in the wrong order. When content exceeded both line count and byte length limits, the function would incorrectly report "Content truncated due to length" instead of the more specific "Content truncated due to line count" message. The truncation logic now prioritizes line count truncation, ensuring users get the most accurate truncation message based on which limit was hit first.

#### Fix HTTP transport usage of go-sdk

Fixed the MCP server HTTP transport implementation to use the correct `NewStreamableHTTPHandler` API from go-sdk instead of the deprecated SSE handler. Also added request/response logging middleware and changed configuration validation errors to warnings to allow server startup in test environments.

#### Fix single-file artifact directory nesting in logs command

When downloading artifacts with a single file, the file is now moved to the parent directory and the unnecessary nested folder is removed. This implements the "artifact unfold rule" which simplifies artifact access by removing unnecessary nesting for single-file artifacts while preserving multi-file artifact directories.

#### Update MCP server workflow for toolset comparison with cache-memory

Enhanced the github-mcp-tools-report workflow to track and compare changes to the GitHub MCP toolset over time. Added cache-memory configuration to enable persistent storage across workflow runs, allowing the workflow to detect new and removed tools since the last report. The workflow now loads previous tools data, compares it with the current toolset, and includes a changes section in the generated report.


## v0.19.0 - 2025-10-12

### Features

#### Add validation step to mcp-server command startup

The `mcp-server` command now validates configuration before starting the server. It runs `gh aw status` to verify that the gh CLI and gh-aw extension are properly installed, and that the working directory is a valid git repository with `.github/workflows`. This provides immediate, actionable feedback to users about configuration issues instead of cryptic errors when tools are invoked.


### Bug Fixes

#### Add git patch preview in fallback issue messages

When the create_pull_request safe output handler fails to push changes or create a PR, it now includes a preview of the git patch (max 500 lines) in the fallback issue message. This improves debugging by providing immediate visibility into the changes that failed to be pushed or converted to a PR.

#### Add lockfile statistics analysis workflow for nightly audits

Adds a new agentic workflow that performs comprehensive statistical and structural analysis of all `.lock.yml` files in the repository, publishing insights to the "audits" discussion category. The workflow runs nightly at 3am UTC and provides valuable visibility into workflow usage patterns, trigger types, safe outputs, file sizes, and structural characteristics.

#### Fix false positives in error validation from environment variable dumps in logs

The audit workflow was failing due to false positives in error pattern matching. The error validation script was matching error pattern definitions that appeared in GitHub Actions logs as environment variable dumps, creating a recursive false positive issue. Added a `shouldSkipLine()` function that filters out GitHub Actions metadata lines (environment variable declarations and section headers) before validation, allowing the audit workflow to successfully parse agent logs without false positives.

#### Fix YAML boolean keyword quoting to prevent workflow validation failures

Fixed the compiler to prevent unquoting the "on" key in generated workflow YAML files. This prevents YAML parsers from misinterpreting "on" as the boolean value `True` instead of a string key, which was causing GitHub Actions workflow validation failures. The fix ensures all compiled workflows generate valid YAML that passes GitHub Actions validation.

#### Mark permission-related error patterns as warnings to reduce false positives

Permission-related error patterns were being classified as fatal errors, causing workflow runs to fail unnecessarily when encountering informational messages about permissions, authentication, or authorization. This change introduces a `Severity` field to the `ErrorPattern` struct that allows explicit override of the automatic level detection logic, enabling fine-grained control over which patterns should be treated as errors versus warnings.

Updated 26 permission and authentication-related patterns across the Codex and Copilot engines to be classified as warnings instead of errors, improving workflow reliability while maintaining visibility of permission issues for troubleshooting.


## v0.18.2 - 2025-10-11

### Bug Fixes

#### Add GitHub Copilot agent setup workflow

Adds a `.github/workflows/copilot-setup-steps.yml` workflow file to configure the GitHub Copilot coding agent environment with preinstalled tools and dependencies. The workflow mirrors the setup steps from the CI workflow's build job, including Node.js, Go, JavaScript dependencies, development tools, and build step. This provides Copilot agents with a fully configured development environment and speeds up agent workflows.

#### Add compiler validation for GitHub Actions 21KB expression size limit

The compiler now validates that expressions in generated YAML files don't exceed GitHub Actions' 21KB limit. This prevents silent failures at runtime by catching oversized environment variables and expressions during compilation. When violations are detected, compilation fails with a descriptive error message and saves the invalid YAML to `*.invalid.yml` for debugging.

#### Enhance CLI version checker workflow with comprehensive version analysis

Enhanced the CLI version checker workflow to perform deeper research summaries when updates are detected. The workflow now includes:

- Version-by-version analysis for all intermediate versions
- Categorized change tracking (breaking changes, features, bugs, security, performance)
- Impact assessment on gh-aw workflows
- Timeline analysis with release dates
- Risk assessment (Low/Medium/High)
- Enhanced research sources and methods documentation
- Improved PR description templates with comprehensive version progression documentation

This internal tooling improvement helps maintainers make more informed decisions about CLI dependency updates.

#### Fix compiler issue generating invalid lock files due to heredoc delimiter

Fixed a critical bug in the workflow compiler where using single-quoted heredoc delimiters (`<< 'EOF'`) prevented GitHub Actions expressions from being evaluated in MCP server configuration files. Changed to unquoted delimiters (`<< EOF`) to allow proper expression evaluation at runtime. This fix affects all generated workflow lock files and ensures MCP configurations are correctly populated with environment variables.

#### Move init command to pkg/cli folder

Refactored the init command structure by moving `NewInitCommand()` from `cmd/gh-aw/init.go` to `pkg/cli/init_command.go` to follow the established pattern for command organization used by other commands in the repository.

#### Remove push trigger from repo-tree-map agentic workflow

The workflow now only triggers via manual `workflow_dispatch`, preventing unnecessary automatic runs when the workflow lock file is modified.

#### Update documentation unbloater workflow with cache-memory and PR checking

Enhanced the unbloat-docs workflow to improve coordination and avoid duplicate work:
- Added cache-memory tool for persistent storage of cleanup notes across runs
- Added search_pull_requests GitHub API tool to check for conflicting PRs
- Updated workflow instructions to check cache and open PRs before selecting files to clean


## v0.18.1 - 2025-10-11

### Bug Fixes

#### Security Fix: Allocation Size Overflow in Bash Tool Merging (Alert #7)

Fixed a potential allocation size overflow vulnerability (CWE-190) in the workflow compiler's bash tool merging logic. The fix implements input validation, overflow detection, and reasonable limits to prevent integer overflow when computing capacity for merged command arrays. This is a preventive security fix that maintains backward compatibility with no breaking changes.

#### Security Fix: Allocation Size Overflow in Domain List Merging (Alert #6)

Fixed CWE-190 (Integer Overflow or Wraparound) vulnerability in the `EnsureLocalhostDomains` function. The function was vulnerable to allocation size overflow when computing capacity for the merged domain list. The fix eliminates the overflow risk by removing pre-allocation and relying on Go's append function to handle capacity growth automatically, preventing potential denial-of-service issues with extremely large domain configurations.

#### Fixed unsafe quoting vulnerability in network hook generation (CodeQL Alert #9)

Implemented proper quote escaping using `strconv.Quote()` when embedding JSON-encoded domain data into Python script templates. This prevents potential code injection vulnerabilities (CWE-78, CWE-89, CWE-94) that could occur if domain data contained special characters. The fix uses Go's standard library for safe string escaping and adds `json.loads()` parsing in the generated Python scripts for defense in depth.

#### Refactor: Extract duplicate MCP config renderers to shared functions

Eliminated 124 lines of duplicate code by extracting MCP configuration rendering logic into shared functions. The Playwright, safe outputs, and custom MCP configuration renderers are now centralized in `mcp-config.go`, ensuring consistency between Claude and Custom engines while maintaining 100% backward compatibility.

#### Update agentic CLI versions

Updates the default versions for agentic CLIs:
- Claude Code: 2.0.13 → 2.0.14
- GitHub Copilot CLI: 0.0.338 → 0.0.339

These are patch version updates and should not contain breaking changes. Users of gh-aw will automatically use these newer versions when they are specified in workflows.


## v0.18.0 - 2025-10-11

### Features

#### Add simonw/llm CLI integration with issue triage workflow

This adds support for using the simonw/llm CLI tool as a custom agentic engine in GitHub Agentic Workflows, with a complete issue triage workflow example. The integration includes:

- A reusable shared component (`.github/workflows/shared/simonw-llm.md`) that enables any workflow to use simonw/llm CLI as its execution engine
- Support for multiple LLM providers: OpenAI, Anthropic Claude, and GitHub Models (free tier)
- Automatic configuration and plugin management
- Safe-outputs integration for GitHub API operations
- An example workflow (`issue-triage-llm.md`) demonstrating automated issue triage
- Comprehensive documentation with setup instructions and examples
- Support for both automatic triggering (on issue opened) and manual workflow dispatch


### Bug Fixes

#### Add repo-tree-map workflow for visualizing repository structure

This introduces a new agentic workflow that generates an ASCII tree map visualization of the repository file structure and publishes it as a GitHub Discussion. The workflow uses bash tools to gather repository statistics and create a formatted report with directory hierarchy, file size distributions, and repository metadata.

#### Add security-fix-pr workflow for automated security issue remediation

This adds a new agentic workflow that automatically generates pull requests to fix code security issues detected by GitHub Code Scanning. The workflow can be triggered manually via workflow_dispatch and will identify the first open security alert, analyze the vulnerability, generate a fix, and create a draft pull request for review.

#### Improve Copilot error detection to treat permission denied messages as warnings

Updated error pattern classification in the Copilot engine to correctly identify "Permission denied and could not request permission from user" messages as warnings instead of errors. This change improves error reporting accuracy and reduces false positives in workflow execution metrics.

#### Fix error pattern false positives in workflow validation

The error validation step was incorrectly flagging false positives when workflow output contained filenames or text with "error" as a substring. Updated error patterns across all AI engines (Copilot, Claude, and Codex) to use word boundaries (`\berror\b`) instead of matching any occurrence of "error", ensuring validation correctly distinguishes between actual error messages and informational text.

#### Fix import directive parsing for new {{#import}} syntax

Fixed a bug in `processIncludesWithWorkflowSpec` where the new `{{#import}}` syntax was incorrectly parsed using manual regex group extraction, causing malformed workflowspec paths. The function now uses the `ParseImportDirective` helper that correctly handles both legacy `@include` and new `{{#import}}` syntax. Also added safety checks for empty file paths and comprehensive unit tests.

#### Add security-events permission to security workflow

Fixed a permissions error in the security-fix-pr workflow that prevented it from accessing code scanning alerts. The workflow now includes the required `security-events: read` permission to successfully query GitHub's Code Scanning API for vulnerability analysis and automated fix generation.

#### Security Fix: Unsafe Quoting in Import Directive Warning (Alert #8)

Fixed unsafe string quoting in the `processIncludesWithVisited` function that could lead to potential injection vulnerabilities. The fix applies Go's `%q` format specifier to safely escape special characters in deprecation warning messages, replacing the unsafe `'%s'` pattern. This addresses CodeQL alert #8 (go/unsafe-quoting) related to CWE-78 (OS Command Injection), CWE-89 (SQL Injection), and CWE-94 (Code Injection).

#### Security fix: Prevent injection vulnerability in secret redaction YAML generation

Fixed a critical security vulnerability (CodeQL go/unsafe-quoting) where secret names containing single quotes could break out of enclosing quotes in generated YAML strings, potentially leading to command injection, SQL injection, or code injection attacks. Added proper escaping via a new `escapeSingleQuote()` helper function that sanitizes secret references before embedding them in YAML.

#### Fix XML comment removal in imported workflows and update GenAI prompt generation

- Fixed a bug where code blocks within XML comments were incorrectly preserved instead of being removed during workflow parsing
- Refactored GenAI prompt generation to use echo commands instead of sed for better readability and maintainability
- Removed the Issue Summarizer workflow
- Updated workflow trigger configurations to run on lock file changes
- Added comprehensive test suite for XML comment handling
- Simplified repository tree map workflow by reducing timeout and streamlining tool permissions

#### Update Codex remote GitHub MCP configuration to new streamable HTTP format

Updated the Codex engine's remote GitHub MCP server configuration to use the new streamable HTTP format with `bearer_token_env_var` instead of deprecated HTTP headers. This includes adding the `experimental_use_rmcp_client` flag, using the `/mcp-readonly/` endpoint for read-only mode, and standardizing on `GH_AW_GITHUB_TOKEN` across workflows. The configuration now aligns with OpenAI Codex documentation requirements.


## v0.17.0 - 2025-10-10

### Features

- Add GenAIScript shared workflow configuration and example
- Add support for GitHub toolsets configuration in agentic workflows
- Add GraphQL sub-issue linking and optional parent parameter to create-issue safe output
- Add mcp-server command to expose CLI tools via Model Context Protocol
- Add top-level `runtimes` field for runtime version overrides
- Add automatic runtime setup detection and insertion for workflow steps
- Display individual errors and warnings in audit command output
- Remove instruction file writing from compile command and remove --no-instructions flag
- Remove timeout requirement for strict mode and set default timeout to 20 minutes
- Add support for common GitHub URL formats in workflow specifications

### Bug Fixes

- Add arXiv MCP server integration to scout workflow
- Add Context7 MCP server integration to scout workflow
- Add comprehensive logging to validate_errors.cjs for infinite loop detection
- Add test coverage for shorthand write permissions in strict mode
- Add verbose logging for artifact download and metric extraction
- Add workflow installation instructions to safe output footers with enterprise support
- Add GH_AW_WORKFLOW_NAME environment variable to add_reaction job
- Add cache-memory support to included workflow schema
- Configure Copilot log parsing to use debug logs from /tmp/gh-aw/.copilot/logs/
- Update duplicate finder workflow to ignore test files
- Fix copilot log parser to show tool success status instead of question marks
- Fix `mcp inspect` to apply imports before extracting MCP configurations
- Fix: Correct MCP server command in .vscode/mcp.json
- Use GITHUB_SERVER_URL instead of hardcoded https://github.com in safe output JavaScript files
- Update ast-grep shared workflow to use mcp/ast-grep docker image
- Organize MCP server shared workflows into dedicated mcp/ subdirectory with cleaner naming
- Organize temp file locations under /tmp/gh-aw/ directory
- Extract common GitHub Script step builder for safe output jobs
- Remove "Print Safe Outputs" step from generated lock files
- Remove Agentic Run Information from step summary
- Update comment message format for add_reaction job
- Update CLI version updater to support GitHub MCP server version monitoring
- Update Codex error patterns to support new Rust-based format
- Update Codex log parser to render tool calls using HTML details with 6 backticks
- Update Codex log parser to support new Rust-based format
- Update error patterns for copilot agentic engine
- Update Copilot log parser to render tool calls using HTML details with 6 backticks
- Update Copilot log parser to render tool calls with 6 backticks and structured format
- Update rendering of tools in summary tag to use HTML code elements
- Update workflow to create discussions instead of issues and adjust related configurations

## v0.15.0 - 2025-10-08

### Features

- Add PR branch checkout when pull request context is available
- Add comment creation for issue/PR reactions with workflow run links
- Add secret redaction step before artifact upload in agentic workflows
- Implement internal changeset script for version management with safety checks

### Bug Fixes

- Convert TypeScript safe output files to CommonJS and remove TypeScript compilation
- Update Claude Code CLI to version 2.0.10

