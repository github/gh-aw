// @ts-check
import { describe, it, expect, beforeEach, vi } from "vitest";

// Mock dependencies before importing the module
vi.mock("./resolve_mentions.cjs", () => {
  return {
    resolveMentionsLazily: vi.fn(async (_text, knownAuthors) => ({
      allowedMentions: knownAuthors,
      totalMentions: knownAuthors.length,
      resolvedCount: 0,
      limitExceeded: false,
    })),
    isPayloadUserBot: vi.fn(user => user?.type === "Bot"),
  };
});

vi.mock("./error_helpers.cjs", () => ({
  getErrorMessage: vi.fn(err => (err instanceof Error ? err.message : String(err))),
}));

const { resolveAllowedMentionsFromPayload, extractKnownAuthorsFromPayload, fetchTeamMembers, pushNonBotUser, pushNonBotAssignees } = await import("./resolve_mentions_from_payload.cjs");

/** @returns {{ info: ReturnType<typeof vi.fn>, warning: ReturnType<typeof vi.fn>, error: ReturnType<typeof vi.fn> }} */
function makeMockCore() {
  return { info: vi.fn(), warning: vi.fn(), error: vi.fn() };
}

/** @returns {any} */
function makeMockGithub() {
  return {};
}

describe("pushNonBotUser", () => {
  it("pushes a regular user login", () => {
    const users = /** @type {string[]} */ [];
    pushNonBotUser(users, { login: "alice", type: "User" });
    expect(users).toEqual(["alice"]);
  });

  it("skips bot users", () => {
    const users = /** @type {string[]} */ [];
    pushNonBotUser(users, { login: "dependabot", type: "Bot" });
    expect(users).toEqual([]);
  });

  it("skips null user", () => {
    const users = /** @type {string[]} */ [];
    pushNonBotUser(users, null);
    expect(users).toEqual([]);
  });

  it("skips user without login", () => {
    const users = /** @type {string[]} */ [];
    pushNonBotUser(users, { type: "User" });
    expect(users).toEqual([]);
  });
});

describe("pushNonBotAssignees", () => {
  it("pushes non-bot assignees", () => {
    const users = /** @type {string[]} */ [];
    pushNonBotAssignees(users, [
      { login: "alice", type: "User" },
      { login: "bot", type: "Bot" },
      { login: "bob", type: "User" },
    ]);
    expect(users).toEqual(["alice", "bob"]);
  });

  it("handles null assignees", () => {
    const users = /** @type {string[]} */ [];
    pushNonBotAssignees(users, null);
    expect(users).toEqual([]);
  });

  it("handles empty assignees array", () => {
    const users = /** @type {string[]} */ [];
    pushNonBotAssignees(users, []);
    expect(users).toEqual([]);
  });
});

