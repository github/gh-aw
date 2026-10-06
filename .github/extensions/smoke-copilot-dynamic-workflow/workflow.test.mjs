import assert from "node:assert/strict";
import { test } from "node:test";
import { smokeWorkflow } from "./workflow.mjs";

const marker = "GH_AW_DYNAMIC_WORKFLOW_SMOKE_OK";

function context(response = { marker }) {
  const phases = [];
  const steps = [];
  const agents = [];
  return {
    args: { marker },
    phases,
    steps,
    agents,
    phase: title => phases.push(title),
    log: () => {},
    step: async (key, producer) => {
      steps.push(key);
      return producer();
    },
    agent: async (prompt, options) => {
      agents.push({ prompt, options });
      return response;
    },
  };
}

test("loads the packaged hidden fixture and verifies one structured subagent", async () => {
  const ctx = context();
  assert.deepEqual(await smokeWorkflow.run(ctx), {
    status: "PASS",
    marker,
    packageVersion: 1,
    supportFile: "support/.fixture.json",
    subagents: 1,
  });
  assert.deepEqual(ctx.phases, ["Package", "Subagent", "Verify"]);
  assert.deepEqual(ctx.steps, ["package-v1", "result-v1"]);
  assert.equal(ctx.agents.length, 1);
  assert.equal(ctx.agents[0].options.schema.properties.marker.const, marker);
});

test("rejects absent, invalid, and mismatched arguments before spawning", async () => {
  for (const args of [undefined, {}, { marker: 42 }, { marker: "wrong" }]) {
    const ctx = context();
    ctx.args = args;
    await assert.rejects(smokeWorkflow.run(ctx), /Expected marker/);
    assert.equal(ctx.agents.length, 0);
  }
});

test("fails instead of reporting success for missing or incorrect subagent results", async () => {
  for (const response of [null, {}, { marker: "wrong" }]) {
    const ctx = context(response);
    await assert.rejects(smokeWorkflow.run(ctx), /exact marker/);
    assert.deepEqual(ctx.steps, ["package-v1"]);
  }
});

test("propagates hard subagent failures", async () => {
  const ctx = context();
  ctx.agent = async () => {
    throw new Error("workflow_limit_reached");
  };
  await assert.rejects(smokeWorkflow.run(ctx), /workflow_limit_reached/);
});
