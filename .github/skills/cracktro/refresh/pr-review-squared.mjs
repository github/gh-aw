#!/usr/bin/env node
// Read-only: recompute the "PR Review²" cracktro strip from gh-aw merged PRs. Needs `gh auth login`.
// Usage: node pr-review-squared.mjs [START END]   (YYYY-MM-DD, inclusive; default: 30 days ending yesterday UTC)
// Prints one JSON object to stdout; progress goes to stderr. RAW=/tmp/x.json also dumps the raw PR nodes.
import { execFileSync } from "node:child_process";

const DAY = 864e5;
const iso = d => new Date(d).toISOString().slice(0, 10);
const [, , a, b] = process.argv;
const end = b || iso(Date.now() - DAY);
const start = a || iso(Date.parse(end) - 29 * DAY);
const REPO = process.env.REPO || "github/gh-aw";

// review-body footers of the three reviewer workflows
const REVIEWERS = { skills: "🧠 *Reviewed using Matt Pocock", quality: "🔎 *Code quality review by", test: "🧪 *Test quality analysis by" };
const NAMES = ["Matt Pocock Skills Reviewer", "PR Code Quality Reviewer", "Test Quality Sentinel"];
const NUDGE_MARK = "gh-aw-workflow-call-id: github/gh-aw/pr-sous-chef";

const Q = `query($q:String!,$after:String){search(query:$q,type:ISSUE,first:20,after:$after){
  pageInfo{hasNextPage endCursor}
  nodes{... on PullRequest{number mergedAt
    reviews(first:60){nodes{id author{login} body}}
    comments(first:100){nodes{author{login} body createdAt}}
    reviewThreads(first:100){totalCount nodes{isResolved comments(first:1){nodes{author{login} pullRequestReview{id}}}}}
    commits(last:100){nodes{commit{committedDate parents{totalCount} authors(first:3){nodes{user{login}}}}}}
    timelineItems(itemTypes:[READY_FOR_REVIEW_EVENT],last:1){nodes{... on ReadyForReviewEvent{createdAt}}}}}}}`;

const search = (q, after) => {
  const args = ["api", "graphql", "-f", `query=${Q}`, "-f", `q=${q}`];
  if (after) args.push("-f", `after=${after}`);
  for (let i = 0; ; i++) {
    try {
      return JSON.parse(execFileSync("gh", args, { encoding: "utf8", maxBuffer: 1 << 28 })).data.search;
    } catch (e) {
      if (i >= 3) throw e;
      execFileSync("sleep", [String(5 * (i + 1))]);
    }
  }
};

const prs = [];
for (let t = Date.parse(start); t <= Date.parse(end); t += 3 * DAY) {
  const w = `${iso(t)}..${iso(Math.min(t + 2 * DAY, Date.parse(end)))}`;
  let after = null;
  do {
    const s = search(`repo:${REPO} is:pr is:merged merged:${w}`, after);
    prs.push(...s.nodes.filter(n => n.number));
    after = s.pageInfo.hasNextPage ? s.pageInfo.endCursor : null;
  } while (after);
  process.stderr.write(`${w}: ${prs.length}\n`);
}

if (process.env.RAW) (await import("node:fs")).writeFileSync(process.env.RAW, JSON.stringify(prs));

const out = {
  window: { start, end, days: Math.round((Date.parse(end) - Date.parse(start)) / DAY) + 1 },
  merged: prs.length,
  checked: 0,
  reviewedPRs: 0,
  reviews: { total: 0, skills: 0, quality: 0, test: 0 },
  threads: 0,
  threadsResolved: 0,
  nudges: 0,
  nudgedPRs: 0,
  fixCommits: 0,
  fixedPRs: 0,
  readyToMergeHours: { median: null, n: 0 },
  singleRound: 0,
  truncated: [],
};
const hours = [];
for (const p of prs) {
  const per = { skills: 0, quality: 0, test: 0 };
  const ids = new Set();
  for (const r of p.reviews.nodes) {
    const k = Object.keys(REVIEWERS).find(k => (r.body || "").includes(REVIEWERS[k]));
    if (k) (per[k]++, out.reviews[k]++, out.reviews.total++, ids.add(r.id));
  }
  // checked = a footer review, or a completion comment from one of the three workflows
  const done = p.comments.nodes.some(c => c.author?.login === "github-actions" && /completed/.test(c.body || "") && NAMES.some(n => c.body.includes(n)));
  if (ids.size || done) out.checked++;
  if (ids.size) {
    out.reviewedPRs++;
    // threads whose first comment belongs to a footer review
    for (const t of p.reviewThreads.nodes)
      if (ids.has(t.comments.nodes[0]?.pullRequestReview?.id)) (out.threads++, t.isResolved && out.threadsResolved++);
    if (Object.values(per).every(v => v <= 1)) out.singleRound++;
    const rfr = p.timelineItems.nodes[0]?.createdAt;
    if (rfr) hours.push((Date.parse(p.mergedAt) - Date.parse(rfr)) / 36e5);
  }
  const nudges = p.comments.nodes.filter(c => (c.body || "").startsWith("@copilot") && c.body.includes(NUDGE_MARK));
  if (nudges.length) {
    out.nudges += nudges.length;
    out.nudgedPRs++;
    const first = nudges.map(c => c.createdAt).sort()[0];
    const fixes = p.commits.nodes.filter(n => n.commit.committedDate > first && n.commit.authors.nodes[0]?.user?.login === "Copilot").length;
    out.fixCommits += fixes;
    if (fixes) out.fixedPRs++;
  }
  if (p.reviews.nodes.length >= 60 || p.comments.nodes.length >= 100 || p.reviewThreads.totalCount > 100 || p.commits.nodes.length >= 100) out.truncated.push(p.number);
}
hours.sort((x, y) => x - y);
const m = hours.length >> 1;
const median = hours.length ? (hours.length % 2 ? hours[m] : (hours[m - 1] + hours[m]) / 2) : null;
out.readyToMergeHours = { median: median === null ? null : +median.toFixed(1), n: hours.length };
console.log(JSON.stringify(out, null, 2));
