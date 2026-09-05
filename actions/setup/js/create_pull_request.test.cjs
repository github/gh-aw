// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createRequire } from "module";
import { fileURLToPath } from "url";
import * as fs from "fs";
import * as path from "path";
import * as os from "os";

const require = createRequire(import.meta.url);
const promptsSourceDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../md");

const { getPatchPathForBranch, getPatchPathForBranchInRepo } = require("./git_patch_utils.cjs");
const { getBundlePathForBranch, getBundlePathForBranchInRepo } = require("./generate_git_bundle.cjs");

// The privileged handler derives patch/bundle paths from `branch` (and `repo`)
// via resolveTransportPaths, so tests must write transport files at the
// canonical derived location and let the handler discover them.
//
// `/tmp/gh-aw` is a process-global path shared by every test file. Vitest runs
// test files in parallel processes, so a cleanup that globbed the whole
// directory would delete another file's in-flight transport files mid-test.
// Track only the paths this file created and delete just those.
const createdTransportPaths = new Set();
function canonicalPatchPath(branch, repo) {
  fs.mkdirSync("/tmp/gh-aw", { recursive: true });
  const p = repo ? getPatchPathForBranchInRepo(branch, repo) : getPatchPathForBranch(branch);
  createdTransportPaths.add(p);
  return p;
}
function canonicalBundlePath(branch, repo) {
  fs.mkdirSync("/tmp/gh-aw", { recursive: true });
  const p = repo ? getBundlePathForBranchInRepo(branch, repo) : getBundlePathForBranch(branch);
  createdTransportPaths.add(p);
  return p;
}
function cleanupCanonicalTransports() {
  for (const p of createdTransportPaths) {
    try {
      fs.rmSync(p, { force: true });
    } catch {}
  }
  createdTransportPaths.clear();
}

function copyPromptTemplate(promptsDir, templateName) {
  fs.copyFileSync(path.join(promptsSourceDir, templateName), path.join(promptsDir, templateName));
}

function ensureDefaultDisclosureHeaderPrompt() {
  const promptsDir = path.join(process.env.RUNNER_TEMP || os.tmpdir(), "gh-aw", "prompts");
  fs.mkdirSync(promptsDir, { recursive: true });
  copyPromptTemplate(promptsDir, "safe_outputs_disclosure_header.md");
}

beforeEach(() => {
  cleanupCanonicalTransports();
  ensureDefaultDisclosureHeaderPrompt();
  process.env.GH_AW_PROMPTS_DIR = promptsSourceDir;
});
afterEach(() => {
  cleanupCanonicalTransports();
});

describe("create_pull_request - draft policy enforcement", () => {
  let tempDir;
  let originalEnv;

  beforeEach(() => {
    originalEnv = { ...process.env };
    process.env.GH_AW_WORKFLOW_ID = "test-workflow";
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "create-pr-draft-test-"));

    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };
    global.github = {
      rest: {
        pulls: {
          create: vi.fn().mockResolvedValue({ data: { number: 1, html_url: "https://github.com/test", head: { sha: "abc123" } } }),
          createReview: vi.fn().mockResolvedValue({ data: { id: 77, html_url: "https://github.com/test/review/77" } }),
        },
        repos: {
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
        },
        issues: {
          addLabels: vi.fn().mockResolvedValue({}),
          addAssignees: vi.fn().mockResolvedValue({}),
        },
      },
      graphql: vi.fn(),
    };
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {},
    };
    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockResolvedValue({ exitCode: 0, stdout: "", stderr: "" }),
    };

    // Clear module cache so globals are picked up fresh
    delete require.cache[require.resolve("./create_pull_request.cjs")];
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) {
        delete process.env[key];
      }
    }
    Object.assign(process.env, originalEnv);

    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }

    delete global.core;
    delete global.github;
    delete global.context;
    delete global.exec;
    vi.clearAllMocks();
  });

  /** Returns the `core.warning` calls related to draft config override attempts. */
  function getDraftOverrideWarnings() {
    return global.core.warning.mock.calls.filter(args => String(args[0]).includes("Agent requested draft"));
  }

  it("should enforce draft: false from config even when agent requests draft: true", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ draft: "false", allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body", draft: true }, {});

    expect(result.success).toBe(true);
    expect(global.github.rest.pulls.create).toHaveBeenCalledWith(expect.objectContaining({ draft: false }));
  });

  it("should enforce draft: true from config even when agent requests draft: false", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ draft: "true", allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body", draft: false }, {});

    expect(result.success).toBe(true);
    expect(global.github.rest.pulls.create).toHaveBeenCalledWith(expect.objectContaining({ draft: true }));
  });

  it("should log a warning when agent attempts to override draft config", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ draft: "false", allow_empty: true });

    await handler({ title: "Test PR", body: "Test body", draft: true }, {});

    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("Agent requested draft: true, but configuration enforces draft: false"));
  });

  it("should not log a warning when agent draft matches config", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ draft: "false", allow_empty: true });

    await handler({ title: "Test PR", body: "Test body", draft: false }, {});

    expect(getDraftOverrideWarnings()).toHaveLength(0);
  });

  it("should not log a warning when agent does not specify draft", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ draft: "false", allow_empty: true });

    await handler({ title: "Test PR", body: "Test body" }, {});

    expect(getDraftOverrideWarnings()).toHaveLength(0);
  });
});

describe("parseAutoMergeConfig unit tests", () => {
  let warnSpy;

  beforeEach(() => {
    global.core = {
      warning: vi.fn(),
      info: vi.fn(),
      debug: vi.fn(),
    };
    warnSpy = global.core.warning;
  });

  afterEach(() => {
    delete global.core;
    vi.clearAllMocks();
    delete require.cache[require.resolve("./create_pull_request.cjs")];
  });

  it("treats boolean true as enabled with SQUASH method", () => {
    const { parseAutoMergeConfig } = require("./create_pull_request.cjs");
    expect(parseAutoMergeConfig(true)).toEqual({ enabled: true, mergeMethod: "SQUASH" });
  });

  it('treats string "true" as enabled with SQUASH method', () => {
    const { parseAutoMergeConfig } = require("./create_pull_request.cjs");
    expect(parseAutoMergeConfig("true")).toEqual({ enabled: true, mergeMethod: "SQUASH" });
  });

  it("treats boolean false as disabled", () => {
    const { parseAutoMergeConfig } = require("./create_pull_request.cjs");
    expect(parseAutoMergeConfig(false)).toEqual({ enabled: false });
  });

  it('treats string "false" as disabled', () => {
    const { parseAutoMergeConfig } = require("./create_pull_request.cjs");
    expect(parseAutoMergeConfig("false")).toEqual({ enabled: false });
  });

  it("treats null as disabled", () => {
    const { parseAutoMergeConfig } = require("./create_pull_request.cjs");
    expect(parseAutoMergeConfig(null)).toEqual({ enabled: false });
  });

  it("treats undefined as disabled", () => {
    const { parseAutoMergeConfig } = require("./create_pull_request.cjs");
    expect(parseAutoMergeConfig(undefined)).toEqual({ enabled: false });
  });

  it('treats "squash" as enabled with SQUASH method', () => {
    const { parseAutoMergeConfig } = require("./create_pull_request.cjs");
    expect(parseAutoMergeConfig("squash")).toEqual({ enabled: true, mergeMethod: "SQUASH" });
  });

  it('treats "merge" as enabled with MERGE method', () => {
    const { parseAutoMergeConfig } = require("./create_pull_request.cjs");
    expect(parseAutoMergeConfig("merge")).toEqual({ enabled: true, mergeMethod: "MERGE" });
  });

  it('treats "rebase" as enabled with REBASE method', () => {
    const { parseAutoMergeConfig } = require("./create_pull_request.cjs");
    expect(parseAutoMergeConfig("rebase")).toEqual({ enabled: true, mergeMethod: "REBASE" });
  });

  it("warns and disables auto-merge for an unrecognized string", () => {
    const { parseAutoMergeConfig } = require("./create_pull_request.cjs");
    expect(parseAutoMergeConfig("sqaush")).toEqual({ enabled: false });
    expect(warnSpy).toHaveBeenCalledWith(expect.stringContaining("Unrecognized auto-merge value"));
  });

  it("does not warn for empty string (treated as disabled)", () => {
    const { parseAutoMergeConfig } = require("./create_pull_request.cjs");
    expect(parseAutoMergeConfig("")).toEqual({ enabled: false });
    expect(warnSpy).not.toHaveBeenCalled();
  });
});

describe("create_pull_request - auto-merge configuration", () => {
  let originalEnv;

  beforeEach(() => {
    originalEnv = { ...process.env };
    process.env.GH_AW_WORKFLOW_ID = "test-workflow";
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";

    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };
    global.github = {
      rest: {
        pulls: {
          create: vi.fn().mockResolvedValue({
            data: {
              number: 1,
              node_id: "PR_node_id",
              html_url: "https://github.com/test-owner/test-repo/pull/1",
              head: { sha: "abc123" },
            },
          }),
          createReview: vi.fn().mockResolvedValue({ data: { id: 77 } }),
        },
        repos: {
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
        },
        issues: {
          addLabels: vi.fn().mockResolvedValue({}),
          addAssignees: vi.fn().mockResolvedValue({}),
        },
      },
      graphql: vi.fn().mockResolvedValue({ enablePullRequestAutoMerge: { pullRequest: { id: "PR_node_id" } } }),
    };
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {},
    };
    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockResolvedValue({ exitCode: 0, stdout: "", stderr: "" }),
    };

    delete require.cache[require.resolve("./create_pull_request.cjs")];
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) {
        delete process.env[key];
      }
    }
    Object.assign(process.env, originalEnv);

    delete global.core;
    delete global.github;
    delete global.context;
    delete global.exec;
    vi.clearAllMocks();
  });

  it("passes an explicit auto-merge method to GraphQL", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ auto_merge: "rebase", allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(true);
    expect(global.github.graphql).toHaveBeenCalledWith(expect.stringContaining("enablePullRequestAutoMerge"), {
      prId: "PR_node_id",
      mergeMethod: "REBASE",
    });
  });

  it("defaults to SQUASH when auto-merge is boolean true", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ auto_merge: true, allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(true);
    expect(global.github.graphql).toHaveBeenCalledWith(expect.stringContaining("enablePullRequestAutoMerge"), {
      prId: "PR_node_id",
      mergeMethod: "SQUASH",
    });
  });

  it("warns and disables auto-merge for unrecognized values", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ auto_merge: "sqaush", allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(true);
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("Unrecognized auto-merge value"));
    expect(global.github.graphql).not.toHaveBeenCalledWith(expect.stringContaining("enablePullRequestAutoMerge"), expect.anything());
  });
});

