---
engine:
  id: kiro
  detection-engine: copilot
  display-name: Kiro
  description: Kiro CLI with headless execution and native MCP support
  experimental: true
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
            transformed.type = "http";
          }
          delete transformed.tools;
          mcpServers[name] = transformed;
        }

        const configPath = path.join(workspace, ".kiro", "settings", "mcp.json");
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
      const installDir = mkdtempSync(join(tmpdir(), "kiro-cli-"));
      const archive = join(installDir, "kiro-cli.tar.gz");
      const version = "2.16.1";
      const releases = {
        x64: {
          arch: "x86_64",
          checksum: "393633c991faab5ef688b2aa0dd420481cab7fca312cd863923c3287e9874b82",
        },
        arm64: {
          arch: "aarch64",
          checksum: "8413b62072780747374c0932147022e290725e934d461e430b5ae12a73f33b23",
        },
      };
      const fail = (result, action) => {
        if (result.error) throw result.error;
        if (result.status !== 0) throw new Error(`${action} failed with exit code ${result.status ?? "unknown"}`);
      };
      const log = message => process.stderr.write(`[kiro-harness] ${message}\n`);

      try {
        if (!process.env.KIRO_API_KEY && process.env.SECRET_KIRO_API_KEY) {
          process.env.KIRO_API_KEY = process.env.SECRET_KIRO_API_KEY;
        }
        const release = releases[process.arch];
        if (!release) throw new Error(`Unsupported Kiro CLI architecture: ${process.arch}`);
        const releaseURL = `https://prod.download.cli.kiro.dev/stable/${version}/kirocli-${release.arch}-linux.tar.gz`;

        fail(spawnSync("curl", ["--fail", "--location", "--silent", "--show-error", "--output", archive, releaseURL], { stdio: "inherit" }), "Kiro CLI download");
        if (createHash("sha256").update(readFileSync(archive)).digest("hex") !== release.checksum) {
          throw new Error("Kiro CLI download checksum did not match");
        }
        fail(spawnSync("tar", ["-xzf", archive, "-C", installDir], { stdio: "inherit" }), "Kiro CLI extraction");

        const binDir = join(installDir, "kirocli", "bin");
        const executable = join(binDir, "kiro-cli");
        if (!existsSync(executable)) throw new Error("Kiro CLI executable was not found in the release archive");
        fail(spawnSync(executable, ["--version"], { stdio: "inherit" }), "Kiro CLI verification");

        const promptPath = process.env.GH_AW_PROMPT;
        if (!promptPath) throw new Error("GH_AW_PROMPT is required");
        const prompt = readFileSync(promptPath, "utf8");
        const selectedModel = process.env.KIRO_MODEL;
        if (!selectedModel?.startsWith("kiro/")) {
          throw new Error("KIRO_MODEL must use kiro/model format");
        }
        const model = selectedModel.slice("kiro/".length);
        if (!model) throw new Error("KIRO_MODEL must include a model name");

        fail(spawnSync(executable, [...commandArgs, "--model", model, prompt], {
          cwd: process.env.GITHUB_WORKSPACE,
          env: { ...process.env, PATH: `${binDir}:${process.env.PATH || ""}` },
          stdio: "inherit",
        }), "Kiro CLI execution");
      } catch (error) {
        log(error instanceof Error ? error.message : String(error));
        process.exitCode = 1;
      } finally {
        rmSync(installDir, { recursive: true, force: true });
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
-->
