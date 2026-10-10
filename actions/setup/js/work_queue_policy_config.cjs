// @ts-check

/**
 * Read protected compiler configuration without exceeding Actions' per-value limit.
 * @param {Record<string, unknown>} [environment]
 * @returns {string}
 */
function readWorkQueuePolicyConfig(environment = process.env) {
  const raw = environment.GH_AW_WORK_QUEUE_POLICY;
  const count = environment.GH_AW_WORK_QUEUE_POLICY_PARTS;
  const partNames = Object.keys(environment).filter(name => /^GH_AW_WORK_QUEUE_POLICY_[0-9]+$/.test(name));
  if (count === undefined) {
    if (partNames.length > 0) throw new Error("work-queue: Policy chunks require GH_AW_WORK_QUEUE_POLICY_PARTS");
    if (raw === undefined) return "";
    if (typeof raw !== "string" || raw.length === 0) throw new Error("work-queue: protected Policy configuration must be a nonempty JSON string");
    return raw;
  }
  if (raw !== undefined) throw new Error("work-queue: conflicting complete and chunked Policy configuration");
  if (typeof count !== "string" || !/^[1-9][0-9]*$/.test(count) || Number(count) > 256) {
    throw new Error("work-queue: GH_AW_WORK_QUEUE_POLICY_PARTS must be an integer from 1 to 256");
  }
  if (partNames.length !== Number(count)) throw new Error("work-queue: missing or extra protected Policy chunks");
  const parts = [];
  for (let index = 0; index < Number(count); index += 1) {
    const part = environment[`GH_AW_WORK_QUEUE_POLICY_${index}`];
    if (typeof part !== "string" || part.length === 0) throw new Error(`work-queue: missing or invalid protected Policy chunk ${index}`);
    parts.push(part);
  }
  return parts.join("");
}

module.exports = { readWorkQueuePolicyConfig };
