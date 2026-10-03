---
description: Guide for configuring new declarative agentic engines — engine definition fields, auth wiring, behavior blocks, and validation.
---

# Configure a New Agentic Engine

Use this guide when adding or updating a declarative engine definition in a repository that uses the `gh aw` extension. Do not assume that the gh-aw source repository, its build system, or its Go packages are available.

## Prefer shared agentic workflow definitions

For CLI-style engines, create a repository-scoped definition in `.github/workflows/shared/<id>.md`. Import it from a workflow that sets `engine.id: <id>`. Use GitHub to inspect the shared [OpenCode](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/opencode.md), [Goose](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/goose.md), [Aider](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/aider.md), [Cursor](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/cursor.md), and [Kiro](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/kiro.md) definitions as patterns.

- express the engine entirely through frontmatter-defined `engine.behaviors`
- keep install, config, execution, MCP, manifest, and capability metadata in the engine markdown file
- keep engine-specific adapters and harnesses with the shared definition
- stop and report the missing declarative capability when the runtime cannot be expressed by the supported schema; do not modify gh-aw internals from the consuming repository

## Gather the engine contract first

Do not begin from a generic engine template. Use GitHub access to inspect the CLI's repository, documentation, release notes, package manifests, configuration examples, and pinned release source. Answer every item below before editing files.

### LLM endpoint and model contract

1. Identify how the CLI accepts its LLM endpoint: environment variable, command flag, or config key.
2. Determine whether the endpoint is OpenAI-compatible, Anthropic-native, or another protocol, including any required path such as `/v1`.
3. Record the workflow-facing model syntax, normally `provider/model`, and the exact syntax passed to the CLI. Document any provider-prefix removal, replacement, or other model transformation.
4. Determine whether the CLI runs on the host or inside the AWF agent container. Do not copy a host URL into a container configuration or hard-code an `api-proxy` port or container IP.
5. Identify which configured provider must be selected at runtime and how to report an actionable error when the provider or model is unavailable.

### MCP contract

1. Determine whether the CLI has native MCP support. If it does not, set `engine.mcp: false` and rely on the compiler's proxy-backed tools.
2. If MCP is supported, identify the accepted transports, config path, root object name, server entry schema, and support for authorization headers.
3. Compare the native schema with the gateway's `{ "mcpServers": ... }` output. If they differ, generate the native config with `behaviors.mcp.config-adapter`.
4. Determine whether the CLI loads that config directly or requires a harness to translate servers into command-line flags or a runtime config overlay.
5. Determine whether CLI-mounted servers must be filtered and whether gateway URLs must use the host or container domain.
6. Treat generated MCP configuration as sensitive when it contains gateway authorization headers; create it with owner-only permissions.

### Installation, config, and execution contract

1. Choose a stable engine `id` and display name. Set `runtime-id` only when reusing a documented runtime adapter.
2. Identify the install source, package manager, package name, binary name, pinned version, and verification command.
3. Identify the config path and format, such as JSON, JSONC, YAML, or TOML. Determine whether gh-aw creates, replaces, or merges the file; do not use a JSON merge strategy for syntax that is only valid as JSONC.
4. Identify the non-interactive execution command, fixed arguments, prompt delivery mechanism, exit-code behavior, and any required environment variables.
5. List every engine-owned config file and directory in `behaviors.manifest`.
6. Identify required secrets and whether they use universal provider routing or engine-specific auth.

Do not implement the engine until each contract is known. If the available GitHub documentation and pinned source disagree, use the pinned release behavior and record the limitation in the shared definition.

## Choose the declarative mechanism

