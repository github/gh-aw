// @ts-check
"use strict";

const { validateEffectResource } = require("./work_queue_resource_scope.cjs");

const DECIMALS = new Set(["repository_id", "resource_id", "number", "comment_id", "run_id"]);
const FIELDS = new Set(["repository", "kind", "host", ...DECIMALS, "ref", "path"]);
const RECEIPT_KINDS = new Set(["comment", "release", "check_run", "deployment", "repository", "unknown", "graphql", "git_blob", "git_tree", "git_commit", "git_ref", "queue_commit", "sarif", "code_coverage"]);

function nativeDecimalIdentity(value) {
  const identity = typeof value === "number" && Number.isSafeInteger(value) ? String(value) : value;
  if (typeof identity !== "string" || !/^[1-9][0-9]{0,255}$/.test(identity)) throw new Error("Claim effect target requires a lossless positive native identity");
  return identity;
}

/** Convert independently resolved native targets, not output selectors or receipt metadata. */
function canonicalResourceTarget(resource) {
  if (!resource || typeof resource !== "object" || Array.isArray(resource)) throw new Error("Claim effect target requires an independently resolved resource");
  /** @type {Record<string, string>} */
  const target = {};
  for (const [field, value] of Object.entries(resource)) {
    if (["id", "url"].includes(field)) continue;
    const key = field === "target_run_id" ? "run_id" : field;
    if (!FIELDS.has(key)) throw new Error(`Unsupported Claim effect target field: ${field}`);
    if (key === "kind" && RECEIPT_KINDS.has(value)) continue;
    target[key] = DECIMALS.has(key) ? nativeDecimalIdentity(value) : value;
  }
  if (resource.id !== undefined && ["issue", "pull_request", "release", "check_run", "deployment", "comment"].includes(resource.kind)) {
    const field = resource.kind === "comment" ? "comment_id" : "resource_id";
    const identity = nativeDecimalIdentity(resource.id);
    if (target[field] !== undefined && target[field] !== identity) throw new Error("Claim effect target contains conflicting native identities");
    target[field] = identity;
  }
  validateEffectResource(target);
  return target;
}

async function resolveRepositoryTarget(github, resource) {
  const target = canonicalResourceTarget(resource);
  if ((process.env.GITHUB_API_URL || "https://api.github.com").replace(/\/$/, "") === "https://api.github.com") target.host = "github.com";
  if (typeof github?.rest?.repos?.get === "function") {
    const [owner, repo] = target.repository.split("/");
    const { data } = await github.rest.repos.get({ owner, repo });
    if (!data || data.full_name !== target.repository) throw new Error("Claim effect repository identity mismatch");
    const identity = nativeDecimalIdentity(data.id);
    if (target.repository_id !== undefined && target.repository_id !== identity) throw new Error("Claim effect repository identity changed");
    target.repository_id = identity;
  }
  return target;
}

async function resolveParentResourceTarget(github, resource, data) {
  const number = nativeDecimalIdentity(resource.number);
  const requestNumber = Number(number);
  if (!Number.isSafeInteger(requestNumber)) throw new Error("Claim resource number is invalid");
  if (!data || nativeDecimalIdentity(data.number) !== number) throw new Error("Claim effect parent identity mismatch");
  let kind = resource.kind;
  if (kind === "issue" && data.pull_request) {
    // Issues API IDs differ from the native Pull Request identity.
    const api = (process.env.GITHUB_API_URL || "https://api.github.com").replace(/\/$/, "");
    if (data.pull_request.url !== `${api}/repos/${resource.repository}/pulls/${number}` || typeof github?.rest?.pulls?.get !== "function") throw new Error("Claim Pull Request target cannot be independently resolved");
    const [owner, repo] = resource.repository.split("/");
    ({ data } = await github.rest.pulls.get({ owner, repo, pull_number: requestNumber }));
    if (!data || nativeDecimalIdentity(data.number) !== number) throw new Error("Claim Pull Request parent identity mismatch");
    kind = "pull_request";
  }
  const target = await resolveRepositoryTarget(github, { ...resource, kind, resource_id: nativeDecimalIdentity(data.id), number });
  if (kind === "pull_request" && data.base?.repo) {
    if (data.base.repo.full_name !== target.repository) throw new Error("Claim Pull Request repository identity mismatch");
    const identity = nativeDecimalIdentity(data.base.repo.id);
    if (target.repository_id !== undefined && target.repository_id !== identity) throw new Error("Claim Pull Request repository identity changed");
    target.repository_id = identity;
  }
  return target;
}

module.exports = { canonicalResourceTarget, nativeDecimalIdentity, resolveRepositoryTarget, resolveParentResourceTarget };
