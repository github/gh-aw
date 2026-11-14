#!/usr/bin/env node
// Read-only: recompute the measured data on the cracktro "Explore · Reuse · Lint" slide.
// Needs git (repo checkout with origin) and an authenticated gh. Prints JSON to stdout.
import { execFileSync } from "node:child_process";
const sh = (cmd, args) => execFileSync(cmd, args, { encoding: "utf8", maxBuffer: 1 << 28 });
const REPO = process.env.REPO || "github/gh-aw";
const MEM = "origin/memory/sergo";
const day = s => s.slice(0, 10);
const span = a => (a.length ? [a.reduce((x, y) => (x < y ? x : y)), a.reduce((x, y) => (x > y ? x : y))] : []);

sh("git", ["fetch", "-q", "origin", "memory/sergo"]);
const stats = JSON.parse(sh("git", ["show", `${MEM}:sergo-stats.json`]));

// Strategy cache: union of every line ever written to sergo-strategies.jsonl (runs can rewrite the file).
const seen = new Map();
for (const rev of sh("git", ["rev-list", MEM]).trim().split("\n")) {
  let txt;
  try { txt = sh("git", ["show", `${rev}:sergo-strategies.jsonl`]); } catch { continue; }
  for (const line of txt.split("\n")) {
    if (!line.trim()) continue;
    try { const e = JSON.parse(line); seen.set(`${e.date}|${e.strategy}`, e); } catch {}
  }
}
const entries = [...seen.values()].filter(e => e.date);
const byMonth = {};
for (const e of entries) {
  const m = e.date.slice(0, 7);
  (byMonth[m] ||= []).push(Number(e.findings) || 0);
}
const fpr = Object.fromEntries(Object.entries(byMonth).sort().map(([m, a]) => [m, +(a.reduce((x, y) => x + y, 0) / a.length).toFixed(1)]));

const issues = JSON.parse(sh("gh", ["issue", "list", "-R", REPO, "--label", "sergo", "--state", "all", "--limit", "2000", "--json", "number,createdAt,state,stateReason"]));
const prs = JSON.parse(sh("gh", ["pr", "list", "-R", REPO, "--label", "sergo", "--state", "all", "--limit", "500", "--json", "number,createdAt,mergedAt,state,headRefName,title"]));

// Merged PRs that closed sergo issues (GraphQL, batched 50 issues per query).
const closers = new Map();
let closedIssues = 0;
for (let i = 0; i < issues.length; i += 50) {
  const q = issues.slice(i, i + 50).map((it, k) => `i${k}: issue(number: ${it.number}) { closedByPullRequestsReferences(first: 10, includeClosedPrs: false) { nodes { number merged mergedAt additions deletions author { login } } } }`).join("\n");
  const [owner, name] = REPO.split("/");
  const r = JSON.parse(sh("gh", ["api", "graphql", "-f", `query=query { repository(owner: "${owner}", name: "${name}") { ${q} } }`]));
  for (const v of Object.values(r.data.repository)) {
    const merged = v.closedByPullRequestsReferences.nodes.filter(p => p.merged);
    if (merged.length) closedIssues++;
    for (const p of merged) closers.set(p.number, p);
  }
}
const cl = [...closers.values()];
const ownMerged = prs.filter(p => p.mergedAt);

console.log(JSON.stringify({
  snapshot: new Date().toISOString().slice(0, 10),
  repo_memory: { total_runs: stats.total_runs, total_findings: stats.total_findings ?? null },
  strategy_cache: { entries: entries.length, dates: new Set(entries.map(e => e.date)).size, window: span(entries.map(e => e.date)) },
  findings_per_run_by_month: fpr,
  latest_cache_entries: entries.sort((a, b) => (a.date < b.date ? -1 : 1)).slice(-5).map(e => ({ date: e.date, strategy: String(e.strategy).slice(0, 160), findings: e.findings })),
  issues: { total: issues.length, completed: issues.filter(i => i.stateReason === "COMPLETED").length, open: issues.filter(i => i.state === "OPEN").length, window: span(issues.map(i => day(i.createdAt))) },
  fix_prs: { merged: cl.length, issues_closed: closedIssues, by_copilot: cl.filter(p => /copilot/i.test(p.author?.login || "")).length, lines: cl.reduce((s, p) => s + p.additions + p.deletions, 0), window: span(cl.map(p => day(p.mergedAt))) },
  own_prs: { total: prs.length, merged: ownMerged.length, window: span(prs.map(p => day(p.createdAt))), merged_list: ownMerged.map(p => ({ merged: day(p.mergedAt), branch: p.headRefName })).sort((a, b) => (a.merged < b.merged ? -1 : 1)) },
}, null, 2));
