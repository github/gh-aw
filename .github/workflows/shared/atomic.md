---
excluded-env:
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
  id: atomic
  detection-engine: copilot
  version: "0.9.79"
  display-name: Atomic
  description: Atomic CLI with sandbox-proxied multi-provider inference and native MCP
  experimental: true
  provider:
    name: github
  behaviors:
    secret-strategy: universal-llm-consumer
    manifest:
      files:
        - AGENTS.md
      path-prefixes:
        - .atomic/
        - .pi/
    network:
      provider-domains:
        copilot: api.githubcopilot.com
        anthropic: api.anthropic.com
        openai: api.openai.com
        codex: api.openai.com
    installation:
      package-manager: npm
      package-name: "@bastani/atomic"
      binary-name: atomic
      step-name: Install Atomic CLI
      include-node-setup: true
      verify-command: atomic --version
      verify-step-name: Verify Atomic CLI installation
      docs-url: https://github.com/bastani-inc/atomic
    execution:
      command-name: atomic
      args:
        - --print
        - --mode
        - json
        - --no-session
        - --no-themes
        - --approve
        - --exclude-tools
        - ask_user_question
      step-name: Execute Atomic CLI
      model-env-var: ATOMIC_MODEL
      mcp-config-env-var: GH_AW_MCP_CONFIG
      provider-env-mode: universal-llm-consumer
      write-timestamp: true
      env:
        ATOMIC_OFFLINE: "1"
        ATOMIC_TELEMETRY: "0"
        NO_COLOR: "1"
    mcp:
      config-path: /tmp/gh-aw/atomic-mcp.json
      config-adapter: |
        const { loadGatewayContext, filterAndTransformServers, rewriteUrl, writeSecureOutput } = require("./convert_gateway_config_shared.cjs");
        const context = loadGatewayContext();
        const servers = filterAndTransformServers(context.servers, context.cliServers, (name, entry) => {
          if (typeof entry.url !== "string") {
            throw new Error(`Atomic requires a gateway HTTP URL for MCP server ${name}`);
          }
          return {
            type: "http",
            url: rewriteUrl(entry.url, context.urlPrefix),
            exposure: "direct",
            ...(entry.headers ? { headers: entry.headers } : {}),
          };
        });
        writeSecureOutput("/tmp/gh-aw/atomic-mcp.json", JSON.stringify({ autoEnableCodemode: false, mcpServers: servers }, null, 2));
    harness-script: |
      const { existsSync, lstatSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, renameSync, rmSync, statSync, writeFileSync } = require("fs");
      const { join } = require("path");
      const { spawnSync } = require("child_process");
      const { createAtomicOutputSanitizers } = require("./atomic_output_redaction.cjs");
      const { fetchAWFReflect, resolveProviderEndpointFromReflect, deriveBaseUrlFromModelsURL, inferWireApiForModel, normalizeReflectProviderName, REFLECT_PROVIDER_ALIASES } = require("./awf_reflect.cjs");

      const [command, ...args] = process.argv.slice(2);
      const sensitiveValues = new Set();
      const { sanitizeText, sanitizeStdout } = createAtomicOutputSanitizers(() => [...sensitiveValues]);
      const log = message => process.stderr.write(sanitizeText(`[atomic-harness] ${message}\n`));
      // A workflow opts in by declaring tools.drive-memory with id: atomic.
      const sessionDrive = "/tmp/gh-aw/drive-memory-atomic";
      const sessionBudgetBytes = 32 * 1024 * 1024;
      const workflowBudgetBytes = 32 * 1024 * 1024;
      const listSessions = dir => {
        try {
          return readdirSync(dir, { withFileTypes: true })
            .filter(entry => entry.isFile() && /^[A-Za-z0-9][A-Za-z0-9._-]*\.jsonl$/.test(entry.name))
            .map(entry => entry.name).sort();
        } catch (error) {
          if (error.code === "ENOENT") return [];
          throw error;
        }
      };
      const isSession = content => {
        const lines = content.split("\n");
        if (lines.at(-1) === "") lines.pop();
        return lines.length > 0 && lines.every(line => {
          try {
            const record = JSON.parse(line);
            return !!record && typeof record === "object" && !Array.isArray(record) && typeof record.type === "string";
          } catch {
            return false;
          }
        });
      };
      // Workflow runs travel as `atomic storage export` files (version 1): a header line, then one line per event.
      const parseRun = content => {
        if (!isSession(content)) return undefined;
        const lines = content.split("\n");
        if (lines.at(-1) === "") lines.pop();
        const records = lines.map(line => JSON.parse(line));
        return records[0].type === "atomic.workflow-run" && records[0].version === 1 ? records : undefined;
      };
      // Import checks these fields exactly (and resume relies on name, cwd and status); only string content may change.
      const exactRunFields = ({ header, payload, ...exact }) =>
        JSON.stringify([exact, header?.runId, exact.type === "StatusChanged" ? payload?.status : undefined]);
      const isFinishedRun = status => ["completed", "failed", "cancelled"].includes(status);
      const restoreSessions = sessionDir => {
        const saved = join(sessionDrive, "sessions");
        if (!existsSync(saved) || !lstatSync(saved).isDirectory()) return;
        for (const name of listSessions(saved)) {
          const content = readFileSync(join(sessionDrive, "sessions", name), "utf8");
          if (isSession(content)) writeFileSync(join(sessionDir, name), content, { mode: 0o600 });
          else log(`Skipping malformed saved session ${name}`);
        }
      };
      const runStorage = (spawn, storageArgs) => {
        const result = spawn(["storage", ...storageArgs]);
        if (result.status === 0) return true;
        log(`Warning: atomic storage ${storageArgs[0]} failed (${result.error?.message || `exit code ${result.status ?? result.signal}`}): ${sanitizeText(result.stderr).trim().slice(-2000)}`);
        return false;
      };
      // `atomic storage export/import <dir> --workflows` (Atomic 0.9.84+) use <dir>/workflows/<runId>.jsonl.
      const supportsRunTransfer = spawn => {
        const help = spawn(["storage", "export", "--help"]);
        return help.status === 0 && /--workflows\b/.test(help.stdout?.toString("utf8") || "");
      };
      // Restored files passed threat detection when they were saved; import refuses a bad batch all or nothing.
      const restoreRuns = (spawn, transferDir) => {
        const saved = new Map();
        const savedDir = join(sessionDrive, "workflows");
        if (!existsSync(savedDir) || !lstatSync(savedDir).isDirectory()) return saved;
        const runsDir = join(transferDir, "workflows");
        mkdirSync(runsDir, { recursive: true, mode: 0o700 });
        for (const name of listSessions(savedDir)) {
          const content = readFileSync(join(savedDir, name), "utf8");
          const records = parseRun(content);
          if (!records) {
            log(`Skipping malformed saved workflow run ${name}`);
            continue;
          }
          writeFileSync(join(runsDir, name), content, { mode: 0o600 });
          saved.set(name, { name, content, header: records[0] });
        }
        if (saved.size === 0 || runStorage(spawn, ["import", transferDir, "--workflows"])) return saved;
        for (const name of saved.keys()) {
          const single = mkdtempSync(join(transferDir, ".import-"));
          mkdirSync(join(single, "workflows"));
          renameSync(join(runsDir, name), join(single, "workflows", name));
          if (!runStorage(spawn, ["import", single, "--workflows"])) log(`Not restoring workflow run ${name}`);
        }
        return saved;
      };
      // Only redacted exports whose exact fields survived redaction reach the drive. Saved runs the export lacks stay;
      // only the budget drops runs, keeping unfinished runs first, then the newest.
      const persistRuns = (spawn, exportDir, saved) => {
        const { redactArtifactContent } = require("./redact_secrets.cjs");
        if (!runStorage(spawn, ["export", exportDir, "--workflows"])) return;
        const runs = new Map(saved);
        for (const name of listSessions(join(exportDir, "workflows"))) {
          const original = readFileSync(join(exportDir, "workflows", name), "utf8");
          const content = redactArtifactContent(original, [...sensitiveValues], []).content;
          const before = parseRun(original);
          const after = parseRun(content);
          if (!before || !after || after.length !== before.length ||
              after.some((record, index) => exactRunFields(record) !== exactRunFields(before[index]))) {
            log(`Not saving this run's export of ${name}: it is malformed or a secret appears in a field import checks exactly`);
            continue;
          }
          runs.set(name, { name, content, header: after[0] });
        }
        const ordered = [...runs.values()].sort((a, b) =>
          Number(isFinishedRun(a.header.status)) - Number(isFinishedRun(b.header.status)) ||
          b.header.createdAt - a.header.createdAt || a.name.localeCompare(b.name));
        const staging = mkdtempSync(join(sessionDrive, ".workflows-"));
        try {
          let bytes = 0;
          for (const { name, content } of ordered) {
            bytes += Buffer.byteLength(content);
            if (bytes > workflowBudgetBytes) {
              log(`Not saving workflow run ${name} or older finished runs: saved workflow runs are limited to ${workflowBudgetBytes} bytes`);
              break;
            }
            writeFileSync(join(staging, name), content, { mode: 0o600 });
          }
          rmSync(join(sessionDrive, "workflows"), { recursive: true, force: true });
          renameSync(staging, join(sessionDrive, "workflows"));
        } finally {
          rmSync(staging, { recursive: true, force: true });
        }
      };
      // Persistence problems are reported, never turned into agent failures.
      const tryPersist = (what, action) => {
        try {
          action();
        } catch (error) {
          log(`Warning: keeping Atomic ${what} on the drive failed: ${error instanceof Error ? error.message : String(error)}`);
        }
      };
      // Only redacted, well-formed transcripts reach the drive; newest names win the size budget.
      const persistSessions = sessionDir => {
        const { redactArtifactContent } = require("./redact_secrets.cjs");
        const staging = mkdtempSync(join(sessionDrive, ".sessions-"));
        try {
          let bytes = 0;
          for (const name of listSessions(sessionDir).reverse()) {
            const content = redactArtifactContent(readFileSync(join(sessionDir, name), "utf8"), [...sensitiveValues], []).content;
            if (!isSession(content)) {
              log(`Not saving malformed session ${name}`);
              continue;
            }
            bytes += Buffer.byteLength(content);
            if (bytes > sessionBudgetBytes) {
              log(`Not saving ${name} or older sessions: saved sessions are limited to ${sessionBudgetBytes} bytes`);
              break;
            }
            writeFileSync(join(staging, name), content, { mode: 0o600 });
          }
          rmSync(join(sessionDrive, "sessions"), { recursive: true, force: true });
          renameSync(staging, join(sessionDrive, "sessions"));
        } finally {
          rmSync(staging, { recursive: true, force: true });
        }
      };
      const main = async () => {
        if (!command) throw new Error("Atomic command is required");
        if (process.env.AWF_REFLECT_ENABLED !== "1") {
          throw new Error("Atomic requires the AWF API proxy; direct credential authentication is not supported");
        }
        const selectedModel = process.env.ATOMIC_MODEL;
        const separator = selectedModel?.indexOf("/") ?? -1;
        if (separator <= 0 || !selectedModel.slice(separator + 1).trim()) {
          throw new Error("ATOMIC_MODEL must use provider/model format");
        }
        const model = selectedModel.slice(separator + 1).trim();
        const provider = process.env.GH_AW_LLM_PROVIDER;
        if (!["github", "anthropic", "openai"].includes(provider)) {
          throw new Error("GH_AW_LLM_PROVIDER must be github, anthropic, or openai");
        }
        const promptPath = process.env.GH_AW_PROMPT;
        if (!promptPath) throw new Error("GH_AW_PROMPT is required");
        const prompt = readFileSync(promptPath, "utf8");
        if (!prompt.trim()) throw new Error("GH_AW_PROMPT must contain a non-empty instruction");
        const reflected = await fetchAWFReflect({ logger: log });
        if (!reflected.ok || !reflected.reflectData) throw new Error("Unable to discover the Atomic LLM endpoint from /reflect");
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
        let baseUrl = deriveBaseUrlFromModelsURL(reflectedEndpoint.models_url);
        const api = provider === "anthropic"
          ? "anthropic-messages"
          : inferWireApiForModel("openai", model) === "responses" ? "openai-responses" : "openai-completions";
        // The Anthropic SDK appends /v1/messages itself.
        if (provider === "anthropic") baseUrl = baseUrl.replace(/\/+$/, "").replace(/\/v1$/, "");
        const mcpPath = process.env.GH_AW_MCP_CONFIG;
        const mcp = mcpPath ? JSON.parse(readFileSync(mcpPath, "utf8")) : { mcpServers: {} };
        if (!mcp || !mcp.mcpServers || typeof mcp.mcpServers !== "object" || Array.isArray(mcp.mcpServers)) {
          throw new Error("Atomic MCP configuration must contain an mcpServers object");
        }
        for (const server of Object.values(mcp.mcpServers)) {
          for (const [name, value] of Object.entries(server?.headers || {})) {
            if (typeof value !== "string" || !value.trim()) continue;
            sensitiveValues.add(value);
            sensitiveValues.add(value.trim());
            if (name.toLowerCase() === "authorization") {
              const bearer = /^Bearer\s+(.+)$/i.exec(value.trim());
              if (bearer) sensitiveValues.add(bearer[1].trim());
            }
          }
        }
        mkdirSync("/tmp/gh-aw/agent", { recursive: true });
        const agentDir = mkdtempSync("/tmp/gh-aw/agent/atomic-");
        let sessionDir;
        let persistWorkflowRuns;
        try {
          if (existsSync(sessionDrive) && statSync(sessionDrive).isDirectory()) {
            sessionDir = join(agentDir, "sessions");
            mkdirSync(sessionDir, { mode: 0o700 });
            tryPersist("sessions", () => restoreSessions(sessionDir));
          }
          const runArgs = sessionDir ? [...args.filter(arg => arg !== "--no-session"), "--session-dir", sessionDir] : args;
          const writeConfig = (name, value) => writeFileSync(join(agentDir, name), JSON.stringify(value), { mode: 0o600 });
          writeConfig("models.json", {
            providers: {
              "awf-proxy": { baseUrl, api, apiKey: "awf-proxy", models: [{ id: model }] },
            },
          });
          writeConfig("settings.json", { quietStartup: true, enableAnalytics: false, enableInstallTelemetry: false });
          writeConfig("mcp.json", mcp);
          // Inherit runtime bindings, never arbitrary workflow secrets or execution hooks.
          const env = {
            ATOMIC_CODING_AGENT_DIR: agentDir,
            PI_CODING_AGENT_DIR: agentDir,
            ATOMIC_OFFLINE: "1",
            ATOMIC_TELEMETRY: "0",
          };
          const conformanceSentinel = process.env.ENGINE_CONFORMANCE_SENTINEL;
          if (conformanceSentinel) {
            if (conformanceSentinel !== "conformance-atomic") {
              throw new Error("ENGINE_CONFORMANCE_SENTINEL must be conformance-atomic when set");
            }
            env.ENGINE_CONFORMANCE_SENTINEL = "conformance-atomic";
          }
          for (const name of [
            "PATH", "HOME", "TMPDIR", "TMP", "TEMP", "LANG", "LC_ALL", "LC_CTYPE",
            "TZ", "TERM", "NO_COLOR", "CI", "HOSTALIASES",
            "GITHUB_WORKSPACE", "GH_AW_ENGINE_CWD", "GH_AW_ENGINE_VERSION",
            "GH_AW_MCP_CONFIG", "GH_AW_TOOL_TIMEOUT", "GH_AW_SAFE_OUTPUTS",
            "GH_AW_SAFE_OUTPUTS_CONFIG_PATH", "GH_AW_SAFE_OUTPUTS_TOOLS_PATH",
            "NO_PROXY", "no_proxy",
          ]) {
            if (process.env[name] !== undefined) env[name] = process.env[name];
          }
          for (const name of ["HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"]) {
            const value = process.env[name];
            if (!value) continue;
            let proxy;
            try { proxy = new URL(value); } catch { throw new Error(`${name} must be a credential-free HTTP proxy URL`); }
            if (!["http:", "https:"].includes(proxy.protocol) || proxy.username || proxy.password ||
                proxy.pathname !== "/" || proxy.search || proxy.hash) {
              throw new Error(`${name} must be a credential-free HTTP proxy URL`);
            }
            env[name] = value;
          }
          const cwd = process.env.GH_AW_ENGINE_CWD || process.env.GITHUB_WORKSPACE;
          if (sessionDir) {
            const spawn = storageArgs => spawnSync(command, storageArgs, {
              cwd, env, stdio: ["ignore", "pipe", "pipe"], maxBuffer: 16 * 1024 * 1024, timeout: 5 * 60 * 1000,
            });
            tryPersist("workflow runs", () => {
              if (!supportsRunTransfer(spawn)) {
                log("This Atomic cannot transfer workflow runs (0.9.84 or newer can); saving sessions only");
                return;
              }
              // A private journal file, so import, the run and export all use the same journal.
              env.ATOMIC_WORKFLOW_JOURNAL_PATH = join(agentDir, "workflows", "journal.sqlite");
              const transferDir = join(agentDir, "workflow-import");
              mkdirSync(transferDir, { mode: 0o700 });
              const saved = restoreRuns(spawn, transferDir);
              persistWorkflowRuns = () => {
                const exportDir = join(agentDir, "workflow-export");
                mkdirSync(exportDir, { mode: 0o700 });
                persistRuns(spawn, exportDir, saved);
              };
            });
          }
          const result = spawnSync(command, [...runArgs, "--provider", "awf-proxy", "--model", model], {
            cwd,
            env,
            input: prompt,
            stdio: ["pipe", "pipe", "pipe"],
            maxBuffer: 64 * 1024 * 1024,
          });
          // A bounded capture failure may leave a credential split at the buffer boundary.
          if (result.error?.code === "ENOBUFS") {
            throw new Error("Atomic output exceeded the 64 MiB capture limit; incomplete output withheld");
          }
          const stdout = sanitizeStdout(result.stdout);
          const stderr = sanitizeText(result.stderr);
          process.stdout.write(stdout);
          process.stderr.write(stderr);
          if (result.error) throw result.error;
          if (result.status !== 0) {
            log(result.signal ? `Atomic terminated by signal ${result.signal}` : `Atomic exited with code ${result.status ?? "unknown"}`);
            process.exitCode = result.status || 1;
          }
        } finally {
          try {
            if (sessionDir) tryPersist("sessions", () => persistSessions(sessionDir));
            if (persistWorkflowRuns) tryPersist("workflow runs", persistWorkflowRuns);
          } finally {
            rmSync(agentDir, { recursive: true, force: true });
          }
        }
      };
      main().catch(error => {
        try {
          log(error instanceof Error ? error.message : String(error));
        } catch {
          process.stderr.write("[atomic-harness] Atomic output redaction failed; output withheld\n");
        }
        process.exitCode = 1;
      });
    log-parser: |
      function parseLog(logContent) {
        return require("./parse_pi_log.cjs").parseAtomicLog(logContent);
      }
