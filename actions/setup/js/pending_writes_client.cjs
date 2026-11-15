// @ts-check
"use strict";

const { pendingWritesEnabled, assertPendingWritesIntegrity, integrityError, currentPendingWrite, assertPendingEffectAllowed, inputDigest } = require("./pending_writes.cjs");
const clients = new WeakMap();
const READ_METHOD = /^(?:get|list|compare|check|download|search|paginate)/;

/**
 * Handler clients retain their ordinary authentication and read behavior.
 * Every mutation is checked at the request boundary, including handler-created
 * PAT clients and error/fallback paths that bypass normal target resolution.
 * @param {any} client
 * @returns {any}
 */
function guardPendingWriteClient(client) {
  if (!pendingWritesEnabled() || !client || typeof client !== "object") return client;
  if (clients.has(client)) return clients.get(client);
  const cache = new WeakMap();
  function guard(object, namespace = "") {
    if (!object || (typeof object !== "object" && typeof object !== "function")) return object;
    if (cache.has(object)) return cache.get(object);
    const proxy = new Proxy(object, {
      get(target, key, receiver) {
        const value = Reflect.get(target, key, receiver);
        if (key === "rest") return guard(value);
        if (namespace === "" && key === "graphql" && typeof value === "function") {
          return (...args) => {
            const query = typeof args[0] === "string" ? args[0] : args[0]?.query;
            if (typeof query !== "string") throw integrityError("unsupported_graphql_write");
            if (/\bmutation\b/.test(query.replace(/#[^\n]*/g, ""))) {
              const item = currentPendingWrite();
              // The signed-commit publisher uses this exact variable-bound
              // mutation. Unknown GraphQL mutation shapes remain unsupported.
              const normalized = query.replace(/\s+/g, " ").trim();
              if (!/^mutation \(\$input: CreateCommitOnBranchInput!\) \{ createCommitOnBranch\(input: \$input\) \{ commit \{ oid \} \} \}$/.test(normalized) || !item) throw integrityError("unsupported_graphql_write");
              const branch = args[1]?.input?.branch;
              const repository = item.target.head_repository || item.target.repository;
              if (branch?.repositoryNameWithOwner !== repository || (item.target.branch && branch?.branchName !== item.target.branch)) throw integrityError("target_changed");
              if (item.publication) item.recheck();
              else assertPendingWritesIntegrity();
              assertPendingEffectAllowed();
            }
            return value.apply(target, args);
          };
        }
        if (namespace === "" && key === "request" && typeof value === "function") {
          return (...args) => {
            const route = typeof args[0] === "string" ? args[0] : "";
            const method = args[0]?.method || route.match(/^([A-Z]+) /)?.[1] || "GET";
            if (!["GET", "HEAD", "OPTIONS"].includes(method)) throw integrityError("unsupported_raw_write");
            return value.apply(target, args);
          };
        }
        if (typeof value === "function" && namespace) {
          const method = value.endpoint?.DEFAULTS?.method;
          const readonly = method ? ["GET", "HEAD", "OPTIONS"].includes(method) : READ_METHOD.test(String(key));
          if (readonly) return value.bind(target);
          return async (...args) => {
            const item = currentPendingWrite();
            if (!item || item.planning) throw integrityError("item_missing");
            if (item.publication) item.recheck();
            else assertPendingWritesIntegrity();
            assertPendingEffectAllowed();
            const request = args[0];
            if (!request || typeof request !== "object") throw integrityError("target_changed");
            if (item.review && (namespace !== "issues" || key !== "create" || request.assignees?.length)) throw integrityError("review_scope_invalid");
            const repository = `${request.owner}/${request.repo}`;
            if (![item.target.repository, item.target.head_repository].includes(repository)) throw integrityError("target_changed");
            if (["dispatch_workflow", "dispatch_repository"].includes(item.type)) {
              const workflow = item.type === "dispatch_workflow";
              if (!item.dispatch || namespace !== (workflow ? "actions" : "repos") || key !== (workflow ? "createWorkflowDispatch" : "createDispatchEvent")) throw integrityError("unsupported_dispatch_write");
              const effect = workflow ? { workflow_id: request.workflow_id, ref: request.ref, inputs: request.inputs } : { event_type: request.event_type, client_payload: request.client_payload };
              if (inputDigest(effect) !== item.dispatch.sha256) throw integrityError("dispatch_changed");
            }
            if (item.code) {
              if (namespace === "issues" && key === "create") throw integrityError("unplanned_fallback");
              if (request.ref && ![`refs/heads/${item.target.branch}`, `heads/${item.target.branch}`].includes(request.ref)) throw integrityError("target_changed");
              if (key === "create" && namespace === "pulls") {
                const expectedHead = item.target.head_repository && item.target.head_repository !== item.target.repository ? `${item.target.head_repository.split("/")[0]}:${item.target.branch}` : item.target.branch;
                if (request.head !== expectedHead || request.base !== item.target.base) throw integrityError("target_changed");
              }
            }
            const number = request.issue_number ?? request.pull_number ?? request.discussion_number;
            if (number !== undefined && number !== (item.target.number ?? item.created_number)) throw integrityError("target_changed");
            if (["create", "createComment"].includes(String(key)) && !item.target.number && namespace === "issues" && item.type !== "create_issue") throw integrityError("unplanned_fallback");
            const result = await value.apply(target, args);
            if (key === "create" && ["issues", "pulls"].includes(namespace) && result?.data?.number) item.created_number = result.data.number;
            return result;
          };
        }
        if (namespace === "" && typeof value === "object" && value) return guard(value, String(key));
        return value;
      },
    });
    cache.set(object, proxy);
    return proxy;
  }
  const guarded = guard(client);
  clients.set(client, guarded);
  clients.set(guarded, guarded);
  return guarded;
}

function installPendingWriteClientGuards() {
  if (!pendingWritesEnabled()) return;
  if (globalThis.github) globalThis.github = guardPendingWriteClient(globalThis.github);
  if (typeof globalThis.getOctokit === "function") {
    const factory = globalThis.getOctokit;
    globalThis.getOctokit = (...args) => guardPendingWriteClient(factory(...args));
  }
}

module.exports = { guardPendingWriteClient, installPendingWriteClientGuards };
