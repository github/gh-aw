import { afterEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { spawnSync } from "child_process";

import { getSetupTimeoutMs } from "./child_process_timeouts.cjs";
import { buildCheckoutManifest, readManifestEntriesFromEnv, resolveDefaultBranch } from "./build_checkout_manifest.cjs";

function execGit(args, options = {}) {
  const result = spawnSync("git", args, { encoding: "utf8", ...options });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    throw new Error(`git ${args.join(" ")} failed:\nstdout: ${result.stdout}\nstderr: ${result.stderr}`);
  }
  return result.stdout;
}

function createTempDir(prefix) {
  return fs.mkdtempSync(path.join(os.tmpdir(), prefix));
}

function removeDir(dir) {
  if (dir && fs.existsSync(dir)) {
    fs.rmSync(dir, { recursive: true, force: true });
  }
}

function setEnv(key, value) {
  if (value === undefined) {
    delete process.env[key];
  } else {
    process.env[key] = value;
  }
}

describe("build_checkout_manifest.cjs", () => {
  const originalEnv = { ...process.env };
  const tempDirs = [];

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) {
        delete process.env[key];
      }
    }
    Object.assign(process.env, originalEnv);
    while (tempDirs.length > 0) {
      removeDir(tempDirs.pop());
    }
  });

  it("reads entries from environment variables", () => {
    setEnv("GH_AW_CHECKOUT_MANIFEST_COUNT", "2");
    setEnv("GH_AW_CHECKOUT_REPO_0", "owner/a");
    setEnv("GH_AW_CHECKOUT_PATH_0", "./a");
    setEnv("GH_AW_CHECKOUT_TOKEN_0", "${{ secrets.REPO_A_TOKEN }}");
    setEnv("GH_AW_CHECKOUT_REPO_1", "owner/b");
    setEnv("GH_AW_CHECKOUT_PATH_1", "");

    expect(readManifestEntriesFromEnv()).toEqual([
      { repository: "owner/a", path: "./a", token: "${{ secrets.REPO_A_TOKEN }}" },
      { repository: "owner/b", path: "", token: "" },
    ]);
  });

  it("resolves default branch from local git checkout before gh fallback", () => {
    const workspace = createTempDir("checkout-manifest-workspace-");
    tempDirs.push(workspace);
    const checkoutPath = "target";
    const repoDir = path.join(workspace, checkoutPath);
    fs.mkdirSync(repoDir, { recursive: true });

    execGit(["init", "-q"], { cwd: repoDir });
    execGit(["symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main"], { cwd: repoDir });

    const ghCalls = [];
    const defaultBranch = resolveDefaultBranch("owner/repo", checkoutPath, {
      workspace,
      runGH: args => {
        ghCalls.push(args);
        return "should-not-be-used\n";
      },
    });

    expect(defaultBranch).toBe("main");
    expect(ghCalls).toHaveLength(0);
  });

  it("passes a timeout to local git default branch lookup", () => {
    const workspace = createTempDir("checkout-manifest-workspace-");
    tempDirs.push(workspace);
    const checkoutPath = "target";
    const repoDir = path.join(workspace, checkoutPath);
    fs.mkdirSync(path.join(repoDir, ".git"), { recursive: true });
    /** @type {any} */
    let gitOptions = null;

    const defaultBranch = resolveDefaultBranch("owner/repo", checkoutPath, {
      workspace,
      runGit: (_args, options) => {
        gitOptions = options;
        return "origin/main\n";
      },
    });

    expect(defaultBranch).toBe("main");
    expect(gitOptions?.timeout).toBe(getSetupTimeoutMs("gitBranch"));
  });

  it("falls back to gh api when local git default branch is unavailable", () => {
    const workspace = createTempDir("checkout-manifest-workspace-");
    tempDirs.push(workspace);
    /** @type {any} */
    let ghOptions = null;

    const defaultBranch = resolveDefaultBranch("owner/repo", "missing", {
      workspace,
      checkoutToken: "${{ secrets.CROSS_REPO_PAT }}",
      runGH: (_args, options) => {
        ghOptions = options;
        return "trunk\n";
      },
    });

    expect(defaultBranch).toBe("trunk");
    expect(ghOptions?.env?.GH_TOKEN).toBe("${{ secrets.CROSS_REPO_PAT }}");
    expect(ghOptions?.timeout).toBe(getSetupTimeoutMs("outcomeGh"));
  });

  it("writes manifest with lowercase keys", () => {
    const workspace = createTempDir("checkout-manifest-workspace-");
    const runnerTemp = createTempDir("checkout-manifest-runner-temp-");
    tempDirs.push(workspace, runnerTemp);

    const { manifestPath, manifest } = buildCheckoutManifest(
      [
        { repository: "Owner/Repo", path: "./repo" },
        { repository: "", path: "./skip" },
      ],
      {
        workspace,
        runnerTemp,
        runGH: () => "main\n",
      }
    );

    expect(manifestPath).toBe(path.join(runnerTemp, "gh-aw", "safeoutputs", "checkout-manifest.json"));
    expect(manifest).toEqual({
      "owner/repo": {
        repository: "Owner/Repo",
        path: "./repo",
        default_branch: "main",
      },
    });

    const fileContents = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
    expect(fileContents).toEqual(manifest);
  });

  it("preserves dynamic entries already written to the manifest", () => {
    const workspace = createTempDir("checkout-manifest-workspace-");
    const runnerTemp = createTempDir("checkout-manifest-runner-temp-");
    tempDirs.push(workspace, runnerTemp);
    const manifestDir = path.join(runnerTemp, "gh-aw", "safeoutputs");
    fs.mkdirSync(manifestDir, { recursive: true });
    fs.writeFileSync(path.join(manifestDir, "checkout-manifest.json"), JSON.stringify({ "owner/dynamic": { repository: "owner/dynamic", path: "dynamic", default_branch: "main" } }));

    const { manifest } = buildCheckoutManifest([{ repository: "owner/static", path: "static" }], {
      workspace,
      runnerTemp,
      runGH: () => "main\n",
    });

    expect(manifest).toHaveProperty("owner/dynamic");
    expect(manifest).toHaveProperty("owner/static");
  });
});
