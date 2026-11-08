// @ts-check
"use strict";

/**
 * @typedef {{type: "input_text", text: string} | {type: "input_image", image_url: string, detail?: "auto" | "low" | "high" | "original"} | {type: "input_json", data: object}} DecisionContentPart
 * @typedef {string | DecisionContentPart[]} DecisionInstructions
 * @typedef {{type: "predicate", name: string, instructions: DecisionInstructions}} PredicateQuestion
 * @typedef {{type: "choice", name: string, instructions: DecisionInstructions, choices: Array<{value: string | boolean, description?: string}>}} ChoiceQuestion
 * @typedef {{type: "score", name: string, instructions: DecisionInstructions, levels: Array<{label: string, description?: string}>}} ScoreQuestion
 * @typedef {PredicateQuestion | ChoiceQuestion | ScoreQuestion} DecisionQuestion
 * @typedef {{type: "predicate", name: string, probability: number} | {type: "choice", name: string, choice: string | boolean} | {type: "score", name: string, score: number} | {type: "refusal", name: string}} DecisionAnswer
 * @typedef {{answers: DecisionAnswer[], model?: string, usage?: unknown}} DecisionResponse
 */

const MAX_DECISION_QUESTIONS = 400;

class DecisionsModelUnavailableError extends Error {
  /** @param {number} status */
  constructor(status) {
    super(`Decisions model is unavailable or unsupported (HTTP ${status})`);
    this.name = "DecisionsModelUnavailableError";
  }
}

class DecisionsAuthUnsupportedError extends Error {
  /** @param {number} status */
  constructor(status) {
    super(`Decisions endpoint does not accept personal access tokens (HTTP ${status})`);
    this.name = "DecisionsAuthUnsupportedError";
  }
}

/**
 * Validate a complete batch before sending any request.
 * @param {DecisionQuestion[]} questions
 */
function validateQuestions(questions) {
  if (!Array.isArray(questions) || questions.length < 1 || questions.length > MAX_DECISION_QUESTIONS) {
    throw new Error(`Decisions requires between 1 and ${MAX_DECISION_QUESTIONS} questions in one request`);
  }
  const names = new Set();
  for (const question of questions) {
    if (!question || typeof question.name !== "string" || !question.name.trim() || names.has(question.name)) {
      throw new Error("Decision question names must be non-empty and unique");
    }
    names.add(question.name);
    if (!(typeof question.instructions === "string" ? question.instructions.trim() : Array.isArray(question.instructions) && question.instructions.length > 0)) {
      throw new Error(`Decision question '${question.name}' requires instructions`);
    }
    switch (question.type) {
      case "predicate":
        break;
      case "choice": {
        const values = question.choices?.map(option => option.value);
        if (!values || values.length < 2 || values.length > 255 || values.some(value => typeof value !== "boolean" && typeof value !== "string") || new Set(values).size !== values.length) {
          throw new Error(`Decision question '${question.name}' requires 2-255 distinct string or boolean choices`);
        }
        break;
      }
      case "score":
        if (!Array.isArray(question.levels) || question.levels.length < 2 || question.levels.length > 10 || question.levels.some(level => typeof level.label !== "string" || !level.label.trim())) {
          throw new Error(`Decision question '${question.name}' requires between 2 and 10 labeled levels`);
        }
        break;
      default:
        throw new Error("Unsupported decision question type");
    }
  }
}

/**
 * Match answers by name, never by position; reject incomplete or malformed batches.
 * @param {unknown} response
 * @param {DecisionQuestion[]} questions
 * @returns {DecisionResponse}
 */
