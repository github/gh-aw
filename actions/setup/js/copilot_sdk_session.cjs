// @ts-check

/**
 * Copilot SDK Session Runner
 *
 * Runs a single Copilot agentic session using the @github/copilot-sdk.
 * Serializes all SDK session events to a JSONL file so that
 * unified_timeline.cjs can render them in the step summary.
 *
 * Event mapping:
 *   SDK "user.message"            → JSONL "user.message"
 *   SDK "tool.execution_start"    → JSONL "tool.execution_start"  (toolName, mcpServerName, command?)
 *   SDK "tool.execution_complete" → JSONL "tool.execution_complete" (toolName, mcpServerName, success, result)
 *   SDK "assistant.message"       → JSONL "assistant.message"     (content)
 *   SDK "assistant.turn_start"    → watchdog disarmed (inAssistantTurn = true)
 *   SDK "assistant.turn_end"      → watchdog re-enabled (inAssistantTurn = false)
 *   SDK "session.task_complete"   → JSONL "session.task_complete" (success, summary)
 *   SDK "subagent.started"        → JSONL "subagent.started"      (agentName, agentDisplayName, toolCallId)
 *   SDK "subagent.completed"      → JSONL "subagent.completed"    (agentName, toolCallId)
 *   SDK "subagent.failed"         → JSONL "subagent.failed"       (agentName, toolCallId, error)
 *
 * The JSONL file is written to:
 *   /tmp/gh-aw/sandbox/agent/logs/copilot-session-state/{sessionId}/events.jsonl
 * which mirrors the path that copy_copilot_session_state.sh produces and that
 * unified_timeline.cjs reads.
 *
 * Consumed directly by copilot_sdk_driver.cjs (the built-in gh-aw driver) and
 * available to any custom driver that wants the same session lifecycle and JSONL
 * telemetry without duplicating the implementation.
 */

"use strict";

const { getErrorMessage } = require("./error_helpers.cjs");
const fs = require("fs");
const path = require("path");
const os = require("os");
const { buildCopilotSDKPermissionHandler, getEnvPositiveIntOrDefault, parseMaxToolDenialsLimit, MAX_TOOL_DENIALS_DEFAULT } = require("./copilot_sdk_permissions.cjs");
const { buildCopilotSDKSessionToolConfig } = require("./copilot_sdk_tool_config.cjs");
const { resolveModelWithFallback } = require("./model_fallback.cjs");
const { extractShellCommandFromToolData } = require("./tool_call_details.cjs");

// Default timeout for a single sendAndWait call: 10 minutes.
// This is intentionally generous — the headless Copilot CLI has its own internal
// timeouts for individual tool calls and model inference.
// Override via the COPILOT_SDK_SEND_TIMEOUT_MS environment variable.
const SDK_SEND_TIMEOUT_MS_DEFAULT = 10 * 60 * 1000;

// Pattern matching the SDK idle-timeout error emitted when sendAndWait reaches its
// deadline waiting for the session.idle event.  This matches the message format
// "Timeout after <N>ms waiting for session.idle" produced by the Copilot SDK.
// Keep in sync with SDK_SESSION_IDLE_TIMEOUT_PATTERN in copilot_harness.cjs.
const SDK_IDLE_TIMEOUT_PATTERN = /Timeout after \d+ms waiting for session\.idle/;

// Default idle period for the post-completion watchdog: 30 seconds.
// When the agent has produced output and all tracked tool calls have completed,
// the driver arms a watchdog timer.  If no new SDK events arrive within this
// window, the driver force-disconnects the session and treats it as a successful
// completion — covering the SDK driver bug where sendAndWait never resolves after
// the final tool result is returned.
// 30 s is chosen to be well under typical step timeouts (≥ 5 min) while still
// being long enough to avoid false-positives during legitimate LLM inference gaps
// (the session.on handler disarms the watchdog on every assistant.turn_start event,
// so the timer only fires when the session is truly quiescent: output collected,
// no tool calls in flight, and no active inference turn).
// Override via the GH_AW_SDK_IDLE_MS environment variable.
const SDK_POST_COMPLETION_IDLE_MS_DEFAULT = 30 * 1000;

