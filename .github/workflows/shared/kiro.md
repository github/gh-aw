---
engine:
  id: kiro
  detection-engine: copilot
  display-name: Kiro
  description: Kiro CLI with headless execution and native MCP support
  experimental: true
  version: "2.27.1"
  provider:
    name: kiro
  auth:
    - role: api-key
      secret: KIRO_API_KEY
  behaviors:
    supported-env-var-keys:
      - KIRO_API_KEY
    plugins:
      directory: .kiro/powers
    manifest:
      files:
        - AGENTS.md
      path-prefixes:
        - .kiro/
    network:
      defaults:
        - host.docker.internal
        - github.com
        - raw.githubusercontent.com
        - api.github.com
        - objects.githubusercontent.com
        - codewhisperer.us-east-1.amazonaws.com
        - cognito-identity.us-east-1.amazonaws.com
        - q.us-east-1.amazonaws.com
        - client-telemetry.us-east-1.amazonaws.com
        - prod.us-east-1.telemetry.kiro.aws.dev
        - prod.assets.shortbread.aws.dev
      provider-domains:
        kiro: "*.kiro.dev"
    execution:
      command-name: kiro-cli
      args:
        - chat
        - --no-interactive
        - --trust-all-tools
        - --require-mcp-startup
      step-name: Execute Kiro CLI
      model-env-var: KIRO_MODEL
      mcp-config-env-var: GH_AW_MCP_CONFIG
      write-timestamp: true
      env:
        KIRO_LOG_NO_COLOR: "1"
        NO_COLOR: "1"
    mcp:
      config-path: .kiro/settings/mcp.json
      config-adapter: |
        const fs = require("fs");
        const path = require("path");
        const log = message => process.stderr.write(`[kiro-mcp] ${message}\n`);

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
        log("Reading MCP gateway configuration");

        let cliServers;
        try {
          cliServers = JSON.parse(process.env.GH_AW_MCP_CLI_SERVERS || "[]");
        } catch {
          throw new Error("GH_AW_MCP_CLI_SERVERS must be a JSON array of server names");
        }
        if (!Array.isArray(cliServers) || !cliServers.every(name => typeof name === "string")) {
          throw new Error("GH_AW_MCP_CLI_SERVERS must be a JSON array of server names");
        }
        cliServers = new Set(cliServers);

        const gatewayContent = fs.readFileSync(gatewayOutputPath, "utf8");
        let gatewayOutput;
        try {
          gatewayOutput = JSON.parse(gatewayContent);
        } catch {
          throw new Error("MCP_GATEWAY_OUTPUT must contain valid JSON; check the MCP gateway logs");
        }
        const servers = gatewayOutput?.mcpServers;
        if (!servers || typeof servers !== "object" || Array.isArray(servers)) {
          throw new Error("MCP_GATEWAY_OUTPUT must contain an mcpServers object");
        }
        const mcpServers = {};
        let skipped = 0;
        let httpServers = 0;

        for (const [name, entry] of Object.entries(servers)) {
          if (cliServers.has(name)) {
            skipped++;
            continue;
          }
          if (!entry || typeof entry !== "object" || Array.isArray(entry)) {
            throw new Error("MCP_GATEWAY_OUTPUT contains an invalid server entry");
          }
          const transformed = { ...entry };
          if (typeof transformed.url === "string") {
            transformed.url = transformed.url.replace(/^http:\/\/[^/]+\/mcp\//, `${gatewayURL}/mcp/`);
            transformed.type = "http";
            httpServers++;
          }
          delete transformed.tools;
          mcpServers[name] = transformed;
        }

        const configPath = path.join(workspace, ".kiro", "settings", "mcp.json");
        fs.mkdirSync(path.dirname(configPath), { recursive: true });
        fs.writeFileSync(configPath, JSON.stringify({ mcpServers }, null, 2), { mode: 0o600 });
        fs.chmodSync(configPath, 0o600);
        log(`Wrote ${Object.keys(mcpServers).length} native MCP server(s), ${httpServers} HTTP server(s), skipped ${skipped} CLI-mounted server(s); permissions=0600`);
    harness-script: |
      const { createHash } = require("crypto");
      const { existsSync, mkdtempSync, readFileSync, rmSync } = require("fs");
      const { tmpdir } = require("os");
      const { join } = require("path");
      const { spawnSync } = require("child_process");

      const [, ...commandArgs] = process.argv.slice(2);
      const installDir = mkdtempSync(join(tmpdir(), "kiro-cli-"));
      const archive = join(installDir, "kiro-cli.tar.gz");
      const version = "2.27.1";
      const started = Date.now();
      const releases = {
        x64: {
          arch: "x86_64",
          checksum: "3c0d7268a4bfb73f8e827822049978fa578e020b271afc1021c7532602d45d99",
        },
        arm64: {
          arch: "aarch64",
          checksum: "33ad5462c3111ba4ef527f1d58bda08a9d1997e0ae73841a7c2ca734ebcba2d0",
        },
      };
      const fail = (result, action) => {
        if (result.error) throw result.error;
        if (result.status !== 0) {
          const detail = result.signal ? `signal ${result.signal}` : `exit code ${result.status ?? "unknown"}`;
          const hint = action === "Kiro CLI execution" && result.status === 3
            ? "; required MCP server startup failed; check .kiro/settings/mcp.json and MCP gateway logs"
            : "";
          const error = new Error(`${action} failed with ${detail}${hint}`);
          error.exitCode = result.status ?? 1;
          throw error;
        }
      };
      const log = message => process.stderr.write(`[kiro-harness] ${message}\n`);
      const run = (command, args, action, options = {}) => {
        const start = Date.now();
        log(`${action} started`);
        fail(spawnSync(command, args, { stdio: "inherit", ...options }), action);
        log(`${action} completed in ${Date.now() - start}ms`);
      };

      try {
        log(`Starting Kiro CLI ${version}; architecture=${process.arch}`);
        if (process.env.GH_AW_ENGINE_VERSION && process.env.GH_AW_ENGINE_VERSION !== version) {
          throw new Error(`This Kiro harness supports only engine.version ${version}; update the pinned release and checksums together`);
        }
        if (!process.env.KIRO_API_KEY && process.env.SECRET_KIRO_API_KEY) {
          process.env.KIRO_API_KEY = process.env.SECRET_KIRO_API_KEY;
          log("Using KIRO_API_KEY from the secret binding");
        }
        if (!process.env.KIRO_API_KEY) throw new Error("KIRO_API_KEY is required for headless execution; configure the KIRO_API_KEY Actions secret");
        log("Headless API key is configured");
        const release = releases[process.arch];
        if (!release) throw new Error(`Unsupported Kiro CLI architecture: ${process.arch}`);

        const promptPath = process.env.GH_AW_PROMPT;
        if (!promptPath) throw new Error("GH_AW_PROMPT is required");
        const prompt = readFileSync(promptPath, "utf8");
        if (!prompt.trim()) throw new Error("GH_AW_PROMPT must contain a non-empty instruction");
        const selectedModel = process.env.KIRO_MODEL;
        if (!selectedModel?.startsWith("kiro/")) {
          throw new Error("KIRO_MODEL must use kiro/model format");
        }
        const model = selectedModel.slice("kiro/".length);
        if (!model.trim()) throw new Error("KIRO_MODEL must include a model name");
        log(`Prompt loaded (${Buffer.byteLength(prompt, "utf8")} bytes); model override is configured`);

        const mcpConfigPath = process.env.GH_AW_MCP_CONFIG;
        if (mcpConfigPath) {
          let config;
          const content = readFileSync(mcpConfigPath, "utf8");
          try {
            config = JSON.parse(content);
          } catch {
            throw new Error("GH_AW_MCP_CONFIG must contain valid JSON; check the Kiro MCP adapter logs");
          }
          if (!config?.mcpServers || typeof config.mcpServers !== "object" || Array.isArray(config.mcpServers)) {
            throw new Error("GH_AW_MCP_CONFIG must contain an mcpServers object");
          }
          log(`Native MCP configuration loaded (${Object.keys(config.mcpServers).length} server(s))`);
        } else {
          log("No GH_AW_MCP_CONFIG path supplied; Kiro will use its default MCP configuration");
        }

        const releaseURL = `https://prod.download.cli.kiro.dev/stable/${version}/kirocli-${release.arch}-linux.tar.gz`;
        run("curl", ["--fail", "--location", "--silent", "--show-error", "--output", archive, releaseURL], "Kiro CLI download");
        if (createHash("sha256").update(readFileSync(archive)).digest("hex") !== release.checksum) {
          throw new Error("Kiro CLI download checksum did not match");
        }
        log("Kiro CLI SHA-256 checksum verified");
        run("tar", ["-xzf", archive, "-C", installDir], "Kiro CLI extraction");

        const binDir = join(installDir, "kirocli", "bin");
        const executable = join(binDir, "kiro-cli");
        if (!existsSync(executable)) throw new Error("Kiro CLI executable was not found in the release archive");
        run(executable, ["--version"], "Kiro CLI verification");

        run(executable, [...commandArgs, "--model", model, "--", prompt], "Kiro CLI execution", {
          cwd: process.env.GITHUB_WORKSPACE,
          env: { ...process.env, PATH: `${binDir}:${process.env.PATH || ""}` },
        });
      } catch (error) {
        log(error instanceof Error ? error.message : String(error));
        process.exitCode = Number.isInteger(error?.exitCode) && error.exitCode > 0 ? error.exitCode : 1;
      } finally {
        rmSync(installDir, { recursive: true, force: true });
        log(`Cleaned up Kiro CLI installation; total duration=${Date.now() - started}ms; exit code=${process.exitCode || 0}`);
      }
---

<!--
# Kiro CLI

Shared engine definition for the [Kiro CLI](https://kiro.dev/). Import this
file and set `engine.id: kiro` to run Kiro in headless mode:

```yaml
engine:
  id: kiro
model: kiro/auto
imports:
  - shared/kiro.md
```

Configure the `KIRO_API_KEY` GitHub Actions secret with an API key from Kiro.
Kiro serves the selected model through its own API, so this engine does not use
universal provider routing.

The harness pins [Kiro CLI 2.27.1](https://kiro.dev/changelog/cli/2-27/) and
verifies the x86_64 or aarch64 Linux archive against the SHA-256 checksum in
the [stable release manifest](https://prod.download.cli.kiro.dev/stable/latest/manifest.json).
Update `engine.version`, the harness version, and both checksums together.
Other `engine.version` overrides are rejected rather than silently installing
a different release.

Headless execution retains `--no-interactive`, `--trust-all-tools`, and
`--require-mcp-startup`; `kiro/<model>` is passed as `--model <model>`.
Native MCP configuration uses `.kiro/settings/mcp.json`, preserves HTTP
authorization headers, and excludes servers already mounted as CLIs.
The config is written with owner-only permissions. MCP startup failures retain
Kiro's exit code 3, with a hint to inspect the gateway logs.

Look for `[kiro-mcp]` and `[kiro-harness]` in the agent logs for MCP server
counts, authentication presence, prompt size, installation timings, checksum
verification, process failures, and cleanup. These diagnostics do not print
API keys, MCP headers, configuration contents, or prompt contents. For Kiro's
own verbose CLI logging, add `--verbose` to `behaviors.execution.args` in this
shared definition when investigating a runtime failure.
-->
