const fs = require("node:fs");
const path = require("node:path");
const { buildCorpus, collectIssues, collectDiscussions, metadata } = require("./aw_issue_clustering_publish.cjs");

const stopWords = new Set(
  `a an and are as at be been by can code daily for from gh github has have in is
it issue issues of on or report reports run runs that the this to was were with
workflow workflows generated agent agents test tests failed failure fix`.split(/\s+/)
);

function features(item) {
  let body = item.body || "";
  body = body.split(/\n(?:> Generated|<!-- gh-aw-agentic-workflow)/)[0];
  body = body.replace(/https?:\/\/\S+|<!--[\s\S]*?-->/g, " ");
  const text = `${item.title} `.repeat(3) + body.slice(0, 5000);
  const counts = new Map();
  for (const word of text.toLowerCase().match(/[a-z][a-z0-9_./-]{2,}/g) || []) {
    if (!stopWords.has(word) && !word.startsWith("202") && !word.startsWith("http")) {
      counts.set(word, (counts.get(word) || 0) + 1);
    }
  }
  return counts;
}

function vectors(items) {
  const counts = items.map(features);
  const frequencies = new Map();
  for (const terms of counts) {
    for (const word of terms.keys()) frequencies.set(word, (frequencies.get(word) || 0) + 1);
  }
  return counts.map(terms => {
    const vector = new Map([...terms].map(([word, count]) => [word, (1 + Math.log(count)) * Math.log(1 + items.length / frequencies.get(word))]));
    const norm = Math.sqrt([...vector.values()].reduce((sum, value) => sum + value * value, 0)) || 1;
    return new Map([...vector].map(([word, value]) => [word, value / norm]));
  });
}

function similarity(left, right) {
  let score = 0;
  for (const [word, value] of left) score += value * (right.get(word) || 0);
  return score;
}

function seedClusters(issues, limit = 30) {
  if (!issues.length) return [];
  const items = [...issues].sort((a, b) => a.number - b.number);
  const points = vectors(items);
  let centers = [0];
  while (centers.length < Math.min(limit, items.length)) {
    let candidate;
    let distance = Infinity;
    for (const [index, point] of points.entries()) {
      if (centers.includes(index)) continue;
      const current = Math.max(...centers.map(center => similarity(point, points[center])));
      if (candidate === undefined || current < distance) {
        candidate = index;
        distance = current;
      }
    }
    centers.push(candidate);
  }
  let groups;
  for (let iteration = 0; iteration < 4; iteration++) {
    groups = centers.map(() => []);
    for (const [index, point] of points.entries()) {
      let group = centers.indexOf(index);
      if (group < 0) {
        let score = -Infinity;
        for (const [candidate, center] of centers.entries()) {
          const current = similarity(point, points[center]);
          if (current > score) {
            group = candidate;
            score = current;
          }
        }
      }
      groups[group].push(index);
    }
    const next = groups.map(group =>
      group.reduce((best, candidate) => {
        const score = index => group.reduce((sum, other) => sum + similarity(points[index], points[other]), 0);
        return score(candidate) > score(best) ? candidate : best;
      })
    );
    if (next.every((center, index) => center === centers[index])) break;
    centers = next;
  }
  return groups.map((group, index) => ({
    members: group.map(member => items[member].number),
    representative: items[centers[index]].title,
    terms: [...points[centers[index]]]
      .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
      .slice(0, 8)
      .map(([word]) => word),
  }));
}

function compact(item) {
  return {
    number: item.number,
    title: item.title,
    url: item.html_url || item.url,
    updatedAt: item.updated_at || item.updatedAt,
    excerpt: (item.body || "").slice(0, 1400),
    labels: (item.labels || []).map(label => label.name),
  };
}

function writeEvidence(corpus, output) {
  fs.mkdirSync(output, { recursive: true });
  fs.writeFileSync(path.join(output, "corpus.json"), JSON.stringify(corpus));
  for (const kind of ["issues", "reports"]) {
    const directory = path.join(output, kind);
    fs.mkdirSync(directory, { recursive: true });
    for (const item of corpus[kind]) fs.writeFileSync(path.join(directory, `${item.number}.json`), JSON.stringify(item));
  }
  const summary = {
    repo: corpus.repo,
    counts: Object.fromEntries(["issues", "reports", "managed", "excluded", "completed", "cleanup"].map(kind => [kind, corpus[kind].length])),
    issues: corpus.issues.map(compact),
    reports: corpus.reports.map(item => ({
      ...compact(item),
      deep_report: (item.body || "").includes("<!-- gh-aw-workflow-id: deep-report -->"),
      references: [...new Set([...(item.body || "").matchAll(/(?<![a-zA-Z0-9])#([0-9]+)\b/g)].map(match => Number(match[1])))].sort((a, b) => a - b),
    })),
    managed: corpus.managed.map(item => ({ number: item.number, assigned: Boolean(item.assignees?.length), cluster: metadata(item) })),
    completed: corpus.completed.map(item => ({ number: item.number, closed_at: item.closed_at, cluster: metadata(item) })),
    cleanup: corpus.cleanup,
    excluded: corpus.excluded,
    seeds: seedClusters(corpus.issues),
  };
  fs.writeFileSync(path.join(output, "index.json"), JSON.stringify(summary, null, 2));
  return summary;
}

async function collect({ github, context, core, output = "/tmp/gh-aw/agent/aw-issue-clustering" }) {
  const { owner, repo } = context.repo;
  const cutoff = (await github.rest.actions.getWorkflowRun({ owner, repo, run_id: context.runId })).data.created_at;
  const issues = await collectIssues(github, owner, repo, cutoff);
  const sources = buildCorpus(`${owner}/${repo}`, issues, [], cutoff).issues;
  const discussions = await collectDiscussions(github, owner, repo, sources, core);
  const summary = writeEvidence(buildCorpus(`${owner}/${repo}`, issues, discussions, cutoff), output);
  core.info(JSON.stringify(summary.counts));
  return summary;
}

module.exports = { collect, features, vectors, seedClusters, compact, writeEvidence };
