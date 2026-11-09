// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { aggregateDecisions, evaluateSkipIfDecision, resolveQuestionContext, recordDecisionSkip } from "./skip_if_decision.cjs";

describe("skip-if-decision", () => {
  let root;
  beforeEach(() => {
    root = fs.mkdtempSync(path.join(os.tmpdir(), "skip-decision-"));
    vi.stubEnv("GITHUB_WORKSPACE", root);
    vi.stubEnv("GH_AW_ENGINE_CWD", "");
  });
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.restoreAllMocks();
    fs.rmSync(root, { recursive: true, force: true });
  });

  const outcomes = ["allow", "deny", "unknown"];
  const combinations = outcomes.flatMap(first => outcomes.flatMap(second => outcomes.map(third => [first, second, third])));
  it.each(combinations.map(answers => [answers, answers.includes("deny") ? "deny" : answers.includes("allow") ? "allow" : "unknown"]))("aggregates %j as %s", (answers, expected) => {
    expect(aggregateDecisions(answers)).toBe(expected);
  });
  it("treats an empty list as unknown", () => expect(aggregateDecisions([])).toBe("unknown"));

  function env(questions = ["Is the task done?", "Is further work unnecessary?"]) {
    return { GH_AW_SKIP_IF_DECISION: JSON.stringify(questions), AWF_REFLECT_ENABLED: "1" };
  }
  function endpoint() {
    return new Response(JSON.stringify({ endpoints: [{ configured: true, provider: "github", models_url: "http://api-proxy:10002/v1/models" }] }));
  }
  function decision(choices) {
    return new Response(JSON.stringify({ answers: choices.map((choice, index) => ({ name: `skip_${index + 1}`, type: "choice", choice })) }));
  }

  it.each([
    [["unknown", "unknown"], "unknown"],
    [["allow", "unknown"], "allow"],
    [["allow", "deny"], "deny"],
    [["deny", "unknown"], "deny"],
  ])("batches named outcomes %j as %s", async (choices, expected) => {
    const fetchImpl = vi.fn().mockResolvedValueOnce(endpoint()).mockResolvedValueOnce(decision(choices));
    const result = await evaluateSkipIfDecision({ env: env(), fetchImpl });
    expect(result).toMatchObject({ outcome: expected, latencyMs: expect.any(Number), questions: choices.map((outcome, index) => ({ question: index + 1, outcome })) });
    expect(fetchImpl).toHaveBeenCalledTimes(2);
    const [url, request] = fetchImpl.mock.calls[1];
    expect(url).toBe("http://api-proxy:10002/v1/decisions");
    expect(request.redirect).toBe("error");
    expect(request.headers.Authorization).toBeUndefined();
    expect(JSON.parse(request.body).questions).toHaveLength(2);
  });

  it("matches shuffled answers by name, not position", async () => {
    const response = {
      answers: [
        { type: "choice", name: "skip_2", choice: "deny" },
        { type: "choice", name: "skip_1", choice: "allow" },
      ],
    };
    const fetchImpl = vi
      .fn()
      .mockResolvedValueOnce(endpoint())
      .mockResolvedValueOnce(new Response(JSON.stringify(response)));
    expect(await evaluateSkipIfDecision({ env: env(), fetchImpl })).toMatchObject({ outcome: "deny", questions: [{ outcome: "allow" }, { outcome: "deny" }] });
  });

  it.each([
    ["HTTP 500", () => Promise.resolve(new Response("private provider body", { status: 500 })), "evaluation_failed"],
    ["HTTP 404", () => Promise.resolve(new Response("", { status: 404 })), "evaluation_failed"],
    ["unsupported model", () => Promise.resolve(new Response('{"error":{"code":"model_not_supported"}}', { status: 400 })), "model_unavailable"],
    ["network error", () => Promise.reject(new Error("private provider diagnostic")), "evaluation_failed"],
    ["timeout", () => Promise.reject(new DOMException("private timeout", "TimeoutError")), "timeout"],
    ["invalid JSON", () => Promise.resolve(new Response("{")), "evaluation_failed"],
    ["missing answers", () => Promise.resolve(new Response('{"answers":[]}')), "evaluation_failed"],
    ["invalid choice", () => Promise.resolve(decision(["deny", "not-an-outcome"])), "evaluation_failed"],
    [
      "duplicate answers",
      () =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              answers: [
                { type: "choice", name: "skip_1", choice: "deny" },
                { type: "choice", name: "skip_1", choice: "allow" },
              ],
            })
          )
        ),
      "evaluation_failed",
    ],
  ])("abstains on %s without retry or diagnostic disclosure", async (_name, response, error) => {
    const log = vi.fn();
    const fetchImpl = vi.fn().mockResolvedValueOnce(endpoint()).mockImplementationOnce(response);
    const result = await evaluateSkipIfDecision({ env: env(), fetchImpl, log });
    expect(result).toMatchObject({
      outcome: "unknown",
      error,
      questions: [
        { outcome: "unknown", error },
        { outcome: "unknown", error },
      ],
    });
    expect(fetchImpl).toHaveBeenCalledTimes(2);
    expect(JSON.stringify(result) + JSON.stringify(log.mock.calls)).not.toContain("private");
  });

  it("abstains on refusal without retry", async () => {
    const fetchImpl = vi.fn().mockResolvedValueOnce(endpoint()).mockResolvedValueOnce(new Response('{"answers":[{"type":"refusal","name":"skip_1"},{"type":"refusal","name":"skip_2"}]}'));
    expect(await evaluateSkipIfDecision({ env: env(), fetchImpl })).toMatchObject({ outcome: "unknown" });
    expect(fetchImpl).toHaveBeenCalledTimes(2);
  });

  it.each([{}, env([]), { ...env(), GH_AW_PHASE: "detection" }, { ...env(), GH_AW_PHASE: "evals" }])("does not call the model for absent, empty, or non-agent configuration %j", async env => {
    const fetchImpl = vi.fn();
    expect(await evaluateSkipIfDecision({ env, fetchImpl })).toBeNull();
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it("never falls back to direct API access outside AWF", async () => {
    const fetchImpl = vi.fn();
    expect(await evaluateSkipIfDecision({ env: { ...env(), AWF_REFLECT_ENABLED: "" }, fetchImpl })).toMatchObject({ outcome: "unknown", error: "sandbox_unavailable" });
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it("rejects public provider endpoints instead of bypassing AWF", async () => {
    const fetchImpl = vi.fn().mockResolvedValue(new Response(JSON.stringify({ endpoints: [{ configured: true, provider: "openai", models_url: "https://api.openai.com/v1/models" }] })));
    expect(await evaluateSkipIfDecision({ env: env(), fetchImpl })).toMatchObject({ outcome: "unknown", error: "invalid_proxy_endpoint" });
    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });

  it("uses the OpenAI Decisions model on a configured OpenAI proxy listener", async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ endpoints: [{ configured: true, provider: "openai", models_url: "http://api-proxy:10001/v1/models" }] })))
      .mockResolvedValueOnce(decision(["deny"]));
    expect(await evaluateSkipIfDecision({ env: env(["Is work complete?"]), fetchImpl })).toMatchObject({ outcome: "deny" });
    expect(JSON.parse(fetchImpl.mock.calls[1][1].body).model).toBe("gpt-6-luna");
  });

  it("reads runtime files fresh on every evaluation without retaining contents", async () => {
    fs.mkdirSync(path.join(root, "context"));
    const filename = path.join(root, "context/task.md");
    fs.writeFileSync(filename, "private: work remaining");
    const readFile = vi.fn(file => fs.readFileSync(file));
    const fetchImpl = vi.fn().mockImplementation(async (url, request) => {
      if (url.endsWith("/reflect")) return endpoint();
      const body = JSON.parse(request.body);
      return decision([body.questions[0].instructions[1].text.includes("work remaining") ? "allow" : "deny"]);
    });
    const options = { env: env(["Is there no work remaining in @context/task.md?"]), fetchImpl, readFile };
    expect(await evaluateSkipIfDecision(options)).toMatchObject({ outcome: "allow" });
    fs.writeFileSync(filename, "private: all work complete");
    const result = await evaluateSkipIfDecision(options);
    expect(result).toMatchObject({ outcome: "deny" });
    expect(readFile).toHaveBeenCalledTimes(2);
    expect(JSON.stringify(result)).not.toContain("private");
  });

  it("isolates failed references while batching the remaining questions", async () => {
    const readFile = vi.fn(() => {
      throw new Error("private missing file");
    });
    const fetchImpl = vi.fn().mockResolvedValueOnce(endpoint()).mockResolvedValueOnce(new Response('{"answers":[{"name":"skip_2","type":"choice","choice":"deny"}]}'));
    expect(await evaluateSkipIfDecision({ env: env(["Is @context/missing.md done?", "Is the task complete?"]), readFile, fetchImpl })).toMatchObject({
      outcome: "deny",
      questions: [
        { question: 1, outcome: "unknown", error: "file_reference_failed" },
        { question: 2, outcome: "deny" },
      ],
    });
    expect(JSON.parse(fetchImpl.mock.calls[1][1].body).questions.map(question => question.name)).toEqual(["skip_2"]);
  });

  it("does not call the model when all runtime files are unavailable", async () => {
    const fetchImpl = vi.fn();
    const result = await evaluateSkipIfDecision({ env: env(["Is @missing.md complete?"]), fetchImpl });
    expect(result).toMatchObject({ outcome: "unknown", questions: [{ error: "file_reference_failed" }] });
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it("confines file references before reading them", () => {
    const readFile = vi.fn();
    expect(() => resolveQuestionContext("Is @/etc/passwd complete?", readFile)).toThrow();
    expect(() => resolveQuestionContext("Is @../../outside.md complete?", readFile)).toThrow();
    expect(() => resolveQuestionContext("Is @.git/config complete?", readFile)).toThrow();
    expect(readFile).not.toHaveBeenCalled();
  });

  it.runIf(process.platform === "linux")("uses the real guarded reader for runtime snapshots and rejects symlinks and hardlinks", () => {
    const filename = path.join(root, "task.md");
    fs.writeFileSync(filename, "work complete");
    expect(resolveQuestionContext("Is @task.md complete?")[1]).toMatchObject({ text: expect.stringContaining("work complete") });
    fs.symlinkSync(filename, path.join(root, "link.md"));
    fs.linkSync(filename, path.join(root, "hardlink.md"));
    expect(() => resolveQuestionContext("Is @link.md complete?")).toThrow();
    expect(() => resolveQuestionContext("Is @hardlink.md complete?")).toThrow();
  });

  it("records an intentional noop without overwriting existing output", () => {
    const filename = path.join(root, "safeoutputs.jsonl");
    vi.stubEnv("GH_AW_SAFE_OUTPUTS", filename);
    recordDecisionSkip(() => {});
    const original = fs.readFileSync(filename, "utf8");
    expect(JSON.parse(original)).toMatchObject({ type: "noop" });
    recordDecisionSkip(() => {});
    expect(fs.readFileSync(filename, "utf8")).toBe(original);
  });
});
