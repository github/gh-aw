---
title: Using Agy with GitHub Agentic Workflows
description: Configure the experimental Google Antigravity CLI engine with Gemini API-key authentication, native models, sandboxed tools and its verified Linux release.
---

> [!WARNING]
> Agy is an experimental built-in engine, not a Gemini alias or a feature-equivalent
> replacement. Initial support is Linux x64 with native CLI v1.3.1 and Gemini
> API-key authentication. ADC, Google WIF and Vertex AI are not supported.

## Selection and authentication

Set `engine: agy`; no shared-engine import is required.

Agy is a first-class Go engine in the compiler, like Gemini. Its embedded Markdown
entry contains catalog metadata only; installation, execution, credentials and
MCP configuration are implemented by `AgyEngine`. The setup action supplies the
native harness, gateway configuration adapter and streaming log parser.

Both selection forms work:

```yaml
engine: agy
```

```yaml
engine:
  id: agy
  model: gemini-3.8-flash-medium
  version: "1.3.1"
```

Configure `GEMINI_API_KEY` as a repository secret, for example with
`gh aw secrets bootstrap --engine agy`. `GOOGLE_API_KEY` is not an alternative.
The harness explicitly selects `modelProvider: gemini` in a private per-run
`~/.gemini/antigravity-cli/settings.json`; an API key alone does not select this
native authentication profile.

The provider credential remains outside the agent sandbox. Agy receives a dummy
credential and the configured Gemini endpoint discovered from AWF. Endpoint
discovery failure is fatal; it does not fall back to direct inference or
interactive authentication.

## Models and installation

The default native model slug is `gemini-3.8-flash-medium`. Use native Agy slugs,
not `google/`, `gemini/` or Copilot provider prefixes. Availability depends on
the native release and the API key's access; an unknown or inaccessible model
fails the run.

The harness downloads the native Linux x64 v1.3.1 release, verifies its published
SHA-256 checksum, extracts only `antigravity`, and verifies the executable version.
Other releases and architectures are not supported. A custom `engine.command`
must report the same verified version. Installation and settings are temporary
and removed after execution; no inherited user authentication profile is used.

The archive checksum applies to the default installer only. `engine.command`
selects a separately trusted executable; its version is checked, but its bytes
are not attested by that archive checksum.

## Tools, configuration and limits

Agy consumes native streaming JSON input from stdin, keeping prompts out of
command arguments. Native streaming events are normalized into session artifacts;
repeated cumulative usage snapshots are not added together. Missing metrics
retain their last valid value when a later result omits them; metrics never
reported remain absent. Status and errors come from the current result.

The agent artifact's `agent-session.jsonl` retains native evidence as canonical
events; conclusion projects its essential fields into `usage/aw_session.jsonl`.
Native `conversation_id` maps to `sessionId`, and initialization preserves the
working directory and tool inventory. `user_input` steps remain `user.message`
observations even when the native stream exposes no prompt text. Streaming
answers retain exact text; exposed reasoning and structured refusals use their
separate channels. Tool completions retain durations and structured outputs or
errors, including `DONE` records with no output. Only explicit success supplies a
successful tool outcome; reported errors or nonzero exit codes take precedence.
Output presence alone does not establish success. Missing outcomes remain unknown,
and orphan completions do not invent invocations. Native numeric tool
`step_index` maps to `stepIndex`, scoped by `sessionId`; it does not become a
fabricated string `toolCallId`. Repeated active tool observations use
`tool.execution_update`, retaining their supplied metadata.

Per-step and checkpoint token observations use `usage.report`, separately from
the cumulative `session.result`; these overlapping observations must not be
summed. Native v1.3.1 `input_tokens` excludes `cache_read_tokens`, represented by
`input_tokens_include_cache: false`. Missing native event IDs and timestamps
remain absent. Unrecognized native events retain their payloads as extensions.

