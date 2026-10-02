// @ts-check
"use strict";

const { DispatchWorkCoordinator } = require("./dispatch_work_coordinator_branch.cjs");
const { createDispatchWorkCoordinatorGitHubClient } = require("./dispatch_work_coordinator_github_client.cjs");

function parseMaintenanceConfig(raw) {
  if (typeof raw !== "string" || Buffer.byteLength(raw, "utf8") > 20 * 1024) {
    throw new TypeError("Dispatch Work Coordinator maintenance configuration is incomplete");
  }
  let config;
  try {
    config = JSON.parse(raw);
  } catch (error) {
    throw new TypeError("Dispatch Work Coordinator maintenance configuration is invalid", { cause: error });
  }
  const workflowIdentity =
    typeof config?.identity === "string" &&
    config.identity.startsWith(".github/workflows/") &&
    config.identity.endsWith(".lock.yml") &&
    !config.identity.slice(".github/workflows/".length).includes("/") &&
    !config.identity.slice(".github/workflows/".length).includes("\\") &&
    !/[\0-\x1f\x7f]/.test(config.identity);
  const namedIdentity = typeof config?.identity === "string" && /^dispatch-work\/[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(config.identity);
  if (
    !config ||
    typeof config !== "object" ||
    Array.isArray(config) ||
    (!workflowIdentity && !namedIdentity) ||
    !config.schema ||
    typeof config.schema !== "object" ||
    Array.isArray(config.schema) ||
    config.schema.type !== "object" ||
    Object.keys(config).some(key => key !== "identity" && key !== "schema")
  ) {
    throw new TypeError("Dispatch Work Coordinator maintenance configuration is invalid");
  }
  return config;
}

async function main(env = process.env) {
  const config = parseMaintenanceConfig(env.GH_AW_DISPATCH_WORK_COORDINATOR_CONFIG);
  const repository = env.GITHUB_REPOSITORY || "";
  const [owner, repo] = repository.split("/");
  if (!owner || !repo || repository.split("/").length !== 2 || !env.GH_AW_DISPATCH_WORK_COORDINATOR_TOKEN || !env.GITHUB_RUN_ID || !env.GITHUB_WORKFLOW_REF) {
    throw new TypeError("Dispatch Work Coordinator maintenance environment is incomplete");
  }

  const githubClient = createDispatchWorkCoordinatorGitHubClient(env.GH_AW_DISPATCH_WORK_COORDINATOR_TOKEN, env.GITHUB_API_URL);
  const coordinator = new DispatchWorkCoordinator({
    githubClient,
    owner,
    repo,
    identity: `${repository}/${config.identity}`,
    runId: env.GITHUB_RUN_ID,
    workflowId: env.GITHUB_WORKFLOW_REF,
    workSchema: config.schema,
  });
  const recoveredClaims = await coordinator.recoverOrphanClaims(runId => githubClient.rest.actions.getWorkflowRun({ owner, repo, run_id: runId }));
  const projection = await coordinator.compact();
  return { recovered_claims: recoveredClaims, projection };
}

if (require.main === module) {
  main().catch(() => {
    console.error("Dispatch Work Coordinator maintenance failed.");
    process.exitCode = 1;
  });
}

module.exports = { main, parseMaintenanceConfig };
