---
excluded-env:
  - FACTORY_API_KEY
  - SECRET_FACTORY_API_KEY
  - COPILOT_GITHUB_TOKEN
  - OPENAI_API_KEY
  - CODEX_API_KEY
  - ANTHROPIC_API_KEY
  - GITHUB_TOKEN
  - GH_TOKEN
  - GITHUB_ENTERPRISE_TOKEN
  - GH_ENTERPRISE_TOKEN
  - GH_AW_GITHUB_TOKEN
  - GH_AW_AGENT_TOKEN
engine:
  id: factory
  detection-engine: copilot
  display-name: Factory
  description: Factory Droid CLI with sandbox-proxied inference and native MCP support
  experimental: true
  version: "0.248.0"
  provider:
    name: github
  behaviors:
    secret-strategy: universal-llm-consumer
    manifest:
      files:
        - AGENTS.md
      path-prefixes:
        - .factory/
    network:
      provider-domains:
        copilot: api.githubcopilot.com
        anthropic: api.anthropic.com
        openai: api.openai.com
        codex: api.openai.com
    installation:
      package-manager: npm
      package-name: "@factory/cli"
      binary-name: droid
      step-name: Install Factory Droid CLI
      include-node-setup: true
      cooldown: true
      verify-command: droid --version
      verify-step-name: Verify Factory Droid CLI installation
      docs-url: https://docs.factory.com/droid-exec/overview
    execution:
      command-name: droid
      args:
        - exec
        - --auto
        - high
        - --output-format
        - text
      step-name: Execute Factory Droid CLI
      model-env-var: FACTORY_MODEL
      provider-env-mode: universal-llm-consumer
      mcp-config-env-var: GH_AW_MCP_CONFIG
      write-timestamp: true
      env:
        NO_COLOR: "1"
    mcp:
      config-path: .factory/mcp.json
      config-adapter: |
        const { join } = require("path");
        const { loadGatewayContext, filterAndTransformServers, rewriteUrl, writeSecureOutput } = require("./convert_gateway_config_shared.cjs");
        const context = loadGatewayContext({ extraRequiredEnv: ["GITHUB_WORKSPACE"] });
        const servers = filterAndTransformServers(context.servers, context.cliServers, (name, entry) => {
          if (typeof entry.url !== "string") {
            throw new Error(`Factory requires a gateway HTTP URL for MCP server ${name}`);
          }
          return {
            type: "http",
            url: rewriteUrl(entry.url, context.urlPrefix),
            oauth: false,
            ...(entry.headers ? { headers: entry.headers } : {}),
          };
        });
        writeSecureOutput(join(context.extraEnv.GITHUB_WORKSPACE, ".factory", "mcp.json"), JSON.stringify({ mcpServers: servers }, null, 2));
    harness-script: |
      const { mkdirSync, mkdtempSync, readFileSync, writeFileSync, rmSync } = require("fs");
      const { join } = require("path");
      const { spawnSync } = require("child_process");
      const { fetchAWFReflect, resolveProviderEndpointFromReflect, deriveBaseUrlFromModelsURL, inferWireApiForModel, normalizeReflectProviderName, REFLECT_PROVIDER_ALIASES } = require("./awf_reflect.cjs");

      const [command, ...args] = process.argv.slice(2);
      const log = message => process.stderr.write(`[factory-harness] ${message}\n`);
      const main = async () => {
        if (!command) throw new Error("Factory Droid command is required");
        if (process.env.AWF_REFLECT_ENABLED !== "1") {
          throw new Error("Factory requires the AWF API proxy; direct credential authentication is not supported");
        }
        const selectedModel = process.env.FACTORY_MODEL;
        const separator = selectedModel?.indexOf("/") ?? -1;
        if (separator <= 0 || !selectedModel.slice(separator + 1).trim()) {
          throw new Error("FACTORY_MODEL must use provider/model format");
        }
        const model = selectedModel.slice(separator + 1).trim();
        const provider = process.env.GH_AW_LLM_PROVIDER;
        if (!["github", "anthropic", "openai"].includes(provider)) {
          throw new Error("GH_AW_LLM_PROVIDER must be github, anthropic, or openai");
        }
        const promptPath = process.env.GH_AW_PROMPT;
        if (!promptPath) throw new Error("GH_AW_PROMPT is required");
        if (!readFileSync(promptPath, "utf8").trim()) throw new Error("GH_AW_PROMPT must contain a non-empty instruction");
        const reflected = await fetchAWFReflect({ logger: log });
        if (!reflected.ok || !reflected.reflectData) throw new Error("Unable to discover the Factory LLM endpoint from /reflect");
        const endpoint = resolveProviderEndpointFromReflect({ provider, reflectData: reflected.reflectData, logger: log });
        if (!endpoint || !REFLECT_PROVIDER_ALIASES[provider].has(normalizeReflectProviderName(endpoint.endpointProvider))) {
          throw new Error(`No configured /reflect endpoint found for provider ${provider}`);
        }
        const reflectedEndpoint = reflected.reflectData.endpoints?.find(
          entry => entry?.configured === true &&
            normalizeReflectProviderName(entry.provider) === normalizeReflectProviderName(endpoint.endpointProvider)
        );
        if (typeof reflectedEndpoint?.models_url !== "string") {
          throw new Error(`No configured /reflect models endpoint found for provider ${provider}`);
        }
        const baseUrl = deriveBaseUrlFromModelsURL(reflectedEndpoint.models_url);
        const isAnthropic = provider === "anthropic";
        const providerType = isAnthropic
          ? "anthropic"
          : provider === "openai" || (provider === "github" && inferWireApiForModel("openai", model) === "responses")
            ? "openai"
            : "generic-chat-completion-api";
        const settings = {
          cloudSessionSync: false,
          blockOnMcpLoad: true,
          customModels: [{
            displayName: "gh-aw",
            model,
            provider: providerType,
            baseUrl: isAnthropic ? baseUrl.replace(/\/+$/, "").replace(/\/v1$/, "") : baseUrl,
            apiKey: "awf-proxy",
          }],
        };
        mkdirSync("/tmp/gh-aw/agent", { recursive: true });
        const settingsDir = mkdtempSync("/tmp/gh-aw/agent/factory-");
        try {
          const settingsPath = join(settingsDir, "settings.json");
          writeFileSync(settingsPath, JSON.stringify(settings), { mode: 0o600 });
          const childEnv = { ...process.env };
          for (const name of [
            "FACTORY_API_KEY", "SECRET_FACTORY_API_KEY", "COPILOT_GITHUB_TOKEN",
            "OPENAI_API_KEY", "CODEX_API_KEY", "ANTHROPIC_API_KEY",
            "GITHUB_TOKEN", "GH_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_ENTERPRISE_TOKEN",
            "GH_AW_GITHUB_TOKEN", "GH_AW_AGENT_TOKEN",
          ]) {
            delete childEnv[name];
          }
          const result = spawnSync(command, [...args, "--settings", settingsPath, "--model", "custom:gh-aw-0", "--file", promptPath], {
            cwd: process.env.GH_AW_ENGINE_CWD || process.env.GITHUB_WORKSPACE,
            env: childEnv,
            stdio: "inherit",
          });
          if (result.error) throw result.error;
          if (result.status !== 0) {
            log(result.signal ? `Droid terminated by signal ${result.signal}` : `Droid exited with code ${result.status ?? "unknown"}`);
            process.exitCode = result.status || 1;
          }
        } finally {
          rmSync(settingsDir, { recursive: true, force: true });
        }
      };
      main().catch(error => {
        log(error instanceof Error ? error.message : String(error));
        process.exitCode = 1;
      });
