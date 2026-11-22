// @ts-check
/// <reference types="@actions/github-script" />

const routes = require("./request_continuation_routes.cjs");
const { currentClaimAssignment, currentClaimHandle, readClaimScopeContext } = require("./work_queue_claim_scope.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");

/** @type {import('./types/handler-factory').HandlerFactoryFunction} */
async function main(config = {}, githubClient = null) {
  const descriptor = routes.normalizeRouteConfig(config);
  if (descriptor) return routedHandler(descriptor, config, githubClient);
  const { max: maxCount = 1 } = config;
  const normalizedMax = typeof maxCount === "string" && /^\d+$/.test(maxCount) ? Number(maxCount) : maxCount;
  if (!Number.isSafeInteger(normalizedMax) || normalizedMax < 1) {
    throw new Error("request_continuation max must be a positive integer");
  }
  let processedCount = 0;

  return async function handleRequestContinuation(message) {
    if (processedCount >= normalizedMax) {
      return { success: false, error: `Max count of ${normalizedMax} reached` };
    }
    if (typeof message.message !== "string" || !message.message.trim()) {
      return { success: false, error: "Missing required field: message" };
    }
    processedCount++;
    // Do not echo potentially sensitive agent content into Actions logs.
    core.info("request_continuation: follow-up requested; continuation not confirmed");
    return {
      success: true,
      message: message.message,
      timestamp: new Date().toISOString(),
    };
  };
}

/**
 * Routed form: the only consumer is Process Safe Outputs. A request is routed only
 * when it is the single bound request of its scope in this attempt's verified output.
 * @param {NonNullable<ReturnType<typeof routes.normalizeRouteConfig>>} descriptor
 * @param {Record<string, any>} config
 * @param {any} githubClient
 */
function routedHandler(descriptor, config, githubClient) {
  let accepted = 0;
  /** @type {ReturnType<typeof routes.bindRoutedRequests> | undefined} */
  let bound;
  const noHandoff = (/** @type {string} */ reasonCode, /** @type {string} */ reason) => {
    core.warning(`request_continuation: ${reason}`);
    return { success: false, skipped: true, route: descriptor.route, status: "not_routed", reasonCode, reason };
  };

  return async function handleRoutedContinuation(/** @type {any} */ message) {
    if (accepted >= 1) return { success: false, error: "Max count of 1 reached" };
    if (typeof message?.message !== "string" || !message.message.trim()) return { success: false, error: "Missing required field: message" };
    const handle = currentClaimHandle();
    const assignment = handle ? currentClaimAssignment() : null;
    bound ??= routes.bindRoutedRequests({ assignment });
    if (!bound.ok) return noHandoff(bound.reason, "agent output is not bound to this run attempt or its collection is not clean; no handoff");
    const request = bound.requests.get(handle ?? "");
    if (!request || request.message !== message.message) return noHandoff("continuation_request_unbound", "request is not bound to the verified agent output; no handoff");
    accepted++;
    const client = githubClient || global.github;

    if (descriptor.route === "dispatch") {
      if (handle || readClaimScopeContext()) return { success: false, route: "dispatch", error: "The dispatch route requires an ordinary, non-queue source" };
      return routes.dispatchContinuation({ client, context: global.context, descriptor, request, staged: isStagedMode(config), coreApi: core });
    }
    if (!handle || !assignment) return { success: false, route: descriptor.route, error: `The ${descriptor.route} route requires an assignment-bound worker Claim` };
    const member = assignment.claims.find(claim => claim.handle === handle);
    if (!member) return { success: false, route: descriptor.route, error: "Claim is foreign to this assignment" };
    try {
      if (descriptor.route === "workqueue") {
        const receipt = routes.workqueueReceipt({ member, request, maxDepth: descriptor.max_depth });
        core.info(`request_continuation: Claim ${handle} successor ${receipt.status}`);
        return { success: true, route: "workqueue", status: receipt.status, receipt };
      }
      if (routes.writesProhibited(member.work)) return { success: false, route: "issue", errorCode: "continuation_writes_prohibited", error: "Work effect_contract prohibits resource writes; no approval issue was created" };
      routes.assertContinuationBudget(member.work, { route: "issue", message: request.message, remaining: 0 });
      if (isStagedMode(config)) return { success: true, staged: true, route: "issue", status: "preview" };
      const issue = await routes.createApprovalIssue({ client, context: global.context, member, request });
      core.info(`request_continuation: Claim ${handle} approval issue #${issue.number} created`);
      return {
        success: true,
        route: "issue",
        status: "ready",
        issue_number: issue.number,
        ...(issue.url ? { url: issue.url } : {}),
        receipt: { claim_handle: member.handle, claim_id: member.claim_id, message_sha256: request.message_sha256, status: "ready", issue: issue.resource },
      };
    } catch (error) {
      const code = error && typeof error === "object" && "code" in error && typeof error.code === "string" ? error.code : "continuation_route_failed";
      return { success: false, route: descriptor.route, errorCode: code, error: `request_continuation ${descriptor.route} route failed: ${error instanceof Error ? error.message : String(error)}` };
    }
  };
}

module.exports = { main };
