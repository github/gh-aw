// @ts-check
"use strict";

const { createServer, registerTool, start } = require("./mcp_server_core.cjs");
const { DispatchWorkCoordinator } = require("./dispatch_work_coordinator_branch.cjs");

function result(value) {
  return { content: [{ type: "text", text: JSON.stringify(value ?? null) }] };
}

function toolHandler(operation) {
  return async args => {
    try {
      return result(await operation(args));
    } catch (error) {
      const message = error instanceof TypeError || error instanceof RangeError ? "Invalid coordinator arguments or transaction-size limit exceeded." : "Coordinator persistence failed; refresh coordinator status before retrying.";
      return {
        isError: true,
        content: [{ type: "text", text: message }],
      };
    }
  };
}

function createDispatchWorkCoordinatorServer({ coordinator }) {
  if (!coordinator) throw new TypeError("Dispatch Work Coordinator is required");
  const server = createServer({ name: "dispatch-work-coordinator", version: "1.0.0" });

  registerTool(server, {
    name: "dispatch_work_submit",
    description: "Submit Work to the durable Dispatch Work Coordinator queue. Identical Work is idempotent.",
    inputSchema: {
      type: "object",
      properties: { work: { type: "object", description: "JSON object describing the Work item." } },
      required: ["work"],
      additionalProperties: false,
    },
    handler: toolHandler(({ work }) => coordinator.submit(work)),
  });

  registerTool(server, {
    name: "dispatch_work_get",
    description: "Read a Work item from the latest replayed coordinator state.",
    inputSchema: {
      type: "object",
      properties: { work_id: { type: "string", minLength: 1, maxLength: 128 } },
      required: ["work_id"],
      additionalProperties: false,
    },
    handler: toolHandler(({ work_id }) => coordinator.get(work_id)),
  });

  registerTool(server, {
    name: "dispatch_work_list",
    description: "List Work items from the latest replayed coordinator state.",
    inputSchema: {
      type: "object",
      properties: {
        filter: {
          type: "object",
          properties: { state: { type: "string", enum: ["available", "claimed", "completed", "cancelled"] } },
          additionalProperties: false,
        },
      },
      additionalProperties: false,
    },
    handler: toolHandler(({ filter }) => coordinator.list(filter)),
  });

  registerTool(server, {
    name: "dispatch_work_status",
    description: "Return counts and projected Work states from the latest coordinator log.",
    inputSchema: { type: "object", properties: {}, additionalProperties: false },
    handler: toolHandler(() => coordinator.status()),
  });

  registerTool(server, {
    name: "dispatch_work_claim",
    description: "Create a Claim for a non-terminal Work item using trusted workflow-run provenance.",
    inputSchema: {
      type: "object",
      properties: { work_id: { type: "string", minLength: 1, maxLength: 128 } },
      required: ["work_id"],
      additionalProperties: false,
    },
    handler: toolHandler(({ work_id }) => coordinator.claim(work_id)),
  });

  registerTool(server, {
    name: "dispatch_work_claim_next",
    description: "Claim the first available Work item in deterministic Work ID order.",
    inputSchema: {
      type: "object",
      properties: {
        filter: {
          type: "object",
          properties: { state: { type: "string", enum: ["available"] } },
          additionalProperties: false,
        },
      },
      additionalProperties: false,
    },
    handler: toolHandler(({ filter }) => coordinator.claimNext(filter)),
  });

  registerTool(server, {
    name: "dispatch_work_cancel",
    description: "Cancel a non-completed Work item in the durable coordinator.",
    inputSchema: {
      type: "object",
      properties: { work_id: { type: "string", minLength: 1, maxLength: 128 } },
      required: ["work_id"],
      additionalProperties: false,
    },
    handler: toolHandler(({ work_id }) => coordinator.cancel(work_id)),
  });

  return server;
}

function createServerFromEnvironment() {
  const token = process.env.GH_AW_DISPATCH_WORK_COORDINATOR_TOKEN;
  const repository = process.env.GITHUB_REPOSITORY;
  if (!token || typeof repository !== "string" || !/^[^/]+\/[^/]+$/.test(repository)) {
    throw new TypeError("Dispatch Work Coordinator runtime configuration is incomplete");
  }
  const [owner, repo] = repository.split("/");
  const workflowRef = process.env.GITHUB_WORKFLOW_REF;
  const apiUrl = process.env.GITHUB_API_URL || "https://api.github.com";
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
      const error = new Error(`GitHub API request failed with status ${response.status}`);
      error.status = response.status;
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
  const githubClient = {
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
    },
  };
  const coordinator = new DispatchWorkCoordinator({
    githubClient,
    owner,
    repo,
    identity: workflowRef?.split("@", 1)[0],
    runId: process.env.GITHUB_RUN_ID,
    workflowId: workflowRef,
  });
  return createDispatchWorkCoordinatorServer({ coordinator });
}

if (require.main === module) {
  try {
    start(createServerFromEnvironment());
  } catch {
    console.error("Dispatch Work Coordinator MCP server could not start. Check its trusted workflow configuration.");
    process.exitCode = 1;
  }
}

module.exports = { createDispatchWorkCoordinatorServer, createServerFromEnvironment };