describe("create_pull_request - bundle transport shallow checkout", () => {
  let tempDir;
  let originalEnv;
  let pushSignedSpy;

  beforeEach(() => {
    originalEnv = { ...process.env };
    process.env.GH_AW_WORKFLOW_ID = "test-workflow";
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";
    delete process.env.GITHUB_TOKEN;
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "create-pr-bundle-test-"));

    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };
    global.github = {
      rest: {
        pulls: {
          create: vi.fn().mockResolvedValue({ data: { number: 42, html_url: "https://github.com/test-owner/test-repo/pull/42" } }),
        },
        repos: {
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
        },
        issues: {
          create: vi.fn().mockResolvedValue({ data: { number: 99, html_url: "https://github.com/test-owner/test-repo/issues/99" } }),
          addLabels: vi.fn().mockResolvedValue({}),
        },
      },
      graphql: vi.fn(),
    };
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {},
    };
    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockImplementation((cmd, args) => {
        if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
          return Promise.resolve({ exitCode: 0, stdout: "true\n", stderr: "" });
        }
        if (cmd === "git" && args[0] === "bundle" && args[1] === "verify") {
          // Declare a fake prerequisite so ensureFullHistoryForBundle proceeds.
          return Promise.resolve({ exitCode: 1, stdout: "", stderr: `The bundle requires this ref:\n${"a".repeat(40)}\n` });
        }
        if (cmd === "git" && args[0] === "cat-file" && args[1] === "-e") {
          // Report the prerequisite object as already present by default so
          // ensureFullHistoryForBundle returns early (no fetch). Tests that need
          // to exercise the fetch/deepen path override this within the test.
          return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
        }
        if (cmd === "git" && args[0] === "rev-list") {
          return Promise.resolve({ exitCode: 0, stdout: "1\n", stderr: "" });
        }
        if (cmd === "git" && args && args[0] === "ls-remote") {
          return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
        }
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }),
    };

    const pushSignedCommitsModule = require("./push_signed_commits.cjs");
    pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue("bundle-tip");
    delete require.cache[require.resolve("./create_pull_request.cjs")];
  });

  afterEach(() => {
    if (pushSignedSpy) {
      pushSignedSpy.mockRestore();
    }

    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) {
        delete process.env[key];
      }
    }
    Object.assign(process.env, originalEnv);

    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }

    delete global.core;
    delete global.github;
    delete global.context;
    delete global.exec;
    vi.clearAllMocks();
  });

  it("should fetch bundle prerequisite commits directly from origin in shallow repositories", async () => {
    const patchPath = canonicalPatchPath("feature/test");
    fs.writeFileSync(
      patchPath,
      `From abc123 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

diff --git a/test.txt b/test.txt
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/test.txt
@@ -0,0 +1 @@
+Hello World
--
2.34.1
`
    );
    const bundlePath = canonicalBundlePath("feature/test");
    fs.writeFileSync(bundlePath, "bundle content");

    // Force the prerequisite-missing path so the direct SHA fetch runs.
    const prereq = "a".repeat(40);
    let prereqFetched = false;
    global.exec.getExecOutput = vi.fn().mockImplementation((cmd, args) => {
      if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
        return Promise.resolve({ exitCode: 0, stdout: "true\n", stderr: "" });
      }
      if (cmd === "git" && args[0] === "bundle" && args[1] === "verify") {
        return Promise.resolve({ exitCode: 1, stdout: "", stderr: `The bundle requires this ref:\n${prereq}\n` });
      }
      if (cmd === "git" && args[0] === "config") {
        return Promise.resolve({ exitCode: 1, stdout: "", stderr: "" });
      }
      if (cmd === "git" && args[0] === "cat-file" && args[1] === "-e") {
        // Missing until the direct SHA fetch brings it in.
        return Promise.resolve({ exitCode: prereqFetched ? 0 : 1, stdout: "", stderr: "" });
      }
      if (cmd === "git" && args[0] === "rev-list") {
        return Promise.resolve({ exitCode: 0, stdout: "1\n", stderr: "" });
      }
      return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
    });
    global.exec.exec = vi.fn().mockImplementation((cmd, args) => {
      if (cmd === "git" && Array.isArray(args) && args[0] === "fetch" && args.includes("origin") && args.includes(prereq)) {
        prereqFetched = true;
      }
      return Promise.resolve(0);
    });

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ base_branch: "main", preserve_branch_name: true });
    const result = await handler({ title: "Test PR", body: "Test body", branch: "feature/test" }, {});

    expect(result.success).toBe(true);
    // Initial bundle fetch is now via getExecOutput (with ignoreReturnCode: true) rather than exec,
    // so the bundle fetch appears in getExecOutput.mock.calls.
    const bundleFetchCall = global.exec.getExecOutput.mock.calls.find(([, args]) => Array.isArray(args) && args[0] === "fetch" && args[1] === bundlePath);
    if (!bundleFetchCall) {
      throw new Error("expected bundle fetch call via getExecOutput");
    }
    expect(bundleFetchCall[1][2]).toMatch(/^refs\/heads\/feature\/test:refs\/bundles\/create-pr-feature-test-[a-f0-9]{8}$/);
    const bundleTempRef = bundleFetchCall[1][2].split(":")[1];
    expect(global.exec.exec).toHaveBeenCalledWith("git", ["update-ref", "refs/heads/feature/test", bundleTempRef]);
    expect(global.exec.exec).toHaveBeenCalledWith("git", ["reset", "--hard"]);
    // Primary path: the exact prerequisite SHA is fetched directly from origin,
    // with no broad iterative deepen and no --unshallow.
    const directFetch = global.exec.exec.mock.calls.find(([, args]) => Array.isArray(args) && args[0] === "fetch" && args.includes("origin") && args.includes(prereq));
    expect(directFetch).toBeTruthy();
    const deepenCall = global.exec.exec.mock.calls.find(([, args]) => Array.isArray(args) && args[0] === "fetch" && typeof args[1] === "string" && args[1].startsWith("--deepen="));
    expect(deepenCall).toBeUndefined();
    const unshallowCall = global.exec.exec.mock.calls.find(([, args]) => Array.isArray(args) && args[0] === "fetch" && args.includes("--unshallow"));
    expect(unshallowCall).toBeUndefined();
  });

  it("should pass signed_commits false to bundle pushes", async () => {
    const patchPath = canonicalPatchPath("feature/test");
    fs.writeFileSync(
      patchPath,
      `From abc123 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

diff --git a/test.txt b/test.txt
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/test.txt
@@ -0,0 +1 @@
+Hello World
--
2.34.1
`
    );
    const bundlePath = canonicalBundlePath("feature/test");
    fs.writeFileSync(bundlePath, "bundle content");

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ base_branch: "main", preserve_branch_name: true, signed_commits: false });
    const result = await handler({ title: "Test PR", body: "Test body", branch: "feature/test" }, {});

    expect(result.success).toBe(true);
    expect(pushSignedSpy).toHaveBeenCalledWith(expect.objectContaining({ signedCommits: false }));
  });

  it("should rewrite bundle history to a single commit and retry when signed push rejects merge commits", async () => {
    const patchPath = canonicalPatchPath("feature/test");
    fs.writeFileSync(
      patchPath,
      `From abc123 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

diff --git a/test.txt b/test.txt
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/test.txt
@@ -0,0 +1 @@
+Hello World
--
2.34.1
`
    );
    const bundlePath = canonicalBundlePath("feature/test");
    fs.writeFileSync(bundlePath, "bundle content");

    let revParseHeadCallCount = 0;
    global.exec.getExecOutput.mockImplementation((cmd, args) => {
      if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
        return Promise.resolve({ exitCode: 0, stdout: "true\n", stderr: "" });
      }
      if (cmd === "git" && args[0] === "rev-list") {
        return Promise.resolve({ exitCode: 0, stdout: "1\n", stderr: "" });
      }
      if (cmd === "git" && args[0] === "ls-remote") {
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }
      if (cmd === "git" && args[0] === "rev-parse" && args[1] === "HEAD") {
        revParseHeadCallCount += 1;
        return Promise.resolve({ exitCode: 0, stdout: revParseHeadCallCount === 1 ? "old-head-sha\n" : "new-head-sha\n", stderr: "" });
      }
      if (cmd === "git" && args[0] === "log" && args[1] === "-1" && args[2] === "--format=%s" && args[3] === "HEAD") {
        return Promise.resolve({ exitCode: 0, stdout: "bundle merge headline\n", stderr: "" });
      }
      if (cmd === "git" && args[0] === "diff" && args[1] === "--cached" && args[2] === "--name-only") {
        return Promise.resolve({ exitCode: 0, stdout: "test.txt\n", stderr: "" });
      }
      return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
    });

    pushSignedSpy
      .mockRejectedValueOnce(new Error("pushSignedCommits: refusing unsigned push for branch 'feature/test': merge commit detected. " + "GitHub's createCommitOnBranch GraphQL mutation cannot represent merge commits."))
      .mockResolvedValueOnce("bundle-tip");

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ base_branch: "main", preserve_branch_name: true });
    const result = await handler({ title: "Test PR", body: "Test body", branch: "feature/test" }, {});

    expect(result.success).toBe(true);
    expect(result.fallback_used).not.toBe(true);
    expect(pushSignedSpy).toHaveBeenCalledTimes(2);
    expect(global.exec.exec).toHaveBeenCalledWith("git", ["reset", "--soft", "origin/main"]);
    expect(global.exec.exec).toHaveBeenCalledWith("git", ["commit", "-m", "bundle merge headline"]);
    expect(global.github.rest.issues.create).not.toHaveBeenCalled();
  });

  it("should resolve bundle source ref from list-heads when JSONL branch ref is missing in bundle", async () => {
    const patchPath = canonicalPatchPath("ops-review-may09-2026");
    fs.writeFileSync(
      patchPath,
      `From abc123 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

diff --git a/test.txt b/test.txt
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/test.txt
@@ -0,0 +1 @@
+Hello World
--
2.34.1
`
    );
    const bundlePath = canonicalBundlePath("ops-review-may09-2026");
    fs.writeFileSync(bundlePath, "bundle content");

    global.exec.getExecOutput.mockImplementation((cmd, args, options) => {
      if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
        return Promise.resolve({ exitCode: 0, stdout: "true\n", stderr: "" });
      }
      if (cmd === "git" && args[0] === "rev-list") {
        return Promise.resolve({ exitCode: 0, stdout: "1\n", stderr: "" });
      }
      // Initial bundle fetch via getExecOutput with ignoreReturnCode: the JSONL branch ref is missing
      if (cmd === "git" && args[0] === "fetch" && args[1] === bundlePath && options && options.ignoreReturnCode) {
        return Promise.resolve({ exitCode: 1, stderr: "fatal: couldn't find remote ref refs/heads/ops-review-may09-2026", stdout: "" });
      }
      if (cmd === "git" && args[0] === "bundle" && args[1] === "list-heads" && args[2] === bundlePath) {
        return Promise.resolve({
          exitCode: 0,
          stdout: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa refs/heads/main\naaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa HEAD\n",
          stderr: "",
        });
      }
      if (cmd === "git" && args && args[0] === "ls-remote") {
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }
      return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
    });

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ base_branch: "main", preserve_branch_name: true });
    const result = await handler({ title: "Test PR", body: "Test body", branch: "ops-review-may09-2026" }, {});

    expect(result.success).toBe(true);
    expect(global.exec.getExecOutput).toHaveBeenCalledWith("git", ["bundle", "list-heads", bundlePath]);
    const resolvedFetchCall = global.exec.exec.mock.calls.find(([, args]) => Array.isArray(args) && args[0] === "fetch" && args[1] === bundlePath && args[2].startsWith("refs/heads/main:"));
    if (!resolvedFetchCall) {
      throw new Error("expected resolved bundle fetch call");
    }
    expect(resolvedFetchCall[1][2]).toMatch(/^refs\/heads\/main:refs\/bundles\/create-pr-ops-review-may09-2026-[a-f0-9]{8}$/);
  });

  it("should fall back to HEAD refspec when bundle contains only HEAD (no refs/heads/* entry)", async () => {
    const patchPath = canonicalPatchPath("docs/update-migration-version-2026-05-19-4fe3b9f7f99fc1d6");
    fs.writeFileSync(
      patchPath,
      `From abc123 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

diff --git a/test.txt b/test.txt
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/test.txt
@@ -0,0 +1 @@
+Hello World
--
2.34.1
`
    );
    const bundlePath = canonicalBundlePath("docs/update-migration-version-2026-05-19-4fe3b9f7f99fc1d6");
    fs.writeFileSync(bundlePath, "bundle content");

    global.exec.getExecOutput.mockImplementation((cmd, args, options) => {
      if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
        return Promise.resolve({ exitCode: 0, stdout: "true\n", stderr: "" });
      }
      if (cmd === "git" && args[0] === "rev-list") {
        return Promise.resolve({ exitCode: 0, stdout: "1\n", stderr: "" });
      }
      // Initial bundle fetch (refs/heads/* refspec) fails because the JSONL branch ref is absent
      if (cmd === "git" && args[0] === "fetch" && args[1] === bundlePath && options && options.ignoreReturnCode && typeof args[2] === "string" && args[2].startsWith("refs/heads/")) {
        return Promise.resolve({ exitCode: 1, stderr: "fatal: couldn't find remote ref refs/heads/docs/update-migration-version-2026-05-19-4fe3b9f7f99fc1d6", stdout: "" });
      }
      // HEAD-based bundle fetch (fallback path) succeeds — no prerequisite errors
      if (cmd === "git" && args[0] === "fetch" && args[1] === bundlePath && options && options.ignoreReturnCode && typeof args[2] === "string" && args[2].startsWith("HEAD:")) {
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }
      // Bundle contains only HEAD — no refs/heads/* entry (the bug scenario)
      if (cmd === "git" && args[0] === "bundle" && args[1] === "list-heads" && args[2] === bundlePath) {
        return Promise.resolve({
          exitCode: 0,
          stdout: "ac85f4047717ec43c931d750575f5251c45dc705 HEAD\n",
          stderr: "",
        });
      }
      if (cmd === "git" && args && args[0] === "ls-remote") {
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }
      return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
    });

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ base_branch: "main", preserve_branch_name: true });
    const result = await handler({ title: "Test PR", body: "Test body", branch: "docs/update-migration-version-2026-05-19-4fe3b9f7f99fc1d6" }, {});

    expect(result.success).toBe(true);
    expect(global.exec.getExecOutput).toHaveBeenCalledWith("git", ["bundle", "list-heads", bundlePath]);
    // HEAD-based bundle fetch is now performed via getExecOutput (ignoreReturnCode: true)
    // so the code can distinguish prerequisite errors from other failures.
    const headFetchCall = global.exec.getExecOutput.mock.calls.find(
      ([, args, opts]) => Array.isArray(args) && args[0] === "fetch" && args[1] === bundlePath && typeof args[2] === "string" && args[2].startsWith("HEAD:") && opts && opts.ignoreReturnCode
    );
    if (!headFetchCall) {
      throw new Error("expected HEAD-based bundle fetch call via getExecOutput");
    }
    expect(headFetchCall[1][2]).toMatch(/^HEAD:refs\/bundles\/create-pr-docs-update-migration-version-2026-05-19-4fe3b9f7f99fc1d6-[a-f0-9]{8}$/);
  });

  it("should fetch prerequisite commits from origin and retry when HEAD-only bundle has missing prerequisites (non-main dispatch scenario)", async () => {
    // Simulates: worker was dispatched from a non-main branch; its bundle has only HEAD
    // (no refs/heads/* entry) AND the prerequisite is the feature-branch tip, which is
    // not reachable from the local main-only shallow checkout in safe_outputs.
    // Fix: the fallback HEAD fetch path must do the same prerequisite recovery as the
    // initial fetch path.
    const branchName = "docs/update-migration-version-2026-05-19-4fe3b9f7f99fc1d6";
    const patchPath = canonicalPatchPath(branchName);
    fs.writeFileSync(
      patchPath,
      `From abc123 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

diff --git a/test.txt b/test.txt
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/test.txt
@@ -0,0 +1 @@
+Hello World
--
2.34.1
`
    );
    const bundlePath = canonicalBundlePath(branchName);
    fs.writeFileSync(bundlePath, "bundle content");

    const featureBranchTip = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2";

    global.exec.getExecOutput.mockImplementation((cmd, args, options) => {
      if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
        return Promise.resolve({ exitCode: 0, stdout: "true\n", stderr: "" });
      }
      if (cmd === "git" && args[0] === "rev-list") {
        return Promise.resolve({ exitCode: 0, stdout: "1\n", stderr: "" });
      }
      // Initial bundle fetch (named ref) fails — bundle only has HEAD
      if (cmd === "git" && args[0] === "fetch" && args[1] === bundlePath && options && options.ignoreReturnCode && typeof args[2] === "string" && args[2].startsWith("refs/heads/")) {
        return Promise.resolve({ exitCode: 1, stderr: `fatal: couldn't find remote ref refs/heads/${branchName}`, stdout: "" });
      }
      // HEAD-based bundle fetch fails because prerequisite (feature-branch tip) is missing
      if (cmd === "git" && args[0] === "fetch" && args[1] === bundlePath && options && options.ignoreReturnCode && typeof args[2] === "string" && args[2].startsWith("HEAD:")) {
        return Promise.resolve({ exitCode: 1, stderr: `error: Repository lacks these prerequisite commits:\nerror: ${featureBranchTip}`, stdout: "" });
      }
      // Bundle contains only HEAD — no refs/heads/* entry
      if (cmd === "git" && args[0] === "bundle" && args[1] === "list-heads" && args[2] === bundlePath) {
        return Promise.resolve({
          exitCode: 0,
          stdout: `ac85f4047717ec43c931d750575f5251c45dc705 HEAD\n`,
          stderr: "",
        });
      }
      if (cmd === "git" && args && args[0] === "ls-remote") {
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }
      return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
    });

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ base_branch: "main", preserve_branch_name: true });
    const result = await handler({ title: "Test PR", body: "Test body", branch: branchName }, {});

    expect(result.success).toBe(true);
    // Feature-branch tip prerequisite is fetched from origin
    expect(global.exec.exec).toHaveBeenCalledWith("git", ["fetch", "--filter=blob:none", "origin", featureBranchTip]);
    // After prerequisite recovery, HEAD bundle is retried via exec
    const bundleRetryFetchCalls = global.exec.exec.mock.calls.filter(([, args]) => Array.isArray(args) && args[0] === "fetch" && args[1] === bundlePath && typeof args[2] === "string" && args[2].startsWith("HEAD:"));
    expect(bundleRetryFetchCalls.length).toBe(1);
  });

  it("should include retry context when HEAD-only bundle fetch still fails after prerequisite recovery", async () => {
    const branchName = "docs/update-migration-version-2026-05-19-4fe3b9f7f99fc1d6";
    const patchPath = canonicalPatchPath(branchName);
    fs.writeFileSync(
      patchPath,
      `From abc123 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

diff --git a/test.txt b/test.txt
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/test.txt
@@ -0,0 +1 @@
+Hello World
--
2.34.1
`
    );
    const bundlePath = canonicalBundlePath(branchName);
    fs.writeFileSync(bundlePath, "bundle content");

    const featureBranchTip = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2";

    global.exec.getExecOutput.mockImplementation((cmd, args, options) => {
      if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
        return Promise.resolve({ exitCode: 0, stdout: "true\n", stderr: "" });
      }
      if (cmd === "git" && args[0] === "rev-list") {
        return Promise.resolve({ exitCode: 0, stdout: "1\n", stderr: "" });
      }
      if (cmd === "git" && args[0] === "fetch" && args[1] === bundlePath && options && options.ignoreReturnCode && typeof args[2] === "string" && args[2].startsWith("refs/heads/")) {
        return Promise.resolve({ exitCode: 1, stderr: `fatal: couldn't find remote ref refs/heads/${branchName}`, stdout: "" });
      }
      if (cmd === "git" && args[0] === "fetch" && args[1] === bundlePath && options && options.ignoreReturnCode && typeof args[2] === "string" && args[2].startsWith("HEAD:")) {
        return Promise.resolve({ exitCode: 1, stderr: `error: Repository lacks these prerequisite commits:\nerror: ${featureBranchTip}`, stdout: "" });
      }
      if (cmd === "git" && args[0] === "bundle" && args[1] === "list-heads" && args[2] === bundlePath) {
        return Promise.resolve({
          exitCode: 0,
          stdout: `ac85f4047717ec43c931d750575f5251c45dc705 HEAD\n`,
          stderr: "",
        });
      }
      if (cmd === "git" && args && args[0] === "ls-remote") {
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }
      return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
    });

    global.exec.exec.mockImplementation((cmd, args) => {
      if (cmd === "git" && Array.isArray(args) && args[0] === "fetch" && args[1] === bundlePath && typeof args[2] === "string" && args[2].startsWith("HEAD:")) {
        throw new Error("fatal: failed to read HEAD-only bundle");
      }
      return Promise.resolve(0);
    });

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ base_branch: "main", preserve_branch_name: true });
    const result = await handler({ title: "Test PR", body: "Test body", branch: branchName }, {});

    expect(result.success).toBe(false);
    expect(result.error).toBe("Failed to apply bundle");
    expect(global.core.error).toHaveBeenCalledWith(expect.stringContaining("HEAD bundle fetch failed after fetching 1 prerequisite commit(s): fatal: failed to read HEAD-only bundle"));
  });

  it("should fetch prerequisite commits and retry bundle fetch when prerequisites are missing", async () => {
    const patchPath = canonicalPatchPath("feature/test");
    fs.writeFileSync(
      patchPath,
      `From abc123 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

diff --git a/test.txt b/test.txt
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/test.txt
@@ -0,0 +1 @@
+Hello World
--
2.34.1
`
    );
    const bundlePath = canonicalBundlePath("feature/test");
    fs.writeFileSync(bundlePath, "bundle content");

    const missingSha = "256f08b38d9ce40cfa5d46385551caba8642a9df";
    // The initial bundle fetch uses getExecOutput (ignoreReturnCode: true) so git stderr is captured.
    // Real @actions/exec.exec only throws "The process '...' failed with exit code 1" — not the
    // git error text — so the recovery path must read stderr from getExecOutput instead.
    global.exec.getExecOutput.mockImplementation((cmd, args, options) => {
      if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
        return Promise.resolve({ exitCode: 0, stdout: "true\n", stderr: "" });
      }
      if (cmd === "git" && args[0] === "rev-list") {
        return Promise.resolve({ exitCode: 0, stdout: "1\n", stderr: "" });
      }
      if (cmd === "git" && args[0] === "fetch" && args[1] === bundlePath && options && options.ignoreReturnCode) {
        return Promise.resolve({ exitCode: 1, stderr: `error: Repository lacks these prerequisite commits:\nerror: ${missingSha}`, stdout: "" });
      }
      if (cmd === "git" && args && args[0] === "ls-remote") {
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }
      return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
    });

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ base_branch: "main", preserve_branch_name: true });
    const result = await handler({ title: "Test PR", body: "Test body", branch: "feature/test" }, {});

    expect(result.success).toBe(true);
    // Prerequisites are fetched from origin via exec with --filter=blob:none to avoid downloading blobs
    expect(global.exec.exec).toHaveBeenCalledWith("git", ["fetch", "--filter=blob:none", "origin", missingSha]);
    // Retry bundle fetch is via exec (only the retry, not the initial attempt which was getExecOutput)
    const bundleRetryFetchCalls = global.exec.exec.mock.calls.filter(([, args]) => Array.isArray(args) && args[0] === "fetch" && args[1] === bundlePath);
    expect(bundleRetryFetchCalls.length).toBe(1);
    expect(global.exec.getExecOutput).not.toHaveBeenCalledWith("git", ["bundle", "list-heads", bundlePath]);
  });

  it("should fetch all prerequisite commits in a single origin fetch and retry bundle fetch", async () => {
    const patchPath = canonicalPatchPath("feature/test");
    fs.writeFileSync(
      patchPath,
      `From abc123 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

diff --git a/test.txt b/test.txt
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/test.txt
@@ -0,0 +1 @@
+Hello World
--
2.34.1
`
    );
    const bundlePath = canonicalBundlePath("feature/test");
    fs.writeFileSync(bundlePath, "bundle content");

    const missingSha1 = "256f08b38d9ce40cfa5d46385551caba8642a9df";
    const missingSha2 = "aabbccddee1122334455667788990011aabbccdd";
    global.exec.getExecOutput.mockImplementation((cmd, args, options) => {
      if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
        return Promise.resolve({ exitCode: 0, stdout: "true\n", stderr: "" });
      }
      if (cmd === "git" && args[0] === "rev-list") {
        return Promise.resolve({ exitCode: 0, stdout: "1\n", stderr: "" });
      }
      if (cmd === "git" && args[0] === "fetch" && args[1] === bundlePath && options && options.ignoreReturnCode) {
        return Promise.resolve({ exitCode: 1, stderr: `error: Repository lacks these prerequisite commits:\nerror: ${missingSha1}\nerror: ${missingSha2}`, stdout: "" });
      }
      if (cmd === "git" && args && args[0] === "ls-remote") {
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }
      return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
    });

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ base_branch: "main", preserve_branch_name: true });
    const result = await handler({ title: "Test PR", body: "Test body", branch: "feature/test" }, {});

    expect(result.success).toBe(true);
    expect(global.exec.exec).toHaveBeenCalledWith("git", ["fetch", "--filter=blob:none", "origin", missingSha1, missingSha2]);
    const bundleRetryFetchCalls = global.exec.exec.mock.calls.filter(([, args]) => Array.isArray(args) && args[0] === "fetch" && args[1] === bundlePath);
    expect(bundleRetryFetchCalls.length).toBe(1);
    expect(global.exec.getExecOutput).not.toHaveBeenCalledWith("git", ["bundle", "list-heads", bundlePath]);
  });

  it("should fail when fetching prerequisite commits from origin fails", async () => {
    const patchPath = canonicalPatchPath("feature/test");
    fs.writeFileSync(
      patchPath,
      `From abc123 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

diff --git a/test.txt b/test.txt
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/test.txt
@@ -0,0 +1 @@
+Hello World
--
2.34.1
`
    );
    const bundlePath = canonicalBundlePath("feature/test");
    fs.writeFileSync(bundlePath, "bundle content");

    const missingSha = "256f08b38d9ce40cfa5d46385551caba8642a9df";
    global.exec.getExecOutput.mockImplementation((cmd, args, options) => {
      if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
        return Promise.resolve({ exitCode: 0, stdout: "true\n", stderr: "" });
      }
      if (cmd === "git" && args[0] === "rev-list") {
        return Promise.resolve({ exitCode: 0, stdout: "1\n", stderr: "" });
      }
      if (cmd === "git" && args[0] === "fetch" && args[1] === bundlePath && options && options.ignoreReturnCode) {
        return Promise.resolve({ exitCode: 1, stderr: `error: Repository lacks these prerequisite commits:\nerror: ${missingSha}`, stdout: "" });
      }
      if (cmd === "git" && args && args[0] === "ls-remote") {
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }
      return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
    });
    global.exec.exec.mockImplementation((cmd, args) => {
      if (cmd === "git" && Array.isArray(args) && args[0] === "fetch" && args.includes("origin") && args.includes(missingSha)) {
        throw new Error("fatal: couldn't connect to 'origin'");
      }
      return Promise.resolve(0);
    });

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ base_branch: "main", preserve_branch_name: true });
    const result = await handler({ title: "Test PR", body: "Test body", branch: "feature/test" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toBe("Failed to apply bundle");
    expect(global.core.error).toHaveBeenCalledWith(expect.stringContaining("Failed to apply bundle: fatal: couldn't connect to 'origin'"));
    // No retry bundle fetch via exec — failed at prerequisite origin fetch
    const bundleRetryFetchCalls = global.exec.exec.mock.calls.filter(([, args]) => Array.isArray(args) && args[0] === "fetch" && args[1] === bundlePath);
    expect(bundleRetryFetchCalls.length).toBe(0);
  });

  it("should include retry context when bundle fetch still fails after prerequisite recovery", async () => {
    const patchPath = canonicalPatchPath("feature/test");
    fs.writeFileSync(
      patchPath,
      `From abc123 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

diff --git a/test.txt b/test.txt
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/test.txt
@@ -0,0 +1 @@
+Hello World
--
2.34.1
`
    );
    const bundlePath = canonicalBundlePath("feature/test");
    fs.writeFileSync(bundlePath, "bundle content");

    const missingSha = "256f08b38d9ce40cfa5d46385551caba8642a9df";
    global.exec.getExecOutput.mockImplementation((cmd, args, options) => {
      if (cmd === "git" && args[0] === "rev-parse" && args[1] === "--is-shallow-repository") {
        return Promise.resolve({ exitCode: 0, stdout: "true\n", stderr: "" });
      }
      if (cmd === "git" && args[0] === "rev-list") {
        return Promise.resolve({ exitCode: 0, stdout: "1\n", stderr: "" });
      }
      if (cmd === "git" && args[0] === "fetch" && args[1] === bundlePath && options && options.ignoreReturnCode) {
        return Promise.resolve({ exitCode: 1, stderr: `error: Repository lacks these prerequisite commits:\nerror: ${missingSha}`, stdout: "" });
      }
      if (cmd === "git" && args && args[0] === "ls-remote") {
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }
      return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
    });
    global.exec.exec.mockImplementation((cmd, args) => {
      if (cmd === "git" && Array.isArray(args) && args[0] === "fetch" && args[1] === bundlePath) {
        throw new Error("fatal: failed to read bundle");
      }
      return Promise.resolve(0);
    });

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ base_branch: "main", preserve_branch_name: true });
    const result = await handler({ title: "Test PR", body: "Test body", branch: "feature/test" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toBe("Failed to apply bundle");
    expect(global.core.error).toHaveBeenCalledWith(expect.stringContaining("Bundle fetch failed after fetching 1 prerequisite commit(s): fatal: failed to read bundle"));
    const bundleRetryFetchCalls = global.exec.exec.mock.calls.filter(([, args]) => Array.isArray(args) && args[0] === "fetch" && args[1] === bundlePath);
    expect(bundleRetryFetchCalls.length).toBe(1);
  });

  it("should not fetch a bundle directly into the target branch", async () => {
    const patchPath = canonicalPatchPath("autoloop/perf-comparison");
    fs.writeFileSync(
      patchPath,
      `From abc123 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

diff --git a/test.txt b/test.txt
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/test.txt
@@ -0,0 +1 @@
+Hello World
--
2.34.1
`
    );
    const bundlePath = canonicalBundlePath("autoloop/perf-comparison");
    fs.writeFileSync(bundlePath, "bundle content");

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ base_branch: "main", preserve_branch_name: true });
    const result = await handler({ title: "Test PR", body: "Test body\n\nCloses #57\nResolves test-owner/test-repo#58", branch: "autoloop/perf-comparison" }, {});

    expect(result.success).toBe(true);
    // The initial bundle fetch uses getExecOutput (not exec.exec) — ensure it never uses the direct branch refspec
    expect(global.exec.getExecOutput).not.toHaveBeenCalledWith("git", ["fetch", bundlePath, "refs/heads/autoloop/perf-comparison:refs/heads/autoloop/perf-comparison"], expect.anything());
    const bundleFetchCall = global.exec.getExecOutput.mock.calls.find(([, args]) => Array.isArray(args) && args[0] === "fetch" && args[1] === bundlePath);
    if (!bundleFetchCall) {
      throw new Error("expected bundle fetch call");
    }
    expect(bundleFetchCall[1][2]).toMatch(/^refs\/heads\/autoloop\/perf-comparison:refs\/bundles\/create-pr-autoloop-perf-comparison-[a-f0-9]{8}$/);
    const bundleTempRef = bundleFetchCall[1][2].split(":")[1];
    expect(global.exec.exec).toHaveBeenCalledWith("git", ["update-ref", "refs/heads/autoloop/perf-comparison", bundleTempRef]);
  });

  it("should give fallback issue bundle instructions that avoid direct branch fetches", async () => {
    const patchPath = canonicalPatchPath("autoloop/perf-comparison");
    fs.writeFileSync(
      patchPath,
      `From abc123 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

diff --git a/test.txt b/test.txt
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/test.txt
@@ -0,0 +1 @@
+Hello World
--
2.34.1
`
    );
    const bundlePath = canonicalBundlePath("autoloop/perf-comparison");
    fs.writeFileSync(bundlePath, "bundle content");
    pushSignedSpy.mockRejectedValueOnce(new Error("push rejected"));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ base_branch: "main", preserve_branch_name: true });
    const result = await handler({ title: "Test PR", body: "Test body\n\nCloses #57\nResolves test-owner/test-repo#58", branch: "autoloop/perf-comparison" }, {});

    expect(result.success).toBe(true);
    expect(result.fallback_used).toBe(true);

    const fallbackIssueBody = global.github.rest.issues.create.mock.calls[0][0].body;
    const tempRefMatch = fallbackIssueBody.match(/temp_ref='(refs\/bundles\/create-pr-autoloop-perf-comparison-[a-f0-9]{8})'/);
    if (!tempRefMatch?.[1]) {
      throw new Error("expected fallback bundle temp ref");
    }
    const fallbackBundleTempRef = tempRefMatch[1];
    expect(fallbackIssueBody).toContain("target_ref='refs/heads/autoloop/perf-comparison'");
    expect(fallbackIssueBody).toContain('git update-ref "$target_ref" "$temp_ref"');
    expect(fallbackIssueBody).toContain("git reset --hard");
    expect(fallbackIssueBody).toContain('git update-ref -d "$temp_ref"');
    expect(fallbackIssueBody).toContain(fallbackBundleTempRef);
    expect(fallbackIssueBody).not.toContain("refs/heads/autoloop/perf-comparison:refs/heads/autoloop/perf-comparison");
    expect(fallbackIssueBody).toContain("**Original error:** push rejected");
    expect(fallbackIssueBody).toContain("Test body");
    expect(fallbackIssueBody).toContain("Closes \\#57");
    expect(fallbackIssueBody).toContain("Resolves test-owner/test-repo\\#58");
    expect(fallbackIssueBody).not.toContain("Closes #57");
    expect(fallbackIssueBody).not.toContain("Resolves test-owner/test-repo#58");
  });

  it("should include original error in fallback issue patch instructions when push fails (no bundle)", async () => {
    const patchPath = canonicalPatchPath("autoloop/perf-comparison");
    fs.writeFileSync(
      patchPath,
      `From abc123 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

diff --git a/test.txt b/test.txt
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/test.txt
@@ -0,0 +1 @@
+Hello World
--
2.34.1
`
    );
    // No bundle file - forces the patch transport fallback path
    pushSignedSpy.mockRejectedValueOnce(new Error("push rejected"));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ base_branch: "main", preserve_branch_name: true });
    const result = await handler({ title: "Test PR", body: "Test body\n\nCloses #57\nResolves test-owner/test-repo#58", branch: "autoloop/perf-comparison" }, {});

    expect(result.success).toBe(true);
    expect(result.fallback_used).toBe(true);

    const fallbackIssueBody = global.github.rest.issues.create.mock.calls[0][0].body;
    expect(fallbackIssueBody).toContain("**Original error:** push rejected");
    expect(fallbackIssueBody).toContain("git am --3way");
    expect(fallbackIssueBody).not.toContain("git update-ref");
    expect(fallbackIssueBody).toContain("Test body");
    expect(fallbackIssueBody).toContain("Closes \\#57");
    expect(fallbackIssueBody).toContain("Resolves test-owner/test-repo\\#58");
    expect(fallbackIssueBody).not.toContain("Closes #57");
    expect(fallbackIssueBody).not.toContain("Resolves test-owner/test-repo#58");
  });
});

