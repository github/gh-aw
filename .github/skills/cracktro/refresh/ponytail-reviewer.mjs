#!/usr/bin/env node
// Read-only: recompute the "Ponytail Reviewer" cracktro strip from gh-aw pull requests and workflow runs. Needs `gh auth login`.
// Usage: node ponytail-reviewer.mjs [START [END]]   (YYYY-MM-DD, inclusive; default: 2026-08-06 to yesterday UTC)
// Prints one JSON object to stdout; progress and warnings go to stderr. `--self-test` checks the noop classifier offline.
// SAMPLE=N also downloads the agent-output-fallback artifact of every Nth run in the window (into $TMPDIR) to measure the verdict share and per-run AI credits.
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import assert from "node:assert/strict";
import { existsSync, mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const run = promisify(execFile);
const DAY = 864e5;
const iso = d => new Date(d).toISOString().slice(0, 10);
const LEAN = /\blean already\b/i;
const SKIP = /not performed|(cannot|can't|unable to) (run|review|read)|(diff|metadata|patch|json)\b[^.]{0,40}\b(missing|empty|not found|0 bytes)/i;
// One sampled run's verdict from its agent_output.json items: a review, a nothing-to-cut noop (`lean`, or `clean` in other words),
// a diagnostic noop that never assessed the code (`skip`, e.g. missing pre-fetched diff), or `other` (report_incomplete, ...).
const classify = items => {
  const types = items.map(i => i.type);
  if (types.includes("submit_pull_request_review") || types.includes("create_pull_request_review_comment")) return "review";
  const msg = items.filter(i => i.type === "noop").map(i => i.message || "").join(" ");
  if (!types.includes("noop")) return "other";
  return LEAN.test(msg) ? "lean" : SKIP.test(msg) ? "skip" : "clean";
};
if (process.argv[2] === "--self-test") {
  const noop = message => [{ type: "noop", message }];
  assert.equal(classify(noop("Lean already. Ship.")), "lean");
  assert.equal(classify(noop("Lean already. Ship. The diff only adds t.Parallel() calls.")), "lean");
  assert.equal(classify(noop("Ponytail review complete: no over-engineering findings.")), "clean");
  assert.equal(classify(noop("Diff is minimal. The server.error -> server.debugError change is a 1-line fix.")), "clean");
  assert.equal(classify(noop("Review not performed: the prefetched PR metadata or diff is missing or empty.")), "skip");
  assert.equal(classify(noop("Cannot run ponytail review: /tmp/gh-aw/agent/pr-diff.patch is empty (0 bytes).")), "skip");
  assert.equal(classify([...noop("Lean already. Ship."), { type: "create_pull_request_review_comment" }]), "review");
  assert.equal(classify([{ type: "report_incomplete" }]), "other");
  console.log("self-test ok");
  process.exit(0);
}
const day = (s, name) => {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(s) || iso(Date.parse(s)) !== s) {
    console.error(`${name} must be a YYYY-MM-DD date, got ${JSON.stringify(s)}`);
    process.exit(2);
  }
  return s;
};
const [, , a, b] = process.argv;
const start = day(a || "2026-08-06", "START");
const end = day(b || iso(Date.now() - DAY), "END");
if (start > end) {
  console.error(`START ${start} is after END ${end}`);
  process.exit(2);
}
const inWindow = at => at.slice(0, 10) >= start && at.slice(0, 10) <= end;
const REPO = process.env.REPO || "github/gh-aw";
const MARK = "workflow_id: ponytail-reviewer";
const LINES = /(?:net|Lean)\s*:\s*[-−–](\d[\d,]*)\s*lines?/i;
const AIC = /·\s*\\?~?\s*([\d.]+)\s*AIC/;
const KIND = /\b(yagni|shrink|delete|stdlib|native)\s*:/i; // tagged findings (Aug–Oct); the prose format since Nov has no tag

const gh = async (args, tries = 5) => {
  for (let i = 0; ; i++) {
    try {
      return (await run("gh", args, { encoding: "utf8", maxBuffer: 1 << 28 })).stdout;
    } catch (e) {
      if (i >= tries) throw e;
      await new Promise(r => setTimeout(r, 6000 * (i + 1)));
    }
  }
};
const pool = async (items, n, fn) => Promise.all(Array.from({ length: n }, async () => { while (items.length) await fn(items.shift()); }));
const med = v => {
  v = v.filter(x => x != null).sort((x, y) => x - y);
  if (!v.length) return null;
  const m = v.length >> 1;
  return v.length % 2 ? v[m] : (v[m - 1] + v[m]) / 2;
};
const inc = (o, k, n = 1) => (o[k] = (o[k] || 0) + n);

// 1. Workflow runs (Actions API). A `created=` filter caps results at 1,000, so page newest-first and filter here.
// New runs shift later pages while paging, so dedupe by id.
const byId = new Map();
for (let page = 1; ; page++) {
  const r = JSON.parse(await gh(["api", `repos/${REPO}/actions/workflows/ponytail-reviewer.lock.yml/runs?per_page=100&page=${page}`]));
  const ws = r.workflow_runs.map(w => ({ id: w.id, at: w.created_at, event: w.event, conclusion: w.conclusion }));
  for (const w of ws.filter(w => inWindow(w.at))) byId.set(w.id, w);
  process.stderr.write(`runs: ${byId.size} (page ${page})\n`);
  if (ws.length < 100 || ws.at(-1).at.slice(0, 10) < start) break;
}
const runs = [...byId.values()].sort((x, y) => y.at.localeCompare(x.at) || y.id - x.id);

// 2. Reviews and threads on PRs created in the window (2-day windows keep each search far below the 1,000 cap).
const Q = `query($q:String!,$after:String){search(query:$q,type:ISSUE,first:30,after:$after){
  pageInfo{hasNextPage endCursor}
  nodes{... on PullRequest{number mergedAt state
    reviews(first:80){nodes{id state submittedAt body}}
    reviewThreads(first:100){nodes{isResolved isOutdated comments(first:6){totalCount nodes{author{login} body createdAt pullRequestReview{id}}}}}}}}}`;
const windows = [];
for (let t = Date.parse(start); t <= Date.parse(end); t += 2 * DAY) windows.push(`${iso(t)}..${iso(Math.min(t + DAY, Date.parse(end)))}`);
let scanned = 0;
const prs = [];
await pool(windows, 4, async w => {
  let after = null;
  do {
    const args = ["api", "graphql", "-f", `query=${Q}`, "-f", `q=repo:${REPO} is:pr created:${w}`];
    if (after) args.push("-f", `after=${after}`);
    const s = JSON.parse(await gh(args)).data.search;
    for (const p of s.nodes.filter(n => n.number)) {
      scanned++;
      const mine = p.reviews.nodes.filter(r => (r.body || "").includes(MARK) && r.submittedAt && inWindow(r.submittedAt));
      if (!mine.length) continue;
      const ids = new Set(mine.map(r => r.id));
      prs.push({ merged: !!p.mergedAt, state: p.state, reviews: mine, threads: p.reviewThreads.nodes.filter(t => ids.has(t.comments.nodes[0]?.pullRequestReview?.id)) });
    }
    after = s.pageInfo.hasNextPage ? s.pageInfo.endCursor : null;
  } while (after);
  process.stderr.write(`${w}: scanned ${scanned}, reviewed ${prs.length}\n`);
});

const reviews = prs.flatMap(p => p.reviews);
const threads = prs.flatMap(p => p.threads);
const month = s => s.slice(0, 7);
const lines = reviews.map(r => LINES.exec(r.body)).filter(Boolean).map(m => +m[1].replace(/,/g, ""));
const aicByMonth = {};
const modelByMonth = {};
for (const r of reviews) {
  const m = AIC.exec(r.body);
  (aicByMonth[month(r.submittedAt)] ||= []).push(m ? +m[1] : null);
  const e = /engine: ([^,]+), model: ([^,]+)/.exec(r.body);
  inc((modelByMonth[month(r.submittedAt)] ||= {}), e ? `${e[1]}/${e[2]}` : "unknown");
}
const kinds = {};
for (const t of threads) inc(kinds, (KIND.exec((t.comments.nodes[0]?.body || "").slice(0, 260)) || [, "untagged"])[1].toLowerCase());
const perPR = {};
for (const p of prs) inc(perPR, p.threads.length);
const at = reviews.map(r => r.submittedAt).sort();

const out = {
  window: { start, end, snapshot: iso(Date.now()), firstReview: at[0], lastReview: at.at(-1) },
  runs: { total: runs.length, byEvent: {}, byConclusion: {}, byMonth: {}, first: runs.at(-1)?.at, last: runs[0]?.at },
  prsScanned: scanned,
  reviewedPRs: prs.length,
  reviewedMerged: prs.filter(p => p.merged).length,
  reviews: { total: reviews.length, byState: {}, byMonth: {} },
  linesClaimed: { reviews: lines.length, total: lines.reduce((s, x) => s + x, 0), median: med(lines) },
  aicPerReview: { total: Math.round(reviews.map(r => AIC.exec(r.body)).filter(Boolean).reduce((s, m) => s + +m[1], 0)), medianByMonth: Object.fromEntries(Object.entries(aicByMonth).sort().map(([k, v]) => [k, med(v)])) },
  modelByMonth,
  findings: {
    total: threads.length,
    perPR,
    kinds: Object.fromEntries(Object.entries(kinds).sort((x, y) => y[1] - x[1])),
    resolved: threads.filter(t => t.isResolved).length,
    outdated: threads.filter(t => t.isOutdated).length,
    replied: threads.filter(t => t.comments.totalCount > 1).length,
    copilotReplied: threads.filter(t => t.comments.nodes.slice(1).some(c => /copilot/i.test(c.author?.login || ""))).length,
    acted: threads.filter(t => t.isResolved || t.isOutdated || t.comments.totalCount > 1).length,
  },
};
for (const r of runs) {
  inc(out.runs.byEvent, r.event);
  inc(out.runs.byConclusion, r.conclusion ?? "running");
  inc(out.runs.byMonth, month(r.at));
}
for (const r of reviews) {
  inc(out.reviews.byState, r.state);
  inc(out.reviews.byMonth, month(r.submittedAt));
}

// 3. Optional: verdict share and per-run AI credits from a 1-in-N systematic sample of all runs in the window (newest first, any conclusion).
const N = +process.env.SAMPLE || 0;
if (N) {
  const dir = mkdtempSync(join(process.env.TMPDIR || tmpdir(), "pnt-"));
  const pick = runs.filter((_, i) => i % N === N - 1);
  const s = { sampled: pick.length, noArtifact: 0, downloadErrors: [], withOutput: 0, review: 0, lean: 0, clean: 0, skip: 0, other: 0, aic: [] };
  let n = 0;
  await pool([...pick], 12, async r => {
    const d = join(dir, String(r.id));
    try {
      const art = JSON.parse(await gh(["api", `repos/${REPO}/actions/runs/${r.id}/artifacts?per_page=100`], 2)).artifacts.find(x => x.name === "agent-output-fallback");
      if (!art || art.expired) return void s.noArtifact++;
      await gh(["run", "download", String(r.id), "-R", REPO, "-n", "agent-output-fallback", "-D", d], 2);
      if (!existsSync(join(d, "agent_output.json"))) return void s.noArtifact++;
    } catch (e) {
      s.downloadErrors.push(r.id);
      process.stderr.write(`sample: run ${r.id} download failed: ${String(e.stderr || e.message).trim().split("\n")[0]}\n`);
      return;
    } finally {
      if (++n % 50 === 0) process.stderr.write(`sample: ${n}/${pick.length}\n`);
    }
    s.withOutput++;
    s[classify(JSON.parse(readFileSync(join(d, "agent_output.json"), "utf8")).items || [])]++;
    if (existsSync(join(d, "agent_usage.json"))) s.aic.push(JSON.parse(readFileSync(join(d, "agent_usage.json"), "utf8")).ai_credits ?? null);
  });
  const verdicts = s.lean + s.clean + s.review;
  out.sample = { every: N, dir, ...s, verdicts, noopShare: verdicts ? +((s.lean + s.clean) / verdicts).toFixed(3) : null, aicRuns: s.aic.filter(x => x != null).length, aicMedianPerRun: med(s.aic) };
  delete out.sample.aic;
  if (s.downloadErrors.length) {
    process.stderr.write(`WARNING: sample incomplete, ${s.downloadErrors.length} download errors; rerun before publishing\n`);
    process.exitCode = 1;
  }
}
console.log(JSON.stringify(out, null, 2));
