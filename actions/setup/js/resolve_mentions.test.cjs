import { describe, it, expect, beforeEach, vi } from "vitest";
const mockCore = { info: vi.fn(), warning: vi.fn() },
  mockGithub = { rest: { repos: { listCollaborators: vi.fn(), getCollaboratorPermissionLevel: vi.fn() }, users: { getByUsername: vi.fn() } } };
((global.core = mockCore), (global.github = mockGithub));
const { extractMentions, isPayloadUserBot, getRecentCollaborators, checkUserPermission, resolveMentionsLazily } = require("./resolve_mentions.cjs");
describe("resolve_mentions.cjs", () => {
  (beforeEach(() => {
    vi.clearAllMocks();
  }),
    describe("extractMentions", () => {
      (it("should extract single mention", () => {
        const mentions = extractMentions("Hello @user");
        expect(mentions).toEqual(["user"]);
      }),
        it("should extract multiple mentions", () => {
          const mentions = extractMentions("Hello @user1 and @user2");
          expect(mentions).toEqual(["user1", "user2"]);
        }),
        it("should deduplicate mentions (case-insensitive)", () => {
          const mentions = extractMentions("Hello @user and @USER and @User");
          expect(mentions).toEqual(["user"]);
        }),
        it("should skip mentions in backticks", () => {
          const mentions = extractMentions("Hello `@user` and @realuser");
          expect(mentions).toEqual(["realuser"]);
        }),
        it("should handle org/team mentions", () => {
          const mentions = extractMentions("Hello @org/team");
          expect(mentions).toEqual(["org/team"]);
        }),
        it("should handle empty text", () => {
          const mentions = extractMentions("");
          expect(mentions).toEqual([]);
        }),
        it("should preserve original case", () => {
          const mentions = extractMentions("Hello @UserName");
          expect(mentions).toEqual(["UserName"]);
        }),
        it("should extract mentions with underscores", () => {
          const mentions = extractMentions("Hello @user_name");
          expect(mentions).toEqual(["user_name"]);
        }),
        it("should extract mentions with multiple underscores", () => {
          const mentions = extractMentions("Hello @user_name_test");
          expect(mentions).toEqual(["user_name_test"]);
        }),
        it("should extract mentions with underscores and hyphens", () => {
          const mentions = extractMentions("Hello @user-name_test");
          expect(mentions).toEqual(["user-name_test"]);
        }),
        it("should extract mentions with underscores at various positions", () => {
          const mentions = extractMentions("@test_user @_invalid @user_ @valid_user_123");
          // @_invalid: starts with underscore - not extracted (starts with non-alphanumeric)
          // @user_: ends with underscore - extracted as "user" (underscore not at end)
          // Other mentions are valid
          expect(mentions).toEqual(["test_user", "user", "valid_user_123"]);
        }),
        it("should handle org/team mentions with underscores", () => {
          const mentions = extractMentions("Hello @my_org/my_team");
          expect(mentions).toEqual(["my_org/my_team"]);
        }));
    }),
    describe("isPayloadUserBot", () => {
      (it("should return true for bot users", () => {
        expect(isPayloadUserBot({ login: "botuser", type: "Bot" })).toBe(!0);
      }),
        it("should return false for regular users", () => {
          expect(isPayloadUserBot({ login: "user", type: "User" })).toBe(!1);
        }),
        it("should return false for null/undefined", () => {
          (expect(isPayloadUserBot(null)).toBe(!1), expect(isPayloadUserBot(void 0)).toBe(!1));
        }));
    }),
    describe("getRecentCollaborators", () => {
      (it("should return map of allowed collaborators", async () => {
        mockGithub.rest.repos.listCollaborators.mockResolvedValue({
          data: [
            { login: "maintainer1", type: "User", permissions: { maintain: !0, admin: !1, push: !1 } },
            { login: "admin1", type: "User", permissions: { maintain: !1, admin: !0, push: !1 } },
            { login: "contributor1", type: "User", permissions: { maintain: !1, admin: !1, push: !0 } },
          ],
        });
        const result = await getRecentCollaborators("owner", "repo", mockGithub, mockCore);
        (expect(result.size).toBe(3), expect(result.get("maintainer1")).toBe(!0), expect(result.get("admin1")).toBe(!0), expect(result.get("contributor1")).toBe(!0));
      }),
        it("should exclude bots", async () => {
          mockGithub.rest.repos.listCollaborators.mockResolvedValue({ data: [{ login: "botuser", type: "Bot", permissions: { maintain: !0, admin: !1, push: !1 } }] });
          const result = await getRecentCollaborators("owner", "repo", mockGithub, mockCore);
          expect(result.get("botuser")).toBe(!1);
        }),
        it("should handle API errors gracefully", async () => {
          mockGithub.rest.repos.listCollaborators.mockRejectedValue(new Error("API error"));
          const result = await getRecentCollaborators("owner", "repo", mockGithub, mockCore);
          (expect(result.size).toBe(0), expect(mockCore.warning).toHaveBeenCalled());
        }),
        it("should fetch only first page (30 items)", async () => {
          (mockGithub.rest.repos.listCollaborators.mockResolvedValue({ data: [] }),
            await getRecentCollaborators("owner", "repo", mockGithub, mockCore),
            expect(mockGithub.rest.repos.listCollaborators).toHaveBeenCalledWith({ owner: "owner", repo: "repo", affiliation: "direct", per_page: 30 }));
        }));
    }),
    describe("checkUserPermission", () => {
      (it("should return true for maintainer", async () => {
        (mockGithub.rest.users.getByUsername.mockResolvedValue({ data: { login: "user", type: "User" } }), mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "maintain" } }));
        const result = await checkUserPermission("user", "owner", "repo", mockGithub, mockCore);
        expect(result).toBe(!0);
      }),
        it("should return true for admin", async () => {
          (mockGithub.rest.users.getByUsername.mockResolvedValue({ data: { login: "user", type: "User" } }), mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "admin" } }));
          const result = await checkUserPermission("user", "owner", "repo", mockGithub, mockCore);
          expect(result).toBe(!0);
        }),
        it("should return true for regular contributor", async () => {
          (mockGithub.rest.users.getByUsername.mockResolvedValue({ data: { login: "user", type: "User" } }), mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "write" } }));
          const result = await checkUserPermission("user", "owner", "repo", mockGithub, mockCore);
          expect(result).toBe(!0);
        }),
        it("should return false for bots", async () => {
          mockGithub.rest.users.getByUsername.mockResolvedValue({ data: { login: "botuser", type: "Bot" } });
          const result = await checkUserPermission("botuser", "owner", "repo", mockGithub, mockCore);
          expect(result).toBe(!1);
        }),
        it("should return false on API errors", async () => {
          mockGithub.rest.users.getByUsername.mockRejectedValue(new Error("User not found"));
          const result = await checkUserPermission("user", "owner", "repo", mockGithub, mockCore);
          expect(result).toBe(!1);
        }));
    }),
    describe("resolveMentionsLazily", () => {
      (beforeEach(() => {
        mockGithub.rest.repos.listCollaborators.mockResolvedValue({ data: [{ login: "maintainer1", type: "User", permissions: { maintain: !0, admin: !1, push: !1 } }] });
      }),
        it("should resolve known authors without API calls", async () => {
          const result = await resolveMentionsLazily("Hello @author1", ["author1"], "owner", "repo", mockGithub, mockCore);
          (expect(result.allowedMentions).toEqual(["author1"]), expect(result.resolvedCount).toBe(0));
        }),
        it("should resolve cached collaborators", async () => {
          const result = await resolveMentionsLazily("Hello @maintainer1", [], "owner", "repo", mockGithub, mockCore);
          (expect(result.allowedMentions).toEqual(["maintainer1"]), expect(result.resolvedCount).toBe(0));
        }),
        it("should query individual users not in cache", async () => {
          (mockGithub.rest.users.getByUsername.mockResolvedValue({ data: { login: "newuser", type: "User" } }), mockGithub.rest.repos.getCollaboratorPermissionLevel.mockResolvedValue({ data: { permission: "maintain" } }));
          const result = await resolveMentionsLazily("Hello @newuser", [], "owner", "repo", mockGithub, mockCore);
          (expect(result.allowedMentions).toEqual(["newuser"]), expect(result.resolvedCount).toBe(1));
        }),
        it("should limit to 50 mentions", async () => {
          const mentions = Array.from({ length: 60 }, (_, i) => `@user${i}`).join(" "),
            result = await resolveMentionsLazily(mentions, [], "owner", "repo", mockGithub, mockCore);
          (expect(result.totalMentions).toBe(60), expect(result.limitExceeded).toBe(!0), expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Mention limit exceeded")));
        }),
        it("should preserve case in allowed mentions", async () => {
          mockGithub.rest.repos.listCollaborators.mockResolvedValue({ data: [{ login: "maintainer1", type: "User", permissions: { maintain: !0, admin: !1, push: !1 } }] });
          const result = await resolveMentionsLazily("Hello @Maintainer1", [], "owner", "repo", mockGithub, mockCore);
          expect(result.allowedMentions).toEqual(["Maintainer1"]);
        }),
        it("should log resolution stats", async () => {
          (await resolveMentionsLazily("Hello @author1 @maintainer1", ["author1"], "owner", "repo", mockGithub, mockCore),
            expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Found 2 unique mentions")),
            expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Total allowed mentions")));
        }));
    }));
});