describe("create_pull_request - fallback-as-issue configuration", () => {
  describe("configuration parsing", () => {
    it("should default fallback_as_issue to true when not specified", () => {
      const config = {};
      const fallbackAsIssue = config.fallback_as_issue !== false;

      expect(fallbackAsIssue).toBe(true);
    });

    it("should respect fallback_as_issue when set to false", () => {
      const config = { fallback_as_issue: false };
      const fallbackAsIssue = config.fallback_as_issue !== false;

      expect(fallbackAsIssue).toBe(false);
    });

    it("should respect fallback_as_issue when explicitly set to true", () => {
      const config = { fallback_as_issue: true };
      const fallbackAsIssue = config.fallback_as_issue !== false;

      expect(fallbackAsIssue).toBe(true);
    });
  });

  describe("error type documentation", () => {
    it("should document expected error types", () => {
      // This test documents the expected error types for different failure scenarios
      const errorTypes = {
        push_failed: "Used when git push operation fails and fallback-as-issue is false",
        pr_creation_failed: "Used when PR creation fails (except permission errors) and fallback-as-issue is false",
        permission_denied: "Used when GitHub Actions lacks permission to create/approve PRs AND fallback issue creation also fails",
      };

      // Verify the error types are documented
      expect(errorTypes.push_failed).toBeDefined();
      expect(errorTypes.pr_creation_failed).toBeDefined();
      expect(errorTypes.permission_denied).toBeDefined();

      // These error types should be returned in the corresponding code paths:
      // - push failure with fallback disabled: error_type: "push_failed"
      // - PR creation failure with fallback disabled: error_type: "pr_creation_failed"
      // - Permission error with successful fallback issue: success=true, fallback_used=true
      // - Permission error when fallback issue also fails: error_type: "permission_denied"
    });
  });
});

describe("create_pull_request - auto-close-issue configuration", () => {
  let tempDir;
  let originalEnv;

  beforeEach(() => {
    originalEnv = { ...process.env };
    process.env.GH_AW_WORKFLOW_ID = "test-workflow";
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "create-pr-auto-close-test-"));

    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };
    global.github = {
      rest: {
        pulls: {
          create: vi.fn().mockResolvedValue({ data: { number: 1, html_url: "https://github.com/test", head: { sha: "abc123" } } }),
          createReview: vi.fn().mockResolvedValue({ data: { id: 77, html_url: "https://github.com/test/review/77" } }),
        },
        repos: {
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
        },
        issues: {
          addLabels: vi.fn().mockResolvedValue({}),
          addAssignees: vi.fn().mockResolvedValue({}),
        },
      },
      graphql: vi.fn(),
    };
    global.context = {
      eventName: "issues",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {
        issue: { number: 42 },
      },
    };
    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockResolvedValue({ exitCode: 0, stdout: "", stderr: "" }),
    };

    delete require.cache[require.resolve("./create_pull_request.cjs")];
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) {
        delete process.env[key];
      }
    }
    Object.assign(process.env, originalEnv);

    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }

    delete global.core;
    delete global.github;
    delete global.context;
    delete global.exec;
    vi.clearAllMocks();
  });

  it("should auto-add 'Fixes #N' when triggered from an issue and auto_close_issue is not set (default)", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });

    await handler({ title: "Test PR", body: "Test body" }, {});

    const createCall = global.github.rest.pulls.create.mock.calls[0]?.[0];
    expect(createCall?.body).toContain("Fixes #42");
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('Auto-added "Fixes #42"'));
  });

  it("should auto-add 'Fixes #N' when triggered from an issue and auto_close_issue is explicitly true", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true, auto_close_issue: true });

    await handler({ title: "Test PR", body: "Test body" }, {});

    const createCall = global.github.rest.pulls.create.mock.calls[0]?.[0];
    expect(createCall?.body).toContain("Fixes #42");
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('Auto-added "Fixes #42"'));
  });

  it("should NOT auto-add 'Fixes #N' when auto_close_issue is false", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true, auto_close_issue: false });

    await handler({ title: "Test PR", body: "Test body" }, {});

    const createCall = global.github.rest.pulls.create.mock.calls[0]?.[0];
    expect(createCall?.body).not.toContain("Fixes #42");
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("Skipping auto-close keyword for #42 (auto-close-issue: false)"));
  });

  it("should NOT auto-add 'Fixes #N' when body already contains a closing keyword, regardless of auto_close_issue", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });

    await handler({ title: "Test PR", body: "Test body\n\nCloses #42" }, {});

    const createCall = global.github.rest.pulls.create.mock.calls[0]?.[0];
    // Should not duplicate the keyword
    const fixesCount = (createCall?.body?.match(/Fixes #42/gi) || []).length;
    const closesCount = (createCall?.body?.match(/Closes #42/gi) || []).length;
    expect(closesCount).toBe(1);
    expect(fixesCount).toBe(0);
  });

  it("should have no effect when not triggered from an issue, regardless of auto_close_issue value", async () => {
    // Override context to not be from an issue
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {},
    };
    delete require.cache[require.resolve("./create_pull_request.cjs")];

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true, auto_close_issue: true });

    await handler({ title: "Test PR", body: "Test body" }, {});

    const createCall = global.github.rest.pulls.create.mock.calls[0]?.[0];
    expect(createCall?.body).not.toContain("Fixes #");
  });

  it("should NOT add 'Fixes #N' when auto_close_issue is false even if body has no closing keyword", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true, auto_close_issue: false });

    await handler({ title: "Test PR", body: "Investigation findings - partial work only" }, {});

    const createCall = global.github.rest.pulls.create.mock.calls[0]?.[0];
    expect(createCall?.body).not.toContain("Fixes #");
    expect(createCall?.body).not.toContain("Closes #");
    expect(createCall?.body).not.toContain("Resolves #");
  });
});

describe("create_pull_request - max limit enforcement", () => {
  let mockFs;

  beforeEach(() => {
    // Mock fs module for patch reading
    mockFs = {
      existsSync: vi.fn().mockReturnValue(true),
      readFileSync: vi.fn(),
    };
  });

  it("should enforce max file limit on patch content", () => {
    // Create a patch with more than MAX_FILES (100) unique files
    const patchLines = [];
    for (let i = 0; i < 101; i++) {
      patchLines.push(`diff --git a/file${i}.txt b/file${i}.txt`);
      patchLines.push("index 1234567..abcdefg 100644");
      patchLines.push(`--- a/file${i}.txt`);
      patchLines.push(`+++ b/file${i}.txt`);
      patchLines.push("@@ -1,1 +1,1 @@");
      patchLines.push("-old content");
      patchLines.push("+new content");
    }
    const patchContent = patchLines.join("\n");

    // Import the enforcement function
    const { enforcePullRequestLimits } = require("./create_pull_request.cjs");

    // Should throw E003 error
    expect(() => enforcePullRequestLimits(patchContent)).toThrow("E003");
    expect(() => enforcePullRequestLimits(patchContent)).toThrow("Cannot create pull request with more than 100 files");
    expect(() => enforcePullRequestLimits(patchContent)).toThrow("received 101");
    expect(() => enforcePullRequestLimits(patchContent)).toThrow("max-patch-files");
  });

  it("should allow patches under the file limit", () => {
    // Create a patch with exactly MAX_FILES (100) files
    const patchLines = [];
    for (let i = 0; i < 100; i++) {
      patchLines.push(`diff --git a/file${i}.txt b/file${i}.txt`);
      patchLines.push("index 1234567..abcdefg 100644");
      patchLines.push(`--- a/file${i}.txt`);
      patchLines.push(`+++ b/file${i}.txt`);
      patchLines.push("@@ -1,1 +1,1 @@");
      patchLines.push("-old content");
      patchLines.push("+new content");
    }
    const patchContent = patchLines.join("\n");

    const { enforcePullRequestLimits } = require("./create_pull_request.cjs");

    // Should not throw
    expect(() => enforcePullRequestLimits(patchContent)).not.toThrow();
  });

  it("should count unique files across multi-commit patches (regression: long-running branch with multi-commit patches)", () => {
    // Simulate `git format-patch` output where the same files are modified across
    // multiple commits. Previously the limit check counted every `diff --git`
    // header (3 commits * 2 files = 6) instead of the 2 unique files actually
    // touched. After the fix it should count 2.
    const { countUniquePatchFiles, enforcePullRequestLimits } = require("./create_pull_request.cjs");

    const patchLines = [];
    for (let commit = 0; commit < 3; commit++) {
      for (const file of ["src/a.ts", "src/b.ts"]) {
        patchLines.push(`diff --git a/${file} b/${file}`);
        patchLines.push("index 1234567..abcdefg 100644");
        patchLines.push(`--- a/${file}`);
        patchLines.push(`+++ b/${file}`);
        patchLines.push("@@ -1,1 +1,1 @@");
        patchLines.push(`-old ${commit}`);
        patchLines.push(`+new ${commit}`);
      }
    }
    const patchContent = patchLines.join("\n");

    expect(countUniquePatchFiles(patchContent)).toBe(2);
    expect(() => enforcePullRequestLimits(patchContent, 5)).not.toThrow();
  });

  it("should accept a configurable max-files override", () => {
    const { enforcePullRequestLimits } = require("./create_pull_request.cjs");

    const patchLines = [];
    for (let i = 0; i < 150; i++) {
      patchLines.push(`diff --git a/file${i}.txt b/file${i}.txt`);
      patchLines.push("index 1234567..abcdefg 100644");
      patchLines.push(`--- a/file${i}.txt`);
      patchLines.push(`+++ b/file${i}.txt`);
      patchLines.push("@@ -1,1 +1,1 @@");
      patchLines.push("-old content");
      patchLines.push("+new content");
    }
    const patchContent = patchLines.join("\n");

    // With default limit (100) it should fail
    expect(() => enforcePullRequestLimits(patchContent)).toThrow("E003");
    // With raised limit it should pass
    expect(() => enforcePullRequestLimits(patchContent, 200)).not.toThrow();
    // With smaller limit, error message reflects override
    expect(() => enforcePullRequestLimits(patchContent, 50)).toThrow("more than 50 files");
    expect(() => enforcePullRequestLimits(patchContent, 50)).toThrow("received 150");
  });

  it("should return 0 for empty patches", () => {
    const { countUniquePatchFiles, enforcePullRequestLimits } = require("./create_pull_request.cjs");
    expect(countUniquePatchFiles("")).toBe(0);
    expect(countUniquePatchFiles("   \n\n")).toBe(0);
    expect(() => enforcePullRequestLimits("")).not.toThrow();
  });

  it("should handle quoted paths with C-style escapes", () => {
    // git emits quoted, escaped headers when filenames contain special chars
    // (e.g. embedded quotes or backslashes). The parser must treat these as
    // distinct unique files and never undercount.
    const { countUniquePatchFiles, parseDiffGitHeader } = require("./create_pull_request.cjs");

    // Embedded escaped quote: "a/foo\"bar" "b/foo\"bar"
    expect(parseDiffGitHeader('diff --git "a/foo\\"bar" "b/foo\\"bar"')).toBe('foo\\"bar');
    // Embedded backslash: "a/foo\\bar" "b/foo\\bar"
    expect(parseDiffGitHeader('diff --git "a/foo\\\\bar" "b/foo\\\\bar"')).toBe("foo\\\\bar");
    // Plain unquoted form
    expect(parseDiffGitHeader("diff --git a/foo.txt b/foo.txt")).toBe("foo.txt");
    // Path with spaces (git always emits quoted form when path contains spaces)
    expect(parseDiffGitHeader('diff --git "a/dir/with space/x" "b/dir/with space/x"')).toBe("dir/with space/x");
    // CRLF line ending should not leak trailing carriage-return into path
    expect(parseDiffGitHeader("diff --git a/crlf.txt b/crlf.txt\r")).toBe("crlf.txt");

    // A patch with three different quoted/escaped files should count as 3.
    const patch = [
      'diff --git "a/foo\\"bar" "b/foo\\"bar"',
      "index 1234567..abcdefg 100644",
      "--- a/x",
      "+++ b/x",
      "@@ -1 +1 @@",
      "-old",
      "+new",
      'diff --git "a/foo\\\\bar" "b/foo\\\\bar"',
      "index 1234567..abcdefg 100644",
      "--- a/x",
      "+++ b/x",
      "@@ -1 +1 @@",
      "-old",
      "+new",
      "diff --git a/normal.txt b/normal.txt",
      "index 1234567..abcdefg 100644",
      "--- a/x",
      "+++ b/x",
      "@@ -1 +1 @@",
      "-old",
      "+new",
    ].join("\n");
    expect(countUniquePatchFiles(patch)).toBe(3);
  });

  it("should count unparseable headers conservatively (never undercount)", () => {
    // Each `diff --git` header that cannot be parsed contributes one entry
    // to the unique-file set, so a malformed header can never silently
    // bypass the limit.
    const { countUniquePatchFiles, enforcePullRequestLimits } = require("./create_pull_request.cjs");
    const patchContent = "diff --git \ndiff --git \ndiff --git \n";
    expect(countUniquePatchFiles(patchContent)).toBe(3);

    // Mixed: 2 parseable + 2 unparseable = 4 unique entries.
    const mixed = ["diff --git a/a.txt b/a.txt", 'diff --git "a/missing b/missing', "diff --git b/b.txt c/b.txt", "diff --git "].join("\n");
    expect(countUniquePatchFiles(mixed)).toBe(4);

    // 200 unparseable headers must still trigger the default 100-file limit.
    const lots = Array.from({ length: 200 }, () => "diff --git ").join("\n");
    expect(() => enforcePullRequestLimits(lots)).toThrow("E003");
  });
});

describe("create_pull_request - security: branch name sanitization", () => {
  it("should sanitize branch names with shell metacharacters", () => {
    const { normalizeBranchName } = require("./normalize_branch_name.cjs");

    // Test shell injection attempts - forward slashes and dots are valid in git branch names
    const dangerousNames = [
      { input: "feature; rm -rf /", expected: "feature-rm-rf-/" },
      { input: "branch$(malicious)", expected: "branch-malicious" },
      { input: "branch`backdoor`", expected: "branch-backdoor" },
      { input: "branch| curl evil.com", expected: "branch-curl-evil.com" },
      { input: "branch && echo hacked", expected: "branch-echo-hacked" },
      { input: "branch || evil", expected: "branch-evil" },
      { input: "branch > /etc/passwd", expected: "branch-/etc/passwd" },
      { input: "branch < input.txt", expected: "branch-input.txt" },
      { input: "branch\x00null", expected: "branch-null" }, // Actual null byte, not escaped string
      { input: "branch\\x00null", expected: "branch-x00null" }, // Escaped string representation
    ];

    for (const { input, expected } of dangerousNames) {
      const result = normalizeBranchName(input);
      expect(result).toBe(expected);
      // Verify dangerous shell metacharacters are removed
      expect(result).not.toContain(";");
      expect(result).not.toContain("$");
      expect(result).not.toContain("`");
      expect(result).not.toContain("|");
      expect(result).not.toContain("&");
      expect(result).not.toContain(">");
      expect(result).not.toContain("<");
      expect(result).not.toContain("\x00"); // Actual null byte
      expect(result).not.toContain("\\x00"); // Escaped string
    }
  });

  it("should sanitize branch names with newlines and control characters", () => {
    const { normalizeBranchName } = require("./normalize_branch_name.cjs");

    const controlCharNames = [
      { input: "branch\nwith\nnewlines", expected: "branch-with-newlines" },
      { input: "branch\rwith\rcarriage", expected: "branch-with-carriage" },
      { input: "branch\twith\ttabs", expected: "branch-with-tabs" },
      { input: "branch\x1b[31mwith\x1b[0mescapes", expected: "branch-31mwith-0mescapes" },
    ];

    for (const { input, expected } of controlCharNames) {
      const result = normalizeBranchName(input);
      expect(result).toBe(expected);
      expect(result).not.toContain("\n");
      expect(result).not.toContain("\r");
      expect(result).not.toContain("\t");
      expect(result).not.toMatch(/\x1b/);
    }
  });

  it("should sanitize branch names with spaces and special characters", () => {
    const { normalizeBranchName } = require("./normalize_branch_name.cjs");

    const specialCharNames = [
      { input: "branch with spaces", expected: "branch-with-spaces" },
      { input: "branch!@#$%^&*()", expected: "branch" },
      { input: "branch[brackets]", expected: "branch-brackets" },
      { input: "branch{braces}", expected: "branch-braces" },
      { input: "branch:colon", expected: "branch-colon" },
      { input: 'branch"quotes"', expected: "branch-quotes" },
      { input: "branch'single'quotes", expected: "branch-single-quotes" },
    ];

    for (const { input, expected } of specialCharNames) {
      const result = normalizeBranchName(input);
      expect(result).toBe(expected);
    }
  });

  it("should preserve valid branch name characters", () => {
    const { normalizeBranchName } = require("./normalize_branch_name.cjs");

    const validNames = [
      { input: "feature/my-branch_v1.0", expected: "feature/my-branch_v1.0" },
      { input: "hotfix-123", expected: "hotfix-123" },
      { input: "release_v2.0.0", expected: "release_v2.0.0" },
    ];

    for (const { input, expected } of validNames) {
      const result = normalizeBranchName(input);
      expect(result).toBe(expected);
    }
  });

  it("should handle empty strings after sanitization", () => {
    const { normalizeBranchName } = require("./normalize_branch_name.cjs");

    // Branch names that become empty after sanitization
    const emptyAfterSanitization = ["!@#$%^&*()", ";;;", "|||", "---"];

    for (const input of emptyAfterSanitization) {
      const result = normalizeBranchName(input);
      expect(result).toBe("");
    }
  });

  it("should truncate long branch names to 128 characters", () => {
    const { normalizeBranchName } = require("./normalize_branch_name.cjs");

    const longBranchName = "a".repeat(200);
    const result = normalizeBranchName(longBranchName);
    expect(result.length).toBeLessThanOrEqual(128);
  });

  it("should collapse multiple dashes to single dash", () => {
    const { normalizeBranchName } = require("./normalize_branch_name.cjs");

    expect(normalizeBranchName("branch---with---dashes")).toBe("branch-with-dashes");
    expect(normalizeBranchName("branch  with  spaces")).toBe("branch-with-spaces");
  });

  it("should remove leading and trailing dashes", () => {
    const { normalizeBranchName } = require("./normalize_branch_name.cjs");

    expect(normalizeBranchName("---branch---")).toBe("branch");
    expect(normalizeBranchName("---")).toBe("");
  });

  it("should preserve original casing (no lowercase conversion)", () => {
    const { normalizeBranchName } = require("./normalize_branch_name.cjs");

    expect(normalizeBranchName("Feature/MyBranch")).toBe("Feature/MyBranch");
    expect(normalizeBranchName("UPPERCASE")).toBe("UPPERCASE");
    // Motivating use-case: Jira keys stay uppercase
    expect(normalizeBranchName("bugfix/BR-329-red")).toBe("bugfix/BR-329-red");
  });
});

