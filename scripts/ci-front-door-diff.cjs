"use strict";

const { execFileSync } = require("node:child_process");

const LIMITS = Object.freeze({ files: 200, patch: 24000, git: 2 * 1024 * 1024, timeout: 10000 });

function summarizeDiff({ cwd, base = "HEAD^", head = "HEAD", git = execFileSync }) {
  const run = args =>
    git("git", args, {
      cwd,
      encoding: "utf8",
      timeout: LIMITS.timeout,
      maxBuffer: LIMITS.git,
      stdio: ["ignore", "pipe", "pipe"],
      env: { ...process.env, GIT_NO_LAZY_FETCH: "1", GIT_TERMINAL_PROMPT: "0" },
    });
  try {
    // Resolve revisions before passing them to diff so user-supplied flags cannot be options.
    const revisions = [base, head].map(ref => run(["rev-parse", "--verify", "--end-of-options", `${ref}^{commit}`]).trim());
    if (revisions.some(ref => !/^[a-f0-9]{40,64}$/.test(ref))) throw new Error("invalid-revision");
    const names = run(["diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-only", "-z", ...revisions, "--"])
      .split("\0")
      .filter(Boolean);
    const patch = Buffer.from(run(["diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--unified=2", ...revisions, "--"]), "utf8");
    const truncated = names.length > LIMITS.files || patch.length > LIMITS.patch;
    const summary = { files: names.slice(0, LIMITS.files), patch: patch.subarray(0, LIMITS.patch).toString("utf8"), truncated, unavailable: false };
    if (Buffer.byteLength(JSON.stringify(summary)) > 65536) return { files: [], patch: "", truncated: true, unavailable: false };
    return summary;
  } catch {
    return { files: [], patch: "", truncated: false, unavailable: true };
  }
}

module.exports = { summarizeDiff };