describe("extractKnownAuthorsFromPayload", () => {
  it("returns empty array when context is undefined", () => {
    expect(extractKnownAuthorsFromPayload(undefined)).toEqual([]);
  });

  it("extracts issue author and assignees for issues event", () => {
    const context = {
      eventName: "issues",
      payload: {
        issue: {
          user: { login: "alice", type: "User" },
          assignees: [{ login: "bob", type: "User" }],
        },
      },
    };
    expect(extractKnownAuthorsFromPayload(context)).toEqual(["alice", "bob"]);
  });

  it("skips bot issue author", () => {
    const context = {
      eventName: "issues",
      payload: {
        issue: {
          user: { login: "dependabot", type: "Bot" },
          assignees: [],
        },
      },
    };
    expect(extractKnownAuthorsFromPayload(context)).toEqual([]);
  });

  it("extracts PR author and assignees for pull_request event", () => {
    const context = {
      eventName: "pull_request",
      payload: {
        pull_request: {
          user: { login: "carol", type: "User" },
          assignees: [{ login: "dave", type: "User" }],
        },
      },
    };
    expect(extractKnownAuthorsFromPayload(context)).toEqual(["carol", "dave"]);
  });

  it("pull_request_target is handled same as pull_request", () => {
    const context = {
      eventName: "pull_request_target",
      payload: {
        pull_request: {
          user: { login: "eve", type: "User" },
          assignees: [],
        },
      },
    };
    expect(extractKnownAuthorsFromPayload(context)).toEqual(["eve"]);
  });

  it("extracts comment author and issue author for issue_comment event", () => {
    const context = {
      eventName: "issue_comment",
      payload: {
        comment: { user: { login: "frank", type: "User" } },
        issue: {
          user: { login: "grace", type: "User" },
          assignees: [],
        },
      },
    };
    expect(extractKnownAuthorsFromPayload(context)).toEqual(["frank", "grace"]);
  });

  it("extracts authors for pull_request_review_comment event", () => {
    const context = {
      eventName: "pull_request_review_comment",
      payload: {
        comment: { user: { login: "henry", type: "User" } },
        pull_request: {
          user: { login: "iris", type: "User" },
          assignees: [],
        },
      },
    };
    expect(extractKnownAuthorsFromPayload(context)).toEqual(["henry", "iris"]);
  });

  it("extracts authors for pull_request_review event", () => {
    const context = {
      eventName: "pull_request_review",
      payload: {
        review: { user: { login: "jack", type: "User" } },
        pull_request: {
          user: { login: "kate", type: "User" },
          assignees: [],
        },
      },
    };
    expect(extractKnownAuthorsFromPayload(context)).toEqual(["jack", "kate"]);
  });

  it("extracts discussion author for discussion event", () => {
    const context = {
      eventName: "discussion",
      payload: {
        discussion: { user: { login: "lily", type: "User" } },
      },
    };
    expect(extractKnownAuthorsFromPayload(context)).toEqual(["lily"]);
  });

  it("extracts comment and discussion author for discussion_comment event", () => {
    const context = {
      eventName: "discussion_comment",
      payload: {
        comment: { user: { login: "mike", type: "User" } },
        discussion: { user: { login: "nina", type: "User" } },
      },
    };
    expect(extractKnownAuthorsFromPayload(context)).toEqual(["mike", "nina"]);
  });

  it("extracts release author for release event", () => {
    const context = {
      eventName: "release",
      payload: {
        release: { author: { login: "oscar", type: "User" } },
      },
    };
    expect(extractKnownAuthorsFromPayload(context)).toEqual(["oscar"]);
  });

  it("extracts actor for workflow_dispatch event", () => {
    const context = {
      eventName: "workflow_dispatch",
      actor: "pat",
      payload: {},
    };
    expect(extractKnownAuthorsFromPayload(context)).toEqual(["pat"]);
  });

  it("skips invalid actor values for workflow_dispatch event", () => {
    const invalidActors = [123, null, undefined, ""];

    for (const actor of invalidActors) {
      const context = {
        eventName: "workflow_dispatch",
        actor,
        payload: {},
      };
      expect(extractKnownAuthorsFromPayload(context)).toEqual([]);
    }
  });

  it("returns empty array for unknown event types", () => {
    const context = {
      eventName: "unknown_event",
      payload: {},
    };
    expect(extractKnownAuthorsFromPayload(context)).toEqual([]);
  });
});

