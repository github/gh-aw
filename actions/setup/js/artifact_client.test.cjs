// @ts-check
import fs from "fs";
import os from "os";
import path from "path";
import childProcess from "child_process";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "module";

const req = createRequire(import.meta.url);
const { DefaultArtifactClient } = req("./artifact_client.cjs");
const { createAPIBudget, getAPIReadFailure } = req("./daily_aic_api_budget.cjs");

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

function makeFetchResponse(status, body, headers = {}) {
  const headersObj = new Headers(headers);
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: String(status),
    headers: headersObj,
    json: async () => (typeof body === "string" ? JSON.parse(body) : body),
    text: async () => (typeof body === "string" ? body : JSON.stringify(body)),
    body: null,
  };
}

// Utility: build a fake JWT with Actions.Results scope
function buildFakeToken(backendIds) {
  const payload = { scp: `Actions.Results:${backendIds}` };
  const encoded = Buffer.from(JSON.stringify(payload)).toString("base64url");
  return `header.${encoded}.sig`;
}

// ──────────────────────────────────────────────────────────────────────────────
// listArtifacts
// ──────────────────────────────────────────────────────────────────────────────

describe("DefaultArtifactClient.listArtifacts", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
  });

  it("throws when findBy options are missing", async () => {
    const client = new DefaultArtifactClient();
    await expect(client.listArtifacts({})).rejects.toThrow("listArtifacts requires");
  });

  it("returns artifacts from the REST API", async () => {
    const artifacts = [
      { id: 1, name: "my-artifact", size_in_bytes: 100, created_at: "2024-01-01T00:00:00Z" },
      { id: 2, name: "other-artifact", size_in_bytes: 200 },
    ];
    const mockFetch = vi.fn().mockResolvedValue(makeFetchResponse(200, { artifacts, total_count: 2 }));
    vi.stubGlobal("fetch", mockFetch);

    const client = new DefaultArtifactClient();
    const result = await client.listArtifacts({
      findBy: { token: "tok", repositoryOwner: "owner", repositoryName: "repo", workflowRunId: "42" },
    });

    expect(result.artifacts).toHaveLength(2);
    expect(result.artifacts[0].id).toBe(1);
    expect(result.artifacts[0].name).toBe("my-artifact");
    expect(result.artifacts[0].size).toBe(100);
    expect(result.artifacts[0].createdAt).toBeInstanceOf(Date);
  });

  it("returns only the latest artifact per name when latest:true", async () => {
    const artifacts = [
      { id: 1, name: "my-artifact", size_in_bytes: 100 },
      { id: 3, name: "my-artifact", size_in_bytes: 150 },
      { id: 2, name: "other", size_in_bytes: 50 },
    ];
    const mockFetch = vi.fn().mockResolvedValue(makeFetchResponse(200, { artifacts, total_count: 3 }));
    vi.stubGlobal("fetch", mockFetch);

    const client = new DefaultArtifactClient();
    const result = await client.listArtifacts({
      latest: true,
      findBy: { token: "tok", repositoryOwner: "owner", repositoryName: "repo", workflowRunId: "42" },
    });

    expect(result.artifacts).toHaveLength(2);
    // highest id wins for each name
    expect(result.artifacts.find(a => a.name === "my-artifact").id).toBe(3);
  });

  it("throws when the REST API returns a non-ok status", async () => {
    const budget = createAPIBudget();
    const mockFetch = vi.fn().mockResolvedValue(makeFetchResponse(403, "private payload"));
    vi.stubGlobal("fetch", mockFetch);
    vi.stubGlobal("core", { info: vi.fn() });

    const client = new DefaultArtifactClient({ request: budget.request });
    const error = await client
      .listArtifacts({
        findBy: { token: "tok", repositoryOwner: "owner", repositoryName: "repo", workflowRunId: "42" },
      })
      .catch(error => error);
    expect(error).toMatchObject({ message: "failed to list artifacts (403)" });
    expect(getAPIReadFailure(error)).toEqual({ attempts: 1, status: 403, reason: "non_transient", requests: 1, operation: "artifact_list" });
    expect(error.message).not.toContain("private payload");
  });

  it("observes quota headers even when the JSON body cannot be parsed", async () => {
    const response = makeFetchResponse(200, "invalid JSON", { "x-ratelimit-remaining": "100" });
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
    vi.stubGlobal("core", { info: vi.fn() });
    const budget = createAPIBudget();
    const client = new DefaultArtifactClient({ request: budget.request });
    await expect(client.listArtifacts({ findBy: { token: "tok", repositoryOwner: "owner", repositoryName: "repo", workflowRunId: "42" } })).rejects.toMatchObject({ code: "DAILY_AIC_QUOTA_STOP" });
    expect(budget.snapshot().requests).toBe(1);
  });

  it("does not retry malformed JSON when the observed response is above the quota reserve", async () => {
    const fetch = vi.fn().mockResolvedValue(makeFetchResponse(200, "invalid JSON", { "x-ratelimit-remaining": "4000" }));
    vi.stubGlobal("fetch", fetch);
    vi.stubGlobal("core", { info: vi.fn() });
    const budget = createAPIBudget();
    const client = new DefaultArtifactClient({ request: budget.request });
    await expect(client.listArtifacts({ findBy: { token: "tok", repositoryOwner: "owner", repositoryName: "repo", workflowRunId: "42" } })).rejects.toThrow("failed to parse list artifacts");
    expect(fetch).toHaveBeenCalledOnce();
    expect(budget.snapshot().requests).toBe(1);
  });

  it("attributes artifact-list failures through the budget, not through fetch arguments", async () => {
    const response = makeFetchResponse(429, "private payload");
    const fetch = vi.fn().mockResolvedValue(response);
    vi.stubGlobal("fetch", fetch);
    vi.stubGlobal("core", { info: vi.fn() });
    const client = new DefaultArtifactClient({ request: createAPIBudget().request });
    const error = await client.listArtifacts({ findBy: { token: "tok", repositoryOwner: "owner", repositoryName: "repo", workflowRunId: "42" } }).catch(error => error);
    expect(getAPIReadFailure(error)).toEqual({ attempts: 1, status: 429, reason: "quota_stop", requests: 1, operation: "artifact_list" });
    expect(fetch).toHaveBeenCalledExactlyOnceWith(expect.any(String), expect.objectContaining({ signal: expect.any(AbortSignal) }));
  });
});

