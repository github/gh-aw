---
engine:
  id: goose
  detection-engine: copilot
  version: "1.45.0"
  display-name: Goose
  description: Goose CLI with headless execution and MCP support
  experimental: true
  provider:
    name: github
  behaviors:
    secret-strategy: universal-llm-consumer
    capabilities:
      max-turns: true
    manifest:
      files:
        - .goosehints
      path-prefixes:
        - .goose/
    network:
      defaults:
        - host.docker.internal
        - github.com
        - raw.githubusercontent.com
        - api.github.com
        - objects.githubusercontent.com
      provider-domains:
        copilot: api.githubcopilot.com
        anthropic: api.anthropic.com
        openai: api.openai.com
        google: generativelanguage.googleapis.com
    execution:
      command-name: goose
      step-name: Execute Goose CLI
      model-env-var: GOOSE_MODEL
      mcp-config-env-var: GH_AW_MCP_CONFIG
      provider-env-mode: universal-llm-consumer
      env:
        GOOSE_PROVIDER: openai
        GOOSE_MODE: auto
        GOOSE_DISABLE_SESSION_NAMING: "true"
    mcp:
      config-path: .goose/mcp.json
      config-adapter: |
        // Converts the MCP gateway's standard HTTP-based configuration to the JSON
        // format expected by the Goose CLI harness (.goose/mcp.json). Reads the
        // gateway output JSON, filters out CLI-mounted servers, sets
        // type:"streamable_http" (Goose's remote MCP extension type), rewrites URLs
        // to use the correct domain, and writes the result to
        // ${GITHUB_WORKSPACE}/.goose/mcp.json (see the harness-script below, which
        // reads this file and translates each entry into a
        // --with-streamable-http-extension or --with-extension CLI flag).
        const fs = require("fs");
        const path = require("path");

        const requireEnvVar = name => {
          const value = process.env[name];
          if (!value) throw new Error(`${name} environment variable is required`);
          return value;
        };

        const gatewayOutputPath = requireEnvVar("MCP_GATEWAY_OUTPUT");
        const workspace = requireEnvVar("GITHUB_WORKSPACE");
        // Goose runs directly on the host runner (not inside a Docker container), so use
        // MCP_GATEWAY_HOST_DOMAIN (localhost) instead of MCP_GATEWAY_DOMAIN (host.docker.internal).
        // host.docker.internal does not resolve on the host runner on Linux.
        const hostDomain = process.env.MCP_GATEWAY_HOST_DOMAIN || "localhost";
        const port = requireEnvVar("MCP_GATEWAY_PORT");
        const urlPrefix = `http://${hostDomain}:${port}`;

        let cliServers;
        try {
          cliServers = new Set(JSON.parse(process.env.GH_AW_MCP_CLI_SERVERS || "[]"));
        } catch (err) {
          throw new Error(`Failed to parse GH_AW_MCP_CLI_SERVERS: ${err instanceof Error ? err.message : String(err)}`);
        }

        const gatewayOutput = JSON.parse(fs.readFileSync(gatewayOutputPath, "utf8"));
        const rawServers = gatewayOutput.mcpServers;
        const servers = rawServers && typeof rawServers === "object" && !Array.isArray(rawServers) ? rawServers : {};

        console.log("Converting gateway configuration to Goose format...");
        console.log(`Input: ${gatewayOutputPath}`);
        console.log(`Target domain: ${hostDomain}:${port}`);
        if (cliServers.size > 0) {
          console.log(`CLI-mounted servers to filter: ${[...cliServers].join(", ")}`);
        }

        const result = {};
        for (const [name, entry] of Object.entries(servers)) {
          if (cliServers.has(name)) continue;
          const transformed = { ...entry };
          if (typeof transformed.url === "string") {
            transformed.url = transformed.url.replace(/^http:\/\/[^/]+\/mcp\//, `${urlPrefix}/mcp/`);
          }
          // Goose's remote MCP extension type is "streamable_http" (per the MCP
          // Streamable HTTP specification); the gateway's default "http" type is
          // not recognized by the Goose harness.
          transformed.type = "streamable_http";
          // The MCP gateway may include a "tools" field for Copilot, but Goose's
          // MCP config format does not support that field.
          delete transformed.tools;
          result[name] = transformed;
        }

        const output = JSON.stringify({ mcpServers: result }, null, 2);
        const totalCount = Object.keys(servers).length;
        console.log(`Servers: ${Object.keys(result).length} included, ${totalCount - Object.keys(result).length} filtered (CLI-mounted)`);

        // Create .goose directory in the workspace (matches behaviors.mcp.config-path above).
        const configFile = path.join(workspace, ".goose", "mcp.json");
        // Write with owner-only permissions (0o600) to protect the gateway bearer token.
        // mcp.json contains the bearer token for the MCP gateway; an attacker who
        // reads it could bypass the tool constraints by issuing raw JSON-RPC calls
        // directly to the gateway.
        fs.mkdirSync(path.dirname(configFile), { recursive: true });
        fs.writeFileSync(configFile, output, { mode: 0o600 });
        fs.chmodSync(configFile, 0o600);

        console.log(`Goose configuration written to ${configFile}`);
    harness-script: |
      const { createHash } = require("crypto");
      const { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } = require("fs");
      const { tmpdir } = require("os");
      const { join } = require("path");
      const { spawnSync } = require("child_process");
      const { fetchAWFReflect, resolveOpenAICompatibleEndpointFromReflect } = require("./awf_reflect.cjs");

      const [command, ...commandArgs] = process.argv.slice(2);
      const installDir = mkdtempSync(join(tmpdir(), "goose-"));
      const archive = join(installDir, "goose.tar.gz");
      const version = process.env.GH_AW_ENGINE_VERSION;
      if (!version) throw new Error("GH_AW_ENGINE_VERSION is required");
      const releaseURL = `https://github.com/aaif-goose/goose/releases/download/v${version}/goose-x86_64-unknown-linux-gnu.tar.gz`;
      const checksum = "e0db638ac437ca0a60b0c1622f45322608d228d1a285214c3bf48fd9763346a5";
      const fail = (result, action) => {
        if (result.error || result.status !== 0) {
          throw new Error(`${action} failed`);
        }
      };
      const quote = (value) => `'${String(value).replace(/'/g, "'\\''")}'`;
      const slugify = (value) =>
        String(value)
          .toLowerCase()
          .replace(/[^a-z0-9_-]+/g, "_");
      const log = (message) => process.stderr.write(`[goose-harness] ${message}\n`);

      const main = async () => {
        try {
        fail(spawnSync("curl", ["--fail", "--location", "--silent", "--show-error", "--output", archive, releaseURL], { stdio: "inherit" }), "Goose download");
        if (createHash("sha256").update(readFileSync(archive)).digest("hex") !== checksum) {
          throw new Error("Goose download checksum did not match");
        }
        fail(spawnSync("tar", ["-xzf", archive, "-C", installDir], { stdio: "inherit" }), "Goose extraction");

        const config = JSON.parse(readFileSync(process.env.GH_AW_MCP_CONFIG, "utf8"));
        const mcpServers = config.mcpServers || {};

        // Stdio-based MCP servers (command/args/env) are passed directly as
        // --with-extension CLI flags.
        const extensions = Object.values(mcpServers).flatMap((server) => {
          if (!server.command) return [];
          const env = Object.entries(server.env || {}).map(([key, value]) => `${key}=${quote(value)}`);
          return ["--with-extension", [...env, quote(server.command), ...(server.args || []).map(quote)].join(" ")];
        });

        // HTTP-based MCP servers (url/headers), such as the ones produced by the
        // MCP gateway, cannot carry an Authorization header via the
        // --with-streamable-http-extension CLI flag (it only accepts a URL and
        // "key=value" options like timeout). Instead, declare them as enabled
        // "streamable_http" extensions in a Goose config-file overlay, which
        // supports a headers map, and merge it in via GOOSE_ADDITIONAL_CONFIG_FILES.
        // Note: serde_yaml (used by Goose to load config files) accepts JSON as a
        // valid subset of YAML, so a plain JSON file works here.
        const httpExtensions = Object.entries(mcpServers).filter(([, server]) => typeof server.url === "string");
        const env = { ...process.env };
        env.GOOSE_MODEL = env.GOOSE_MODEL?.split("/", 2).at(-1);
        if (env.AWF_REFLECT_ENABLED === "1") {
          const result = await fetchAWFReflect({ logger: log });
          if (!result.ok || !result.reflectData) {
            throw new Error(`Unable to discover the Goose LLM endpoint from /reflect: ${result.reason || "empty response"}`);
          }
          const endpoint = resolveOpenAICompatibleEndpointFromReflect({
            provider: env.GH_AW_LLM_PROVIDER,
            reflectData: result.reflectData,
            logger: log,
          });
          if (!endpoint) {
            throw new Error(`No configured /reflect endpoint found for provider ${env.GH_AW_LLM_PROVIDER || "(missing)"}`);
          }
          env.OPENAI_HOST = endpoint.host;
          env.OPENAI_BASE_PATH = endpoint.basePath;
          log(`configured Goose endpoint for provider=${endpoint.provider}: ${endpoint.host}/${endpoint.basePath}`);
        }
        if (httpExtensions.length > 0) {
          const extensionsConfig = {
            extensions: Object.fromEntries(
              httpExtensions.map(([name, server]) => [
                slugify(name),
                {
                  enabled: true,
                  type: "streamable_http",
                  name,
                  uri: server.url,
                  headers: server.headers || {},
                  timeout: 300,
                },
              ])
            ),
          };
          const extensionsConfigFile = join(installDir, "goose-mcp-extensions.json");
          writeFileSync(extensionsConfigFile, JSON.stringify(extensionsConfig, null, 2), { mode: 0o600 });
          const existingConfigFiles = env.GOOSE_ADDITIONAL_CONFIG_FILES ? env.GOOSE_ADDITIONAL_CONFIG_FILES.split(":").filter(Boolean) : [];
          env.GOOSE_ADDITIONAL_CONFIG_FILES = [...existingConfigFiles, extensionsConfigFile].join(":");
        }

        const prompt = readFileSync(process.env.GH_AW_PROMPT, "utf8");
        fail(spawnSync(join(installDir, command), [...commandArgs, "run", "--no-session", ...extensions, "-t", prompt], { stdio: "inherit", env }), "Goose execution");
        } finally {
          if (existsSync(installDir)) rmSync(installDir, { recursive: true, force: true });
        }
      };

      main().catch((error) => {
        log(error instanceof Error ? error.message : String(error));
        process.exitCode = 1;
      });
    log-parser: |
      function parseLog(logContent) {
        const lines = logContent.split("\n");
        const logEntries = [];
        const mcpFailures = [];
        let maxTurnsHit = false;
        let turnCount = 0;
        let toolCallIndex = 0;
        let currentRole = null;
        let currentText = [];
        const AWF_INFRA_RE = /^\[(INFO|WARN|SUCCESS|ERROR|entrypoint|health-check|goose-harness)\]|^ (?:Container|Network|Volume) |^Process exiting with code:/;

        function flushEntry() {
          if (!currentRole || currentText.length === 0) { currentText = []; return; }
          const text = currentText.join("\n").trim();
          if (!text) { currentText = []; return; }
          if (currentRole === "tool_use") {
            const toolId = `goose_tool_${toolCallIndex++}`;
            const nameMatch = text.match(/(?:calling|using|tool[_\s]*(?:call|use))\s+(\S+)/i);
            const toolName = nameMatch ? nameMatch[1] : "unknown_tool";
            logEntries.push({ type: "assistant", message: { content: [{ type: "tool_use", id: toolId, name: toolName, input: {} }] } });
            logEntries.push({ type: "user", message: { content: [{ type: "tool_result", tool_use_id: toolId, content: text }] } });
          } else if (currentRole === "assistant") {
            logEntries.push({ type: "assistant", message: { content: [{ type: "text", text }] } });
            turnCount++;
          }
          currentText = [];
        }

        // Init entry
        logEntries.push({ type: "system", subtype: "init", model: null, session_id: null });

        for (const line of lines) {
          if (AWF_INFRA_RE.test(line)) continue;
          if (/max.?turns|maximum.*turns.*reached|turn limit/i.test(line)) maxTurnsHit = true;
          if (/MCP server .* failed|MCP.*connection.*error|Failed to connect to MCP/i.test(line)) {
            const serverMatch = line.match(/MCP server ['"]?([^\s'"]+)['"]?/i);
            mcpFailures.push(serverMatch ? serverMatch[1] : line.trim());
          }

          if (/^─{3,}|^━{3,}/.test(line)) {
            flushEntry();
            currentRole = null;
            continue;
          }
          if (/^(calling|using|tool[_\s]*(call|use|result))\b/i.test(line.trim())) {
            flushEntry();
            currentRole = "tool_use";
            currentText.push(line);
            continue;
          }
          if (/^(assistant|goose)\s*[>:]/i.test(line.trim())) {
            if (currentRole !== "assistant") { flushEntry(); currentRole = "assistant"; }
            currentText.push(line);
            continue;
          }
          if (/^(user|human)\s*[>:]/i.test(line.trim())) {
            flushEntry();
            currentRole = null;
            continue;
          }
          if (currentRole) currentText.push(line);
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
# Goose CLI

Shared engine definition for the [Goose](https://github.com/aaif-goose/goose)
open-source AI agent. Import this file and set `engine: id: goose` to use it.
-->
