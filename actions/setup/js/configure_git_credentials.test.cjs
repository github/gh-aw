import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { spawnSync } from "child_process";

// Path to the shell script under test.
const SCRIPT_PATH = path.join(__dirname, "..", "sh", "configure_git_credentials.sh");

function createTempDir(prefix) {
  return fs.mkdtempSync(path.join(os.tmpdir(), prefix));
}

function removeDir(dir) {
  if (dir && fs.existsSync(dir)) {
    fs.rmSync(dir, { recursive: true, force: true });
  }
}

/**
 * Run configure_git_credentials.sh in an isolated HOME so that the global git
 * config it writes does not affect the developer's real ~/.gitconfig.
 */
function runScript(env, cwd) {
  return spawnSync("sh", [SCRIPT_PATH], {
    cwd,
    encoding: "utf8",
    env: { ...process.env, GITHUB_TOKEN: "", GIT_TOKEN: "", GH_AW_GIT_REPOSITORY: undefined, ...env },
  });
}

describe("configure_git_credentials.sh repository identity", () => {
  let root;
  let target;
  let env;

  function authenticatedURL(token, repo = "target") {
    return `https://${"x-access-token"}:${token}@github.com/owner/${repo}.git`;
  }

  function git(...args) {
    const result = spawnSync("git", args, { cwd: target, encoding: "utf8", env: { ...process.env, ...env } });
    expect(result.status, result.stderr).toBe(0);
    return result.stdout.trim();
  }

  beforeEach(() => {
    root = createTempDir("cfg-git-identity-");
    const home = path.join(root, "home");
    const workspace = path.join(root, "ws");
    target = path.join(workspace, "target");
    fs.mkdirSync(home);
    fs.mkdirSync(target, { recursive: true });
    // ScriptHandler exports the workflow context into reserved GITHUB_* variables,
    // even when a step attempted to set GITHUB_REPOSITORY to the target.
    env = {
      HOME: home,
      GITHUB_WORKSPACE: workspace,
      RUNNER_TEMP: root,
      GITHUB_REPOSITORY: "owner/workflow",
      GITHUB_SERVER_URL: "https://github.com",
      GH_AW_GIT_REPOSITORY: "owner/target",
      GITHUB_TOKEN: "test-token",
    };
    git("init", workspace);
    git("-C", workspace, "remote", "add", "origin", "https://github.com/owner/workflow.git");
    git("init", target);
    git("remote", "add", "origin", "https://github.com/owner/target.git");
    git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "target commit");
  });

  afterEach(() => removeDir(root));

  it("preserves the external current checkout despite runner-reserved workflow identity", () => {
    const head = git("rev-parse", "HEAD");
    const result = runScript(env, target);
    expect(result.status, result.stderr).toBe(0);
    expect(git("remote", "get-url", "origin")).toBe(authenticatedURL("test-token"));
    expect(git("-C", env.GITHUB_WORKSPACE, "remote", "get-url", "origin")).toBe("https://github.com/owner/workflow.git");
    expect(git("rev-parse", "HEAD")).toBe(head);
    expect(git("status", "--short")).toBe("");
    expect(result.stderr).toContain("identity verified before credential configuration");
    expect(result.stderr).toContain("identity verified after credential configuration");
    expect(result.stdout + result.stderr).not.toContain("test-token");
  });

  it.each(["https://github.com/OWNER/Target", "git@github.com:owner/target.git", "ssh://git@github.com/owner/target.git", authenticatedURL("old-token")])("recognizes equivalent repository identity for %s", remote => {
    git("remote", "set-url", "origin", remote);
    expect(runScript(env, target).status).toBe(0);
  });

  it.each([
    ["wrong repository", authenticatedURL("private-secret", "workflow")],
    ["wrong server", "https://elsewhere.example/owner/target.git"],
  ])("rejects %s before changing the remote without disclosing credentials", (_name, remote) => {
    git("remote", "set-url", "origin", remote);
    const result = runScript(env, target);
    expect(result.status).not.toBe(0);
    expect(git("remote", "get-url", "origin")).toBe(remote);
    expect(result.stderr).toContain("identity verification failed before credential configuration");
    expect(result.stdout + result.stderr).not.toMatch(/private-secret|test-token|https:\/\//);
  });

  it("rejects a mismatched push URL", () => {
    git("remote", "set-url", "--push", "origin", "https://github.com/owner/workflow.git");
    const result = runScript(env, target);
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("failed before credential configuration");
  });

  it("rejects multiple fetch URLs if any identify a different repository", () => {
    git("remote", "set-url", "--add", "origin", "https://github.com/owner/workflow.git");
    expect(runScript(env, target).status).not.toBe(0);
  });

  it("rejects a missing origin rather than creating one", () => {
    git("remote", "remove", "origin");
    expect(runScript(env, target).status).not.toBe(0);
    expect(git("remote")).toBe("");
  });

  it("checks identity even when no authentication token is present", () => {
    git("remote", "set-url", "origin", "https://github.com/owner/workflow.git");
    expect(runScript({ ...env, GITHUB_TOKEN: "" }, target).status).not.toBe(0);
  });

  it("does not fall back to the workflow repository for an empty explicit target", () => {
    git("remote", "set-url", "origin", "https://github.com/owner/workflow.git");
    expect(runScript({ ...env, GH_AW_GIT_REPOSITORY: "" }, target).status).not.toBe(0);
  });

  it("fails after authentication if a URL rewrite changes the effective push identity", () => {
    git("config", "url.https://github.com/owner/workflow.git.pushInsteadOf", authenticatedURL("test-token"));
    const result = runScript(env, target);
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("identity verified before credential configuration");
    expect(result.stderr).toContain("identity verification failed after credential configuration");
    expect(result.stdout + result.stderr).not.toContain("test-token");
  });

  it("supports legacy compiled workflows and GIT_TOKEN authentication", () => {
    const result = runScript({ ...env, GH_AW_GIT_REPOSITORY: undefined, GITHUB_REPOSITORY: "owner/target", GITHUB_TOKEN: "", GIT_TOKEN: "legacy-token" }, target);
    expect(result.status, result.stderr).toBe(0);
    expect(git("remote", "get-url", "origin")).toContain("legacy-token@github.com/owner/target.git");
  });
});

