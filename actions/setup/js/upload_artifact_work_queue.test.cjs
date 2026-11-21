// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";
const require = createRequire(import.meta.url);
const { claimArtifactPath, scopedArtifactFilename, withClaimExecution } = require("./work_queue_claim_scope.cjs");
const REPOSITORY = "owner/repo";
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
  const assignment = {
    dispatch_id: "d1",
    claims: Array.from({ length: count }, (_, index) => ({
      handle: `h${index + 1}`,
      claim_id: `c${index + 1}`,
      work_id: `w${index + 1}`,
      work: { effect_contract: { version: 1, outputs: [{ type: "upload_artifact", min: 0, max: 1 }] } },
      result_refs: [],
    })),
  };
  const fixture = { assignment };
  const finish = new Map();
  const authorize = async request => {
    const state = finish.get(request.claim_handle);
    if (state === "cancelled") return { claim_handle: request.claim_handle, authorized: false, state, suppressed: true };
    if (request.requireCompletion && state !== "completed") return { claim_handle: request.claim_handle, authorized: false, state: "failed", suppressed: true };
    return { claim_handle: request.claim_handle, authorized: true, state: "open" };
  };
  global.github = { rest: { repos: { get: vi.fn(async () => ({ data: { id: 7, full_name: REPOSITORY } })) } } };
  const effects = new Map(assignment.claims.map(member => [member.handle, []]));
  const execute = (handle, callback, scope = {}) => withClaimExecution({ assignment, claim_handle: handle, authorize, effects: effects.get(handle), ...scope }, callback);
  const complete = async () => {
    for (const member of assignment.claims) finish.set(member.handle, "completed");
  };
  const stage = async handle =>
    execute(handle, () => {
      const directory = claimArtifactPath(path.join(root, "gh-aw", "safeoutputs", "upload-artifacts"), handle);
      fs.mkdirSync(directory, { recursive: true });
      fs.writeFileSync(path.join(directory, "report.txt"), `${handle}-own`);
      return directory + path.sep;
    });
  return { fixture, finish, effects, execute, complete, stage };
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

  it("requires a completed finish intent before an implicit singleton upload and records the native artifact effect", async () => {
    const f = setup();
    const directory = await f.stage("h1");
    const handler = await f.execute("h1", () => runtime.main({}));
    const message = { type: "upload_artifact", path: "report.txt", name: "report", temporary_id: "aw_shared" };
    await expect(f.execute("h1", () => handler(message))).rejects.toThrow(/not completed/);
    expect(upload).not.toHaveBeenCalled();
    expect(f.effects.get("h1")).toEqual([]);
    await f.complete();
    const result = await f.execute("h1", () => handler(message));
    expect(result.success).toBe(true);
    expect(upload.mock.calls[0][2]).toBe(directory);
    expect(result.artifactName).toMatch(/^claim-[a-f0-9]{64}-report$/);
    expect(f.effects.get("h1")).toMatchObject([{ outcome: "succeeded", id: "101", claim_handle: "h1" }]);
  });

  it("authorizes Claim artifact uploads on the configured enterprise host", async () => {
    process.env.GITHUB_SERVER_URL = "https://contoso.example";
    const f = setup(1, "contoso.example");
    await f.stage("h1");
    await f.complete();
    const handler = await f.execute("h1", () => runtime.main({}));
    const result = await f.execute("h1", () => handler({ type: "upload_artifact", path: "report.txt", name: "report" }));
    expect(result.success).toBe(true);
    expect(f.effects.get("h1")).toMatchObject([{ outcome: "succeeded", claim_handle: "h1" }]);
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
    f.finish.set("h1", "cancelled");
    f.finish.set("h2", "completed");
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
      });
    }
    expect(upload).not.toHaveBeenCalled();
    expect([...f.effects.values()].flat()).toEqual([]);
    expect(global.core.summary.write).toHaveBeenCalledTimes(count);
  });
});
