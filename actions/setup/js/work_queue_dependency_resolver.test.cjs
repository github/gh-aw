// @ts-check
import { describe, expect, it, vi } from "vitest";
import { allowedScope, approvalPredicate, dependencyKey, predicate, resolveDependencies, resolveExternalEdges } from "./work_queue_dependency_resolver.cjs";

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

describe("continuation approval gate", () => {
  const approved = { ...issue, condition: "approved" };
  const writer = { login: "octo-writer", id: 4242, type: "User" };
  const closedBy = (closer, extra = {}) => ({ id: 55, number: 5, state: "closed", state_reason: "completed", closed_by: closer, ...extra });
  const permission = (level, user = writer, extra = {}) => vi.fn(async () => ({ status: 200, data: { permission: level, user: { login: user.login, id: user.id }, ...extra } }));
  const approvalClient = (data, lookup = permission("write")) => {
    const reader = client({ data });
    reader.rest.repos.getCollaboratorPermissionLevel = lookup;
    return reader;
  };

  it.each([
    ["open", { state: "open" }, { state: "waiting", state_reason: "reopened" }],
    ["closed as not planned", { state: "closed", state_reason: "not_planned", closed_by: writer }, { state: "failed", state_reason: "not_planned" }],
    ["an unrecognized close reason", { state: "closed", state_reason: "duplicate", closed_by: writer }, { state: "unknown" }],
    ["no closer", { state: "closed", state_reason: "completed" }, { state: "unknown" }],
    ["a bot closer", closedBy({ login: "app[bot]", id: 9, type: "Bot" }), { state: "failed", state_reason: "completed" }],
    ["a malformed closer ID", closedBy({ ...writer, id: 0 }), { state: "unknown" }],
  ])("does not approve %s without a permission read", async (_name, data, expected) => {
    const lookup = vi.fn();
    expect(await approvalPredicate(data, lookup)).toEqual(expected);
    expect(lookup).not.toHaveBeenCalled();
  });

  it.each(["write", "maintain", "admin"])("approves a %s User close as completed with the closer's account ID", async level => {
    const lookup = vi.fn(async () => ({ permission: level === "maintain" ? "write" : level, role_name: level, user: { login: "Octo-Writer", id: 4242 } }));
    expect(await approvalPredicate(closedBy(writer), lookup)).toEqual({ state: "ready", state_reason: "completed", approved_by: "4242" });
    expect(lookup).toHaveBeenCalledWith("octo-writer");
  });

  it("fails read/triage closers and custom roles without a writer base role", async () => {
    for (const data of [
      { permission: "read", role_name: "triage" },
      { permission: "read", role_name: "read" },
      { permission: "admin", role_name: "Security Champions" },
    ]) {
      expect(await approvalPredicate(closedBy(writer), async () => ({ ...data, user: { login: writer.login, id: writer.id } }))).toEqual({ state: "failed", state_reason: "completed" });
    }
  });

  it("leaves an indeterminate permission read unknown", async () => {
    const denied = async () => Promise.reject(Object.assign(new Error("Forbidden"), { status: 403 }));
    expect(await approvalPredicate(closedBy(writer), denied)).toEqual({ state: "unknown" });
    expect(await approvalPredicate(closedBy(writer), async () => ({ permission: "write", user: { login: "someone-else", id: 4242 } }))).toEqual({ state: "unknown" });
    expect(await approvalPredicate(closedBy(writer), async () => ({ permission: "write" }))).toEqual({ state: "unknown" });
  });

  it("records approved_by and counts the permission read in the read budget", async () => {
    const lookup = permission("write");
    const reader = approvalClient(closedBy(writer), lookup);
    const { results, reads } = await resolveDependencies([approved], { scopes, getClient: async () => reader });
    expect(reads).toBe(3);
    expect(lookup.mock.calls[0][0]).toMatchObject({ owner: "owner", repo: "repo", username: "octo-writer" });
    expect(results.get(dependencyKey(approved)).observation).toMatchObject({ condition: "approved", state: "ready", state_reason: "completed", approved_by: "4242" });
    const exhausted = await resolveDependencies([approved], { scopes, getClient: async () => approvalClient(closedBy(writer)), maxReads: 2 });
    expect([...exhausted.results.values()][0]).toMatchObject({ error: "external_read_budget_exhausted" });
  });

  it("keeps ordinary completed gates on their original predicate without a collaborator lookup", async () => {
    const lookup = permission("write");
    const reader = approvalClient(closedBy({ login: "app[bot]", id: 9, type: "Bot" }), lookup);
    const { results, reads } = await resolveDependencies([issue], { scopes, getClient: async () => reader });
    expect(results.get(dependencyKey(issue)).observation).toMatchObject({ condition: "completed", state: "ready" });
    expect(reads).toBe(2);
    expect(lookup).not.toHaveBeenCalled();
    expect(dependencyKey(issue)).not.toBe(dependencyKey(approved));
  });

  it("never approves a pull request or a mismatched Issue identity", async () => {
    await expect(approvalPredicate(closedBy(writer, { pull_request: {} }), permission("write"))).rejects.toThrow(/type_mismatch/);
    const wrong = await resolveDependencies([{ ...approved, repository_id: "1", resource_id: "56" }], { scopes, getClient: async () => approvalClient(closedBy(writer)) });
    expect([...wrong.results.values()][0]).toEqual(expect.objectContaining({ error: "external_identity_mismatch" }));
  });
});
