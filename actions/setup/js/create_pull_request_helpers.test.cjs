// @ts-check
import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { createRequire } from "module";
import crypto from "crypto";
import fs from "fs";
import os from "os";
import path from "path";
import { spawnSync } from "child_process";

const require = createRequire(import.meta.url);

// Set up globals required by modules that reference `core` at load time.
global.core = {
  debug: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
};

const {
  MANAGED_FALLBACK_ISSUE_LABEL,
  LABEL_MAX_RETRIES,
  LABEL_INITIAL_DELAY_MS,
  LABEL_MAX_DELAY_MS,
  summarizeListForLog,
  createBundleTempRef,
  isLabelTransientError,
  parseAllowedBaseBranches,
  isBaseBranchAllowed,
  parseStringListConfig,
  mergeFallbackIssueLabels,
  sanitizeFallbackAssignees,
  neutralizeClosingKeywordsForIssueBody,
  generatePatchPreview,
  buildManifestProtectionCreatePrUrl,
  buildPushErrorSection,
  buildManualBranchRecoveryCommands,
  buildManualBranchApplyCommands,
} = require("./create_pull_request_helpers.cjs");

function runGit(args, cwd) {
  const result = spawnSync("git", args, { cwd, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
  expect(result.status, `git ${args.join(" ")} failed\nstdout:\n${result.stdout}\nstderr:\n${result.stderr}`).toBe(0);
  return result.stdout.trim();
}

function runShell(script, cwd) {
  const result = spawnSync("bash", ["-c", `set -euo pipefail\n${script}`], { cwd, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
  expect(result.status, `shell failed\nstdout:\n${result.stdout}\nstderr:\n${result.stderr}\nscript:\n${script}`).toBe(0);
  return result.stdout.trim();
}

function skipArtifactDownload(commands) {
  return commands.replace(/^gh run download .*$/m, "# artifact already downloaded");
}

describe("create_pull_request_helpers - constants", () => {
  it("MANAGED_FALLBACK_ISSUE_LABEL is the correct triage label", () => {
    expect(MANAGED_FALLBACK_ISSUE_LABEL).toBe("agentic-workflows");
  });

  it("label retry constants have sensible values", () => {
    expect(LABEL_MAX_RETRIES).toBeGreaterThan(0);
    expect(LABEL_INITIAL_DELAY_MS).toBeGreaterThan(0);
    expect(LABEL_MAX_DELAY_MS).toBeGreaterThan(LABEL_INITIAL_DELAY_MS);
  });
});

// ---------------------------------------------------------------------------
// summarizeListForLog
// ---------------------------------------------------------------------------
describe("summarizeListForLog", () => {
  it("returns (none) for empty array", () => {
    expect(summarizeListForLog([])).toBe("(none)");
  });

  it("returns (none) for non-array input", () => {
    // @ts-ignore – deliberate bad input test
    expect(summarizeListForLog(null)).toBe("(none)");
    // @ts-ignore
    expect(summarizeListForLog(undefined)).toBe("(none)");
    // @ts-ignore
    expect(summarizeListForLog("string")).toBe("(none)");
  });

  it("returns all items joined when count is within limit", () => {
    expect(summarizeListForLog(["a", "b", "c"])).toBe("a, b, c");
  });

  it("truncates with overflow count when exceeding default limit of 10", () => {
    const items = Array.from({ length: 15 }, (_, i) => `item${i}`);
    const result = summarizeListForLog(items);
    expect(result).toContain("... and 5 more");
    expect(result).toContain("item0");
    expect(result).not.toContain("item10");
  });

  it("respects a custom limit", () => {
    const result = summarizeListForLog(["a", "b", "c", "item-d", "item-e"], 3);
    expect(result).toContain("... and 2 more");
    expect(result).toContain("a, b, c");
    expect(result).not.toContain("item-d");
  });

  it("does not truncate when count exactly equals limit", () => {
    const items = ["x", "y", "z"];
    const result = summarizeListForLog(items, 3);
    expect(result).toBe("x, y, z");
    expect(result).not.toContain("more");
  });

  it("single item returns that item with no trailing comma", () => {
    expect(summarizeListForLog(["only"])).toBe("only");
  });
});

// ---------------------------------------------------------------------------
// createBundleTempRef
// ---------------------------------------------------------------------------
describe("createBundleTempRef", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("produces a ref under refs/bundles/", () => {
    expect(createBundleTempRef("feature/my-branch")).toMatch(/^refs\/bundles\//);
  });

  it("replaces non-alphanumeric/hyphen characters with hyphens", () => {
    const ref = createBundleTempRef("feature/my_branch.with.dots");
    // slashes and dots should become hyphens
    expect(ref).toMatch(/^refs\/bundles\/create-pr-feature-my-branch-with-dots-[a-f0-9]{8}$/);
  });

  it("appends an 8-char hex suffix for collision avoidance", () => {
    const ref = createBundleTempRef("main");
    expect(ref).toMatch(/^refs\/bundles\/create-pr-main-[a-f0-9]{8}$/);
  });

  it("produces different refs when crypto returns different bytes", () => {
    vi.spyOn(crypto, "randomBytes").mockReturnValueOnce(Buffer.from("aabbccdd", "hex")).mockReturnValueOnce(Buffer.from("11223344", "hex"));

    const ref1 = createBundleTempRef("same-branch");
    const ref2 = createBundleTempRef("same-branch");

    expect(ref1).toBe("refs/bundles/create-pr-same-branch-aabbccdd");
    expect(ref2).toBe("refs/bundles/create-pr-same-branch-11223344");
  });
});

describe("manual branch recovery/apply commands", () => {
  it("shell-quotes dynamic recovery arguments and resolves bundle heads", () => {
    const commands = buildManualBranchRecoveryCommands({
      hasBundleFile: true,
      runId: "123; echo injected",
      artifactFileName: "agent'changes.bundle",
      branchName: "feature/branch'; echo injected",
      baseBranch: "main",
      tempRef: "refs/bundles/tmp'; echo injected",
    });

    expect(commands).toContain("git bundle list-heads");
    expect(commands).toContain('$2 == "HEAD"');
    expect(commands).toContain("gh run download '123; echo injected' -n agent -D '/tmp/agent-123; echo injected'");
    expect(commands).toContain("bundle_path='/tmp/agent-123; echo injected/agent'\\''changes.bundle'");
    expect(commands).toContain("target_ref='refs/heads/feature/branch'\\''; echo injected'");
    expect(commands).not.toContain("refs/heads/feature/branch'; echo injected:");
  });

  it("manual recovery bundle commands work with a real HEAD-only bundle", () => {
    const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), "manual-recovery-head-bundle-"));
    const artifactRunId = `head-only-recovery-${process.pid}-${Date.now()}`;
    const artifactDir = path.join(os.tmpdir(), `agent-${artifactRunId}`);

    try {
      const workDir = path.join(tempRoot, "work");
      const targetDir = path.join(tempRoot, "target");
      fs.mkdirSync(artifactDir, { recursive: true });
      fs.mkdirSync(workDir, { recursive: true });
      fs.mkdirSync(targetDir, { recursive: true });

      runGit(["init"], workDir);
      runGit(["config", "user.email", "test@example.com"], workDir);
      runGit(["config", "user.name", "Test User"], workDir);
      fs.writeFileSync(path.join(workDir, "file.txt"), "updated\n");
      runGit(["add", "file.txt"], workDir);
      runGit(["commit", "-m", "bundle head"], workDir);
      runGit(["bundle", "create", path.join(artifactDir, "head-only.bundle"), "HEAD"], workDir);

      runGit(["init"], targetDir);
      const commands = buildManualBranchRecoveryCommands({
        hasBundleFile: true,
        runId: artifactRunId,
        artifactFileName: "head-only.bundle",
        branchName: "feature/recovered",
        tempRef: "refs/bundles/manual-recovery",
      });

      expect(runGit(["bundle", "list-heads", path.join(artifactDir, "head-only.bundle")], targetDir)).toMatch(/\sHEAD$/);
      runShell(skipArtifactDownload(commands), targetDir);

      expect(fs.readFileSync(path.join(targetDir, "file.txt"), "utf8")).toBe("updated\n");
      expect(runGit(["rev-parse", "feature/recovered"], targetDir)).toBe(runGit(["rev-parse", "HEAD"], workDir));
    } finally {
      fs.rmSync(tempRoot, { recursive: true, force: true });
      fs.rmSync(artifactDir, { recursive: true, force: true });
    }
  });

  it("manual apply bundle commands work with a real HEAD-only bundle", () => {
    const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), "manual-apply-head-bundle-"));
    const artifactRunId = `head-only-${process.pid}-${Date.now()}`;
    const artifactDir = path.join(os.tmpdir(), `agent-${artifactRunId}`);

    try {
      const remoteDir = path.join(tempRoot, "remote.git");
      const workDir = path.join(tempRoot, "work");
      const targetDir = path.join(tempRoot, "target");
      fs.mkdirSync(artifactDir, { recursive: true });

      runGit(["init", "--bare", remoteDir], tempRoot);
      fs.mkdirSync(workDir, { recursive: true });
      runGit(["init"], workDir);
      runGit(["config", "user.email", "test@example.com"], workDir);
      runGit(["config", "user.name", "Test User"], workDir);
      fs.writeFileSync(path.join(workDir, "file.txt"), "base\n");
      runGit(["add", "file.txt"], workDir);
      runGit(["commit", "-m", "base"], workDir);
      runGit(["branch", "-M", "feature"], workDir);
      runGit(["remote", "add", "origin", remoteDir], workDir);
      runGit(["push", "-u", "origin", "feature"], workDir);

      runGit(["clone", remoteDir, targetDir], tempRoot);
      runGit(["checkout", "feature"], targetDir);

      fs.writeFileSync(path.join(workDir, "file.txt"), "updated\n");
      runGit(["commit", "-am", "update"], workDir);
      runGit(["bundle", "create", path.join(artifactDir, "head-only.bundle"), "HEAD"], workDir);

      const commands = buildManualBranchApplyCommands({
        hasBundleFile: true,
        runId: artifactRunId,
        artifactFileName: "head-only.bundle",
        branchName: "feature",
        branchRemote: "origin",
      });

      expect(commands).toContain("git checkout -B 'feature' FETCH_HEAD");
      expect(commands).toContain("git push 'origin' 'HEAD:refs/heads/feature'");
      expect(runGit(["bundle", "list-heads", path.join(artifactDir, "head-only.bundle")], targetDir)).toMatch(/\sHEAD$/);
      runShell(skipArtifactDownload(commands), targetDir);

      expect(fs.readFileSync(path.join(targetDir, "file.txt"), "utf8")).toBe("updated\n");
      expect(runGit(["rev-parse", "feature"], targetDir)).toBe(runGit(["--git-dir", remoteDir, "rev-parse", "feature"], tempRoot));
    } finally {
      fs.rmSync(tempRoot, { recursive: true, force: true });
      fs.rmSync(artifactDir, { recursive: true, force: true });
    }
  });
});

