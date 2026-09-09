---
engine:
  id: deepseek-harness
  detection-engine: copilot
  version: "0.1.0-rc.6"
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
      const { mkdirSync, readFileSync, writeFileSync } = require("fs");
      const { join } = require("path");
      const { spawnSync } = require("child_process");
      const { fetchAWFReflect, resolveProviderEndpointFromReflect, deriveBaseUrlFromModelsURL } = require("./awf_reflect.cjs");

      const [command, ...commandArgs] = process.argv.slice(2);
      const log = message => process.stderr.write(`[deepseek-harness] ${message}\n`);
      const fail = (result, action) => {
        if (result.error) throw result.error;
        if (result.status !== 0) {
          const error = new Error(`${action} failed with exit code ${result.status ?? "unknown"}`);
          // Surface the child's own status so the step fails with the same code.
          error.exitCode = typeof result.status === "number" && result.status !== 0 ? result.status : 1;
          throw error;
        }
      };

      const main = async () => {
        const workspace = process.env.GITHUB_WORKSPACE;
        if (!workspace) throw new Error("GITHUB_WORKSPACE is required");

        const selectedModel = process.env.DSH_MODEL;
        if (!selectedModel || !selectedModel.includes("/")) {
          throw new Error("DSH_MODEL must use provider/model format");
        }
        const model = selectedModel.slice(selectedModel.indexOf("/") + 1);
        if (!model) throw new Error("DSH_MODEL must include a model name");

        const provider = process.env.GH_AW_LLM_PROVIDER;
        if (!provider) throw new Error("GH_AW_LLM_PROVIDER is required");
        const isAnthropic = provider === "anthropic";

        let baseURL = process.env.OPENAI_BASE_URL;
        if (process.env.AWF_REFLECT_ENABLED === "1") {
          const result = await fetchAWFReflect({ logger: log });
          if (!result.ok || !result.reflectData) {
            throw new Error(`Unable to discover the DeepSeek Harness LLM endpoint from /reflect: ${result.reason || "empty response"}`);
          }
          const endpoint = resolveProviderEndpointFromReflect({
            provider,
            reflectData: result.reflectData,
            logger: log,
          });
          if (!endpoint?.baseUrl) {
            throw new Error(`No configured /reflect endpoint found for provider ${provider}`);
          }
          baseURL = endpoint.baseUrl;
          if (!isAnthropic) {
            // `resolveProviderEndpointFromReflect` returns the origin only, but an
            // OpenAI-compatible route needs the path prefix carried by models_url
            // (typically `/v1`); otherwise dsh would POST to `/chat/completions`.
            const reflectedEndpoint = result.reflectData.endpoints?.find(
              entry => entry?.configured === true && entry.provider === endpoint.endpointProvider
            );
            if (typeof reflectedEndpoint?.models_url === "string") {
              baseURL = deriveBaseUrlFromModelsURL(reflectedEndpoint.models_url);
            }
          }
        }
        if (!baseURL) {
          throw new Error("DeepSeek Harness requires AWF endpoint discovery or OPENAI_BASE_URL");
        }

        const dshHome = join(workspace, ".dsh");
        mkdirSync(dshHome, { recursive: true, mode: 0o700 });
        // dsh's settings provider picks the document format from the file
        // extension, so `settings.yaml` must hold YAML. Every value written here
        // is a scalar, and a JSON string literal is also a valid YAML
        // double-quoted scalar, so quoting through JSON.stringify is safe.
        const scalar = value => JSON.stringify(String(value));
        const settings = [
          "# Ephemeral DeepSeek Harness settings generated by gh-aw for this run.",
          "agent-default-model:",
          "  provider: awf-proxy",
          `  model: ${scalar(model)}`,
          "llm-pi-ai:",
          "  providers:",
          "    awf-proxy:",
          `      displayName: ${scalar("GitHub Agentic Workflows")}`,
          `      apiKeyEnv: ${scalar(isAnthropic ? "ANTHROPIC_API_KEY" : "OPENAI_API_KEY")}`,
          `      api: ${scalar(isAnthropic ? "anthropic-messages" : "openai-completions")}`,
          `      baseURL: ${scalar(baseURL)}`,
          "      models:",
          `        - id: ${scalar(model)}`,
          `          name: ${scalar(model)}`,
          "",
        ].join("\n");
        const settingsPath = join(dshHome, "settings.yaml");
        writeFileSync(settingsPath, settings, { mode: 0o600 });

        const promptPath = process.env.GH_AW_PROMPT;
        if (!promptPath) throw new Error("GH_AW_PROMPT is required");
        const prompt = readFileSync(promptPath, "utf8");
        const env = { ...process.env, DSH_HOME: dshHome };
        log(`configured provider=${provider} model=${model}`);
        fail(
          spawnSync(command, [...commandArgs, prompt], {
            cwd: workspace,
            env,
            stdio: "inherit",
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
model: copilot/claude-sonnet-4.5
imports:
  - shared/deepseek-harness.md
```

The integration pins the developer-preview `@deepseek-ai/dsh` package and runs
its one-shot `headless` profile. Provider credentials and the selected endpoint
are routed through the AWF proxy and written as YAML to an ephemeral
`$DSH_HOME/settings.yaml` under `.dsh`. Telemetry is disabled and the harness
runs with `DSH_TOOLS_MODE: native`, which selects how dsh's own tools are
presented to the model (every tool schema, rather than Code Mode's single
`run_code` entry point). Native MCP configuration is intentionally disabled for
this initial integration; gh-aw exposes configured tools through its CLI proxy.
-->
