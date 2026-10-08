import { describe, expect, it } from "vitest";

const fs = require("node:fs");
const path = require("node:path");
const { DEFAULT_LIMITS } = require("./work_queue_limits.cjs");
const { validatePolicy } = require("./work_queue_policy.cjs");

describe("work-queue deployment documentation", () => {
  it("installs the documented policy after replacing identity placeholders", () => {
    const source = fs.readFileSync(path.resolve(__dirname, "../../../.github/aw/work-queue.md"), "utf8");
    const examples = [...source.matchAll(/```json\n([\s\S]*?)\n```/g)];
    expect(examples).toHaveLength(1);
    const policy = JSON.parse(examples[0][1].replaceAll("REPLACE_WITH_PRODUCER_ACTOR_ID", "11").replaceAll("REPLACE_WITH_WORKER_CREDENTIAL_ACTOR_ID", "12").replaceAll("REPLACE_WITH_40_OR_64_HEX_COMMIT_SHA", "a".repeat(40)));

    expect(validatePolicy(policy)).toBe(policy);
    expect(policy.accounting_weights).toEqual({ "": 1 });
    expect(policy.producers["11"].fairness_keys).toEqual([""]);
    expect(policy.limits).toEqual(DEFAULT_LIMITS);
  });
});