// ──────────────────────────────────────────────────────────────────────────────
// downloadArtifact
// ──────────────────────────────────────────────────────────────────────────────

describe("DefaultArtifactClient.downloadArtifact", () => {
  let tmpDir;

  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-artifact-test-"));
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  it("throws when findBy options are missing", async () => {
    const client = new DefaultArtifactClient();
    await expect(client.downloadArtifact(1, {})).rejects.toThrow("downloadArtifact requires");
  });

  it("throws when redirect response is not a 3xx status", async () => {
    const mockFetch = vi.fn().mockResolvedValue(makeFetchResponse(200, "OK"));
    vi.stubGlobal("fetch", mockFetch);

    const client = new DefaultArtifactClient();
    await expect(
      client.downloadArtifact(1, {
        path: tmpDir,
        findBy: { token: "tok", repositoryOwner: "owner", repositoryName: "repo" },
      })
    ).rejects.toThrow("unexpected status 200");
  });

  it("accepts all standard redirect status codes (301, 302, 303, 307, 308)", async () => {
    for (const status of [301, 302, 303, 307, 308]) {
      const mockFetch = vi
        .fn()
        .mockResolvedValueOnce(
          makeFetchResponse(status, "", {
            location: "https://storage.example.com/artifact.bin",
          })
        )
        .mockResolvedValueOnce({
          ok: true,
          status: 200,
          headers: new Headers({ "content-type": "application/octet-stream", "content-disposition": 'attachment; filename="artifact.bin"' }),
          body: null,
          text: async () => "fake content",
        });

      vi.stubGlobal("fetch", mockFetch);

      const client = new DefaultArtifactClient();
      // We only check that we do NOT throw "unexpected status <code>" for redirect codes.
      // Subsequent stream/pipeline steps may fail; that is acceptable for this assertion.
      try {
        await client.downloadArtifact(1, {
          path: tmpDir,
          skipDecompress: true,
          findBy: { token: "tok", repositoryOwner: "owner", repositoryName: "repo" },
        });
      } catch (err) {
        expect(String(err)).not.toContain(`unexpected status ${status}`);
      }
    }
  });

  it("throws when the redirect location header is missing", async () => {
    const mockFetch = vi.fn().mockResolvedValue(makeFetchResponse(302, ""));
    vi.stubGlobal("fetch", mockFetch);

    const client = new DefaultArtifactClient();
    await expect(
      client.downloadArtifact(1, {
        path: tmpDir,
        findBy: { token: "tok", repositoryOwner: "owner", repositoryName: "repo" },
      })
    ).rejects.toThrow("missing redirect location");
  });

  it.each([
    ["redirect", 403, "artifact_redirect", 1],
    ["blob download", 403, "artifact_download", 1],
  ])("preserves %s failure attribution through the wrapper", async (failure, status, operation, requests) => {
    const budget = createAPIBudget();
    const mockFetch =
      failure === "redirect"
        ? vi.fn().mockResolvedValue(makeFetchResponse(status, "private payload"))
        : vi
            .fn()
            .mockResolvedValueOnce(makeFetchResponse(302, "", { location: "https://storage.example.com/artifact.zip" }))
            .mockResolvedValueOnce(makeFetchResponse(status, "private payload"));
    vi.stubGlobal("fetch", mockFetch);
    vi.stubGlobal("core", { info: vi.fn() });

    const client = new DefaultArtifactClient({ request: budget.request });
    const error = await client
      .downloadArtifact(1, {
        path: tmpDir,
        findBy: { token: "tok", repositoryOwner: "owner", repositoryName: "repo" },
      })
      .catch(error => error);

    expect(getAPIReadFailure(error)).toEqual({ attempts: 1, status, reason: "non_transient", requests, operation });
    expect(error.message).not.toContain("private payload");
  });
});

