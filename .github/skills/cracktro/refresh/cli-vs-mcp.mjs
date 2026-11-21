#!/usr/bin/env node
// Read-only: same-workflow before/after comparison of the GitHub MCP server (tools.github.mode: local)
// versus gh-proxy (`gh` CLI, no GitHub MCP tool schemas). Mines each run's `usage` artifact.
// Run from the repo root (needs git history and `gh auth login`):
//   node .github/skills/cracktro/refresh/cli-vs-mcp.mjs > out.json      (RAW=rows.json dumps per-run rows)
// Env: CAP runs per side nearest the switch (15), DAYS max window per side (7), CONTROLS (15),
// CACHE trusted directory for reuse across invocations (default: private temporary directory).
import { execFile } from "node:child_process";
import { rmSync } from "node:fs";
import { promisify } from "node:util";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

const sh = promisify(execFile);
const REPO = process.env.REPO || "github/gh-aw";
const CAP = +(process.env.CAP || 15);
const DAYS = +(process.env.DAYS || 7);
const NCTRL = +(process.env.CONTROLS || 15);
const CACHE = process.env.CACHE || (await mkdtemp(join(tmpdir(), "cvm-cache-")));
if (process.env.CACHE) {
  await mkdir(CACHE, { recursive: true, mode: 0o700 });
} else {
  process.once("exit", () => rmSync(CACHE, { recursive: true, force: true }));
}
const MIN = 3; // runs per side for a workflow to count

// Commits that flip tools.github.mode between local (MCP) and gh-proxy and change nothing else that
// affects the prompt. Every .md that flips in the commit becomes a case; engine/model edits are excluded.
const SWITCHES = [
  { commit: "d893c7da042", note: "gh-proxy -> local in bulk; same commit moved sandbox runtime to cloud-hypervisor" },
  { commit: "e973b8cc974", note: "ponytail-reviewer: local -> gh-proxy (+ cli-proxy: true)" },
];
const CONTROL_AT = "d893c7da042"; // drift control: workflows with no .md edit within DAYS of this commit

const out = async (cmd, args) => (await sh(cmd, args, { encoding: "utf8", maxBuffer: 1 << 28 })).stdout;
const git = (...a) => out("git", a);
const iso = s => new Date(s).toISOString().replace(".000", "");
const shift = (t, d) => iso(new Date(+new Date(t) + d * 864e5));
const med = a => {
  if (!a.length) return null;
  const s = [...a].sort((x, y) => x - y),
    m = s.length >> 1;
  return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2;
};
const pct = (a, b) => (a ? Math.round(((b - a) / a) * 1000) / 10 : null);
// /responses and OpenAI-style chat count cached tokens inside input_tokens; Anthropic counts them separately.
const inclusive = r => r.path === "/responses" || (!/claude/i.test(r.model) && (r.cache_read_tokens || 0) <= (r.input_tokens || 0));
const promptOf = r => (r.input_tokens || 0) + (inclusive(r) ? 0 : (r.cache_read_tokens || 0) + (r.cache_write_tokens || 0));

async function pool(items, n, fn) {
  const res = [];
  let i = 0;
  await Promise.all(
    Array.from({ length: n }, async () => {
      while (i < items.length) {
        const k = i++;
        res[k] = await fn(items[k], k);
      }
    })
  );
  return res;
}

const editsOf = async f => (await git("log", "--format=%cI", "--", `.github/workflows/${f}.md`)).split("\n").filter(Boolean).map(iso);

