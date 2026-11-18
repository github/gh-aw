// @ts-check
"use strict";

const { createFirewallSteeringMonitor } = require("./harness_firewall_steering.cjs");

/**
 * @param {{on: (event: "turn_end", handler: (event: any) => Promise<void>) => void, sendUserMessage: (message: string, options: {deliverAs: "steer"}) => void}} pi
 * @param {{remainingMs: () => number | undefined, minimumRemainingMs: number, env?: NodeJS.ProcessEnv, log?: (message: string) => void}} options
 */
function installPiFirewallSteering(pi, { remainingMs, minimumRemainingMs, env = process.env, log = message => process.stderr.write(`[gh-aw/firewall-steering] ${message}\n`) }) {
  const reserve = Math.max(10000, minimumRemainingMs);
  const monitor = createFirewallSteeringMonitor({ env, minimumRemainingMs: reserve, log });
  if (!monitor) return;
  /** @type {import("./harness_policy.cjs").HarnessPolicyAdapter} */
  const adapter = {
    engine: "pi",
    actions: {
      steer(action) {
        const remaining = remainingMs();
        if (process.exitCode || remaining === undefined || !Number.isFinite(remaining) || remaining <= reserve) {
          log("firewall-steering: suppressed; terminal guard or completion window reached");
          return;
        }
        pi.sendUserMessage(action.message, { deliverAs: "steer" });
      },
    },
  };
  pi.on("turn_end", async event => {
    const remaining = remainingMs();
    if (process.exitCode || remaining === undefined || event.message?.stopReason !== "toolUse") return;
    await monitor.observe(adapter, remaining);
  });
}

module.exports = { installPiFirewallSteering };
