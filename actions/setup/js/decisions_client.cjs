// @ts-check
"use strict";

const DECISIONS_MODEL = "gpt-6-luna";
const MAX_DECISION_QUESTIONS = 400;

/**
 * A deliberately payload-free error: upstream bodies and network errors can
 * contain evidence or credentials and must never reach logs.
 */
class DecisionsError extends Error {
  /** @param {string} code @param {number | undefined} [status] */
  constructor(code, status) {
    super(`Decisions request failed (${code})`);
    this.name = "DecisionsError";
    this.code = code;
    this.status = status;
  }
}

/**
 * Evaluate named predicates against shared evidence with a single API request.
 * No retries are performed. Telemetry contains counts/timing/status only.
 *
 * @param {{input: string | Array<object>, questions: Array<{type: "predicate", name: string, instructions: string}>}} request
 * @param {{apiKey: string, fetch?: typeof fetch, telemetry?: (event: {questionCount: number, durationMs: number, status?: number, outcome: string}) => void, timeoutMs?: number}} options
 * @returns {Promise<Array<{type: "predicate", name: string, probability: number}>>}
 */
async function createDecisions(request, options) {
  const { questions, input } = request;
  if (!options.apiKey || !Array.isArray(questions) || !questions.length || questions.length > MAX_DECISION_QUESTIONS || !(typeof input === "string" || Array.isArray(input))) {
    throw new DecisionsError("invalid_request");
  }
  const names = new Set();
  for (const question of questions) {
    if (!question || question.type !== "predicate" || typeof question.name !== "string" || !question.name.trim() || typeof question.instructions !== "string" || !question.instructions.trim() || names.has(question.name)) {
      throw new DecisionsError("invalid_request");
    }
    names.add(question.name);
  }

  const started = Date.now();
  let status;
  /** @type {AbortSignal | undefined} */
  let signal;
  let outcome = "network_error";
  try {
    signal = AbortSignal.timeout(options.timeoutMs ?? 60000);
    const response = await (options.fetch || globalThis.fetch)("https://api.openai.com/v1/decisions", {
      method: "POST",
      headers: { Authorization: ["Bearer", options.apiKey].join(" "), "Content-Type": "application/json" },
      redirect: "error",
      signal,
      body: JSON.stringify({ model: DECISIONS_MODEL, input, questions }),
    });
    status = response.status;
    if (!response.ok) {
      outcome = "http_error";
      throw new DecisionsError(outcome, status);
    }
    outcome = "invalid_response";
    /** @type {any} */
    const data = await response.json();
    if (!Array.isArray(data?.answers) || data.answers.length !== questions.length) {
      throw new DecisionsError(outcome, status);
    }
    const seen = new Set();
    const answers = data.answers.map(answer => {
      if (answer?.type !== "predicate" || !names.has(answer.name) || seen.has(answer.name) || typeof answer.probability !== "number" || !Number.isFinite(answer.probability) || answer.probability < 0 || answer.probability > 1) {
        throw new DecisionsError(outcome, status);
      }
      seen.add(answer.name);
      return { type: /** @type {const} */ "predicate", name: answer.name, probability: answer.probability };
    });
    outcome = "success";
    return answers;
  } catch (error) {
    if (error instanceof DecisionsError) throw error;
    if (signal?.aborted) outcome = "timeout";
    throw new DecisionsError(outcome, status);
  } finally {
    options.telemetry?.({ questionCount: questions.length, durationMs: Date.now() - started, status, outcome });
  }
}

module.exports = { createDecisions, DecisionsError, DECISIONS_MODEL, MAX_DECISION_QUESTIONS };
