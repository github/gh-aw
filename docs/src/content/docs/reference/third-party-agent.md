---
title: Custom AI Engines
description: Import declarative engine definitions and configure third-party coding agents.
sidebar:
  order: 330
---

Third-party coding agents integrate through a Markdown engine definition, maintained by the engine's publisher. Its `engine.behaviors` frontmatter declares installation, configuration, execution, and log parsing. Importing the definition registers the engine at compile time without changing the gh-aw binary.

For built-in engines and their feature matrix, see [AI engines](/gh-aw/reference/engines/).

## Shared imported engines

Custom engines require object form with `engine.id`. String form accepts only built-in IDs; there is no `engine: custom`.

```yaml wrap
engine:
  id: example-engine
imports:
  - owner/repository/.github/workflows/example-engine.md@v1.2.3
```

The ID must match the imported definition. Prefer a publisher-maintained definition pinned to a reviewed commit SHA or tag. Local definitions can be vendored under `.github/workflows/shared/` and imported by path.

An imported definition is executable configuration: review its installation commands, permissions, network access, secret handling, tool restrictions, and parser. Compatibility and support belong to its publisher; gh-aw does not guarantee native-agent features or restrictions merely because a definition compiles.

Agent Plugins are available only when the definition declares `behaviors.plugins`; see [plugin configuration](/gh-aw/reference/frontmatter/#agent-plugins-plugins).

## Authentication and inference

Credentials, model prefixes, and provider routing depend on the definition. Configure the secrets and network destinations it declares. A custom engine can support multiple inference providers, but that does not imply compatibility with every provider API.

`safe-outputs.threat-detection: true` uses Copilot by default, even when the main agent is custom. Supply Copilot authentication or choose a supported detection engine explicitly; see [Threat detection](/gh-aw/reference/threat-detection/).

## Unsupported engine samples

These definitions are examples for integration authors, **not supported gh-aw engines**. They have no compatibility or maintenance commitment. Production users should obtain a definition maintained by the engine owner.

| Engine | Sample definition |
| --- | --- |
| [OpenCode](https://opencode.ai) | [opencode.md](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/opencode.md) |
| [Aider](https://aider.chat/docs/) | [aider.md](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/aider.md) |
| [Crush](https://github.com/charmbracelet/crush) | [crush.md](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/crush.md) |
| [Cursor](https://cursor.com/docs/cli) | [cursor.md](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/cursor.md) |
| [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) | [deepseek-harness.md](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/deepseek-harness.md) |
| [Goose](https://github.com/aaif-goose/goose) | [goose.md](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/goose.md) |
| [Kiro](https://kiro.dev/) | [kiro.md](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/kiro.md) |
| [Pydantic AI](https://ai.pydantic.dev/) | [Publisher definition](https://github.com/pydantic/pydantic-ai/blob/main/src/pydantic_ai_harness/gh-aw/pydantic.md) |

### Sample-specific constraints

| Sample | Runtime and limitations |
| --- | --- |
| OpenCode | Installs OpenCode 2 from `@opencode/cli`, uses `--standalone`, native MCP with Code Mode disabled, and JSONL events. Copilot requires AWF; direct Anthropic/OpenAI BYOK is supported without it. Repository JSON/JSONC settings are overlaid; updates/sharing are disabled. V2 has no language servers, and V1 plugins require porting. Native npm plugins are not gh-aw Agent Plugins. |
| DeepSeek Harness | Pins `dsh` v0.2.0-rc.2. Uses headless stdin prompts, a private per-run configuration patch, and CLI-proxy tools rather than native MCP. Copilot requires AWF; direct Anthropic/OpenAI credentials apply only without AWF. Per-command native bash restrictions are rejected. No native reasoning, tool, refusal, or token-accounting observations are inferred from stdout. |
| Goose | Pins CLI v1.53.0 with archive checksum verification. Uses OpenAI-compatible inference, native authenticated MCP, file-based prompts, and JSONL tool/usage events. Configuration/session state is temporary; keyring is disabled. No native gh-aw `web-fetch`; use MCP or shell. Copilot can use `copilot-requests: write` without a PAT. |
| Kiro | Sample pins CLI 2.31.1 with the V2 harness; Powers plugins are not supported. |

Read the pinned definition for its exact install and provider contract. Native events and accounting are retained only when observed; process exit zero alone does not prove task completion.

## Defining an engine

A definition contains `engine.id`, metadata, and `engine.behaviors`. Its behaviors install the CLI, configure inference and MCP, invoke it headlessly, and expose logs for session reconstruction. Keep secrets outside the agent sandbox and fail explicitly when required credentials, endpoints, or restrictions cannot be honored.

Start from a sample with a matching transport rather than copying installation or configuration code into every workflow. Publish the definition with versioned changes and document its credentials, models, tool restrictions, supported platforms, and [unified session](/gh-aw/specs/unified-agent-session-specification/) coverage.

See [imports](/gh-aw/reference/imports/) for resolution and pinning rules.

### Protected session capture

`engine.behaviors.execution.protected-session-capture` is a boolean, defaulting to `false`. It belongs to the shared declarative engine definition, not the workflow root or built-in `engine.harness`. Merge this fragment into a definition that already has its installation, isolated recorder, and verifying `behaviors.log-parser`:

```aw wrap
engine:
  id: example-engine
  behaviors:
    execution:
      command-name: example-engine
      protected-session-capture: true
```

The consuming workflow imports the compatible definition:

```aw wrap
imports:
  - shared/example-engine.md
engine:
  id: example-engine
sandbox:
  agent:
    runtime: docker
```

`engine.behaviors.execution.protected-session-capture: true` uses AWF's native `--capture-command-output` (AWF v0.28.67 or newer). It requires a Linux runner and an enabled, non-TTY Docker runtime; external microVM runtimes and agent access to a Docker socket are unsupported. AWF validates effective mount isolation before execution, including aliases and custom mounts. Capture paths are scoped to the workflow run, attempt, job, and matrix index, with a separate explicit AWF working directory. Outer startup retries are disabled; AWF owns native attempt boundaries.

The definition's `parseLog(logContent)` receives a string and returns `{ markdown, logEntries, mcpFailures, maxTurnsHit }`. Protected publication requires nonempty canonical `logEntries` after recorder verification; a Markdown-only parser is insufficient. The harness does not create capture files or choose their path. gh-aw manages the native invocation and capture environment; the definition must not override them. `docker` is the default runtime, and `docker-sudo-iptables` is also supported.

The parser receives only the final started attempt's **stderr**, selected from `metadata.json`. Stdout, runner presentation, and sandbox-written `agent-stdio.log` are never fallback sources. Missing, incomplete, undrained, or truncated capture fails publication. A nonzero command exit can still have a complete capture; the native exit code remains authoritative over any shared exit-code sidecar. The 64 MiB capture limit applies to both streams and all attempts together. Raw captures remain private; only redacted structured events and normalized metrics are published. Configured workflow secrets and mask requests from either native stream are used to redact event payloads and summary copies; mask requests are never session observations. The sandbox-writable `agent-errors.jsonl` source and its temporary sibling are discarded. Redaction that would alter structural fields fails publication and removes stale session artifacts; unified session-artifact uploads are eligible only after verification, publication, and cleanup all succeed.

Protected session capture is not compatible with `evals`, which runs a secondary engine invocation that consumes `evals.log`; compilation rejects this combination.

> [!CAUTION]
> Native capture protects storage, not the origin of guest-side bytes. The engine must isolate its recorder from same-UID tools and verify its session framing before returning events. A matching key prefix or a public-key header on an otherwise accessible pipe is not authentication. The compiler explicitly `exec`s the final harness, but the harness must replace remaining launchers, protect its key and capture descriptors, route ordinary tool output to stdout, and reject invalid or incomplete signed streams without a legacy-parser fallback. Pydantic's existing keyed-stdout integration must adapt before enabling this option.

The [session ingestion specification](/gh-aw/specs/unified-agent-session-specification/#protected-declarative-session-ingestion) defines the source, verification, and publication requirements. Compile a consuming workflow with `gh aw compile <workflow-name> --strict` to check configuration and generated wiring; compilation does not prove recorder authenticity or descriptor isolation.

Engine authors can exercise the generated launcher's Linux descriptor boundary with `GH_AW_NATIVE_CAPTURE_E2E=1 go test -tags integration ./pkg/workflow -run '^TestProtectedSessionCaptureAWFDescriptorIsolation$' -count=1`. This opt-in regression requires the pinned `awf` CLI, Docker, and a `RUNNER_TEMP` outside agent-visible mounts. It uses a non-dumpable Python recorder reached through Node `execve`; it does not validate an engine's signing protocol.
