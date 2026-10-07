---
engine:
  id: deepseek-harness
  detection-engine: copilot
  version: "0.2.0-rc.2"
  display-name: DeepSeek Harness
  description: DeepSeek Harness (dsh) with headless execution and multi-provider LLM support
  experimental: true
  mcp: false
  provider:
    name: github
  behaviors:
    secret-strategy: universal-llm-consumer
    manifest:
      files:
        - AGENTS.md
        - AGENTS.local.md
        - CLAUDE.md
        - CLAUDE.local.md
      path-prefixes:
        - .dsh/
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
    installation:
      package-manager: npm
      package-name: "@deepseek-ai/dsh"
      step-name: Install DeepSeek Harness
      binary-name: dsh
      include-node-setup: true
      # dsh ships native addons and profile bundles that are wired by npm
      # lifecycle scripts, so they must run for `dsh --profile headless` to boot.
      # Risk is bounded by the pinned version above.
      post-install-scripts: true
      cooldown: false
      verify-command: dsh --version
      verify-step-name: Verify DeepSeek Harness installation
      docs-url: https://github.com/deepseek-ai/deepseek-harness
    execution:
      command-name: dsh
      args:
        - --profile
        - headless
      step-name: Execute DeepSeek Harness
      model-env-var: DSH_MODEL
      provider-env-mode: universal-llm-consumer
      write-timestamp: true
      env:
        # dsh's sandbox-policy defaults to workspace-write + interactive approval;
        # an unattended workflow run has nobody to answer the prompts, and the
        # agent already runs inside the gh-aw sandbox, so approvals are waived here.
        DSH_PERMISSION_MODE: danger-full-access
        DSH_TELEMETRY_DISABLED: "1"
        # Tool *presentation* mode, not MCP: `native` shows every tool schema
        # directly, whereas `code` would expose a single `run_code` entry point.
        DSH_TOOLS_MODE: native
        NO_COLOR: "1"
    harness-script: |
      const { mkdirSync, mkdtempSync, readFileSync, writeFileSync } = require("fs");
      const { join } = require("path");
      const { spawnSync } = require("child_process");
      const { constants } = require("os");
      const { fetchAWFReflect, deriveBaseUrlFromModelsURL, normalizeReflectProviderName, REFLECT_PROVIDER_ALIASES } = require("./awf_reflect.cjs");

      const [command, ...commandArgs] = process.argv.slice(2);
      const log = message => process.stderr.write(`[deepseek-harness] ${message}\n`);
      const fail = (result, action) => {
        if (result.error) throw result.error;
        if (result.status !== 0) {
          const exitCode = result.status ?? (result.signal && constants.signals[result.signal] ? 128 + constants.signals[result.signal] : 1);
          const error = new Error(`${action} failed with exit code ${exitCode}${result.signal ? ` (signal=${result.signal})` : ""}`);
          error.exitCode = exitCode;
          throw error;
        }
      };

      const main = async () => {
        if (!command) throw new Error("DeepSeek Harness command is required");
        const workspace = process.env.GITHUB_WORKSPACE;
        if (!workspace) throw new Error("GITHUB_WORKSPACE is required");

        const selectedModel = process.env.DSH_MODEL;
        const separator = selectedModel?.indexOf("/") ?? -1;
        if (separator <= 0 || separator === selectedModel.length - 1) {
          throw new Error("DSH_MODEL must use provider/model format");
        }
        const model = selectedModel.slice(separator + 1);

        const provider = process.env.GH_AW_LLM_PROVIDER;
        if (!["github", "anthropic", "openai"].includes(provider)) {
          throw new Error("GH_AW_LLM_PROVIDER must be github, anthropic, or openai");
        }
        const isAnthropic = provider === "anthropic";
        const apiKeyEnv = isAnthropic ? "ANTHROPIC_API_KEY" : "OPENAI_API_KEY";

        let baseURL;
        let apiKey;
        if (process.env.AWF_REFLECT_ENABLED === "1") {
          const result = await fetchAWFReflect({ logger: log });
          if (!result.ok || !result.reflectData) {
            throw new Error(`Unable to discover the DeepSeek Harness LLM endpoint from /reflect: ${result.reason || "empty response"}`);
          }
          const aliases = REFLECT_PROVIDER_ALIASES[provider];
          const endpoint = result.reflectData.endpoints?.find(
            entry => entry?.configured === true && aliases.has(normalizeReflectProviderName(entry.provider))
          );
          if (!endpoint || typeof endpoint.models_url !== "string") {
            throw new Error(`No configured /reflect models endpoint found for provider ${provider}`);
          }
          baseURL = deriveBaseUrlFromModelsURL(endpoint.models_url);
          // AWF supplies the real credentials; dsh only needs a nonempty key.
          apiKey = "awf-proxy";
        } else {
          if (provider === "github") throw new Error("DeepSeek Harness Copilot routing requires the AWF sandbox");
          baseURL = isAnthropic ? process.env.ANTHROPIC_BASE_URL || "https://api.anthropic.com" : process.env.OPENAI_BASE_URL || "https://api.openai.com/v1";
          apiKey = isAnthropic ? process.env.ANTHROPIC_API_KEY : process.env.OPENAI_API_KEY || process.env.CODEX_API_KEY;
          if (!apiKey) throw new Error(`${apiKeyEnv} is required without AWF`);
        }
        // The Anthropic SDK appends /v1/messages itself.
        if (isAnthropic) baseURL = baseURL.replace(/\/+$/, "").replace(/\/v1$/, "");

        const promptPath = process.env.GH_AW_PROMPT;
        if (!promptPath) throw new Error("GH_AW_PROMPT is required");
        const prompt = readFileSync(promptPath, "utf8");
        const homeRoot = join(workspace, ".dsh");
        mkdirSync(homeRoot, { recursive: true, mode: 0o700 });
        const dshHome = mkdtempSync(join(homeRoot, "gh-aw-"));
        const patchPath = join(dshHome, "cordis.patch.yml");
        const patch = [
          { id: "agent-default-model", config: { provider: "awf-proxy", model } },
          {
            id: "llm-pi-ai",
            config: {
              providers: {
                "awf-proxy": {
                  displayName: "GitHub Agentic Workflows",
                  apiKeyEnv,
                  api: isAnthropic ? "anthropic-messages" : "openai-completions",
                  baseURL,
                  models: [{ id: model, name: model }],
                },
              },
            },
          },
        ];
        writeFileSync(patchPath, JSON.stringify(patch, null, 2) + "\n", { mode: 0o600 });
        const env = { ...process.env, DSH_HOME: dshHome, [apiKeyEnv]: apiKey };
        log(`configured provider=${provider} model=${model}`);
        fail(
          spawnSync(command, [...commandArgs, "--patch", patchPath, "-"], {
            cwd: process.env.GH_AW_ENGINE_CWD || workspace,
            env,
            input: prompt,
            stdio: ["pipe", "inherit", "inherit"],
          }),
          "DeepSeek Harness execution"
        );
      };

      main().catch(error => {
        log(error instanceof Error ? error.message : String(error));
        process.exitCode = typeof error?.exitCode === "number" && error.exitCode !== 0 ? error.exitCode : 1;
      });
---

<!--
# DeepSeek Harness

Shared engine definition for [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness),
the open-source `dsh` coding agent. Import this file and set
`engine.id: deepseek-harness` with a `provider/model` model selection:

```yaml
engine:
  id: deepseek-harness
model: copilot/auto
imports:
  - shared/deepseek-harness.md
```

This unsupported sample pins `@deepseek-ai/dsh@0.2.0-rc.2` and runs its one-shot
`headless` profile. The prompt is piped verbatim through stdin, not exposed in
process arguments. Each run uses a fresh private `$DSH_HOME` under `.dsh`;
`cordis.patch.yml` selects the model and provider endpoint without overwriting
repository settings. The patch contains credential environment-variable names,
never keys. AWF routing requires a configured endpoint matching the selected
provider; Copilot requires AWF. With AWF disabled, Anthropic and OpenAI/Codex use
their API-key and base-URL environment variables. Telemetry is disabled and the harness
runs with `DSH_TOOLS_MODE: native`, which selects how dsh's own tools are
presented to the model (every tool schema, rather than Code Mode's single
`run_code` entry point). Native MCP configuration is intentionally disabled for
this initial integration; gh-aw exposes configured tools through its CLI proxy.
-->
