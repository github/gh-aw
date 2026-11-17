#!/usr/bin/env node
// Read-only source inventory and Actions run counts; no workflow dispatches or artifact downloads.
import { execFileSync } from "node:child_process";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { extractInlineSkills } = require("../../../../actions/setup/js/extract_inline_skills.cjs");
const { extractInlineSubAgents } = require("../../../../actions/setup/js/extract_inline_sub_agents.cjs");
const DAY = 864e5;
const iso = time => new Date(time).toISOString().slice(0, 10);
const [, , start = iso(Date.now() - 30 * DAY), end = iso(Date.now() - DAY), ref = "origin/main"] = process.argv;
if (!/^\d{4}-\d{2}-\d{2}$/.test(start) || !/^\d{4}-\d{2}-\d{2}$/.test(end) || !Number.isFinite(Date.parse(start)) || !Number.isFinite(Date.parse(end)) || start > end) {
  throw new Error("Usage: node inlined-skills-agents.mjs START END [REF]; use inclusive YYYY-MM-DD UTC dates.");
}
const git = (...args) => execFileSync("git", args, { encoding: "utf8", maxBuffer: 8 << 20 }).trim();
const sourceCommit = git("rev-parse", "--verify", `${ref}^{commit}`);
const files = git("ls-tree", "--name-only", `${sourceCommit}:.github/workflows`)
  .split("\n")
  .filter(file => file.endsWith(".md") && !file.startsWith("smoke-"));
const rows = files.map(file => {
  const text = git("show", `${sourceCommit}:.github/workflows/${file}`);
  const skills = extractInlineSkills(text).skills.map(skill => skill.name);
  const agents = extractInlineSubAgents(text).agents.map(agent => ({
    name: agent.name,
    model: agent.content.match(/^model:\s*(.+)$/m)?.[1] ?? "inherited",
  }));
  return { file, skills, agents };
});
const inventory = {
  workflows: rows.length,
  withSkills: rows.filter(row => row.skills.length).length,
  withAgents: rows.filter(row => row.agents.length).length,
  withEither: rows.filter(row => row.skills.length || row.agents.length).length,
  withBoth: rows.filter(row => row.skills.length && row.agents.length).length,
  skillBlocks: rows.reduce((sum, row) => sum + row.skills.length, 0),
  agentBlocks: rows.reduce((sum, row) => sum + row.agents.length, 0),
};
const count = (file, lo, hi, success) => {
  const endpoint = `repos/github/gh-aw/actions/workflows/${file.replace(/\.md$/, ".lock.yml")}/runs?per_page=1&created=${lo}..${hi}${success ? "&status=success" : ""}`;
  const result = JSON.parse(execFileSync("gh", ["api", endpoint], { encoding: "utf8" }));
  if (!Number.isSafeInteger(result.total_count) || result.total_count < 0 || result.total_count >= 1000) {
    throw new Error(`Missing or possibly capped count for ${file} (${lo}..${hi}); narrow the query window.`);
  }
  return result.total_count;
};
const examples = ["code-simplifier.md", "pr-code-quality-reviewer.md", "github-mcp-tools-report.md"].map(file => {
  const row = rows.find(row => row.file === file);
  if (!row) throw new Error(`Workflow missing from source snapshot: ${file}`);
  let runs = 0;
  let successfulRuns = 0;
  for (let time = Date.parse(start); time <= Date.parse(end); time += 7 * DAY) {
    const lo = iso(time);
    const hi = iso(Math.min(time + 6 * DAY, Date.parse(end)));
    runs += count(file, lo, hi, false);
    successfulRuns += count(file, lo, hi, true);
  }
  if (successfulRuns > runs) throw new Error(`Inconsistent live run counts for ${file}; repeat after reruns settle.`);
  return { ...row, runs, successfulRuns };
});
console.log(JSON.stringify({ snapshot: iso(Date.now()), sourceCommit, window: { start, end, days: (Date.parse(end) - Date.parse(start)) / DAY + 1 }, inventory, examples }, null, 2));
