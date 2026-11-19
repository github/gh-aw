// @ts-check
"use strict";

/**
 * Trusted effect-preparation adapters for resolved-effect safe-output authorization.
 *
 * A compiler/runtime-owned adapter prepares a finite, ordered list of outbound
 * effects (GitHub REST requests, allowlisted GraphQL mutations or git ref
 * updates), freezes them into a private plan and obtains exactly one
 * `resolved_effect` decision for that plan. Execution can only consume the
 * frozen effects, in order, once each, through adapter-owned transports that
 * verify the final wire request immediately before network I/O.
 *
 * Identifiers of resources created by an earlier effect of the same plan
 * (issue numbers, node IDs, commit OIDs) cannot be known before authorization.
 * They are bound as typed derived slots whose source effect and receipt field
 * are part of the plan digest; no other part of a request may vary.
 *
 * The plan never leaves this process: the authority receives its canonical
 * digest and bounded opaque references through the existing v1 envelope.
 */

const { currentSafeOutputInvocation } = require("./safe_output_execution.cjs");

/** @typedef {Readonly<{contract: string, adapter: string, route: string, plan_digest: string, effect_count: number}>} EffectPlanHandle */
/** @typedef {"confirmed"|"rejected"|"indeterminate"|"absent"} SettledStatus */

