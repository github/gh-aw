// @ts-check

const { describe, it, expect, beforeAll, afterAll } = require("vitest");
const fs = require("fs");
const path = require("path");
const os = require("os");
const {
  computeFrontmatterHash,
  extractFrontmatter,
  marshalSorted,
  buildCanonicalFrontmatter,
} = require("./frontmatter_hash.cjs");

describe("frontmatter_hash", () => {
  let tempDir;

  beforeAll(() => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "frontmatter-hash-test-"));
  });

  afterAll(() => {
    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true });
    }
  });

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

  describe("extractFrontmatter", () => {
    it("should extract frontmatter from markdown", () => {
      const content = `---
engine: copilot
description: Test workflow
on:
  schedule: daily
---

# Test Workflow

This is a test.
`;
      const frontmatter = extractFrontmatter(content);
      expect(frontmatter.engine).toBe("copilot");
      expect(frontmatter.description).toBe("Test workflow");
      expect(frontmatter.on).toEqual({ schedule: "daily" });
    });

    it("should return empty object for content without frontmatter", () => {
      const content = "# Just a heading\n\nSome content.";
      const frontmatter = extractFrontmatter(content);
      expect(frontmatter).toEqual({});
    });

    it("should throw error for unclosed frontmatter", () => {
      const content = "---\nengine: copilot\nno closing delimiter";
      expect(() => extractFrontmatter(content)).toThrow("not properly closed");
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
        mergedTools: '{"mcp":{"server":"remote"}}',
        mergedEngines: ["claude", "copilot"],
        mergedSafeOutputs: [],
        mergedSafeInputs: [],
        mergedSteps: "",
        mergedRuntimes: "",
        mergedServices: "",
        mergedNetwork: "",
        mergedPermissions: "",
        mergedSecretMasking: "",
        mergedBots: [],
        mergedPostSteps: "",
        mergedLabels: [],
        mergedCaches: [],
        importedFiles: ["shared/common.md"],
        agentFile: "",
        importInputs: {},
      };

      const canonical = buildCanonicalFrontmatter(frontmatter, importsResult);

      expect(canonical.engine).toBe("copilot");
      expect(canonical.description).toBe("Test");
      expect(canonical.on).toEqual({ schedule: "daily" });
      expect(canonical.tools).toEqual({ playwright: { version: "v1.41.0" } });
      expect(canonical["merged-tools"]).toBe('{"mcp":{"server":"remote"}}');
      expect(canonical["merged-engines"]).toEqual(["claude", "copilot"]);
      expect(canonical.imports).toEqual(["shared/common.md"]);
    });

    it("should omit empty fields", () => {
      const frontmatter = {
        engine: "copilot",
      };

      const importsResult = {
        mergedTools: "",
        mergedMCPServers: "",
        mergedEngines: [],
        mergedSafeOutputs: [],
        mergedSafeInputs: [],
        mergedSteps: "",
        mergedRuntimes: "",
        mergedServices: "",
        mergedNetwork: "",
        mergedPermissions: "",
        mergedSecretMasking: "",
        mergedBots: [],
        mergedPostSteps: "",
        mergedLabels: [],
        mergedCaches: [],
        importedFiles: [],
        agentFile: "",
        importInputs: {},
      };

      const canonical = buildCanonicalFrontmatter(frontmatter, importsResult);

      expect(canonical.engine).toBe("copilot");
      expect(canonical["merged-tools"]).toBeUndefined();
      expect(canonical["merged-engines"]).toBeUndefined();
      expect(canonical.imports).toBeUndefined();
    });
  });

  describe("computeFrontmatterHash", () => {
    it("should compute hash for simple workflow", async () => {
      const workflowFile = path.join(tempDir, "simple.md");
      const content = `---
engine: copilot
description: Test workflow
on:
  schedule: daily
---

# Test Workflow
`;
      fs.writeFileSync(workflowFile, content);

      const hash = await computeFrontmatterHash(workflowFile);
      expect(hash).toMatch(/^[a-f0-9]{64}$/);
    });

    it("should produce deterministic hashes", async () => {
      const workflowFile = path.join(tempDir, "deterministic.md");
      const content = `---
engine: copilot
description: Test
on:
  schedule: daily
---

# Test
`;
      fs.writeFileSync(workflowFile, content);

      const hash1 = await computeFrontmatterHash(workflowFile);
      const hash2 = await computeFrontmatterHash(workflowFile);
      expect(hash1).toBe(hash2);
    });

    it("should produce identical hashes regardless of key order", async () => {
      const workflow1 = path.join(tempDir, "ordered1.md");
      const content1 = `---
engine: copilot
description: Test
on:
  schedule: daily
---

# Test
`;
      fs.writeFileSync(workflow1, content1);

      const workflow2 = path.join(tempDir, "ordered2.md");
      const content2 = `---
on:
  schedule: daily
description: Test
engine: copilot
---

# Test
`;
      fs.writeFileSync(workflow2, content2);

      const hash1 = await computeFrontmatterHash(workflow1);
      const hash2 = await computeFrontmatterHash(workflow2);
      expect(hash1).toBe(hash2);
    });

    it("should handle workflow with imports", async () => {
      // Create shared workflow
      const sharedDir = path.join(tempDir, "shared");
      fs.mkdirSync(sharedDir, { recursive: true });

      const sharedFile = path.join(sharedDir, "common.md");
      const sharedContent = `---
tools:
  playwright:
    version: v1.41.0
labels:
  - shared
---

# Shared
`;
      fs.writeFileSync(sharedFile, sharedContent);

      // Create main workflow
      const mainFile = path.join(tempDir, "main-with-imports.md");
      const mainContent = `---
engine: copilot
description: Main
imports:
  - shared/common.md
labels:
  - main
---

# Main
`;
      fs.writeFileSync(mainFile, mainContent);

      const hash = await computeFrontmatterHash(mainFile);
      expect(hash).toMatch(/^[a-f0-9]{64}$/);

      // Should be deterministic
      const hash2 = await computeFrontmatterHash(mainFile);
      expect(hash).toBe(hash2);
    });

    it("should handle complex frontmatter", async () => {
      const workflowFile = path.join(tempDir, "complex.md");
      const content = `---
engine: claude
description: Complex workflow
tracker-id: complex-test
timeout-minutes: 30
on:
  schedule: daily
  workflow_dispatch: true
permissions:
  contents: read
  actions: read
tools:
  playwright:
    version: v1.41.0
    domains:
      - github.com
      - example.com
network:
  allowed:
    - api.github.com
runtimes:
  node:
    version: "20"
labels:
  - test
  - complex
bots:
  - copilot
---

# Complex Workflow
`;
      fs.writeFileSync(workflowFile, content);

      const hash = await computeFrontmatterHash(workflowFile);
      expect(hash).toMatch(/^[a-f0-9]{64}$/);
    });
  });
});
