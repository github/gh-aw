#!/usr/bin/env node
// Read-only: recomputes the measured data for the cracktro "Multi-device tester" slide.
// Usage: node .github/skills/cracktro/refresh/multi-device-tester.mjs [END=YYYY-MM-DD] > /tmp/mdt.json
// Needs an authenticated `gh`; makes only read queries.
import { execFileSync } from "node:child_process";

const REPO = process.env.REPO || "github/gh-aw";
const END = process.argv[2] || new Date().toISOString().slice(0, 10);
const gh = args => JSON.parse(execFileSync("gh", args, { encoding: "utf8", maxBuffer: 1 << 28 }));
const DAY = 864e5;
const median = a => {
  const s = [...a].sort((x, y) => x - y);
  return s.length ? (s.length % 2 ? s[(s.length - 1) / 2] : (s[s.length / 2 - 1] + s[s.length / 2]) / 2) : null;
};

// 1. Daily report issues. The search tokenises the emoji and matches bodies, so filter on the title.
const issues = gh(["issue", "list", "-R", REPO, "--state", "all", "--limit", "1000", "--search", `"Multi-Device" in:title created:<=${END}`, "--json", "number,title,createdAt,closedAt,state,stateReason,assignees,closedByPullRequestsReferences"])
  .filter(i => /^🔍 Multi-Device Doc/.test(i.title))
  .sort((a, b) => a.createdAt.localeCompare(b.createdAt));

// 2. Classify each closing PR by the files it touched.
const SITE = /^docs\/(src|public)\/|^docs\/astro\.config/;
const TESTER = /daily-multi-device-docs-tester\.(md|lock\.yml)$/;
const prCache = new Map();
const pr = n => {
  if (!prCache.has(n)) prCache.set(n, gh(["pr", "view", String(n), "-R", REPO, "--json", "number,author,mergedAt,title,files"]));
  return prCache.get(n);
};
const classOf = p => {
  const f = p.files.map(x => x.path);
  if (f.some(x => SITE.test(x))) return "s";
  if (f.some(x => TESTER.test(x))) return "t";
  return "o";
};

const first = Date.parse(issues[0].createdAt.slice(0, 10) + "T00:00:00Z");
const REPORTS = [];
const detail = [];
const hours = [];
let copilotAssigned = 0;
for (const i of issues) {
  const day = Math.round((Date.parse(i.createdAt.slice(0, 10) + "T00:00:00Z") - first) / DAY);
  const merged = i.closedByPullRequestsReferences.map(r => pr(r.number)).filter(p => p.mergedAt);
  if (i.assignees.some(a => a.login === "Copilot")) copilotAssigned++;
  let c = "x";
  if (merged.length) {
    const cls = merged.map(classOf);
    c = cls.includes("s") ? "s" : cls.includes("t") ? "t" : "o";
    hours.push((Date.parse(merged[0].mergedAt) - Date.parse(i.createdAt)) / 36e5);
  } else if (i.state === "OPEN") c = "open";
  REPORTS.push([day, c]);
  detail.push([day, c, i.number, i.stateReason, merged.map(p => `${p.number} ${p.author.login} ${p.title}`)]);
}
const count = c => REPORTS.filter(r => r[1] === c).length;
const fixPrs = [...prCache.values()].filter(p => p.mergedAt);
const out = {
  snapshot: END,
  window: [issues[0].createdAt.slice(0, 10), issues.at(-1).createdAt.slice(0, 10)],
  days: REPORTS.at(-1)[0],
  reports: issues.length,
  copilotAssigned,
  fixed: REPORTS.length - count("x") - count("open"),
  site: count("s"),
  tester: count("t"),
  other: count("o"),
  notPlanned: issues.filter(i => i.stateReason === "NOT_PLANNED" && !REPORTS[issues.indexOf(i)][1].match(/[sto]/)).length,
  noMergedPr: count("x"),
  open: count("open"),
  mergedPrs: fixPrs.length,
  copilotPrs: fixPrs.filter(p => p.author.login === "app/copilot-swe-agent").length,
  medianHoursToMerge: Math.round(median(hours) * 10) / 10,
  REPORTS,
  detail,
};
process.stdout.write(JSON.stringify(out, null, 1) + "\n");
