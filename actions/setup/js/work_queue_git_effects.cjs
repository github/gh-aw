// @ts-check
"use strict";

const { currentClaimHandle, readClaimScopeContext, assertClaimAuthorized, withClaimResourceEffects } = require("./work_queue_claim_scope.cjs");
const { execGitSync } = require("./git_helpers.cjs");
const { randomUUID } = require("crypto");
const { resolveRepositoryTarget } = require("./work_queue_effect_resource.cjs");

async function withGitClaimEffects(options, callback) {
  if (!currentClaimHandle() && !readClaimScopeContext()) return callback();
  const message = { type: "work_queue_git_effect", claim_handle: currentClaimHandle(), repo: options.repository, branch_name: options.branch };
  await assertClaimAuthorized(message, { authorize: options.authorize });
  const ref = `refs/heads/${options.branch}`;
  const paths = options.paths?.length ? [...new Set(options.paths)] : [undefined];
  const repository = await resolveRepositoryTarget(options.github || global.github, { repository: options.repository, ref });
  const resources = paths.map(path => ({ ...repository, ...(path === undefined ? {} : { path }) }));
  return withClaimResourceEffects(resources, ["rest.git.createBlob", "rest.git.createTree", "rest.git.createCommit", "rest.git.createRef", "git.push"], callback);
}

function gitPushRepository(url) {
  const expected = new URL(process.env.GITHUB_SERVER_URL || "https://github.com");
  let hostname;
  let pathname;
  if (/^(?:https?|ssh):\/\//.test(url)) {
    const parsed = new URL(url);
    hostname = parsed.hostname;
    pathname = parsed.pathname;
    if (parsed.port !== expected.port) throw new Error("Claim git push uses an unapproved server");
  } else {
    const match = url.match(/^(?:[^@/:]+@)?([^/:]+):(.+)$/);
    if (!match) throw new Error("Claim git push target is not a canonical GitHub remote");
    hostname = match[1];
    pathname = match[2];
  }
  if (hostname.toLowerCase() !== expected.hostname.toLowerCase()) throw new Error("Claim git push uses an unapproved server");
  const repository = pathname.replace(/^\/+/, "").replace(/\.git$/, "");
  if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository)) throw new Error("Claim git push repository is invalid");
  return repository;
}

/** @param {{remote?: string, cwd?: string, branch: string, execGitSync?: typeof execGitSync, gitAuthEnv?: NodeJS.ProcessEnv, github?: object, authorize?: (request: Record<string, unknown>) => unknown}} options */
async function assertGitPushAuthorized(options) {
  if (!currentClaimHandle() && !readClaimScopeContext()) return;
  if (!currentClaimHandle()) throw new Error("Queue git pushes require a trusted per-Claim context");
  const remote = options.remote || "origin";
  const direct = /^(?:https?|ssh):\/\//.test(remote) || remote.includes(":");
  if (!direct && !/^[A-Za-z0-9_.-]{1,256}$/.test(remote)) throw new Error("Claim git remote alias is invalid");
  const alias = `gh-aw-scope-${randomUUID()}`;
  // get-url does not recognize command-scoped remotes on all Git versions.
  // remote -v still expands both insteadOf and pushInsteadOf without persisting config.
  const args = direct ? ["-c", `remote.${alias}.url=${remote}`, "remote", "-v"] : ["remote", "get-url", "--push", "--all", "--", remote];
  const execute = options.execGitSync || execGitSync;
  const resolved = execute(args, { cwd: options.cwd, suppressLogs: true, env: { ...process.env, ...(options.gitAuthEnv || {}) } });
  if (Buffer.byteLength(resolved) > 8192) throw new Error("Claim git remote targets exceed the byte bound");
  const urls = direct
    ? resolved
        .split("\n")
        .filter(line => line.startsWith(`${alias}\t`) && line.endsWith(" (push)"))
        .map(line => line.slice(alias.length + 1, -" (push)".length))
    : resolved.trim().split("\n").filter(Boolean);
  if (!urls.length || urls.length > 128) throw new Error("Claim git remote targets exceed the count bound");
  for (const url of urls) {
    const repository = gitPushRepository(url);
    await withGitClaimEffects({ repository, branch: options.branch, github: options.github, authorize: options.authorize }, () => {});
  }
}

module.exports = { assertGitPushAuthorized, gitPushRepository, withGitClaimEffects };
