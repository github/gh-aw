// @ts-check
//
// apply_samples.test.cjs
//
// Smoke test for the deterministic samples replay driver. Spawns the
// driver as a subprocess (so it actually launches the real MCP server) and
// asserts that:
//   - the driver exits 0
//   - the MCP server appends the expected JSONL entry to GH_AW_SAFE_OUTPUTS
//   - the synthetic agent-stdio log includes a `terminal_reason: completed` marker
//
// Tests intentionally use the simplest safe-output tool (`create_issue`) so we
// do not need to set up a git working tree for patch sidecars.

import { describe, it, expect, beforeAll, vi } from "vitest";
import { spawnSync } from "child_process";
import { createRequire } from "module";
import fs from "fs";
import path from "path";
import os from "os";
import { fileURLToPath } from "url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const driverPath = path.join(__dirname, "apply_samples.cjs");
const require = createRequire(import.meta.url);

function makeTempDir(prefix) {
  return fs.mkdtempSync(path.join(os.tmpdir(), prefix));
}

function git(args, cwd) {
  const r = spawnSync("git", args, { cwd, encoding: "utf8" });
  if (r.status !== 0) {
    throw new Error(`git ${args.join(" ")} failed: ${r.stderr || r.stdout}`);
  }
  return r.stdout;
}

function initRepo(dir, defaultBranch) {
  git(["init", "-q", "-b", defaultBranch], dir);
  git(["config", "user.email", "ghaw-test@example.com"], dir);
  git(["config", "user.name", "ghaw test"], dir);
  fs.writeFileSync(path.join(dir, "README.md"), "# seed\n");
  git(["add", "."], dir);
  git(["commit", "-q", "-m", "seed"], dir);
}