---

<!--
# Atomic

Unsupported experimental sample for [Atomic](https://github.com/bastani-inc/atomic).
Import this definition and select a concrete provider/model ID:

```yaml
engine:
  id: atomic
model: copilot/gpt-5.3-codex
imports:
  - shared/atomic.md
```

The sample pins `@bastani/atomic@0.9.79` (released November 12, 2026), with
npm lifecycle scripts disabled and Node.js 24. Copilot uses
`copilot-requests: write` or `COPILOT_GITHUB_TOKEN`; Anthropic uses
`ANTHROPIC_API_KEY`; OpenAI and Codex use `OPENAI_API_KEY` or `CODEX_API_KEY`.
The universal provider strategy supplies these credentials to AWF, not Atomic.
Models use `copilot/`, `anthropic/`, `openai/`, or `codex/` prefixes. AWF is
required; direct-key execution is not supported. The harness discovers the
selected endpoint through reflection and configures a private `awf-proxy`
provider with a placeholder key. Anthropic uses Messages; GPT-5 and newer
models use Responses; other models use Chat Completions.

The child environment uses an explicit runtime allowlist: executable paths,
home and temporary directories, locale and terminal settings, workspace paths,
engine version, MCP and safe-output file bindings, tool timeout, host aliases,
and proxy routing. Proxy URLs must be credential-free HTTP(S) addresses.
The reserved `ENGINE_CONFORMANCE_SENTINEL` binding is forwarded only when its
value is exactly `conformance-atomic`; other non-empty values fail before launch.
Arbitrary workflow variables, repository and cloud secrets, future credential
variables, and execution hooks such as `NODE_OPTIONS` are not inherited.
MCP authentication remains in the private configuration, not the environment.
This does not remove secrets already exposed to the harness or stored in files.

Prompts arrive verbatim on stdin, never as command arguments. Each run has an
owner-only temporary agent directory containing `models.json`, `settings.json`,
and `mcp.json`, removed on exit. Native HTTP MCP retains gateway authorization
headers, rewrites container-facing URLs, and exposes tools directly without
Code Mode. CLI-mounted servers are excluded. The gateway credential is visible
to the agent and is not an upstream provider credential or an agent boundary.
The harness captures stdout and stderr and masks exact configured MCP header
values, including bare bearer credentials and JSON-escaped occurrences, before
emitting logs or launch diagnostics. The private MCP configuration stays intact.
Capture is bounded to 64 MiB; overflow fails explicitly without publishing
incomplete output. Logs appear after the process exits rather than streaming.
This known-value masking does not contain the agent or cover arbitrary encodings,
partial credentials, other secrets, or files the agent writes.

Startup network checks, telemetry, and themes are disabled. Sessions are not
saved unless the workflow declares a drive-memory entry with `id: atomic`
(mounted at `/tmp/gh-aw/drive-memory-atomic/`). Then the harness restores
well-formed `sessions/*.jsonl` transcripts from the drive into a private
JSONL session directory, runs Atomic with `--session-dir`, and afterwards
replaces `sessions/` with redacted transcripts (configured MCP header values
and built-in token patterns) whose every line is a JSON object with a string
`type`, newest first, up to 32 MiB. Workflow-stage transcripts are included.
With Atomic 0.9.84 or newer (detected from `atomic storage export --help`
offering `--workflows`), workflow runs are kept too: valid `workflows/*.jsonl`
exports (a version 1 `atomic.workflow-run` header, then JSON object lines) are
imported into a private journal (`ATOMIC_WORKFLOW_JOURNAL_PATH`) with
`atomic storage import <dir> --workflows`. Afterwards `atomic storage export
<dir> --workflows` output is redacted the same way and dropped when redaction
would change a field import checks exactly. Saved runs the export lacks are
kept, and `workflows/` is replaced only after a successful export, with
unfinished runs first and then the newest, up to another 32 MiB. Older Atomic
versions save sessions only and leave `workflows/` alone.
Persistence problems are logged as warnings and never fail the agent step.
Drive memory stages these files and publishes them only after threat
detection accepts the run.
Everything else in Atomic is on, minus its one interactive tool. `--approve`
loads the repository's `.atomic/` extensions, workflows, skills, prompt
templates, and settings, so a repository can add custom tools, workflows, and
skills by committing them there; skills gh-aw installs into `.atomic/skills`
are loaded too. gh-aw restores `.atomic/` from the
base branch before the agent starts, so a pull request cannot supply its own.
The fresh agent directory excludes user-installed extensions; bundled
extensions remain loaded. Every Atomic tool is enabled inside the gh-aw sandbox,
including workflows, subagents, Intercom, web access, native MCP, and tools
registered by the repository's extensions; only the interactive
`ask_user_question` is excluded, because no person answers in a workflow run.
Headless runs return a workflow's run ID at launch, and a durable approval gate
stays pending rather than proceeding. Web tools reach only domains the workflow
allows. JSONL event output feeds the step summary and unified session converter
with session identity, tool calls and results, assistant messages, errors, turn
count, and token usage. This sample does not advertise
gh-aw tool allowlists, native iteration limits, or Agent Plugins. The smoke workflow runs
every two days and on manual dispatch, and checks native MCP, file readback, build
status, and credentials.
-->
