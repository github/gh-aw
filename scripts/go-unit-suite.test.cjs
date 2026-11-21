"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { execFileSync, spawnSync } = require("node:child_process");
const { test } = require("node:test");
const { slowPattern } = require("./go-unit-suite.cjs");
const manifest = require("../.github/ci/go-unit-slow-tests.json");
const root = path.join(__dirname, "..");
const workflow = fs.readFileSync(path.join(root, ".github/workflows/cgo.yml"), "utf8");
const slowJob = workflow.split("\n  test-slow:\n")[1].split("\n  canary-go:\n")[0];
const shards = [...slowJob.matchAll(/pattern: (.+)/g)].map(match => match[1]);

test("measured slow tests are excluded from fast tests and run in exactly one slow shard", () => {
  const all = new RegExp(slowPattern());
  const fastShards = [
    ...workflow
      .split("\n  test:\n")[1]
      .split("\n  test-slow:\n")[0]
      .matchAll(/          pattern: (.+)/g),
  ].map(match => new RegExp(match[1]));
  const slowShards = shards.map(shard => new RegExp(slowPattern(shard)));
  assert.equal(new Set(manifest.tests.map(test => test.name)).size, manifest.tests.length);
  for (const entry of manifest.tests) {
    assert.ok(entry.elapsed_seconds >= manifest.threshold_seconds, entry.name);
    assert.equal(fastShards.filter(pattern => pattern.test(entry.name) && !all.test(entry.name)).length, 0, entry.name);
    assert.equal(slowShards.filter(pattern => pattern.test(entry.name)).length, 1, entry.name);
    const source = execFileSync("git", ["grep", "-l", `^func ${entry.name}(`, "--", `${entry.package}/*_test.go`], { cwd: root, encoding: "utf8" });
    assert.ok(source.trim(), entry.name);
    assert.equal(all.test(`${entry.name}Extra`), false);
  }
  for (const name of ["TestJavaScriptSummaryCatalogSourceIdentity", "TestNewUnit", "TestZ", "TestÉ"]) {
    assert.equal(fastShards.filter(pattern => pattern.test(name) && !all.test(name)).length, 1, name);
    assert.equal(slowShards.filter(pattern => pattern.test(name)).length, 0, name);
  }
  assert.throws(() => slowPattern("missing"), /Unknown or empty/);
});

test("workflow passes exact fast/slow selection to Go and preserves failures", t => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "go-unit-suite-"));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  t.after(() => {
    for (const file of ["test-result-unit-fixture.json", "coverage-fixture.out", "coverage-fixture.html"]) {
      fs.rmSync(path.join(root, file), { force: true });
    }
  });
  fs.writeFileSync(path.join(directory, "go"), '#!/bin/bash\nprintf "%s\\n" "$@" > "$CAPTURE_ARGS"\nexit "${GO_EXIT:-0}"\n', { mode: 0o755 });
  const script = workflow
    .split("    - name: Run unit tests with coverage\n")[1]
    .split("      run: |\n")[1]
    .split("\n    - name: Report test failures")[0]
    .replace(/^        /gm, "")
    .replaceAll("${{ matrix.shard }}", "fixture");
  for (const suite of ["fast", "slow", "invalid"]) {
    for (const exit of [0, 17]) {
      const capture = path.join(directory, "args");
      const result = spawnSync("bash", ["-e", "-c", script], {
        cwd: root,
        env: { ...process.env, PATH: `${directory}:${process.env.PATH}`, UNIT_TEST_SUITE: suite, UNIT_TEST_TIMEOUT: "5m", TEST_PATTERN: suite === "fast" ? "^TestA" : "repository", CAPTURE_ARGS: capture, GO_EXIT: String(exit) },
        encoding: "utf8",
      });
      assert.equal(result.status === 0, suite !== "invalid" && exit === 0, `${suite}: ${exit}: ${result.stderr}`);
      if (suite !== "invalid" && exit !== 0) {
        const args = fs.readFileSync(capture, "utf8").trim().split("\n");
        assert.equal(args[args.indexOf("-run") + 1], suite === "fast" ? "^TestA" : slowPattern("repository"));
        assert.equal(args.includes("-skip"), suite === "fast");
        if (suite === "fast") assert.equal(args[args.indexOf("-skip") + 1], slowPattern());
      }
    }
  }
});

test("local fast/slow make targets retain Go failures and the complete partition", t => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "go-unit-make-"));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  fs.writeFileSync(path.join(directory, "go"), '#!/bin/bash\nprintf "%s\\n" "$@" > "$CAPTURE_ARGS"\nexit 17\n', { mode: 0o755 });
  for (const suite of ["fast", "slow"]) {
    const capture = path.join(directory, `${suite}.args`);
    const result = spawnSync("make", [`test-unit-${suite}`, "VERSION=test"], {
      cwd: root,
      env: { ...process.env, PATH: `${directory}:${process.env.PATH}`, CAPTURE_ARGS: capture },
      encoding: "utf8",
    });
    assert.notEqual(result.status, 0);
    const args = fs.readFileSync(capture, "utf8").trim().split("\n");
    assert.ok(args.includes(suite === "fast" ? `-skip=${slowPattern()}` : `-run=${slowPattern()}`));
    assert.ok(args.includes("-short"));
    assert.ok(args.includes("./..."));
  }
});
