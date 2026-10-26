import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { generatePlainTextSummary, generateCopilotCliStyleSummary, generateConversationMarkdown, formatToolUse, formatInitializationSummary, MAX_STEP_SUMMARY_SIZE } from "./log_parser_shared.cjs";
import { publishUnifiedSessionSummary, validateSessionFileHeader } from "./unified_session_render.cjs";
import { MCP_GATEWAY_CONFIG_PATHS } from "./redact_secrets.cjs";
import { main, mergeSessionSources } from "./unified_session.cjs";
import { normalizeClaudeSession } from "./claude_session.cjs";
import { dynamicWorkflow } from "./fixtures/claude_dynamic_workflow.cjs";

function event(type, data, component, index, timestampMs, sourcePath = `${component}.jsonl`) {
  return { type, data, provenance: { component, phase: component === "agent" ? "agent" : "conclusion", path: sourcePath, index, ...(timestampMs !== undefined ? { timestampMs } : {}) } };
}
const header = event("session.format", { version: 1 }, "collector", 0);
const trace = [
  header,
  event("tool.execution_start", { toolName: "lookup", toolCallId: "shared" }, "agent", 0, 1, "session-a.jsonl"),
  event("tool.execution_start", { toolName: "lookup", toolCallId: "shared" }, "agent", 0, 2, "session-b.jsonl"),
  event("mcp.rpc.request", { server_id: "github", method: "tools/call", payload: { id: 0, params: { name: "list_issues", arguments: { prompt: "PRIVATE_RPC_ARGUMENT" } } } }, "mcp", 0, 3),
  event("firewall.http_access", { host: "example.com", method: "CONNECT", status: 403, decision: "TCP_DENIED" }, "firewall", 0, 4),
  event("tool.execution_complete", { toolCallId: "shared", success: false, output: "failed" }, "agent", 1, 5, "session-a.jsonl"),
  event("tool.execution_complete", { toolCallId: "shared", success: true, output: "done" }, "agent", 1, 6, "session-b.jsonl"),
  event("user.message", { content: "PRIVATE_USER_PROMPT" }, "agent", 2, 7, "session-a.jsonl"),
  event("prompt.system", { content: "PRIVATE_SYSTEM_INSTRUCTIONS" }, "prompt", 0, undefined, "aw-prompts/system.txt"),
  event("prompt.user", { content: "PRIVATE_SPLIT_USER_PROMPT" }, "prompt", 0, undefined, "aw-prompts/user.txt"),
  event("assistant.message", { content: "Done <details>.\n" }, "agent", 3, 8, "session-a.jsonl"),
  event("session.result", { numTurns: 1, usage: { input_tokens: 0, output_tokens: 2 } }, "agent", 4, 9, "session-a.jsonl"),
  event("session.result", { numTurns: 2, usage: { input_tokens: 10, output_tokens: 3 } }, "agent", 2, 10, "session-b.jsonl"),
  event("mcp.rpc.response", { server_id: "github", payload: { id: 0, result: { content: "PRIVATE_RPC_RESPONSE" } } }, "mcp", 1, 11),
  event("mcp.difc.filtered", { tool_name: "lookup", reason: "policy" }, "mcp", 2, 12),
  event("mcp.guard.blocked", { server_name: "github", reason: "guard" }, "mcp", 3, 13),
  event("mcp.tool_call", { tool_name: "get_issue", status: "failed", duration: 0 }, "mcp", 4, 14),
  event("mcp.event", { event: "future", payload: "PRIVATE_EXTENSION" }, "mcp", 5, 15),
  event("firewall.token_usage", { input_tokens: 999, output_tokens: 5, ai_credits_total: 0 }, "firewall", 1, 16),
  event("firewall.steering", { eventName: "token_steering", message: "budget warning" }, "firewall", 2, 17),
  event("firewall.event", { event: "TRACK_END", status: 200, body: "PRIVATE_EXTENSION" }, "firewall", 3, 18),
  event("safe_output.request", { type: "create_issue", title: "PRIVATE_REQUEST_TITLE" }, "safe_output", 0, 19),
  event("safe_output.result", { type: "create_issue", number: 1, repo: "example/repo" }, "safe_output", 1, 20),
  event("safe_output.error", { errors: [{ error: "PRIVATE_ERROR_CONTEXT" }] }, "safe_output", 2, 21),
  event("experiment.assignment", { experiment: "B" }, "experiment", 0, 22),
  event("experiment.state", { run_id: "1", assignments: { experiment: "B" } }, "experiment", 1, 23),
  event("grader.manifest", { graders: [{ id: "quality", script: "PRIVATE_EVALUATOR" }] }, "grader", 0),
  event("grader.result", { results: [{ id: "quality", value: 0, unit: "ratio", passed: false, message: "PRIVATE_GRADER_MESSAGE" }] }, "grader", 1),
  event("eval.result", { id: "builds", answer: "no", question: "PRIVATE_EVAL_QUESTION" }, "eval", 0),
  event("usage.report", { input_tokens: 999, output_tokens: 50 }, "usage", 0),
  event("execution.result", { outcome: "failure", duration_ms: 0 }, "execution", 0),
  event("agent.execution", { categories: ["agentic_engine_timeout"], errorCodes: [502], errorTypes: ["server_error"], exitCode: 0 }, "execution", 1),
  event("detection.result", { job_result: "success", conclusion: "warning", reason: "threat_detected", prompt_injection: true, secret_leak: false, malicious_patch: false, reasons: "PRIVATE_DETECTION_REASON" }, "detection", 0),
  event("workflow.info", { engine_id: "copilot", model: "fixture", run_id: 1 }, "workflow", 0),
  event("guardrail.admission", { status: "admitted", maxAiCredits: 18372645, threatDetectionMaxAiCredits: 18372646, evalsMaxAiCredits: 18372647 }, "guardrail", 0),
  event("model_routing.outcome", { status: "selected", wireModel: "claude-sonnet-5", effectiveEndpoint: "/v1/messages", selectedEndpoint: "/chat/completions", appliedEffort: "medium" }, "agent", 6),
  event("vendor.progress", { private: "PRIVATE_EXTENSION" }, "agent", 5, undefined, "session-a.jsonl"),
  event("session.collection_warning", { path: "gateway.jsonl", line: 2, code: "malformed_jsonl" }, "collector", 1),
  event("session.collection", { sources: [], warnings: 1, untimedEvents: 10, absentComponents: [] }, "collector", 2),
];