| Requirement | Mechanism |
|---|---|
| Shared provider credentials and environment | `secret-strategy: universal-llm-consumer` and `execution.provider-env-mode: universal-llm-consumer` |
| User-facing custom gateway upstream | `execution.api-target-env-var`, binding workflow `engine.api-target` to `OPENAI_BASE_URL`, `ANTHROPIC_BASE_URL`, or `GEMINI_API_BASE_URL` |
| Runner-side session credential exchange | Pair `execution.prepare-script` with `execution.prepare-env-var`; mask and emit exactly the prepared credential to stdout for AWF sidecar isolation, never perform inference in this hook |
| Model passed through an environment variable | `execution.model-env-var` |
| `provider/model` must be rewritten for the CLI | `execution.model-env-provider-prefix`, or a harness for more complex transformations |
| Static engine configuration | `behaviors.config-file` with the correct path, content, and merge strategy |
| Gateway MCP output already matches the CLI | `behaviors.mcp.config-path` and an execution MCP config binding |
| Gateway MCP output needs another schema | `behaviors.mcp.config-adapter` |
| MCP servers must become CLI flags or a runtime overlay | `execution.mcp-config-env-var` and `behaviors.harness-script` |
| Runtime endpoint discovery or custom invocation | `behaviors.harness-script` using `awf_reflect.cjs` |
| No native MCP client | `engine.mcp: false` |
| CLI emits logs the built-in parsers cannot normalize | `behaviors.log-parser` |

Use a `config-adapter` only to transform generated MCP configuration. Use a `harness-script` when endpoint selection, model transformation, prompt delivery, or CLI invocation requires runtime logic.

## Resolve LLM endpoints at runtime

Do not hard-code proxy ports or addresses in new shared engine definitions. Add a harness and resolve the configured endpoint from `/reflect` at execution time. Prefer the versioned helper beside the generated harness:

```javascript
const {
  fetchAWFReflect,
  resolveProviderEndpointFromReflect,
} = require("./awf_reflect.cjs");
```

The harness must:

1. require `AWF_REFLECT_ENABLED=1`; fail rather than enabling direct inference when AWF is disabled
2. call `fetchAWFReflect()` and require a successful response
3. select the requested provider from `GH_AW_LLM_PROVIDER`
4. use only an endpoint with `configured: true` for the requested provider (`requireProviderMatch: true`); never fall back to another provider
5. map the reflected `base_url` into every inference/control endpoint environment variable, flag, or config key used by the CLI
6. transform and validate the selected model using the discovered provider's syntax
7. read the prompt from `GH_AW_PROMPT`, spawn the CLI without shell interpolation, and preserve its exit status
8. fail with an actionable message when endpoint or model resolution is impossible

Use `resolveProviderEndpointFromReflect()` when the CLI accepts a base URL. Use `resolveOpenAICompatibleEndpointFromReflect()` when it needs an OpenAI-compatible host and request path separately, as in the shared Goose harness. Use `resolveMultiProviderFromReflect()` only when the CLI consumes a generated multi-provider catalog. Parse `/reflect` directly only when the shared helpers cannot represent the engine's contract.

`AWF_REFLECT_ENABLED=1` only indicates that reflection is available; it does not configure the CLI. The harness must fetch and apply the result. All LLM inference must use the gateway: do not use direct provider URLs, add direct inference domains to the network allowlist as a workaround, or fall back to native provider configuration when discovery fails.

Configure a custom upstream through `engine.env.OPENAI_BASE_URL` or `ANTHROPIC_BASE_URL`; gh-aw translates it to AWF's provider target and base path. Bind the provider key on the runner for the sidecar and pass only a placeholder to the CLI. Consult the pinned AWF `awf-config-spec.md` and `api-proxy-sidecar.md` for custom target and authentication semantics. Native-protocol pass-through does not imply protocol conversion, model-policy enforcement, or token accounting; verify and document those controls separately. See [LLM API Endpoint Discovery](llms.md) for the response shape and model-discovery behavior.

## Generate MCP configuration only when needed

When the gateway output already matches the CLI's native schema, point `behaviors.mcp.config-path` at the native config path and bind it through `execution.mcp-config-env-var` or `execution.mcp-config-flag`.

When the schemas differ, add a `config-adapter` that reads the gateway environment, filters CLI-mounted servers, preserves supported authorization headers, rewrites the gateway domain for the engine's execution context, and writes the native format with owner-only permissions.

When the CLI requires MCP servers as flags or cannot represent all gateway fields in its static config, add that translation to the execution harness. Read the generated path from the configured MCP environment variable, pass arguments as an array to the spawned process, and use a temporary runtime overlay for fields such as HTTP headers that cannot be represented safely as flags. Do not interpolate generated MCP values into a shell command. The shared Goose definition demonstrates both a config adapter and runtime harness.

## Engine definition shape

