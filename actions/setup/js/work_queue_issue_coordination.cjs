// @ts-check
"use strict";

const { randomUUID } = require("node:crypto");
const { digest, queueError } = require("./work_queue_codec.cjs");

// Locks have no expiring lease: a slow or interrupted write must never overlap a
// successor projector. An unresolved lock is pending, not permission to steal it.
/** @param {import("./work_queue_store.cjs").QueueReadOptions & {head: string, keys: string[], repositoryId?: string, scopes?: Array<{work_id: string, keys: string[]}>}} options */
async function withProjectionLocks({ githubClient, owner, repo, head, keys, repositoryId, scopes }, operation) {
  if (repositoryId) return withGraphQLProjectionLocks({ githubClient, repositoryId, head, keys, scopes }, operation);
  const locks = [...new Set(keys)].sort().map(key => ({ key, branch: `gh-aw-issue-projection/${digest(key)}` }));
  const held = [];
  let uncertain = false;
  let retained;
  try {
    for (const lock of locks) {
      const { branch } = lock;
      try {
        await githubClient.rest.git.createRef({ owner, repo, ref: `refs/heads/${branch}`, sha: head, request: { retries: 0, timeout: 15000 } });
      } catch (error) {
        // Even a lost acquisition response is uncertain. Do not delete a lock
        // that might have been acquired by a competing invocation.
        throw queueError("projection_coordination_pending", `projection lock is held or acquisition is uncertain (${error.status ?? "transport"})`);
      }
      held.push(lock);
    }
    const result = await operation();
    uncertain = result?.ambiguous === true;
    retained = result?.retain_keys;
    return result;
  } catch (error) {
    uncertain = error?.code === "projection_write_ambiguous";
    throw error;
  } finally {
    for (const { key, branch } of held.reverse()) {
      if (uncertain && (!Array.isArray(retained) || retained.includes(key))) continue;
      await githubClient.rest.git.deleteRef({ owner, repo, ref: `heads/${branch}`, request: { retries: 0, timeout: 15000 } });
    }
  }
}

async function withGraphQLProjectionLocks({ githubClient, repositoryId, head, keys, scopes }, operation) {
  const locks = [...new Set(keys)].sort().map(key => ({ key, name: `refs/heads/gh-aw-issue-projection/${digest(key)}` }));
  if (locks.length > 50) throw queueError("projection_limit", "at most 50 coordination refs per phase");
  const variables = {};
  const declarations = [];
  const selections = [];
  for (const [index, lock] of locks.entries()) {
    const alias = `l${index}`;
    variables[alias] = { repositoryId, name: lock.name, oid: head };
    declarations.push(`$${alias}:CreateRefInput!`);
    selections.push(`${alias}: createRef(input:$${alias}) { ref { id name prefix target { oid } } }`);
  }
  let response;
  let errors = [];
  try {
    response = await githubClient.graphql(`mutation WorkQueueProjectionLocks(${declarations.join(",")}) { ${selections.join("\n")} }`, { ...variables, request: { retries: 0, timeout: 30000 } });
  } catch (error) {
    response = error.data;
    errors = error.errors || [];
  }
  const held = locks.flatMap((lock, index) => {
    const value = response?.[`l${index}`]?.ref;
    return value?.id && `${value.prefix}${value.name}` === lock.name && value.target?.oid === head && !errors.some(error => !error.path || error.path[0] === `l${index}`) ? [{ ...lock, id: value.id }] : [];
  });
  const ownedKeys = new Set(held.map(lock => lock.key));
  const accepted = scopes?.filter(scope => scope.keys.every(key => ownedKeys.has(key)));
  const skipped = scopes?.filter(scope => !accepted.includes(scope)).map(scope => ({ work_id: scope.work_id, reason: "queue committed; projection lock is held or acquisition is uncertain" })) || [];
  let retain = [];
  try {
    if (!scopes && held.length !== locks.length) throw queueError("projection_coordination_pending", "projection lock is held or acquisition is uncertain");
    const result = scopes && !accepted.length ? { pending: [] } : await operation(accepted?.map(scope => scope.work_id));
    if (result?.ambiguous) retain = result.retain_keys || keys;
    return { ...result, pending: [...skipped, ...(result?.pending || [])] };
  } catch (error) {
    if (error?.code === "projection_write_ambiguous") retain = keys;
    throw error;
  } finally {
    const release = held.filter(lock => !retain.includes(lock.key));
    if (release.length) {
      const nonce = projectionNonce();
      const variables = Object.fromEntries(release.map((lock, index) => [`u${index}`, { refId: lock.id, clientMutationId: nonce }]));
      const declarations = release.map((_, index) => `$u${index}:DeleteRefInput!`);
      const selections = release.map((_, index) => `u${index}: deleteRef(input:$u${index}) { clientMutationId }`);
      const result = await githubClient.graphql(`mutation WorkQueueProjectionUnlocks(${declarations.join(",")}) { ${selections.join("\n")} }`, { ...variables, request: { retries: 0, timeout: 30000 } });
      if (release.some((_, index) => result?.[`u${index}`]?.clientMutationId !== nonce)) throw queueError("projection_coordination_pending", "coordination release is uncertain");
    }
  }
}

function projectionNonce() {
  return randomUUID();
}

module.exports = { withProjectionLocks, projectionNonce };
