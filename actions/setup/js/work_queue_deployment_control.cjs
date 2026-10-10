// @ts-check
"use strict";

const { canonical, digest } = require("./work_queue_codec.cjs");
const { policyProposalFor, loadQueue, publishOperations } = require("./work_queue_binding.cjs");
const { verifyWorkerRoute } = require("./work_queue_provisioning.cjs");

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

// The proposal and target allowlists come exclusively from protected compiler
// options. Intent parameters cannot register workers or expand their scope.
async function synchronizeDeployments(options, context) {
  if (!["administrator", "producer", "dispatcher"].includes(context.role)) return;
  const proposal = policyProposalFor(options);
  if (proposal?.authorization !== "aw") return;
  let latest = await loadQueue({ ...options, policyProposal: undefined });
  if (!latest.projection.deployments?.size) return;
  const updateRoute = async (pool, name, proposed, activate = true) => {
    for (let attempt = 0; attempt < 3; attempt++) {
      const worker = latest.projection.deployments.get(pool).get(name);
      const profile = proposed ?? worker.revisions[worker.current_ref].profile;
      if (!profile.logical_contract) return;
      let available = true;
      if (available) {
        try {
          await verifyWorkerRoute({ githubClient: options.githubClient, owner: options.context.repo.owner, repo: options.context.repo.repo, profile });
        } catch (error) {
          if (error?.code !== "policy_missing") throw error;
          available = false;
        }
      }
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
  for (const [pool, workers] of latest.projection.deployments) {
    const approved = new Set(approvedWorkerProfiles(latest.projection, pool, options.config ?? {}));
    for (const [name] of workers) {
      if (!approved.has(name)) continue;
      await updateRoute(pool, name, undefined);
      const worker = latest.projection.deployments.get(pool).get(name);
      const pinned = new Set(
        [...latest.projection.works.values()]
          .filter(work => work.state === "available" && work.pool === pool && work.worker_profile === name && work.execution_ref && work.execution_ref !== worker.current_ref)
          .map(work => work.execution_ref)
      );
      for (const ref of pinned) await updateRoute(pool, name, worker.revisions[ref].profile, false);
    }
  }
}

module.exports = { approvedWorkerProfiles, synchronizeDeployments };
