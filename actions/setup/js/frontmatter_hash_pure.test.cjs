// @ts-check
import { describe, it, expect } from "vitest";
const path = require("path");
const fs = require("fs");
const {
  computeFrontmatterHash,
  extractFrontmatterAndBody,
  parseSimpleYAML,
  extractRelevantTemplateExpressions,
  marshalCanonicalJSON,
  marshalSorted,
  extractHashFromLockFile,
} = require("./frontmatter_hash_pure.cjs");

describe("frontmatter_hash_pure", () => {
  describe("extractFrontmatterAndBody", () => {
    it("should extract frontmatter and body", () => {
      const content = `---
engine: copilot
description: Test workflow
---

# Workflow Body

Test content here`;

      const result = extractFrontmatterAndBody(content);
      expect(result.frontmatter).toEqual({
        engine: "copilot",
        description: "Test workflow",
      });
      expect(result.markdown).toContain("# Workflow Body");
    });

    it("should handle empty frontmatter", () => {
      const content = `# No frontmatter here`;
      const result = extractFrontmatterAndBody(content);
      expect(result.frontmatter).toEqual({});
      expect(result.markdown).toBe(content);
    });
  });

  describe("parseSimpleYAML", () => {
    it("should parse simple key-value pairs", () => {
      const yaml = `engine: copilot
description: Test workflow
timeout-minutes: 30`;
      
      const result = parseSimpleYAML(yaml);
      expect(result).toEqual({
        engine: "copilot",
        description: "Test workflow",
        "timeout-minutes": 30,
      });
    });

    it("should parse boolean values", () => {
      const yaml = `enabled: true
disabled: false`;
      
      const result = parseSimpleYAML(yaml);
      expect(result).toEqual({
        enabled: true,
        disabled: false,
      });
    });

    it("should parse arrays", () => {
      const yaml = `labels:
  - bug
  - enhancement
  - documentation`;
      
      const result = parseSimpleYAML(yaml);
      expect(result.labels).toEqual(["bug", "enhancement", "documentation"]);
    });

    it("should ignore comments", () => {
      const yaml = `# This is a comment
engine: copilot
# Another comment
description: Test`;
      
      const result = parseSimpleYAML(yaml);
      expect(result).toEqual({
        engine: "copilot",
        description: "Test",
      });
    });
  });

  describe("extractRelevantTemplateExpressions", () => {
    it("should extract env. expressions", () => {
      const markdown = "Use ${{ env.MY_VAR }} in workflow";
      const expressions = extractRelevantTemplateExpressions(markdown);
      expect(expressions).toEqual(["${{ env.MY_VAR }}"]);
    });

    it("should extract vars. expressions", () => {
      const markdown = "Use ${{ vars.CONFIG }} in workflow";
      const expressions = extractRelevantTemplateExpressions(markdown);
      expect(expressions).toEqual(["${{ vars.CONFIG }}"]);
    });

    it("should extract both env and vars", () => {
      const markdown = "Use ${{ env.VAR1 }} and ${{ vars.VAR2 }}";
      const expressions = extractRelevantTemplateExpressions(markdown);
      expect(expressions).toEqual(["${{ env.VAR1 }}", "${{ vars.VAR2 }}"]);
    });

    it("should ignore non-env/vars expressions", () => {
      const markdown = "Use ${{ github.repository }} and ${{ env.MY_VAR }}";
      const expressions = extractRelevantTemplateExpressions(markdown);
      expect(expressions).toEqual(["${{ env.MY_VAR }}"]);
    });

    it("should remove duplicates and sort", () => {
      const markdown = "${{ env.B }} and ${{ env.A }} and ${{ env.B }}";
      const expressions = extractRelevantTemplateExpressions(markdown);
      expect(expressions).toEqual(["${{ env.A }}", "${{ env.B }}"]);
    });
  });

  describe("marshalSorted", () => {
    it("should sort object keys", () => {
      const data = { z: 1, a: 2, m: 3 };
      const json = marshalSorted(data);
      expect(json).toBe('{"a":2,"m":3,"z":1}');
    });

    it("should handle nested objects", () => {
      const data = { b: { y: 1, x: 2 }, a: 3 };
      const json = marshalSorted(data);
      expect(json).toBe('{"a":3,"b":{"x":2,"y":1}}');
    });

    it("should handle arrays", () => {
      const data = { arr: [3, 1, 2] };
      const json = marshalSorted(data);
      expect(json).toBe('{"arr":[3,1,2]}');
    });

    it("should handle null and undefined", () => {
      expect(marshalSorted(null)).toBe("null");
      expect(marshalSorted(undefined)).toBe("null");
    });

    it("should handle primitives", () => {
      expect(marshalSorted("test")).toBe('"test"');
      expect(marshalSorted(42)).toBe("42");
      expect(marshalSorted(true)).toBe("true");
    });
  });

  describe("marshalCanonicalJSON", () => {
    it("should produce deterministic JSON", () => {
      const data1 = { z: 1, a: 2 };
      const data2 = { a: 2, z: 1 };
      
      const json1 = marshalCanonicalJSON(data1);
      const json2 = marshalCanonicalJSON(data2);
      
      expect(json1).toBe(json2);
      expect(json1).toBe('{"a":2,"z":1}');
    });
  });

  describe("extractHashFromLockFile", () => {
    it("should extract hash from lock file", () => {
      const content = `# frontmatter-hash: abc123def456

name: "Test Workflow"`;
      
      const hash = extractHashFromLockFile(content);
      expect(hash).toBe("abc123def456");
    });

    it("should return empty string if no hash found", () => {
      const content = `name: "Test Workflow"`;
      const hash = extractHashFromLockFile(content);
      expect(hash).toBe("");
    });
  });

  describe("computeFrontmatterHash", () => {
    it("should compute hash for simple workflow", async () => {
      const tempDir = require("os").tmpdir();
      const testFile = path.join(tempDir, `test-workflow-${Date.now()}.md`);
      
      const content = `---
engine: copilot
description: Test workflow
---

# Test Workflow

Use \${{ env.TEST_VAR }} here.`;
      
      fs.writeFileSync(testFile, content, "utf8");
      
      try {
        const hash = await computeFrontmatterHash(testFile);
        
        // Verify hash format
        expect(hash).toMatch(/^[a-f0-9]{64}$/);
        expect(hash.length).toBe(64);
        
        // Compute again to verify determinism
        const hash2 = await computeFrontmatterHash(testFile);
        expect(hash).toBe(hash2);
      } finally {
        fs.unlinkSync(testFile);
      }
    });

    it("should produce consistent hash for same input", async () => {
      const tempDir = require("os").tmpdir();
      const testFile = path.join(tempDir, `test-workflow-${Date.now()}.md`);
      
      const content = `---
engine: copilot
on:
  schedule: daily
---

# Test`;
      
      fs.writeFileSync(testFile, content, "utf8");
      
      try {
        const hashes = [];
        for (let i = 0; i < 5; i++) {
          hashes.push(await computeFrontmatterHash(testFile));
        }
        
        // All hashes should be identical
        const uniqueHashes = new Set(hashes);
        expect(uniqueHashes.size).toBe(1);
      } finally {
        fs.unlinkSync(testFile);
      }
    });
  });
});
