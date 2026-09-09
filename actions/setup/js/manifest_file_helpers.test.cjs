// @ts-check
import { describe, it, expect } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { extractFilenamesFromPatch, checkForManifestFiles, checkAllowedFiles, checkExcludedFiles, checkFileProtection, checkForTopLevelDotFolders } = require("./manifest_file_helpers.cjs");

describe("manifest_file_helpers", () => {
  describe("extractFilenamesFromPatch", () => {
    it("should return empty array for empty patch", () => {
      expect(extractFilenamesFromPatch("")).toEqual([]);
      expect(extractFilenamesFromPatch(null)).toEqual([]);
      expect(extractFilenamesFromPatch(undefined)).toEqual([]);
    });

    it("should extract a single filename", () => {
      const patch = `diff --git a/src/index.js b/src/index.js
index abc..def 100644
--- a/src/index.js
+++ b/src/index.js
@@ -1 +1 @@
-old
+new
`;
      expect(extractFilenamesFromPatch(patch)).toEqual(["index.js"]);
    });

    it("should extract basename only (no directory path)", () => {
      const patch = `diff --git a/path/to/deep/package.json b/path/to/deep/package.json
index abc..def 100644
--- a/path/to/deep/package.json
+++ b/path/to/deep/package.json
`;
      expect(extractFilenamesFromPatch(patch)).toEqual(["package.json"]);
    });

    it("should extract multiple filenames", () => {
      const patch = `diff --git a/src/index.js b/src/index.js
index abc..def 100644
diff --git a/package.json b/package.json
index abc..def 100644
diff --git a/README.md b/README.md
index abc..def 100644
`;
      const result = extractFilenamesFromPatch(patch);
      expect(result).toContain("index.js");
      expect(result).toContain("package.json");
      expect(result).toContain("README.md");
      expect(result).toHaveLength(3);
    });

    it("should deduplicate filenames", () => {
      const patch = `diff --git a/src/index.js b/src/index.js
index abc..def 100644
diff --git a/lib/index.js b/lib/index.js
index abc..def 100644
`;
      const result = extractFilenamesFromPatch(patch);
      expect(result).toEqual(["index.js"]);
    });

    it("should handle files at root (no directory)", () => {
      const patch = `diff --git a/package.json b/package.json
index abc..def 100644
`;
      expect(extractFilenamesFromPatch(patch)).toEqual(["package.json"]);
    });

    it("should capture both sides of a rename header", () => {
      // When package.json is renamed, the a/ side is the original manifest filename.
      // Both sides must be captured so the manifest check catches the rename.
      const patch = `diff --git a/package.json b/package.json.bak
similarity index 100%
rename from package.json
rename to package.json.bak
`;
      const result = extractFilenamesFromPatch(patch);
      expect(result).toContain("package.json");
      expect(result).toContain("package.json.bak");
    });

    it("should parse quoted headers with spaces and escapes", () => {
      const patch = `diff --git "a/dir/with space/package.json" "b/dir/with space/package-lock.json"
index abc..def 100644
diff --git "a/foo\\\\bar/config.json" "b/foo\\\\bar/config.json"
index abc..def 100644
`;
      const result = extractFilenamesFromPatch(patch);
      expect(result).toContain("package.json");
      expect(result).toContain("package-lock.json");
      expect(result).toContain("config.json");
    });

    it("should ignore dev/null sentinel in new-file diffs", () => {
      const patch = `diff --git a/dev/null b/src/new-file.js
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/src/new-file.js
@@ -0,0 +1 @@
+hello
`;
      const result = extractFilenamesFromPatch(patch);
      expect(result).toEqual(["new-file.js"]);
      expect(result).not.toContain("null");
    });

    it("should ignore dev/null sentinel in deleted-file diffs", () => {
      const patch = `diff --git a/src/old-file.js b/dev/null
deleted file mode 100644
index abc1234..0000000
--- a/src/old-file.js
+++ /dev/null
@@ -1 +0,0 @@
-hello
`;
      const result = extractFilenamesFromPatch(patch);
      expect(result).toEqual(["old-file.js"]);
      expect(result).not.toContain("null");
    });
  });

  describe("checkForManifestFiles", () => {
    it("should return false for empty patch", () => {
      const result = checkForManifestFiles("", ["package.json"]);
      expect(result.hasManifestFiles).toBe(false);
      expect(result.manifestFilesFound).toEqual([]);
    });

    it("should return false for empty manifest files list", () => {
      const patch = `diff --git a/package.json b/package.json\n`;
      const result = checkForManifestFiles(patch, []);
      expect(result.hasManifestFiles).toBe(false);
      expect(result.manifestFilesFound).toEqual([]);
    });

    it("should return false for null manifest files list", () => {
      const patch = `diff --git a/package.json b/package.json\n`;
      const result = checkForManifestFiles(patch, null);
      expect(result.hasManifestFiles).toBe(false);
      expect(result.manifestFilesFound).toEqual([]);
    });

    it("should detect package.json as a manifest file", () => {
      const patch = `diff --git a/package.json b/package.json
index abc..def 100644
--- a/package.json
+++ b/package.json
@@ -1 +1 @@
-{"name": "old"}
+{"name": "new"}
`;
      const result = checkForManifestFiles(patch, ["package.json", "go.mod"]);
      expect(result.hasManifestFiles).toBe(true);
      expect(result.manifestFilesFound).toContain("package.json");
    });

    it("should detect manifest files in nested directories", () => {
      const patch = `diff --git a/nested/path/go.mod b/nested/path/go.mod
index abc..def 100644
`;
      const result = checkForManifestFiles(patch, ["go.mod", "go.sum"]);
      expect(result.hasManifestFiles).toBe(true);
      expect(result.manifestFilesFound).toContain("go.mod");
    });

    it("should not detect non-manifest files", () => {
      const patch = `diff --git a/src/index.js b/src/index.js
index abc..def 100644
diff --git a/README.md b/README.md
index abc..def 100644
`;
      const result = checkForManifestFiles(patch, ["package.json", "go.mod", "requirements.txt"]);
      expect(result.hasManifestFiles).toBe(false);
      expect(result.manifestFilesFound).toEqual([]);
    });

    it("should return all manifest files found", () => {
      const patch = `diff --git a/package.json b/package.json
index abc..def 100644
diff --git a/package-lock.json b/package-lock.json
index abc..def 100644
diff --git a/src/index.js b/src/index.js
index abc..def 100644
`;
      const result = checkForManifestFiles(patch, ["package.json", "package-lock.json", "yarn.lock"]);
      expect(result.hasManifestFiles).toBe(true);
      expect(result.manifestFilesFound).toContain("package.json");
      expect(result.manifestFilesFound).toContain("package-lock.json");
      expect(result.manifestFilesFound).toHaveLength(2);
    });

    it("should match by filename only, not partial name", () => {
      const patch = `diff --git a/src/my-package.json b/src/my-package.json
index abc..def 100644
`;
      const result = checkForManifestFiles(patch, ["package.json"]);
      expect(result.hasManifestFiles).toBe(false);
    });

    it("should detect manifest file via the a/ side of a rename header", () => {
      // package.json is renamed to package.json.bak - the original name must be flagged
      const patch = `diff --git a/package.json b/package.json.bak
similarity index 100%
rename from package.json
rename to package.json.bak
`;
      const result = checkForManifestFiles(patch, ["package.json", "package-lock.json"]);
      expect(result.hasManifestFiles).toBe(true);
      expect(result.manifestFilesFound).toContain("package.json");
    });
  });

  describe("extractPathsFromPatch", () => {
    const { extractPathsFromPatch } = require("./manifest_file_helpers.cjs");

    it("should return empty array for empty patch", () => {
      expect(extractPathsFromPatch("")).toEqual([]);
      expect(extractPathsFromPatch(null)).toEqual([]);
    });

    it("should return full paths not just basenames", () => {
      const patch = `diff --git a/.github/workflows/ci.yml b/.github/workflows/ci.yml
index abc..def 100644
`;
      const result = extractPathsFromPatch(patch);
      expect(result).toContain(".github/workflows/ci.yml");
      expect(result).not.toContain("ci.yml"); // basenames not returned
    });

    it("should include both a/ and b/ paths for renames", () => {
      const patch = `diff --git a/.github/old.yml b/.github/new.yml
similarity index 100%
rename from .github/old.yml
rename to .github/new.yml
`;
      const result = extractPathsFromPatch(patch);
      expect(result).toContain(".github/old.yml");
      expect(result).toContain(".github/new.yml");
    });

    it("should skip dev/null sentinel", () => {
      const patch = `diff --git a/dev/null b/.github/workflows/new.yml
new file mode 100644
index 0000000..abc
`;
      const result = extractPathsFromPatch(patch);
      expect(result).toContain(".github/workflows/new.yml");
      expect(result).not.toContain("dev/null");
    });

    it("should parse CRLF patch headers without trailing carriage returns", () => {
      const patch = "diff --git a/.github/workflows/ci.yml b/.github/workflows/ci.yml\r\nindex abc..def 100644\r\n";
      const result = extractPathsFromPatch(patch);
      expect(result).toEqual([".github/workflows/ci.yml"]);
    });

    it("should parse quoted headers and throw on malformed headers (fail-closed)", () => {
      const patch = [`diff --git "a/.github/workflows/ci file.yml" "b/.github/workflows/ci file.yml"`, "index abc..def 100644", "diff --git ", "index abc..def 100644"].join("\n");
      expect(() => extractPathsFromPatch(patch)).toThrow(/unparseable diff --git header/);
    });

    it("should parse quoted headers when no malformed headers present", () => {
      const patch = [`diff --git "a/.github/workflows/ci file.yml" "b/.github/workflows/ci file.yml"`, "index abc..def 100644"].join("\n");
      const result = extractPathsFromPatch(patch);
      expect(result).toContain(".github/workflows/ci file.yml");
      expect(result).toHaveLength(1);
    });

    it("should preserve full escaped paths from quoted headers", () => {
      const patch = `diff --git "a/foo\\\\bar/config.json" "b/foo\\\\bar/config.json"
index abc..def 100644
`;
      const result = extractPathsFromPatch(patch);
      expect(result).toContain("foo\\\\bar/config.json");
    });
  });

  describe("checkForProtectedPaths", () => {
    const { checkForProtectedPaths } = require("./manifest_file_helpers.cjs");

    it("should return false for empty patch", () => {
      const result = checkForProtectedPaths("", [".github/"]);
      expect(result.hasProtectedPaths).toBe(false);
      expect(result.protectedPathsFound).toEqual([]);
    });

    it("should return false for empty prefixes list", () => {
      const patch = `diff --git a/.github/workflows/ci.yml b/.github/workflows/ci.yml\n`;
      const result = checkForProtectedPaths(patch, []);
      expect(result.hasProtectedPaths).toBe(false);
    });

    it("should detect .github/ files", () => {
      const patch = `diff --git a/.github/workflows/ci.yml b/.github/workflows/ci.yml
index abc..def 100644
`;
      const result = checkForProtectedPaths(patch, [".github/"]);
      expect(result.hasProtectedPaths).toBe(true);
      expect(result.protectedPathsFound).toContain(".github/workflows/ci.yml");
    });

    it("should not flag files outside protected path prefix", () => {
      const patch = `diff --git a/src/ci.yml b/src/ci.yml
index abc..def 100644
`;
      const result = checkForProtectedPaths(patch, [".github/"]);
      expect(result.hasProtectedPaths).toBe(false);
    });

    it("should detect AGENTS.md via basename check (not path prefix)", () => {
      // AGENTS.md is checked via checkForManifestFiles (basename), not path prefix
      const patch = `diff --git a/AGENTS.md b/AGENTS.md
index abc..def 100644
`;
      const basenameResult = checkForManifestFiles(patch, ["AGENTS.md"]);
      expect(basenameResult.hasManifestFiles).toBe(true);
    });
  });

  describe("checkForTopLevelDotFolders", () => {
    it("should return false for empty patch", () => {
      const result = checkForTopLevelDotFolders("", []);
      expect(result.hasTopLevelDotFolders).toBe(false);
      expect(result.topLevelDotFoldersFound).toEqual([]);
    });

    it("should detect a file inside a top-level dot-folder", () => {
      const patch = `diff --git a/.cursor/settings.json b/.cursor/settings.json\nindex abc..def 100644\n`;
      const result = checkForTopLevelDotFolders(patch);
      expect(result.hasTopLevelDotFolders).toBe(true);
      expect(result.topLevelDotFoldersFound).toContain(".cursor/settings.json");
    });

    it("should detect multiple dot-folders", () => {
      const patch = [`diff --git a/.vscode/settings.json b/.vscode/settings.json`, `index abc..def 100644`, `diff --git a/.cursor/rules.md b/.cursor/rules.md`, `index abc..def 100644`].join("\n");
      const result = checkForTopLevelDotFolders(patch);
      expect(result.hasTopLevelDotFolders).toBe(true);
      expect(result.topLevelDotFoldersFound).toContain(".vscode/settings.json");
      expect(result.topLevelDotFoldersFound).toContain(".cursor/rules.md");
    });

    it("should not flag root-level dot-files (not inside a dot-folder)", () => {
      const patch = `diff --git a/.env b/.env\nindex abc..def 100644\n`;
      const result = checkForTopLevelDotFolders(patch);
      expect(result.hasTopLevelDotFolders).toBe(false);
    });

    it("should not flag files inside non-dot top-level folders", () => {
      const patch = `diff --git a/src/index.js b/src/index.js\nindex abc..def 100644\n`;
      const result = checkForTopLevelDotFolders(patch);
      expect(result.hasTopLevelDotFolders).toBe(false);
    });

    it("should not flag files already covered by .github/ when that is in excludes", () => {
      const patch = `diff --git a/.github/workflows/ci.yml b/.github/workflows/ci.yml\nindex abc..def 100644\n`;
      const result = checkForTopLevelDotFolders(patch, [".github/"]);
      expect(result.hasTopLevelDotFolders).toBe(false);
    });

    it("should still flag non-excluded dot-folders when some are excluded", () => {
      const patch = [`diff --git a/.github/workflows/ci.yml b/.github/workflows/ci.yml`, `index abc..def 100644`, `diff --git a/.cursor/settings.json b/.cursor/settings.json`, `index abc..def 100644`].join("\n");
      const result = checkForTopLevelDotFolders(patch, [".github/"]);
      expect(result.hasTopLevelDotFolders).toBe(true);
      expect(result.topLevelDotFoldersFound).toContain(".cursor/settings.json");
      expect(result.topLevelDotFoldersFound).not.toContain(".github/workflows/ci.yml");
    });

    it("should not flag deeply nested files under non-dot top-level folders", () => {
      const patch = `diff --git a/pkg/workflow/.hidden/file b/pkg/workflow/.hidden/file\nindex abc..def 100644\n`;
      const result = checkForTopLevelDotFolders(patch);
      expect(result.hasTopLevelDotFolders).toBe(false);
    });
  });

  describe("checkAllowedFiles", () => {
    it("should return no disallowed files when patterns is empty", () => {
      const patch = `diff --git a/src/index.js b/src/index.js\n`;
      const result = checkAllowedFiles(patch, []);
      expect(result.hasDisallowedFiles).toBe(false);
      expect(result.disallowedFiles).toEqual([]);
    });

    it("should return no disallowed files for empty patch", () => {
      const result = checkAllowedFiles("", [".changeset/**"]);
      expect(result.hasDisallowedFiles).toBe(false);
      expect(result.disallowedFiles).toEqual([]);
    });

    it("should allow all files when all match the allowlist", () => {
      const patch = `diff --git a/.changeset/patch-fix.md b/.changeset/patch-fix.md\nindex abc..def 100644\n`;
      const result = checkAllowedFiles(patch, [".changeset/**"]);
      expect(result.hasDisallowedFiles).toBe(false);
      expect(result.disallowedFiles).toEqual([]);
    });

    it("should flag files not matching any allowed pattern", () => {
      const patch = `diff --git a/src/index.js b/src/index.js\nindex abc..def 100644\n`;
      const result = checkAllowedFiles(patch, [".changeset/**"]);
      expect(result.hasDisallowedFiles).toBe(true);
      expect(result.disallowedFiles).toContain("src/index.js");
    });

    it("should flag only the file outside the allowlist when mixed", () => {
      const patch = [`diff --git a/.changeset/patch-fix.md b/.changeset/patch-fix.md`, `index abc..def 100644`, `diff --git a/src/index.js b/src/index.js`, `index abc..def 100644`].join("\n");
      const result = checkAllowedFiles(patch, [".changeset/**"]);
      expect(result.hasDisallowedFiles).toBe(true);
      expect(result.disallowedFiles).toContain("src/index.js");
      expect(result.disallowedFiles).not.toContain(".changeset/patch-fix.md");
    });

    it("should not flag a protected file that is in the allowlist", () => {
      const patch = `diff --git a/.github/aw/instructions.md b/.github/aw/instructions.md\nindex abc..def 100644\n`;
      const result = checkAllowedFiles(patch, [".github/aw/instructions.md"]);
      expect(result.hasDisallowedFiles).toBe(false);
    });

    it("should flag protected files not in the allowlist", () => {
      const patch = `diff --git a/.github/workflows/ci.yml b/.github/workflows/ci.yml\nindex abc..def 100644\n`;
      const result = checkAllowedFiles(patch, [".github/aw/instructions.md"]);
      expect(result.hasDisallowedFiles).toBe(true);
      expect(result.disallowedFiles).toContain(".github/workflows/ci.yml");
    });

    it("should support ** glob for deep path matching", () => {
      const patch = `diff --git a/.changeset/deep/nested/entry.md b/.changeset/deep/nested/entry.md\nindex abc..def 100644\n`;
      const result = checkAllowedFiles(patch, [".changeset/**"]);
      expect(result.hasDisallowedFiles).toBe(false);
    });
  });

  describe("checkExcludedFiles", () => {
    const makePatch = (...filePaths) => filePaths.map(p => `diff --git a/${p} b/${p}\nindex abc..def 100644\n`).join("\n");

    it("should return empty when patterns is empty", () => {
      const result = checkExcludedFiles(makePatch("src/index.js"), []);
      expect(result.excludedFiles).toEqual([]);
    });

    it("should return empty for empty patch", () => {
      const result = checkExcludedFiles("", ["auto-generated/**"]);
      expect(result.excludedFiles).toEqual([]);
    });

    it("should identify files matching ignored patterns", () => {
      const result = checkExcludedFiles(makePatch("auto-generated/file.txt", "src/index.js"), ["auto-generated/**"]);
      expect(result.excludedFiles).toContain("auto-generated/file.txt");
      expect(result.excludedFiles).not.toContain("src/index.js");
    });

    it("should return all files when all match ignored patterns", () => {
      const result = checkExcludedFiles(makePatch("auto-generated/a.txt", "auto-generated/b.txt"), ["auto-generated/**"]);
      expect(result.excludedFiles).toHaveLength(2);
    });

    it("should support ** glob for deep path matching", () => {
      const result = checkExcludedFiles(makePatch("dist/deep/nested/bundle.js"), ["dist/**"]);
      expect(result.excludedFiles).toContain("dist/deep/nested/bundle.js");
    });
  });

  describe("checkFileProtection", () => {
    const makePatch = (...filePaths) => filePaths.map(p => `diff --git a/${p} b/${p}\nindex abc..def 100644\n`).join("\n");

    it("should allow when patch is empty", () => {
      const result = checkFileProtection("", {});
      expect(result.action).toBe("allow");
    });

    it("should allow when no protected files or allowlist configured", () => {
      const result = checkFileProtection(makePatch("src/index.js"), {});
      expect(result.action).toBe("allow");
    });

    it("should deny when file is outside the allowlist", () => {
      const result = checkFileProtection(makePatch("src/index.js"), { allowed_files: [".changeset/**"] });
      expect(result.action).toBe("deny");
      expect(result.source).toBe("allowlist");
      expect(result.files).toContain("src/index.js");
    });

    it("should allow when all files match the allowlist and no protected-files configured", () => {
      const result = checkFileProtection(makePatch(".changeset/fix.md"), { allowed_files: [".changeset/**"] });
      expect(result.action).toBe("allow");
    });

    it("should allow CHANGELOG.md while protecting adjacent top-level documentation", () => {
      const changelogResult = checkFileProtection(makePatch("CHANGELOG.md"), {
        protected_files: ["README.md", "CONTRIBUTING.md"],
        protected_files_policy: "request-review",
      });
      expect(changelogResult.action).toBe("allow");

      const readmeResult = checkFileProtection(makePatch("README.md"), {
        protected_files: ["README.md", "CONTRIBUTING.md"],
        protected_files_policy: "request-review",
      });
      expect(readmeResult.action).toBe("request_review");
      expect(readmeResult.files).toContain("README.md");
    });

    it("should deny protected file even when it matches the allowlist (orthogonal checks)", () => {
      const result = checkFileProtection(makePatch("package.json"), {
        allowed_files: ["package.json"],
        protected_files: ["package.json"],
        protected_files_policy: "blocked",
      });
      expect(result.action).toBe("deny");
      expect(result.source).toBe("protected");
      expect(result.files).toContain("package.json");
    });

    it("should allow protected file when allowlist matches and protected-files: allowed", () => {
      const result = checkFileProtection(makePatch("package.json"), {
        allowed_files: ["package.json"],
        protected_files: ["package.json"],
        protected_files_policy: "allowed",
      });
      expect(result.action).toBe("allow");
    });

    it("should return fallback when protected file found and policy is fallback-to-issue", () => {
      const result = checkFileProtection(makePatch("package.json"), {
        protected_files: ["package.json"],
        protected_files_policy: "fallback-to-issue",
      });
      expect(result.action).toBe("fallback");
      expect(result.files).toContain("package.json");
    });

    it("should return request_review when protected file found and policy is request_review", () => {
      const result = checkFileProtection(makePatch("package.json"), {
        protected_files: ["package.json"],
        protected_files_policy: "request_review",
      });
      expect(result.action).toBe("request_review");
      expect(result.files).toContain("package.json");
    });

    it("should deny on protected path prefix when no allowlist", () => {
      const result = checkFileProtection(makePatch(".github/workflows/ci.yml"), {
        protected_path_prefixes: [".github/"],
        protected_files_policy: "blocked",
      });
      expect(result.action).toBe("deny");
      expect(result.source).toBe("protected");
      expect(result.files).toContain(".github/workflows/ci.yml");
    });

    it("should deny when file is inside a top-level dot-folder and protect_top_level_dot_folders is true", () => {
      const result = checkFileProtection(makePatch(".cursor/settings.json"), {
        protect_top_level_dot_folders: true,
        protected_files: [],
        protected_path_prefixes: [],
      });
      expect(result.action).toBe("deny");
      expect(result.source).toBe("protected");
      expect(result.files).toContain(".cursor/settings.json");
    });

    it("should allow when file is inside an excluded dot-folder", () => {
      const result = checkFileProtection(makePatch(".cursor/settings.json"), {
        protect_top_level_dot_folders: true,
        protected_dot_folder_excludes: [".cursor/"],
        protected_files: [],
        protected_path_prefixes: [],
      });
      expect(result.action).toBe("allow");
    });

    it("should deduplicate files caught by both path-prefix and dot-folder checks", () => {
      const result = checkFileProtection(makePatch(".github/workflows/ci.yml"), {
        protect_top_level_dot_folders: true,
        protected_path_prefixes: [".github/"],
        protected_files: [],
      });
      expect(result.action).toBe("deny");
      // .github/workflows/ci.yml must appear exactly once in the files array
      expect(result.files.filter(f => f === ".github/workflows/ci.yml")).toHaveLength(1);
    });

    it("should not apply dot-folder check when protect_top_level_dot_folders is false", () => {
      const result = checkFileProtection(makePatch(".cursor/settings.json"), {
        protect_top_level_dot_folders: false,
        protected_files: [],
        protected_path_prefixes: [],
      });
      expect(result.action).toBe("allow");
    });

    it("should not flag a root-level dot-file via dot-folder check", () => {
      const result = checkFileProtection(makePatch(".env"), {
        protect_top_level_dot_folders: true,
        protected_files: [],
        protected_path_prefixes: [],
      });
      expect(result.action).toBe("allow");
    });

    it("should deny when a top-level .md file is in the protected_files list", () => {
      const result = checkFileProtection(makePatch("README.md"), {
        protected_files: ["README.md"],
        protected_path_prefixes: [],
      });
      expect(result.action).toBe("deny");
      expect(result.source).toBe("protected");
      expect(result.files).toContain("README.md");
    });

    it("should allow .md files in subdirectories even when their basename is in protected_files", () => {
      // protected_files uses basename matching — docs/guide.md has basename guide.md, not README.md
      const result = checkFileProtection(makePatch("docs/guide.md"), {
        protected_files: ["README.md", "CONTRIBUTING.md"],
        protected_path_prefixes: [],
      });
      expect(result.action).toBe("allow");
    });

    it("should deny allowlist violation before checking protected-files (deny on first failure)", () => {
      // file is outside allowlist AND would be protected — allowlist check fires first
      const result = checkFileProtection(makePatch("src/outside.js"), {
        allowed_files: [".changeset/**"],
        protected_files: ["src/outside.js"],
        protected_files_policy: "blocked",
      });
      expect(result.action).toBe("deny");
      expect(result.source).toBe("allowlist");
    });

    it("should fail closed (deny) when protected_files_policy resolves to an invalid value", () => {
      // An expression like ${{ inputs.policy }} that resolves to an unrecognised string
      // must not silently allow protected file changes — it must deny (fail closed).
      const result = checkFileProtection(makePatch("package.json"), {
        protected_files: ["package.json"],
        protected_files_policy: "not-a-valid-policy",
      });
      expect(result.action).toBe("deny");
      expect(result.source).toBe("protected");
    });

    it("should fail closed (deny) when protected_files_policy is undefined", () => {
      const result = checkFileProtection(makePatch("package.json"), {
        protected_files: ["package.json"],
        // protected_files_policy intentionally absent
      });
      expect(result.action).toBe("deny");
      expect(result.source).toBe("protected");
    });
  });

  // ===================================================================
  // Security regression tests for patch-parser differential bypass
  // (see github/agentic-workflows#539)
  // ===================================================================
  describe("extractPathsFromPatch - fail-closed on unparseable headers (security)", () => {
    const { extractPathsFromPatch } = require("./manifest_file_helpers.cjs");

    it("should throw on unterminated quoted path (parseable: false)", () => {
      const patch = `diff --git "a/x b/y\nindex abc..def 100644\n`;
      expect(() => extractPathsFromPatch(patch)).toThrow(/unparseable diff --git header/);
    });

    it("should throw on diff --git with no paths", () => {
      const patch = `diff --git \nindex abc..def 100644\n`;
      expect(() => extractPathsFromPatch(patch)).toThrow(/unparseable diff --git header/);
    });

    it("should throw on diff --git with only one token", () => {
      const patch = `diff --git a/only-one-token\nindex abc..def 100644\n`;
      expect(() => extractPathsFromPatch(patch)).toThrow(/unparseable diff --git header/);
    });

    it("should escape unparseable diff --git headers in the error message", () => {
      const patch = 'diff --git "a/\u001b[31mevil b/file.txt\nindex abc..def 100644\n';
      let thrown;
      try {
        extractPathsFromPatch(patch);
      } catch (error) {
        thrown = error;
      }
      expect(thrown).toBeInstanceOf(Error);
      expect(thrown.message).toContain("\\u001b");
      expect(thrown.message).not.toContain("\u001b");
    });

    it("should NOT throw on valid diff --git headers", () => {
      const patch = `diff --git a/README.md b/README.md\nindex abc..def 100644\n`;
      expect(() => extractPathsFromPatch(patch)).not.toThrow();
      expect(extractPathsFromPatch(patch)).toEqual(["README.md"]);
    });
  });

  describe("checkFileProtectionPostApply", () => {
    const { checkFileProtectionPostApply } = require("./manifest_file_helpers.cjs");

    it("should allow when no files modified", () => {
      const result = checkFileProtectionPostApply([], {});
      expect(result.action).toBe("allow");
    });

    it("should allow when files are within allowed-files list", () => {
      const result = checkFileProtectionPostApply(["src/index.js", "src/utils.js"], { allowed_files: ["src/**"] });
      expect(result.action).toBe("allow");
    });

    it("should deny when files violate allowed-files (post-apply detection)", () => {
      const result = checkFileProtectionPostApply(["src/index.js", ".github/workflows/evil.yml"], { allowed_files: ["src/**"] });
      expect(result.action).toBe("deny");
      expect(result.source).toBe("post-apply");
      expect(result.files).toContain(".github/workflows/evil.yml");
    });

    it("should detect protected path prefixes in actual files", () => {
      const result = checkFileProtectionPostApply(["README.md", ".github/CODEOWNERS"], { protected_path_prefixes: [".github/"], protected_files_policy: "deny" });
      expect(result.action).toBe("deny");
      expect(result.files).toContain(".github/CODEOWNERS");
    });

    it("should detect top-level dot folders", () => {
      const result = checkFileProtectionPostApply(["src/app.js", ".husky/pre-commit"], { protect_top_level_dot_folders: true, protected_dot_folder_excludes: [], protected_files_policy: "deny" });
      expect(result.action).toBe("deny");
      expect(result.files).toContain(".husky/pre-commit");
    });

    it("should respect dot folder excludes", () => {
      const result = checkFileProtectionPostApply([".changeset/fix.md"], { protect_top_level_dot_folders: true, protected_dot_folder_excludes: [".changeset/"], protected_files_policy: "deny" });
      expect(result.action).toBe("allow");
    });

    it("should return fallback when policy is fallback-to-issue", () => {
      const result = checkFileProtectionPostApply([".github/CODEOWNERS"], { protected_path_prefixes: [".github/"], protected_files_policy: "fallback-to-issue" });
      expect(result.action).toBe("fallback");
      expect(result.files).toContain(".github/CODEOWNERS");
    });

    it("should return request_review when policy is request_review", () => {
      const result = checkFileProtectionPostApply(["package.json"], { protected_files: ["package.json"], protected_files_policy: "request_review" });
      expect(result.action).toBe("request_review");
      expect(result.files).toContain("package.json");
    });

    // Regression: variant 1 from the issue — hunk with no diff --git header
    it("should catch files that bypass the pre-apply parser (variant 1: no diff --git header)", () => {
      // Simulates what git am actually applies vs what the parser sees
      const actualFilesFromGitDiff = ["README.md", ".github/workflows/evil.yml"];
      const result = checkFileProtectionPostApply(actualFilesFromGitDiff, { allowed_files: ["README.md", "src/**"] });
      expect(result.action).toBe("deny");
      expect(result.source).toBe("post-apply");
      expect(result.files).toContain(".github/workflows/evil.yml");
    });

    // Regression: variant 2 — rename to overrides diff --git line
    it("should catch files that bypass via rename-to extended header (variant 2)", () => {
      const actualFilesFromGitDiff = [".github/CODEOWNERS"];
      const result = checkFileProtectionPostApply(actualFilesFromGitDiff, { allowed_files: ["README.md", "docs/**"] });
      expect(result.action).toBe("deny");
      expect(result.source).toBe("post-apply");
      expect(result.files).toContain(".github/CODEOWNERS");
    });
  });
});
