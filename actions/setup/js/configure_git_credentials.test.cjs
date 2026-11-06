import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "fs";
import path from "path";
import { spawnSync } from "child_process";

// Path to the shell script under test.
const SCRIPT_PATH = path.join(__dirname, "..", "sh", "configure_git_credentials.sh");

function createTempDir(prefix) {
  return fs.mkdtempSync(path.join(__dirname, prefix));
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

  it("skips a missing origin rather than creating one", () => {
    git("remote", "remove", "origin");
    expect(runScript(env, target).status).toBe(0);
    expect(git("remote")).toBe("");
  });

  it("does not mistake an untrusted checkout for a missing origin", () => {
    const configPath = path.join(target, ".git", "config");
    const configBefore = fs.readFileSync(configPath, "utf8");
    // Runner Git configuration may trust every directory, overriding the ownership check.
    const result = runScript(
      {
        ...env,
        GIT_TEST_ASSUME_DIFFERENT_OWNER: "1",
        GIT_CONFIG_NOSYSTEM: "1",
        GIT_CONFIG_GLOBAL: path.join(env.HOME, ".gitconfig"),
        GIT_CONFIG_COUNT: "0",
      },
      target
    );
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("Git checkout configuration could not be read");
    expect(result.stdout + result.stderr).not.toContain("test-token");
    expect(fs.readFileSync(configPath, "utf8")).toBe(configBefore);
  });

  it.each([true, false])("skips a directory without its own checkout with authentication=%s", authenticate => {
    fs.rmSync(path.join(target, ".git"), { recursive: true });
    const result = runScript({ ...env, GITHUB_TOKEN: authenticate ? "test-token" : "" }, target);
    expect(result.status, result.stderr).toBe(0);
    expect(git("-C", env.GITHUB_WORKSPACE, "remote", "get-url", "origin")).toBe("https://github.com/owner/workflow.git");
    expect(result.stderr).not.toContain("identity verified");
  });

  it("skips a workspace without its own checkout", () => {
    const result = runScript(env, root);
    expect(result.status, result.stderr).toBe(0);
  });

  it("checks identity even when no authentication token is present", () => {
    git("remote", "set-url", "origin", "https://github.com/owner/workflow.git");
    expect(runScript({ ...env, GITHUB_TOKEN: "" }, target).status).not.toBe(0);
  });

  it("does not fall back to the workflow repository for an empty explicit target", () => {
    git("remote", "set-url", "origin", "https://github.com/owner/workflow.git");
    expect(runScript({ ...env, GH_AW_GIT_REPOSITORY: "" }, target).status).not.toBe(0);
  });

  it("rejects an empty explicit target even when the origin path is empty", () => {
    git("remote", "set-url", "origin", "https://github.com/");
    const result = runScript({ ...env, GH_AW_GIT_REPOSITORY: "" }, target);
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("identity target is empty");
    expect(git("config", "--get", "remote.origin.url")).toBe("https://github.com/");
  });

  it("fails after authentication if a URL rewrite changes the effective push identity", () => {
    git("config", "url.https://github.com/owner/workflow.git.pushInsteadOf", authenticatedURL("test-token"));
    const configBefore = fs.readFileSync(path.join(target, ".git", "config"), "utf8");
    const result = runScript(env, target);
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("identity verified before credential configuration");
    expect(result.stderr).toContain("identity verification failed after credential configuration");
    expect(result.stdout + result.stderr).not.toContain("test-token");
    expect(git("config", "--get", "remote.origin.url")).toBe("https://github.com/owner/target.git");
    expect(fs.readFileSync(path.join(target, ".git", "config"), "utf8")).toBe(configBefore);
  });

  it("preserves all raw fetch URLs when authentication configuration is rejected", () => {
    const original = "git@github.com:owner/target.git";
    git("remote", "set-url", "origin", original);
    git("remote", "set-url", "--add", "origin", "https://github.com/owner/target");
    const before = git("config", "--get-all", "remote.origin.url");
    git("config", "url.https://github.com/owner/workflow.git.pushInsteadOf", authenticatedURL("test-token"));
    const result = runScript(env, target);
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("remote authentication configuration failed");
    expect(git("config", "--get-all", "remote.origin.url")).toBe(before);
  });

  it.each([true, false])("reports rollback failure with authentication URL removal succeeding=%s", removeSucceeds => {
    git("config", "url.https://github.com/owner/workflow.git.pushInsteadOf", authenticatedURL("test-token"));
    const realGit = spawnSync("sh", ["-c", "command -v git"], { encoding: "utf8" }).stdout.trim();
    const bin = path.join(root, "rollback-bin");
    fs.mkdirSync(bin);
    fs.writeFileSync(
      path.join(bin, "git"),
      `#!/bin/sh
if [ "$1" = remote ] && [ "$2" = set-url ] && [ "$3" = origin ] && [ "$4" = https://github.com/owner/target.git ]; then
  echo "test-token: simulated restoration failure" >&2
  exit 1
fi
if [ "$1" = config ] && [ "$2" = --unset-all ] && [ "$GH_AW_TEST_REMOVE_SUCCEEDS" = false ]; then
  echo "test-token: simulated removal failure" >&2
  exit 1
fi
exec "$GH_AW_TEST_REAL_GIT" "$@"
`,
      { mode: 0o755 }
    );
    const result = runScript({ ...env, PATH: `${bin}${path.delimiter}${process.env.PATH}`, GH_AW_TEST_REAL_GIT: realGit, GH_AW_TEST_REMOVE_SUCCEEDS: String(removeSucceeds) }, target);
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("failed after credential configuration");
    expect(result.stdout + result.stderr).not.toContain("test-token");
    if (removeSucceeds) {
      expect(result.stderr).toContain("Git remote restoration failed; authentication URL removed.");
      expect(spawnSync("git", ["config", "--get-all", "remote.origin.url"], { cwd: target, env: { ...process.env, ...env } }).status).toBe(1);
    } else {
      expect(result.stderr).toContain("authentication URL removal failed; credentials may remain in Git configuration.");
      expect(git("config", "--get", "remote.origin.url")).toBe(authenticatedURL("test-token"));
    }
  });

  it("rejects a plaintext HTTP rewrite before installing credentials", () => {
    git("config", "url.http://.insteadOf", "https://");
    const result = runScript(env, target);
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("failed before credential configuration");
    expect(git("config", "--get", "remote.origin.url")).toBe("https://github.com/owner/target.git");
  });

  it("rejects a plaintext HTTP rewrite after installing credentials and restores origin", () => {
    git("config", "url.http://github.com/owner/target.git.pushInsteadOf", authenticatedURL("test-token"));
    const result = runScript(env, target);
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("failed after credential configuration");
    expect(git("config", "--get", "remote.origin.url")).toBe("https://github.com/owner/target.git");
  });

  it.each([true, false])("checks identity without Node on PATH with matching origin=%s", matches => {
    const bin = path.join(root, "bin");
    fs.mkdirSync(bin);
    for (const name of ["git", "tr"]) {
      const executable = spawnSync("sh", ["-c", `command -v ${name}`], { encoding: "utf8" }).stdout.trim();
      fs.symlinkSync(executable, path.join(bin, name));
    }
    if (!matches) git("remote", "set-url", "origin", "https://github.com/owner/workflow.git");
    const result = spawnSync("/bin/sh", [SCRIPT_PATH], {
      cwd: target,
      encoding: "utf8",
      env: { ...process.env, ...env, PATH: bin },
    });
    if (matches) {
      expect(result.status, result.stderr).toBe(0);
      expect(result.stderr).toContain("identity verified after credential configuration");
      expect(git("remote", "get-url", "origin")).toBe(authenticatedURL("test-token"));
    } else {
      expect(result.status).not.toBe(0);
      expect(result.stderr).toContain("failed before credential configuration");
      expect(git("remote", "get-url", "origin")).toBe("https://github.com/owner/workflow.git");
    }
  });

  it("supports linked worktrees with a .git file", () => {
    const worktree = path.join(root, "linked");
    git("worktree", "add", "--detach", worktree, "HEAD");
    expect(fs.statSync(path.join(worktree, ".git")).isFile()).toBe(true);
    const result = runScript(env, worktree);
    expect(result.status, result.stderr).toBe(0);
    expect(git("remote", "get-url", "origin")).toBe(authenticatedURL("test-token"));
  });

  it.each([
    "http://github.com/owner/target.git",
    "https://github.com/owner/target.git?query",
    "https://github.com/owner/target.git#fragment",
    "https://github.com/owner/%74arget.git",
    "https://github.com/owner/../owner/target.git",
    "https://github.com/owner/target.git\nhttps://github.com/owner/workflow.git",
    "https://github.com\\@elsewhere.example/owner/target.git",
  ])("rejects unsafe or ambiguous URL %s", remote => {
    git("remote", "set-url", "origin", remote);
    const result = runScript(env, target);
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("failed before credential configuration");
    expect(git("config", "--get", "remote.origin.url")).toBe(remote);
  });

  it.each(["github.com", "https://github.com/", "https://github.com:443"])("supports server spelling %s", server => {
    const result = runScript({ ...env, GITHUB_SERVER_URL: server }, target);
    expect(result.status, result.stderr).toBe(0);
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
