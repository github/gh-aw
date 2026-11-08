---
runtimes:
  python:
    version: "3.12"
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
  id: mini-swe-agent
  detection-engine: copilot
  version: "2.4.6"
  display-name: mini-SWE-agent
  description: mini-SWE-agent with non-interactive shell execution and AWF-proxied inference
  experimental: true
  mcp: false
  provider:
    name: github
  behaviors:
    secret-strategy: universal-llm-consumer
    capabilities:
      max-turns: true
    network:
      defaults:
        - host.docker.internal
        - pypi.org
        - files.pythonhosted.org
      provider-domains:
        copilot: api.githubcopilot.com
        anthropic: api.anthropic.com
        openai: api.openai.com
    execution:
      command-name: python3
      step-name: Execute mini-SWE-agent
      model-env-var: GH_AW_MINI_MODEL
      provider-env-mode: universal-llm-consumer
      write-timestamp: true
      env:
        MSWEA_SILENT_STARTUP: "1"
        LITELLM_TELEMETRY: "false"
        GH_AW_MINI_DEPS: /tmp/gh-aw/agent/mini-swe-agent-deps
        GH_AW_MINI_TRAJECTORY: /tmp/gh-aw/agent/mini-swe-agent.traj.json
      args:
        - -I
        - -c
        - |
          import os
          import subprocess
          import sys
          from importlib.metadata import version
          from pathlib import Path

          dependencies = Path(os.environ["GH_AW_MINI_DEPS"])
          subprocess.run([
              sys.executable, "-I", "-m", "pip", "install", "--quiet",
              "--disable-pip-version-check", "--only-binary=:all:",
              "--target", str(dependencies),
              f"mini-swe-agent=={os.environ['GH_AW_ENGINE_VERSION']}",
          ], check=True)
          sys.path.insert(0, str(dependencies))
          assert version("mini-swe-agent") == os.environ["GH_AW_ENGINE_VERSION"]

          import yaml
          from minisweagent.agents.default import DefaultAgent
          from minisweagent.config import builtin_config_dir
          from minisweagent.environments.local import LocalEnvironment
          from minisweagent.models.litellm_textbased_model import LitellmTextbasedModel

          config = yaml.safe_load((builtin_config_dir / "default.yaml").read_text())
          config["agent"]["step_limit"] = int(os.environ["GH_AW_MINI_STEP_LIMIT"])
          config["agent"]["cost_limit"] = 0
          trajectory = Path(os.environ["GH_AW_MINI_TRAJECTORY"])
          trajectory.parent.mkdir(parents=True, exist_ok=True)
          fd = os.open(trajectory, os.O_CREAT | os.O_WRONLY | os.O_TRUNC, 0o600)
          os.fchmod(fd, 0o600)
          os.close(fd)
          config["agent"]["output_path"] = trajectory
          model = LitellmTextbasedModel(
              **config["model"],
              model_name=os.environ["GH_AW_MINI_LITELLM_MODEL"],
              cost_tracking="ignore_errors",
          )
          model.config.model_kwargs.update(
              api_base=os.environ["GH_AW_MINI_API_BASE"],
              api_key="awf-proxy",
          )
          environment = LocalEnvironment(
              **config["environment"],
              cwd=os.environ["GH_AW_ENGINE_CWD"],
          )
          agent = DefaultAgent(model, environment, **config["agent"])
          result = agent.run(Path(os.environ["GH_AW_PROMPT"]).read_text())
          status = result.get("exit_status")
          print(f"mini-SWE-agent exit status: {status}", flush=True)
          if status != "Submitted":
              raise SystemExit(1)
    harness-script: |
      const { spawnSync } = require("child_process");
      const { constants } = require("os");
      const { resolve } = require("path");
      const { fetchAWFReflect, deriveBaseUrlFromModelsURL, normalizeReflectProviderName, REFLECT_PROVIDER_ALIASES } = require("./awf_reflect.cjs");

      const log = message => process.stderr.write(`[mini-swe-agent] ${message}\n`);
      const main = async () => {
        const [command, ...args] = process.argv.slice(2);
        if (!command) throw new Error("mini-SWE-agent command is required");
        if (process.env.AWF_REFLECT_ENABLED !== "1") {
          throw new Error("mini-SWE-agent requires the AWF sandbox and a configured LLM provider");
        }
        const selected = process.env.GH_AW_MINI_MODEL;
        const separator = selected?.indexOf("/") ?? -1;
        if (separator <= 0 || separator === selected.length - 1) {
          throw new Error("GH_AW_MINI_MODEL must use provider/model format");
        }
        const provider = process.env.GH_AW_LLM_PROVIDER;
        if (!["github", "openai", "anthropic"].includes(provider)) {
          throw new Error("mini-SWE-agent supports copilot, openai/codex, or anthropic models");
        }
        const limit = process.env.GH_AW_MAX_TURNS || "20";
        if (!/^[1-9][0-9]*$/.test(limit) || !Number.isSafeInteger(Number(limit))) {
          throw new Error("GH_AW_MAX_TURNS must be a positive safe integer");
        }
        if (!process.env.GH_AW_PROMPT || !process.env.GITHUB_WORKSPACE) {
          throw new Error("GH_AW_PROMPT and GITHUB_WORKSPACE are required");
        }
        const reflected = await fetchAWFReflect({ logger: log });
        if (!reflected.ok || !reflected.reflectData) {
          throw new Error(`Unable to discover the mini-SWE-agent LLM endpoint: ${reflected.reason || "empty /reflect response"}`);
        }
        const aliases = REFLECT_PROVIDER_ALIASES[provider];
        const endpoint = reflected.reflectData.endpoints?.find(entry =>
          entry?.configured === true && aliases.has(normalizeReflectProviderName(entry.provider)) &&
          typeof entry.models_url === "string"
        );
        if (!endpoint) {
          throw new Error(`No configured mini-SWE-agent /reflect models endpoint for provider ${provider}`);
        }
        const model = selected.slice(separator + 1);
        let base = deriveBaseUrlFromModelsURL(endpoint.models_url);
        if (provider === "anthropic") base = base.replace(/\/+$/, "").replace(/\/v1$/, "");
        const env = {
          ...process.env,
          GH_AW_ENGINE_CWD: resolve(process.env.GITHUB_WORKSPACE, process.env.GH_AW_ENGINE_CWD || "."),
          GH_AW_MINI_LITELLM_MODEL: `${provider === "anthropic" ? "anthropic" : "openai"}/${model}`,
          GH_AW_MINI_API_BASE: base,
          GH_AW_MINI_STEP_LIMIT: limit,
        };
        const result = spawnSync(command, args, { env, cwd: env.GH_AW_ENGINE_CWD, stdio: "inherit" });
        if (result.error) throw result.error;
        if (result.status !== 0) {
          const error = new Error(`mini-SWE-agent execution failed: ${result.signal || `exit code ${result.status ?? "unknown"}`}`);
          error.exitCode = result.status ?? (constants.signals[result.signal] ? 128 + constants.signals[result.signal] : 1);
          throw error;
        }
      };
      main().catch(error => {
        log(error instanceof Error ? error.message : String(error));
        process.exitCode = error.exitCode || 1;
      });
