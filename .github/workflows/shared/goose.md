---
engine:
  id: goose
  detection-engine: copilot
  version: "1.53.0"
  display-name: Goose
  description: Goose CLI with headless execution and MCP support
  experimental: true
  provider:
    name: github
  behaviors:
    secret-strategy: universal-llm-consumer
    capabilities:
      max-turns: true
      tools-allowlist: true
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
        GOOSE_DISABLE_KEYRING: "1"
    mcp:
      config-path: .goose/mcp.json
      config-adapter: |
        const path = require("path");
        const { loadGatewayContext, filterAndTransformServers, normalizeGatewayEntry, writeSecureOutput } = require("./convert_gateway_config_shared.cjs");
        const context = loadGatewayContext({ extraRequiredEnv: ["GITHUB_WORKSPACE"] });
        // The harness executes inside AWF, so use the container-facing gateway domain.
        const servers = filterAndTransformServers(context.servers, context.cliServers, (name, entry) => {
          if (typeof entry.url !== "string" && typeof entry.command !== "string") {
            throw new Error(`Goose MCP server ${name} requires a URL or command`);
          }
          return normalizeGatewayEntry(entry, context.urlPrefix, transformed => {
            transformed.type = typeof entry.url === "string" ? "streamable_http" : "stdio";
          });
        });
        writeSecureOutput(path.join(context.extraEnv.GITHUB_WORKSPACE, ".goose", "mcp.json"), JSON.stringify({ mcpServers: servers }, null, 2));
    harness-script: |
      const { createHash } = require("crypto");
      const { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } = require("fs");
      const { tmpdir } = require("os");
      const { delimiter, join } = require("path");
      const { spawnSync } = require("child_process");
      const { fetchAWFReflect, resolveOpenAICompatibleEndpointFromReflect } = require("./awf_reflect.cjs");

      const [command, ...commandArgs] = process.argv.slice(2);
      const installDir = mkdtempSync(join(tmpdir(), "goose-"));
      const archive = join(installDir, "goose.tar.gz");
      const checksums = {
        "1.53.0": "deb2191a6b75acc0a20232fc5c52655ea2f9cc8fa2f5dffc8622e8d378a915dc",
      };
      const fail = (result, action) => {
        if (result.error) throw result.error;
        if (result.status !== 0) {
          const error = new Error(`${action} failed with ${result.signal ? `signal ${result.signal}` : `exit code ${result.status ?? "unknown"}`}`);
          error.exitCode = result.status || 1;
          throw error;
        }
      };
      const slugify = (value) =>
        String(value)
          .toLowerCase()
          .replace(/[^a-z0-9_-]+/g, "_");
      const log = (message) => process.stderr.write(`[goose-harness] ${message}\n`);

      const main = async () => {
        try {
        const version = process.env.GH_AW_ENGINE_VERSION;
        if (!version) throw new Error("GH_AW_ENGINE_VERSION is required");
        let binary = command;
        if (command === "goose") {
        const checksum = checksums[version];
        if (!checksum) throw new Error(`No verified Goose archive for version ${version}; update the shared definition's checksum or set engine.command to a verified binary`);
        const releaseURL = `https://github.com/aaif-goose/goose/releases/download/v${version}/goose-x86_64-unknown-linux-gnu.tar.gz`;
        fail(spawnSync("curl", ["--fail", "--location", "--silent", "--show-error", "--output", archive, releaseURL], { stdio: "inherit" }), "Goose download");
        if (createHash("sha256").update(readFileSync(archive)).digest("hex") !== checksum) {
          throw new Error("Goose download checksum did not match");
        }
        fail(spawnSync("tar", ["-xzf", archive, "-C", installDir], { stdio: "inherit" }), "Goose extraction");
        binary = join(installDir, "goose");
        }
        const verification = spawnSync(binary, ["--version"], { encoding: "utf8" });
        fail(verification, "Goose version verification");
        if (verification.stdout.trim() !== version) throw new Error(`Goose binary version does not match GH_AW_ENGINE_VERSION (${version})`);
        log(`verified Goose ${version}`);

        const config = JSON.parse(readFileSync(process.env.GH_AW_MCP_CONFIG, "utf8"));
        const mcpServers = config.mcpServers;
        if (!mcpServers || typeof mcpServers !== "object" || Array.isArray(mcpServers)) throw new Error("Goose MCP configuration requires an mcpServers object");

        // Native config preserves names, stdio arguments, and HTTP authorization
        // without putting credentials in CLI flags.
        const env = { ...process.env };
        env.GOOSE_PROVIDER = "openai";
        env.GOOSE_PATH_ROOT = join(installDir, "runtime");
        env.GOOSE_DISABLE_KEYRING = "1";
        env.PATH = [installDir, env.PATH].filter(Boolean).join(delimiter);
        env.GOOSE_MODEL = env.GOOSE_MODEL?.replace(/^[^/]+\//, "");
        if (!env.GOOSE_MODEL) throw new Error("GOOSE_MODEL is required");
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
        const extensions = {};
        for (const [name, server] of Object.entries(mcpServers)) {
          const key = slugify(name);
          if (!key || Object.hasOwn(extensions, key)) throw new Error(`Goose MCP extension name collision: ${name}`);
          const common = { enabled: true, name, timeout: 300, available_tools: server.tools?.includes("*") ? [] : server.tools || [] };
          if (typeof server.url === "string") {
            extensions[key] = { ...common, type: "streamable_http", uri: server.url, headers: server.headers || {} };
          } else if (typeof server.command === "string") {
            extensions[key] = { ...common, type: "stdio", cmd: server.command, args: server.args || [], envs: server.env || {} };
          } else {
            throw new Error(`Goose MCP server ${name} requires a URL or command`);
          }
        }
        const extensionsConfigFile = join(installDir, "goose-mcp-extensions.json");
        writeFileSync(extensionsConfigFile, JSON.stringify({ extensions }, null, 2), { mode: 0o600 });
        env.GOOSE_ADDITIONAL_CONFIG_FILES = extensionsConfigFile;

        const promptPath = process.env.GH_AW_PROMPT;
        if (!promptPath || !existsSync(promptPath)) throw new Error("GH_AW_PROMPT must point to an existing prompt file");
        const args = [...commandArgs, "run", "--no-session", "--with-builtin", "developer", "--output-format", "stream-json"];
        if (env.GH_AW_MAX_TURNS) {
          if (!/^[1-9][0-9]*$/.test(env.GH_AW_MAX_TURNS) || Number(env.GH_AW_MAX_TURNS) > 4294967295) throw new Error("GH_AW_MAX_TURNS must be a positive 32-bit integer");
          args.push("--max-turns", env.GH_AW_MAX_TURNS);
        }
        args.push("--instructions", promptPath);
        fail(spawnSync(binary, args, { stdio: "inherit", env }), "Goose execution");
        } finally {
          if (existsSync(installDir)) rmSync(installDir, { recursive: true, force: true });
        }
      };

      main().catch((error) => {
        log(error instanceof Error ? error.message : String(error));
        process.exitCode = error.exitCode || 1;
      });
    log-parser: |
      function parseLog(logContent) {
        return require("./parse_goose_log.cjs").parseGooseLog(logContent);
      }
---

<!--
# Goose CLI

Shared engine definition for the [Goose](https://github.com/aaif-goose/goose)
open-source AI agent. Import this file and set `engine: id: goose` to use it.

This is an unsupported repository sample, pinned to
[v1.53.0](https://github.com/aaif-goose/goose/releases/tag/v1.53.0).
The Linux x86-64 archive is SHA-256 verified before extraction; other versions
require a reviewed checksum or a verified binary supplied through `engine.command`.

The harness runs inside AWF, discovers the selected OpenAI-compatible endpoint
through `/reflect`, and passes `OPENAI_HOST` and the complete `OPENAI_BASE_PATH`
to Goose's `openai` provider. Workflow models use `provider/model`; only the
first provider prefix is removed, preserving slashes in the model ID.
`permissions: { copilot-requests: write }` uses the workflow's GitHub token for
Copilot inference without a PAT or `COPILOT_GITHUB_TOKEN` secret.

Execution uses `goose run --no-session --with-builtin developer --output-format
stream-json --instructions <prompt-file>` and forwards `GH_AW_MAX_TURNS` to
`--max-turns`. Native streamable HTTP extensions retain gateway authorization
headers; stdio extensions retain names, argument arrays, and environment values.
Configuration and session state are isolated in a temporary `GOOSE_PATH_ROOT`,
and keyring access is disabled for unattended execution. Goose's CLI HTTP
extension flag cannot carry headers, so the harness writes an owner-only native
config overlay instead. Native JSONL events provide tool calls and token usage.
Goose has no gh-aw-native `web-fetch` tool; workflows must supply an MCP tool
or explicitly use their shell capability for web access.
-->
