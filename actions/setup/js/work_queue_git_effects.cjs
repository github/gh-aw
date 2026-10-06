// @ts-check
"use strict";

const { currentClaimHandle, readClaimScopeContext, assertClaimAuthorized } = require("./work_queue_claim_scope.cjs");
const { execGitSync } = require("./git_helpers.cjs");
const { randomUUID } = require("crypto");

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

/** @param {Record<string, any>} options */
async function assertGitPushAuthorized(options) {
  if (!currentClaimHandle() && !readClaimScopeContext()) return;
  if (!currentClaimHandle()) throw new Error("Queue git pushes require a trusted per-Claim context");
  const remote = options.remote || "origin";
  const direct = /^(?:https?|ssh):\/\//.test(remote) || remote.includes(":");
  if (!direct && !/^[A-Za-z0-9_.-]{1,256}$/.test(remote)) throw new Error("Claim git remote alias is invalid");
  const alias = `gh-aw-scope-${randomUUID()}`;
  const args = direct ? ["-c", `remote.${alias}.url=${remote}`, "remote", "get-url", "--push", "--all", "--", alias] : ["remote", "get-url", "--push", "--all", "--", remote];
  const execute = options.execGitSync || execGitSync;
  const resolved = execute(args, { cwd: options.cwd, suppressLogs: true, env: { ...process.env, ...(options.gitAuthEnv || {}) } });
  if (Buffer.byteLength(resolved) > 8192) throw new Error("Claim git remote targets exceed the byte bound");
  const urls = resolved.trim().split("\n").filter(Boolean);
  if (!urls.length || urls.length > 128) throw new Error("Claim git remote targets exceed the count bound");
  for (const url of urls) {
    const repository = gitPushRepository(url);
    await assertClaimAuthorized(
      { type: "work_queue_git_effect", claim_handle: currentClaimHandle(), repo: repository, branch_name: options.branch },
      { authorize: options.authorize, effect: true, resource: { repository, ref: options.branch } }
    );
  }
}

module.exports = { assertGitPushAuthorized, gitPushRepository };
