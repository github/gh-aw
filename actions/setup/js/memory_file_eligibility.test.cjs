import { describe, it, expect, afterEach } from "vitest";
import fs from "fs";
import path from "path";
import os from "os";
import { compileFileGlobPatterns, isMemoryFileEligible, filterIneligibleMemoryFiles } from "./memory_file_eligibility.cjs";

describe("memory_file_eligibility.cjs", () => {
  describe("isMemoryFileEligible", () => {
    it("allows all files when no extensions or patterns are configured", () => {
      expect(isMemoryFileEligible("notes.json", [], [])).toEqual({ eligible: true });
      expect(isMemoryFileEligible("notes.json.new", [], [])).toEqual({ eligible: true });
    });

    it("rejects files with disallowed extensions", () => {
      const result = isMemoryFileEligible("notes.json.new", [".json"], []);
      expect(result.eligible).toBe(false);
      expect(result.reason).toContain("disallowed extension");
      expect(result.reason).toContain(".new");
    });

    it("accepts files with an allowed extension", () => {
      expect(isMemoryFileEligible("notes.json", [".json"], [])).toEqual({ eligible: true });
    });

    it("is case-insensitive and trims whitespace on allowed extensions", () => {
      expect(isMemoryFileEligible("notes.JSON", [" .JSON "], [])).toEqual({ eligible: true });
    });

    it("rejects files that do not match any glob pattern", () => {
      const { compiledPatterns } = compileFileGlobPatterns("*.md");
      const result = isMemoryFileEligible("notes.json", [], compiledPatterns);
      expect(result.eligible).toBe(false);
      expect(result.reason).toBe("no pattern matched");
    });

    it("requires both extension and glob filters to pass when both are configured", () => {
      const { compiledPatterns } = compileFileGlobPatterns("sub/*.json");
      // Matches glob but not the allowed extension list -> rejected on extension first
      expect(isMemoryFileEligible("sub/notes.json.new", [".json"], compiledPatterns).eligible).toBe(false);
      // Passes both filters
      expect(isMemoryFileEligible("sub/notes.json", [".json"], compiledPatterns).eligible).toBe(true);
    });

    it("matches slashless patterns against root-level (depth 0) files, per the documented FILE_GLOB_FILTER contract", () => {
      // FILE_GLOB_FILTER docs (push_repo_memory.cjs) document that a file at the memory
      // directory root, e.g. "history.jsonl", is matched by the slashless pattern "*.jsonl".
      const { compiledPatterns } = compileFileGlobPatterns("*.jsonl");
      expect(isMemoryFileEligible("history.jsonl", [], compiledPatterns).eligible).toBe(true);
      // A nested file should not match a slashless pattern (single * doesn't cross directories).
      expect(isMemoryFileEligible("sub/history.jsonl", [], compiledPatterns).eligible).toBe(false);
    });
  });

  describe("compileFileGlobPatterns", () => {
    it("returns empty arrays for an empty filter", () => {
      expect(compileFileGlobPatterns("")).toEqual({ patternStrs: [], compiledPatterns: [] });
    });

    it("compiles space-separated patterns", () => {
      const { patternStrs, compiledPatterns } = compileFileGlobPatterns("*.json *.md");
      expect(patternStrs).toEqual(["*.json", "*.md"]);
      expect(compiledPatterns).toHaveLength(2);
    });
  });

  describe("filterIneligibleMemoryFiles", () => {
    let tmpDir;
    const mockCore = { info: () => {} };

    afterEach(() => {
      if (tmpDir) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
        tmpDir = undefined;
      }
    });

    it("removes disallowed files and keeps allowed ones (regression: notes.json.new)", () => {
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-memory-filter-"));
      fs.writeFileSync(path.join(tmpDir, "notes.json"), "{}");
      fs.writeFileSync(path.join(tmpDir, "notes.json.new"), "");

      const result = filterIneligibleMemoryFiles(tmpDir, [".json"], "", mockCore);

      expect(result.kept).toEqual(["notes.json"]);
      expect(result.removed).toEqual([{ path: "notes.json.new", reason: 'disallowed extension ".new"' }]);
      expect(fs.existsSync(path.join(tmpDir, "notes.json"))).toBe(true);
      expect(fs.existsSync(path.join(tmpDir, "notes.json.new"))).toBe(false);
    });

    it("is a no-op success when no files are eligible", () => {
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-memory-filter-"));
      fs.writeFileSync(path.join(tmpDir, "notes.json.new"), "");

      const result = filterIneligibleMemoryFiles(tmpDir, [".json"], "", mockCore);

      expect(result.kept).toEqual([]);
      expect(result.removed).toHaveLength(1);
      expect(fs.existsSync(path.join(tmpDir, "notes.json.new"))).toBe(false);
    });

    it("interacts correctly with file-glob filtering in addition to allowed extensions", () => {
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-memory-filter-"));
      fs.mkdirSync(path.join(tmpDir, "sub"));
      fs.writeFileSync(path.join(tmpDir, "sub", "keep.json"), "{}");
      fs.writeFileSync(path.join(tmpDir, "sub", "skip-glob.json"), "{}");
      fs.writeFileSync(path.join(tmpDir, "sub", "skip-ext.md"), "text");

      const result = filterIneligibleMemoryFiles(tmpDir, [".json"], "sub/keep.json", mockCore);

      expect(result.kept).toEqual(["sub/keep.json"]);
      expect(result.removed.map(f => f.path).sort()).toEqual(["sub/skip-ext.md", "sub/skip-glob.json"]);
    });

    it("handles nested directories, skipping .git", () => {
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-memory-filter-"));
      fs.mkdirSync(path.join(tmpDir, "sub"));
      fs.mkdirSync(path.join(tmpDir, ".git"));
      fs.writeFileSync(path.join(tmpDir, "sub", "data.json"), "{}");
      fs.writeFileSync(path.join(tmpDir, "sub", "data.bin"), "x");
      fs.writeFileSync(path.join(tmpDir, ".git", "HEAD"), "ref: refs/heads/main");

      const result = filterIneligibleMemoryFiles(tmpDir, [".json"], "", mockCore);

      expect(result.kept).toEqual(["sub/data.json"]);
      expect(result.removed).toEqual([{ path: "sub/data.bin", reason: 'disallowed extension ".bin"' }]);
      // .git contents must be left untouched
      expect(fs.existsSync(path.join(tmpDir, ".git", "HEAD"))).toBe(true);
    });

    it("returns empty results when the memory directory does not exist", () => {
      const result = filterIneligibleMemoryFiles(path.join(os.tmpdir(), "gh-aw-memory-filter-does-not-exist"), [".json"], "", mockCore);
      expect(result).toEqual({ kept: [], removed: [] });
    });
  });
});
