// Regression test for the cracktro slide performance guard: a calm runtime-only deck passes, and each
// runaway behaviour in the fixture (leaked interval, endless rAF loop, off-screen animation, growing DOM,
// busy tick) fails the guard with the right check on the right slide.
// #supply-chain-hardening is checked in the real deck and, without a browser, by running its slide code.
// Chrome-driven tests need Chrome or Chromium (CHROME_PATH) and skip without one; the other tests do not.
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

test("#supply-chain-hardening stays within the default budgets in the real deck", { skip }, async () => {
  const json = join(tmpdir(), `cracktro-perf-${process.pid}-supply-chain.json`);
  const code = await run(["--slides", "supply-chain-hardening", "--revisits", "0", "--json", json], () => {});
  const report = JSON.parse(readFileSync(json, "utf8"));
  rmSync(json, { force: true });
  assert.deepEqual(report.violations, []);
  assert.ok(report.rows[0].busy <= 0.5, `busy ${report.rows[0].busy}`);
  assert.equal(code, 0);
});

// Runs the real #supply-chain-hardening slide block from deck.js against a recording 2D context. Canvas
// shadowBlur re-blurs a shape's whole bounding box on every draw; its six blurred ~700px rings kept the
// main thread over half busy, so steady-state frames must draw no blurred shapes and reuse baked sprites.
const runSupplyChainSlide = () => {
  const lines = readFileSync(join(docs, "public/cracktro/deck.js"), "utf8").split("\n");
  const start = lines.findIndex(l => l.startsWith("  // Supply chain hardening:"));
  const end = lines.findIndex((l, i) => i > start && /^ {2}\/\/ \S/.test(l));
  assert.ok(start >= 0 && end > start, "slide block not found in deck.js");
  const canvases = [];
  const recorder = () => {
    const ops = [],
      state = { shadowBlur: 0 },
      stack = [];
    const own = {
      save: () => stack.push({ ...state }),
      restore: () => Object.assign(state, stack.pop()),
      createRadialGradient: () => ({ addColorStop() {} }),
    };
    const ctx = new Proxy(state, {
      get: (_, k) => own[k] || (k in state ? state[k] : (...args) => ops.push({ op: k, blur: state.shadowBlur, args })),
      set: (_, k, v) => ((state[k] = v), true),
    });
    return { ctx, ops };
  };
  const el = (tag, attrs = {}) => {
    const e = { tag, attrs, classList: { add() {}, remove() {} }, querySelector: () => ({}) };
    if (tag === "canvas") {
      e.rec = recorder();
      e.getContext = () => e.rec.ctx;
      canvases.push(e);
    }
    return e;
  };
  const slides = [];
  new Function("slides", "h", "letters", "resetIndex", "document", lines.slice(start, end).join("\n"))(
    slides,
    el,
    () => el("span"),
    () => {},
    { createElement: tag => el(tag) }
  );
  const [slide] = slides;
  const stage = canvases.find(c => c.attrs.class === "ch-cv");
  let t = 1000;
  const frames = n => {
    stage.rec.ops.length = 0;
    for (let i = 0; i < n; i++) slide.tick((t += 1000 / 60));
    return stage.rec.ops;
  };
  return { slide, canvases, frames };
};

test("#supply-chain-hardening keeps animating without per-frame shadow blurs", () => {
  const { slide, canvases, frames } = runSupplyChainSlide();
  slide.enter(1);
  frames(180); // one full core pulse bakes every glow sprite
  const baked = canvases.length;
  const ops = frames(300);
  assert.deepEqual(
    ops.filter(o => o.blur > 0).map(o => o.op),
    [],
    "steady frames drew shapes with shadowBlur"
  );
  assert.equal(canvases.length, baked, "glow sprites were rebuilt instead of reused");
  const count = op => ops.filter(o => o.op === op).length;
  assert.ok(count("drawImage") >= 300, "core glow sprite not drawn every frame");
  assert.ok(count("stroke") >= 300 * 6 * 3, "rotating shells and their glow strokes not drawn every frame");
  const blocked = ops.filter(o => o.op === "fillText" && o.args[1] === 975 && o.args[2] === 654).map(o => +o.args[0]);
  assert.ok(blocked.at(-1) > blocked[0], `blocked counter stalled at ${blocked[0]}`);
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
