import { describe, it, expect, beforeEach, vi } from "vitest";

// Mock core for loadTemporaryIdMap
const mockCore = {
  warning: vi.fn(),
};
global.core = mockCore;

// Mock context for loadTemporaryIdMap and resolveIssueNumber
global.context = {
  repo: {
    owner: "testowner",
    repo: "testrepo",
  },
};

describe("temporary_id.cjs", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    delete process.env.GH_AW_TEMPORARY_ID_MAP;
  });

  describe("generateTemporaryId", () => {
    it("should generate an aw_ prefixed 8-character alphanumeric string", async () => {
      const { generateTemporaryId } = await import("./temporary_id.cjs");
      const id = generateTemporaryId();
      expect(id).toMatch(/^aw_[A-Za-z0-9]{8}$/);
    });

    it("should generate unique IDs", async () => {
      const { generateTemporaryId } = await import("./temporary_id.cjs");
      const ids = new Set();
      for (let i = 0; i < 100; i++) {
        ids.add(generateTemporaryId());
      }
      expect(ids.size).toBe(100);
    });
  });

  describe("isTemporaryId", () => {
    it("should return true for valid aw_ prefixed 4-8 char alphanumeric strings", async () => {
      const { isTemporaryId } = await import("./temporary_id.cjs");
      expect(isTemporaryId("aw_abc1")).toBe(true);
      expect(isTemporaryId("aw_Test123")).toBe(true);
      expect(isTemporaryId("aw_A1B2C3D4")).toBe(true);
      expect(isTemporaryId("aw_12345678")).toBe(true);
      expect(isTemporaryId("aw_ABCD")).toBe(true);
      expect(isTemporaryId("aw_xyz9")).toBe(true);
    });

    it("should return false for invalid strings", async () => {
      const { isTemporaryId } = await import("./temporary_id.cjs");
      expect(isTemporaryId("abc123def456")).toBe(false); // Missing aw_ prefix
      expect(isTemporaryId("aw_abc")).toBe(false); // Too short (3 chars)
      expect(isTemporaryId("aw_123456789")).toBe(false); // Too long (9 chars)
      expect(isTemporaryId("aw_test-id")).toBe(false); // Contains hyphen
      expect(isTemporaryId("aw_id_123")).toBe(false); // Contains underscore
      expect(isTemporaryId("")).toBe(false);
      expect(isTemporaryId("temp_abc123")).toBe(false); // Wrong prefix
    });

    it("should return false for non-string values", async () => {
      const { isTemporaryId } = await import("./temporary_id.cjs");
      expect(isTemporaryId(123)).toBe(false);
      expect(isTemporaryId(null)).toBe(false);
      expect(isTemporaryId(undefined)).toBe(false);
      expect(isTemporaryId({})).toBe(false);
    });
  });

  describe("normalizeTemporaryId", () => {
    it("should convert to lowercase", async () => {
      const { normalizeTemporaryId } = await import("./temporary_id.cjs");
      expect(normalizeTemporaryId("aw_ABC123")).toBe("aw_abc123");
      expect(normalizeTemporaryId("AW_Test123")).toBe("aw_test123");
    });
  });

  describe("replaceTemporaryIdReferences", () => {
    it("should replace #aw_ID with issue numbers (same repo)", async () => {
      const { replaceTemporaryIdReferences } = await import("./temporary_id.cjs");
      const map = new Map([["aw_abc123", { repo: "owner/repo", number: 100 }]]);
      const text = "Check #aw_abc123 for details";
      expect(replaceTemporaryIdReferences(text, map, "owner/repo")).toBe("Check #100 for details");
    });

    it("should replace #aw_ID with full reference (cross-repo)", async () => {
      const { replaceTemporaryIdReferences } = await import("./temporary_id.cjs");
      const map = new Map([["aw_abc123", { repo: "other/repo", number: 100 }]]);
      const text = "Check #aw_abc123 for details";
      expect(replaceTemporaryIdReferences(text, map, "owner/repo")).toBe("Check other/repo#100 for details");
    });

    it("should handle multiple references", async () => {
      const { replaceTemporaryIdReferences } = await import("./temporary_id.cjs");
      const map = new Map([
        ["aw_abc123", { repo: "owner/repo", number: 100 }],
        ["aw_test123", { repo: "owner/repo", number: 200 }],
      ]);
      const text = "See #aw_abc123 and #aw_Test123";
      expect(replaceTemporaryIdReferences(text, map, "owner/repo")).toBe("See #100 and #200");
    });

    it("should preserve unresolved references", async () => {
      const { replaceTemporaryIdReferences } = await import("./temporary_id.cjs");
      const map = new Map();
      const text = "Check #aw_abc123 for details";
      expect(replaceTemporaryIdReferences(text, map, "owner/repo")).toBe("Check #aw_abc123 for details");
    });

    it("should be case-insensitive", async () => {
      const { replaceTemporaryIdReferences } = await import("./temporary_id.cjs");
      const map = new Map([["aw_abc123", { repo: "owner/repo", number: 100 }]]);
      const text = "Check #AW_ABC123 for details";
      expect(replaceTemporaryIdReferences(text, map, "owner/repo")).toBe("Check #100 for details");
    });

    it("should not match invalid temporary ID formats", async () => {
      const { replaceTemporaryIdReferences } = await import("./temporary_id.cjs");
      const map = new Map([["aw_abc123", { repo: "owner/repo", number: 100 }]]);
      const text = "Check #aw_abc and #temp:abc123 for details";
      expect(replaceTemporaryIdReferences(text, map, "owner/repo")).toBe("Check #aw_abc and #temp:abc123 for details");
    });
  });

  describe("replaceTemporaryIdReferencesLegacy", () => {
    it("should replace #aw_ID with issue numbers", async () => {
      const { replaceTemporaryIdReferencesLegacy } = await import("./temporary_id.cjs");
      const map = new Map([["aw_abc123", 100]]);
      const text = "Check #aw_abc123 for details";
      expect(replaceTemporaryIdReferencesLegacy(text, map)).toBe("Check #100 for details");
    });
  });

  describe("loadTemporaryIdMap", () => {
    it("should return empty map when env var is not set", async () => {
      const { loadTemporaryIdMap } = await import("./temporary_id.cjs");
      const map = loadTemporaryIdMap();
      expect(map.size).toBe(0);
    });

    it("should return empty map when env var is empty object", async () => {
      process.env.GH_AW_TEMPORARY_ID_MAP = "{}";
      const { loadTemporaryIdMap } = await import("./temporary_id.cjs");
      const map = loadTemporaryIdMap();
      expect(map.size).toBe(0);
    });

    it("should parse legacy format (number only)", async () => {
      process.env.GH_AW_TEMPORARY_ID_MAP = JSON.stringify({ aw_abc123: 100, aw_test123: 200 });
      const { loadTemporaryIdMap } = await import("./temporary_id.cjs");
      const map = loadTemporaryIdMap();
      expect(map.size).toBe(2);
      expect(map.get("aw_abc123")).toEqual({ repo: "testowner/testrepo", number: 100 });
      expect(map.get("aw_test123")).toEqual({ repo: "testowner/testrepo", number: 200 });
    });

    it("should parse new format (repo, number)", async () => {
      process.env.GH_AW_TEMPORARY_ID_MAP = JSON.stringify({
        aw_abc123: { repo: "owner/repo", number: 100 },
        aw_test123: { repo: "other/repo", number: 200 },
      });
      const { loadTemporaryIdMap } = await import("./temporary_id.cjs");
      const map = loadTemporaryIdMap();
      expect(map.size).toBe(2);
      expect(map.get("aw_abc123")).toEqual({ repo: "owner/repo", number: 100 });
      expect(map.get("aw_test123")).toEqual({ repo: "other/repo", number: 200 });
    });

    it("should normalize keys to lowercase", async () => {
      process.env.GH_AW_TEMPORARY_ID_MAP = JSON.stringify({ AW_ABC123: { repo: "owner/repo", number: 100 } });
      const { loadTemporaryIdMap } = await import("./temporary_id.cjs");
      const map = loadTemporaryIdMap();
      expect(map.get("aw_abc123")).toEqual({ repo: "owner/repo", number: 100 });
    });

    it("should warn and return empty map on invalid JSON", async () => {
      process.env.GH_AW_TEMPORARY_ID_MAP = "not valid json";
      const { loadTemporaryIdMap } = await import("./temporary_id.cjs");
      const map = loadTemporaryIdMap();
      expect(map.size).toBe(0);
      expect(mockCore.warning).toHaveBeenCalled();
    });
  });

  describe("resolveIssueNumber", () => {
    it("should return error for null value", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map();
      const result = resolveIssueNumber(null, map);
      expect(result.resolved).toBe(null);
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toBe("Issue number is missing");
    });

    it("should return error for undefined value", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map();
      const result = resolveIssueNumber(undefined, map);
      expect(result.resolved).toBe(null);
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toBe("Issue number is missing");
    });

    it("should resolve temporary ID from map", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map([["aw_abc123", { repo: "owner/repo", number: 100 }]]);
      const result = resolveIssueNumber("aw_abc123", map);
      expect(result.resolved).toEqual({ repo: "owner/repo", number: 100 });
      expect(result.wasTemporaryId).toBe(true);
      expect(result.errorMessage).toBe(null);
    });

    it("should return error for unresolved temporary ID", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map();
      const result = resolveIssueNumber("aw_abc123", map);
      expect(result.resolved).toBe(null);
      expect(result.wasTemporaryId).toBe(true);
      expect(result.errorMessage).toContain("Temporary ID 'aw_abc123' not found in map");
    });

    it("should handle numeric issue numbers", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map();
      const result = resolveIssueNumber(123, map);
      expect(result.resolved).toEqual({ repo: "testowner/testrepo", number: 123 });
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toBe(null);
    });

    it("should handle string issue numbers", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map();
      const result = resolveIssueNumber("456", map);
      expect(result.resolved).toEqual({ repo: "testowner/testrepo", number: 456 });
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toBe(null);
    });

    it("should return error for invalid issue number", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map();
      const result = resolveIssueNumber("invalid", map);
      expect(result.resolved).toBe(null);
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toContain("Invalid issue number: invalid");
    });

    it("should return error for zero issue number", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map();
      const result = resolveIssueNumber(0, map);
      expect(result.resolved).toBe(null);
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toContain("Invalid issue number: 0");
    });

    it("should return error for negative issue number", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map();
      const result = resolveIssueNumber(-5, map);
      expect(result.resolved).toBe(null);
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toContain("Invalid issue number: -5");
    });

    it("should return specific error for malformed temporary ID (contains non-alphanumeric chars)", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map();
      const result = resolveIssueNumber("aw_test-id", map);
      expect(result.resolved).toBe(null);
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toContain("Invalid temporary ID format");
      expect(result.errorMessage).toContain("aw_test-id");
      expect(result.errorMessage).toContain("4 to 8 alphanumeric characters");
    });

    it("should return specific error for malformed temporary ID (too short)", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map();
      const result = resolveIssueNumber("aw_abc", map);
      expect(result.resolved).toBe(null);
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toContain("Invalid temporary ID format");
      expect(result.errorMessage).toContain("aw_abc");
    });

    it("should return specific error for malformed temporary ID (too long)", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map();
      const result = resolveIssueNumber("aw_abc123456789", map);
      expect(result.resolved).toBe(null);
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toContain("Invalid temporary ID format");
      expect(result.errorMessage).toContain("aw_abc123456789");
    });

    it("should handle temporary ID with # prefix", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map([["aw_abc123", { repo: "owner/repo", number: 100 }]]);
      const result = resolveIssueNumber("#aw_abc123", map);
      expect(result.resolved).toEqual({ repo: "owner/repo", number: 100 });
      expect(result.wasTemporaryId).toBe(true);
      expect(result.errorMessage).toBe(null);
    });

    it("should handle issue number with # prefix", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map();
      const result = resolveIssueNumber("#123", map);
      expect(result.resolved).toEqual({ repo: "testowner/testrepo", number: 123 });
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toBe(null);
    });

    it("should handle malformed temporary ID with # prefix", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map();
      const result = resolveIssueNumber("#aw_test-id", map);
      expect(result.resolved).toBe(null);
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toContain("Invalid temporary ID format");
      expect(result.errorMessage).toContain("#aw_test-id");
    });
  });

  describe("serializeTemporaryIdMap", () => {
    it("should serialize map to JSON", async () => {
      const { serializeTemporaryIdMap } = await import("./temporary_id.cjs");
      const map = new Map([
        ["aw_abc123", { repo: "owner/repo", number: 100 }],
        ["aw_test123", { repo: "other/repo", number: 200 }],
      ]);
      const result = serializeTemporaryIdMap(map);
      const parsed = JSON.parse(result);
      expect(parsed).toEqual({
        aw_abc123: { repo: "owner/repo", number: 100 },
        aw_test123: { repo: "other/repo", number: 200 },
      });
    });
  });

  describe("hasUnresolvedTemporaryIds", () => {
    it("should return false when text has no temporary IDs", async () => {
      const { hasUnresolvedTemporaryIds } = await import("./temporary_id.cjs");
      const map = new Map();
      expect(hasUnresolvedTemporaryIds("Regular text without temp IDs", map)).toBe(false);
    });

    it("should return false when all temporary IDs are resolved", async () => {
      const { hasUnresolvedTemporaryIds } = await import("./temporary_id.cjs");
      const map = new Map([
        ["aw_abc123", { repo: "owner/repo", number: 100 }],
        ["aw_test123", { repo: "other/repo", number: 200 }],
      ]);
      const text = "See #aw_abc123 and #aw_Test123 for details";
      expect(hasUnresolvedTemporaryIds(text, map)).toBe(false);
    });

    it("should return true when text has unresolved temporary IDs", async () => {
      const { hasUnresolvedTemporaryIds } = await import("./temporary_id.cjs");
      const map = new Map([["aw_abc123", { repo: "owner/repo", number: 100 }]]);
      const text = "See #aw_abc123 and #aw_unresol for details";
      expect(hasUnresolvedTemporaryIds(text, map)).toBe(true);
    });

    it("should return true when text has only unresolved temporary IDs", async () => {
      const { hasUnresolvedTemporaryIds } = await import("./temporary_id.cjs");
      const map = new Map();
      const text = "Check #aw_abc123 for details";
      expect(hasUnresolvedTemporaryIds(text, map)).toBe(true);
    });

    it("should work with plain object tempIdMap", async () => {
      const { hasUnresolvedTemporaryIds } = await import("./temporary_id.cjs");
      const obj = {
        aw_abc123: { repo: "owner/repo", number: 100 },
      };
      const text = "See #aw_abc123 and #aw_unresol for details";
      expect(hasUnresolvedTemporaryIds(text, obj)).toBe(true);
    });

    it("should handle case-insensitive temporary IDs", async () => {
      const { hasUnresolvedTemporaryIds } = await import("./temporary_id.cjs");
      const map = new Map([["aw_abc123", { repo: "owner/repo", number: 100 }]]);
      const text = "See #AW_ABC123 for details";
      expect(hasUnresolvedTemporaryIds(text, map)).toBe(false);
    });

    it("should return false for empty or null text", async () => {
      const { hasUnresolvedTemporaryIds } = await import("./temporary_id.cjs");
      const map = new Map();
      expect(hasUnresolvedTemporaryIds("", map)).toBe(false);
      expect(hasUnresolvedTemporaryIds(null, map)).toBe(false);
      expect(hasUnresolvedTemporaryIds(undefined, map)).toBe(false);
    });

    it("should handle multiple unresolved IDs", async () => {
      const { hasUnresolvedTemporaryIds } = await import("./temporary_id.cjs");
      const map = new Map();
      const text = "See #aw_abc123, #aw_test123, and #aw_xyz9";
      expect(hasUnresolvedTemporaryIds(text, map)).toBe(true);
    });
  });

  describe("replaceTemporaryProjectReferences", () => {
    it("should replace #aw_ID with project URLs", async () => {
      const { replaceTemporaryProjectReferences } = await import("./temporary_id.cjs");
      const map = new Map([["aw_abc123", "https://github.com/orgs/myorg/projects/123"]]);
      const text = "Project created: #aw_abc123";
      expect(replaceTemporaryProjectReferences(text, map)).toBe("Project created: https://github.com/orgs/myorg/projects/123");
    });

    it("should handle multiple project references", async () => {
      const { replaceTemporaryProjectReferences } = await import("./temporary_id.cjs");
      const map = new Map([
        ["aw_abc123", "https://github.com/orgs/myorg/projects/123"],
        ["aw_test123", "https://github.com/orgs/myorg/projects/456"],
      ]);
      const text = "See #aw_abc123 and #aw_Test123";
      expect(replaceTemporaryProjectReferences(text, map)).toBe("See https://github.com/orgs/myorg/projects/123 and https://github.com/orgs/myorg/projects/456");
    });

    it("should leave unresolved project references unchanged", async () => {
      const { replaceTemporaryProjectReferences } = await import("./temporary_id.cjs");
      const map = new Map([["aw_abc123", "https://github.com/orgs/myorg/projects/123"]]);
      const text = "See #aw_unresol";
      expect(replaceTemporaryProjectReferences(text, map)).toBe("See #aw_unresol");
    });

    it("should be case insensitive", async () => {
      const { replaceTemporaryProjectReferences } = await import("./temporary_id.cjs");
      const map = new Map([["aw_abc123", "https://github.com/orgs/myorg/projects/123"]]);
      const text = "Project: #AW_ABC123";
      expect(replaceTemporaryProjectReferences(text, map)).toBe("Project: https://github.com/orgs/myorg/projects/123");
    });
  });

  describe("loadTemporaryProjectMap", () => {
    it("should return empty map when env var is not set", async () => {
      delete process.env.GH_AW_TEMPORARY_PROJECT_MAP;
      const { loadTemporaryProjectMap } = await import("./temporary_id.cjs");
      const map = loadTemporaryProjectMap();
      expect(map.size).toBe(0);
    });

    it("should load project map from environment", async () => {
      process.env.GH_AW_TEMPORARY_PROJECT_MAP = JSON.stringify({
        aw_abc123: "https://github.com/orgs/myorg/projects/123",
        aw_test123: "https://github.com/users/jdoe/projects/456",
      });
      const { loadTemporaryProjectMap } = await import("./temporary_id.cjs");
      const map = loadTemporaryProjectMap();
      expect(map.size).toBe(2);
      expect(map.get("aw_abc123")).toBe("https://github.com/orgs/myorg/projects/123");
      expect(map.get("aw_test123")).toBe("https://github.com/users/jdoe/projects/456");
    });

    it("should normalize keys to lowercase", async () => {
      process.env.GH_AW_TEMPORARY_PROJECT_MAP = JSON.stringify({
        AW_ABC123: "https://github.com/orgs/myorg/projects/123",
      });
      const { loadTemporaryProjectMap } = await import("./temporary_id.cjs");
      const map = loadTemporaryProjectMap();
      expect(map.get("aw_abc123")).toBe("https://github.com/orgs/myorg/projects/123");
    });
  });

  describe("extractTemporaryIdReferences", () => {
    it("should extract temporary IDs from body field", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const message = {
        type: "create_issue",
        title: "Test Issue",
        body: "See #aw_abc123 and #aw_test123 for details",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(2);
      expect(refs.has("aw_abc123")).toBe(true);
      expect(refs.has("aw_test123")).toBe(true);
    });

    it("should extract temporary IDs from title field", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const message = {
        type: "create_issue",
        title: "Follow up to #aw_abc123",
        body: "Details here",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(1);
      expect(refs.has("aw_abc123")).toBe(true);
    });

    it("should extract temporary IDs from direct ID fields", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const message = {
        type: "link_sub_issue",
        parent_issue_number: "aw_aaaa12",
        sub_issue_number: "aw_bbbb12",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(2);
      expect(refs.has("aw_aaaa12")).toBe(true);
      expect(refs.has("aw_bbbb12")).toBe(true);
    });

    it("should handle # prefix in ID fields", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const message = {
        type: "add_comment",
        issue_number: "#aw_abc123",
        body: "Comment text",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(1);
      expect(refs.has("aw_abc123")).toBe(true);
    });

    it("should normalize temporary IDs to lowercase", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const message = {
        type: "create_issue",
        body: "See #AW_ABC123",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(1);
      expect(refs.has("aw_abc123")).toBe(true);
    });

    it("should extract from items array for bulk operations", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const message = {
        type: "add_comment",
        items: [
          { issue_number: "aw_dddd11", body: "Comment 1" },
          { issue_number: "aw_eeee22", body: "Comment 2" },
        ],
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(2);
      expect(refs.has("aw_dddd11")).toBe(true);
      expect(refs.has("aw_eeee22")).toBe(true);
    });

    it("should return empty set for messages without temp IDs", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const message = {
        type: "create_issue",
        title: "Regular Issue",
        body: "No temporary IDs here",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(0);
    });

    it("should extract temporary IDs from item_url field (full URL format)", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const message = {
        type: "create_project",
        title: "Test Project",
        item_url: "https://github.com/owner/repo/issues/aw_abc123",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(1);
      expect(refs.has("aw_abc123")).toBe(true);
    });

    it("should extract temporary IDs from item_url field (with # prefix)", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const message = {
        type: "create_project",
        title: "Test Project",
        item_url: "https://github.com/owner/repo/issues/#aw_abc123",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(1);
      expect(refs.has("aw_abc123")).toBe(true);
    });

    it("should extract temporary IDs from item_url field (plain ID format)", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const message = {
        type: "create_project",
        title: "Test Project",
        item_url: "aw_abc123",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(1);
      expect(refs.has("aw_abc123")).toBe(true);
    });

    it("should extract temporary IDs from item_url field (plain ID with # prefix)", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const message = {
        type: "create_project",
        title: "Test Project",
        item_url: "#aw_abc123",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(1);
      expect(refs.has("aw_abc123")).toBe(true);
    });

    it("should not extract from item_url with regular issue number", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const message = {
        type: "create_project",
        title: "Test Project",
        item_url: "https://github.com/owner/repo/issues/123",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(0);
    });

    it("should extract temporary IDs from content_number field", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const message = {
        type: "update_project",
        project: "https://github.com/orgs/myorg/projects/1",
        content_type: "issue",
        content_number: "aw_abc123",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(1);
      expect(refs.has("aw_abc123")).toBe(true);
    });

    it("should extract temporary IDs from content_number field (with # prefix)", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const message = {
        type: "update_project",
        project: "https://github.com/orgs/myorg/projects/1",
        content_type: "issue",
        content_number: "#aw_abc123",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(1);
      expect(refs.has("aw_abc123")).toBe(true);
    });

    it("should ignore invalid temporary ID formats", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const message = {
        type: "create_issue",
        body: "Invalid: #aw_ab #aw- #temp_123456",
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(0);
    });
  });

  describe("getCreatedTemporaryId", () => {
    it("should return temporary_id when present and valid", async () => {
      const { getCreatedTemporaryId } = await import("./temporary_id.cjs");

      const message = {
        type: "create_issue",
        temporary_id: "aw_abc123",
        title: "Test",
      };

      const created = getCreatedTemporaryId(message);

      expect(created).toBe("aw_abc123");
    });

    it("should normalize created temporary ID to lowercase", async () => {
      const { getCreatedTemporaryId } = await import("./temporary_id.cjs");

      const message = {
        type: "create_issue",
        temporary_id: "AW_ABC123",
        title: "Test",
      };

      const created = getCreatedTemporaryId(message);

      expect(created).toBe("aw_abc123");
    });

    it("should return null when temporary_id is missing", async () => {
      const { getCreatedTemporaryId } = await import("./temporary_id.cjs");

      const message = {
        type: "create_issue",
        title: "Test",
      };

      const created = getCreatedTemporaryId(message);

      expect(created).toBe(null);
    });

    it("should return null when temporary_id is invalid", async () => {
      const { getCreatedTemporaryId } = await import("./temporary_id.cjs");

      const message = {
        type: "create_issue",
        temporary_id: "invalid_id",
        title: "Test",
      };

      const created = getCreatedTemporaryId(message);

      expect(created).toBe(null);
    });
  });
});
