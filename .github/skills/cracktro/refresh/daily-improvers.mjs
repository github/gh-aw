#!/usr/bin/env node
// Read-only: recompute the "Daily Improvers" measured panel from gh-aw [duplicate-code] issues.
// Usage: node daily-improvers.mjs [FROM=2025-10-07] [TO=2026-10-26]   (needs `gh auth login`; prints JSON)
import { execFileSync } from "node:child_process";
const [FROM = "2025-10-07", TO = "2026-10-26"] = process.argv.slice(2);
const REPO = "github/gh-aw", PREFIX = "[duplicate-code]", BOT = "github-actions";
const Q = `query($q:String!,$c:String){search(type:ISSUE,query:$q,first:50,after:$c){pageInfo{hasNextPage endCursor}
nodes{...on Issue{number title createdAt author{login} assignees(first:10){nodes{login}}
timelineItems(first:100,itemTypes:[ASSIGNED_EVENT,CLOSED_EVENT]){nodes{__typename
...on AssignedEvent{assignee{...on Bot{login}...on User{login}}}
...on ClosedEvent{createdAt closer{__typename ...on PullRequest{number merged mergedAt author{login}}}}}}}}}}`;
const gql = (q, c) => JSON.parse(execFileSync("gh", ["api", "graphql", "-f", `query=${Q}`, "-f", `q=${q}`, ...(c ? ["-f", `c=${c}`] : [])], { encoding: "utf8", maxBuffer: 1 << 28 })).data.search;
// GitHub search returns at most 1,000 hits per query, so page month by month.
const issues = [];
for (let d = new Date(FROM + "T00:00:00Z"); d <= new Date(TO + "T00:00:00Z"); d.setUTCMonth(d.getUTCMonth() + 1, 1)) {
  const a = d.toISOString().slice(0, 10), e = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 0)).toISOString().slice(0, 10);
  const q = `repo:${REPO} is:issue in:title "${PREFIX}" author:app/${BOT} created:${a < FROM ? FROM : a}..${e > TO ? TO : e}`;
  let c = null;
  do { const r = gql(q, c); issues.push(...r.nodes); c = r.pageInfo.hasNextPage ? r.pageInfo.endCursor : null; } while (c);
}
const rows = issues.filter(i => i.title.startsWith(PREFIX)).map(i => {
  const ev = i.timelineItems.nodes;
  const pr = ev.filter(e => e.__typename === "ClosedEvent" && e.closer?.__typename === "PullRequest" && e.closer.merged).map(e => e.closer)[0];
  return {
    n: i.number, created: i.createdAt,
    // Copilot as an assignment event OR current assignee (assigned-at-creation issues have no event)
    copilot: ev.some(e => e.__typename === "AssignedEvent" && /^copilot/i.test(e.assignee?.login || "")) || i.assignees.nodes.some(a => /^copilot/i.test(a.login)),
    pr: pr && { n: pr.number, mergedAt: pr.mergedAt, byCopilot: /^copilot/i.test(pr.author?.login || "") },
  };
});
const merged = rows.filter(r => r.pr);
const hrs = merged.map(r => (Date.parse(r.pr.mergedAt) - Date.parse(r.created)) / 36e5).sort((a, b) => a - b);
const med = hrs.length ? (hrs[(hrs.length - 1) >> 1] + hrs[hrs.length >> 1]) / 2 : null;
const months = [];
for (let d = new Date(FROM.slice(0, 7) + "-01T00:00:00Z"); d.toISOString().slice(0, 7) <= TO.slice(0, 7); d.setUTCMonth(d.getUTCMonth() + 1)) {
  const m = d.toISOString().slice(0, 7);
  months.push([m, rows.filter(r => r.created.startsWith(m)).length, merged.filter(r => r.pr.mergedAt.startsWith(m)).length]);
}
console.log(JSON.stringify({
  window: { from: FROM, to: TO, days: Math.round((Date.parse(TO) - Date.parse(FROM)) / 864e5) + 1 },
  snapshot: new Date().toISOString().slice(0, 10),
  filed: rows.length, assignedToCopilot: rows.filter(r => r.copilot).length,
  closedByMergedPR: merged.length, mergedPRsByCopilot: merged.filter(r => r.pr.byCopilot).length,
  medianIssueToMergeHours: med && +med.toFixed(1),
  months, // [YYYY-MM, filed (by issue createdAt), merged (by PR mergedAt)]
}, null, 1));
