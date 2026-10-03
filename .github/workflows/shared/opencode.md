---
engine:
  id: opencode
  detection-engine: copilot
  version: "1.18.33"
  display-name: OpenCode
  description: OpenCode CLI with headless mode and multi-provider LLM support
  runtime-id: opencode
  experimental: true
  provider:
    name: github
  behaviors:
    secret-strategy: universal-llm-consumer
    capabilities:
      tools-allowlist: true
      max-turns: true
    manifest:
      files:
        - opencode.json
        - opencode.jsonc
        - AGENTS.md
      path-prefixes:
        - .opencode/
    network:
      defaults:
        - host.docker.internal
        - github.com
        - raw.githubusercontent.com
      provider-domains:
        copilot: api.githubcopilot.com
        anthropic: api.anthropic.com
        openai: api.openai.com
        google: generativelanguage.googleapis.com
        codex: api.openai.com
    installation:
      package-manager: npm
      package-name: opencode-ai
      step-name: Install OpenCode CLI
      binary-name: opencode
      include-node-setup: true
      # The pinned package's postinstall links its platform binary into bin/opencode.exe.
      post-install-scripts: true
      cooldown: true
      verify-command: opencode --version
      verify-step-name: Verify OpenCode CLI installation
      docs-url: https://opencode.ai/docs
    execution:
      command-name: opencode
      args:
        - run
        - --format
        - json
        - --agent
        - build
        - --thinking
        - --print-logs
        - --log-level
        - INFO
      step-name: Execute OpenCode CLI
      model-env-var: OPENCODE_MODEL
      mcp-config-env-var: GH_AW_MCP_CONFIG
      write-timestamp: true
      provider-env-mode: universal-llm-consumer
      env:
        XDG_DATA_HOME: /tmp/opencode-data
        XDG_CONFIG_HOME: /tmp/opencode-config
        XDG_CACHE_HOME: /tmp/opencode-cache
        XDG_STATE_HOME: /tmp/opencode-state
        OPENCODE_AUTH_CONTENT: "{}"
        OPENCODE_DISABLE_AUTOUPDATE: "1"
        OPENCODE_DISABLE_DEFAULT_PLUGINS: "1"
        OPENCODE_DISABLE_LSP_DOWNLOAD: "1"
        OPENCODE_DISABLE_MODELS_FETCH: "1"
        NO_COLOR: "1"
    mcp:
      config-path: /tmp/gh-aw/opencode-mcp.json
      config-adapter: |
        const { loadGatewayContext, filterAndTransformServers, rewriteUrl, writeSecureOutput } = require("./convert_gateway_config_shared.cjs");
        const context = loadGatewayContext();
        const mcp = filterAndTransformServers(context.servers, context.cliServers, (name, entry) => {
          if (typeof entry.url !== "string") {
            throw new Error(`OpenCode requires a gateway HTTP URL for MCP server ${name}`);
          }
          return {
            type: "remote",
            url: rewriteUrl(entry.url, context.urlPrefix),
            enabled: true,
            oauth: false,
            ...(entry.headers ? { headers: entry.headers } : {}),
          };
        });
        writeSecureOutput("/tmp/gh-aw/opencode-mcp.json", JSON.stringify({ mcp }, null, 2));
    harness-script: |
      const { readFileSync } = require("fs");
      const { runProcess } = require("./process_runner.cjs");
      const { isOpenCodeEvent, getOpenCodeMCPFailures } = require("./parse_opencode_log.cjs");
      const { fetchAWFReflect, deriveBaseUrlFromModelsURL, normalizeReflectProviderName, REFLECT_PROVIDER_ALIASES } = require("./awf_reflect.cjs");

      const [command, ...commandArgs] = process.argv.slice(2);
      const log = message => process.stderr.write(`[opencode-harness] ${message}\n`);
      const main = async () => {
        if (!command) throw new Error("OpenCode command is required");
        const selectedModel = process.env.OPENCODE_MODEL;
        const separator = selectedModel?.indexOf("/") ?? -1;
        if (separator <= 0 || separator === selectedModel.length - 1) {
          throw new Error("OPENCODE_MODEL must use provider/model format");
        }
        const model = selectedModel.slice(separator + 1);
        const provider = process.env.GH_AW_LLM_PROVIDER;
        if (!["github", "anthropic", "openai"].includes(provider)) {
          throw new Error("GH_AW_LLM_PROVIDER must be github, anthropic, or openai");
        }
        const rawMaxTurns = process.env.GH_AW_MAX_TURNS;
        const maxTurns = rawMaxTurns === undefined || rawMaxTurns === "" ? undefined : Number(rawMaxTurns);
        if (maxTurns !== undefined && (!Number.isSafeInteger(maxTurns) || maxTurns <= 0)) {
          throw new Error("GH_AW_MAX_TURNS must be a positive integer");
        }
        const isAnthropic = provider === "anthropic";
        const timeoutMs = name => {
          const value = process.env[name];
          if (value === undefined || value === "") return undefined;
          const milliseconds = Number(value) * 1000;
          if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0) {
            throw new Error(`${name} must specify a positive timeout in seconds`);
          }
          return milliseconds;
        };
        const startupTimeout = timeoutMs("GH_AW_STARTUP_TIMEOUT");
        const toolTimeout = timeoutMs("GH_AW_TOOL_TIMEOUT");
        let baseURL;
        let apiKey;
        if (process.env.AWF_REFLECT_ENABLED === "1") {
          const result = await fetchAWFReflect({ logger: log });
          if (!result.ok || !result.reflectData) throw new Error("Unable to discover the OpenCode LLM endpoint from /reflect");
          const aliases = REFLECT_PROVIDER_ALIASES[provider];
          const endpoint = result.reflectData.endpoints?.find(
            entry => entry?.configured === true && aliases.has(normalizeReflectProviderName(entry.provider))
          );
          if (!endpoint || typeof endpoint.models_url !== "string") {
            throw new Error(`No configured /reflect models endpoint found for provider ${provider}`);
          }
          baseURL = deriveBaseUrlFromModelsURL(endpoint.models_url);
          // The firewall owns upstream credentials; the agent only needs a placeholder.
          apiKey = "awf-proxy";
        } else {
          if (provider === "github") throw new Error("OpenCode Copilot routing requires the AWF sandbox");
          baseURL = isAnthropic ? process.env.ANTHROPIC_BASE_URL || "https://api.anthropic.com/v1" : process.env.OPENAI_BASE_URL || "https://api.openai.com/v1";
          apiKey = isAnthropic ? process.env.ANTHROPIC_API_KEY : process.env.OPENAI_API_KEY || process.env.CODEX_API_KEY;
          if (!apiKey) throw new Error("OpenCode provider API key is required without AWF");
        }
        const config = {
          model: `awf-proxy/${model}`,
          autoupdate: false,
          share: "disabled",
          enabled_providers: ["awf-proxy"],
          disabled_providers: [],
          permission: "allow",
          agent: { build: { permission: "allow", ...(maxTurns !== undefined ? { steps: maxTurns } : {}) } },
          ...(toolTimeout !== undefined ? { experimental: { mcp_timeout: toolTimeout } } : {}),
          provider: {
            "awf-proxy": {
              name: "GitHub Agentic Workflows",
              npm: isAnthropic ? "@ai-sdk/anthropic" : "@ai-sdk/openai-compatible",
              options: { baseURL, apiKey },
              models: { [model]: { name: model } },
            },
          },
        };
        const mcpPath = process.env.GH_AW_MCP_CONFIG;
        if (mcpPath) {
          const content = readFileSync(mcpPath, "utf8");
          let mcpConfig;
          try {
            mcpConfig = JSON.parse(content);
          } catch (error) {
            throw new Error("Failed to parse OpenCode MCP configuration", { cause: error });
          }
          if (!mcpConfig.mcp || typeof mcpConfig.mcp !== "object" || Array.isArray(mcpConfig.mcp)) {
            throw new Error("OpenCode MCP configuration must contain an mcp object");
          }
          config.mcp = mcpConfig.mcp;
          if (startupTimeout !== undefined) {
            config.mcp = Object.fromEntries(Object.entries(config.mcp).map(([name, server]) => [name, { ...server, timeout: startupTimeout }]));
          }
        }
        const promptPath = process.env.GH_AW_PROMPT;
        if (!promptPath) throw new Error("GH_AW_PROMPT is required");
        const prompt = readFileSync(promptPath, "utf8");
        let turns = 0;
        let reportedError = false;
        let maxTurnsHit = false;
        const finishedParts = new Set();
        const observedMCPFailures = new Set();
        const result = await runProcess({
          command,
          args: [...commandArgs, "--model", `awf-proxy/${model}`],
          logArgs: [...commandArgs, "--model", "(configured model)"],
          attempt: 0,
          log,
          stdin: prompt,
          env: { ...process.env, OPENCODE_CONFIG_CONTENT: JSON.stringify(config) },
          maxCollectedOutputBytes: 1024 * 1024,
          onStderrLine: line => {
            for (const server of getOpenCodeMCPFailures(line)) observedMCPFailures.add(server);
          },
          onStdoutLine: line => {
            let entry;
            try { entry = JSON.parse(line); } catch { return; }
            if (!isOpenCodeEvent(entry)) return;
            if (entry?.type === "error" && entry.error) reportedError = true;
            if (entry?.type !== "step_finish") return;
            const identity = typeof entry.part?.id === "string" ? JSON.stringify([entry.sessionID, entry.part.id]) : undefined;
            if (identity !== undefined && finishedParts.has(identity)) return;
            if (identity !== undefined) finishedParts.add(identity);
            turns++;
            if (maxTurns !== undefined && turns >= maxTurns && entry.part?.reason === "tool-calls") {
              maxTurnsHit = true;
            }
          },
          runtimeGuard: { shouldTerminate: () => ({ terminate: maxTurnsHit, reason: "OpenCode max-turns limit reached" }), pollIntervalMs: 100 },
        });
        if (maxTurnsHit) {
          process.stdout.write(JSON.stringify({ type: "opencode.max_turns", timestamp: Date.now(), data: { maxTurns } }) + "\n");
          throw new Error("OpenCode max-turns limit reached");
        }
        if (result.exitCode !== 0) {
          process.exitCode = result.exitCode;
          throw new Error(`OpenCode execution failed with exit code ${result.exitCode}`);
        }
        const mcpFailures = [...new Set([...observedMCPFailures, ...getOpenCodeMCPFailures(result.stderr)])];
        if (mcpFailures.length) {
          for (const serverName of mcpFailures) {
            process.stdout.write(JSON.stringify({ type: "opencode.mcp_failure", timestamp: Date.now(), data: { serverName } }) + "\n");
          }
          throw new Error(`OpenCode MCP server(s) failed to start: ${mcpFailures.join(", ")}`);
        }
        if (reportedError) throw new Error("OpenCode reported an error despite exiting with code 0");
      };
      main().catch(error => {
        log(error instanceof Error ? error.message : String(error));
        if (!process.exitCode) process.exitCode = 1;
      });
    log-parser: |
      function parseLog(logContent) {
        return require("./parse_opencode_log.cjs").parseOpenCodeLog(logContent);
      }