// ──────────────────────────────────────────────────────
// normalizeBranchName: salt argument
// ──────────────────────────────────────────────────────

describe("create_pull_request - normalizeBranchName: salt argument", () => {
  it("should append salt suffix when salt argument is provided", () => {
    const { normalizeBranchName } = require("./normalize_branch_name.cjs");

    expect(normalizeBranchName("feature/my-branch", "abc123")).toBe("feature/my-branch-abc123");
    expect(normalizeBranchName("bugfix/BR-329-red", "cde2a954af3b8fa8")).toBe("bugfix/BR-329-red-cde2a954af3b8fa8");
  });

  it("should preserve original casing and add salt (default behaviour)", () => {
    const { normalizeBranchName } = require("./normalize_branch_name.cjs");

    // Default: preserve case + salt
    expect(normalizeBranchName("bugfix/BR-329-red", "cde2a954")).toBe("bugfix/BR-329-red-cde2a954");

    // preserve-branch-name=true: no salt
    expect(normalizeBranchName("bugfix/BR-329-red")).toBe("bugfix/BR-329-red");
  });

  it("should still replace shell metacharacters for security even when preserving case (CWE-78)", () => {
    const { normalizeBranchName } = require("./normalize_branch_name.cjs");

    const dangerousNames = [
      { input: "Feature; rm -rf /", expected: "Feature-rm-rf-/" },
      { input: "Branch$(malicious)", expected: "Branch-malicious" },
      { input: "BRANCH`backdoor`", expected: "BRANCH-backdoor" },
      { input: "Branch| curl EVIL.COM", expected: "Branch-curl-EVIL.COM" },
      { input: "Branch && echo HACKED", expected: "Branch-echo-HACKED" },
    ];

    for (const { input, expected } of dangerousNames) {
      const result = normalizeBranchName(input);
      expect(result).toBe(expected);
      expect(result).not.toContain(";");
      expect(result).not.toContain("$");
      expect(result).not.toContain("`");
      expect(result).not.toContain("|");
      expect(result).not.toContain("&");
    }
  });
});

// ──────────────────────────────────────────────────────
// allowed-files strict allowlist
// ──────────────────────────────────────────────────────

describe("create_pull_request - allowed-files strict allowlist", () => {
  let tempDir;
  let originalEnv;
  let pushSignedSpy;

  beforeEach(() => {
    originalEnv = { ...process.env };
    process.env.GH_AW_WORKFLOW_ID = "test-workflow";
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "create-pr-allowed-test-"));

    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };
    global.github = {
      rest: {
        pulls: {
          create: vi.fn().mockResolvedValue({ data: { number: 1, html_url: "https://github.com/test", head: { sha: "abc123" } } }),
          createReview: vi.fn().mockResolvedValue({ data: { id: 77, html_url: "https://github.com/test/review/77" } }),
        },
        repos: {
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
        },
      },
      graphql: vi.fn(),
    };
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {},
    };
    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" }),
    };
    const pushSignedCommitsModule = require("./push_signed_commits.cjs");
    pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue("bundle-tip");
    const promptsDir = path.join(tempDir, "prompts");
    fs.mkdirSync(promptsDir, { recursive: true });
    copyPromptTemplate(promptsDir, "manifest_protection_request_review.md");
    copyPromptTemplate(promptsDir, "manifest_protection_request_changes_review.md");
    copyPromptTemplate(promptsDir, "threat_warning_request_changes_review.md");
    copyPromptTemplate(promptsDir, "safe_outputs_disclosure_header.md");
    process.env.GH_AW_PROMPTS_DIR = promptsDir;

    // Clear module cache so globals are picked up fresh
    delete require.cache[require.resolve("./create_pull_request.cjs")];
  });

  afterEach(() => {
    if (pushSignedSpy) {
      pushSignedSpy.mockRestore();
    }
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) {
        delete process.env[key];
      }
    }
    Object.assign(process.env, originalEnv);

    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }

    delete global.core;
    delete global.github;
    delete global.context;
    delete global.exec;
    vi.clearAllMocks();
  });

  /**
   * Creates a minimal git patch touching the given file paths.
   */
  function createPatchWithFiles(...filePaths) {
    const diffs = filePaths
      .map(
        p => `diff --git a/${p} b/${p}
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/${p}
@@ -0,0 +1 @@
+content
`
      )
      .join("\n");
    return `From abc123 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

${diffs}
--
2.34.1
`;
  }

  function writePatch(branch, content) {
    const p = canonicalPatchPath(branch);
    fs.writeFileSync(p, content);
    return p;
  }

  function extractCompareUrlFromIssueBody(issueBody) {
    const match = String(issueBody).match(/https:\/\/[^)\s]+\/compare\/[^)\s]+/);
    return match ? match[0] : null;
  }

  it("should reject files outside the allowed-files allowlist", async () => {
    const patchPath = writePatch("should-reject-files-outside-the-allowed-files-allowlist", createPatchWithFiles("src/index.js"));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allowed_files: [".github/aw/**"] });
    const result = await handler({ title: "Test PR", body: "", branch: "should-reject-files-outside-the-allowed-files-allowlist" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("outside the allowed-files list");
    expect(result.error).toContain("src/index.js");
  });

  it("should reject a mixed patch where some files are outside the allowlist", async () => {
    const patchPath = writePatch("should-reject-a-mixed-patch-where-some-files-are-outside-the", createPatchWithFiles(".github/aw/github-agentic-workflows.md", "src/index.js"));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allowed_files: [".github/aw/**"] });
    const result = await handler({ title: "Test PR", body: "", branch: "should-reject-a-mixed-patch-where-some-files-are-outside-the" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("outside the allowed-files list");
    expect(result.error).toContain("src/index.js");
    expect(result.error).not.toContain(".github/aw/github-agentic-workflows.md");
  });

  it("should still enforce protected-files when allowed-files matches (orthogonal checks)", async () => {
    // allowed-files and protected-files are orthogonal: both checks must pass.
    // Matching the allowlist does NOT bypass the protected-files policy.
    const patchPath = writePatch("should-still-enforce-protected-files-when-allowed-files-matc", createPatchWithFiles(".github/aw/instructions.md"));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({
      allowed_files: [".github/aw/**"],
      protected_path_prefixes: [".github/"],
      protected_files_policy: "blocked",
    });
    const result = await handler({ title: "Test PR", body: "", branch: "should-still-enforce-protected-files-when-allowed-files-matc" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("protected files");
  });

  it("should allow a protected file when both allowed-files matches and protected-files: allowed is set", async () => {
    // Both checks are satisfied explicitly: allowlist scope + protected-files permission.
    const patchPath = writePatch("should-allow-a-protected-file-when-both-allowed-files-matche", createPatchWithFiles(".github/aw/instructions.md"));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({
      allowed_files: [".github/aw/**"],
      protected_path_prefixes: [".github/"],
      protected_files_policy: "allowed",
    });
    const result = await handler({ title: "Test PR", body: "", branch: "should-allow-a-protected-file-when-both-allowed-files-matche" }, {});

    // Should not be blocked by either check
    expect(result.error || "").not.toContain("protected files");
    expect(result.error || "").not.toContain("outside the allowed-files list");
  });

  it("should still enforce protected-files when allowed-files is not set", async () => {
    const patchPath = writePatch("should-still-enforce-protected-files-when-allowed-files-is-n", createPatchWithFiles(".github/aw/instructions.md"));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({
      protected_path_prefixes: [".github/"],
      protected_files_policy: "blocked",
    });
    const result = await handler({ title: "Test PR", body: "", branch: "should-still-enforce-protected-files-when-allowed-files-is-n" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("protected files");
  });

  it("should create PR with caution and request-changes review when protected-files is request_review", async () => {
    const patchPath = writePatch("should-create-pr-with-caution-and-request-changes-review-whe", createPatchWithFiles(".github/aw/instructions.md"));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({
      protected_path_prefixes: [".github/"],
      protected_files_policy: "request_review",
    });
    const result = await handler({ title: "Test PR", body: "Body text", branch: "should-create-pr-with-caution-and-request-changes-review-whe" }, {});

    expect(result.success).toBe(true);
    expect(global.github.rest.pulls.create).toHaveBeenCalledTimes(1);
    const createPrCall = global.github.rest.pulls.create.mock.calls[0][0];
    expect(createPrCall.body).toContain("Protected files were modified in this change.");
    expect(createPrCall.body).toContain(".github/aw/instructions.md");

    expect(global.github.rest.pulls.createReview).toHaveBeenCalledTimes(1);
    const createReviewCall = global.github.rest.pulls.createReview.mock.calls[0][0];
    expect(createReviewCall.event).toBe("REQUEST_CHANGES");
    expect(createReviewCall.body).toContain("Protected files were modified");
    expect(createReviewCall.body).toContain(".github/aw/instructions.md");
  });

  it("should default to request_review when protected-files policy is unset", async () => {
    const patchPath = writePatch("should-default-to-request-review-when-protected-files-policy", createPatchWithFiles(".github/aw/instructions.md"));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({
      protected_path_prefixes: [".github/"],
    });
    const result = await handler({ title: "Test PR", body: "Body text", branch: "should-default-to-request-review-when-protected-files-policy" }, {});

    expect(result.success).toBe(true);
    expect(global.github.rest.pulls.create).toHaveBeenCalledTimes(1);
    expect(global.github.rest.pulls.createReview).toHaveBeenCalledTimes(1);
    const createReviewCall = global.github.rest.pulls.createReview.mock.calls[0][0];
    expect(createReviewCall.event).toBe("REQUEST_CHANGES");
  });

  it("should retry with COMMENT when REQUEST_CHANGES review is rejected on own pull request", async () => {
    const patchPath = writePatch("should-retry-with-comment-when-request-changes-review-is-rej", createPatchWithFiles(".github/aw/instructions.md"));
    global.github.rest.pulls.createReview = vi
      .fn()
      .mockRejectedValueOnce(new Error("Can not request changes on your own pull request"))
      .mockResolvedValueOnce({ data: { id: 78, html_url: "https://github.com/test/review/78" } });

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({
      protected_path_prefixes: [".github/"],
      protected_files_policy: "request_review",
    });
    const result = await handler({ title: "Test PR", body: "Body text", branch: "should-retry-with-comment-when-request-changes-review-is-rej" }, {});

    expect(result.success).toBe(true);
    expect(global.github.rest.pulls.createReview).toHaveBeenCalledTimes(2);
    expect(global.github.rest.pulls.createReview.mock.calls[0][0].event).toBe("REQUEST_CHANGES");
    expect(global.github.rest.pulls.createReview.mock.calls[1][0].event).toBe("COMMENT");
  });

  it("should push branch with compare URL for protected-files fallback (patch transport)", async () => {
    const patchPath = writePatch("feature/protected", createPatchWithFiles(".github/aw/instructions.md"));
    const promptsDir = path.join(tempDir, "prompts");
    fs.mkdirSync(promptsDir, { recursive: true });
    copyPromptTemplate(promptsDir, "manifest_protection_create_pr_fallback.md");
    copyPromptTemplate(promptsDir, "manifest_protection_push_failed_fallback.md");
    copyPromptTemplate(promptsDir, "safe_outputs_disclosure_header.md");
    process.env.GH_AW_PROMPTS_DIR = promptsDir;

    global.github.rest.issues = {
      create: vi.fn().mockResolvedValue({ data: { number: 77, html_url: "https://github.com/test-owner/test-repo/issues/77" } }),
      update: vi.fn().mockResolvedValue({ data: {} }),
    };

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({
      protected_path_prefixes: [".github/"],
      protected_files_policy: "fallback-to-issue",
    });
    const result = await handler({ title: "Test PR", body: "Test body", branch: "feature/protected" }, {});

    expect(result.success).toBe(true);
    expect(result.fallback_used).toBe(true);
    expect(result.issue_number).toBe(77);
    // Branch must be pushed so the fallback issue can include a compare URL
    expect(pushSignedSpy).toHaveBeenCalled();
    expect(global.github.rest.issues.create).toHaveBeenCalledTimes(1);
    // Issue body is updated after creation to add the close-keyword link
    expect(global.github.rest.issues.update).toHaveBeenCalled();

    const createCall = global.github.rest.issues.create.mock.calls[0][0];
    // Should use the create-PR fallback template (compare URL), not the push-failed template
    expect(createCall.body).toContain("/compare/main...");
    expect(createCall.body).not.toContain("gh run download");
    expect(createCall.body).not.toContain("git am --3way");
    expect(createCall.body).toContain("Your pull request is ready to create! 🎉 ✅");
    expect(createCall.body).toContain("The original pull request description is below.");
    expect(createCall.body.indexOf("Your pull request is ready to create! 🎉 ✅")).toBeLessThan(createCall.body.indexOf("Test body"));
    expect(createCall.body.indexOf("Test body")).toBeLessThan(createCall.body.indexOf("Protected files"));
  });

  it("should push branch with compare URL for protected-files fallback (bundle transport)", async () => {
    const patchPath = writePatch("feature/protected", createPatchWithFiles(".github/aw/instructions.md"));
    const bundlePath = canonicalBundlePath("feature/protected");
    fs.writeFileSync(bundlePath, "bundle content");
    const promptsDir = path.join(tempDir, "prompts");
    fs.mkdirSync(promptsDir, { recursive: true });
    copyPromptTemplate(promptsDir, "manifest_protection_create_pr_fallback.md");
    copyPromptTemplate(promptsDir, "manifest_protection_push_failed_fallback.md");
    copyPromptTemplate(promptsDir, "safe_outputs_disclosure_header.md");
    process.env.GH_AW_PROMPTS_DIR = promptsDir;

    global.github.rest.issues = {
      create: vi.fn().mockResolvedValue({ data: { number: 77, html_url: "https://github.com/test-owner/test-repo/issues/77" } }),
      update: vi.fn().mockResolvedValue({ data: {} }),
    };

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({
      protected_path_prefixes: [".github/"],
      protected_files_policy: "fallback-to-issue",
    });
    const result = await handler({ title: "Test PR", body: "Test body", branch: "feature/protected" }, {});

    expect(result.success).toBe(true);
    expect(result.fallback_used).toBe(true);
    expect(result.issue_number).toBe(77);
    // Branch must be pushed so the fallback issue can include a compare URL
    expect(pushSignedSpy).toHaveBeenCalled();
    // Issue body is updated after creation to add the close-keyword link
    expect(global.github.rest.issues.update).toHaveBeenCalled();

    const createCall = global.github.rest.issues.create.mock.calls[0][0];
    // Should use the create-PR fallback template (compare URL), not the push-failed template
    expect(createCall.body).toContain("/compare/main...");
    expect(createCall.body).not.toContain("gh run download");
    expect(createCall.body).not.toContain("git am --3way");
  });

  it("should give patch-based manual recovery instructions for protected-files push-failure fallback (patch transport)", async () => {
    writePatch("feature/protected", createPatchWithFiles(".github/aw/instructions.md"));
    const promptsDir = path.join(tempDir, "prompts");
    fs.mkdirSync(promptsDir, { recursive: true });
    copyPromptTemplate(promptsDir, "manifest_protection_create_pr_fallback.md");
    copyPromptTemplate(promptsDir, "manifest_protection_push_failed_fallback.md");
    copyPromptTemplate(promptsDir, "safe_outputs_disclosure_header.md");
    process.env.GH_AW_PROMPTS_DIR = promptsDir;

    global.github.rest.issues = {
      create: vi.fn().mockResolvedValue({ data: { number: 78, html_url: "https://github.com/test-owner/test-repo/issues/78" } }),
      update: vi.fn().mockResolvedValue({ data: {} }),
    };
    pushSignedSpy.mockRejectedValueOnce(new Error("refusing to allow a GitHub App to create or update workflow"));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({
      protected_path_prefixes: [".github/"],
      protected_files_policy: "fallback-to-issue",
    });
    const result = await handler({ title: "Test PR", body: "Test body", branch: "feature/protected" }, {});

    expect(result.success).toBe(true);
    expect(result.fallback_used).toBe(true);
    expect(result.issue_number).toBe(78);

    const createCall = global.github.rest.issues.create.mock.calls[0][0];
    // No compare URL is possible since the branch was never pushed
    expect(createCall.body).not.toContain("/compare/main...");
    expect(createCall.body).toContain("gh run download");
    // Patch transport: instructions should create a new branch off the base branch, then git am
    expect(createCall.body).toMatch(/git checkout -b 'feature\/protected\S*' 'main'/);
    expect(createCall.body).toContain("git am --3way");
    expect(createCall.body).not.toContain("git update-ref");
  });

  it("should give bundle-based manual recovery instructions for protected-files push-failure fallback (bundle transport)", async () => {
    writePatch("feature/protected", createPatchWithFiles(".github/aw/instructions.md"));
    const bundlePath = canonicalBundlePath("feature/protected");
    fs.writeFileSync(bundlePath, "bundle content");
    const promptsDir = path.join(tempDir, "prompts");
    fs.mkdirSync(promptsDir, { recursive: true });
    copyPromptTemplate(promptsDir, "manifest_protection_create_pr_fallback.md");
    copyPromptTemplate(promptsDir, "manifest_protection_push_failed_fallback.md");
    copyPromptTemplate(promptsDir, "safe_outputs_disclosure_header.md");
    process.env.GH_AW_PROMPTS_DIR = promptsDir;

    global.github.rest.issues = {
      create: vi.fn().mockResolvedValue({ data: { number: 79, html_url: "https://github.com/test-owner/test-repo/issues/79" } }),
      update: vi.fn().mockResolvedValue({ data: {} }),
    };
    pushSignedSpy.mockRejectedValueOnce(new Error("refusing to allow a GitHub App to create or update workflow"));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({
      protected_path_prefixes: [".github/"],
      protected_files_policy: "fallback-to-issue",
    });
    const result = await handler({ title: "Test PR", body: "Test body", branch: "feature/protected" }, {});

    expect(result.success).toBe(true);
    expect(result.fallback_used).toBe(true);
    expect(result.issue_number).toBe(79);

    const createCall = global.github.rest.issues.create.mock.calls[0][0];
    // No compare URL is possible since the branch was never pushed
    expect(createCall.body).not.toContain("/compare/main...");
    expect(createCall.body).toContain("gh run download");
    // Bundle transport: instructions should fetch the bundle into a temp ref and reset --hard,
    // not use git am (which fails on bundle files - see issue #55509)
    expect(createCall.body).toContain("git bundle list-heads");
    expect(createCall.body).toContain('$2 == "HEAD"');
    expect(createCall.body).toContain('git update-ref "$target_ref" "$temp_ref"');
    expect(createCall.body).toContain("git reset --hard");
    expect(createCall.body).not.toContain("git am --3way");
  });
});

// excluded-files exclusion list
// ──────────────────────────────────────────────────────

describe("create_pull_request - excluded-files exclusion list", () => {
  let tempDir;
  let originalEnv;

  beforeEach(() => {
    originalEnv = { ...process.env };
    process.env.GH_AW_WORKFLOW_ID = "test-workflow";
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "create-pr-ignored-test-"));

    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };
    global.github = {
      rest: {
        pulls: {
          create: vi.fn().mockResolvedValue({ data: { number: 1, html_url: "https://github.com/test" } }),
        },
        repos: {
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
        },
      },
      graphql: vi.fn(),
    };
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {},
    };
    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockResolvedValue({ exitCode: 0, stdout: "abc123\n", stderr: "" }),
    };

    // Clear module cache so globals are picked up fresh
    delete require.cache[require.resolve("./create_pull_request.cjs")];
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) {
        delete process.env[key];
      }
    }
    Object.assign(process.env, originalEnv);

    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }

    delete global.core;
    delete global.github;
    delete global.context;
    delete global.exec;
    vi.clearAllMocks();
  });

  /**
   * Creates a minimal git patch touching the given file paths.
   */
  function createPatchWithFiles(...filePaths) {
    const diffs = filePaths
      .map(
        p => `diff --git a/${p} b/${p}
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/${p}
@@ -0,0 +1 @@
+content
`
      )
      .join("\n");
    return `From abc123 Mon Sep 17 00:00:00 2001
From: Test Author <test@example.com>
Date: Mon, 1 Jan 2024 00:00:00 +0000
Subject: [PATCH] Test commit

${diffs}
--
2.34.1
`;
  }

  function writePatch(branch, content) {
    const p = canonicalPatchPath(branch);
    fs.writeFileSync(p, content);
    return p;
  }

  it("should ignore files matching excluded-files patterns (not blocked by allowed-files)", async () => {
    // excluded-files are excluded at patch generation time via git :(exclude) pathspecs.
    // Simulate post-generation: the patch already contains only the non-ignored file.
    const patchPath = writePatch("should-ignore-files-matching-excluded-files-patterns-not-blo", createPatchWithFiles("src/index.js"));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({
      excluded_files: ["auto-generated/**"],
      allowed_files: ["src/**"],
    });
    const result = await handler({ title: "Test PR", body: "", branch: "should-ignore-files-matching-excluded-files-patterns-not-blo" }, {});

    expect(result.error || "").not.toContain("outside the allowed-files list");
  });

  it("should still block non-ignored files that violate the allowed-files list", async () => {
    const patchPath = writePatch("should-still-block-non-ignored-files-that-violate-the-allowe", createPatchWithFiles("src/index.js", "other/file.txt"));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({
      excluded_files: ["auto-generated/**"],
      allowed_files: ["src/**"],
    });
    const result = await handler({ title: "Test PR", body: "", branch: "should-still-block-non-ignored-files-that-violate-the-allowe" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("outside the allowed-files list");
    expect(result.error).toContain("other/file.txt");
    expect(result.error).not.toContain("src/index.js");
  });

  it("should ignore files matching excluded-files patterns (not blocked by protected-files)", async () => {
    // excluded-files are excluded at patch generation time via git :(exclude) pathspecs.
    // Simulate post-generation: the patch already contains only the non-ignored file.
    const patchPath = writePatch("should-ignore-files-matching-excluded-files-patterns-not-blo", createPatchWithFiles("src/index.js"));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({
      excluded_files: ["package.json"],
      protected_files: ["package.json"],
      protected_files_policy: "blocked",
    });
    const result = await handler({ title: "Test PR", body: "", branch: "should-ignore-files-matching-excluded-files-patterns-not-blo" }, {});

    expect(result.error || "").not.toContain("protected files");
  });

  it("should allow when all patch files are ignored (even with allowed-files set)", async () => {
    // excluded-files are excluded at patch generation time via git :(exclude) pathspecs.
    // Simulate post-generation: all files were excluded so the patch file is absent.
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({
      excluded_files: ["dist/**"],
      allowed_files: ["src/**"],
    });
    // No patch file — simulates all changes being ignored at generation time
    const result = await handler({ title: "Test PR", body: "", branch: "should-allow-when-all-patch-files-are-ignored-even-with-allo" }, {});

    // No patch → treated as no changes, not an allowlist violation
    expect(result.error || "").not.toContain("outside the allowed-files list");
  });
});