---

<!--
# mini-SWE-agent

This unsupported experimental sample pins `mini-swe-agent==2.4.6` from PyPI
and uses Python 3.12. Dependencies are installed inside AWF at the mounted path
`/tmp/gh-aw/agent/mini-swe-agent-deps`, not in the host user site. Isolated
Python ignores workspace imports and Python environment search paths; only
the explicit dependency directory is added. PyPI wheel downloads require
`pypi.org` and `files.pythonhosted.org`, included in the engine network defaults.
Import it and select a workflow-facing `provider/model`:

```yaml
engine:
  id: mini-swe-agent
model: copilot/claude-haiku-5.5
imports:
  - shared/mini-swe-agent.md
sandbox:
  agent:
    id: awf
```

Copilot uses `copilot-requests: write` or `COPILOT_GITHUB_TOKEN`; OpenAI/Codex
and Anthropic use their standard gh-aw provider secrets. AWF is required for
all providers. The harness discovers the matching endpoint through `/reflect`,
maps Copilot/OpenAI/Codex models to LiteLLM's `openai/<model>` and Anthropic
models to `anthropic/<model>`, and uses a placeholder key. Real provider
credentials are excluded from the agent environment.

The pinned Python API bypasses CLI first-run setup and interactive confirmations.
It reads the workflow prompt from a file, uses the packaged `default.yaml`
with the text-based LiteLLM model and local shell environment inside AWF, and
fails unless the agent explicitly submits with
`echo COMPLETE_TASK_AND_SUBMIT_FINAL_OUTPUT`. `max-turns` limits model calls,
defaulting to 20 when unset.
LiteLLM's best-effort cost estimates do not enforce a dollar budget; AWF
enforces the workflow inference budget.

There is no native MCP client: configured tools and safe outputs are available
through gh-aw's shell CLI proxies. Native shell command allowlists, disabling
file edits, Agent Plugins, repository-specific mini configuration, and structured
session parsing are not supported. The private native trajectory is written to
`/tmp/gh-aw/agent/mini-swe-agent.traj.json` but is not uploaded as an artifact;
stdout is not treated as verified
tool or token-accounting evidence. Dependencies must have binary wheels; source
builds are deliberately disabled.
-->