/**
 * Extract the prompt text from a resolved args array.
 * Looks for the first occurrence of "-p <value>" or "--prompt <value>".
 *
 * @param {string[]} args - Resolved args (after resolvePromptFileArgs has run).
 * @returns {string | null} The prompt text, or null if not found.
 */
function extractPromptFromArgs(args) {
  for (let i = 0; i < args.length - 1; i++) {
    if (args[i] === "-p" || args[i] === "--prompt") {
      return args[i + 1];
    }
  }
  return null;
}

/**
 * Run a Copilot agentic session using the @github/copilot-sdk.
 *
 * Connects to the already-running headless Copilot CLI server at sdkUri, creates
 * a session, sends the prompt, waits for the session to go idle, and returns a
 * result shape that mirrors what runProcess() returns so that callers can treat
 * both modes uniformly.
 *
 * All SDK events are serialised to a JSONL file under the session state directory
 * so that unified_timeline.cjs can render them in the step summary.
 *
 * @param {{
 *   sdkUri: string,
 *   prompt: string,
 *   logger: (msg: string) => void,
 *   attempt?: number,
 *   model?: string,
 *   connectionToken?: string,
 *   providers?: import("@github/copilot-sdk").NamedProviderConfig[],
 *   models?: import("@github/copilot-sdk").ProviderModelConfig[],
 *   maxToolDenials?: number | string,
 *   permissionConfig?: {
 *     allowAllTools?: boolean,
 *     allowedTools?: string[],
 *   },
 *   toolConfig?: import("./copilot_sdk_tool_config.cjs").CopilotSDKToolConfig,
 *   webFetchOptions?: {fetchImpl?: typeof fetch, timeoutMs?: number, maxRedirects?: number},
 *   coreLogger?: import("./copilot_sdk_permissions.cjs").CopilotSDKCoreLogger,
 *   sdkModule?: {
 *     CopilotClient: typeof import("@github/copilot-sdk").CopilotClient,
 *     RuntimeConnection: typeof import("@github/copilot-sdk").RuntimeConnection,
 *     approveAll: typeof import("@github/copilot-sdk").approveAll,
 *     ToolSet?: typeof import("@github/copilot-sdk").ToolSet,
 *     BuiltInTools?: typeof import("@github/copilot-sdk").BuiltInTools,
 *     defineTool?: typeof import("@github/copilot-sdk").defineTool,
 *   },
 *   sessionStateBaseDir?: string,
 * }} options
 * @returns {Promise<{exitCode: number, output: string, hasOutput: boolean, durationMs: number}>}
 */
