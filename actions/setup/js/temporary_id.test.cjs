import { describe, it, expect, beforeEach, vi } from "vitest";

// Mock core for loadTemporaryIdMap
const mockCore = {
  info: vi.fn(),
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
    it("should return true for valid aw_ prefixed 3-12 char alphanumeric strings", async () => {
      const { isTemporaryId } = await import("./temporary_id.cjs");
      expect(isTemporaryId("aw_abc")).toBe(true);
      expect(isTemporaryId("aw_abc1")).toBe(true);
      expect(isTemporaryId("aw_Test123")).toBe(true);
      expect(isTemporaryId("aw_A1B2C3D4")).toBe(true);
      expect(isTemporaryId("aw_12345678")).toBe(true);
      expect(isTemporaryId("aw_ABCD")).toBe(true);
      expect(isTemporaryId("aw_xyz9")).toBe(true);
      expect(isTemporaryId("aw_xyz")).toBe(true);
      expect(isTemporaryId("aw_123456789")).toBe(true); // 9 chars - valid with extended limit
      expect(isTemporaryId("aw_123456789abc")).toBe(true); // 12 chars - at the limit
    });

    it("should return true for valid #aw_ prefixed strings (canonical form)", async () => {
      const { isTemporaryId } = await import("./temporary_id.cjs");
      expect(isTemporaryId("#aw_abc")).toBe(true);
      expect(isTemporaryId("#aw_abc1")).toBe(true);
      expect(isTemporaryId("#aw_pr_fix")).toBe(true);
      expect(isTemporaryId("#aw_123456789abc")).toBe(true); // 12 chars - at the limit
    });

    it("should return true for valid aw_ prefixed strings with underscores", async () => {
      const { isTemporaryId } = await import("./temporary_id.cjs");
      expect(isTemporaryId("aw_id_123")).toBe(true); // Contains underscore - now valid
      expect(isTemporaryId("aw_pr_fix")).toBe(true); // Underscore-separated words
      expect(isTemporaryId("aw_pr_testfix")).toBe(true); // From the original issue
    });

    it("should return false for invalid strings", async () => {
      const { isTemporaryId } = await import("./temporary_id.cjs");
      expect(isTemporaryId("abc123def456")).toBe(false); // Missing aw_ prefix
      expect(isTemporaryId("aw_ab")).toBe(false); // Too short (2 chars)
      expect(isTemporaryId("aw_1234567890abc")).toBe(false); // Too long (13 chars)
      expect(isTemporaryId("aw_test-id")).toBe(false); // Contains hyphen
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

    it("should strip leading # before lowercasing", async () => {
      const { normalizeTemporaryId } = await import("./temporary_id.cjs");
      expect(normalizeTemporaryId("#aw_ABC123")).toBe("aw_abc123");
      expect(normalizeTemporaryId("#aw_pr_fix")).toBe("aw_pr_fix");
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
      const text = "Check #aw_ab and #temp:abc123 for details";
      expect(replaceTemporaryIdReferences(text, map, "owner/repo")).toBe("Check #aw_ab and #temp:abc123 for details");
    });

    it("should warn about malformed temporary ID reference that is too short", async () => {
      const { replaceTemporaryIdReferences } = await import("./temporary_id.cjs");
      const map = new Map();
      const text = "Check #aw_ab for details";
      const result = replaceTemporaryIdReferences(text, map, "owner/repo");
      expect(result).toBe("Check #aw_ab for details");
      expect(mockCore.warning).toHaveBeenCalledOnce();
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("#aw_ab"));
    });

    it("should warn about malformed temporary ID reference that is too long", async () => {
      const { replaceTemporaryIdReferences } = await import("./temporary_id.cjs");
      const map = new Map();
      const text = "Check #aw_toolongname123 for details";
      const result = replaceTemporaryIdReferences(text, map, "owner/repo");
      expect(result).toBe("Check #aw_toolongname123 for details");
      expect(mockCore.warning).toHaveBeenCalledOnce();
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("#aw_toolongname123"));
    });

    it("should not warn for valid temporary ID references", async () => {
      const { replaceTemporaryIdReferences } = await import("./temporary_id.cjs");
      const map = new Map([["aw_abc123", { repo: "owner/repo", number: 100 }]]);
      const text = "Check #aw_abc123 for details";
      replaceTemporaryIdReferences(text, map, "owner/repo");
      expect(mockCore.warning).not.toHaveBeenCalled();
    });

    it("should not warn for valid unresolved temporary ID references", async () => {
      const { replaceTemporaryIdReferences } = await import("./temporary_id.cjs");
      const map = new Map();
      const text = "Check #aw_abc123 for details";
      replaceTemporaryIdReferences(text, map, "owner/repo");
      expect(mockCore.warning).not.toHaveBeenCalled();
    });

    it("should warn once per malformed reference when multiple are present", async () => {
      const { replaceTemporaryIdReferences } = await import("./temporary_id.cjs");
      const map = new Map();
      const text = "See #aw_ab and #aw_toolongname123 here";
      replaceTemporaryIdReferences(text, map, "owner/repo");
      expect(mockCore.warning).toHaveBeenCalledTimes(2);
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("#aw_ab"));
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("#aw_toolongname123"));
    });

    it("should warn about malformed temporary ID reference containing a hyphen", async () => {
      const { replaceTemporaryIdReferences } = await import("./temporary_id.cjs");
      const map = new Map();
      const text = "Check #aw_test-id for details";
      const result = replaceTemporaryIdReferences(text, map, "owner/repo");
      expect(result).toBe("Check #aw_test-id for details");
      expect(mockCore.warning).toHaveBeenCalledOnce();
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("#aw_test-id"));
    });
  });

  describe("replaceTemporaryIdReferencesInPatch", () => {
    it("should replace #aw_ID in text context within patch content", async () => {
      const { replaceTemporaryIdReferencesInPatch } = await import("./temporary_id.cjs");
      const map = new Map([["aw_abc123", { repo: "owner/repo", number: 100 }]]);
      const text = '+    [QuarantinedTest("#aw_abc123")]';
      expect(replaceTemporaryIdReferencesInPatch(text, map, "owner/repo")).toBe('+    [QuarantinedTest("#100")]');
    });

    it("should replace #aw_ID in URL context without '#' prefix", async () => {
      const { replaceTemporaryIdReferencesInPatch } = await import("./temporary_id.cjs");
      const map = new Map([["aw_navqry1", { repo: "dotnet/aspnetcore", number: 66195 }]]);
      const text = '+    [QuarantinedTest("https://github.com/dotnet/aspnetcore/issues/#aw_navqry1")]';
      expect(replaceTemporaryIdReferencesInPatch(text, map, "dotnet/aspnetcore")).toBe('+    [QuarantinedTest("https://github.com/dotnet/aspnetcore/issues/66195")]');
    });

    it("should handle mixed URL and text context references", async () => {
      const { replaceTemporaryIdReferencesInPatch } = await import("./temporary_id.cjs");
      const map = new Map([["aw_issue1", { repo: "owner/repo", number: 42 }]]);
      const text = "URL: https://github.com/owner/repo/issues/#aw_issue1 and ref #aw_issue1";
      expect(replaceTemporaryIdReferencesInPatch(text, map, "owner/repo")).toBe("URL: https://github.com/owner/repo/issues/42 and ref #42");
    });

    it("should preserve unresolved references in patch content", async () => {
      const { replaceTemporaryIdReferencesInPatch } = await import("./temporary_id.cjs");
      const map = new Map();
      const text = "+    link: https://github.com/owner/repo/issues/#aw_unknown1";
      expect(replaceTemporaryIdReferencesInPatch(text, map, "owner/repo")).toBe("+    link: https://github.com/owner/repo/issues/#aw_unknown1");
    });

    it("should handle cross-repo references in text context", async () => {
      const { replaceTemporaryIdReferencesInPatch } = await import("./temporary_id.cjs");
      const map = new Map([["aw_abc123", { repo: "other/repo", number: 100 }]]);
      const text = "See #aw_abc123 for details";
      expect(replaceTemporaryIdReferencesInPatch(text, map, "owner/repo")).toBe("See other/repo#100 for details");
    });

    it("should be case-insensitive for URL context", async () => {
      const { replaceTemporaryIdReferencesInPatch } = await import("./temporary_id.cjs");
      const map = new Map([["aw_abc123", { repo: "owner/repo", number: 100 }]]);
      const text = "https://github.com/owner/repo/issues/#AW_ABC123";
      expect(replaceTemporaryIdReferencesInPatch(text, map, "owner/repo")).toBe("https://github.com/owner/repo/issues/100");
    });

    it("should handle multiline patch content", async () => {
      const { replaceTemporaryIdReferencesInPatch } = await import("./temporary_id.cjs");
      const map = new Map([["aw_navqry1", { repo: "dotnet/aspnetcore", number: 66195 }]]);
      const text = ["diff --git a/test.cs b/test.cs", "--- a/test.cs", "+++ b/test.cs", "@@ -1,3 +1,4 @@", " existing line", '+[QuarantinedTest("https://github.com/dotnet/aspnetcore/issues/#aw_navqry1")]', " another line"].join("\n");
      expect(replaceTemporaryIdReferencesInPatch(text, map, "dotnet/aspnetcore")).toContain('QuarantinedTest("https://github.com/dotnet/aspnetcore/issues/66195")');
    });
  });

  describe("getOrGenerateTemporaryId", () => {
    it("should auto-generate a temporary ID when not provided", async () => {
      const { getOrGenerateTemporaryId } = await import("./temporary_id.cjs");
      const result = getOrGenerateTemporaryId({ title: "Test" });
      expect(result.error).toBeNull();
      expect(result.temporaryId).toMatch(/^aw_[A-Za-z0-9]{8}$/);
    });

    it("should return valid temporary ID when provided", async () => {
      const { getOrGenerateTemporaryId } = await import("./temporary_id.cjs");
      const result = getOrGenerateTemporaryId({ temporary_id: "aw_abc123" });
      expect(result.error).toBeNull();
      expect(result.temporaryId).toBe("aw_abc123");
    });

    it("should accept temporary ID with underscores", async () => {
      const { getOrGenerateTemporaryId } = await import("./temporary_id.cjs");
      const result = getOrGenerateTemporaryId({ temporary_id: "aw_pr_fix" });
      expect(result.error).toBeNull();
      expect(result.temporaryId).toBe("aw_pr_fix");
    });

    it("should accept the aw_pr_testfix format from the original issue", async () => {
      const { getOrGenerateTemporaryId } = await import("./temporary_id.cjs");
      const result = getOrGenerateTemporaryId({ temporary_id: "aw_pr_testfix" });
      expect(result.error).toBeNull();
      expect(result.temporaryId).toBe("aw_pr_testfix");
    });

    it("should warn and auto-generate when format is invalid instead of failing", async () => {
      const { getOrGenerateTemporaryId } = await import("./temporary_id.cjs");
      const result = getOrGenerateTemporaryId({ temporary_id: "aw_toolongidentifier123" });
      expect(result.error).toBeNull();
      expect(result.temporaryId).toMatch(/^aw_[A-Za-z0-9]{8}$/);
      expect(mockCore.warning).toHaveBeenCalledOnce();
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("aw_toolongidentifier123"));
    });

    it("should return error when temporary_id is not a string", async () => {
      const { getOrGenerateTemporaryId } = await import("./temporary_id.cjs");
      const result = getOrGenerateTemporaryId({ temporary_id: 123 });
      expect(result.error).toContain("temporary_id must be a string");
      expect(result.temporaryId).toBeNull();
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
      expect(result.errorMessage).toContain("3 to 12 alphanumeric or underscore characters");
    });

    it("should return specific error for malformed temporary ID (too short)", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map();
      const result = resolveIssueNumber("aw_ab", map);
      expect(result.resolved).toBe(null);
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toContain("Invalid temporary ID format");
      expect(result.errorMessage).toContain("aw_ab");
    });

    it("should return specific error for malformed temporary ID (too long)", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map();
      const result = resolveIssueNumber("aw_abc1234567890", map); // 13 chars - too long
      expect(result.resolved).toBe(null);
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toContain("Invalid temporary ID format");
      expect(result.errorMessage).toContain("aw_abc1234567890");
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

    it("should strip surrounding double quotes from temporary ID", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map([["aw_blazwasm", { repo: "owner/repo", number: 42 }]]);
      const result = resolveIssueNumber('"aw_blazwasm"', map);
      expect(result.resolved).toEqual({ repo: "owner/repo", number: 42 });
      expect(result.wasTemporaryId).toBe(true);
      expect(result.errorMessage).toBe(null);
    });

    it("should strip surrounding single quotes from temporary ID", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map([["aw_foo123", { repo: "owner/repo", number: 7 }]]);
      const result = resolveIssueNumber("'aw_foo123'", map);
      expect(result.resolved).toEqual({ repo: "owner/repo", number: 7 });
      expect(result.wasTemporaryId).toBe(true);
      expect(result.errorMessage).toBe(null);
    });

    it("should strip surrounding double quotes from numeric issue number", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map();
      const result = resolveIssueNumber('"789"', map);
      expect(result.resolved).toEqual({ repo: "testowner/testrepo", number: 789 });
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toBe(null);
    });

    it("should not strip mismatched surrounding quotes from temporary ID", async () => {
      const { resolveIssueNumber } = await import("./temporary_id.cjs");
      const map = new Map();
      const result = resolveIssueNumber("\"aw_foo123'", map);
      expect(result.resolved).toBe(null);
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).not.toBe(null);
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

    it("should treat ID as resolved when present in artifactUrlMap", async () => {
      const { hasUnresolvedTemporaryIds } = await import("./temporary_id.cjs");
      const tempIdMap = new Map();
      const artifactUrlMap = new Map([["aw_chart1", "https://github.com/owner/repo/actions/runs/1/artifacts/42"]]);
      const text = "![chart](#aw_chart1)";
      expect(hasUnresolvedTemporaryIds(text, tempIdMap, artifactUrlMap)).toBe(false);
    });

    it("should still detect unresolved ID when not in either map", async () => {
      const { hasUnresolvedTemporaryIds } = await import("./temporary_id.cjs");
      const tempIdMap = new Map([["aw_issue1", { repo: "owner/repo", number: 1 }]]);
      const artifactUrlMap = new Map([["aw_chart1", "https://github.com/owner/repo/actions/runs/1/artifacts/42"]]);
      const text = "See #aw_issue1 and ![chart](#aw_chart1) and #aw_unknown";
      expect(hasUnresolvedTemporaryIds(text, tempIdMap, artifactUrlMap)).toBe(true);
    });

    it("should return false when all IDs are covered across both maps", async () => {
      const { hasUnresolvedTemporaryIds } = await import("./temporary_id.cjs");
      const tempIdMap = new Map([["aw_issue1", { repo: "owner/repo", number: 1 }]]);
      const artifactUrlMap = new Map([["aw_chart1", "https://github.com/owner/repo/actions/runs/1/artifacts/42"]]);
      const text = "See #aw_issue1 and ![chart](#aw_chart1)";
      expect(hasUnresolvedTemporaryIds(text, tempIdMap, artifactUrlMap)).toBe(false);
    });
  });

  describe("replaceArtifactUrlReferences", () => {
    it("should replace #aw_ID with the artifact URL", async () => {
      const { replaceArtifactUrlReferences } = await import("./temporary_id.cjs");
      const artifactUrlMap = new Map([["aw_chart1", "https://github.com/owner/repo/actions/runs/1/artifacts/42"]]);
      const text = "![chart](#aw_chart1)";
      expect(replaceArtifactUrlReferences(text, artifactUrlMap)).toBe("![chart](https://github.com/owner/repo/actions/runs/1/artifacts/42)");
    });

    it("should be case-insensitive for the temporary ID", async () => {
      const { replaceArtifactUrlReferences } = await import("./temporary_id.cjs");
      const artifactUrlMap = new Map([["aw_chart1", "https://github.com/owner/repo/actions/runs/1/artifacts/42"]]);
      const text = "![chart](#aw_Chart1)";
      expect(replaceArtifactUrlReferences(text, artifactUrlMap)).toBe("![chart](https://github.com/owner/repo/actions/runs/1/artifacts/42)");
    });

    it("should leave non-artifact temporary IDs unchanged", async () => {
      const { replaceArtifactUrlReferences } = await import("./temporary_id.cjs");
      const artifactUrlMap = new Map([["aw_chart1", "https://github.com/owner/repo/actions/runs/1/artifacts/42"]]);
      const text = "See #aw_issue1 and ![chart](#aw_chart1)";
      expect(replaceArtifactUrlReferences(text, artifactUrlMap)).toBe("See #aw_issue1 and ![chart](https://github.com/owner/repo/actions/runs/1/artifacts/42)");
    });

    it("should handle multiple artifact references in the same text", async () => {
      const { replaceArtifactUrlReferences } = await import("./temporary_id.cjs");
      const artifactUrlMap = new Map([
        ["aw_img1", "https://github.com/owner/repo/actions/runs/1/artifacts/10"],
        ["aw_img2", "https://github.com/owner/repo/actions/runs/1/artifacts/20"],
      ]);
      const text = "![before](#aw_img1) and ![after](#aw_img2)";
      expect(replaceArtifactUrlReferences(text, artifactUrlMap)).toBe("![before](https://github.com/owner/repo/actions/runs/1/artifacts/10) and ![after](https://github.com/owner/repo/actions/runs/1/artifacts/20)");
    });

    it("should return text unchanged when artifactUrlMap is empty", async () => {
      const { replaceArtifactUrlReferences } = await import("./temporary_id.cjs");
      const artifactUrlMap = new Map();
      const text = "![chart](#aw_chart1)";
      expect(replaceArtifactUrlReferences(text, artifactUrlMap)).toBe(text);
    });

    it("should return text unchanged when artifactUrlMap is null/undefined", async () => {
      const { replaceArtifactUrlReferences } = await import("./temporary_id.cjs");
      const text = "![chart](#aw_chart1)";
      expect(replaceArtifactUrlReferences(text, null)).toBe(text);
      expect(replaceArtifactUrlReferences(text, undefined)).toBe(text);
    });

    it("should warn about malformed #aw_ references (too short) when map is non-empty", async () => {
      const { replaceArtifactUrlReferences } = await import("./temporary_id.cjs");
      const artifactUrlMap = new Map([["aw_chart1", "https://github.com/owner/repo/actions/runs/1/artifacts/42"]]);
      // "#aw_ab" has only 2 characters after "aw_" (too short, minimum is 3)
      const text = "![bad](#aw_ab)";
      replaceArtifactUrlReferences(text, artifactUrlMap);
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("#aw_ab"));
    });

    it("should warn about malformed #aw_ references containing hyphens when map is non-empty", async () => {
      const { replaceArtifactUrlReferences } = await import("./temporary_id.cjs");
      const artifactUrlMap = new Map([["aw_chart1", "https://github.com/owner/repo/actions/runs/1/artifacts/42"]]);
      const text = "![bad](#aw_bad-id)";
      replaceArtifactUrlReferences(text, artifactUrlMap);
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("#aw_bad-id"));
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

    it("should extract temporary IDs from Azure DevOps work-item fields", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const refs = extractTemporaryIdReferences({
        type: "ado_link_work_items",
        id: "#aw_item1",
        work_item_id: "#aw_item2",
        source_id: "#aw_item3",
        target_id: "#aw_item4",
      });

      expect(refs).toEqual(new Set(["aw_item1", "aw_item2", "aw_item3", "aw_item4"]));
    });

    it("should extract temporary IDs from blocked_by dependencies", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const refs = extractTemporaryIdReferences({
        type: "create_issue",
        blocked_by: ["aw_prereq", "#aw_other"],
      });

      expect(refs).toEqual(new Set(["aw_prereq", "aw_other"]));
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

    it("should extract temporary IDs from item_number field", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const message = {
        type: "add_labels",
        item_number: "aw_report1",
        labels: ["bug"],
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(1);
      expect(refs.has("aw_report1")).toBe(true);
    });

    it("should extract temporary IDs from item_number field (with # prefix)", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const message = {
        type: "remove_labels",
        item_number: "#aw_report1",
        labels: ["bug"],
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(1);
      expect(refs.has("aw_report1")).toBe(true);
    });

    it("should not extract from item_number with regular issue number", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const message = {
        type: "add_labels",
        item_number: 42,
        labels: ["bug"],
      };

      const refs = extractTemporaryIdReferences(message);

      expect(refs.size).toBe(0);
    });

    it("should ignore invalid temporary ID formats", async () => {
      const { extractTemporaryIdReferences } = await import("./temporary_id.cjs");

      const message = {
        type: "create_issue",
        body: "Invalid: #aw_a #aw- #temp_123456",
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

  describe("resolveNumberFromTemporaryId", () => {
    it("should resolve a temporary ID to its number", async () => {
      const { resolveNumberFromTemporaryId } = await import("./temporary_id.cjs");
      const resolvedTemporaryIds = { aw_disc1: { repo: "owner/repo", number: 99 } };
      const result = resolveNumberFromTemporaryId("aw_disc1", resolvedTemporaryIds);
      expect(result.resolved).toBe(99);
      expect(result.wasTemporaryId).toBe(true);
      expect(result.errorMessage).toBeNull();
    });

    it("should resolve a temporary ID with # prefix", async () => {
      const { resolveNumberFromTemporaryId } = await import("./temporary_id.cjs");
      const resolvedTemporaryIds = { aw_disc1: { repo: "owner/repo", number: 99 } };
      const result = resolveNumberFromTemporaryId("#aw_disc1", resolvedTemporaryIds);
      expect(result.resolved).toBe(99);
      expect(result.wasTemporaryId).toBe(true);
      expect(result.errorMessage).toBeNull();
    });

    it("should return error for unresolved temporary ID", async () => {
      const { resolveNumberFromTemporaryId } = await import("./temporary_id.cjs");
      const result = resolveNumberFromTemporaryId("aw_disc1", {});
      expect(result.resolved).toBeNull();
      expect(result.wasTemporaryId).toBe(true);
      expect(result.errorMessage).toContain("aw_disc1");
    });

    it("should return error when temporary ID has no number", async () => {
      const { resolveNumberFromTemporaryId } = await import("./temporary_id.cjs");
      const resolvedTemporaryIds = { aw_disc1: { repo: "owner/repo" } };
      const result = resolveNumberFromTemporaryId("aw_disc1", resolvedTemporaryIds);
      expect(result.resolved).toBeNull();
      expect(result.wasTemporaryId).toBe(true);
      expect(result.errorMessage).not.toBeNull();
    });

    it("should resolve a numeric string to a number", async () => {
      const { resolveNumberFromTemporaryId } = await import("./temporary_id.cjs");
      const result = resolveNumberFromTemporaryId("42", null);
      expect(result.resolved).toBe(42);
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toBeNull();
    });

    it("should resolve a numeric value to a number", async () => {
      const { resolveNumberFromTemporaryId } = await import("./temporary_id.cjs");
      const result = resolveNumberFromTemporaryId(99, null);
      expect(result.resolved).toBe(99);
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toBeNull();
    });

    it("should return error for invalid non-numeric string", async () => {
      const { resolveNumberFromTemporaryId } = await import("./temporary_id.cjs");
      const result = resolveNumberFromTemporaryId("invalid", null);
      expect(result.resolved).toBeNull();
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toContain("Invalid number");
    });

    it("should reject partially-numeric strings like '42abc'", async () => {
      const { resolveNumberFromTemporaryId } = await import("./temporary_id.cjs");
      const result = resolveNumberFromTemporaryId("42abc", null);
      expect(result.resolved).toBeNull();
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toContain("Invalid number");
    });

    it("should reject decimal strings like '3.14'", async () => {
      const { resolveNumberFromTemporaryId } = await import("./temporary_id.cjs");
      const result = resolveNumberFromTemporaryId("3.14", null);
      expect(result.resolved).toBeNull();
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toContain("Invalid number");
    });

    it("should return error for missing value", async () => {
      const { resolveNumberFromTemporaryId } = await import("./temporary_id.cjs");
      const result = resolveNumberFromTemporaryId(null, null);
      expect(result.resolved).toBeNull();
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toContain("missing");
    });

    it("should return error for non-numeric invalid string", async () => {
      const { resolveNumberFromTemporaryId } = await import("./temporary_id.cjs");
      const result = resolveNumberFromTemporaryId("not-a-number", null);
      expect(result.resolved).toBeNull();
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toContain("Invalid number: not-a-number");
    });

    it("should return error for zero (not a valid discussion/issue number)", async () => {
      const { resolveNumberFromTemporaryId } = await import("./temporary_id.cjs");
      const result = resolveNumberFromTemporaryId(0, null);
      expect(result.resolved).toBeNull();
      expect(result.wasTemporaryId).toBe(false);
      expect(result.errorMessage).toContain("Invalid number");
    });
  });

  describe("resolveSafeOutputIssueTarget", () => {
    const repoParts = { owner: "testowner", repo: "testrepo" };

    it("should return number: null when no alias fields are present", async () => {
      const { resolveSafeOutputIssueTarget } = await import("./temporary_id.cjs");
      const result = resolveSafeOutputIssueTarget({ message: { body: "hello" }, repoParts, handlerType: "test_handler" });
      expect(result.success).toBe(true);
      expect(result.number).toBeNull();
    });

    it("should return number: null when alias fields are all null or undefined", async () => {
      const { resolveSafeOutputIssueTarget } = await import("./temporary_id.cjs");
      const result = resolveSafeOutputIssueTarget({ message: { item_number: null, issue_number: undefined }, repoParts, handlerType: "test_handler" });
      expect(result.success).toBe(true);
      expect(result.number).toBeNull();
    });

    it("should resolve an explicit item_number", async () => {
      const { resolveSafeOutputIssueTarget } = await import("./temporary_id.cjs");
      const result = resolveSafeOutputIssueTarget({ message: { item_number: 42 }, repoParts, handlerType: "test_handler" });
      expect(result.success).toBe(true);
      expect(result.number).toBe(42);
    });

    it("should resolve issue_number alias when item_number is absent", async () => {
      const { resolveSafeOutputIssueTarget } = await import("./temporary_id.cjs");
      const result = resolveSafeOutputIssueTarget({ message: { issue_number: 99 }, repoParts, handlerType: "test_handler" });
      expect(result.success).toBe(true);
      expect(result.number).toBe(99);
    });

    it("should prefer item_number over issue_number when both are present", async () => {
      const { resolveSafeOutputIssueTarget } = await import("./temporary_id.cjs");
      const result = resolveSafeOutputIssueTarget({ message: { item_number: 10, issue_number: 20 }, repoParts, handlerType: "test_handler" });
      expect(result.success).toBe(true);
      expect(result.number).toBe(10);
    });

    it("should resolve a resolved temporary ID", async () => {
      const { resolveSafeOutputIssueTarget } = await import("./temporary_id.cjs");
      const resolvedTemporaryIds = { aw_issue1: { repo: "testowner/testrepo", number: 55 } };
      const result = resolveSafeOutputIssueTarget({ message: { item_number: "aw_issue1" }, resolvedTemporaryIds, repoParts, handlerType: "test_handler" });
      expect(result.success).toBe(true);
      expect(result.number).toBe(55);
    });

    it("should defer when temporary ID is not yet resolved", async () => {
      const { resolveSafeOutputIssueTarget } = await import("./temporary_id.cjs");
      const result = resolveSafeOutputIssueTarget({ message: { item_number: "aw_notyet1" }, resolvedTemporaryIds: {}, repoParts, handlerType: "my_handler" });
      expect(result.success).toBe(false);
      expect(result.deferred).toBe(true);
      expect(result.error).toContain("aw_notyet1");
      expect(mockCore.warning).not.toHaveBeenCalled();
    });

    it("should return error for invalid item number", async () => {
      const { resolveSafeOutputIssueTarget } = await import("./temporary_id.cjs");
      const result = resolveSafeOutputIssueTarget({ message: { item_number: "not-valid" }, repoParts, handlerType: "test_handler" });
      expect(result.success).toBe(false);
      expect(result.deferred).toBeFalsy();
      expect(result.error).toContain("Invalid item number");
    });

    it("should accept custom aliases", async () => {
      const { resolveSafeOutputIssueTarget } = await import("./temporary_id.cjs");
      const result = resolveSafeOutputIssueTarget({ message: { issue_number: 77 }, repoParts, handlerType: "test_handler", aliases: ["issue_number"] });
      expect(result.success).toBe(true);
      expect(result.number).toBe(77);
    });

    it("should use a pre-built tempIdMap when provided", async () => {
      const { resolveSafeOutputIssueTarget } = await import("./temporary_id.cjs");
      const tempIdMap = new Map([["aw_map1", { repo: "testowner/testrepo", number: 88 }]]);
      const result = resolveSafeOutputIssueTarget({ message: { item_number: "aw_map1" }, tempIdMap, repoParts, handlerType: "test_handler" });
      expect(result.success).toBe(true);
      expect(result.number).toBe(88);
    });

    it("should support hyphenated aliases like pr-number", async () => {
      const { resolveSafeOutputIssueTarget } = await import("./temporary_id.cjs");
      const result = resolveSafeOutputIssueTarget({ message: { "pr-number": 33 }, repoParts, handlerType: "test_handler", aliases: ["item_number", "issue_number", "pr-number"] });
      expect(result.success).toBe(true);
      expect(result.number).toBe(33);
    });
  });
});