describe("resolveAllowedMentionsFromPayload", () => {
  let mockCore;
  let mockGithub;

  beforeEach(() => {
    mockCore = makeMockCore();
    mockGithub = makeMockGithub();
    vi.clearAllMocks();
  });

  it("returns empty array when context is null", async () => {
    const result = await resolveAllowedMentionsFromPayload(null, mockGithub, mockCore);
    expect(result).toEqual([]);
  });

  it("returns empty array when github is null", async () => {
    const context = { eventName: "issues", payload: {}, repo: { owner: "o", repo: "r" } };
    const result = await resolveAllowedMentionsFromPayload(context, null, mockCore);
    expect(result).toEqual([]);
  });

  it("returns empty array when core is null", async () => {
    const context = { eventName: "issues", payload: {}, repo: { owner: "o", repo: "r" } };
    const result = await resolveAllowedMentionsFromPayload(context, mockGithub, null);
    expect(result).toEqual([]);
  });

  it("returns empty array when mentions explicitly disabled", async () => {
    const context = { eventName: "issues", payload: {}, repo: { owner: "o", repo: "r" } };
    const result = await resolveAllowedMentionsFromPayload(context, mockGithub, mockCore, { enabled: false });
    expect(result).toEqual([]);
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("disabled"));
  });

  it("respects allowContext: false by skipping payload extraction", async () => {
    const context = {
      eventName: "issues",
      payload: { issue: { user: { login: "alice", type: "User" }, assignees: [] } },
      repo: { owner: "o", repo: "r" },
    };
    const result = await resolveAllowedMentionsFromPayload(context, mockGithub, mockCore, { allowContext: false, allowTeamMembers: false });
    expect(result).not.toContain("alice");
  });

  it("respects allowedCollaborators: false", async () => {
    const context = {
      eventName: "issues",
      payload: { issue: { user: { login: "alice", type: "User" }, assignees: [] } },
      repo: { owner: "o", repo: "r" },
    };
    const result = await resolveAllowedMentionsFromPayload(context, mockGithub, mockCore, {
      allowContext: false,
      allowedCollaborators: false,
      allowed: ["trusted-user"],
    });
    expect(result).toEqual(["trusted-user"]);
  });

  it("resolves mentions with team members enabled", async () => {
    const context = {
      eventName: "issues",
      payload: { issue: { user: { login: "alice", type: "User" }, assignees: [] } },
      repo: { owner: "owner", repo: "repo" },
    };
    const result = await resolveAllowedMentionsFromPayload(context, mockGithub, mockCore);
    expect(result).toContain("alice");
  });

  it("includes extra known authors", async () => {
    const context = {
      eventName: "issues",
      payload: { issue: { user: { login: "alice", type: "User" }, assignees: [] } },
      repo: { owner: "owner", repo: "repo" },
      actor: "actor",
    };
    const result = await resolveAllowedMentionsFromPayload(context, mockGithub, mockCore, undefined, ["extra-user"]);
    expect(result).toContain("extra-user");
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("extra known author"));
  });

  it("does not truncate known authors to max when allowTeamMembers is false", async () => {
    const context = {
      eventName: "issues",
      payload: {
        issue: {
          user: { login: "alice", type: "User" },
          assignees: Array.from({ length: 5 }, (_, i) => ({ login: `user${i}`, type: "User" })),
        },
      },
      repo: { owner: "o", repo: "r" },
    };
    const result = await resolveAllowedMentionsFromPayload(context, mockGithub, mockCore, {
      allowTeamMembers: false,
      max: 3,
    });
    expect(result).toEqual(["alice", "user0", "user1", "user2", "user3", "user4"]);
  });

  it("does not truncate resolved aliases to max", async () => {
    const context = {
      eventName: "workflow_dispatch",
      payload: {},
      repo: { owner: "o", repo: "r" },
    };
    const allowed = Array.from({ length: 60 }, (_, i) => `user${i}`);
    const result = await resolveAllowedMentionsFromPayload(context, mockGithub, mockCore, {
      allowContext: false,
      allowed,
      max: 3,
    });
    expect(result).toEqual(allowed);
  });

  it("returns empty array and logs warning on error", async () => {
    // Passing a context with a missing `repo` property causes the internal
    // destructuring to throw, exercising the catch branch.
    const context = {
      eventName: "issues",
      payload: { issue: { user: { login: "alice", type: "User" }, assignees: [] } },
      repo: null, // will cause "Cannot destructure property 'owner'" error
    };
    const result = await resolveAllowedMentionsFromPayload(context, mockGithub, mockCore);
    expect(result).toEqual([]);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to resolve mentions"));
  });

  it("includes allowed list from config regardless of context", async () => {
    const context = {
      eventName: "workflow_dispatch",
      actor: "actor",
      payload: {},
      repo: { owner: "owner", repo: "repo" },
    };
    const result = await resolveAllowedMentionsFromPayload(context, mockGithub, mockCore, {
      allowed: ["trusted-user"],
      allowTeamMembers: false,
    });
    expect(result).toContain("trusted-user");
  });

  it("does not include copilot by default", async () => {
    const context = {
      eventName: "workflow_dispatch",
      actor: "actor",
      payload: {},
      repo: { owner: "owner", repo: "repo" },
    };
    const result = await resolveAllowedMentionsFromPayload(context, mockGithub, mockCore, {
      allowContext: false,
      allowTeamMembers: false,
    });
    expect(result).not.toContain("copilot");
  });

  it("includes members from allowed-teams", async () => {
    const context = {
      eventName: "workflow_dispatch",
      actor: "actor",
      payload: {},
      repo: { owner: "myorg", repo: "repo" },
    };
    const mockGithubWithTeams = {
      rest: {
        teams: {
          listMembersInOrg: vi.fn(async () => ({
            data: [
              { login: "alice", type: "User" },
              { login: "bob", type: "User" },
            ],
          })),
        },
      },
    };
    const result = await resolveAllowedMentionsFromPayload(context, mockGithubWithTeams, mockCore, {
      allowedTeams: ["myorg/eng"],
      allowContext: false,
      allowTeamMembers: false,
    });
    expect(result).toContain("alice");
    expect(result).toContain("bob");
    expect(mockGithubWithTeams.rest.teams.listMembersInOrg).toHaveBeenCalledWith({
      org: "myorg",
      team_slug: "eng",
      per_page: 100,
      page: 1,
    });
  });

  it("allowed-teams with team-slug-only uses context owner", async () => {
    const context = {
      eventName: "workflow_dispatch",
      actor: "actor",
      payload: {},
      repo: { owner: "contextorg", repo: "repo" },
    };
    const mockGithubWithTeams = {
      rest: {
        teams: {
          listMembersInOrg: vi.fn(async () => ({
            data: [{ login: "charlie", type: "User" }],
          })),
        },
      },
    };
    const result = await resolveAllowedMentionsFromPayload(context, mockGithubWithTeams, mockCore, {
      allowedTeams: ["eng-team"],
      allowContext: false,
      allowTeamMembers: false,
    });
    expect(result).toContain("charlie");
    expect(mockGithubWithTeams.rest.teams.listMembersInOrg).toHaveBeenCalledWith({
      org: "contextorg",
      team_slug: "eng-team",
      per_page: 100,
      page: 1,
    });
  });

  it("allowed-teams skips bots from team members", async () => {
    const context = {
      eventName: "workflow_dispatch",
      actor: "actor",
      payload: {},
      repo: { owner: "myorg", repo: "repo" },
    };
    const mockGithubWithTeams = {
      rest: {
        teams: {
          listMembersInOrg: vi.fn(async () => ({
            data: [
              { login: "alice", type: "User" },
              { login: "bot-user", type: "Bot" },
            ],
          })),
        },
      },
    };
    const result = await resolveAllowedMentionsFromPayload(context, mockGithubWithTeams, mockCore, {
      allowedTeams: ["myorg/eng"],
      allowContext: false,
      allowTeamMembers: false,
    });
    expect(result).toContain("alice");
    expect(result).not.toContain("bot-user");
  });

  it("allowed-teams gracefully handles API errors", async () => {
    const context = {
      eventName: "workflow_dispatch",
      actor: "actor",
      payload: {},
      repo: { owner: "myorg", repo: "repo" },
    };
    const mockGithubWithTeams = {
      rest: {
        teams: {
          listMembersInOrg: vi.fn(async () => {
            throw new Error("API error");
          }),
        },
      },
    };
    const result = await resolveAllowedMentionsFromPayload(context, mockGithubWithTeams, mockCore, {
      allowedTeams: ["myorg/eng"],
      allowContext: false,
      allowTeamMembers: false,
    });
    expect(result).toEqual([]);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to fetch members for team"));
  });
});

