// @ts-check
"use strict";

const { createHmac, timingSafeEqual } = require("node:crypto");
const { applyTransactions, replayTransactions } = require("./work_queue_replay.cjs");

const QUEUE_LABEL = "aw:work-queue";
const STATE_LABELS = ["aw:work-queue:available", "aw:work-queue:claimed", "aw:work-queue:completed", "aw:work-queue:cancelled"];
const RECORD_PREFIX = "<!-- gh-aw-work-queue:v1 -->\n";
// github-script's default GITHUB_TOKEN publishes issues and comments as this bot.
const PUBLISHER = "github-actions[bot]";

function debugLog(message, details = {}) {
  const debug = process.env.DEBUG || "";
  if (debug === "*" || debug.includes("work_queue")) {
    console.error(`[work_queue_issues] ${message} ${JSON.stringify(details)}`);
  }
}

function canonical(value) {
  if (value === null || typeof value !== "object") return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
  return `{${Object.keys(value)
    .sort()
    .map(key => `${JSON.stringify(key)}:${canonical(value[key])}`)
    .join(",")}}`;
}

function requireSecret(secret) {
  if (typeof secret !== "string" || !secret) throw new Error("Work queue HMAC secret is not configured");
  return secret;
}

function sign(payload, secret) {
  return createHmac("sha256", requireSecret(secret)).update(canonical(payload)).digest("hex");
}

function serializeRecord(payload, secret) {
  return RECORD_PREFIX + JSON.stringify({ payload, sig: sign(payload, secret) });
}

