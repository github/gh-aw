// @ts-check

import { describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";

const { checkoutRepository, normalizeCheckout, parseDynamicCheckouts, writeManifest } = require("./dynamic_checkouts.cjs");

describe("parseDynamicCheckouts", () => {
  it("accepts one object or an array", () => {
    expect(parseDynamicCheckouts('{"repository":"owner/repo"}')).toHaveLength(1);
    expect(parseDynamicCheckouts('[{"repository":"owner/a"},{"repository":"owner/b"}]')).toHaveLength(2);
  });

  it("rejects scalar results", () => {
    expect(() => parseDynamicCheckouts('"owner/repo"')).toThrow("checkout object or an array");
  });
});

describe("normalizeCheckout", () => {
  it("derives a safe path and defaults to a shallow checkout", () => {
    const checkout = normalizeCheckout({ repository: "owner/repo" }, "/workspace");
    expect(checkout.path).toBe("repo");
    expect(checkout.fetchDepth).toBe(1);
  });

  it("rejects traversal and unsupported fields", () => {
    expect(() => normalizeCheckout({ repository: "owner/repo", path: "../repo" }, "/workspace")).toThrow("relative path");
    expect(() => normalizeCheckout({ repository: "owner/repo", fetch: ["main"] }, "/workspace")).toThrow("field 'fetch' is not supported");
    expect(() => normalizeCheckout({ repository: "owner/repo", current: true }, "/workspace")).toThrow("field 'current' is not supported");
  });
});

describe("checkoutRepository", () => {
  it("uses an ephemeral credential and leaves no credential when persistence is disabled", async () => {
    const workspace = fs.mkdtempSync(path.join(os.tmpdir(), "dynamic-checkout-"));
    const target = path.join(workspace, "repo");
    const calls = [];
    const runGit = async args => {
      calls.push(args);
      if (args.includes("clone")) {
        fs.mkdirSync(path.join(target, ".git"), { recursive: true });
      }
      if (args.includes("symbolic-ref")) {
        return "origin/main";
      }
      return "";
    };

    const checkout = normalizeCheckout({ repository: "owner/repo", "github-token": "secret" }, workspace);
    const result = await checkoutRepository(checkout, {
      workspace,
      runGit,
      serverURL: "https://github.com",
      overrideToken: "fallback-token",
      maskSecret: () => {},
    });

    expect(result.default_branch).toBe("main");
    expect(calls[0]).toContain("http.https://github.com/.extraheader=AUTHORIZATION: basic eC1hY2Nlc3MtdG9rZW46c2VjcmV0");
    expect(calls.some(args => args.includes("--unset-all"))).toBe(true);
  });

  it("rejects a symlinked parent before creating directories outside the workspace", async () => {
    const workspace = fs.mkdtempSync(path.join(os.tmpdir(), "dynamic-checkout-workspace-"));
    const outside = fs.mkdtempSync(path.join(os.tmpdir(), "dynamic-checkout-outside-"));
    fs.symlinkSync(outside, path.join(workspace, "linked"));
    const checkout = normalizeCheckout({ repository: "owner/repo", path: "linked/nested/repo" }, workspace);

    await expect(
      checkoutRepository(checkout, {
        workspace,
        runGit: async () => "",
        maskSecret: () => {},
      })
    ).rejects.toThrow("traverses a symbolic link");
    expect(fs.existsSync(path.join(outside, "nested"))).toBe(false);
  });
});

describe("writeManifest", () => {
  it("merges dynamic entries into an existing manifest", () => {
    const runnerTemp = fs.mkdtempSync(path.join(os.tmpdir(), "dynamic-manifest-"));
    const manifestDir = path.join(runnerTemp, "gh-aw", "safeoutputs");
    fs.mkdirSync(manifestDir, { recursive: true });
    fs.writeFileSync(path.join(manifestDir, "checkout-manifest.json"), JSON.stringify({ "owner/static": { repository: "owner/static", path: "static" } }));

    const manifestPath = writeManifest([{ repository: "owner/dynamic", path: "dynamic", default_branch: "main" }], runnerTemp);
    const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
    expect(manifest).toHaveProperty("owner/static");
    expect(manifest).toHaveProperty("owner/dynamic");
  });
});