async function casesFor({ commit, note }) {
  const at = iso((await git("show", "-s", "--format=%cI", commit)).trim());
  const diff = await git("show", "--format=", "-U0", commit, "--", ".github/workflows/*.md");
  const files = {};
  let f;
  for (const l of diff.split("\n")) {
    const m = l.match(/^\+\+\+ b\/\.github\/workflows\/(.+)\.md$/);
    if (m) {
      f = m[1];
      files[f] = {};
      continue;
    }
    if (!f || f.includes("/")) continue;
    const mode = l.match(/^([-+])\s+mode: (gh-proxy|local)\s*$/);
    if (mode) files[f][mode[1]] = mode[2];
    if (/^[-+](engine:|model:|\s+id:|\s+model:)/.test(l)) files[f].engine = true;
    if (/^\+\s+github:\s*$/.test(l)) files[f].addedGithub = true;
  }
  const cases = [];
  for (const [wf, x] of Object.entries(files)) {
    x["-"] ??= x.addedGithub ? "local" : undefined; // a new github: block replaces the default (local MCP)
    if (x.engine || !x["+"] || !x["-"] || x["-"] === x["+"]) continue;
    const edits = await editsOf(wf);
    const prev = edits.filter(t => t < at)[0] ?? shift(at, -DAYS);
    const next = edits.filter(t => t > at).at(-1) ?? shift(at, DAYS);
    const before = [prev > shift(at, -DAYS) ? prev : shift(at, -DAYS), at];
    const after = [at, next < shift(at, DAYS) ? next : shift(at, DAYS)];
    // Read the MCP side's compiled lock: GH_AW_MCP_CLI_SERVERS_LIST names servers that cli-proxy mounts as CLIs
    // on PATH (no tool schemas in context). Frontmatter alone misses cli-proxy set by imports.
    const lock = await git("show", `${x["-"] === "local" ? commit + "^" : commit}:.github/workflows/${wf}.lock.yml`).catch(() => "");
    const mcpCli = /GH_AW_MCP_CLI_SERVERS_LIST: .*`github`/.test(lock);
    cases.push({ wf, commit, note, at, from: x["-"], to: x["+"], mcpSurface: mcpCli ? "cli" : "schemas", before, after });
  }
  return cases;
}