function record(body, secret) {
  if (typeof body !== "string" || !body.startsWith(RECORD_PREFIX)) return null;
  let envelope;
  try {
    envelope = JSON.parse(body.slice(RECORD_PREFIX.length));
  } catch {
    debugLog("invalid record JSON");
    throw new TypeError("Invalid work queue issue record");
  }
  if (
    !envelope ||
    typeof envelope !== "object" ||
    Array.isArray(envelope) ||
    !envelope.payload ||
    typeof envelope.payload !== "object" ||
    Array.isArray(envelope.payload) ||
    typeof envelope.sig !== "string" ||
    !/^[a-f0-9]{64}$/.test(envelope.sig)
  ) {
    debugLog("ignored invalid record envelope");
    return null;
  }
  const expected = Buffer.from(sign(envelope.payload, secret), "hex");
  const actual = Buffer.from(envelope.sig, "hex");
  if (!timingSafeEqual(actual, expected)) {
    debugLog("ignored record with invalid signature");
    return null;
  }
  return envelope.payload;
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

async function readIssues({ githubClient, owner, repo, secret = process.env.WORK_QUEUE_HMAC_SECRET }) {
  const hmacSecret = requireSecret(secret);
  const publisher = PUBLISHER;
  debugLog("reading issue-backed queue");
  const listedIssues = await pages(githubClient, githubClient.rest.issues.listForRepo, { owner, repo, state: "all", labels: QUEUE_LABEL });
  const issues = listedIssues.filter(issue => !issue.pull_request).sort((a, b) => a.number - b.number);
  debugLog("listed queue issues", { issues: listedIssues.length, pullRequests: listedIssues.length - issues.length });
  const entries = [];
  const byWork = new Map();
  let ignoredIssues = 0;
  let ignoredComments = 0;
  for (const issue of issues) {
    if (issue.user?.login !== publisher) {
      ignoredIssues++;
      continue;
    }
    const work = record(issue.body, hmacSecret);
    if (!work || work.kind !== "Work") throw new TypeError("Labeled work queue issue has no valid Work record");
    applyTransactions([], [work]);
    if (byWork.has(work.work)) {
      const original = entries.find(entry => entry.transaction.kind === "Work" && entry.transaction.work === work.work);
      if (!original || original.transaction.version !== work.version || original.transaction.enqueued !== work.enqueued) throw new Error("Multiple work queue issues have conflicting Work records");
      const duplicateComments = await pages(githubClient, githubClient.rest.issues.listComments, { owner, repo, issue_number: issue.number });
      if (duplicateComments.some(comment => comment.user?.login === publisher && record(comment.body, hmacSecret))) throw new Error("Duplicate work queue issue has transactions");
      continue;
    }
    byWork.set(work.work, issue.number);
    entries.push({ transaction: work, timestamp: issue.created_at, order: issue.id, issue: issue.number, comment: 0 });
    const comments = await pages(githubClient, githubClient.rest.issues.listComments, { owner, repo, issue_number: issue.number });
    for (const comment of comments) {
      if (comment.user?.login !== publisher) {
        ignoredComments++;
        continue;
      }
      const transaction = record(comment.body, hmacSecret);
      if (transaction) {
        if (transaction.kind === "Work" || transaction.work !== work.work) throw new TypeError("Work queue comment targets the wrong Work");
        entries.push({ transaction, timestamp: comment.created_at, order: comment.id, issue: issue.number, comment: comment.id });
      }
    }
  }
  entries.sort((a, b) => a.timestamp.localeCompare(b.timestamp) || Number(b.comment === 0) - Number(a.comment === 0) || a.order - b.order);
  debugLog("replayed issue-backed queue", {
    issues: issues.length,
    entries: entries.length,
    ignoredIssues,
    ignoredComments,
  });
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

async function applyAndPublishIssues({ githubClient, owner, repo, intents, core: coreApi, secret = process.env.WORK_QUEUE_HMAC_SECRET }) {
  const hmacSecret = requireSecret(secret);
  debugLog("applying issue-backed intents", { intents: intents.length });
  let persisted = false;
  let idempotent = 0;
  const rejected = [];
  for (const intent of intents) {
    const current = await readIssues({ githubClient, owner, repo, secret: hmacSecret });
    const applied = applyTransactions(current.transactions, [intent]);
    rejected.push(...applied.rejected);
    idempotent += applied.idempotent;
    if (applied.rejected.length) {
      debugLog("rejected issue-backed intent", { kind: intent.kind, reason: applied.rejected[0].reason });
      continue;
    }
    if (applied.idempotent) {
      debugLog("issue-backed intent already applied", { kind: intent.kind });
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
        body: serializeRecord(intent, hmacSecret),
        labels: [QUEUE_LABEL, STATE_LABELS[0]],
      });
      issueNumber = created.data.number;
      debugLog("created work queue issue", { issue: issueNumber });
    } else {
      if (!issueNumber) throw new Error("Work queue issue is missing");
      await githubClient.rest.issues.createComment({ owner, repo, issue_number: issueNumber, body: serializeRecord(intent, hmacSecret) });
      debugLog("created work queue transaction comment", { issue: issueNumber, kind: intent.kind });
    }
    persisted = true;
    // Always refresh before trusting the write: a concurrent comment or duplicate
    // issue can invalidate this writer's proposed transaction.
    const latest = await readIssues({ githubClient, owner, repo, secret: hmacSecret });
    const accepted = applyTransactions(latest.transactions, [intent]).idempotent === 1;
    if (!accepted) {
      debugLog("issue-backed intent lost concurrent arbitration", { kind: intent.kind });
      throw new Error("Work queue transaction lost concurrent arbitration");
    }
    if (intent.kind === "Work" && issueNumber !== latest.byWork.get(intent.work)) {
      await githubClient.rest.issues.removeLabel({ owner, repo, issue_number: issueNumber, name: QUEUE_LABEL });
      await githubClient.rest.issues.removeLabel({ owner, repo, issue_number: issueNumber, name: STATE_LABELS[0] });
    }
    await syncStateLabel(githubClient, owner, repo, latest.byWork.get(intent.work), replayTransactions(latest.transactions).work[intent.work]);
    coreApi?.info("Work queue: issue transaction published");
  }
  const latest = await readIssues({ githubClient, owner, repo, secret: hmacSecret });
  debugLog("issue-backed intent application completed", {
    persisted,
    rejected: rejected.length,
    idempotent,
    transactions: latest.transactions.length,
  });
  return { sha: latest.sha, transactions: latest.transactions, rejected, idempotent, persisted };
}

module.exports = { QUEUE_LABEL, STATE_LABELS, RECORD_PREFIX, readIssues, applyAndPublishIssues };
