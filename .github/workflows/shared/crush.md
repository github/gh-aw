---
engine:
  id: crush
  detection-engine: copilot
  version: "0.98.1"
  display-name: Crush
  description: Crush CLI with non-interactive execution and native MCP support
  experimental: true
  provider:
    name: github
  behaviors:
    secret-strategy: universal-llm-consumer
    manifest:
      files:
        - .crush.json
        - crush.json
        - .crushrc
        - crushrc
        - CRUSH.md
        - CRUSH.local.md
        - AGENTS.md
      path-prefixes:
        - .crush/
    network:
      defaults:
        - host.docker.internal
        - github.com
        - raw.githubusercontent.com
        - api.github.com
        - objects.githubusercontent.com
        - release-assets.githubusercontent.com
      provider-domains:
        copilot: api.githubcopilot.com
        anthropic: api.anthropic.com
        openai: api.openai.com
    installation:
      package-manager: npm
      package-name: "@charmland/crush"
      step-name: Install Crush CLI
      binary-name: crush
      include-node-setup: true
      post-install-scripts: true
      cooldown: true
      verify-command: crush --version
      verify-step-name: Verify Crush CLI installation
      docs-url: https://github.com/charmbracelet/crush
    config-file:
      path: .crush.json
      step-name: Write Crush Config
      content: |-
        {
          "$schema": "https://charm.land/crush.json",
          "options": {
            "auto_lsp": false,
            "disable_default_providers": true,
            "disable_metrics": true,
            "disable_provider_auto_update": true,
            "progress": false
          },
          "permissions": {
            "allowed_tools": [
              "view",
              "ls",
              "grep",
              "edit",
              "write",
              "bash",
              "webfetch",
              "websearch"
            ]
          }
        }
      merge-strategy: json-merge
    execution:
      command-name: crush
      args:
        - run
        - --quiet
        - --verbose
      step-name: Execute Crush CLI
      model-env-var: CRUSH_MODEL
      mcp-config-env-var: GH_AW_MCP_CONFIG
      provider-env-mode: universal-llm-consumer
      write-timestamp: true
      env:
        CRUSH_DISABLE_DEFAULT_PROVIDERS: "1"
        CRUSH_DISABLE_PROVIDER_AUTO_UPDATE: "1"
    mcp:
      config-path: .crush.json
      config-adapter: |
        const fs = require("fs");
        const path = require("path");

        const requireEnvVar = name => {
          const value = process.env[name];
          if (!value) throw new Error(`${name} environment variable is required`);
          return value;
        };

        const gatewayOutputPath = requireEnvVar("MCP_GATEWAY_OUTPUT");
        const workspace = requireEnvVar("GITHUB_WORKSPACE");
        const gatewayDomain = process.env.MCP_GATEWAY_DOMAIN || "host.docker.internal";
        const gatewayPort = requireEnvVar("MCP_GATEWAY_PORT");
        const gatewayURL = `http://${gatewayDomain}:${gatewayPort}`;

        let cliServers;
        try {
          cliServers = new Set(JSON.parse(process.env.GH_AW_MCP_CLI_SERVERS || "[]"));
        } catch (error) {
          throw new Error(`Failed to parse GH_AW_MCP_CLI_SERVERS: ${error instanceof Error ? error.message : String(error)}`);
        }

        const gatewayOutput = JSON.parse(fs.readFileSync(gatewayOutputPath, "utf8"));
        const rawServers = gatewayOutput.mcpServers;
        const servers = rawServers && typeof rawServers === "object" && !Array.isArray(rawServers) ? rawServers : {};
        const mcp = {};

        for (const [name, entry] of Object.entries(servers)) {
          if (cliServers.has(name) || !entry || typeof entry !== "object") continue;
          const transformed = { ...entry };
          if (typeof transformed.url === "string") {
            transformed.url = transformed.url.replace(/^http:\/\/[^/]+\/mcp\//, `${gatewayURL}/mcp/`);
            transformed.type = "http";
          } else {
            transformed.type = "stdio";
          }
          delete transformed.tools;
          mcp[name] = transformed;
        }

        const configPath = path.join(workspace, ".crush.json");
        fs.writeFileSync(configPath, JSON.stringify({ mcp }, null, 2), { mode: 0o600 });
        fs.chmodSync(configPath, 0o600);
    harness-script: |
      const { spawnSync } = require("child_process");
      const { accessSync, closeSync, constants, fstatSync, openSync, readFileSync, writeFileSync } = require("fs");
      const { isAbsolute, join } = require("path");
      const { fetchAWFReflect, resolveProviderEndpointFromReflect, deriveBaseUrlFromModelsURL, inferWireApiForModel, REFLECT_PROVIDER_ALIASES } = require("./awf_reflect.cjs");

      const [command, ...commandArgs] = process.argv.slice(2);
      const log = message => process.stderr.write(`[crush-harness] ${message}\n`);
      const fail = (result, action) => {
        if (result.error) throw result.error;
        if (result.signal) throw new Error(`${action} terminated by signal ${result.signal}`);
        if (result.status !== 0) throw new Error(`${action} failed with exit code ${result.status ?? "unknown"}`);
      };

      // Preflight the CLI lookup inside the sandbox: the binary is installed on the runner
      // host, so a missing sandbox mount surfaces as a bare ENOENT from spawnSync.
      const resolveCommand = name => {
        const searchPath = process.env.PATH || "";
        if (name.includes("/")) {
          const candidate = isAbsolute(name) ? name : join(process.cwd(), name);
          try {
            accessSync(candidate, constants.X_OK);
            return candidate;
          } catch {
            throw new Error(`${name} not executable in sandbox: ${candidate}`);
          }
        }
        for (const dir of searchPath.split(":")) {
          if (!dir) continue;
          const candidate = join(dir, name);
          try {
            accessSync(candidate, constants.X_OK);
            return candidate;
          } catch {
            continue;
          }
        }
        throw new Error(`${name} not found in sandbox PATH: ${searchPath}`);
      };

      const main = async () => {
        log("preparing non-interactive execution");
        const selectedModel = process.env.CRUSH_MODEL;
        if (!selectedModel || !selectedModel.includes("/")) {
          throw new Error("CRUSH_MODEL must use provider/model format");
        }
        const model = selectedModel.slice(selectedModel.indexOf("/") + 1);
        const provider = process.env.GH_AW_LLM_PROVIDER;
        if (!provider) throw new Error("GH_AW_LLM_PROVIDER is required");

        let baseUrl = process.env.OPENAI_BASE_URL;
        if (process.env.AWF_REFLECT_ENABLED === "1") {
          const result = await fetchAWFReflect({ logger: log });
          if (!result.ok || !result.reflectData) {
            throw new Error(`Unable to discover the Crush LLM endpoint from /reflect: ${result.reason || "empty response"}`);
          }
          const endpoint = resolveProviderEndpointFromReflect({
            provider,
            reflectData: result.reflectData,
            logger: log,
          });
          if (!endpoint || !endpoint.baseUrl || !REFLECT_PROVIDER_ALIASES[provider]?.has(endpoint.endpointProvider)) {
            throw new Error(`No configured /reflect endpoint found for provider ${provider}`);
          }
          baseUrl = endpoint.baseUrl;
          const reflectedEndpoint = result.reflectData.endpoints?.find(
            entry => entry?.configured === true && entry.provider === endpoint.endpointProvider
          );
          if (typeof reflectedEndpoint?.models_url === "string") {
            // Re-derive the base URL from models_url (rather than reusing endpoint.baseUrl,
            // which points at the models-listing endpoint) while still applying the same
            // api-proxy -> host.docker.internal HOSTALIASES bridge rewrite, so the crush
            // binary's own chat-completions request never targets the unresolvable
            // "api-proxy" hostname.
            baseUrl = deriveBaseUrlFromModelsURL(reflectedEndpoint.models_url);
          }
        }
        if (!baseUrl) {
          throw new Error("Crush requires AWF endpoint discovery or OPENAI_BASE_URL");
        }
        log("LLM endpoint discovery complete");

        const workspace = process.env.GITHUB_WORKSPACE;
        if (!workspace) throw new Error("GITHUB_WORKSPACE is required");
        const configPath = process.env.GH_AW_MCP_CONFIG || join(workspace, ".crush.json");
        const config = JSON.parse(readFileSync(configPath, "utf8"));
        const providerType = provider === "anthropic"
          ? "anthropic"
          : inferWireApiForModel("openai", model) === "responses" ? "openai" : "openai-compat";
        const apiKey = providerType === "anthropic" ? "$ANTHROPIC_API_KEY" : "$OPENAI_API_KEY";
        config.providers = {
          ...(config.providers || {}),
          "awf-proxy": {
            id: "awf-proxy",
            name: "GitHub Agentic Workflows",
            base_url: baseUrl,
            type: providerType,
            api_key: apiKey,
            discover_models: false,
            models: [{
              id: model,
              name: model,
              cost_per_1m_in: 0,
              cost_per_1m_out: 0,
              cost_per_1m_in_cached: 0,
              cost_per_1m_out_cached: 0,
              context_window: 200000,
              default_max_tokens: 64000,
              can_reason: false,
              supports_attachments: false,
            }],
          },
        };
        config.models = {
          ...(config.models || {}),
          large: { provider: "awf-proxy", model },
        };
        writeFileSync(configPath, JSON.stringify(config, null, 2), { mode: 0o600 });
        log(`configuration ready (provider type: ${providerType}; MCP servers: ${Object.keys(config.mcp || {}).length})`);

        const promptPath = process.env.GH_AW_PROMPT;
        if (!promptPath) throw new Error("GH_AW_PROMPT is required");
        const executable = resolveCommand(command);
        log(`resolved ${command} to ${executable}`);
        const startedAt = Date.now();
        const promptFd = openSync(promptPath, "r");
        let result;
        try {
          log(`prompt loaded (${fstatSync(promptFd).size} bytes)`);
          log("starting Crush CLI");
          result = spawnSync(executable, [...commandArgs, "--model", `awf-proxy/${model}`], {
            cwd: workspace,
            env: process.env,
            stdio: [promptFd, "inherit", "inherit"],
          });
        } finally {
          closeSync(promptFd);
        }
        log(`Crush CLI finished after ${Date.now() - startedAt}ms (exit: ${result.status ?? "none"}; signal: ${result.signal ?? "none"})`);
        fail(result, "Crush execution");
      };

      main().catch(error => {
        log(error instanceof Error ? error.message : String(error));
        process.exitCode = 1;
      });
    log-parser: |
      function parseLog(logContent) {
        const { isSessionEvent } = require("./agent_session.cjs");
        const { normalizeEngineLogEntries } = require("./engine_log_parser.cjs");
        const AWF_INFRA_RE = /^\[(INFO|WARN|SUCCESS|ERROR|entrypoint|health-check|crush-harness)\]|^ (?:Container|Network|Volume) |^Process exiting with code:/;
        const executionStart = logContent.match(/^\[crush-harness\] starting Crush CLI\r?\n/m);
        const preamble = executionStart ? logContent.slice(executionStart.index).match(/^\[crush-harness\] starting Crush CLI\r?\n(INFO {2}Running in non-interactive mode(?:\r?\n(?:INFO|WARN|ERROR) {2}[^\r\n]*)*)/) : undefined;
        const nativeLines = preamble?.[1].split("\n") ?? [];
        const nativeLine = /^(?:INFO {2}(?:Running in non-interactive mode|Overriding large model for non-interactive run provider=\S+ model=\S+|Created session for non-interactive run session_id=[a-f0-9-]+|ModelProvider called provider=\S+ model=\S+)|WARN {2}(?:Failed to parse skill file path=.+ error="no YAML frontmatter found"|Ripgrep \(rg\) not found in \$PATH\. Some grep features might be limited or slower\.|Using large model as small model for unknown provider provider=\S+ model=\S+))\r?$/;
        const firstAnswer = nativeLines.findIndex(line => !nativeLine.test(line));
        const nativePreamble = nativeLines.slice(0, firstAnswer < 0 ? nativeLines.length : firstAnswer).join("\n");
        const model = nativePreamble.match(/^INFO {2}Overriding large model for non-interactive run provider=(\S+) model=(\S+)\r?$/m);
        const session = nativePreamble.match(/^INFO {2}Created session for non-interactive run session_id=([a-f0-9-]+)\r?$/m);
        const nativeInit = model && session ? { sourceEngine: "crush", provider: model[1], model: model[2], sessionId: session[1] } : undefined;
        const source = nativeInit ? logContent.replace(executionStart[0] + nativePreamble, "[crush-harness] starting Crush CLI") : logContent;
        const lines = source.split("\n").filter(line => !AWF_INFRA_RE.test(line));
        // Historical gh-aw logs may append an explicit result; quiet CLI text
        // itself contains no tool identities, token usage, or turn counters.
        let terminalIndex = lines.length - 1;
        while (terminalIndex >= 0 && !lines[terminalIndex].trim()) terminalIndex--;
        let terminal;
        try {
          const record = JSON.parse(lines[terminalIndex] || "");
          if (record.type === "result" && (Object.hasOwn(record, "num_turns") || Object.hasOwn(record, "usage"))) {
            terminal = record;
            lines.splice(terminalIndex);
          }
        } catch {}

        const text = lines.join("\n");
        let records;
        try {
          const document = JSON.parse(text);
          if (Array.isArray(document)) records = document;
        } catch {}
        if (!records) {
          const observedLines = lines.filter(line => line.trim());
          if (observedLines.length) records = observedLines.map(line => {
            try { return JSON.parse(line); } catch { return null; }
          });
        }
        const logEntries = [];
        if (nativeInit) {
          logEntries.push({ type: "session.init", data: nativeInit });
          logEntries.push({ type: "crush.diagnostic", data: { content: nativePreamble } });
        }
        if (records && records.every(isSessionEvent)) {
          return {
            markdown: "Crush saved session events",
            logEntries: [...logEntries, ...records, ...(terminal ? normalizeEngineLogEntries([terminal], "crush") : [])],
            mcpFailures: [], maxTurnsHit: false,
          };
        }

        if (text.trim()) logEntries.push({ type: "assistant.message", data: { content: text } });
        if (terminal) logEntries.push(...normalizeEngineLogEntries([terminal], "crush"));
        return {
          markdown: "Crush plain-text output (tool and usage telemetry unavailable)",
          // Unframed answer text cannot establish MCP failures or turn limits.
          logEntries, mcpFailures: [], maxTurnsHit: false,
        };
      }
---

<!--
# Crush CLI

Shared engine definition for the [Crush](https://github.com/charmbracelet/crush)
coding agent. Import this file and set `engine.id: crush` with a
`provider/model` model selection.

This unsupported sample pins Crush 0.98.1. Its npm package uses an install
script to provision the native binary, so lifecycle scripts remain enabled
only for the pinned installation.

The harness discovers the selected provider's endpoint through AWF reflection
and retains its API path prefix. GPT-5 and newer models use Crush's native
OpenAI Responses provider; other non-Anthropic models retain Chat Completions.
Anthropic uses its native Messages provider. Credential references remain
environment-variable expressions in the private config, not literal API keys.
Copilot inference uses the workflow's `copilot-requests: write` permission.

JSON configuration and native HTTP/stdio MCP remain supported. CLI-mounted
servers are excluded from the native MCP config. `run --quiet` emits plain
text, so unified sessions preserve the answer without inventing tool calls,
turn counts, token usage, MCP failures, or turn-limit failures from answer prose.
The harness-framed verbose startup preamble supplies observed model, local
provider, and session identifiers; its diagnostics remain separate from the
answer and are retained before saved canonical session events. This mapping is
covered by a sanitized excerpt from
[smoke run 42930955037](https://github.com/github/gh-aw/actions/runs/42930955037).
Saved canonical JSON arrays/JSONL and explicit
historical terminal records retain their observed fields.
Native verbose logging and harness startup/exit milestones are written to
stderr for timeout diagnosis. The harness logs prompt size, never prompt contents,
configuration contents, or credential values.
The prompt file is opened as stdin with an explicit EOF, not passed in process
arguments. Crush reads regular files and named pipes before initialization:
inheriting an open sandbox pipe stalls before MCP startup or inference, while
Node's socket-backed child-process pipes are not recognized as prompt input.
-->
