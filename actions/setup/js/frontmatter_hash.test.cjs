// @ts-check
import { describe, it, expect } from "vitest";
const { computeFrontmatterHash, extractHashFromLockFile } = require("./frontmatter_hash.cjs");
const fs = require("fs");
const path = require("path");

describe("frontmatter_hash (API)", () => {
  describe("computeFrontmatterHash", () => {
    it("should verify the Copilot Session Insights lock with all imports readable", async () => {
      const workflowPath = path.resolve(__dirname, "../../../.github/workflows/copilot-session-insights.md");
      const lockPath = workflowPath.replace(/\.md$/, ".lock.yml");
      const hash = await computeFrontmatterHash(workflowPath, {
        onMissingImport: (importPath, error) => {
          throw new Error(`Unreadable import ${importPath}: ${error.message}`);
        },
      });

      expect(hash).toBe(extractHashFromLockFile(fs.readFileSync(lockPath, "utf8")));
    });

    it("should compute hash deterministically", async () => {
      // Create a temporary test file
      const testFile = path.join(__dirname, "test-api-workflow.md");
      const content = "---\nengine: copilot\ndescription: API Test\n---\n\nBody content";

      fs.writeFileSync(testFile, content, "utf8");

      try {
        const hash1 = await computeFrontmatterHash(testFile);
        const hash2 = await computeFrontmatterHash(testFile);

        // Should be deterministic
        expect(hash1).toBe(hash2);

        // Should be a valid hash
        expect(hash1).toMatch(/^[a-f0-9]{64}$/);
      } finally {
        if (fs.existsSync(testFile)) {
          fs.unlinkSync(testFile);
        }
      }
    });
  });

  describe("extractHashFromLockFile", () => {
    it("should extract hash from old format lock file", () => {
      const content = "# frontmatter-hash: abc123\n\nname: Test";
      const hash = extractHashFromLockFile(content);
      expect(hash).toBe("abc123");
    });

    it("should extract hash from new JSON metadata format", () => {
      const content = '# gh-aw-metadata: {"schema_version":"v1","frontmatter_hash":"abc123def"}\n\nname: Test';
      const hash = extractHashFromLockFile(content);
      expect(hash).toBe("abc123def");
    });

    it("should return empty string if no hash", () => {
      const content = "name: Test\non: push";
      const hash = extractHashFromLockFile(content);
      expect(hash).toBe("");
    });
  });
});
