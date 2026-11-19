// @ts-check
"use strict";

/**
 * Read-only target observation shared by resolved-effect adapters. Identities
 * are read independently from GitHub; request locators alone are not proof.
 */

/**
 * @param {any} githubClient
 * @param {{owner: string, repo: string}} repoParts
 */
async function observeRepository(githubClient, repoParts) {
  const { data } = await githubClient.rest.repos.get({ owner: repoParts.owner, repo: repoParts.repo });
  if (!Number.isSafeInteger(data?.id) || String(data?.full_name || "").toLowerCase() !== `${repoParts.owner}/${repoParts.repo}`.toLowerCase()) {
    throw new Error("Effect target repository identity could not be independently resolved");
  }
  return { repository_id: String(data.id), full_name: data.full_name, default_branch: typeof data.default_branch === "string" ? data.default_branch : null };
}

/**
 * Observe an issue or pull request through the Issues API.
 * @param {any} githubClient
 * @param {{owner: string, repo: string}} repoParts
 * @param {number} itemNumber
 */
async function observeIssueTarget(githubClient, repoParts, itemNumber) {
  const repository = await observeRepository(githubClient, repoParts);
  const { data: item } = await githubClient.rest.issues.get({ owner: repoParts.owner, repo: repoParts.repo, issue_number: itemNumber });
  if (item?.number !== itemNumber || !Number.isSafeInteger(item?.id)) throw new Error("Effect target identity could not be independently resolved");
  /** @type {Map<string, number>} */
  const labels = new Map();
  for (const label of item.labels || []) {
    const name = typeof label === "string" ? label : label?.name;
    if (typeof name !== "string" || !Number.isSafeInteger(label?.id)) throw new Error("Label identity could not be independently resolved");
    labels.set(name.toLowerCase(), label.id);
  }
  return {
    identity: {
      repository_id: repository.repository_id,
      resource_id: String(item.id),
      kind: item.pull_request ? "pull_request" : "issue",
      node_id: typeof item.node_id === "string" ? item.node_id : undefined,
    },
    names: (item.labels || []).map(label => (typeof label === "string" ? label : label.name)),
    title: typeof item.title === "string" ? item.title : "",
    body: typeof item.body === "string" ? item.body : null,
    state: item.state,
    labels,
    item,
  };
}

/**
 * Stable comparable projection of an observed target for drift checks.
 * @param {Awaited<ReturnType<typeof observeIssueTarget>>} observed
 */
function issueSnapshot(observed) {
  return {
    identity: { ...observed.identity, node_id: observed.identity.node_id ?? null },
    labels: [...observed.labels.entries()].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)),
    title: observed.title,
    body_sha256: require("node:crypto")
      .createHash("sha256")
      .update(observed.body ?? "\u0000null")
      .digest("hex"),
    state: observed.state ?? null,
  };
}

/** @param {{identity: any}} observed */
function issueTargets(observed, repository, number) {
  const { identity } = observed;
  return [
    { kind: "repository", repository, repository_id: identity.repository_id },
    { kind: identity.kind, repository, repository_id: identity.repository_id, number, resource_id: identity.resource_id, ...(identity.node_id ? { node_id: identity.node_id } : {}) },
  ];
}

/** @param {string} owner @param {string} repo */
function repositoryPath(owner, repo) {
  return `/repos/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}`;
}

module.exports = { observeRepository, observeIssueTarget, issueSnapshot, issueTargets, repositoryPath };
