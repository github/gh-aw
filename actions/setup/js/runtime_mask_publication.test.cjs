import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { collectUnifiedSession, writeUnifiedSession } from "./unified_session.cjs";
import { publishUnifiedSessionSummary } from "./unified_session_render.cjs";

const redactScript = fs.readFileSync(path.join(import.meta.dirname, "redact_secrets.cjs"), "utf8");
const bootstrapScript = fs.readFileSync(path.join(import.meta.dirname, "log_parser_bootstrap.cjs"), "utf8");

describe("runtime mask publication boundary", () => {
  let root, otherRoot, originalEnv, originalCore;
  const opaque = 'dynamic"\\credential%0A';
  const secrets = [opaque, "runtime-line-one", "runtime-line-two", "runtime-line-three", "x$"];
  const text = `${opaque} runtime-line-one\nruntime-line-two\rruntime-line-three x$`;

  beforeEach(() => {
    originalEnv = { ...process.env };
    originalCore = global.core;
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-mask-publication-"));
    otherRoot = path.join(root, "runner-temp");
    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setFailed: vi.fn(),
      summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue() },
    };
    delete process.env.GH_AW_SECRET_NAMES;
    delete process.env.GITHUB_STEP_SUMMARY;
    delete process.env.GH_AW_SAFE_OUTPUTS;
  });

  afterEach(() => {
    process.env = originalEnv;
    global.core = originalCore;
    vi.restoreAllMocks();
    fs.rmSync(root, { recursive: true, force: true });
  });

  function write(file, value) {
    const target = path.join(root, file);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, typeof value === "string" ? value : JSON.stringify(value) + "\n");
    return target;
  }

  async function redactSources() {
    const script = redactScript.replace('findFiles("/tmp/gh-aw", targetExtensions)', "findFiles(root, targetExtensions)").replace("findFiles(`${process.env.RUNNER_TEMP}/gh-aw`, targetExtensions)", "findFiles(otherRoot, targetExtensions)");
    await eval(`(async () => { ${script}; await main(); })()`);
  }

  function expectSafe(value) {
    if (typeof value === "string") for (const secret of secrets) expect(value).not.toContain(secret);
    else if (Array.isArray(value)) value.forEach(expectSafe);
    else if (value && typeof value === "object")
      Object.entries(value).forEach(([key, nested]) => {
        expectSafe(key);
        expectSafe(nested);
      });
  }

  it("sanitizes all uploaded sources before bootstrap, then safely collects and renders with no mask handoff", async () => {
    const encoded = opaque.replace(/%/g, "%25");
    const builtin = "ghs_" + "a".repeat(40);
    write("agent-stdio.log", `::add-mask::${encoded}\n::add-mask:: runtime-line-one%0Aruntime-line-two%0Druntime-line-three \n::add-mask::x$\n::add-mask::${builtin}\n${text}\n`);
    const nativePath = write("sandbox/agent/logs/copilot-session-state/session/events.jsonl", { type: "assistant.message", id: opaque, data: { content: text }, timestamp: "2026-10-02T00:00:01Z" });
    const sources = [
      write("mcp-logs/rpc.jsonl", { event: "rpc_request", server_id: opaque, payload: { params: { arguments: { credential: text } } } }),
      write("sandbox/firewall/logs/audit.jsonl", { event: "token_steering", message: text }),
      write("safeoutputs.jsonl", { type: "add_comment", repo: opaque, body: text }),
      write("safe-output-items.jsonl", { type: "add_comment", repo: opaque, url: text }),
      write("safe-output-errors.json", { errors: [{ type: "add_comment", message: text }] }),
      write("experiments/assignments.json", { assignments: { variant: text } }),
      write("agent/graders/grader_results.json", { results: [{ id: opaque, error: text }] }),
      write("evals/evals.jsonl", { id: opaque, answer: text }),
      write("runner-temp/mcp-logs/rpc.jsonl", { payload: { secret: text, builtin } }),
      nativePath,
    ];
    await redactSources();
    expect(core.setFailed).not.toHaveBeenCalled();
    expect(fs.existsSync(path.join(root, "publication-masks.json"))).toBe(false);
    expect(fs.existsSync(path.join(root, "usage/publication-masks.json"))).toBe(false);

    process.env.GH_AW_AGENT_OUTPUT = nativePath;
    const script = bootstrapScript.replaceAll("/tmp/gh-aw", root);
    const parseLog = content => ({ markdown: "native session", logEntries: content.trim().split("\n").map(JSON.parse) });
    await eval(`(async () => { ${script}; await runLogParser({ parseLog, parserName: "Copilot" }); })()`);
    expect(fs.readFileSync(path.join(root, "agent-stdio.log"), "utf8")).not.toContain("::add-mask::");
    expectSafe(JSON.parse(fs.readFileSync(path.join(root, "agent-session.jsonl"), "utf8")));

    for (const useNativeFallback of [false, true]) {
      if (useNativeFallback) fs.unlinkSync(path.join(root, "agent-session.jsonl"));
      expect(collectUnifiedSession({ rootDir: root }).maskedValues).toEqual([]);
      const events = writeUnifiedSession({ rootDir: root });
      expect(events[0].data.version).toBe(1);
      expectSafe(events);
      await publishUnifiedSessionSummary(path.join(root, "usage/aw_session.jsonl"));
      expectSafe(core.info.mock.calls.flat());
      expectSafe(core.summary.addRaw.mock.calls.flat());
      const summaryPath = path.join(root, "step-summary.md");
      process.env.GITHUB_STEP_SUMMARY = summaryPath;
      await publishUnifiedSessionSummary(path.join(root, "usage/aw_session.jsonl"));
      expectSafe(fs.readFileSync(summaryPath, "utf8"));
      delete process.env.GITHUB_STEP_SUMMARY;
    }
    for (const file of sources) expectSafe(JSON.parse(fs.readFileSync(file, "utf8")));
    expect(fs.readFileSync(sources.at(-2), "utf8")).not.toContain(builtin);
  });

  it("sanitizes sources with oversized runtime masks without removing artifacts", async () => {
    const mask = "x".repeat(200000);
    const stdio = write("agent-stdio.log", `::add-mask::${mask}\noutput ${mask}\n`);
    const source = write("safeoutputs.jsonl", { body: mask });
    await redactSources();
    expect(core.setFailed).not.toHaveBeenCalled();
    expect(core.warning).not.toHaveBeenCalled();
    expect(fs.readFileSync(stdio, "utf8")).toBe("output ***\n");
    expect(JSON.parse(fs.readFileSync(source, "utf8"))).toEqual({ body: "***" });
  });

  it("sanitizes regex-shaped multiline gateway text without removing artifacts", async () => {
    const gatewayAgentId = "runtime-gateway-agent-id";
    const actualGatewayLines = [
      "MCP_GATEWAY_AGENT_ID=$(openssl rand -base64 45 | tr -d '/+=')",
      `echo "::add-mask::${gatewayAgentId}"`,
      "export MCP_GATEWAY_AGENT_ID",
      'export MCP_GATEWAY_PAYLOAD_DIR="/tmp/gh-aw/mcp-payloads"',
      'mkdir -p "/tmp/gh-aw/mcp-payloads"',
    ];
    const mask = actualGatewayLines.join("\n");
    const stdio = write("agent-stdio.log", `::add-mask::${mask.replace(/\n/g, "%0A")}\n${mask}\n`);
    const source = write("safeoutputs.jsonl", { body: mask });
    await redactSources();
    expect(core.setFailed).not.toHaveBeenCalled();
    expect(core.warning).not.toHaveBeenCalled();
    expect(fs.readFileSync(stdio, "utf8")).toBe("***\n***\n***\n***\n");
    expect(JSON.parse(fs.readFileSync(source, "utf8"))).toEqual({ body: "***\n***\n***\n***" });
  });

  it("removes a source on failed runtime-mask writes without logging masks and continues sanitizing the other files", async () => {
    write("agent-stdio.log", `::add-mask::${opaque.replace(/%/g, "%25")}\n`);
    const failing = write("mcp-logs/first.jsonl", { credential: opaque });
    const remaining = write("safeoutputs.jsonl", { body: opaque });
    const originalWrite = fs.writeFileSync;
    vi.spyOn(fs, "writeFileSync").mockImplementation((file, ...args) => {
      if (file === failing) throw new Error(`write denied: ${opaque}`);
      return originalWrite(file, ...args);
    });
    await redactSources();
    expect(fs.existsSync(failing)).toBe(false);
    expectSafe(JSON.parse(fs.readFileSync(remaining, "utf8")));
    expect(core.setFailed).toHaveBeenCalledWith(expect.stringContaining("Removed artifact source after runtime mask redaction failed"));
    expectSafe(core.warning.mock.calls.flat());
    expectSafe(core.setFailed.mock.calls.flat());
  });

  it("removes publication sources if stdio masks cannot be read", async () => {
    const stdio = write("agent-stdio.log", `::add-mask::${opaque.replace(/%/g, "%25")}\n`);
    const source = write("mcp-logs/rpc.jsonl", { credential: opaque });
    const originalRead = fs.readFileSync;
    vi.spyOn(fs, "readFileSync").mockImplementation((file, ...args) => {
      if (file === stdio) throw new Error("read denied");
      return originalRead(file, ...args);
    });
    await redactSources();
    expect(fs.existsSync(stdio)).toBe(false);
    expect(fs.existsSync(source)).toBe(false);
    expect(core.setFailed).toHaveBeenCalledWith(expect.stringContaining("runtime mask collection failed"));
  });
});