// ──────────────────────────────────────────────────────────────────────────────
// uploadArtifact
// ──────────────────────────────────────────────────────────────────────────────

describe("DefaultArtifactClient.uploadArtifact", () => {
  let tmpDir;

  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-artifact-upload-test-"));
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
    vi.restoreAllMocks();
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  it("throws when no files are provided", async () => {
    const client = new DefaultArtifactClient();
    await expect(client.uploadArtifact("art", [], tmpDir)).rejects.toThrow("at least one file");
  });

  it("throws when skipArchive is used with multiple files", async () => {
    const client = new DefaultArtifactClient();
    await expect(client.uploadArtifact("art", ["/a", "/b"], tmpDir, { skipArchive: true })).rejects.toThrow("single file");
  });

  it("rejects leading-dash staged filenames before invoking zip", async () => {
    const filePath = path.join(tmpDir, "-T");
    fs.writeFileSync(filePath, "test content");
    const spawnSyncSpy = vi.spyOn(childProcess, "spawnSync");

    const client = new DefaultArtifactClient();
    await expect(client.uploadArtifact("artifact", [filePath], tmpDir)).rejects.toThrow("leading-dash filenames");

    expect(spawnSyncSpy).not.toHaveBeenCalled();
  });

  it("sends camelCase field names and mimeType as a plain string in CreateArtifact request", async () => {
    const filePath = path.join(tmpDir, "test.bin");
    fs.writeFileSync(filePath, "test content");

    /** @type {Record<string, any>|null} */
    let capturedCreateBody = null;
    /** @type {Record<string, any>|null} */
    let capturedFinalizeBody = null;

    const mockFetch = vi.fn().mockImplementation(async (url, opts) => {
      const u = String(url);
      if (u.includes("CreateArtifact")) {
        capturedCreateBody = JSON.parse(opts?.body || "{}");
        return makeFetchResponse(200, { ok: true, signedUploadUrl: "https://storage.example.com/upload" });
      }
      if (u.includes("FinalizeArtifact")) {
        capturedFinalizeBody = JSON.parse(opts?.body || "{}");
        return makeFetchResponse(200, { ok: true, artifactId: "42" });
      }
      return makeFetchResponse(200, "");
    });
    vi.stubGlobal("fetch", mockFetch);

    vi.stubEnv("ACTIONS_RUNTIME_TOKEN", buildFakeToken("runId:jobId"));
    vi.stubEnv("ACTIONS_RESULTS_URL", "https://results.example.com");

    const client = new DefaultArtifactClient();
    await client.uploadArtifact("my-artifact", [filePath], tmpDir, { skipArchive: true, retentionDays: 30 });

    // CreateArtifact request must use camelCase field names
    expect(capturedCreateBody).not.toBeNull();
    expect(capturedCreateBody).toHaveProperty("workflowRunBackendId");
    expect(capturedCreateBody).toHaveProperty("workflowJobRunBackendId");
    expect(capturedCreateBody).not.toHaveProperty("workflow_run_backend_id");
    expect(capturedCreateBody).not.toHaveProperty("workflow_job_run_backend_id");

    // mimeType must be a plain string, not a wrapped object
    expect(typeof capturedCreateBody?.mimeType).toBe("string");
    expect(capturedCreateBody?.mimeType).toBe("application/octet-stream");
    expect(capturedCreateBody).not.toHaveProperty("mime_type");

    // expiresAt must use camelCase
    expect(capturedCreateBody).toHaveProperty("expiresAt");
    expect(capturedCreateBody).not.toHaveProperty("expires_at");

    // FinalizeArtifact request must use camelCase field names
    expect(capturedFinalizeBody).not.toBeNull();
    expect(capturedFinalizeBody).toHaveProperty("workflowRunBackendId");
    expect(capturedFinalizeBody).toHaveProperty("workflowJobRunBackendId");
    expect(capturedFinalizeBody).not.toHaveProperty("workflow_run_backend_id");

    // hash must be a plain string (not a wrapped object)
    expect(typeof capturedFinalizeBody?.hash).toBe("string");
    expect(capturedFinalizeBody?.hash).toMatch(/^sha256:/);
  });

  it("preserves caller-provided artifact name when skipArchive is true", async () => {
    const filePath = path.join(tmpDir, "output.bin");
    fs.writeFileSync(filePath, "hello artifact");

    const createResp = { ok: true, signedUploadUrl: "https://storage.example.com/upload" };
    const finalizeResp = { ok: true, artifactId: "99" };

    let uploadedArtifactName;
    const mockFetch = vi.fn().mockImplementation(async (url, opts) => {
      const u = String(url);
      if (u.includes("CreateArtifact")) {
        const body = JSON.parse(opts?.body || "{}");
        uploadedArtifactName = body.name;
        return makeFetchResponse(200, createResp);
      }
      if (u.includes("FinalizeArtifact")) {
        return makeFetchResponse(200, finalizeResp);
      }
      // signed upload URL
      return makeFetchResponse(200, "");
    });
    vi.stubGlobal("fetch", mockFetch);

    vi.stubEnv("ACTIONS_RUNTIME_TOKEN", buildFakeToken("runId:jobId"));
    vi.stubEnv("ACTIONS_RESULTS_URL", "https://results.example.com");

    const client = new DefaultArtifactClient();
    await client.uploadArtifact("my-custom-name", [filePath], tmpDir, { skipArchive: true });

    expect(uploadedArtifactName).toBe("my-custom-name");
  });

  it("uses caller-provided artifact name (not basename) for skipArchive", async () => {
    const filePath = path.join(tmpDir, "deeply-nested-file.bin");
    fs.writeFileSync(filePath, "content");

    let capturedName;
    const mockFetch = vi.fn().mockImplementation(async (url, opts) => {
      if (String(url).includes("CreateArtifact")) {
        capturedName = JSON.parse(opts?.body || "{}").name;
        return makeFetchResponse(200, { ok: true, signedUploadUrl: "https://example.com/upload" });
      }
      if (String(url).includes("FinalizeArtifact")) {
        return makeFetchResponse(200, { ok: true, artifactId: "1" });
      }
      return makeFetchResponse(200, "");
    });
    vi.stubGlobal("fetch", mockFetch);

    vi.stubEnv("ACTIONS_RUNTIME_TOKEN", buildFakeToken("runId:jobId"));
    vi.stubEnv("ACTIONS_RESULTS_URL", "https://results.example.com");

    const client = new DefaultArtifactClient();
    await client.uploadArtifact("explicit-name", [filePath], tmpDir, { skipArchive: true });

    // Must NOT fall back to path.basename(filePath)
    expect(capturedName).toBe("explicit-name");
    expect(capturedName).not.toBe("deeply-nested-file.bin");
  });

  it("uses zip-based archive name when skipArchive is false", async () => {
    const filePath = path.join(tmpDir, "data.txt");
    fs.writeFileSync(filePath, "data content");

    // Mock spawnSync (zip) to succeed without actually creating a zip
    const zipOutputPath = path.join(tmpDir, "archive-name.zip");
    fs.writeFileSync(zipOutputPath, "fake zip data");
    const spawnSyncSpy = vi.spyOn(childProcess, "spawnSync").mockReturnValue({
      status: 0,
      stdout: "",
      stderr: "",
      pid: 1,
      output: [],
      signal: null,
    });
    vi.stubEnv("GH_AW_ARTIFACT_ARCHIVE_PROBE_TIMEOUT_MS", "12345");
    vi.stubEnv("GH_AW_ARTIFACT_ARCHIVE_TIMEOUT_MS", "67890");
    const artifactClientPath = req.resolve("./artifact_client.cjs");
    delete req.cache[artifactClientPath];
    const { DefaultArtifactClient: FreshArtifactClient } = req("./artifact_client.cjs");

    let capturedName;
    const mockFetch = vi.fn().mockImplementation(async (url, opts) => {
      if (String(url).includes("CreateArtifact")) {
        capturedName = JSON.parse(opts?.body || "{}").name;
        return makeFetchResponse(200, { ok: true, signedUploadUrl: "https://example.com/upload" });
      }
      if (String(url).includes("FinalizeArtifact")) {
        return makeFetchResponse(200, { ok: true, artifactId: "1" });
      }
      return makeFetchResponse(200, "");
    });
    vi.stubGlobal("fetch", mockFetch);

    vi.stubEnv("ACTIONS_RUNTIME_TOKEN", buildFakeToken("runId:jobId"));
    vi.stubEnv("ACTIONS_RESULTS_URL", "https://results.example.com");

    const client = new FreshArtifactClient();
    // Will fail at zip creation (no real zip output), but we still capture the name from CreateArtifact
    try {
      await client.uploadArtifact("archive-name", [filePath], tmpDir);
    } catch {
      // uploadFileToSignedURL will fail without a real zip on disk — that is expected
    }

    expect(capturedName).toBe("archive-name");
    expect(spawnSyncSpy).toHaveBeenNthCalledWith(1, "zip", ["-v"], expect.objectContaining({ timeout: 12_345 }));
    expect(spawnSyncSpy).toHaveBeenNthCalledWith(2, "zip", ["-q", "-r", expect.any(String), "./data.txt"], expect.objectContaining({ timeout: 67_890 }));
    spawnSyncSpy.mockRestore();
    delete req.cache[artifactClientPath];
  });
});
