"use strict";

const { execFileSync } = require("node:child_process");

const LIMITS = Object.freeze({ files: 2000, patch: 24000, groupPatch: 8000, summary: 128 * 1024, git: 2 * 1024 * 1024, timeout: 10000 });
const EXCLUDE_LOCKS = ":(exclude,glob)**/*.lock.yml";
const GROUPS = Object.freeze({
  javascript: [":(glob)**/*.js", ":(glob)**/*.cjs"],
  go: [":(glob)**/*.go"],
  other: [":(exclude,glob)**/*.js", ":(exclude,glob)**/*.cjs", ":(exclude,glob)**/*.go"],
});

function summarizeDiff({ cwd, base = "HEAD^", head = "HEAD", git = execFileSync }) {
  const run = (args, maxBuffer = LIMITS.git) =>
    git("git", args, {
      cwd,
      encoding: "utf8",
      timeout: LIMITS.timeout,
      maxBuffer,
      stdio: ["ignore", "pipe", "pipe"],
      env: { ...process.env, GIT_NO_LAZY_FETCH: "1", GIT_TERMINAL_PROMPT: "0" },
    });
  try {
    // Resolve revisions before passing them to diff so user-supplied flags cannot be options.
    const revisions = [base, head].map(ref => run(["rev-parse", "--verify", "--end-of-options", `${ref}^{commit}`]).trim());
    if (revisions.some(ref => !/^[a-f0-9]{40,64}$/.test(ref))) throw new Error("invalid-revision");
    const diffArgs = ["diff", "--no-ext-diff", "--no-textconv", "--no-renames"];
    const names = run([...diffArgs, "--name-only", "-z", ...revisions, "--", EXCLUDE_LOCKS])
      .split("\0")
      .filter(Boolean);
    const files = names.slice(0, LIMITS.files);
    const groups = Object.fromEntries(Object.keys(GROUPS).map(id => [id, { files: [], patch: "", patch_truncated: false }]));
    for (const file of files) {
      const id = /\.(?:cjs|js)$/.test(file) ? "javascript" : file.endsWith(".go") ? "go" : "other";
      groups[id].files.push(file);
    }
    for (const [id, patterns] of Object.entries(GROUPS)) {
      if (!groups[id].files.length) continue;
      let output;
      try {
        output = run([...diffArgs, "--unified=2", ...revisions, "--", ...patterns, EXCLUDE_LOCKS], LIMITS.groupPatch);
      } catch (error) {
        if (!["ENOBUFS", "ERR_CHILD_PROCESS_STDIO_MAXBUFFER"].includes(error.code) || !error.stdout || Buffer.byteLength(error.stdout) <= LIMITS.groupPatch) throw error;
        output = error.stdout;
        groups[id].patch_truncated = true;
      }
      const bytes = Buffer.from(output);
      groups[id].patch_truncated ||= bytes.length > LIMITS.groupPatch;
      groups[id].patch = new TextDecoder().decode(bytes.subarray(0, LIMITS.groupPatch), { stream: true });
    }
    // Retain the flat replay fields without duplicating grouped patch content.
    const summary = { files, patch: "", groups, truncated: names.length > LIMITS.files, unavailable: false };
    if (Buffer.byteLength(JSON.stringify(summary)) > LIMITS.summary) return { files: [], patch: "", truncated: true, unavailable: false };
    return summary;
  } catch {
    return { files: [], patch: "", truncated: false, unavailable: true };
  }
}

module.exports = { summarizeDiff, LIMITS };
