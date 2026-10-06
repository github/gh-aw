// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Paths match what upload_artifact.cjs computes at runtime.
// When RUNNER_TEMP is unset (test default), STAGING_DIR falls back to /tmp/gh-aw/.
const STAGING_DIR = "/tmp/gh-aw/safeoutputs/upload-artifacts/";
const RESOLVER_FILE = "/tmp/gh-aw/artifact-resolver.json";

describe("upload_artifact.cjs", () => {
  let mockCore;
  let mockArtifactClient;
  let originalEnv;

  /**
   * @param {string} relPath
   * @param {string} content
   */
  function writeStaging(relPath, content = "test content") {
    const fullPath = path.join(STAGING_DIR, relPath);
    fs.mkdirSync(path.dirname(fullPath), { recursive: true });
    fs.writeFileSync(fullPath, content);
  }

  /**
   * Build a config object.
   * @param {object} overrides
   */
  function buildConfig(overrides = {}) {
    return {
      "max-uploads": 3,
      "retention-days": 30,
      "max-size-bytes": 104857600,
      ...overrides,
    };
  }

  /**
   * Run the handler against a list of messages using the per-message handler pattern.
   * Injects global.__createArtifactClient so tests never hit the real REST API.
   * @param {object} config
   * @param {object[]} messages
   * @returns {Promise<object[]>}
   */
  async function runHandler(config, messages) {
    const scriptText = fs.readFileSync(path.join(__dirname, "upload_artifact.cjs"), "utf8");
    global.core = mockCore;
    global.__createArtifactClient = () => mockArtifactClient;
    let handlerFn;
    await eval(`(async () => { ${scriptText}; handlerFn = await main(config); })()`);
    const results = [];
    for (const msg of messages) {
      const result = await handlerFn(msg, {}, new Map());
      results.push(result);
      if (result && result.success === false && !result.skipped) {
        mockCore.setFailed(result.error);
      }
    }
    return results;
  }

  beforeEach(() => {
    vi.clearAllMocks();

    mockCore = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setOutput: vi.fn(),
      setFailed: vi.fn(),
      summary: {
        addHeading: vi.fn().mockReturnThis(),
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };

    mockArtifactClient = {
      uploadArtifact: vi.fn().mockResolvedValue({ id: 42, size: 100 }),
    };

    originalEnv = { ...process.env };

    delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
    // Clear RUNNER_TEMP so the handler falls back to /tmp, matching the test's STAGING_DIR
    delete process.env.RUNNER_TEMP;

    // Ensure staging dir exists and is clean
    if (fs.existsSync(STAGING_DIR)) {
      fs.rmSync(STAGING_DIR, { recursive: true });
    }
    fs.mkdirSync(STAGING_DIR, { recursive: true });

    // Clean resolver file
    if (fs.existsSync(RESOLVER_FILE)) {
      fs.unlinkSync(RESOLVER_FILE);
    }
  });

  afterEach(() => {
    process.env = originalEnv;
    delete global.__createArtifactClient;
  });

  describe("path-based upload", () => {
    it.each(["7", "400"])("uses expression-resolved retention %s", async retention => {
      writeStaging("report.json");
      await runHandler(buildConfig({ "retention-days": retention }), [{ type: "upload_artifact", path: "report.json" }]);
      expect(mockArtifactClient.uploadArtifact.mock.calls[0][3].retentionDays).toBe(Number(retention));
    });

    it.each(["", "0", "-1", "401", "7days", "1.5", true, {}, []])("rejects invalid resolved retention %j", async retention => {
      await expect(runHandler(buildConfig({ "retention-days": retention }), [])).rejects.toThrow("retention-days must resolve to an integer between 1 and 400");
      expect(mockArtifactClient.uploadArtifact).not.toHaveBeenCalled();
    });

    it("uploads a single file using config retention days", async () => {
      writeStaging("report.json", '{"result": "ok"}');

      const results = await runHandler(buildConfig({ "retention-days": 14 }), [{ type: "upload_artifact", path: "report.json" }]);

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(results[0].success).toBe(true);
      expect(mockArtifactClient.uploadArtifact).toHaveBeenCalledOnce();
      const [name, files, rootDir, opts] = mockArtifactClient.uploadArtifact.mock.calls[0];
      expect(name).toBe("report.json");
      expect(files).toContain(path.join(STAGING_DIR, "report.json"));
      expect(rootDir).toBe(STAGING_DIR);
      expect(opts.retentionDays).toBe(14);
      expect(mockCore.setOutput).toHaveBeenCalledWith("upload_artifact_count", "1");
    });

    it("returns temporaryId matching tmpId in the result", async () => {
      writeStaging("report.json");

      const results = await runHandler(buildConfig(), [{ type: "upload_artifact", path: "report.json" }]);

      expect(results[0].success).toBe(true);
      expect(results[0].tmpId).toBeDefined();
      expect(results[0].temporaryId).toBe(results[0].tmpId);
    });

    it("uses default retention of 30 when retention-days not in config", async () => {
      writeStaging("report.json");

      // Omit retention-days from config to test default
      await runHandler({ "max-uploads": 1, "max-size-bytes": 104857600 }, [{ type: "upload_artifact", path: "report.json" }]);

      const [, , , opts] = mockArtifactClient.uploadArtifact.mock.calls[0];
      expect(opts.retentionDays).toBe(30);
    });

    it("ignores retention_days in the message (agent cannot override)", async () => {
      writeStaging("report.json");

      // Even if the agent sends retention_days: 999, the config value (14) should be used.
      await runHandler(buildConfig({ "retention-days": 14 }), [{ type: "upload_artifact", path: "report.json", retention_days: 999 }]);

      const [, , , opts] = mockArtifactClient.uploadArtifact.mock.calls[0];
      expect(opts.retentionDays).toBe(14);
    });
  });

  describe("validation errors", () => {
    it("fails when both path and filters are present", async () => {
      writeStaging("report.json");

      await runHandler(buildConfig(), [{ type: "upload_artifact", path: "report.json", filters: { include: ["**/*.json"] } }]);

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("exactly one of 'path' or 'filters'"));
      expect(mockArtifactClient.uploadArtifact).not.toHaveBeenCalled();
    });

    it("fails when neither path nor filters are present", async () => {
      await runHandler(buildConfig(), [{ type: "upload_artifact" }]);
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("exactly one of 'path' or 'filters'"));
      expect(mockArtifactClient.uploadArtifact).not.toHaveBeenCalled();
    });

    it("fails when path traverses outside staging dir", async () => {
      await runHandler(buildConfig(), [{ type: "upload_artifact", path: "../etc/passwd" }]);
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("must not traverse outside staging directory"));
      expect(mockArtifactClient.uploadArtifact).not.toHaveBeenCalled();
    });

    it("fails when absolute path does not exist", async () => {
      await runHandler(buildConfig(), [{ type: "upload_artifact", path: "/nonexistent/path/file.json" }]);
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("absolute path does not exist"));
      expect(mockArtifactClient.uploadArtifact).not.toHaveBeenCalled();
    });

    it("fails when path does not exist in staging dir", async () => {
      await runHandler(buildConfig(), [{ type: "upload_artifact", path: "nonexistent.json" }]);
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("does not exist in staging directory"));
      expect(mockArtifactClient.uploadArtifact).not.toHaveBeenCalled();
    });

    it("fails when max-uploads is exceeded", async () => {
      writeStaging("a.json");
      writeStaging("b.json");

      const results = await runHandler(buildConfig({ "max-uploads": 1 }), [
        { type: "upload_artifact", path: "a.json" },
        { type: "upload_artifact", path: "b.json" },
      ]);

      expect(results[0].success).toBe(true);
      expect(results[1].success).toBe(false);
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("exceeded max-uploads policy"));
      expect(mockArtifactClient.uploadArtifact).toHaveBeenCalledOnce();
    });

    it("fails when skip-archive=true in config with multiple files", async () => {
      writeStaging("output/a.json");
      writeStaging("output/b.json");

      await runHandler(buildConfig({ "skip-archive": true }), [{ type: "upload_artifact", filters: { include: ["output/**"] } }]);

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("skip-archive requires exactly one selected file"));
      expect(mockArtifactClient.uploadArtifact).not.toHaveBeenCalled();
    });

    it("fails when upload client throws", async () => {
      writeStaging("report.json");
      mockArtifactClient.uploadArtifact.mockRejectedValue(new Error("network failure"));

      const results = await runHandler(buildConfig(), [{ type: "upload_artifact", path: "report.json" }]);

      expect(results[0].success).toBe(false);
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("network failure"));
    });
  });

  describe("skip-archive from config", () => {
    it("succeeds with skip-archive=true in config and a single file", async () => {
      writeStaging("app.bin", "binary data");

      const results = await runHandler(buildConfig({ "skip-archive": true }), [{ type: "upload_artifact", path: "app.bin" }]);

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(results[0].success).toBe(true);
      expect(mockArtifactClient.uploadArtifact).toHaveBeenCalledOnce();
    });

    it("passes skipArchive option to artifact client when skip-archive=true", async () => {
      writeStaging("chart.png", "png data");

      await runHandler(buildConfig({ "skip-archive": true }), [{ type: "upload_artifact", path: "chart.png" }]);

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      const [, , , opts] = mockArtifactClient.uploadArtifact.mock.calls[0];
      expect(opts.skipArchive).toBe(true);
    });

    it("does not pass skipArchive option when skip-archive is false", async () => {
      writeStaging("report.json");

      await runHandler(buildConfig({ "skip-archive": false }), [{ type: "upload_artifact", path: "report.json" }]);

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      const [, , , opts] = mockArtifactClient.uploadArtifact.mock.calls[0];
      expect(opts.skipArchive).toBeUndefined();
    });

    it("ignores skip_archive in the message (agent cannot override)", async () => {
      writeStaging("app.bin", "binary data");

      // Config has skip-archive: false; agent sends skip_archive: true — config wins
      const results = await runHandler(buildConfig({ "skip-archive": false }), [{ type: "upload_artifact", path: "app.bin", skip_archive: true }]);

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(results[0].success).toBe(true);
      // No skip-archive error since config says false (so no single-file constraint check triggers)
      expect(mockArtifactClient.uploadArtifact).toHaveBeenCalledOnce();
    });
  });

  describe("artifact URL output", () => {
    it("outputs artifact_id and artifact_url when upload succeeds", async () => {
      process.env.GITHUB_SERVER_URL = "https://github.com";
      process.env.GITHUB_REPOSITORY = "owner/repo";
      process.env.GITHUB_RUN_ID = "12345";
      writeStaging("report.json");

      const results = await runHandler(buildConfig(), [{ type: "upload_artifact", path: "report.json" }]);

      expect(results[0].success).toBe(true);
      expect(results[0].artifactId).toBe(42);
      expect(results[0].artifactUrl).toBe("https://github.com/owner/repo/actions/runs/12345/artifacts/42");
      expect(mockCore.setOutput).toHaveBeenCalledWith("slot_0_artifact_id", "42");
      expect(mockCore.setOutput).toHaveBeenCalledWith("slot_0_artifact_url", "https://github.com/owner/repo/actions/runs/12345/artifacts/42");
    });

    it("does not output artifact_url when env vars are missing", async () => {
      delete process.env.GITHUB_SERVER_URL;
      delete process.env.GITHUB_REPOSITORY;
      delete process.env.GITHUB_RUN_ID;
      writeStaging("report.json");

      const results = await runHandler(buildConfig(), [{ type: "upload_artifact", path: "report.json" }]);

      expect(results[0].success).toBe(true);
      expect(results[0].artifactId).toBe(42);
      expect(results[0].artifactUrl).toBe("");
    });

    it("does not output artifact_id or artifact_url in staged mode", async () => {
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
      process.env.GITHUB_SERVER_URL = "https://github.com";
      process.env.GITHUB_REPOSITORY = "owner/repo";
      process.env.GITHUB_RUN_ID = "12345";
      writeStaging("report.json");

      const results = await runHandler(buildConfig(), [{ type: "upload_artifact", path: "report.json" }]);

      expect(results[0].success).toBe(true);
      expect(results[0].artifactUrl).toBe("");
      const setOutputCalls = mockCore.setOutput.mock.calls.map(c => c[0]);
      expect(setOutputCalls).not.toContain("slot_0_artifact_id");
      expect(setOutputCalls).not.toContain("slot_0_artifact_url");
    });
  });

  describe("filter-based upload", () => {
    it("selects files matching include pattern", async () => {
      writeStaging("reports/daily/summary.json", "{}");
      writeStaging("reports/weekly/summary.json", "{}");
      writeStaging("reports/private/secret.json", "{}");

      await runHandler(buildConfig(), [
        {
          type: "upload_artifact",
          filters: { include: ["reports/**/*.json"], exclude: ["reports/private/**"] },
        },
      ]);

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockArtifactClient.uploadArtifact).toHaveBeenCalledOnce();
      const [, files] = mockArtifactClient.uploadArtifact.mock.calls[0];
      expect(files).toHaveLength(2);
      expect(mockCore.setOutput).toHaveBeenCalledWith("slot_0_file_count", "2");
    });

    it("handles no-files with if-no-files=ignore", async () => {
      await runHandler(buildConfig({ "default-if-no-files": "ignore" }), [{ type: "upload_artifact", filters: { include: ["nonexistent/**"] } }]);

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockArtifactClient.uploadArtifact).not.toHaveBeenCalled();
    });

    it("fails when no files match and if-no-files=error (default)", async () => {
      await runHandler(buildConfig(), [{ type: "upload_artifact", filters: { include: ["nonexistent/**"] } }]);

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("no files matched"));
      expect(mockArtifactClient.uploadArtifact).not.toHaveBeenCalled();
    });
  });

  describe("allowed-paths policy", () => {
    it("filters out files not in allowed-paths", async () => {
      writeStaging("dist/app.js");
      writeStaging("secret.env");

      await runHandler(buildConfig({ "allowed-paths": ["dist/**"] }), [{ type: "upload_artifact", filters: { include: ["**"] } }]);

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      const [, files] = mockArtifactClient.uploadArtifact.mock.calls[0];
      expect(files).toHaveLength(1);
      expect(mockCore.setOutput).toHaveBeenCalledWith("slot_0_file_count", "1");
    });
  });

  describe("filters-include / filters-exclude from config", () => {
    it("uses config filters-include as default when request has no filters", async () => {
      writeStaging("dist/app.js");
      writeStaging("secret.env");

      await runHandler(buildConfig({ "filters-include": ["dist/**"] }), [{ type: "upload_artifact", filters: {} }]);

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.setOutput).toHaveBeenCalledWith("slot_0_file_count", "1");
    });
  });

  describe("staged mode", () => {
    it("skips upload client call in staged mode (env var)", async () => {
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
      writeStaging("report.json");

      const results = await runHandler(buildConfig(), [{ type: "upload_artifact", path: "report.json" }]);

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(results[0].success).toBe(true);
      expect(mockArtifactClient.uploadArtifact).not.toHaveBeenCalled();
      expect(mockCore.setOutput).toHaveBeenCalledWith("slot_0_tmp_id", expect.stringMatching(/^aw_[A-Za-z0-9]{8}$/));
    });

    it("skips upload client call when staged=true in config", async () => {
      writeStaging("report.json");

      const results = await runHandler(buildConfig({ staged: true }), [{ type: "upload_artifact", path: "report.json" }]);

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(results[0].success).toBe(true);
      expect(mockArtifactClient.uploadArtifact).not.toHaveBeenCalled();
    });

    it("emits a staged mode preview summary when staged mode is active", async () => {
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
      writeStaging("output.json");

      await runHandler(buildConfig(), [{ type: "upload_artifact", path: "output.json" }]);

      expect(mockCore.summary.addRaw).toHaveBeenCalled();
      const summaryContent = mockCore.summary.addRaw.mock.calls[0][0];
      expect(summaryContent).toContain("🎭 Staged Mode");
      expect(summaryContent).toContain("Upload Artifact Preview");
      expect(summaryContent).toContain("output.json");
      expect(mockCore.summary.write).toHaveBeenCalled();
    });
  });

  describe("resolver file", () => {
    it("writes a resolver mapping with temporary IDs", async () => {
      writeStaging("report.json");

      await runHandler(buildConfig(), [{ type: "upload_artifact", path: "report.json" }]);

      expect(fs.existsSync(RESOLVER_FILE)).toBe(true);
      const resolver = JSON.parse(fs.readFileSync(RESOLVER_FILE, "utf8"));
      const keys = Object.keys(resolver);
      expect(keys.length).toBe(1);
      expect(keys[0]).toMatch(/^aw_[A-Za-z0-9]{8}$/);
    });
  });

  describe("temporary_id field", () => {
    it("uses declared temporary_id from message when valid", async () => {
      writeStaging("chart.png", "PNG_DATA");

      const results = await runHandler(buildConfig({ "skip-archive": true }), [{ type: "upload_artifact", path: "chart.png", temporary_id: "aw_chart1" }]);

      expect(results[0].success).toBe(true);
      expect(results[0].tmpId).toBe("aw_chart1");
      expect(mockCore.setOutput).toHaveBeenCalledWith("slot_0_tmp_id", "aw_chart1");
    });

    it("normalises declared temporary_id to lowercase", async () => {
      writeStaging("chart.png", "PNG_DATA");

      const results = await runHandler(buildConfig({ "skip-archive": true }), [{ type: "upload_artifact", path: "chart.png", temporary_id: "aw_CHART1" }]);

      expect(results[0].success).toBe(true);
      expect(results[0].tmpId).toBe("aw_chart1");
    });

    it("strips leading '#' from declared temporary_id", async () => {
      writeStaging("chart.png", "PNG_DATA");

      const results = await runHandler(buildConfig({ "skip-archive": true }), [{ type: "upload_artifact", path: "chart.png", temporary_id: "#aw_chart1" }]);

      expect(results[0].success).toBe(true);
      expect(results[0].tmpId).toBe("aw_chart1");
    });

    it("generates a random ID when temporary_id is not provided", async () => {
      writeStaging("report.json");

      const results = await runHandler(buildConfig(), [{ type: "upload_artifact", path: "report.json" }]);

      expect(results[0].success).toBe(true);
      expect(results[0].tmpId).toMatch(/^aw_[A-Za-z0-9]{8}$/);
    });

    it("generates a random ID and emits warning when temporary_id format is invalid", async () => {
      writeStaging("report.json");

      const results = await runHandler(buildConfig(), [{ type: "upload_artifact", path: "report.json", temporary_id: "bad-format" }]);

      expect(results[0].success).toBe(true);
      expect(results[0].tmpId).toMatch(/^aw_[A-Za-z0-9]{8}$/);
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("invalid temporary_id format"));
    });

    it("uses declared temporary_id in resolver file", async () => {
      writeStaging("chart.png", "PNG_DATA");

      await runHandler(buildConfig({ "skip-archive": true }), [{ type: "upload_artifact", path: "chart.png", temporary_id: "aw_chart1" }]);

      const resolver = JSON.parse(fs.readFileSync(RESOLVER_FILE, "utf8"));
      expect(resolver["aw_chart1"]).toBe("chart.png");
    });

    it("warns and keeps first mapping when the same temporary_id is used in multiple uploads", async () => {
      writeStaging("chart1.png", "PNG_DATA_1");
      writeStaging("chart2.png", "PNG_DATA_2");

      const results = await runHandler(buildConfig({ "max-uploads": 2 }), [
        { type: "upload_artifact", path: "chart1.png", temporary_id: "aw_chart1" },
        { type: "upload_artifact", path: "chart2.png", temporary_id: "aw_chart1" },
      ]);

      // Both uploads succeed individually
      expect(results[0].success).toBe(true);
      expect(results[1].success).toBe(true);

      // Resolver should keep the first mapping (chart1.png)
      const resolver = JSON.parse(fs.readFileSync(RESOLVER_FILE, "utf8"));
      expect(resolver["aw_chart1"]).toBe("chart1.png");

      // A warning should have been emitted for the duplicate
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining('duplicate temporary_id "aw_chart1"'));
    });
  });

  describe("max-size-bytes enforcement", () => {
    it("fails when total file size exceeds max-size-bytes", async () => {
      writeStaging("large.bin", "x".repeat(1024));

      const results = await runHandler(buildConfig({ "max-size-bytes": 100 }), [{ type: "upload_artifact", path: "large.bin" }]);

      expect(results[0].success).toBe(false);
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("exceeds max-size-bytes limit"));
      expect(mockArtifactClient.uploadArtifact).not.toHaveBeenCalled();
    });

    it("succeeds when total file size is exactly at the max-size-bytes limit", async () => {
      writeStaging("exact.bin", "x".repeat(100));

      const results = await runHandler(buildConfig({ "max-size-bytes": 100 }), [{ type: "upload_artifact", path: "exact.bin" }]);

      expect(results[0].success).toBe(true);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockArtifactClient.uploadArtifact).toHaveBeenCalledOnce();
    });
  });

  describe("artifact name derivation", () => {
    it("uses custom name from message when provided", async () => {
      writeStaging("report.json");

      await runHandler(buildConfig(), [{ type: "upload_artifact", path: "report.json", name: "my-custom-name" }]);

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      const [name] = mockArtifactClient.uploadArtifact.mock.calls[0];
      expect(name).toBe("my-custom-name");
    });

    it("sanitises special characters in custom name", async () => {
      writeStaging("report.json");

      await runHandler(buildConfig(), [{ type: "upload_artifact", path: "report.json", name: "my report (v1.0)!" }]);

      const [name] = mockArtifactClient.uploadArtifact.mock.calls[0];
      expect(name).not.toContain(" ");
      expect(name).not.toContain("(");
      expect(name).not.toContain("!");
    });

    it("falls back to basename of path when no name given", async () => {
      writeStaging("sub/output.json");

      await runHandler(buildConfig(), [{ type: "upload_artifact", path: "sub/output.json" }]);

      const [name] = mockArtifactClient.uploadArtifact.mock.calls[0];
      expect(name).toBe("output.json");
    });

    it("falls back to slot index name when path basename is empty", async () => {
      // Use filter-based request (no path) so deriveArtifactName falls back to slot index.
      // Files must be in a subdirectory to match the **/*.json pattern.
      writeStaging("output/data.json");

      await runHandler(buildConfig(), [{ type: "upload_artifact", filters: { include: ["**/*.json"] } }]);

      const [name] = mockArtifactClient.uploadArtifact.mock.calls[0];
      expect(name).toBe("artifact-slot-0");
    });
  });

  describe("slot index outputs", () => {
    it("increments slot index across multiple successful uploads", async () => {
      writeStaging("a.json");
      writeStaging("b.json");

      const results = await runHandler(buildConfig({ "max-uploads": 2 }), [
        { type: "upload_artifact", path: "a.json" },
        { type: "upload_artifact", path: "b.json" },
      ]);

      expect(results[0].slotIndex).toBe(0);
      expect(results[1].slotIndex).toBe(1);
      expect(mockCore.setOutput).toHaveBeenCalledWith("slot_0_tmp_id", expect.any(String));
      expect(mockCore.setOutput).toHaveBeenCalledWith("slot_1_tmp_id", expect.any(String));
      expect(mockCore.setOutput).toHaveBeenCalledWith("upload_artifact_count", "2");
    });

    it("sets slot file_count and size_bytes outputs", async () => {
      writeStaging("data.json", "hello world");

      await runHandler(buildConfig(), [{ type: "upload_artifact", path: "data.json" }]);

      expect(mockCore.setOutput).toHaveBeenCalledWith("slot_0_file_count", "1");
      expect(mockCore.setOutput).toHaveBeenCalledWith("slot_0_size_bytes", expect.stringMatching(/^\d+$/));
    });
  });

  describe("auto-copy from outside staging directory", () => {
    const WORKSPACE_DIR = "/tmp/gh-aw-test-workspace";

    beforeEach(() => {
      if (fs.existsSync(WORKSPACE_DIR)) {
        fs.rmSync(WORKSPACE_DIR, { recursive: true });
      }
      fs.mkdirSync(WORKSPACE_DIR, { recursive: true });
      // Set GITHUB_WORKSPACE so auto-copy is allowed from WORKSPACE_DIR.
      process.env.GITHUB_WORKSPACE = WORKSPACE_DIR;
    });

    afterEach(() => {
      delete process.env.GITHUB_WORKSPACE;
      if (fs.existsSync(WORKSPACE_DIR)) {
        fs.rmSync(WORKSPACE_DIR, { recursive: true });
      }
    });

    /**
     * Write a file into the test workspace directory.
     * @param {string} relPath
     * @param {string} content
     */
    function writeWorkspace(relPath, content = "workspace content") {
      const fullPath = path.join(WORKSPACE_DIR, relPath);
      fs.mkdirSync(path.dirname(fullPath), { recursive: true });
      fs.writeFileSync(fullPath, content);
    }

    it("auto-copies a file from an absolute path", async () => {
      const absFile = path.join(WORKSPACE_DIR, "report.json");
      writeWorkspace("report.json", '{"ok":true}');

      const results = await runHandler(buildConfig(), [{ type: "upload_artifact", path: absFile }]);

      expect(results[0].success).toBe(true);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockArtifactClient.uploadArtifact).toHaveBeenCalledOnce();
      // The file should have been copied into the staging directory.
      expect(fs.existsSync(path.join(STAGING_DIR, "report.json"))).toBe(true);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Auto-copied file"));
    });

    it("auto-copies a directory from an absolute path", async () => {
      writeWorkspace("output/a.txt", "aaa");
      writeWorkspace("output/sub/b.txt", "bbb");
      const absDir = path.join(WORKSPACE_DIR, "output");

      const results = await runHandler(buildConfig(), [{ type: "upload_artifact", path: absDir }]);

      expect(results[0].success).toBe(true);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(fs.existsSync(path.join(STAGING_DIR, "output/a.txt"))).toBe(true);
      expect(fs.existsSync(path.join(STAGING_DIR, "output/sub/b.txt"))).toBe(true);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Auto-copied directory"));
    });

    it("auto-copies a relative path from GITHUB_WORKSPACE", async () => {
      process.env.GITHUB_WORKSPACE = WORKSPACE_DIR;
      writeWorkspace("data/results.csv", "col1,col2");

      const results = await runHandler(buildConfig(), [{ type: "upload_artifact", path: "data/results.csv" }]);

      expect(results[0].success).toBe(true);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(fs.existsSync(path.join(STAGING_DIR, "data/results.csv"))).toBe(true);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Auto-copied file"));
    });

    it("fails for an absolute path that does not exist", async () => {
      const absFile = path.join(WORKSPACE_DIR, "missing.json");

      await runHandler(buildConfig(), [{ type: "upload_artifact", path: absFile }]);

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("absolute path does not exist"));
    });

    it("still prefers files already in the staging directory", async () => {
      process.env.GITHUB_WORKSPACE = WORKSPACE_DIR;
      writeStaging("report.json", "staged version");
      writeWorkspace("report.json", "workspace version");

      const results = await runHandler(buildConfig(), [{ type: "upload_artifact", path: "report.json" }]);

      expect(results[0].success).toBe(true);
      // Verify the staged version was used (not overwritten by the workspace version).
      const content = fs.readFileSync(path.join(STAGING_DIR, "report.json"), "utf8");
      expect(content).toBe("staged version");
    });

    it("does not overwrite pre-staged file when auto-copying from absolute path", async () => {
      writeStaging("data.json", "original staged");
      writeWorkspace("data.json", "workspace version");
      const absFile = path.join(WORKSPACE_DIR, "data.json");

      const results = await runHandler(buildConfig(), [{ type: "upload_artifact", path: absFile }]);

      expect(results[0].success).toBe(true);
      // The pre-staged file must not be overwritten.
      const content = fs.readFileSync(path.join(STAGING_DIR, "data.json"), "utf8");
      expect(content).toBe("original staged");
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("already exists in staging"));
    });

    it("rejects symlinks during auto-copy from absolute path", async () => {
      writeWorkspace("real.txt", "real content");
      const linkPath = path.join(WORKSPACE_DIR, "link.txt");
      fs.symlinkSync(path.join(WORKSPACE_DIR, "real.txt"), linkPath);

      await runHandler(buildConfig(), [{ type: "upload_artifact", path: linkPath }]);

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("symlinks are not allowed"));
    });
  });

  describe("RUNNER_TEMP staging directory", () => {
    const CUSTOM_TEMP = "/tmp/gh-aw-test-runner-temp";
    const CUSTOM_STAGING = path.join(CUSTOM_TEMP, "gh-aw", "safeoutputs", "upload-artifacts");

    beforeEach(() => {
      // Set RUNNER_TEMP so the handler uses a custom staging dir
      process.env.RUNNER_TEMP = CUSTOM_TEMP;
      if (fs.existsSync(CUSTOM_STAGING)) {
        fs.rmSync(CUSTOM_STAGING, { recursive: true });
      }
      fs.mkdirSync(CUSTOM_STAGING, { recursive: true });
    });

    afterEach(() => {
      if (fs.existsSync(CUSTOM_STAGING)) {
        fs.rmSync(CUSTOM_STAGING, { recursive: true });
      }
    });

    it("uses RUNNER_TEMP as the staging directory base", async () => {
      const filePath = path.join(CUSTOM_STAGING, "custom-report.json");
      fs.writeFileSync(filePath, '{"ok": true}');

      const results = await runHandler(buildConfig(), [{ type: "upload_artifact", path: "custom-report.json" }]);

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(results[0].success).toBe(true);
      expect(mockArtifactClient.uploadArtifact).toHaveBeenCalledOnce();
      const [, files, rootDir] = mockArtifactClient.uploadArtifact.mock.calls[0];
      expect(files).toContain(path.join(CUSTOM_STAGING, "custom-report.json"));
      expect(rootDir).toBe(CUSTOM_STAGING + path.sep);
    });

    it("falls back to /tmp when RUNNER_TEMP is unset", async () => {
      // Clear RUNNER_TEMP to verify the fallback
      delete process.env.RUNNER_TEMP;

      writeStaging("fallback-report.json", '{"ok": true}');

      const results = await runHandler(buildConfig(), [{ type: "upload_artifact", path: "fallback-report.json" }]);

      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(results[0].success).toBe(true);
      const [, files, rootDir] = mockArtifactClient.uploadArtifact.mock.calls[0];
      expect(files).toContain(path.join(STAGING_DIR, "fallback-report.json"));
      expect(rootDir).toBe(STAGING_DIR);
    });
  });

  describe("path validation (security)", () => {
    const WORKSPACE_DIR = "/tmp/gh-aw-security-test-workspace";

    beforeEach(() => {
      if (fs.existsSync(WORKSPACE_DIR)) {
        fs.rmSync(WORKSPACE_DIR, { recursive: true });
      }
      fs.mkdirSync(WORKSPACE_DIR, { recursive: true });
      process.env.GITHUB_WORKSPACE = WORKSPACE_DIR;
    });

    afterEach(() => {
      delete process.env.GITHUB_WORKSPACE;
      if (fs.existsSync(WORKSPACE_DIR)) {
        fs.rmSync(WORKSPACE_DIR, { recursive: true });
      }
    });

    it("rejects an absolute path under /etc", async () => {
      if (!fs.existsSync("/etc/hosts")) return;
      const stat = (() => {
        try {
          return fs.lstatSync("/etc/hosts");
        } catch {
          return null;
        }
      })();
      if (!stat || stat.isSymbolicLink()) return;

      await runHandler(buildConfig(), [{ type: "upload_artifact", path: "/etc/hosts" }]);

      // macOS canonicalizes /etc to /private/etc, which the allowed-root check rejects.
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringMatching(/^ERR_VALIDATION: upload_artifact: path (refers to a system directory|is outside allowed source roots)/));
      expect(mockArtifactClient.uploadArtifact).not.toHaveBeenCalled();
    });

    it("rejects a path containing a .git directory component", async () => {
      const gitDir = path.join(WORKSPACE_DIR, ".git");
      const gitConfig = path.join(gitDir, "config");
      fs.mkdirSync(gitDir, { recursive: true });
      fs.writeFileSync(gitConfig, "[core]\n  repositoryformatversion = 0\n");

      await runHandler(buildConfig(), [{ type: "upload_artifact", path: gitConfig }]);

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("sensitive repository metadata"));
      expect(mockArtifactClient.uploadArtifact).not.toHaveBeenCalled();
    });

    it("rejects an absolute path outside allowed roots (GITHUB_WORKSPACE, staging)", async () => {
      const outsideDir = "/tmp/gh-aw-out-of-bounds-" + Math.random().toString(36).substring(7);
      try {
        fs.mkdirSync(outsideDir, { recursive: true });
        const outsideFile = path.join(outsideDir, "data.json");
        fs.writeFileSync(outsideFile, "{}");

        // Unset GITHUB_WORKSPACE so the outsideDir is not covered.
        delete process.env.GITHUB_WORKSPACE;

        await runHandler(buildConfig(), [{ type: "upload_artifact", path: outsideFile }]);

        expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("outside allowed source roots"));
        expect(mockArtifactClient.uploadArtifact).not.toHaveBeenCalled();
      } finally {
        try {
          fs.rmSync(outsideDir, { recursive: true, force: true });
        } catch {}
      }
    });

    it("rejects a nested .git directory during recursive auto-copy", async () => {
      const srcDir = path.join(WORKSPACE_DIR, "project");
      const gitDir = path.join(srcDir, ".git");
      fs.mkdirSync(gitDir, { recursive: true });
      fs.writeFileSync(path.join(gitDir, "config"), "[core]\n  repositoryformatversion = 0\n");
      fs.writeFileSync(path.join(srcDir, "readme.md"), "hello");

      await runHandler(buildConfig(), [{ type: "upload_artifact", path: srcDir }]);

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("sensitive repository metadata"));
      expect(mockArtifactClient.uploadArtifact).not.toHaveBeenCalled();
    });

    it("allows a path within GITHUB_WORKSPACE", async () => {
      const wsFile = path.join(WORKSPACE_DIR, "output.json");
      fs.writeFileSync(wsFile, '{"result":"ok"}');

      const results = await runHandler(buildConfig(), [{ type: "upload_artifact", path: wsFile }]);

      expect(results[0].success).toBe(true);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockArtifactClient.uploadArtifact).toHaveBeenCalledOnce();
    });

    it("sets restrictive permissions (0o600) on auto-copied staged files", async () => {
      const wsFile = path.join(WORKSPACE_DIR, "secure.json");
      fs.writeFileSync(wsFile, "secure data");

      await runHandler(buildConfig(), [{ type: "upload_artifact", path: wsFile }]);

      const stagedFile = path.join(STAGING_DIR, "secure.json");
      expect(fs.existsSync(stagedFile)).toBe(true);
      const stat = fs.statSync(stagedFile);
      expect(stat.mode & 0o777).toBe(0o600);
    });
  });
});
