// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import { expectedGitHubHost, frozenResourceScope, validateEffectResource } from "./work_queue_resource_scope.cjs";
import { resolveParentResourceTarget, resolveRepositoryTarget } from "./work_queue_effect_resource.cjs";

afterEach(() => {
  vi.unstubAllEnvs();
});

describe("effect resource targets", () => {
  it.each([
    [undefined, "github.com", "https://api.github.com"],
    ["https://github.com", "github.com", "https://api.github.com"],
    ["https://contoso.ghe.com", "contoso.ghe.com", "https://api.contoso.ghe.com"],
  ])("binds resolved native targets to the configured server %s", async (server, host, api) => {
    vi.stubEnv("GITHUB_SERVER_URL", server);
    vi.stubEnv("GITHUB_API_URL", api);
    expect(expectedGitHubHost()).toBe(host);
    const target = { host, repository: "owner/repo", repository_id: "7", kind: "issue", resource_id: "100", number: "42" };
    expect(validateEffectResource(target)).toEqual(target);
    expect(() => validateEffectResource({ ...target, host: "other.ghe.com" })).toThrow(/host is unsupported/);
    const github = { rest: { repos: { get: async () => ({ data: { id: 7, full_name: "owner/repo" } }) } } };
    expect(await resolveRepositoryTarget(github, { repository: "owner/repo" })).toEqual({ repository: "owner/repo", repository_id: "7", host });
    expect(await resolveParentResourceTarget(github, { repository: "owner/repo", kind: "issue", number: "42" }, { id: 100, number: 42 })).toEqual(target);
  });

  it("rejects an invalid server URL", () => {
    vi.stubEnv("GITHUB_SERVER_URL", "invalid-server-url");
    expect(() => expectedGitHubHost()).toThrow("Work queue requires a valid GITHUB_SERVER_URL");
  });

  it("rejects malformed targets and enforces UTF-8 identity bounds", () => {
    for (const target of [
      null,
      [],
      { repository: "owner/repo", number: 7 },
      { repository: "owner/repo", ref: "\uD800" },
      { repository: "owner/repo", path: "/src/a" },
      { repository: "owner/repo", path: "src//a" },
      { repository: "owner/repo", path: "src/../a" },
      { repository: "owner/repo", path: "src\\a" },
      { repository: "owner/repo", unknown: "x" },
    ]) {
      expect(() => validateEffectResource(target)).toThrow(/claim_scope_invalid|effect/);
    }
    for (const ref of ["\\".repeat(256), "\u00e9".repeat(128)]) expect(() => validateEffectResource({ repository: "owner/repo", ref })).not.toThrow();
    expect(() => validateEffectResource({ repository: "owner/repo", ref: "\u00e9".repeat(129) })).toThrow();
  });

  it("validates a Work payload resource_scope with unique detached selectors", () => {
    const target = { repository: "owner/repo", number: "7" };
    const payload = { resource_scope: { version: 1, resources: [target] } };
    const scope = frozenResourceScope(payload);
    payload.resource_scope.resources[0].number = "9";
    expect(scope).toEqual({ version: 1, resources: [{ repository: "owner/repo", number: "7" }] });
    expect(frozenResourceScope({ task: "none" })).toBeUndefined();
    expect(() =>
      frozenResourceScope({
        resource_scope: {
          version: 1,
          resources: [
            { repository: "owner/repo", number: "7" },
            { number: "7", repository: "owner/repo" },
          ],
        },
      })
    ).toThrow(/unique/);
    const many = Array.from({ length: 129 }, (_, index) => ({ repository: "owner/repo", ref: `refs/heads/${index}` }));
    expect(() => frozenResourceScope({ resource_scope: { version: 1, resources: many } })).toThrow(/128/);
    expect(frozenResourceScope({ resource_scope: { version: 1, resources: many.slice(0, 128) } })?.resources).toHaveLength(128);
  });
});
