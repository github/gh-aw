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
        - api2.cursor.sh
        - agentn.global.api5.cursor.sh
      provider-domains:
        cursor: api2.cursor.sh
    execution:
      command-name: cursor-agent
      args:
        - -p
        - --force
        - --trust
        - --approve-mcps
        - --output-format
        - stream-json
      step-name: Execute Cursor Agent CLI
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
      const { existsSync, mkdtempSync, readFileSync, rmSync } = require("fs");
      const { tmpdir } = require("os");
      const { join } = require("path");
      const { spawnSync } = require("child_process");

      const [, ...commandArgs] = process.argv.slice(2);
      const installDir = mkdtempSync(join(tmpdir(), "cursor-agent-"));
      const archive = join(installDir, "cursor-agent.tar.gz");
      const version = "2026.10.14-941492e";
      const releases = {
        x64: {
          arch: "x64",
          checksum: "6f6bf39dd2df29dcd38643cdfab0155ce71699953889f732deec123f6562bb2d",
        },
        arm64: {
          arch: "arm64",
          checksum: "cbba40d90d7f699874e133a5c15bf3854cee006565bd3bb00a6c79b49d62b0d0",
        },
      };
      const fail = (result, action) => {
        if (result.error) throw result.error;
        if (result.status !== 0) throw new Error(`${action} failed with exit code ${result.status ?? "unknown"}`);
      };
      const log = message => process.stderr.write(`[cursor-harness] ${message}\n`);

      try {
        if (!process.env.CURSOR_API_KEY && process.env.SECRET_CURSOR_API_KEY) {
          process.env.CURSOR_API_KEY = process.env.SECRET_CURSOR_API_KEY;
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
        fail(spawnSync(executable, [...commandArgs, ...modelArgs, "--", prompt], {
          stdio: "inherit",
          env: process.env,
        }), "Cursor Agent execution");
      } catch (error) {
        log(error instanceof Error ? error.message : String(error));
        process.exitCode = 1;
      } finally {
        rmSync(installDir, { recursive: true, force: true });
      }
    log-parser: |
      function parseLog(logContent) {
        const { normalizeAgentSession, isSessionEvent, isTokenCount } = require("./agent_session.cjs");
        const normalizeCursorRecord = record => {
          const events = normalizeAgentSession([record], { sourceEngine: "cursor" });
          for (const event of events) {
            const usage = event.data.usage;
            if (record.type === "result" && event.type === "session.result" && usage &&
                !Object.hasOwn(record.usage, "input_tokens_include_cache") &&
                !Object.hasOwn(record.usage, "inputTokensIncludeCache") &&
                [usage.cache_read_input_tokens, usage.cache_creation_input_tokens].some(isTokenCount)) {
              usage.input_tokens_include_cache = false;
            }
          }
          return events;
        };
        const logEntries = [];
        const AWF_INFRA_RE = /^\[(INFO|WARN|SUCCESS|ERROR|entrypoint|health-check|cursor-harness)\]|^ (?:Container|Network|Volume) |^Process exiting with code:/;
        const plaintext = [];
        const append = event => {
          const timestamp = event.data.timestamp_ms;
          if (event.timestamp === undefined && Number.isSafeInteger(timestamp) && timestamp >= 0) event.timestamp = timestamp;
          logEntries.push(event);
        };
        const emit = (type, data) => append({ type, data });
        const mcpFailures = new Set();
        let parseErrors = 0;
        let fence;
        let lastAssistant;
        const lines = logContent.split("\n").map((line, index) => {
          const marker = line.match(/^ {0,3}(`{3,}|~{3,})(.*)$/);
          const fenced = Boolean(fence || marker);
          if (fence) {
            if (marker && marker[1][0] === fence[0] && marker[1].length >= fence.length && !marker[2].trim()) fence = undefined;
          } else if (marker) fence = marker[1];
          let record;
          if (!fenced && line.trimStart().startsWith("{")) {
            try { record = JSON.parse(line); } catch {}
          }
          return { line, index, fenced, record };
        }).filter(({ line }) => !AWF_INFRA_RE.test(line));
        // An init outside Markdown fences establishes native framing even with
        // startup diagnostics. Otherwise only an all-JSONL log is a native stream.
        const structured = lines.some(({ fenced, record }) => !fenced &&
          (record?.type === "system" && record.subtype === "init" || record?.type === "session.init")) ||
          lines.every(({ line, fenced }) => !line.trim() || !fenced && line.trimStart().startsWith("{")) &&
          lines.some(({ record }) => typeof record?.type === "string");
        const trailer = lines.findLast(entry => entry.line.trim());

        for (const { line, index, fenced, record } of lines) {
          const previousAssistant = lastAssistant;
          lastAssistant = undefined;
          if (!structured) {
            if (!fenced && record?.type === "result" && record.subtype === undefined && record.session_id === undefined &&
                trailer?.index === index) {
              logEntries.push(...normalizeCursorRecord(record));
            } else plaintext.push(line);
            continue;
          }
          if (!line.trim()) continue;
          if (!line.trimStart().startsWith("{")) {
            plaintext.push(line);
            if (/MCP server .* failed|MCP.*connection.*error|Failed to connect to MCP/i.test(line)) {
              const server = line.match(/MCP server ['"]?([^\s'"]+)['"]?/i);
              mcpFailures.add(server ? server[1] : line.trim());
            }
            continue;
          }
          if (record === undefined) {
            parseErrors++;
            emit("session.collection_warning", { sourceEngine: "cursor", code: "malformed_jsonl", line: index + 1 });
            continue;
          }
          if (!record || typeof record !== "object" || typeof record.type !== "string") {
            parseErrors++;
            emit("session.collection_warning", { sourceEngine: "cursor", code: "malformed_cursor_event", line: index + 1 });
            continue;
          }
          if (isSessionEvent(record)) {
            logEntries.push(record);
            if (record.type === "assistant.message") lastAssistant = record;
            continue;
          }
          const context = { ...record, sourceEngine: "cursor" };
          if (typeof record.session_id === "string") context.sessionId = record.session_id;
          if (record.type === "tool_call") {
            const calls = record.tool_call && typeof record.tool_call === "object" ? Object.entries(record.tool_call).filter(([name, call]) =>
              (name === "function" || name.endsWith("ToolCall")) && call && typeof call === "object" && !Array.isArray(call)) : [];
            for (const [name, call] of calls) {
              const data = { ...context, toolName: name === "function" ? call.name : name };
              if (typeof record.call_id === "string") data.toolCallId = record.call_id;
              if (typeof record.model_call_id === "string") data.messageId = record.model_call_id;
              if (Object.hasOwn(call, "args")) data.input = call.args;
              else if (Object.hasOwn(call, "arguments")) data.input = call.arguments;
              if (record.subtype === "started") {
                emit("tool.execution_start", data);
              } else if (record.subtype === "completed") {
                if (Object.hasOwn(call, "result")) {
                  data.output = call.result;
                  if (call.result?.error != null) data.success = false;
                  else if (call.result?.success != null) data.success = true;
                }
                emit("tool.execution_complete", data);
              } else {
                emit("cursor.tool_call", context);
              }
            }
            if (calls.length === 0) emit("cursor.tool_call", context);
          } else if (record.type === "thinking" && (record.subtype === "delta" || record.subtype === "completed")) {
            const data = { ...context, delta: record.subtype === "delta" };
            if (typeof record.text === "string") data.content = record.text;
            emit("assistant.reasoning", data);
          } else {
            const normalized = normalizeCursorRecord(record);
            if (normalized.length === 0) emit("cursor.event", context);
            for (const event of normalized) {
              const data = { ...context, ...event.data };
              if (event.type === "assistant.message" && typeof record.model_call_id === "string") data.apiCallId = record.model_call_id;
              if (event.type === "session.result" && typeof record.is_error === "boolean") data.status = record.is_error ? "error" : "success";
              for (const key of Object.keys(data)) if (data[key] === undefined) delete data[key];
              const appended = { ...event, data };
              append(appended);
              lastAssistant = record.type === "assistant" && event.type === "assistant.message" ? appended : undefined;
            }
            if (record.type === "result" && typeof record.result === "string") {
              const previous = previousAssistant?.data;
              const sameSession = typeof context.sessionId === "string" && context.sessionId.length > 0 && previous?.sessionId === context.sessionId;
              const sameRequest = typeof record.request_id === "string" && record.request_id.length > 0 && previous?.apiCallId === record.request_id;
              const conflictingSession = typeof previous?.sessionId === "string" && typeof context.sessionId === "string" && previous.sessionId !== context.sessionId;
              const conflictingRequest = typeof previous?.apiCallId === "string" && typeof record.request_id === "string" && previous.apiCallId !== record.request_id;
              if (previous?.content === record.result && (sameSession || sameRequest) && !conflictingSession && !conflictingRequest) {
                if (typeof record.request_id === "string") {
                  previous.request_id = record.request_id;
                  previous.apiCallId = record.request_id;
                }
              } else {
                const data = { ...context, content: record.result };
                if (typeof record.request_id === "string") data.apiCallId = record.request_id;
                emit("assistant.message", data);
              }
            }
          }
        }
        // Legacy text contains final-answer prose, not observable tool activity.
        if (plaintext.some(line => line.trim())) emit(structured ? "cursor.diagnostic" : "assistant.message", { sourceEngine: "cursor", content: plaintext.join("\n") });
        const toolStarts = logEntries.filter(entry => entry.type === "tool.execution_start").length;
        const markdown = `**Observed tool starts:** ${toolStarts}` + (parseErrors ? ` · **Parse errors:** ${parseErrors}` : "");
        return { markdown, logEntries, mcpFailures: [...mcpFailures], maxTurnsHit: false };
      }
---

<!--
# Cursor Agent CLI

Shared engine definition for the [Cursor Agent CLI](https://cursor.com/docs/cli).
Import this file and set `engine: id: cursor` to use it:

```yaml
engine:
  id: cursor
model: cursor/auto
imports:
  - shared/cursor.md
```

Configure the `CURSOR_API_KEY` GitHub Actions secret with an API key from the
Cursor dashboard. Cursor serves the selected model through its own API, so this
engine does not use universal provider routing.

The Linux x64 and arm64 archives are pinned to `2026.10.14-941492e`, the
distribution selected by the [official installer](https://cursor.com/install)
on October 15, 2026, and verified with architecture-specific SHA-256 checksums.
The [CLI changelog](https://cursor.com/docs/cli/changelog) currently documents
changes through October 8, not this individual build. Recent changes include
headless subagent completion, exact model selection, MCP tool refresh, and
plugin hook support; the existing print, trust, MCP approval, and API-key
interfaces remain supported.

Native MCP configuration is written to `.cursor/mcp.json` with mode `0600`.
Plugins are loaded from `~/.cursor/plugins/local`.

The CLI emits [stream-json](https://cursor.com/docs/cli/reference/output-format)
without partial text deltas. The parser preserves observed session, message,
tool-call IDs, arguments, results, and terminal metadata as unified session
events. Hook contexts and tool envelope metadata are not tool calls. Native
reasoning deltas and completion markers use `assistant.reasoning`; observed
millisecond timestamps, model-call correlations, cache counters, and distinct
terminal answer text are retained in their canonical fields. Observed separate
cache counters are excluded from input tokens unless an inclusion flag is
explicitly supplied. Terminal-answer deduplication requires an immediately
preceding matching answer and compatible session or request identity. It does not invent
turn counts, token usage, or tool success when
these are absent. Legacy text is retained as final-answer prose, never parsed
as tool calls, including JSON examples in Markdown fences. Native framing
requires an initialization record outside a fence or an all-JSONL log.
Rejected native records emit payload-free collection warnings with 1-based
line numbers. MCP connection failures are recognized only in non-JSON native
diagnostics, not in legacy answers or JSON-encoded assistant messages.
Sanitized regressions cover the native shapes observed in an existing
2026.10.14-941492e conformance run. Failed-tool results and subagent events
remain fixture-only or unverified where absent from live evidence.
-->
