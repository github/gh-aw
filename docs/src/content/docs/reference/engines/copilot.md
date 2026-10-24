---
title: GitHub Copilot reference
description: GitHub Copilot engine configuration, supported tools, runtime behavior, and limitations.
---

For first-time setup, see [Using GitHub Copilot](/gh-aw/engines/copilot/). Shared options are in [AI engines](/gh-aw/reference/engines/).

## Selection and authentication

Copilot is the default engine. Grant [`copilot-requests: write`](/gh-aw/reference/auth/#copilot-requests-write-permission) for organization-billed inference, or supply [`COPILOT_GITHUB_TOKEN`](/gh-aw/reference/auth/#copilot_github_token). External providers use [BYOK configuration](#copilot-bring-your-own-key-byok-mode).

The default CLI pin is `1.0.101`. The compiler embeds this version and its reviewed checksum-manifest digest in the generated workflow. Installation prefers that exact version from the trusted runner toolcache; a cache miss downloads and verifies the pinned release instead of selecting a version from the live compatibility matrix. `engine.version` overrides the default. CLI-only execution is the default. `engine.copilot-sdk: true` or `engine.driver` selects SDK mode.

## Capabilities and limitations

Copilot supports `engine.agent`, custom harnesses, `max-continuations`, bare mode, and per-command bash allowlists. Compilation rejects native `tools.web-search` and CLI-mode `tools.web-fetch`; use MCP instead. SDK mode provides a proxy-aware `web-fetch` tool. Offline BYOK disables native web tools.

Sandboxed CLI sessions derive URL approvals from finalized AWF network rules, including `network_allowed` additions. Domain rules cover apex/subdomains and HTTP/HTTPS unless restricted by protocol/port. These approvals do not grant shell access or bypass the firewall.

Copilot resolves [model aliases](/gh-aw/reference/engines/#model-aliases) before launch. Exact IDs pass through unchanged; unavailable IDs fail the first inference request.

### Dynamic workflows

Copilot [dynamic workflows](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/use-dynamic-workflows) are experimental and disabled by default. Current CLI releases require both settings:

```yaml
engine:
  id: copilot
  dynamic-workflows: true
  args: ["--experimental"]
```

gh-aw enables `EXTENSIONS`, loads project extensions, and pre-approves workflow runs. SDK sessions honor the same switch. Store definitions in `.github/extensions/<name>/extension.mjs` and request the registered workflow by name in the prompt. Their agents retain workflow tool/sandbox restrictions. Availability also depends on the Copilot account; enabling extensions does not make them available with BYOK.

The trusted activation snapshot recursively restores `.github/` after agent checkouts, even with PR checkout disabled, preventing PR-head extensions/settings/hooks from replacing trusted definitions. Use `ambient-folders` for support files elsewhere. Without dynamic workflows, project extensions are not loaded, runs are denied, and configuration restoration occurs only when PR checkout requires it.

See the [dynamic workflow example](https://github.com/github/gh-aw/blob/main/.github/workflows/smoke-copilot-dynamic-workflow.md).

## Copilot release checksum pins

To download a Copilot CLI version not covered by the setup action's built-in pins, provide `engine.checksums-sha256` alongside `engine.version`:

```yaml wrap
engine:
  id: copilot
  version: "1.0.101"
  checksums-sha256: "018625eac68670d66ede904b9b82cdcd6ab8f87ba20e6e1fbdf0b62c8295d5be"
```

The example uses the manifest digest for v1.0.101; change both fields together for another release. The digest authenticates **`SHA256SUMS.txt`**, not an individual platform archive. It must be a literal string of exactly 64 lowercase hexadecimal characters obtained through an independently trusted channel. Fetching a checksum from the same potentially intercepted origin as the executable is not independent authentication.

An explicit pin overrides the built-in manifest pin and bypasses the runner toolcache. Both the host archive and a requested musl guest archive are verified against the authenticated manifest. Omit `command`, which replaces the built-in installer. This field is only supported by the built-in Copilot CLI installer, not other engines or inline engine definitions.

The engine's version and pin are inherited by detection and eval jobs that reuse that engine. A separate `safe-outputs.threat-detection.engine` override accepts its own `version` and `checksums-sha256`. For AWF, use [`sandbox.agent.checksums-sha256`](/gh-aw/reference/sandbox/#release-integrity-sandboxagentchecksums-sha256).

The compiler supplies the reviewed digest automatically when `engine.version` is omitted or matches the compiler's default version. These generated installs pass `--prefer-toolcache`: the pin authenticates downloaded archives, not the cached host binary. A musl guest archive is still downloaded and verified when the host uses cache. User-supplied frontmatter pins continue to bypass the toolcache.

Compilation fails if the compiler's default version or digest is missing or malformed. Other versions without an explicit pin retain the trusted toolcache policy and setup action's built-in release pins; an unreviewed download still fails closed. A frontmatter pin requires a literal semantic release version such as `"1.0.101"` or `"v1.0.101"`; it cannot be combined with `latest`, an omitted version, or a version expression.

For maintainers, `pkg/constants/version_constants.go` stores `DefaultCopilotVersion` and `DefaultCopilotChecksumsSHA256` together. `bash scripts/update-copilot-version.sh VERSION` resolves both from the reviewed pin table, without downloading or trusting new release data. New releases must first be authenticated and added with `scripts/update-release-pins.sh`; the resolver refuses releases without a reviewed pin.

## Copilot Custom Configuration

Use `agent` to reference a custom agent file in `.github/agents/` (omit the `.agent.md` extension):

```yaml wrap
engine:
  id: copilot
  agent: technical-doc-writer  # .github/agents/technical-doc-writer.agent.md
```

See [Copilot Agent Files](/gh-aw/reference/copilot-custom-agents/) for details.

## Copilot Bring Your Own Key (BYOK) Mode

The Copilot engine supports routing requests to an external LLM provider instead of GitHub's default routing. This is useful when you want to use a different model or provider (e.g., OpenAI, Anthropic, Azure OpenAI, or a local Ollama/vLLM instance) while still using the Copilot CLI tooling.

Set `COPILOT_PROVIDER_BASE_URL` in `engine.env` to activate BYOK mode. The credential variables `COPILOT_PROVIDER_BASE_URL`, `COPILOT_PROVIDER_API_KEY`, and `COPILOT_PROVIDER_BEARER_TOKEN` are explicitly allowed to carry `${{ secrets.* }}` references in `engine.env` under strict mode — they are not leaked to the agent container. Other `COPILOT_PROVIDER_*` variables hold non-sensitive configuration and can be set as plain strings. When `COPILOT_PROVIDER_BASE_URL` is a literal URL, gh-aw automatically adds its provider hostname to the AWF allow-list for both the main agent run and the threat-detection Copilot step, and the threat-detection step now derives its Copilot API target from that literal BYOK URL even when `engine.api-target` and `GITHUB_COPILOT_BASE_URL` are unset. When it is supplied via a secret or variable expression, add the provider hostname explicitly to `network.allowed` so the threat-detection step can reuse that concrete host safely.

| Variable | Required | Description |
|---|---|---|
| `COPILOT_PROVIDER_BASE_URL` | ✅ for BYOK | Base URL of the external provider (e.g. `https://api.openai.com/v1` or `https://RESOURCE.openai.azure.com/openai/v1` for Azure Foundry OpenAI) |
| `COPILOT_MODEL` | ✅ for BYOK | Model to use (e.g. `claude-sonnet-4`, `gpt-4o`); required by most providers |
| `COPILOT_PROVIDER_API_KEY` | Optional | API key for cloud providers (OpenAI, Anthropic, etc.); not needed for local providers |
| `COPILOT_PROVIDER_BEARER_TOKEN` | Optional | Bearer-token alternative to `COPILOT_PROVIDER_API_KEY`; takes precedence when set |
| `COPILOT_PROVIDER_TYPE` | Optional | Provider format: `openai` (default), `azure`, or `anthropic` |
| `COPILOT_PROVIDER_WIRE_API` | Optional | Wire API variant: `completions` (default) or `responses` (for GPT-5 series) |
| `COPILOT_PROVIDER_MODEL_ID` | Optional | Model ID sent on the wire when it differs from `COPILOT_MODEL` (e.g. an Azure deployment name) |
| `COPILOT_PROVIDER_WIRE_MODEL` | Optional | Alternative to `COPILOT_PROVIDER_MODEL_ID` for overriding the wire model |
| `COPILOT_PROVIDER_MAX_PROMPT_TOKENS` | Optional | Override the maximum prompt token limit (otherwise resolved from model catalog) |
| `COPILOT_PROVIDER_MAX_OUTPUT_TOKENS` | Optional | Override the maximum output token limit |

**Example:**

```yaml wrap
engine:
  id: copilot
  env:
    COPILOT_PROVIDER_BASE_URL: ${{ secrets.PROVIDER_BASE_URL }}   # REQUIRED — activates BYOK
    COPILOT_MODEL: claude-sonnet-4                                # REQUIRED for most providers
    COPILOT_PROVIDER_API_KEY: ${{ secrets.PROVIDER_API_KEY }}     # OPTIONAL for local providers
    COPILOT_PROVIDER_TYPE: anthropic                              # OPTIONAL — default: openai

network:
  allowed:
    - defaults
    - your-provider-domain.example.com
```

> [!NOTE]
> Credentials are kept out of the agent container — only a dummy API key activating the AWF BYOK detection path is visible to the agent process; the real credential is isolated in the AWF API proxy sidecar. See [AWF sandbox architecture](/gh-aw/reference/sandbox/).

### Azure Foundry OpenAI

Use the resource's `/openai/v1` endpoint with API-key or GitHub OIDC authentication. Keep `engine.model` on the advertised model ID and set `COPILOT_PROVIDER_MODEL_ID` when the deployment name differs. See [Azure OpenAI BYOK](/gh-aw/reference/azure-openai-byok/) for complete configurations, Responses API selection, and troubleshooting.

## Copilot SDK Support

Set `engine.copilot-sdk: true` to run a local Copilot sidecar and SDK driver.
In this mode, the harness starts a local sidecar and runs the
SDK driver process instead of the default CLI-only flow.

The built-in driver registers native MCP servers from converter-owned `RUNNER_TEMP/gh-aw/mcp-config/copilot-sdk.json`, not the user-managed CLI catalog. `tools.cli-proxy` servers remain CLI-only. A SHA-256 integrity value is bound to the run, attempt, and job; missing, malformed, changed, linked, or stale catalogs fail before inference. This does not protect against a compromised runner able to replace both catalog and trusted environment.

The built-in SDK driver loads inline sub-agents and repository
`.github/agents/*.agent.md` files as custom agents. It qualifies session and
sub-agent models against the AWF model catalog. Copilot models use separate
Responses and Chat Completions providers according to their supported endpoints,
so GPT and Claude agents can use different wire APIs in the same session.
Sub-agent activity and final model results, including BYOK status, are retained
in `usage/aw_session.jsonl`.

In both SDK and CLI mode, the harness resolves each sub-agent's `model:` before
Copilot starts, including on routed runs. It uses gh-aw model aliases (for
example `small` and `large`) and checks the live AWF model catalog. Each result
is recorded as a `model.resolution` event whose role is the agent name. `medium`
is not an alias and is treated as a literal model id. A sub-agent without
`model:` inherits the main model and has no resolution record.

Workflow-declared sub-agents (inline `## agent:` blocks) fail closed: if a model
cannot be resolved, is not in the live catalog, or does not qualify in SDK mode,
the run fails before Copilot starts. Other repository `.github/agents/*.agent.md`
files, which are often shared with VS Code, do not stop the run: a model that
cannot be resolved is recorded with `status: failed` and logged as a warning. In
CLI mode, that agent is used unchanged; in SDK mode, it inherits the session
model. An inline sub-agent overrides a repository agent with the same name.

In CLI mode, when the resolved id differs from the declared `model:`, the harness
writes a copy of the agent file, with the same file name and only `model:`
changed, to Copilot CLI's personal agents directory
(`${COPILOT_HOME:-$HOME/.copilot}/agents/`). A personal agent overrides the
repository agent with the same ID, so files in the checkout are never modified.
Sub-agent resolution is skipped in the threat-detection phase.

Use top-level `max-tool-denials` to stop SDK inference when
tool requests are repeatedly denied. The default is `5`.
For Copilot, this requires `engine.copilot-sdk: true`.

Use top-level `max-tool-calls` to configure a positive run-wide limit on
tool dispatches. The built-in Copilot SDK driver's pre-tool hook reserves the
budget before execution, so failed and permission-denied calls count, concurrent
calls cannot overspend it, and calls made by SDK subagents share the same count.
When exhausted, further calls are rejected with an explanatory reason and
`guard.tool_call_budget_exceeded` run event.

For Copilot, aggregate `max-tool-calls` requires SDK mode and the built-in driver/harness; CLI mode and custom drivers/harnesses reject it. [Pi](/gh-aw/reference/engines/pi/) has separate support for this budget. It is distinct from
`tools.github.allowed[].max-calls`, which remains a per-tool MCP Gateway limit
and does not cover local tools such as bash.

Use `engine.driver` to replace the built-in
`copilot_sdk_driver.cjs` implementation. On the Copilot engine,
setting `engine.driver` also enables `engine.copilot-sdk: true`:

```yaml wrap
engine:
  id: copilot
  driver: .github/drivers/custom-copilot-driver.js
max-tool-denials: 8
```

```yaml wrap
engine:
  id: copilot
  copilot-sdk: true
max-tool-calls: 100
```

`engine.driver` must be a **relative path from the workspace root**
(no absolute paths, `..`, backslashes, or shell metacharacters). It supports:

- script filenames ending with `.js`, `.cjs`, `.mjs`,
  `.py`, `.ts`, `.mts`, or `.rb`
- bare command names without an extension (resolved from
  `PATH`)
- inline source blocks using exactly one of `node:`,
  `python:`, `go:`, or `java:`

Inline driver sources are materialized into runtime files under
`${GITHUB_WORKSPACE}/.gh-aw/copilot-sdk/` before execution.
This lets the agent emit a complete Copilot SDK driver inline in
workflow frontmatter:

```yaml wrap
engine:
  id: copilot
  driver:
    python: |
      import sys
      print("hello from inline driver", file=sys.stderr)
```

See [Copilot SDK Driver Specification](/gh-aw/specs/copilot-sdk-driver-specification/)
for the full driver contract.

### SDK driver environment variables

The specification defines the driver environment contract.
In SDK mode, gh-aw injects required runtime values:

- `GH_AW_PROMPT`
- `COPILOT_SDK_URI`
- `COPILOT_CONNECTION_TOKEN`

`COPILOT_MODEL` is required and must be set to the model to use
(e.g. `gpt-4o`, `claude-sonnet-4`). Drivers MUST fail fast when
it is not set.

For runtime controls, the driver should consume:

- `COPILOT_SDK_SEND_TIMEOUT_MS`
- `COPILOT_SDK_LOG_LEVEL`

In gh-aw, `COPILOT_SDK_SEND_TIMEOUT_MS` is usually injected
automatically from workflow `timeout-minutes` (via
`GH_AW_TIMEOUT_MINUTES`) with safety headroom. Override it in
`engine.env` only when you need a custom SDK send timeout.
`COPILOT_SDK_LOG_LEVEL` is a host-provided driver control and
should be honored when gh-aw passes it to the driver process.

Do not set `COPILOT_CONNECTION_TOKEN` manually. The harness
generates it per run and passes the same token to both the
sidecar and driver process.

```yaml wrap
engine:
  id: copilot
  driver: .github/drivers/my_driver.ts
  model: gpt-5
  env:
    COPILOT_SDK_SEND_TIMEOUT_MS: "900000"
    COPILOT_SDK_LOG_LEVEL: info
```

## Session artifacts

SDK sessions retain `model.call_finished` and durable native events, including file-edit details and human-response provenance. Live `tool.shell_output` chunks are excluded from artifacts/stderr to avoid leaking secrets split across chunks. A finished model dispatch is not workflow completion. See the [unified session specification](/gh-aw/specs/unified-agent-session-specification/) for event and accounting contracts.