describe("apply_samples.cjs", () => {
  let tempDir;
  let configPath;
  let outputsPath;
  let logPath;

  beforeAll(() => {
    tempDir = makeTempDir("gh-aw-apply-samples-");
    configPath = path.join(tempDir, "config.json");
    outputsPath = path.join(tempDir, "outputs.jsonl");
    logPath = path.join(tempDir, "agent-stdio.log");

    // Minimal safe-outputs config enabling only the `create_issue` tool. The
    // bootstrap loader keys off the snake-case keys present here.
    fs.writeFileSync(
      configPath,
      JSON.stringify({
        create_issue: { max: 1 },
      })
    );
  });

  it("replays a create_issue sample through the real MCP server and emits a completed marker", () => {
    const samples = [
      {
        tool: "create_issue",
        arguments: {
          title: "Deterministic sample issue",
          body: "This issue was emitted by the apply_samples driver during a unit test.",
        },
      },
    ];

    const result = spawnSync(process.execPath, [driverPath], {
      env: {
        ...process.env,
        GH_AW_SAMPLES: JSON.stringify(samples),
        GH_AW_SAFE_OUTPUTS_CONFIG_PATH: configPath,
        GH_AW_SAFE_OUTPUTS: outputsPath,
        GH_AW_AGENT_STDIO_LOG: logPath,
      },
      encoding: "utf8",
      timeout: 15000,
    });

    if (result.status !== 0) {
      // Surface stderr so failures are diagnosable in CI.
      throw new Error(`driver exited with status ${result.status}\nstderr:\n${result.stderr}\nstdout:\n${result.stdout}`);
    }

    expect(fs.existsSync(outputsPath)).toBe(true);
    const outputLines = fs
      .readFileSync(outputsPath, "utf8")
      .split("\n")
      .filter(line => line.trim().length > 0);
    expect(outputLines.length).toBeGreaterThanOrEqual(1);

    const firstEntry = JSON.parse(outputLines[0]);
    expect(firstEntry.type).toBe("create_issue");
    expect(firstEntry.title).toBe("Deterministic sample issue");

    expect(fs.existsSync(logPath)).toBe(true);
    const logText = fs.readFileSync(logPath, "utf8");
    expect(logText).toContain("terminal_reason");
    expect(logText).toContain("completed");
  });

  it("exits cleanly when GH_AW_SAMPLES is empty", () => {
    const result = spawnSync(process.execPath, [driverPath], {
      env: {
        ...process.env,
        GH_AW_SAMPLES: "[]",
        GH_AW_SAFE_OUTPUTS_CONFIG_PATH: configPath,
        GH_AW_SAFE_OUTPUTS: outputsPath,
        GH_AW_AGENT_STDIO_LOG: path.join(tempDir, "empty-log.log"),
      },
      encoding: "utf8",
      timeout: 10000,
    });

    expect(result.status).toBe(0);
    const logText = fs.readFileSync(path.join(tempDir, "empty-log.log"), "utf8");
    expect(logText).toContain("terminal_reason");
  });

  // Defense in depth: an older compiler that marshaled a nil Go slice would
  // emit `null` into GH_AW_SAMPLES. Newer drivers must tolerate that and
  // treat it as "no samples", not crash with `must be a JSON array`.
  it("exits cleanly when GH_AW_SAMPLES is the literal `null`", () => {
    const logPath = path.join(tempDir, "null-log.log");
    const result = spawnSync(process.execPath, [driverPath], {
      env: {
        ...process.env,
        GH_AW_SAMPLES: "null",
        GH_AW_SAFE_OUTPUTS_CONFIG_PATH: configPath,
        GH_AW_SAFE_OUTPUTS: outputsPath,
        GH_AW_AGENT_STDIO_LOG: logPath,
      },
      encoding: "utf8",
      timeout: 10000,
    });

    if (result.status !== 0) {
      throw new Error(`driver exited with status ${result.status}\nstderr:\n${result.stderr}\nstdout:\n${result.stdout}`);
    }
    expect(result.stderr).toContain("GH_AW_SAMPLES is null");
    const logText = fs.readFileSync(logPath, "utf8");
    expect(logText).toContain("terminal_reason");
  });

  it("replays dynamic call_workflow samples with workflow_name materialized", () => {
    const dynamicConfigPath = path.join(tempDir, "call-workflow-config.json");
    const dynamicToolsPath = path.join(tempDir, "call-workflow-tools.json");
    const dynamicOutputsPath = path.join(tempDir, "call-workflow-outputs.jsonl");
    const dynamicLogPath = path.join(tempDir, "call-workflow-agent-stdio.log");

    fs.writeFileSync(
      dynamicConfigPath,
      JSON.stringify({
        "call-workflow": {
          workflows: ["test-copilot-call-worker"],
        },
      })
    );
    fs.writeFileSync(
      dynamicToolsPath,
      JSON.stringify([
        {
          name: "test_copilot_call_worker",
          _call_workflow_name: "test-copilot-call-worker",
          description: "Call the test-copilot-call-worker workflow",
          inputSchema: {
            type: "object",
            properties: {
              sentinel: { type: "string" },
            },
            additionalProperties: false,
          },
        },
      ])
    );

    const samples = [
      {
        tool: "test_copilot_call_worker",
        arguments: {
          sentinel: "deterministic-sample",
        },
      },
    ];

    const result = spawnSync(process.execPath, [driverPath], {
      env: {
        ...process.env,
        GH_AW_SAMPLES: JSON.stringify(samples),
        GH_AW_SAFE_OUTPUTS_CONFIG_PATH: dynamicConfigPath,
        GH_AW_SAFE_OUTPUTS_TOOLS_PATH: dynamicToolsPath,
        GH_AW_SAFE_OUTPUTS: dynamicOutputsPath,
        GH_AW_AGENT_STDIO_LOG: dynamicLogPath,
      },
      encoding: "utf8",
      timeout: 15000,
    });

    if (result.status !== 0) {
      throw new Error(`driver exited with status ${result.status}\nstderr:\n${result.stderr}\nstdout:\n${result.stdout}`);
    }

    const outputLines = fs
      .readFileSync(dynamicOutputsPath, "utf8")
      .split("\n")
      .filter(line => line.trim().length > 0);
    expect(outputLines).toHaveLength(1);

    const firstEntry = JSON.parse(outputLines[0]);
    expect(firstEntry.type).toBe("call_workflow");
    expect(firstEntry.workflow_name).toBe("test-copilot-call-worker");
    expect(firstEntry.inputs).toEqual({
      sentinel: "deterministic-sample",
    });
  });
});

describe("apply_samples.cjs sendJsonRpc", () => {
  const { sendJsonRpc } = require("./apply_samples.cjs");

  async function* fromLines(lines) {
    for (const line of lines) {
      yield line;
    }
  }

  it("skips non-JSON stdout lines until a JSON-RPC response arrives", async () => {
    const writes = [];
    const stdin = {
      write: chunk => writes.push(chunk),
    };
    const response = await sendJsonRpc({}, stdin, { jsonrpc: "2.0", id: 99, method: "tools/call", params: {} }, fromLines(["[debug] Executing git command: git status", '{"jsonrpc":"2.0","id":99,"result":{"ok":true}}']));

    expect(writes.length).toBe(1);
    expect(writes[0]).toContain('"id":99');
    expect(response).toEqual({ jsonrpc: "2.0", id: 99, result: { ok: true } });
  });

  it("throws a helpful error for malformed JSON lines that look like protocol frames", async () => {
    const stdin = { write: () => {} };
    await expect(sendJsonRpc({}, stdin, { jsonrpc: "2.0", id: 4, method: "initialize", params: {} }, fromLines(["{not-json"]))).rejects.toThrow("failed to parse MCP JSON-RPC response");
  });
});

