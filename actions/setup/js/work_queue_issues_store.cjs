// @ts-check
"use strict";

const { applyTransactions, replayTransactions } = require("./work_queue_replay.cjs");

const QUEUE_LABEL = "aw:work-queue";
const STATE_LABELS = ["aw:work-queue:available", "aw:work-queue:claimed", "aw:work-queue:completed", "aw:work-queue:cancelled"];
const RECORD_PREFIX = "<!-- gh-aw-work-queue:v1 -->\n";

function record(body) {
  if (typeof body !== "string" || !body.startsWith(RECORD_PREFIX)) return null;
  let value;
  try {
    value = JSON.parse(body.slice(RECORD_PREFIX.length));
  } catch {
    throw new TypeError("Invalid work queue issue record");
  }
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new TypeError("Invalid work queue issue record");
  return value;
}

async function pages(githubClient, method, params) {
  const result = [];
  for (let page = 1; ; page++) {
    const response = await method({ ...params, per_page: 100, page });
    if (!Array.isArray(response.data)) throw new TypeError("Invalid work queue issue response");
    result.push(...response.data);
    if (response.data.length < 100) return result;
  }
}

async function readIssues({ githubClient, owner, repo }) {
  const issues = (await pages(githubClient, githubClient.rest.issues.listForRepo, { owner, repo, state: "all", labels: QUEUE_LABEL })).filter(issue => !issue.pull_request);
  const entries = [];
  const byWork = new Map();
  for (const issue of issues) {
    const work = record(issue.body);
    if (!work || work.kind !== "Work") throw new TypeError("Labeled work queue issue has no valid Work record");
    if (byWork.has(work.work)) throw new Error("Multiple work queue issues have the same Work identity");
    byWork.set(work.work, issue.number);
    entries.push({ transaction: work, timestamp: issue.created_at, order: issue.id, issue: issue.number, comment: 0 });
    const comments = await pages(githubClient, githubClient.rest.issues.listComments, { owner, repo, issue_number: issue.number });
    for (const comment of comments) {
      const transaction = record(comment.body);
      if (transaction) {
        if (transaction.kind === "Work" || transaction.work !== work.work) throw new TypeError("Work queue comment targets the wrong Work");
        entries.push({ transaction, timestamp: comment.created_at, order: comment.id, issue: issue.number, comment: comment.id });
      }
    }
  }
  entries.sort((a, b) => a.timestamp.localeCompare(b.timestamp) || Number(b.comment === 0) - Number(a.comment === 0) || a.order - b.order);
  let transactions = [];
  for (const entry of entries) {
    // Comments are append-only, but another writer may have published a stale intent.
    // The first accepted terminal fact wins; later invalid intents have no authority.
    transactions = applyTransactions(transactions, [entry.transaction]).transactions;
  }
  const sha = entries.length ? String(entries[entries.length - 1].comment || entries[entries.length - 1].issue) : null;
  return { sha, transactions, byWork };
}

async function syncStateLabel(githubClient, owner, repo, issueNumber, state) {
  const label = `aw:work-queue:${state}`;
  if (!STATE_LABELS.includes(label)) throw new TypeError("Invalid work queue state");
  await ensureLabel(githubClient, owner, repo, label);
  const issue = await githubClient.rest.issues.get({ owner, repo, issue_number: issueNumber });
  const labels = issue.data.labels.map(value => (typeof value === "string" ? value : value.name));
  for (const previous of STATE_LABELS) {
    if (previous !== label && labels.includes(previous)) {
      await githubClient.rest.issues.removeLabel({ owner, repo, issue_number: issueNumber, name: previous });
    }
  }
  if (!labels.includes(label)) await githubClient.rest.issues.addLabels({ owner, repo, issue_number: issueNumber, labels: [label] });
}

async function ensureLabel(githubClient, owner, repo, name) {
  try {
    await githubClient.rest.issues.getLabel({ owner, repo, name });
  } catch (error) {
    if (!(error instanceof Error && "status" in error && error.status === 404)) throw error;
    try {
      await githubClient.rest.issues.createLabel({ owner, repo, name, color: "1d76db" });
    } catch (conflict) {
      if (!(conflict instanceof Error && "status" in conflict && conflict.status === 422)) throw conflict;
    }
  }
}

async function applyAndPublishIssues({ githubClient, owner, repo, intents, core: coreApi }) {
  let persisted = false;
  let idempotent = 0;
  const rejected = [];
  for (const intent of intents) {
    const current = await readIssues({ githubClient, owner, repo });
    const applied = applyTransactions(current.transactions, [intent]);
    rejected.push(...applied.rejected);
    idempotent += applied.idempotent;
    if (applied.rejected.length) continue;
    if (applied.idempotent) {
      const issueNumber = current.byWork.get(intent.work);
      if (issueNumber) await syncStateLabel(githubClient, owner, repo, issueNumber, replayTransactions(current.transactions).work[intent.work]);
      continue;
    }
    let issueNumber = current.byWork.get(intent.work);
    if (intent.kind === "Work") {
      await ensureLabel(githubClient, owner, repo, QUEUE_LABEL);
      await ensureLabel(githubClient, owner, repo, STATE_LABELS[0]);
      const created = await githubClient.rest.issues.create({
        owner,
        repo,
        title: "Work queue item",
        body: RECORD_PREFIX + JSON.stringify(intent),
        labels: [QUEUE_LABEL, STATE_LABELS[0]],
      });
      issueNumber = created.data.number;
    } else {
      if (!issueNumber) throw new Error("Work queue issue is missing");
      await githubClient.rest.issues.createComment({ owner, repo, issue_number: issueNumber, body: RECORD_PREFIX + JSON.stringify(intent) });
    }
    persisted = true;
    // Always refresh before trusting the write: a concurrent comment or duplicate
    // issue can invalidate this writer's proposed transaction.
    const latest = await readIssues({ githubClient, owner, repo });
    const accepted = applyTransactions(latest.transactions, [intent]).idempotent === 1;
    if (!accepted) throw new Error("Work queue transaction lost concurrent arbitration");
    await syncStateLabel(githubClient, owner, repo, issueNumber, replayTransactions(latest.transactions).work[intent.work]);
    coreApi?.info("Work queue: issue transaction published");
  }
  const latest = await readIssues({ githubClient, owner, repo });
  return { sha: latest.sha, transactions: latest.transactions, rejected, idempotent, persisted };
}

module.exports = { QUEUE_LABEL, STATE_LABELS, RECORD_PREFIX, readIssues, applyAndPublishIssues };
