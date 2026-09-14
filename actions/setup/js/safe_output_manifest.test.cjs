// @ts-check
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import fs from "fs";
import path from "path";
import {
  MANIFEST_FILE_PATH,
  TEMPORARY_ID_MAP_FILE_PATH,
  SAFE_OUTPUT_ERRORS_FILE_PATH,
  CREATE_ITEM_TYPES,
  NOT_LOGGED_TYPES,
  createManifestLogger,
  ensureManifestExists,
  extractCreatedItemFromResult,
  writeTemporaryIdMapFile,
  writeSafeOutputErrorReport,
} from "./safe_output_manifest.cjs";

describe("safe_output_manifest", () => {
  let testManifestFile;

  beforeEach(() => {
    const testId = Math.random().toString(36).substring(7);
    testManifestFile = `/tmp/test-safe-output-manifest-${testId}/items.jsonl`;
    fs.mkdirSync(path.dirname(testManifestFile), { recursive: true });
  });

  afterEach(() => {
    try {
      const testDir = path.dirname(testManifestFile);
      if (fs.existsSync(testDir)) {
        fs.rmSync(testDir, { recursive: true, force: true });
      }
    } catch (_err) {
      // Ignore cleanup errors
    }
  });

  describe("MANIFEST_FILE_PATH", () => {
    it("should be the expected default path", () => {
      expect(MANIFEST_FILE_PATH).toBe("/tmp/gh-aw/safe-output-items.jsonl");
    });
  });

  describe("CREATE_ITEM_TYPES", () => {
    it("should include expected create types", () => {
      expect(CREATE_ITEM_TYPES.has("create_issue")).toBe(true);
      expect(CREATE_ITEM_TYPES.has("add_comment")).toBe(true);
      expect(CREATE_ITEM_TYPES.has("create_discussion")).toBe(true);
      expect(CREATE_ITEM_TYPES.has("create_pull_request")).toBe(true);
      expect(CREATE_ITEM_TYPES.has("create_project")).toBe(true);
    });

    it("should not include update/delete types", () => {
      expect(CREATE_ITEM_TYPES.has("update_issue")).toBe(false);
      expect(CREATE_ITEM_TYPES.has("close_issue")).toBe(false);
      expect(CREATE_ITEM_TYPES.has("add_labels")).toBe(false);
      expect(CREATE_ITEM_TYPES.has("noop")).toBe(false);
    });
  });

  describe("NOT_LOGGED_TYPES", () => {
    it("should contain only noop and internal meta types", () => {
      expect(NOT_LOGGED_TYPES.has("noop")).toBe(true);
      expect(NOT_LOGGED_TYPES.has("missing_tool")).toBe(true);
      expect(NOT_LOGGED_TYPES.has("missing_data")).toBe(true);
      expect(NOT_LOGGED_TYPES.has("report_incomplete")).toBe(true);
    });

    it("should not contain any handler or modification types (all are logged by default)", () => {
      expect(NOT_LOGGED_TYPES.has("create_issue")).toBe(false);
      expect(NOT_LOGGED_TYPES.has("add_labels")).toBe(false);
      expect(NOT_LOGGED_TYPES.has("close_issue")).toBe(false);
      expect(NOT_LOGGED_TYPES.has("update_issue")).toBe(false);
    });

    it("should not contain CREATE_ITEM_TYPES (they are logged)", () => {
      for (const type of CREATE_ITEM_TYPES) {
        expect(NOT_LOGGED_TYPES.has(type)).toBe(false);
      }
    });

    it("should allow custom safe job types to be logged automatically (not in exclusion list)", () => {
      // Custom safe job types are never added to NOT_LOGGED_TYPES so they are always logged
      expect(NOT_LOGGED_TYPES.has("my_custom_job_type")).toBe(false);
      expect(NOT_LOGGED_TYPES.has("deploy_to_staging")).toBe(false);
    });
  });

  describe("createManifestLogger", () => {
    it("should append a JSONL entry when called with a url", () => {
      const log = createManifestLogger(testManifestFile);
      log({
        type: "create_issue",
        url: "https://github.com/owner/repo/issues/1",
        number: 1,
        repo: "owner/repo",
        temporaryId: "aw_abc123",
        before_state: { title: "Before" },
        after_state: { title: "After" },
      });

      const content = fs.readFileSync(testManifestFile, "utf8");
      const lines = content.trim().split("\n");
      expect(lines).toHaveLength(1);

      const entry = JSON.parse(lines[0]);
      expect(entry.type).toBe("create_issue");
      expect(entry.url).toBe("https://github.com/owner/repo/issues/1");
      expect(entry.number).toBe(1);
      expect(entry.repo).toBe("owner/repo");
      expect(entry.temporaryId).toBe("aw_abc123");
      expect(entry.before_state).toEqual({ title: "Before" });
      expect(entry.after_state).toEqual({ title: "After" });
      expect(entry.timestamp).toBeDefined();
      // timestamp should be a valid ISO 8601 string (Date.parse returns NaN for invalid dates)
      expect(Date.parse(entry.timestamp)).not.toBeNaN();
    });

    it("should append a JSONL entry for an item without a url (modification type)", () => {
      const log = createManifestLogger(testManifestFile);
      log({ type: "add_labels", number: 20875 });

      const content = fs.readFileSync(testManifestFile, "utf8");
      const lines = content.trim().split("\n");
      expect(lines).toHaveLength(1);

      const entry = JSON.parse(lines[0]);
      expect(entry.type).toBe("add_labels");
      expect(entry.url).toBeUndefined();
      expect(entry.number).toBe(20875);
      expect(entry.timestamp).toBeDefined();
    });

    it("should append metadata when provided", () => {
      const log = createManifestLogger(testManifestFile);
      log({
        type: "submit_pull_request_review",
        url: "https://github.com/owner/repo/pull/1#pullrequestreview-10",
        number: 1,
        repo: "owner/repo",
        metadata: { review_id: 10, review_event: "APPROVE" },
      });

      const entry = JSON.parse(fs.readFileSync(testManifestFile, "utf8").trim());
      expect(entry.metadata).toEqual({ review_id: 10, review_event: "APPROVE" });
    });

    it("should persist provider-neutral identity and label target details", () => {
      const log = createManifestLogger(testManifestFile);
      log({
        type: "add_labels",
        provider: "github",
        repo: "owner/repo",
        number: 7,
        target: { provider: "github", repository: "owner/repo", number: 7, kind: "pull_request" },
        labels: [{ name: "bug", database_id: 42, node_id: "LA_kwDO" }],
      });

      const entry = JSON.parse(fs.readFileSync(testManifestFile, "utf8").trim());
      expect(entry.target).toEqual({ provider: "github", repository: "owner/repo", number: 7, kind: "pull_request" });
      expect(entry.labels).toEqual([{ name: "bug", database_id: 42, node_id: "LA_kwDO" }]);
    });

    it("should persist label outcomes even when empty", () => {
      const log = createManifestLogger(testManifestFile);
      log({ type: "add_labels", number: 20875, labelsAdded: [], labelsSuggested: [] });

      const content = fs.readFileSync(testManifestFile, "utf8");
      const entry = JSON.parse(content.trim());
      expect(entry.labelsAdded).toEqual([]);
      expect(entry.labelsSuggested).toEqual([]);
    });

    it("should skip null/undefined items", () => {
      const log = createManifestLogger(testManifestFile);
      log(null);
      log(undefined);

      // File is created by createManifestLogger() immediately, but should be empty
      expect(fs.existsSync(testManifestFile)).toBe(true);
      expect(fs.readFileSync(testManifestFile, "utf8")).toBe("");
    });

    it("should omit optional fields when not provided", () => {
      const log = createManifestLogger(testManifestFile);
      log({ type: "create_discussion", url: "https://github.com/owner/repo/discussions/5" });

      const content = fs.readFileSync(testManifestFile, "utf8");
      const entry = JSON.parse(content.trim());
      expect(entry.type).toBe("create_discussion");
      expect(entry.url).toBe("https://github.com/owner/repo/discussions/5");
      expect(entry.number).toBeUndefined();
      expect(entry.repo).toBeUndefined();
      expect(entry.temporaryId).toBeUndefined();
    });

    it("should append multiple entries in JSONL format (one per line)", () => {
      const log = createManifestLogger(testManifestFile);
      log({ type: "create_issue", url: "https://github.com/owner/repo/issues/1", number: 1, repo: "owner/repo" });
      log({ type: "create_issue", url: "https://github.com/owner/repo/issues/2", number: 2, repo: "owner/repo" });
      log({ type: "add_comment", url: "https://github.com/owner/repo/issues/1#issuecomment-123", repo: "owner/repo" });

      const content = fs.readFileSync(testManifestFile, "utf8");
      const lines = content.trim().split("\n");
      expect(lines).toHaveLength(3);

      const entries = lines.map(l => JSON.parse(l));
      expect(entries[0].type).toBe("create_issue");
      expect(entries[0].number).toBe(1);
      expect(entries[1].number).toBe(2);
      expect(entries[2].type).toBe("add_comment");
    });

    it("should write single-line JSON (no formatting) per entry", () => {
      const log = createManifestLogger(testManifestFile);
      log({ type: "create_issue", url: "https://github.com/owner/repo/issues/1", number: 1, repo: "owner/repo" });

      const content = fs.readFileSync(testManifestFile, "utf8");
      const lines = content.split("\n");
      // Two lines: the JSON entry + trailing newline
      expect(lines).toHaveLength(2);
      expect(lines[1]).toBe("");
      // First line should be parseable as a single JSON object
      expect(() => JSON.parse(lines[0])).not.toThrow();
    });

    it("should redact secret values even when JSON-escaped by quotes or backslashes", () => {
      global.core = { info: () => {}, warning: () => {} };
      process.env.GH_AW_SECRET_NAMES = "CUSTOM_TEST";
      process.env.SECRET_CUSTOM_TEST = 'my"secret\\value';

      const log = createManifestLogger(testManifestFile);
      log({ type: "create_issue", url: "https://github.com/owner/repo/issues/1", metadata: { note: 'contains my"secret\\value here' } });

      const content = fs.readFileSync(testManifestFile, "utf8");
      expect(content).not.toContain('my\\"secret\\\\value');
      expect(content).toContain("***REDACTED***");

      delete global.core;
      delete process.env.GH_AW_SECRET_NAMES;
      delete process.env.SECRET_CUSTOM_TEST;
    });

    it("should warn and fall back to minimal fields when redaction fails, without leaking other fields", () => {
      const warnings = [];
      global.core = { info: () => {}, warning: message => warnings.push(message) };

      const log = createManifestLogger(testManifestFile);
      // A circular metadata object makes the recursive redaction walk blow the call
      // stack, forcing the logger's redaction catch branch to run.
      const circular = { secretLookingField: "should-not-leak" };
      circular.self = circular;
      log({
        type: "create_issue",
        number: 7,
        provider: "github",
        repo: "owner/repo",
        url: "https://github.com/owner/repo/issues/7",
        metadata: circular,
      });

      const content = fs.readFileSync(testManifestFile, "utf8");
      const entry = JSON.parse(content.trim());
      expect(entry).toEqual({ type: "create_issue", provider: "github", number: 7, timestamp: entry.timestamp });
      expect(content).not.toContain("should-not-leak");
      expect(warnings).toHaveLength(1);
      expect(warnings[0]).toContain("create_issue");

      delete global.core;
    });

    it("should throw when the manifest file cannot be written", () => {
      // Create a directory where the file should be to force a write error
      fs.mkdirSync(testManifestFile, { recursive: true });

      const log = createManifestLogger(testManifestFile);
      expect(() => log({ type: "create_issue", url: "https://github.com/owner/repo/issues/1" })).toThrow("Failed to write to manifest file");

      // Clean up
      fs.rmSync(testManifestFile, { recursive: true, force: true });
    });
  });

  describe("ensureManifestExists", () => {
    it("should create an empty file if the manifest does not exist", () => {
      expect(fs.existsSync(testManifestFile)).toBe(false);
      ensureManifestExists(testManifestFile);
      expect(fs.existsSync(testManifestFile)).toBe(true);
      expect(fs.readFileSync(testManifestFile, "utf8")).toBe("");
    });

    it("should not overwrite an existing file", () => {
      const content = '{"type":"create_issue","url":"https://github.com/o/r/issues/1","timestamp":"2024-01-01T00:00:00.000Z"}\n';
      fs.writeFileSync(testManifestFile, content);

      ensureManifestExists(testManifestFile);

      expect(fs.readFileSync(testManifestFile, "utf8")).toBe(content);
    });

    it("should throw when the file cannot be created", () => {
      // Use a path under a non-existent directory without creating it
      const badFile = "/tmp/nonexistent-dir-xyz/items.jsonl";
      expect(() => ensureManifestExists(badFile)).toThrow("Failed to create manifest file");
    });
  });

  describe("extractCreatedItemFromResult", () => {
    it("should extract item from create_issue result", () => {
      const result = { success: true, repo: "owner/repo", number: 42, url: "https://github.com/owner/repo/issues/42", temporaryId: "aw_def456" };
      const item = extractCreatedItemFromResult("create_issue", result);
      expect(item).toEqual({
        type: "create_issue",
        url: "https://github.com/owner/repo/issues/42",
        number: 42,
        repo: "owner/repo",
        provider: "github",
        target: { provider: "github", repository: "owner/repo", number: 42 },
        temporaryId: "aw_def456",
      });
    });

    it.each([
      ["add_comment", { commentId: 123, itemNumber: 7, repo: "owner/repo" }, { provider: "github", id: 123 }],
      ["jira_create_issue", { issue_id: "10001", issue_key: "ENG-7" }, { provider: "jira", id: "10001", identifier: "ENG-7" }],
      ["linear_create_issue", { id: "linear-id", identifier: "ENG-8" }, { provider: "linear", id: "linear-id", identifier: "ENG-8" }],
    ])("should preserve identity for %s", (type, result, expected) => {
      expect(extractCreatedItemFromResult(type, result)).toMatchObject(expected);
    });

    it("should infer the azure-devops provider (hyphenated) from the metadata provider, matching ADO handler naming", () => {
      const item = extractCreatedItemFromResult("ado_add_comment", {
        success: true,
        number: 123,
        metadata: { provider: "azure-devops", project: "my-project", comment_id: 5 },
      });
      expect(item?.provider).toBe("azure-devops");
      expect(item?.provider).not.toBe("azure_devops");
    });

    it("should convert a scalar target (e.g. Linear issue key) into a provider-neutral object", () => {
      const item = extractCreatedItemFromResult("linear_add_comment", {
        success: true,
        id: "comment-1",
        target: "ENG-123",
      });
      expect(item?.target).toEqual({ provider: "linear", identifier: "ENG-123" });
    });

    it("should associate added labels with their target and IDs", () => {
      const item = extractCreatedItemFromResult("add_labels", {
        repo: "owner/repo",
        number: 12,
        contextType: "pull_request",
        labelsAdded: ["bug"],
        labels: [{ name: "bug", database_id: 42 }],
      });

      expect(item).toMatchObject({
        provider: "github",
        target: { provider: "github", repository: "owner/repo", number: 12, kind: "pull_request" },
        labels: [{ name: "bug", database_id: 42 }],
      });
    });

    it("should persist metadata for review lifecycle items", () => {
      const item = extractCreatedItemFromResult("add_reviewer", {
        success: true,
        repo: "owner/repo",
        number: 42,
        metadata: {
          requested_reviewers: ["reviewer1"],
          requested_team_reviewers: ["platform-team"],
        },
      });
      expect(item).toEqual({
        type: "add_reviewer",
        number: 42,
        repo: "owner/repo",
        provider: "github",
        target: { provider: "github", repository: "owner/repo", number: 42 },
        metadata: {
          requested_reviewers: ["reviewer1"],
          requested_team_reviewers: ["platform-team"],
        },
      });
    });

    it("should extract item from create_project result using projectUrl", () => {
      const result = { success: true, projectUrl: "https://github.com/orgs/owner/projects/5", temporaryId: "aw_proj01" };
      const item = extractCreatedItemFromResult("create_project", result);
      expect(item).not.toBeNull();
      expect(item.url).toBe("https://github.com/orgs/owner/projects/5");
      expect(item.type).toBe("create_project");
    });

    it("should extract item from add_comment result", () => {
      const result = { success: true, commentId: 999, url: "https://github.com/owner/repo/issues/1#issuecomment-999", repo: "owner/repo", itemNumber: 1 };
      const item = extractCreatedItemFromResult("add_comment", result);
      expect(item).not.toBeNull();
      expect(item.url).toBe("https://github.com/owner/repo/issues/1#issuecomment-999");
      expect(item.type).toBe("add_comment");
    });

    it("should return null for excluded types (noop and internal meta types)", () => {
      const result = { success: true, url: "https://github.com/owner/repo/issues/1" };
      expect(extractCreatedItemFromResult("noop", result)).toBeNull();
      expect(extractCreatedItemFromResult("missing_tool", result)).toBeNull();
      expect(extractCreatedItemFromResult("missing_data", result)).toBeNull();
      expect(extractCreatedItemFromResult("report_incomplete", result)).toBeNull();
    });

    it("should extract item from custom safe job type (generic: any type not excluded is logged)", () => {
      const result = { success: true, number: 42 };
      const item = extractCreatedItemFromResult("my_custom_job_type", result);
      expect(item).not.toBeNull();
      expect(item.type).toBe("my_custom_job_type");
      expect(item.number).toBe(42);
    });

    it("should extract item from add_labels result (modification type without url)", () => {
      const result = {
        success: true,
        number: 20875,
        repo: "owner/repo",
        labelsAdded: ["bug", "cli"],
        labelsSuggested: ["needs-review"],
        contextType: "issue",
        before_state: { labels: ["triage"] },
        after_state: { labels: ["triage", "bug", "cli"] },
      };
      const item = extractCreatedItemFromResult("add_labels", result);
      expect(item).not.toBeNull();
      expect(item.type).toBe("add_labels");
      expect(item.url).toBeUndefined();
      expect(item.number).toBe(20875);
      expect(item.repo).toBe("owner/repo");
      expect(item.labelsAdded).toEqual(["bug", "cli"]);
      expect(item.labelsSuggested).toEqual(["needs-review"]);
      expect(item.before_state).toEqual({ labels: ["triage"] });
      expect(item.after_state).toEqual({ labels: ["triage", "bug", "cli"] });
    });

    it("should defer submit_pull_request_review manifest logging until final review result is available", () => {
      const bufferedResult = { success: true, event: "COMMENT", body_length: 12 };
      expect(extractCreatedItemFromResult("submit_pull_request_review", bufferedResult)).toBeNull();

      const finalizedResult = {
        success: true,
        review_url: "https://github.com/owner/repo/pull/1#pullrequestreview-2",
        pull_request_number: 1,
        repo: "owner/repo",
        before_state: { reviews: [] },
        after_state: { reviews: [{ id: 2, state: "COMMENTED" }] },
      };
      expect(extractCreatedItemFromResult("submit_pull_request_review", finalizedResult)).toEqual({
        type: "submit_pull_request_review",
        url: "https://github.com/owner/repo/pull/1#pullrequestreview-2",
        number: 1,
        repo: "owner/repo",
        provider: "github",
        target: { provider: "github", repository: "owner/repo", number: 1 },
        before_state: { reviews: [] },
        after_state: { reviews: [{ id: 2, state: "COMMENTED" }] },
      });
    });

    it("should extract item from close_issue result (modification type with url)", () => {
      const result = { success: true, number: 123, url: "https://github.com/owner/repo/issues/123", title: "Test" };
      const item = extractCreatedItemFromResult("close_issue", result);
      expect(item).not.toBeNull();
      expect(item.type).toBe("close_issue");
      expect(item.url).toBe("https://github.com/owner/repo/issues/123");
      expect(item.number).toBe(123);
    });

    it("should return null for staged results (no item actually modified)", () => {
      // Staged results have staged: true — nothing was really changed
      const stagedResult = { success: true, staged: true, previewInfo: { repo: "owner/repo", title: "Test" } };
      expect(extractCreatedItemFromResult("create_issue", stagedResult)).toBeNull();
      expect(extractCreatedItemFromResult("add_comment", stagedResult)).toBeNull();
      expect(extractCreatedItemFromResult("add_labels", stagedResult)).toBeNull();
    });

    it("should return null for staged results even if url is somehow present", () => {
      // Defensive: staged flag takes precedence over any URL
      const stagedResultWithUrl = { success: true, staged: true, url: "https://github.com/owner/repo/issues/1" };
      expect(extractCreatedItemFromResult("create_issue", stagedResultWithUrl)).toBeNull();
    });

    it("should return null for deferred manifest results", () => {
      expect(extractCreatedItemFromResult("submit_pull_request_review", { success: true, deferred_manifest: true })).toBeNull();
      expect(extractCreatedItemFromResult("add_reviewer", { success: true, skipped: true, repo: "owner/repo", number: 42 })).toBeNull();
    });

    it("should return item without url when result has no URL (for logged types)", () => {
      const result = { success: true, repo: "owner/repo", number: 1 };
      const item = extractCreatedItemFromResult("create_issue", result);
      expect(item).not.toBeNull();
      expect(item.url).toBeUndefined();
      expect(item.number).toBe(1);
    });

    it("should return null for null/undefined result", () => {
      expect(extractCreatedItemFromResult("create_issue", null)).toBeNull();
      expect(extractCreatedItemFromResult("create_issue", undefined)).toBeNull();
    });

    it("should omit optional fields when not present in result", () => {
      const result = { success: true, url: "https://github.com/owner/repo/issues/1" };
      const item = extractCreatedItemFromResult("create_issue", result);
      expect(item).not.toBeNull();
      expect(item.number).toBeUndefined();
      expect(item.repo).toBeUndefined();
      expect(item.temporaryId).toBeUndefined();
    });
  });

  describe("TEMPORARY_ID_MAP_FILE_PATH", () => {
    it("should be the expected default path", () => {
      expect(TEMPORARY_ID_MAP_FILE_PATH).toBe("/tmp/gh-aw/temporary-id-map.json");
    });
  });

  describe("writeTemporaryIdMapFile", () => {
    let testTempIdFile;

    beforeEach(() => {
      const testId = Math.random().toString(36).substring(7);
      testTempIdFile = `/tmp/test-temp-id-map-${testId}/temporary-id-map.json`;
    });

    afterEach(() => {
      try {
        const testDir = path.dirname(testTempIdFile);
        if (fs.existsSync(testDir)) {
          fs.rmSync(testDir, { recursive: true, force: true });
        }
      } catch (_err) {
        // Ignore cleanup errors
      }
    });

    it("should write an empty map as pretty-printed JSON", () => {
      writeTemporaryIdMapFile({}, testTempIdFile);

      const content = fs.readFileSync(testTempIdFile, "utf8");
      expect(content).toBe("{}\n");
    });

    it("should write a map with entries as pretty-printed JSON", () => {
      const tempIdMap = {
        aw_abc123: { repo: "owner/repo", number: 42 },
        aw_def456: { repo: "owner/repo", number: 100 },
      };

      writeTemporaryIdMapFile(tempIdMap, testTempIdFile);

      const content = fs.readFileSync(testTempIdFile, "utf8");
      const parsed = JSON.parse(content);
      expect(parsed).toEqual(tempIdMap);
      // Verify pretty-printing (contains newlines and indentation)
      expect(content).toContain("\n");
      expect(content).toContain("  ");
      // Verify trailing newline
      expect(content.endsWith("\n")).toBe(true);
    });

    it("should create parent directories if they do not exist", () => {
      const deepPath = `/tmp/test-temp-id-deep-${Math.random().toString(36).substring(7)}/a/b/c/map.json`;
      writeTemporaryIdMapFile({ aw_test: { repo: "o/r", number: 1 } }, deepPath);

      expect(fs.existsSync(deepPath)).toBe(true);
      const parsed = JSON.parse(fs.readFileSync(deepPath, "utf8"));
      expect(parsed.aw_test).toEqual({ repo: "o/r", number: 1 });

      // Cleanup
      fs.rmSync(path.dirname(deepPath).split("/a/")[0] + "/a", { recursive: true, force: true });
    });

    it("should overwrite an existing file", () => {
      writeTemporaryIdMapFile({ aw_old: { repo: "o/r", number: 1 } }, testTempIdFile);
      writeTemporaryIdMapFile({ aw_new: { repo: "o/r", number: 2 } }, testTempIdFile);

      const parsed = JSON.parse(fs.readFileSync(testTempIdFile, "utf8"));
      expect(parsed.aw_old).toBeUndefined();
      expect(parsed.aw_new).toEqual({ repo: "o/r", number: 2 });
    });

    it("should throw when the file cannot be written", () => {
      // Use a path under /dev/null which is a file, not a directory — mkdirSync fails immediately
      expect(() => writeTemporaryIdMapFile({}, "/dev/null/fake/map.json")).toThrow("Failed to write temporary ID map file");
    });
  });
  describe("writeSafeOutputErrorReport", () => {
    let testErrorsFile;

    beforeEach(() => {
      const testId = Math.random().toString(36).substring(7);
      testErrorsFile = `/tmp/test-safe-output-errors-${testId}/safe-output-errors.json`;
      global.core = {
        info: () => {},
        warning: () => {},
      };
      delete process.env.GH_AW_SECRET_NAMES;
      delete process.env.SECRET_CUSTOM_TEST;
    });

    afterEach(() => {
      try {
        const testDir = path.dirname(testErrorsFile);
        if (fs.existsSync(testDir)) {
          fs.rmSync(testDir, { recursive: true, force: true });
        }
        fs.rmSync("/tmp/gh-aw/mcp-config", { recursive: true, force: true });
      } catch (_err) {
        // Ignore cleanup errors
      }
    });

    it("should be exported with the expected default path", () => {
      expect(SAFE_OUTPUT_ERRORS_FILE_PATH).toBe("/tmp/gh-aw/safe-output-errors.json");
    });

    it("should write structured failure diagnostics", () => {
      writeSafeOutputErrorReport(
        {
          errorCode: "E099",
          message: "1 safe output(s) failed:\n  - create_issue: boom",
          failures: [{ type: "create_issue", errorCode: "E007", error: "boom" }],
        },
        testErrorsFile
      );

      const parsed = JSON.parse(fs.readFileSync(testErrorsFile, "utf8"));
      expect(parsed.status).toBe("failure");
      expect(parsed.errorCode).toBe("E099");
      expect(parsed.message).toContain("create_issue: boom");
      expect(parsed.failures).toEqual([{ type: "create_issue", errorCode: "E007", error: "boom" }]);
      expect(typeof parsed.timestamp).toBe("string");
    });

    it("should default failures to an empty array and create parent directories", () => {
      writeSafeOutputErrorReport({ errorCode: "ERR_VALIDATION", message: "Handler manager failed" }, testErrorsFile);

      const parsed = JSON.parse(fs.readFileSync(testErrorsFile, "utf8"));
      expect(parsed.failures).toEqual([]);
      expect(parsed.message).toBe("Handler manager failed");
    });

    it("should redact credential patterns from the report", () => {
      writeSafeOutputErrorReport({ message: `token ghp_${"a".repeat(36)} leaked` }, testErrorsFile);

      const content = fs.readFileSync(testErrorsFile, "utf8");
      expect(content).not.toContain("ghp_aaaa");
      expect(content).toContain("***REDACTED***");
    });

    it("should redact workflow-configured custom secrets from the report", () => {
      process.env.GH_AW_SECRET_NAMES = "CUSTOM_TEST";
      process.env.SECRET_CUSTOM_TEST = "my-custom-safe-output-secret";

      writeSafeOutputErrorReport({ message: "leaked my-custom-safe-output-secret value" }, testErrorsFile);

      const content = fs.readFileSync(testErrorsFile, "utf8");
      expect(content).not.toContain("my-custom-safe-output-secret");
      expect(content).toContain("***REDACTED***");
    });

    it("should redact MCP gateway bearer tokens and raw credentials from the report", () => {
      const gatewayCredential = "mcp_gateway_token_for_test_123456";
      const bearerValue = "Bearer " + gatewayCredential;
      const gatewayConfigPath = "/tmp/gh-aw/mcp-config/gateway-output.json";
      fs.mkdirSync(path.dirname(gatewayConfigPath), { recursive: true });
      fs.writeFileSync(
        gatewayConfigPath,
        JSON.stringify({
          mcpServers: {
            gateway: {
              headers: {
                Authorization: bearerValue,
              },
            },
          },
        })
      );

      writeSafeOutputErrorReport({ message: `headers: ${bearerValue} and ${gatewayCredential}` }, testErrorsFile);

      const content = fs.readFileSync(testErrorsFile, "utf8");
      expect(content).not.toContain(bearerValue);
      expect(content).not.toContain(gatewayCredential);
      expect(content).toContain("***REDACTED***");
    });

    it("should throw when the file cannot be written", () => {
      expect(() => writeSafeOutputErrorReport({ message: "x" }, "/dev/null/fake/errors.json")).toThrow("Failed to write safe output error report");
    });
  });
});
