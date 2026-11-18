#!/usr/bin/env node
// Cracktro slide performance guard: drives the real deck in headless Chrome over the DevTools protocol
// (no npm dependencies), dwells on every slide, revisits the deck, and fails when a slide leaves work
// running off screen, grows without bound, or keeps the main thread busy.
// Usage and thresholds: .github/skills/cracktro/perf.md. Exit 0 pass, 1 budget exceeded, 2 tool error.
import { spawn } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { extname, join, normalize, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";

const DOCS = fileURLToPath(new URL("..", import.meta.url));

// Budgets; every value is overridable with the flag of the same name (see --help).
export const BUDGETS = {
  // main-thread busy fraction while idling on one slide (Performance TaskDuration / wall time); headless software
  // rendering puts all canvas raster here, so a deliberate full-stage canvas animation measures about 0.5 and saturation about 1
  "max-busy": 0.75,
  "min-fps": 30, // frames delivered per second while idling on one slide
  "max-offscreen-animations": 0, // running CSS/Web animations inside slides that are not on screen
  "max-foreign-callbacks": 1, // timer/rAF callbacks per second still firing for slides that are not on screen
  "max-dwell-growth": 60, // elements added while idling on one slide (strictly increasing across both windows)
  "max-tour-growth-elements": 100, // elements left behind by each extra tour of the deck
  "max-tour-growth-listeners": 50, // event listeners left behind by each extra tour
  "max-tour-growth-intervals": 0, // live setInterval timers left behind by each extra tour
  "max-tour-growth-heap-mb": 4, // JS heap (after GC) left behind by each extra tour
};

const OPTIONS = {
  root: { type: "string", default: join(DOCS, "public") },
  path: { type: "string", default: "/cracktro/" },
  slides: { type: "string" },
  settle: { type: "string", default: "1300" },
  window: { type: "string", default: "800" },
  revisits: { type: "string", default: "2" },
  chrome: { type: "string" },
  "chrome-arg": { type: "string", multiple: true, default: (process.env.CRACKTRO_PERF_CHROME_ARGS || "").split(/\s+/).filter(Boolean) },
  json: { type: "string" },
  heartbeat: { type: "string", default: "5000" },
  "cdp-timeout": { type: "string", default: "60000" },
  help: { type: "boolean" },
  ...Object.fromEntries(Object.keys(BUDGETS).map(k => [k, { type: "string" }])),
};

const MIME = {
  ".html": "text/html",
  ".js": "text/javascript",
  ".mjs": "text/javascript",
  ".css": "text/css",
  ".json": "application/json",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".woff2": "font/woff2",
  ".woff": "font/woff",
  ".ttf": "font/ttf",
};

export function serve(root) {
  const server = createServer((req, res) => {
    let p = decodeURIComponent(new URL(req.url, "http://x").pathname);
    if (p.endsWith("/")) p += "index.html";
    const file = normalize(join(root, p));
    if (!file.startsWith(root) || !existsSync(file)) return res.writeHead(404).end();
    try {
      res.writeHead(200, { "content-type": MIME[extname(file)] || "application/octet-stream" }).end(readFileSync(file));
    } catch {
      res.writeHead(404).end();
    }
  });
  return new Promise(ok => server.listen(0, "127.0.0.1", () => ok(server)));
}

export function findChrome(explicit) {
  const candidates = [
    explicit,
    process.env.CHROME_PATH,
    "/usr/bin/chromium-browser",
    "/usr/bin/chromium",
    "/usr/bin/google-chrome",
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    "/Applications/Chromium.app/Contents/MacOS/Chromium",
    "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
  ];
  return candidates.find(c => c && existsSync(c));
}

// Minimal CDP client over --remote-debugging-pipe (fd 3 in, fd 4 out, NUL-delimited JSON).
// Every command fails after timeoutMs, so a Chrome that never answers cannot hang the run.
export function launch(chrome, extra, timeoutMs = 60000) {
  const profile = mkdtempSync(join(tmpdir(), "cracktro-perf-"));
  const args = [
    "--headless=new",
    "--remote-debugging-pipe",
    `--user-data-dir=${profile}`,
    "--no-first-run",
    "--no-default-browser-check",
    "--mute-audio",
    "--hide-scrollbars",
    "--disable-extensions",
    "--disable-background-timer-throttling",
    "--disable-renderer-backgrounding",
    "--disable-backgrounding-occluded-windows",
    "--window-size=1920,1080",
    ...extra,
    "about:blank",
  ];
  const proc = spawn(chrome, args, { stdio: ["ignore", "ignore", "pipe", "pipe", "pipe"] });
  let id = 0,
    buf = "",
    stderr = "";
  const pending = new Map(),
    listeners = [];
  proc.stderr.on("data", d => (stderr = (stderr + d).slice(-4000)));
  proc.stdio[4].on("data", d => {
    buf += d;
    let i;
    while ((i = buf.indexOf("\0")) >= 0) {
      const msg = JSON.parse(buf.slice(0, i));
      buf = buf.slice(i + 1);
      if (msg.id && pending.has(msg.id)) {
        const { ok, fail, method } = pending.get(msg.id);
        pending.delete(msg.id);
        msg.error ? fail(new Error(`${method}: ${msg.error.message}`)) : ok(msg.result);
      } else if (msg.method) listeners.forEach(l => l(msg));
    }
  });
  let dead = null;
  const exited = new Promise(ok => proc.on("exit", ok));
  proc.on("error", e => (dead = e));
  proc.stdio[3].on("error", () => {});
  exited.then(code => {
    dead ??= new Error(`Chrome exited (${code}): ${stderr.trim().split("\n").slice(-3).join(" ")}`);
    pending.forEach(p => p.fail(dead));
  });
  const send = (method, params = {}, sessionId) =>
    new Promise((ok, fail) => {
      if (dead) return fail(dead);
      const n = ++id;
      const timer = setTimeout(() => {
        pending.delete(n);
        fail(new Error(`${method}: no reply from Chrome after ${timeoutMs / 1000}s`));
      }, timeoutMs);
      pending.set(n, { ok: v => (clearTimeout(timer), ok(v)), fail: e => (clearTimeout(timer), fail(e)), method });
      proc.stdio[3].write(JSON.stringify({ id: n, method, params, sessionId }) + "\0");
    });
  const close = async () => {
    if (!dead) await Promise.race([send("Browser.close").catch(() => {}), new Promise(r => setTimeout(r, 2000))]);
    proc.kill();
    await Promise.race([exited, new Promise(r => setTimeout(r, 2000))]);
    rmSync(profile, { recursive: true, force: true });
  };
  return { send, on: l => listeners.push(l), close };
}

// Runs in the page before any deck script: captures the deck instance and attributes timers,
// rAF loops and their callbacks to the slide that started them. Work started by the runtime
// (its frame loop, before the deck exists) is "boot"; slide hooks run as their slide.
export const PROBE = `(() => {
  const P = (window.__perf = { deck: null, frames: 0, maxGap: 0, inCb: null, intervals: new Map(), timeouts: new Map(), fired: {}, errors: [] });
  const owner = () => (P.inCb != null ? P.inCb : P.deck ? P.deck.current() : "boot");
  const own = (s, i) => { for (const k of ["enter", "tick", "onNext", "onPrev", "onKey"]) if (typeof s[k] === "function") { const f = s[k]; s[k] = function (...x) { const prev = P.inCb; P.inCb = i; try { return f.apply(this, x); } finally { P.inCb = prev; } }; } };
  const fire = (o, f, x) => { P.fired[o] = (P.fired[o] || 0) + 1; const prev = P.inCb; P.inCb = o; try { return typeof f === "function" ? f(...x) : undefined; } finally { P.inCb = prev; } };
  const si = setInterval, ci = clearInterval, st = setTimeout, ct = clearTimeout, raf = requestAnimationFrame;
  window.setInterval = (f, ms, ...a) => { const o = owner(); const id = si((...x) => fire(o, f, x), ms, ...a); P.intervals.set(id, o); return id; };
  window.clearInterval = id => { P.intervals.delete(id); ci(id); };
  window.setTimeout = (f, ms, ...a) => { const o = owner(); const id = st((...x) => (P.timeouts.delete(id), fire(o, f, x)), ms, ...a); P.timeouts.set(id, o); return id; };
  window.clearTimeout = id => { P.timeouts.delete(id); ct(id); };
  window.requestAnimationFrame = f => { const o = owner(); return raf(t => fire(o, f, [t])); };
  let last = 0;
  const count = t => { if (last) P.maxGap = Math.max(P.maxGap, t - last); last = t; P.frames++; raf(count); };
  raf(count);
  addEventListener("error", e => P.errors.push(String(e.message)));
  let CD;
  Object.defineProperty(window, "CracktroDeck", { configurable: true, get: () => CD, set(v) {
    const create = v.create;
    CD = { ...v, create: cfg => { P.deck = create(cfg); P.deck.slides.forEach(own); return P.deck; } };
  } });
  const tally = m => { const r = {}; for (const o of m.values()) r[o] = (r[o] || 0) + 1; return r; };
  P.snap = () => {
    const d = P.deck, cur = d.current(), el = d.slides[cur].el, idx = new Map(d.slides.map((s, i) => [s.el, i]));
    const off = {};
    let running = 0;
    for (const a of document.getAnimations()) {
      if (a.playState !== "running") continue;
      running++;
      const s = a.effect && a.effect.target && a.effect.target.closest && a.effect.target.closest(".slide");
      if (s && s !== el && idx.has(s) && !s.classList.contains("leaving")) off[idx.get(s)] = (off[idx.get(s)] || 0) + 1;
    }
    const r = { cur, t: performance.now(), frames: P.frames, maxGap: P.maxGap, els: document.getElementsByTagName("*").length, running, off, intervals: tally(P.intervals), timeouts: P.timeouts.size, fired: { ...P.fired }, errors: P.errors.splice(0) };
    P.maxGap = 0;
    return r;
  };
})();`;

const sleep = ms => new Promise(r => setTimeout(r, ms));
const sum = o => Object.values(o).reduce((a, b) => a + b, 0);

// Logs elapsed time and the current phase every `every` ms (0 disables) so long sweeps are never silent.
export function heartbeat(log, every = 5000, now = Date.now) {
  const t0 = now();
  let phase = "starting";
  const elapsed = () => `${Math.round((now() - t0) / 1000)}s`;
  const timer = every > 0 ? setInterval(() => log(`… ${elapsed()} ${phase}`), every) : null;
  return {
    elapsed,
    get phase() {
      return phase;
    },
    set: p => (phase = p),
    stop: () => clearInterval(timer),
  };
}

export async function run(argv = process.argv.slice(2), log = console.log) {
  const { values: o } = parseArgs({ args: argv, options: OPTIONS, allowPositionals: false });
  if (o.help) {
    log(
      `node docs/scripts/cracktro-perf.mjs [--slides id,id] [--settle ms] [--window ms] [--revisits n] [--json file] [--chrome path] [--root dir --path /page.html] [--heartbeat ms (0 off)] [--cdp-timeout ms]\nBudgets (flag = default):\n${Object.entries(
        BUDGETS
      )
        .map(([k, v]) => `  --${k}=${v}`)
        .join("\n")}`
    );
    return 0;
  }
  // Validate numbers before starting anything: NaN or negative values would silently disable checks or timers.
  const num = (name, { positive = false, integer = false } = {}) => {
    const raw = o[name],
      v = Number(raw);
    if (!/\S/.test(raw) || !Number.isFinite(v) || v < 0 || (positive && v === 0) || (integer && !Number.isInteger(v)))
      throw new Error(`--${name} must be a finite ${integer ? "integer" : "number"} ${positive ? "> 0" : ">= 0"}, got ${JSON.stringify(raw)}`);
    return v;
  };
  const budget = Object.fromEntries(Object.entries(BUDGETS).map(([k, v]) => [k, o[k] != null ? num(k) : v]));
  const settle = num("settle"),
    win = num("window", { positive: true }),
    revisits = num("revisits", { integer: true }),
    cdpTimeout = num("cdp-timeout", { positive: true }),
    every = num("heartbeat");
  const chrome = findChrome(o.chrome);
  if (!chrome) throw new Error("No Chrome/Chromium found; pass --chrome or set CHROME_PATH.");
  const server = await serve(resolve(o.root));
  const cdp = launch(chrome, o["chrome-arg"], cdpTimeout);
  const beat = heartbeat(log, every);
  const phase = (p, announce) => (beat.set(p), announce && log(`… ${beat.elapsed()} ${p}`));
  try {
    phase(`starting ${chrome}`, true);
    const { targetId } = await cdp.send("Target.createTarget", { url: "about:blank" });
    const { sessionId: S } = await cdp.send("Target.attachToTarget", { targetId, flatten: true });
    const send = (m, p) => cdp.send(m, p, S);
    const pageErrors = [];
    cdp.on(m => m.sessionId === S && m.method === "Runtime.exceptionThrown" && pageErrors.push(m.params.exceptionDetails.exception?.description || m.params.exceptionDetails.text));
    for (const m of ["Page.enable", "Runtime.enable", "Performance.enable"]) await send(m);
    await send("Emulation.setDeviceMetricsOverride", { width: 1920, height: 1080, deviceScaleFactor: 1, mobile: false });
    await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-reduced-motion", value: "no-preference" }] });
    await send("Page.addScriptToEvaluateOnNewDocument", { source: PROBE });
    const evaluate = async expr => {
      const r = await send("Runtime.evaluate", { expression: expr, returnByValue: true, awaitPromise: true });
      if (r.exceptionDetails) throw new Error(`page: ${r.exceptionDetails.exception?.description || r.exceptionDetails.text}`);
      return r.result.value;
    };
    const metrics = async () => Object.fromEntries((await send("Performance.getMetrics")).metrics.map(m => [m.name, m.value]));
    phase(`loading ${o.path}`, true);
    await send("Page.navigate", { url: `http://127.0.0.1:${server.address().port}${o.path}` });
    for (let i = 0; !(await evaluate("!!(window.__perf && __perf.deck)")); i++) {
      if (i > 150) throw new Error(`deck did not start at ${o.path}: ${pageErrors.join("; ") || "no CracktroDeck.create call"}`);
      await sleep(100);
    }
    await evaluate("document.fonts.ready.then(() => 1)");
    const ids = await evaluate("__perf.deck.slides.map(s => s.id)");
    const want = o.slides ? o.slides.split(",").map(s => s.trim()) : ids;
    const unknown = want.filter(id => !ids.includes(id));
    if (unknown.length) throw new Error(`unknown slide ids: ${unknown.join(", ")}`);
    const order = want.map(id => ids.indexOf(id));
    const total = order.length;
    phase(`measuring ${total} slides`, true);

    const goTo = async n => {
      for (let i = 0; (await evaluate(`(__perf.deck.go(${n}), __perf.deck.current())`)) !== n; i++) {
        if (i > 80) throw new Error(`could not navigate to #${ids[n]}`);
        await sleep(50);
      }
    };
    const sample = async () => ({ m: await metrics(), p: await evaluate("__perf.snap()") });
    const tourState = async () => {
      await goTo(order[0]);
      await sleep(settle);
      await send("HeapProfiler.collectGarbage");
      const m = await metrics(),
        p = await evaluate("__perf.snap()");
      return { els: p.els, listeners: m.JSEventListeners, intervals: sum(p.intervals), heapMb: m.JSHeapUsedSize / 2 ** 20 };
    };

    const rows = [];
    const violations = [];
    const flag = (id, check, value, limit, detail = "") => violations.push({ slide: id, check, value, limit, detail, op: check.startsWith("min-") ? "<" : ">" });
    await goTo(order[0]);
    await sleep(settle);
    for (const [i, n] of order.entries()) {
      const id = ids[n];
      phase(`measuring slide ${i + 1}/${total} #${id}`);
      await goTo(n);
      await sleep(settle);
      const a = await sample();
      await sleep(win);
      const b = await sample();
      await sleep(win);
      const c = await sample();
      const wall = (c.p.t - a.p.t) / 1000;
      const busy = (c.m.TaskDuration - a.m.TaskDuration) / wall;
      const fps = (c.p.frames - a.p.frames) / wall;
      const foreign = {};
      for (const [k, v] of Object.entries(c.p.fired)) if (k !== "boot" && +k !== n) foreign[k] = (v - (a.p.fired[k] || 0)) / wall;
      const offscreen = sum(c.p.off);
      const grew = b.p.els > a.p.els && c.p.els > b.p.els ? c.p.els - a.p.els : 0;
      const row = {
        id,
        busy: +busy.toFixed(3),
        fps: +fps.toFixed(1),
        maxGapMs: Math.round(Math.max(b.p.maxGap, c.p.maxGap)),
        scriptMsPerS: +(((c.m.ScriptDuration - a.m.ScriptDuration) * 1000) / wall).toFixed(1),
        styleMsPerS: +(((c.m.RecalcStyleDuration - a.m.RecalcStyleDuration) * 1000) / wall).toFixed(1),
        layoutMsPerS: +(((c.m.LayoutDuration - a.m.LayoutDuration) * 1000) / wall).toFixed(1),
        running: c.p.running,
        offscreen,
        foreignPerS: +sum(foreign).toFixed(1),
        els: c.p.els,
        dwellGrowth: grew,
        listeners: c.m.JSEventListeners,
        intervals: sum(c.p.intervals),
        errors: [...a.p.errors, ...b.p.errors, ...c.p.errors],
      };
      rows.push(row);
      if (busy > budget["max-busy"]) flag(id, "max-busy", row.busy, budget["max-busy"], `script ${row.scriptMsPerS} style ${row.styleMsPerS} layout ${row.layoutMsPerS} ms/s`);
      if (fps < budget["min-fps"]) flag(id, "min-fps", row.fps, budget["min-fps"], `max frame gap ${row.maxGapMs} ms`);
      if (offscreen > budget["max-offscreen-animations"])
        flag(
          id,
          "max-offscreen-animations",
          offscreen,
          budget["max-offscreen-animations"],
          Object.entries(c.p.off)
            .sort((x, y) => y[1] - x[1])
            .slice(0, 5)
            .map(([k, v]) => `#${ids[k]}×${v}`)
            .join(" ")
        );
      for (const [k, v] of Object.entries(foreign)) if (v > budget["max-foreign-callbacks"]) flag(id, "max-foreign-callbacks", +v.toFixed(1), budget["max-foreign-callbacks"], `from #${ids[k]}`);
      if (grew > budget["max-dwell-growth"]) flag(id, "max-dwell-growth", grew, budget["max-dwell-growth"], `${a.p.els} → ${c.p.els} elements`);
      if (row.errors.length) flag(id, "page-errors", row.errors.length, 0, row.errors[0]);
      log(
        `${violations.some(v => v.slide === id) ? "FAIL" : "ok  "} ${`${i + 1}/${total}`.padStart(7)} #${id.padEnd(34)} busy ${(busy * 100).toFixed(0).padStart(3)}%  fps ${fps.toFixed(0).padStart(3)}  style ${row.styleMsPerS.toFixed(0).padStart(4)}ms/s  anim ${String(row.running).padStart(4)} off ${String(offscreen).padStart(4)}  foreign ${row.foreignPerS.toFixed(0).padStart(3)}/s  els ${row.els}${grew ? ` +${grew}` : ""}`
      );
    }

    // Revisits: tour the deck again; whatever every extra tour leaves behind is a leak.
    phase("revisit baseline: collecting garbage");
    const tours = [await tourState()];
    for (let r = 0; r < revisits; r++) {
      phase(`revisit ${r + 1}/${revisits}: touring ${total} slides`, true);
      for (const [i, n] of order.entries()) {
        beat.set(`revisit ${r + 1}/${revisits}, slide ${i + 1}/${total} #${ids[n]}`);
        await goTo(n);
        await sleep(Math.min(settle, 900));
      }
      beat.set(`revisit ${r + 1}/${revisits}: collecting garbage`);
      tours.push(await tourState());
      const t = tours.at(-1);
      log(`… ${beat.elapsed()} revisit ${r + 1}/${revisits} done: elements ${t.els}, listeners ${t.listeners}, intervals ${t.intervals}, heap ${t.heapMb.toFixed(1)} MB`);
    }
    const growth = {};
    if (tours.length > 1) {
      for (const [k, b] of [
        ["els", "max-tour-growth-elements"],
        ["listeners", "max-tour-growth-listeners"],
        ["intervals", "max-tour-growth-intervals"],
        ["heapMb", "max-tour-growth-heap-mb"],
      ]) {
        // the smallest per-tour increase, so one-off lazy setup on a second visit is not a leak
        growth[k] = +Math.min(...tours.slice(1).map((t, i) => t[k] - tours[i][k])).toFixed(2);
        if (growth[k] > budget[b]) flag("(revisit)", b, growth[k], budget[b], tours.map(t => +t[k].toFixed(1)).join(" → "));
      }
      log(`revisit x${revisits}: per-tour growth elements ${growth.els}, listeners ${growth.listeners}, intervals ${growth.intervals}, heap ${growth.heapMb} MB`);
    }
    if (pageErrors.length) flag("(page)", "page-errors", pageErrors.length, 0, pageErrors[0]);

    const busiest = [...rows].sort((x, y) => y.busy - x.busy).slice(0, 5);
    log(`busiest: ${busiest.map(r => `#${r.id} ${(r.busy * 100).toFixed(0)}%`).join(", ")}`);
    if (o.json) writeFileSync(o.json, JSON.stringify({ chrome, budget, settle, window: win, revisits, rows, tours, growth, violations }, null, 2));
    if (violations.length) {
      log(`\n${violations.length} budget violation(s):`);
      for (const v of violations) log(`  #${v.slide} ${v.check}: ${v.value} ${v.op} ${v.limit}${v.detail ? ` (${v.detail})` : ""}`);
      return 1;
    }
    log(`\nAll ${rows.length} slides within budget.`);
    return 0;
  } catch (e) {
    e.message += ` (while ${beat.phase}, after ${beat.elapsed()})`;
    throw e;
  } finally {
    beat.stop();
    await cdp.close();
    server.close();
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  run().then(
    code => process.exit(code),
    err => {
      console.error(`cracktro-perf: ${err.message}`);
      process.exit(2);
    }
  );
}
