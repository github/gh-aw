---
runtimes:
  python:
    version: "3.12"
pre-agent-steps:
  - name: Preinstall Pydantic AI coder agent
    run: |
      python3 -m pip install --quiet --user --disable-pip-version-check "pydantic-ai-harness[cli]==$GH_AW_ENGINE_VERSION" "pydantic-ai-slim[openai,mcp]"
      "$HOME/.local/bin/pai" --version
      python3 -c "from pydantic_ai_harness import Coder"
engine:
  id: pydantic-ai
  detection-engine: copilot
  version: "0.21.0"
  display-name: Pydantic AI
  description: Pydantic AI CLI (pai) running the pydantic-ai-harness coder agent with MCP tool support
  experimental: true
  mcp: true
  provider:
    name: github
  behaviors:
    secret-strategy: universal-llm-consumer
    manifest:
      files:
        - AGENTS.md
      path-prefixes:
        - .pydantic-ai/
    network:
      defaults:
        - host.docker.internal
        - github.com
        - raw.githubusercontent.com
        - api.github.com
        - objects.githubusercontent.com
        - pypi.org
        - files.pythonhosted.org
      provider-domains:
        copilot: api.githubcopilot.com
        anthropic: api.anthropic.com
        openai: api.openai.com
    execution:
      command-name: pai
      step-name: Execute Pydantic AI CLI
      model-env-var: PAI_MODEL
      write-timestamp: true
      provider-env-mode: universal-llm-consumer
    harness-script: |
      const { spawnSync } = require("child_process");
      const { mkdirSync, readFileSync, writeFileSync } = require("fs");
      const { homedir } = require("os");
      const { join } = require("path");
      const { fetchAWFReflect, resolveProviderEndpointFromReflect, deriveBaseUrlFromModelsURL } = require("./awf_reflect.cjs");

      const [command, ...commandArgs] = process.argv.slice(2);
      const log = message => process.stderr.write(`[pydantic-ai] ${message}\n`);

      // `pai -a` takes one target — either a `module:variable` import path or a
      // JSON/YAML agent spec — and the spec format resolves capability names
      // through a closed registry that the harness capabilities are not part of,
      // so "coder tools plus these MCP servers" cannot be expressed as a spec.
      // The agent is composed in Python instead: `Coder()` supplies the
      // filesystem, shell, planning and sub-agent tools, and the gateway's
      // servers arrive as toolsets read from the Claude-shaped `mcp.json` the
      // config adapter writes next to this module. `load_mcp_toolsets` expands
      // `${VAR}` references in that file, which is how header credentials reach
      // the servers; the servers are optional so the module still imports when
      // a workflow configures no MCP tools at all.
      const AGENT_MODULE = `from pathlib import Path

      from pydantic_ai import Agent
      from pydantic_ai.mcp import load_mcp_toolsets
      from pydantic_ai_harness import Coder

      _mcp_config = Path(__file__).with_name("mcp.json")
      agent = Agent(
          name="coder",
          capabilities=[Coder()],
          toolsets=load_mcp_toolsets(_mcp_config) if _mcp_config.exists() else [],
      )
      `;

      const main = async () => {
        const workspace = process.env.GITHUB_WORKSPACE;
        if (!workspace) throw new Error("GITHUB_WORKSPACE is required");
        const promptFile = process.env.GH_AW_PROMPT;
        if (!promptFile) throw new Error("GH_AW_PROMPT is required");

        const agentDir = join(workspace, ".pydantic-ai");
        mkdirSync(agentDir, { recursive: true, mode: 0o700 });
        writeFileSync(join(agentDir, "gh_aw_agent.py"), AGENT_MODULE, { mode: 0o600 });

        const env = { ...process.env };
        // `pip install --user` puts `pai` here. The runner tool cache that holds
        // `uv` and the interpreter's own bin directory is under /opt, which the
        // sandbox exposes read-only, but the home directory is where the CLI and
        // its user site-packages actually live.
        //
        // Which interpreter owns those user site-packages matters: only the one
        // that ran the pre-agent `pip install --user` can import them, and the
        // sandbox prelude prepends every `bin` directory under the runner tool
        // cache — which caches several Python versions — so a bare `python3`
        // there resolves by `find` order rather than to the installing
        // interpreter. `actions/setup-python` names that one in `pythonLocation`;
        // putting its `bin` on PATH also gives the agent's own shell tool a
        // `python3` that can see the installed packages.
        const pythonBin = process.env.pythonLocation ? join(process.env.pythonLocation, "bin") : "";
        const python = pythonBin ? join(pythonBin, "python3") : "python3";
        env.PATH = [join(homedir(), ".local", "bin"), pythonBin, process.env.PATH || ""].filter(Boolean).join(":");
        // `.pydantic-ai` is not a legal package name, so the generated module is
        // reached through PYTHONPATH rather than by importing it as a package
        // from the workspace root. Prepending keeps a caller-supplied
        // PYTHONPATH usable.
        env.PYTHONPATH = process.env.PYTHONPATH ? `${agentDir}:${process.env.PYTHONPATH}` : agentDir;
        delete env.COPILOT_GITHUB_TOKEN;

        const provider = process.env.GH_AW_LLM_PROVIDER;
        if (!provider) throw new Error("GH_AW_LLM_PROVIDER is required");
        let baseUrl = process.env.OPENAI_BASE_URL;
        if (process.env.AWF_REFLECT_ENABLED === "1") {
          const result = await fetchAWFReflect({ logger: log });
          if (!result.ok || !result.reflectData) {
            throw new Error(`Unable to discover the Pydantic AI LLM endpoint from /reflect: ${result.reason || "empty response"}`);
          }
          const endpoint = resolveProviderEndpointFromReflect({
            provider,
            reflectData: result.reflectData,
            logger: log,
          });
          if (!endpoint?.baseUrl) {
            throw new Error(`No configured /reflect endpoint found for provider ${provider}`);
          }
          baseUrl = endpoint.baseUrl;
          const reflectedEndpoint = result.reflectData.endpoints?.find(
            entry => entry?.configured === true && entry.provider === endpoint.endpointProvider
          );
          if (typeof reflectedEndpoint?.models_url === "string") {
            // `endpoint.baseUrl` is the models-listing origin, while the
            // OpenAI-compatible client posts to `<base>/chat/completions`, so the
            // path prefix carried by models_url (`/v1` on some providers) has to
            // come along — and this helper applies the same api-proxy ->
            // host.docker.internal rewrite.
            baseUrl = deriveBaseUrlFromModelsURL(reflectedEndpoint.models_url);
          }
        }
        if (!baseUrl) {
          throw new Error("Pydantic AI requires AWF endpoint discovery or OPENAI_BASE_URL");
        }
        env.OPENAI_BASE_URL = baseUrl;
        // The AWF api-proxy injects the real upstream credentials and ignores the
        // inbound key, but the OpenAI-compatible client refuses to construct
        // itself without one.
        env.OPENAI_API_KEY = "awf-copilot-proxy";

        // `pai` reports a failed `-a` load as a single line naming the target and
        // nothing else, so the module is imported here first: a missing install,
        // an unreadable mcp.json or an unresolvable `${VAR}` in it then surfaces
        // as the real Python traceback instead of "Could not load agent from".
        const preflight = spawnSync(
          python,
          ["-c", "import gh_aw_agent; from pydantic_ai import Agent; assert isinstance(gh_aw_agent.agent, Agent)"],
          { cwd: workspace, env, encoding: "utf8" }
        );
        if (preflight.error) throw preflight.error;
        if (preflight.status !== 0) {
          throw new Error(
            `Could not load the Pydantic AI coder agent with ${python}:\n${preflight.stderr || preflight.stdout || `it exited with code ${preflight.status ?? "unknown"}`}`
          );
        }

        // `pai` sends the model name verbatim, minus the `openai-chat:` provider
        // marker that selects its OpenAI-compatible client, so the bare model ID
        // reaches the api-proxy — which steers to the configured provider by the
        // port it is reached on, not by a prefix in the model name: Copilot
        // rejects `copilot/<model>` with `model_not_supported`. The proxy exposes
        // Copilot Claude models under their dotted IDs, so
        // `copilot/claude-sonnet-4-5` becomes `claude-sonnet-4.5`.
        const model = env.PAI_MODEL?.replace(/^.*\//, "").replace(/^(claude-(?:haiku|sonnet|opus)-\d+)-(\d+)$/, "$1.$2") || "gpt-5";
        // `-m` is always passed: the composed agent carries no model, and without
        // the flag `pai` silently falls back to its own `openai:gpt-5` default,
        // billing a model the workflow never asked for. The fallback here is the
        // same model, made explicit.
        const args = [...commandArgs, "-a", "gh_aw_agent:agent", "-m", `openai-chat:${model}`, readFileSync(promptFile, "utf8")];
        log(`provider=${provider} model=${model} baseUrl=${baseUrl}`);
        const result = spawnSync(command, args, { cwd: workspace, env, stdio: "inherit" });
        if (result.error) throw result.error;
        if (result.status !== 0) {
          const error = new Error(`Pydantic AI execution failed with exit code ${result.status ?? "unknown"}`);
          // Surface the child's own status so the step fails with the same code.
          error.exitCode = typeof result.status === "number" && result.status !== 0 ? result.status : 1;
          throw error;
        }
      };

      main().catch(error => {
        log(error instanceof Error ? error.message : String(error));
        process.exitCode = typeof error?.exitCode === "number" && error.exitCode !== 0 ? error.exitCode : 1;
      });
    mcp:
      config-path: .pydantic-ai/mcp.json
      config-adapter: |
        // Renders the MCP gateway's configuration as the Claude-style
        // `mcpServers` document that `pydantic_ai.mcp.load_mcp_toolsets` reads,
        // written next to the generated agent module so the module can find it by
        // name. Only HTTP entries are carried: `load_mcp_toolsets` can host stdio
        // servers too, but the gateway already fronts every configured server
        // over HTTP, and CLI-mounted servers are excluded because the agent
        // reaches those as executables on PATH instead.
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
          if (typeof entry.url !== "string") {
            console.log(`Skipping MCP server ${name}: the Pydantic AI engine only supports HTTP MCP servers`);
            continue;
          }
          const server = { url: entry.url.replace(/^http:\/\/[^/]+\/mcp\//, `${gatewayURL}/mcp/`) };
          if (entry.headers && typeof entry.headers === "object") server.headers = entry.headers;
          mcpServers[name] = server;
        }

        const configPath = path.join(workspace, ".pydantic-ai", "mcp.json");
        fs.mkdirSync(path.dirname(configPath), { recursive: true, mode: 0o700 });
        fs.writeFileSync(configPath, JSON.stringify({ mcpServers }, null, 2), { mode: 0o600 });
        fs.chmodSync(configPath, 0o600);
        console.log(`Wrote ${Object.keys(mcpServers).length} MCP server(s) to ${configPath}`);
    log-parser: |
      function parseLog(logContent) {
        const lines = logContent.split("\n");
        const logEntries = [];
        const mcpFailures = [];
        let maxTurnsHit = false;
        const AWF_INFRA_RE = /^\[(INFO|WARN|SUCCESS|ERROR|entrypoint|health-check)\]|^ (?:Container|Network|Volume) |^Process exiting with code:/;
        let inputTokens = 0;
        let outputTokens = 0;
        let toolCallIndex = 0;
        let turnCount = 0;
        let pendingText = [];

        function flushText() {
          if (pendingText.length === 0) return;
          const text = pendingText.join("\n").trim();
          if (text) {
            logEntries.push({ type: "assistant", message: { content: [{ type: "text", text }] } });
            turnCount++;
          }
          pendingText = [];
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

          let parsed = null;
          try {
            if (line.trim().startsWith("{")) parsed = JSON.parse(line.trim());
          } catch (e) { /* not JSON */ }

          if (parsed) {
            if (parsed.input_tokens) inputTokens += parsed.input_tokens;
            if (parsed.output_tokens) outputTokens += parsed.output_tokens;
            const entryType = parsed.type != null ? String(parsed.type) : "log";
            const msg = parsed.msg || parsed.message || parsed.content || "";

            if (/tool[._]call|tool[._]use/i.test(entryType)) {
              flushText();
              const toolId = `pai_tool_${toolCallIndex++}`;
              const toolName = parsed.tool || parsed.name || entryType;
              logEntries.push({ type: "assistant", message: { content: [{ type: "tool_use", id: toolId, name: toolName, input: {} }] } });
              logEntries.push({ type: "user", message: { content: [{ type: "tool_result", tool_use_id: toolId, content: msg }] } });
            } else if (msg) {
              pendingText.push(msg);
            }
          } else {
            pendingText.push(line.trim());
          }
        }
        flushText();

        const usage = {};
        if (inputTokens) usage.input_tokens = inputTokens;
        if (outputTokens) usage.output_tokens = outputTokens;
        logEntries.push({ type: "result", num_turns: turnCount, usage });
        const parts = [`**Turns:** ${turnCount}`, `**Tool calls:** ${toolCallIndex}`];
        if (inputTokens || outputTokens) parts.push(`**Tokens:** ${((inputTokens ?? 0) + (outputTokens ?? 0)).toLocaleString()}`);
        if (mcpFailures.length) parts.push(`**MCP failures:** ${mcpFailures.length}`);
        if (maxTurnsHit) parts.push("**Max turns reached**");
        return { markdown: parts.join(" · "), logEntries, mcpFailures, maxTurnsHit };
      }
---

<!--
# Pydantic AI

Shared engine definition for the [Pydantic AI](https://ai.pydantic.dev) CLI
(`pai`), running the coder agent from
[pydantic-ai-harness](https://github.com/pydantic/pydantic-ai-harness). Import
this file and set `engine: id: pydantic-ai` to use it:

```yaml
engine:
  id: pydantic-ai
model: copilot/claude-sonnet-4-5
imports:
  - shared/pydantic.md
```

The agent is a `pydantic_ai.Agent` composed from the harness `Coder`
capability — filesystem, shell, planning, repository context and an explorer
sub-agent, with the harness's own context-management guardrails — plus one
toolset per MCP server the gateway exposes. `pai -a` accepts a single target and
its JSON agent-spec format cannot name harness capabilities, so the harness
script writes that composition as a Python module at
`.pydantic-ai/gh_aw_agent.py`, puts the directory on `PYTHONPATH`, and always
passes `-a gh_aw_agent:agent`. Because `pai` reduces a failed load to a single
line naming the target, the harness imports the module itself first and fails
the step with the underlying Python traceback.

MCP servers are rendered into `.pydantic-ai/mcp.json` in the same `mcpServers`
shape Claude Desktop and Cursor use, which `pydantic_ai.mcp.load_mcp_toolsets`
reads (including `${VAR}` expansion of header values). Tools are prefixed with
their server name, so safe outputs are reachable as `safeoutputs_create_issue`
and the like. Only HTTP servers are carried over; CLI-mounted servers stay
available to the agent's shell as executables on `PATH`.

`model` must use `provider/model` format. Supported providers are `copilot`,
`anthropic`, and `openai`. Requests are routed through the AWF api-proxy, whose
endpoint is discovered from `/reflect` at run time, so the provider segment is
dropped and the bare model ID is passed with `-m openai-chat:<model>`
(`openai-chat:` selects the Pydantic AI OpenAI-compatible client and is not part
of the model name sent upstream). Copilot Claude aliases such as
`claude-sonnet-4-5` are normalized to the dotted model IDs exposed by the proxy,
such as `claude-sonnet-4.5`. `-m` is always passed, because a workflow that
declares no model would otherwise inherit the CLI's own `openai:gpt-5` default
silently.

Responses are streamed. The proxy's aggregated non-streaming body omits
`object` and `choices[].index`, which Pydantic AI rejects during response
validation, so `--no-stream` is deliberately not passed.

`pai` renders its output as Markdown for a terminal and has no structured
output mode, so the log parser reconstructs turns from that text and reads token
counts only from any JSON lines the run happens to emit. Structured output is
tracked upstream in
[pydantic/pydantic-ai#1374](https://github.com/pydantic/pydantic-ai/pull/1374).

The CLI and the coder capabilities are installed before the agent runs with
`pip install --user "pydantic-ai-harness[cli]==<engine version>"
"pydantic-ai-slim[openai,mcp]"`, into `~/.local` because the runner tool cache
holding `uv` is not writable from inside the sandbox.
-->