describe("fetchTeamMembers", () => {
  let mockCore;

  beforeEach(() => {
    mockCore = makeMockCore();
    vi.clearAllMocks();
  });

  it("fetches team members with org/team-slug format", async () => {
    const mockGithub = {
      rest: {
        teams: {
          listMembersInOrg: vi.fn(async () => ({
            data: [
              { login: "alice", type: "User" },
              { login: "bob", type: "User" },
            ],
          })),
        },
      },
    };
    const result = await fetchTeamMembers("myorg/eng", "defaultorg", mockGithub, mockCore);
    expect(result).toEqual(["alice", "bob"]);
    expect(mockGithub.rest.teams.listMembersInOrg).toHaveBeenCalledWith({
      org: "myorg",
      team_slug: "eng",
      per_page: 100,
      page: 1,
    });
  });

  it("uses default org when only team-slug is given", async () => {
    const mockGithub = {
      rest: {
        teams: {
          listMembersInOrg: vi.fn(async () => ({
            data: [{ login: "charlie", type: "User" }],
          })),
        },
      },
    };
    const result = await fetchTeamMembers("eng", "defaultorg", mockGithub, mockCore);
    expect(result).toEqual(["charlie"]);
    expect(mockGithub.rest.teams.listMembersInOrg).toHaveBeenCalledWith({
      org: "defaultorg",
      team_slug: "eng",
      per_page: 100,
      page: 1,
    });
  });

  it("paginates through multiple pages to collect all members", async () => {
    const page1 = Array.from({ length: 100 }, (_, i) => ({ login: `user${i}`, type: "User" }));
    const page2 = [{ login: "last-user", type: "User" }];
    let callCount = 0;
    const mockGithub = {
      rest: {
        teams: {
          listMembersInOrg: vi.fn(async () => {
            callCount++;
            return { data: callCount === 1 ? page1 : page2 };
          }),
        },
      },
    };
    const result = await fetchTeamMembers("myorg/eng", "defaultorg", mockGithub, mockCore);
    expect(result).toHaveLength(101);
    expect(result).toContain("last-user");
    expect(mockGithub.rest.teams.listMembersInOrg).toHaveBeenCalledTimes(2);
  });

  it("excludes bots from team members", async () => {
    const mockGithub = {
      rest: {
        teams: {
          listMembersInOrg: vi.fn(async () => ({
            data: [
              { login: "alice", type: "User" },
              { login: "bot-user", type: "Bot" },
            ],
          })),
        },
      },
    };
    const result = await fetchTeamMembers("myorg/eng", "defaultorg", mockGithub, mockCore);
    expect(result).toEqual(["alice"]);
    expect(result).not.toContain("bot-user");
  });

  it("returns empty array on API error and warns", async () => {
    const mockGithub = {
      rest: {
        teams: {
          listMembersInOrg: vi.fn(async () => {
            throw new Error("Not Found");
          }),
        },
      },
    };
    const result = await fetchTeamMembers("myorg/unknown-team", "defaultorg", mockGithub, mockCore);
    expect(result).toEqual([]);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to fetch members for team myorg/unknown-team"));
  });

  it("warns with rate-limit message on HTTP 429", async () => {
    const mockGithub = {
      rest: {
        teams: {
          listMembersInOrg: vi.fn(async () => {
            const err = new Error("Too Many Requests");
            /** @type {any} */ err.status = 429;
            throw err;
          }),
        },
      },
    };
    const result = await fetchTeamMembers("myorg/eng", "defaultorg", mockGithub, mockCore);
    expect(result).toEqual([]);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Rate limit"));
  });

  it("warns with permission message on HTTP 403", async () => {
    const mockGithub = {
      rest: {
        teams: {
          listMembersInOrg: vi.fn(async () => {
            const err = new Error("Forbidden");
            /** @type {any} */ err.status = 403;
            throw err;
          }),
        },
      },
    };
    const result = await fetchTeamMembers("myorg/eng", "defaultorg", mockGithub, mockCore);
    expect(result).toEqual([]);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("read:org"));
  });

  it("warns with permission message on HTTP 404", async () => {
    const mockGithub = {
      rest: {
        teams: {
          listMembersInOrg: vi.fn(async () => {
            const err = new Error("Not Found");
            /** @type {any} */ err.status = 404;
            throw err;
          }),
        },
      },
    };
    const result = await fetchTeamMembers("myorg/eng", "defaultorg", mockGithub, mockCore);
    expect(result).toEqual([]);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("read:org"));
  });

  it("returns empty array for invalid team entry and warns", async () => {
    const mockGithub = { rest: { teams: { listMembersInOrg: vi.fn() } } };
    const result = await fetchTeamMembers("", "defaultorg", mockGithub, mockCore);
    expect(result).toEqual([]);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("invalid team entry"));
    expect(mockGithub.rest.teams.listMembersInOrg).not.toHaveBeenCalled();
  });
});