describe("create_pull_request - configured reviewers", () => {
  let tempDir;
  let originalEnv;

  beforeEach(() => {
    originalEnv = { ...process.env };
    process.env.GH_AW_WORKFLOW_ID = "test-workflow";
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "create-pr-reviewer-test-"));

    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };
    global.github = {
      rest: {
        pulls: {
          create: vi.fn().mockResolvedValue({ data: { number: 42, html_url: "https://github.com/test/pull/42", node_id: "PR_42" } }),
          createReview: vi.fn().mockResolvedValue({ data: { id: 77, html_url: "https://github.com/test/review/77" } }),
          requestReviewers: vi.fn().mockResolvedValue({}),
        },
        repos: {
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
        },
        issues: {
          addLabels: vi.fn().mockResolvedValue({}),
          addAssignees: vi.fn().mockResolvedValue({}),
        },
      },
      graphql: vi.fn(),
    };
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {},
    };
    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockResolvedValue({ exitCode: 0, stdout: "", stderr: "" }),
    };

    delete require.cache[require.resolve("./create_pull_request.cjs")];
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) {
        delete process.env[key];
      }
    }
    Object.assign(process.env, originalEnv);

    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }

    delete global.core;
    delete global.github;
    delete global.context;
    delete global.exec;
    vi.clearAllMocks();
  });

  it("should request configured reviewers after creating the PR", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ reviewers: ["user1", "user2"], allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(true);
    expect(global.github.rest.pulls.requestReviewers).toHaveBeenCalledWith(
      expect.objectContaining({
        owner: "test-owner",
        repo: "test-repo",
        pull_number: 42,
        reviewers: ["user1", "user2"],
      })
    );
  });

  it("should request configured team reviewers after creating the PR", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ team_reviewers: ["platform-team"], allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(true);
    expect(global.github.rest.pulls.requestReviewers).toHaveBeenCalledWith(
      expect.objectContaining({
        owner: "test-owner",
        repo: "test-repo",
        pull_number: 42,
        reviewers: [],
        team_reviewers: ["platform-team"],
      })
    );
  });

  it("should handle copilot reviewer separately from regular reviewers", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ reviewers: ["user1", "copilot"], allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(true);
    // Should be called twice: once for regular reviewers, once for copilot bot
    expect(global.github.rest.pulls.requestReviewers).toHaveBeenCalledTimes(2);
    expect(global.github.rest.pulls.requestReviewers).toHaveBeenCalledWith(expect.objectContaining({ reviewers: ["user1"] }));
    expect(global.github.rest.pulls.requestReviewers).toHaveBeenCalledWith(expect.objectContaining({ reviewers: ["copilot-pull-request-reviewer[bot]"] }));
  });

  it("should keep configured team reviewers with non-copilot reviewers when copilot is configured", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ reviewers: ["user1", "copilot"], team_reviewers: ["platform-team"], allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(true);
    expect(global.github.rest.pulls.requestReviewers).toHaveBeenCalledTimes(2);
    expect(global.github.rest.pulls.requestReviewers).toHaveBeenNthCalledWith(1, expect.objectContaining({ reviewers: ["user1"], team_reviewers: ["platform-team"] }));
    expect(global.github.rest.pulls.requestReviewers).toHaveBeenNthCalledWith(2, expect.objectContaining({ reviewers: ["copilot-pull-request-reviewer[bot]"] }));
  });

  it("should not call requestReviewers when no reviewers are configured", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(true);
    expect(global.github.rest.pulls.requestReviewers).not.toHaveBeenCalled();
  });

  it("should continue successfully even if requestReviewers fails", async () => {
    global.github.rest.pulls.requestReviewers.mockRejectedValue(new Error("API error"));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ reviewers: ["user1"], allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(true);
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to request reviewers"));
  });

  it("should assign configured assignees to the created PR", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ assignees: ["user1", "user2"], allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(true);
    expect(global.github.rest.issues.addAssignees).toHaveBeenCalledWith(
      expect.objectContaining({
        owner: "test-owner",
        repo: "test-repo",
        issue_number: 42,
        assignees: ["user1", "user2"],
      })
    );
  });

  it("should not assign assignees to the created PR when none remain after sanitization", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ assignees: ["copilot"], allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(true);
    expect(global.github.rest.issues.addAssignees).not.toHaveBeenCalled();
  });

  it("should continue successfully even if addAssignees fails", async () => {
    global.github.rest.issues.addAssignees.mockRejectedValue(new Error("API error"));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ assignees: ["user1"], allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(true);
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to assign assignees"));
  });

  it("should strip copilot before assigning PR assignees", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ assignees: ["copilot", "user1"], allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(true);
    expect(global.github.rest.issues.addAssignees).toHaveBeenCalledWith(
      expect.objectContaining({
        issue_number: 42,
        assignees: ["user1"],
      })
    );
  });

  it("should retry addLabels on race condition and warn after all retries exhausted", async () => {
    // GitHub API transiently fails to resolve the PR node ID immediately after creation.
    // withRetry retries 5 times (6 total calls); after exhaustion it should warn but NOT fall back to an issue.
    vi.useFakeTimers();
    try {
      global.github.rest.issues.addLabels.mockRejectedValue(new Error("Validation Failed: Could not resolve to a node with the global id of 'PR_kwDOPc1QR87OOJzM'."));

      const { main } = require("./create_pull_request.cjs");
      const handler = await main({ labels: ["automation"], allow_empty: true });

      const resultPromise = handler({ title: "Test PR", body: "Test body", labels: ["automation"] }, {});

      // Advance all fake timers to skip the retry delays (6s, 12s, 24s, 30s, 30s)
      await vi.runAllTimersAsync();

      const result = await resultPromise;

      expect(result.success).toBe(true);
      expect(result.fallback_used).toBeUndefined();
      // addLabels called once initially + 5 retries = 6 total
      expect(global.github.rest.issues.addLabels).toHaveBeenCalledTimes(6);
      expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to add labels to PR #42"));
    } finally {
      vi.useRealTimers();
    }
  });

  it("should succeed when addLabels recovers on a retry", async () => {
    // Simulates a transient race condition that resolves on the second attempt.
    vi.useFakeTimers();
    try {
      global.github.rest.issues.addLabels.mockRejectedValueOnce(new Error("Validation Failed: Could not resolve to a node with the global id of 'PR_kwDOPc1QR87OOJzM'.")).mockResolvedValue({});

      const { main } = require("./create_pull_request.cjs");
      const handler = await main({ labels: ["automation"], allow_empty: true });

      const resultPromise = handler({ title: "Test PR", body: "Test body", labels: ["automation"] }, {});

      await vi.runAllTimersAsync();

      const result = await resultPromise;

      expect(result.success).toBe(true);
      // addLabels called twice: first attempt fails, second succeeds
      expect(global.github.rest.issues.addLabels).toHaveBeenCalledTimes(2);
      // No warning about final failure — the retry succeeded
      expect(global.core.warning).not.toHaveBeenCalledWith(expect.stringContaining("Failed to add labels to PR #42"));
    } finally {
      vi.useRealTimers();
    }
  });

  it("should not retry addLabels for non-transient errors", async () => {
    // Non-transient errors (e.g., 404 label not found) should not be retried.
    global.github.rest.issues.addLabels.mockRejectedValue(new Error("Validation Failed: label does not exist"));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ labels: ["nonexistent"], allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body", labels: ["nonexistent"] }, {});

    expect(result.success).toBe(true);
    // No retry — called only once since the error is non-transient
    expect(global.github.rest.issues.addLabels).toHaveBeenCalledTimes(1);
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to add labels to PR #42"));
  });

  it("should accept reviewers as a comma-separated string", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ reviewers: "user1,user2", allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(true);
    expect(global.github.rest.pulls.requestReviewers).toHaveBeenCalledWith(expect.objectContaining({ reviewers: ["user1", "user2"] }));
  });
});

describe("create_pull_request - wildcard target-repo", () => {
  let tempDir;
  let originalEnv;

  beforeEach(() => {
    originalEnv = { ...process.env };
    process.env.GH_AW_WORKFLOW_ID = "test-workflow";
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "create-pr-wildcard-test-"));

    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };
    global.github = {
      rest: {
        pulls: {
          create: vi.fn().mockResolvedValue({ data: { number: 99, html_url: "https://github.com/any-org/any-repo/pull/99", node_id: "PR_99" } }),
          requestReviewers: vi.fn().mockResolvedValue({}),
        },
        repos: {
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
        },
        issues: {
          addLabels: vi.fn().mockResolvedValue({}),
        },
      },
      graphql: vi.fn(),
    };
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {},
    };
    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockResolvedValue({ exitCode: 0, stdout: "", stderr: "" }),
    };

    delete require.cache[require.resolve("./create_pull_request.cjs")];
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) {
        delete process.env[key];
      }
    }
    Object.assign(process.env, originalEnv);

    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }

    delete global.core;
    delete global.github;
    delete global.context;
    delete global.exec;
    vi.clearAllMocks();
  });

  it('should create PR in any repo when target-repo is "*"', async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ "target-repo": "*", allow_empty: true });

    const result = await handler(
      {
        title: "Test PR",
        body: "Test body",
        repo: "any-org/any-repo",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(global.github.rest.pulls.create).toHaveBeenCalledWith(
      expect.objectContaining({
        owner: "any-org",
        repo: "any-repo",
      })
    );
  });

  it('should reject invalid repo slug when target-repo is "*"', async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ "target-repo": "*", allow_empty: true });

    const result = await handler(
      {
        title: "Test PR",
        body: "Test body",
        repo: "not-a-valid-slug",
      },
      {}
    );

    expect(result.success).toBe(false);
  });
});

describe("create_pull_request - base branch override policy", () => {
  let tempDir;
  let originalEnv;

  beforeEach(() => {
    originalEnv = { ...process.env };
    process.env.GH_AW_WORKFLOW_ID = "test-workflow";
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "create-pr-base-override-test-"));

    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };
    global.github = {
      rest: {
        pulls: {
          create: vi.fn().mockResolvedValue({ data: { number: 100, html_url: "https://github.com/test-owner/test-repo/pull/100", node_id: "PR_100" } }),
          requestReviewers: vi.fn().mockResolvedValue({}),
        },
        repos: {
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
        },
        issues: {
          addLabels: vi.fn().mockResolvedValue({}),
        },
      },
      graphql: vi.fn(),
    };
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {},
    };
    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockResolvedValue({ exitCode: 0, stdout: "", stderr: "" }),
    };

    delete require.cache[require.resolve("./create_pull_request.cjs")];
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) {
        delete process.env[key];
      }
    }
    Object.assign(process.env, originalEnv);

    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }

    delete global.core;
    delete global.github;
    delete global.context;
    delete global.exec;
    vi.clearAllMocks();
  });

  it("should reject base override when allowed-base-branches is not configured", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body", base: "release/1.0" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("Base branch override is not allowed");
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("allowed-base-branches is not configured"));
  });

  it("should allow base override when it matches the default base branch and no allowed-base-branches is configured", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });

    // GITHUB_BASE_REF is set to "main" in beforeEach, so requesting base: "main" should be implicitly allowed
    const result = await handler({ title: "Test PR", body: "Test body", base: "main" }, {});

    expect(result.success).toBe(true);
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('Base branch override requested: "main"'));
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('Base branch "main" matches the default base branch, no override needed'));
  });

  it("should allow base override when it matches allowed-base-branches", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true, allowed_base_branches: ["release/*", "main"] });

    const result = await handler({ title: "Test PR", body: "Test body", base: "release/1.0" }, {});

    expect(result.success).toBe(true);
    expect(global.github.rest.pulls.create).toHaveBeenCalledWith(expect.objectContaining({ base: "release/1.0" }));
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('Base branch override requested: "release/1.0"'));
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('Base branch override accepted: "release/1.0"'));
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("Using agent-provided base branch override: release/1.0"));
  });

  it("should reject base override when it does not match allowed-base-branches", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true, allowed_base_branches: ["release/*"] });

    const result = await handler({ title: "Test PR", body: "Test body", base: "main" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("Base branch override 'main' is not allowed");
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("does not match allowed patterns"));
  });
});

