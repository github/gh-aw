#!/usr/bin/env node
// Read-only: recompute the "Ponytail Reviewer" cracktro strip from gh-aw pull requests and workflow runs. Needs `gh auth login`.
// Usage: node ponytail-reviewer.mjs [START END]   (YYYY-MM-DD, inclusive; default: 2026-08-06 to today UTC)
// Prints one JSON object to stdout; progress goes to stderr.
// SAMPLE=N also downloads the agent-output-fallback artifact of every Nth finished run (into $TMPDIR) to measure the noop rate and per-run AI credits.
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { existsSync, mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const run = promisify(execFile);
const DAY = 864e5;
const iso = d => new Date(d).toISOString().slice(0, 10);
const [, , a, b] = process.argv;
const start = a || "2026-08-06";
const end = b || iso(Date.now());
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
const med = v => (v = v.filter(x => x != null).sort((x, y) => x - y)).length ? v[Math.floor((v.length - 1) / 2)] : null;
const inc = (o, k, n = 1) => (o[k] = (o[k] || 0) + n);

// 1. Workflow runs (Actions API). A `created=` filter caps results at 1,000, so page newest-first and filter here.
const runs = [];
for (let page = 1; ; page++) {
  const r = JSON.parse(await gh(["api", `repos/${REPO}/actions/workflows/ponytail-reviewer.lock.yml/runs?per_page=100&page=${page}`]));
  const ws = r.workflow_runs.map(w => ({ id: w.id, at: w.created_at, event: w.event, conclusion: w.conclusion }));
  runs.push(...ws.filter(w => w.at.slice(0, 10) >= start && w.at.slice(0, 10) <= end));
  process.stderr.write(`runs: ${runs.length} (page ${page})\n`);
  if (ws.length < 100 || ws.at(-1).at.slice(0, 10) < start) break;
}

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
      const mine = p.reviews.nodes.filter(r => (r.body || "").includes(MARK));
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

// 3. Optional: noop rate and per-run AI credits from a 1-in-N sample of finished runs.
const N = +process.env.SAMPLE || 0;
if (N) {
  const dir = mkdtempSync(join(process.env.TMPDIR || tmpdir(), "pnt-"));
  const pick = runs.filter(r => r.conclusion === "success" || r.conclusion === "failure").filter((_, i) => i % N === N - 1);
  const s = { sampled: pick.length, withOutput: 0, noop: 0, review: 0, other: 0, aic: [] };
  let n = 0;
  await pool([...pick], 12, async r => {
    const d = join(dir, String(r.id));
    try {
      await gh(["run", "download", String(r.id), "-R", REPO, "-n", "agent-output-fallback", "-D", d], 1);
    } catch {}
    if (++n % 50 === 0) process.stderr.write(`sample: ${n}/${pick.length}\n`);
    if (!existsSync(join(d, "agent_output.json"))) return;
    s.withOutput++;
    const types = (JSON.parse(readFileSync(join(d, "agent_output.json"), "utf8")).items || []).map(i => i.type);
    if (types.includes("submit_pull_request_review") || types.includes("create_pull_request_review_comment")) s.review++;
    else if (types.includes("noop")) s.noop++;
    else s.other++;
    if (existsSync(join(d, "agent_usage.json"))) s.aic.push(JSON.parse(readFileSync(join(d, "agent_usage.json"), "utf8")).ai_credits ?? null);
  });
  out.sample = { every: N, dir, ...s, noopShare: +(s.noop / (s.noop + s.review)).toFixed(3), aicRuns: s.aic.filter(x => x != null).length, aicMedianPerRun: med(s.aic) };
  delete out.sample.aic;
}
console.log(JSON.stringify(out, null, 2));
