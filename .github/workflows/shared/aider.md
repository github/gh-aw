---
runtimes:
  python:
    version: "3.12"
pre-agent-steps:
  - name: Preinstall Aider CLI
    run: |
      # fastuuid only ships manylinux wheels for CPython; installing it explicitly first
      # ensures pip resolves the prebuilt wheel instead of falling back to a source build
      # that would require Cargo/crates.io network access.
      python3 -m pip install --quiet --user --disable-pip-version-check --only-binary=:all: fastuuid==0.14.0
      python3 -m pip install --quiet --user --disable-pip-version-check "aider-chat==$GH_AW_ENGINE_VERSION"
      "$HOME/.local/bin/aider" --version
    env:
      AIDER_ANALYTICS_DISABLE: "true"
      AIDER_CHECK_UPDATE: "false"
engine:
  id: aider
  detection-engine: copilot
  version: "0.86.2"
  display-name: Aider
  description: Aider AI pair programming CLI running in scripting (non-interactive) mode
  experimental: true
  mcp: false
  provider:
    name: github
  behaviors:
    secret-strategy: universal-llm-consumer
    manifest:
      files:
        - .aider.conf.yml
        - CONVENTIONS.md
      path-prefixes:
        - .aider/
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
      command-name: python3
      args:
        - -c
        - |
          import json
          import os
          import sys
          import time
          from pathlib import Path
          from uuid import uuid4
          from aider import main as aider_main
          from aider.coders import base_coder
          from aider.io import InputOutput

          session_id = str(uuid4())
          started_at = time.monotonic()
          turns = 0
          errors = []

          def emit(event_type, data):
              print("\n" + json.dumps({
                  "type": event_type,
                  "timestamp": int(time.time() * 1000),
                  "data": {"sourceEngine": "aider", "sessionId": session_id, **data},
              }), flush=True)

          class WorkflowInputOutput(InputOutput):
              def confirm_ask(self, question, *args, **kwargs):
                  if question in ("Run shell command?", "Run shell commands?"):
                      kwargs["explicit_yes_required"] = False
                  return super().confirm_ask(question, *args, **kwargs)

              def assistant_output(self, message, pretty=None):
                  global turns
                  if not message:
                      return super().assistant_output(message, pretty)
                  turns += 1
                  emit("assistant.message", {"content": message})

              def tool_error(self, message="", strip=True):
                  errors.append(str(message))
                  return super().tool_error(message, strip)

          original_run_cmd = base_coder.run_cmd

          def workflow_run_cmd(command, *args, **kwargs):
              tool_id = str(uuid4())
              emit("tool.execution_start", {
                  "toolCallId": tool_id, "toolName": "bash", "input": {"command": command},
              })
              tool_started_at = time.monotonic()
              exit_code, output = original_run_cmd(command, *args, **kwargs)
              emit("tool.execution_complete", {
                  "toolCallId": tool_id, "toolName": "bash", "success": exit_code == 0,
                  "exitCode": exit_code, "output": output,
                  "durationMs": int((time.monotonic() - tool_started_at) * 1000),
              })
              return exit_code, output

          aider_main.InputOutput = WorkflowInputOutput
          base_coder.run_cmd = workflow_run_cmd
          emit("session.init", {"model": os.environ.get("AIDER_MODEL")})
          emit("user.message", {
              "content": Path(os.environ["GH_AW_PROMPT"]).read_text(encoding="utf-8"),
          })
          try:
              exit_code = aider_main.main()
          finally:
              emit("session.result", {
                  "numTurns": turns, "durationMs": int((time.monotonic() - started_at) * 1000),
                  "errors": errors,
              })
          sys.exit(exit_code)
        - --yes-always
        - --edit-format
        - diff
        - --no-auto-commits
        - --no-check-update
        - --no-show-release-notes
        - --no-detect-urls
        - --no-pretty
        - --no-stream
        - --no-fancy-input
        - --analytics-disable
      step-name: Execute Aider CLI
      model-env-var: AIDER_MODEL
      provider-env-mode: universal-llm-consumer
      write-timestamp: true
      env:
        AIDER_GIT: "false"
        AIDER_CHECK_UPDATE: "false"
        AIDER_ANALYTICS_DISABLE: "true"
    harness-script: |
      const { readFileSync } = require("fs");
      const { join } = require("path");
      const { homedir } = require("os");
      const { runProcess } = require("./process_runner.cjs");
      const { fetchAWFReflect, deriveBaseUrlFromModelsURL, normalizeReflectProviderName, REFLECT_PROVIDER_ALIASES } = require("./awf_reflect.cjs");

      const [command, ...commandArgs] = process.argv.slice(2);
      const log = message => process.stderr.write(`[aider-harness] ${message}\n`);

      const main = async () => {
        if (!command) throw new Error("Aider command is required");
        const selectedModel = process.env.AIDER_MODEL;
        const separator = selectedModel?.indexOf("/") ?? -1;
        if (separator <= 0 || separator === selectedModel.length - 1) {
          throw new Error("AIDER_MODEL must use provider/model format");
        }
        const provider = process.env.GH_AW_LLM_PROVIDER;
        if (!["github", "anthropic", "openai"].includes(provider)) {
          throw new Error("GH_AW_LLM_PROVIDER must be github, anthropic, or openai");
        }
        const isAnthropic = provider === "anthropic";
        let model = selectedModel.slice(separator + 1);
        if (provider === "github") {
          model = model.replace(/^(claude-(?:haiku|sonnet|opus)-\d+)-(\d+)$/, "$1.$2");
        }
        const promptFile = process.env.GH_AW_PROMPT;
        if (!promptFile) throw new Error("GH_AW_PROMPT is not set");
        readFileSync(promptFile, "utf8");

        let baseURL;
        let apiKey;
        if (process.env.AWF_REFLECT_ENABLED === "1") {
          const result = await fetchAWFReflect({ logger: log });
          if (!result.ok || !result.reflectData) {
            throw new Error("Unable to discover the Aider LLM endpoint from /reflect");
          }
          const aliases = REFLECT_PROVIDER_ALIASES[provider];
          const endpoint = result.reflectData.endpoints?.find(
            entry => entry?.configured === true && aliases.has(normalizeReflectProviderName(entry.provider))
          );
          if (!endpoint || typeof endpoint.models_url !== "string") {
            throw new Error(`No configured /reflect models endpoint found for provider ${provider}`);
          }
          baseURL = deriveBaseUrlFromModelsURL(endpoint.models_url);
          apiKey = "awf-proxy";
        } else {
          if (provider === "github") throw new Error("Aider Copilot routing requires the AWF sandbox");
          baseURL = isAnthropic ? process.env.ANTHROPIC_BASE_URL || "https://api.anthropic.com" : process.env.OPENAI_BASE_URL || "https://api.openai.com/v1";
          apiKey = isAnthropic ? process.env.ANTHROPIC_API_KEY : process.env.OPENAI_API_KEY || process.env.CODEX_API_KEY;
          if (!apiKey) throw new Error("Aider provider API key is required without AWF");
        }
        const localBin = join(homedir(), ".local", "bin");
        const env = {
          ...process.env,
          PATH: `${localBin}:${process.env.PATH || ""}`,
          AIDER_MODEL: `${isAnthropic ? "anthropic" : "openai"}/${model}`,
        };
        delete env.GITHUB_COPILOT_TOKEN;
        delete env.COPILOT_GITHUB_TOKEN;
        delete env.CODEX_API_KEY;
        if (isAnthropic) {
          // LiteLLM 1.81.10 appends /v1/messages to the Anthropic base URL.
          const anthropicBaseURL = baseURL.replace(/\/+$/, "").replace(/\/v1$/, "");
          env.ANTHROPIC_API_BASE = anthropicBaseURL;
          env.ANTHROPIC_BASE_URL = anthropicBaseURL;
          env.ANTHROPIC_API_KEY = apiKey;
          env.AIDER_ANTHROPIC_API_KEY = apiKey;
          delete env.OPENAI_API_KEY;
          delete env.AIDER_OPENAI_API_KEY;
          delete env.AIDER_OPENAI_API_BASE;
        } else {
          env.OPENAI_API_BASE = baseURL;
          env.OPENAI_BASE_URL = baseURL;
          env.AIDER_OPENAI_API_BASE = baseURL;
          env.OPENAI_API_KEY = apiKey;
          env.AIDER_OPENAI_API_KEY = apiKey;
          delete env.ANTHROPIC_API_KEY;
          delete env.AIDER_ANTHROPIC_API_KEY;
        }
        let reportedError = false;
        const observeLine = line => {
          if (/\blitellm\.\w*Error:/.test(line)) reportedError = true;
        };
        const result = await runProcess({
          command,
          args: [...commandArgs, "--message-file", promptFile],
          logArgs: ["(configured arguments)", "--message-file", "(prompt file)"],
          attempt: 0,
          log,
          env,
          maxCollectedOutputBytes: 1024 * 1024,
          onStdoutLine: observeLine,
          onStderrLine: observeLine,
        });
        if (result.exitCode !== 0) {
          process.exitCode = result.exitCode;
          throw new Error(`Aider execution failed with exit code ${result.exitCode}`);
        }
        if (reportedError) throw new Error("Aider execution reported a LiteLLM error");
      };

      main().catch(error => {
        log(error instanceof Error ? error.message : String(error));
        if (!process.exitCode) process.exitCode = 1;
      });
    log-parser: |
      function parseLog(logContent) {
        const { parseLogEntries, generateCopilotCliStyleSummary } = require("./log_parser_shared.cjs");
        const { isSessionEvent } = require("./agent_session.cjs");
        const logEntries = (parseLogEntries(logContent) || []).filter(
          entry => isSessionEvent(entry) && entry.data.sourceEngine === "aider"
        );
        return {
          markdown: generateCopilotCliStyleSummary(logEntries),
          logEntries,
          mcpFailures: [],
          maxTurnsHit: false,
        };
      }
