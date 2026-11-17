#!/usr/bin/env node
// Read-only data pull for the cracktro "From Delivery to Value" slide (dtv- classes). Ports the Schema Feature
// Coverage value report: weekly first-parent main snapshots of the workflow's own textual check, the workflow's
// delivered and merged pull requests, exact-parent merge deltas, and seven-day before/after windows sampled daily.
// Uses git and gh only; prints one JSON object. Run from the repository root on an up-to-date origin/main:
//   node .github/skills/cracktro/refresh/from-delivery-to-value.mjs [END] [REF] [WITNESSES] [EARLY_END]
//   defaults: 2026-11-17 origin/main 63949,72028,74061 2026-06-30
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const [, , END = "2026-11-17", REF = "origin/main", WITNESSES = "63949,72028,74061", EARLY_END = "2026-06-30"] = process.argv;
if (![END, EARLY_END].every(d => /^\d{4}-\d{2}-\d{2}$/.test(d))) throw new Error("Usage: from-delivery-to-value.mjs [END] [REF] [WITNESSES] [EARLY_END]; dates are YYYY-MM-DD UTC.");
const START = "2026-03-22";
const SCHEMA = "pkg/parser/schemas/main_workflow_schema.json";
const QUERY = 'repo:github/gh-aw is:pr author:app/github-actions "workflow_id: schema-feature-coverage" in:body';
const FOOTER = /<!-- gh-aw-agentic-workflow: [^\n]*workflow_id: schema-feature-coverage, run: https:\/\/github\.com\/github\/gh-aw\/actions\/runs\/\d+/;
const DAY = 864e5;
const ROOT = fileURLToPath(new URL("../../../../", import.meta.url));
const run = (cmd, args) => execFileSync(cmd, args, { cwd: ROOT, encoding: "utf8", maxBuffer: 1 << 28 });
const git = (...args) => run("git", args).trim();
const iso = ms => new Date(ms).toISOString().replace(".000Z", "Z");
const noon = date => Date.parse(`${date}T12:00:00Z`);
const tip = git("rev-parse", "--verify", `${REF}^{commit}`);
const now = Date.now();

const snapshot = ms => {
  const sha = git("rev-list", "--first-parent", "-1", `--before=${iso(ms)}`, tip);
  if (!sha) throw new Error(`No main snapshot at ${iso(ms)}`);
  return sha;
};
// The workflow's check: a schema top-level property is covered when a Markdown line under .github/workflows or
// schema-demos (any depth) starts with `<field>:`. It also matches code blocks; it never compiles anything.
const memo = new Map();
const measure = sha => {
  if (!memo.has(sha)) {
    const fields = Object.keys(JSON.parse(run("git", ["show", `${sha}:${SCHEMA}`])).properties).sort();
    let lines = "";
    try {
      lines = run("git", ["grep", "-h", "-o", "-E", "^[^:[:space:]]+:", sha, "--", ":(glob).github/workflows/**/*.md", ":(glob)schema-demos/**/*.md"]);
    } catch (error) {
      if (error.status !== 1) throw error;
    }
    const have = new Set(
      lines
        .split("\n")
        .filter(Boolean)
        .map(line => line.slice(0, -1))
    );
    if (!fields.length || !have.size) throw new Error(`Missing schema or Markdown inventory at ${sha} is not zero coverage`);
    const missing = fields.filter(field => !have.has(field));
    memo.set(sha, { sha, covered: fields.length - missing.length, total: fields.length, missing, fields });
  }
  return memo.get(sha);
};
const pair = m => [m.covered, m.total];
const minus = (a, b) => a.filter(x => !b.includes(x));

// 1. Weekly outcome, whoever changed the repository: every Sunday 12:00 UTC from START, plus END.
const cutoffs = [];
for (let t = noon(START); t <= noon(END); t += 7 * DAY) cutoffs.push(t);
if (cutoffs.at(-1) !== noon(END)) cutoffs.push(noon(END));
const weekly = cutoffs.map(t => measure(snapshot(t)));
const cohort = weekly[0].fields.filter(field => weekly.every(w => w.fields.includes(field)));

// 2. Delivery and adoption proxies: the workflow's marked pull requests (search results, not native lifecycle history).
const items = [];
for (let page = 1; ; page++) {
  const res = JSON.parse(run("gh", ["api", "-X", "GET", "search/issues", "-f", `q=${QUERY}`, "-f", "per_page=100", "-f", `page=${page}`]));
  if (res.incomplete_results) throw new Error("Incomplete search results; retry or narrow the query");
  items.push(...res.items);
  if (items.length >= res.total_count || !res.items.length) {
    if (items.length !== res.total_count) throw new Error(`Search returned ${items.length} of ${res.total_count}`);
    break;
  }
}
const outputs = items
  .map(pr => {
    if (pr.user.login !== "github-actions[bot]" || !FOOTER.test(pr.body || "")) throw new Error(`PR ${pr.number} lacks the bot author or producer footer`);
    const merged = pr.pull_request.merged_at;
    if (merged && Date.parse(merged) < Date.parse(pr.created_at)) throw new Error(`PR ${pr.number} merges before delivery`);
    return { pr: pr.number, delivered: pr.created_at, merged };
  })
  .sort((a, b) => Date.parse(a.delivered) - Date.parse(b.delivered) || a.pr - b.pr);