The parser regression fixture samples the native transcript and published
artifacts from [Smoke Agy run 37848575608](https://github.com/github/gh-aw/actions/runs/37848575608).
Native inference succeeded, but the workflow's conformance checker failed; this
is not evidence of a passing production gate. The successful
[authentication run 37732637929](https://github.com/github/gh-aw/actions/runs/37732637929)
publishes only a sanitized receipt, including negative-probe status and usage,
not the native transcript. Reasoning, refusal, and additional failure cases in
the parser tests are synthetic, not observations from these runs.

Agent writes to `GITHUB_STEP_SUMMARY` use an isolated file that is appended to
the runner's step summary only after secret redaction, matching other built-in engines.

Configured HTTP MCP servers are translated from the gh-aw gateway into
owner-only `.agents/mcp_config.json`, using native `serverUrl` entries and
validated headers. All configured MCP servers, including `safeoutputs`,
`mcpscripts`, custom servers and `awf-enclave`, remain available through native
MCP. Only explicit `tools.cli-proxy` excludes CLI-mounted servers from the native
configuration; the existence of an infrastructure CLI wrapper does not exclude
its native MCP route.
Repository MCP configuration is replaced by the explicitly configured gateway servers.
`AGENTS.md`, `GEMINI.md`, `.agents/` and `.gemini/` are protected instruction
and configuration surfaces.

Native permissions use blanket approvals **inside the outer gh-aw sandbox**,
not as the security boundary. This preserves the existing unattended execution
profile; the bypass is not intrinsically required for headless mode. Agy supports
scoped `permissions.allow` rules, but this integration does not yet translate
workflow tool restrictions into that native policy. Without advance grants,
approval-required tools are soft-denied in headless mode, which the gh-aw harness
treats as a failure. See the [native headless permission documentation](https://antigravity.google/docs/cli/headless).
Per-command bash restrictions, `bash: false`,
empty bash allowlists, and disabling native editing or web tools are rejected
rather than silently ignored. The fixed native timeout is five minutes, with
a wrapper watchdog and the Actions step timeout as additional bounds.
Cancellation and timeout signal the isolated native process group, escalate to
SIGKILL, and bound pipe draining so tool descendants cannot stall cleanup.
Permission denials, pending tool executions, interruption and non-success
native results fail execution even when the CLI exits zero. Successful execution
also requires a completed native turn with positive input and output token usage.

`engine.args`, `engine.bare`, `engine.api-target`, `engine.config`, `engine.cwd`,
provider overrides and `engine.auth` are rejected. Native `engine.permission-mode`,
`max-turns`, continuations, and harness or driver overrides are also rejected.
Use `max-turn-cache-misses`, `max-ai-credits` and `timeout-minutes` for outer
execution limits. Agent Plugins and native custom-agent selection are not
supported. Agy-specific settings, skills and hooks are not interchangeable with
Gemini settings.

`gh aw compile --dry-run` permits the built-in Agy harness's internal
`--dangerously-skip-permissions` flag. Its dangerous-feature filter applies to
enabled `dangerously-*` entries authored in workflow Markdown configuration,
including imports, not built-in engine implementation flags. Runtime sandbox
and credential isolation remain separate security requirements. Dry-run
compilation does not authorize live execution.

## Troubleshooting and conformance

Missing credentials fail activation. A model-provider error usually means the
native profile was not selected; gh-aw creates that profile automatically.
AWF endpoint errors require a working configured Gemini proxy, not an additional
credential inside the agent.

The manual native authentication probe checks real inference and negative
authentication/model cases. `engine-conformance-agy.md` separately exercises the
production installer, AWF, native MCP, CLI-mounted MCP tools and staged safe
outputs. It remains dispatch-only; Credentials Check calls the feature-branch
`agy-conformance-reusable.lock.yml`, compiled from the same shared probes and
prompt. Stable promotion is gated on that production path, not on mocked tests
or native authentication alone. Gemini is deprecated in favor of experimental
Agy, but remains supported for workflows outside Agy's capabilities.

`smoke-agy.md` provides an on-demand smoke test using the same shared conformance
suite. Run it with `workflow_dispatch`, `/smoke-agy` in an issue or pull request
body or comment, or by adding the `smoke` label to a pull request. Commands and
labels are routed through the centralized command workflow; the Agy workflow
does not remove the trigger label. It checks file
reads and writes, shell execution and environment forwarding,
inference accounting, native and CLI-mounted MCP round trips, and staged safe
outputs. Runs are bounded to ten minutes and fifty AI credits; results are recorded
in the step summary and conformance artifacts, not published as issues or comments.

Both checkers require exactly one staged noop receipt, not an empty output file
or duplicate completion messages. Complete all probes before emitting that noop
through Agy's native `safeoutputs` MCP server, not its CLI wrapper.
The native checker requires completed `call_mcp_tool` events for both the custom
`agy-native` server and the built-in `mcpscripts` server, with the expected tools
and fixture nonce and responses matching their recorded receipts. It also verifies
the native `safeoutputs(noop)` event and exact staged output.
The native MCP allowlist uses `native_challenge`, matching the underscore-normalized
name exposed by the MCP scripts server; the script's authored name remains
`native-challenge`.
The Agy gate explicitly disables the separate Copilot threat-detection job;
it forwards only the Gemini key and retains its own bounded, read-only checks.

See the [engine reference](/gh-aw/reference/engines/) and
[authentication reference](/gh-aw/reference/auth/).

## Evaluating a Gemini API-key workflow

Do not migrate a WIF workflow or one requiring native bash restrictions.
For an API-key workflow within Agy's documented capabilities, changing the
engine is an explicit choice, not an automatic migration:

| Gemini configuration | Experimental Agy configuration |
|---|---|
| `engine: gemini` | `engine: agy` |
| Gemini CLI version pin | Verified native `version: "1.3.1"` only |
| Gemini model name | Native Agy slug, such as `gemini-3.8-flash-medium` |
| `GEMINI_API_KEY` | Same secret name; private `modelProvider: gemini` settings |
| Gemini settings, hooks or customizations | Not copied; review native configuration differences |
| Native bash allowlist or disabled shell | Unsupported; keep Gemini |

Recompile and evaluate the bounded production conformance workflow before
relying on the replacement. Existing Gemini binaries, pins and historical logs
retain their identity; no alias, codemod or removal date is introduced.