describe("create_pull_request - patch apply fallback to original base commit", () => {
  let tempDir;
  let originalEnv;
  let patchFilePath;

  const MOCK_BASE_COMMIT_SHA = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef";
  // Minimal valid format-patch output
  const PATCH_CONTENT =
    `From a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2 Mon Sep 17 00:00:00 2001\n` +
    `X-GH-AW-Base-Commit: ${MOCK_BASE_COMMIT_SHA}\n` +
    `From: Test Author <test@example.com>\n` +
    `Date: Wed, 26 Mar 2026 12:00:00 +0000\n` +
    `Subject: [PATCH] Test change\n\n` +
    `---\n` +
    ` file.txt | 1 +\n\n` +
    `diff --git a/file.txt b/file.txt\n` +
    `index 1234567..abcdefg 100644\n` +
    `--- a/file.txt\n` +
    `+++ b/file.txt\n` +
    `@@ -1 +1,2 @@\n` +
    ` existing content\n` +
    `+new content\n` +
    `--\n` +
    `2.39.0\n`;

  beforeEach(() => {
    originalEnv = { ...process.env };
    process.env.GH_AW_WORKFLOW_ID = "test-workflow";
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "create-pr-fallback-test-"));
    patchFilePath = path.join(tempDir, "test.patch");
    fs.writeFileSync(patchFilePath, PATCH_CONTENT, "utf8");
    // Write the same patch content at every canonical path consumed by tests in
    // this describe, so the handler (which re-derives the path from
    // message.branch) finds it.
    for (const branch of ["test-branch", "preserve-me", "chaos/preserve-me", "some-branch"]) {
      fs.writeFileSync(canonicalPatchPath(branch), PATCH_CONTENT, "utf8");
    }

    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };
    global.github = {
      rest: {
        pulls: {
          create: vi.fn().mockResolvedValue({ data: { number: 42, html_url: "https://github.com/test/pull/42", node_id: "PR_42" } }),
          createReview: vi.fn().mockResolvedValue({ data: { id: 77, html_url: "https://github.com/test/review/77" } }),
          requestReviewers: vi.fn().mockResolvedValue({}),
        },
        repos: {
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
        },
        issues: {
          addLabels: vi.fn().mockResolvedValue({}),
        },
        git: {
          deleteRef: vi.fn().mockResolvedValue({}),
        },
      },
      graphql: vi.fn(),
    };
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {},
    };

    delete require.cache[require.resolve("./create_pull_request.cjs")];
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) {
        delete process.env[key];
      }
    }
    Object.assign(process.env, originalEnv);

    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }

    delete global.core;
    delete global.github;
    delete global.context;
    delete global.exec;
    vi.clearAllMocks();
  });

  /**
   * Helper to detect git am calls in both formats:
   * - exec("git", ["am", "--3way", path])  (array form)
   * - exec("git am --3way /path")          (string form)
   */
  function isGitAmCall(cmd, args) {
    if (cmd === "git" && Array.isArray(args) && args[0] === "am") return true;
    if (typeof cmd === "string" && cmd.startsWith("git am")) return true;
    return false;
  }

  function isGitAmAbort(cmd, args) {
    if (cmd === "git" && Array.isArray(args) && args[0] === "am" && args.includes("--abort")) return true;
    if (typeof cmd === "string" && cmd.includes("am --abort")) return true;
    return false;
  }

  function isGitAm3Way(cmd, args) {
    if (cmd === "git" && Array.isArray(args) && args[0] === "am" && args.includes("--3way")) return true;
    if (typeof cmd === "string" && cmd.startsWith("git am --3way")) return true;
    return false;
  }

  it("should create the PR branch from the patch-embedded base commit when available", async () => {
    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockResolvedValue({ exitCode: 0, stdout: "", stderr: "" }),
    };

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({});
    const result = await handler({ title: "Test PR", body: "Test body", branch: "test-branch", base_commit: `  ${MOCK_BASE_COMMIT_SHA}  ` }, {});

    expect(result.success).toBe(true);
    expect(global.exec.exec).toHaveBeenCalledWith("git", ["cat-file", "-e", MOCK_BASE_COMMIT_SHA]);
    const checkoutWithBaseCommit = global.exec.exec.mock.calls.find(([cmd, args]) => cmd === "git" && Array.isArray(args) && args[0] === "checkout" && args[1] === "-b" && args[3] === MOCK_BASE_COMMIT_SHA);
    expect(checkoutWithBaseCommit).toBeTruthy();
  });

  it("should ignore agent-supplied base_commit values when creating the branch", async () => {
    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockResolvedValue({ exitCode: 0, stdout: "", stderr: "" }),
    };

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({});
    const result = await handler({ title: "Test PR", body: "Test body", branch: "test-branch", base_commit: "not-a-sha --bad" }, {});

    expect(result.success).toBe(true);
    expect(global.exec.exec).not.toHaveBeenCalledWith("git", ["cat-file", "-e", "not-a-sha --bad"]);
    expect(global.core.warning).not.toHaveBeenCalledWith(expect.stringContaining("base_commit"));
  });

  it("should fall back to base branch when base_commit is unavailable", async () => {
    global.exec = {
      exec: vi.fn().mockImplementation(async (cmd, args) => {
        if (cmd === "git" && Array.isArray(args) && args[0] === "cat-file" && args[1] === "-e" && args[2] === MOCK_BASE_COMMIT_SHA) {
          throw new Error("not in object store");
        }
        return 0;
      }),
      getExecOutput: vi.fn().mockResolvedValue({ exitCode: 0, stdout: "", stderr: "" }),
    };

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({});
    const result = await handler({ title: "Test PR", body: "Test body", branch: "test-branch", base_commit: MOCK_BASE_COMMIT_SHA }, {});

    expect(result.success).toBe(true);
    expect(global.exec.exec).toHaveBeenCalledWith("git", ["cat-file", "-e", MOCK_BASE_COMMIT_SHA]);
    const checkoutWithBaseBranch = global.exec.exec.mock.calls.find(([cmd, args]) => cmd === "git" && Array.isArray(args) && args[0] === "checkout" && args[1] === "-b" && args[3] === "main");
    expect(checkoutWithBaseBranch).toBeTruthy();
  });

  it("should fall back to original base commit when git am --3way fails with merge conflicts", async () => {
    let primaryAmAttempted = false;
    global.exec = {
      exec: vi.fn().mockImplementation((cmd, args) => {
        // Fail the first "git am --3way" call to simulate a merge conflict
        if (isGitAm3Way(cmd, args) && !primaryAmAttempted) {
          primaryAmAttempted = true;
          throw new Error("CONFLICT (content): Merge conflict in file.txt");
        }
        return Promise.resolve(0);
      }),
      getExecOutput: vi.fn().mockImplementation((cmd, args) => {
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }),
    };

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({});

    const result = await handler({ title: "Test PR", body: "Test body", branch: "test-branch", base_commit: MOCK_BASE_COMMIT_SHA }, {});

    expect(result.success).toBe(true);
    // Should warn that the PR will show merge conflicts
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("merge conflicts"));
  });

  it("should diff against the fallback pre-apply HEAD for post-apply protection checks", async () => {
    let primaryAmAttempted = false;
    const promptsDir = path.join(tempDir, "prompts");
    fs.mkdirSync(promptsDir, { recursive: true });
    fs.writeFileSync(path.join(promptsDir, "manifest_protection_request_review.md"), "Protected files: {{files}}", "utf8");
    fs.writeFileSync(path.join(promptsDir, "manifest_protection_request_changes_review.md"), "Protected files: {{files}}", "utf8");
    copyPromptTemplate(promptsDir, "safe_outputs_disclosure_header.md");
    process.env.GH_AW_PROMPTS_DIR = promptsDir;
    global.exec = {
      exec: vi.fn().mockImplementation((cmd, args) => {
        if (isGitAm3Way(cmd, args) && !primaryAmAttempted) {
          primaryAmAttempted = true;
          throw new Error("CONFLICT (content): Merge conflict in file.txt");
        }
        return Promise.resolve(0);
      }),
      getExecOutput: vi.fn().mockImplementation((cmd, args) => {
        if (cmd === "git" && Array.isArray(args) && args[0] === "merge-base") {
          return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
        }
        if (cmd === "git" && Array.isArray(args) && args[0] === "rev-parse" && args[1] === "HEAD") {
          return Promise.resolve({ exitCode: 0, stdout: primaryAmAttempted ? `${MOCK_BASE_COMMIT_SHA}\n` : "origin-main-head\n", stderr: "" });
        }
        if (cmd === "git" && Array.isArray(args) && args[0] === "diff" && args[1] === "--name-only" && args[2] === "--no-renames") {
          expect(args[3]).toBe(`${MOCK_BASE_COMMIT_SHA}..HEAD`);
          return Promise.resolve({ exitCode: 0, stdout: ".github/CODEOWNERS\n", stderr: "" });
        }
        if (cmd === "git" && Array.isArray(args) && args[0] === "diff" && args.includes("--diff-filter=U")) {
          return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
        }
        if (cmd === "git" && Array.isArray(args) && args[0] === "status") {
          return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
        }
        if (cmd === "git" && Array.isArray(args) && args[0] === "am" && args[1] === "--show-current-patch=diff") {
          return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
        }
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }),
    };

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({
      protected_path_prefixes: [".github/"],
    });
    const result = await handler(
      {
        title: "Test PR",
        body: "Test body",
        branch: "test-branch",
        base_commit: MOCK_BASE_COMMIT_SHA,
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(global.github.rest.pulls.createReview).toHaveBeenCalled();
    expect(global.exec.getExecOutput).toHaveBeenCalledWith("git", ["diff", "--name-only", "--no-renames", `${MOCK_BASE_COMMIT_SHA}..HEAD`]);
  });

  it("should return error when both git am --3way and the fallback git am fail", async () => {
    global.exec = {
      exec: vi.fn().mockImplementation((cmd, args) => {
        // Fail all git am calls except git am --abort
        if (isGitAmCall(cmd, args) && !isGitAmAbort(cmd, args)) {
          throw new Error("CONFLICT (content): Merge conflict in file.txt");
        }
        return Promise.resolve(0);
      }),
      getExecOutput: vi.fn().mockImplementation((cmd, args) => {
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }),
    };

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({});

    const result = await handler({ title: "Test PR", body: "Test body", branch: "test-branch", base_commit: MOCK_BASE_COMMIT_SHA }, {});

    expect(result.success).toBe(false);
    expect(result.error).toBe("Failed to apply patch");
  });

  it("should recover add/add conflicts during fallback git am --3way and continue", async () => {
    let am3WayAttempts = 0;
    let unresolvedChecks = 0;
    const conflictedPath = " docs/file.md ";
    global.exec = {
      exec: vi.fn().mockImplementation((cmd, args) => {
        if (isGitAm3Way(cmd, args)) {
          am3WayAttempts += 1;
          throw new Error("CONFLICT (add/add): Merge conflict in docs/file.md");
        }
        return Promise.resolve(0);
      }),
      getExecOutput: vi.fn().mockImplementation((cmd, args) => {
        if (cmd === "git" && Array.isArray(args) && args[0] === "diff" && args.includes("--diff-filter=U")) {
          unresolvedChecks += 1;
          // First recovery attempt (after initial apply failure): no add/add conflicts.
          // Second recovery attempt (during fallback): add/add conflict present.
          if (unresolvedChecks === 1) {
            return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
          }
          return Promise.resolve({ exitCode: 0, stdout: `${conflictedPath}\0`, stderr: "" });
        }
        if (cmd === "git" && Array.isArray(args) && args[0] === "status" && args[1] === "--porcelain") {
          return Promise.resolve({ exitCode: 0, stdout: `AA ${conflictedPath}\0`, stderr: "" });
        }
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }),
    };

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({});

    const result = await handler({ title: "Test PR", body: "Test body", branch: "test-branch", base_commit: MOCK_BASE_COMMIT_SHA }, {});

    expect(result.success).toBe(true);
    expect(global.exec.exec).toHaveBeenCalledWith("git", ["checkout", "--theirs", "--", conflictedPath]);
    expect(global.exec.exec).toHaveBeenCalledWith("git", ["am", "--continue"]);
    expect(global.core.debug).toHaveBeenCalledWith(expect.stringContaining("Add/add recovery probe unresolved files"));
  });

  it("should log recovery failure details when add/add recovery throws", async () => {
    global.exec = {
      exec: vi.fn().mockImplementation((cmd, args) => {
        if (isGitAm3Way(cmd, args)) {
          throw new Error("CONFLICT (add/add): Merge conflict in docs/file.md");
        }
        if (cmd === "git" && Array.isArray(args) && args[0] === "checkout" && args[1] === "--theirs") {
          throw new Error("failed to checkout theirs");
        }
        return Promise.resolve(0);
      }),
      getExecOutput: vi.fn().mockImplementation((cmd, args) => {
        if (cmd === "git" && Array.isArray(args) && args[0] === "diff" && args.includes("--diff-filter=U")) {
          return Promise.resolve({ exitCode: 0, stdout: "docs/file.md\0", stderr: "" });
        }
        if (cmd === "git" && Array.isArray(args) && args[0] === "status" && args[1] === "--porcelain") {
          return Promise.resolve({ exitCode: 0, stdout: "AA docs/file.md\0", stderr: "" });
        }
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }),
    };

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({});
    const result = await handler({ title: "Test PR", body: "Test body", branch: "test-branch", base_commit: MOCK_BASE_COMMIT_SHA }, {});

    expect(result.success).toBe(false);
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("Automatic add/add conflict recovery attempt failed"));
  });

  it("should return error when original base commit is not available (cross-repo scenario)", async () => {
    global.exec = {
      exec: vi.fn().mockImplementation((cmd, args) => {
        // Fail git am --3way
        if (isGitAm3Way(cmd, args)) {
          throw new Error("CONFLICT (content): Merge conflict in file.txt");
        }
        // Fail git cat-file to simulate commit not present in local repo
        if (cmd === "git" && Array.isArray(args) && args[0] === "cat-file") {
          throw new Error("Not a valid object name");
        }
        return Promise.resolve(0);
      }),
      getExecOutput: vi.fn().mockImplementation((cmd, args) => {
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }),
    };

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({});

    const result = await handler({ title: "Test PR", body: "Test body", branch: "test-branch", base_commit: MOCK_BASE_COMMIT_SHA }, {});

    expect(result.success).toBe(false);
    expect(result.error).toBe("Failed to apply patch");
  });

  it("should return error when no base_commit is provided and git am --3way fails", async () => {
    const patchWithoutBaseCommit = PATCH_CONTENT.replace(`X-GH-AW-Base-Commit: ${MOCK_BASE_COMMIT_SHA}\n`, "");
    fs.writeFileSync(canonicalPatchPath("test-branch"), patchWithoutBaseCommit, "utf8");
    global.exec = {
      exec: vi.fn().mockImplementation((cmd, args) => {
        if (isGitAm3Way(cmd, args)) {
          throw new Error("CONFLICT (content): Merge conflict in file.txt");
        }
        return Promise.resolve(0);
      }),
      getExecOutput: vi.fn().mockImplementation(() => {
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }),
    };

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({});

    // No base_commit provided - fallback should not be possible
    const result = await handler({ title: "Test PR", body: "Test body", branch: "test-branch" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toBe("Failed to apply patch");
    expect(global.core.warning).toHaveBeenCalledWith("No base_commit embedded in patch - fallback not possible");
  });

  it("should reuse existing remote branch when preserve-branch-name and recreate-ref are true (force-delete then recreate)", async () => {
    // Simulate the remote branch existing (ls-remote returns content)
    let renameCalled = false;
    global.exec = {
      exec: vi.fn().mockImplementation((cmd, args) => {
        const cmdStr = `${cmd} ${(args || []).join(" ")}`;
        if (cmdStr.includes("git branch -m")) {
          renameCalled = true;
        }
        return Promise.resolve(0);
      }),
      getExecOutput: vi.fn().mockImplementation((cmd, args) => {
        const cmdStr = `${cmd} ${(args || []).join(" ")}`;
        if (cmdStr.includes("ls-remote --heads origin")) {
          return Promise.resolve({ exitCode: 0, stdout: "abc123\trefs/heads/preserve-me\n", stderr: "" });
        }
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }),
    };

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ preserve_branch_name: true, recreate_ref: true });

    const result = await handler({ title: "Test PR", body: "Test body", branch: "preserve-me", base_commit: MOCK_BASE_COMMIT_SHA }, {});

    expect(result.success).toBe(true);
    // Should have called deleteRef to force-delete the existing remote branch
    expect(global.github.rest.git.deleteRef).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      ref: "heads/preserve-me",
    });
    // Should NOT have renamed the local branch (preserve-branch-name keeps the name)
    expect(renameCalled).toBe(false);
    // Should NOT have warned about appending random suffix
    const warningCalls = global.core.warning.mock.calls.map(call => String(call[0]));
    expect(warningCalls.some(msg => msg.includes("appending random suffix"))).toBe(false);
    // Should have warned about reusing the branch
    expect(warningCalls.some(msg => msg.includes("reusing it") && msg.includes("recreate-ref"))).toBe(true);
  });

  it("should fall back when preserve-branch-name is true but recreate-ref is false and remote branch exists", async () => {
    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockImplementation((cmd, args) => {
        const cmdStr = `${cmd} ${(args || []).join(" ")}`;
        if (cmdStr.includes("ls-remote --heads origin")) {
          return Promise.resolve({ exitCode: 0, stdout: "abc123\trefs/heads/preserve-me\n", stderr: "" });
        }
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }),
    };

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ preserve_branch_name: true, fallback_as_issue: false });

    const result = await handler({ title: "Test PR", body: "Test body", branch: "preserve-me", base_commit: MOCK_BASE_COMMIT_SHA }, {});

    expect(result.success).toBe(false);
    expect(result.error_type).toBe("push_failed");
    expect(result.error).toContain("already exists and preserve-branch-name is enabled");
    expect(result.error).toContain("recreate-ref");
    // Should NOT have called deleteRef when recreate-ref is not enabled
    expect(global.github.rest.git.deleteRef).not.toHaveBeenCalled();
  });

  it("should fall back to issue when deleteRef fails for recreate-ref reuse", async () => {
    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockImplementation((cmd, args) => {
        const cmdStr = `${cmd} ${(args || []).join(" ")}`;
        if (cmdStr.includes("ls-remote --heads origin")) {
          return Promise.resolve({ exitCode: 0, stdout: "abc123\trefs/heads/preserve-me\n", stderr: "" });
        }
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }),
    };
    // Simulate deleteRef failing with a non-recoverable error
    global.github.rest.git.deleteRef = vi.fn().mockRejectedValue(Object.assign(new Error("Forbidden"), { status: 403 }));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ preserve_branch_name: true, recreate_ref: true, fallback_as_issue: false });

    const result = await handler({ title: "Test PR", body: "Test body", branch: "preserve-me", base_commit: MOCK_BASE_COMMIT_SHA }, {});

    expect(result.success).toBe(false);
    expect(result.error_type).toBe("push_failed");
    expect(result.error).toContain('Failed to delete existing remote branch "preserve-me"');
  });

  it("should rename with random suffix when deleteRef is blocked by branch protection rules (recreate-ref fallback)", async () => {
    /** @type {any} */
    let capturedRenamedBranch = null;
    global.exec = {
      exec: vi.fn().mockImplementation((cmd, args) => {
        const cmdStr = `${cmd} ${(args || []).join(" ")}`;
        // Capture the new branch name from: git branch -m <old> <new>
        const renameMatch = cmdStr.match(/git branch -m \S+ (\S+)/);
        if (renameMatch) {
          capturedRenamedBranch = renameMatch[1];
        }
        return Promise.resolve(0);
      }),
      getExecOutput: vi.fn().mockImplementation((cmd, args) => {
        const cmdStr = `${cmd} ${(args || []).join(" ")}`;
        if (cmdStr.includes("ls-remote --heads origin")) {
          return Promise.resolve({ exitCode: 0, stdout: "abc123\trefs/heads/chaos/preserve-me\n", stderr: "" });
        }
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }),
    };
    // Simulate the 422 "Repository rule violations found / Cannot delete this branch" error
    // that occurs when chaos/* branches are protected by a ruleset that blocks deletion.
    const ruleViolationError = Object.assign(new Error("Repository rule violations found\n\nCannot delete this branch\n\n - https://docs.github.com/rest/git/refs#delete-a-reference"), { status: 422 });
    global.github.rest.git.deleteRef = vi.fn().mockRejectedValue(ruleViolationError);

    const pushSignedCommitsModule = require("./push_signed_commits.cjs");
    const pushSignedSpy = vi.spyOn(pushSignedCommitsModule, "pushSignedCommits").mockResolvedValue("bundle-tip");

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ preserve_branch_name: true, recreate_ref: true });

    const result = await handler({ title: "Test PR", body: "Test body", branch: "chaos/preserve-me", base_commit: MOCK_BASE_COMMIT_SHA }, {});

    expect(result.success).toBe(true);
    // Should have attempted to delete the ref
    expect(global.github.rest.git.deleteRef).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      ref: "heads/chaos/preserve-me",
    });
    // Should have fallen back to rename with suffix
    expect(capturedRenamedBranch).not.toBeNull();
    expect(capturedRenamedBranch).toMatch(/^chaos\/preserve-me-[0-9a-f]{8}$/);
    const warningCalls = global.core.warning.mock.calls.map(call => String(call[0]));
    expect(warningCalls.some(msg => msg.includes("cannot be deleted due to branch protection rules"))).toBe(true);
    expect(warningCalls.some(msg => msg.includes("appending random suffix"))).toBe(true);
    // The renamed branch must be used for the push and PR creation, not the original protected name
    expect(pushSignedSpy).toHaveBeenCalledWith(expect.objectContaining({ branch: capturedRenamedBranch }));
    expect(global.github.rest.pulls.create).toHaveBeenCalledWith(expect.objectContaining({ head: capturedRenamedBranch }));
    expect(global.github.rest.pulls.create).not.toHaveBeenCalledWith(expect.objectContaining({ head: "chaos/preserve-me" }));

    pushSignedSpy.mockRestore();
  });

  it("should append random suffix when preserve-branch-name is false and remote branch already exists", async () => {
    let renameCalled = false;
    global.exec = {
      exec: vi.fn().mockImplementation((cmd, args) => {
        const cmdStr = `${cmd} ${(args || []).join(" ")}`;
        if (cmdStr.includes("git branch -m")) {
          renameCalled = true;
        }
        return Promise.resolve(0);
      }),
      getExecOutput: vi.fn().mockImplementation((cmd, args) => {
        const cmdStr = `${cmd} ${(args || []).join(" ")}`;
        if (cmdStr.includes("ls-remote --heads origin")) {
          return Promise.resolve({ exitCode: 0, stdout: "abc123\trefs/heads/some-branch\n", stderr: "" });
        }
        return Promise.resolve({ exitCode: 0, stdout: "", stderr: "" });
      }),
    };

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({});

    const result = await handler({ title: "Test PR", body: "Test body", branch: "some-branch", base_commit: MOCK_BASE_COMMIT_SHA }, {});

    expect(result.success).toBe(true);
    expect(renameCalled).toBe(true);
    const warningCalls = global.core.warning.mock.calls.map(call => String(call[0]));
    expect(warningCalls.some(msg => msg.includes("appending random suffix"))).toBe(true);
  });
});

