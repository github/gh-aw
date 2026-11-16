#!/usr/bin/env node
// Read-only: recomputes the measured data for the cracktro "Personas · Probe · Patch" slide.
// Usage: node .github/skills/cracktro/refresh/personas-probe-patch.mjs [END=YYYY-MM-DD] > /tmp/ppx.json
// Needs an authenticated `gh` and a checkout of the repository; makes only read queries.
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";

const REPO = process.env.REPO || "github/gh-aw";
const END = process.argv[2] || new Date().toISOString().slice(0, 10);
const gh = args => JSON.parse(execFileSync("gh", args, { encoding: "utf8", maxBuffer: 1 << 28 }));
const DAY = 864e5;
// Snapshot cutoff: end of the END day (UTC). Later merges and closures do not count.
const CUTOFF = Date.parse(END + "T00:00:00Z") + DAY;
const before = t => t && Date.parse(t) < CUTOFF;

// 1. Daily report issues (label + title prefix; the label alone also matches older experiments).
const issues = gh(["issue", "list", "-R", REPO, "--state", "all", "--limit", "1000", "--label", "agent-research", "--search", `created:<=${END}`, "--json", "number,title,createdAt,closedAt,stateReason,closedByPullRequestsReferences"])
  .filter(i => i.title.startsWith("Agent Persona Exploration - "))
  .sort((a, b) => a.createdAt.localeCompare(b.createdAt));

// 2. Classify each closing PR: g = merged and changed agent guidance, o = merged elsewhere.
const GUIDE = /^\.github\/(aw|agents|skills\/agentic-workflows)\//;
const BOTS = /^(gh-aw-bot|app\/github-actions)$/;
const prCache = new Map();
const pr = n => {
  if (!prCache.has(n)) prCache.set(n, gh(["pr", "view", String(n), "-R", REPO, "--json", "number,author,mergedAt,files"]));
  return prCache.get(n);
};

const first = Date.parse(issues[0].createdAt.slice(0, 10) + "T00:00:00Z");
const REPORTS = [];
const detail = [];
for (const i of issues) {
  const day = Math.round((Date.parse(i.createdAt.slice(0, 10) + "T00:00:00Z") - first) / DAY);
  const merged = i.closedByPullRequestsReferences.map(r => pr(r.number)).filter(p => before(p.mergedAt));
  let c = before(i.closedAt) ? "x" : "open";
  if (merged.length) c = merged.some(p => p.files.some(f => GUIDE.test(f.path))) ? "g" : "o";
  REPORTS.push([day, c]);
  detail.push([day, c, i.number, i.stateReason, merged.map(p => `${p.number} ${p.author.login}`)]);
}
const fixPrs = [...prCache.values()].filter(p => before(p.mergedAt));
const count = c => REPORTS.filter(r => r[1] === c).length;
const out = {
  snapshot: END,
  window: [issues[0].createdAt.slice(0, 10), issues.at(-1).createdAt.slice(0, 10)],
  days: REPORTS.at(-1)[0],
  reports: issues.length,
  fixed: count("g") + count("o"),
  guidance: count("g"),
  other: count("o"),
  noMergedPr: count("x"),
  open: count("open"),
  mergedPrs: fixPrs.length,
  copilotPrs: fixPrs.filter(p => p.author.login === "app/copilot-swe-agent").length,
  botPrs: fixPrs.filter(p => BOTS.test(p.author.login)).length,
  personas: (readFileSync(".github/workflows/agent-persona-explorer.md", "utf8").match(/^\d+\. \*\*[^*]+\*\* - /gm) || []).length,
  agentBytes: Buffer.byteLength(readFileSync(".github/agents/agentic-workflows.md")),
  REPORTS,
  // Paste into the slide block: one bar per report, classified by index (days can repeat).
  deck: `const DAYS = ${JSON.stringify(REPORTS.map(r => r[0]))};\nconst CLS = "${REPORTS.map(r => (r[1] === "open" ? "x" : r[1])).join("")}";`,
  detail,
};
process.stdout.write(JSON.stringify(out, null, 1) + "\n");
