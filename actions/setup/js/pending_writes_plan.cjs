// @ts-check
"use strict";

const { SCHEMA, canonicalPendingJSON, inputDigest, itemDigest, parsePendingJSON } = require("./pending_writes.cjs");
const { readInput, describeDirectory, sha256, MAX_INPUT_BYTES } = require("./pending_writes_files.cjs");
const { resolveTarget } = require("./safe_output_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo, isExactRepoSlug } = require("./repo_helpers.cjs");
const { describeCodeWrite } = require("./pending_writes_code.cjs");
const { buildSafeOutputDependencyPlan } = require("./safe_output_dependencies.cjs");
const { extractTemporaryIdReferences, getCreatedTemporaryId } = require("./temporary_id.cjs");
const { normalizeSafeOutputName } = require("./safe_output_names.cjs");
const { normalizeBranchName } = require("./normalize_branch_name.cjs");
const { getBaseBranch } = require("./get_base_branch.cjs");
const { parseBoolTemplatable } = require("./templatable.cjs");
const { normalizeThreatKinds } = require("./threat_detection_warning.cjs");
const { normalizeLedgerAppends } = require("./ledger_transactions.cjs");

const NO_WRITE_TYPES = new Set(["noop", "report_missing_tool", "report_missing_data", "report_incomplete", "ledger_mutation"]);
const CREATION_TYPES = new Set(["create_issue", "create_discussion", "create_security_advisory", "create_project", "create_check_run", "create_agent_session"]);
const ISSUE_TYPES = new Set(["close_issue", "update_issue", "set_issue_type", "set_issue_field", "link_sub_issue"]);
const BOTH_TYPES = new Set(["add_comment", "add_labels", "remove_labels", "replace_label", "assign_milestone", "assign_to_user", "unassign_from_user", "assign_to_agent", "comment_memory"]);
const PR_TYPES = new Set([
  "push_to_pull_request_branch",
  "update_pull_request",
  "merge_pull_request",
  "close_pull_request",
  "mark_pull_request_as_ready_for_review",
  "add_reviewer",
  "create_pull_request_review_comment",
  "submit_pull_request_review",
  "dismiss_pull_request_review",
  "reply_to_pull_request_review_comment",
  "resolve_pull_request_review_thread",
]);
const TEXT_FIELDS = new Set(["title", "body", "message", "reason", "details", "content", "description", "name", "record", "value", "patch", "labels", "assignees", "reviewers", "text", "comment"]);

function withoutCredentials(value) {
  if (Array.isArray(value)) return value.map(withoutCredentials);
  if (!value || typeof value !== "object") return value;
  return Object.fromEntries(
    Object.entries(value)
      .filter(([key]) => !["github-token", "head-github-token", "github_token", "head_github_token", "_prReviewBufferRegistry", "_activationCommentLinks", "allowedMentionAliases"].includes(key))
      .map(([key, child]) => [key, withoutCredentials(child)])
  );
}

/** No arbitrary object keys or string values are copied into the public manifest. */
function describeText(value, field = "", result = []) {
  if (typeof value === "string") result.push({ field, length: Buffer.byteLength(value, "utf8"), sha256: sha256(value) });
  else if (Array.isArray(value)) value.forEach((child, index) => describeText(child, `${field}/${index}`, result));
  else if (value && typeof value === "object") {
    for (const key of Object.keys(value).sort()) describeText(value[key], `${field}/${TEXT_FIELDS.has(key) ? key : sha256(key)}`, result);
  }
  return result;
}

function describeDetection(detection = {}) {
  const enums = (value, allowed, fallback) => (allowed.includes(value) ? value : fallback);
  return {
    enabled: detection.enabled === true,
    mode: enums(detection.mode, ["block", "warn", "disabled"], detection.enabled ? "block" : "disabled"),
    conclusion: enums(detection.conclusion, ["success", "warning", "failure", "skipped", "cancelled"], "unknown"),
    reason_category: enums(detection.reason, ["threat_detected", "agent_failure", "parse_error", "github_api_quota_exhausted"], "unknown"),
    threat_flags: normalizeThreatKinds(detection.threatKinds)
      .split(",")
      .filter(kind => kind !== "unknown")
      .sort(),
  };
}

function safeTargetString(value) {
  if (typeof value !== "string" || value.length > 1024 || /[\x00-\x1f\x7f]/.test(value)) throw new Error("Invalid pending-write target");
  return value;
}

/**
 * Reuse the same repo/number resolvers as the handlers. Unsupported selectors
 * are explicit incomplete plans; they never silently become triggering.
 */
async function describeTarget(message, config, context, branches) {
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  const repoResult = resolveAndValidateRepo(message, defaultTargetRepo, allowedRepos, message.type);
  if (!repoResult.success || !isExactRepoSlug(repoResult.repoParts)) throw new Error("Pending-write repository cannot be resolved");
  /** @type {Record<string, any>} */
  const target = { repository: safeTargetString(repoResult.repo) };
  if (message.type === "create_pull_request") {
    const base = config.base_branch || (await getBaseBranch(repoResult.repoParts));
    target.base = safeTargetString(message.base || base);
    target.branch = branches.get(message);
    if (config.head_repo) target.head_repository = safeTargetString(config.head_repo);
    return target;
  }
  if (CREATION_TYPES.has(message.type) || NO_WRITE_TYPES.has(message.type) || message.type === "ledger_append") return target;
  if (message.type === "add_comment" && ["discussion", "discussion_comment"].includes(context.eventName) && (config.target || "triggering") === "triggering") {
    target.number = context.payload?.discussion?.number;
    target.kind = "discussion";
    if (!Number.isSafeInteger(target.number)) throw new Error("Pending-write discussion cannot be resolved");
    return target;
  }
  if (ISSUE_TYPES.has(message.type) || BOTH_TYPES.has(message.type) || PR_TYPES.has(message.type)) {
    const result = resolveTarget({
      targetConfig: config.target || "triggering",
      item: message,
      context,
      itemType: message.type,
      supportsPR: BOTH_TYPES.has(message.type),
      supportsIssue: ISSUE_TYPES.has(message.type),
    });
    if (!result.success) throw new Error("Pending-write item number cannot be resolved");
    target.number = result.number;
    target.kind = result.contextType === "issue" ? "issue" : "pull_request";
    if (message.type === "push_to_pull_request_branch") {
      const { data: pr } = await github.rest.pulls.get({ ...repoResult.repoParts, pull_number: result.number });
      if (pr?.number !== result.number || !pr.head?.sha || !pr.head?.ref || !pr.base?.ref) throw new Error("Pending-write PR head cannot be resolved");
      target.branch = safeTargetString(pr.head.ref);
      target.base = safeTargetString(pr.base.ref);
      target.head_repository = safeTargetString(pr.head.repo?.full_name || repoResult.repo);
      target.head_commit = pr.head.sha;
    }
    return target;
  }
  throw new Error("Pending-write handler target is unsupported");
}

function dynamicSettings(config, message) {
  // These modes discover additional mutation targets during execution.
  return (
    ["close_older_issues", "close_older_pull_requests", "group", "group_by_day", "hide_older_comments", "replace_older_comments"].some(key => parseBoolTemplatable(config[key]?.enabled ?? config[key], false)) ||
    ["parent", "parent_issue_number", "blocked_by", "artifacts", "comment_id", "commentId", "comment-id"].some(key => message[key] != null)
  );
}

function describeInput(binding) {
  if (binding.kind === "directory") return { id: binding.id, kind: binding.kind, files: describeDirectory(binding.path) };
  if (binding.kind === "value") return { id: binding.id, kind: binding.kind, sha256: inputDigest(binding.value) };
  const { length, sha256: digest, mode } = readInput(binding.path);
  return { id: binding.id, kind: "file", length, sha256: digest, mode };
}

function describeLedgerTransaction(messages, config, run) {
  const requests = messages.filter(message => message.type === "ledger_append").map(({ type, output_id, depends_on, "depends-on": dependencies, ...request }) => request);
  const ledgers = Object.fromEntries((config.ledger_append?.ledgers || []).map(ledger => [ledger.name, ledger]));
  const transaction = normalizeLedgerAppends(requests, { transactionId: `${run.run_id}:${run.run_attempt}`, ledgerNames: new Set(Object.keys(ledgers)), ledgers });
  const artifact = { version: 1, transaction_id: transaction.transaction_id, ledgers: {} };
  for (const append of transaction.appends) {
    artifact.ledgers[append.ledger] ||= { appends: [] };
    artifact.ledgers[append.ledger].appends.push(append);
  }
  return artifact;
}

/**
 * @param {Record<string, any>} options
 */
async function buildPendingWrites(options) {
  const { bytes: agentBytes } = readInput(options.agentOutputPath);
  const output = parsePendingJSON(agentBytes.toString("utf8"));
  if (!output || !Array.isArray(output.items) || output.items.length > 1000) throw new Error("Pending writes require a bounded items array");
  const messages = output.items.map(message => {
    if (!message || typeof message !== "object" || Array.isArray(message) || typeof message.type !== "string") throw new Error("Pending-write item is malformed");
    return { ...message, type: normalizeSafeOutputName(message.type) };
  });
  const config = withoutCredentials(options.handlerConfig || {});
  const run = options.run;
  if (!run || !run.repository || !run.workflow_ref || !run.workflow_sha || !run.run_id || !run.run_attempt || !run.event) throw new Error("Pending writes require complete run provenance");
  const context = options.context || globalThis.context;
  const bindings = [{ id: "agent_output", kind: "file", path: options.agentOutputPath }, ...(options.bindings || [])];
  const inputs = bindings.map(describeInput);
  bindings.push({ id: "handler_config", kind: "value", value: config });
  inputs.push({ id: "handler_config", kind: "value", sha256: inputDigest(config) });
  const graph = buildSafeOutputDependencyPlan(messages);
  const ids = messages.map((message, index) => message.output_id || `so_${inputDigest([index, message]).slice(0, 12)}`);
  const branches = new Map();
  const salts = new Map();
  messages.forEach((message, index) => {
    if (message.type !== "create_pull_request") return;
    const salt = inputDigest([run, ids[index]]).slice(0, 16);
    salts.set(message, salt);
    const handlerConfig = config.create_pull_request || {};
    const branch = message.branch ? normalizeBranchName(message.branch.trim(), handlerConfig.preserve_branch_name === true ? null : salt) : `${options.workflowId || "workflow"}-${salt}`;
    branches.set(message, `${handlerConfig.branch_prefix || ""}${branch}`);
  });
  const producers = new Map(messages.map((message, index) => [getCreatedTemporaryId(message), index]).filter(([id]) => id));
  const items = [];
  for (const [index, message] of messages.entries()) {
    const handlerConfig = config[message.type] || {};
    const dependencies = new Set([...graph.prerequisites[index]].map(producer => ids[producer]));
    for (const temporaryId of extractTemporaryIdReferences(message)) {
      const producer = producers.get(temporaryId);
      if (producer !== undefined && producer !== index) dependencies.add(ids[producer]);
    }
    /** @type {Record<string, any>} */
    const item = {
      output_id: ids[index],
      type: message.type,
      target: {},
      depends_on: [...dependencies].sort(),
      settings: {
        staged: parseBoolTemplatable(handlerConfig.staged, options.staged === true),
        draft: parseBoolTemplatable(message.draft, parseBoolTemplatable(handlerConfig.draft, false)),
        protected_files_policy: handlerConfig.protected_files_policy || (message.type === "create_pull_request" ? "request_review" : "blocked"),
        sha256: inputDigest(handlerConfig),
      },
      free_text: describeText(message),
      input_sha256: inputDigest(message),
      complete: true,
      reason_category: "ready",
    };
    try {
      if (!config[message.type] && !NO_WRITE_TYPES.has(message.type)) throw new Error("Unconfigured pending-write handler");
      if (graph.invalid.has(index) || graph.cyclic) throw new Error("Invalid pending-write dependency graph");
      item.target = await describeTarget(message, handlerConfig, context, branches);
      if (dynamicSettings(handlerConfig, message)) throw new Error("Pending-write plan requires dynamic auxiliary targets");
      if (["create_pull_request", "push_to_pull_request_branch"].includes(message.type)) {
        const described = await describeCodeWrite(message, item.target, handlerConfig, salts.get(message), options.readToken);
        item.code = described.code;
        for (const binding of described.bindings) {
          bindings.push({ id: `${binding.suffix}:${ids[index]}`, kind: binding.kind, path: binding.path });
          inputs.push(describeInput(bindings[bindings.length - 1]));
        }
        if (!item.code.complete) throw new Error("Pending-write code inventory is incomplete");
      }
    } catch {
      item.complete = false;
      item.reason_category = "unsupported_plan";
    }
    item.item_sha256 = itemDigest(item);
    items.push(item);
  }
  for (const publication of options.publications || []) {
    let supplied = publication.supplied;
    if (publication.kind === "ledger") supplied = describeLedgerTransaction(messages, config, run);
    else if (publication.binding_id) supplied = inputs.find(input => input.id === publication.binding_id);
    /** @type {Record<string, any>} */
    const item = {
      output_id: `job_${inputDigest([publication.job, publication.id]).slice(0, 16)}`,
      type: publication.type,
      target: { repository: publication.repository || run.repository, job: publication.job, ...(publication.branch ? { branch: publication.branch } : {}) },
      depends_on: publication.kind === "ledger" ? items.filter(item => item.type === "ledger_append").map(item => item.output_id) : [],
      settings: { staged: options.staged === true, sha256: inputDigest(withoutCredentials(publication.settings || {})) },
      free_text: [],
      input_sha256: inputDigest(supplied ?? null),
      publication: { id: publication.id, scope: "supplied_inputs", sha256: inputDigest(supplied ?? null), ...(supplied?.files ? { files: supplied.files } : {}) },
      complete: supplied !== undefined,
      reason_category: supplied === undefined ? "unsupported_plan" : "ready",
    };
    item.item_sha256 = itemDigest(item);
    items.push(item);
  }
  const manifest = { schema: SCHEMA, run, detection: describeDetection(options.detection), complete: items.every(item => item.complete), inputs, items };
  const bytes = Buffer.from(canonicalPendingJSON(manifest), "utf8");
  for (const [key, value] of Object.entries(process.env)) {
    if (/(?:TOKEN|SECRET|PASSWORD|PRIVATE_KEY)/.test(key) && value && value.length >= 8 && bytes.includes(Buffer.from(value))) throw new Error("Pending-write metadata contains a secret; refusing publication");
  }
  return { manifest, bytes, sha256: sha256(bytes), bindings };
}

module.exports = { buildPendingWrites, describeText, describeDetection, describeLedgerTransaction, withoutCredentials };