describe("create_pull_request - copilot assignee on fallback issues", () => {
  let originalEnv;
  let tempDir;

  beforeEach(() => {
    originalEnv = { ...process.env };
    process.env.GH_AW_WORKFLOW_ID = "test-workflow";
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "create-pr-copilot-test-"));

    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };

    // Push fails to trigger the fallback-issue path; issue creation succeeds
    global.github = {
      request: vi.fn().mockResolvedValue({ data: { id: "task-123" } }),
      rest: {
        pulls: {
          create: vi.fn().mockRejectedValue(Object.assign(new Error("Permission denied"), { status: 403 })),
          requestReviewers: vi.fn().mockResolvedValue({}),
        },
        repos: {
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
        },
        issues: {
          create: vi.fn().mockResolvedValue({ data: { number: 99, html_url: "https://github.com/test/issues/99" } }),
          addLabels: vi.fn().mockResolvedValue({}),
          checkUserCanBeAssigned: vi.fn().mockResolvedValue({}),
          get: vi.fn().mockResolvedValue({ data: { id: 12345, number: 99, assignees: [], html_url: "", title: "", body: "" } }),
        },
        users: {
          getByUsername: vi.fn().mockResolvedValue({ data: { id: 99999 } }),
        },
      },
    };

    global.context = {
      eventName: "issues",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {},
      runId: "12345",
    };

    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockImplementation(async (program, args) => {
        // Return empty for rev-list so pushSignedCommits exits early (no commits to replay).
        // These tests focus on copilot assignment, not the signed-commit push path.
        if (program === "git" && args[0] === "rev-list") {
          return { exitCode: 0, stdout: "", stderr: "" };
        }
        return { exitCode: 0, stdout: "main", stderr: "" };
      }),
    };

    delete require.cache[require.resolve("./create_pull_request.cjs")];
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) {
        delete process.env[key];
      }
    }
    Object.assign(process.env, originalEnv);

    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }

    delete global.core;
    delete global.github;
    delete global.context;
    delete global.exec;
    vi.clearAllMocks();
  });

  it("should not call request for copilot assignment when GH_AW_ASSIGN_COPILOT is not set", async () => {
    delete process.env.GH_AW_ASSIGN_COPILOT;

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ assignees: ["copilot"], allow_empty: true });
    await handler({ title: "Test PR", body: "Test body" }, {});

    // No REST task creation calls for copilot assignment
    expect(global.github.request).not.toHaveBeenCalled();
  });

  it("should not call request when copilot is not in assignees even if GH_AW_ASSIGN_COPILOT is true", async () => {
    process.env.GH_AW_ASSIGN_COPILOT = "true";

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ assignees: ["user1"], allow_empty: true });
    await handler({ title: "Test PR", body: "Test body" }, {});

    expect(global.github.request).not.toHaveBeenCalled();
  });

  it("should strip copilot from REST assignees for fallback issue but assign via issue assignees REST when enabled", async () => {
    process.env.GH_AW_ASSIGN_COPILOT = "true";

    // Mock findAgent → getIssueDetails → assignAgentToIssue
    global.github.rest.issues.get.mockResolvedValueOnce({
      data: { id: 12345, number: 99, assignees: [], html_url: "", title: "", body: "" },
    });
    // GET assignability check + POST assignment
    global.github.request.mockResolvedValueOnce({ status: 204 }).mockResolvedValueOnce({});

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ assignees: ["copilot", "user1"], allow_empty: true });
    await handler({ title: "Test PR", body: "Test body" }, {});

    // Copilot should NOT appear in the REST issue creation payload
    const issueCall = global.github.rest.issues.create.mock.calls[0][0];
    expect(issueCall.assignees).not.toContain("copilot");
    expect(issueCall.assignees).toContain("user1");

    // One request for issue-scoped alias validation and one for REST assignment
    expect(global.github.request).toHaveBeenCalledTimes(2);
    const requestedRoutes = global.github.request.mock.calls.map(([route]) => route);
    expect(requestedRoutes).toEqual(expect.arrayContaining(["GET /repos/{owner}/{repo}/issues/{issue_number}/assignees/{assignee}", "POST /repos/{owner}/{repo}/issues/{issue_number}/assignees"]));
  });

  it("should use configured fallback_labels for fallback issues instead of PR labels", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true, fallback_labels: ["failure", "automated"] });
    await handler({ title: "Test PR", body: "Test body", labels: ["pr-label"] }, {});

    const issueCall = global.github.rest.issues.create.mock.calls[0][0];
    expect(issueCall.labels).toEqual(["agentic-workflows", "failure", "automated"]);
  });

  it("should sanitize closing keywords in permission-denied fallback issue body", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });
    await handler({ title: "Test PR", body: "Test body\n\nCloses #57\nResolves test-owner/test-repo#58" }, {});

    const issueCall = global.github.rest.issues.create.mock.calls[0][0];
    expect(issueCall.body).toContain("Closes \\#57");
    expect(issueCall.body).toContain("Resolves test-owner/test-repo\\#58");
    expect(issueCall.body).not.toContain("Closes #57");
    expect(issueCall.body).not.toContain("Resolves test-owner/test-repo#58");
  });

  it("should keep permission details before the footer in the fallback issue body", async () => {
    global.github.rest.pulls.create.mockRejectedValue(new Error("GitHub Actions is not permitted to create or approve pull requests"));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });
    await handler({ title: "Test PR", body: "Permission path body" }, {});

    const issueBody = global.github.rest.issues.create.mock.calls[0][0].body;
    const tipIndex = issueBody.indexOf("Your pull request is ready to create! 🎉 ✅");
    const bodyIndex = issueBody.indexOf("Permission path body");
    const noteIndex = issueBody.indexOf("GitHub Actions is not permitted");
    const footerIndex = issueBody.indexOf("<!-- gh-aw-workflow-id: test-workflow -->");
    expect(tipIndex).toBeLessThan(bodyIndex);
    expect(bodyIndex).toBeLessThan(noteIndex);
    expect(noteIndex).toBeLessThan(footerIndex);
  });
});

describe("create_pull_request - threat detection caution", () => {
  let tempDir;
  let originalEnv;

  beforeEach(() => {
    originalEnv = { ...process.env };
    process.env.GH_AW_WORKFLOW_ID = "test-workflow";
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "create-pr-threat-test-"));
    const promptsDir = path.join(tempDir, "prompts");
    fs.mkdirSync(promptsDir, { recursive: true });
    copyPromptTemplate(promptsDir, "manifest_protection_request_review.md");
    copyPromptTemplate(promptsDir, "manifest_protection_request_changes_review.md");
    copyPromptTemplate(promptsDir, "threat_warning_request_changes_review.md");
    copyPromptTemplate(promptsDir, "threat_detection_caution.md");
    copyPromptTemplate(promptsDir, "threat_detection_engine_error.md");
    copyPromptTemplate(promptsDir, "safe_outputs_disclosure_header.md");
    process.env.GH_AW_PROMPTS_DIR = promptsDir;

    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };
    global.github = {
      rest: {
        pulls: {
          create: vi.fn().mockResolvedValue({ data: { number: 42, html_url: "https://github.com/test/pull/42", node_id: "PR_42" } }),
          createReview: vi.fn().mockResolvedValue({ data: { id: 77, html_url: "https://github.com/test/review/77" } }),
          requestReviewers: vi.fn().mockResolvedValue({}),
        },
        repos: {
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
        },
        issues: {
          addLabels: vi.fn().mockResolvedValue({}),
        },
      },
      graphql: vi.fn(),
    };
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {},
    };
    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockResolvedValue({ exitCode: 0, stdout: "", stderr: "" }),
    };

    delete require.cache[require.resolve("./create_pull_request.cjs")];
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) {
        delete process.env[key];
      }
    }
    Object.assign(process.env, originalEnv);

    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }

    delete global.core;
    delete global.github;
    delete global.context;
    delete global.exec;
    vi.clearAllMocks();
  });

  it("should prepend caution alert at top of PR body when GH_AW_DETECTION_CONCLUSION is warning", async () => {
    process.env.GH_AW_DETECTION_CONCLUSION = "warning";

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });
    await handler({ title: "Test PR", body: "Agent body content" }, {});

    const prBody = global.github.rest.pulls.create.mock.calls[0][0].body;
    // Caution alert should appear before the agent body content
    const cautionIndex = prBody.indexOf("[!CAUTION]");
    const bodyIndex = prBody.indexOf("Agent body content");
    expect(cautionIndex).toBeGreaterThanOrEqual(0);
    expect(bodyIndex).toBeGreaterThan(cautionIndex);
    expect(global.github.rest.pulls.createReview).toHaveBeenCalledTimes(1);
    const createReviewCall = global.github.rest.pulls.createReview.mock.calls[0][0];
    expect(createReviewCall.event).toBe("REQUEST_CHANGES");
    expect(createReviewCall.body).toContain("Threat detection produced a warning");
    expect(createReviewCall.body).toContain("need to be scrutinized");
  });

  it("should compose one REQUEST_CHANGES review when protected-files and threat warning are both active", async () => {
    process.env.GH_AW_DETECTION_CONCLUSION = "warning";
    process.env.GH_AW_DETECTION_REASON = "pattern-match";
    const patchPath = canonicalPatchPath("should-compose-one-request-changes-review-when-protected-fil");
    fs.writeFileSync(
      patchPath,
      `diff --git a/.github/aw/instructions.md b/.github/aw/instructions.md
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/.github/aw/instructions.md
@@ -0,0 +1 @@
+content
`
    );

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true, protected_path_prefixes: [".github/"], protected_files_policy: "request_review" });
    await handler({ title: "Test PR", body: "Agent body content", branch: "should-compose-one-request-changes-review-when-protected-fil" }, {});

    expect(global.github.rest.pulls.createReview).toHaveBeenCalledTimes(1);
    const createReviewCall = global.github.rest.pulls.createReview.mock.calls[0][0];
    expect(createReviewCall.event).toBe("REQUEST_CHANGES");
    expect(createReviewCall.body).toContain("Protected files were modified");
    expect(createReviewCall.body).toContain("Threat detection produced a warning");
    expect(createReviewCall.body).toContain("\n\n---\n\n");
  });

  it("should not include caution alert in PR body when GH_AW_DETECTION_CONCLUSION is not warning", async () => {
    delete process.env.GH_AW_DETECTION_CONCLUSION;

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });
    await handler({ title: "Test PR", body: "Agent body content" }, {});

    const prBody = global.github.rest.pulls.create.mock.calls[0][0].body;
    expect(prBody).not.toContain("[!CAUTION]");
    expect(global.github.rest.pulls.createReview).not.toHaveBeenCalled();
  });

  it("should add agentic-threat-detected label when GH_AW_DETECTION_CONCLUSION is warning", async () => {
    process.env.GH_AW_DETECTION_CONCLUSION = "warning";

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });
    await handler({ title: "Test PR", body: "Agent body content" }, {});

    const labelsCall = global.github.rest.issues.addLabels.mock.calls[0][0];
    expect(labelsCall.labels).toContain("agentic-threat-detected");
  });

  it("should not add agentic-threat-detected label when GH_AW_DETECTION_CONCLUSION is not warning", async () => {
    delete process.env.GH_AW_DETECTION_CONCLUSION;

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });
    await handler({ title: "Test PR", body: "Agent body content", labels: ["automation"] }, {});

    const labelsCall = global.github.rest.issues.addLabels.mock.calls[0][0];
    expect(labelsCall.labels).not.toContain("agentic-threat-detected");
  });

  it("should separate caution alert from body content with blank lines", async () => {
    process.env.GH_AW_DETECTION_CONCLUSION = "warning";

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });
    await handler({ title: "Test PR", body: "Agent body content" }, {});

    const prBody = global.github.rest.pulls.create.mock.calls[0][0].body;
    const cautionIndex = prBody.indexOf("[!CAUTION]");
    const bodyIndex = prBody.indexOf("Agent body content");
    // There must be at least two newlines between the end of the caution block and the body content
    const between = prBody.slice(cautionIndex, bodyIndex);
    expect((between.match(/\n/g) || []).length).toBeGreaterThanOrEqual(2);
  });
});

describe("create_pull_request - rate-limit retry", () => {
  let originalEnv;
  let tempDir;

  /**
   * Creates a mock GitHub API rate-limit error object (HTTP 403 with x-ratelimit-remaining: 0)
   * that matches what octokit returns when the installation token quota is exhausted.
   * @param {string} [message]
   * @returns {Error}
   */
  function createRateLimitError(message = "API rate limit exceeded") {
    return Object.assign(new Error(message), {
      status: 403,
      response: { headers: { "x-ratelimit-remaining": "0" }, status: 403 },
    });
  }

  beforeEach(() => {
    originalEnv = { ...process.env };
    process.env.GH_AW_WORKFLOW_ID = "test-workflow";
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "create-pr-rate-limit-test-"));

    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };

    global.github = {
      rest: {
        pulls: {
          create: vi.fn().mockResolvedValue({ data: { number: 42, html_url: "https://github.com/test/pull/42" } }),
          requestReviewers: vi.fn().mockResolvedValue({}),
        },
        repos: {
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
        },
        issues: {
          create: vi.fn().mockResolvedValue({ data: { number: 99, html_url: "https://github.com/test/issues/99" } }),
          addLabels: vi.fn().mockResolvedValue({}),
        },
      },
      graphql: vi.fn(),
    };

    global.context = {
      eventName: "issues",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {},
      runId: "12345",
    };

    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockImplementation(async (program, args) => {
        if (program === "git" && args[0] === "rev-list") {
          return { exitCode: 0, stdout: "1", stderr: "" };
        }
        return { exitCode: 0, stdout: "main", stderr: "" };
      }),
    };

    delete require.cache[require.resolve("./create_pull_request.cjs")];
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) {
        delete process.env[key];
      }
    }
    Object.assign(process.env, originalEnv);

    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }

    delete global.core;
    delete global.github;
    delete global.context;
    delete global.exec;
    vi.clearAllMocks();
  });

  it("should retry PR creation on rate limit error and succeed", async () => {
    vi.useFakeTimers();
    try {
      global.github.rest.pulls.create.mockRejectedValueOnce(createRateLimitError()).mockResolvedValue({ data: { number: 42, html_url: "https://github.com/test/pull/42" } });

      const { main } = require("./create_pull_request.cjs");
      const handler = await main({ allow_empty: true });

      const resultPromise = handler({ title: "Test PR", body: "Test body" }, {});

      await vi.runAllTimersAsync();

      const result = await resultPromise;

      expect(result.success).toBe(true);
      expect(result.number).toBe(42);
      // 1 initial (rate-limited) + 1 retry (succeeds) = 2 calls total
      expect(global.github.rest.pulls.create).toHaveBeenCalledTimes(2);
      expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("create pull request"));
    } finally {
      vi.useRealTimers();
    }
  });

  it("should fall back to issue when PR creation fails after all rate-limit retries", async () => {
    vi.useFakeTimers();
    try {
      global.github.rest.pulls.create.mockRejectedValue(createRateLimitError());
      global.github.rest.issues.create.mockResolvedValue({ data: { number: 99, html_url: "https://github.com/test/issues/99" } });

      const { main } = require("./create_pull_request.cjs");
      const handler = await main({ allow_empty: true });

      const resultPromise = handler({ title: "Test PR", body: "Test body" }, {});

      await vi.runAllTimersAsync();

      const result = await resultPromise;

      // Should fall back to issue creation after PR retries are exhausted
      expect(result.success).toBe(true);
      expect(result.fallback_used).toBe(true);
      expect(result.issue_number).toBe(99);
      // 1 initial + 5 retries = 6 total PR creation attempts (RATE_LIMIT_RETRY_CONFIG.maxRetries = 5)
      expect(global.github.rest.pulls.create).toHaveBeenCalledTimes(6);
      expect(global.github.rest.issues.create).toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });

  it("should retry fallback issue creation on rate limit error and succeed", async () => {
    vi.useFakeTimers();
    try {
      // PR creation fails with a non-rate-limit error to trigger fallback immediately
      global.github.rest.pulls.create.mockRejectedValue(new Error("Some PR creation error"));
      // Fallback issue creation first fails with rate limit, then succeeds
      global.github.rest.issues.create.mockRejectedValueOnce(createRateLimitError()).mockResolvedValue({ data: { number: 99, html_url: "https://github.com/test/issues/99" } });

      const { main } = require("./create_pull_request.cjs");
      const handler = await main({ allow_empty: true });

      const resultPromise = handler({ title: "Test PR", body: "Test body" }, {});

      await vi.runAllTimersAsync();

      const result = await resultPromise;

      expect(result.success).toBe(true);
      expect(result.fallback_used).toBe(true);
      expect(result.issue_number).toBe(99);
      // Fallback issue: 1 rate-limited attempt + 1 successful retry = 2 calls
      expect(global.github.rest.issues.create).toHaveBeenCalledTimes(2);
      expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("create fallback issue"));
    } finally {
      vi.useRealTimers();
    }
  });

  it("should append a note to the fallback issue body when assignees are removed due to 422 error", async () => {
    // PR creation fails with a non-rate-limit error to trigger fallback immediately
    global.github.rest.pulls.create.mockRejectedValue(new Error("Some PR creation error"));

    const assigneeError = Object.assign(new Error("Validation Failed: assignees are invalid"), {
      status: 422,
      response: { status: 422 },
    });
    // First call fails with assignee 422, second succeeds
    global.github.rest.issues.create.mockRejectedValueOnce(assigneeError).mockResolvedValue({ data: { number: 77, html_url: "https://github.com/test/issues/77" } });

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true, assignees: ["user1", "user2"] });

    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(true);
    expect(result.fallback_used).toBe(true);
    expect(result.issue_number).toBe(77);
    expect(global.github.rest.issues.create).toHaveBeenCalledTimes(2);
    // Second call (without assignees) should have a note in the body
    const secondCall = global.github.rest.issues.create.mock.calls[1][0];
    expect(secondCall.assignees).toBeUndefined();
    expect(secondCall.body).toContain("user1");
    expect(secondCall.body).toContain("user2");
    expect(secondCall.body).toContain("could not be set");
  });
});

describe("create_pull_request - fallback issue issues-disabled (410)", () => {
  let originalEnv;
  let tempDir;

  function createIssuesDisabledError() {
    return Object.assign(new Error("Issues are disabled for this repository"), {
      status: 410,
      response: { status: 410 },
    });
  }

  function createAssigneeError(message = "assignee is invalid") {
    return Object.assign(new Error(message), {
      status: 422,
      response: { status: 422 },
    });
  }

  beforeEach(() => {
    originalEnv = { ...process.env };
    process.env.GH_AW_WORKFLOW_ID = "test-workflow";
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "create-pr-issues-disabled-test-"));

    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };

    global.github = {
      rest: {
        pulls: {
          create: vi.fn().mockResolvedValue({ data: { number: 42, html_url: "https://github.com/test/pull/42" } }),
          requestReviewers: vi.fn().mockResolvedValue({}),
        },
        repos: {
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
        },
        issues: {
          create: vi.fn().mockResolvedValue({ data: { number: 99, html_url: "https://github.com/test/issues/99" } }),
          addLabels: vi.fn().mockResolvedValue({}),
        },
      },
      graphql: vi.fn(),
    };

    global.context = {
      eventName: "issues",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {},
      runId: "12345",
    };

    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockImplementation(async (program, args) => {
        if (program === "git" && args[0] === "rev-list") {
          return { exitCode: 0, stdout: "1", stderr: "" };
        }
        return { exitCode: 0, stdout: "main", stderr: "" };
      }),
    };

    delete require.cache[require.resolve("./create_pull_request.cjs")];
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) {
        delete process.env[key];
      }
    }
    Object.assign(process.env, originalEnv);

    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }

    delete global.core;
    delete global.github;
    delete global.context;
    delete global.exec;
    vi.clearAllMocks();
  });

  it("should fall back to workflow repo when target repo has issues disabled (410)", async () => {
    // Scenario: workflow runs in workflow-owner/workflow-repo but the PR targets
    // the context repo (test-owner/test-repo). Issues are disabled in test-owner/test-repo.
    // The fallback issue should be created in workflow-owner/workflow-repo instead.
    process.env.GITHUB_REPOSITORY = "workflow-owner/workflow-repo";
    global.github.rest.pulls.create.mockRejectedValue(new Error("Some PR creation error"));
    // Issue creation in PR target (test-owner/test-repo) fails with 410; succeeds in workflow repo
    global.github.rest.issues.create.mockRejectedValueOnce(createIssuesDisabledError()).mockResolvedValue({ data: { number: 55, html_url: "https://github.com/workflow-owner/workflow-repo/issues/55" } });

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });

    // Message targets context repo (test-owner/test-repo); workflow repo is workflow-owner/workflow-repo
    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(true);
    expect(result.fallback_used).toBe(true);
    expect(result.issue_number).toBe(55);
    // First call targets the PR repo (issues disabled), second targets the workflow repo
    expect(global.github.rest.issues.create).toHaveBeenCalledTimes(2);
    const secondCall = global.github.rest.issues.create.mock.calls[1][0];
    expect(secondCall.owner).toBe("workflow-owner");
    expect(secondCall.repo).toBe("workflow-repo");
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("Issues are disabled"));
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("workflow-owner/workflow-repo"));
  });

  it("should use GH_AW_FAILURE_ISSUE_REPO over workflow repo when issues are disabled (410)", async () => {
    // Scenario: issues are disabled in the PR target repo; GH_AW_FAILURE_ISSUE_REPO is configured.
    process.env.GITHUB_REPOSITORY = "workflow-owner/workflow-repo";
    process.env.GH_AW_FAILURE_ISSUE_REPO = "failure-owner/failure-repo";
    global.github.rest.pulls.create.mockRejectedValue(new Error("Some PR creation error"));
    // Issue creation in target repo fails with 410; succeeds in failure-issue repo
    global.github.rest.issues.create.mockRejectedValueOnce(createIssuesDisabledError()).mockResolvedValue({ data: { number: 66, html_url: "https://github.com/failure-owner/failure-repo/issues/66" } });

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(true);
    expect(result.fallback_used).toBe(true);
    expect(result.issue_number).toBe(66);
    expect(global.github.rest.issues.create).toHaveBeenCalledTimes(2);
    const secondCall = global.github.rest.issues.create.mock.calls[1][0];
    expect(secondCall.owner).toBe("failure-owner");
    expect(secondCall.repo).toBe("failure-repo");
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("failure-owner/failure-repo"));
  });

  it("should skip GH_AW_FAILURE_ISSUE_REPO when it matches the disabled repo and fall back to GITHUB_REPOSITORY", async () => {
    // Scenario: GH_AW_FAILURE_ISSUE_REPO is set but equals the disabled PR-target repo.
    // The fallback should skip it and use GITHUB_REPOSITORY instead.
    process.env.GITHUB_REPOSITORY = "workflow-owner/workflow-repo";
    // GH_AW_FAILURE_ISSUE_REPO points to the same repo the workflow is running against
    // (test-owner/test-repo), which is the repo that has issues disabled.
    process.env.GH_AW_FAILURE_ISSUE_REPO = "test-owner/test-repo";
    global.github.rest.pulls.create.mockRejectedValue(new Error("Some PR creation error"));
    // Issue creation in test-owner/test-repo fails with 410; succeeds in workflow repo
    global.github.rest.issues.create.mockRejectedValueOnce(createIssuesDisabledError()).mockResolvedValue({ data: { number: 77, html_url: "https://github.com/workflow-owner/workflow-repo/issues/77" } });

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(true);
    expect(result.fallback_used).toBe(true);
    expect(result.issue_number).toBe(77);
    // The second call should target the workflow repo, not GH_AW_FAILURE_ISSUE_REPO
    const secondCall = global.github.rest.issues.create.mock.calls[1][0];
    expect(secondCall.owner).toBe("workflow-owner");
    expect(secondCall.repo).toBe("workflow-repo");
  });

  it("should handle 422 assignee error in the alternate repo after a 410 redirect", async () => {
    // Scenario: 410 from original repo redirects to alternate. The alternate repo
    // then returns a 422 assignee error. The recovery should remove assignees and retry.
    process.env.GITHUB_REPOSITORY = "workflow-owner/workflow-repo";
    global.github.rest.pulls.create.mockRejectedValue(new Error("Some PR creation error"));

    global.github.rest.issues.create
      .mockRejectedValueOnce(createIssuesDisabledError()) // 410 in original repo
      .mockRejectedValueOnce(createAssigneeError()) // 422 in alternate repo
      .mockResolvedValue({ data: { number: 88, html_url: "https://github.com/workflow-owner/workflow-repo/issues/88" } });

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true, assignees: ["invalid-user"] });

    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(true);
    expect(result.fallback_used).toBe(true);
    expect(result.issue_number).toBe(88);
    // Three calls: (1) 410 original, (2) 422 alternate, (3) success alternate without assignees
    expect(global.github.rest.issues.create).toHaveBeenCalledTimes(3);
    const thirdCall = global.github.rest.issues.create.mock.calls[2][0];
    expect(thirdCall.owner).toBe("workflow-owner");
    expect(thirdCall.repo).toBe("workflow-repo");
    expect(thirdCall.assignees).toBeUndefined();
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("assignee error"));
  });

  it("should return success:false when issues are disabled (410) and no alternate repo is available", async () => {
    // Workflow repo same as target repo — no alternate available
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    global.github.rest.pulls.create.mockRejectedValue(new Error("Some PR creation error"));
    global.github.rest.issues.create.mockRejectedValue(createIssuesDisabledError());

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });

    // No cross-repo target: itemRepo defaults to workflow repo, so payload matches GITHUB_REPOSITORY
    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toBeDefined();
    // Should warn about issues being disabled but not crash the whole step
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("Issues are disabled"));
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("no alternate repo"));
  });

  it("should trim whitespace from GH_AW_FAILURE_ISSUE_REPO env var", async () => {
    // Env vars set via YAML multiline scalars can carry leading/trailing whitespace.
    // parseRepo must trim before splitting to avoid a malformed owner slug.
    process.env.GITHUB_REPOSITORY = "workflow-owner/workflow-repo";
    process.env.GH_AW_FAILURE_ISSUE_REPO = "  failure-owner/failure-repo  ";
    global.github.rest.pulls.create.mockRejectedValue(new Error("Some PR creation error"));
    global.github.rest.issues.create.mockRejectedValueOnce(createIssuesDisabledError()).mockResolvedValue({ data: { number: 11, html_url: "https://github.com/failure-owner/failure-repo/issues/11" } });

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(true);
    // The trimmed failure repo should be used, not a malformed slug with spaces
    const secondCall = global.github.rest.issues.create.mock.calls[1][0];
    expect(secondCall.owner).toBe("failure-owner");
    expect(secondCall.repo).toBe("failure-repo");
  });

  it("should not loop infinitely when all candidate repos also have issues disabled", async () => {
    // Scenario: original target, GH_AW_FAILURE_ISSUE_REPO, and GITHUB_REPOSITORY all
    // have issues disabled. The triedOwnerRepos Set must prevent the back-and-forth
    // bounce and terminate gracefully with success:false.
    process.env.GITHUB_REPOSITORY = "workflow-owner/workflow-repo";
    process.env.GH_AW_FAILURE_ISSUE_REPO = "failure-owner/failure-repo";
    global.github.rest.pulls.create.mockRejectedValue(new Error("Some PR creation error"));
    // All repos return 410
    global.github.rest.issues.create.mockRejectedValue(createIssuesDisabledError());

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });

    const result = await handler({ title: "Test PR", body: "Test body" }, {});

    expect(result.success).toBe(false);
    // Should have tried original + failure + workflow repos (3 total), then stopped
    expect(global.github.rest.issues.create.mock.calls.length).toBe(3);
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("no alternate repo"));
  });
});

