#!/usr/bin/env node
// Read-only: recomputes the measured data for the cracktro "Docs that heal" slide.
// Usage: node .github/skills/cracktro/refresh/docs-that-heal.mjs [END=YYYY-MM-DD] > /tmp/dh.json
// Needs an authenticated `gh`; makes only read queries.
import { execFileSync } from "node:child_process";

const REPO = process.env.REPO || "github/gh-aw";
const START = "2025-10-01";
const END = process.argv[2] || new Date().toISOString().slice(0, 10);
const gh = args => JSON.parse(execFileSync("gh", args, { encoding: "utf8", maxBuffer: 1 << 28 }));

// 1. [docs] PRs by github-actions, one search per month (search caps at 1000).
const prs = new Map();
for (let d = new Date(START + "T00:00:00Z"); d.toISOString().slice(0, 10) <= END; d.setUTCMonth(d.getUTCMonth() + 1)) {
  const a = d.toISOString().slice(0, 10);
  const e = new Date(d);
  e.setUTCMonth(e.getUTCMonth() + 1);
  e.setUTCDate(0);
  const b = e.toISOString().slice(0, 10) < END ? e.toISOString().slice(0, 10) : END;
  const rows = gh(["pr", "list", "-R", REPO, "--state", "all", "--limit", "1000", "--search", `in:title docs author:app/github-actions created:${a}..${b}`, "--json", "number,title,body,state,mergedAt,createdAt,additions,deletions"]);
  for (const p of rows) if (p.title.startsWith("[docs]")) prs.set(p.number, p);
}

const IDS = { "unbloat-docs": "unbloat", "daily-doc-updater": "updater", "glossary-maintainer": "glossary", "daily-doc-healer": "healer", "developer-docs-consolidator": "consolidator" };
const FOOT = [
  ["unbloat", /unbloat-docs|Documentation Unbloat/i],
  ["updater", /daily-doc-updater|Daily Documentation Updater/],
  ["glossary", /glossary-maintainer|Glossary Maintainer/],
  ["healer", /daily-doc-healer|Documentation Healer/],
  ["consolidator", /developer-docs-consolidator|Documentation Consolidator/],
  ["other", /dictation-prompt|Dictation Prompt|update-astro|Update Astro|spellcheck|Spellcheck|editors-health|Editors Health|Multi-Device|Noob/],
];
const TITLE = [
  ["other", /dictation|spell/i],
  ["glossary", /glossary/i],
  ["unbloat", /unbloat|bloat|reduc|simplif|trim|concise/i],
  ["healer", /heal/i],
  ["consolidator", /consolidat|developer (docs|spec)|instructions/i],
  ["updater", /update|document|add /i],
];
const agentOf = p => {
  const b = p.body || "";
  const m = b.match(/gh-aw-workflow-id: ([a-z0-9-]+)/);
  if (m) return IDS[m[1]] || "other";
  for (const [k, r] of FOOT) if (r.test(b)) return k;
  for (const [k, r] of TITLE) if (r.test(p.title)) return k;
  return "unknown";
};
const agents = {};
for (const p of prs.values()) {
  const k = agentOf(p);
  const a = (agents[k] ||= { opened: 0, merged: 0, additions: 0, deletions: 0, first: p.createdAt });
  a.opened++;
  if (p.createdAt < a.first) a.first = p.createdAt;
  if (p.mergedAt) {
    a.merged++;
    a.additions += p.additions;
    a.deletions += p.deletions;
  }
}
for (const a of Object.values(agents)) {
  a.net = a.additions - a.deletions;
  a.first = a.first.slice(0, 10);
}

// 2. [doc-healer] issues and the PRs that closed them -> RULES [dayOffset, u|h|x].
const iss = gh(["issue", "list", "-R", REPO, "--state", "all", "--limit", "500", "--search", '"[doc-healer]" in:title', "--json", "number,title,createdAt,closedAt,state,stateReason,closedByPullRequestsReferences"])
  .filter(i => i.title.startsWith("[doc-healer]"))
  .sort((x, y) => x.createdAt.localeCompare(y.createdAt));
const day0 = iss.length ? Date.parse(iss[0].createdAt.slice(0, 10)) : 0;
const cache = new Map();
const pr = n => {
  if (!cache.has(n)) {
    const v = gh(["pr", "view", String(n), "-R", REPO, "--json", "files,mergedAt"]);
    cache.set(n, { merged: !!v.mergedAt, updater: v.files.some(f => f.path === ".github/workflows/daily-doc-updater.md") });
  }
  return cache.get(n);
};
const rules = iss.map(i => {
  const refs = (i.closedByPullRequestsReferences || []).map(r => pr(r.number));
  const s = refs.some(r => r.merged && r.updater) ? "u" : refs.some(r => r.merged) ? "h" : "x";
  return [Math.round((Date.parse(i.createdAt.slice(0, 10)) - day0) / 864e5), s, i.number, i.stateReason];
});

console.log(
  JSON.stringify(
    {
      snapshot: END,
      prs: { total: prs.size, window: [START, END], agents },
      healer: {
        issues: iss.length,
        open: iss.filter(i => i.state === "OPEN").length,
        window: iss.length ? [iss[0].createdAt.slice(0, 10), iss.at(-1).createdAt.slice(0, 10)] : [],
        merged: rules.filter(r => r[1] !== "x").length,
        inUpdaterPrompt: rules.filter(r => r[1] === "u").length,
        RULES: rules.map(([d, s]) => [d, s]),
        detail: rules,
      },
    },
    null,
    1
  )
);
