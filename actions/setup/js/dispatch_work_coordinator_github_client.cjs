// @ts-check
"use strict";

function createDispatchWorkCoordinatorGitHubClient(token, apiUrl = "https://api.github.com") {
  if (typeof token !== "string" || !token) throw new TypeError("Coordinator GitHub token is required");
  /** @param {string} method @param {string} path @param {{query?: Record<string, string | number | undefined>, body?: Record<string, unknown>}} [options] */
  const request = async (method, path, { query = {}, body: payload } = {}) => {
    let url;
    try {
      url = new URL(path, `${apiUrl.replace(/\/$/, "")}/`);
    } catch (error) {
      throw new TypeError("Invalid coordinator GitHub API URL", { cause: error });
    }
    const search = new URLSearchParams();
    for (const [key, value] of Object.entries(query)) {
      if (value !== undefined) search.set(key, String(value));
    }
    url.search = search.toString();
    const body = payload === undefined ? undefined : JSON.stringify(payload);
    let response;
    try {
      response = await fetch(url, {
        method,
        headers: {
          Accept: "application/vnd.github+json",
          Authorization: ["Bearer", token].join(" "),
          "X-GitHub-Api-Version": "2022-11-28",
          ...(body ? { "Content-Type": "application/json" } : {}),
        },
        body,
        signal: AbortSignal.timeout(30_000),
      });
    } catch (error) {
      throw new Error("Coordinator GitHub API request failed", { cause: error });
    }
    if (!response.ok) {
      const error = Object.assign(new Error(`GitHub API request failed with status ${response.status}`), { status: response.status });
      throw error;
    }
    if (response.status === 204) return { data: undefined };
    try {
      return { data: await response.json() };
    } catch (error) {
      throw new Error("Coordinator GitHub API response was invalid", { cause: error });
    }
  };
  const repositoryPath = ({ owner, repo }, suffix = "") => `/repos/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}${suffix}`;
  return {
    rest: {
      git: {
        getRef: params => request("GET", repositoryPath(params, `/git/ref/${encodeURIComponent(params.ref)}`)),
        getCommit: params => request("GET", repositoryPath(params, `/git/commits/${params.commit_sha}`)),
        getTree: params => request("GET", repositoryPath(params, `/git/trees/${params.tree_sha}`), { query: { recursive: params.recursive } }),
        getBlob: params => request("GET", repositoryPath(params, `/git/blobs/${params.file_sha}`)),
        createBlob: params => request("POST", repositoryPath(params, "/git/blobs"), { body: { content: params.content, encoding: params.encoding } }),
        createTree: params => request("POST", repositoryPath(params, "/git/trees"), { body: { tree: params.tree } }),
        createCommit: params => request("POST", repositoryPath(params, "/git/commits"), { body: { message: params.message, tree: params.tree, parents: params.parents } }),
        updateRef: params => request("PATCH", repositoryPath(params, `/git/refs/${encodeURIComponent(params.ref)}`), { body: { sha: params.sha, force: params.force } }),
        createRef: params => request("POST", repositoryPath(params, "/git/refs"), { body: { ref: params.ref, sha: params.sha } }),
      },
      repos: {},
      actions: {
        getWorkflowRun: params => request("GET", repositoryPath(params, `/actions/runs/${encodeURIComponent(params.run_id)}`)),
      },
    },
  };
}

module.exports = { createDispatchWorkCoordinatorGitHubClient };
