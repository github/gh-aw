#!/usr/bin/env node
// Read-only: recompute the MEASURED column of the cracktro "Human Agent Processes" slide.
// Usage (repo root): node .github/skills/cracktro/refresh/human-agent-processes.mjs [END=YYYY-MM-DD] [DAYS=30]
// Needs an authenticated gh with public read access. Prints JSON; writes nothing.
import { execFileSync } from "node:child_process";
import { readdirSync, readFileSync } from "node:fs";

const REPO = "github/gh-aw";
const DAY = 864e5;
const iso = t => new Date(t).toISOString().slice(0, 10);
const END = process.argv[2] || iso(Date.now());
const DAYS = Number(process.argv[3] || 30);
const START = iso(Date.parse(END) - DAYS * DAY);

const gh = (...a) => JSON.parse(execFileSync("gh", a, { encoding: "utf8", maxBuffer: 1 << 28 }));

// The runs API caps each query at 1000 results, so query 5-day chunks and fail loudly if one overflows.
function runs(file) {
  const out = [];
  for (let t = Date.parse(START); t <= Date.parse(END); t += 5 * DAY) {
    const range = `${iso(t)}..${iso(Math.min(t + 4 * DAY, Date.parse(END)))}`;
    const pages = gh("api", "--paginate", "--slurp", `repos/${REPO}/actions/workflows/${file}/runs?created=${range}&per_page=100`);
    if ((pages[0]?.total_count ?? 0) > 1000) throw new Error(`${file} ${range}: over 1000 runs, shrink the chunk`);
    for (const p of pages) out.push(...p.workflow_runs);
  }
  return out;
}

function ok(file, event) {
  const r = runs(file).filter(x => x.conclusion === "success" && x.event === event);
  return { success_runs: r.length, distinct_branches: new Set(r.map(x => x.head_branch)).size };
}

function issuesByPrefix(prefix) {
  const q = `repo:${REPO} is:issue in:title "${prefix}" created:${START}..${END}`;
  const items = gh("api", "--paginate", "--slurp", "-X", "GET", "search/issues", "-f", `q=${q}`, "-f", "per_page=100")
    .flatMap(p => p.items)
    .filter(i => i.title.startsWith(prefix));
  const by = s => items.filter(i => i.state_reason === s).length;
  return { issues: items.length, completed: by("completed"), not_planned: by("not_planned"), open: items.filter(i => i.state === "open").length };
}

// Snapshot of today's checkout: `on:` block of .github/workflows/*.md frontmatter, smoke-* excluded, comments stripped.
function triggers() {
  const t = { issues_opened: 0, schedule: 0, ready_for_review: 0, label: 0 };
  for (const f of readdirSync(".github/workflows").filter(f => f.endsWith(".md") && !f.startsWith("smoke-"))) {
    const fm = readFileSync(`.github/workflows/${f}`, "utf8").match(/^---\n([\s\S]*?)\n---/)?.[1] ?? "";
    const lines = fm.split("\n");
    const i = lines.findIndex(l => /^on:/.test(l));
    if (i < 0) continue;
    let j = i + 1;
    while (j < lines.length && !/^[^\s#]/.test(lines[j])) j++;
    const on = lines.slice(i, j).map(l => l.replace(/(^|\s)(#|\/\/).*$/, "")).join("\n");
    if (/^\s*issues:[\s\S]*?opened/m.test(on)) t.issues_opened++;
    if (/^on:\s*"?(daily|weekly|hourly|every)|^\s*schedule:/m.test(on)) t.schedule++;
    if (/ready_for_review/.test(on)) t.ready_for_review++;
    if (/\blabeled\b|label_command/.test(on)) t.label++;
  }
  return t;
}

console.log(JSON.stringify({
  window: { start: START, end: END, days: DAYS },
  issueops: { workflow: "auto-triage-issues", ...ok("auto-triage-issues.lock.yml", "issues") },
  dailyops: { workflow: "daily-cli-tools-tester", ...issuesByPrefix("[cli-tools-test]") },
  reviewops: { workflow: "pr-code-quality-reviewer", ...ok("pr-code-quality-reviewer.lock.yml", "pull_request") },
  labelops: { workflow: "changeset", ...ok("changeset.lock.yml", "pull_request") },
  triggers: triggers(),
}, null, 2));