---

<!--
# Factory Droid

Unsupported, experimental sample for the [Factory CLI](https://factory.com/product/cli).
Import this definition and select an exact provider/model ID:

```yaml
engine:
  id: factory
model: copilot/gpt-5.2
imports:
  - shared/factory.md
```

Inference uses Droid's custom-model (BYOK) support through the AWF API proxy.
Supported prefixes are `copilot/`, `anthropic/`, `openai/`, and `codex/`.
Configure the corresponding gh-aw provider credential (`COPILOT_GITHUB_TOKEN`,
`ANTHROPIC_API_KEY`, or `OPENAI_API_KEY`/`CODEX_API_KEY`), or use
`permissions.copilot-requests: write` for Copilot token-based inference.
Real credentials remain outside the agent; Droid receives only the
`awf-proxy` placeholder and a proxy endpoint discovered through `/reflect`.

No Factory account or `FACTORY_API_KEY` is required. Factory-managed inference
and unsandboxed/direct-key execution are deliberately unsupported because they
would expose a reusable credential to agent tools. Compiler environment exclusions
keep known ambient Factory, provider, and GitHub credentials outside the agent
sandbox. As defense in depth, the harness also removes these names from Droid's
child environment; this cannot protect credentials already exposed to the parent
process or credentials stored under other names.
Cloud session sync is disabled. The per-run settings file contains only a
placeholder credential, is owner-readable, and is removed after execution.

Installation pins `@factory/cli` 0.248.0 and verifies `droid --version`.
Its npm platform packages support Linux x64 and arm64 with glibc. Lifecycle
scripts stay disabled: the package's JavaScript shim resolves its pinned
optional platform dependency without the postinstall optimization.

The harness runs `droid exec --auto high --output-format text --settings <settings>
--model custom:gh-aw-0 --file <prompt>`. High autonomy permits local builds and
MCP calls without interactive approval; use this sample only inside the gh-aw agent sandbox.
Droid's own command blocks and organization policies still apply. The harness
preserves nonzero CLI exit codes and never prints credentials or prompt contents.

Native MCP uses an owner-only `.factory/mcp.json` generated from the gh-aw
gateway. HTTP authorization headers are retained, OAuth is disabled, and servers
mounted as CLIs are excluded. The generated file is ephemeral and must not be
committed. Droid discovers it from the workflow workspace.

This sample does not implement a structured session parser: text output is
retained in agent logs, but native tool events, token usage, refusals, and
reasoning are not normalized. It does not advertise gh-aw tool allowlists,
max-turn enforcement, or Agent Plugins. A successful process exit is not
independent proof of task completion.
-->
