// @ts-check
import { describe, expect, it } from "vitest";
import { defaultPolicy } from "./work_queue_policy.cjs";
import { newWork } from "./work_queue_graph.cjs";
import { newRequest } from "./work_queue_replay.cjs";
import { publishWorkQueueRequest } from "./work_queue_store.cjs";
import { fakeGitHub, options } from "./work_queue_store_checks.cjs";
import { producer } from "./work_queue_test_helpers.cjs";

function bootstrapFixture() {
  const fake = fakeGitHub();
  const policy = defaultPolicy({ repository: "owner/repo", principal: "1001", ref: fake.state.defaultRevision });
  policy.authorization = "aw";
  policy.producers = {};
  const pool = policy.pools.default;
  delete pool.profiles.default.principal;
  for (let index = 0; index < 30; index++) pool.profiles[`unused-${index}`] = { ...pool.profiles.default, workflow: `.github/workflows/unused-${index}.lock.yml` };
  policy.pools.daily = structuredClone(pool);
  const nodes = ["first", "second"].map(key => newWork({ task: key }, "bootstrap-graph", key, "default", policy, 100));
  const request = newRequest("aw-global-bootstrap", "submit", producer, { nodes });
  return { fake, policy, request };
}

describe("AW shared-registry first-use route verification", () => {
  it("installs the global registry while verifying only submitted routes, without requiring unrelated active registrations", async () => {
    const { fake, policy, request } = bootstrapFixture();
    const result = await publishWorkQueueRequest(options(fake, request, producer, { policyProposal: policy, maxRetries: 0 }));
    expect(result.publishedNow).toBe(true);
    expect(result.state.policy).toEqual(policy);
    expect(Object.keys(result.state.policy.pools.daily.profiles)).toHaveLength(31);
    expect(result.state.works.size).toBe(2);
    expect(fake.state.calls.filter(call => call.startsWith("getContent:"))).toEqual([`getContent:.github/workflows/worker.lock.yml@${fake.state.defaultRevision}`]);
    expect(fake.state.calls.filter(call => call.startsWith("getWorkflow:"))).toEqual(["getWorkflow:worker.lock.yml"]);
    for (const profile of Object.values(result.state.policy.pools.default.profiles)) expect(profile).not.toHaveProperty("principal");
    await publishWorkQueueRequest(options(fake, request, producer, { policyProposal: policy, maxRetries: 0 }));
    expect(fake.log()).toHaveLength(1);
    expect(fake.state.calls.filter(call => call.startsWith("getWorkflow:"))).toHaveLength(1);
  });

  it.each(["missing-content", "missing-registration", "disabled-registration", "moved-registration"])("rejects the relevant submitted route with %s before publishing a queue", async failure => {
    const { fake, policy, request } = bootstrapFixture();
    if (failure === "missing-content") fake.state.workerContent = null;
    if (failure === "missing-registration") fake.state.workerWorkflow = null;
    if (failure === "disabled-registration") fake.state.workerWorkflow.state = "disabled_manually";
    if (failure === "moved-registration") fake.state.workerWorkflow.path = ".github/workflows/other.lock.yml";
    await expect(publishWorkQueueRequest(options(fake, request, producer, { policyProposal: policy, maxRetries: 0 }))).rejects.toThrow(/policy_missing/);
    expect(fake.refs.has("work-queue")).toBe(false);
    expect(fake.state.updates).toBe(0);
    expect(fake.blobs.size).toBe(0);
  });

  it("still schema-validates unused compiler-managed routes before any native calls or publication", async () => {
    const { fake, policy, request } = bootstrapFixture();
    policy.pools.daily.profiles["unused-29"].ref = "main";
    await expect(publishWorkQueueRequest(options(fake, request, producer, { policyProposal: policy, maxRetries: 0 }))).rejects.toThrow(/policy_invalid/);
    expect(fake.refs.has("work-queue")).toBe(false);
    expect(fake.state.calls.some(call => call.startsWith("getContent:") || call.startsWith("getWorkflow:"))).toBe(false);
  });
});
