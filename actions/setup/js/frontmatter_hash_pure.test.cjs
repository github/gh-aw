// @ts-check
import { describe, it, expect } from "vitest";
const path = require("path");
const fs = require("fs");
const {
  computeFrontmatterHash,
  computeBodyHash,
  extractFrontmatterAndBody,
  extractImportsFromText,
  extractRelevantTemplateExpressions,
  extractAllTemplateExpressions,
  extractRuntimeImportReferences,
  marshalCanonicalJSON,
  marshalSorted,
  extractHashFromLockFile,
  extractBodyHashFromLockFile,
  normalizeFrontmatterText,
  parseBoolFromFrontmatter,
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

    it("should extract path from object-form uses: import", () => {
      const frontmatterText = `imports:
  - uses: ./serena.md
    with:
      languages: ["go"]
  - shared/common.md`;

      const result = extractImportsFromText(frontmatterText);
      // Both the object-form uses: import and the plain string import are extracted.
      expect(result).toEqual(["./serena.md", "shared/common.md"]);
    });

    it("should extract path from object-form path: import", () => {
      const frontmatterText = `imports:
  - path: shared/tool.md
    inputs:
      key: value`;

      const result = extractImportsFromText(frontmatterText);
      // Object-form path: import path is extracted.
      expect(result).toEqual(["shared/tool.md"]);
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

  describe("runtime import expression hashing", () => {
    it("should extract all expressions from runtime imports and legacy import macros", () => {
      const markdown = [
        "{{#runtime-import prompts/direct.md}}",
        "{{#runtime-import? prompts/optional.md}}",
        "{{#import: prompts/legacy.md}}",
        "{{#runtime-import prompts/ranged.md:2-3}}",
        "{{#runtime-import https://example.com/ignored.md}}",
      ].join("\n");

      expect(extractRuntimeImportReferences(markdown)).toEqual([
        { path: "prompts/direct.md", startLine: null, endLine: null },
        { path: "prompts/optional.md", startLine: null, endLine: null },
        { path: "prompts/legacy.md", startLine: null, endLine: null },
        { path: "prompts/ranged.md", startLine: 2, endLine: 3 },
      ]);

      expect(extractAllTemplateExpressions("A $" + "{{ needs.a.outputs.x }} B $" + "{{ vars.CFG }}")).toEqual(["$" + "{{ needs.a.outputs.x }}", "$" + "{{ vars.CFG }}"]);
    });

    it("should include the runtime-import expression set in the frontmatter hash", async () => {
      const workflowPath = "/repo/.github/workflows/runtime-import-hash.md";
      const promptPath = "/repo/.github/prompts/runtime.md";
      const files = new Map([
        [
          workflowPath,
          `---
engine: copilot
on:
  workflow_dispatch:
---
{{#runtime-import prompts/runtime.md}}
`,
        ],
        [promptPath, "Issue: $" + "{{ needs.select.outputs.issue_numbers }}\n"],
      ]);
      const fileReader = async filePath => {
        if (!files.has(filePath)) throw new Error(`missing ${filePath}`);
        return files.get(filePath);
      };

      const hashA = await computeFrontmatterHash(workflowPath, { fileReader });
      files.set(promptPath, "Unrelated text\nIssue: $" + "{{ needs.select.outputs.issue_numbers }}\n");
      const hashSameExpression = await computeFrontmatterHash(workflowPath, { fileReader });
      expect(hashSameExpression).toBe(hashA);

      files.set(promptPath, "Issue: $" + "{{ needs.select.outputs.marker }}\n");
      const hashB = await computeFrontmatterHash(workflowPath, { fileReader });
      expect(hashB).not.toBe(hashA);
    });

    it("should skip symlink runtime imports that resolve outside the workspace", async () => {
      const tempDir = fs.mkdtempSync(path.join(require("os").tmpdir(), "frontmatter-hash-symlink-"));
      const workflowsDir = path.join(tempDir, ".github", "workflows");
      fs.mkdirSync(workflowsDir, { recursive: true });
      const workflowPath = path.join(workflowsDir, "runtime-import-hash.md");
      const outsidePath = path.join(tempDir, "outside.md");
      const symlinkPath = path.join(workflowsDir, "outside-link.md");
      fs.writeFileSync(
        workflowPath,
        `---
engine: copilot
on:
  workflow_dispatch:
---
{{#runtime-import outside-link.md}}
`
      );
      fs.writeFileSync(outsidePath, "Outside $" + "{{ needs.outside.outputs.value }}\n");
      fs.symlinkSync(outsidePath, symlinkPath);

      const hashA = await computeFrontmatterHash(workflowPath);
      fs.writeFileSync(outsidePath, "Outside $" + "{{ needs.outside.outputs.changed }}\n");
      const hashB = await computeFrontmatterHash(workflowPath);

      expect(hashB).toBe(hashA);
    });

    it("should terminate when frontmatter-imported bodies contain cyclic runtime imports", async () => {
      const workflowPath = "/repo/.github/workflows/runtime-import-cycle-hash.md";
      const files = new Map([
        [
          workflowPath,
          `---
engine: copilot
imports:
  - shared/a.md
on:
  workflow_dispatch:
---
Cycle hash coverage.
`,
        ],
        ["/repo/.github/workflows/shared/a.md", "A $" + "{{ needs.a.outputs.value }}\n{{#runtime-import shared/b.md}}\n"],
        ["/repo/.github/workflows/shared/b.md", "B $" + "{{ needs.b.outputs.value }}\n{{#runtime-import shared/a.md}}\n"],
      ]);
      const fileReader = async filePath => {
        if (!files.has(filePath)) throw new Error(`missing ${filePath}`);
        return files.get(filePath);
      };

      const hash = await computeFrontmatterHash(workflowPath, { fileReader });

      expect(hash).toMatch(/^[a-f0-9]{64}$/);
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
    it("should extract hash from old format lock file", () => {
      const content = `# frontmatter-hash: abc123def456

name: "Test Workflow"
on:
  push:`;

      const result = extractHashFromLockFile(content);
      expect(result).toBe("abc123def456");
    });

    it("should extract hash from new JSON metadata format", () => {
      const content = `# gh-aw-metadata: {"schema_version":"v1","frontmatter_hash":"abc123def456789"}

name: "Test Workflow"
on:
  push:`;

      const result = extractHashFromLockFile(content);
      expect(result).toBe("abc123def456789");
    });

    it("should extract hash from new JSON metadata format with additional fields", () => {
      const content = `# gh-aw-metadata: {"schema_version":"v1","frontmatter_hash":"xyz789abc123","stop_time":"2025-01-01T00:00:00Z","compiler_version":"0.1.0"}

name: "Test Workflow"
on:
  push:`;

      const result = extractHashFromLockFile(content);
      expect(result).toBe("xyz789abc123");
    });

    it("should handle new format with whitespace variations", () => {
      const content = `#  gh-aw-metadata:  {"schema_version":"v1","frontmatter_hash":"whitespace123"}

name: "Test Workflow"`;

      const result = extractHashFromLockFile(content);
      expect(result).toBe("whitespace123");
    });

    it("should fall back to old format if JSON parsing fails", () => {
      const content = `# gh-aw-metadata: {invalid json}
# frontmatter-hash: fallback123

name: "Test Workflow"`;

      const result = extractHashFromLockFile(content);
      expect(result).toBe("fallback123");
    });

    it("should prefer new format over old format when both present", () => {
      const content = `# gh-aw-metadata: {"schema_version":"v1","frontmatter_hash":"new123"}
# frontmatter-hash: old123

name: "Test Workflow"`;

      const result = extractHashFromLockFile(content);
      expect(result).toBe("new123");
    });

    it("should return empty string if no hash found", () => {
      const content = `name: "Test Workflow"
on:
  push:`;

      const result = extractHashFromLockFile(content);
      expect(result).toBe("");
    });

    it("should return empty string if metadata has no frontmatter_hash field", () => {
      const content = `# gh-aw-metadata: {"schema_version":"v1"}

name: "Test Workflow"`;

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

  describe("parseBoolFromFrontmatter", () => {
    it("should return true when key is present with value true", () => {
      const frontmatter = "engine: copilot\ninlined-imports: true\ndescription: test";
      expect(parseBoolFromFrontmatter(frontmatter, "inlined-imports")).toBe(true);
    });

    it("should return false when key is present with value false", () => {
      const frontmatter = "engine: copilot\ninlined-imports: false\ndescription: test";
      expect(parseBoolFromFrontmatter(frontmatter, "inlined-imports")).toBe(false);
    });

    it("should return false when key is absent", () => {
      const frontmatter = "engine: copilot\ndescription: test";
      expect(parseBoolFromFrontmatter(frontmatter, "inlined-imports")).toBe(false);
    });

    it("should return false for empty frontmatter", () => {
      expect(parseBoolFromFrontmatter("", "inlined-imports")).toBe(false);
    });
  });

  describe("computeFrontmatterHash", () => {
    it.each([
      {
        id: "FH-TV-001",
        content: "---\n---\n\n# Empty Workflow\n",
        expectedHash: "4c8309afbcf816cd80c0824dce2b50047834b29e14b34b96953e88ae81048c46",
      },
      {
        id: "FH-TV-002",
        content: "---\nengine: copilot\ndescription: Test workflow\non:\n  schedule: daily\n---\n\n# Test Workflow\n",
        expectedHash: "b9def9907e3328e2e03e8c47c315723df39788f251627313b1a984bb61b9cbce",
      },
      {
        id: "FH-TV-003",
        content:
          "---\nengine: claude\ndescription: Complex workflow\ntracker-id: complex-test\ntimeout-minutes: 30\non:\n  schedule: daily\n  workflow_dispatch: true\npermissions:\n  contents: read\n  actions: read\ntools:\n  playwright:\n    version: v1.41.0\nlabels:\n  - test\n  - complex\nbots:\n  - copilot\n---\n\n# Complex Workflow\n",
        expectedHash: "8c63a05ef42cbfaff9be87a06257282cb4dcb952f71481d9d65ec3037003dbe8",
      },
    ])("should match Appendix A vector $id", async ({ content, expectedHash }) => {
      const testFile = path.join(__dirname, `test-workflow-${expectedHash}.md`);
      fs.writeFileSync(testFile, content, "utf8");
      try {
        const hash = await computeFrontmatterHash(testFile);
        expect(hash).toBe(expectedHash);
      } finally {
        if (fs.existsSync(testFile)) {
          fs.unlinkSync(testFile);
        }
      }
    });

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

    it("should include body-text in hash when inlined-imports is true", async () => {
      const tmpDir = fs.mkdtempSync(path.join(require("os").tmpdir(), "frontmatter-hash-test-"));
      const testFile = path.join(tmpDir, "test.md");

      const withBody = "---\nengine: copilot\ninlined-imports: true\n---\n\nBody content here";
      const withDifferentBody = "---\nengine: copilot\ninlined-imports: true\n---\n\nDifferent body content";
      const withoutFlag = "---\nengine: copilot\n---\n\nBody content here";

      const fileSystem = {};
      const makeReader = content => async () => content;

      try {
        const hashWithBody = await computeFrontmatterHash(testFile, { fileReader: makeReader(withBody) });
        const hashDifferentBody = await computeFrontmatterHash(testFile, { fileReader: makeReader(withDifferentBody) });
        const hashWithoutFlag = await computeFrontmatterHash(testFile, { fileReader: makeReader(withoutFlag) });

        // Different body content → different hash when inlined-imports: true
        expect(hashWithBody).not.toBe(hashDifferentBody);
        // Same body but without inlined-imports flag → different canonical data → different hash
        expect(hashWithBody).not.toBe(hashWithoutFlag);
      } finally {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("should not include body-text in hash when inlined-imports is false", async () => {
      const tmpDir = fs.mkdtempSync(path.join(require("os").tmpdir(), "frontmatter-hash-test-"));
      const testFile = path.join(tmpDir, "test.md");

      const withBodyA = "---\nengine: copilot\n---\n\nBody content A";
      const withBodyB = "---\nengine: copilot\n---\n\nBody content B";

      const makeReader = content => async () => content;

      try {
        const hashA = await computeFrontmatterHash(testFile, { fileReader: makeReader(withBodyA) });
        const hashB = await computeFrontmatterHash(testFile, { fileReader: makeReader(withBodyB) });

        // Body changes should not affect hash when inlined-imports is not set
        expect(hashA).toBe(hashB);
      } finally {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("should reject oversized normalized frontmatter input (FH-TV-NEG-001)", async () => {
      const testFile = path.join(__dirname, "test-workflow-hash-oversized.md");
      const oversizedValue = "a".repeat(1_048_577);
      const content = `---\ndescription: ${oversizedValue}\n---\n\n# Oversized Workflow`;

      fs.writeFileSync(testFile, content, "utf8");

      try {
        await expect(computeFrontmatterHash(testFile)).rejects.toThrow("frontmatter hash input exceeds 1048576 bytes after normalization");
      } finally {
        if (fs.existsSync(testFile)) {
          fs.unlinkSync(testFile);
        }
      }
    });
  });

  describe("extractBodyHashFromLockFile", () => {
    it("should return empty string when no body hash is present", () => {
      const content = `# gh-aw-metadata: {"schema_version":"v3","frontmatter_hash":"abc123"}
name: "Test Workflow"`;
      expect(extractBodyHashFromLockFile(content)).toBe("");
    });

    it("should extract body hash from JSON metadata format", () => {
      const content = `# gh-aw-metadata: {"schema_version":"v4","frontmatter_hash":"abc123","body_hash":"def456"}
name: "Test Workflow"`;
      expect(extractBodyHashFromLockFile(content)).toBe("def456");
    });

    it("should return empty string when no gh-aw-metadata comment is present", () => {
      const content = `# frontmatter-hash: abc123
name: "Test Workflow"`;
      expect(extractBodyHashFromLockFile(content)).toBe("");
    });

    it("should return empty string when metadata JSON is invalid", () => {
      const content = `# gh-aw-metadata: {invalid}
name: "Test Workflow"`;
      expect(extractBodyHashFromLockFile(content)).toBe("");
    });
  });

  describe("computeBodyHash", () => {
    it("should compute a 64-char hex SHA-256 hash", async () => {
      const tmpDir = fs.mkdtempSync(path.join(require("os").tmpdir(), "body-hash-test-"));
      const testFile = path.join(tmpDir, "test.md");
      const content = "---\nengine: copilot\n---\n\n# My Workflow\n\nDo some stuff.";
      const makeReader = () => async () => content;

      try {
        const hash = await computeBodyHash(testFile, { fileReader: makeReader() });
        expect(hash).toMatch(/^[a-f0-9]{64}$/);
      } finally {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("should produce the same hash for identical body content", async () => {
      const tmpDir = fs.mkdtempSync(path.join(require("os").tmpdir(), "body-hash-test-"));
      const testFile = path.join(tmpDir, "test.md");
      const content = "---\nengine: copilot\n---\n\n# My Workflow\n\nDo some stuff.";
      const makeReader = () => async () => content;

      try {
        const hash1 = await computeBodyHash(testFile, { fileReader: makeReader() });
        const hash2 = await computeBodyHash(testFile, { fileReader: makeReader() });
        expect(hash1).toBe(hash2);
      } finally {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("should produce different hashes when body content differs", async () => {
      const tmpDir = fs.mkdtempSync(path.join(require("os").tmpdir(), "body-hash-test-"));
      const testFile = path.join(tmpDir, "test.md");
      const contentA = "---\nengine: copilot\n---\n\n# Body A";
      const contentB = "---\nengine: copilot\n---\n\n# Body B";

      try {
        const hashA = await computeBodyHash(testFile, { fileReader: async () => contentA });
        const hashB = await computeBodyHash(testFile, { fileReader: async () => contentB });
        expect(hashA).not.toBe(hashB);
      } finally {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("should produce the same hash when only frontmatter changes", async () => {
      const tmpDir = fs.mkdtempSync(path.join(require("os").tmpdir(), "body-hash-test-"));
      const testFile = path.join(tmpDir, "test.md");
      const contentA = "---\nengine: copilot\ndescription: version 1\n---\n\nSame body";
      const contentB = "---\nengine: copilot\ndescription: version 2\n---\n\nSame body";

      try {
        const hashA = await computeBodyHash(testFile, { fileReader: async () => contentA });
        const hashB = await computeBodyHash(testFile, { fileReader: async () => contentB });
        expect(hashA).toBe(hashB);
      } finally {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("should include imported file bodies in the hash", async () => {
      const tmpDir = fs.mkdtempSync(path.join(require("os").tmpdir(), "body-hash-test-"));
      const mainFile = path.join(tmpDir, "main.md");
      const importedFile = path.join(tmpDir, "shared", "imported.md");

      const fileSystemBase = {
        [mainFile]: "---\nengine: copilot\nimports:\n  - shared/imported.md\n---\n\nMain body",
        [importedFile]: "---\ntools:\n  bash: true\n---\n\nImported body v1",
      };
      const fileSystemChanged = {
        [mainFile]: "---\nengine: copilot\nimports:\n  - shared/imported.md\n---\n\nMain body",
        [importedFile]: "---\ntools:\n  bash: true\n---\n\nImported body v2 (changed)",
      };

      const makeReader = fs_map => async filePath => {
        if (fs_map[filePath]) return fs_map[filePath];
        throw new Error(`File not found: ${filePath}`);
      };

      try {
        const hashBase = await computeBodyHash(mainFile, { fileReader: makeReader(fileSystemBase) });
        const hashChanged = await computeBodyHash(mainFile, { fileReader: makeReader(fileSystemChanged) });
        expect(hashBase).not.toBe(hashChanged);
      } finally {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("should not be affected by changes to imported file frontmatter only", async () => {
      const tmpDir = fs.mkdtempSync(path.join(require("os").tmpdir(), "body-hash-test-"));
      const mainFile = path.join(tmpDir, "main.md");
      const importedFile = path.join(tmpDir, "shared", "imported.md");

      const fileSystemBase = {
        [mainFile]: "---\nengine: copilot\nimports:\n  - shared/imported.md\n---\n\nMain body",
        [importedFile]: "---\ntools:\n  bash: true\n---\n\nImported body",
      };
      const fileSystemFrontmatterChanged = {
        [mainFile]: "---\nengine: copilot\nimports:\n  - shared/imported.md\n---\n\nMain body",
        [importedFile]: "---\ntools:\n  bash: true\ndescription: changed frontmatter\n---\n\nImported body",
      };

      const makeReader = fs_map => async filePath => {
        if (fs_map[filePath]) return fs_map[filePath];
        throw new Error(`File not found: ${filePath}`);
      };

      try {
        const hashBase = await computeBodyHash(mainFile, { fileReader: makeReader(fileSystemBase) });
        const hashFrontmatterChanged = await computeBodyHash(mainFile, { fileReader: makeReader(fileSystemFrontmatterChanged) });
        // Only imported frontmatter changed, body is the same → hashes should match
        expect(hashBase).toBe(hashFrontmatterChanged);
      } finally {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });
  });
});

// ---------------------------------------------------------------------------
// Symlink traversal regression tests for activation-hash stale-lock false-positives
// These tests verify that createGitHubFileReader, resolveRemoteSymlinks, and
// computeFrontmatterHash correctly handle import paths that traverse symlinked
// directories — the scenario that triggered false "lock file is outdated"
// failures when imported content was silently skipped after a 404 on a
// symlinked path component in the GitHub Contents API.
// ---------------------------------------------------------------------------

const { checkRemoteSymlink, resolveRemoteSymlinks } = require("./frontmatter_hash_pure.cjs");

describe("symlink traversal regression for activation hash symlink handling", () => {
  // ---------------------------------------------------------------------------
  // checkRemoteSymlink
  // ---------------------------------------------------------------------------
  describe("checkRemoteSymlink", () => {
    it("should return target string when path is a symlink", async () => {
      const github = {
        rest: {
          repos: {
            getContent: async () => ({
              data: { type: "symlink", target: "../.ai/agents" },
            }),
          },
        },
      };
      const result = await checkRemoteSymlink(github, "owner", "repo", ".github/agents", "main");
      expect(result).toBe("../.ai/agents");
    });

    it("should return null when path is a regular file", async () => {
      const github = {
        rest: {
          repos: {
            getContent: async () => ({
              data: { type: "file", content: "aGVsbG8=", encoding: "base64" },
            }),
          },
        },
      };
      const result = await checkRemoteSymlink(github, "owner", "repo", ".github/agents/file.md", "main");
      expect(result).toBeNull();
    });

    it("should return null when path is a directory", async () => {
      const github = {
        rest: {
          repos: {
            getContent: async () => ({
              data: [{ name: "file.md", type: "file" }],
            }),
          },
        },
      };
      const result = await checkRemoteSymlink(github, "owner", "repo", ".github/agents", "main");
      expect(result).toBeNull();
    });

    it("should return null when the API returns a 404 error", async () => {
      const github = {
        rest: {
          repos: {
            getContent: async () => {
              const err = new Error("Not Found");
              err.status = 404;
              throw err;
            },
          },
        },
      };
      const result = await checkRemoteSymlink(github, "owner", "repo", ".github/agents", "main");
      expect(result).toBeNull();
    });

    it("should return null for forbidden errors", async () => {
      const github = {
        rest: {
          repos: {
            getContent: async () => {
              const err = new Error("Forbidden");
              err.status = 403;
              throw err;
            },
          },
        },
      };
      const result = await checkRemoteSymlink(github, "owner", "repo", ".github/agents", "main");
      expect(result).toBeNull();
    });

    it("should return null for unauthorized errors", async () => {
      const github = {
        rest: {
          repos: {
            getContent: async () => {
              const err = new Error("Unauthorized");
              err.status = 401;
              throw err;
            },
          },
        },
      };
      const result = await checkRemoteSymlink(github, "owner", "repo", ".github/agents", "main");
      expect(result).toBeNull();
    });

    it("should rethrow transient API errors", async () => {
      const github = {
        rest: {
          repos: {
            getContent: async () => {
              const err = new Error("Bad Gateway");
              err.status = 502;
              throw err;
            },
          },
        },
      };
      await expect(checkRemoteSymlink(github, "owner", "repo", ".github/agents", "main")).rejects.toThrow("Bad Gateway");
    });

    it("should return null when symlink has no target", async () => {
      const github = {
        rest: {
          repos: {
            getContent: async () => ({
              data: { type: "symlink", target: "" },
            }),
          },
        },
      };
      const result = await checkRemoteSymlink(github, "owner", "repo", ".github/agents", "main");
      expect(result).toBeNull();
    });
  });

  // ---------------------------------------------------------------------------
  // resolveRemoteSymlinks
  // ---------------------------------------------------------------------------
  describe("resolveRemoteSymlinks", () => {
    it("should resolve a symlinked directory component in the path", async () => {
      // .github/agents is a symlink → ../.ai/agents
      // path: .github/agents/e2etest.md → .ai/agents/e2etest.md
      const github = {
        rest: {
          repos: {
            getContent: async ({ path: p }) => {
              if (p === ".github/agents") {
                return { data: { type: "symlink", target: "../.ai/agents" } };
              }
              // .github is a directory
              return { data: [{ name: "agents", type: "symlink" }] };
            },
          },
        },
      };
      const result = await resolveRemoteSymlinks(github, "owner", "repo", ".github/agents/e2etest.md", "main");
      expect(result).toBe(".ai/agents/e2etest.md");
    });

    it("should resolve a deeply nested symlink component", async () => {
      // .github/workflows/shared → ../../gh-agent-workflows/shared
      // path: .github/workflows/shared/elastic-tools.md → gh-agent-workflows/shared/elastic-tools.md
      const github = {
        rest: {
          repos: {
            getContent: async ({ path: p }) => {
              if (p === ".github/workflows/shared") {
                return { data: { type: "symlink", target: "../../gh-agent-workflows/shared" } };
              }
              return { data: [{ name: "file.md" }] };
            },
          },
        },
      };
      const result = await resolveRemoteSymlinks(github, "owner", "repo", ".github/workflows/shared/elastic-tools.md", "main");
      expect(result).toBe("gh-agent-workflows/shared/elastic-tools.md");
    });

    it("should resolve a symlink at the first path component (root-level)", async () => {
      // link-dir is a symlink → actual-dir
      // path: link-dir/subdir/file.md → actual-dir/subdir/file.md
      const github = {
        rest: {
          repos: {
            getContent: async ({ path: p }) => {
              if (p === "link-dir") {
                return { data: { type: "symlink", target: "actual-dir" } };
              }
              return { data: [{ name: "subdir" }] };
            },
          },
        },
      };
      const result = await resolveRemoteSymlinks(github, "owner", "repo", "link-dir/subdir/file.md", "main");
      expect(result).toBe("actual-dir/subdir/file.md");
    });

    it("should return null when no symlinks are found in any component", async () => {
      const github = {
        rest: {
          repos: {
            getContent: async () => ({
              data: [{ name: "file.md", type: "file" }], // directory listing
            }),
          },
        },
      };
      const result = await resolveRemoteSymlinks(github, "owner", "repo", ".github/workflows/file.md", "main");
      expect(result).toBeNull();
    });

    it("should return null for a single-component path (no directory to resolve)", async () => {
      const github = { rest: { repos: { getContent: async () => ({ data: {} }) } } };
      const result = await resolveRemoteSymlinks(github, "owner", "repo", "file.md", "main");
      expect(result).toBeNull();
    });

    it("should return null when the resolved path would escape the repository root", async () => {
      // symlink points far above the repo root
      const github = {
        rest: {
          repos: {
            getContent: async ({ path: p }) => {
              if (p === "dir") {
                return { data: { type: "symlink", target: "../../../outside" } };
              }
              return { data: [] };
            },
          },
        },
      };
      const result = await resolveRemoteSymlinks(github, "owner", "repo", "dir/file.md", "main");
      expect(result).toBeNull();
    });

    it("should return null when the symlink target is absolute", async () => {
      const github = {
        rest: {
          repos: {
            getContent: async ({ path: p }) => {
              if (p === "dir") {
                return { data: { type: "symlink", target: "/absolute/path" } };
              }
              return { data: [] };
            },
          },
        },
      };
      const result = await resolveRemoteSymlinks(github, "owner", "repo", "dir/file.md", "main");
      expect(result).toBeNull();
    });

    it("should never probe .github or .github/workflows for a .github/workflows/ path", async () => {
      // Regression: a 404 on a nested workflow path must not trigger probes for
      // the well-known non-symlink prefixes ".github" or ".github/workflows".
      // Only ".github/workflows/shared" (and deeper) should be checked.
      const probedPaths = [];
      const github = {
        rest: {
          repos: {
            getContent: async ({ path: p }) => {
              probedPaths.push(p);
              if (p === ".github/workflows/shared") {
                return { data: { type: "symlink", target: "../../gh-agent-workflows/shared" } };
              }
              return { data: [{ name: "file.md" }] };
            },
          },
        },
      };
      const result = await resolveRemoteSymlinks(github, "owner", "repo", ".github/workflows/shared/otlp.md", "main");
      expect(result).toBe("gh-agent-workflows/shared/otlp.md");
      // Neither ".github" nor ".github/workflows" must ever be probed.
      expect(probedPaths).not.toContain(".github");
      expect(probedPaths).not.toContain(".github/workflows");
      // The deeper directory ".github/workflows/shared" must have been probed.
      expect(probedPaths).toContain(".github/workflows/shared");
    });
  });

  // ---------------------------------------------------------------------------
  // createGitHubFileReader with symlink traversal
  // ---------------------------------------------------------------------------
  describe("createGitHubFileReader with symlink traversal", () => {
    it("should follow a symlinked directory on 404 and return the resolved file content", async () => {
      // .github/agents is a symlink → ../.ai/agents
      // Requesting .github/agents/e2etest.md should transparently return .ai/agents/e2etest.md
      const agentContent = "---\nengine: copilot\ndescription: e2e test agent\n---\n\nDo stuff.";

      const github = {
        rest: {
          repos: {
            getContent: async ({ path: p }) => {
              if (p === ".github/agents/e2etest.md") {
                const err = new Error("Not Found");
                err.status = 404;
                throw err;
              }
              if (p === ".github/agents") {
                return { data: { type: "symlink", target: "../.ai/agents" } };
              }
              if (p === ".github") {
                return { data: [{ name: "agents", type: "symlink" }] };
              }
              if (p === ".ai/agents/e2etest.md") {
                return {
                  data: {
                    type: "file",
                    encoding: "base64",
                    content: Buffer.from(agentContent).toString("base64"),
                  },
                };
              }
              const err = new Error(`Not Found: ${p}`);
              err.status = 404;
              throw err;
            },
          },
        },
      };

      const fileReader = createGitHubFileReader(github, "owner", "repo", "main");
      const content = await fileReader(".github/agents/e2etest.md");
      expect(content).toBe(agentContent);
    });

    it("should memoize symlink lookups across reads under the same symlinked directory", async () => {
      const callCounts = new Map();
      const github = {
        rest: {
          repos: {
            getContent: async ({ path: p }) => {
              callCounts.set(p, (callCounts.get(p) || 0) + 1);
              if (p === ".github/agents/one.md" || p === ".github/agents/two.md") {
                const err = new Error("Not Found");
                err.status = 404;
                throw err;
              }
              if (p === ".github/agents") {
                return { data: { type: "symlink", target: "../.ai/agents" } };
              }
              if (p === ".github") {
                return { data: [{ name: "agents", type: "symlink" }] };
              }
              if (p === ".ai/agents/one.md" || p === ".ai/agents/two.md") {
                return {
                  data: {
                    type: "file",
                    encoding: "base64",
                    content: Buffer.from(`content:${p}`).toString("base64"),
                  },
                };
              }
              const err = new Error(`Not Found: ${p}`);
              err.status = 404;
              throw err;
            },
          },
        },
      };

      const fileReader = createGitHubFileReader(github, "owner", "repo", "main");
      await expect(fileReader(".github/agents/one.md")).resolves.toBe("content:.ai/agents/one.md");
      await expect(fileReader(".github/agents/two.md")).resolves.toBe("content:.ai/agents/two.md");

      // ".github" alone is no longer probed (startIndex=2 for ".github/" paths skips it).
      expect(callCounts.get(".github")).toBeUndefined();
      // ".github/agents" is still probed and memoized (only once for both reads).
      expect(callCounts.get(".github/agents")).toBe(1);
    });

    it("should follow chained symlinked directories across recursive retries", async () => {
      const github = {
        rest: {
          repos: {
            getContent: async ({ path: p }) => {
              if (p === "a/b/d/file.md" || p === "c/b/d/file.md") {
                const err = new Error("Not Found");
                err.status = 404;
                throw err;
              }
              if (p === "a") {
                return { data: [{ name: "b", type: "symlink" }] };
              }
              if (p === "a/b") {
                return { data: { type: "symlink", target: "../c/b" } };
              }
              if (p === "c" || p === "c/b") {
                return { data: [{ name: "d", type: "symlink" }] };
              }
              if (p === "c/b/d") {
                return { data: { type: "symlink", target: "../../e/d" } };
              }
              if (p === "e/d/file.md") {
                return {
                  data: {
                    type: "file",
                    encoding: "base64",
                    content: Buffer.from("chained symlink content").toString("base64"),
                  },
                };
              }
              const err = new Error(`Not Found: ${p}`);
              err.status = 404;
              throw err;
            },
          },
        },
      };

      const fileReader = createGitHubFileReader(github, "owner", "repo", "main");
      await expect(fileReader("a/b/d/file.md")).resolves.toBe("chained symlink content");
    });

    it("should throw when the file is truly missing (no symlink resolves it)", async () => {
      const github = {
        rest: {
          repos: {
            getContent: async ({ path: p }) => {
              const err = new Error("Not Found");
              err.status = 404;
              throw err;
            },
          },
        },
      };

      const fileReader = createGitHubFileReader(github, "owner", "repo", "main");
      await expect(fileReader(".github/workflows/missing.md")).rejects.toThrow("Failed to read file");
    });

    it("should not attempt symlink resolution for non-404 errors", async () => {
      let getContentCallCount = 0;
      const github = {
        rest: {
          repos: {
            getContent: async () => {
              getContentCallCount++;
              const err = new Error("Forbidden");
              err.status = 403;
              throw err;
            },
          },
        },
      };

      const fileReader = createGitHubFileReader(github, "owner", "repo", "main");
      await expect(fileReader(".github/workflows/file.md")).rejects.toThrow("Failed to read file");
      // Should only have been called once (no symlink resolution retries for 403)
      expect(getContentCallCount).toBe(1);
    });

    it("should surface an explicit error when chained symlinks exceed max depth", async () => {
      const github = {
        rest: {
          repos: {
            getContent: async ({ path: p }) => {
              if (/^link\d+\/file\.md$/.test(p)) {
                const err = new Error("Not Found");
                err.status = 404;
                throw err;
              }
              const match = /^link(\d+)$/.exec(p);
              if (match) {
                const next = Number(match[1]) + 1;
                return { data: { type: "symlink", target: `link${next}` } };
              }
              const err = new Error(`Not Found: ${p}`);
              err.status = 404;
              throw err;
            },
          },
        },
      };

      const fileReader = createGitHubFileReader(github, "owner", "repo", "main");
      await expect(fileReader("link0/file.md")).rejects.toThrow("symlink chain exceeded maximum depth of 5");
    });
  });

  // ---------------------------------------------------------------------------
  // Hash parity: computeFrontmatterHash with symlinked imports
  // Verifies that the API reader (with symlink traversal) produces the same hash
  // as a direct filesystem reader — the core guarantee the activation job needs.
  // ---------------------------------------------------------------------------
  describe("hash parity between filesystem reader and API reader with symlinks", () => {
    it("should produce identical hashes regardless of whether the import path traverses a symlink", async () => {
      // Simulate a workflow that imports an agent via a symlinked path:
      //   .github/workflows/my-workflow.md imports ../agents/helper.md
      //   .github/agents is a symlink → ../.ai/agents
      //   so the resolved path is .ai/agents/helper.md
      //
      // The filesystem reader resolves symlinks at the OS level and sees the content directly.
      // The API reader must traverse the symlink via the GitHub Contents API to reach the same content.
      // Both should produce the same hash.

      const helperContent = "---\nengine: copilot\ndescription: helper agent\n---\n\nHelp with stuff.";
      const mainContent = "---\nengine: copilot\ndescription: my workflow\nimports:\n  - ../agents/helper.md\n---\n\nRun the helper.";

      // File system mapping using resolved paths (as the OS would see them after symlink traversal)
      const mainPath = ".github/workflows/my-workflow.md";
      const resolvedImportPath = ".ai/agents/helper.md"; // resolved target of .github/agents symlink

      // Direct filesystem reader: uses resolved paths (simulates os.ReadFile following symlinks)
      const fsFileSystem = {
        [mainPath]: mainContent,
        // The filesystem reader is given the fully resolved path:
        ".github/agents/helper.md": helperContent, // simulates OS-level symlink resolution: the filesystem sees content at the symlinked path
      };
      const fsReader = async filePath => {
        if (fsFileSystem[filePath]) return fsFileSystem[filePath];
        throw new Error(`File not found: ${filePath}`);
      };

      // GitHub API reader: simulates the Contents API where .github/agents is a symlink
      const apiFileSystem = {
        [mainPath]: mainContent,
        [resolvedImportPath]: helperContent, // only accessible via the resolved path
      };
      const apiFetches = [];
      const github = {
        rest: {
          repos: {
            getContent: async ({ path: p }) => {
              apiFetches.push(p);
              if (apiFileSystem[p]) {
                return {
                  data: {
                    type: "file",
                    encoding: "base64",
                    content: Buffer.from(apiFileSystem[p]).toString("base64"),
                  },
                };
              }
              if (p === ".github/agents") {
                return { data: { type: "symlink", target: "../.ai/agents" } };
              }
              const err = new Error(`Not Found: ${p}`);
              err.status = 404;
              throw err;
            },
          },
        },
      };
      const apiReader = createGitHubFileReader(github, "owner", "repo", "main");

      const fsHash = await computeFrontmatterHash(mainPath, { fileReader: fsReader });
      const apiHash = await computeFrontmatterHash(mainPath, { fileReader: apiReader });

      // Both hashes must agree — this is the invariant the activation job relies on
      expect(apiHash).toBe(fsHash);
      expect(apiFetches).toContain(".ai/agents/helper.md");
    });
  });
});
