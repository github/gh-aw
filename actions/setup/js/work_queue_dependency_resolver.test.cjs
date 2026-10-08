// @ts-check
import { describe, expect, it, vi } from "vitest";
import { dependencyKey, isFreshObservation, predicate, resolveDependencies, resolveExternalEdges } from "./work_queue_dependency_resolver.cjs";
import { gateKey } from "./work_queue_graph.cjs";

const resource = { kind: "issue", host: "github.com", repository: "foreign/design", number: "7", condition: "completed", repository_id: "20", resource_id: "30" };
const scope = { host: "github.com", repository: "foreign/design", access_generation: "g1" };
function setup(data = { id: 30, number: 7, state: "closed", state_reason: "completed", updated_at: "2026-10-05T00:00:00Z" }) {
  const client = { rest: { repos: { get: vi.fn().mockResolvedValue({ data: { id: 20, full_name: "foreign/design" } }) }, issues: { get: vi.fn().mockResolvedValue({ data }) }, pulls: { get: vi.fn().mockResolvedValue({ data }) } } };
  return { client, scopes: [scope], getClient: vi.fn().mockResolvedValue(client), now: 1000 };
}

describe("typed trusted external dependency observations", () => {
  it("deduplicates resource reads including distinct predicates and does not return write authority", async () => {
    const options = setup();
    const result = await resolveDependencies([resource, resource, { ...resource, condition: "closed" }], options);
    expect(result.reads).toBe(2);
    expect(result.observations).toHaveLength(2);
    expect(result.observations[0]).toMatchObject({ state: "ready", access_generation: "g1", observed_at: 1000, resource });
    expect(result.observations[0]).not.toHaveProperty("dispatch");
    expect(options.client.rest.issues.get).toHaveBeenCalledTimes(1);
    expect(options.client.rest.issues.get.mock.calls[0][0].request).toEqual({ retries: 0, timeout: 15000 });
  });

  it("rejects foreign access without an explicit credential-bound allowlist", async () => {
    const options = setup();
    await expect(resolveDependencies([resource], { ...options, scopes: [] })).rejects.toThrow("external_not_allowlisted");
    expect(options.getClient).not.toHaveBeenCalled();
    expect((await resolveDependencies([resource], { ...options, getClient: async () => null })).observations[0]).toMatchObject({ state: "unknown", read_status: "external_read_credentials_missing" });
  });

  it("deduplicates approved renamed coordinates by immutable IDs while preserving admission bindings", async () => {
    const options = setup();
    const renamed = { ...resource, repository: "foreign/renamed" };
    const renamedScope = { ...scope, repository: renamed.repository };
    options.scopes.push(renamedScope);
    options.client.rest.repos.get.mockResolvedValue({ data: { id: 20, full_name: renamed.repository } });
    const result = await resolveDependencies([resource, renamed], options);
    expect(result.reads).toBe(2);
    expect(result.observations).toHaveLength(1);
    expect(result.bindings.get(dependencyKey(resource))).toMatchObject(renamed);
    expect(isFreshObservation(result.observations[0], resource, scope, options.now, 1000)).toBe(true);
    expect(dependencyKey({ ...resource, resource_id: "31" })).not.toBe(dependencyKey(resource));
    expect(dependencyKey({ ...resource, resource_id: undefined })).not.toBe(dependencyKey({ ...resource, resource_id: undefined, repository_id: "21" }));
    await expect(resolveDependencies([{ ...resource, number: "8" }, renamed], options)).rejects.toThrow("external_identity_mismatch");
    expect((await resolveDependencies([{ ...resource, resource_id: "31" }, renamed], options)).observations.map(observation => observation.state)).toEqual(["failed", "ready"]);
  });

  it("uses shared stable gate identities without granting access through unapproved display aliases", async () => {
    expect(dependencyKey(resource)).toBe(gateKey(resource, resource.condition));
    expect(dependencyKey({ ...resource, repository: "foreign/renamed", number: "99" })).toBe(gateKey(resource, resource.condition));
    expect(dependencyKey({ ...resource, condition: "closed" })).not.toBe(dependencyKey(resource));
    const options = setup();
    await expect(resolveDependencies([resource, { ...resource, repository: "unauthorized/alias" }], options)).rejects.toThrow("external_not_allowlisted");
    expect(options.getClient).not.toHaveBeenCalled();
  });

  it("checks immutable resource and repository IDs, API type and transfer destinations", async () => {
    for (const data of [
      { id: 31, number: 7 },
      { id: 30, number: 8 },
      { id: 30, number: 7, pull_request: {} },
    ]) {
      expect((await resolveDependencies([resource], setup(data))).observations[0].state).toBe("failed");
    }
    const options = setup();
    options.client.rest.repos.get.mockResolvedValue({ data: { id: 20, full_name: "unauthorized/design" } });
    expect((await resolveDependencies([resource], options)).observations[0]).toMatchObject({ state: "failed", read_status: "external_redirect_not_allowlisted" });
    options.scopes.push({ ...scope, repository: "unauthorized/design", access_generation: "g2" });
    expect((await resolveDependencies([resource], options)).observations[0]).toMatchObject({ state: "failed", read_status: "external_redirect_not_allowlisted" });
    options.client.rest.repos.get.mockResolvedValue({ status: 403, data: { id: 20, full_name: resource.repository } });
    expect((await resolveDependencies([resource], options)).observations[0]).toMatchObject({ state: "unknown", read_status: "external_access_denied" });
  });

  it("requires completed Issue reasons and actual PR merge provenance", () => {
    expect(predicate(resource, { state: "closed", state_reason: "not_planned" }).state).toBe("failed");
    expect(predicate(resource, { state: "closed" }).state).toBe("unknown");
    expect(predicate(resource, { state: "open" })).toEqual({ state: "waiting", read_status: "external_reopened" });
    const pr = { ...resource, kind: "pull_request", condition: "merged" };
    expect(predicate(pr, { state: "closed", merged: false }).state).toBe("failed");
    expect(predicate(pr, { state: "closed", merged: true }).state).toBe("unknown");
    expect(predicate(pr, { merged: true, merged_at: "2026-10-05T00:00:00Z", merge_commit_sha: "a".repeat(40) }).state).toBe("ready");
    expect(predicate(pr, { merged: true, merged_at: "2026-10-05T00:00:00Z", merge_commit_sha: "a".repeat(64) }).state).toBe("ready");
    expect(predicate(pr, { merged: true, merged_at: "invalid", merge_commit_sha: "a".repeat(40) }).state).toBe("unknown");
  });

  it("uses pulls API for Pull Requests rather than Issues compatibility data", async () => {
    const options = setup({ id: 30, number: 7, state: "closed", merged: true, merged_at: "2026-10-05T00:00:00Z", merge_commit_sha: "a".repeat(40), base: { repo: { id: 20 } } });
    expect((await resolveDependencies([{ ...resource, kind: "pull_request", condition: "merged" }], options)).observations[0].state).toBe("ready");
    expect(options.client.rest.pulls.get).toHaveBeenCalledTimes(1);
    expect(options.client.rest.issues.get).not.toHaveBeenCalled();
  });

  it("records explicit unknown for denied/missing/unavailable reads, never success", async () => {
    for (const status of [401, 403, 404, 429, 500]) {
      const options = setup();
      options.client.rest.issues.get.mockRejectedValue(Object.assign(new Error("private details must not be retained"), { status }));
      const observation = (await resolveDependencies([resource], options)).observations[0];
      expect(observation.state).toBe("unknown");
      expect(JSON.stringify(observation)).not.toContain("private details");
    }
  });

  it("bounds reads and requires fresh current-generation observations", async () => {
    expect((await resolveDependencies([resource], { ...setup(), maxReads: 1 })).observations[0]).toMatchObject({ state: "unknown", read_status: "external_read_budget_exhausted" });
    const observation = { resource, state: "ready", observed_at: 900, access_generation: "g1" };
    expect(isFreshObservation(observation, resource, scope, 1000, 100)).toBe(true);
    expect(isFreshObservation(observation, resource, scope, 1001, 100)).toBe(false);
    expect(isFreshObservation(observation, resource, { ...scope, access_generation: "g2" }, 1000, 100)).toBe(false);
    expect(isFreshObservation({ ...observation, invalidated: true }, resource, scope, 1000, 100)).toBe(false);
  });

  it("builds bounded durable canonical Observation operations rather than an authority sidecar", async () => {
    const { condition, ...identity } = resource;
    const result = await resolveExternalEdges([{ kind: "issue", resource: identity, condition }], setup());
    expect(result.operations[0]).toMatchObject({ kind: "Observation", observation_id: expect.stringMatching(/^observation:[a-f0-9]{64}$/), resource: identity, condition, credential_generation: "g1", state: "ready" });
    expect(result.operations[0].resource).not.toHaveProperty("condition");
    expect(result.operations[0]).not.toHaveProperty("access_generation");
  });
});
