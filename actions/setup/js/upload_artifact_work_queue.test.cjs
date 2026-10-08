// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";
const require = createRequire(import.meta.url);
const { authorizeWorkerClaim, reconcileWorkerClaim } = require("./finish_work_queue_claim.cjs");
const { claimArtifactPath, scopedArtifactFilename, withClaimExecution } = require("./work_queue_claim_scope.cjs");
const { queueFixture, REF, REPOSITORY, WORKFLOW } = require("./work_queue_lifecycle.test_helpers.cjs");
const { main: writeSnapshot } = require("./write_work_queue_snapshot.cjs");
let root;
let runtime;
let upload;
let originalEnv;
let originalGlobals;
const digest = "b".repeat(64);

beforeEach(() => {
  originalEnv = { ...process.env };
  originalGlobals = { core: global.core, context: global.context, github: global.github, __createArtifactClient: global.__createArtifactClient };
  root = fs.mkdtempSync(path.join(fs.realpathSync(process.cwd()), ".gh-aw-artifact-claims-"));
  Object.assign(process.env, { RUNNER_TEMP: root, GH_AW_ARTIFACT_RESOLVER_FILE: path.join(root, "resolver.json"), GITHUB_REPOSITORY: REPOSITORY, GITHUB_RUN_ID: "42", GITHUB_SERVER_URL: "https://github.com" });
  delete process.env.GH_AW_WORK_QUEUE_ENABLED;
  delete process.env.GH_AW_WORK_QUEUE_ROLE;
  delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
  global.core = { info: vi.fn(), warning: vi.fn(), setOutput: vi.fn(), summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue() } };
  upload = vi.fn().mockResolvedValue({ id: 101, size: 6, digest });
  global.__createArtifactClient = () => ({ uploadArtifact: upload });
  delete require.cache[require.resolve("./upload_artifact.cjs")];
  runtime = require("./upload_artifact.cjs");
});

afterEach(() => {
  process.env = originalEnv;
  Object.assign(global, originalGlobals);
  fs.rmSync(root, { recursive: true, force: true });
  delete require.cache[require.resolve("./upload_artifact.cjs")];
  vi.restoreAllMocks();
});

function setup(count = 1) {
  const fixture = queueFixture({
    count,
    bound: true,
    workDefaults: {
      payload: {
        effect_contract: { version: 1, outputs: [{ type: "upload_artifact", min: 0, max: 1 }] },
        resource_scope: { version: 1, resources: [{ repository: REPOSITORY, host: "github.com", repository_id: "7", run_id: "42" }] },
      },
    },
  });
  const options = {
    assignment: fixture.assignment,
    githubClient: fixture.githubClient,
    context: fixture.workerContext,
    workflowRef: `${REPOSITORY}/${WORKFLOW}@${REF}`,
    finishIntentPath: path.join(root, "finish.jsonl"),
    readWorkQueueLog: fixture.readWorkQueueLog,
    publishWorkQueueRequest: fixture.publishWorkQueueRequest,
  };
  global.context = fixture.workerContext;
  global.github = fixture.githubClient;
  const effects = new Map(fixture.assignment.claims.map(member => [member.handle, []]));
  const execute = (handle, callback, scope = {}) =>
    withClaimExecution({ assignment: fixture.assignment, claim_handle: handle, authorize: request => authorizeWorkerClaim({ ...options, ...request }), effects: effects.get(handle), ...scope }, callback);
  const complete = async () => {
    fs.writeFileSync(
      options.finishIntentPath,
      fixture.assignment.claims.map(member => JSON.stringify({ version: 3, kind: "finish", intent_id: `finish:${member.handle}`, parameters: { outcome: "completed" }, ...(count === 1 ? {} : { claim_handle: member.handle }) })).join("\n")
    );
    return reconcileWorkerClaim(options);
  };
  const stage = async handle =>
    execute(handle, () => {
      const directory = claimArtifactPath(path.join(root, "gh-aw", "safeoutputs", "upload-artifacts"), handle);
      fs.mkdirSync(directory, { recursive: true });
      fs.writeFileSync(path.join(directory, "report.txt"), `${handle}-own`);
      return directory + path.sep;
    });
  return { fixture, options, effects, execute, complete, stage };
}

