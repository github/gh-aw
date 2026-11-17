"use strict";

const { execFileSync } = require("node:child_process");

const LIMITS = Object.freeze({ files: 2000, patch: 24000, groupPatch: 8000, summary: 128 * 1024, git: 2 * 1024 * 1024, compareFiles: 300, compareResponse: 2 * 1024 * 1024, timeout: 10000 });
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

async function summarizeGitHubDiff({ repository, base, head, token, fetchImpl = fetch, onRequest = () => {} }) {
  try {
    if (
      typeof repository !== "string" ||
      !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository) ||
      repository.split("/").some(part => part === "." || part === "..") ||
      !/^[a-f0-9]{40}$/.test(base || "") ||
      !/^[a-f0-9]{40}$/.test(head || "") ||
      typeof token !== "string" ||
      !token.trim()
    )
      throw new Error("invalid-comparison-input");
    onRequest();
    const response = await fetchImpl(`https://api.github.com/repos/${repository}/compare/${base}...${head}?per_page=1`, {
      method: "GET",
      headers: { Accept: "application/vnd.github+json", Authorization: `Bearer ${token}` },
      redirect: "error",
      signal: AbortSignal.timeout(LIMITS.timeout),
    });
    if (response.status !== 200) {
      await response.body?.cancel();
      throw new Error("comparison-unavailable");
    }
    const chunks = [];
    let size = 0;
    for await (const chunk of response.body) {
      size += chunk.length;
      if (size > LIMITS.compareResponse) throw new Error("comparison-too-large");
      chunks.push(chunk);
    }
    const comparison = JSON.parse(Buffer.concat(chunks).toString("utf8"));
    if (comparison.base_commit?.sha !== base || comparison.merge_base_commit?.sha !== base || comparison.status !== "ahead" || !Array.isArray(comparison.files)) throw new Error("invalid-comparison");
    // The compare API never paginates its file list beyond 300 entries.
    if (comparison.files.length >= LIMITS.compareFiles) {
      process.stderr.write("CI Front Door GitHub file inventory may be truncated; running all suites.\n");
      return { files: [], patch: "", truncated: true, unavailable: false };
    }
    const groups = Object.fromEntries(Object.keys(GROUPS).map(id => [id, { files: [], patch: "", patch_truncated: false }]));
    const files = new Set();
    const entries = new Set();
    for (const file of comparison.files) {
      if (!file || typeof file.filename !== "string" || !file.filename || entries.has(file.filename) || !["added", "removed", "modified", "renamed"].includes(file.status)) throw new Error("invalid-comparison-file");
      entries.add(file.filename);
      if (file.status === "renamed" && (typeof file.previous_filename !== "string" || !file.previous_filename)) throw new Error("missing-rename-source");
      // Match git diff --no-renames: a rename retains both the old and new paths.
      const names = [file.filename, ...(file.status === "renamed" ? [file.previous_filename] : [])].filter(name => !name.endsWith(".lock.yml"));
      if (!names.length) continue;
      if (![file.additions, file.deletions, file.changes].every(value => Number.isSafeInteger(value) && value >= 0) || file.changes !== file.additions + file.deletions) throw new Error("invalid-comparison-statistics");
      const patch = file.patch ?? "";
      if (typeof patch !== "string") throw new Error("invalid-comparison-patch");
      if (file.changes > 0 && !/^@@ -\d+(?:,\d+)? \+\d+(?:,\d+)? @@/.test(patch)) throw new Error("missing-comparison-hunk");
      const lines = patch.split("\n");
      if (lines.filter(line => line.startsWith("+")).length !== file.additions || lines.filter(line => line.startsWith("-")).length !== file.deletions) throw new Error("incomplete-comparison-patch");
      const affectedGroups = new Set();
      for (const name of names) {
        files.add(name);
        affectedGroups.add(/\.(?:cjs|js)$/.test(name) ? "javascript" : name.endsWith(".go") ? "go" : "other");
      }
      const evidence = `diff --git ${JSON.stringify(`a/${file.previous_filename || file.filename}`)} ${JSON.stringify(`b/${file.filename}`)}\n${patch}\n`;
      for (const id of affectedGroups) {
        const group = groups[id];
        const bytes = Buffer.from(group.patch + evidence);
        group.patch_truncated ||= bytes.length > LIMITS.groupPatch;
        group.patch = new TextDecoder().decode(bytes.subarray(0, LIMITS.groupPatch), { stream: true });
      }
    }
    const inventory = [...files].sort();
    for (const name of inventory) {
      const id = /\.(?:cjs|js)$/.test(name) ? "javascript" : name.endsWith(".go") ? "go" : "other";
      groups[id].files.push(name);
    }
    const summary = { files: inventory, patch: "", groups, truncated: false, unavailable: false };
    if (inventory.length > LIMITS.files || Buffer.byteLength(JSON.stringify(summary)) > LIMITS.summary) {
      process.stderr.write("CI Front Door GitHub diff summary exceeds its bounds; running all suites.\n");
      return { files: [], patch: "", truncated: true, unavailable: false };
    }
    return summary;
  } catch {
    process.stderr.write("CI Front Door GitHub diff unavailable or incomplete; running all suites.\n");
    return { files: [], patch: "", truncated: false, unavailable: true };
  }
}

module.exports = { summarizeDiff, summarizeGitHubDiff, LIMITS };
