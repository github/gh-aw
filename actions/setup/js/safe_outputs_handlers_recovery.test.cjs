import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "module";
import crypto from "crypto";
import fs from "fs";
import path from "path";

const require = createRequire(import.meta.url);
const patchModule = require("./generate_git_patch.cjs");
const bundleModule = require("./generate_git_bundle.cjs");
const packModule = require("./object_pack_transport.cjs");
const gitModule = require("./git_helpers.cjs");
const baseModule = require("./get_base_branch.cjs");
const handlerPath = require.resolve("./safe_outputs_handlers.cjs");
const repoSlug = "test-owner/recovery";
const tip = "a".repeat(40);
const checkoutGuidance = "No local base ref. Configure checkout.fetch and checkout.fetch-depth before agent execution.";

describe("safe-output handler recovery errors", () => {
  let root, originalEnv, originalCore, originalContext, cachedHandler;
  let createHandlers, append, server, generatePatch, generateBundle, createPack, trace;

  beforeEach(() => {
    originalEnv = { ...process.env };
    originalCore = global.core;
    originalContext = global.context;
    cachedHandler = require.cache[handlerPath];
    root = path.resolve(`.handler-recovery-${crypto.randomUUID()}`);
    fs.mkdirSync(root);
    process.env.GITHUB_WORKSPACE = root;
    process.env.GITHUB_REPOSITORY = repoSlug;
    delete process.env.GITHUB_SHA;
    delete process.env.GH_AW_PR_HEAD_BASELINE;
    global.core = { debug: vi.fn(), info: vi.fn(), warning: vi.fn(), error: vi.fn() };
    global.context = {
      eventName: "pull_request",
      repo: { owner: "test-owner", repo: "recovery" },
      payload: { pull_request: { number: 7, head: { sha: tip, repo: { full_name: repoSlug } }, base: { ref: "main", repo: { full_name: repoSlug } } } },
    };
    generatePatch = vi.spyOn(patchModule, "generateGitPatch").mockResolvedValue({ success: false, errorCode: "missing_base", error: checkoutGuidance });
    generateBundle = vi.spyOn(bundleModule, "generateGitBundle").mockResolvedValue({ success: false, error: checkoutGuidance });
    createPack = vi.spyOn(packModule, "createObjectPack").mockReturnValue({ size: 123 });
    trace = vi.spyOn(packModule, "traceObjectPack");
    vi.spyOn(gitModule, "execLocalGitSync").mockReturnValue(`${tip}\n`);
    vi.spyOn(gitModule, "ensureSafeDirectoryTrust").mockImplementation(() => {});
    vi.spyOn(baseModule, "getBaseBranch").mockResolvedValue("main");
    delete require.cache[handlerPath];
    ({ createHandlers } = require(handlerPath));
    append = vi.fn();
    server = { debug: vi.fn() };
  });

  afterEach(() => {
    vi.restoreAllMocks();
    if (cachedHandler) require.cache[handlerPath] = cachedHandler;
    else delete require.cache[handlerPath];
    fs.rmSync(root, { recursive: true, force: true });
    process.env = originalEnv;
    global.core = originalCore;
    global.context = originalContext;
  });

  for (const [type, method, args] of [
    ["create_pull_request", "createPullRequestHandler", { branch: "agent-change", title: "Preserve checkout recovery", body: "Restore validated output generation for incomplete checkouts." }],
    ["push_to_pull_request_branch", "pushToPullRequestBranchHandler", { branch: "agent-change" }],
  ]) {
    for (const failure of ["patch", "bundle"]) {
      function handler() {
        if (failure === "bundle") {
          generatePatch.mockResolvedValue({ success: true, patchPath: path.join(root, "output.patch"), patchSize: 1, patchLines: 1 });
        }
        return createHandlers(server, append, { [type]: { base_branch: "main", target: "triggering", max: 1 } })[method];
      }

      it(`${type} preserves the original ${failure} checkout diagnostic when packaging fails`, async () => {
        createPack.mockImplementation(() => {
          throw new Error("synthetic-private-packaging-error");
        });
        const result = await handler()(args);
        expect(result.isError).toBe(true);
        expect(JSON.parse(result.content[0].text).error).toBe(checkoutGuidance);
        expect(createPack).toHaveBeenCalledTimes(1);
        expect(append).not.toHaveBeenCalled();
        expect(trace).toHaveBeenCalledWith("queue-failed");
        expect(trace).not.toHaveBeenCalledWith("queued");
        expect(JSON.stringify([result, server.debug.mock.calls, global.core.debug.mock.calls])).not.toContain("synthetic-private-packaging-error");
        expect(generateBundle).toHaveBeenCalledTimes(failure === "bundle" ? 1 : 0);
      });

      it(`${type} propagates structured operation-limit guidance after ${failure} recovery packaging`, async () => {
        const invoke = handler();
        expect(JSON.parse((await invoke(args)).content[0].text)).toMatchObject({ result: "queued", recovery: "object-pack" });
        await expect(invoke(args)).rejects.toMatchObject({
          code: -32602,
          data: {
            constraint: "max",
            type,
            limit: 1,
            guidance: expect.stringContaining(`You have used all 1 ${type} operations for this run.`),
          },
        });
        expect(append).toHaveBeenCalledTimes(1);
        expect(createPack).toHaveBeenCalledTimes(2);
        expect(trace).not.toHaveBeenCalledWith("queue-failed");
      });

      it(`${type} propagates append errors unchanged after ${failure} recovery packaging`, async () => {
        const error = { code: -32000, message: "Output write failed", data: { guidance: "Retry after restoring the output sink." } };
        append.mockImplementation(() => {
          throw error;
        });
        await expect(handler()(args)).rejects.toBe(error);
        expect(trace).not.toHaveBeenCalledWith("queue-failed");
        expect(trace).not.toHaveBeenCalledWith("queued");
      });
    }
  }
});
