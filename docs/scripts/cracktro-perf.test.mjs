// Regression test for the cracktro slide performance guard: a calm runtime-only deck passes, and each
// runaway behaviour in the fixture (leaked interval, endless rAF loop, off-screen animation, growing DOM,
// busy tick) fails the guard with the right check on the right slide.
// Chrome-driven tests need Chrome or Chromium (CHROME_PATH) and skip without one; progress and timeout tests do not.
import assert from "node:assert/strict";
import { chmodSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { findChrome, heartbeat, launch, run } from "./cracktro-perf.mjs";

const docs = fileURLToPath(new URL("..", import.meta.url));
const skip = findChrome() ? false : "no Chrome/Chromium found (set CHROME_PATH)";
const sleep = ms => new Promise(r => setTimeout(r, ms));
const guard = async bad => {
  const json = join(tmpdir(), `cracktro-perf-${process.pid}-${bad || "calm"}.json`);
  const code = await run(["--root", docs, "--path", `/scripts/cracktro-perf.fixture.html?bad=${bad}`, "--json", json], () => {});
  const report = JSON.parse(readFileSync(json, "utf8"));
  rmSync(json, { force: true });
  return { code, report };
};

test("calm fixture deck passes every budget", { skip }, async () => {
  const { code, report } = await guard("");
  assert.deepEqual(report.violations, []);
  assert.equal(code, 0);
});

test("runaway slides fail the guard with attributed findings", { skip }, async () => {
  const { code, report } = await guard("interval,raf,anim,dom,cpu");
  assert.equal(code, 1);
  const found = new Set(report.violations.map(v => `${v.check} ${v.slide} ${v.detail}`));
  const has = (check, slide, detail = "") =>
    assert.ok(
      [...found].some(f => f.startsWith(`${check} ${slide}`) && f.includes(detail)),
      `expected ${check} on #${slide} ${detail}\n${[...found].join("\n")}`
    );
  has("max-foreign-callbacks", "rest", "from #leaky-interval");
  has("max-foreign-callbacks", "rest", "from #endless-raf");
  has("max-offscreen-animations", "rest", "#spinner");
  has("max-dwell-growth", "grower");
  has("max-busy", "burner");
  has("max-tour-growth-intervals", "(revisit)");
});

test("heartbeat reports elapsed time and the current phase until stopped", async () => {
  const lines = [];
  let t = 0;
  const beat = heartbeat(
    l => lines.push(l),
    10,
    () => t
  );
  beat.set("revisit 1/2, slide 3/9 #x");
  t = 7000;
  await sleep(45);
  beat.stop();
  const seen = lines.length;
  assert.ok(seen >= 2, `expected regular updates, got ${seen}`);
  assert.equal(lines[0], "… 7s revisit 1/2, slide 3/9 #x");
  await sleep(40);
  assert.equal(lines.length, seen, "heartbeat kept firing after stop()");
});

test("a failed startup reports its phase and stops the heartbeat", async () => {
  // Node rejects Chrome's flags and exits, standing in for a browser that dies at launch.
  const lines = [];
  await assert.rejects(
    run(["--chrome", process.execPath, "--heartbeat", "10"], l => lines.push(l)),
    /Chrome exited.*while starting/
  );
  assert.match(lines[0], /^… 0s starting /);
  const seen = lines.length;
  await sleep(40);
  assert.equal(lines.length, seen, "heartbeat kept firing after run() failed");
});

test("invalid numeric flags are rejected before any server or browser starts", async () => {
  const bad = [
    ["--heartbeat", "abc"],
    ["--heartbeat", "-1"],
    ["--heartbeat", ""],
    ["--cdp-timeout", "NaN"],
    ["--cdp-timeout", "0"],
    ["--window", "0"],
    ["--settle", "-5"],
    ["--revisits", "1.5"],
    ["--max-busy", "nope"],
    ["--min-fps", "Infinity"],
  ];
  for (const [flag, value] of bad) {
    const lines = [];
    await assert.rejects(
      run(["--chrome", "/nonexistent/chrome", `${flag}=${value}`], l => lines.push(l)),
      new RegExp(`^Error: ${flag} must be a finite`),
      `${flag}=${value}`
    );
    assert.deepEqual(lines, [], `${flag}=${value} started the guard`);
  }
});

test("a browser that never answers times out instead of hanging", { skip: process.platform === "win32" && "needs a POSIX shell" }, async () => {
  const mute = join(tmpdir(), `cracktro-perf-mute-${process.pid}.sh`);
  writeFileSync(mute, "#!/bin/sh\nexec sleep 30\n");
  chmodSync(mute, 0o755);
  const cdp = launch(mute, [], 50);
  try {
    await assert.rejects(cdp.send("Browser.getVersion"), /no reply from Chrome after 0.05s/);
  } finally {
    await cdp.close();
    rmSync(mute, { force: true });
  }
});