describe("upload_artifact actual disabled/single/multi-Claim entrypoints", () => {
  it("retains legacy names, outputs, file roots and resolver IDs with queue disabled and stray inputs", async () => {
    process.env.GH_AW_WORK_QUEUE_ENABLED = "false";
    process.env.GH_AW_WORK_QUEUE_ROLE = "worker";
    global.context = { payload: { inputs: { work_queue_assignment: null } } };
    global.github = {
      rest: {
        repos: {
          get: vi.fn(() => {
            throw new Error("Disabled upload must not resolve queue authority");
          }),
        },
      },
    };
    const directory = path.join(root, "gh-aw", "safeoutputs", "upload-artifacts");
    fs.mkdirSync(directory, { recursive: true });
    fs.writeFileSync(path.join(directory, "report.txt"), "legacy");
    const handler = await runtime.main({});
    const result = await handler({ type: "upload_artifact", path: "report.txt", name: "legacy", temporary_id: "aw_shared", claim_handle: "ignored-ordinary-field" });
    expect(result).toMatchObject({ success: true, artifactName: "legacy", artifactId: 101, temporaryId: "aw_shared" });
    expect(upload).toHaveBeenCalledWith("legacy", [path.join(directory, "report.txt")], directory + path.sep, { retentionDays: 30 });
    expect(global.github.rest.repos.get).not.toHaveBeenCalled();
    expect(global.core.setOutput).toHaveBeenCalledWith("slot_0_tmp_id", "aw_shared");
    expect(global.core.setOutput).toHaveBeenCalledWith("upload_artifact_count", "1");
    expect(JSON.parse(fs.readFileSync(process.env.GH_AW_ARTIFACT_RESOLVER_FILE, "utf8"))).toEqual({ aw_shared: "legacy" });
  });

  it("keeps the disabled staging root cached at module load as on main", async () => {
    const directory = path.join(root, "gh-aw", "safeoutputs", "upload-artifacts");
    fs.mkdirSync(directory, { recursive: true });
    fs.writeFileSync(path.join(directory, "report.txt"), "legacy");
    process.env.RUNNER_TEMP = path.join(root, "later-environment");
    const handler = await runtime.main({});
    expect((await handler({ type: "upload_artifact", path: "report.txt" })).success).toBe(true);
    expect(upload.mock.calls[0][2]).toBe(directory + path.sep);
  });

  it("requires durable Completion before an implicit singleton upload and verifies the original native artifact", async () => {
    const f = setup();
    const directory = await f.stage("h1");
    const handler = await f.execute("h1", () => runtime.main({}));
    const message = { type: "upload_artifact", path: "report.txt", name: "report", temporary_id: "aw_shared" };
    await expect(f.execute("h1", () => handler(message))).rejects.toThrow(/Completion|authorized/);
    expect(upload).not.toHaveBeenCalled();
    expect(f.effects.get("h1")).toEqual([]);
    await f.complete();
    const result = await f.execute("h1", () => handler(message));
    expect(result.success).toBe(true);
    expect(upload.mock.calls[0][2]).toBe(directory);
    expect(result.artifactName).toMatch(/^claim-[a-f0-9]{64}-report$/);
    expect(f.effects.get("h1")).toMatchObject([{ outcome: "succeeded", id: "101", claim_handle: "h1" }]);
    const read = vi.fn().mockResolvedValue({ data: { id: 101, name: result.artifactName, expired: false, workflow_run: { id: 42 }, digest: `sha256:${digest}`, size_in_bytes: 6 } });
    const nativeUrl = result.artifactUrl;
    result.artifactUrl = "https://untrusted.invalid/replaced";
    const proof = await f.execute("h1", () => runtime.verifyArtifactDelivery({ claim: f.fixture.assignment.claims[0], result, github: { rest: { actions: { getArtifact: read } } } }), { closedEffectChannel: true });
    expect(proof).toMatchObject({ verified: true, claim_handle: "h1", resource: { target_run_id: "42", url: nativeUrl } });
    expect(read).toHaveBeenCalledWith({ owner: "owner", repo: "repo", artifact_id: 101 });
    expect((await f.execute("h1", () => runtime.verifyArtifactDelivery({ claim: f.fixture.assignment.claims[0], result: { ...result }, github: {} }))).verified).toBe(false);
  });

  it("isolates explicit multi-Claim file roots, resolver IDs and failures without narrowing the original assignment", async () => {
    const f = setup(2);
    const roots = [await f.stage("h1"), await f.stage("h2")];
    await f.complete();
    const handlers = await Promise.all(["h1", "h2"].map(handle => f.execute(handle, () => runtime.main({}))));
    await expect(f.execute("h1", () => handlers[0]({ type: "upload_artifact", path: "report.txt" }))).rejects.toThrow(/claim_handle is required/);
    upload.mockRejectedValueOnce(new Error("independent upload failure"));
    const first = await f.execute("h1", () => handlers[0]({ type: "upload_artifact", claim_handle: "h1", path: "report.txt", name: "failed", temporary_id: "aw_shared" }));
    expect(first).toMatchObject({ success: false, error: expect.stringContaining("independent upload failure") });
    const second = await f.execute("h2", () => handlers[1]({ type: "upload_artifact", claim_handle: "h2", path: "report.txt", name: "second", temporary_id: "aw_shared" }));
    expect(second.success).toBe(true);
    const retry = await f.execute("h1", () => handlers[0]({ type: "upload_artifact", claim_handle: "h1", path: "report.txt", name: "retry", temporary_id: "aw_shared" }));
    expect(retry.success).toBe(true);
    expect(upload.mock.calls.map(call => call[2])).toEqual([roots[0], roots[1], roots[0]]);
    expect(f.effects.get("h1").map(effect => effect.outcome)).toEqual(["unknown", "succeeded"]);
    expect(f.effects.get("h2").map(effect => effect.outcome)).toEqual(["succeeded"]);
    for (const [index, handle] of ["h1", "h2"].entries()) {
      const resolver = await f.execute(handle, () => scopedArtifactFilename(process.env.GH_AW_ARTIFACT_RESOLVER_FILE));
      expect(JSON.parse(fs.readFileSync(resolver, "utf8"))).toEqual({ aw_shared: index ? second.artifactName : retry.artifactName });
    }
    expect(fs.existsSync(process.env.GH_AW_ARTIFACT_RESOLVER_FILE)).toBe(false);
    expect(global.core.setOutput.mock.calls.map(([name]) => name)).not.toContain("upload_artifact_count");
    expect((await f.execute("h2", () => runtime.verifyArtifactDelivery({ claim: f.fixture.assignment.claims[1], result: retry, github: {} }))).verified).toBe(false);
  });

  it.each([null, undefined, "", "foreign"])("rejects a supplied invalid singleton selector %j before touching files or effects", async claim_handle => {
    const f = setup();
    await f.stage("h1");
    await f.complete();
    const handler = await f.execute("h1", () => runtime.main({}));
    await expect(f.execute("h1", () => handler({ type: "upload_artifact", claim_handle, path: "report.txt" }))).rejects.toThrow();
    expect(upload).not.toHaveBeenCalled();
    expect(f.effects.get("h1")).toEqual([]);
  });

  it("denies foreign immutable member selectors, reused factories and closed write channels", async () => {
    const f = setup(2);
    await f.stage("h1");
    await f.stage("h2");
    await f.complete();
    const ordinary = await runtime.main({});
    const scoped = await f.execute("h1", () => runtime.main({}));
    const message = { type: "upload_artifact", claim_handle: "h1", path: "report.txt" };
    await expect(f.execute("h1", () => ordinary(message))).rejects.toThrow(/original Claim/);
    await expect(f.execute("h2", () => scoped({ ...message, claim_handle: "h2" }))).rejects.toThrow(/original Claim/);
    await expect(f.execute("h1", () => scoped({ ...message, work_id: f.fixture.assignment.claims[1].work_id }))).rejects.toThrow(/work_id conflicts/);
    expect((await f.execute("h1", () => scoped(message), { closedEffectChannel: true })).success).toBe(false);
    expect(upload).not.toHaveBeenCalled();
    expect(f.effects.get("h1")).toEqual([]);
  });

  it.each([
    { count: 1, enabled: "false", role: "invalid" },
    { count: 2, enabled: "false", role: "invalid" },
    { count: 1, enabled: "true", role: "observer" },
    { count: 2, enabled: "true", role: "observer" },
  ])("preserves the private $count-Claim frame after ambient ENABLED=$enabled ROLE=$role changes", async ({ count, enabled, role }) => {
    const f = setup(count);
    const directory = await f.stage("h1");
    await f.complete();
    Object.assign(process.env, { GH_AW_WORK_QUEUE_ENABLED: "true", GH_AW_WORK_QUEUE_ROLE: "worker", GH_AW_WORK_QUEUE_SNAPSHOT: path.join(root, "absent-ambient-snapshot.json") });
    let handler;
    await f.execute("h1", async () => {
      handler = await runtime.main({});
      process.env.GH_AW_WORK_QUEUE_ENABLED = enabled;
      process.env.GH_AW_WORK_QUEUE_ROLE = role;
      for (const claim_handle of count === 1 ? ["foreign"] : ["h2", "foreign"]) {
        await expect(handler({ type: "upload_artifact", claim_handle, path: "report.txt" })).rejects.toThrow();
      }
      if (count === 2) await expect(handler({ type: "upload_artifact", path: "report.txt" })).rejects.toThrow(/claim_handle is required/);
      expect(upload).not.toHaveBeenCalled();
      expect((await handler({ type: "upload_artifact", ...(count === 1 ? {} : { claim_handle: "h1" }), path: "report.txt" })).success).toBe(true);
    });
    expect(upload).toHaveBeenCalledOnce();
    expect(upload.mock.calls[0][2]).toBe(directory);
    expect(f.effects.get("h1")).toMatchObject([{ outcome: "succeeded", claim_handle: "h1" }]);
    if (count === 2) expect(f.effects.get("h2")).toEqual([]);
    expect(global.core.setOutput.mock.calls.map(([name]) => name)).not.toContain("upload_artifact_count");
    expect(fs.existsSync(process.env.GH_AW_ARTIFACT_RESOLVER_FILE)).toBe(false);
    const deny = vi.fn().mockResolvedValue({ authorized: false });
    await expect(f.execute("h1", () => handler({ type: "upload_artifact", ...(count === 1 ? {} : { claim_handle: "h1" }), path: "report.txt" }), { authorize: deny })).rejects.toThrow(/authorized|authorization|denied/);
    expect(deny).toHaveBeenCalledOnce();
    expect(upload).toHaveBeenCalledOnce();
    expect(f.effects.get("h1")).toHaveLength(1);
  });

  it("suppresses a cancelled member at the actual upload entrypoint without consuming its sibling's upload budget", async () => {
    const f = setup(2);
    await f.stage("h1");
    await f.stage("h2");
    fs.writeFileSync(
      f.options.finishIntentPath,
      f.fixture.assignment.claims.map((member, i) => JSON.stringify({ version: 3, kind: "finish", intent_id: `finish:${member.handle}`, claim_handle: member.handle, parameters: { outcome: i ? "completed" : "cancelled" } })).join("\n")
    );
    await reconcileWorkerClaim(f.options);
    const first = await f.execute("h1", () => runtime.main({}));
    await expect(f.execute("h1", () => first({ type: "upload_artifact", claim_handle: "h1", path: "report.txt" }))).rejects.toMatchObject({ code: "claim_cancelled", suppressed: true });
    const second = await f.execute("h2", () => runtime.main({}));
    expect((await f.execute("h2", () => second({ type: "upload_artifact", claim_handle: "h2", path: "report.txt" }))).success).toBe(true);
    expect(upload).toHaveBeenCalledOnce();
    expect(f.effects.get("h1")).toEqual([]);
  });

  it.each(["claim-root", "nested-parent"])("rejects a restored %s alias into a sibling's artifact namespace before uploading", async alias => {
    const f = setup(2);
    const firstRoot = await f.stage("h1");
    const siblingRoot = await f.stage("h2");
    await f.complete();
    const handler = await f.execute("h1", () => runtime.main({}));
    let filename = "report.txt";
    if (alias === "claim-root") {
      fs.rmSync(firstRoot, { recursive: true });
      fs.symlinkSync(siblingRoot, path.resolve(firstRoot), "dir");
    } else {
      fs.symlinkSync(siblingRoot, path.join(firstRoot, "restored-alias"), "dir");
      filename = "restored-alias/report.txt";
    }
    await expect(f.execute("h1", () => handler({ type: "upload_artifact", claim_handle: "h1", path: filename }))).rejects.toThrow(/redirects|escapes/);
    expect(upload).not.toHaveBeenCalled();
    expect(f.effects.get("h1")).toEqual([]);
    expect(fs.readFileSync(path.join(siblingRoot, "report.txt"), "utf8")).toBe("h2-own");
  });

  it.each(["id", "name", "expired", "workflow_run", "digest", "size_in_bytes"])("does not verify a native artifact with changed %s readback", async field => {
    const f = setup();
    await f.stage("h1");
    await f.complete();
    const result = await f.execute("h1", async () => {
      const handler = await runtime.main({});
      return handler({ type: "upload_artifact", path: "report.txt" });
    });
    expect(result.success).toBe(true);
    const data = { id: 101, name: result.artifactName, expired: false, workflow_run: { id: 42 }, digest: `sha256:${digest}`, size_in_bytes: 6 };
    data[field] = { id: 102, name: "foreign-name", expired: true, workflow_run: { id: 43 }, digest: `sha256:${"c".repeat(64)}`, size_in_bytes: 7 }[field];
    const read = vi.fn().mockResolvedValue({ data });
    const proof = await f.execute("h1", () => runtime.verifyArtifactDelivery({ claim: f.fixture.assignment.claims[0], result, github: { rest: { actions: { getArtifact: read } } } }), { closedEffectChannel: true });
    expect(proof).toEqual({ verified: false });
    expect(read).toHaveBeenCalledOnce();
  });

  it.each([1, 2])("does not upload through an unscoped factory when a trusted %i-Claim worker snapshot exists", async count => {
    const f = setup(count);
    await f.complete();
    process.env.GH_AW_WORK_QUEUE_ENABLED = "true";
    process.env.GH_AW_WORK_QUEUE_ROLE = "worker";
    process.env.GH_AW_WORK_QUEUE_SNAPSHOT = path.join(root, "snapshot.json");
    await writeSnapshot({ ...f.options, snapshotPath: process.env.GH_AW_WORK_QUEUE_SNAPSHOT, role: "worker", core: global.core });
    await expect(runtime.main({})).rejects.toThrow(/original per-Claim execution context/);
    expect(upload).not.toHaveBeenCalled();
    expect([...f.effects.values()].flat()).toEqual([]);
  });

  it.each([1, 2])("previews %i completed Claims without artifacts or trusted delivery receipts, and handles empty selections", async count => {
    const f = setup(count);
    for (const member of f.fixture.assignment.claims) await f.stage(member.handle);
    await f.complete();
    for (const member of f.fixture.assignment.claims) {
      await f.execute(member.handle, async () => {
        const handler = await runtime.main({ staged: true, "default-if-no-files": "ignore" });
        const selector = count === 1 ? {} : { claim_handle: member.handle };
        expect(await handler({ type: "upload_artifact", ...selector, filters: { include: ["missing-*"] } })).toMatchObject({ success: false, skipped: true });
        const result = await handler({ type: "upload_artifact", ...selector, path: "report.txt" });
        expect(result).toMatchObject({ success: true, slotIndex: 0 });
        expect((await runtime.verifyArtifactDelivery({ claim: member, result, github: {} })).verified).toBe(false);
      });
    }
    expect(upload).not.toHaveBeenCalled();
    expect([...f.effects.values()].flat()).toEqual([]);
    expect(global.core.summary.write).toHaveBeenCalledTimes(count);
  });
});
