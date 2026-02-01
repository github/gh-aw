// @ts-check
import { describe, it, expect } from "vitest";
const path = require("path");
const fs = require("fs");
const {
  computeFrontmatterHash,
  extractFrontmatterAndBody,
  extractImportsFromText,
  extractRelevantTemplateExpressions,
  marshalCanonicalJSON,
  marshalSorted,
  extractHashFromLockFile,
  normalizeFrontmatterText,
  defaultFileReader,
  createGitHubFileReader,
} = require("./frontmatter_hash_pure.cjs");

describe("frontmatter_hash_pure (text-based)", () => {
  describe("extractFrontmatterAndBody", () => {
    it("should extract frontmatter text and body", () => {
      const content = `---
engine: copilot
description: Test workflow
---

# Workflow Body

Test content here`;

      const result = extractFrontmatterAndBody(content);
      expect(result.frontmatterText).toContain("engine: copilot");
      expect(result.frontmatterText).toContain("description: Test workflow");
      expect(result.markdown).toContain("# Workflow Body");
    });

    it("should handle empty frontmatter", () => {
      const content = `# No frontmatter here`;
      const result = extractFrontmatterAndBody(content);
      expect(result.frontmatterText).toBe("");
      expect(result.markdown).toBe(content);
    });

    it("should handle frontmatter with imports", () => {
      const content = `---
engine: copilot
imports:
  - shared/test.md
  - shared/common.md
---

# Body`;

      const result = extractFrontmatterAndBody(content);
      expect(result.frontmatterText).toContain("imports:");
      expect(result.frontmatterText).toContain("- shared/test.md");
    });
  });

  describe("extractImportsFromText", () => {
    it("should extract imports from frontmatter text", () => {
      const frontmatterText = `engine: copilot
imports:
  - shared/test.md
  - shared/common.md
description: Test`;

      const result = extractImportsFromText(frontmatterText);
      expect(result).toEqual(["shared/test.md", "shared/common.md"]);
    });

    it("should handle no imports", () => {
      const frontmatterText = `engine: copilot
description: Test`;

      const result = extractImportsFromText(frontmatterText);
      expect(result).toEqual([]);
    });

    it("should handle imports with quotes", () => {
      const frontmatterText = `imports:
  - "shared/test.md"
  - 'shared/common.md'`;

      const result = extractImportsFromText(frontmatterText);
      expect(result).toEqual(["shared/test.md", "shared/common.md"]);
    });

    it("should stop at next top-level key", () => {
      const frontmatterText = `imports:
  - shared/test.md
engine: copilot`;

      const result = extractImportsFromText(frontmatterText);
      expect(result).toEqual(["shared/test.md"]);
    });
  });

  describe("extractRelevantTemplateExpressions", () => {
    it("should extract env expressions", () => {
      const markdown = "Use $" + "{{ env.MY_VAR }} here\nAnd also $" + "{{ env.OTHER }}";

      const result = extractRelevantTemplateExpressions(markdown);
      expect(result).toEqual(["$" + "{{ env.MY_VAR }}", "$" + "{{ env.OTHER }}"]);
    });

    it("should extract vars expressions", () => {
      const markdown = "Use $" + "{{ vars.CONFIG }} here";

      const result = extractRelevantTemplateExpressions(markdown);
      expect(result).toEqual(["$" + "{{ vars.CONFIG }}"]);
    });

    it("should ignore non-env/vars expressions", () => {
      const markdown = "Use $" + "{{ github.repository }} here\nBut include $" + "{{ env.TEST }}";

      const result = extractRelevantTemplateExpressions(markdown);
      expect(result).toEqual(["$" + "{{ env.TEST }}"]);
    });

    it("should deduplicate and sort expressions", () => {
      const markdown = "$" + "{{ env.B }} and $" + "{{ env.A }} and $" + "{{ env.B }}";

      const result = extractRelevantTemplateExpressions(markdown);
      expect(result).toEqual(["$" + "{{ env.A }}", "$" + "{{ env.B }}"]);
    });
  });

  describe("marshalCanonicalJSON", () => {
    it("should serialize with sorted keys", () => {
      const data = { c: 3, a: 1, b: 2 };
      const result = marshalCanonicalJSON(data);
      expect(result).toBe('{"a":1,"b":2,"c":3}');
    });

    it("should handle nested objects", () => {
      const data = { outer: { z: 26, a: 1 } };
      const result = marshalCanonicalJSON(data);
      expect(result).toBe('{"outer":{"a":1,"z":26}}');
    });

    it("should handle arrays", () => {
      const data = { items: [3, 1, 2] };
      const result = marshalCanonicalJSON(data);
      expect(result).toBe('{"items":[3,1,2]}');
    });

    it("should handle mixed types", () => {
      const data = {
        str: "value",
        num: 42,
        bool: true,
        nil: null,
        arr: [1, 2],
        obj: { x: 1 },
      };
      const result = marshalCanonicalJSON(data);
      expect(result).toBe('{"arr":[1,2],"bool":true,"nil":null,"num":42,"obj":{"x":1},"str":"value"}');
    });
  });

  describe("marshalSorted", () => {
    it("should handle primitives", () => {
      expect(marshalSorted("test")).toBe('"test"');
      expect(marshalSorted(42)).toBe("42");
      expect(marshalSorted(true)).toBe("true");
      expect(marshalSorted(null)).toBe("null");
    });

    it("should handle empty collections", () => {
      expect(marshalSorted([])).toBe("[]");
      expect(marshalSorted({})).toBe("{}");
    });
  });

  describe("extractHashFromLockFile", () => {
    it("should extract hash from lock file", () => {
      const content = `# frontmatter-hash: abc123def456

name: "Test Workflow"
on:
  push:`;

      const result = extractHashFromLockFile(content);
      expect(result).toBe("abc123def456");
    });

    it("should return empty string if no hash found", () => {
      const content = `name: "Test Workflow"
on:
  push:`;

      const result = extractHashFromLockFile(content);
      expect(result).toBe("");
    });
  });

  describe("normalizeFrontmatterText", () => {
    it("should trim whitespace", () => {
      const text = `  engine: copilot  
  description: test  `;

      const result = normalizeFrontmatterText(text);
      expect(result).toBe("engine: copilot  \n  description: test");
    });

    it("should normalize line endings", () => {
      const text = "engine: copilot\r\ndescription: test\r\n";

      const result = normalizeFrontmatterText(text);
      expect(result).toBe("engine: copilot\ndescription: test");
    });
  });

  describe("computeFrontmatterHash", () => {
    it("should compute hash for simple frontmatter", async () => {
      // Create a temporary test file
      const testFile = path.join(__dirname, "test-workflow-hash-simple.md");
      const content = "---\nengine: copilot\ndescription: Test workflow\n---\n\nUse $" + "{{ env.TEST }} here";

      fs.writeFileSync(testFile, content, "utf8");

      try {
        const hash = await computeFrontmatterHash(testFile);

        // Hash should be a 64-character hex string
        expect(hash).toMatch(/^[a-f0-9]{64}$/);

        // Computing again should produce the same hash (deterministic)
        const hash2 = await computeFrontmatterHash(testFile);
        expect(hash2).toBe(hash);
      } finally {
        if (fs.existsSync(testFile)) {
          fs.unlinkSync(testFile);
        }
      }
    });

    it("should include template expressions in hash", async () => {
      const testFile1 = path.join(__dirname, "test-workflow-hash-expr1.md");
      const testFile2 = path.join(__dirname, "test-workflow-hash-expr2.md");

      const content1 = "---\nengine: copilot\n---\n\nUse $" + "{{ env.VAR1 }}";
      const content2 = "---\nengine: copilot\n---\n\nUse $" + "{{ env.VAR2 }}";

      fs.writeFileSync(testFile1, content1, "utf8");
      fs.writeFileSync(testFile2, content2, "utf8");

      try {
        const hash1 = await computeFrontmatterHash(testFile1);
        const hash2 = await computeFrontmatterHash(testFile2);

        // Different expressions should produce different hashes
        expect(hash1).not.toBe(hash2);
      } finally {
        if (fs.existsSync(testFile1)) fs.unlinkSync(testFile1);
        if (fs.existsSync(testFile2)) fs.unlinkSync(testFile2);
      }
    });

    it("should work with custom file reader", async () => {
      const tmpDir = fs.mkdtempSync(path.join(require("os").tmpdir(), "frontmatter-hash-test-"));
      const testFile = path.join(tmpDir, "test.md");
      const content = "---\nengine: copilot\ndescription: Test\n---\n\nBody";

      // Create an in-memory file system mock
      const mockFileSystem = {
        [testFile]: content,
      };

      const customFileReader = async filePath => {
        if (mockFileSystem[filePath]) {
          return mockFileSystem[filePath];
        }
        throw new Error(`File not found: ${filePath}`);
      };

      try {
        const hash = await computeFrontmatterHash(testFile, { fileReader: customFileReader });
        expect(hash).toHaveLength(64); // SHA-256 is 64 hex chars
        expect(hash).toMatch(/^[0-9a-f]{64}$/);
      } finally {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("should handle imports with custom file reader", async () => {
      const tmpDir = fs.mkdtempSync(path.join(require("os").tmpdir(), "frontmatter-hash-test-"));
      const mainFile = path.join(tmpDir, "main.md");
      const sharedDir = path.join(tmpDir, "shared");
      const importedFile = path.join(sharedDir, "imported.md");

      // Create an in-memory file system mock
      const mockFileSystem = {
        [mainFile]: "---\nengine: copilot\nimports:\n  - shared/imported.md\n---\n\nMain body",
        [importedFile]: "---\ntools:\n  bash: true\n---\n\nImported content",
      };

      const customFileReader = async filePath => {
        if (mockFileSystem[filePath]) {
          return mockFileSystem[filePath];
        }
        throw new Error(`File not found: ${filePath}`);
      };

      try {
        const hash = await computeFrontmatterHash(mainFile, { fileReader: customFileReader });
        expect(hash).toHaveLength(64);
        expect(hash).toMatch(/^[0-9a-f]{64}$/);
      } finally {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });
  });
});