const CONTRACT = "gh-aw.safe-output-effect-plan.v1";
const MAX_EFFECTS = 128;
const MAX_TARGETS = 40;
const MAX_PRECONDITIONS = 8;
const CODE = /^[a-z][a-z0-9_.]{0,63}$/;
const SLOT = /^[a-z][a-z0-9_]{0,31}$/;
const SLOT_TOKEN = /\{\{([a-z][a-z0-9_]{0,31})\}\}/g;
const NUMERIC_ID = /^[1-9][0-9]{0,19}$/;
const OID = /^(?:[0-9a-f]{40}|[0-9a-f]{64})$/;
const NODE = /^[A-Za-z0-9_=-]{1,128}$/;
const REF = /^refs\/heads\/(?!.*(?:\.\.|@\{|\/\/|\/$|\.lock(?:\/|$)))[A-Za-z0-9._/-]{1,240}$/;
const ACCEPT = "application/vnd.github+json";
const JSON_CONTENT_TYPE = "application/json; charset=utf-8";
// Credentials and client identification are transport concerns. Changing them
// cannot alter the method, target, body or semantic headers checked below.
const TRANSPORT_HEADERS = new Set(["authorization", "user-agent"]);

const REPO = "/repos/[^/]+/[^/]+";
const NUMBER = "(?:[1-9][0-9]*|\\{\\{[a-z][a-z0-9_]{0,31}\\}\\})";
const RECEIPT_TYPES = Object.freeze({
  integer: value => Number.isSafeInteger(value) && value > 0,
  node: value => typeof value === "string" && NODE.test(value),
  oid: value => typeof value === "string" && OID.test(value),
});

/**
 * Exact allowlisted GraphQL mutation documents. Adapters supply variables only.
 * Receipts are read from the response `data`.
 */
const GRAPHQL_MUTATIONS = Object.freeze({
  "graphql.createCommitOnBranch": Object.freeze({
    query: "mutation($input: CreateCommitOnBranchInput!) { createCommitOnBranch(input: $input) { commit { oid } } }",
    receipt: Object.freeze({ oid: Object.freeze(["createCommitOnBranch.commit.oid", "oid"]) }),
  }),
  "graphql.updateIssueLabels": Object.freeze({
    query: "mutation($issueId: ID!, $labels: [LabelUpdateInput!]!) { updateIssue(input: { id: $issueId, labels: $labels }) { issue { id labels(first: 100) { nodes { id name } } } } }",
    headers: Object.freeze({ "graphql-features": "update_issue_suggestions" }),
    receipt: Object.freeze({}),
  }),
  "graphql.setIssueFieldValue": Object.freeze({
    query: "mutation($input: SetIssueFieldValueInput!) { setIssueFieldValue(input: $input) { issue { id } } }",
    receipt: Object.freeze({}),
  }),
  "graphql.addSubIssue": Object.freeze({
    query: "mutation($parentId: ID!, $subIssueId: ID!) { addSubIssue(input: { issueId: $parentId, subIssueId: $subIssueId }) { issue { id number } subIssue { id number } } }",
    receipt: Object.freeze({}),
  }),
  "graphql.enablePullRequestAutoMerge": Object.freeze({
    query: "mutation($prId: ID!, $mergeMethod: PullRequestMergeMethod) { enablePullRequestAutoMerge(input: { pullRequestId: $prId, mergeMethod: $mergeMethod }) { pullRequest { id } } }",
    receipt: Object.freeze({}),
  }),
});

/** @param {string} method @param {string} path @param {boolean} body @param {Record<string, string[]>} [receipt] */
function rest(method, path, body, receipt = {}) {
  return Object.freeze({ kind: "rest", method, path: `^${REPO}${path}$`, body, receipt: Object.freeze(receipt) });
}

const ISSUE_RECEIPT = { number: ["number", "integer"], id: ["id", "integer"], node_id: ["node_id", "node"] };
const OPERATIONS = Object.freeze({
  "issues.create": rest("POST", "/issues", true, ISSUE_RECEIPT),
  "issues.update": rest("PATCH", `/issues/${NUMBER}`, true, ISSUE_RECEIPT),
  "issues.setLabels": rest("PUT", `/issues/${NUMBER}/labels`, true),
  "issues.addLabels": rest("POST", `/issues/${NUMBER}/labels`, true),
  "issues.removeLabel": rest("DELETE", `/issues/${NUMBER}/labels/[^/]+`, false),
  "issues.addAssignees": rest("POST", `/issues/${NUMBER}/assignees`, true),
  "issues.addBlockedBy": rest("POST", `/issues/${NUMBER}/dependencies/blocked_by`, true),
  "pulls.create": rest("POST", "/pulls", true, { ...ISSUE_RECEIPT, head_sha: ["head.sha", "oid"] }),
  "pulls.merge": rest("PUT", `/pulls/${NUMBER}/merge`, true),
  "pulls.requestReviewers": rest("POST", `/pulls/${NUMBER}/requested_reviewers`, true),
  "pulls.createReview": rest("POST", `/pulls/${NUMBER}/reviews`, true),
  "git.createRef": rest("POST", "/git/refs", true, { sha: ["object.sha", "oid"] }),
  ...Object.fromEntries(Object.entries(GRAPHQL_MUTATIONS).map(([id, mutation]) => [id, Object.freeze({ kind: "graphql", method: "POST", ...mutation })])),
  "git.push": Object.freeze({ kind: "git", receipt: Object.freeze({ oid: Object.freeze(["oid", "oid"]) }) }),
});

/** @param {string} id @param {number} maxEffects @param {string[]} operations */
function adapterContract(id, maxEffects, operations) {
  return Object.freeze({ id, version: 1, routes: Object.freeze(["manager", "claim"]), max_effects: maxEffects, operations: Object.freeze(operations) });
}

/**
 * Coverage is per output type, execution route and contract version. Routes not
 * listed here (prepared/script Claim adapters, standalone, workflow, action,
 * job, script and synthetic phases) remain invocation-bound.
 */
const ADAPTERS = Object.freeze({
  add_labels: adapterContract("gh-aw.add-labels", 2, ["issues.addLabels", "graphql.updateIssueLabels"]),
  create_issue: adapterContract("gh-aw.create-issue", 34, ["issues.create", "graphql.setIssueFieldValue", "issues.addBlockedBy", "graphql.addSubIssue"]),
  create_pull_request: adapterContract("gh-aw.create-pull-request", MAX_EFFECTS, [
    "git.push",
    "git.createRef",
    "graphql.createCommitOnBranch",
    "pulls.create",
    "issues.addLabels",
    "pulls.requestReviewers",
    "issues.addAssignees",
    "pulls.createReview",
    "graphql.enablePullRequestAutoMerge",
  ]),
  merge_pull_request: adapterContract("gh-aw.merge-pull-request", 1, ["pulls.merge"]),
  push_to_pull_request_branch: adapterContract("gh-aw.push-to-pull-request-branch", MAX_EFFECTS, ["git.push", "graphql.createCommitOnBranch"]),
  remove_labels: adapterContract("gh-aw.remove-labels", 32, ["issues.removeLabel"]),
  replace_label: adapterContract("gh-aw.replace-label", 1, ["issues.setLabels"]),
  update_issue: adapterContract("gh-aw.update-issue", 2, ["issues.update", "graphql.updateIssueLabels"]),
});

/** @type {WeakMap<object, {type: string, adapter: any, route: string, state: any, used: boolean}>} */
const capabilities = new WeakMap();
/** @type {WeakMap<object, any>} */
const plans = new WeakMap();
/** @type {WeakSet<Function>} */
const transports = new WeakSet();

class EffectTransportError extends Error {
  /**
   * @param {string} message
   * @param {{transmitted: boolean, reason: string, status?: number, cause?: unknown}} details
   */
  constructor(message, { transmitted, reason, status, cause }) {
    super(message, cause === undefined ? undefined : { cause });
    this.name = "EffectTransportError";
    this.transmitted = transmitted;
    this.reason_code = reason;
    if (status !== undefined) this.status = status;
  }
}

function protocol() {
  return require("./safe_output_authorization.cjs");
}

/** @returns {never} */
function invalid(message) {
  throw new Error(`Safe-output effect plan: ${message}`);
}

function deepFreeze(value) {
  if (value && typeof value === "object") {
    Object.values(value).forEach(deepFreeze);
    Object.freeze(value);
  }
  return value;
}

/** JSON-only snapshot preserving absent, null and empty values faithfully. */
function snapshot(value) {
  if (value === undefined) return undefined;
  // canonicalDigest rejects undefined members, non-plain objects and non-finite numbers.
  protocol().canonicalDigest(value);
  return deepFreeze(structuredClone(value));
}

/**
 * @param {string} type
 * @param {string} route
 */
function selectEffectAdapter(type, route) {
  const adapter = Object.hasOwn(ADAPTERS, type) ? ADAPTERS[type] : null;
  if (!adapter || !adapter.routes.includes(route)) return null;
  return Object.freeze({ id: adapter.id, version: adapter.version, route, max_effects: adapter.max_effects });
}

/** Publishable coverage matrix: output type -> adapter reference, routes and operations. */
function effectAdapterCoverage() {
  return Object.fromEntries(Object.entries(ADAPTERS).map(([type, adapter]) => [type, { adapter: `${adapter.id}@${adapter.version}`, routes: [...adapter.routes], operations: [...adapter.operations] }]));
}

/**
 * Manager-only: mint a one-time preparation capability for a selected adapter
 * route. It is not an authorization decision and permits no writes.
 * @param {{type: string, route: string, state: any}} options
 */
function createEffectPreparation({ type, route, state }) {
  const adapter = Object.hasOwn(ADAPTERS, type) ? ADAPTERS[type] : null;
  if (!adapter || !adapter.routes.includes(route) || !state || typeof state !== "object" || !state.invocation) invalid("unsupported adapter route");
  const capability = Object.freeze({ adapter: `${adapter.id}@${adapter.version}`, route, type });
  capabilities.set(capability, { type, adapter, route, state, used: false });
  return capability;
}

/**
 * @param {unknown} capability
 * @param {string} type
 * @param {any} state
 */
function isEffectPreparation(capability, type, state) {
  const record = capability && typeof capability === "object" ? capabilities.get(capability) : undefined;
  return !!record && record.type === type && record.state === state;
}

/**
 * Adapter entry point. Returns null when the trusted manager did not select
 * resolved preparation for this invocation.
 * @param {string} type
 */
function currentEffectPreparation(type) {
  const state = currentSafeOutputInvocation();
  const capability = state?.effectPreparation;
  if (capability === undefined || capability === null) return null;
  if (!isEffectPreparation(capability, type, state)) invalid("preparation capability is not bound to this invocation");
  return capability;
}

/**
 * Fail closed before any decision when a selected adapter cannot enumerate an
 * invocation's effects. Never a fallback to invocation binding.
 * @param {string} type
 * @param {string} feature
 */
function unsupportedEffect(type, feature) {
  return new Error(`ERR_VALIDATION: ${type} cannot bind ${feature} to an exact effect plan; remove it or disable authorization for this output`);
}

function apiBase() {
  let base;
  try {
    base = new URL(process.env.GITHUB_API_URL || "https://api.github.com");
  } catch {
    invalid("approved API origin is invalid");
  }
  if (base.protocol !== "https:" || base.username || base.password || base.search || base.hash) invalid("approved API origin must be HTTPS without credentials, query or fragment");
  return base;
}

/** Mirrors @octokit/graphql: GHES `/api/v3` maps to `/api/graphql`. */
function graphqlUrl() {
  const base = apiBase();
  const pathname = base.pathname.replace(/\/$/, "");
  return new URL(/\/api\/v3$/.test(pathname) ? pathname.replace(/\/api\/v3$/, "/api/graphql") : `${pathname}/graphql`, base.origin).href;
}

function serverOrigin() {
  try {
    return new URL(process.env.GITHUB_SERVER_URL || "https://github.com").origin;
  } catch {
    invalid("approved server origin is invalid");
  }
}

/** Collect every `{{slot}}` placeholder in a path or JSON body. */
function collectSlots(value, slots = new Set(), depth = 0) {
  if (depth > 32) invalid("effect body is too deep");
  if (typeof value === "string") {
    for (const match of value.matchAll(SLOT_TOKEN)) slots.add(match[1]);
  } else if (value && typeof value === "object") {
    for (const nested of Object.values(value)) collectSlots(nested, slots, depth + 1);
  }
  return slots;
}

/**
 * @param {any} adapter
 * @param {any} effect
 * @param {number} index
 * @param {any[]} earlier
 */
function effectSnapshot(adapter, effect, index, earlier) {
  if (!effect || typeof effect !== "object" || typeof effect.operation !== "string" || !adapter.operations.includes(effect.operation)) invalid("effect operation is not supported by its adapter");
  const operation = OPERATIONS[effect.operation];
  const derive = effect.derive === undefined ? {} : effect.derive;
  if (!derive || typeof derive !== "object" || Array.isArray(derive)) invalid("effect derivations are malformed");
  for (const [slot, source] of Object.entries(derive)) {
    if (!SLOT.test(slot) || !source || !Number.isSafeInteger(source.effect) || source.effect < 0 || source.effect >= index || typeof source.field !== "string") invalid("effect derivation must reference an earlier effect");
    if (!Object.hasOwn(OPERATIONS[earlier[source.effect].operation].receipt, source.field)) invalid("effect derivation references an undeclared receipt field");
  }
  /** @type {Record<string, any>} */
  let request;
  if (operation.kind === "git") {
    const git = effect.git;
    if (!git || typeof git !== "object" || effect.path !== undefined || effect.body !== undefined) invalid("git effect is malformed");
    let remote;
    try {
      remote = new URL(git.remote);
    } catch {
      invalid("git remote is invalid");
    }
    if (remote.protocol !== "https:" || remote.origin !== serverOrigin() || remote.username || remote.password || remote.search || remote.hash || !/^\/[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+?(?:\.git)?$/.test(remote.pathname)) {
      invalid("git remote must be a credential-free repository URL on the approved server");
    }
    if (typeof git.ref !== "string" || !REF.test(git.ref)) invalid("git ref is invalid");
    if (!(git.old === null || OID.test(git.old)) || !OID.test(git.new) || git.old === git.new) invalid("git ref transition is invalid");
    request = { kind: "git", remote: remote.href, ref: git.ref, old: git.old, new: git.new };
  } else if (operation.kind === "graphql") {
    if (effect.path !== undefined || !effect.variables || typeof effect.variables !== "object" || Array.isArray(effect.variables)) invalid("GraphQL effect requires variables only");
    if (Object.keys(effect.variables).some(key => ["method", "baseUrl", "url", "headers", "request", "query", "mediaType", "operationName"].includes(key))) invalid("GraphQL variables cannot use transport option names");
    request = {
      kind: "graphql",
      method: "POST",
      url: graphqlUrl(),
      headers: { accept: ACCEPT, "x-github-api-version": require("./constants.cjs").GITHUB_API_VERSION, "content-type": JSON_CONTENT_TYPE, ...(operation.headers || {}) },
      body: { query: operation.query, variables: snapshot(effect.variables) },
    };
  } else {
    if (typeof effect.path !== "string" || !new RegExp(operation.path).test(effect.path) || /[?#\\]|\/\.\.?(?:\/|$)/.test(effect.path)) invalid("effect path does not match its operation");
    if (operation.body !== (effect.body !== undefined)) invalid("effect body does not match its operation");
    if (effect.body !== undefined && (!effect.body || typeof effect.body !== "object" || Array.isArray(effect.body))) invalid("effect body must be a JSON object");
    const base = apiBase();
    let url;
    try {
      url = new URL(`${base.pathname.replace(/\/$/, "")}${effect.path}`, base.origin);
    } catch {
      invalid("effect request URL is invalid");
    }
    if (url.origin !== base.origin || url.search || url.hash) invalid("effect request escapes the approved API origin");
    const body = snapshot(effect.body);
    request = {
      kind: "rest",
      method: operation.method,
      url: url.href,
      origin: url.origin,
      path: url.pathname.replace(/%7B%7B([a-z][a-z0-9_]{0,31})%7D%7D/g, "{{$1}}"),
      query: "",
      headers: { accept: ACCEPT, "x-github-api-version": require("./constants.cjs").GITHUB_API_VERSION, ...(body === undefined ? {} : { "content-type": JSON_CONTENT_TYPE }) },
      ...(body === undefined ? {} : { body }),
    };
  }
  const used = collectSlots(operation.kind === "rest" ? [request.path, request.body ?? null] : (request.body ?? null));
  if (operation.kind === "git") collectSlots([request.old, request.new], used);
  if ([...used].some(slot => !Object.hasOwn(derive, slot)) || Object.keys(derive).some(slot => !used.has(slot))) invalid("effect placeholders must match its derivations exactly");
  if (operation.kind === "rest" && collectSlots(request.body ?? null).size && !isWholeSlotValues(request.body)) invalid("derived body values must be whole placeholders");
  return { index, operation: effect.operation, request, ...(Object.keys(derive).length ? { derive: Object.fromEntries(Object.entries(derive).map(([slot, source]) => [slot, { effect: source.effect, field: source.field }])) } : {}) };
}

/** Body placeholders must be whole string values so they can carry typed receipts. */
function isWholeSlotValues(value) {
  if (typeof value === "string") return !value.includes("{{") || /^\{\{[a-z][a-z0-9_]{0,31}\}\}$/.test(value);
  if (value && typeof value === "object") return Object.values(value).every(isWholeSlotValues);
  return true;
}

function target(value) {
  if (!value || typeof value !== "object" || !["repository", "pull_request", "issue", "label", "ref"].includes(value.kind)) invalid("target kind is unsupported");
  if (typeof value.repository !== "string" || !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(value.repository)) invalid("target repository locator is invalid");
  if (typeof value.repository_id !== "string" || !NUMERIC_ID.test(value.repository_id)) invalid("target repository identity is unresolved");
  const result = { kind: value.kind, repository: value.repository, repository_id: value.repository_id };
  if (value.kind === "ref") {
    if (typeof value.ref !== "string" || !REF.test(value.ref) || !(value.oid === null || OID.test(value.oid))) invalid("target ref is invalid");
    return { ...result, ref: value.ref, oid: value.oid };
  }
  if (value.kind !== "repository") {
    if (value.kind !== "label" && (!Number.isSafeInteger(value.number) || value.number < 1)) invalid("target number is invalid");
    if (typeof value.resource_id !== "string" || !NUMERIC_ID.test(value.resource_id)) invalid("target resource identity is unresolved");
    if (value.number !== undefined) result.number = value.number;
    result.resource_id = value.resource_id;
  }
  if (value.node_id !== undefined) {
    if (typeof value.node_id !== "string" || !NODE.test(value.node_id)) invalid("target node identity is invalid");
    result.node_id = value.node_id;
  }
  return result;
}

function targetReference(value) {
  if (value.kind === "repository") return `effect-target:repository:${value.repository_id}`;
  if (value.kind === "label") return `effect-target:label:${value.repository_id}@${value.resource_id}`;
  if (value.kind === "ref") return `effect-target:ref:${value.repository_id}@${require("node:crypto").createHash("sha256").update(value.ref).digest("hex").slice(0, 32)}@${value.oid ?? "absent"}`;
  return `effect-target:${value.kind}:${value.repository_id}#${value.number}@${value.resource_id}`;
}

/**
 * Freeze the complete effect plan. Callers must apply defaults, sanitization,
 * temporary-ID substitution, filtering and deduplication before this call.
 * @param {object} capability
 * @param {{targets: any[], effects: any[], preconditions: Array<{id: string, enforcement: "atomic"|"observed", observation: unknown}>}} spec
 * @returns {EffectPlanHandle}
 */
function freezeEffectPlan(capability, { targets, effects, preconditions }) {
  const record = capability && typeof capability === "object" ? capabilities.get(capability) : undefined;
  if (!record || record.state !== currentSafeOutputInvocation()) invalid("plan requires a trusted preparation capability for this invocation");
  if (record.used) invalid("preparation capability was already consumed");
  record.used = true;
  const { adapter, state, route } = record;
  if (!Array.isArray(effects) || effects.length < 1 || effects.length > adapter.max_effects) invalid("effect count is outside the adapter bound");
  if (!Array.isArray(targets) || targets.length < 1 || targets.length > MAX_TARGETS) invalid("target count is outside the contract bound");
  if (!Array.isArray(preconditions) || preconditions.length > MAX_PRECONDITIONS) invalid("precondition count is outside the contract bound");
  const ids = new Set();
  const conditions = preconditions.map(condition => {
    if (!condition || typeof condition.id !== "string" || !CODE.test(condition.id) || ids.has(condition.id) || !["atomic", "observed"].includes(condition.enforcement)) invalid("precondition is malformed");
    ids.add(condition.id);
    return { id: condition.id, enforcement: condition.enforcement, digest: protocol().canonicalDigest(JSON.parse(JSON.stringify(condition.observation ?? null))) };
  });
  const output = state.invocation.output;
  const context = state.invocation.context || {};
  const limits = { max_effects: adapter.max_effects, effect_count: effects.length, freshness: "per_send", on_drift: "stop", on_expiry: "stop_remaining", on_partial: "preserve_receipts_no_retry" };
  const plan = deepFreeze({
    contract: CONTRACT,
    adapter: { id: adapter.id, version: adapter.version, route, implementation_digest: output.implementation_digest },
    invocation: {
      output_type: output.type,
      output_name: output.name,
      executor: output.executor,
      batch_digest: state.batchDigest,
      record_index: state.recordIndex,
      ...(context.policy_digest === undefined ? {} : { policy_digest: context.policy_digest }),
      ...(context.claim_id === undefined ? {} : { claim_id: context.claim_id }),
      ...(context.delivery_id === undefined ? {} : { delivery_id: context.delivery_id }),
    },
    targets: targets.map(target),
    effects: effects.map((effect, index) => effectSnapshot(adapter, effect, index, effects)),
    preconditions: conditions,
    limits,
  });
  const digest = protocol().canonicalDigest(plan);
  const refs = [`effect-contract:${CONTRACT}`, `effect-adapter:${adapter.id}@${adapter.version}`, `effect-route:${route}`, `effect-plan:sha256:${digest}`, `effect-count:${plan.effects.length}`];
  for (const operation of new Set(plan.effects.map(effect => effect.operation))) refs.push(`effect-operation:${operation}`);
  if (plan.effects.some(effect => effect.derive)) refs.push("effect-derived:receipt");
  for (const value of plan.targets) refs.push(targetReference(value));
  for (const condition of plan.preconditions) refs.push(`effect-precondition:${condition.id}:${condition.enforcement}`);
  if (new Set(refs).size !== refs.length || refs.length > 64) invalid("plan references exceed the envelope bound");
  const handle = Object.freeze({ contract: CONTRACT, adapter: `${adapter.id}@${adapter.version}`, route, plan_digest: digest, effect_count: plan.effects.length });
  plans.set(handle, {
    capability,
    state,
    plan,
    digest,
    scope: deepFreeze({ binding: "resolved_effect", refs, limits_digest: protocol().canonicalDigest(limits) }),
    requested: false,
    authorization: null,
    next: 0,
    effects: plan.effects.map(() => ({ status: "not_attempted", transmitted: false, receipt: null })),
  });
  return handle;
}

function planRecord(handle) {
  const record = handle && typeof handle === "object" ? plans.get(handle) : undefined;
  if (!record) invalid("plan handle is not registered by a trusted adapter");
  return record;
}

/** Read-only, non-secret metadata projection. */
function describeEffectPlan(handle) {
  const record = planRecord(handle);
  return deepFreeze({
    contract: CONTRACT,
    adapter: handle.adapter,
    route: handle.route,
    plan_digest: record.digest,
    limits_digest: record.scope.limits_digest,
    refs: [...record.scope.refs],
    effects: record.effects.map((effect, index) => ({ index, operation: record.plan.effects[index].operation, status: effect.status })),
    preconditions: record.plan.preconditions.map(condition => ({ id: condition.id, enforcement: condition.enforcement })),
  });
}

/**
 * Gate-only: return the immutable plan and scope for one authorization request.
 * @param {object} handle
 * @param {any} state
 */
function beginEffectPlanAuthorization(handle, state) {
  const record = planRecord(handle);
  if (record.state !== state || state.effectPreparation !== record.capability) invalid("plan is not bound to the current invocation");
  if (record.requested) invalid("plan authorization was already requested");
  record.requested = true;
  return { plan: record.plan, scope: record.scope };
}

/**
 * Protocol-only: a resolved_effect envelope must be exactly a registered plan.
 * @param {unknown} handle
 * @param {unknown} normalizedInputs
 * @param {any} scope
 */
function assertEffectPlanScope(handle, normalizedInputs, scope) {
  const record = planRecord(handle);
  if (!record.requested || record.authorization || normalizedInputs !== record.plan || scope !== record.scope) invalid("resolved-effect binding requires its registered plan");
}

/**
 * @param {object} handle
 * @param {any} authorization
 * @param {any} invocation
 */
function bindEffectPlanAuthorization(handle, authorization, invocation) {
  const record = planRecord(handle);
  if (!record.requested || record.authorization) invalid("plan authorization binding is not pending");
  if (authorization?.state !== "allowed" || invocation?.scope?.binding !== "resolved_effect" || invocation.input_digest !== record.digest) invalid("plan has no resolved-effect permission");
  if (authorization.binding !== undefined && authorization.binding !== "resolved_effect") invalid("decision binding does not match its plan");
  if (authorization.invocation_id !== undefined && authorization.invocation_id !== invocation.invocation_id) invalid("decision invocation does not match its plan");
  record.authorization = authorization;
  return authorization;
}

/** @param {any} client */
function nativeFetch(client) {
  const claim = require("./work_queue_effect_client.cjs").claimClientNativeFetch(client);
  if (claim !== undefined) return typeof claim === "function" ? claim : globalThis.fetch;
  const fetch = client?.request?.endpoint?.DEFAULTS?.request?.fetch;
  return typeof fetch === "function" ? fetch : globalThis.fetch;
}

/** @param {Function} value */
function isEffectTransport(value) {
  return typeof value === "function" && transports.has(value);
}

function headerSnapshot(headers) {
  /** @type {Record<string, string>} */
  const result = {};
  const entries = typeof Headers === "function" && headers instanceof Headers ? [...headers.entries()] : Object.entries(headers || {});
  for (const [name, value] of entries) {
    const key = String(name).toLowerCase();
    if (Object.hasOwn(result, key)) throw new Error("duplicate header");
    result[key] = String(value);
  }
  return result;
}

/**
 * Refuse any difference between the final SDK request and the frozen request.
 * @param {any} expected
 * @param {unknown} url
 * @param {any} init
 */
function verifyWireRequest(expected, url, init) {
  const { canonicalDigest } = protocol();
  if (String(url) !== expected.url || String(init?.method || "GET").toUpperCase() !== expected.method) throw new Error("method or target changed");
  const headers = headerSnapshot(init?.headers);
  const rawBody = init?.body;
  const emptyBody = rawBody === undefined || rawBody === null || rawBody === "";
  for (const [name, value] of Object.entries(headers)) {
    if (TRANSPORT_HEADERS.has(name)) continue;
    // Octokit marks an empty forwarded body as JSON; with no body it has no effect.
    if (name === "content-type" && expected.body === undefined && emptyBody) continue;
    if (expected.headers[name] !== value) throw new Error("semantic header changed");
  }
  for (const [name, value] of Object.entries(expected.headers)) {
    if (headers[name] !== value) throw new Error("semantic header removed");
  }
  if (expected.body === undefined) {
    if (!emptyBody) throw new Error("body added");
    return;
  }
  if (typeof rawBody !== "string") throw new Error("body is not resolved JSON");
  const { parseStrictJSON } = require("./work_queue_codec.cjs");
  if (canonicalDigest(parseStrictJSON(rawBody)) !== canonicalDigest(expected.body)) throw new Error("body changed");
}

/** @param {any} value @param {string} pointer */
function readPointer(value, pointer) {
  return pointer.split(".").reduce((current, key) => (current && typeof current === "object" && Object.hasOwn(current, key) ? current[key] : undefined), value);
}

/** Typed, schema-limited receipt projection; secrets and bodies are never retained. */
function extractReceipt(operationId, data) {
  /** @type {Record<string, unknown>} */
  const receipt = {};
  for (const [field, [pointer, type]] of Object.entries(OPERATIONS[operationId].receipt)) {
    const value = readPointer(data, pointer);
    if (RECEIPT_TYPES[type](value)) receipt[field] = value;
  }
  return Object.freeze(receipt);
}

/** Replace derived placeholders with typed receipts from confirmed earlier effects. */
function resolveRequest(record, index) {
  const effect = record.plan.effects[index];
  if (!effect.derive) return effect.request;
  /** @type {Record<string, unknown>} */
  const values = {};
  for (const [slot, source] of Object.entries(effect.derive)) {
    const earlier = record.effects[source.effect];
    const value = earlier.status === "confirmed" || earlier.status === "absent" ? earlier.receipt?.[source.field] : undefined;
    if (value === undefined) throw new Error("derived receipt is unavailable");
    values[slot] = value;
  }
  const substitute = item => {
    if (typeof item === "string") {
      const whole = item.match(/^\{\{([a-z][a-z0-9_]{0,31})\}\}$/);
      if (whole) return values[whole[1]];
      return item.replace(SLOT_TOKEN, (_match, slot) => String(values[slot]));
    }
    if (Array.isArray(item)) return item.map(substitute);
    if (item && typeof item === "object") return Object.fromEntries(Object.entries(item).map(([key, nested]) => [key, substitute(nested)]));
    return item;
  };
  const resolved = substitute(effect.request);
  if (resolved.kind === "rest") {
    const base = apiBase();
    const url = new URL(`${base.pathname.replace(/\/$/, "")}${resolved.path}`, base.origin);
    if (url.origin !== base.origin || url.search || url.hash) throw new Error("derived request escapes the approved API origin");
    resolved.url = url.href;
  }
  return deepFreeze(resolved);
}

/**
 * Run a fixed git argv; credentials come only from the supplied environment or
 * credential wrapper, never from argv.
 * @param {string[]} args
 * @param {{cwd?: string, env?: Record<string, string>}} options
 */
async function runGit(args, options) {
  /** @type {any} */
  const runtime = global; // `exec` is the github-script global.
  return runtime.exec.getExecOutput("git", args, { cwd: options.cwd, env: { ...process.env, ...(options.env || {}) }, ignoreReturnCode: true, silent: true });
}

async function remoteOid(request, git) {
  const result = await runGit(["ls-remote", "--refs", "--", request.remote, request.ref], git);
  if (result.exitCode !== 0) return undefined;
  const line = String(result.stdout || "")
    .split("\n")
    .find(entry => entry.endsWith(`\t${request.ref}`));
  if (!line) return null;
  const oid = line.split("\t")[0];
  return OID.test(oid) ? oid : undefined;
}

/**
 * The adapter-owned git transport: verify no URL rewriting, push exactly
 * `new:ref` under a lease on `old`, then read back the remote ref.
 * @returns {Promise<{status: SettledStatus, receipt: Record<string, unknown>}>}
 */
/**
 * @param {SettledStatus} status
 * @param {Record<string, unknown>} receipt
 */
function settled(status, receipt) {
  return { status, receipt };
}

async function sendGitEffect(request, git, onTransmit) {
  const alias = `gh-aw-effect-${require("node:crypto").randomUUID()}`;
  const resolved = await runGit(["-c", `remote.${alias}.url=${request.remote}`, "remote", "-v"], git);
  const pushUrls = String(resolved.stdout || "")
    .split("\n")
    .filter(line => line.startsWith(`${alias}\t`) && line.endsWith(" (push)"))
    .map(line => line.slice(alias.length + 1, -" (push)".length));
  if (resolved.exitCode !== 0 || pushUrls.length !== 1 || pushUrls[0] !== request.remote) throw new Error("git remote is rewritten by configuration");
  if (typeof git.authorize === "function") await git.authorize(request);
  // Disable inherited configuration that could push tags or submodules beyond the approved ref.
  const argv = [
    "-c",
    "core.hooksPath=/dev/null",
    "push",
    "--porcelain",
    "--no-verify",
    "--no-follow-tags",
    "--recurse-submodules=no",
    `--force-with-lease=${request.ref}:${request.old ?? ""}`,
    "--",
    request.remote,
    `${request.new}:${request.ref}`,
  ];
  const run = async () => {
    onTransmit();
    const pushed = await runGit(argv, git);
    const observed = await remoteOid(request, git);
    if (observed === request.new) return settled("confirmed", Object.freeze({ oid: request.new }));
    if (pushed.exitCode !== 0 && observed !== undefined && observed === request.old) return settled("rejected", Object.freeze({}));
    return settled("indeterminate", Object.freeze({}));
  };
  return typeof git.withCredentials === "function" ? git.withCredentials(run) : run();
}

/**
 * Consume exactly the next approved effect through the adapter-owned transport.
 * Returns the SDK response (REST), response data (GraphQL) or `{oid}` (git) for
 * a confirmed effect. Throws EffectTransportError with `transmitted` and the
 * observed HTTP status otherwise.
 * @param {object} handle
 * @param {number} index
 * @param {{client?: any, git?: {cwd?: string, env?: Record<string, string>, withCredentials?: (run: () => Promise<any>) => Promise<any>, authorize?: (request: any) => Promise<void>}}} options
 */
async function sendPlannedEffect(handle, index, { client, git } = {}) {
  const record = planRecord(handle);
  const effect = record.effects[index];
  const blocked = (reason, cause = undefined) => {
    if (effect && effect.status === "pending") effect.status = "blocked";
    return new EffectTransportError(`Safe-output effect was not sent: ${reason}`, { transmitted: false, reason, cause });
  };
  if (!effect || !Number.isSafeInteger(index) || index !== record.next) throw blocked("effect_out_of_order");
  if (!record.authorization || record.state !== currentSafeOutputInvocation()) throw blocked("plan_not_authorized");
  record.next++;
  effect.status = "pending";
  const planned = record.plan.effects[index];
  const operationId = planned.operation;
  const fresh = () => protocol().assertAuthorizationFresh({ authorization: record.authorization });
  let expected;
  try {
    fresh();
    const invocation = record.plan.invocation;
    if (invocation.claim_id !== undefined) {
      const scope = require("./work_queue_claim_scope.cjs");
      const claimHandle = scope.currentClaimHandle();
      const identity = claimHandle ? scope.claimIdentity(claimHandle) : null;
      if (!identity || identity.claim_id !== invocation.claim_id || identity.dispatch_id !== invocation.delivery_id) throw new Error("Claim identity changed");
      scope.assertClaimIdentity(identity);
    }
    expected = resolveRequest(record, index);
  } catch (error) {
    const reason = error?.reason_code === "invalid_validity" || error?.reason_code === "evidence_failed" ? "authorization_expired" : error?.message === "derived receipt is unavailable" ? "receipt_unavailable" : "precondition_changed";
    throw blocked(reason, error);
  }

  if (expected.kind === "git") {
    if (!git) throw blocked("transport_unavailable");
    try {
      const { status, receipt } = await sendGitEffect(expected, git, () => {
        fresh();
        effect.transmitted = true;
      });
      effect.status = status;
      effect.receipt = receipt;
      if (status === "confirmed") return receipt;
      throw new EffectTransportError("Safe-output git effect did not confirm its ref transition", { transmitted: true, reason: status });
    } catch (error) {
      if (error instanceof EffectTransportError) throw error;
      if (!effect.transmitted) throw blocked(error?.reason_code === "invalid_validity" || error?.reason_code === "evidence_failed" ? "authorization_expired" : "transport_unavailable", error);
      effect.status = "indeterminate";
      throw new EffectTransportError("Safe-output git effect outcome is indeterminate", { transmitted: true, reason: "indeterminate", cause: error });
    }
  }

  if (!client) throw blocked("transport_unavailable");
  const original = nativeFetch(client);
  let attempts = 0;
  /** @type {number|undefined} */
  let observedStatus;
  /** @type {EffectTransportError|undefined} */
  let refused;
  const transport = async (url, init) => {
    if (attempts++ > 0) throw new Error("Safe-output effect transport refuses to replay an approved request");
    try {
      verifyWireRequest(expected, url, init);
      fresh();
    } catch (error) {
      refused = blocked(error?.reason_code === "invalid_validity" || error?.reason_code === "evidence_failed" ? "authorization_expired" : "request_changed", error);
      throw refused;
    }
    effect.transmitted = true;
    const response = await original(url, { ...init, redirect: "manual" });
    observedStatus = response.status;
    if (response.status >= 300 && response.status < 400) {
      void response.body?.cancel?.().catch(() => {
        // The redirect is refused either way; releasing the body is best effort.
      });
      throw new Error("Safe-output effect transport refused a redirect");
    }
    return response;
  };
  transports.add(transport);
  try {
    let result;
    if (expected.kind === "graphql") {
      if (typeof client.graphql !== "function") throw blocked("transport_unavailable");
      result = await client.graphql(expected.body.query, { ...expected.body.variables, headers: { ...expected.headers }, request: { fetch: transport, redirect: "manual", retries: 0 } });
      effect.receipt = extractReceipt(operationId, result);
    } else {
      result = await client.request({
        method: expected.method,
        url: expected.url,
        headers: { ...expected.headers },
        ...(expected.body === undefined ? {} : { data: JSON.stringify(expected.body) }),
        request: { fetch: transport, redirect: "manual", retries: 0 },
      });
      effect.receipt = extractReceipt(operationId, result?.data);
    }
    // A client that ignored the verifying transport may have sent an unverified
    // request: never confirm it, and never treat it as unsent.
    if (attempts !== 1 || !effect.transmitted) {
      effect.transmitted = true;
      effect.status = "indeterminate";
      effect.receipt = null;
      throw new EffectTransportError("Safe-output effect client bypassed the verifying transport", { transmitted: true, reason: "indeterminate" });
    }
    effect.status = "confirmed";
    return result;
  } catch (error) {
    if (error instanceof EffectTransportError && error.transmitted) throw error;
    if (!effect.transmitted) {
      if (refused) throw refused;
      if (error instanceof EffectTransportError) throw error;
      throw blocked("transport_unavailable", error);
    }
    // GraphQL errors arrive with HTTP 200 and may follow a partial mutation.
    effect.status = expected.kind === "rest" && observedStatus !== undefined && observedStatus >= 400 && observedStatus < 500 ? "rejected" : "indeterminate";
    throw new EffectTransportError("Safe-output effect response did not confirm success", { transmitted: true, reason: effect.status, status: observedStatus, cause: error });
  } finally {
    transports.delete(transport);
  }
}

/** @param {object} handle @param {number} index */
function effectTransmitted(handle, index) {
  return planRecord(handle).effects[index]?.transmitted === true;
}

/** @param {object} handle @param {number} index */
function effectReceipt(handle, index) {
  const effect = planRecord(handle).effects[index];
  return effect && (effect.status === "confirmed" || effect.status === "absent") ? effect.receipt : null;
}

/**
 * Settle an uncertain or rejected effect after adapter-owned read-only reconciliation.
 * A reconciled receipt is validated against the operation's receipt schema.
 * @param {object} handle
 * @param {number} index
 * @param {SettledStatus} status
 * @param {Record<string, unknown>} [receipt]
 */
function settleEffect(handle, index, status, receipt = undefined) {
  const record = planRecord(handle);
  const effect = record.effects[index];
  // A 2xx with a malformed body is already confirmed; read-back agreement is idempotent.
  if (effect?.status === "confirmed" && status === "confirmed") return;
  if (!effect?.transmitted || !["rejected", "indeterminate"].includes(effect.status) || !["confirmed", "rejected", "indeterminate", "absent"].includes(status)) invalid("only transmitted unconfirmed effects can be reconciled");
  effect.status = status;
  if (receipt !== undefined) effect.receipt = extractReceipt(record.plan.effects[index].operation, receipt);
}

/**
 * Aggregate execution outcome using the repository's operation taxonomy.
 * @param {object} handle
 * @returns {"confirmed"|"partial"|"failed"|"indeterminate"}
 */
function effectPlanOutcome(handle) {
  const statuses = planRecord(handle).effects.map(effect => effect.status);
  if (statuses.some(status => status === "indeterminate" || status === "pending")) return "indeterminate";
  const applied = statuses.filter(status => status === "confirmed" || status === "absent").length;
  if (applied === statuses.length) return "confirmed";
  return applied > 0 ? "partial" : "failed";
}

/**
 * Generic sequential runner. `beforeEach` revalidates observed preconditions
 * before every send (throw to stop). `reconcile` performs read-only
 * reconciliation of a transmitted, unconfirmed effect. Any non-confirmed effect
 * stops the remaining effects; nothing is retried.
 * @param {object} handle
 * @param {{client?: any | ((index: number) => any), git?: any, beforeEach?: (index: number) => Promise<void> | void,
 *   reconcile?: (index: number, error: EffectTransportError) => Promise<{status: SettledStatus, receipt?: Record<string, unknown>} | null>,
 *   onConfirmed?: (index: number, result: any) => void}} [options]
 */
async function executeEffectPlan(handle, { client, git, beforeEach, reconcile, onConfirmed } = {}) {
  const record = planRecord(handle);
  let stop_reason = "";
  for (let index = record.next; index < record.effects.length; index++) {
    try {
      await beforeEach?.(index);
    } catch {
      stop_reason = "precondition_changed";
      break;
    }
    try {
      const result = await sendPlannedEffect(handle, index, { client: typeof client === "function" && !client.request ? client(index) : client, git });
      onConfirmed?.(index, result);
    } catch (error) {
      if (!(error instanceof EffectTransportError)) throw error;
      if (!error.transmitted) {
        stop_reason = error.reason_code;
        break;
      }
      /** @type {{status: SettledStatus, receipt?: Record<string, unknown>} | null} */
      let reconciled = null;
      try {
        reconciled = reconcile ? await reconcile(index, error) : null;
      } catch {
        reconciled = null;
      }
      if (reconciled) settleEffect(handle, index, reconciled.status, reconciled.receipt);
      const status = record.effects[index].status;
      if (status !== "confirmed" && status !== "absent") {
        stop_reason = status;
        break;
      }
    }
  }
  return { outcome: effectPlanOutcome(handle), stop_reason, effects: describeEffectPlan(handle).effects };
}

module.exports = {
  CONTRACT,
  MAX_EFFECTS,
  GRAPHQL_MUTATIONS,
  EffectTransportError,
  selectEffectAdapter,
  effectAdapterCoverage,
  createEffectPreparation,
  isEffectPreparation,
  currentEffectPreparation,
  unsupportedEffect,
  freezeEffectPlan,
  describeEffectPlan,
  beginEffectPlanAuthorization,
  assertEffectPlanScope,
  bindEffectPlanAuthorization,
  isEffectTransport,
  sendPlannedEffect,
  effectTransmitted,
  effectReceipt,
  settleEffect,
  effectPlanOutcome,
  executeEffectPlan,
};
