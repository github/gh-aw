// @ts-check
"use strict";

const assert = require("node:assert/strict");
const { test } = require("node:test");
const { invokeDecisions } = require("../decisions_client.cjs");

test("Copilot Decisions accepts the copilot-requests: write job token", { timeout: 45000 }, async () => {
  const apiKey = process.env.GITHUB_TOKEN;
  assert.ok(apiKey?.trim(), "GITHUB_TOKEN with copilot-requests: write is required");

  const response = await invokeDecisions({
    baseURL: "https://api.githubcopilot.com/v1",
    apiKey,
    model: "gpt-6-luna-decisions",
    input: "The basket contains three apples and no other fruit.",
    questions: [{ type: "choice", name: "basket_fruit", instructions: "Which fruit does the basket contain?", choices: [{ value: "apples" }, { value: "bananas" }] }],
    timeoutMs: 30000,
  });

  assert.equal(response.answers.length, 1);
  const answer = response.answers[0];
  assert.equal(answer.name, "basket_fruit");
  assert.equal(answer.type, "choice");
  assert.equal(answer.choice, "apples", "The Decisions API must recognize the apples in the supplied evidence");
});
