---
engine:
  id: opencode
  detection-engine: copilot
  version: "2.0.26"
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
      package-name: "@opencode/cli"
      step-name: Install OpenCode CLI
      binary-name: opencode
      include-node-setup: true
      # The pinned package's postinstall links its platform binary into bin/opencode.exe.
      post-install-scripts: true
      cooldown: true
      verify-command: opencode --version
      verify-step-name: Verify OpenCode CLI installation
      docs-url: https://opencode.ai/v2/docs
    execution:
      command-name: opencode
      args:
        - run
        - --standalone
        - --format
        - json
        - --agent
        - build
        - --thinking
        - --print-logs
        - --log-level
        - info
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
        OPENCODE_DISABLE_MODELS_FETCH: "1"
        NO_COLOR: "1"
    mcp:
      config-path: /tmp/gh-aw/opencode-mcp.json
      config-adapter: |
        const { loadGatewayContext, filterAndTransformServers, rewriteUrl, writeSecureOutput, addWebFetchMCPServer } = require("./convert_gateway_config_shared.cjs");
        const context = loadGatewayContext();
        const servers = filterAndTransformServers(context.servers, context.cliServers, (name, entry) => {
          if (typeof entry.url !== "string") {
            throw new Error(`OpenCode requires a gateway HTTP URL for MCP server ${name}`);
          }
          return {
            type: "remote",
            url: rewriteUrl(entry.url, context.urlPrefix),
            disabled: false,
            codemode: false,
            oauth: false,
            ...(entry.headers ? { headers: entry.headers } : {}),
          };
        });
        addWebFetchMCPServer(servers, config => ({ type: "local", command: [config.command, ...config.args], disabled: false, codemode: false }));
        writeSecureOutput("/tmp/gh-aw/opencode-mcp.json", JSON.stringify({ mcp: { servers } }, null, 2));
    harness-script: |
      const { readFileSync, existsSync } = require("fs");
      const { execFile } = require("child_process");
      const { runProcess } = require("./process_runner.cjs");
      const { isOpenCodeEvent, getOpenCodeMCPFailures, getOpenCodeSessionSteps } = require("./parse_opencode_log.cjs");
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
        const rosterEnv = process.env.GH_AW_OPENCODE_MODELS || "";
        const roster = rosterEnv
          .split(",")
          .map(entry => entry.trim())
          .filter((entry, index, all) => entry && all.indexOf(entry) === index);
        if (!roster.includes(model)) roster.unshift(model);
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
          update: "disable",
          share: "disabled",
          experimental: {
            policies: [
              { action: "provider.use", resource: "*", effect: "deny" },
              { action: "provider.use", resource: "awf-proxy", effect: "allow" },
            ],
          },
          permissions: [{ action: "*", resource: "*", effect: "allow" }],
          agents: { build: { permissions: [{ action: "*", resource: "*", effect: "allow" }], ...(maxTurns !== undefined ? { steps: maxTurns } : {}) } },
          mcp: {
            servers: {},
            timeout: {
              ...(startupTimeout !== undefined ? { startup: startupTimeout, catalog: startupTimeout } : {}),
              ...(toolTimeout !== undefined ? { execution: toolTimeout } : {}),
            },
          },
          providers: {
            "awf-proxy": {
              name: "GitHub Agentic Workflows",
              package: isAnthropic ? "@opencode/ai/providers/anthropic" : "@opencode/ai/providers/openai-compatible",
              settings: { baseURL, apiKey },
              models: Object.fromEntries(roster.map(entry => [entry, { name: entry }])),
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
          if (!mcpConfig?.mcp?.servers || typeof mcpConfig.mcp.servers !== "object" || Array.isArray(mcpConfig.mcp.servers)) {
            throw new Error("OpenCode MCP configuration must contain an mcp.servers object");
          }
          config.mcp.servers = mcpConfig.mcp.servers;
        }
        const promptPath = process.env.GH_AW_PROMPT;
        if (!promptPath) throw new Error("GH_AW_PROMPT is required");
        const prompt = readFileSync(promptPath, "utf8");
        let turns = 0;
        let reportedError = false;
        let maxTurnsHit = false;
        let sessionID;
        const finishedSteps = new Set();
        const finishedMessageParts = new Map();
        const observedMCPFailures = new Set();
        const runtimeEnv = { ...process.env, OPENCODE_CONFIG_CONTENT: JSON.stringify(config) };
        delete runtimeEnv.PWD;
        const hasSafeOutputs = () => {
          const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
          return !safeOutputsPath || (existsSync(safeOutputsPath) && readFileSync(safeOutputsPath, "utf8").trim() !== "");
        };
        const handleEvent = line => {
          let entry;
          try { entry = JSON.parse(line); } catch { return; }
          if (!isOpenCodeEvent(entry)) return;
          if (entry.sessionID) sessionID = entry.sessionID;
          if (entry?.type === "error" && entry.error) reportedError = true;
          if (maxTurns !== undefined && entry.type === "tool_use" && entry.part?.state?.status === "error" && entry.part.state.error === "Tools are disabled after the maximum agent steps") {
            maxTurnsHit = true;
          }
          if (entry?.type !== "step_finish") return;
          const messageIdentity = typeof entry.part.messageID === "string" ? JSON.stringify([entry.sessionID, entry.part.messageID]) : undefined;
          const identity = messageIdentity ?? (typeof entry.part.id === "string" ? JSON.stringify([entry.sessionID, "part", entry.part.id]) : undefined);
          if (identity !== undefined && finishedSteps.has(identity)) return;
          if (identity !== undefined) finishedSteps.add(identity);
          if (messageIdentity !== undefined) finishedMessageParts.set(messageIdentity, { id: entry.part.id, messageID: entry.part.messageID });
          turns++;
          if (maxTurns !== undefined && turns >= maxTurns && entry.part?.reason === "tool-calls") {
            maxTurnsHit = true;
          }
        };
        const runOptions = {
          command,
          log,
          env: runtimeEnv,
          maxCollectedOutputBytes: 1024 * 1024,
          onStderrLine: line => {
            for (const server of getOpenCodeMCPFailures(line)) observedMCPFailures.add(server);
          },
          onStdoutLine: handleEvent,
          runtimeGuard: { shouldTerminate: () => ({ terminate: maxTurnsHit, reason: "OpenCode max-turns limit reached" }), pollIntervalMs: 100 },
        };
        const exportSessionRecords = async () => {
          const exported = await new Promise((resolve, reject) => {
            execFile(command, ["session", "export", "--standalone", sessionID], {
              env: runtimeEnv,
              cwd: process.env.GH_AW_ENGINE_CWD || process.env.GITHUB_WORKSPACE || undefined,
              timeout: 60000,
              maxBuffer: 64 * 1024 * 1024,
            }, (error, stdout) => {
              if (error) {
                const diagnostics = [];
                if (Number.isSafeInteger(error.code) || (typeof error.code === "string" && /^[A-Z][A-Z0-9_]{0,63}$/.test(error.code))) diagnostics.push(`code=${error.code}`);
                if (typeof error.signal === "string" && /^SIG[A-Z0-9]{1,16}$/.test(error.signal)) diagnostics.push(`signal=${error.signal}`);
                if (error.killed === true) diagnostics.push("killed=true");
                const detail = diagnostics.length ? ` (${diagnostics.join(", ")})` : "";
                return reject(new Error(`Unable to export the OpenCode session for step reconciliation${detail}`, { cause: error }));
              }
              try { resolve(JSON.parse(stdout)); } catch {
                reject(new Error("Failed to parse the OpenCode session export"));
              }
            });
          });
          return getOpenCodeSessionSteps(exported, sessionID);
        };
        const writeSessionRecords = records => {
          for (const entry of records) {
            if (entry.type === "step_finish") {
              const observedPart = finishedMessageParts.get(JSON.stringify([entry.sessionID, entry.part.messageID]));
              if (observedPart) entry.part = { ...entry.part, id: observedPart.id, messageID: observedPart.messageID };
            }
            process.stdout.write(JSON.stringify(entry) + "\n");
          }
        };
        let result = await runProcess({
          ...runOptions,
          args: [...commandArgs, "--model", `awf-proxy/${model}`],
          logArgs: [...commandArgs, "--model", "(configured model)"],
          attempt: 0,
          stdin: prompt,
        });
        let reconciledRecords;
        for (let nudge = 1; nudge <= 2; nudge++) {
          if (result.exitCode !== 0 || maxTurnsHit || hasSafeOutputs() || !sessionID) break;
          let remainingTurns;
          if (maxTurns !== undefined) {
            reconciledRecords = await exportSessionRecords();
            const completedSteps = reconciledRecords.filter(entry => entry.type === "step_finish").length;
            if (!completedSteps) throw new Error("OpenCode session export contains no completed model steps");
            turns = Math.max(turns, completedSteps);
            if (turns >= maxTurns) {
              maxTurnsHit = true;
              break;
            }
            remainingTurns = maxTurns - turns;
          }
          log(`No safe output yet, continuing the session (nudge ${nudge} of 2)`);
          result = await runProcess({
            ...runOptions,
            env: remainingTurns === undefined ? runtimeEnv : {
              ...runtimeEnv,
              OPENCODE_CONFIG_CONTENT: JSON.stringify({
                ...config,
                agents: { ...config.agents, build: { ...config.agents.build, steps: remainingTurns } },
              }),
            },
            args: [...commandArgs, "--session", sessionID],
            logArgs: [...commandArgs, "--session", "(continued session)"],
            attempt: nudge,
            stdin: "Your last message ended without a tool call, so nothing has been delivered and this run is about to end with no output. Do not explain or plan: continue the task now and finish with the safe-output call the task names.",
          });
        }
        if (maxTurnsHit) {
          if (reconciledRecords) writeSessionRecords(reconciledRecords);
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
        if (!sessionID) throw new Error("OpenCode produced no session events");
        const records = await exportSessionRecords();
        const steps = records.filter(entry => entry.type === "step_finish");
        if (!steps.length) throw new Error("OpenCode session export contains no completed model steps");
        writeSessionRecords(records);
        if (maxTurns !== undefined && steps.slice(maxTurns - 1).some(entry => entry.part.reason === "tool-calls")) {
          process.stdout.write(JSON.stringify({ type: "opencode.max_turns", timestamp: Date.now(), data: { maxTurns } }) + "\n");
          throw new Error("OpenCode max-turns limit reached");
        }
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
prompt on stdin, and requests JSONL events. `run --standalone` starts a private,
short-lived server so the run cannot reuse a background service with different
configuration or credentials. Provider credentials remain in AWF;
only a placeholder key reaches the agent. `engine.max-turns` limits completed
model steps, and a tool-calling turn at the limit fails explicitly. Before each
empty-run continuation, the harness reconciles completed steps and configures
OpenCode with only the remaining cumulative step budget.
MCP startup failures also fail the step, even when OpenCode exits successfully.
Configured MCP startup/tool timeouts are converted from gh-aw seconds to
OpenCode milliseconds: startup also bounds catalog loading, while tool timeout
bounds execution.

`GH_AW_OPENCODE_MODELS` optionally adds a comma-separated roster of model IDs;
entries are trimmed and deduplicated, and the lead model is always included.
All roster models use the same proxy, so the provider policy surface is unchanged.

After a clean run with no safe output, the harness can continue the captured
session up to two times to request the task's safe-output call. It does not
continue nonzero runs, max-turns hits, runs with existing safe output, or runs
without a captured session ID. If the cumulative turn budget is exhausted before
a safe output is produced, the run fails without starting another model step.

Repository `opencode.json`, `opencode.jsonc`, `.opencode/` agents, and `AGENTS.md`
retain native discovery and JSONC parsing. Runtime configuration is supplied
through `OPENCODE_CONFIG_CONTENT`; the harness does not rewrite repository
settings. Supported v1 configuration and file-based definitions continue to load
through OpenCode's compatibility layer. Gateway
MCP servers use OpenCode's remote transport, preserve authentication headers,
disable interactive OAuth, and set `codemode: false` to retain direct gateway
tools. Automatic updates and sharing are disabled. V2 does not run language
servers; use the project's lint, typecheck, or compiler commands instead.
Global configuration, model cache, state, and
session/auth data use isolated `/tmp/opencode-*` directories and are not cached
across runs; pre-existing authentication stores are not loaded.

The pinned CLI is [`@opencode/cli@2.0.26`](https://github.com/anomalyco/opencode/tree/v2.0.26)
(npm publication October 8, 2026, 19:43:12 UTC), outside the three-day npm
cooldown and the newest version available from the registry used for validation.
The [upstream changelog](https://opencode.ai/changelog) lists newer releases;
the v1 `opencode-ai` npm package and GitHub's v1 release channel do not install v2.
See the [v1 migration guide](https://opencode.ai/v2/docs/migrate-v1/).
Runtime configuration uses native v2 `providers`, `agents`, `permissions`, and
`mcp.servers`. Provider restrictions use `experimental.policies`, the policy
location supported by this pin (also accepted by later v2 releases).
Installation runs the pinned package's lifecycle script to link its native
binary. The native provider runtimes are bundled; model catalog fetching is disabled.
Custom models without explicit limits retain OpenCode's unknown-limit defaults;
set `providers.awf-proxy.models.<model>.limit` in native repository configuration
when authoritative context/output limits are available.

Actions summaries, unified session artifacts, and local session reconstruction
share the JSONL parser. Text, reasoning, correlated tool calls/results, structured
errors, timestamps, per-step cost, and input/output/cache token usage are retained.
The v2 CLI can omit step events or their usage while reconciling completed parts.
After a successful run, the harness captures `session export --standalone` with a
60-second/64-MiB bound and emits only safe completed-step and settled compaction
accounting, retaining observed part identities so usage is merged without extra
turns. It never emits the export's user messages or provider state. Export
failures retain their cause and report only safe exit-code/signal diagnostics
rather than captured output. Native maximum-step tool rejections also fail even
when the CLI returns zero. Both run and export discard inherited `PWD` so the
configured working directory controls native repository discovery.
The parser accepts v1 and v2 tool identities and exit metadata so older session
artifacts remain readable. Native JavaScript/npm plugins are not gh-aw Agent
Plugins and are not advertised as supported by this sample. Existing v1 plugins
must be ported to OpenCode's new plugin API; moving their files is insufficient.
-->