describe("apply_samples.cjs selectTokenForRepo", () => {
  const { selectTokenForRepo } = require("./apply_samples.cjs");

  function withEnv(vars, fn) {
    const saved = {};
    for (const k of Object.keys(vars)) {
      saved[k] = process.env[k];
      if (vars[k] === undefined) delete process.env[k];
      else process.env[k] = vars[k];
    }
    try {
      return fn();
    } finally {
      for (const k of Object.keys(saved)) {
        if (saved[k] === undefined) delete process.env[k];
        else process.env[k] = saved[k];
      }
    }
  }

  it("prefers the per-repo token from GH_AW_REPO_TOKENS over GITHUB_TOKEN", () => {
    withEnv(
      {
        GH_AW_REPO_TOKENS: JSON.stringify({ "owner/cross": "cross-token", "owner/auto": "auto-token" }),
        GITHUB_TOKEN: "default-token",
        GH_TOKEN: undefined,
      },
      () => {
        expect(selectTokenForRepo("owner", "cross")).toBe("cross-token");
        expect(selectTokenForRepo("owner", "auto")).toBe("auto-token");
      }
    );
  });

  it("falls back to GITHUB_TOKEN when the slug is not present in GH_AW_REPO_TOKENS", () => {
    withEnv(
      {
        GH_AW_REPO_TOKENS: JSON.stringify({ "owner/cross": "cross-token" }),
        GITHUB_TOKEN: "default-token",
        GH_TOKEN: undefined,
      },
      () => {
        expect(selectTokenForRepo("owner", "other")).toBe("default-token");
      }
    );
  });

  it("falls back to GITHUB_TOKEN when GH_AW_REPO_TOKENS is unset", () => {
    withEnv({ GH_AW_REPO_TOKENS: undefined, GITHUB_TOKEN: "default-token", GH_TOKEN: undefined }, () => {
      expect(selectTokenForRepo("owner", "auto")).toBe("default-token");
    });
  });

  it("falls back to GITHUB_TOKEN when GH_AW_REPO_TOKENS is malformed JSON", () => {
    withEnv({ GH_AW_REPO_TOKENS: "{not json", GITHUB_TOKEN: "default-token", GH_TOKEN: undefined }, () => {
      expect(selectTokenForRepo("owner", "auto")).toBe("default-token");
    });
  });

  it("returns undefined when no env-var token is available and slug is missing", () => {
    withEnv({ GH_AW_REPO_TOKENS: undefined, GITHUB_TOKEN: undefined, GH_TOKEN: undefined }, () => {
      expect(selectTokenForRepo("owner", "auto")).toBeUndefined();
    });
  });
});