---

## Aider execution constraints

Aider runs one non-interactive turn: the prompt is delivered with `--message-file` and your
single reply is the whole run. Plan for that:

- **Edit files with *SEARCH/REPLACE* blocks.** Aider applies them for you, including for new
  files (empty `SEARCH` section). Do not write source files with `cat`/heredocs.
- **Put shell commands in ```bash blocks, one complete command per line.** Aider executes each
  line separately, so multi-line commands, backslash continuations and heredocs do not work.
  Chain steps with `&&` or `;` on a single line instead.
- **Suggest at most a few commands**; they all run from the repository root.
- **Emit safe outputs through the `safeoutputs` MCP CLI**, for example
  `safeoutputs noop --message "..."`. Every `safeoutputs` command must be inside
  a ```bash block. Commands in prose or inline code are not executed.

<!--
# Aider CLI

Unsupported sample engine definition for [Aider](https://github.com/Aider-AI/aider), the
open-source AI pair programming CLI ([docs](https://aider.chat/docs/)).
Import this file and set `engine: id: aider` to use it:

```yaml
engine:
  id: aider
model: copilot/auto
imports:
  - shared/aider.md
```

`model` must use `provider/model` format. Supported providers are `copilot`,
`anthropic`, `openai`, and `codex` (an alias for `openai`). The sample pins
`aider-chat==0.86.2`, the latest stable PyPI package (GitHub's latest release
entry still names 0.86.0).
Requests use the selected provider's configured AWF `/reflect` endpoint,
preserving its API path prefix. Copilot and OpenAI use Aider's
`openai/<model>` LiteLLM form; Anthropic uses native `anthropic/<model>` requests.
Endpoint and authentication settings are supplied through environment variables,
without overwriting repository `.aider.conf.yml` settings.
Copilot Claude aliases such as `claude-sonnet-4-5` are normalized to the dotted
model IDs exposed by the proxy, such as `claude-sonnet-4.5`.
Without AWF, OpenAI and Anthropic use their provider API key and optional
`OPENAI_BASE_URL` or `ANTHROPIC_BASE_URL`. Copilot requires AWF.

Aider runs in scripting mode: the generated prompt file is passed with
`--message-file`. A small Python entrypoint subclasses `InputOutput` so
`--yes-always` also accepts suggested shell commands, which stock Aider
deliberately declines when explicit confirmation is required. The adapter
changes only the two shell-command confirmation prompts; other explicit
confirmation requirements remain intact. Commands run in the configured
workflow sandbox.
The entrypoint emits native session events for model replies, shell commands
and their results, and session boundaries. The declarative log parser publishes
normalized agent logs used by Actions summaries and the unified session artifact;
local log reconstruction also recognizes these events.
The edit format is pinned to `diff` (the editblock coder) because the proxied
model names are unknown to Aider and would otherwise fall back to the `whole`
format, which rejects ```bash blocks and cannot run shell commands.
Aider reports some LiteLLM request failures with exit code 0, so the harness
also detects those errors in its output and fails the workflow.
Output is forwarded as it arrives, without Node's synchronous output-buffer
limit, and child exit codes and termination signals are preserved.
Aider has no MCP client, so the compiler exposes MCP-backed tools through
`cli-proxy` and GitHub access through `gh-proxy`. Both proxies are enabled
automatically and cannot be disabled for this engine.
-->
