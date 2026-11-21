const { createInterface } = require("node:readline");
const backlogLabels = require("../backlog-labels.json");

const runAlertTitles = new Set(["[aw] agentic workflows out of sync", "[aw] Failed runs", "[aw] No-Op Runs", "[aw] Detection Runs", "[aw] Failure cascade detected", "[aw] Daily failure issue cap exceeded"]);

function backlogCandidate(issue) {
  const labels = issue.labels.map(label => label.name);
  if (issue.pull_request || labels.some(label => label.startsWith("backlog:"))) return null;

  const isRunAlert =
    issue.user?.login === "github-actions[bot]" &&
    labels.includes("agentic-workflows") &&
    (runAlertTitles.has(issue.title) || (issue.title.startsWith("[aw] Failed jobs: ") && /<!-- gh-aw-agentic-workflow:/.test(issue.body ?? "")) || (issue.title.startsWith("[aw] ") && /<!-- gh-aw-failure-issue:/.test(issue.body ?? "")));

  return {
    number: issue.number,
    title: issue.title,
    body: issue.body,
    labels,
    ...(isRunAlert ? { primary_backlog_label: backlogLabels["Run alerts and incidents"] } : {}),
  };
}

async function collectCandidates(input) {
  const limit = 20;
  const candidates = [];
  const deferred = [];
  for await (const line of createInterface({ input, crlfDelay: Infinity })) {
    if (candidates.length >= limit) continue;
    const candidate = backlogCandidate(JSON.parse(line));
    if (!candidate) continue;
    if (candidate.labels.includes("needs-triage") && !candidate.primary_backlog_label) {
      if (deferred.length < limit) deferred.push(candidate);
    } else {
      candidates.push(candidate);
    }
  }
  return candidates.concat(deferred.slice(0, limit - candidates.length));
}

if (require.main === module) {
  collectCandidates(process.stdin).then(
    candidates => console.log(JSON.stringify(candidates)),
    error => {
      console.error(error);
      process.exitCode = 1;
    }
  );
}

module.exports = { collectCandidates };
