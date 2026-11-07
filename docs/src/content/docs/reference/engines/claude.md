---
title: Claude Code reference
description: Claude Code engine configuration, supported tools, runtime behavior, and limitations.
---

For first-time setup, see [Using Claude Code](/gh-aw/engines/claude/). Shared options are in [AI engines](/gh-aw/reference/engines/).

## Selection and authentication

Use `engine: claude` with [`ANTHROPIC_API_KEY`](/gh-aw/reference/auth/#anthropic_api_key) or [Anthropic WIF](/gh-aw/reference/auth/#anthropic-workload-identity-federation-wif) to use an Anthropic-hosted model. Subscription OAuth tokens are unsupported.

## GitHub Copilot inference

Alternatively, select a Copilot-hosted Anthropic model with inference billed through GitHub Copilot:

```aw wrap
engine: claude
model: copilot/claude-haiku-4.5
permissions:
  copilot-requests: write
```

`copilot-requests: write` authenticates inference with `${{ github.token }}`; no PAT, `COPILOT_GITHUB_TOKEN` secret, or `ANTHROPIC_API_KEY` is required. Without that permission, configure `COPILOT_GITHUB_TOKEN` instead. The default agent sandbox is required: AWF holds the GitHub credential, and Claude receives only a placeholder key and the reflected Copilot proxy endpoint.

The `copilot/` prefix selects the provider and is removed from the model ID passed to Claude. An explicit `engine.model-provider` overrides provider selection. For a fully dynamic model expression whose provider cannot be inferred at compile time, set `engine.model-provider: github`.

Native `tools.web-fetch` and `tools.web-search` are unavailable on GitHub Copilot inference because the proxy sets `COPILOT_OFFLINE=true`, even for allowlisted domains. The compiler rejects these tools on this provider. Use `engine.model-provider: anthropic`, or configure an MCP fetch server for `web-fetch` or an MCP search server for `web-search`.

Copilot writes Claude model versions with a dot (`claude-opus-5.5`), while Anthropic uses hyphenated ids (`claude-opus-5-5`). With GitHub inference, the gh-aw harness maps Anthropic ids to Copilot ids before launching Claude Code, including fixed models, dynamic expressions, and threat-detection models. Dotted ids and major-only ids such as `claude-sonnet-5` remain unchanged; Anthropic inference keeps its native ids.

gh-aw also passes a `--settings` value containing a `modelOverrides` map, but Claude Code does not apply that map on every inference route. If `engine.args` already passes `--settings`, gh-aw keeps that value, does not add the map, and emits a compile warning. Harness model-id normalization still applies independently of these settings.

Claude uses its native Messages API, not the OpenAI Responses or Chat Completions API. Both `copilot/auto` and `auto` with `engine.model-provider: github` select an advertised Claude model from the configured Copilot gateway: newest Sonnet first, then Opus, then Haiku. Selection fails before inference if no supported Claude model is advertised; it never selects a GPT model or switches providers. Bare `auto` on the native Anthropic route uses Claude's `sonnet` alias. Model access, beta headers, and request features remain subject to GitHub Copilot API (CAPI) support.

## Capabilities and limitations

Claude Code supports native web search, bare mode, top-level `max-turns`, per-command bash pre-approval, and custom `engine.harness` scripts. It does not support Copilot-specific `max-continuations` or native `engine.agent` selection. See the [AI engine feature comparison](/gh-aw/reference/engines/#engine-feature-comparison).

The default permission mode is `dontAsk`: actions requiring approval run only when pre-approved. Disabled Bash/web tools are removed, and `tools.edit: false` denies repository edits even with an explicit permission-mode override. Safe outputs do not independently grant file-write access. Sandbox and MCP gateway policies provide the isolation boundaries.

Bare mode skips ambient discovery and restricts native tools to Bash, Edit, and Read in Claude Code 2.1.288. Workflow-declared skills load through the `gh-aw-workflow` plugin and can be explicitly invoked as `/gh-aw-workflow:<skill-name>`. Top-level pinned plugins continue to load explicitly. Do not rely on automatic Skill-tool invocation or subagent delegation in bare mode; use `bare: false` when the workflow needs those capabilities.

### Dynamic workflows

Claude Code [dynamic workflows](https://code.claude.com/docs/en/workflows) are disabled by default. `engine.dynamic-workflows: true` pre-approves the `Workflow` tool. Headless mode requires an explicit prompt requesting a saved workflow; interactive keyword triggers do not work. Keep `engine.bare: false`.

Store scripts/support files under `.claude/workflows/`. gh-aw recursively snapshots `.claude/` from the trusted activation checkout and restores it after agent checkout, even without PR checkout. Use `ambient-folders` for files elsewhere. Without dynamic workflows, restoration occurs only when PR checkout requires it. Workflow agents remain subject to configured tool permissions.

See the [saved-workflow example](https://github.com/github/gh-aw/blob/main/.github/workflows/smoke-claude-dynamic.md). Task lifecycle events retain correlation, progress, and completion without embedded scripts/prompts. Task usage snapshots remain separate from parent token totals.

### Harness recovery

Retries resume the session ID with a short continuation prompt. `CLAUDE_CODE_MAX_RETRIES: 0` leaves transient retries to the harness; `ANTHROPIC_MAX_RETRIES` does not control Claude's retry loop. A resumed run with progress cannot silently restart fresh; `GH_AW_CLAUDE_ALLOW_FRESH_RESTART: true` opts into that behavior. See [shared harness settings](/gh-aw/reference/engines/#harness-retry-and-post-result-watchdog-policy).

The harness resolves [model aliases](/gh-aw/reference/engines/#model-aliases) before Claude Code starts, so `model: copilot/opus` on GitHub Copilot inference runs on the newest live Opus. `sonnet`, `opus`, and `haiku` use gh-aw's alias map, not Claude Code's built-in meaning, on every provider. `auto` keeps Claude's rule: on Copilot inference it picks the newest Sonnet, then Opus, then Haiku. Exact ids pass through unchanged, after Claude Code's model-id overrides.

## Session trace mapping

`agent-session.jsonl` retains parsed native events; conclusion projects essential payloads into `usage/aw_session.jsonl`. Older artifacts may contain only `agent-stdio.log`.

The adapter maps messages/thinking, structured images/documents, tool calls/results, refusals, provider errors, and permission denials. Explicit failures override contradictory success fields; absent duration/exit/outcome remain unknown. API errors/retries appear once under `session.result.errors`; permission notices use `session.result.permissionDenials`, not fabricated tool calls or assistant answers.

Streaming and finalized snapshots enrich the same canonical events without duplication; estimated thinking tokens do not become session accounting. See the [unified session specification](/gh-aw/specs/unified-agent-session-specification/) for the event contract.

## Claude Tool Enforcement Security Model

Claude's `--allowed-tools` flag pre-approves actions; it does not remove every other tool. The default `engine.permission-mode: dontAsk` denies actions that would require approval. Supported overrides are `default`, `dontAsk`, `auto`, `acceptEdits`, `plan`, and `bypassPermissions`:

```yaml wrap
engine:
  id: claude
  permission-mode: dontAsk
```

`engine.permission-mode` takes precedence over any `--permission-mode` flag supplied through `engine.args`. When unset, the default is `dontAsk`. Disabled Bash/web tools are removed, and `tools.edit: false` denies repository edits. Safe outputs grant MCP access, not unrestricted file writes. Memory and sandbox paths use absolute recursive `Read`/`Edit` grants. gh-aw does not derive `bypassPermissions` from unrestricted bash.

| `engine.permission-mode` | Effective mode | Actions beyond pre-approved rules | Gateway `allowed:` enforced? |
|---|---|:---:|:---:|
| unset / `dontAsk` | `dontAsk` | Actions requiring approval are denied; built-in read-only actions remain | ✅ Yes |
| `default` | `default` | Uses Claude's normal approval policy | ✅ Yes |
| `auto` | `auto` | Classifier may approve additional actions | ✅ Yes |
| `acceptEdits` | `acceptEdits` | Automatically approves edits and some filesystem commands | ✅ Yes |
| `plan` | `plan` | Uses Claude's plan-mode policy | ✅ Yes |
| `bypassPermissions` | `bypassPermissions` | Skips approval checks except explicit deny rules | ✅ Yes |

### Gateway-side enforcement

The MCP gateway's `allowed:` filter constrains MCP access independently of Claude's permission mode. Always specify `allowed:` on each `mcp-servers:` entry to restrict which MCP tools are reachable:

```yaml wrap
mcp-servers:
  notion:
    container: "mcp/notion"
    allowed: ["search_pages", "get_page"]   # enforced at gateway level
```

> [!WARNING]
> Pre-approval rules are not OS-level isolation. Use the sandbox for filesystem/network boundaries and the gateway for MCP access. Explicit `auto`, `acceptEdits`, or `bypassPermissions` can approve more than the pre-approved actions.
