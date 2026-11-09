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
| [Atomic](https://github.com/bastani-inc/atomic) | [atomic.md](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/atomic.md) |
| [Crush](https://github.com/charmbracelet/crush) | [crush.md](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/crush.md) |
| [Cursor](https://cursor.com/docs/cli) | [cursor.md](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/cursor.md) |
| [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) | [deepseek-harness.md](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/deepseek-harness.md) |
| [Factory Droid](https://factory.com/product/cli) | [factory.md](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/factory.md) |
| [Goose](https://github.com/aaif-goose/goose) | [goose.md](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/goose.md) |
| [Kiro](https://kiro.dev/) | [kiro.md](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/kiro.md) |
| [mini-SWE-agent](https://github.com/SWE-agent/mini-swe-agent) | [mini-swe-agent.md](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/mini-swe-agent.md) |
| [Pydantic AI](https://ai.pydantic.dev/) | [Publisher definition](https://github.com/pydantic/pydantic-ai/blob/main/src/pydantic_ai_harness/gh-aw/pydantic.md) |

### Sample-specific constraints

| Sample | Runtime and limitations |
| --- | --- |
| Atomic | Pins `@bastani/atomic` 0.9.71 with npm lifecycle scripts disabled. Uses AWF-only Copilot, Anthropic, or OpenAI/Codex inference, stdin prompts, private temporary configuration, and native authenticated HTTP MCP with direct tool exposure. The child environment allowlists runtime bindings and credential-free proxy addresses; arbitrary workflow secrets and execution hooks are not inherited. Startup network checks, telemetry, saved sessions, and project-local configuration are disabled; the isolated agent directory excludes user-installed extensions. Gateway MCP policies, including GitHub integrity and repository restrictions, remain enforced. Native bash restrictions and `edit: false` fail compilation. No native gh-aw tool allowlists, iteration limits, Agent Plugins, custom Atomic process workflows, or structured session parsing. |
| OpenCode | Installs OpenCode 2 from `@opencode/cli`, uses `--standalone`, native MCP with Code Mode disabled, and JSONL events. Copilot requires AWF; direct Anthropic/OpenAI BYOK is supported without it. Repository JSON/JSONC settings are overlaid; updates/sharing are disabled. V2 has no language servers, and V1 plugins require porting. Native npm plugins are not gh-aw Agent Plugins. |
| DeepSeek Harness | Pins `dsh` v0.2.0-rc.2. Uses headless stdin prompts, a private per-run configuration patch, and CLI-proxy tools rather than native MCP. Copilot requires AWF; direct Anthropic/OpenAI credentials apply only without AWF. Per-command native bash restrictions are rejected. No native reasoning, tool, refusal, or token-accounting observations are inferred from stdout. |
| Factory Droid | Pins `@factory/cli` 0.248.0 with lifecycle scripts disabled. Uses custom models through the AWF API proxy with placeholder credentials; real provider and Factory keys are excluded from the agent. Supports Copilot, Anthropic, and OpenAI inference, file-based prompts, high autonomy inside the sandbox, and native HTTP MCP. Native MCP retains the gateway session authorization in agent-readable `.factory/mcp.json`; this is not an upstream provider credential or a security boundary against the agent (see [gateway authentication](/gh-aw/reference/mcp-gateway/#71-authorization-header-format)). Removing it prevents authenticated MCP calls. No Factory API key is required. Factory-managed inference, direct-key execution, structured session parsing, gh-aw tool allowlists, max-turn enforcement, and Agent Plugins are unsupported. |
| Goose | Pins CLI v1.53.0 with archive checksum verification. Uses OpenAI-compatible inference, native authenticated MCP, file-based prompts, and JSONL tool/usage events. Configuration/session state is temporary; keyring is disabled. No native gh-aw `web-fetch`; use MCP or shell. Copilot can use `copilot-requests: write` without a PAT. |
| Kiro | Sample pins CLI 2.31.1 with the V2 harness; Powers plugins are not supported. |
| mini-SWE-agent | Pins PyPI 2.4.6 on Python 3.12. Uses the non-interactive Python API, text-based shell actions, file-based prompts, and AWF-only Copilot, OpenAI/Codex, or Anthropic inference. Native task submission is required for success; `max-turns` limits model calls. Tools use CLI proxies, not native MCP. The untrusted native trajectory is retained in the unified agent artifact only after secret redaction and recovery-envelope validation, including failed executions. No native shell allowlists, edit disabling, Agent Plugins, repository mini configuration, or structured session parser. |

Read the pinned definition for its exact install and provider contract. Native events and accounting are retained only when observed; process exit zero alone does not prove task completion.

DeepSeek Harness captures attributed startup errors, matching failed wrapper exits, and the observed AWF timeout diagnostic as `session.error` events. The generated host detector preserves these errors even when the canonical artifact contains only session initialization, without duplicating errors already captured there. Live step failure or timeout metadata also supplies an error when stdout has no diagnostic. These failures survive unified-session collection even if a wrapper records exit zero: an observed nonzero code is retained, otherwise `agent.execution.exitCode` is omitted rather than fabricated. A timeout does not imply native reasoning, tool, usage, or final-answer coverage.

## Defining an engine

A definition contains `engine.id`, metadata, and `engine.behaviors`. Its behaviors install the CLI, configure inference and MCP, invoke it headlessly, and expose logs for session reconstruction. Keep secrets outside the agent sandbox and fail explicitly when required credentials, endpoints, or restrictions cannot be honored.

Start from a sample with a matching transport rather than copying installation or configuration code into every workflow. Publish the definition with versioned changes and document its credentials, models, tool restrictions, supported platforms, and [unified session](/gh-aw/specs/unified-agent-session-specification/) coverage.

See [imports](/gh-aw/reference/imports/) for resolution and pinning rules.

### Secret environment overrides

An imported definition declares supported environment variable names in
`engine.behaviors.supported-env-var-keys`. In strict mode, these names are
allowed to carry secret references in the consuming workflow's `engine.env`.
The compiler checks those overrides after resolving imports and registering
the engine, so the imported definition's allowlist applies.

This does not permit arbitrary secret-bearing variables: undeclared keys in
`engine.env` remain subject to strict-mode validation, and secret references in
top-level `env` are still rejected. In strict mode, secret overrides require
AWF v0.25.3 or newer to exclude them from the agent container. See
[workflow environment variables](/gh-aw/reference/frontmatter/#environment-variables-env)
for the restrictions on top-level `env`.

### Durable execution

> [!WARNING]
> Durable execution is experimental, including for imported engines. Enabling
> `engine.durable` emits a compile-time warning. Configuration and behavior may
> change without deprecation.

A workflow can opt into artifact-backed continuation using `engine.durable:
{ retention-days: 7 }` alongside its custom `engine.id`. Compilation fails unless
the definition declares `behaviors.durable`. Engines that implement the Go
interface provide `GetDurableExecutionPlan` with the files to persist and the
continuation execution steps.

An engine does not need a native durable library. It can declare its session
folder and provide a harness that continues the restored session:

```aw wrap
engine:
  id: example-engine
  behaviors:
    durable:
      files:
        - /tmp/gh-aw/agent/example/sessions
      execution:
        command-name: example-engine
        step-name: Continue example-engine session
      harness-script: |
        const resumed = process.env.GH_AW_DURABLE_RESUMED === "true";
        // Invoke this engine's resume command when resumed, or start a new session.
```

Merge this fragment into the publisher's existing definition. Configure the
agent to write its sessions to the declared directory. The harness must use its
engine-specific continuation API or CLI flags when `GH_AW_DURABLE_RESUMED` is
`true`; the parent run is available as `GH_AW_DURABLE_PARENT_RUN_ID`. No extra
durable dependencies are needed for session-file continuation. This restores
conversation state, not the native library's durable task or replay guarantees.

`behaviors.durable.files` is required. Paths must be explicit, non-overlapping
files or directories under `/tmp/gh-aw/agent/`, without globs or symlinks. gh-aw
recursively snapshots only these paths and restores them before the harness
runs. A native implementation can declare `$GH_AW_DURABLE_CHECKPOINT` to persist
the compiler-managed checkpoint file instead.

The `execution` object is required; `installation` and `harness-script` are
optional. When a native implementation needs extra dependencies, each dependency
uses the existing installation fields: `package-manager: npm`, a valid
`package-name`, an exact literal `version`, and a `verify-command`. Durable
dependencies are installed and verified in addition to the ordinary engine
installation. The durable execution and harness replace the ordinary ones;
workflow-level command, driver, and harness overrides are rejected.
Durable and ordinary execution must use the same `protected-session-capture`
setting so capture verification and publication remain consistent.

The harness also receives `GH_AW_DURABLE_STORAGE=artifact`,
`GH_AW_DURABLE_DIR`, and `GH_AW_DURABLE_CHECKPOINT`. Persist only session state,
never credentials or runtime authentication. gh-aw restores and publishes the artifact and applies
the configured retention period. The engine is responsible for checkpoint
compatibility, replay safety, fresh credential binding, telemetry, and unified
session events. See [Durable runs](/gh-aw/reference/engines/#durable-runs)
for parent selection, retention, and continuation limitations.

Artifact files include an integrity-checked UTF-8 projection so host secret
redaction also examines binary session state. If redaction changes a snapshot,
publication fails instead of uploading credentials or a corrupted session.

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

Protected verification and publication run before safe-output ingestion. The trusted parser step receives configured secret values for publication redaction; these bindings are not added to the guest execution environment. Outcome collection uses only the verified canonical agent session, without shared-log or error-sidecar recovery. Both unified and fallback artifact uploads require successful protected parsing.

The definition's parser is repository-authorized executable configuration, not untrusted captured data. It runs on the runner in `actions/github-script`, outside the agent sandbox, with the agent job's GitHub token permissions and runner network access. Protected capture does not isolate malicious engine definitions: review and pin the parser along with the definition's other executable behaviors before importing it. Secret bindings are limited to references collected from the workflow environment and generated workflow YAML; their values are required for exact-match redaction of configured secrets that have no recognizable token format. Supplying only secret names would leave those values exposed in published artifacts and summaries.

> [!CAUTION]
> Native capture protects storage, not the origin of guest-side bytes. The engine must isolate its recorder from same-UID tools and verify its session framing before returning events. A matching key prefix or a public-key header on an otherwise accessible pipe is not authentication. The compiler explicitly `exec`s the final harness, but the harness must replace remaining launchers, protect its key and capture descriptors, route ordinary tool output to stdout, and reject invalid or incomplete signed streams without a legacy-parser fallback. Pydantic's existing keyed-stdout integration must adapt before enabling this option.

The [session ingestion specification](/gh-aw/specs/unified-agent-session-specification/#protected-declarative-session-ingestion) defines the source, verification, and publication requirements. Compile a consuming workflow with `gh aw compile <workflow-name> --strict` to check configuration and generated wiring; compilation does not prove recorder authenticity or descriptor isolation.

Engine authors can exercise the generated launcher's Linux descriptor boundary with `GH_AW_NATIVE_CAPTURE_E2E=1 go test -tags integration ./pkg/workflow -run '^TestProtectedSessionCaptureAWFDescriptorIsolation$' -count=1`. This opt-in regression requires the pinned `awf` CLI, Docker, and a `RUNNER_TEMP` outside agent-visible mounts. It uses a non-dumpable Python recorder reached through Node `execve`; it does not validate an engine's signing protocol.
