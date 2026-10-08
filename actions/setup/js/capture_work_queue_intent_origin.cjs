// @ts-check
"use strict";

const { resolveWorkQueueRuntime } = require("./aw_context.cjs");
const { authenticatePublisher } = require("./work_queue_native.cjs");
const { actorFromContext } = require("./work_queue_policy.cjs");
const { canonical } = require("./work_queue_codec.cjs");

async function main(options = {}) {
  const output = options.core || global.core;
  if (typeof output?.setOutput !== "function" || typeof output.exportVariable !== "function") throw new Error("work_queue_origin_output_unavailable");
  const configuration = { ...options, githubClient: options.githubClient || global.github, context: options.context || global.context };
  const runtime = resolveWorkQueueRuntime(configuration.context?.payload, { role: options.role, requireAssignment: options.requireAssignment });
  if (runtime.role === "observer") throw new Error("work_queue_observer_read_only");
  // New agent executions capture their current origin; publisher retries consume that protected output.
  const source = await authenticatePublisher({
    ...configuration,
    role: runtime.role,
    ...(runtime.assignment ? { dispatch_id: runtime.assignment.dispatch_id } : {}),
  });
  const origin = actorFromContext(source);
  const encoded = canonical(origin);
  output.setOutput("work_queue_origin", encoded);
  output.exportVariable("GH_AW_WORK_QUEUE_INTENT_ORIGIN", encoded);
  return origin;
}

module.exports = { main };