function validateDecisionResponse(response, questions) {
  if (!response || typeof response !== "object" || !("answers" in response) || !Array.isArray(response.answers) || response.answers.length !== questions.length) {
    throw new Error("Decisions response must contain exactly one answer per question");
  }
  const model = "model" in response ? response.model : undefined;
  if (model !== undefined && typeof model !== "string") throw new Error("Decisions response model must be a string");
  const answers = response.answers;
  const pending = new Map(questions.map(question => [question.name, question]));
  for (const answer of response.answers) {
    const question = answer && pending.get(answer.name);
    if (!question) throw new Error("Decisions response contains an unknown or duplicate answer name");
    pending.delete(answer.name);
    if (answer.type === "refusal") continue;
    if (answer.type !== question.type) throw new Error(`Decisions response type does not match question '${question.name}'`);
    if (answer.type === "predicate" && (!Number.isFinite(answer.probability) || answer.probability < 0 || answer.probability > 1)) {
      throw new Error(`Invalid predicate probability for '${question.name}'`);
    }
    if (answer.type === "choice" && question.type === "choice" && !question.choices.some(option => option.value === answer.choice)) {
      throw new Error(`Invalid decision choice for '${question.name}'`);
    }
    if (answer.type === "score" && question.type === "score" && (!Number.isFinite(answer.score) || answer.score < 0 || answer.score > question.levels.length - 1)) {
      throw new Error(`Invalid decision score for '${question.name}'`);
    }
  }
  return { ...response, answers, ...(model !== undefined ? { model } : {}) };
}

/**
 * Invoke an OpenAI-compatible Decisions endpoint with one POST for the entire batch.
 * No SDK, Actions globals, agent engine, automatic retries, or credential discovery.
 * Refusals are returned to the caller; transport and protocol failures throw.
 *
 * @param {{baseURL: string, apiKey?: string, integrationId?: string, model: string, input: string | object[], questions: DecisionQuestion[], timeoutMs?: number, fetchImpl?: typeof fetch, onUsage?: (usage: unknown, model: string) => void}} options
 * @returns {Promise<DecisionResponse>}
 */
async function invokeDecisions({ baseURL, apiKey, integrationId, model, input, questions, timeoutMs = 120000, fetchImpl = fetch, onUsage }) {
  validateQuestions(questions);
  if (typeof model !== "string" || !model.trim()) throw new Error("Decisions requires a model");
  if (!Number.isFinite(timeoutMs) || timeoutMs <= 0) throw new Error("Decisions timeout must be positive");
  const url = new URL(baseURL);
  if (!["http:", "https:"].includes(url.protocol) || url.username || url.password || url.search || url.hash) {
    throw new Error("Decisions base URL must be an HTTP(S) URL without credentials, query, or fragment");
  }
  url.pathname = url.pathname.replace(/\/+$/, "") + "/decisions";
  const response = await fetchImpl(url.toString(), {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      ...(apiKey ? { Authorization: `Bearer ${apiKey}` } : {}),
      ...(integrationId ? { "Copilot-Integration-Id": integrationId } : {}),
    },
    body: JSON.stringify({ model, input, questions }),
    signal: AbortSignal.timeout(timeoutMs),
    redirect: "error",
  });
  // Inspect only allowlisted codes and token rejection text; never expose provider bodies or messages.
  if (!response.ok) {
    // A 404 may come from routing infrastructure, even with a model error code.
    if ([400, 401, 403, 422].includes(response.status)) {
      const text = (await response.text()).slice(0, 2048);
      if ([400, 401, 403].includes(response.status) && text.includes("Personal Access Tokens are not supported")) {
        throw new DecisionsAuthUnsupportedError(response.status);
      }
      let body;
      try {
        body = JSON.parse(text);
      } catch (error) {
        if (!(error instanceof SyntaxError)) throw error;
      }
      const code = body?.error?.code ?? body?.code;
      if ([400, 422].includes(response.status) && ["model_not_supported", "model_not_found", "model_not_available", "model_unavailable", "unsupported_model"].includes(code)) {
        throw new DecisionsModelUnavailableError(response.status);
      }
    }
    throw new Error(`Decisions request failed (HTTP ${response.status})`);
  }
  const result = await response.json();
  // Account for billed requests even if the answer batch fails validation.
  if (onUsage && result && typeof result === "object") {
    onUsage("usage" in result ? result.usage : undefined, "model" in result && typeof result.model === "string" ? result.model : model);
  }
  return validateDecisionResponse(result, questions);
}

module.exports = { invokeDecisions, validateDecisionResponse, validateQuestions, MAX_DECISION_QUESTIONS, DecisionsModelUnavailableError, DecisionsAuthUnsupportedError };
