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
          import math
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
          response_usage = []
          observed_cost = None
          capturing_response = False

          def emit(event_type, data):
              print("\n" + json.dumps({
                  "type": event_type,
                  "timestamp": int(time.time() * 1000),
                  "data": {"sourceEngine": "aider", "sessionId": session_id, **data},
              }), flush=True)

          def native_value(value):
              if hasattr(value, "model_dump"):
                  return value.model_dump()
              return value

          def field(value, name):
              if isinstance(value, dict):
                  return value.get(name)
              return getattr(value, name, None)

          def has_field(value, name):
              return name in value if isinstance(value, dict) else hasattr(value, name)

          def accounting(usage):
              mapped = {}
              for target, names in {
                  "input_tokens": ("prompt_tokens", "input_tokens"),
                  "output_tokens": ("completion_tokens", "output_tokens"),
                  "total_tokens": ("total_tokens",),
                  "cache_read_input_tokens": ("cache_read_input_tokens", "prompt_cache_hit_tokens"),
                  "cache_creation_input_tokens": ("cache_creation_input_tokens",),
              }.items():
                  for name in names:
                      if name in usage:
                          mapped[target] = usage[name]
                          break
              for target, details, name in (
                  ("cache_read_input_tokens", "prompt_tokens_details", "cached_tokens"),
                  ("reasoning_output_tokens", "completion_tokens_details", "reasoning_tokens"),
              ):
                  nested = usage.get(details)
                  if target not in mapped and isinstance(nested, dict) and name in nested:
                      mapped[target] = nested[name]
              if "prompt_tokens" in usage:
                  mapped["input_tokens_include_cache"] = not (
                      "cache_read_input_tokens" in usage or "cache_creation_input_tokens" in usage
                  )
              return mapped

          def total_usage():
              if not response_usage:
                  return {}
              result = {}
              for name in set.intersection(*(set(usage) for usage in response_usage)):
                  values = [usage[name] for usage in response_usage]
                  if name == "input_tokens_include_cache":
                      if all(value is values[0] for value in values):
                          result[name] = values[0]
                  elif all(type(value) is int and 0 <= value <= 9007199254740991 for value in values):
                      total = sum(values)
                      if total <= 9007199254740991:
                          result[name] = total
              return result

          class WorkflowInputOutput(InputOutput):
              def confirm_ask(self, question, *args, **kwargs):
                  if question in ("Run shell command?", "Run shell commands?"):
                      kwargs["explicit_yes_required"] = False
                  return super().confirm_ask(question, *args, **kwargs)

              def assistant_output(self, message, pretty=None):
                  global turns
                  if not capturing_response and message:
                      turns += 1
                      emit("assistant.message", {"content": message})
                  # Keep empty-response warnings without duplicating rendered provider text.
                  if not message:
                      return super().assistant_output(message, pretty)

              def tool_error(self, message="", strip=True):
                  content = native_value(message)
                  if not isinstance(content, (str, int, float, bool, list, dict, type(None))):
                      content = str(message)
                  errors.append(content)
                  emit("session.error", {"content": content})
                  return super().tool_error(message, strip)

          original_show_send_output = base_coder.Coder.show_send_output
          original_calculate_usage = base_coder.Coder.calculate_and_show_tokens_and_cost

          def workflow_show_send_output(coder, completion):
              global turns, capturing_response
              choices = field(completion, "choices")
              usage = native_value(field(completion, "usage"))
              if isinstance(usage, dict):
                  response_usage.append(accounting(usage))
              else:
                  response_usage.append({})
              if choices:
                  turns += 1
              for choice in choices or []:
                  message = field(choice, "message")
                  if message is None:
                      continue
                  metadata = {}
                  for name in ("id", "model", "created"):
                      if has_field(completion, name):
                          metadata["apiCallId" if name == "id" else name] = field(completion, name)
                  metadata["message"] = native_value(message)
                  finish_reason = field(choice, "finish_reason")
                  if has_field(choice, "finish_reason"):
                      metadata["finishReason"] = finish_reason
                  if isinstance(usage, dict):
                      metadata["usage"] = usage
                  reasoning = field(message, "reasoning_content")
                  if reasoning is None:
                      reasoning = field(message, "reasoning")
                  content = field(message, "content")
                  partial = {"partial": True} if finish_reason == "length" else {}
                  if reasoning is not None:
                      emit("assistant.reasoning", {**metadata, **partial, "content": reasoning})
                  refusal = field(message, "refusal")
                  if refusal is not None or finish_reason == "content_filter":
                      emit("assistant.refusal", {
                          **metadata,
                          "reason": "content_filter" if finish_reason == "content_filter" else "refusal",
                          **({"content": refusal if refusal is not None else content} if refusal is not None or has_field(message, "content") else {}),
                      })
                  elif has_field(message, "content"):
                      emit("assistant.message", {**metadata, **partial, "content": content})
              capturing_response = True
              try:
                  return original_show_send_output(coder, completion)
              finally:
                  capturing_response = False

          def workflow_calculate_usage(coder, messages, completion=None):
              global observed_cost
              result = original_calculate_usage(coder, messages, completion)
              if getattr(coder, "usage_report", None) and coder.main_model.info.get("input_cost_per_token"):
                  cost = getattr(coder, "total_cost", None)
                  if type(cost) in (int, float) and math.isfinite(cost) and cost >= 0:
                      observed_cost = cost
              return result

          original_run_cmd = base_coder.run_cmd

          def workflow_run_cmd(command, *args, **kwargs):
              tool_id = str(uuid4())
              emit("tool.execution_start", {
                  "toolCallId": tool_id, "toolName": "bash", "input": {"command": command},
              })
              tool_started_at = time.monotonic()
              try:
                  exit_code, output = original_run_cmd(command, *args, **kwargs)
              except BaseException as error:
                  emit("tool.execution_complete", {
                      "toolCallId": tool_id, "toolName": "bash", "success": False,
                      "error": str(error), "status": "failed",
                      "durationMs": int((time.monotonic() - tool_started_at) * 1000),
                  })
                  raise
              emit("tool.execution_complete", {
                  "toolCallId": tool_id, "toolName": "bash", "success": exit_code == 0,
                  "exitCode": exit_code, "output": output,
                  "durationMs": int((time.monotonic() - tool_started_at) * 1000),
              })
              return exit_code, output

          aider_main.InputOutput = WorkflowInputOutput
          base_coder.Coder.show_send_output = workflow_show_send_output
          base_coder.Coder.calculate_and_show_tokens_and_cost = workflow_calculate_usage
          base_coder.run_cmd = workflow_run_cmd
          emit("session.init", {"model": os.environ.get("AIDER_MODEL"), "cwd": os.getcwd()})
          emit("user.message", {
              "content": Path(os.environ["GH_AW_PROMPT"]).read_text(encoding="utf-8"),
          })
          terminal = {}
          try:
              exit_code = aider_main.main()
              if type(exit_code) is int:
                  terminal = {
                      "status": "completed" if exit_code == 0 else "failed",
                      "sourceType": "process.exit",
                      "exitCode": exit_code,
                  }
          except BaseException as error:
              errors.append(str(error))
              terminal = {"status": "failed", "sourceType": type(error).__name__}
              raise
          finally:
              usage = total_usage()
              emit("session.result", {
                  **terminal,
                  "numTurns": turns, "durationMs": int((time.monotonic() - started_at) * 1000),
                  "errors": errors,
                  **({"usage": usage} if usage else {}),
                  **({"totalCostUsd": observed_cost} if observed_cost is not None else {}),
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
        // Shell stdout can contain JSON; interpreted evidence requires Aider attribution.
        // Unknown native extensions stay opaque, including within core namespaces.
        const interpretedTypes = new Set([
          "session.init", "session.start", "session.info", "session.shutdown",
          "session.task_complete", "user.message",
          "assistant.message", "assistant.message_delta",
          "assistant.reasoning", "assistant.reasoning_delta", "assistant.refusal",
          "assistant.usage", "assistant.turn_end", "model.call_failure",
          "tool.execution_start", "tool.execution_update", "tool.execution_complete",
          "session.result", "session.error", "agent.execution",
          "detection.result", "session.format",
          "session.collection", "session.collection_warning",
          "prompt.system", "prompt.user",
          "mcp.rpc.request", "mcp.rpc.response", "mcp.difc.filtered",
          "mcp.guard.blocked", "mcp.tool_call", "mcp.event",
          "firewall.http_access", "firewall.token_usage", "firewall.model_routing",
          "firewall.steering", "firewall.event", "model_routing.outcome",
          "safe_output.request", "safe_output.result", "safe_output.error",
          "experiment.state", "experiment.assignment", "grader.manifest", "grader.result",
          "eval.result", "github_api.rate_limit", "usage.report", "execution.result",
          "guardrail.daily_aic", "guard.tool_denials_exceeded", "workflow.info",
          "workflow.run_started", "workflow.run_updated", "workflow.run_settled",
          "dynamicWorkflows.task_started", "dynamicWorkflows.task_progress",
          "dynamicWorkflows.task_updated", "dynamicWorkflows.task_notification",
          "dynamicWorkflows.background_tasks_changed",
          "claude.assistant_error", "claude.api_retry", "turn.failed",
          "claude.stream_event", "claude.assistant_snapshot", "gemini.message_snapshot",
          "pi.message_snapshot",
        ]);
        const isInterpreted = entry => interpretedTypes.has(entry.type) ||
          entry.type.startsWith("subagent.");
        const logEntries = (parseLogEntries(logContent) || []).filter(
          entry => isSessionEvent(entry) && (
            entry.data.sourceEngine === "aider" ||
            (entry.data.sourceEngine === undefined && !isInterpreted(entry))
          )
        );
        // Older Aider versions expose provider failures before any model reply.
        // Require the native startup preamble; unframed conversation/tool text
        // cannot establish diagnostic attribution.
        if (!logEntries.length) {
          const lines = logContent.split("\n");
          let banner = false;
          let startup = false;
          for (let index = 0; index < lines.length; index++) {
            const line = lines[index];
            if (/^Aider v\d+\.\d+\.\d+$/.test(line)) banner = true;
            if (banner && /^Repo-map: /.test(line)) {
              startup = true;
              continue;
            }
            if (!startup || !line.trim() || /^Retrying in \d+(?:\.\d+)? seconds\.\.\.$/.test(line)) continue;
            const diagnostic = line.match(/^litellm\.([A-Za-z][A-Za-z0-9]*Error): .+/);
            if (!diagnostic) {
              startup = false;
              continue;
            }
            let content = line + (index < lines.length - 1 ? "\n" : "");
            while (index + 1 < lines.length) {
              const next = lines[index + 1];
              if (!next.trim() || /^(?:Retrying in |litellm\.|```|~~~|>|[\[{"]|(?:assistant|user|tool|exec)(?::|\s*$)|Error:|Process exiting)/i.test(next)) break;
              content += next + (index + 1 < lines.length - 1 ? "\n" : "");
              index++;
            }
            logEntries.push({
              type: "session.error",
              data: { sourceEngine: "aider", errorType: diagnostic[1], content },
            });
          }
        }
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
The entrypoint emits canonical session events for exact provider reply text,
separate reasoning and structured refusals, shell commands and their results,
errors, and session boundaries. Provider token counts and observed Aider cost
are captured before display rounding; missing accounting remains absent, and
session totals are emitted once. Length-limited replies retain `partial: true`.
Repository configuration that enables streaming instead of this profile's
`--no-stream` is outside this response-hook coverage. Historical logs with only
rounded `Tokens:`/`Cost:` prose cannot recover exact accounting, and older runs
without structured events cannot reconstruct conversations. Before-response
LiteLLM diagnostics are retained as `session.error` only when the native Aider
startup preamble establishes their origin; quoted, tool and conversation text
does not establish attribution. These diagnostics do not imply a session result.
The declarative log parser publishes
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