// 3. Direct merge deltas: the accepted merge commit against its actual first parent.
const merges = outputs
  .filter(o => o.merged)
  .map(o => {
    const sha = JSON.parse(run("gh", ["api", `repos/github/gh-aw/pulls/${o.pr}`])).merge_commit_sha;
    git("merge-base", "--is-ancestor", sha, tip);
    const before = measure(git("rev-parse", `${sha}^1`)),
      after = measure(sha);
    const same = before.fields.join() === after.fields.join();
    return { ...o, sha, before, after, same, closed: same ? minus(before.missing, after.missing) : [], removed: minus(before.fields, after.fields), added: minus(after.fields, before.fields) };
  })
  .sort((a, b) => Date.parse(a.merged) - Date.parse(b.merged));

// 4. Inspected cases: early same-day merge batches, then the named single witnesses.
const label = ms => new Date(ms).toLocaleDateString("en", { month: "short", day: "numeric", timeZone: "UTC" });
const cases = [];
const early = merges.filter(m => Date.parse(m.merged) < noon(EARLY_END) + 12 * 36e5);
for (const dayKey of [...new Set(early.map(m => m.merged.slice(0, 10)))]) {
  const members = early.filter(m => m.merged.startsWith(dayKey));
  cases.push({ label: label(Date.parse(members[0].merged)), kind: "batch", members, gaps: new Set(members.flatMap(m => m.closed)).size, removed: [...new Set(members.flatMap(m => m.removed))].sort() });
}
for (const n of WITNESSES.split(",").map(Number)) {
  const m = merges.find(x => x.pr === n);
  if (!m) throw new Error(`Witness PR ${n} is not a merged workflow output`);
  if (!m.same) throw new Error(`Schema population changed in PR ${n}; not an isolated example addition`);
  cases.push({ label: label(Date.parse(m.merged)), kind: "single", members: [m], gaps: m.closed.length, removed: [] });
}
cases.sort((a, b) => Date.parse(a.members[0].merged) - Date.parse(b.members[0].merged));

// 5. Windows: [first merge - 7 days, first merge) and [last merge, last merge + 7 days), daily, no lag.
// Day 0 uses the exact last accepted commit; samples after now stay pending (null).
// Survival (single merges): every first-parent main commit in (merge, merge + 7 days] must leave the merge's files untouched.
const windows = cases.map(c => {
  const first = Date.parse(c.members[0].merged),
    last = Date.parse(c.members.at(-1).merged),
    end = last + 7 * DAY;
  const samples = [];
  for (let d = -7; d < 7; d++) {
    const t = (d < 0 ? first : last) + d * DAY;
    samples.push(t > now ? null : pair(measure(d === 0 ? c.members.at(-1).sha : snapshot(t))));
  }
  let survival = null;
  if (c.kind === "single" && end <= now) {
    const m = c.members[0],
      endSha = snapshot(end),
      files = git("diff", "--name-only", `${m.sha}^1`, m.sha).split("\n").filter(Boolean);
    survival = {
      through: iso(end),
      commits: Number(git("rev-list", "--first-parent", "--count", `${m.sha}..${endSha}`)),
      files: files.length,
      touched: git("log", "--first-parent", "--format=%h", `${m.sha}..${endSha}`, "--", ...files)
        .split("\n")
        .filter(Boolean).length,
    };
  }
  return {
    label: c.label,
    kind: c.kind,
    prs: c.members.map(m => m.pr),
    delivered: c.members.map(m => m.delivered),
    first: iso(first),
    last: iso(last),
    gaps: c.gaps,
    removed: c.removed,
    closed: [...new Set(c.members.flatMap(m => m.closed))].sort(),
    before: pair(c.members[0].before),
    end: end <= now ? pair(measure(snapshot(end))) : null,
    samples,
    survival,
  };
});

const inspected = new Set(cases.flatMap(c => c.members.map(m => m.pr)));
console.log(
  JSON.stringify(
    {
      window: { start: START, end: END, ref: tip, snapshots: weekly.length },
      weekly: weekly.map(pair),
      first: { ...Object.fromEntries(["covered", "total"].map(k => [k, weekly[0][k]])) },
      last: { ...Object.fromEntries(["covered", "total"].map(k => [k, weekly.at(-1)[k]])) },
      cohort: { fields: cohort.length, before: cohort.filter(f => !weekly[0].missing.includes(f)).length, after: cohort.filter(f => !weekly.at(-1).missing.includes(f)).length },
      outputs: { delivered: outputs.length, merged: merges.length, items: outputs.map(o => [o.delivered.slice(0, 16), o.merged?.slice(0, 16) ?? null]) },
      merges: merges.map(m => ({ pr: m.pr, merged: m.merged, before: pair(m.before), after: pair(m.after), closed: m.closed, removed: m.removed, added: m.added, inspected: inspected.has(m.pr) })),
      windows,
    },
    null,
    1
  )
);
