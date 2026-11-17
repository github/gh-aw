// @ts-check

const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

const MAX_GIT_LOG_BYTES = 64 * 1024 * 1024;

/** @param {string} repoRoot @param {string[]} args */
function git(repoRoot, args) {
  const result = spawnSync("git", ["-C", repoRoot, ...args], {
    encoding: "utf8",
    maxBuffer: MAX_GIT_LOG_BYTES,
    windowsHide: true,
  });
  if (result.error || result.status !== 0) {
    const detail = String(result.stderr || result.error?.message || "git command failed").trim();
    throw new Error(detail);
  }
  return result.stdout;
}

/** @param {string} repoRoot */
function shallowBoundaryCommits(repoRoot) {
  const shallowPath = git(repoRoot, ["rev-parse", "--git-path", "shallow"]).trim();
  const resolved = path.isAbsolute(shallowPath) ? shallowPath : path.join(repoRoot, shallowPath);
  if (!fs.existsSync(resolved)) return new Set();
  return new Set(
    fs
      .readFileSync(resolved, "utf8")
      .split(/\r?\n/)
      .map(line => line.trim())
      .filter(Boolean)
  );
}

/** @param {string} output */
function parseHistory(output) {
  return output
    .split("\x1e")
    .filter(Boolean)
    .map(block => {
      const lines = block.replace(/^\r?\n/, "").split(/\r?\n/);
      const [commit, committedAt] = (lines.shift() || "").split("\x1f");
      const changes = lines
        .filter(Boolean)
        .map(line => {
          const fields = line.split("\t");
          const status = fields.shift() || "";
          return { status, paths: fields };
        })
        .filter(change => change.status && change.paths.length);
      return { commit, committed_at: committedAt, changes };
    })
    .filter(record => /^[a-f0-9]{40,64}$/.test(record.commit) && Number.isFinite(Date.parse(record.committed_at)));
}

/** @param {{commit: string, committed_at: string} | null} left @param {{commit: string, committed_at: string} | null} right */
function later(left, right) {
  if (!left) return right;
  if (!right) return left;
  return Date.parse(left.committed_at) >= Date.parse(right.committed_at) ? left : right;
}

/**
 * @param {{
 *   repoRoot: string,
 *   revision?: string,
 *   paths: string[],
 * }} options
 */
function collectGitProvenance({ repoRoot, revision = "HEAD", paths }) {
  const empty = Object.fromEntries(
    paths.map(filename => [
      filename,
      {
        history_status: "missing",
        introduced_commit: null,
        introduced_at: null,
        revised_commit: null,
        revised_at: null,
      },
    ])
  );
  if (typeof revision !== "string" || !revision || revision.length > 200 || revision.startsWith("-")) {
    return { status: "missing", revision: null, shallow: null, error: "invalid_revision", files: empty };
  }

  let resolvedRevision;
  let shallow;
  let boundaries;
  let records;
  try {
    resolvedRevision = git(repoRoot, ["rev-parse", "--verify", "--end-of-options", `${revision}^{commit}`]).trim();
    shallow = git(repoRoot, ["rev-parse", "--is-shallow-repository"]).trim() === "true";
    boundaries = shallow ? shallowBoundaryCommits(repoRoot) : new Set();
    records = parseHistory(git(repoRoot, ["-c", "core.quotePath=false", "log", "--format=%x1e%H%x1f%cI", "--name-status", "--find-renames=50%", resolvedRevision, "--", ".github/workflows"]));
  } catch (error) {
    return {
      status: "missing",
      revision: null,
      shallow: null,
      error: error instanceof Error ? error.message : String(error),
      files: empty,
    };
  }

  /** @type {Map<string, {alias: string, introduced: {commit: string, committed_at: string} | null, revised: {commit: string, committed_at: string} | null, complete: boolean}>} */
  const trackers = new Map(
    paths.map(filename => [
      filename,
      {
        alias: filename,
        introduced: null,
        revised: null,
        complete: false,
      },
    ])
  );
  for (const record of records) {
    for (const change of record.changes) {
      const kind = change.status[0];
      if ((kind === "R" || kind === "C") && change.paths.length === 2) {
        const [oldPath, newPath] = change.paths;
        for (const tracker of trackers.values()) {
          if (tracker.complete || tracker.alias !== newPath) continue;
          if (!tracker.revised) tracker.revised = record;
          if (kind === "R") {
            tracker.alias = oldPath;
          } else if (!boundaries.has(record.commit)) {
            tracker.introduced = record;
            tracker.complete = true;
          }
        }
        continue;
      }
      const changedPath = change.paths[0];
      for (const tracker of trackers.values()) {
        if (tracker.complete || tracker.alias !== changedPath) continue;
        if (!tracker.revised) tracker.revised = record;
        if (kind === "A" && !boundaries.has(record.commit)) {
          tracker.introduced = record;
          tracker.complete = true;
        }
      }
    }
  }

  const files = Object.fromEntries(
    [...trackers.entries()].map(([filename, tracker]) => {
      const historyStatus = shallow ? "shallow" : tracker.introduced && tracker.revised ? "complete" : "missing";
      return [
        filename,
        {
          history_status: historyStatus,
          introduced_commit: tracker.introduced?.commit ?? null,
          introduced_at: tracker.introduced?.committed_at ?? null,
          revised_commit: tracker.revised?.commit ?? null,
          revised_at: tracker.revised?.committed_at ?? null,
        },
      ];
    })
  );
  const statuses = new Set(Object.values(files).map(file => file.history_status));
  return {
    status: statuses.has("missing") ? "missing" : shallow ? "shallow" : "complete",
    revision: resolvedRevision,
    shallow,
    error: null,
    files,
  };
}

/** @param {Record<string, any>} source @param {Record<string, any>} lock */
function pairGitProvenance(source, lock) {
  const introduced =
    source.introduced_at && lock.introduced_at
      ? Date.parse(source.introduced_at) >= Date.parse(lock.introduced_at)
        ? { commit: source.introduced_commit, committed_at: source.introduced_at }
        : { commit: lock.introduced_commit, committed_at: lock.introduced_at }
      : null;
  const revised = later(source.revised_at ? { commit: source.revised_commit, committed_at: source.revised_at } : null, lock.revised_at ? { commit: lock.revised_commit, committed_at: lock.revised_at } : null);
  const statuses = new Set([source.history_status, lock.history_status]);
  return {
    history_status: statuses.has("missing") ? "missing" : statuses.has("shallow") ? "shallow" : "complete",
    introduced_commit: introduced?.commit ?? null,
    introduced_at: introduced?.committed_at ?? null,
    revised_commit: revised?.commit ?? null,
    revised_at: revised?.committed_at ?? null,
  };
}

module.exports = { collectGitProvenance, pairGitProvenance, parseHistory };