```aw wrap
engine:
  id: auggie
  display-name: Auggie
  experimental: true
  auth:
    - role: session
      secret: AUGMENT_SESSION_AUTH
  behaviors:
    supported-env-var-keys:
      - AUGMENT_SESSION_AUTH
    installation:
      package-manager: npm
      package-name: "@augmentcode/auggie"
      version: "1.0.0"
      step-name: Install Auggie
      binary-name: auggie
      include-node-setup: true
    config-file:
      path: .auggie.json
      step-name: Write Auggie Config
      content: '{"sandbox":"workspace-write"}'
      merge-strategy: json-merge
    execution:
      command-name: auggie
      args: [run]
      step-name: Execute Auggie CLI
      model-env-var: AUGGIE_MODEL
      mcp-config-env-var: AUGGIE_MCP_CONFIG
      write-timestamp: true
```

## Field guide

- `engine.id` is the public identifier used by workflow authors in `engine: <id>`.
- `version` sets the default CLI version applied when a workflow references this engine without specifying `engine.version`; distinct from `behaviors.installation.version`, which pins the version actually installed on the runner.
- `display-name` and `description` should be human-readable because they surface in validation and docs.
- `runtime-id` is only needed when the definition reuses a different registered runtime adapter.
- `experimental: true` should be set for engines that are not yet considered stable.
- `provider` and `models` describe provider defaults and supported model metadata.
- `auth` declares engine-specific secret bindings forwarded into the runtime environment.
- `env` supplies definition defaults applied before AWF configuration is built; use it for custom gateway upstream URLs and runner-side credential bindings. Workflow `engine.env` overrides take precedence.
- `behaviors.capabilities` advertises runtime support such as `max-turns`, `tools-allowlist`, or `native-agent-file`.
- `behaviors.manifest` lists engine-owned files and path prefixes that affect runtime behavior.
- `behaviors.installation` defines CLI installation and optional verification steps.
- `behaviors.config-file` writes engine config before execution; use `json-merge` when the file must merge with rendered MCP content.
- `behaviors.execution` defines the command, fixed args, model binding, MCP binding, and timestamp behavior.
- `behaviors.mcp.config-path` points to the file where rendered MCP configuration should be written.
- `behaviors.log-parser` supplies a JavaScript `parseLog(logContent)` function (not exported directly — a shared wrapper handles exports and bootstrap) run in the post-agent log-parsing step. It must return `{markdown, logEntries, mcpFailures, maxTurnsHit}` so behavior-defined engines produce normalized events files like built-in engine parsers.
- `behaviors.plugins` (experimental) opts the engine into top-level `plugins:` (Agent Plugins) support; omit it to make `plugins:` a compile-time error for this engine. Set `directory` (folder the engine CLI scans for staged plugins, workspace- or home-relative) and/or `command-name`/`install-args` (CLI invoked as `<command-name> <install-args...> <local-plugin-path>`) — see the shared Cursor and Kiro engine definitions for examples.

## Auth and provider rules

- prefer `secret-strategy: universal-llm-consumer` when the engine can reuse shared provider/model routing
- pair that with `execution.provider-env-mode: universal-llm-consumer` when the CLI expects provider env vars
- use `engine.auth` only for engine-specific secrets that must be injected directly into the CLI runtime
- keep `supported-env-var-keys` aligned with the env var names the CLI actually accepts
- do not hard-code credential values in the shared definition or generated configuration

## Validation loop

1. add or update `.github/workflows/shared/<id>.md`
2. import it from a minimal workflow that exercises the selected provider, model syntax, MCP mode, and prompt delivery
3. compile that workflow in strict mode with the installed extension
4. inspect the generated `.lock.yml` and verify the installation, config, MCP adapter, harness, model, and prompt wiring
5. repeat until compilation succeeds without engine-related warnings:

```bash
gh aw compile <workflow-name> --strict
```

## Anti-patterns

- do not require a gh-aw source checkout, Go changes, or repository-internal build commands
- do not scatter install metadata, CLI args, or config-file templates across consuming workflows
- do not attempt to create a built-in engine when a shared agentic workflow definition can express the contract
- do not hard-code LLM proxy ports, container IPs, or a single provider endpoint when `/reflect` can resolve the selected provider
- do not claim MCP support until the generated gateway configuration matches the CLI's accepted schema and transport
- do not omit manifest files for engine-owned config that changes runtime behavior
- do not use a mismatched `runtime-id` unless an existing runtime adapter is intentionally being reused
