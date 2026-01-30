// @ts-check
import { describe, it, expect } from "vitest";
const { computeFrontmatterHash, extractHashFromLockFile } = require("./frontmatter_hash.cjs");
const fs = require("fs");
const path = require("path");

describe("frontmatter_hash (API)", () => {
  describe("computeFrontmatterHash", () => {
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
    it("should extract hash from lock file", () => {
      const content = "# frontmatter-hash: abc123\n\nname: Test";
      const hash = extractHashFromLockFile(content);
      expect(hash).toBe("abc123");
    });

    it("should return empty string if no hash", () => {
      const content = "name: Test\non: push";
      const hash = extractHashFromLockFile(content);
      expect(hash).toBe("");
    });
  });
});
