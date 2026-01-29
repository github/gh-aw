// @ts-check
import { describe, it, expect } from "vitest";
const {
  marshalSorted,
  buildCanonicalFrontmatter,
} = require("./frontmatter_hash.cjs");

describe("frontmatter_hash", () => {
  describe("marshalSorted", () => {
    it("should marshal primitives correctly", () => {
      expect(marshalSorted("test")).toBe('"test"');
      expect(marshalSorted(42)).toBe("42");
      expect(marshalSorted(3.14)).toBe("3.14");
      expect(marshalSorted(true)).toBe("true");
      expect(marshalSorted(false)).toBe("false");
      expect(marshalSorted(null)).toBe("null");
    });

    it("should marshal empty containers correctly", () => {
      expect(marshalSorted({})).toBe("{}");
      expect(marshalSorted([])).toBe("[]");
    });

    it("should sort object keys alphabetically", () => {
      const input = {
        zebra: 1,
        apple: 2,
        banana: 3,
        charlie: 4,
      };
      const result = marshalSorted(input);
      expect(result).toBe('{"apple":2,"banana":3,"charlie":4,"zebra":1}');
    });

    it("should sort nested object keys", () => {
      const input = {
        outer: {
          z: 1,
          a: 2,
        },
        another: {
          nested: {
            y: 3,
            b: 4,
          },
        },
      };
      const result = marshalSorted(input);
      expect(result).toContain('"another"');
      expect(result).toContain('"outer"');
      expect(result).toContain('"a":2');
      expect(result).toContain('"z":1');
    });

    it("should preserve array order", () => {
      const input = ["zebra", "apple", "banana"];
      const result = marshalSorted(input);
      expect(result).toBe('["zebra","apple","banana"]');
    });
  });

  describe("buildCanonicalFrontmatter", () => {
    it("should include expected fields", () => {
      const frontmatter = {
        engine: "copilot",
        description: "Test",
        on: { schedule: "daily" },
        tools: { playwright: { version: "v1.41.0" } },
      };

      const importsResult = {
        importedFiles: ["shared/common.md"],
        mergedEngines: ["claude", "copilot"],
        mergedLabels: [],
        mergedBots: [],
      };

      const canonical = buildCanonicalFrontmatter(frontmatter, importsResult);

      expect(canonical.engine).toBe("copilot");
      expect(canonical.description).toBe("Test");
      expect(canonical.on).toEqual({ schedule: "daily" });
      expect(canonical.tools).toEqual({ playwright: { version: "v1.41.0" } });
      expect(canonical.imports).toEqual(["shared/common.md"]);
    });

    it("should omit empty fields", () => {
      const frontmatter = {
        engine: "copilot",
      };

      const importsResult = {
        importedFiles: [],
        mergedEngines: [],
        mergedLabels: [],
        mergedBots: [],
      };

      const canonical = buildCanonicalFrontmatter(frontmatter, importsResult);

      expect(canonical.engine).toBe("copilot");
      expect(canonical.imports).toBeUndefined();
    });
  });
});
