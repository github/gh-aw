#!/usr/bin/env node
// Read-only: recomputes the measured data for the cracktro "Findings that land" slide.
// Usage: node .github/skills/cracktro/refresh/findings-that-land.mjs [END=YYYY-MM-DD] > /tmp/ftl.json
// Needs an authenticated `gh`; makes only read queries.
import { execFileSync } from "node:child_process";

const REPO = process.env.REPO || "github/gh-aw";
const END = process.argv[2] || new Date().toISOString().slice(0, 10);
const CUTOFF = Date.parse(END + "T23:59:59Z");
const DAY = 864e5;
const median = a => {
  const s = [...a].sort((x, y) => x - y);
  return s.length ? (s.length % 2 ? s[(s.length - 1) / 2] : (s[s.length / 2 - 1] + s[s.length / 2]) / 2) : null;
};

// Docs auditors that file issues. `test` filters the loose search on the title; `search` is the search phrase.
const AUDITORS = [
  ["enterprise-docs-audit", "enterprise-docs", t => t.startsWith("[enterprise-docs] ")],
  ["daily-multi-device-docs-tester", "Multi-Device", t => /^🔍 Multi-Device Doc/.test(t)],
  ["daily-doc-healer", "doc-healer", t => t.startsWith("[doc-healer] ")],
  ["spec-librarian", "spec-librarian", t => t.startsWith("[spec-librarian] ")],
  ["cli-consistency-checker", "cli-consistency", t => t.startsWith("[cli-consistency] ")],
  ["designer-drift-audit", "Designer Drift Audit", t => t.startsWith("Designer Drift Audit")],
  ["daily-spec-coverage-kiro", "spec-coverage", t => t.startsWith("[spec-coverage] ")],
  ["daily-github-docs-seo-optimizer", "github-docs-seo", t => t.startsWith("[github-docs-seo] ")],
];

const QUERY = `query($q: String!, $endCursor: String) {
  search(type: ISSUE, query: $q, first: 100, after: $endCursor) {
    pageInfo { hasNextPage endCursor }
    nodes { ... on Issue { number title createdAt closedAt state stateReason
      assignees(first: 5) { nodes { login } }
      closedByPullRequestsReferences(first: 10, includeClosedPrs: true) { nodes { number merged mergedAt author { login } } } } }
  }
}`;
const search = phrase => {
  const q = `repo:${REPO} is:issue "${phrase}" in:title created:<=${END}`;
  const out = execFileSync("gh", ["api", "graphql", "--paginate", "-f", `query=${QUERY}`, "-f", `q=${q}`, "--jq", ".data.search.nodes[]"], { encoding: "utf8", maxBuffer: 1 << 28 });
  return out
    .split("\n")
    .filter(Boolean)
    .map(l => JSON.parse(l));
};

const rows = [];
for (const [id, phrase, test] of AUDITORS) {
  const seen = new Set();
  const issues = search(phrase)
    .filter(i => test(i.title) && !seen.has(i.number) && seen.add(i.number))
    .sort((a, b) => a.createdAt.localeCompare(b.createdAt));
  const hours = [];
  let fixed = 0,
    copilotFix = 0,
    open = 0,
    notPlanned = 0,
    completedNoPr = 0,
    copilotAssigned = 0,
    assignedFixed = 0;
  for (const i of issues) {
    const merged = i.closedByPullRequestsReferences.nodes.filter(p => p.merged && Date.parse(p.mergedAt) <= CUTOFF).sort((a, b) => a.mergedAt.localeCompare(b.mergedAt));
    const toCopilot = i.assignees.nodes.some(a => a.login === "Copilot");
    if (toCopilot) copilotAssigned++;
    if (merged.length) {
      fixed++;
      if (toCopilot) assignedFixed++;
      if (merged.some(p => p.author?.login === "copilot-swe-agent")) copilotFix++;
      hours.push((Date.parse(merged[0].mergedAt) - Date.parse(i.createdAt)) / 36e5);
    } else if (i.state === "OPEN") open++;
    else if (i.stateReason === "NOT_PLANNED") notPlanned++;
    else completedNoPr++;
  }
  const first = issues[0]?.createdAt.slice(0, 10) ?? null;
  rows.push({
    id,
    filed: issues.length,
    fixed,
    rate: issues.length ? Math.round((100 * fixed) / issues.length) : 0,
    copilotFix,
    copilotAssigned,
    assignedFixed,
    open,
    notPlanned,
    completedNoPr,
    medianHoursToMerge: hours.length ? Math.round(median(hours) * 10) / 10 : null,
    window: [first, issues.at(-1)?.createdAt.slice(0, 10) ?? null],
    days: first ? Math.round((Date.parse(END + "T00:00:00Z") - Date.parse(first + "T00:00:00Z")) / DAY) : 0,
  });
}
rows.sort((a, b) => b.rate - a.rate || b.fixed - a.fixed);
const sum = k => rows.reduce((s, r) => s + r[k], 0);
const firstAll = rows.map(r => r.window[0]).filter(Boolean).sort()[0];
const out = {
  snapshot: END,
  window: [firstAll, END],
  days: Math.round((Date.parse(END + "T00:00:00Z") - Date.parse(firstAll + "T00:00:00Z")) / DAY),
  filed: sum("filed"),
  fixed: sum("fixed"),
  copilotFix: sum("copilotFix"),
  copilotAssigned: sum("copilotAssigned"),
  assignedFixed: sum("assignedFixed"),
  rows,
};
process.stdout.write(JSON.stringify(out, null, 1) + "\n");