describe("create_pull_request - branch-prefix config", () => {
  let originalEnv;
  let tempDir;

  beforeEach(() => {
    originalEnv = { ...process.env };
    process.env.GH_AW_WORKFLOW_ID = "jsweep";
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "create-pr-branch-prefix-test-"));

    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) },
    };
    global.github = {
      rest: {
        pulls: { create: vi.fn().mockResolvedValue({ data: { number: 1, html_url: "https://github.com/test/pull/1" } }) },
        repos: { get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }) },
        issues: { addLabels: vi.fn().mockResolvedValue({}) },
      },
      graphql: vi.fn(),
    };
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {},
    };
    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockResolvedValue({ exitCode: 0, stdout: "", stderr: "" }),
    };

    delete require.cache[require.resolve("./create_pull_request.cjs")];
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) delete process.env[key];
    }
    Object.assign(process.env, originalEnv);
    if (tempDir && fs.existsSync(tempDir)) fs.rmSync(tempDir, { recursive: true, force: true });
    delete global.core;
    delete global.github;
    delete global.context;
    delete global.exec;
    vi.clearAllMocks();
  });

  it("should prepend branch-prefix to auto-generated branch name when agent provides no branch", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ branch_prefix: "signed/", allow_empty: true });

    await handler({ title: "Test PR", body: "body" }, {});

    const branchArg = global.github.rest.pulls.create.mock.calls[0][0].head;
    expect(branchArg).toMatch(/^signed\/jsweep-/);
  });

  it("should prepend branch-prefix to agent-specified branch name", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ branch_prefix: "signed/", allow_empty: true });

    await handler({ title: "Test PR", body: "body", branch: "my-feature" }, {});

    const branchArg = global.github.rest.pulls.create.mock.calls[0][0].head;
    expect(branchArg).toMatch(/^signed\/my-feature/);
  });

  it("should not double-apply branch-prefix when agent branch already starts with the prefix", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ branch_prefix: "signed/", allow_empty: true });

    await handler({ title: "Test PR", body: "body", branch: "signed/already-prefixed" }, {});

    const branchArg = global.github.rest.pulls.create.mock.calls[0][0].head;
    expect(branchArg).toMatch(/^signed\/already-prefixed/);
    expect(branchArg).not.toMatch(/^signed\/signed\//);
  });

  it("should not add any prefix when branch-prefix is not configured", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true });

    await handler({ title: "Test PR", body: "body" }, {});

    const branchArg = global.github.rest.pulls.create.mock.calls[0][0].head;
    expect(branchArg).toMatch(/^jsweep-/);
    expect(branchArg).not.toContain("signed/");
  });

  it("should use an owner-qualified PR head when head-repo differs from target-repo", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({
      allow_empty: true,
      "head-repo": "fork-owner/test-repo",
      allowed_repos: ["test-owner/test-repo", "fork-owner/test-repo"],
    });

    await handler({ title: "Test PR", body: "body", branch: "my-feature" }, {});

    const createCall = global.github.rest.pulls.create.mock.calls[0][0];
    expect(createCall.owner).toBe("test-owner");
    expect(createCall.repo).toBe("test-repo");
    expect(createCall.head).toMatch(/^fork-owner:my-feature(?:-[0-9a-f]+)?$/);
  });

  it("should normalize an invalid branch-prefix and emit a warning", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ branch_prefix: "bad prefix: ", allow_empty: true });

    await handler({ title: "Test PR", body: "body" }, {});

    expect(global.core.warning).toHaveBeenCalledWith(expect.stringMatching(/branch prefix.*characters that are invalid/i));
    const branchArg = global.github.rest.pulls.create.mock.calls[0][0].head;
    // normalized prefix "bad-prefix" should be applied
    expect(branchArg).toMatch(/^bad-prefix/);
  });
});

describe("create_pull_request - E003 file-limit fallback-to-issue", () => {
  let originalEnv;
  let tempDir;

  beforeEach(() => {
    originalEnv = { ...process.env };
    process.env.GH_AW_WORKFLOW_ID = "test-workflow";
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "create-pr-e003-test-"));

    // Set up prompts directory with the E003 template so getPromptPath resolves
    const promptsDir = path.join(tempDir, "prompts");
    fs.mkdirSync(promptsDir, { recursive: true });
    copyPromptTemplate(promptsDir, "e003_file_limit_fallback.md");
    copyPromptTemplate(promptsDir, "safe_outputs_disclosure_header.md");
    process.env.GH_AW_PROMPTS_DIR = promptsDir;

    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) },
    };

    global.github = {
      rest: {
        pulls: { create: vi.fn().mockResolvedValue({ data: { number: 1, html_url: "https://github.com/test/pull/1" } }) },
        repos: { get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }) },
        issues: {
          create: vi.fn().mockResolvedValue({ data: { number: 55, html_url: "https://github.com/test/issues/55" } }),
          addLabels: vi.fn().mockResolvedValue({}),
        },
      },
      graphql: vi.fn(),
    };

    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {},
      runId: "99999",
    };

    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockResolvedValue({ exitCode: 0, stdout: "", stderr: "" }),
    };

    delete require.cache[require.resolve("./create_pull_request.cjs")];
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) delete process.env[key];
    }
    Object.assign(process.env, originalEnv);
    if (tempDir && fs.existsSync(tempDir)) fs.rmSync(tempDir, { recursive: true, force: true });
    delete global.core;
    delete global.github;
    delete global.context;
    delete global.exec;
    vi.clearAllMocks();
  });

  // Build a patch string touching `n` unique files
  function buildOversizedPatch(n) {
    const lines = [];
    for (let i = 0; i < n; i++) {
      lines.push(`diff --git a/file${i}.txt b/file${i}.txt`);
      lines.push("index 1234567..abcdefg 100644");
      lines.push(`--- a/file${i}.txt`);
      lines.push(`+++ b/file${i}.txt`);
      lines.push("@@ -1,1 +1,1 @@");
      lines.push("-old");
      lines.push("+new");
    }
    return lines.join("\n");
  }

  it("should create a fallback issue when E003 fires and fallback_as_issue is true (default)", async () => {
    const patchPath = canonicalPatchPath("data/refresh");
    fs.writeFileSync(patchPath, buildOversizedPatch(101));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({});
    const result = await handler({ title: "Data refresh PR", body: "Daily update", branch: "data/refresh" }, {});

    expect(result.success).toBe(true);
    expect(result.fallback_used).toBe(true);
    expect(result.issue_number).toBe(55);

    // A fallback issue should have been created
    expect(global.github.rest.issues.create).toHaveBeenCalledTimes(1);
    const issueCall = global.github.rest.issues.create.mock.calls[0][0];

    // The body should contain the E003 error message and the actionable fix
    expect(issueCall.body).toContain("E003");
    expect(issueCall.body).toContain("max-patch-files");

    // The suggested limit must be >= the actual file count (101), not maxFiles * 2 (200)
    expect(issueCall.body).toContain("max-patch-files: 101");
    expect(issueCall.body).not.toContain("max-patch-files: 200");

    // PR creation should NOT have been attempted
    expect(global.github.rest.pulls.create).not.toHaveBeenCalled();
  });

  it("should use the actual received file count (not maxFiles*2) as the suggested limit", async () => {
    const patchPath = canonicalPatchPath("api/regen");
    fs.writeFileSync(patchPath, buildOversizedPatch(220));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({});
    const result = await handler({ title: "API regen PR", body: "Daily update", branch: "api/regen" }, {});

    expect(result.success).toBe(true);
    expect(result.fallback_used).toBe(true);

    const issueCall = global.github.rest.issues.create.mock.calls[0][0];
    // With default limit=100 and 220 files, old code would suggest 200; correct is 220
    expect(issueCall.body).toContain("max-patch-files: 220");
    expect(issueCall.body).not.toContain("max-patch-files: 200");
  });

  it("should sanitize and apply title prefix to fallback issue title", async () => {
    const patchPath = canonicalPatchPath("data/refresh");
    fs.writeFileSync(patchPath, buildOversizedPatch(101));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ title_prefix: "[bot]" });
    const result = await handler({ title: "Data refresh PR", body: "Daily update", branch: "data/refresh" }, {});

    expect(result.success).toBe(true);
    const issueCall = global.github.rest.issues.create.mock.calls[0][0];
    // Title prefix should be applied
    expect(issueCall.title).toMatch(/^\[bot\]/);
  });

  it("should return staged preview instead of creating a fallback issue when in staged mode", async () => {
    process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";

    const patchPath = canonicalPatchPath("data/refresh");
    fs.writeFileSync(patchPath, buildOversizedPatch(101));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({});
    const result = await handler({ title: "Data refresh PR", body: "Daily update", branch: "data/refresh" }, {});

    // Staged mode: no API side effects, just a preview
    expect(result.success).toBe(true);
    expect(result.staged).toBe(true);
    expect(result.fallback_used).toBeUndefined();
    expect(global.github.rest.issues.create).not.toHaveBeenCalled();
    expect(global.github.rest.pulls.create).not.toHaveBeenCalled();
    expect(global.core.summary.addRaw).toHaveBeenCalled();
  });

  it("should return success: false when E003 fires and fallback_as_issue is false", async () => {
    const patchPath = canonicalPatchPath("data/refresh");
    fs.writeFileSync(patchPath, buildOversizedPatch(101));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ fallback_as_issue: false });
    const result = await handler({ title: "Data refresh PR", body: "Daily update", branch: "data/refresh" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("E003");

    // No fallback issue should have been created
    expect(global.github.rest.issues.create).not.toHaveBeenCalled();
  });

  it("should pass when max_patch_files is raised above the file count", async () => {
    const patchPath = canonicalPatchPath("data/refresh");
    fs.writeFileSync(patchPath, buildOversizedPatch(150));

    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ max_patch_files: 200 });
    const result = await handler({ title: "Data refresh PR", body: "Daily update", branch: "data/refresh" }, {});

    // Should succeed — limit was raised
    expect(result.success).toBe(true);
    expect(result.fallback_used).toBeUndefined();
    expect(global.github.rest.pulls.create).toHaveBeenCalledTimes(1);
    expect(global.github.rest.issues.create).not.toHaveBeenCalled();
  });
});

describe("create_pull_request - stacked pull requests", () => {
  let tempDir;
  let originalEnv;
  let createdPrNumber;

  beforeEach(() => {
    originalEnv = { ...process.env };
    process.env.GH_AW_WORKFLOW_ID = "test-workflow";
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "create-pr-stacked-test-"));

    createdPrNumber = 100;

    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      startGroup: vi.fn(),
      endGroup: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };
    global.github = {
      rest: {
        pulls: {
          create: vi.fn().mockImplementation(async () => {
            const number = createdPrNumber++;
            return { data: { number, html_url: `https://github.com/test-owner/test-repo/pull/${number}`, node_id: `PR_${number}` } };
          }),
          requestReviewers: vi.fn().mockResolvedValue({}),
        },
        repos: {
          get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }),
          getBranch: vi.fn().mockResolvedValue({ data: { name: "release/1.0" } }),
        },
        issues: {
          addLabels: vi.fn().mockResolvedValue({}),
        },
      },
      graphql: vi.fn(),
    };
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {},
    };
    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockResolvedValue({ exitCode: 0, stdout: "", stderr: "" }),
    };

    delete require.cache[require.resolve("./create_pull_request.cjs")];
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) {
        delete process.env[key];
      }
    }
    Object.assign(process.env, originalEnv);

    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }

    delete global.core;
    delete global.github;
    delete global.context;
    delete global.exec;
    vi.clearAllMocks();
  });

  it("stacks a pull request on a branch created earlier in the same run", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true, max: 2, preserve_branch_name: true });

    const first = await handler({ title: "First", body: "First body", branch: "feature-1" }, {});
    expect(first.success).toBe(true);

    const second = await handler({ title: "Second", body: "Second body", branch: "feature-2", base: "feature-1", stack_position: 2, stack_root: "main" }, {});

    expect(second.success).toBe(true);
    expect(global.github.rest.pulls.create).toHaveBeenLastCalledWith(expect.objectContaining({ base: "feature-1", head: "feature-2" }));
    const secondBody = global.github.rest.pulls.create.mock.calls[1][0].body;
    expect(first.number).toBe(100);
    expect(secondBody).toContain(`Depends on #${first.number}`);
    expect(secondBody).toContain("<!-- gh-aw-stack:");
    expect(secondBody).toContain('"position":2');
  });

  it("resolves salted branch names when stacking on an earlier pull request", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true, max: 2 });

    const first = await handler({ title: "First", body: "First body", branch: "feature-1" }, {});
    expect(first.success).toBe(true);
    const firstBranch = global.github.rest.pulls.create.mock.calls[0][0].head;
    expect(firstBranch).not.toBe("feature-1");

    const second = await handler({ title: "Second", body: "Second body", branch: "feature-2", base: "feature-1" }, {});

    expect(second.success).toBe(true);
    expect(global.github.rest.pulls.create).toHaveBeenLastCalledWith(expect.objectContaining({ base: firstBranch }));
  });

  it("rejects a stacked pull request when stacked pull requests are disabled", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true, max: 2, preserve_branch_name: true, stacked: false });

    const first = await handler({ title: "First", body: "First body", branch: "feature-1" }, {});
    expect(first.success).toBe(true);

    const second = await handler({ title: "Second", body: "Second body", branch: "feature-2", base: "feature-1" }, {});

    expect(second.success).toBe(false);
    expect(second.error).toContain("Stacked pull requests are disabled");
    expect(second.error).toContain("main");
    expect(global.github.rest.pulls.create).toHaveBeenCalledTimes(1);
  });

  it("still allows the default base branch when stacked pull requests are disabled", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true, stacked: false });

    const result = await handler({ title: "Test PR", body: "Test body", base: "main", stack_position: 1 }, {});

    expect(result.success).toBe(true);
    const body = global.github.rest.pulls.create.mock.calls[0][0].body;
    expect(body).toContain("<!-- gh-aw-stack:");
  });

  it("rejects a stacked pull request when the base branch does not exist", async () => {
    const { main } = require("./create_pull_request.cjs");
    const notFound = Object.assign(new Error("Branch not found"), { status: 404 });
    global.github.rest.repos.getBranch = vi.fn().mockRejectedValue(notFound);

    const handler = await main({ allow_empty: true, allowed_base_branches: ["release/*"] });

    const result = await handler({ title: "Test PR", body: "Test body", base: "release/1.0" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("does not exist");
    expect(result.error).toContain("dependency order");
    expect(global.github.rest.pulls.create).not.toHaveBeenCalled();
  });

  it("creates a stacked pull request when the allowed base branch exists", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true, allowed_base_branches: ["release/*"] });

    const result = await handler({ title: "Test PR", body: "Test body", base: "release/1.0" }, {});

    expect(result.success).toBe(true);
    expect(global.github.rest.repos.getBranch).toHaveBeenCalledWith(expect.objectContaining({ branch: "release/1.0" }));
    expect(global.github.rest.pulls.create).toHaveBeenCalledWith(expect.objectContaining({ base: "release/1.0" }));
  });

  it("rejects a stacked pull request targeting a repo outside the allowlist before checking the base branch", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true, allowed_repos: ["test-owner/test-repo"] });

    const result = await handler({ title: "Test PR", body: "Test body", repo: "other-owner/other-repo", base: "release/1.0" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("is not in the allowed-repos list");
    // The base-branch existence check must never run against an unvalidated repo.
    expect(global.github.rest.repos.getBranch).not.toHaveBeenCalled();
    expect(global.github.rest.pulls.create).not.toHaveBeenCalled();
  });

  it("rejects a circular stacked pull request dependency", async () => {
    const { main } = require("./create_pull_request.cjs");
    const handler = await main({ allow_empty: true, max: 3, preserve_branch_name: true });

    expect((await handler({ title: "First", body: "First body", branch: "feature-1" }, {})).success).toBe(true);
    expect((await handler({ title: "Second", body: "Second body", branch: "feature-2", base: "feature-1" }, {})).success).toBe(true);

    // feature-1 already stacks below feature-2, so basing feature-1 on feature-2 closes the loop.
    const third = await handler({ title: "Third", body: "Third body", branch: "feature-1", base: "feature-2" }, {});

    expect(third.success).toBe(false);
    expect(third.error).toContain("Circular stacked pull request dependency detected");
  });
});
