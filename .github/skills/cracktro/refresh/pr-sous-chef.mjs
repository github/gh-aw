#!/usr/bin/env node
// Read-only: recompute the "PR Sous Chef" cracktro strip from gh-aw pull request comments. Needs `gh auth login`.
// Usage: node pr-sous-chef.mjs [START END]   (YYYY-MM-DD, inclusive nudge dates; default: 2026-05-13 .. yesterday UTC)
// Prints one JSON object to stdout; progress goes to stderr. RAW=/tmp/x.json also dumps the raw PR nodes; IN=/tmp/x.json recomputes from such a dump without searching.
import { execFileSync } from "node:child_process";

const DAY = 864e5;
const iso = d => new Date(d).toISOString().slice(0, 10);
const [, , a, b] = process.argv;
const start = a || "2026-05-13";
const end = b || iso(Date.now() - DAY);
const REPO = process.env.REPO || "github/gh-aw";
const MARK = "gh-aw-workflow-call-id: github/gh-aw/pr-sous-chef";
const BOTS = new Set(["gh-aw-bot", "github-actions"]);
const COPILOT = new Set(["copilot-swe-agent", "Copilot"]);
const PREP = "Copilot assignment requested for the following outstanding work";

const Q = `query($q:String!,$after:String){search(query:$q,type:ISSUE,first:15,after:$after){
  issueCount pageInfo{hasNextPage endCursor}
  nodes{... on PullRequest{number createdAt mergedAt
    comments(last:100){totalCount nodes{author{login} body createdAt}}}}}}`;

const search = (q, after) => {
  const args = ["api", "graphql", "-f", `query=${Q}`, "-f", `q=${q}`];
  if (after) args.push("-f", `after=${after}`);
  for (let i = 0; ; i++) {
    try {
      return JSON.parse(execFileSync("gh", args, { encoding: "utf8", maxBuffer: 1 << 28 })).data.search;
    } catch (e) {
      if (i >= 3) throw e;
      execFileSync("sleep", [String(10 * (i + 1))]);
    }
  }
};

// PRs whose comments carry the Sous Chef marker, partitioned by PR creation date (7-day windows stay below the 1,000-result search cap)
const prs = new Map();
if (process.env.IN) for (const n of JSON.parse((await import("node:fs")).readFileSync(process.env.IN, "utf8"))) prs.set(n.number, n);
else for (let t = Date.parse("2026-05-01"); t <= Date.parse(end); t += 7 * DAY) {
  const w = `${iso(t)}..${iso(Math.min(t + 6 * DAY, Date.parse(end)))}`;
  let after = null;
  do {
    const s = search(`repo:${REPO} is:pr "${MARK}" in:comments created:${w}`, after);
    if (s.issueCount >= 1000) throw new Error(`window ${w} hits the search cap`);
    for (const n of s.nodes) if (n.number) prs.set(n.number, n);
    after = s.pageInfo.hasNextPage ? s.pageInfo.endCursor : null;
  } while (after);
  process.stderr.write(`${w}: ${prs.size}\n`);
}
if (process.env.RAW && !process.env.IN) (await import("node:fs")).writeFileSync(process.env.RAW, JSON.stringify([...prs.values()]));

const inWin = c => c.createdAt.slice(0, 10) >= start && c.createdAt.slice(0, 10) <= end;
const isChef = c => BOTS.has(c.author?.login) && (c.body || "").includes("pr-sous-chef");
// a nudge addresses Copilot first, after an optional ```hidden marker block, or is a prep list (the first prep lists, 4-8 Nov 2026, had no @copilot); acknowledgements and reports do not
const isNudge = c =>
  (c.body || "").replace(/\\/g, "").includes(PREP) ||
  /^`?@copilot\b/.test(
    (c.body || "")
      .replace(/^\s*```hidden[\s\S]*?```\s*/, "")
      .replace(/\\/g, "")
      .trimStart()
  );
const median = xs => {
  const s = [...xs].sort((x, y) => x - y),
    m = s.length >> 1;
  return s.length ? +(s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2).toFixed(1) : null;
};

const out = {
  window: { start, end, days: Math.round((Date.parse(end) - Date.parse(start)) / DAY) + 1 },
  nudges: 0,
  nudgedPRs: 0,
  prepLists: 0,
  prepItems: { branch: 0, review: 0, check: 0, other: 0 },
  ciFollowUps: 0,
  answered: 0,
  merged: 0,
  nudgeToMergeHours: null,
  months: {},
  truncated: [],
};
const hours = [];
for (const p of prs.values()) {
  const cs = p.comments.nodes;
  if (p.comments.totalCount > cs.length) out.truncated.push(p.number);
  const nudges = cs.filter(c => isChef(c) && isNudge(c) && inWin(c));
  if (!nudges.length) continue;
  out.nudgedPRs++;
  const firstMonth = nudges[0].createdAt.slice(0, 7);
  (out.months[firstMonth] ??= { nudges: 0, prs: 0 }).prs++;
  nudges.forEach((n, i) => {
    out.nudges++;
    out.months[n.createdAt.slice(0, 7)] = out.months[n.createdAt.slice(0, 7)] || { nudges: 0, prs: 0 };
    out.months[n.createdAt.slice(0, 7)].nudges++;
    const body = n.body.replace(/\\/g, "");
    if (/"kind":\s*"ci-follow-up"|Sous-chef ci-follow-up:/.test(body)) out.ciFollowUps++;
    if (body.includes(PREP)) {
      out.prepLists++;
      for (const l of body.split("\n").filter(l => /^\d+\. /.test(l)))
        out.prepItems[/^\d+\. Update this branch/.test(l) ? "branch" : /^\d+\. Review \(/.test(l) ? "review" : /^\d+\. Fix failing check/.test(l) ? "check" : "other"]++;
    }
    // answered: the Copilot coding agent (copilot-swe-agent) comments after this nudge and before the next one
    const next = nudges[i + 1]?.createdAt || "9999";
    if (cs.some(c => COPILOT.has(c.author?.login) && c.createdAt > n.createdAt && c.createdAt < next)) out.answered++;
  });
  if (p.mergedAt) {
    out.merged++;
    hours.push((Date.parse(p.mergedAt) - Date.parse(nudges[0].createdAt)) / 36e5);
  }
}
out.nudgeToMergeHours = { median: median(hours), n: hours.length };
out.months = Object.fromEntries(Object.entries(out.months).sort());
console.log(JSON.stringify(out, null, 2));
