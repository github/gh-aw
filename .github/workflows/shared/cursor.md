---
engine:
  id: cursor
  detection-engine: copilot
  display-name: Cursor
  description: Cursor Agent CLI with headless execution and native MCP support
  experimental: true
  provider:
    name: cursor
  auth:
    - role: api-key
      secret: CURSOR_API_KEY
  env:
    OPENAI_BASE_URL: https://api2.cursor.sh
    OPENAI_API_KEY: ${{ secrets.CURSOR_API_KEY }}
    CURSOR_API_KEY: awf-proxy
  behaviors:
    supported-env-var-keys:
      - CURSOR_API_KEY
    plugins:
      directory: ~/.cursor/plugins/local
    manifest:
      files:
        - AGENTS.md
        - .cursorignore
      path-prefixes:
        - .cursor/
    network:
      defaults:
        - host.docker.internal
        - github.com
        - raw.githubusercontent.com
        - api.github.com
        - objects.githubusercontent.com
        - downloads.cursor.com
    execution:
      command-name: cursor-agent
      args:
        - -p
        - --force
        - --trust
        - --approve-mcps
        - --output-format
        - text
      step-name: Execute Cursor Agent CLI
      api-target-env-var: OPENAI_BASE_URL
      prepare-env-var: OPENAI_API_KEY
      prepare-script: |
        const main = async () => {
          const rawEndpoint = process.env.OPENAI_BASE_URL;
          const apiKey = process.env.OPENAI_API_KEY;
          if (!rawEndpoint || !apiKey) throw new Error("Cursor gateway authentication requires its upstream and CURSOR_API_KEY secret");
          const endpoint = new URL(rawEndpoint.includes("://") ? rawEndpoint : `https://${rawEndpoint}`);
          if (endpoint.protocol !== "https:" || endpoint.username || endpoint.password || endpoint.search || endpoint.hash) {
            throw new Error("Cursor authentication requires an HTTPS upstream without userinfo, query, or fragment");
          }
          const exchangeURL = new URL(`${endpoint.pathname.replace(/\/+$/, "")}/auth/exchange_user_api_key`, endpoint.origin);
          const response = await fetch(exchangeURL, {
            method: "POST",
            headers: { "Content-Type": "application/json", Authorization: `Bearer ${apiKey}` },
            body: "{}",
            redirect: "error",
            signal: AbortSignal.timeout(30000),
          });
          if (!response.ok) throw new Error(`Cursor API-key exchange failed with HTTP ${response.status}`);
          const data = await response.json();
          if (typeof data.accessToken !== "string" || !data.accessToken || /[\r\n]/.test(data.accessToken)) {
            throw new Error("Cursor API-key exchange returned no valid access token");
          }
          const masked = data.accessToken.replace(/%/g, "%25").replace(/\r/g, "%0D").replace(/\n/g, "%0A");
          process.stderr.write(`::add-mask::${masked}\n`);
          process.stdout.write(data.accessToken);
        };
        main().catch(() => {
          process.stderr.write("Cursor gateway authentication failed; verify CURSOR_API_KEY and engine.api-target\n");
          process.exitCode = 1;
        });
      model-env-var: CURSOR_MODEL
      mcp-config-env-var: GH_AW_MCP_CONFIG
      write-timestamp: true
    mcp:
      config-path: .cursor/mcp.json
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
        const mcpServers = {};

        for (const [name, entry] of Object.entries(servers)) {
          if (cliServers.has(name) || !entry || typeof entry !== "object") continue;
          const transformed = { ...entry };
          if (typeof transformed.url === "string") {
            transformed.url = transformed.url.replace(/^http:\/\/[^/]+\/mcp\//, `${gatewayURL}/mcp/`);
            delete transformed.type;
          }
          delete transformed.tools;
          mcpServers[name] = transformed;
        }

        const configPath = path.join(workspace, ".cursor", "mcp.json");
        fs.mkdirSync(path.dirname(configPath), { recursive: true });
        fs.writeFileSync(configPath, JSON.stringify({ mcpServers }, null, 2), { mode: 0o600 });
        fs.chmodSync(configPath, 0o600);
    harness-script: |
      const { createHash } = require("crypto");
      const { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } = require("fs");
      const { tmpdir } = require("os");
      const { join } = require("path");
      const { spawnSync } = require("child_process");
      const { fetchAWFReflect, resolveProviderEndpointFromReflect } = require("./awf_reflect.cjs");

      const [, ...commandArgs] = process.argv.slice(2);
      const installDir = mkdtempSync(join(tmpdir(), "cursor-agent-"));
      const archive = join(installDir, "cursor-agent.tar.gz");
      const version = "2026.07.20-8cc9c0b";
      const releases = {
        x64: {
          arch: "x64",
          checksum: "6e9f17247ffeb5f8f7e2246b4bcd6bb26cb2d5a9f9a4b0012c9a80d868ed25b4",
        },
        arm64: {
          arch: "arm64",
          checksum: "2986152b283c70a666b015035b2e99a96d13afd2660a587b8639417cfdd147fb",
        },
      };
      const fail = (result, action) => {
        if (result.error) throw result.error;
        if (result.status !== 0) throw new Error(`${action} failed with exit code ${result.status ?? "unknown"}`);
      };
      const log = message => process.stderr.write(`[cursor-harness] ${message}\n`);

      const main = async () => {
        try {
          if (process.env.AWF_REFLECT_ENABLED !== "1") {
            throw new Error("Cursor requires the AWF LLM gateway; enable sandbox.agent: awf");
          }
          const result = await fetchAWFReflect({ logger: log, outputPath: "/tmp/gh-aw/awf-reflect.json" });
          if (!result.ok || !result.reflectData) {
            throw new Error(`Unable to discover the Cursor LLM endpoint from /reflect: ${result.reason || "empty response"}`);
          }
          const endpoint = resolveProviderEndpointFromReflect({
            provider: "openai",
            reflectData: result.reflectData,
            requireProviderMatch: true,
            logger: log,
          });
          if (!endpoint?.baseUrl) {
            throw new Error("No configured OpenAI gateway endpoint found in AWF /reflect; configure the Cursor custom upstream and CURSOR_API_KEY secret");
          }

          const release = releases[process.arch];
          if (!release) throw new Error(`Unsupported Cursor Agent architecture: ${process.arch}`);
          const releaseURL = `https://downloads.cursor.com/lab/${version}/linux/${release.arch}/agent-cli-package.tar.gz`;

          fail(spawnSync("curl", ["--fail", "--location", "--silent", "--show-error", "--output", archive, releaseURL], { stdio: "inherit" }), "Cursor Agent download");
          if (createHash("sha256").update(readFileSync(archive)).digest("hex") !== release.checksum) {
            throw new Error("Cursor Agent download checksum did not match");
          }
          fail(spawnSync("tar", ["-xzf", archive, "-C", installDir], { stdio: "inherit" }), "Cursor Agent extraction");

          const executable = join(installDir, "dist-package", "cursor-agent");
          if (!existsSync(executable)) throw new Error("Cursor Agent executable was not found in the release archive");
          fail(spawnSync(executable, ["--version"], { stdio: "inherit" }), "Cursor Agent verification");

          const promptPath = process.env.GH_AW_PROMPT;
          if (!promptPath) throw new Error("GH_AW_PROMPT is required");
          const prompt = readFileSync(promptPath, "utf8");
          const selectedModel = process.env.CURSOR_MODEL;
          const model = selectedModel?.includes("/") ? selectedModel.slice(selectedModel.indexOf("/") + 1) : selectedModel;
          const modelArgs = model ? ["--model", model] : [];
          const configDir = join(installDir, "config");
          mkdirSync(configDir, { recursive: true, mode: 0o700 });
          writeFileSync(join(configDir, "cli-config.json"), JSON.stringify({
            version: 1,
            editor: { vimMode: false },
            permissions: { allow: [], deny: [] },
            network: { useHttp1ForAgent: true },
          }), { mode: 0o600 });
          const gatewayArgs = ["--auth-token", "awf-proxy", "--endpoint", endpoint.baseUrl, "--agent-endpoint", endpoint.baseUrl, "--http-version", "1.1"];
          fail(spawnSync(executable, [...commandArgs, ...modelArgs, ...gatewayArgs, prompt], {
            stdio: "inherit",
            env: {
              ...process.env,
              CURSOR_API_KEY: "awf-proxy",
              CURSOR_CONFIG_DIR: configDir,
              CURSOR_API_ENDPOINT: endpoint.baseUrl,
              CURSOR_API_BASE_URL: endpoint.baseUrl,
            },
          }), "Cursor Agent execution");
        } finally {
          rmSync(installDir, { recursive: true, force: true });
        }
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
        const AWF_INFRA_RE = /^\[(INFO|WARN|SUCCESS|ERROR|entrypoint|health-check|cursor-harness)\]|^ (?:Container|Network|Volume) |^Process exiting with code:/;
        let toolCallIndex = 0;
        let turnCount = 0;
        let currentRole = null;
        let currentText = [];

        function flushEntry() {
          if (!currentRole || currentText.length === 0) { currentText = []; return; }
          const text = currentText.join("\n").trim();
          if (!text) { currentText = []; return; }
          if (currentRole === "tool_use") {
            const toolId = `cursor_tool_${toolCallIndex++}`;
            const nameMatch = text.match(/^(?:Tool|Running|Executing|>\s*)([\w_.-]+)/i);
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

          if (/^(Tool|Running|Executing|>\s*[\w_.-]+)\b/i.test(line.trim())) {
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
          if (!currentRole) currentRole = "assistant";
          currentText.push(line);
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
# Cursor Agent CLI

Shared engine definition for the [Cursor Agent CLI](https://cursor.com/docs/cli).
Import this file and set `engine: id: cursor` to use it:

```yaml
engine:
  id: cursor
  api-target: api2.cursor.sh
model: cursor/auto
imports:
  - shared/cursor.md
```

Configure the `CURSOR_API_KEY` GitHub Actions secret with an API key from the
Cursor dashboard. The definition configures Cursor's upstream API as the AWF
OpenAI route's custom target via `engine.env.OPENAI_BASE_URL`. The real key is
exchanged on the runner for a session token before AWF starts. The token is
masked, bound to the runner's `OPENAI_API_KEY` for sidecar authentication, and
excluded from the agent. Cursor receives only the non-secret `awf-proxy`
session-token placeholder, so the CLI does not perform its own API-key exchange.

`api-target` is optional: omitting it binds the gateway to `api2.cursor.sh`.
To use an organization-managed Cursor endpoint, change just
`engine.api-target` to that hostname. This changes AWF's upstream destination,
not the CLI's local gateway URL; no OpenAI-specific environment variables or
gateway ports are required in the consuming workflow.

The harness resolves the provider's live gateway URL using `awf_reflect.cjs`,
binds both `--endpoint` and `--agent-endpoint`, and uses an ephemeral HTTP/1.1
configuration. It does not hard-code a gateway port, fall back to another
provider, or permit direct Cursor inference as a fallback. Disabling AWF is not
supported. The OpenAI route forwards Cursor's native HTTP requests to the
custom upstream; this is not an OpenAI protocol conversion. Cursor-specific
model validation and token accounting are not supplied by this endpoint
configuration, so do not assume those gateway controls cover Cursor's native
protocol.
-->
