// @ts-check
"use strict";

/**
 * Trusted effect-preparation adapters for resolved-effect safe-output authorization.
 *
 * A compiler/runtime-owned adapter prepares a finite, ordered list of outbound
 * GitHub API requests, freezes them into a private plan and obtains exactly one
 * `resolved_effect` decision for that plan. Execution can only consume the
 * frozen requests, in order, once each, through a transport that verifies the
 * final wire request immediately before network I/O.
 *
 * The plan never leaves this process: the authority receives its canonical
 * digest and bounded opaque references through the existing v1 envelope.
 */

const { currentSafeOutputInvocation } = require("./safe_output_execution.cjs");

/** @typedef {Readonly<{contract: string, adapter: string, route: string, plan_digest: string, effect_count: number}>} EffectPlanHandle */

const CONTRACT = "gh-aw.safe-output-effect-plan.v1";
const MAX_EFFECTS = 32;
const MAX_TARGETS = 40;
const MAX_PRECONDITIONS = 8;
const CODE = /^[a-z][a-z0-9_.]{0,63}$/;
const NUMERIC_ID = /^[1-9][0-9]{0,19}$/;
const ACCEPT = "application/vnd.github+json";
const JSON_CONTENT_TYPE = "application/json; charset=utf-8";
// Credentials and client identification are transport concerns. Changing them
// cannot alter the method, target, body or semantic headers checked below.
const TRANSPORT_HEADERS = new Set(["authorization", "user-agent"]);

/**
 * Coverage is per output type, execution route and contract version. Routes not
 * listed here (prepared/script Claim adapters, standalone, workflow, action,
 * job, script and synthetic phases) remain invocation-bound.
 */
