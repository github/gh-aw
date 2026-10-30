---
engine:
  id: factory
  detection-engine: copilot
  display-name: Factory
  description: Factory Droid CLI with headless execution and native MCP support
  experimental: true
  version: "0.248.0"
  provider:
    name: factory
  auth:
    - role: api-key
      secret: FACTORY_API_KEY
  behaviors:
    manifest:
      files:
        - AGENTS.md
      path-prefixes:
        - .factory/
    network:
      defaults:
        - host.docker.internal
        - github.com
        - raw.githubusercontent.com
      provider-domains:
        factory: "*.factory.ai"
    installation:
      package-manager: npm
      package-name: "@factory/cli"
      binary-name: droid
      step-name: Install Factory Droid CLI
      include-node-setup: true
      cooldown: true
      verify-command: droid --version
      verify-step-name: Verify Factory Droid CLI installation
      docs-url: https://docs.factory.com/droid-exec/overview
    execution:
      command-name: droid
      args:
        - exec
        - --auto
        - high
        - --output-format
        - text
      step-name: Execute Factory Droid CLI
      model-env-var: FACTORY_MODEL
      mcp-config-env-var: GH_AW_MCP_CONFIG
      write-timestamp: true
      env:
        NO_COLOR: "1"
    mcp:
      config-path: .factory/mcp.json
      config-adapter: |
        const { join } = require("path");
        const { loadGatewayContext, filterAndTransformServers, rewriteUrl, writeSecureOutput } = require("./convert_gateway_config_shared.cjs");
        const context = loadGatewayContext({ extraRequiredEnv: ["GITHUB_WORKSPACE"] });
        const servers = filterAndTransformServers(context.servers, context.cliServers, (name, entry) => {
          if (typeof entry.url !== "string") {
            throw new Error(`Factory requires a gateway HTTP URL for MCP server ${name}`);
          }
          return {
            type: "http",
            url: rewriteUrl(entry.url, context.urlPrefix),
            oauth: false,
            ...(entry.headers ? { headers: entry.headers } : {}),
          };
        });
        writeSecureOutput(join(context.extraEnv.GITHUB_WORKSPACE, ".factory", "mcp.json"), JSON.stringify({ mcpServers: servers }, null, 2));
    harness-script: |
      const { readFileSync } = require("fs");
      const { spawnSync } = require("child_process");

      const [command, ...args] = process.argv.slice(2);
      const log = message => process.stderr.write(`[factory-harness] ${message}\n`);
      try {
        if (!command) throw new Error("Factory Droid command is required");
        if (!process.env.FACTORY_API_KEY && process.env.SECRET_FACTORY_API_KEY) {
          process.env.FACTORY_API_KEY = process.env.SECRET_FACTORY_API_KEY;
        }
        if (!process.env.FACTORY_API_KEY) {
          throw new Error("FACTORY_API_KEY is required; configure the FACTORY_API_KEY Actions secret");
        }
        const selectedModel = process.env.FACTORY_MODEL;
        if (!selectedModel?.startsWith("factory/") || !selectedModel.slice("factory/".length).trim()) {
          throw new Error("FACTORY_MODEL must use factory/model format");
        }
        const model = selectedModel.slice("factory/".length);
        const promptPath = process.env.GH_AW_PROMPT;
        if (!promptPath) throw new Error("GH_AW_PROMPT is required");
        if (!readFileSync(promptPath, "utf8").trim()) throw new Error("GH_AW_PROMPT must contain a non-empty instruction");
        const result = spawnSync(command, [...args, "--model", model, "--file", promptPath], {
          cwd: process.env.GITHUB_WORKSPACE,
          env: process.env,
          stdio: "inherit",
        });
        if (result.error) throw result.error;
        if (result.status !== 0) {
          log(result.signal ? `Droid terminated by signal ${result.signal}` : `Droid exited with code ${result.status ?? "unknown"}`);
          process.exitCode = result.status || 1;
        }
      } catch (error) {
        log(error instanceof Error ? error.message : String(error));
        process.exitCode = 1;
      }
---

<!--
# Factory Droid

Unsupported, experimental sample for the [Factory CLI](https://factory.com/product/cli).
Import this definition and select a Factory-hosted model:

```yaml
engine:
  id: factory
model: factory/claude-sonnet-4-6
imports:
  - shared/factory.md
```

Set the `FACTORY_API_KEY` GitHub Actions secret to an API key from
[Factory settings](https://app.factory.ai/settings/api-keys). The `factory/`
prefix is removed before passing the model ID to Droid. This sample uses
Factory-managed inference, not gh-aw universal provider routing or custom BYOK
models. Factory's documented `*.factory.ai` endpoints are allowed for inference.

Installation pins `@factory/cli` 0.248.0 and verifies `droid --version`.
Its npm platform packages support Linux x64 and arm64 with glibc. Lifecycle
scripts stay disabled: the package's JavaScript shim resolves its pinned
optional platform dependency without the postinstall optimization.

The harness runs `droid exec --auto high --output-format text --model <model>
--file <prompt>`. High autonomy permits local builds and MCP calls without
interactive approval; use this sample only inside the gh-aw agent sandbox.
Droid's own command blocks and organization policies still apply. The harness
preserves nonzero CLI exit codes and never prints credentials or prompt contents.

Native MCP uses an owner-only `.factory/mcp.json` generated from the gh-aw
gateway. HTTP authorization headers are retained, OAuth is disabled, and servers
mounted as CLIs are excluded. The generated file is ephemeral and must not be
committed. Droid discovers it from the workflow workspace.

This sample does not implement a structured session parser: text output is
retained in agent logs, but native tool events, token usage, refusals, and
reasoning are not normalized. It does not advertise gh-aw tool allowlists,
max-turn enforcement, Agent Plugins, or custom provider routing. A successful
process exit is not independent proof of task completion.
-->
