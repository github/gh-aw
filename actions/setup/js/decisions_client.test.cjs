import { describe, expect, it, vi } from "vitest";
import { createRequire } from "module";
import { createServer } from "node:http";
const { invokeDecisions, validateQuestions } = createRequire(import.meta.url)("./decisions_client.cjs");

const questions = [
  { type: "predicate", name: "relevant", instructions: "Is it relevant?" },
  { type: "choice", name: "category", instructions: "Select a category", choices: [{ value: "a" }, { value: "b" }] },
  { type: "score", name: "severity", instructions: "Rate severity", levels: [{ label: "low" }, { label: "high" }] },
];
const answers = [
  { type: "predicate", name: "relevant", probability: 0.7 },
  { type: "choice", name: "category", choice: "b" },
  { type: "score", name: "severity", score: 0.8 },
];
function options(response = { answers }) {
  return {
    baseURL: "https://example.com/v1/",
    apiKey: "test-key",
    model: "decision-model",
    input: "Shared evidence",
    questions,
    fetchImpl: vi.fn().mockResolvedValue({ ok: true, json: async () => response }),
  };
}

describe("generic Decisions client", () => {
  it("uses one real HTTP POST for a full batch", async () => {
    const requests = [];
    const server = createServer(async (request, response) => {
      let body = "";
      for await (const chunk of request) body += chunk;
      requests.push({ method: request.method, url: request.url, body: JSON.parse(body) });
      response.writeHead(200, { "Content-Type": "application/json" });
      response.end(JSON.stringify({ answers }));
    });
    await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
    try {
      const result = await invokeDecisions({
        baseURL: `http://127.0.0.1:${server.address().port}/v1`,
        model: "decision-model",
        input: "Shared evidence",
        questions,
      });
      expect(result.answers).toEqual(answers);
      expect(requests).toHaveLength(1);
      expect(requests[0]).toEqual({
        method: "POST",
        url: "/v1/decisions",
        body: { model: "decision-model", input: "Shared evidence", questions },
      });
    } finally {
      await new Promise(resolve => server.close(resolve));
    }
  });

  it("sends predicate, choice, and score questions together with shared input", async () => {
    const config = options();
    expect(await invokeDecisions(config)).toEqual({ answers });
    expect(config.fetchImpl).toHaveBeenCalledTimes(1);
    const [url, request] = config.fetchImpl.mock.calls[0];
    expect(url).toBe("https://example.com/v1/decisions");
    expect(request.method).toBe("POST");
    expect(request.redirect).toBe("error");
    expect(request.headers.Authorization).toBe("Bearer test-key");
    expect(request.signal).toBeInstanceOf(AbortSignal);
    expect(JSON.parse(request.body)).toEqual({ model: config.model, input: config.input, questions });
  });

  it("supports credential-free proxies and named refusals", async () => {
    const config = options({ answers: answers.map(answer => ({ type: "refusal", name: answer.name })) });
    config.apiKey = undefined;
    await invokeDecisions(config);
    expect(config.fetchImpl.mock.calls[0][1].headers).not.toHaveProperty("Authorization");
  });

  it("supports typed boolean choices and question-specific content parts", async () => {
    const config = options();
    config.questions = [
      {
        type: "choice",
        name: "matches",
        instructions: [
          { type: "input_text", text: "Does the image match?" },
          { type: "input_image", image_url: "https://example.com/image.png" },
        ],
        choices: [{ value: true }, { value: false }, { value: "true" }],
      },
    ];
    config.fetchImpl.mockResolvedValue({ ok: true, json: async () => ({ answers: [{ type: "choice", name: "matches", choice: true }] }) });
    expect((await invokeDecisions(config)).answers[0].choice).toBe(true);
    expect(JSON.parse(config.fetchImpl.mock.calls[0][1].body).questions).toEqual(config.questions);
  });

  it.each([
    [],
    Array.from({ length: 401 }, (_, i) => ({ ...questions[0], name: String(i) })),
    [questions[0], questions[0]],
    [{ ...questions[0], instructions: "" }],
    [{ ...questions[1], choices: [{ value: "a" }, { value: "a" }] }],
    [{ ...questions[2], levels: [{ label: "low" }] }],
  ])("rejects invalid question batches before POST: %j", async batch => {
    const config = { ...options(), questions: batch };
    await expect(invokeDecisions(config)).rejects.toThrow();
    expect(config.fetchImpl).not.toHaveBeenCalled();
  });

  it("accepts the documented 400-question limit without splitting", async () => {
    const config = options();
    config.questions = Array.from({ length: 400 }, (_, i) => ({ ...questions[0], name: String(i) }));
    config.fetchImpl.mockResolvedValue({ ok: true, json: async () => ({ answers: config.questions.map(q => ({ type: "predicate", name: q.name, probability: 1 })) }) });
    await invokeDecisions(config);
    expect(config.fetchImpl).toHaveBeenCalledTimes(1);
    expect(() => validateQuestions(config.questions)).not.toThrow();
  });

  it.each([
    [],
    [answers[0], answers[0], answers[2]],
    [answers[0], { ...answers[1], name: "unknown" }, answers[2]],
    [{ ...answers[0], probability: -0.1 }, answers[1], answers[2]],
    [{ ...answers[0], probability: 1.1 }, answers[1], answers[2]],
    [{ ...answers[0], probability: null }, answers[1], answers[2]],
    [answers[0], { ...answers[1], choice: "c" }, answers[2]],
    [answers[0], answers[1], { ...answers[2], score: 2 }],
    [answers[0], { type: "predicate", name: "category", probability: 1 }, answers[2]],
  ])("rejects incomplete, duplicate, mismatched, or out-of-range answers: %j", async invalid => {
    await expect(invokeDecisions(options({ answers: invalid }))).rejects.toThrow();
  });

  it.each([400, 401, 403, 429, 500])("surfaces HTTP %s without echoing response bodies or retrying", async status => {
    const config = options();
    const text = vi.fn().mockResolvedValue("sensitive provider response");
    config.fetchImpl.mockResolvedValue({ ok: false, status, text });
    await expect(invokeDecisions(config)).rejects.toThrow(`Decisions request failed (HTTP ${status})`);
    expect(text).not.toHaveBeenCalled();
    expect(config.fetchImpl).toHaveBeenCalledTimes(1);
  });

  it("propagates network failures", async () => {
    const config = options();
    config.fetchImpl.mockRejectedValue(new Error("connection failed"));
    await expect(invokeDecisions(config)).rejects.toThrow("connection failed");
    expect(config.fetchImpl).toHaveBeenCalledTimes(1);
  });

  it("propagates invalid JSON", async () => {
    const config = options();
    config.fetchImpl.mockResolvedValue({
      ok: true,
      json: async () => {
        throw new SyntaxError("Invalid JSON");
      },
    });
    await expect(invokeDecisions(config)).rejects.toThrow("Invalid JSON");
  });
});
