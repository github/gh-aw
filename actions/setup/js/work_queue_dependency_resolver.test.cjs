// @ts-check
import { describe, expect, it, vi } from "vitest";
import { allowedScope, dependencyKey, predicate, resolveDependencies, resolveExternalEdges } from "./work_queue_dependency_resolver.cjs";

const scopes = [{ host: "github.com", repository: "owner/repo" }];
const issue = { kind: "issue", host: "github.com", repository: "owner/repo", number: "5", condition: "completed" };

function client({ repository = { full_name: "owner/repo", id: 1 }, data = { id: 55, number: 5, state: "closed", state_reason: "completed" } } = {}) {
  return {
    rest: {
      repos: { get: vi.fn(async () => ({ status: 200, data: repository })) },
      issues: { get: vi.fn(async () => ({ status: 200, data })) },
      pulls: { get: vi.fn(async () => ({ status: 200, data })) },
    },
  };
}

describe("external dependency resolver", () => {
  it("maps Issue and pull request reads to v4 Observation fields", () => {
    expect(predicate(issue, { state: "closed", state_reason: "completed" })).toEqual({ state: "ready", state_reason: "completed" });
    expect(predicate(issue, { state: "closed", state_reason: "not_planned" })).toEqual({ state: "failed", state_reason: "not_planned" });
    expect(predicate(issue, { state: "open" })).toEqual({ state: "waiting", state_reason: "reopened" });
    expect(predicate({ ...issue, condition: "closed" }, { state: "closed", state_reason: null })).toEqual({ state: "ready" });
    const pull = { ...issue, kind: "pull_request", condition: "merged" };
    expect(predicate(pull, { merged: true, merge_commit_sha: "a".repeat(40) })).toEqual({ state: "ready", merged: true, merge_commit: "a".repeat(40) });
    expect(predicate(pull, { merged: true, merge_commit_sha: null })).toEqual({ state: "unknown", merged: true });
    expect(predicate(pull, { merged: false, state: "closed" })).toEqual({ state: "failed", merged: false });
    expect(() => predicate(issue, { state: "closed", pull_request: {} })).toThrow(/type_mismatch/);
  });

  it("resolves identities and a ready Observation within the allowed scopes", async () => {
    const reader = client();
    const { results, reads } = await resolveDependencies([issue], { scopes, getClient: async () => reader });
    const result = results.get(dependencyKey(issue));
    const binding = { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: "55", number: "5" };
    expect(result.binding).toEqual(binding);
    expect(result.observation).toEqual({ kind: "Observation", observation_id: expect.stringMatching(/^observation:[a-f0-9]{64}$/), resource: binding, condition: "completed", state: "ready", state_reason: "completed" });
    expect(reads).toBe(2);
    expect(await resolveExternalEdges([{ kind: "issue", resource: binding, condition: "completed" }], { scopes, getClient: async () => reader })).toEqual([result.observation]);
  });

  it("reports unreadable or mismatched references without throwing", async () => {
    const failing = { rest: { repos: { get: async () => Promise.reject(Object.assign(new Error("Not Found"), { status: 404 })) } } };
    const missing = await resolveDependencies([issue], { scopes, getClient: async () => failing });
    expect(missing.results.get(dependencyKey(issue))).toEqual({ resource: issue, error: "external_not_found" });
    const foreign = await resolveDependencies([{ ...issue, repository: "other/repo" }], { scopes, getClient: async () => client() });
    expect([...foreign.results.values()][0].error).toBe("external_not_allowlisted");
    const mismatch = await resolveDependencies([{ ...issue, resource_id: "9" }], { scopes, getClient: async () => client() });
    expect([...mismatch.results.values()][0].error).toBe("external_identity_mismatch");
    expect(await resolveExternalEdges([{ kind: "issue", resource: { ...issue, repository_id: "1", resource_id: "55" }, condition: "completed" }], { scopes, getClient: async () => failing })).toEqual([]);
  });

  it("validates reference shape and scope", () => {
    expect(allowedScope(issue, scopes)).toBe(scopes[0]);
    expect(() => allowedScope({ ...issue, condition: "merged" }, scopes)).toThrow(/condition/);
    expect(() => allowedScope({ ...issue, number: "0" }, scopes)).toThrow();
    expect(() => allowedScope({ ...issue, repository: "other/repo" }, scopes)).toThrow(/not_allowlisted/);
  });
});