describe("unified session publication views", () => {
  it("renders safe-output execution status alongside its canonical resource URI", () => {
    const events = [header, event("safe_output.result", { type: "publish", status: "success", url: "https://example.com/resources/42" }, "safe_output", 0)];
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events), generateConversationMarkdown(events, { formatToolCallback: formatToolUse, formatInitCallback: formatInitializationSummary }).markdown]) {
      expect(output).toContain("status=success");
      expect(output).toContain("https://example.com/resources/42");
    }
  });

  it("renders model inventories with provider, full model list, and supported endpoints", () => {
    const events = [
      header,
      event(
        "model.inventory",
        { endpoints: [{ provider: "copilot", models: ["gpt-5.6-luna", "claude-haiku-4.5"], routingModels: [{ modelId: "gpt-5.6-luna", supportedEndpoints: ["/responses"] }], token: "PRIVATE_TOKEN" }], modelsFetchComplete: false },
        "firewall",
        0
      ),
    ];
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events), generateConversationMarkdown(events, { formatToolCallback: formatToolUse, formatInitCallback: formatInitializationSummary }).markdown]) {
      expect(output).toContain("model.inventory");
      expect(output).toContain("copilot");
      expect(output).toContain("gpt-5.6-luna");
      expect(output).toContain("claude-haiku-4.5");
      expect(output).toContain("/responses");
      expect(output).toContain("modelsFetchComplete=false");
      expect(output).not.toContain("PRIVATE_TOKEN");
    }
  });

  it("displays task identity and observed errors without inventing tool completion", () => {
    const events = [header, event("tool.execution_update", { toolCallId: "shell", taskId: "background", taskType: "local_bash", status: "failed", error: "observed failure" }, "agent", 0)];
    for (const render of [generatePlainTextSummary, generateCopilotCliStyleSummary]) {
      const output = render(events);
      expect(output).toContain("taskId=background");
      expect(output).toContain("taskType=local_bash");
      expect(output).toContain("status=failed");
      expect(output).toContain("observed failure");
      expect(output).toContain("[updated]");
      expect(output).not.toContain("tool.execution_complete");
    }
  });

  let previousEnv;
  let previousCore;
  let directory;
  beforeEach(() => {
    previousEnv = { ...process.env };
    previousCore = global.core;
    global.core = { info: vi.fn(), warning: vi.fn() };
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-unified-render-"));
  });
  afterEach(() => {
    process.env = previousEnv;
    global.core = previousCore;
    vi.restoreAllMocks();
    fs.rmSync(directory, { recursive: true, force: true });
  });

  it("renders model/endpoint mismatch cause and fix in both publication views", () => {
    const events = [
      header,
      event(
        "model_endpoint.mismatch",
        {
          category: "model_endpoint_mismatch",
          phase: "runtime",
          configured_model: "alias",
          resolved_model: "resolved",
          wire_api: "responses",
          wire_api_source: "engine",
          supported_endpoints: ["/chat/completions"],
          model: "provider-model",
          endpoint: "/responses",
          detail: "Unsupported API",
          fix: "Select a compatible model",
        },
        "model_endpoint",
        0,
        undefined,
        "agent/model-endpoint-mismatch.json"
      ),
    ];
    for (const summary of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      expect(summary).toContain("model_endpoint.mismatch");
      expect(summary).toContain("cause=Unsupported API");
      expect(summary).toContain("fix=Select a compatible model");
      expect(summary).toContain("wire_api_source=engine");
      expect(summary).toContain("/chat/completions");
      expect(summary).toContain("model=provider-model");
      expect(summary).toContain("endpoint=/responses");
    }
  });

  it("fences and redacts hostile mismatch model, endpoint, and detail values", () => {
    process.env.GH_AW_SECRET_NAMES = "MISMATCH_TEST";
    process.env.SECRET_MISMATCH_TEST = "mismatch-secret-fixture";
    const hostile = "````\n# heading\n<img src=x>\n::error::oops mismatch-secret-fixture";
    const events = [
      header,
      event(
        "model_endpoint.mismatch",
        {
          category: "model_endpoint_mismatch",
          phase: "runtime",
          configured_model: hostile,
          resolved_model: hostile,
          wire_api: hostile,
          wire_api_source: hostile,
          supported_endpoints: [hostile],
          model: hostile,
          endpoint: hostile,
          detail: hostile,
          fix: hostile,
        },
        "model_endpoint",
        0,
        undefined,
        "agent/model-endpoint-mismatch.json"
      ),
    ];
    const markdown = generateCopilotCliStyleSummary(events);
    const plain = generatePlainTextSummary(events);
    for (const summary of [markdown, plain]) expect(summary).not.toContain("mismatch-secret-fixture");
    expect(markdown).toContain("<img src=x>");
    const opening = markdown.match(/^(`{3,})text$/m)?.[1];
    expect(opening.length).toBeGreaterThan(4);
    expect(plain).not.toContain("::error::");
  });

  it.each(['{"query":"PRIVATE_TOOL_ARGUMENT_DELTA"}', { query: "PRIVATE_TOOL_ARGUMENT_DELTA" }])("renders standard diagnostics without dumping tool delta %j, inputs or error context", delta => {
    const events = [
      header,
      event(
        "session.error",
        { severity: "warning", message: "Temporary diagnostic", code: 0, statusCode: 503, error: { code: "provider_error", message: "Provider unavailable", context: "PRIVATE_ERROR_CONTEXT" }, stack: "PRIVATE_STACK" },
        "agent",
        0
      ),
      event("session.info", { status: "retrying", message: "Retry scheduled" }, "agent", 1),
      event("tool.execution_update", { toolName: "lookup", stepIndex: 0, partial: true, delta, input: "PRIVATE_TOOL_INPUT", output: "PRIVATE_TOOL_OUTPUT" }, "agent", 2),
      event("mcp.event", { event: "extension_start_failure", serverName: "github", status: "error" }, "mcp", 0),
    ];
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      expect(output).toContain("severity=warning");
      expect(output).toContain("Temporary diagnostic");
      expect(output).toContain("statusCode=503");
      expect(output).toContain("Provider unavailable");
      expect(output).toContain("status=retrying");
      expect(output).toContain("toolName=lookup stepIndex=0 partial=true [updated]");
      expect(output).toContain("event=extension_start_failure serverName=github status=error");
      expect(output).not.toContain("PRIVATE_");
      expect(output).not.toContain("[extension payload omitted]");
    }
    expect(events[3].data.delta).toEqual(delta);
  });

  it.each(["PRIVATE_SESSION_CONTENT", { payload: "PRIVATE_SESSION_CONTENT" }, ["PRIVATE_SESSION_CONTENT"]])("omits opaque session content %j and structured metadata", content => {
    const events = [
      header,
      event("session.info", { content, status: "retrying", isAuthenticating: false, message: { private: "PRIVATE_INFO_MESSAGE" }, rateLimitInfo: ["PRIVATE_RATE_LIMIT"] }, "agent", 0),
      event("session.error", { content, code: 0, exitCode: 1, message: ["PRIVATE_ERROR_MESSAGE"], error: { code: "provider_error", message: "Provider unavailable", type: { private: "PRIVATE_ERROR_TYPE" } } }, "agent", 1),
      event("session.error", { content, error: ["PRIVATE_ERROR_ARRAY"] }, "agent", 2),
    ];
    const original = structuredClone(events);
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      expect(output).toContain("session.info status=retrying isAuthenticating=false");
      expect(output).toContain("session.error code=0 exitCode=1 code=provider_error message=Provider unavailable");
      expect(output).not.toContain("PRIVATE_");
      expect(output).not.toContain("content=");
      expect(output).not.toContain("rateLimitInfo=");
    }
    expect(events).toEqual(original);
  });

  it("renders caller context alongside episode lineage in both sinks", () => {
    const events = [
      header,
      event(
        "workflow.info",
        {
          run_id: "300",
          run_attempt: "2",
          workflow_id: "worker.yml",
          workflow_call_id: "300-2:worker.yml",
          caller_workflow_ref: "org/parent/.github/workflows/caller.yml@refs/heads/main",
          ref: "refs/heads/main",
          sha: "abc123",
          actor: "octocat",
          target_repo: "org/target",
          context: {
            episode_id: "100-1:root",
            hop_id: "200-1:caller",
            parent_hop_id: "",
            origin_event: "issues",
            root_repo: "org/repo",
            root_workflow_id: "root.yml",
            root_run_id: "100",
            workflow_call_id: "200-1:caller",
            repo: "org/parent",
            run_id: "200",
            run_attempt: "1",
            workflow_id: "caller.yml",
            event_type: "workflow_dispatch",
            work_queue_assignment: { payload: { enabled: false, count: 0, optional: null } },
          },
        },
        "workflow",
        0
      ),
    ];
    const normalized = mergeSessionSources([{ component: "workflow", phase: "activation", path: "aw_info.json", events: events.slice(1) }]);
    const original = structuredClone(normalized);
    for (const records of [events, [header, ...normalized]]) {
      for (const output of [generatePlainTextSummary(records), generateCopilotCliStyleSummary(records)]) {
        expect(output).toContain("runId=300");
        expect(output).toContain("runAttempt=2");
        expect(output).toContain("workflowId=worker.yml");
        expect(output).toContain("workflowCallId=300-2:worker.yml");
        expect(output).toContain("callerWorkflowRef=org/parent/.github/workflows/caller.yml@refs/heads/main");
        expect(output).toContain("ref=refs/heads/main");
        expect(output).toContain("sha=abc123");
        expect(output).toContain("actor=octocat");
        expect(output).toContain("targetRepo=org/target");
        expect(output).toContain("episodeId=100-1:root");
        expect(output).toContain("hopId=200-1:caller");
        expect(output).toContain("parentHopId=");
        expect(output).toContain("originEvent=issues");
        expect(output).toContain("rootRepo=org/repo");
        expect(output).toContain("rootWorkflowId=root.yml");
        expect(output).toContain("rootRunId=100");
        expect(output).toContain("parentRepo=org/parent");
        expect(output).toContain("parentRunId=200");
        expect(output).toContain("parentRunAttempt=1");
        expect(output).toContain("parentWorkflowId=caller.yml");
        expect(output).toContain("parentWorkflowCallId=200-1:caller");
        expect(output).toContain("parentEventType=workflow_dispatch");
        expect(output).toContain(`awContext=${JSON.stringify(normalized[0].data.awContext)}`);
      }
    }
    expect(normalized).toEqual(original);
  });

  it("renders complete JSON context without interpreting content or text fields as messages", () => {
    const awContext = { text: "Caller note", content: "Caller content", run_id: "100", nested: { values: [false, 0, null] } };
    const events = [header, event("workflow.info", { awContext }, "workflow", 0)];
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      expect(output).toContain(`awContext=${JSON.stringify(awContext)}`);
    }
  });

  it.each([{}, { awContext: {} }, { awContext: null }, { awContext: [] }])("renders absent or empty caller context consistently: %j", data => {
    const events = [header, event("workflow.info", data, "workflow", 0)];
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      if (data.awContext && !Array.isArray(data.awContext)) expect(output).toContain("awContext={}");
      else expect(output).not.toContain("awContext=");
    }
  });

  it("redacts caller context keys and nested values and safely fences hostile context", () => {
    const secret = 'caller-secret"\\\nvalue';
    process.env.GH_AW_SECRET_NAMES = "CALLER_CONTEXT";
    process.env.SECRET_CALLER_CONTEXT = secret;
    const awContext = { [secret]: { values: [secret] }, note: "````\n</details>\n::error::untrusted" };
    const events = [header, event("workflow.info", { awContext }, "workflow", 0)];
    const original = structuredClone(events);
    const plain = generatePlainTextSummary(events);
    const markdown = generateCopilotCliStyleSummary(events);
    for (const output of [plain, markdown]) {
      expect(output).toContain('awContext={"***REDACTED***":{"values":["***REDACTED***"]}');
      expect(output).not.toContain("caller-secret");
    }
    expect(plain).not.toContain("::error::");
    expect(markdown.match(/^(`{3,})text$/m)?.[1].length).toBeGreaterThan(4);
    expect(events).toEqual(original);
  });

  it("bounds large caller context using existing line and summary byte limits", () => {
    const awContext = { note: "context ".repeat(10000) };
    const events = [header, event("workflow.info", { awContext }, "workflow", 0)];
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      expect(output).toContain('awContext={"note":');
      expect(output).toContain("[truncated: long summary line]");
      expect(Buffer.byteLength(output, "utf8")).toBeLessThan(MAX_STEP_SUMMARY_SIZE);
    }
    const bounded = generateCopilotCliStyleSummary(events, { maxBytes: 1024 });
    expect(Buffer.byteLength(bounded, "utf8")).toBeLessThanOrEqual(1024);
    expect(bounded).toContain("summary truncated: byte limit reached");
    expect(events[1].data.awContext).toEqual(awContext);
  });

  it("renders every known message in both sinks without dumping prompts or opaque payloads", () => {
    const original = structuredClone(trace);
    for (const output of [generatePlainTextSummary(trace), generateCopilotCliStyleSummary(trace)]) {
      expect(output).toContain("File format version: 1");
      for (const type of new Set(trace.map(record => record.type))) {
        if (!["user.message", "prompt.system", "prompt.user"].includes(type)) expect(output).toContain(type);
      }
      expect(output).toContain("list_issues");
      expect(output).toContain("rpcId=0");
      expect(output).toContain("TCP_DENIED");
      expect(output).toContain("budget warning");
      expect(output).toContain("effectiveEndpoint=/v1/messages");
      expect(output).toContain("selectedEndpoint=/chat/completions");
      expect(output).toContain("[requested, not executed]");
      expect(output).toContain("[execution recorded]");
      expect(output).toContain("value=0 unit=ratio passed=false");
      expect(output).toContain("answer=no");
      expect(output).toContain("durationMs=0");
      expect(output).toContain('categories=["agentic_engine_timeout"] errorCodes=[502] errorTypes=["server_error"] exitCode=0');
      expect(output).toContain("secretLeak=false");
      expect(output).toContain("jobResult=success conclusion=warning reason=threat_detected promptInjection=true secretLeak=false maliciousPatch=false");
      expect(output).toContain("untimedEvents=10");
      expect(output).toContain("malformed_jsonl");
      expect(output).toContain("status=admitted (allowance values available in session JSONL)");
      for (const amount of ["18372645", "18372646", "18372647"]) expect(output).not.toContain(amount);
      expect(output).not.toContain("PRIVATE_");
      expect(output.indexOf("mcp.rpc.request")).toBeLessThan(output.indexOf("firewall.http_access"));
      expect(output.indexOf("firewall.http_access")).toBeLessThan(output.indexOf("mcp.rpc.response"));
      expect(output).toContain("untimed");
    }
    expect(trace).toEqual(original);
    const markdown = generateCopilotCliStyleSummary(trace);
    expect(markdown).toContain("Done <details>");
    expect(markdown).toMatch(/^<details><summary>Unified session<\/summary>\n\n/);
    expect(markdown).not.toContain("### Unified session");
    expect(markdown).not.toContain("=== Unified session ===");
    expect(markdown).not.toContain("| Time |");
  });

  it("leads with a source-local conversation and keeps statistics and runtime metadata collapsed", () => {
    const events = [
      header,
      event("tool.execution_start", { toolName: "Bash", toolCallId: "shared", input: { command: "echo first" } }, "agent", 0, 1, "first.jsonl"),
      event("tool.execution_start", { toolName: "Bash", toolCallId: "shared", input: { command: "echo second" } }, "agent", 0, 2, "second.jsonl"),
      event("assistant.message", { content: "First answer\nwith two lines." }, "agent", 2, 3, "first.jsonl"),
      event("tool.execution_complete", { toolCallId: "shared", success: true, output: "first output" }, "agent", 1, 4, "first.jsonl"),
      event("tool.execution_complete", { toolCallId: "shared", success: false, error: { message: "second failure" } }, "agent", 1, 5, "second.jsonl"),
      event("firewall.http_access", { host: "example.com", status: 403 }, "firewall", 0, 6),
    ];
    const original = structuredClone(events);
    const markdown = generateCopilotCliStyleSummary(events);
    const conversation = markdown.split("### Conversation\n\n")[1].split("<details><summary>Full timeline</summary>")[0];
    const first = conversation.split("Agent source: agent/first.jsonl")[1].split("Agent source: agent/second.jsonl")[0];
    const second = conversation.split("Agent source: agent/second.jsonl")[1];
    expect(first).toContain("$ echo first");
    expect(first).toContain("first output");
    expect(first.indexOf("$ echo first")).toBeLessThan(first.indexOf("First answer"));
    expect(first).toContain("with two lines.");
    expect(first).not.toContain("second failure");
    expect(second).toContain("$ echo second");
    expect(second).toContain("second failure");
    expect(second).not.toContain("first output");
    expect(conversation).not.toContain("tool.execution");
    expect(conversation).not.toContain("firewall.http_access");
    expect(conversation).not.toContain("Statistics:");
    expect(markdown).toContain("<details><summary>Session overview</summary>");
    expect(markdown).not.toContain("<details open");
    expect(events).toEqual(original);
  });

  it("explains an absent conversation without inventing agent activity", () => {
    const markdown = generateCopilotCliStyleSummary([header, event("execution.result", { outcome: "failure" }, "execution", 0)]);
    expect(markdown).toContain("### Conversation\n\n```text\nNo agent conversation recorded.\n```");
    expect(markdown).toContain("outcome=failure");
    expect(markdown).not.toContain("Agent source:");
  });

  it.each([128, 512, 1024, 2048, 4096])("keeps all generated containers balanced within a %i-byte budget", maxBytes => {
    const events = [header, event("assistant.message", { content: "multibyte \u{1f600} ".repeat(2000) }, "agent", 0, 0)];
    const markdown = generateCopilotCliStyleSummary(events, { maxBytes });
    expect(Buffer.byteLength(markdown, "utf8")).toBeLessThanOrEqual(maxBytes);
    expect(markdown.match(/<details>/g)?.length ?? 0).toBe(markdown.match(/<\/details>/g)?.length ?? 0);
    expect(markdown.match(/^```(?:text)?$/gm)?.length ?? 0).toBe(markdown.includes("### Conversation") ? 6 : 0);
    expect(markdown).not.toContain("\ufffd");
    expect(events[1].data.content).toHaveLength("multibyte \u{1f600} ".repeat(2000).length);
  });

  it.each(["completed", "failed", "stopped"])("distinguishes a Workflow launch from task status %s and renders progress without embedded prompts", status => {
    const records = dynamicWorkflow.map(record => (record.subtype === "task_notification" ? { ...record, status } : record));
    const events = mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-stdio.log", events: normalizeClaudeSession(records) }]);
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      expect(output).toContain("[launch succeeded; workflow outcome pending]");
      expect(output).toContain("workflowName=smoke-claude-dynamic workflowRunId=dynamic-run");
      expect(output).toContain("dynamicWorkflows.task_started taskId=dynamic-task toolCallId=workflow-tool taskType=local_workflow");
      expect(output).toContain("dynamicWorkflows.task_progress taskId=dynamic-task toolCallId=workflow-tool totalTokens=0 toolUses=0 durationMs=0");
      expect(output).toContain("agentId=dynamic-agent model=claude-sonnet-4-6 state=done");
      expect(output).toContain(`dynamicWorkflows.task_notification taskId=dynamic-task toolCallId=workflow-tool status=${status}`);
      expect(output).toContain("dynamicWorkflows.background_tasks_changed tasks=[]");
      expect(output).toContain("Tokens: 18 total (7 in / 11 out)");
      expect(output).not.toContain("PRIVATE_");
    }
  });

  it("renders parent-agent hierarchy without confusing it with event-chain parent correlation", () => {
    const events = [
      header,
      { ...event("subagent.started", { agentName: "child", parentId: "parent-id" }, "agent", 1, 1), agentId: "child-id", parentId: "previous-event-id" },
      { ...event("subagent.started", { agentName: "root" }, "agent", 2, 2), agentId: "root-id", parentId: "another-event-id" },
    ];
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      expect(output).toContain("subagent.started agentId=child-id agentName=child parentId=parent-id");
      expect(output).not.toContain("parentId=previous-event-id");
      expect(output).not.toContain("parentId=another-event-id");
    }
  });

  it("scopes agent pairing, snapshots and accounting without adding firewall usage", () => {
    const output = generatePlainTextSummary(trace);
    const first = output.slice(output.indexOf("Agent source: agent/session-a.jsonl"), output.indexOf("Agent source: agent/session-b.jsonl"));
    const second = output.slice(output.indexOf("Agent source: agent/session-b.jsonl"), output.indexOf("Chronological trace"));
    expect(first).toContain("Tools: 0/1 succeeded");
    expect(first).toContain("Failed Tools: 1");
    expect(first).toContain("Tokens: 2 total (0 in / 2 out)");
    expect(second).toContain("Tools: 1/1 succeeded");
    expect(second).toContain("Tokens: 13 total (10 in / 3 out)");
    expect(first + second).not.toContain("999");
  });

  it("collapses streamed text into one timeline message, preserving the native record count", () => {
    const events = [header, ...["Hello", " ", "world."].map((content, index) => event("assistant.message", { content, delta: true, messageId: "answer" }, "agent", index, index + 1))];
    const original = structuredClone(events);
    for (const [output, copies] of [
      [generatePlainTextSummary(events), 1],
      [generateCopilotCliStyleSummary(events), 2],
    ]) {
      expect(output.match(/◆ Hello world\./g)).toHaveLength(copies);
      expect(output.match(/agent\/agent assistant\.message /g)).toHaveLength(1);
      expect(output).toContain("Records: 4");
      expect(output).toContain("agent/agent assistant.message +0s");
      expect(output).not.toContain("1970-01-01");
    }
    expect(events).toEqual(original);
  });

  it("does not merge message identities that publication redaction collapses", () => {
    process.env.GH_AW_SECRET_NAMES = "FIRST_ID,SECOND_ID";
    process.env.SECRET_FIRST_ID = "secret-first-message";
    process.env.SECRET_SECOND_ID = "secret-second-message";
    const events = [
      header,
      event("assistant.message", { content: "First message", delta: true, messageId: "secret-first-message" }, "agent", 0, 1),
      event("assistant.message", { content: "Second message", delta: true, messageId: "secret-second-message" }, "agent", 1, 2),
    ];
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      expect(output).not.toContain("First messageSecond message");
      expect(output.match(/agent\/agent assistant\.message /g)).toHaveLength(2);
      expect(output).not.toContain("secret-first-message");
      expect(output).not.toContain("secret-second-message");
    }
  });

  it("collapses interleaved streams only within their source and conversation", () => {
    const scopes = [
      { path: "first.jsonl", sessionId: "root", parentToolUseId: null, content: "Root " },
      { path: "first.jsonl", sessionId: "root", parentToolUseId: "child", content: "Child " },
      { path: "first.jsonl", sessionId: "second", parentToolUseId: null, content: "Second " },
      { path: "other.jsonl", sessionId: "root", parentToolUseId: null, content: "Other " },
    ];
    const events = [
      header,
      ...scopes.map((scope, index) => event("assistant.message", { content: scope.content, delta: true, messageId: "reused", sessionId: scope.sessionId, parentToolUseId: scope.parentToolUseId }, "agent", 0, index + 1, scope.path)),
      event("firewall.http_access", { host: "example.com", status: 200 }, "firewall", 0, 5),
      ...scopes.map((scope, index) => event("assistant.message", { content: "answer", delta: true, messageId: "reused", sessionId: scope.sessionId, parentToolUseId: scope.parentToolUseId }, "agent", 1, index + 6, scope.path)),
    ];
    const original = structuredClone(events);
    for (const [output, copies] of [
      [generatePlainTextSummary(events), 1],
      [generateCopilotCliStyleSummary(events), 2],
    ]) {
      for (const scope of scopes) expect(output.split(`◆ ${scope.content}answer`)).toHaveLength(copies + 1);
      expect(output.match(/agent\/agent assistant\.message /g)).toHaveLength(4);
      expect(output).toContain("Records: 10");
      expect(output.indexOf("◆ Root answer")).toBeLessThan(output.indexOf("◆ Child answer"));
      expect(output.indexOf("◆ Other answer")).toBeLessThan(output.indexOf("firewall/conclusion firewall.http_access"));
    }
    expect(events).toEqual(original);
  });

  it("redacts secrets formed by joining individually innocuous stream fragments", () => {
    process.env.GH_AW_SECRET_NAMES = "STREAM";
    process.env.SECRET_STREAM = "cross-stream-secret-value";
    const events = [header, ...["Result: cross-stream-", "secret-value", "."].map((content, index) => event("assistant.message", { content, delta: true }, "agent", index, index + 1))];
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      expect(output).toContain("Result: ");
      expect(output).not.toContain("cross-stream-secret-value");
    }
    expect(events[1].data.content).toBe("Result: cross-stream-");
  });

  it("retains private pairing keys until projection so redaction cannot merge distinct tool calls", () => {
    process.env.GH_AW_SECRET_NAMES = "FIRST_ID,SECOND_ID";
    process.env.SECRET_FIRST_ID = "secret-first-tool";
    process.env.SECRET_SECOND_ID = "secret-second-tool";
    const events = [
      header,
      event("tool.execution_start", { toolName: "first", toolCallId: "secret-first-tool" }, "agent", 0, 0),
      event("tool.execution_start", { toolName: "second", toolCallId: "secret-second-tool" }, "agent", 1, 1),
      event("tool.execution_complete", { toolCallId: "secret-first-tool", success: false }, "agent", 2, 2),
      event("tool.execution_complete", { toolCallId: "secret-second-tool", success: true }, "agent", 3, 3),
    ];
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      expect(output).toContain("Tools: 1/2 succeeded");
      expect(output).toContain("Failed Tools: 1");
      expect(output).not.toContain("secret-first-tool");
      expect(output).not.toContain("secret-second-tool");
    }
    const projection = generateConversationMarkdown(events, { formatToolCallback: formatToolUse, formatInitCallback: formatInitializationSummary });
    expect(projection.commandSummary[0]).toContain("❌");
    expect(projection.commandSummary[1]).toContain("✅");
  });

  it("uses the unified view through the conversation formatter without breaking source tool summaries", () => {
    const result = generateConversationMarkdown(trace, { formatToolCallback: formatToolUse, formatInitCallback: formatInitializationSummary });
    expect(result.markdown).toContain("firewall.http_access");
    expect(result.commandSummary).toHaveLength(2);
    expect(result.sizeLimitReached).toBe(false);
    expect(result.markdown).not.toContain("PRIVATE_");
  });

  it("renders multiline messages, tool arguments, output and errors in the full timeline", () => {
    const events = [
      header,
      event("user.message", { content: "PRIVATE_USER_PROMPT" }, "agent", 0, 0),
      event("assistant.reasoning", { content: "First thought\nSecond thought" }, "agent", 1, 1),
      event("assistant.message", { content: "First answer\nSecond answer" }, "agent", 2, 2),
      event("tool.execution_start", { toolName: "Bash", toolCallId: "shell", input: { command: "printf fixture-command", description: "Run fixture" } }, "agent", 3, 3),
      event("tool.execution_complete", { toolCallId: "shell", success: false, output: "fixture-output", error: { code: "DENIED", message: "fixture-error" }, durationMs: 0 }, "agent", 4, 4),
      event("tool.execution_start", { toolName: "lookup", toolCallId: "pending", input: { query: "fixture-query" } }, "agent", 5, 5),
      event("tool.execution_complete", { toolName: "lookup", toolCallId: "orphan", success: true, output: "" }, "agent", 6, 6),
      event("mcp.rpc.request", { method: "tools/call", arguments: { query: "PRIVATE_RPC_ARGUMENT" } }, "mcp", 0, 7),
    ];
    const original = structuredClone(events);
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events), generateConversationMarkdown(events, { formatToolCallback: formatToolUse, formatInitCallback: formatInitializationSummary }).markdown]) {
      expect(output).toContain("Chronological trace");
      expect(output).toContain("path=agent.jsonl");
      expect(output).toContain("◐ First thought\n  Second thought");
      expect(output).toContain("◆ First answer\n  Second answer");
      expect(output).toContain("✗ $ printf fixture-command");
      expect(output).toContain("fixture-output");
      expect(output).toContain("Error:");
      expect(output).toContain("fixture-error");
      expect(output).toContain("DENIED");
      expect(output).toContain("fixture-query");
      expect(output).toContain("[pending]");
      expect(output).toContain("[start unavailable]");
      expect(output).toContain("[empty output]");
      expect(output.indexOf("assistant.reasoning")).toBeLessThan(output.indexOf("assistant.message"));
      expect(output.indexOf("fixture-output")).toBeLessThan(output.indexOf("mcp.rpc.request"));
      expect(output).not.toContain("PRIVATE_");
    }
    expect(events).toEqual(original);
  });

  it("keeps tool pairing source-local while displaying the merged timeline order", () => {
    const events = [
      header,
      event("tool.execution_complete", { toolCallId: "shared", success: true, output: "second source result" }, "agent", 1, 0, "second.jsonl"),
      event("tool.execution_complete", { toolCallId: "shared", success: false, output: "first source result" }, "agent", 1, 1, "first.jsonl"),
      event("tool.execution_start", { toolCallId: "shared", toolName: "first", input: false }, "agent", 0, 2, "first.jsonl"),
      event("tool.execution_start", { toolCallId: "shared", toolName: "second", input: null }, "agent", 0, 3, "second.jsonl"),
    ];
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      const timeline = output.slice(output.indexOf("Chronological trace"));
      const second = timeline.slice(timeline.indexOf("[2]"), timeline.indexOf("[3]"));
      const first = timeline.slice(timeline.indexOf("[3]"), timeline.indexOf("[4]"));
      expect(second).toContain("✓ second(null)");
      expect(second).toContain("second source result");
      expect(second).not.toContain("first source result");
      expect(first).toContain("✗ first(false)");
      expect(first).toContain("first source result");
      expect(first).not.toContain("second source result");
    }
  });

  it.each([
    [{ stepIndex: 0 }, { stepIndex: 0 }],
    [
      { toolCallId: "", stepIndex: 1 },
      { toolCallId: "", stepIndex: 2 },
    ],
    [{ toolCallId: 123, stepIndex: 0 }, { stepIndex: 0 }],
    [{}, {}],
    [{ toolName: "bash" }, { toolName: "Bash" }],
    [{ toolName: "lookup", mcpServerName: "github" }, { toolName: "mcp__github__lookup" }],
  ])("pairs tools using scoped ID, step, or unique normalized name: %j / %j", (startData, completeData) => {
    const events = [
      header,
      event("tool.execution_start", { toolName: "lookup", ...startData, input: { command: "printf fixture", query: "paired-input" } }, "agent", 0, 2000),
      event("tool.execution_complete", { toolName: "lookup", ...completeData, success: true, output: "paired-output" }, "agent", 1, 1000),
    ];
    const original = structuredClone(events);
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events), generateConversationMarkdown(events, { formatToolCallback: formatToolUse, formatInitCallback: formatInitializationSummary }).markdown]) {
      const timeline = output.slice(output.indexOf("Chronological trace"));
      const completion = timeline.slice(timeline.indexOf("[3]"));
      expect(completion).toContain("✓");
      expect(completion).toContain("paired-output");
      expect(completion).toContain(startData.toolName?.toLowerCase() === "bash" ? "printf fixture" : "paired-input");
      expect(completion).not.toContain("[start unavailable]");
      expect(completion).not.toContain("[pending]");
    }
    expect(events).toEqual(original);
  });

  it.each([{ toolCallId: "missing", stepIndex: 0 }, { stepIndex: 1 }, {}, { stepIndex: -1 }, { stepIndex: 0.5 }])("does not guess a tool pairing for unmatched identities or ambiguous names: %j", completionData => {
    const events = [
      header,
      event("tool.execution_start", { toolName: "lookup", stepIndex: 0, input: "first-input" }, "agent", 0, 0),
      event("tool.execution_start", { toolName: "lookup", stepIndex: 2, input: "second-input" }, "agent", 1, 1),
      event("tool.execution_complete", { toolName: "lookup", ...completionData, success: true, output: "orphan-output" }, "agent", 2, 2),
    ];
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      const timeline = output.slice(output.indexOf("Chronological trace"));
      const completion = timeline.slice(timeline.indexOf("[4]"));
      expect(completion).toContain("[start unavailable]");
      expect(completion).toContain("orphan-output");
      expect(completion).not.toContain("first-input");
      expect(completion).not.toContain("second-input");
    }
  });

  it.each([{ stepIndex: 0 }, {}])("isolates fallback pairing by source, phase and conversation: %j", correlation => {
    const scopes = [
      { path: "first.jsonl", phase: "agent", sessionId: "root", parentToolUseId: null },
      { path: "other.jsonl", phase: "agent", sessionId: "root", parentToolUseId: null },
      { path: "first.jsonl", phase: "retry", sessionId: "root", parentToolUseId: null },
      { path: "first.jsonl", phase: "agent", sessionId: "second", parentToolUseId: null },
      { path: "first.jsonl", phase: "agent", sessionId: "root", parentToolUseId: "child" },
    ];
    const scoped = (scope, index, type, data) => ({
      ...event(type, { toolName: "lookup", ...correlation, sessionId: scope.sessionId, parentToolUseId: scope.parentToolUseId, ...data }, "agent", index, index, scope.path),
      provenance: { ...event(type, {}, "agent", index, index, scope.path).provenance, phase: scope.phase },
    });
    const events = [
      header,
      ...scopes.map((scope, index) => scoped(scope, 0, "tool.execution_start", { input: `scope-input-${index}` })),
      ...scopes.map((scope, index) => scoped(scope, 1, "tool.execution_complete", { success: true, output: `scope-output-${index}` })),
    ];
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      const timeline = output.slice(output.indexOf("Chronological trace"));
      for (const index of scopes.keys()) {
        const completion = timeline.split(`[${index + 7}]`)[1].split(/\n\[\d+\]/)[0];
        expect(completion).toContain(`✓ lookup("scope-input-${index}")`);
        expect(completion).toContain(`scope-output-${index}`);
        expect(completion).not.toContain("[start unavailable]");
        for (const other of scopes.keys()) {
          if (other !== index) expect(completion).not.toContain(`scope-input-${other}`);
        }
      }
    }
  });

  it("keeps the display clock monotonic across regressions, omitted prompts and untimed records", () => {
    const events = [
      header,
      event("assistant.message", { content: "First" }, "agent", 0, 5000),
      event("assistant.message", { content: "Regressed" }, "agent", 1, 3000),
      event("vendor.progress", {}, "agent", 2),
      event("user.message", { content: "PRIVATE_PROMPT" }, "agent", 3, 20000),
      event("assistant.message", { content: "Still behind" }, "agent", 4, 4000),
      event("assistant.message", { content: "Caught up" }, "agent", 5, 6000),
    ];
    const original = structuredClone(events);
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      const timeline = output.slice(output.indexOf("Chronological trace"));
      expect(timeline.match(/assistant\.message \+\d+s/g)).toEqual(["assistant.message +0s", "assistant.message +0s", "assistant.message +0s", "assistant.message +1s"]);
      expect(timeline).toContain("vendor.progress [extension payload omitted] [untimed]");
      expect(timeline.indexOf("First")).toBeLessThan(timeline.indexOf("Regressed"));
      expect(timeline.indexOf("Regressed")).toBeLessThan(timeline.indexOf("Still behind"));
      expect(timeline).not.toMatch(/ -\d+(?:\.\d+)?s/);
      expect(timeline).not.toContain("PRIVATE_PROMPT");
    }
    expect(events).toEqual(original);
  });

  it("interleaves runtime events and messages, showing results only at completion with elapsed suffixes", () => {
    const events = [
      header,
      event("assistant.message", { content: "Starting work" }, "agent", 0, 1000),
      event("tool.execution_start", { toolName: "Bash", toolCallId: "shell", input: { command: "printf done" } }, "agent", 1, 1250),
      event("firewall.http_access", { host: "example.com", decision: "allowed" }, "firewall", 0, 2500),
      event("assistant.reasoning", { content: "Waiting for the command" }, "agent", 2, 2500),
      event("tool.execution_complete", { toolCallId: "shell", success: true, output: "Command finished" }, "agent", 3, 4500),
      event("vendor.progress", { content: "PRIVATE_EXTENSION" }, "agent", 4),
      event("execution.result", { outcome: "success", startedAt: "2026-10-11T00:00:00Z", finishedAt: "2026-10-11T00:01:00Z", durationMs: 60000 }, "execution", 0),
    ];
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      const timeline = output.slice(output.indexOf("Chronological trace"));
      expect(timeline.indexOf("Starting work")).toBeLessThan(timeline.indexOf("tool.execution_start"));
      expect(timeline.indexOf("tool.execution_start")).toBeLessThan(timeline.indexOf("firewall.http_access"));
      expect(timeline.indexOf("firewall.http_access")).toBeLessThan(timeline.indexOf("Waiting for the command"));
      expect(timeline.indexOf("Waiting for the command")).toBeLessThan(timeline.indexOf("tool.execution_complete"));
      expect(timeline.indexOf("tool.execution_complete")).toBeLessThan(timeline.indexOf("Command finished"));
      expect(timeline.slice(0, timeline.indexOf("tool.execution_complete"))).not.toContain("✓");
      expect(timeline).toContain("assistant.message +0s");
      expect(timeline).toContain("[started] +0.25s");
      expect(timeline).toContain("decision=allowed +1.25s");
      expect(timeline).toContain("assistant.reasoning +0s");
      expect(timeline).toContain("[succeeded] +2s");
      expect(timeline).toContain("vendor.progress [extension payload omitted] [untimed]");
      expect(timeline).not.toMatch(/\d{4}-\d{2}-\d{2}T/);
      expect(timeline).not.toContain("PRIVATE_EXTENSION");
      expect(output).not.toContain("Agent conversation:");
    }
  });

  it("renders update, model, guardrail, permission and skill observations without their private envelopes", () => {
    const events = [
      header,
      event("assistant.message_update", { sourceType: "content_block_delta", contentIndex: 0, partial: true, content: "PRIVATE_UPDATE", usageSnapshot: { inputTokens: 0, outputTokens: 2 } }, "agent", 0),
      event("model.call_final_result", { model: "fixture", isByok: false, result: { status: "success", stopReason: "end_turn", usage: { inputTokens: 0, outputTokens: 42 }, content: "PRIVATE_MODEL" } }, "agent", 1),
      event("guardrail.daily_aic", { status: "checked", exceeded: false, total: 0, estimated: 2, threshold: 10 }, "guardrail", 0),
      event("session.mode_changed", { previousMode: "plan", newMode: "interactive" }, "agent", 2),
      event("assistant.turn_start", { turnId: "turn-1", interactionId: "interaction-1" }, "agent", 3),
      event("assistant.turn_end", { turnId: "turn-1" }, "agent", 4),
      event("permission.requested", { requestId: "request-1", permissionRequest: { kind: "shell", toolCallId: "shell", fullCommandText: "PRIVATE_PERMISSION" } }, "agent", 5),
      event("permission.completed", { requestId: "request-1", toolCallId: "shell", result: { kind: "approved" }, decisionSource: "user" }, "agent", 6),
      event("skill.invoked", { name: "developer", content: "PRIVATE_SKILL", source: "project" }, "agent", 7),
      event("skill.context_delivered_ref", { contentId: "skill-1", prefix: "PRIVATE_PREFIX" }, "agent", 8),
      event("system.message", { content: "PRIVATE_SYSTEM" }, "agent", 9),
    ];
    const original = structuredClone(events);
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      for (const record of events) expect(output).toContain(record.type);
      expect(output).toContain("sourceType=content_block_delta contentIndex=0 partial=true");
      expect(output).toContain("model=fixture isByok=false status=success stopReason=end_turn inputTokens=0 outputTokens=42");
      expect(output).toContain("exceeded=false total=0 estimated=2 threshold=10");
      expect(output).toContain("previousMode=plan newMode=interactive");
      expect(output).toContain("kind=approved");
      expect(output).toContain("name=developer source=project");
      expect(output).not.toContain("PRIVATE_");
      expect(output).not.toContain("[extension payload omitted]");
    }
    expect(events).toEqual(original);
  });

  it("keeps distinct source scopes when redaction makes their publication labels identical", () => {
    const firstPath = "private-first-source.jsonl";
    const secondPath = "private-second-source.jsonl";
    process.env.GH_AW_SECRET_NAMES = "FIRST_PATH,SECOND_PATH";
    process.env.SECRET_FIRST_PATH = firstPath;
    process.env.SECRET_SECOND_PATH = secondPath;
    const events = [
      header,
      event("tool.execution_start", { toolCallId: "shared", toolName: "first" }, "agent", 0, 0, firstPath),
      event("tool.execution_start", { toolCallId: "shared", toolName: "second" }, "agent", 0, 1, secondPath),
      event("tool.execution_complete", { toolCallId: "shared", success: false, output: "first failed" }, "agent", 1, 2, firstPath),
      event("tool.execution_complete", { toolCallId: "shared", success: true, output: "second succeeded" }, "agent", 1, 3, secondPath),
    ];
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      expect(output).toContain("✗ first");
      expect(output).toContain("✓ second");
      expect(output).toContain("Tools: 0/1 succeeded");
      expect(output).toContain("Tools: 1/1 succeeded");
      expect(output).not.toContain(firstPath);
      expect(output).not.toContain(secondPath);
    }
  });

  it("rejects unsupported, missing or misplaced version headers without assuming compatibility", () => {
    expect(() => validateSessionFileHeader([])).toThrow("ERR_VALIDATION: Unified session file is missing");
    for (const version of [0, "1", 2, undefined]) {
      expect(() => generatePlainTextSummary([{ ...header, data: { version } }])).toThrow("ERR_VALIDATION: Unsupported");
    }
    expect(() => generateCopilotCliStyleSummary([trace[1], header])).toThrow("ERR_VALIDATION: Unified session file is missing");
    expect(() => generatePlainTextSummary([header, header])).toThrow("ERR_VALIDATION: Unified session file contains multiple");
  });

  it("redacts escaped secret leaves before shortening previews and neutralizes hostile markup/fences", () => {
    const secret = 'secret"\\\n' + "b".repeat(200);
    const token = "ghp_" + "a".repeat(36);
    process.env.GH_AW_SECRET_NAMES = "RENDER_KEY";
    process.env.SECRET_RENDER_KEY = secret;
    const events = [
      header,
      event("assistant.message", { content: `${secret} ${token}\n\`\`\`\`\`\`\`\`\`\n</details>\n::warning::untrusted` }, "agent", 0, 0),
      event("tool.execution_start", { toolCallId: "shell", toolName: "bash", input: { command: `echo ${token}` } }, "agent", 1, 1),
      event("tool.execution_complete", { toolCallId: "shell", success: false, output: token, error: { code: "DENIED", message: token } }, "agent", 2, 2),
    ];
    const original = structuredClone(events);
    const plain = generatePlainTextSummary(events);
    const markdown = generateCopilotCliStyleSummary(events);
    for (const output of [plain, markdown]) {
      expect(output).not.toContain(token);
      expect(output).not.toContain(secret.slice(0, 40));
      expect(output).toContain("***REDACTED***");
      expect(output).toContain("$ echo ***REDACTED***");
      expect(output).toContain("DENIED: ***REDACTED***");
      expect(output).not.toMatch(/^::warning::/m);
    }
    expect(markdown).toContain("\n  </details>\n");
    expect(markdown).toContain("\n``````````\n");
    expect(plain).not.toContain("::warning::");
    expect(events).toEqual(original);
  });

  it("keeps command operators and hostile markup literal inside balanced, payload-safe fences", () => {
    const events = [
      header,
      event("tool.execution_start", { toolName: "Bash", toolCallId: "shell", input: { command: "test -f file && cat file 2>/dev/null" } }, "agent", 0, 0),
      event("assistant.message", { content: "</details>\n<script>alert('fixture')</script>\n````\n# not a heading" }, "agent", 1, 1),
    ];
    const markdown = generateCopilotCliStyleSummary(events);
    const fences = [...markdown.matchAll(/^(`{5,})(?:text)?$/gm)];
    expect(fences).toHaveLength(6);
    expect(new Set(fences.map(match => match[1])).size).toBe(1);
    expect(markdown).toContain("&&");
    expect(markdown).toContain("2>/dev/null");
    expect(markdown).not.toContain("&amp;");
    expect(markdown).not.toContain("&gt;");
    expect(markdown.slice(fences[2].index, fences[3].index)).toContain("<script>");
    expect(markdown.slice(fences[4].index, fences[5].index)).toContain("<script>");
    expect(markdown.slice(fences[5].index + fences[5][0].length).trim()).toMatch(/^<\/details>\s+<\/details>$/);
  });

  it("clips oversized views with explicit notices without trimming the source artifact", () => {
    const events = [header, ...Array.from({ length: 2000 }, (_, index) => event("assistant.message", { content: "a".repeat(2000) }, "agent", index, index))];
    for (const output of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events)]) {
      expect(Buffer.byteLength(output)).toBeLessThanOrEqual(MAX_STEP_SUMMARY_SIZE);
      expect(output).toContain("summary truncated: byte limit reached");
    }
    expect(events.at(-1).data.content).toHaveLength(2000);
  });

  it("prioritizes conversation over the overview and keeps the timeline visible while collecting secrets once", () => {
    const existsSync = vi.spyOn(fs, "existsSync");
    const events = [header, ...Array.from({ length: 128 }, (_, index) => event("assistant.message", { content: "answer" }, "agent", 0, index, `source-${index}-${"p".repeat(9000)}.jsonl`))];
    const output = generateCopilotCliStyleSummary(events);
    const gatewayConfigChecks = existsSync.mock.calls.filter(([candidate]) => MCP_GATEWAY_CONFIG_PATHS.includes(candidate)).length;

    expect(Buffer.byteLength(output)).toBeLessThanOrEqual(MAX_STEP_SUMMARY_SIZE);
    expect(output).toContain("<summary>Session overview</summary>");
    expect(output).toContain("### Conversation");
    expect(output).toContain("Agent source: agent/source-0-");
    expect(output).toContain("◆ answer");
    expect(output).toContain("<summary>Full timeline</summary>");
    expect(output).toContain("Full timeline omitted: byte limit reached");
    expect(output).not.toContain("Conversation omitted: byte limit reached");
    expect(gatewayConfigChecks).toBe(MCP_GATEWAY_CONFIG_PATHS.length);
  });

  it("keeps tight summary budgets byte-safe, including omission notices", () => {
    const events = [header, ...Array.from({ length: 12 }, (_, index) => event("assistant.message", { content: "answer" }, "agent", 0, index, `source-${index}-${"p".repeat(2000)}.jsonl`))];

    for (const maxBytes of [0, 1, 32, 511, 900, 901, 1024]) {
      const output = generateCopilotCliStyleSummary(events, { maxBytes });
      expect(Buffer.byteLength(output, "utf8")).toBeLessThanOrEqual(maxBytes);
    }
  });

  it("publishes the final redacted file into logs and appends to the existing step summary", async () => {
    const summary = path.join(directory, "summary.md");
    const previousSummary = global.core.summary;
    global.core.summary = { addDetails: vi.fn(), write: vi.fn() };
    fs.writeFileSync(summary, "Earlier summary\n");
    process.env.GITHUB_STEP_SUMMARY = summary;
    fs.writeFileSync(path.join(directory, "agent-session.jsonl"), JSON.stringify({ type: "assistant.message", data: { content: "done" } }) + "\n");
    fs.mkdirSync(path.join(directory, "mcp-logs"));
    fs.writeFileSync(path.join(directory, "mcp-logs", "gateway.jsonl"), JSON.stringify({ event: "tool_call", tool_name: "lookup" }) + "\n");
    try {
      await main({ rootDir: directory });
    } finally {
      if (previousSummary === undefined) delete global.core.summary;
      else global.core.summary = previousSummary;
    }
    const artifact = fs.readFileSync(path.join(directory, "usage", "aw_session.jsonl"), "utf8");
    expect(JSON.parse(artifact.split("\n")[0]).data.version).toBe(1);
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("mcp.tool_call"));
    const output = fs.readFileSync(summary, "utf8");
    expect(output.startsWith("Earlier summary\n")).toBe(true);
    expect(output.match(/<summary>Unified session<\/summary>/g)).toHaveLength(1);
    expect(output).toContain("mcp.tool_call");
  });

  it("respects the remaining byte budget of an existing step summary and still publishes logs", async () => {
    const source = path.join(directory, "aw_session.jsonl");
    fs.writeFileSync(source, trace.map(record => JSON.stringify(record)).join("\n") + "\n");
    const summary = path.join(directory, "summary.md");
    const existing = "x".repeat(MAX_STEP_SUMMARY_SIZE - 10);
    fs.writeFileSync(summary, existing);
    process.env.GITHUB_STEP_SUMMARY = summary;
    await publishUnifiedSessionSummary(source);
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("firewall.http_access"));
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("byte limit"));
    expect(fs.readFileSync(summary, "utf8")).toBe(existing);
  });

  it("does not publish a stale or incomplete session when assembly fails before the atomic write completes", async () => {
    const summaryPath = path.join(directory, "summary.md");
    const sessionPath = path.join(directory, "usage", "aw_session.jsonl");
    process.env.GITHUB_STEP_SUMMARY = summaryPath;
    fs.writeFileSync(summaryPath, "Earlier summary\n");
    fs.mkdirSync(path.dirname(sessionPath));
    fs.writeFileSync(sessionPath, trace.map(record => JSON.stringify(record)).join("\n") + "\n");
    fs.writeFileSync(path.join(directory, "agent-session.jsonl"), JSON.stringify({ type: "assistant.message", data: { content: "new answer" } }) + "\n");
    vi.spyOn(fs, "renameSync").mockImplementationOnce(() => {
      throw new Error("atomic write failed");
    });

    await expect(main({ rootDir: directory })).rejects.toThrow("atomic write failed");
    expect(fs.readFileSync(summaryPath, "utf8")).toBe("Earlier summary\n");
    expect(fs.existsSync(sessionPath)).toBe(false);
    expect(fs.existsSync(`${sessionPath}.tmp`)).toBe(false);
    expect(global.core.info).not.toHaveBeenCalled();
  });

  it("reports publication I/O failures while retaining the valid unified artifact", async () => {
    const source = path.join(directory, "aw_session.jsonl");
    fs.writeFileSync(source, JSON.stringify(header) + "\n");
    process.env.GITHUB_STEP_SUMMARY = path.join(directory, "absent", "summary.md");
    await expect(publishUnifiedSessionSummary(source)).rejects.toThrow("ERR_SYSTEM: Failed to publish unified session");
    expect(fs.readFileSync(source, "utf8")).toBe(JSON.stringify(header) + "\n");
  });
});
