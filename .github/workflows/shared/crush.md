---
engine:
  id: crush
  detection-engine: copilot
  version: "0.88.0"
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
      const { accessSync, constants, readFileSync, writeFileSync } = require("fs");
      const { isAbsolute, join } = require("path");
      const { fetchAWFReflect, resolveProviderEndpointFromReflect, deriveBaseUrlFromModelsURL } = require("./awf_reflect.cjs");

      const [command, ...commandArgs] = process.argv.slice(2);
      const log = message => process.stderr.write(`[crush-harness] ${message}\n`);
      const fail = (result, action) => {
        if (result.error) throw result.error;
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
          if (!endpoint || !endpoint.baseUrl) {
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

        const workspace = process.env.GITHUB_WORKSPACE;
        if (!workspace) throw new Error("GITHUB_WORKSPACE is required");
        const configPath = process.env.GH_AW_MCP_CONFIG || join(workspace, ".crush.json");
        const config = JSON.parse(readFileSync(configPath, "utf8"));
        const providerType = provider === "anthropic" ? "anthropic" : "openai-compat";
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

        const promptPath = process.env.GH_AW_PROMPT;
        if (!promptPath) throw new Error("GH_AW_PROMPT is required");
        const prompt = readFileSync(promptPath, "utf8");
        const executable = resolveCommand(command);
        log(`resolved ${command} to ${executable}`);
        fail(
          spawnSync(executable, [...commandArgs, "--model", `awf-proxy/${model}`, prompt], {
            cwd: workspace,
            env: process.env,
            stdio: "inherit",
          }),
          "Crush execution"
        );
      };

      main().catch(error => {
        log(error instanceof Error ? error.message : String(error));
        process.exitCode = 1;
      });
    log-parser: |
      function parseLog(logContent) {
        const lines = logContent.split("\n");
        const logEntries = [];
        const mcpFailures = [];
        let maxTurnsHit = false;
        const AWF_INFRA_RE = /^\[(INFO|WARN|SUCCESS|ERROR|entrypoint|health-check|crush-harness)\]|^ (?:Container|Network|Volume) |^Process exiting with code:/;
        let toolCallIndex = 0;
        let turnCount = 0;
        let currentRole = null;
        let currentText = [];

        function flushEntry() {
          if (!currentRole || currentText.length === 0) { currentText = []; return; }
          const text = currentText.join("\n").trim();
          if (!text) { currentText = []; return; }
          if (currentRole === "tool_use") {
            const toolId = `crush_tool_${toolCallIndex++}`;
            const nameMatch = text.match(/^(?:Tool|Running|Executing|>\s*)(view|edit|bash|grep|ls|write|webfetch|websearch)\b/i);
            const toolName = nameMatch ? nameMatch[1] : "unknown_tool";
            logEntries.push({ type: "assistant", message: { content: [{ type: "tool_use", id: toolId, name: toolName, input: {} }] } });
            logEntries.push({ type: "user", message: { content: [{ type: "tool_result", tool_use_id: toolId, content: text }] } });
          } else if (currentRole === "assistant") {
            logEntries.push({ type: "assistant", message: { content: [{ type: "text", text }] } });
            turnCount++;
          }
          currentText = [];
        }

        logEntries.push({ type: "system", subtype: "init", model: null, session_id: null });

        for (const line of lines) {
          if (!line.trim()) continue;
          if (AWF_INFRA_RE.test(line)) continue;
          if (/max.?turns|maximum.*turns.*reached|turn limit/i.test(line)) maxTurnsHit = true;
          if (/MCP server .* failed|MCP.*connection.*error|Failed to connect to MCP/i.test(line)) {
            const serverMatch = line.match(/MCP server ['"]?([^\s'"]+)['"]?/i);
            mcpFailures.push(serverMatch ? serverMatch[1] : line.trim());
          }

          if (/^(Tool|Running|Executing|>\s*(view|edit|bash|grep|ls|write|webfetch|websearch))\b/i.test(line.trim())) {
            flushEntry();
            currentRole = "tool_use";
            currentText.push(line);
            continue;
          }
          if (/^(Assistant|Response|Output)\s*[>:]/i.test(line.trim())) {
            if (currentRole !== "assistant") { flushEntry(); currentRole = "assistant"; }
            currentText.push(line);
            continue;
          }
          if (currentRole) {
            currentText.push(line);
          } else {
            currentText.push(line);
            currentRole = "assistant";
          }
        }
        flushEntry();

        logEntries.push({ type: "result", num_turns: turnCount, usage: {} });
        const parts = [`**Turns:** ${turnCount}`, `**Tool calls:** ${toolCallIndex}`];
        if (mcpFailures.length) parts.push(`**MCP failures:** ${mcpFailures.length}`);
        if (maxTurnsHit) parts.push("**Max turns reached**");
        return { markdown: parts.join(" · "), logEntries, mcpFailures, maxTurnsHit };
      }
---

<!--
# Crush CLI

Shared engine definition for the [Crush](https://github.com/charmbracelet/crush)
coding agent. Import this file and set `engine.id: crush` with a
`provider/model` model selection.
-->
