import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import validateAgentSessions, { validationExitCode } from "./validate-agent-sessions.mjs";

const checkout = path.resolve(import.meta.dirname, "..");

function context(args) {
  const parallel = thunks => Promise.all(thunks.map(thunk => thunk()));
  return {
    args,
    runId: "validation-test",
    signal: new AbortController().signal,
    phase() {},
    log() {},
    step: async (_key, producer) => producer(),
    parallel,
    pipeline: (items, ...stages) =>
      parallel(
        items.map((item, index) => async () => {
          let previous;
          for (const stage of stages) previous = await stage(previous, item, index);
          return previous;
        })
      ),
  };
}

test("validates exact coverage and explicit exit conditions", () => {
  const passed = { requested: 50, completed: 50, failed: 0, blocked: 0, missing: [], contractProbes: [] };
  assert.equal(validationExitCode(passed), 0);
  for (const change of [{ completed: 49 }, { failed: 1 }, { blocked: 1 }, { missing: [1] }, { contractProbes: [{}] }]) assert.equal(validationExitCode({ ...passed, ...change }), 1);
  assert.equal(validationExitCode({ ...passed, warnings: 10 }), 0);
});

test("rejects invalid counts before invoking GitHub or writing evidence", async () => {
  for (const count of [0, -1, 1.5, NaN]) await assert.rejects(validateAgentSessions(context({ repo: "github/gh-aw", repoPath: checkout, outputRoot: os.tmpdir(), count })), /positive integers/);
});

async function fixture(t, canonical, stdio) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "gh-aw-session-validation-test-"));
  t.after(() => fs.rm(root, { recursive: true, force: true }));
  const prior = path.join(root, "prior");
  const raw = path.join(prior, "123", "raw", "agent");
  await fs.mkdir(raw, { recursive: true });
  const content = canonical.map(event => JSON.stringify(event)).join("\n") + "\n";
  await fs.writeFile(path.join(raw, "agent-session.jsonl"), content);
  if (stdio) await fs.writeFile(path.join(raw, "agent-stdio.log"), stdio);
  const manifest = {
    repo: "github/gh-aw",
    head: "previous-parser-revision",
    runs: [{ id: 123, path: ".github/workflows/smoke-claude.lock.yml", url: "https://github.com/github/gh-aw/actions/runs/123", artifacts: [{ id: 456, name: "agent", size: 1 }] }],
  };
  await fs.writeFile(path.join(prior, "manifest.json"), JSON.stringify(manifest));
  return { root, prior, raw, content, manifest };
}

test("replays a retained run without network access and persists canonical format-1 output", async t => {
  const data = await fixture(t, [{ type: "assistant.message", id: "message", data: { content: "  exact\ntext\t\n" } }]);
  const result = await validateAgentSessions(context({ repo: "github/gh-aw", repoPath: checkout, outputRoot: path.join(data.root, "output"), reuseRoot: data.prior, count: 1 }));
  assert.equal(result.completed, 1);
  assert.equal(result.passed, 1);
  assert.equal(result.currentFailed, 0);
  assert.equal(result.historicalFailed, 0);
  assert.deepEqual(result.contractProbes, []);
  assert.equal(validationExitCode(result), 0);
  const report = JSON.parse(await fs.readFile(result.reportJson, "utf8"));
  assert.match(report.parserFingerprint, /^[a-f0-9]{64}$/);
  assert.equal(report.selection.previousValidationHead, "previous-parser-revision");
  const events = (await fs.readFile(path.join(result.root, "123", "normalized", "aw_session.jsonl"), "utf8")).trimEnd().split("\n").map(JSON.parse);
  assert.equal(events[0].type, "session.format");
  assert.equal(events[0].data.version, 1);
  assert.equal(events.find(event => event.type === "assistant.message").data.content, "  exact\ntext\t\n");
  assert.equal(await fs.readFile(path.join(data.raw, "agent-session.jsonl"), "utf8"), data.content);
});

test("distinguishes historical legacy artifacts from current parser failures without rewriting history", async t => {
  const legacy = [
    { type: "system", subtype: "init", model: "fixture" },
    { type: "assistant", message: { content: [{ type: "text", text: "retained" }] } },
    { type: "result", num_turns: 1 },
  ];
  const data = await fixture(t, legacy, legacy.map(event => JSON.stringify(event)).join("\n") + "\n");
  const result = await validateAgentSessions(context({ repo: "github/gh-aw", repoPath: checkout, outputRoot: path.join(data.root, "output"), reuseRoot: data.prior, count: 1 }));
  assert.equal(result.completed, 1);
  assert.equal(result.currentFailed, 0);
  assert.equal(result.historicalFailed, 1);
  assert.equal(result.failed, 1);
  assert.equal(validationExitCode(result), 1);
  assert.deepEqual(result.contractProbes, []);
  const report = JSON.parse(await fs.readFile(result.reportJson, "utf8"));
  assert.equal(report.runs[0].agentEvents, 3);
  assert.equal(await fs.readFile(path.join(data.raw, "agent-session.jsonl"), "utf8"), data.content);
});

test("rejects path traversal in a reused artifact manifest", async t => {
  const data = await fixture(t, []);
  data.manifest.runs[0].artifacts.push({ id: 789, name: "../escape" });
  await fs.writeFile(path.join(data.prior, "manifest.json"), JSON.stringify(data.manifest));
  await assert.rejects(validateAgentSessions(context({ repo: "github/gh-aw", repoPath: checkout, outputRoot: path.join(data.root, "output"), reuseRoot: data.prior, count: 1 })), /Invalid artifact/);
});