async function runWithCopilotSDK({
  sdkUri,
  prompt,
  logger,
  attempt = 0,
  model,
  connectionToken,
  providers,
  models: providerModels,
  maxToolDenials,
  permissionConfig,
  toolConfig,
  webFetchOptions,
  coreLogger,
  sdkModule,
  sessionStateBaseDir,
}) {
  // Lazy-require to avoid loading the SDK when it is not needed.
  // The SDK is large and has side-effects on import (worker threads, etc.).
  const sdk = sdkModule ?? require("@github/copilot-sdk");
  const { CopilotClient, RuntimeConnection, approveAll } = sdk;

  const startTime = Date.now();
  let output = "";
  let hasOutput = false;

  const log = msg => logger(`[sdk-driver] ${msg}`);
  log(`attempt ${attempt + 1}: connecting to Copilot SDK at ${sdkUri}`);
  let maxToolDenialsLimit = MAX_TOOL_DENIALS_DEFAULT;
  if (maxToolDenials === undefined) {
    maxToolDenialsLimit = getEnvPositiveIntOrDefault("GH_AW_MAX_TOOL_DENIALS", MAX_TOOL_DENIALS_DEFAULT);
  } else {
    maxToolDenialsLimit = parseMaxToolDenialsLimit(maxToolDenials);
  }
  log(`max-tool-denials threshold: ${maxToolDenialsLimit}`);

  // Session state directory — mirrors the target path used by unified_timeline.cjs.
  // /tmp/gh-aw/sandbox/agent/logs/copilot-session-state/{sessionId}/events.jsonl
  // GH_AW_SESSION_STATE_BASE_DIR may be set in tests to redirect writes to an isolated directory.
  const defaultSessionStateBase = path.join(os.tmpdir(), "gh-aw", "sandbox", "agent", "logs", "copilot-session-state");
  const sessionStateBase = sessionStateBaseDir ?? process.env.GH_AW_SESSION_STATE_BASE_DIR ?? defaultSessionStateBase;

  /** @type {ReadonlyArray<NonNullable<import("@github/copilot-sdk").CopilotClientOptions["logLevel"]>>} */
  const VALID_LOG_LEVELS = ["none", "error", "warning", "info", "debug", "all"];
  const rawLogLevel = process.env.COPILOT_SDK_LOG_LEVEL ?? "";
  /**
   * @param {string} value
   * @returns {value is NonNullable<import("@github/copilot-sdk").CopilotClientOptions["logLevel"]>}
   */
  const isValidLogLevel = value => {
    /** @type {readonly string[]} */
    const validLogLevels = VALID_LOG_LEVELS;
    return validLogLevels.includes(value);
  };
  /** @type {import("@github/copilot-sdk").CopilotClientOptions["logLevel"]} */
  const logLevel = isValidLogLevel(rawLogLevel) ? rawLogLevel : "warning";

  const connection = RuntimeConnection.forUri(sdkUri, {
    connectionToken,
  });
  const client = new CopilotClient({
    connection,
    workingDirectory: process.env.GITHUB_WORKSPACE || process.cwd(),
    logLevel,
  });
  /** @type {any} */
  let session = null;
  /** @type {fs.WriteStream | null} */
  let eventsStream = null;
  let clientStarted = false;
  let toolDenialCount = 0;
  let assistantTurnCount = 0;
  /** @type {any} */
  let catastrophicToolDenialsError = null;
  let catastrophicToolDenialsTriggered = false;
  /**
   * Map from toolCallId → {toolName, mcpServerName} for enriching tool.execution_complete
   * events and for tracking in-flight tool calls when the idle-timeout fires.
   * Declared at function scope so the catch block can check pendingToolCalls.size.
   * @type {Map<string, {toolName: string, mcpServerName: string}>}
   */
  const pendingToolCalls = new Map();

  // Post-completion idle watchdog.
  // When the agent has produced output and all tracked tool calls have completed,
  // this timer is armed.  If no new SDK events arrive within GH_AW_SDK_IDLE_MS
  // (default 5 minutes), the watchdog force-disconnects the session and the catch
  // block treats the result as a successful completion.  This bounds the damage
  // from the SDK driver bug where sendAndWait never resolves after the final
  // tool result is returned.
  const postCompletionIdleMs = getEnvPositiveIntOrDefault("GH_AW_SDK_IDLE_MS", SDK_POST_COMPLETION_IDLE_MS_DEFAULT);
  let postCompletionWatchdogTriggered = false;
  /** @type {ReturnType<typeof setTimeout> | null} */
  let postCompletionWatchdog = null;
  // Tracks whether the LLM is mid-inference (between assistant.turn_start and
  // assistant.turn_end).  The watchdog must not fire during this window because
  // pendingToolCalls may legitimately be empty before the model dispatches its
  // first tool call of the new turn.
  let inAssistantTurn = false;

  /**
   * Best-effort write of a driver-level event to events.jsonl and stderr.
   * @param {string} type
   * @param {any} data
   */
  function writeDriverEvent(type, data) {
    const entry = { type, timestamp: new Date().toISOString(), data };
    const jsonl = JSON.stringify(entry) + "\n";
    if (eventsStream) {
      eventsStream.write(jsonl);
    }
    process.stderr.write(jsonl);
  }

  /**
   * @param {string} reason
   */
  function recordToolDenial(reason) {
    toolDenialCount += 1;
    log(`tool denial ${toolDenialCount}/${maxToolDenialsLimit}: ${reason}`);
    if (catastrophicToolDenialsTriggered || toolDenialCount < maxToolDenialsLimit) {
      return;
    }
    catastrophicToolDenialsTriggered = true;
    catastrophicToolDenialsError = new Error(`max tool denials threshold reached (${toolDenialCount}/${maxToolDenialsLimit})`);
    writeDriverEvent("guard.tool_denials_exceeded", {
      denialCount: toolDenialCount,
      threshold: maxToolDenialsLimit,
      reason,
    });
    log(`${catastrophicToolDenialsError.message}; stopping SDK session early`);
    if (session) {
      void session.disconnect().catch(() => {
        // best-effort early stop
      });
    }
  }

  try {
    await client.start();
    clientStarted = true;
    log("client started");

    /**
     * Build the session on-permission handler from configuration input.
     * @type {import("@github/copilot-sdk").PermissionHandler}
     */
    const onPermissionRequest = buildCopilotSDKPermissionHandler(permissionConfig, approveAll, {
      coreLogger,
      logger: log,
      onDenied: requestSummary => recordToolDenial(`permission denied: ${requestSummary}`),
      workspaceRoot: process.env.GITHUB_WORKSPACE,
    });

    // Build session config using the multi-provider surface.
    /** @type {import("@github/copilot-sdk").SessionConfig} */
    const sessionConfig = {
      model: model || resolveModelWithFallback(process.env, "COPILOT_MODEL") || undefined,
      providers,
      models: providerModels,
      onPermissionRequest,
      ...buildCopilotSDKSessionToolConfig(toolConfig, sdk, webFetchOptions),
    };
    log(`creating session with model="${sessionConfig.model || "(none)"}" providers=${providers?.length ?? 0} models=${providerModels?.length ?? 0}`);
    session = await client.createSession(sessionConfig);
    log(`session created: sessionId=${session.sessionId}`);

    // Prepare JSONL output file for this session.
    const sessionDir = path.join(sessionStateBase, session.sessionId);
    fs.mkdirSync(sessionDir, { recursive: true });
    const eventsPath = path.join(sessionDir, "events.jsonl");
    eventsStream = fs.createWriteStream(eventsPath, { flags: "a" });
    // Snapshot to a non-null local for closure-safe writes (JSDoc nullability narrowing).
    const stream = eventsStream;
    log(`serialising SDK events to ${eventsPath}`);

    /**
     * Write one JSONL entry to the events file and stderr.
     * Uses the event's own ISO-8601 timestamp when available.
     *
     * @param {string} type
     * @param {any} data
     * @param {string | undefined} [timestamp]
     */
    function writeEvent(type, data, timestamp) {
      const entry = { type, timestamp: timestamp ?? new Date().toISOString(), data };
      const jsonl = JSON.stringify(entry) + "\n";
      stream.write(jsonl);
      process.stderr.write(jsonl);
    }

    // Subscribe to all session events and serialise the ones we care about.
    session.on(event => {
      // Skip transient events that are not persisted by the server.
      if (event.ephemeral) return;

      switch (event.type) {
        case "user.message":
          writeEvent("user.message", {}, event.timestamp);
          break;

        case "tool.execution_start": {
          const toolName = event.data?.toolName ?? "unknown";
          const mcpServerName = event.data?.mcpServerName ?? "";
          const toolCallId = event.data?.toolCallId;
          const command = extractShellCommandFromToolData(event.data);
          if (toolCallId) {
            pendingToolCalls.set(toolCallId, { toolName, mcpServerName });
          }
          const eventData = command ? { toolName, mcpServerName, command } : { toolName, mcpServerName };
          writeEvent("tool.execution_start", eventData, event.timestamp);
          break;
        }

        case "tool.execution_complete": {
          const toolCallId = event.data?.toolCallId;
          // Resolve toolName/mcpServerName from the matching start event when available.
          const pending = toolCallId ? pendingToolCalls.get(toolCallId) : undefined;
          const toolName = pending?.toolName ?? event.data?.toolDescription?.name ?? "unknown";
          const mcpServerName = pending?.mcpServerName ?? "";
          if (toolCallId) pendingToolCalls.delete(toolCallId);
          const success = event.data?.success ?? !event.data?.error;
          // Include result.content (concise LLM-facing output) so that the log
          // parser can render tool output previews from events.jsonl directly.
          const result = event.data?.result ?? undefined;
          // max-tool-denials intentionally tracks permission denials only.
          // Tool execution failures are still logged, but do not increment the guardrail counter.
          writeEvent("tool.execution_complete", { toolName, mcpServerName, success, result }, event.timestamp);
          break;
        }

        case "assistant.message": {
          const content = event.data?.content ?? "";
          if (content) {
            hasOutput = true;
            output += content;
            assistantTurnCount++;
          }
          writeEvent("assistant.message", { content }, event.timestamp);
          break;
        }

        case "assistant.turn_start":
          // LLM inference started for a new turn. Disarm the watchdog — the model
          // may not have dispatched any tool calls yet, so pendingToolCalls can be
          // empty while real work is still in progress.
          inAssistantTurn = true;
          if (postCompletionWatchdog) {
            clearTimeout(postCompletionWatchdog);
            postCompletionWatchdog = null;
          }
          break;

        case "assistant.turn_end":
          // LLM inference finished. Allow the watchdog to re-arm on the next event
          // that satisfies the completion conditions.
          inAssistantTurn = false;
          break;

        case "session.task_complete":
          writeEvent("session.task_complete", { success: event.data?.success, summary: event.data?.summary }, event.timestamp);
          break;

        case "subagent.started":
          writeEvent(
            "subagent.started",
            {
              agentName: event.data?.agentName,
              agentDisplayName: event.data?.agentDisplayName,
              toolCallId: event.data?.toolCallId,
            },
            event.timestamp
          );
          break;

        case "subagent.completed":
          writeEvent("subagent.completed", { agentName: event.data?.agentName, toolCallId: event.data?.toolCallId }, event.timestamp);
          break;

        case "subagent.failed":
          writeEvent(
            "subagent.failed",
            {
              agentName: event.data?.agentName,
              toolCallId: event.data?.toolCallId,
              error: event.data?.error,
            },
            event.timestamp
          );
          break;

        default:
          // Other event types are not consumed by unified_timeline.cjs; skip them.
          break;
      }

      // After processing each event, update the post-completion watchdog:
      // - Arm (or rearm) the watchdog when the session looks complete: output
      //   collected, no tool calls still in flight, and not mid-LLM-inference.
      // - Disarm the watchdog whenever the session is still mid-turn (a new
      //   tool call was just started, LLM inference is in progress, or no output yet).
      // The watchdog fires only if sendAndWait never resolves on its own after
      // the final tool result is returned — the common SDK post-completion hang.
      if (hasOutput && pendingToolCalls.size === 0 && !inAssistantTurn) {
        if (postCompletionWatchdog) clearTimeout(postCompletionWatchdog);
        postCompletionWatchdog = setTimeout(() => {
          postCompletionWatchdog = null;
          // Re-check conditions at fire time: a new tool call could have started
          // or a new turn could have begun between arming the watchdog and the
          // timer firing (race condition guard).
          if (!hasOutput || pendingToolCalls.size !== 0 || inAssistantTurn || !session) return;
          log(`warning: post-completion idle watchdog fired after ${postCompletionIdleMs}ms — force-disconnecting session`);
          postCompletionWatchdogTriggered = true;
          void session.disconnect().catch(err => {
            log(`warning: post-completion watchdog disconnect failed: ${getErrorMessage(err)}`);
          });
        }, postCompletionIdleMs);
      } else {
        if (postCompletionWatchdog) {
          clearTimeout(postCompletionWatchdog);
          postCompletionWatchdog = null;
        }
      }
    });

    log("sending prompt...");
    const sendTimeoutMs = getEnvPositiveIntOrDefault("COPILOT_SDK_SEND_TIMEOUT_MS", SDK_SEND_TIMEOUT_MS_DEFAULT);
    const result = await session.sendAndWait({ prompt }, sendTimeoutMs);

    if (catastrophicToolDenialsError) {
      throw catastrophicToolDenialsError;
    }

    // sendAndWait returns the last assistant.message event; capture its content
    // as a fallback in case the on() handler missed it.
    if (result && !hasOutput) {
      const content = result.data?.content ?? "";
      if (content) {
        output = content;
        hasOutput = true;
        assistantTurnCount++;
      }
    }

    const durationMs = Date.now() - startTime;
    log(`session completed: hasOutput=${hasOutput} assistantTurns=${assistantTurnCount} durationMs=${durationMs}`);

    return { exitCode: 0, output, hasOutput, durationMs };
  } catch (err) {
    const durationMs = Date.now() - startTime;
    const failure = catastrophicToolDenialsError ?? (err instanceof Error ? err : new Error(String(err)));
    log(`error: ${failure.message}`);

    // When the post-completion idle watchdog force-disconnected the session, the
    // agent's work is done — the SDK simply failed to resolve sendAndWait after
    // the final tool result was returned.  Treat it as a successful completion.
    if (postCompletionWatchdogTriggered && !catastrophicToolDenialsError && hasOutput && pendingToolCalls.size === 0) {
      log(`warning: post-completion watchdog triggered disconnect — treating as completed`);
      log(`session completed: hasOutput=${hasOutput} assistantTurns=${assistantTurnCount} durationMs=${durationMs}`);
      return { exitCode: 0, output, hasOutput, durationMs };
    }

    // When sendAndWait times out waiting for session.idle but the agent produced
    // output and all tracked tool calls have already completed, the session work is
    // done — the SDK simply failed to emit the idle signal.  Treat it as a successful
    // run so the harness does not classify it as a failure or waste retry attempts.
    const isIdleTimeout = !catastrophicToolDenialsError && SDK_IDLE_TIMEOUT_PATTERN.test(failure.message);
    if (isIdleTimeout && hasOutput && pendingToolCalls.size === 0) {
      log(`warning: SDK idle-timeout with collected output and no pending tool calls — treating as completed`);
      log(`session completed: hasOutput=${hasOutput} assistantTurns=${assistantTurnCount} durationMs=${durationMs}`);
      return { exitCode: 0, output, hasOutput, durationMs };
    }

    // Preserve any output collected before the error so the harness can use it
    // for retry decisions and diagnostics.
    return {
      exitCode: 1,
      output: hasOutput ? output : failure.message,
      hasOutput,
      durationMs,
    };
  } finally {
    // Clear the post-completion watchdog if it has not already fired.
    if (postCompletionWatchdog) {
      clearTimeout(postCompletionWatchdog);
      postCompletionWatchdog = null;
    }
    // Snapshot for null-safe cleanup in this scope.
    const stream = eventsStream;
    if (stream) {
      await new Promise(resolve => stream.end(resolve));
    }
    // Timeout budget for each cleanup operation.  Prevents the driver from
    // hanging indefinitely when the server is unresponsive after sendAndWait
    // times out or the watchdog force-disconnects the session.
    // The helper resolves normally on success and also on timeout (signalling
    // timeout via the returned boolean) so the caller can log a warning.
    const CLEANUP_TIMEOUT_MS = 5_000;
    /**
     * Race a cleanup promise against a fixed deadline.
     * Resolves to true when the promise settled in time, false on timeout.
     * @param {Promise<unknown>} p
     * @returns {Promise<boolean>}
     */
    const withCleanupTimeout = p => {
      /** @type {any} */
      let timeoutId = null;
      const deadline = new Promise(resolve => {
        timeoutId = setTimeout(() => {
          timeoutId = null;
          resolve(false);
        }, CLEANUP_TIMEOUT_MS);
        if (typeof timeoutId?.unref === "function") timeoutId.unref();
      });
      return Promise.race([
        p.then(
          () => true,
          () => true
        ),
        deadline,
      ]).then(settled => {
        if (timeoutId) clearTimeout(timeoutId);
        if (!settled) {
          log(`warning: cleanup operation timed out after ${CLEANUP_TIMEOUT_MS}ms`);
        }
        return settled;
      });
    };
    if (session) {
      try {
        await withCleanupTimeout(session.disconnect());
      } catch {
        // best-effort cleanup
      }
    }
    if (clientStarted) {
      try {
        await withCleanupTimeout(client.stop());
      } catch {
        // best-effort cleanup
      }
    }
  }
}

module.exports = { SDK_SEND_TIMEOUT_MS_DEFAULT, SDK_POST_COMPLETION_IDLE_MS_DEFAULT, SDK_IDLE_TIMEOUT_PATTERN, extractPromptFromArgs, runWithCopilotSDK };
