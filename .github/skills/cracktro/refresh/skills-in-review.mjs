#!/usr/bin/env node
// Read-only: recompute the "Skills in review" cracktro panel from gh-aw pull requests. Needs `gh auth login`.
// Usage: node skills-in-review.mjs [START END]   (YYYY-MM-DD PR creation dates, inclusive; default: 2026-05-04 to yesterday UTC)
// Prints one JSON object to stdout; progress goes to stderr. RAW=/tmp/x.json also dumps the matched reviews and threads.
import { execFile } from "node:child_process";
import { promisify } from "node:util";

const run = promisify(execFile);
const DAY = 864e5;
const iso = d => new Date(d).toISOString().slice(0, 10);
const [, , a, b] = process.argv;
const start = a || "2026-05-04";
const end = b || iso(Date.now() - DAY);
const REPO = process.env.REPO || "github/gh-aw";
const FOOTER = "🧠 *Reviewed using Matt Pocock";
const TAG = /^\s*\*\*\\?\[\/([a-z0-9-]+)\\?\]\*\*/; // the safe output escapes brackets: **\[/tdd\]**

const Q = `query($q:String!,$after:String){search(query:$q,type:ISSUE,first:30,after:$after){
  pageInfo{hasNextPage endCursor}
  nodes{... on PullRequest{number createdAt mergedAt
    reviews(first:60){nodes{id state submittedAt body}}
    reviewThreads(first:100){nodes{isResolved isOutdated comments(first:5){totalCount nodes{author{login} body createdAt pullRequestReview{id}}}}}}}}}`;

async function search(q, after) {
  const args = ["api", "graphql", "-f", `query=${Q}`, "-f", `q=${q}`];
  if (after) args.push("-f", `after=${after}`);
  for (let i = 0; ; i++) {
    try {
      const { stdout } = await run("gh", args, { encoding: "utf8", maxBuffer: 1 << 28 });
      return JSON.parse(stdout).data.search;
    } catch (e) {
      if (i >= 4) throw e;
      await new Promise(r => setTimeout(r, 6000 * (i + 1)));
    }
  }
}

const windows = [];
for (let t = Date.parse(start); t <= Date.parse(end); t += 3 * DAY) windows.push(`${iso(t)}..${iso(Math.min(t + 2 * DAY, Date.parse(end)))}`);

const prs = [];
let done = 0;
async function worker() {
  while (windows.length) {
    const w = windows.shift();
    let after = null;
    do {
      const s = await search(`repo:${REPO} is:pr created:${w}`, after);
      prs.push(...s.nodes.filter(n => n.number));
      after = s.pageInfo.hasNextPage ? s.pageInfo.endCursor : null;
    } while (after);
    process.stderr.write(`${w}: ${prs.length} (${++done})\n`);
  }
}
await Promise.all(Array.from({ length: 4 }, worker));

const month = s => s.slice(0, 7);
const out = {
  window: { start, end, snapshot: iso(Date.now()) },
  prsScanned: prs.length,
  reviewedPRs: 0,
  reviewedMerged: 0,
  reviews: { total: 0, APPROVED: 0, CHANGES_REQUESTED: 0, COMMENTED: 0, DISMISSED: 0 },
  reviewsWithComments: 0,
  comments: 0,
  bySkill: {},
  resolved: 0,
  outdated: 0,
  replied: 0,
  copilotReplied: 0,
  acted: 0,
  commentedPRs: 0,
  medianPerPR: 0,
  maxPerPR: 0,
  weekly: {},
  monthly: {},
  firstReview: null,
  lastReview: null,
};
const raw = [];
const perPR = [];
for (const p of prs) {
  const mine = p.reviews.nodes.filter(r => (r.body || "").includes(FOOTER));
  const ids = new Set(mine.map(r => r.id));
  const threads = p.reviewThreads.nodes.filter(t => {
    const c = t.comments.nodes[0];
    return c && (ids.has(c.pullRequestReview?.id) || (c.author?.login === "github-actions" && TAG.test(c.body || "")));
  });
  if (!mine.length && !threads.length) continue;
  out.reviewedPRs++;
  if (p.mergedAt) out.reviewedMerged++;
  for (const r of mine) {
    out.reviews.total++;
    out.reviews[r.state] = (out.reviews[r.state] || 0) + 1;
    const m = (out.monthly[month(r.submittedAt)] ||= { reviews: 0, comments: 0 });
    m.reviews++;
    if (!out.firstReview || r.submittedAt < out.firstReview) out.firstReview = r.submittedAt;
    if (!out.lastReview || r.submittedAt > out.lastReview) out.lastReview = r.submittedAt;
  }
  const withComments = new Set();
  for (const t of threads) {
    const c = t.comments.nodes[0];
    const skill = (TAG.exec(c.body || "") || [, "untagged"])[1];
    out.comments++;
    out.bySkill[skill] = (out.bySkill[skill] || 0) + 1;
    if (t.isResolved || t.isOutdated || t.comments.totalCount > 1) out.acted++;
    const wk = Math.floor((Date.parse(c.createdAt) - Date.parse(start)) / (7 * DAY));
    const ws = (out.weekly[skill] ||= []);
    ws[wk] = (ws[wk] || 0) + 1;
    if (t.isResolved) out.resolved++;
    if (t.isOutdated) out.outdated++;
    const replies = t.comments.nodes.slice(1);
    if (t.comments.totalCount > 1) out.replied++;
    if (replies.some(r => /copilot/i.test(r.author?.login || ""))) out.copilotReplied++;
    (out.monthly[month(c.createdAt)] ||= { reviews: 0, comments: 0 }).comments++;
    if (c.pullRequestReview?.id) withComments.add(c.pullRequestReview.id);
    raw.push({ pr: p.number, skill, at: c.createdAt, resolved: t.isResolved, outdated: t.isOutdated, replies: t.comments.totalCount - 1 });
  }
  out.reviewsWithComments += mine.filter(r => withComments.has(r.id)).length;
  if (threads.length) perPR.push(threads.length);
}
perPR.sort((x, y) => x - y);
out.commentedPRs = perPR.length;
out.medianPerPR = perPR[Math.floor((perPR.length - 1) / 2)] || 0;
out.maxPerPR = perPR.at(-1) || 0;
const nWeeks = Math.floor((Date.parse(end) - Date.parse(start)) / (7 * DAY)) + 1;
for (const k in out.weekly) out.weekly[k] = Array.from({ length: nWeeks }, (_, i) => out.weekly[k][i] || 0); // weeks start on `start`
out.monthly = Object.fromEntries(Object.entries(out.monthly).sort());
out.bySkill = Object.fromEntries(Object.entries(out.bySkill).sort((x, y) => y[1] - x[1]));
if (process.env.RAW) (await import("node:fs")).writeFileSync(process.env.RAW, JSON.stringify(raw));
console.log(JSON.stringify(out, null, 2));
