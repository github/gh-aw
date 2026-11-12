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
        - text
        - --no-session
        - --no-skills
        - --no-prompt-templates
        - --no-themes
        - --no-approve
        - --tools
        - read,bash,edit,write,mcp__*
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
      const { mkdirSync, mkdtempSync, readFileSync, writeFileSync, rmSync } = require("fs");
      const { join } = require("path");
      const { spawnSync } = require("child_process");
      const { redactArtifactMaskedValues } = require("./add_mask_redaction.cjs");
      const { fetchAWFReflect, resolveProviderEndpointFromReflect, deriveBaseUrlFromModelsURL, inferWireApiForModel, normalizeReflectProviderName, REFLECT_PROVIDER_ALIASES } = require("./awf_reflect.cjs");

      const [command, ...args] = process.argv.slice(2);
      const sensitiveValues = new Set();
      const sanitize = output => redactArtifactMaskedValues(
        output?.toString("utf8") || "", [...sensitiveValues], undefined, () => "[REDACTED]", { textMode: true }
      );
      const log = message => process.stderr.write(sanitize(`[atomic-harness] ${message}\n`));
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
        try {
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
          const result = spawnSync(command, [...args, "--provider", "awf-proxy", "--model", model], {
            cwd: process.env.GH_AW_ENGINE_CWD || process.env.GITHUB_WORKSPACE,
            env,
            input: prompt,
            stdio: ["pipe", "pipe", "pipe"],
            maxBuffer: 64 * 1024 * 1024,
          });
          // A bounded capture failure may leave a credential split at the buffer boundary.
          if (result.error?.code === "ENOBUFS") {
            throw new Error("Atomic output exceeded the 64 MiB capture limit; incomplete output withheld");
          }
          const stdout = sanitize(result.stdout);
          const stderr = sanitize(result.stderr);
          process.stdout.write(stdout);
          process.stderr.write(stderr);
          if (result.error) throw result.error;
          if (result.status !== 0) {
            log(result.signal ? `Atomic terminated by signal ${result.signal}` : `Atomic exited with code ${result.status ?? "unknown"}`);
            process.exitCode = result.status || 1;
          }
        } finally {
          rmSync(agentDir, { recursive: true, force: true });
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

Startup network checks, telemetry, saved sessions, skills,
prompt templates, and themes are disabled. `--no-approve` ignores project-local
Atomic/Pi configuration; repository settings and workflow definitions are not
overwritten. The fresh agent directory excludes user-installed extensions;
bundled extensions remain loaded so native MCP works. The explicit tool
selection excludes Intercom and unrelated bundled tools. Native read, bash, edit, write, and MCP
tools are enabled inside the gh-aw sandbox. This sample does not advertise
gh-aw tool allowlists, native iteration limits, Agent Plugins, custom Atomic
process workflows, or structured session/token parsing. Plain-text output does
not establish tool, usage, or MCP-failure telemetry. The manually dispatched
smoke workflow checks native MCP, file readback, build status, and credentials.
-->
