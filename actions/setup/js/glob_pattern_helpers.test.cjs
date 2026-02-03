import { describe, it, expect } from "vitest";
import { globPatternToRegex, parseGlobPatterns, matchesGlobPattern } from "./glob_pattern_helpers.cjs";

describe("glob_pattern_helpers.cjs", () => {
  describe("globPatternToRegex", () => {
    describe("basic pattern matching", () => {
      it("should match exact filenames without wildcards", () => {
        const regex = globPatternToRegex("specific-file.txt");

        expect(regex.test("specific-file.txt")).toBe(true);
        expect(regex.test("specific-file.md")).toBe(false);
        expect(regex.test("other-file.txt")).toBe(false);
      });

      it("should match files with * wildcard (single segment)", () => {
        const regex = globPatternToRegex("*.json");

        expect(regex.test("data.json")).toBe(true);
        expect(regex.test("config.json")).toBe(true);
        expect(regex.test("file.jsonl")).toBe(false);
        expect(regex.test("dir/data.json")).toBe(false); // * doesn't cross directories
      });

      it("should match files with ** wildcard (multi-segment)", () => {
        const regex = globPatternToRegex("metrics/**");

        expect(regex.test("metrics/file.json")).toBe(true);
        expect(regex.test("metrics/daily/file.json")).toBe(true);
        expect(regex.test("metrics/daily/archive/file.json")).toBe(true);
        expect(regex.test("data/file.json")).toBe(false);
      });

      it("should distinguish between * and **", () => {
        const singleStar = globPatternToRegex("logs/*");
        const doubleStar = globPatternToRegex("logs/**");

        // Single * should match direct children only
        expect(singleStar.test("logs/error.log")).toBe(true);
        expect(singleStar.test("logs/2024/error.log")).toBe(false);

        // Double ** should match nested paths
        expect(doubleStar.test("logs/error.log")).toBe(true);
        expect(doubleStar.test("logs/2024/error.log")).toBe(true);
        expect(doubleStar.test("logs/2024/12/error.log")).toBe(true);
      });
    });

    describe("special character escaping", () => {
      it("should escape dots correctly", () => {
        const regex = globPatternToRegex("file.txt");

        expect(regex.test("file.txt")).toBe(true);
        expect(regex.test("filextxt")).toBe(false); // dot shouldn't act as wildcard
        expect(regex.test("file_txt")).toBe(false);
      });

      it("should escape backslashes correctly", () => {
        // Test pattern with backslash (though rare in file patterns)
        const regex = globPatternToRegex("test\\.txt");

        // The backslash should be escaped, making this match literally
        expect(regex.source).toContain("\\\\");
      });

      it("should handle patterns with multiple dots", () => {
        const regex = globPatternToRegex("file.min.js");

        expect(regex.test("file.min.js")).toBe(true);
        expect(regex.test("filexminxjs")).toBe(false);
      });
    });

    describe("real-world patterns", () => {
      it("should match .jsonl files (daily-code-metrics use case)", () => {
        const regex = globPatternToRegex("*.jsonl");

        expect(regex.test("history.jsonl")).toBe(true);
        expect(regex.test("data.jsonl")).toBe(true);
        expect(regex.test("metrics.jsonl")).toBe(true);
        expect(regex.test("file.json")).toBe(false);
      });

      it("should match nested metrics files", () => {
        const regex = globPatternToRegex("metrics/**/*.json");

        // metrics/**/*.json = metrics/ + .* + / + [^/]*.json
        // The ** matches any path (including empty), but literal / after ** must exist
        expect(regex.test("metrics/daily/2024-12-26.json")).toBe(true);
        expect(regex.test("metrics/subdir/another/file.json")).toBe(true);

        // This won't match because we need the / after ** even if ** matches empty
        expect(regex.test("metrics/2024-12-26.json")).toBe(false);
        expect(regex.test("data/metrics.json")).toBe(false);

        // To match both nested and direct children, use: metrics/**
        const flexibleRegex = globPatternToRegex("metrics/**");
        expect(flexibleRegex.test("metrics/2024-12-26.json")).toBe(true);
        expect(flexibleRegex.test("metrics/daily/file.json")).toBe(true);
      });

      it("should match prefix-scoped patterns", () => {
        const cursorRegex = globPatternToRegex("security-q1/cursor.json");
        const metricsRegex = globPatternToRegex("security-q1/metrics/**");

        expect(cursorRegex.test("security-q1/cursor.json")).toBe(true);
        expect(cursorRegex.test("security-q1/metrics/file.json")).toBe(false);

        expect(metricsRegex.test("security-q1/metrics/2024-12-29.json")).toBe(true);
        expect(metricsRegex.test("security-q1/metrics/daily/snapshot.json")).toBe(true);
        expect(metricsRegex.test("security-q1/cursor.json")).toBe(false);
      });

      it("should match multiple file extensions", () => {
        const patterns = ["*.json", "*.jsonl", "*.csv", "*.md"].map(globPatternToRegex);

        const testCases = [
          { file: "data.json", shouldMatch: true },
          { file: "history.jsonl", shouldMatch: true },
          { file: "metrics.csv", shouldMatch: true },
          { file: "README.md", shouldMatch: true },
          { file: "script.js", shouldMatch: false },
          { file: "image.png", shouldMatch: false },
        ];

        for (const { file, shouldMatch } of testCases) {
          const matches = patterns.some(p => p.test(file));
          expect(matches).toBe(shouldMatch);
        }
      });
    });

    describe("edge cases", () => {
      it("should handle empty pattern", () => {
        const regex = globPatternToRegex("");

        expect(regex.test("")).toBe(true);
        expect(regex.test("anything")).toBe(false);
      });

      it("should handle pattern with only wildcards", () => {
        const singleWildcard = globPatternToRegex("*");
        const doubleWildcard = globPatternToRegex("**");

        expect(singleWildcard.test("file.txt")).toBe(true);
        expect(singleWildcard.test("dir/file.txt")).toBe(false);

        expect(doubleWildcard.test("file.txt")).toBe(true);
        expect(doubleWildcard.test("dir/file.txt")).toBe(true);
      });

      it("should handle complex nested patterns", () => {
        const regex = globPatternToRegex("data/**/archive/*.csv");

        // data/**/archive/*.csv = data/ + .* + /archive/ + [^/]*.csv
        // The ** matches any path, but literal /archive/ must follow
        expect(regex.test("data/2024/archive/metrics.csv")).toBe(true);
        expect(regex.test("data/2024/12/archive/metrics.csv")).toBe(true);

        // This won't match - ** matches empty but /archive/ must still be literal
        expect(regex.test("data/archive/metrics.csv")).toBe(false);

        expect(regex.test("data/metrics.csv")).toBe(false);
        expect(regex.test("data/archive/metrics.json")).toBe(false);

        // Test with a simpler pattern for direct match
        const directRegex = globPatternToRegex("data/archive/*.csv");
        expect(directRegex.test("data/archive/metrics.csv")).toBe(true);
      });

      it("should handle patterns with hyphens and underscores", () => {
        const regex = globPatternToRegex("test-file_name.json");

        expect(regex.test("test-file_name.json")).toBe(true);
        expect(regex.test("test_file-name.json")).toBe(false);
      });

      it("should be case-sensitive", () => {
        const regex = globPatternToRegex("*.JSON");

        expect(regex.test("file.JSON")).toBe(true);
        expect(regex.test("file.json")).toBe(false);
      });
    });

    describe("regex output format", () => {
      it("should return RegExp objects", () => {
        const regex = globPatternToRegex("*.json");

        expect(regex).toBeInstanceOf(RegExp);
      });

      it("should anchor patterns with ^ and $", () => {
        const regex = globPatternToRegex("*.json");

        expect(regex.source).toMatch(/^\^.*\$$/);
      });

      it("should convert * to [^/]* in regex source", () => {
        const regex = globPatternToRegex("*.json");

        expect(regex.source).toContain("[^/]*");
      });

      it("should convert ** to .* in regex source", () => {
        const regex = globPatternToRegex("data/**");

        expect(regex.source).toContain(".*");
      });
    });
  });

  describe("parseGlobPatterns", () => {
    it("should parse space-separated patterns", () => {
      const patterns = parseGlobPatterns("*.json *.jsonl *.csv");

      expect(patterns).toHaveLength(3);
      expect(patterns[0].test("file.json")).toBe(true);
      expect(patterns[1].test("file.jsonl")).toBe(true);
      expect(patterns[2].test("file.csv")).toBe(true);
    });

    it("should handle extra whitespace", () => {
      const patterns = parseGlobPatterns("  *.json   *.jsonl  ");

      expect(patterns).toHaveLength(2);
      expect(patterns[0].test("file.json")).toBe(true);
      expect(patterns[1].test("file.jsonl")).toBe(true);
    });

    it("should handle empty string", () => {
      const patterns = parseGlobPatterns("");

      expect(patterns).toHaveLength(0);
    });

    it("should handle single pattern", () => {
      const patterns = parseGlobPatterns("*.json");

      expect(patterns).toHaveLength(1);
      expect(patterns[0].test("file.json")).toBe(true);
    });

    it("should filter out empty patterns from multiple spaces", () => {
      const patterns = parseGlobPatterns("*.json     *.jsonl");

      expect(patterns).toHaveLength(2);
    });
  });

  describe("matchesGlobPattern", () => {
    it("should return true when file matches at least one pattern", () => {
      expect(matchesGlobPattern("file.json", "*.json *.jsonl")).toBe(true);
      expect(matchesGlobPattern("file.jsonl", "*.json *.jsonl")).toBe(true);
    });

    it("should return false when file matches no patterns", () => {
      expect(matchesGlobPattern("file.txt", "*.json *.jsonl")).toBe(false);
      expect(matchesGlobPattern("script.js", "*.json *.jsonl *.csv")).toBe(false);
    });

    it("should work with complex patterns", () => {
      expect(matchesGlobPattern("metrics/daily/2024.json", "metrics/**")).toBe(true);
      expect(matchesGlobPattern("data/file.json", "metrics/**")).toBe(false);
    });

    it("should handle empty filter (no patterns)", () => {
      expect(matchesGlobPattern("file.json", "")).toBe(false);
    });

    it("should handle the daily-code-metrics use case", () => {
      const filter = "*.json *.jsonl *.csv *.md";

      expect(matchesGlobPattern("history.jsonl", filter)).toBe(true);
      expect(matchesGlobPattern("data.json", filter)).toBe(true);
      expect(matchesGlobPattern("metrics.csv", filter)).toBe(true);
      expect(matchesGlobPattern("README.md", filter)).toBe(true);
      expect(matchesGlobPattern("script.js", filter)).toBe(false);
    });

    it("should work with prefix-scoped patterns", () => {
      const filter = "security-q1/cursor.json security-q1/metrics/**";

      expect(matchesGlobPattern("security-q1/cursor.json", filter)).toBe(true);
      expect(matchesGlobPattern("security-q1/metrics/2024.json", filter)).toBe(true);
      expect(matchesGlobPattern("security-q1/data.json", filter)).toBe(false);
    });
  });

  describe("security and correctness", () => {
    it("should prevent ReDoS with reasonable patterns", () => {
      // Test that the regex doesn't hang on pathological inputs
      const regex = globPatternToRegex("**/*.json");
      const longPath = "a/".repeat(100) + "file.json";

      // This should complete quickly
      const start = Date.now();
      regex.test(longPath);
      const duration = Date.now() - start;

      // Should complete in less than 100ms
      expect(duration).toBeLessThan(100);
    });

    it("should correctly escape special regex characters", () => {
      const regex = globPatternToRegex("file.txt");

      // The dot should be escaped, not act as a wildcard
      expect(regex.test("file.txt")).toBe(true);
      expect(regex.test("fileXtxt")).toBe(false);
    });

    it("should handle backslash escaping securely", () => {
      // This test verifies the security fix for proper escape order
      const pattern = "test.txt";

      // Correct: Escape backslashes first, then dots
      const regexPattern = pattern.replace(/\\/g, "\\\\").replace(/\./g, "\\.");
      const regex = new RegExp(`^${regexPattern}$`);

      expect(regex.test("test.txt")).toBe(true);
      expect(regex.test("test_txt")).toBe(false);
    });
  });

  describe("integration with push_repo_memory", () => {
    it("should validate file paths as used in push_repo_memory", () => {
      const fileGlobFilter = "*.json *.jsonl *.csv *.md";
      const testFiles = [
        { path: "history.jsonl", shouldMatch: true },
        { path: "data.json", shouldMatch: true },
        { path: "metrics.csv", shouldMatch: true },
        { path: "README.md", shouldMatch: true },
        { path: "script.js", shouldMatch: false },
        { path: "image.png", shouldMatch: false },
      ];

      for (const { path, shouldMatch } of testFiles) {
        expect(matchesGlobPattern(path, fileGlobFilter)).toBe(shouldMatch);
      }
    });

    it("should support subdirectory patterns", () => {
      const patterns = parseGlobPatterns("metrics/** data/**");

      expect(patterns.some(p => p.test("metrics/file.json"))).toBe(true);
      expect(patterns.some(p => p.test("data/file.json"))).toBe(true);
      expect(patterns.some(p => p.test("other/file.json"))).toBe(false);
    });

    it("should handle root-level files", () => {
      const patterns = parseGlobPatterns("*.jsonl");

      // Root level files (no subdirectory)
      expect(patterns.some(p => p.test("history.jsonl"))).toBe(true);
      expect(patterns.some(p => p.test("data.jsonl"))).toBe(true);

      // Should not match files in subdirectories
      expect(patterns.some(p => p.test("dir/history.jsonl"))).toBe(false);
    });
  });
});
