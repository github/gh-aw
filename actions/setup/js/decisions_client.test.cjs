import { describe, expect, it, vi } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { createDecisions, DecisionsError } = require("./decisions_client.cjs");
const question = { type: "predicate", name: "builds", instructions: "Does it compile?" };
const request = { input: "private execution trace", questions: [question] };
const apiKey = "test-only-credential";
const answer = { type: "predicate", name: "builds", probability: 0.9 };
const response = answers => ({ ok: true, status: 200, json: async () => ({ answers }) });

describe("Decisions driver", () => {
  it("sends shared evidence once and returns named typed probabilities", async () => {
    const fetch = vi.fn().mockResolvedValue(response([answer]));
    const telemetry = vi.fn();
    expect(await createDecisions(request, { apiKey, fetch, telemetry })).toEqual([answer]);
    expect(fetch).toHaveBeenCalledTimes(1);
    const [url, options] = fetch.mock.calls[0];
    expect(url).toBe("https://api.openai.com/v1/decisions");
    expect(options.method).toBe("POST");
    expect(options.redirect).toBe("error");
    expect(options.signal).toBeInstanceOf(AbortSignal);
    expect(options.headers.Authorization).toBe(["Bearer", apiKey].join(" "));
    expect(JSON.parse(options.body)).toEqual({ ...request, model: "gpt-6-luna" });
    expect(telemetry).toHaveBeenCalledWith({ questionCount: 1, durationMs: expect.any(Number), status: 200, outcome: "success" });
    expect(JSON.stringify(telemetry.mock.calls)).not.toContain(request.input);
    expect(JSON.stringify(telemetry.mock.calls)).not.toContain(apiKey);
    expect(JSON.stringify(telemetry.mock.calls)).not.toContain(question.instructions);
  });

  it("maps answers by name even when returned out of order", async () => {
    const questions = [question, { ...question, name: "tests" }];
    const answers = [{ ...answer, name: "tests" }, answer];
    expect(await createDecisions({ ...request, questions }, { apiKey, fetch: vi.fn().mockResolvedValue(response(answers)) })).toEqual(answers);
  });

  it.each([[], [null], [{ ...question, type: "score" }], Array.from({ length: 401 }, (_, i) => ({ ...question, name: String(i) })), [question, question]])("rejects invalid batches and question types", async questions => {
    const fetch = vi.fn();
    await expect(createDecisions({ ...request, questions }, { apiKey, fetch })).rejects.toBeInstanceOf(DecisionsError);
    expect(fetch).not.toHaveBeenCalled();
  });

  it.each([
    [],
    [answer, answer],
    [{ ...answer, name: "unexpected" }],
    [{ ...answer, type: "score" }],
    [{ ...answer, probability: "0.9" }],
    [{ ...answer, probability: NaN }],
    [{ ...answer, probability: -0.1 }],
    [{ ...answer, probability: 1.1 }],
  ])("rejects malformed or incomplete answers", async answers => {
    await expect(createDecisions(request, { apiKey, fetch: vi.fn().mockResolvedValue(response(answers)) })).rejects.toThrow("invalid_response");
  });

  it("rejects duplicate response names even with the correct answer count", async () => {
    await expect(
      createDecisions(
        { ...request, questions: [question, { ...question, name: "tests" }] },
        {
          apiKey,
          fetch: vi.fn().mockResolvedValue(response([answer, answer])),
        }
      )
    ).rejects.toThrow("invalid_response");
  });

  it.each([401, 403, 404, 429, 500])("never reads or logs HTTP error bodies or retries (%s)", async status => {
    const json = vi.fn().mockRejectedValue(new Error(request.input));
    const fetch = vi.fn().mockResolvedValue({ ok: false, status, json });
    const telemetry = vi.fn();
    await expect(createDecisions(request, { apiKey, fetch, telemetry })).rejects.toMatchObject({ code: "http_error", status });
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(json).not.toHaveBeenCalled();
    expect(JSON.stringify(telemetry.mock.calls)).not.toContain(request.input);
  });

  it("sanitizes network and JSON decode errors", async () => {
    const fetch = vi.fn().mockRejectedValue(new Error(`${apiKey}: ${request.input}`));
    await expect(createDecisions(request, { apiKey, fetch })).rejects.toThrow("network_error");
    fetch.mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => {
        throw new Error(request.input);
      },
    });
    await expect(createDecisions(request, { apiKey, fetch })).rejects.toThrow("invalid_response");
  });

  it("bounds request lifetime and sanitizes timeout errors", async () => {
    const fetch = vi.fn(
      (_url, options) =>
        new Promise((_resolve, reject) => {
          options.signal.addEventListener("abort", () => reject(new Error(request.input)), { once: true });
        })
    );
    await expect(createDecisions(request, { apiKey, fetch, timeoutMs: 10 })).rejects.toThrow("timeout");
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("accepts the maximum 400 predicates in one request", async () => {
    const questions = Array.from({ length: 400 }, (_, i) => ({ ...question, name: `q${i}` }));
    const answers = questions.map(q => ({ ...answer, name: q.name }));
    const fetch = vi.fn().mockResolvedValue(response(answers));
    expect(await createDecisions({ ...request, questions }, { apiKey, fetch })).toHaveLength(400);
    expect(fetch).toHaveBeenCalledTimes(1);
  });
});
