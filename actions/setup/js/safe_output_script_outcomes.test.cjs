// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createManifestLogger, extractCreatedItemFromResult, writeSafeOutputErrorReport } from "./safe_output_manifest.cjs";
import { computeSafeOutputsStatus, logCreatedItemFromResult, processMessages } from "./safe_output_handler_manager.cjs";

describe("safe-output script outcome persistence", () => {
  let directory;
  let manifest;

  beforeEach(() => {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-script-outcomes-"));
    manifest = path.join(directory, "items.jsonl");
    vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn() });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
    fs.rmSync(directory, { recursive: true, force: true });
  });

  it.each(["https://api.example.com/messages/123", "slack://channel/C123/message/456", "urn:example:resource:789"])("persists handler-returned canonical URI %s and derived success", async url => {
    const log = createManifestLogger(manifest);
    const result = await processMessages(new Map([["post_message", async () => ({ success: true, url, status: "failure" })]]), [{ type: "post_message", url: "https://untrusted.example/request", status: "skipped" }], log);
    expect(result.results[0].success).toBe(true);
    expect(JSON.parse(fs.readFileSync(manifest, "utf8"))).toMatchObject({ type: "post_message", url, status: "success" });
  });

  it("preserves void-return compatibility without fabricating resource evidence from the request", async () => {
    const log = createManifestLogger(manifest);
    const result = await processMessages(new Map([["post_message", async () => {}]]), [{ type: "post_message", success: true, status: "success", url: "https://untrusted.example/request" }], log);
    expect(result.results[0].success).toBe(true);
    expect(fs.readFileSync(manifest, "utf8")).toBe("");
  });

  it("does not require a URI for an explicit successful result", () => {
    logCreatedItemFromResult(createManifestLogger(manifest), "post_message", { success: true });
    const entry = JSON.parse(fs.readFileSync(manifest, "utf8"));
    expect(entry.status).toBe("success");
    expect(entry.url).toBeUndefined();
  });

  it("keeps legacy identity-only results without inventing execution status", () => {
    logCreatedItemFromResult(createManifestLogger(manifest), "legacy_job", { number: 7, status: "success" });
    const entry = JSON.parse(fs.readFileSync(manifest, "utf8"));
    expect(entry.number).toBe(7);
    expect(entry.status).toBeUndefined();
  });

  it.each([
    { success: false },
    { success: true, skipped: true },
    { success: false, skipped: true },
    { success: true, deferred: true },
    { success: false, deferred: true },
    { success: true, cancelled: true },
    { success: false, cancelled: true },
    { success: true, staged: true },
    { success: true, deferred_manifest: true },
  ])("does not count non-applied result %j as a created resource", outcome => {
    logCreatedItemFromResult(createManifestLogger(manifest), "post_message", { ...outcome, status: "success", url: "urn:example:unapplied" });
    expect(fs.readFileSync(manifest, "utf8")).toBe("");
  });

  it("keeps failures in the error report and skips/deferred outcomes in processing status, not the resource manifest", async () => {
    const log = createManifestLogger(manifest);
    const result = await processMessages(
      new Map([
        ["failed_script", async () => ({ success: false, error: "API rejected operation", url: "urn:example:failed" })],
        ["skipped_script", async () => ({ success: true, skipped: true, reason: "Already applied", url: "urn:example:skipped" })],
        ["deferred_script", async () => ({ success: false, deferred: true, url: "urn:example:deferred" })],
      ]),
      [{ type: "failed_script" }, { type: "skipped_script" }, { type: "deferred_script" }],
      log
    );
    expect(computeSafeOutputsStatus(result.results)).toMatchObject({ itemsFailed: 1, itemsSkipped: 1, itemsDeferred: 1, itemsApplied: 0 });
    expect(fs.readFileSync(manifest, "utf8")).toBe("");
    const reportFile = path.join(directory, "errors.json");
    writeSafeOutputErrorReport({ failures: [{ type: "failed_script", error: result.results[0].error }] }, reportFile);
    expect(JSON.parse(fs.readFileSync(reportFile, "utf8")).failures).toEqual([{ type: "failed_script", error: "API rejected operation" }]);
  });

  it("does not record buffered review placeholders as successes", () => {
    expect(extractCreatedItemFromResult("submit_pull_request_review", { success: true })).toBeNull();
    expect(extractCreatedItemFromResult("create_pull_request_review_comment", { success: true, deferred_manifest: true })).toBeNull();
  });

  it("redacts secrets from canonical URIs while retaining the derived status", () => {
    vi.stubEnv("GH_AW_SECRET_NAMES", "SCRIPT_TOKEN");
    vi.stubEnv("SECRET_SCRIPT_TOKEN", "private-script-token");
    logCreatedItemFromResult(createManifestLogger(manifest), "post_message", { success: true, url: "custom://resource/private-script-token" });
    const content = fs.readFileSync(manifest, "utf8");
    expect(content).not.toContain("private-script-token");
    expect(JSON.parse(content).status).toBe("success");
  });
});
