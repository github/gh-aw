// @ts-check
"use strict";

const { createHash } = require("node:crypto");
const { closed, queueError, parseStrictJSON } = require("./work_queue_codec.cjs");
const { readWorkerFile, parsedWorkflow } = require("./work_queue_provisioning.cjs");
const { applyDeployment, executionProfile } = require("./work_queue_deployment.cjs");

const MANIFEST = ".github/aw/work-queue-contract-routes.json";
const READERS = ["actions/setup/js/work_queue_replay.cjs", "actions/setup/js/work_queue_deployment.cjs"];

async function approvedContractBridges(options, revision) {
  let content;
  try {
    content = await readWorkerFile({ ...options, path: MANIFEST, ref: revision });
  } catch (error) {
    if (error?.status === 404) return undefined;
    throw error;
  }
  let approval;
  try {
    approval = parseStrictJSON(content);
  } catch {
    throw queueError("deployment_invalid", "contract bridge approval must be valid JSON");
  }
  closed(approval, ["version", "readers", "routes"], [], "contract bridge approval", "deployment_invalid");
  if (approval.version !== 1) throw queueError("deployment_invalid", "contract bridge approval requires version 1");
  closed(approval.readers, READERS, [], "contract bridge readers", "deployment_invalid");
  for (const path of READERS) if (!/^[a-f0-9]{64}$/.test(approval.readers[path])) throw queueError("deployment_invalid", "contract bridge approval requires reviewed reader SHA256 digests");
  if (!approval.routes || typeof approval.routes !== "object" || Array.isArray(approval.routes) || Object.keys(approval.routes).length > 4096)
    throw queueError("deployment_invalid", "contract bridge approval requires bounded worker routes");
  for (const [workflow, routes] of Object.entries(approval.routes)) {
    if (!/^\.github\/workflows\/[^/]+\.lock\.yml$/.test(workflow) || !routes || typeof routes !== "object" || Array.isArray(routes) || Object.keys(routes).length > 4096)
      throw queueError("deployment_invalid", "contract bridge approval requires exact worker paths and bounded routes");
    for (const [contract, ref] of Object.entries(routes))
      if (!/^[a-f0-9]{64}$/.test(contract) || typeof ref !== "string" || !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(ref) || /^0+$/.test(ref))
        throw queueError("deployment_invalid", "contract bridge approval requires exact contract and immutable revision bindings");
  }
  return approval;
}

function bridgeContractRoutes(state, pool, name, profile, approval) {
  const routes = {};
  for (const work of state.works.values())
    if (work.pool === pool && work.worker_profile === name && !work.execution_ref && ["available", "claimed"].includes(work.state) && work.admission_contract !== profile.logical_contract) {
      const ref = approval.routes[profile.workflow]?.[work.admission_contract];
      if (!ref) throw queueError("deployment_invalid", "unfinished contract requires an approved exact-contract bridge");
      routes[work.admission_contract] = ref;
    }
  return routes;
}

async function verifyBridgeReaders(options, approval, ref, workflow) {
  const lock = await readWorkerFile({ ...options, path: workflow, ref });
  if (!compiledBridgeRuntime(lock, `${options.owner}/${options.repo}`, ref)) throw queueError("deployment_invalid", "selected worker must load reviewed readers through same-repository immutable checkout and local actions/setup");
  for (const path of READERS) {
    const content = await readWorkerFile({ ...options, path, ref });
    if (createHash("sha256").update(content).digest("hex") !== approval.readers[path]) throw queueError("deployment_invalid", "selected execution revision lacks the approved queue reader ABI; publish a reviewed exact-contract bridge");
  }
}

function compiledBridgeRuntime(contents, repository, ref) {
  const workflow = parsedWorkflow(contents);
  const jobs = workflow instanceof Map ? workflow.get("jobs") : undefined;
  if (!(jobs instanceof Map)) return false;
  let found = false;
  for (const job of jobs.values()) {
    const steps = job instanceof Map ? job.get("steps") : undefined;
    if (!Array.isArray(steps)) continue;
    let checkout = false;
    for (const step of steps) {
      if (!(step instanceof Map)) return false;
      const uses = step.get("uses") ?? "";
      const withOptions = step.get("with");
      if (typeof uses !== "string") return false;
      if (uses.startsWith("actions/checkout@")) {
        if (step.has("with") && !(withOptions instanceof Map)) return false;
        const from = withOptions instanceof Map ? withOptions.get("repository") : undefined;
        const revision = withOptions instanceof Map ? withOptions.get("ref") : undefined;
        const path = withOptions instanceof Map ? withOptions.get("path") : undefined;
        checkout =
          /^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(uses.slice("actions/checkout@".length)) &&
          !/^0+$/.test(uses.slice("actions/checkout@".length)) &&
          (from === undefined || (typeof from === "string" && from.toLowerCase() === repository.toLowerCase())) &&
          (revision === undefined || revision === ref || revision === "${{ github.sha }}") &&
          (path === undefined || path === ".");
      } else if (uses === "./actions/setup") {
        if (!checkout) return false;
        found = true;
        checkout = false;
      } else {
        if (uses.toLowerCase().includes("/setup@")) return false;
        if (uses || step.has("run")) checkout = false;
      }
    }
  }
  return found;
}

async function verifyProspectiveBridgeReaders(options, state, approval, operations) {
  const prospective = { ...state, deployments: structuredClone(state.deployments) };
  for (const operation of operations) applyDeployment(prospective, operation);
  const profiles = new Map();
  for (const workers of prospective.deployments.values())
    for (const worker of workers.values())
      if (worker.revisions[worker.current_ref].available) {
        const profile = worker.revisions[worker.current_ref].profile;
        profiles.set(`${profile.workflow}\n${profile.ref}`, profile);
      }
  for (const work of prospective.works.values()) {
    if (work.state !== "available") continue;
    const selected = executionProfile(prospective, work);
    if (selected.reason === "ready") profiles.set(`${selected.profile.workflow}\n${selected.profile.ref}`, selected.profile);
  }
  for (const dispatch of prospective.dispatches.values()) if (!dispatch.released) profiles.set(`${dispatch.profile.workflow}\n${dispatch.profile.ref}`, dispatch.profile);
  for (const key of [...profiles.keys()].sort()) {
    const profile = profiles.get(key);
    await verifyBridgeReaders(options, approval, profile.ref, profile.workflow);
  }
}

module.exports = { approvedContractBridges, bridgeContractRoutes, verifyBridgeReaders, verifyProspectiveBridgeReaders, compiledBridgeRuntime };