// ---------------------------------------------------------------------------
// isLabelTransientError
// ---------------------------------------------------------------------------
describe("isLabelTransientError", () => {
  it("returns true for GitHub node-ID race-condition message", () => {
    const err = new Error("Could not resolve to a node with the global id 'PRI_xxx'");
    expect(isLabelTransientError(err)).toBe(true);
  });

  it("returns false for a plain non-transient error", () => {
    const err = new Error("Not found");
    expect(isLabelTransientError(err)).toBe(false);
  });

  it("returns true for a 429 rate-limit error (isTransientError path)", () => {
    const err = Object.assign(new Error("API rate limit exceeded"), { status: 429 });
    expect(isLabelTransientError(err)).toBe(true);
  });

  it("returns true for a 503 service unavailable error (isTransientError path)", () => {
    // isTransientError checks both message content AND status codes; this exercises the message path
    const err = new Error("503 service unavailable");
    expect(isLabelTransientError(err)).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// parseAllowedBaseBranches
// ---------------------------------------------------------------------------
describe("parseAllowedBaseBranches", () => {
  it("returns empty Set for undefined input", () => {
    expect(parseAllowedBaseBranches(undefined).size).toBe(0);
  });

  it("parses an array of branch names", () => {
    const result = parseAllowedBaseBranches(["main", "develop"]);
    expect(result).toEqual(new Set(["main", "develop"]));
  });

  it("trims whitespace from array entries", () => {
    const result = parseAllowedBaseBranches([" main ", "  develop  "]);
    expect(result).toEqual(new Set(["main", "develop"]));
  });

  it("filters out empty array entries", () => {
    const result = parseAllowedBaseBranches(["main", "", "  "]);
    expect(result).toEqual(new Set(["main"]));
  });

  it("parses a comma-separated string", () => {
    const result = parseAllowedBaseBranches("main,develop,release/1.0");
    expect(result).toEqual(new Set(["main", "develop", "release/1.0"]));
  });

  it("trims whitespace from comma-separated string entries", () => {
    const result = parseAllowedBaseBranches("main , develop , release/1.0");
    expect(result).toEqual(new Set(["main", "develop", "release/1.0"]));
  });

  it("filters empty entries from comma-separated string", () => {
    const result = parseAllowedBaseBranches("main,,develop");
    expect(result).toEqual(new Set(["main", "develop"]));
  });
});

// ---------------------------------------------------------------------------
// isBaseBranchAllowed
// ---------------------------------------------------------------------------
describe("isBaseBranchAllowed", () => {
  it("returns true for exact match", () => {
    expect(isBaseBranchAllowed("main", new Set(["main", "develop"]))).toBe(true);
  });

  it("returns false when not in allowed set", () => {
    expect(isBaseBranchAllowed("feature/x", new Set(["main", "develop"]))).toBe(false);
  });

  it("returns true when '*' is in the allowed set (allow all)", () => {
    expect(isBaseBranchAllowed("any-branch", new Set(["*"]))).toBe(true);
  });

  it("returns true when branch matches a glob pattern like 'release/*'", () => {
    expect(isBaseBranchAllowed("release/1.0", new Set(["release/*"]))).toBe(true);
  });

  it("returns false when branch does not match the glob pattern", () => {
    expect(isBaseBranchAllowed("feature/1.0", new Set(["release/*"]))).toBe(false);
  });

  it("matches nested branches against multi-level glob", () => {
    expect(isBaseBranchAllowed("release/v2/hotfix", new Set(["release/**"]))).toBe(true);
  });

  it("returns false for empty allowed set", () => {
    expect(isBaseBranchAllowed("main", new Set())).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// parseStringListConfig
// ---------------------------------------------------------------------------
describe("parseStringListConfig", () => {
  it("returns empty array for falsy input", () => {
    expect(parseStringListConfig(undefined)).toEqual([]);
    expect(parseStringListConfig("")).toEqual([]);
    // @ts-ignore
    expect(parseStringListConfig(null)).toEqual([]);
  });

  it("returns the same array after trimming and filtering", () => {
    expect(parseStringListConfig(["bug", " enhancement ", ""])).toEqual(["bug", "enhancement"]);
  });

  it("splits a comma-separated string", () => {
    expect(parseStringListConfig("bug,enhancement,question")).toEqual(["bug", "enhancement", "question"]);
  });

  it("trims whitespace from comma-separated entries", () => {
    expect(parseStringListConfig(" bug , enhancement , question ")).toEqual(["bug", "enhancement", "question"]);
  });

  it("filters blank entries from comma-separated string", () => {
    expect(parseStringListConfig("bug,,enhancement")).toEqual(["bug", "enhancement"]);
  });

  it("coerces non-string array items to strings", () => {
    // @ts-ignore – deliberate mixed-type test
    expect(parseStringListConfig([1, true, "label"])).toEqual(["1", "true", "label"]);
  });
});

// ---------------------------------------------------------------------------
// mergeFallbackIssueLabels
// ---------------------------------------------------------------------------
describe("mergeFallbackIssueLabels", () => {
  it("always includes MANAGED_FALLBACK_ISSUE_LABEL as the first entry", () => {
    const result = mergeFallbackIssueLabels(["custom-label"]);
    expect(result[0]).toBe(MANAGED_FALLBACK_ISSUE_LABEL);
  });

  it("returns only MANAGED_FALLBACK_ISSUE_LABEL when called with no args", () => {
    expect(mergeFallbackIssueLabels()).toEqual([MANAGED_FALLBACK_ISSUE_LABEL]);
  });

  it("deduplicates when MANAGED_FALLBACK_ISSUE_LABEL is passed explicitly", () => {
    const result = mergeFallbackIssueLabels([MANAGED_FALLBACK_ISSUE_LABEL, "other"]);
    expect(result.filter(l => l === MANAGED_FALLBACK_ISSUE_LABEL)).toHaveLength(1);
  });

  it("includes additional labels after the managed label", () => {
    const result = mergeFallbackIssueLabels(["alpha", "beta"]);
    expect(result).toEqual([MANAGED_FALLBACK_ISSUE_LABEL, "alpha", "beta"]);
  });

  it("filters out empty and whitespace-only labels", () => {
    const result = mergeFallbackIssueLabels(["valid", "", "  "]);
    expect(result).toEqual([MANAGED_FALLBACK_ISSUE_LABEL, "valid"]);
  });

  it("trims whitespace from label values", () => {
    const result = mergeFallbackIssueLabels([" trimmed "]);
    expect(result).toContain("trimmed");
    expect(result).not.toContain(" trimmed ");
  });
});

// ---------------------------------------------------------------------------
// sanitizeFallbackAssignees
// ---------------------------------------------------------------------------
describe("sanitizeFallbackAssignees", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("returns null for empty input", () => {
    expect(sanitizeFallbackAssignees([])).toBeNull();
    // @ts-ignore
    expect(sanitizeFallbackAssignees(null)).toBeNull();
    // @ts-ignore
    expect(sanitizeFallbackAssignees(undefined)).toBeNull();
  });

  it("returns null when all assignees are filtered out", () => {
    expect(sanitizeFallbackAssignees(["copilot", "COPILOT", "  "])).toBeNull();
  });

  it("removes 'copilot' (case-insensitive) from the list", () => {
    const result = sanitizeFallbackAssignees(["alice", "Copilot", "bob"]);
    expect(result).toEqual(["alice", "bob"]);
    expect(result).not.toContain("Copilot");
  });

  it("trims whitespace from assignee names", () => {
    const result = sanitizeFallbackAssignees([" alice ", " bob "]);
    expect(result).toEqual(["alice", "bob"]);
  });

  it("truncates to MAX_ASSIGNEES (5) and calls core.warning", () => {
    const warnSpy = vi.spyOn(global.core, "warning");
    const assignees = ["a1", "a2", "a3", "a4", "a5", "a6"];
    const result = sanitizeFallbackAssignees(assignees);
    expect(result).toHaveLength(5);
    expect(warnSpy).toHaveBeenCalledOnce();
    expect(warnSpy.mock.calls[0][0]).toMatch(/assignees limit exceeded/i);
  });

  it("filters out non-string entries", () => {
    // @ts-ignore
    const result = sanitizeFallbackAssignees([42, "alice", null, "bob"]);
    expect(result).toEqual(["alice", "bob"]);
  });

  it("returns valid assignees unchanged", () => {
    expect(sanitizeFallbackAssignees(["alice", "bob"])).toEqual(["alice", "bob"]);
  });
});

// ---------------------------------------------------------------------------
// neutralizeClosingKeywordsForIssueBody
// ---------------------------------------------------------------------------
describe("neutralizeClosingKeywordsForIssueBody", () => {
  it("escapes 'Closes #N' patterns", () => {
    expect(neutralizeClosingKeywordsForIssueBody("Closes #42")).toBe("Closes \\#42");
  });

  it("escapes all supported keywords case-insensitively", () => {
    const keywords = ["fix", "fixes", "fixed", "close", "closes", "closed", "resolve", "resolves", "resolved"];
    for (const kw of keywords) {
      const result = neutralizeClosingKeywordsForIssueBody(`${kw} #1`);
      expect(result).toBe(`${kw} \\#1`);
    }
  });

  it("escapes cross-repo references like 'owner/repo#N'", () => {
    const result = neutralizeClosingKeywordsForIssueBody("Resolves test-owner/test-repo#58");
    expect(result).toBe("Resolves test-owner/test-repo\\#58");
  });

  it("does not alter text without closing keywords", () => {
    const text = "This PR adds a new feature and updates docs.";
    expect(neutralizeClosingKeywordsForIssueBody(text)).toBe(text);
  });

  it("does not escape #N references that are not preceded by a closing keyword", () => {
    const text = "See issue #42 for context";
    expect(neutralizeClosingKeywordsForIssueBody(text)).toBe(text);
  });

  it("handles multiple closing keywords in the same body", () => {
    const text = "Closes #1\nFixes #2\nResolves owner/repo#3";
    const result = neutralizeClosingKeywordsForIssueBody(text);
    expect(result).toBe("Closes \\#1\nFixes \\#2\nResolves owner/repo\\#3");
  });

  it("returns empty string/falsy values unchanged", () => {
    expect(neutralizeClosingKeywordsForIssueBody("")).toBe("");
    // @ts-ignore
    expect(neutralizeClosingKeywordsForIssueBody(null)).toBe(null);
    // @ts-ignore
    expect(neutralizeClosingKeywordsForIssueBody(undefined)).toBe(undefined);
  });
});

// ---------------------------------------------------------------------------
// generatePatchPreview
// ---------------------------------------------------------------------------
describe("generatePatchPreview", () => {
  it("returns empty string for empty/whitespace-only input", () => {
    expect(generatePatchPreview("")).toBe("");
    expect(generatePatchPreview("   ")).toBe("");
    // @ts-ignore
    expect(generatePatchPreview(null)).toBe("");
  });

  it("wraps content in a <details> block with a diff code fence", () => {
    const result = generatePatchPreview("diff --git a/file b/file\n+changed");
    expect(result).toContain("<details>");
    expect(result).toContain("</details>");
    expect(result).toContain("```diff");
    expect(result).toContain("```");
  });

  it("shows total line count in summary when under both limits", () => {
    const patch = Array.from({ length: 5 }, (_, i) => `line${i}`).join("\n");
    const result = generatePatchPreview(patch);
    expect(result).toContain("Show patch (5 lines)");
    expect(result).not.toContain("truncated");
  });

  it("truncates and indicates truncation when over 500 lines", () => {
    const patch = Array.from({ length: 600 }, (_, i) => (i === 500 ? "omitted-line" : "x")).join("\n");
    const result = generatePatchPreview(patch);
    expect(result).toContain("Show patch preview (500 of 600 lines)");
    expect(result).toContain("... (truncated)");
    // Content from line 500+ must not appear
    expect(result).not.toContain("omitted-line");
  });

  it("truncates and indicates truncation when over 2000 characters", () => {
    // 3 lines totaling well over 2000 chars
    const longLine = "x".repeat(1000);
    const patch = `${longLine}\n${longLine}\n${longLine}`;
    const result = generatePatchPreview(patch);
    expect(result).toContain("Show patch preview (2 of 3 lines)");
    expect(result).toContain("... (truncated)");
  });

  it("includes patch content in the output when within limits", () => {
    const patch = "diff --git a/foo b/foo\n+hello";
    const result = generatePatchPreview(patch);
    expect(result).toContain("+hello");
  });
});

// ---------------------------------------------------------------------------
// buildManifestProtectionCreatePrUrl
// ---------------------------------------------------------------------------
describe("buildManifestProtectionCreatePrUrl", () => {
  const repoParts = { owner: "my-org", repo: "my-repo" };

  it("builds a compare URL with title", () => {
    const url = buildManifestProtectionCreatePrUrl("https://github.com", repoParts, "main", "feature/x", "My PR Title");
    expect(url).toContain("https://github.com/my-org/my-repo/compare/main...feature/x");
    expect(url).toContain("expand=1");
    expect(url).toContain(`title=${encodeURIComponent("My PR Title")}`);
  });

  it("appends a Closes body param when fallbackIssueNumber is provided", () => {
    const url = buildManifestProtectionCreatePrUrl("https://github.com", repoParts, "main", "feature/x", "Title", 42);
    expect(url).toContain(`body=${encodeURIComponent("Closes #42")}`);
  });

  it("does not append a body param when fallbackIssueNumber is not provided", () => {
    const url = buildManifestProtectionCreatePrUrl("https://github.com", repoParts, "main", "feature/x", "Title");
    expect(url).not.toContain("body=");
  });

  it("URL-encodes branch names with special characters via encodePathSegments", () => {
    const url = buildManifestProtectionCreatePrUrl("https://github.com", repoParts, "main", "feature/my branch#1", "Title");
    // encodePathSegments encodes each slash-delimited segment but preserves '/';
    // space → %20, # → %23
    expect(url).toContain("/compare/main...feature/my%20branch%231");
  });

  it("URL-encodes the PR title", () => {
    const url = buildManifestProtectionCreatePrUrl("https://github.com", repoParts, "main", "feat", "Title with spaces & symbols");
    expect(url).toContain(encodeURIComponent("Title with spaces & symbols"));
  });

  it("uses the provided github server URL", () => {
    const url = buildManifestProtectionCreatePrUrl("https://github.example.com", repoParts, "main", "feat", "T");
    expect(url.startsWith("https://github.example.com/")).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// buildPushErrorSection
// ---------------------------------------------------------------------------
describe("buildPushErrorSection", () => {
  const SIGNED_COMMITS_RAW =
    "pushSignedCommits: refusing unsigned push for branch 'my-branch': merge commit detected. " +
    "GitHub's createCommitOnBranch GraphQL mutation cannot represent merge commits, symlinks (mode 120000), " +
    "submodule entries (mode 160000), or executable bits (mode 100755). " +
    "Rewrite the commits to use only regular files (mode 100644) with no merge commits, " +
    "or set signed-commits: false if the repository does not require signed commits.";
  const SIGNED_COMMITS_SANITIZED = SIGNED_COMMITS_RAW.replace(/\s+/g, " ").trim();

  it("returns a generic fallback line for non-signed-commits errors", () => {
    const raw = "Some other push error occurred";
    const sanitized = "Some other push error occurred";
    const result = buildPushErrorSection(raw, sanitized);
    expect(result).toBe(`> **Original error:** ${sanitized}`);
  });

  it("returns structured markdown for a signed-commits refusal", () => {
    const result = buildPushErrorSection(SIGNED_COMMITS_RAW, SIGNED_COMMITS_SANITIZED);
    expect(result).toContain("> **Error:** Signed commit push refused");
    expect(result).not.toContain("Original error:");
  });

  it("extracts the cause from the error message", () => {
    const result = buildPushErrorSection(SIGNED_COMMITS_RAW, SIGNED_COMMITS_SANITIZED);
    expect(result).toContain("merge commit detected");
  });

  it("lists all unsupported commit shapes", () => {
    const result = buildPushErrorSection(SIGNED_COMMITS_RAW, SIGNED_COMMITS_SANITIZED);
    expect(result).toContain("Merge commits");
    expect(result).toContain("Symlinks");
    expect(result).toContain("Submodule entries");
    expect(result).toContain("Executable files");
  });

  it("includes merge-commit-specific remediation (git rebase) and signed-commits: false", () => {
    const result = buildPushErrorSection(SIGNED_COMMITS_RAW, SIGNED_COMMITS_SANITIZED);
    expect(result).toContain("git rebase");
    expect(result).toContain("signed-commits: false");
  });

  it("produces only blockquote lines (each line starts with '>')", () => {
    const result = buildPushErrorSection(SIGNED_COMMITS_RAW, SIGNED_COMMITS_SANITIZED);
    const lines = result.split("\n");
    for (const line of lines) {
      expect(line.startsWith(">")).toBe(true);
    }
  });

  it("falls back to generic error line for PushSignedCommitsPolicyViolation (no cannot-represent boilerplate)", () => {
    const raw = "pushSignedCommits: refusing unsigned push for branch 'my-branch': " + "E003: Signed-commit payload exceeds max-patch-files (10). Synthesized payload touches 15 file(s): a.txt, b.txt.";
    const sanitized = raw.replace(/\s+/g, " ").trim();
    const result = buildPushErrorSection(raw, sanitized);
    expect(result).toBe(`> **Original error:** ${sanitized}`);
  });

  it("uses a generic cause when the cause pattern does not match", () => {
    // Has the boilerplate but lacks the standard 'for branch ...' extraction pattern.
    const malformed = "pushSignedCommits: refusing unsigned push without branch info. " + "GitHub's createCommitOnBranch GraphQL mutation cannot represent merge commits.";
    const result = buildPushErrorSection(malformed, malformed);
    expect(result).toContain("unsupported commit shape");
  });

  it("extracts cause correctly from branch name containing an apostrophe", () => {
    const raw =
      "pushSignedCommits: refusing unsigned push for branch 'it's-great': merge commit detected. " +
      "GitHub's createCommitOnBranch GraphQL mutation cannot represent merge commits, symlinks (mode 120000), " +
      "submodule entries (mode 160000), or executable bits (mode 100755). " +
      "Rewrite the commits to use only regular files (mode 100644) with no merge commits, " +
      "or set signed-commits: false if the repository does not require signed commits.";
    const result = buildPushErrorSection(raw, raw);
    expect(result).toContain("merge commit detected");
    expect(result).not.toContain("unsupported commit shape");
  });

  it("handles submodule change detected cause with submodule-specific remediation", () => {
    const raw =
      "pushSignedCommits: refusing unsigned push for branch 'feat': submodule change detected. " +
      "GitHub's createCommitOnBranch GraphQL mutation cannot represent merge commits, symlinks (mode 120000), " +
      "submodule entries (mode 160000), or executable bits (mode 100755). " +
      "Rewrite the commits to use only regular files (mode 100644) with no merge commits, " +
      "or set signed-commits: false if the repository does not require signed commits.";
    const result = buildPushErrorSection(raw, raw);
    expect(result).toContain("submodule change detected");
    expect(result).toContain("Remove the submodule entry");
    expect(result).not.toContain("git rebase");
  });

  it("handles symlink cause with symlink-specific remediation", () => {
    const raw =
      "pushSignedCommits: refusing unsigned push for branch 'feat': symlink file mode requires git push fallback. " +
      "GitHub's createCommitOnBranch GraphQL mutation cannot represent merge commits, symlinks (mode 120000), " +
      "submodule entries (mode 160000), or executable bits (mode 100755). " +
      "Rewrite the commits to use only regular files (mode 100644) with no merge commits, " +
      "or set signed-commits: false if the repository does not require signed commits.";
    const result = buildPushErrorSection(raw, raw);
    expect(result).toContain("symlink file mode requires git push fallback");
    expect(result).toContain("Remove the symlink");
    expect(result).not.toContain("git rebase");
  });
});
