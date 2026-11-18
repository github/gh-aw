// @ts-check
"use strict";

const { createHarnessPolicyDispatcher } = require("./harness_policy.cjs");
const { createFirewallLogReader } = require("./harness_firewall_logs.cjs");

const SAFE_GUIDANCE =
  "Do not repeat blocked requests or work around restrictions using alternate hosts, IP addresses, proxies, transports, credentials, or disabled verification. Do not change firewall rules or request broader access. Continue only with already-permitted tools and local or cached inputs without accessing a denied resource by another route. If required inputs remain unavailable, report the limitation through configured safe outputs; do not claim successful completion.";

/** @param {NodeJS.ProcessEnv} [env] */
function isFirewallSteeringEnabled(env = process.env) {
  return env.GH_AW_HARNESS_FIREWALL_STEERING === "1" && (!env.GH_AW_PHASE || env.GH_AW_PHASE === "agent") && env.GH_AW_PI_SUBAGENT_CHILD !== "1";
}

/**
 * One warning and one denial message at most; a denial supersedes warnings.
 * @param {{minimumRemainingMs: number, log: (message: string) => void}} options
 * @returns {import("./harness_policy.cjs").HarnessPolicy}
 */
function createFirewallSteeringPolicy({ minimumRemainingMs, log }) {
  let warned = false;
  let denied = false;
  return {
    id: "firewall-steering",
    onEvent(event, context) {
      if (event.type !== "firewall.observed" || !Number.isSafeInteger(event.count) || event.count <= 0 || !Number.isFinite(event.remainingMs) || event.remainingMs <= minimumRemainingMs) return;
      const type = context.capabilities.includes("steer") ? "steer" : context.capabilities.includes("retry_guidance") ? "retry_guidance" : undefined;
      if (!type || denied || (event.severity === "warning" && warned)) return;
      if (event.severity === "denied") denied = true;
      else if (event.severity === "warning") warned = true;
      else return;
      log(`firewall-steering: severity=${event.severity}; count=${event.count}; action=${type}`);
      const prefix =
        event.severity === "denied"
          ? "The workflow firewall denied network access. Treat this as a policy boundary, not a transient authentication failure."
          : "The workflow firewall recorded a network warning. Access is unconfirmed; do not assume that another attempt or route is permitted.";
      return [{ type, message: `${prefix} ${SAFE_GUIDANCE}` }];
    },
  };
}

/**
 * Owns run-local evidence offsets and policy state, shared across checkpoints.
 * No polling timer, model request, firewall mutation, or arbitrary log path.
 * @param {{env?: NodeJS.ProcessEnv, minimumRemainingMs: number, log: (message: string) => void}} options
 */
function createFirewallSteeringMonitor({ env = process.env, minimumRemainingMs, log }) {
  if (!isFirewallSteeringEnabled(env)) return;
  const read = createFirewallLogReader({ env, log });
  const policy = createFirewallSteeringPolicy({ minimumRemainingMs, log });
  return {
    /** @param {import("./harness_policy.cjs").HarnessPolicyAdapter} adapter @param {number} remainingMs */
    async observe(adapter, remainingMs) {
      if (!Number.isFinite(remainingMs) || remainingMs <= minimumRemainingMs) return;
      const counts = read();
      const dispatcher = createHarnessPolicyDispatcher({ adapter, policies: [policy] });
      // The more actionable denial takes precedence over warnings in this batch.
      /** @type {("denied" | "warning")[]} */
      const severities = ["denied", "warning"];
      for (const severity of severities) {
        if (counts[severity]) await dispatcher.dispatch({ type: "firewall.observed", severity, count: counts[severity], remainingMs });
      }
    },
  };
}

module.exports = { isFirewallSteeringEnabled, createFirewallSteeringPolicy, createFirewallSteeringMonitor };