const ADAPTERS = Object.freeze({
  merge_pull_request: Object.freeze({
    id: "gh-aw.merge-pull-request",
    version: 1,
    routes: Object.freeze(["manager", "claim"]),
    max_effects: 1,
    operations: Object.freeze({ "pulls.merge": Object.freeze({ method: "PUT", path: "^/repos/[^/]+/[^/]+/pulls/[1-9][0-9]*/merge$", body: true }) }),
  }),
  remove_labels: Object.freeze({
    id: "gh-aw.remove-labels",
    version: 1,
    routes: Object.freeze(["manager", "claim"]),
    max_effects: MAX_EFFECTS,
    operations: Object.freeze({ "issues.removeLabel": Object.freeze({ method: "DELETE", path: "^/repos/[^/]+/[^/]+/issues/[1-9][0-9]*/labels/[^/]+$", body: false }) }),
  }),
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

/** Publishable coverage matrix: output type -> adapter reference and supported routes. */
function effectAdapterCoverage() {
  return Object.fromEntries(Object.entries(ADAPTERS).map(([type, adapter]) => [type, { adapter: `${adapter.id}@${adapter.version}`, routes: [...adapter.routes] }]));
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

/**
 * @param {any} adapter
 * @param {any} effect
 * @param {number} index
 */
function requestSnapshot(adapter, effect, index) {
  if (!effect || typeof effect !== "object" || typeof effect.operation !== "string" || !Object.hasOwn(adapter.operations, effect.operation)) invalid("effect operation is not supported by its adapter");
  const operation = adapter.operations[effect.operation];
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
  return {
    index,
    operation: effect.operation,
    request: {
      method: operation.method,
      url: url.href,
      origin: url.origin,
      path: url.pathname,
      query: "",
      headers: { accept: ACCEPT, "x-github-api-version": require("./constants.cjs").GITHUB_API_VERSION, ...(body === undefined ? {} : { "content-type": JSON_CONTENT_TYPE }) },
      ...(body === undefined ? {} : { body }),
    },
  };
}

function target(value) {
  if (!value || typeof value !== "object" || !["repository", "pull_request", "issue", "label"].includes(value.kind)) invalid("target kind is unsupported");
  if (typeof value.repository !== "string" || !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(value.repository)) invalid("target repository locator is invalid");
  if (typeof value.repository_id !== "string" || !NUMERIC_ID.test(value.repository_id)) invalid("target repository identity is unresolved");
  const result = { kind: value.kind, repository: value.repository, repository_id: value.repository_id };
  if (value.kind !== "repository") {
    if (value.kind !== "label" && (!Number.isSafeInteger(value.number) || value.number < 1)) invalid("target number is invalid");
    if (typeof value.resource_id !== "string" || !NUMERIC_ID.test(value.resource_id)) invalid("target resource identity is unresolved");
    if (value.number !== undefined) result.number = value.number;
    result.resource_id = value.resource_id;
  }
  if (value.node_id !== undefined) {
    if (typeof value.node_id !== "string" || !/^[A-Za-z0-9_=-]{1,128}$/.test(value.node_id)) invalid("target node identity is invalid");
    result.node_id = value.node_id;
  }
  return result;
}

function targetReference(value) {
  if (value.kind === "repository") return `effect-target:repository:${value.repository_id}`;
  if (value.kind === "label") return `effect-target:label:${value.repository_id}@${value.resource_id}`;
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
    effects: effects.map((effect, index) => requestSnapshot(adapter, effect, index)),
    preconditions: conditions,
    limits,
  });
  const digest = protocol().canonicalDigest(plan);
  const refs = [`effect-contract:${CONTRACT}`, `effect-adapter:${adapter.id}@${adapter.version}`, `effect-route:${route}`, `effect-plan:sha256:${digest}`, `effect-count:${plan.effects.length}`];
  for (const operation of new Set(plan.effects.map(effect => effect.operation))) refs.push(`effect-operation:${operation}`);
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
    effects: plan.effects.map(() => ({ status: "not_attempted", transmitted: false })),
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

/**
 * Consume exactly the next approved effect through the adapter-owned transport.
 * Returns the SDK response for a confirmed (2xx) request. Throws
 * EffectTransportError with `transmitted` and the observed HTTP status otherwise.
 * @param {object} handle
 * @param {number} index
 * @param {{client: any}} options
 */
async function sendPlannedEffect(handle, index, { client }) {
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
  const expected = record.plan.effects[index].request;
  const fresh = () => protocol().assertAuthorizationFresh({ authorization: record.authorization });
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
  } catch (error) {
    throw blocked(error?.reason_code === "invalid_validity" || error?.reason_code === "evidence_failed" ? "authorization_expired" : "precondition_changed", error);
  }
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
    const response = await client.request({
      method: expected.method,
      url: expected.url,
      headers: { ...expected.headers },
      ...(expected.body === undefined ? {} : { data: JSON.stringify(expected.body) }),
      request: { fetch: transport, redirect: "manual", retries: 0 },
    });
    effect.status = "confirmed";
    return response;
  } catch (error) {
    if (!effect.transmitted) {
      if (refused) throw refused;
      throw blocked("transport_unavailable", error);
    }
    effect.status = observedStatus !== undefined && observedStatus >= 400 && observedStatus < 500 ? "rejected" : "indeterminate";
    throw new EffectTransportError("Safe-output effect response did not confirm success", { transmitted: true, reason: effect.status, status: observedStatus, cause: error });
  } finally {
    transports.delete(transport);
  }
}

/** @param {object} handle @param {number} index */
function effectTransmitted(handle, index) {
  return planRecord(handle).effects[index]?.transmitted === true;
}

/**
 * Settle an uncertain or rejected effect after adapter-owned read-only reconciliation.
 * @param {object} handle
 * @param {number} index
 * @param {"confirmed"|"rejected"|"indeterminate"|"absent"} status
 */
function settleEffect(handle, index, status) {
  const effect = planRecord(handle).effects[index];
  // A 2xx with a malformed body is already confirmed; read-back agreement is idempotent.
  if (effect?.status === "confirmed" && status === "confirmed") return;
  if (!effect?.transmitted || !["rejected", "indeterminate"].includes(effect.status) || !["confirmed", "rejected", "indeterminate", "absent"].includes(status)) invalid("only transmitted unconfirmed effects can be reconciled");
  effect.status = status;
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

module.exports = {
  CONTRACT,
  MAX_EFFECTS,
  EffectTransportError,
  selectEffectAdapter,
  effectAdapterCoverage,
  createEffectPreparation,
  isEffectPreparation,
  currentEffectPreparation,
  freezeEffectPlan,
  describeEffectPlan,
  beginEffectPlanAuthorization,
  assertEffectPlanScope,
  bindEffectPlanAuthorization,
  isEffectTransport,
  sendPlannedEffect,
  effectTransmitted,
  settleEffect,
  effectPlanOutcome,
};
