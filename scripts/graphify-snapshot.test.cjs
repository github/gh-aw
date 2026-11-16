"use strict";

const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const { createHash } = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { afterEach, beforeEach, test } = require("node:test");

const source = fs.readFileSync(path.join(__dirname, "../.github/workflows/shared/graphify.md"), "utf8");
const lines = source.split("\n");
const scripts = [];
for (let i = 0; i < lines.length; i++) {
  const match = lines[i].match(/^( +)script: \|$/);
  if (!match) continue;
  const indent = match[1].length + 2;
  const body = [];
  while (++i < lines.length && (!lines[i].trim() || lines[i].startsWith(" ".repeat(indent)))) {
    body.push(lines[i].slice(indent));
  }
  scripts.push(body.join("\n"));
  i--;
}
assert.equal(scripts.length, 2, "Expected inline publisher and validator scripts");
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
const revision = execFileSync("git", ["rev-parse", "HEAD"], { encoding: "utf8" }).trim();
let root;
let graph;
let originalEnv;

beforeEach(() => {
  originalEnv = { ...process.env };
  root = fs.mkdtempSync(path.join(os.tmpdir(), "graphify-snapshot-"));
  fs.mkdirSync(path.join(root, "build/graphify-out"), { recursive: true });
  graph = Buffer.from(
    JSON.stringify({
      nodes: [{ id: "example", source_file: "pkg/example.go" }],
      edges: [],
    })
  );
  fs.writeFileSync(path.join(root, "build/graphify-out/graph.json"), graph);
  Object.assign(process.env, { SOURCE_SHA: revision, BUILD_ID: "123", BUILD_ATTEMPT: "1" });
});

afterEach(() => {
  for (const key of Object.keys(process.env)) {
    if (!(key in originalEnv)) delete process.env[key];
  }
  Object.assign(process.env, originalEnv);
  fs.rmSync(root, { recursive: true, force: true });
});

async function runInline(operation) {
  const outputs = {};
  const script = scripts[operation].replaceAll("/tmp/gh-aw-graphify", root);
  await new AsyncFunction("require", "core", script)(require, {
    setOutput: (name, value) => {
      outputs[name] = value;
    },
  });
  return outputs;
}

async function seal() {
  const outputs = await runInline(0);
  Object.assign(process.env, {
    EXPECTED_KEY: outputs["cache-key"],
    RESTORED_KEY: outputs["cache-key"],
    EXPECTED_DIGEST: outputs["snapshot-digest"],
    EXPECTED_SOURCE_SHA: outputs["source-sha"],
  });
}

function rewriteManifest(changes) {
  const file = path.join(root, "index/manifest.json");
  const manifest = { ...JSON.parse(fs.readFileSync(file)), ...changes };
  const bytes = Buffer.from(JSON.stringify(manifest));
  fs.writeFileSync(file, bytes);
  process.env.EXPECTED_DIGEST = createHash("sha256").update(graph).update("\0").update(bytes).digest("hex");
}

function snapshot() {
  return Object.fromEntries(fs.readdirSync(path.join(root, "index")).map(name => [name, fs.readFileSync(path.join(root, "index", name))]));
}

test("Sealed snapshot validates without writes and preserves source paths", async () => {
  await seal();
  const before = snapshot();
  await runInline(1);
  assert.deepEqual(snapshot(), before);
  assert.deepEqual(before["graph.json"], graph);
  assert.equal(JSON.parse(before["manifest.json"]).graph_sha256, createHash("sha256").update(graph).digest("hex"));
});

test("Tampering cannot authenticate using a digest inside the cache", async () => {
  await seal();
  const changed = Buffer.concat([graph, Buffer.from(" ")]);
  fs.writeFileSync(path.join(root, "index/graph.json"), changed);
  const file = path.join(root, "index/manifest.json");
  const manifest = JSON.parse(fs.readFileSync(file));
  manifest.graph_sha256 = createHash("sha256").update(changed).digest("hex");
  fs.writeFileSync(file, JSON.stringify(manifest));
  await assert.rejects(runInline(1), /digest/);
});

test("Wrong cache key, source and compatibility are rejected", async () => {
  await seal();
  for (const key of ["RESTORED_KEY", "EXPECTED_KEY", "EXPECTED_SOURCE_SHA"]) {
    const original = process.env[key];
    process.env[key] = "different";
    await assert.rejects(runInline(1));
    process.env[key] = original;
  }
  rewriteManifest({ source_sha: "different" });
  process.env.EXPECTED_SOURCE_SHA = "different";
  await assert.rejects(runInline(1), /source mismatch/);
  process.env.EXPECTED_SOURCE_SHA = revision;
  rewriteManifest({ source_sha: revision, tool: "graphifyy==other" });
  await assert.rejects(runInline(1), /incompatible/);
});

test("Age boundaries are accepted; stale, future and malformed timestamps are rejected", async t => {
  await seal();
  const now = Date.now();
  t.mock.method(Date, "now", () => now);
  for (const age of [0, 86400000]) {
    rewriteManifest({ built_at: new Date(now - age).toISOString() });
    await runInline(1);
  }
  for (const built_at of [new Date(now - 86400001).toISOString(), new Date(now + 1).toISOString(), "invalid"]) {
    rewriteManifest({ built_at });
    await assert.rejects(runInline(1), /24 hours/);
  }
});

test("Missing, extra and symlinked snapshot files are rejected", async () => {
  await seal();
  const index = path.join(root, "index");
  const extra = path.join(index, "extra");
  fs.writeFileSync(extra, "not part of the authenticated snapshot");
  await assert.rejects(runInline(1), /expected only/);
  fs.unlinkSync(extra);
  const file = path.join(index, "graph.json");
  fs.unlinkSync(file);
  await assert.rejects(runInline(1), /expected only/);
  fs.symlinkSync(path.join(root, "build/graphify-out/graph.json"), file);
  await assert.rejects(runInline(1), /symlink/);
});

test("Empty extraction is not published and existing snapshots are not overwritten", async () => {
  const file = path.join(root, "build/graphify-out/graph.json");
  for (const data of [{ nodes: [], edges: [] }, { nodes: [{}] }, [], null]) {
    fs.writeFileSync(file, JSON.stringify(data));
    await assert.rejects(runInline(0), /empty/);
    assert.equal(fs.existsSync(path.join(root, "index")), false);
  }
  fs.writeFileSync(file, graph);
  await seal();
  await assert.rejects(runInline(0), { code: "EEXIST" });
});
