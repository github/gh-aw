// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import { policyProposalFor, loadQueue } from "./work_queue_binding.cjs";
import { defaultPolicy } from "./work_queue_policy.cjs";
import { readWorkQueuePolicyConfig } from "./work_queue_policy_config.cjs";
import { newState } from "./work_queue_replay.cjs";

function clearPolicyEnvironment() {
  for (const name of Object.keys(process.env)) if (/^GH_AW_WORK_QUEUE_POLICY(?:_PARTS|_[0-9]+)?$/.test(name)) vi.stubEnv(name, undefined);
}

afterEach(() => vi.unstubAllEnvs());

describe("trusted binding Policy configuration", () => {
  it("retains missing configuration and explicit trusted option precedence", () => {
    clearPolicyEnvironment();
    expect(policyProposalFor({})).toBeUndefined();
    const policy = defaultPolicy({ repository: "owner/repo", principal: "11", ref: "a".repeat(40) });
    vi.stubEnv("GH_AW_WORK_QUEUE_POLICY", JSON.stringify(policy));
    expect(policyProposalFor({})).toEqual(policy);
    expect(policyProposalFor({ policyProposal: undefined })).toBeUndefined();
    vi.stubEnv("GH_AW_WORK_QUEUE_POLICY_PARTS", "2");
    expect(() => policyProposalFor({})).toThrow(/conflicting/);
    expect(policyProposalFor({ policyProposal: policy })).toBe(policy);
  });

  it("reconstructs actual compiler-emitted global registry chunks through the production binding reader", async () => {
    clearPolicyEnvironment();
    const compiled = fs.readFileSync(new URL("../../../.github/workflows/eslint-factory-dispatcher.lock.yml", import.meta.url), "utf8");
    const blocks = [...compiled.matchAll(/(?:^ +GH_AW_WORK_QUEUE_POLICY(?:_PARTS|_[0-9]+)?: "[^\n]+"\n)+/gm)];
    expect(blocks.length).toBeGreaterThanOrEqual(2);
    for (const [block] of blocks) {
      clearPolicyEnvironment();
      const environment = Object.fromEntries(
        [...block.matchAll(/(GH_AW_WORK_QUEUE_POLICY(?:_PARTS|_[0-9]+)?): ("[^\n]+")/g)].map(([, name, value]) => [
          name,
          JSON.parse(value).replaceAll("${{ github.repository }}", "owner/repo").replaceAll("${{ github.sha }}", "a".repeat(40)),
        ])
      );
      const expected = JSON.parse(readWorkQueuePolicyConfig(environment));
      expect(Buffer.byteLength(JSON.stringify(expected))).toBeGreaterThan(21 * 1024);
      expect(Object.keys(expected.pools)).toHaveLength(3);
      expect(Object.keys(expected.pools.default.profiles).length).toBeGreaterThanOrEqual(30);
      for (const [name, value] of Object.entries(environment)) {
        expect(Buffer.byteLength(value)).toBeLessThan(21 * 1024);
        vi.stubEnv(name, value);
      }
      expect(policyProposalFor({})).toEqual(expected);
      const queue = await loadQueue({
        context: { repo: { owner: "owner", repo: "repo" } },
        readWorkQueueLog: async () => ({ transactions: [], sha: null, state: newState() }),
      });
      expect(queue.projection.policy).toBeNull();
      expect(queue.projection.deployments.size).toBe(0);
    }
  });

  it("rejects missing and conflicting compiler chunks before accepting an absent queue", async () => {
    clearPolicyEnvironment();
    const options = { context: { repo: { owner: "owner", repo: "repo" } }, readWorkQueueLog: async () => ({ transactions: [], sha: null, state: newState() }) };
    vi.stubEnv("GH_AW_WORK_QUEUE_POLICY_PARTS", "2");
    vi.stubEnv("GH_AW_WORK_QUEUE_POLICY_0", "{}");
    await expect(loadQueue(options)).rejects.toThrow(/missing or extra/);
    vi.stubEnv("GH_AW_WORK_QUEUE_POLICY", "{}");
    await expect(loadQueue(options)).rejects.toThrow(/conflicting/);
  });
});