describe("apply_samples.cjs preStagePatch (create_pull_request / push_to_pull_request_branch)", () => {
  // Load the module under test directly so we can drive preStagePatch in
  // isolation against a real, throwaway git working tree. This is the
  // critical code path that turns a `patch` sidecar on a sample entry into
  // a real branch + commit that the downstream MCP `create_pull_request`
  // handler (which derives a git diff) can act on.
  const { preStagePatch } = require("./apply_samples.cjs");

  /**
   * Build a unified diff that adds a brand-new file. Synthetic but realistic.
   */
  function newFileDiff(filePath, contents) {
    const lines = contents.split("\n");
    // Strip trailing empty element produced by a terminating "\n" so the
    // hunk header line count matches what git apply expects.
    if (lines[lines.length - 1] === "") lines.pop();
    const body = lines.map(l => "+" + l).join("\n");
    return `diff --git a/${filePath} b/${filePath}\n` + `new file mode 100644\n` + `index 0000000..1111111\n` + `--- /dev/null\n` + `+++ b/${filePath}\n` + `@@ -0,0 +1,${lines.length} @@\n` + body + "\n";
  }

  it("checks out the requested branch and commits the patch on it (create_pull_request)", async () => {
    const workspace = makeTempDir("gh-aw-prestage-cpr-");
    initRepo(workspace, "main");

    const branchName = "feat/gh-aw-sample-branch";
    const fileToAdd = "sample-feature.txt";
    const fileBody = "hello from a deterministic sample\nsecond line\n";
    const entry = {
      tool: "create_pull_request",
      arguments: {
        title: "Sample PR",
        body: "Sample PR body",
        branch: branchName,
      },
      sidecars: { patch: newFileDiff(fileToAdd, fileBody) },
    };

    // GH_AW_CUSTOM_BASE_BRANCH steers preStagePatch to check out the right
    // base ref inside our fresh repo (default is GITHUB_BASE_REF / "main").
    const prev = process.env.GH_AW_CUSTOM_BASE_BRANCH;
    process.env.GH_AW_CUSTOM_BASE_BRANCH = "main";
    try {
      await preStagePatch(entry, 0, workspace);
    } finally {
      if (prev === undefined) delete process.env.GH_AW_CUSTOM_BASE_BRANCH;
      else process.env.GH_AW_CUSTOM_BASE_BRANCH = prev;
    }

    // 1. Branch name on the entry is preserved (driver must forward it to MCP).
    expect(entry.arguments.branch).toBe(branchName);

    // 2. The named branch exists in the working repo.
    const branches = git(["branch", "--list", branchName], workspace).trim();
    expect(branches).toContain(branchName);

    // 3. Current HEAD is that branch.
    const head = git(["rev-parse", "--abbrev-ref", "HEAD"], workspace).trim();
    expect(head).toBe(branchName);

    // 4. The patch was applied AND committed (not just sitting in the worktree).
    const status = git(["status", "--porcelain"], workspace).trim();
    expect(status).toBe("");
    expect(fs.existsSync(path.join(workspace, fileToAdd))).toBe(true);
    expect(fs.readFileSync(path.join(workspace, fileToAdd), "utf8")).toBe(fileBody);

    // 5. The commit message identifies the sample so failures are diagnosable.
    const lastMsg = git(["log", "-1", "--pretty=%s"], workspace).trim();
    expect(lastMsg).toMatch(/gh-aw sample 1: create_pull_request/);

    // 6. The new file shows up as a real diff against the base branch — this is
    // precisely what the downstream MCP create_pull_request handler will read.
    const diff = git(["diff", "main..." + branchName, "--", fileToAdd], workspace);
    expect(diff).toContain("+hello from a deterministic sample");
  });

  it("stages the patch in the cross-repo checkout subdirectory (path: github) (issue #40086)", async () => {
    // Reproduce the failing layout: a main repo at the workspace root and a
    // side-repo checked out into a `github/` subdirectory. The checkout manifest
    // maps the target repo to path "github", so preStagePatch must create the
    // branch + commit inside ${workspace}/github — not the main repo root —
    // otherwise the safe-outputs MCP server cannot pin the branch.
    const workspace = makeTempDir("gh-aw-prestage-subdir-");
    initRepo(workspace, "main");

    const subdir = path.join(workspace, "github");
    fs.mkdirSync(subdir);
    initRepo(subdir, "main");

    const manifestPath = path.join(workspace, "checkout-manifest.json");
    fs.writeFileSync(
      manifestPath,
      JSON.stringify({
        "githubnext/gh-aw-side-repo": {
          repository: "githubnext/gh-aw-side-repo",
          path: "github",
          default_branch: "main",
        },
      })
    );

    const configPath = path.join(workspace, "config.json");
    fs.writeFileSync(
      configPath,
      JSON.stringify({
        create_pull_request: { "target-repo": "githubnext/gh-aw-side-repo" },
      })
    );

    const branchName = "gh-aw-sample-copilot-siderepo-subdir-pr";
    const fileToAdd = "subdir-notes.md";
    const entry = {
      tool: "create_pull_request",
      arguments: { title: "Subdir PR", body: "body", branch: branchName },
      sidecars: { patch: newFileDiff(fileToAdd, "side repo content\n") },
    };

    // Reset the checkout-manifest module cache so our manifest is loaded fresh.
    require("./checkout_manifest.cjs")._resetCache();

    const prevBase = process.env.GH_AW_CUSTOM_BASE_BRANCH;
    const prevManifest = process.env.GH_AW_CHECKOUT_MANIFEST;
    const prevConfig = process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH;
    process.env.GH_AW_CUSTOM_BASE_BRANCH = "main";
    process.env.GH_AW_CHECKOUT_MANIFEST = manifestPath;
    process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = configPath;
    try {
      await preStagePatch(entry, 0, workspace);
    } finally {
      require("./checkout_manifest.cjs")._resetCache();
      if (prevBase === undefined) delete process.env.GH_AW_CUSTOM_BASE_BRANCH;
      else process.env.GH_AW_CUSTOM_BASE_BRANCH = prevBase;
      if (prevManifest === undefined) delete process.env.GH_AW_CHECKOUT_MANIFEST;
      else process.env.GH_AW_CHECKOUT_MANIFEST = prevManifest;
      if (prevConfig === undefined) delete process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH;
      else process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = prevConfig;
    }

    // The branch + commit + file land in the side-repo subdirectory...
    expect(git(["rev-parse", "--abbrev-ref", "HEAD"], subdir).trim()).toBe(branchName);
    expect(git(["branch", "--list", branchName], subdir)).toContain(branchName);
    expect(fs.existsSync(path.join(subdir, fileToAdd))).toBe(true);

    // ...and NOT in the main repo root (which stays on its seed branch).
    expect(git(["rev-parse", "--abbrev-ref", "HEAD"], workspace).trim()).toBe("main");
    expect(git(["branch", "--list", branchName], workspace).trim()).toBe("");
    expect(fs.existsSync(path.join(workspace, fileToAdd))).toBe(false);
  });

  it("derives push_to_pull_request_branch branch from pull_request event payload", async () => {
    const workspace = makeTempDir("gh-aw-prestage-push-pr-");
    initRepo(workspace, "main");

    const headRef = "feat/copilot-push-branch";
    const eventPath = path.join(workspace, "event.json");
    fs.writeFileSync(
      eventPath,
      JSON.stringify({
        pull_request: { number: 654, head: { ref: headRef } },
      })
    );

    const entry = {
      tool: "push_to_pull_request_branch",
      // No `branch` in arguments — the agent never supplies one now.
      arguments: { message: "Push update" },
      sidecars: { patch: newFileDiff("push-feature.txt", "from push sample\n") },
    };

    const prevBase = process.env.GH_AW_CUSTOM_BASE_BRANCH;
    const prevEvent = process.env.GITHUB_EVENT_PATH;
    process.env.GH_AW_CUSTOM_BASE_BRANCH = "main";
    process.env.GITHUB_EVENT_PATH = eventPath;
    try {
      await preStagePatch(entry, 0, workspace);
    } finally {
      if (prevBase === undefined) delete process.env.GH_AW_CUSTOM_BASE_BRANCH;
      else process.env.GH_AW_CUSTOM_BASE_BRANCH = prevBase;
      if (prevEvent === undefined) delete process.env.GITHUB_EVENT_PATH;
      else process.env.GITHUB_EVENT_PATH = prevEvent;
    }

    // The staged branch is the PR's head ref (not a synthetic "gh-aw-sample-N").
    expect(git(["rev-parse", "--abbrev-ref", "HEAD"], workspace).trim()).toBe(headRef);
    expect(fs.existsSync(path.join(workspace, "push-feature.txt"))).toBe(true);
    // The agent input never carries `branch` for this tool.
    expect(entry.arguments.branch).toBeUndefined();
  });

  it("derives push_to_pull_request_branch branch via PR API for issue_comment events", async () => {
    const workspace = makeTempDir("gh-aw-prestage-push-issue-");
    initRepo(workspace, "main");

    const headRef = "feat/issue-comment-branch";
    const eventPath = path.join(workspace, "event.json");
    fs.writeFileSync(
      eventPath,
      JSON.stringify({
        issue: { number: 42, pull_request: { url: "https://api.github.com/repos/owner/repo/pulls/42" } },
      })
    );

    // Spy on global fetch so the call-URL assertion happens on the test side
    // (an assertion failure inside a global-mutating stub is hard to attribute).
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ head: { ref: headRef } }),
    });

    const prevBase = process.env.GH_AW_CUSTOM_BASE_BRANCH;
    const prevEvent = process.env.GITHUB_EVENT_PATH;
    const prevRepo = process.env.GITHUB_REPOSITORY;
    process.env.GH_AW_CUSTOM_BASE_BRANCH = "main";
    process.env.GITHUB_EVENT_PATH = eventPath;
    process.env.GITHUB_REPOSITORY = "owner/repo";
    try {
      const entry = {
        tool: "push_to_pull_request_branch",
        arguments: { message: "Push update" },
        sidecars: { patch: newFileDiff("issue-comment.txt", "from issue comment\n") },
      };
      await preStagePatch(entry, 0, workspace);
      expect(git(["rev-parse", "--abbrev-ref", "HEAD"], workspace).trim()).toBe(headRef);
      expect(fetchSpy).toHaveBeenCalledWith(expect.stringContaining("/repos/owner/repo/pulls/42"), expect.anything());
    } finally {
      fetchSpy.mockRestore();
      if (prevBase === undefined) delete process.env.GH_AW_CUSTOM_BASE_BRANCH;
      else process.env.GH_AW_CUSTOM_BASE_BRANCH = prevBase;
      if (prevEvent === undefined) delete process.env.GITHUB_EVENT_PATH;
      else process.env.GITHUB_EVENT_PATH = prevEvent;
      if (prevRepo === undefined) delete process.env.GITHUB_REPOSITORY;
      else process.env.GITHUB_REPOSITORY = prevRepo;
    }
  });

  it("fails fast for push_to_pull_request_branch when no PR context is available", async () => {
    const workspace = makeTempDir("gh-aw-prestage-push-nopr-");
    initRepo(workspace, "main");

    const prevEvent = process.env.GITHUB_EVENT_PATH;
    delete process.env.GITHUB_EVENT_PATH;
    try {
      const entry = {
        tool: "push_to_pull_request_branch",
        arguments: { message: "Push update" },
        sidecars: { patch: newFileDiff("orphan.txt", "no PR context\n") },
      };
      await expect(preStagePatch(entry, 0, workspace)).rejects.toThrow(/cannot derive pull-request head branch/);
    } finally {
      if (prevEvent !== undefined) process.env.GITHUB_EVENT_PATH = prevEvent;
    }
  });

  it("derives push_to_pull_request_branch branch from workflow_dispatch inputs.pull_request_number", async () => {
    const workspace = makeTempDir("gh-aw-prestage-push-dispatch-input-");
    initRepo(workspace, "main");

    const headRef = "feat/dispatch-pr-number";
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ head: { ref: headRef } }),
    });

    const eventPath = path.join(workspace, "event.json");
    fs.writeFileSync(
      eventPath,
      JSON.stringify({
        inputs: { pull_request_number: "123" },
      })
    );

    const prevBase = process.env.GH_AW_CUSTOM_BASE_BRANCH;
    const prevEvent = process.env.GITHUB_EVENT_PATH;
    const prevRepo = process.env.GITHUB_REPOSITORY;
    process.env.GH_AW_CUSTOM_BASE_BRANCH = "main";
    process.env.GITHUB_EVENT_PATH = eventPath;
    process.env.GITHUB_REPOSITORY = "owner/repo";
    try {
      const entry = {
        tool: "push_to_pull_request_branch",
        arguments: { message: "Push update" },
        sidecars: { patch: newFileDiff("dispatch-input.txt", "via workflow_dispatch input\n") },
      };
      await preStagePatch(entry, 0, workspace);
      expect(git(["rev-parse", "--abbrev-ref", "HEAD"], workspace).trim()).toBe(headRef);
      expect(fetchSpy).toHaveBeenCalledWith(expect.stringContaining("/repos/owner/repo/pulls/123"), expect.anything());
    } finally {
      fetchSpy.mockRestore();
      if (prevBase === undefined) delete process.env.GH_AW_CUSTOM_BASE_BRANCH;
      else process.env.GH_AW_CUSTOM_BASE_BRANCH = prevBase;
      if (prevEvent === undefined) delete process.env.GITHUB_EVENT_PATH;
      else process.env.GITHUB_EVENT_PATH = prevEvent;
      if (prevRepo === undefined) delete process.env.GITHUB_REPOSITORY;
      else process.env.GITHUB_REPOSITORY = prevRepo;
    }
  });

  it("derives push_to_pull_request_branch branch from explicit arguments.pull_request_number when no event payload exists", async () => {
    // Resolution path 3: no GITHUB_EVENT_PATH, but the sample entry carries an
    // explicit `arguments.pull_request_number`. The driver should hit the PR
    // API directly rather than fail fast.
    const workspace = makeTempDir("gh-aw-prestage-push-argpr-");
    initRepo(workspace, "main");

    const headRef = "feat/explicit-pr-number";
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ head: { ref: headRef } }),
    });

    const prevBase = process.env.GH_AW_CUSTOM_BASE_BRANCH;
    const prevEvent = process.env.GITHUB_EVENT_PATH;
    const prevRepo = process.env.GITHUB_REPOSITORY;
    delete process.env.GITHUB_EVENT_PATH; // force path 3
    process.env.GH_AW_CUSTOM_BASE_BRANCH = "main";
    process.env.GITHUB_REPOSITORY = "owner/repo";
    try {
      const entry = {
        tool: "push_to_pull_request_branch",
        arguments: { message: "Push update", pull_request_number: 99 },
        sidecars: { patch: newFileDiff("arg-pr.txt", "via arg pr\n") },
      };
      await preStagePatch(entry, 0, workspace);
      expect(git(["rev-parse", "--abbrev-ref", "HEAD"], workspace).trim()).toBe(headRef);
      expect(fetchSpy).toHaveBeenCalledWith(expect.stringContaining("/repos/owner/repo/pulls/99"), expect.anything());
    } finally {
      fetchSpy.mockRestore();
      if (prevBase === undefined) delete process.env.GH_AW_CUSTOM_BASE_BRANCH;
      else process.env.GH_AW_CUSTOM_BASE_BRANCH = prevBase;
      if (prevEvent !== undefined) process.env.GITHUB_EVENT_PATH = prevEvent;
      if (prevRepo === undefined) delete process.env.GITHUB_REPOSITORY;
      else process.env.GITHUB_REPOSITORY = prevRepo;
    }
  });

  it("prefers explicit arguments.repo over safe-outputs target-repo when deriving PR branch", async () => {
    const workspace = makeTempDir("gh-aw-prestage-push-explicit-repo-");
    initRepo(workspace, "main");

    const headRef = "feat/explicit-repo-pr";
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ head: { ref: headRef } }),
    });

    const configPath = path.join(workspace, "config.json");
    fs.writeFileSync(
      configPath,
      JSON.stringify({
        push_to_pull_request_branch: { "target-repo": "githubnext/gh-aw-side-repo" },
      })
    );

    const prevBase = process.env.GH_AW_CUSTOM_BASE_BRANCH;
    const prevEvent = process.env.GITHUB_EVENT_PATH;
    const prevRepo = process.env.GITHUB_REPOSITORY;
    const prevConfig = process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH;
    delete process.env.GITHUB_EVENT_PATH;
    process.env.GH_AW_CUSTOM_BASE_BRANCH = "main";
    process.env.GITHUB_REPOSITORY = "githubnext/gh-aw-test";
    process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = configPath;
    try {
      const entry = {
        tool: "push_to_pull_request_branch",
        arguments: {
          message: "Push update",
          pull_request_number: 88,
          repo: "owner/explicit-repo",
        },
        sidecars: { patch: newFileDiff("explicit-repo.txt", "explicit repo wins\n") },
      };
      await preStagePatch(entry, 0, workspace);
      expect(git(["rev-parse", "--abbrev-ref", "HEAD"], workspace).trim()).toBe(headRef);
      expect(fetchSpy).toHaveBeenCalledWith(expect.stringContaining("/repos/owner/explicit-repo/pulls/88"), expect.anything());
      expect(fetchSpy).not.toHaveBeenCalledWith(expect.stringContaining("/repos/githubnext/gh-aw-side-repo/pulls/88"), expect.anything());
    } finally {
      fetchSpy.mockRestore();
      if (prevBase === undefined) delete process.env.GH_AW_CUSTOM_BASE_BRANCH;
      else process.env.GH_AW_CUSTOM_BASE_BRANCH = prevBase;
      if (prevEvent !== undefined) process.env.GITHUB_EVENT_PATH = prevEvent;
      if (prevRepo === undefined) delete process.env.GITHUB_REPOSITORY;
      else process.env.GITHUB_REPOSITORY = prevRepo;
      if (prevConfig === undefined) delete process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH;
      else process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = prevConfig;
    }
  });

  it("derives push_to_pull_request_branch branch using target-repo from safe-outputs config (issue #41292 siderepo workflow_dispatch)", async () => {
    // Reproduces the siderepo failure: a workflow_dispatch provides
    // `pull_request_number` but no `repo` override in the sample arguments.
    // The safe-outputs config carries `target-repo: "githubnext/gh-aw-side-repo"`,
    // which derivePrHeadRef must use when building the PR fetch URL.
    const workspace = makeTempDir("gh-aw-prestage-push-siderepo-");
    initRepo(workspace, "main");

    const headRef = "feat/siderepo-pr-branch";
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ head: { ref: headRef } }),
    });

    const configPath = path.join(workspace, "config.json");
    fs.writeFileSync(
      configPath,
      JSON.stringify({
        push_to_pull_request_branch: { "target-repo": "githubnext/gh-aw-side-repo" },
      })
    );

    const prevBase = process.env.GH_AW_CUSTOM_BASE_BRANCH;
    const prevEvent = process.env.GITHUB_EVENT_PATH;
    const prevRepo = process.env.GITHUB_REPOSITORY;
    const prevConfig = process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH;
    delete process.env.GITHUB_EVENT_PATH; // no event; rely on config
    process.env.GH_AW_CUSTOM_BASE_BRANCH = "main";
    process.env.GITHUB_REPOSITORY = "githubnext/gh-aw-test"; // host repo (wrong repo)
    process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = configPath;
    try {
      const entry = {
        tool: "push_to_pull_request_branch",
        // No `repo` override — agent emits only pull_request_number.
        arguments: { message: "Multi-commit test push from Copilot in side repo", pull_request_number: 447 },
        sidecars: { patch: newFileDiff("src/siderepo-feature.py", "# side repo\n") },
      };
      await preStagePatch(entry, 0, workspace);
      expect(git(["rev-parse", "--abbrev-ref", "HEAD"], workspace).trim()).toBe(headRef);
      // Must fetch from the side repo, NOT the host repo.
      expect(fetchSpy).toHaveBeenCalledWith(expect.stringContaining("/repos/githubnext/gh-aw-side-repo/pulls/447"), expect.anything());
      expect(fetchSpy).not.toHaveBeenCalledWith(expect.stringContaining("/repos/githubnext/gh-aw-test/pulls/447"), expect.anything());
    } finally {
      fetchSpy.mockRestore();
      if (prevBase === undefined) delete process.env.GH_AW_CUSTOM_BASE_BRANCH;
      else process.env.GH_AW_CUSTOM_BASE_BRANCH = prevBase;
      if (prevEvent !== undefined) process.env.GITHUB_EVENT_PATH = prevEvent;
      if (prevRepo === undefined) delete process.env.GITHUB_REPOSITORY;
      else process.env.GITHUB_REPOSITORY = prevRepo;
      if (prevConfig === undefined) delete process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH;
      else process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = prevConfig;
    }
  });

  it("derives PR-linked issue payload branch using target-repo from safe-outputs config", async () => {
    const workspace = makeTempDir("gh-aw-prestage-push-issue-siderepo-");
    initRepo(workspace, "main");

    const headRef = "feat/issue-side-repo";
    const eventPath = path.join(workspace, "event.json");
    fs.writeFileSync(
      eventPath,
      JSON.stringify({
        issue: { number: 42, pull_request: { url: "https://api.github.com/repos/githubnext/gh-aw-side-repo/pulls/42" } },
      })
    );

    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ head: { ref: headRef } }),
    });

    const configPath = path.join(workspace, "config.json");
    fs.writeFileSync(
      configPath,
      JSON.stringify({
        push_to_pull_request_branch: { "target-repo": "githubnext/gh-aw-side-repo" },
      })
    );

    const prevBase = process.env.GH_AW_CUSTOM_BASE_BRANCH;
    const prevEvent = process.env.GITHUB_EVENT_PATH;
    const prevRepo = process.env.GITHUB_REPOSITORY;
    const prevConfig = process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH;
    process.env.GH_AW_CUSTOM_BASE_BRANCH = "main";
    process.env.GITHUB_EVENT_PATH = eventPath;
    process.env.GITHUB_REPOSITORY = "githubnext/gh-aw-test";
    process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = configPath;
    try {
      const entry = {
        tool: "push_to_pull_request_branch",
        arguments: { message: "Push update" },
        sidecars: { patch: newFileDiff("issue-side-repo.txt", "issue side repo\n") },
      };
      await preStagePatch(entry, 0, workspace);
      expect(git(["rev-parse", "--abbrev-ref", "HEAD"], workspace).trim()).toBe(headRef);
      expect(fetchSpy).toHaveBeenCalledWith(expect.stringContaining("/repos/githubnext/gh-aw-side-repo/pulls/42"), expect.anything());
      expect(fetchSpy).not.toHaveBeenCalledWith(expect.stringContaining("/repos/githubnext/gh-aw-test/pulls/42"), expect.anything());
    } finally {
      fetchSpy.mockRestore();
      if (prevBase === undefined) delete process.env.GH_AW_CUSTOM_BASE_BRANCH;
      else process.env.GH_AW_CUSTOM_BASE_BRANCH = prevBase;
      if (prevEvent === undefined) delete process.env.GITHUB_EVENT_PATH;
      else process.env.GITHUB_EVENT_PATH = prevEvent;
      if (prevRepo === undefined) delete process.env.GITHUB_REPOSITORY;
      else process.env.GITHUB_REPOSITORY = prevRepo;
      if (prevConfig === undefined) delete process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH;
      else process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = prevConfig;
    }
  });

  it("is a no-op when the sample tool isn't in the patch-sidecar set", async () => {
    // We assert this at the driver level (PATCH_SIDECAR_TOOLS gate in main()),
    // but preStagePatch itself should also be a no-op when called with an
    // entry that has no patch sidecar — protecting against misuse.
    const workspace = makeTempDir("gh-aw-prestage-noop-");
    initRepo(workspace, "main");

    const entry = {
      tool: "create_issue",
      arguments: { title: "x", body: "y" },
    };
    await preStagePatch(entry, 0, workspace);

    // Still on main, no extra commits, no new files.
    expect(git(["rev-parse", "--abbrev-ref", "HEAD"], workspace).trim()).toBe("main");
    const log = git(["log", "--pretty=%s"], workspace).trim().split("\n");
    expect(log).toEqual(["seed"]);
  });
});

