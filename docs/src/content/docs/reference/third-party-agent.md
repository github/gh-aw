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
| Atomic | Pins `@bastani/atomic` 0.9.79 with npm lifecycle scripts disabled. Uses AWF-only Copilot, Anthropic, or OpenAI/Codex inference, stdin prompts, private temporary configuration, and native authenticated HTTP MCP with direct tool exposure. The child environment allowlists runtime bindings and credential-free proxy addresses; arbitrary workflow secrets and execution hooks are not inherited. Startup network checks, telemetry, and themes are disabled; `--approve` loads base-restored `.atomic/` extensions, workflows, skills, prompt templates, and settings while the isolated agent directory excludes user-installed extensions. Saves sessions and workflow runs under `/tmp/gh-aw/agent/atomic-store` with `engine.durable` checkpoint continuation and optional libSQL (`@bastani/atomic-storage-libsql@0.9.79`) storage. Gateway MCP policies, including GitHub integrity and repository restrictions, remain enforced. Native bash restrictions and `edit: false` fail compilation. No native gh-aw tool allowlists, iteration limits, Agent Plugins, or structured session parsing. |
| OpenCode | Installs OpenCode 2 from `@opencode/cli`, uses `--standalone`, native MCP with Code Mode disabled, and JSONL events. Copilot requires AWF; direct Anthropic/OpenAI BYOK is supported without it. Repository JSON/JSONC settings are overlaid; updates/sharing are disabled. V2 has no language servers, and V1 plugins require porting. Native npm plugins are not gh-aw Agent Plugins. |
| DeepSeek Harness | Pins `dsh` v0.2.0-rc.2. Uses headless stdin prompts, a private per-run configuration patch, and CLI-proxy tools rather than native MCP. Copilot requires AWF; direct Anthropic/OpenAI credentials apply only without AWF. Per-command native bash restrictions are rejected. No native reasoning, tool, refusal, or token-accounting observations are inferred from stdout. |
| Factory Droid | Pins `@factory/cli` 0.248.0 with lifecycle scripts disabled. Uses custom models through the AWF API proxy with placeholder credentials; real provider and Factory keys are excluded from the agent. Supports Copilot, Anthropic, and OpenAI inference, file-based prompts, high autonomy inside the sandbox, and native HTTP MCP. Native MCP retains the gateway session authorization in agent-readable `.factory/mcp.json`; this is not an upstream provider credential or a security boundary against the agent (see [gateway authentication](/gh-aw/reference/mcp-gateway/#71-authorization-header-format)). Removing it prevents authenticated MCP calls. No Factory API key is required. Factory-managed inference, direct-key execution, structured session parsing, gh-aw tool allowlists, max-turn enforcement, and Agent Plugins are unsupported. |
| Goose | Pins CLI v1.53.0 with archive checksum verification. Uses OpenAI-compatible inference, native authenticated MCP, file-based prompts, and JSONL tool/usage events. Configuration/session state is temporary; keyring is disabled. No native gh-aw `web-fetch`; use MCP or shell. Copilot can use `copilot-requests: write` without a PAT. |
| Kiro | Sample pins CLI 2.31.1 with the V2 harness; Powers plugins are not supported. |
| mini-SWE-agent | Pins PyPI 2.4.6 on Python 3.12. Uses the non-interactive Python API, text-based shell actions, file-based prompts, and AWF-only Copilot, OpenAI/Codex, or Anthropic inference. Native task submission is required for success; `max-turns` limits model calls. Tools use CLI proxies, not native MCP. The untrusted native trajectory is retained in the unified agent artifact only after secret redaction and recovery-envelope validation, including failed executions. No native shell allowlists, edit disabling, Agent Plugins, repository mini configuration, or structured session parser. |

Read the pinned definition for its exact install and provider contract. Native events and accounting are retained only when observed; process exit zero alone does not prove task completion.

### Atomic sessions and workflow runs

Every Atomic run saves its sessions, workflow journal, and workflow run artifacts in `/tmp/gh-aw/agent/atomic-store`. Subagents, workflow stages, and `history://` share that store during the job. On a run without `engine.durable` (such as a `workflow_call` or `work-queue` worker), that folder is deleted with the runner when the job ends, and only the redacted plain-text output log stays in the `agent` artifact.

Every standalone Atomic workflow should turn on `engine.durable` so sessions and workflow runs carry across runs:

```yaml wrap
engine:
  id: atomic
  durable:
    retention-days: 7
```

With `engine.durable` set:

- After the agent exits, gh-aw checks `/tmp/gh-aw/agent/atomic-store` for credentials and uploads it as a checkpoint artifact kept for `retention-days` (1 to 90, default 7, capped by repository and organization retention settings).
- On the next run on the same branch, or from an explicit `aw_context` parent run, gh-aw restores `/tmp/gh-aw/agent/atomic-store` before Atomic starts. The harness reads the saved root session ID and passes `--session <id>` so Atomic continues that conversation (`--continue` if no session ID file is present).
- Interrupted Atomic workflow runs stay in the restored journal. The harness does not resume workflow runs on its own, so the agent can inspect and resume them with the `workflow` tool.

Durable checkpoint artifacts hold restorable session history, including prompts, tool output, and file contents the agent read. Anyone who can download workflow artifacts in the repository can read them, and secret redaction is heuristic. Turn on `engine.durable` only where that fits your repository's artifact access and retention rules. Because `--approve` loads `.atomic/settings.json`, leave `storage`, `sessions.storage`, and `workflows.journal` out of `.atomic/settings.json` so repository settings do not redirect sessions and workflow runs away from `/tmp/gh-aw/agent/atomic-store`.

#### Use Turso or your own sqld server (optional)

Point Atomic at a libSQL database (Turso or a self-hosted `sqld` server) when artifact checkpoints are not enough:

- Every session and workflow run stays past GitHub Actions artifact expiry, even if a job is cancelled or a runner is lost before artifact upload. Join the same database from your machine to search history with `history://`, inspect runs with `/workflow status`, or resume a session with `/resume`.
- A workflow run cut off by a timeout or a cancelled job can be resumed from your machine or from a later run. Atomic gives each run one executor at a time. Once the owner has stopped renewing its lease for about two minutes, any machine can take the run over, and two machines resuming at once cannot both win.
- Concurrent runs can message each other with Intercom, and so can your own Atomic sessions. Brokers exchange messages through the database, so runners need no network path to each other or to you. Sessions of the same repository share the group `project:<owner>/<repo>`; other repositories in the same database cannot list or message them.

##### Set up Turso

```bash
turso db create gh-aw-atomic
gh secret set ATOMIC_STORAGE_URL --body "$(turso db show gh-aw-atomic --url)"
turso db tokens create gh-aw-atomic | gh secret set ATOMIC_STORAGE_TOKEN
```

The Atomic engine definition installs `@bastani/atomic-storage-libsql` declaratively alongside the CLI, using the same effective pinned engine version and npm release cooldown. When `ATOMIC_STORAGE_URL` is configured, a pre-agent step only masks and stages the credentials in a private file; it does not install packages. Enable external storage on a workflow with `ATOMIC_STORAGE: "on"` in `engine.env` and allow the database host through the firewall. The compiler rejects this setting in strict mode and warns otherwise because agent-controlled session and workflow data reaches the external database before post-run threat detection:

```yaml wrap
engine:
  id: atomic
  durable:
    retention-days: 7
  env:
    ATOMIC_STORAGE: "on"
network:
  allowed:
    - defaults
    - gh-aw-atomic-<org>.turso.io
```

Workflows in the same repository that omit `ATOMIC_STORAGE: "on"` delete the staging file before Atomic starts and keep using `/tmp/gh-aw/agent/atomic-store`.

##### Use your own sqld server

`sqld` is the libSQL server, a SQLite-compatible database you can host yourself. Save its address (`libsql://`, `https://`, or `wss://`) in the `ATOMIC_STORAGE_URL` secret and a token it accepts in `ATOMIC_STORAGE_TOKEN`, then set `ATOMIC_STORAGE: "on"` in `engine.env`. If the server runs with `--enable-namespaces`, also set `ATOMIC_STORAGE_NAMESPACE` in `engine.env` (supported with `libsql://`, `https://`, and `http://` URLs); each namespace is a separate database with its own token. A plain `http://` URL without a token works for a server without authentication on a private network, for example from self-hosted runners, but the traffic is not encrypted. Add the server's host to `network.allowed`.

##### Read CI runs from your machine

The harness stores each repository's data under a prefix made from the lowercased, sanitized owner/repository name and the first 32 hexadecimal characters of its SHA-256 hash. The hash is computed from the full lowercased `owner/repo` identity so names that sanitize to the same text remain separate. Join the database with the same prefix:

```bash
GITHUB_REPOSITORY="owner/repo"
repository="$(printf '%s' "$GITHUB_REPOSITORY" | tr '[:upper:]' '[:lower:]')"
sanitized_repository="$(printf '%s' "$repository" | sed 's/[^a-z0-9_]/_/g')"
repository_hash="$(printf '%s' "$repository" | { sha256sum 2>/dev/null || shasum -a 256; } | cut -c1-32)"
repository_prefix="gh_aw_${sanitized_repository}_${repository_hash}"
atomic install npm:@bastani/atomic-storage-libsql
atomic storage join "$(turso db show gh-aw-atomic --url)" --token-file turso-token --prefix "$repository_prefix"
```

This points your global Atomic settings at the shared database. To go back to your local store, remove the `storage` setting from `~/.atomic/agent/settings.json`.

##### Before you turn it on

- The agent can read files in its own agent directory, including the token. Create a token for this one database and rotate it on a schedule.
- Anyone with the token can read every saved session: prompts, tool output, and the file contents the agent read. Protect the database as you would the repository's logs.
- Intercom message bodies stay in the database until shortly after delivery.
- Because `--approve` loads `.atomic/settings.json`, a `storage`, `sessions.storage`, or `workflows.journal` setting in `.atomic/settings.json` overrides the harness's database for sessions and workflow runs while Intercom keeps using the shared database. Leave those keys out of `.atomic/settings.json`.
- If the database is unreachable, the run fails. Atomic never falls back to a temporary store.

### mini-SWE-agent package access

The mini-SWE sample downloads hash-locked binary wheels in trusted `pre-agent-steps`,
before AWF starts. Agent-time installation is isolated and offline; pip verifies
the same hashes again. No dependencies are installed into the host user site.

The shared import also declares `runtimes.python`, which explicitly grants the
agent Python ecosystem access, including `pypi.org` and `files.pythonhosted.org`.
Engine defaults do not grant this access. For registry-free execution, vendor
the definition without `runtimes`, retain its bootstrap steps, and provide Python
3.12 on a Linux x86-64 runner independently. Callers can opt back in with
`network.allowed: [python]` or `runtimes.python`.

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
