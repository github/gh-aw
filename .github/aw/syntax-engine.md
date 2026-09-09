---
description: `engine:` frontmatter field detail for GitHub Agentic Workflows.
---

# Engine Configuration

See [syntax-agentic.md](syntax-agentic.md) for the full frontmatter field index.

- **`engine:`** - AI processor configuration
  - String format: `"copilot"` (current default), `"claude"`, `"codex"`, `"gemini"`, or `"pi"`. Omit `engine:` when there is no engine preference or engine-specific requirement so the configured default remains in effect. If an explicit model requirement forces engine selection, try Copilot first.
  - The experimental `opencode` engine is available through `imports: [shared/opencode.md]`; see [`smoke-opencode.md`](../workflows/smoke-opencode.md) for an example.
  - The experimental `deepseek-harness` engine is available through `imports: [shared/deepseek-harness.md]`; see [`smoke-deepseek-harness.md`](../workflows/smoke-deepseek-harness.md) for an example. It runs the developer-preview `dsh` headless profile with AWF provider routing and uses `provider/model` syntax.
  - The experimental `cursor` engine is available through `imports: [shared/cursor.md]`; see [`smoke-cursor.md`](../workflows/smoke-cursor.md) for an example. Requires the `CURSOR_API_KEY` secret. Cursor reads project rules from `.cursor/rules/*.mdc` and respects the root-level `.cursorignore` and `AGENTS.md`; both are protected in the manifest. Use `model: cursor/auto` or a specific model such as `cursor/claude-3-7-sonnet`.
  - The experimental `kiro` engine is available through `imports: [shared/kiro.md]`; see [`smoke-kiro.md`](../workflows/smoke-kiro.md) for an example. Requires the `KIRO_API_KEY` secret. Kiro reads steering documents from `.kiro/steering/` and hook definitions from `.kiro/hooks/`; these directories and `AGENTS.md` are protected in the manifest. Model must use `kiro/` prefix, e.g. `model: kiro/claude-sonnet-4-5`.
  - The experimental `pydantic-ai` engine is available through `imports: [shared/pydantic.md]`; see [`smoke-pydantic.md`](../workflows/smoke-pydantic.md) for an example. It runs the Pydantic AI `pai` CLI with the `pydantic-ai-harness` coder agent and uses `provider/model` syntax.
  - The experimental `crush`, `aider`, `goose`, and `custom` (GenAIScript) engines are also available through `imports: [shared/<engine>.md]`; see [`smoke-crush.md`](../workflows/smoke-crush.md), [`smoke-aider.md`](../workflows/smoke-aider.md), and [`smoke-goose.md`](../workflows/smoke-goose.md) for examples. Full list of import-based engines: see `.github/aw/engines.json`.
  - Object format for extended configuration:

    ```yaml
    engine:
      id: copilot                       # Required: coding agent identifier (copilot, claude, codex, gemini, pi)
      version: beta                     # Optional: version of the action (has sensible default); also accepts GitHub Actions expressions: ${{ inputs.engine-version }}
      model: gpt-5                      # Deprecated alias for the top-level `model`; prefer the top-level field
      permission-mode: acceptEdits      # Optional (claude only): auto | acceptEdits | plan | bypassPermissions. Default: acceptEdits (auto when tools.edit is false)
      agent: technical-doc-writer       # Optional: custom agent file (Copilot only, references .github/agents/{agent}.agent.md)
      max-turns: 5                      # Deprecated alias for the top-level `max-turns`; prefer the top-level field
      max-continuations: 3              # Optional: max autopilot continuations (copilot only; >1 enables --autopilot mode, default: 1)
      concurrency: "gh-aw-${{ github.workflow }}"  # Optional: agent job concurrency group (string or GitHub Actions concurrency object)
      env:                              # Optional: custom environment variables (object)
        DEBUG_MODE: "true"
      args: ["--verbose"]               # Optional: custom CLI arguments injected before prompt (array)
      api-target: api.acme.ghe.com      # Optional: custom API endpoint hostname for GHEC/GHES (hostname only, no protocol/path)
      command: /usr/local/bin/copilot   # Optional: override default engine executable (skips installation)
      bare: true                        # Optional: disable automatic context loading. Only supported by 'copilot' (--no-custom-instructions) and 'claude' (--bare); ignored with a warning on other engines. Default: false
      user-agent: "myapp/1.0"           # Optional: custom user agent string (codex engine only)
      config: |                         # Optional: additional TOML config appended to config.toml (codex engine only)
        [extra]
        key = "value"
    ```

  - **`gemini` engine**: Google Gemini CLI. Requires `GEMINI_API_KEY` secret. Does not support `web-search`. Supports AWF firewall and LLM gateway.
  - **`engine.driver:`** — canonical field to run a custom inner driver script instead of the engine's built-in CLI. For the `pi` engine it launches the driver directly with Node.js (e.g. built-in `pi_agent_core_driver.cjs`, or a workspace-relative path like `.github/drivers/pi_agent_core_driver_sample_node.cjs`); the driver must emit JSONL compatible with `parse_pi_log.cjs` so step summaries and token tracking keep working. Accepts a bare basename (resolved from the setup-action directory) or a workspace-relative path; no absolute paths, no `..`, only `.js`/`.cjs`/`.mjs` (pi).
  - **`copilot-sdk`** (copilot only): set `copilot-sdk: true` to start a headless Copilot CLI SDK sidecar. **`engine.driver`** (experimental, copilot only): set `driver: <path-or-command>` to supply a custom SDK driver (`.js`/`.cjs`/`.mjs`/`.py`/`.ts`/`.mts`/`.rb`, or a bare PATH command); this also enables `copilot-sdk: true` automatically. Tune the repeated-tool-denial safeguard with the top-level `max-tool-denials:` field (default `5`).

    **Inline driver source** (copilot engine only): instead of pointing to a checked-in file, you can embed the driver source directly in the frontmatter using an object with exactly one runtime key (`node`, `python`, `go`, or `java`). The compiler materializes the source under `.gh-aw/copilot-sdk/` at runtime and generates a launcher wrapper. The required SDK package is installed automatically.

    ```yaml
    # Node.js / TypeScript inline driver (SDK installed via npm)
    engine:
      id: copilot
      driver:
        node: |
          const sdk = require("@github/copilot-sdk");
          // ... driver implementation
    ```

    ```yaml
    # Python inline driver (SDK installed via pip into workspace target dir)
    engine:
      id: copilot
      driver:
        python: |
          import sys
          from github_copilot_sdk import CopilotAgent
          # ... driver implementation
    ```

    ```yaml
    # Go inline driver (SDK installed via go get; go.mod generated automatically)
    engine:
      id: copilot
      driver:
        go: |
          package main
          import "github.com/github/copilot-sdk/go"
          func main() { /* driver implementation */ }
    ```

    ```yaml
    # Java inline driver (SDK resolved via Maven pom.xml generated automatically)
    engine:
      id: copilot
      driver:
        java: |
          public class Main {
              public static void main(String[] args) { /* driver implementation */ }
          }
    ```

    Constraints: exactly one runtime key per `driver` object; source must be non-empty; only supported on the `copilot` engine. Use `runtimes.<id>.version` to pin the runtime version used for the generated module files (e.g. `runtimes.go.version: "1.22"`).
  - **`engine.auth:`** — keyless Workload Identity Federation via the AWF API proxy instead of a static API key; requires `id-token: write`. Set `type: github-oidc` (only supported type) plus `provider: azure` (`azure-tenant-id`, `azure-client-id`, optional `azure-scope`/`azure-cloud`) for Azure OpenAI, `provider: anthropic` (`federation-rule-id`, `organization-id`, `service-account-id`, `workspace-id`) for Claude, or `provider: gcp` (`workload-identity-provider`, `service-account`, optional `project`/`location`, default region `us-central1`) for Vertex AI / Gemini Enterprise. Optional `audience:`. Maps to `AWF_AUTH_*` env vars.
  - **Advanced engine sub-fields** (see the `engine_config` definition in `pkg/parser/schemas/main_workflow_schema.json`): `model-provider` (`github` | `anthropic` | `openai`), `harness` (`max-retries`/`initial-delay-ms`/`backoff-multiplier`/`max-delay-ms` retry policy, plus `watchdog-timeout` — a post-result idle-process watchdog, in seconds, for the built-in Copilot/Codex harnesses), engine-level `mcp` (`session-timeout`/`tool-timeout`), `extensions`, and `cwd`. See [Harness Settings and Runtime Tuning Variables](https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/environment-variables.md#harness-settings-and-runtime-tuning-variables) for defaults, units, and `GH_AW_HARNESS_*` env var equivalents.