---

<!--
# OpenCode CLI

Shared engine definition for the [OpenCode](https://opencode.ai) multi-provider AI
coding agent (BYOK). Import this file and set `engine: opencode` to use it:

```yaml
engine:
  id: opencode
model: copilot/auto
imports:
  - shared/opencode.md
```

This integration is an unsupported repository sample. `model` must use
`provider/model` format with `copilot`, `anthropic`, `openai`, or `codex`.
Copilot requires AWF; direct Anthropic and OpenAI BYOK execution is available
when the sandbox is explicitly disabled.

The harness discovers the selected AWF provider endpoint rather than assuming
a fixed proxy address. It passes the model through `--model`, supplies the
prompt on stdin, and requests JSONL events. Provider credentials remain in AWF;
only a placeholder key reaches the agent. `engine.max-turns` limits completed
model steps, and a tool-calling turn at the limit fails explicitly.
MCP startup failures also fail the step, even when OpenCode exits successfully.
Configured MCP startup/tool timeouts are converted from gh-aw seconds to
OpenCode milliseconds.

Repository `opencode.json`, `opencode.jsonc`, `.opencode/` agents, and `AGENTS.md`
retain native discovery and JSONC parsing. Runtime configuration is supplied
through `OPENCODE_CONFIG_CONTENT`; the harness does not rewrite repository
settings. OpenCode itself may add `$schema` to a configuration file that lacks
it, while preserving its JSONC settings and comments. Gateway
MCP servers use OpenCode's remote transport, preserve authentication headers,
and disable interactive OAuth. Automatic updates, sharing, default plugins,
and LSP downloads are disabled. Global configuration, model cache, state, and
session/auth data use isolated `/tmp/opencode-*` directories and are not cached
across runs; pre-existing authentication stores are not loaded.

The pinned CLI is `1.18.33` (published September 28, 2026), outside the three-day
npm cooldown. `1.18.34` is still inside that window as of October 3, 2026.
Installation runs the pinned package's lifecycle script to link its native
binary. The provider SDKs are bundled; model catalog fetching is disabled.
Custom models without explicit limits retain OpenCode's unknown-limit defaults;
set `provider.awf-proxy.models.<model>.limit` in native repository configuration
when authoritative context/output limits are available.

Actions summaries, unified session artifacts, and local session reconstruction
share the JSONL parser. Text, reasoning, correlated tool calls/results, structured
errors, timestamps, per-step cost, and input/output/cache token usage are retained.
Native JavaScript/npm plugins are not gh-aw Agent Plugins and are not advertised
as supported by this sample.
-->