async function controls() {
  const at = iso((await git("show", "-s", "--format=%cI", CONTROL_AT)).trim());
  const edited = new Set((await git("log", `--since=${shift(at, -DAYS)}`, `--until=${shift(at, DAYS)}`, "--name-only", "--format=", "--", ".github/workflows/*.md")).split("\n").filter(Boolean));
  const all = (await git("ls-tree", "--name-only", `${CONTROL_AT}^`, ".github/workflows/")).split("\n").filter(p => p.endsWith(".md") && !edited.has(p));
  return all.map(p => ({ wf: p.replace(/^.*\//, "").replace(/\.md$/, ""), commit: CONTROL_AT, at, control: true, before: [shift(at, -DAYS), at], after: [at, shift(at, DAYS)] }));
}

async function listRuns(c) {
  try {
    const j = await out("gh", [
      "run",
      "list",
      "-R",
      REPO,
      "--workflow",
      `${c.wf}.lock.yml`,
      "--status",
      "success",
      "--created",
      `${c.before[0].slice(0, 10)}..${c.after[1].slice(0, 10)}`,
      "--limit",
      "1000",
      "--json",
      "databaseId,createdAt,event",
    ]);
    const runs = JSON.parse(j);
    const side = ([a, b]) => runs.filter(r => r.createdAt >= a && r.createdAt < b);
    // nearest the switch first
    return { before: side(c.before).sort((x, y) => y.createdAt.localeCompare(x.createdAt)), after: side(c.after).sort((x, y) => x.createdAt.localeCompare(y.createdAt)) };
  } catch {
    return { before: [], after: [] };
  }
}

async function mine(id) {
  const file = join(CACHE, `${id}.json`);
  try {
    return JSON.parse(await readFile(file, "utf8"));
  } catch {}
  const dir = await mkdtemp(join(tmpdir(), "cvm-"));
  let row = null;
  try {
    await out("gh", ["run", "download", String(id), "-R", REPO, "-n", "usage", "-D", dir]);
    const reqs = (await readFile(join(dir, "agent", "token_usage.jsonl"), "utf8"))
      .split("\n")
      .filter(Boolean)
      .map(l => JSON.parse(l))
      .filter(r => r.event === "token_usage" && r.status === 200)
      .sort((a, b) => a.timestamp.localeCompare(b.timestamp));
    if (reqs.length)
      row = {
        model: reqs[0].model,
        path: reqs[0].path,
        requests: reqs.length,
        first: promptOf(reqs[0]),
        prompt: reqs.reduce((s, r) => s + promptOf(r), 0),
        output: reqs.reduce((s, r) => s + (r.output_tokens || 0), 0),
        aic: Math.round(reqs.reduce((s, r) => s + (r.ai_credits_this_response || 0), 0) * 100) / 100,
      };
  } catch {
  } finally {
    // no usage artifact: agent skipped, or artifact expired
    await rm(dir, { recursive: true, force: true });
  }
  await writeFile(file, JSON.stringify(row));
  return row;
}

const METRICS = ["first", "prompt", "requests", "output", "aic"];
async function measure(c, runs, raw) {
  const sides = {};
  for (const s of ["before", "after"]) {
    const rows = [];
    for (const r of runs[s]) {
      if (rows.length >= CAP) break;
      const m = await mine(r.databaseId);
      if (m) rows.push({ run: r.databaseId, at: r.createdAt, event: r.event, ...m });
    }
    sides[s] = rows;
  }
  const model = Object.entries([...sides.before, ...sides.after].reduce((a, r) => ((a[r.model] = (a[r.model] || 0) + 1), a), {})).sort((a, b) => b[1] - a[1])[0]?.[0];
  const res = { wf: c.wf, commit: c.commit, control: !!c.control, from: c.from, to: c.to, mcpSurface: c.mcpSurface, at: c.at, model };
  for (const s of ["before", "after"]) {
    const rs = sides[s].filter(r => r.model === model);
    raw.push(...rs.map(r => ({ wf: c.wf, control: !!c.control, side: s, mode: c.control ? null : s === "before" ? c.from : c.to, ...r })));
    res[s] = { n: rs.length, window: c[s], ...Object.fromEntries(METRICS.map(k => [k, med(rs.map(r => r[k]))])) };
  }
  res.ok = res.before.n >= MIN && res.after.n >= MIN;
  // % change of the gh-proxy side vs the MCP side. Controls: before vs after, time-aligned with the bulk
  // switch (gh-proxy was the before period), so bulk minus control removes platform drift.
  const [mcp, proxy] = c.control ? [res.after, res.before] : c.from === "local" ? [res.before, res.after] : [res.after, res.before];
  res.delta = Object.fromEntries(METRICS.map(k => [k, pct(mcp[k], proxy[k])]));
  return res;
}

const raw = [];
const flips = (await Promise.all(SWITCHES.map(casesFor))).flat();
const ctrl = await controls();
const listed = await pool([...flips, ...ctrl], 8, async c => ({ c, runs: await listRuns(c) }));
const enough = x => x.runs.before.length >= MIN && x.runs.after.length >= MIN;
const flipSel = listed.filter(x => !x.c.control && enough(x));
const ctrlSel = listed
  .filter(x => x.c.control && enough(x))
  .sort((a, b) => b.runs.before.length + b.runs.after.length - (a.runs.before.length + a.runs.after.length))
  .slice(0, NCTRL);
process.stderr.write(`cases: ${flipSel.length}/${flips.length} switches, ${ctrlSel.length}/${ctrl.length} controls\n`);
const results = await pool([...flipSel, ...ctrlSel], 6, async x => {
  const r = await measure(x.c, x.runs, raw);
  process.stderr.write(`${r.control ? "ctrl" : "flip"} ${r.wf}: ${r.before.n}/${r.after.n} ${JSON.stringify(r.delta)}\n`);
  return r;
});
const summarize = rs => {
  const ok = rs.filter(r => r.ok);
  return {
    workflows: ok.length,
    runs: ok.reduce((s, r) => s + r.before.n + r.after.n, 0),
    ...Object.fromEntries(METRICS.map(k => [k, { medianPct: med(ok.map(r => r.delta[k]).filter(v => v != null)), lower: ok.filter(r => r.delta[k] < 0).length }])),
  };
};
const flipsOut = results.filter(r => !r.control);
const report = {
  repo: REPO,
  snapshot: new Date().toISOString().slice(0, 10),
  cap: CAP,
  days: DAYS,
  metric: "per run: first = prompt tokens of request 1; prompt = sum of prompt tokens over requests; aic = AI credits. delta = % change of gh-proxy vs GitHub MCP (controls: before vs after, same orientation as the bulk switch)",
  switches: SWITCHES,
  summary: {
    mcpAsCli: summarize(flipsOut.filter(r => r.mcpSurface === "cli")), // GitHub MCP behind cli-proxy vs gh
    mcpSchemas: summarize(flipsOut.filter(r => r.mcpSurface === "schemas")), // GitHub MCP tool schemas in context vs gh
    controls: summarize(results.filter(r => r.control)),
  },
  cases: results,
};
if (process.env.RAW) await writeFile(process.env.RAW, JSON.stringify(raw));
console.log(JSON.stringify(report, null, 1));
