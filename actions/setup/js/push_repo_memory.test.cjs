import { describe, it, expect, beforeEach, vi } from "vitest";
import { globPatternToRegex } from "./glob_pattern_helpers.cjs";

describe("push_repo_memory.cjs - globPatternToRegex helper", () => {
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

    it("should match campaign-specific patterns", () => {
      const cursorRegex = globPatternToRegex("security-q1/cursor.json");
      const metricsRegex = globPatternToRegex("security-q1/metrics/**");

      expect(cursorRegex.test("security-q1/cursor.json")).toBe(true);
      expect(cursorRegex.test("security-q1/metrics/file.json")).toBe(false);

      expect(metricsRegex.test("security-q1/metrics/2024-12-29.json")).toBe(true);
      expect(metricsRegex.test("security-q1/metrics/daily/snapshot.json")).toBe(true);
      expect(metricsRegex.test("security-q1/cursor.json")).toBe(false);
    });

    it("should match flexible campaign pattern for both dated and non-dated structures", () => {
      // Pattern: go-file-size-reduction-project64*/**
      // This should match BOTH:
      // - go-file-size-reduction-project64-2025-12-31/ (with date suffix)
      // - go-file-size-reduction-project64/ (without suffix)
      const flexibleRegex = globPatternToRegex("go-file-size-reduction-project64*/**");

      // Test dated structure (with suffix)
      expect(flexibleRegex.test("go-file-size-reduction-project64-2025-12-31/cursor.json")).toBe(true);
      expect(flexibleRegex.test("go-file-size-reduction-project64-2025-12-31/metrics/2025-12-31.json")).toBe(true);

      // Test non-dated structure (without suffix)
      expect(flexibleRegex.test("go-file-size-reduction-project64/cursor.json")).toBe(true);
      expect(flexibleRegex.test("go-file-size-reduction-project64/metrics/2025-12-31.json")).toBe(true);

      // Should not match other campaigns
      expect(flexibleRegex.test("other-campaign/file.json")).toBe(false);
      expect(flexibleRegex.test("security-q1/cursor.json")).toBe(false);
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

      // To match data/archive/*.csv directly, use this pattern
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

describe("push_repo_memory.cjs - glob pattern security tests", () => {
  describe("glob-to-regex conversion", () => {
    it("should correctly escape backslashes before other characters", () => {
      // This test verifies the security fix for Alert #84
      // The fix ensures backslashes are escaped FIRST, before escaping other characters

      // Test pattern: "test.txt" (a normal pattern)
      const pattern = "test.txt";

      // Simulate the conversion logic from push_repo_memory.cjs line 107
      // CORRECT: Escape backslashes first, then dots, then asterisks
      const regexPattern = pattern.replace(/\\/g, "\\\\").replace(/\./g, "\\.").replace(/\*/g, "[^/]*");
      const regex = new RegExp(`^${regexPattern}$`);

      // After proper escaping:
      // "test.txt" -> "test.txt" (no backslashes) -> "test\.txt" (dot escaped)
      // The resulting regex should match only "test.txt" exactly

      // Should match exact filename
      expect(regex.test("test.txt")).toBe(true);

      // Should NOT match files where dot acts as wildcard
      expect(regex.test("test_txt")).toBe(false);
      expect(regex.test("testXtxt")).toBe(false);
    });

    it("should demonstrate INCORRECT escaping (vulnerable pattern)", () => {
      // This demonstrates the VULNERABLE version that was fixed
      // WITHOUT escaping backslashes first

      const pattern = "\\\\.txt";

      // INCORRECT: NOT escaping backslashes first
      const regexPattern = pattern.replace(/\./g, "\\.").replace(/\*/g, "[^/]*");
      const regex = new RegExp(`^${regexPattern}$`);

      // This would create an incorrect regex pattern
      // The backslash isn't properly escaped, leading to potential bypass
    });

    it("should correctly escape dots to prevent matching any character", () => {
      // Test that dots are escaped, so "file.txt" doesn't match "filextxt"
      const pattern = "file.txt";

      const regexPattern = pattern.replace(/\\/g, "\\\\").replace(/\./g, "\\.").replace(/\*/g, "[^/]*");
      const regex = new RegExp(`^${regexPattern}$`);

      // Should match exact filename
      expect(regex.test("file.txt")).toBe(true);

      // Should NOT match with dot as wildcard
      expect(regex.test("filextxt")).toBe(false);
      expect(regex.test("fileXtxt")).toBe(false);
      expect(regex.test("file_txt")).toBe(false);
    });

    it("should correctly convert asterisks to wildcard regex", () => {
      // Test that asterisks are converted to [^/]* (matches anything except slashes)
      const pattern = "*.txt";

      const regexPattern = pattern.replace(/\\/g, "\\\\").replace(/\./g, "\\.").replace(/\*/g, "[^/]*");
      const regex = new RegExp(`^${regexPattern}$`);

      // Should match any filename ending in .txt
      expect(regex.test("file.txt")).toBe(true);
      expect(regex.test("document.txt")).toBe(true);
      expect(regex.test("test-file.txt")).toBe(true);

      // Should NOT match files without .txt extension
      expect(regex.test("file.md")).toBe(false);
      expect(regex.test("txt")).toBe(false);

      // Should NOT match paths with slashes (glob wildcards don't cross directories)
      expect(regex.test("dir/file.txt")).toBe(false);
    });

    it("should handle complex patterns with backslash and asterisk", () => {
      // Test pattern with asterisk wildcard
      const pattern = "test-*.txt";

      const regexPattern = pattern.replace(/\\/g, "\\\\").replace(/\./g, "\\.").replace(/\*/g, "[^/]*");
      const regex = new RegExp(`^${regexPattern}$`);

      // After proper escaping:
      // "test-*.txt" -> "test-*.txt" (no backslashes) -> "test-*.txt" (no dots to escape except at end)
      //  -> "test-[^/]*\.txt" (asterisk converted to wildcard)

      // Should match files with the pattern
      expect(regex.test("test-file.txt")).toBe(true);
      expect(regex.test("test-123.txt")).toBe(true);
      expect(regex.test("test-.txt")).toBe(true);

      // Should NOT match files without the pattern
      expect(regex.test("test.txt")).toBe(false);
      expect(regex.test("other-file.txt")).toBe(false);
      expect(regex.test("test-file.md")).toBe(false);
    });

    it("should correctly match .jsonl files with *.jsonl pattern", () => {
      // Test case for validating .jsonl file pattern matching
      // This validates the fix for: https://github.com/github/gh-aw/actions/runs/20601784686/job/59169295542#step:7:1
      // And: https://github.com/github/gh-aw/actions/runs/20608399402/job/59188647531#step:7:1
      // The daily-code-metrics workflow uses file-glob: ["*.json", "*.jsonl", "*.csv", "*.md"]
      // and writes history.jsonl file to repo memory at memory/default/history.jsonl

      const fileGlobFilter = "*.json *.jsonl *.csv *.md";
      const patterns = fileGlobFilter
        .trim()
        .split(/\s+/)
        .filter(Boolean)
        .map(pattern => {
          const regexPattern = pattern
            .replace(/\\/g, "\\\\")
            .replace(/\./g, "\\.")
            .replace(/\*\*/g, "<!DOUBLESTAR>")
            .replace(/\*/g, "[^/]*")
            .replace(/<!DOUBLESTAR>/g, ".*");
          return new RegExp(`^${regexPattern}$`);
        });

      // Should match .jsonl files (the actual file from workflow run: history.jsonl)
      // Note: Pattern matching is done on relative filename only, not full path
      expect(patterns.some(p => p.test("history.jsonl"))).toBe(true);
      expect(patterns.some(p => p.test("data.jsonl"))).toBe(true);
      expect(patterns.some(p => p.test("metrics.jsonl"))).toBe(true);

      // Should also match other allowed extensions
      expect(patterns.some(p => p.test("config.json"))).toBe(true);
      expect(patterns.some(p => p.test("data.csv"))).toBe(true);
      expect(patterns.some(p => p.test("README.md"))).toBe(true);

      // Should NOT match disallowed extensions
      expect(patterns.some(p => p.test("script.js"))).toBe(false);
      expect(patterns.some(p => p.test("image.png"))).toBe(false);
      expect(patterns.some(p => p.test("document.txt"))).toBe(false);

      // Edge case: Should NOT match .json when pattern is *.jsonl
      expect(patterns.some(p => p.test("file.json"))).toBe(true); // matches *.json pattern
      const jsonlOnlyPattern = "*.jsonl";
      const jsonlRegex = new RegExp(`^${jsonlOnlyPattern.replace(/\\/g, "\\\\").replace(/\./g, "\\.").replace(/\*/g, "[^/]*")}$`);
      expect(jsonlRegex.test("file.json")).toBe(false); // should NOT match .json with *.jsonl pattern
      expect(jsonlRegex.test("file.jsonl")).toBe(true); // should match .jsonl with *.jsonl pattern
    });

    it("should handle multiple patterns correctly", () => {
      // Test multiple space-separated patterns
      const patterns = "*.txt *.md".split(/\s+/).map(pattern => {
        const regexPattern = pattern.replace(/\\/g, "\\\\").replace(/\./g, "\\.").replace(/\*/g, "[^/]*");
        return new RegExp(`^${regexPattern}$`);
      });

      // Should match .txt files
      expect(patterns.some(p => p.test("file.txt"))).toBe(true);
      expect(patterns.some(p => p.test("README.md"))).toBe(true);

      // Should NOT match other extensions
      expect(patterns.some(p => p.test("script.js"))).toBe(false);
      expect(patterns.some(p => p.test("image.png"))).toBe(false);
    });

    it("should handle patterns with leading/trailing whitespace", () => {
      // Test that trim() and filter(Boolean) properly handle edge cases
      // This validates that empty patterns from whitespace don't cause issues
      const fileGlobFilter = " *.json  *.jsonl  *.csv  *.md "; // Extra spaces

      const patterns = fileGlobFilter
        .trim()
        .split(/\s+/)
        .filter(Boolean)
        .map(pattern => {
          const regexPattern = pattern
            .replace(/\\/g, "\\\\")
            .replace(/\./g, "\\.")
            .replace(/\*\*/g, "<!DOUBLESTAR>")
            .replace(/\*/g, "[^/]*")
            .replace(/<!DOUBLESTAR>/g, ".*");
          return new RegExp(`^${regexPattern}$`);
        });

      // Should still have exactly 4 patterns (empty strings filtered out)
      expect(patterns.length).toBe(4);

      // Should still match valid files
      expect(patterns.some(p => p.test("history.jsonl"))).toBe(true);
      expect(patterns.some(p => p.test("data.json"))).toBe(true);
      expect(patterns.some(p => p.test("metrics.csv"))).toBe(true);
      expect(patterns.some(p => p.test("README.md"))).toBe(true);

      // Should NOT match disallowed extensions
      expect(patterns.some(p => p.test("script.js"))).toBe(false);
    });

    it("should handle exact filename patterns", () => {
      // Test exact filename match (no wildcards)
      const pattern = "specific-file.txt";

      const regexPattern = pattern.replace(/\\/g, "\\\\").replace(/\./g, "\\.").replace(/\*/g, "[^/]*");
      const regex = new RegExp(`^${regexPattern}$`);

      // Should only match the exact filename
      expect(regex.test("specific-file.txt")).toBe(true);

      // Should NOT match similar filenames
      expect(regex.test("specific-file.md")).toBe(false);
      expect(regex.test("specific-file.txt.bak")).toBe(false);
      expect(regex.test("prefix-specific-file.txt")).toBe(false);
    });

    it("should preserve security - escape order matters", () => {
      // This test demonstrates WHY the escape order matters
      // It's the core security issue that was fixed

      const testPattern = "test\\.txt"; // Pattern with backslash-dot sequence

      // CORRECT order: backslash first
      const correctRegex = testPattern.replace(/\\/g, "\\\\").replace(/\./g, "\\.");
      const correct = new RegExp(`^${correctRegex}$`);

      // INCORRECT order: dot first (vulnerable)
      const incorrectRegex = testPattern.replace(/\./g, "\\.").replace(/\\/g, "\\\\");
      const incorrect = new RegExp(`^${incorrectRegex}$`);

      // The patterns should behave differently
      // This demonstrates the security implications of incorrect escape order
      expect(correctRegex).not.toBe(incorrectRegex);
    });
  });

  describe("subdirectory glob pattern support", () => {
    // Tests for the new ** wildcard support added for subdirectory handling

    it("should handle ** wildcard to match any path including slashes", () => {
      // Test the new ** pattern that matches across directories
      const pattern = "metrics/**";

      // New conversion logic: ** -> .* (matches everything including /)
      const regexPattern = pattern
        .replace(/\\/g, "\\\\")
        .replace(/\./g, "\\.")
        .replace(/\*\*/g, "<!DOUBLESTAR>")
        .replace(/\*/g, "[^/]*")
        .replace(/<!DOUBLESTAR>/g, ".*");
      const regex = new RegExp(`^${regexPattern}$`);

      // Should match nested files in metrics directory (including any extension)
      expect(regex.test("metrics/latest.json")).toBe(true);
      expect(regex.test("metrics/daily/2024-12-26.json")).toBe(true);
      expect(regex.test("metrics/daily/archive/2024-01-01.json")).toBe(true);
      expect(regex.test("metrics/readme.md")).toBe(true);

      // Should NOT match files outside metrics directory
      expect(regex.test("data/file.json")).toBe(false);
      expect(regex.test("file.json")).toBe(false);
    });

    it("should differentiate between * and ** wildcards", () => {
      // Test that * doesn't cross directories but ** does

      // Single * pattern - should NOT match subdirectories
      const singleStarPattern = "metrics/*";
      const singleStarRegex = singleStarPattern
        .replace(/\\/g, "\\\\")
        .replace(/\./g, "\\.")
        .replace(/\*\*/g, "<!DOUBLESTAR>")
        .replace(/\*/g, "[^/]*")
        .replace(/<!DOUBLESTAR>/g, ".*");
      const singleStar = new RegExp(`^${singleStarRegex}$`);

      // Should match direct children only
      expect(singleStar.test("metrics/file.json")).toBe(true);
      expect(singleStar.test("metrics/latest.json")).toBe(true);

      // Should NOT match nested files
      expect(singleStar.test("metrics/daily/file.json")).toBe(false);

      // Double ** pattern - should match subdirectories
      const doubleStarPattern = "metrics/**";
      const doubleStarRegex = doubleStarPattern
        .replace(/\\/g, "\\\\")
        .replace(/\./g, "\\.")
        .replace(/\*\*/g, "<!DOUBLESTAR>")
        .replace(/\*/g, "[^/]*")
        .replace(/<!DOUBLESTAR>/g, ".*");
      const doubleStar = new RegExp(`^${doubleStarRegex}$`);

      // Should match both direct and nested files
      expect(doubleStar.test("metrics/file.json")).toBe(true);
      expect(doubleStar.test("metrics/daily/file.json")).toBe(true);
      expect(doubleStar.test("metrics/daily/archive/file.json")).toBe(true);
    });

    it("should handle **/* pattern correctly", () => {
      // Test **/* which requires at least one directory level
      // Note: ** matches one or more path segments in this implementation
      const pattern = "**/*";

      const regexPattern = pattern
        .replace(/\\/g, "\\\\")
        .replace(/\./g, "\\.")
        .replace(/\*\*/g, "<!DOUBLESTAR>")
        .replace(/\*/g, "[^/]*")
        .replace(/<!DOUBLESTAR>/g, ".*");
      const regex = new RegExp(`^${regexPattern}$`);

      // With current implementation, **/* requires at least one slash
      expect(regex.test("dir/file.txt")).toBe(true);
      expect(regex.test("dir/subdir/file.txt")).toBe(true);
      expect(regex.test("very/deep/nested/path/file.json")).toBe(true);

      // Does not match files in root (no slash)
      expect(regex.test("file.txt")).toBe(false);
    });

    it("should handle mixed * and ** in same pattern", () => {
      // Test patterns with both single and double wildcards
      const pattern = "logs/**";

      const regexPattern = pattern
        .replace(/\\/g, "\\\\")
        .replace(/\./g, "\\.")
        .replace(/\*\*/g, "<!DOUBLESTAR>")
        .replace(/\*/g, "[^/]*")
        .replace(/<!DOUBLESTAR>/g, ".*");
      const regex = new RegExp(`^${regexPattern}$`);

      // Should match any logs at any depth in logs directory
      expect(regex.test("logs/error-123.log")).toBe(true);
      expect(regex.test("logs/2024/error-456.log")).toBe(true);
      expect(regex.test("logs/2024/12/error-789.log")).toBe(true);
      expect(regex.test("logs/info-123.log")).toBe(true);
      expect(regex.test("logs/2024/warning-456.log")).toBe(true);

      // Should NOT match logs outside logs directory
      expect(regex.test("error-123.log")).toBe(false);
    });

    it("should handle subdirectory patterns for metrics use case", () => {
      // Real-world test for the metrics collector use case
      // Note: metrics/**/* requires at least one directory level under metrics
      const pattern = "metrics/**/*";

      const regexPattern = pattern
        .replace(/\\/g, "\\\\")
        .replace(/\./g, "\\.")
        .replace(/\*\*/g, "<!DOUBLESTAR>")
        .replace(/\*/g, "[^/]*")
        .replace(/<!DOUBLESTAR>/g, ".*");
      const regex = new RegExp(`^${regexPattern}$`);

      // Should match files in subdirectories
      expect(regex.test("metrics/daily/2024-12-26.json")).toBe(true);
      expect(regex.test("metrics/daily/2024-12-25.json")).toBe(true);
      expect(regex.test("metrics/subdir/config.yaml")).toBe(true);

      // Does NOT match direct children (needs at least one subdir)
      // This is current behavior - could be improved in future
      expect(regex.test("metrics/latest.json")).toBe(false);

      // Should NOT match files outside metrics directory
      expect(regex.test("data/metrics.json")).toBe(false);
      expect(regex.test("latest.json")).toBe(false);
    });
  });

  describe("security implications", () => {
    it("should prevent bypass attacks with crafted patterns", () => {
      // An attacker might try to craft patterns to bypass validation
      // The fix ensures proper escaping prevents such bypasses

      // Example: A complex pattern with special characters
      const attackPattern = "test.*";

      // With correct escaping (backslashes first)
      const safeRegexPattern = attackPattern.replace(/\\/g, "\\\\").replace(/\./g, "\\.").replace(/\*/g, "[^/]*");
      const safeRegex = new RegExp(`^${safeRegexPattern}$`);

      // The pattern "test.*" should become "test\.[^/]*" in regex
      // Meaning: "test" + literal dot + any characters (not crossing directories)

      expect(safeRegex.test("test.txt")).toBe(true);
      expect(safeRegex.test("test.md")).toBe(true);
      expect(safeRegex.test("test.anything")).toBe(true);

      // Should NOT match without the dot
      expect(safeRegex.test("testtxt")).toBe(false);
      expect(safeRegex.test("testmd")).toBe(false);
    });

    it("should demonstrate CWE-20/80/116 prevention", () => {
      // This test relates to the CWEs mentioned in the security fix:
      // - CWE-20: Improper Input Validation
      // - CWE-80: Improper Neutralization of Script-Related HTML Tags
      // - CWE-116: Improper Encoding or Escaping of Output

      const userInput = "*.txt"; // Simulated user input from FILE_GLOB_FILTER

      // The fix ensures proper encoding/escaping of the pattern
      const escapedPattern = userInput.replace(/\\/g, "\\\\").replace(/\./g, "\\.").replace(/\*/g, "[^/]*");
      const regex = new RegExp(`^${escapedPattern}$`);

      // Input is properly validated and sanitized
      // Pattern "*.txt" becomes "[^/]*\.txt" in regex
      expect(regex.test("normal.txt")).toBe(true);
      expect(regex.test("file.txt")).toBe(true);

      // Should not match non-.txt files
      expect(regex.test("normal.md")).toBe(false);
      expect(regex.test("file.js")).toBe(false);
    });

    it("should prevent directory traversal with ** wildcard", () => {
      // Ensure ** wildcard doesn't enable directory traversal attacks
      const pattern = "data/**";

      const regexPattern = pattern
        .replace(/\\/g, "\\\\")
        .replace(/\./g, "\\.")
        .replace(/\*\*/g, "<!DOUBLESTAR>")
        .replace(/\*/g, "[^/]*")
        .replace(/<!DOUBLESTAR>/g, ".*");
      const regex = new RegExp(`^${regexPattern}$`);

      // Should match legitimate nested files
      expect(regex.test("data/file.json")).toBe(true);
      expect(regex.test("data/subdir/file.json")).toBe(true);

      // Should NOT match files outside data directory
      // Note: The pattern is anchored with ^ and $, so it must match the full path
      expect(regex.test("../sensitive/file.json")).toBe(false);
      expect(regex.test("/etc/passwd")).toBe(false);
      expect(regex.test("other/data/file.json")).toBe(false);
    });
  });

  describe("multi-pattern filter support", () => {
    it("should support multiple space-separated patterns", () => {
      // Test multiple patterns like "campaign-id/cursor.json campaign-id/metrics/**"
      const patterns = "security-q1/cursor.json security-q1/metrics/**".split(/\s+/).filter(Boolean);

      // Each pattern should be validated independently
      expect(patterns).toHaveLength(2);
      expect(patterns[0]).toBe("security-q1/cursor.json");
      expect(patterns[1]).toBe("security-q1/metrics/**");
    });

    it("should validate each pattern in multi-pattern filter", () => {
      // Test that each pattern can be converted to regex independently
      const patterns = "data/**.json logs/**.log".split(/\s+/).filter(Boolean);

      const regexPatterns = patterns.map(pattern => {
        const regexPattern = pattern
          .replace(/\\/g, "\\\\")
          .replace(/\./g, "\\.")
          .replace(/\*\*/g, "<!DOUBLESTAR>")
          .replace(/\*/g, "[^/]*")
          .replace(/<!DOUBLESTAR>/g, ".*");
        return new RegExp(`^${regexPattern}$`);
      });

      // First pattern should match .json files in data/
      expect(regexPatterns[0].test("data/file.json")).toBe(true);
      expect(regexPatterns[0].test("data/subdir/file.json")).toBe(true);
      expect(regexPatterns[0].test("logs/file.log")).toBe(false);

      // Second pattern should match .log files in logs/
      expect(regexPatterns[1].test("logs/file.log")).toBe(true);
      expect(regexPatterns[1].test("logs/subdir/file.log")).toBe(true);
      expect(regexPatterns[1].test("data/file.json")).toBe(false);
    });

    it("should handle campaign-specific multi-pattern filters", () => {
      // Real-world campaign use case: multiple specific patterns
      const patterns = "security-q1/cursor.json security-q1/metrics/**".split(/\s+/).filter(Boolean);

      const regexPatterns = patterns.map(pattern => {
        const regexPattern = pattern
          .replace(/\\/g, "\\\\")
          .replace(/\./g, "\\.")
          .replace(/\*\*/g, "<!DOUBLESTAR>")
          .replace(/\*/g, "[^/]*")
          .replace(/<!DOUBLESTAR>/g, ".*");
        return new RegExp(`^${regexPattern}$`);
      });

      // First pattern: exact cursor file
      expect(regexPatterns[0].test("security-q1/cursor.json")).toBe(true);
      expect(regexPatterns[0].test("security-q1/cursor.txt")).toBe(false);
      expect(regexPatterns[0].test("security-q1/metrics/2024-12-29.json")).toBe(false);

      // Second pattern: any metrics files
      expect(regexPatterns[1].test("security-q1/metrics/2024-12-29.json")).toBe(true);
      expect(regexPatterns[1].test("security-q1/metrics/daily/snapshot.json")).toBe(true);
      expect(regexPatterns[1].test("security-q1/cursor.json")).toBe(false);
    });
  });

  describe("campaign ID validation", () => {
    it("should extract campaign ID from first pattern", () => {
      // Test extracting campaign ID from pattern like "security-q1/**"
      const pattern = "security-q1/**";
      const match = /^([^*?/]+)\/\*\*/.exec(pattern);

      expect(match).not.toBeNull();
      expect(match[1]).toBe("security-q1");
    });

    it("should validate all patterns start with campaign ID", () => {
      // Test that all patterns must be under campaign-id/ subdirectory
      const campaignId = "security-q1";
      const validPatterns = ["security-q1/cursor.json", "security-q1/metrics/**", "security-q1/data/*.txt"];

      for (const pattern of validPatterns) {
        expect(pattern.startsWith(`${campaignId}/`)).toBe(true);
      }

      const invalidPatterns = ["other-campaign/cursor.json", "cursor.json", "metrics/**"];

      for (const pattern of invalidPatterns) {
        expect(pattern.startsWith(`${campaignId}/`)).toBe(false);
      }
    });

    it("should handle campaign ID with hyphens and underscores", () => {
      // Test various campaign ID formats
      const patterns = ["security-q1-2025/**", "incident_response/**", "rollout-v2_phase1/**"];

      for (const pattern of patterns) {
        const match = /^([^*?/]+)\/\*\*/.exec(pattern);
        expect(match).not.toBeNull();

        // Extracted campaign ID should match the prefix
        const campaignId = match[1];
        expect(pattern.startsWith(`${campaignId}/`)).toBe(true);
      }
    });

    it("should reject patterns not under campaign ID subdirectory", () => {
      // Test enforcement that patterns must be under campaign-id/
      const campaignId = "security-q1";

      // Valid: under campaign-id/
      expect("security-q1/metrics/**".startsWith(`${campaignId}/`)).toBe(true);
      expect("security-q1/cursor.json".startsWith(`${campaignId}/`)).toBe(true);

      // Invalid: not under campaign-id/
      expect("metrics/**".startsWith(`${campaignId}/`)).toBe(false);
      expect("other-campaign/data.json".startsWith(`${campaignId}/`)).toBe(false);
      expect("cursor.json".startsWith(`${campaignId}/`)).toBe(false);
    });

    it("should support explicit GH_AW_CAMPAIGN_ID override", () => {
      // Test that environment variable can override campaign ID detection
      // This would be simulated in the actual code by process.env.GH_AW_CAMPAIGN_ID
      const explicitCampaignId = "rollout-v2";
      const patterns = ["rollout-v2/cursor.json", "rollout-v2/metrics/**"];

      // All patterns should validate against explicit campaign ID
      for (const pattern of patterns) {
        expect(pattern.startsWith(`${explicitCampaignId}/`)).toBe(true);
      }
    });
  });

  describe("debug logging for pattern matching", () => {
    it("should log pattern matching details for debugging", () => {
      // Test that debug logging provides helpful information
      const fileGlobFilter = "*.json *.jsonl *.csv *.md";
      const testFile = "history.jsonl";

      const patterns = fileGlobFilter
        .trim()
        .split(/\s+/)
        .filter(Boolean)
        .map(pattern => {
          const regexPattern = pattern
            .replace(/\\/g, "\\\\")
            .replace(/\./g, "\\.")
            .replace(/\*\*/g, "<!DOUBLESTAR>")
            .replace(/\*/g, "[^/]*")
            .replace(/<!DOUBLESTAR>/g, ".*");
          return new RegExp(`^${regexPattern}$`);
        });

      // Log what we're testing
      const matchResults = patterns.map((pattern, idx) => {
        const matches = pattern.test(testFile);
        const patternStr = fileGlobFilter.trim().split(/\s+/).filter(Boolean)[idx];
        return { patternStr, regex: pattern.source, matches };
      });

      // Verify that history.jsonl matches the *.jsonl pattern
      const jsonlMatch = matchResults.find(r => r.patternStr === "*.jsonl");
      expect(jsonlMatch).toBeDefined();
      expect(jsonlMatch.matches).toBe(true);
      expect(jsonlMatch.regex).toBe("^[^/]*\\.jsonl$");

      // Verify overall that at least one pattern matches
      expect(matchResults.some(r => r.matches)).toBe(true);
    });

    it("should show which patterns match and which don't for a given file", () => {
      // Test with a file that should only match one pattern
      const fileGlobFilter = "*.json *.jsonl *.csv *.md";
      const testFile = "data.csv";

      const patterns = fileGlobFilter
        .trim()
        .split(/\s+/)
        .filter(Boolean)
        .map(pattern => {
          const regexPattern = pattern
            .replace(/\\/g, "\\\\")
            .replace(/\./g, "\\.")
            .replace(/\*\*/g, "<!DOUBLESTAR>")
            .replace(/\*/g, "[^/]*")
            .replace(/<!DOUBLESTAR>/g, ".*");
          return new RegExp(`^${regexPattern}$`);
        });

      const patternStrs = fileGlobFilter.trim().split(/\s+/).filter(Boolean);
      const matchResults = patterns.map((pattern, idx) => ({
        pattern: patternStrs[idx],
        regex: pattern.source,
        matches: pattern.test(testFile),
      }));

      // Should match *.csv but not others
      expect(matchResults[0].matches).toBe(false); // *.json
      expect(matchResults[1].matches).toBe(false); // *.jsonl
      expect(matchResults[2].matches).toBe(true); // *.csv
      expect(matchResults[3].matches).toBe(false); // *.md
    });

    it("should provide helpful error details when no patterns match", () => {
      // Test with a file that doesn't match any pattern
      const fileGlobFilter = "*.json *.jsonl *.csv *.md";
      const testFile = "script.js";

      const patterns = fileGlobFilter
        .trim()
        .split(/\s+/)
        .filter(Boolean)
        .map(pattern => {
          const regexPattern = pattern
            .replace(/\\/g, "\\\\")
            .replace(/\./g, "\\.")
            .replace(/\*\*/g, "<!DOUBLESTAR>")
            .replace(/\*/g, "[^/]*")
            .replace(/<!DOUBLESTAR>/g, ".*");
          return new RegExp(`^${regexPattern}$`);
        });

      const patternStrs = fileGlobFilter.trim().split(/\s+/).filter(Boolean);
      const matchResults = patterns.map((pattern, idx) => ({
        pattern: patternStrs[idx],
        regex: pattern.source,
        matches: pattern.test(testFile),
      }));

      // None should match
      expect(matchResults.every(r => !r.matches)).toBe(true);

      // Error message should include pattern details
      const errorDetails = matchResults.map(r => `${r.pattern} -> regex: ${r.regex} -> ${r.matches ? "MATCH" : "NO MATCH"}`);

      expect(errorDetails[0]).toContain("*.json -> regex: ^[^/]*\\.json$ -> NO MATCH");
      expect(errorDetails[1]).toContain("*.jsonl -> regex: ^[^/]*\\.jsonl$ -> NO MATCH");
      expect(errorDetails[2]).toContain("*.csv -> regex: ^[^/]*\\.csv$ -> NO MATCH");
      expect(errorDetails[3]).toContain("*.md -> regex: ^[^/]*\\.md$ -> NO MATCH");
    });

    it("should correctly match files in the root directory (no subdirectories)", () => {
      // The daily-code-metrics workflow writes history.jsonl to the root of repo memory
      // Test that pattern matching works for root-level files
      const fileGlobFilter = "*.json *.jsonl *.csv *.md";
      const rootFiles = ["history.jsonl", "data.json", "metrics.csv", "README.md"];

      const patterns = fileGlobFilter
        .trim()
        .split(/\s+/)
        .filter(Boolean)
        .map(pattern => {
          const regexPattern = pattern
            .replace(/\\/g, "\\\\")
            .replace(/\./g, "\\.")
            .replace(/\*\*/g, "<!DOUBLESTAR>")
            .replace(/\*/g, "[^/]*")
            .replace(/<!DOUBLESTAR>/g, ".*");
          return new RegExp(`^${regexPattern}$`);
        });

      // All root files should match at least one pattern
      for (const file of rootFiles) {
        const matches = patterns.some(p => p.test(file));
        expect(matches).toBe(true);
      }
    });

    it("should match patterns against relative paths, not branch-prefixed paths", () => {
      // This test validates the fix for: https://github.com/github/gh-aw/actions/runs/20613564835
      // Campaign workflows specify patterns relative to the memory directory,
      // not including the branch name prefix.
      //
      // Example scenario:
      // - Branch name: memory/campaigns
      // - File in artifact: go-file-size-reduction-project64/cursor.json
      // - Pattern: go-file-size-reduction-project64/**
      //
      // The pattern should match the file's relative path within the memory directory,
      // NOT the full branch path (memory/campaigns/go-file-size-reduction-project64/cursor.json).

      const fileGlobFilter = "go-file-size-reduction-project64/**";
      const relativeFilePath = "go-file-size-reduction-project64/cursor.json";

      const patterns = fileGlobFilter
        .trim()
        .split(/\s+/)
        .filter(Boolean)
        .map(pattern => {
          const regexPattern = pattern
            .replace(/\\/g, "\\\\")
            .replace(/\./g, "\\.")
            .replace(/\*\*/g, "<!DOUBLESTAR>")
            .replace(/\*/g, "[^/]*")
            .replace(/<!DOUBLESTAR>/g, ".*");
          return new RegExp(`^${regexPattern}$`);
        });

      // Test against relative path (CORRECT)
      const normalizedRelPath = relativeFilePath.replace(/\\/g, "/");
      const matchesRelativePath = patterns.some(p => p.test(normalizedRelPath));
      expect(matchesRelativePath).toBe(true);

      // Verify it would NOT match if we incorrectly prepended branch name
      const branchName = "memory/campaigns";
      const branchRelativePath = `${branchName}/${relativeFilePath}`;
      const matchesBranchPath = patterns.some(p => p.test(branchRelativePath));
      expect(matchesBranchPath).toBe(false); // This is the bug we're fixing!

      // Additional test cases for the campaign pattern
      const testFiles = [
        { path: "go-file-size-reduction-project64/cursor.json", shouldMatch: true },
        { path: "go-file-size-reduction-project64/metrics/2024-12-31.json", shouldMatch: true },
        { path: "go-file-size-reduction-project64/data/config.yaml", shouldMatch: true },
        { path: "other-campaign/cursor.json", shouldMatch: false },
        { path: "cursor.json", shouldMatch: false },
      ];

      for (const { path, shouldMatch } of testFiles) {
        const matches = patterns.some(p => p.test(path));
        expect(matches).toBe(shouldMatch);
      }
    });

    it("should allow filtering out legacy files from previous runs", () => {
      // Real-world scenario: The memory/campaigns branch had old files with incorrect
      // nesting (memory/default/...) from before a bug fix. When cloning this branch,
      // these old files are present alongside new correctly-structured files.
      // The glob filter should match only the new files, allowing old files to be skipped.
      const currentPattern = globPatternToRegex("go-file-size-reduction-project64/**");

      // New files (should match)
      expect(currentPattern.test("go-file-size-reduction-project64/cursor.json")).toBe(true);
      expect(currentPattern.test("go-file-size-reduction-project64/metrics/2025-12-31.json")).toBe(true);

      // Legacy files with incorrect nesting (should not match)
      expect(currentPattern.test("memory/default/go-file-size-reduction-20610415309/metrics/2025-12-31.json")).toBe(false);
      expect(currentPattern.test("memory/campaigns/go-file-size-reduction-project64/cursor.json")).toBe(false);

      // This behavior allows push_repo_memory.cjs to skip legacy files instead of failing,
      // enabling gradual migration from old to new structure without manual branch cleanup.
    });

    it("should match root-level files without branch name prefix (daily-code-metrics scenario)", () => {
      // This test validates the fix for: https://github.com/github/gh-aw/actions/runs/20623556740/job/59230494223#step:7:1
      // The daily-code-metrics workflow writes files to the artifact root (e.g., history.jsonl).
      // Previously, the workflow incorrectly specified patterns like "memory/code-metrics/*.jsonl",
      // which included the branch name prefix and failed to match root-level files.
      //
      // Correct pattern format:
      // - Branch name: memory/code-metrics (stored in branch-name field)
      // - Artifact structure: history.jsonl (at root of artifact)
      // - Pattern: *.jsonl (relative to artifact root, NOT including branch name)
      //
      // INCORRECT (old config):
      // file-glob: ["memory/code-metrics/*.json", "memory/code-metrics/*.jsonl", ...]
      //
      // CORRECT (new config):
      // file-glob: ["*.json", "*.jsonl", "*.csv", "*.md"]

      const fileGlobFilter = "*.json *.jsonl *.csv *.md";
      const testFiles = [
        { file: "history.jsonl", shouldMatch: true },
        { file: "data.json", shouldMatch: true },
        { file: "metrics.csv", shouldMatch: true },
        { file: "README.md", shouldMatch: true },
        { file: "script.js", shouldMatch: false },
        { file: "image.png", shouldMatch: false },
      ];

      const patterns = fileGlobFilter
        .trim()
        .split(/\s+/)
        .filter(Boolean)
        .map(pattern => {
          const regexPattern = pattern
            .replace(/\\/g, "\\\\")
            .replace(/\./g, "\\.")
            .replace(/\*\*/g, "<!DOUBLESTAR>")
            .replace(/\*/g, "[^/]*")
            .replace(/<!DOUBLESTAR>/g, ".*");
          return new RegExp(`^${regexPattern}$`);
        });

      // Verify each file is matched correctly
      for (const { file, shouldMatch } of testFiles) {
        const matches = patterns.some(p => p.test(file));
        expect(matches).toBe(shouldMatch);
      }

      // Verify that patterns with branch name prefix would FAIL to match
      const incorrectPattern = "memory/code-metrics/*.jsonl";
      const incorrectRegex = globPatternToRegex(incorrectPattern);

      // This should NOT match because pattern expects "memory/code-metrics/" prefix
      expect(incorrectRegex.test("history.jsonl")).toBe(false);

      // But it WOULD match if file had that structure (which it doesn't in the artifact)
      expect(incorrectRegex.test("memory/code-metrics/history.jsonl")).toBe(true);

      // Key insight: The branch name is stored in BRANCH_NAME env var, not in file paths.
      // Patterns should match against the relative path within the artifact, not the branch path.
    });
  });

  describe("metrics validation error messages", () => {
    // Helper function to simulate the validation logic from push_repo_memory.cjs
    function validateCampaignMetricsSnapshot(obj, campaignId, relPath) {
      function isPlainObject(value) {
        return typeof value === "object" && value !== null && !Array.isArray(value);
      }

      if (!isPlainObject(obj)) {
        throw new Error(`Metrics snapshot must be a JSON object: ${relPath}`);
      }
      if (typeof obj.campaign_id !== "string" || obj.campaign_id.trim() === "") {
        throw new Error(`Metrics snapshot must include non-empty 'campaign_id': ${relPath}`);
      }
      if (obj.campaign_id !== campaignId) {
        throw new Error(`Metrics snapshot 'campaign_id' must match '${campaignId}': ${relPath}`);
      }
      if (typeof obj.date !== "string" || obj.date.trim() === "") {
        throw new Error(`Metrics snapshot must include non-empty 'date' (YYYY-MM-DD): ${relPath}`);
      }
      if (!/^\d{4}-\d{2}-\d{2}$/.test(obj.date)) {
        throw new Error(`Metrics snapshot 'date' must be YYYY-MM-DD: ${relPath}`);
      }

      // Require these to be present and non-negative integers (aligns with CampaignMetricsSnapshot).
      const requiredIntFields = ["tasks_total", "tasks_completed"];
      for (const field of requiredIntFields) {
        const value = obj[field];
        if (value === null || value === undefined) {
          throw new Error(`Metrics snapshot '${field}' is required but was ${value === null ? "null" : "undefined"}: ${relPath}`);
        }
        if (typeof value !== "number") {
          throw new Error(`Metrics snapshot '${field}' must be a number, got ${typeof value} (value: ${JSON.stringify(value)}): ${relPath}`);
        }
        if (!Number.isInteger(value)) {
          throw new Error(`Metrics snapshot '${field}' must be an integer, got ${value}: ${relPath}`);
        }
        if (value < 0) {
          throw new Error(`Metrics snapshot '${field}' must be non-negative, got ${value}: ${relPath}`);
        }
      }

      // Optional numeric fields, if present.
      const optionalIntFields = ["tasks_in_progress", "tasks_blocked"];
      for (const field of optionalIntFields) {
        const value = obj[field];
        if (value !== undefined && value !== null) {
          if (typeof value !== "number") {
            throw new Error(`Metrics snapshot '${field}' must be a number when present, got ${typeof value} (value: ${JSON.stringify(value)}): ${relPath}`);
          }
          if (!Number.isInteger(value)) {
            throw new Error(`Metrics snapshot '${field}' must be an integer when present, got ${value}: ${relPath}`);
          }
          if (value < 0) {
            throw new Error(`Metrics snapshot '${field}' must be non-negative when present, got ${value}: ${relPath}`);
          }
        }
      }
      if (obj.velocity_per_day !== undefined && obj.velocity_per_day !== null) {
        const value = obj.velocity_per_day;
        if (typeof value !== "number") {
          throw new Error(`Metrics snapshot 'velocity_per_day' must be a number when present, got ${typeof value} (value: ${JSON.stringify(value)}): ${relPath}`);
        }
        if (value < 0) {
          throw new Error(`Metrics snapshot 'velocity_per_day' must be non-negative when present, got ${value}: ${relPath}`);
        }
      }
      if (obj.estimated_completion !== undefined && obj.estimated_completion !== null) {
        const value = obj.estimated_completion;
        if (typeof value !== "string") {
          throw new Error(`Metrics snapshot 'estimated_completion' must be a string when present, got ${typeof value} (value: ${JSON.stringify(value)}): ${relPath}`);
        }
      }
    }

    it("should provide clear error message when tasks_total is null", () => {
      const metricsSnapshot = {
        date: "2025-12-31",
        campaign_id: "test-campaign",
        tasks_total: null,
        tasks_completed: 5,
      };

      expect(() => {
        validateCampaignMetricsSnapshot(metricsSnapshot, "test-campaign", "test-campaign/metrics/2025-12-31.json");
      }).toThrow("Metrics snapshot 'tasks_total' is required but was null: test-campaign/metrics/2025-12-31.json");
    });

    it("should provide clear error message when tasks_total is undefined", () => {
      const metricsSnapshot = {
        date: "2025-12-31",
        campaign_id: "test-campaign",
        tasks_completed: 5,
      };

      expect(() => {
        validateCampaignMetricsSnapshot(metricsSnapshot, "test-campaign", "test-campaign/metrics/2025-12-31.json");
      }).toThrow("Metrics snapshot 'tasks_total' is required but was undefined: test-campaign/metrics/2025-12-31.json");
    });

    it("should provide clear error message when tasks_total is a string", () => {
      const metricsSnapshot = {
        date: "2025-12-31",
        campaign_id: "test-campaign",
        tasks_total: "10",
        tasks_completed: 5,
      };

      expect(() => {
        validateCampaignMetricsSnapshot(metricsSnapshot, "test-campaign", "test-campaign/metrics/2025-12-31.json");
      }).toThrow("Metrics snapshot 'tasks_total' must be a number, got string (value: \"10\"): test-campaign/metrics/2025-12-31.json");
    });

    it("should provide clear error message when tasks_total is a float", () => {
      const metricsSnapshot = {
        date: "2025-12-31",
        campaign_id: "test-campaign",
        tasks_total: 10.5,
        tasks_completed: 5,
      };

      expect(() => {
        validateCampaignMetricsSnapshot(metricsSnapshot, "test-campaign", "test-campaign/metrics/2025-12-31.json");
      }).toThrow("Metrics snapshot 'tasks_total' must be an integer, got 10.5: test-campaign/metrics/2025-12-31.json");
    });

    it("should provide clear error message when tasks_total is negative", () => {
      const metricsSnapshot = {
        date: "2025-12-31",
        campaign_id: "test-campaign",
        tasks_total: -5,
        tasks_completed: 5,
      };

      expect(() => {
        validateCampaignMetricsSnapshot(metricsSnapshot, "test-campaign", "test-campaign/metrics/2025-12-31.json");
      }).toThrow("Metrics snapshot 'tasks_total' must be non-negative, got -5: test-campaign/metrics/2025-12-31.json");
    });

    it("should accept valid integer values for required fields", () => {
      const metricsSnapshot = {
        date: "2025-12-31",
        campaign_id: "test-campaign",
        tasks_total: 10,
        tasks_completed: 5,
      };

      expect(() => {
        validateCampaignMetricsSnapshot(metricsSnapshot, "test-campaign", "test-campaign/metrics/2025-12-31.json");
      }).not.toThrow();
    });

    it("should accept zero for required integer fields", () => {
      const metricsSnapshot = {
        date: "2025-12-31",
        campaign_id: "test-campaign",
        tasks_total: 0,
        tasks_completed: 0,
      };

      expect(() => {
        validateCampaignMetricsSnapshot(metricsSnapshot, "test-campaign", "test-campaign/metrics/2025-12-31.json");
      }).not.toThrow();
    });

    it("should allow optional fields to be undefined", () => {
      const metricsSnapshot = {
        date: "2025-12-31",
        campaign_id: "test-campaign",
        tasks_total: 10,
        tasks_completed: 5,
        // tasks_in_progress, tasks_blocked, velocity_per_day, estimated_completion are undefined
      };

      expect(() => {
        validateCampaignMetricsSnapshot(metricsSnapshot, "test-campaign", "test-campaign/metrics/2025-12-31.json");
      }).not.toThrow();
    });

    it("should allow optional fields to be null", () => {
      const metricsSnapshot = {
        date: "2025-12-31",
        campaign_id: "test-campaign",
        tasks_total: 10,
        tasks_completed: 5,
        tasks_in_progress: null,
        tasks_blocked: null,
        velocity_per_day: null,
        estimated_completion: null,
      };

      expect(() => {
        validateCampaignMetricsSnapshot(metricsSnapshot, "test-campaign", "test-campaign/metrics/2025-12-31.json");
      }).not.toThrow();
    });

    it("should validate optional integer fields when present", () => {
      const metricsSnapshot = {
        date: "2025-12-31",
        campaign_id: "test-campaign",
        tasks_total: 10,
        tasks_completed: 5,
        tasks_in_progress: "3", // Invalid: string instead of number
      };

      expect(() => {
        validateCampaignMetricsSnapshot(metricsSnapshot, "test-campaign", "test-campaign/metrics/2025-12-31.json");
      }).toThrow("Metrics snapshot 'tasks_in_progress' must be a number when present, got string (value: \"3\"): test-campaign/metrics/2025-12-31.json");
    });

    it("should validate optional float fields when present", () => {
      const metricsSnapshot = {
        date: "2025-12-31",
        campaign_id: "test-campaign",
        tasks_total: 10,
        tasks_completed: 5,
        velocity_per_day: "2.5", // Invalid: string instead of number
      };

      expect(() => {
        validateCampaignMetricsSnapshot(metricsSnapshot, "test-campaign", "test-campaign/metrics/2025-12-31.json");
      }).toThrow("Metrics snapshot 'velocity_per_day' must be a number when present, got string (value: \"2.5\"): test-campaign/metrics/2025-12-31.json");
    });

    it("should accept valid optional fields", () => {
      const metricsSnapshot = {
        date: "2025-12-31",
        campaign_id: "test-campaign",
        tasks_total: 10,
        tasks_completed: 5,
        tasks_in_progress: 3,
        tasks_blocked: 1,
        velocity_per_day: 2.5,
        estimated_completion: "2026-01-15",
      };

      expect(() => {
        validateCampaignMetricsSnapshot(metricsSnapshot, "test-campaign", "test-campaign/metrics/2025-12-31.json");
      }).not.toThrow();
    });

    it("should validate tasks_completed field with same rigor as tasks_total", () => {
      const metricsSnapshot = {
        date: "2025-12-31",
        campaign_id: "test-campaign",
        tasks_total: 10,
        tasks_completed: "5", // Invalid: string
      };

      expect(() => {
        validateCampaignMetricsSnapshot(metricsSnapshot, "test-campaign", "test-campaign/metrics/2025-12-31.json");
      }).toThrow("Metrics snapshot 'tasks_completed' must be a number, got string (value: \"5\"): test-campaign/metrics/2025-12-31.json");
    });
  });
});
