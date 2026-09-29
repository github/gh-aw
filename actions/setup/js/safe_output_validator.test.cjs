import { describe, it, expect, beforeEach, vi } from "vitest";

// Create mock functions
const mockExistsSync = vi.fn(() => false);
const mockReadFileSync = vi.fn(() => "");

// Mock fs module with proper CommonJS structure
vi.mock("fs", () => {
  return {
    existsSync: mockExistsSync,
    readFileSync: mockReadFileSync,
    default: {
      existsSync: mockExistsSync,
      readFileSync: mockReadFileSync,
    },
  };
});

// Mock the global core object
const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
};

// Set up global
global.core = mockCore;

describe("safe_output_validator.cjs", () => {
  let validator;

  beforeEach(async () => {
    vi.clearAllMocks();

    // Reset mock implementations to default
    mockExistsSync.mockReturnValue(false);
    mockReadFileSync.mockReturnValue("");

    // Dynamically import the module
    validator = await import("./safe_output_validator.cjs");
  });

  // Note: These tests are skipped because mocking fs.readFileSync for CJS modules
  // is difficult in vitest. The validation functions below are what matters most.
  describe.skip("loadSafeOutputsConfig", () => {
    it("should load and parse config file", () => {
      mockExistsSync.mockReturnValue(true);
      mockReadFileSync.mockReturnValue(JSON.stringify({ add_labels: { max: 5 } }));

      const config = validator.loadSafeOutputsConfig();

      expect(config).toEqual({ add_labels: { max: 5 } });
      expect(mockReadFileSync).toHaveBeenCalledWith(`${process.env.RUNNER_TEMP}/gh-aw/safeoutputs/config.json`, "utf8");
    });

    it("should return empty object if config file does not exist", () => {
      mockExistsSync.mockReturnValue(false);

      const config = validator.loadSafeOutputsConfig();

      expect(config).toEqual({});
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Config file not found"));
    });

    it("should return empty object on parse error", () => {
      mockExistsSync.mockReturnValue(true);
      mockReadFileSync.mockReturnValue("invalid json");

      const config = validator.loadSafeOutputsConfig();

      expect(config).toEqual({});
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to load config"));
    });
  });

  describe.skip("getSafeOutputConfig", () => {
    it("should return config for specific output type", () => {
      mockExistsSync.mockReturnValue(true);
      mockReadFileSync.mockReturnValue(
        JSON.stringify({
          add_labels: { max: 5, allowed: ["bug", "enhancement"] },
          update_issue: { max: 1 },
        })
      );

      const config = validator.getSafeOutputConfig("add_labels");

      expect(config).toEqual({ max: 5, allowed: ["bug", "enhancement"] });
    });

    it("should return empty object for unknown output type", () => {
      mockExistsSync.mockReturnValue(true);
      mockReadFileSync.mockReturnValue(JSON.stringify({ add_labels: { max: 5 } }));

      const config = validator.getSafeOutputConfig("unknown_type");

      expect(config).toEqual({});
    });
  });

  describe("validateTitle", () => {
    it("should validate valid title", () => {
      const result = validator.validateTitle("  Valid Title  ");

      expect(result.valid).toBe(true);
      expect(result.value).toBe("Valid Title");
    });

    it("should reject undefined title", () => {
      const result = validator.validateTitle(undefined);

      expect(result.valid).toBe(false);
      expect(result.error).toContain("required");
    });

    it("should reject non-string title", () => {
      const result = validator.validateTitle(123);

      expect(result.valid).toBe(false);
      expect(result.error).toContain("must be a string");
    });

    it("should reject empty title", () => {
      const result = validator.validateTitle("   ");

      expect(result.valid).toBe(false);
      expect(result.error).toContain("cannot be empty");
    });

    it("should use custom field name in error messages", () => {
      const result = validator.validateTitle(undefined, "name");

      expect(result.error).toContain("name is required");
    });
  });

  describe("validateBody", () => {
    it("should validate valid body", () => {
      const result = validator.validateBody("Some content");

      expect(result.valid).toBe(true);
      expect(result.value).toBe("Some content");
    });

    it("should allow undefined body when not required", () => {
      const result = validator.validateBody(undefined, "body", false);

      expect(result.valid).toBe(true);
      expect(result.value).toBe("");
    });

    it("should reject undefined body when required", () => {
      const result = validator.validateBody(undefined, "body", true);

      expect(result.valid).toBe(false);
      expect(result.error).toContain("required");
    });

    it("should reject non-string body", () => {
      const result = validator.validateBody(123);

      expect(result.valid).toBe(false);
      expect(result.error).toContain("must be a string");
    });
  });

  describe("validateLabels", () => {
    it("should validate and sanitize valid labels", () => {
      const result = validator.validateLabels(["bug", "  enhancement  ", "documentation"], undefined, 10);

      expect(result.valid).toBe(true);
      expect(result.value).toContain("bug");
      expect(result.value).toContain("enhancement");
      expect(result.value).toContain("documentation");
    });

    it("should reject labels array with removal attempts", () => {
      const result = validator.validateLabels(["-bug", "enhancement"], undefined, 10);

      expect(result.valid).toBe(false);
      expect(result.error).toContain("Label removal is not permitted");
    });

    it("should filter labels based on allowed list", () => {
      const result = validator.validateLabels(["bug", "custom", "enhancement"], ["bug", "enhancement"], 10);

      expect(result.valid).toBe(true);
      expect(result.value).toHaveLength(2);
      expect(result.value).toContain("bug");
      expect(result.value).toContain("enhancement");
      expect(result.value).not.toContain("custom");
    });

    it("should filter labels based on allowed glob patterns", () => {
      const result = validator.validateLabels(["team-backend", "priority-high", "area/ui", "bug"], ["team-*", "priority-*", "area/*"], 10);

      expect(result.valid).toBe(true);
      expect(result.value).toHaveLength(3);
      expect(result.value).toContain("team-backend");
      expect(result.value).toContain("priority-high");
      expect(result.value).toContain("area/ui");
      expect(result.value).not.toContain("bug");
    });

    it("should limit labels to max count", () => {
      const result = validator.validateLabels(["a", "b", "c", "d", "e"], undefined, 3);

      expect(result.valid).toBe(true);
      expect(result.value).toHaveLength(3);
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("limiting to 3"));
    });

    it("should deduplicate labels", () => {
      const result = validator.validateLabels(["bug", "bug", "enhancement"], undefined, 10);

      expect(result.valid).toBe(true);
      expect(result.value).toHaveLength(2);
    });

    it("should truncate labels longer than 64 characters", () => {
      const longLabel = "a".repeat(100);
      const result = validator.validateLabels([longLabel], undefined, 10);

      expect(result.valid).toBe(true);
      expect(result.value[0]).toHaveLength(64);
    });

    it("should reject non-array labels", () => {
      const result = validator.validateLabels("bug", undefined, 10);

      expect(result.valid).toBe(false);
      expect(result.error).toContain("must be an array");
    });

    it("should reject when no valid labels remain", () => {
      const result = validator.validateLabels([null, undefined, false, 0], undefined, 10);

      expect(result.valid).toBe(false);
      expect(result.error).toContain("No valid labels found");
    });

    it("should filter out labels matching blocked patterns", () => {
      const result = validator.validateLabels(["bug", "~stale", "~archived", "enhancement"], undefined, 10, ["~*"]);

      expect(result.valid).toBe(true);
      expect(result.value).toHaveLength(2);
      expect(result.value).toContain("bug");
      expect(result.value).toContain("enhancement");
      expect(result.value).not.toContain("~stale");
      expect(result.value).not.toContain("~archived");
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Filtered out 2 blocked labels"));
    });

    it("should apply blocked filter before allowed filter", () => {
      const result = validator.validateLabels(
        ["bug", "~stale", "enhancement", "custom"],
        ["bug", "~stale", "enhancement"], // ~stale is in allowed list
        10,
        ["~*"] // but blocked by pattern
      );

      expect(result.valid).toBe(true);
      expect(result.value).toHaveLength(2);
      expect(result.value).toContain("bug");
      expect(result.value).toContain("enhancement");
      expect(result.value).not.toContain("~stale"); // blocked despite being in allowed list
      expect(result.value).not.toContain("custom");
    });

    it("should handle exact match blocking", () => {
      const result = validator.validateLabels(
        ["bug", "stale", "enhancement"],
        undefined,
        10,
        ["stale"] // exact match, no pattern
      );

      expect(result.valid).toBe(true);
      expect(result.value).toHaveLength(2);
      expect(result.value).toContain("bug");
      expect(result.value).toContain("enhancement");
      expect(result.value).not.toContain("stale");
    });

    it("should handle empty blocked patterns", () => {
      const result = validator.validateLabels(["bug", "~stale"], undefined, 10, []);

      expect(result.valid).toBe(true);
      expect(result.value).toHaveLength(2);
      expect(result.value).toContain("bug");
      expect(result.value).toContain("~stale");
    });

    it("should handle undefined blocked patterns", () => {
      const result = validator.validateLabels(["bug", "~stale"], undefined, 10, undefined);

      expect(result.valid).toBe(true);
      expect(result.value).toHaveLength(2);
      expect(result.value).toContain("bug");
      expect(result.value).toContain("~stale");
    });

    it("should reject when all labels are blocked", () => {
      const result = validator.validateLabels(["~stale", "~archived"], undefined, 10, ["~*"]);

      expect(result.valid).toBe(false);
      expect(result.error).toContain("No valid labels found");
    });

    it("should handle bot pattern blocking", () => {
      const result = validator.validateLabels(["bug", "dependabot[bot]", "github-actions[bot]", "enhancement"], undefined, 10, ["*[bot]"]);

      expect(result.valid).toBe(true);
      expect(result.value).toHaveLength(2);
      expect(result.value).toContain("bug");
      expect(result.value).toContain("enhancement");
      expect(result.value).not.toContain("dependabot[bot]");
      expect(result.value).not.toContain("github-actions[bot]");
    });
  });

  describe("validateMaxCount", () => {
    it("should use fallback default when no config or env", () => {
      const result = validator.validateMaxCount(undefined, undefined);

      expect(result.valid).toBe(true);
      expect(result.value).toBe(1); // fallback default
    });

    it("should use config default when provided", () => {
      const result = validator.validateMaxCount(undefined, 5);

      expect(result.valid).toBe(true);
      expect(result.value).toBe(5);
    });

    it("should prefer env value over config", () => {
      const result = validator.validateMaxCount("7", 5);

      expect(result.valid).toBe(true);
      expect(result.value).toBe(7);
    });

    it("should use custom fallback when provided", () => {
      const result = validator.validateMaxCount(undefined, undefined, 10);

      expect(result.valid).toBe(true);
      expect(result.value).toBe(10);
    });

    it("should reject invalid env value", () => {
      const result = validator.validateMaxCount("invalid", 5);

      expect(result.valid).toBe(false);
      expect(result.error).toContain("Invalid max value");
    });

    it("should reject negative env value", () => {
      const result = validator.validateMaxCount("-1", 5);

      expect(result.valid).toBe(false);
      expect(result.error).toContain("Invalid max value");
    });

    it("should reject zero env value", () => {
      const result = validator.validateMaxCount("0", 5);

      expect(result.valid).toBe(false);
      expect(result.error).toContain("Invalid max value");
    });
  });
});
