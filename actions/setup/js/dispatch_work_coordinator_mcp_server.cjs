// @ts-check
"use strict";

const { createServer, registerTool, start } = require("./mcp_server_core.cjs");
const { DispatchWorkCoordinator, coordinatorIdentity } = require("./dispatch_work_coordinator_branch.cjs");
const { createDispatchWorkCoordinatorGitHubClient } = require("./dispatch_work_coordinator_github_client.cjs");

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

function createDispatchWorkCoordinatorServer({ coordinator, workSchema }) {
  if (!coordinator) throw new TypeError("Dispatch Work Coordinator is required");
  if (!workSchema || typeof workSchema !== "object" || Array.isArray(workSchema)) throw new TypeError("Dispatch Work Coordinator requires a Work schema");
  const server = createServer({ name: "dispatch-work-coordinator", version: "1.0.0" });

  registerTool(server, {
    name: "dispatch_work_submit",
    description: "Submit Work to the durable Dispatch Work Coordinator queue. Identical Work is idempotent.",
    inputSchema: {
      type: "object",
      properties: { work: workSchema },
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
  const schemaJSON = process.env.GH_AW_DISPATCH_WORK_COORDINATOR_SCHEMA;
  const repository = process.env.GITHUB_REPOSITORY;
  if (!token || typeof schemaJSON !== "string" || Buffer.byteLength(schemaJSON, "utf8") > 16 * 1024 || typeof repository !== "string" || !/^[^/]+\/[^/]+$/.test(repository)) {
    throw new TypeError("Dispatch Work Coordinator runtime configuration is incomplete");
  }
  let workSchema;
  try {
    workSchema = JSON.parse(schemaJSON);
  } catch (error) {
    throw new TypeError("Dispatch Work Coordinator Work schema is invalid", { cause: error });
  }
  if (!workSchema || typeof workSchema !== "object" || Array.isArray(workSchema) || workSchema.type !== "object") {
    throw new TypeError("Dispatch Work Coordinator Work schema is invalid");
  }
  const [owner, repo] = repository.split("/");
  const workflowRef = process.env.GITHUB_WORKFLOW_REF;
  const coordinator = new DispatchWorkCoordinator({
    githubClient: createDispatchWorkCoordinatorGitHubClient(token, process.env.GITHUB_API_URL),
    owner,
    repo,
    identity: coordinatorIdentity({ owner, repo, workflowRef, coordinatorId: process.env.GH_AW_DISPATCH_WORK_COORDINATOR_ID }),
    runId: process.env.GITHUB_RUN_ID,
    workflowId: workflowRef,
    workSchema,
  });
  return createDispatchWorkCoordinatorServer({ coordinator, workSchema });
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
