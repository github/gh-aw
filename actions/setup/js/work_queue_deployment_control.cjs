// @ts-check
"use strict";

const { canonical, digest, utf8Compare } = require("./work_queue_codec.cjs");
const { policyProposalFor, loadQueue, publishOperations } = require("./work_queue_binding.cjs");
const { verifiedDefaultReference, configuredWorkerDeployment } = require("./work_queue_provisioning.cjs");
const { obsoleteWork } = require("./work_queue_deployment.cjs");
const { authenticatePublisher } = require("./work_queue_native.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");

async function reconcileObsoleteWork(options) {
  if (isStagedMode(options) || isStagedMode(options.config)) return { cancelled: 0 };
  const latest = await loadQueue({ ...options, policyProposal: undefined });
  if (latest.projection.policy?.authorization !== "aw") return { cancelled: 0 };
  const operations = [...latest.projection.works.values()]
    .filter(work => obsoleteWork(latest.projection, work))
    .sort((a, b) => utf8Compare(a.work_id, b.work_id))
    .slice(0, Math.min(256, latest.projection.policy.limits.operations))
    .map(work => ({ kind: "WorkCancellation", work_id: work.work_id, reason: "worker_contract_obsolete" }));
  if (!operations.length) return { cancelled: 0 };
  const context = await authenticatePublisher({ ...options, role: "reconciler" });
  await publishOperations(options, context, ["obsolete-work", latest.projection.tip, operations], "cancel_work", operations);
  const final = await loadQueue({ ...options, policyProposal: undefined });
  if (operations.some(operation => final.projection.works.get(operation.work_id)?.state !== "cancelled")) throw new Error("work_queue_obsolete_cancellation_not_durable");
  return { cancelled: operations.length };
}

function approvedWorkerProfiles(state, pool, config) {
  const workflows = new Set(config.work_queue_workflows ?? []);
  const contexts = new Set(config.aw_context_workflows ?? []);
  return Object.entries(state.policy.pools[pool]?.profiles ?? {})
    .filter(([, profile]) => {
      const name = profile.workflow.replace(/^\.github\/workflows\//, "").replace(/\.lock\.yml$/, "");
      return workflows.has(name) && contexts.has(name);
    })
    .map(([name]) => name)
    .sort();
}

// Caller-local compiler options may narrow targets, never approve code. Resolve
// activation from the verified default revision, not the invoking run's SHA.
/** @param {string[]} [pools] */
async function synchronizeDeployments(options, context, pools = undefined) {
  if (!["administrator", "producer", "dispatcher"].includes(context.role)) return;
  const proposal = policyProposalFor(options);
  if (proposal?.authorization !== "aw") return;
  let latest = await loadQueue({ ...options, policyProposal: undefined });
  if (!latest.projection.deployments?.size) return;
  const targets = [...latest.projection.deployments]
    .filter(([pool]) => pools === undefined || pools.includes(pool))
    .flatMap(([pool, workers]) =>
      approvedWorkerProfiles(latest.projection, pool, options.config ?? {})
        .filter(name => workers.has(name))
        .map(name => [pool, name])
    );
  if (!targets.length) return;
  const provisioning = { githubClient: options.githubClient, owner: options.context.repo.owner, repo: options.context.repo.repo, sleepFn: options.sleepFn };
  const revision = await verifiedDefaultReference(provisioning);
  const updateRoute = async (pool, name, ref = undefined) => {
    const activate = ref === undefined;
    for (let attempt = 0; attempt < 3; attempt++) {
      const worker = latest.projection.deployments.get(pool).get(name);
      const installed = worker.revisions[ref ?? worker.current_ref].profile;
      const checked = await configuredWorkerDeployment({ ...provisioning, profile: installed }, ref ?? revision);
      const profile = activate ? checked.profile : installed;
      const available = checked.available && (activate || canonical(checked.profile) === canonical(installed));
      const current = worker.revisions[profile.ref];
      if (current && canonical(current.profile) === canonical(profile) && current.available === available && (!activate || worker.current_ref === profile.ref)) return;
      const operation = {
        kind: "Deployment",
        pool,
        worker_profile: name,
        expected_ref: worker.current_ref,
        expected_contract: worker.current_contract,
        profile,
        available,
        ...(!activate ? { activate: false } : {}),
        reason: available ? "compiler_deployment" : "worker_unavailable",
      };
      try {
        await publishOperations(options, context, { deployment: digest(operation), tip: latest.projection.tip }, "deployment", [operation]);
        latest = await loadQueue({ ...options, policyProposal: undefined });
        return;
      } catch (error) {
        if (error?.code !== "deployment_conflict" || attempt === 2) throw error;
        latest = await loadQueue({ ...options, policyProposal: undefined });
      }
    }
  };
  for (const [pool, name] of targets) {
    await updateRoute(pool, name);
    const worker = latest.projection.deployments.get(pool).get(name);
    const pinned = new Set(
      [...latest.projection.works.values()].filter(work => work.state === "available" && work.pool === pool && work.worker_profile === name && work.execution_ref && work.execution_ref !== worker.current_ref).map(work => work.execution_ref)
    );
    for (const ref of pinned) await updateRoute(pool, name, ref);
  }
}

module.exports = { approvedWorkerProfiles, synchronizeDeployments, reconcileObsoleteWork };