function readSafeDirectories(home) {
  const result = spawnSync("git", ["config", "--global", "--get-all", "safe.directory"], {
    encoding: "utf8",
    env: { ...process.env, HOME: home },
  });
  if (result.status !== 0) {
    return [];
  }
  return result.stdout.split("\n").filter(Boolean);
}

describe("configure_git_credentials.sh checkout manifest trust", () => {
  const tempDirs = [];

  function tempDir(prefix) {
    const dir = createTempDir(prefix);
    tempDirs.push(dir);
    return dir;
  }

  afterEach(() => {
    while (tempDirs.length > 0) {
      removeDir(tempDirs.pop());
    }
  });

  function setup(manifest) {
    const root = tempDir("cfg-git-creds-");
    const home = path.join(root, "home");
    const workspace = path.join(root, "ws");
    const runnerTemp = path.join(root, "runner");
    const safeOutputs = path.join(runnerTemp, "gh-aw", "safeoutputs");
    fs.mkdirSync(home, { recursive: true });
    fs.mkdirSync(workspace, { recursive: true });
    fs.mkdirSync(safeOutputs, { recursive: true });
    if (manifest !== undefined) {
      fs.writeFileSync(path.join(safeOutputs, "checkout-manifest.json"), JSON.stringify(manifest), "utf8");
    }
    return { home, workspace, runnerTemp };
  }

  it("trusts cross-repo checkout subdirectories listed in the manifest", () => {
    const { home, workspace, runnerTemp } = setup({
      "owner/repo": { repository: "owner/repo", path: "github", default_branch: "main" },
      "owner/tools": { repository: "owner/tools", path: "vendor/tools", default_branch: "main" },
    });

    const result = runScript({ HOME: home, GITHUB_WORKSPACE: workspace, RUNNER_TEMP: runnerTemp });
    expect(result.status).toBe(0);

    const entries = readSafeDirectories(home);
    expect(entries).toContain(workspace);
    expect(entries).toContain(path.join(workspace, "github"));
    expect(entries).toContain(path.join(workspace, "vendor", "tools"));
  });

  it("skips manifest entries with an empty path", () => {
    const { home, workspace, runnerTemp } = setup({
      "owner/repo": { repository: "owner/repo", path: "", default_branch: "main" },
    });

    const result = runScript({ HOME: home, GITHUB_WORKSPACE: workspace, RUNNER_TEMP: runnerTemp });
    expect(result.status).toBe(0);

    const entries = readSafeDirectories(home);
    // Only the workspace itself is trusted; the empty-path entry adds nothing extra.
    expect(entries).toEqual([workspace]);
  });

  it("rejects manifest paths that escape the workspace (path traversal)", () => {
    const { home, workspace, runnerTemp } = setup({
      "evil/repo": { repository: "evil/repo", path: "../../escape", default_branch: "main" },
    });

    const result = runScript({ HOME: home, GITHUB_WORKSPACE: workspace, RUNNER_TEMP: runnerTemp });
    expect(result.status).toBe(0);

    const entries = readSafeDirectories(home);
    expect(entries).toEqual([workspace]);
    expect(entries.some(e => e.includes("escape"))).toBe(false);
  });

  it("honors GH_AW_CHECKOUT_MANIFEST override", () => {
    const root = tempDir("cfg-git-creds-override-");
    const home = path.join(root, "home");
    const workspace = path.join(root, "ws");
    fs.mkdirSync(home, { recursive: true });
    fs.mkdirSync(workspace, { recursive: true });
    const manifestPath = path.join(root, "custom-manifest.json");
    fs.writeFileSync(manifestPath, JSON.stringify({ "owner/repo": { repository: "owner/repo", path: "sub", default_branch: "main" } }), "utf8");

    const result = runScript({ HOME: home, GITHUB_WORKSPACE: workspace, GH_AW_CHECKOUT_MANIFEST: manifestPath });
    expect(result.status).toBe(0);

    const entries = readSafeDirectories(home);
    expect(entries).toContain(path.join(workspace, "sub"));
  });

  it("succeeds when no manifest is present", () => {
    const { home, workspace, runnerTemp } = setup(undefined);

    const result = runScript({ HOME: home, GITHUB_WORKSPACE: workspace, RUNNER_TEMP: runnerTemp });
    expect(result.status).toBe(0);

    const entries = readSafeDirectories(home);
    expect(entries).toEqual([workspace]);
  });
});