describe("apply_samples.cjs sampleResultIsError", () => {
  const { sampleResultIsError } = require("./apply_samples.cjs");

  it("returns false for a successful result (no isError, no error content)", () => {
    const result = { content: [{ type: "text", text: JSON.stringify({ result: "success", id: 1 }) }], isError: false };
    expect(sampleResultIsError(result)).toBe(false);
  });

  it("returns true when isError is true on the result (flag takes precedence)", () => {
    const result = {
      // Content is a success payload — proves the early-return on the isError flag,
      // not the content-text fallback path.
      content: [{ type: "text", text: JSON.stringify({ result: "success", id: 1 }) }],
      isError: true,
    };
    expect(sampleResultIsError(result)).toBe(true);
  });

  it("returns true (defense-in-depth) when content text has result:error but isError is false", () => {
    // This simulates the bug: an older server that forgot to set isError:true.
    const result = {
      content: [{ type: "text", text: JSON.stringify({ result: "error", error: "patch failed", details: "no merge base" }) }],
      isError: false,
    };
    expect(sampleResultIsError(result)).toBe(true);
  });

  it("returns false for non-JSON content text", () => {
    const result = { content: [{ type: "text", text: "plain text, not JSON" }], isError: false };
    expect(sampleResultIsError(result)).toBe(false);
  });

  it("returns false for null result", () => {
    expect(sampleResultIsError(null)).toBe(false);
  });

  it("returns false for empty content array", () => {
    const result = { content: [], isError: false };
    expect(sampleResultIsError(result)).toBe(false);
  });
});
