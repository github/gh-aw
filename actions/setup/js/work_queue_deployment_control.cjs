// @ts-check
"use strict";

const { canonical, digest, queueError } = require("./work_queue_codec.cjs");
const { policyProposalFor, loadQueue, publishOperations } = require("./work_queue_binding.cjs");
const { serializeProjection } = require("./work_queue_replay.cjs");
const { verifiedDefaultReference, configuredWorkerDeployment } = require("./work_queue_provisioning.cjs");
const { approvedContractBridges, bridgeContractRoutes, verifyBridgeReaders, verifyProspectiveBridgeReaders, requiresContractBridgeApproval } = require("./work_queue_contract_bridges.cjs");

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
  const approval = await approvedContractBridges(provisioning, revision);
  for (let attempt = 0; attempt < 3; attempt++) {
    if (!approval && requiresContractBridgeApproval(latest.projection)) throw queueError("deployment_invalid", "deployment history requires protected bridge approval; do not remove the migration manifest");
    const operations = [];
    const requirements = [];
    for (const [pool, name] of targets) {
      const worker = latest.projection.deployments.get(pool).get(name);
      const revisions = structuredClone(worker.revisions);
      const installed = worker.revisions[worker.current_ref].profile;
      const checked = await configuredWorkerDeployment({ ...provisioning, profile: installed }, revision);
      const profile = checked.profile;
      const available = checked.available;
      const routes = approval ? bridgeContractRoutes(latest.projection, pool, name, profile, approval) : {};
      for (const [contract, ref] of Object.entries(routes)) {
        const bridge = { ...profile, ref, logical_contract: contract };
        const checkedBridge = await configuredWorkerDeployment({ ...provisioning, profile: bridge }, ref);
        if ((!checkedBridge.available && !revisions[ref]) || canonical(checkedBridge.profile) !== canonical(bridge)) throw queueError("deployment_invalid", "approved contract bridge requires exact active compiled authority");
        await verifyBridgeReaders(provisioning, approval, ref, bridge.workflow);
        if (!revisions[ref]) {
          operations.push({
            kind: "Deployment",
            pool,
            worker_profile: name,
            expected_ref: worker.current_ref,
            expected_contract: worker.current_contract,
            profile: bridge,
            available: true,
            activate: false,
            register: true,
            reason: "contract_bridge",
          });
          revisions[ref] = { profile: bridge, available: true };
        } else if (canonical(revisions[ref].profile) !== canonical(bridge)) throw queueError("deployment_invalid", "immutable bridge revision cannot change authority");
      }
      const current = worker.revisions[profile.ref];
      if (!approval && Object.keys(worker.contract_routes ?? {}).length) throw queueError("deployment_invalid", "installed contract routes require protected bridge approval");
      if (!current || canonical(current.profile) !== canonical(profile) || current.available !== available || worker.current_ref !== profile.ref || canonical(worker.contract_routes ?? {}) !== canonical(routes)) {
        operations.push({
          kind: "Deployment",
          pool,
          worker_profile: name,
          expected_ref: worker.current_ref,
          expected_contract: worker.current_contract,
          profile,
          available,
          ...(approval ? { contract_routes: routes } : {}),
          reason: available ? "compiler_deployment" : "worker_unavailable",
        });
      }
      const retained = new Set(Object.values(routes));
      for (const work of latest.projection.works.values()) if (work.state === "available" && work.pool === pool && work.worker_profile === name && work.execution_ref && work.execution_ref !== profile.ref) retained.add(work.execution_ref);
      for (const ref of [...retained].sort()) {
        const historical = revisions[ref];
        const result = await configuredWorkerDeployment({ ...provisioning, profile: historical.profile }, ref);
        const historicalAvailable = result.available && canonical(result.profile) === canonical(historical.profile);
        if (historicalAvailable !== historical.available)
          operations.push({
            kind: "Deployment",
            pool,
            worker_profile: name,
            expected_ref: profile.ref,
            expected_contract: profile.logical_contract,
            profile: historical.profile,
            available: historicalAvailable,
            activate: false,
            reason: historicalAvailable ? "compiler_deployment" : "worker_unavailable",
          });
      }
      requirements.push({ pool, name, profile, routes, retained, worker });
    }
    if (!operations.length) return;
    if (approval) await verifyProspectiveBridgeReaders(provisioning, latest.projection, approval, operations);
    const expectedReaders = approval ? serializeProjection(latest.projection) : undefined;
    try {
      await publishOperations(options, context, { deployment: digest(operations), tip: latest.projection.tip }, "deployment", operations, state => {
        if (!approval && requiresContractBridgeApproval(state)) throw queueError("deployment_conflict", "deployment history changed; refresh protected bridge approval");
        if (expectedReaders) {
          const freshReaders = serializeProjection(state);
          if (["deployments", "works", "dispatches"].some(key => canonical(freshReaders[key] ?? {}) !== canonical(expectedReaders[key] ?? {})))
            throw queueError("deployment_conflict", "queue reader requirements changed; refresh approved bridge evidence");
        }
        for (const { pool, name, profile, routes, retained, worker } of requirements) {
          const freshRoutes = approval ? bridgeContractRoutes(state, pool, name, profile, approval) : {};
          const freshRetained = new Set(Object.values(freshRoutes));
          for (const work of state.works.values()) if (work.state === "available" && work.pool === pool && work.worker_profile === name && work.execution_ref && work.execution_ref !== profile.ref) freshRetained.add(work.execution_ref);
          if (canonical(state.deployments.get(pool)?.get(name)) !== canonical(worker) || canonical(freshRoutes) !== canonical(routes))
            throw queueError("deployment_conflict", "unfinished Work or deployment changed during contract migration; refresh the deployment routes");
          if (canonical([...freshRetained].sort()) !== canonical([...retained].sort())) throw queueError("deployment_conflict", "historical pin requirements changed; refresh their immutable deployment evidence");
        }
      });
      return;
    } catch (error) {
      if (error?.code !== "deployment_conflict" || attempt === 2) throw error;
      latest = await loadQueue({ ...options, policyProposal: undefined });
    }
  }
}

module.exports = { approvedWorkerProfiles, synchronizeDeployments };
