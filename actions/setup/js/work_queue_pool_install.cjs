// @ts-check
"use strict";

const { canonical, digest, queueError } = require("./work_queue_codec.cjs");
const store = require("./work_queue_store.cjs");
const { policyProposalFor, loadQueue } = require("./work_queue_binding.cjs");
const { requestForIntent } = require("./work_queue_intents.cjs");
const { actorFromContext, validatePolicy } = require("./work_queue_policy.cjs");
const { poolIdentity } = require("./work_queue_replay.cjs");
const { verifyWorkerRoute } = require("./work_queue_provisioning.cjs");

function policyWithoutPools(policy) {
  const { pools: _pools, ...rest } = policy;
  return rest;
}

function profileBoundary(profile) {
  const { ref: _ref, logical_contract: _contract, ...boundary } = profile;
  return boundary;
}

function poolBoundary(pool) {
  return { ...pool, profiles: Object.fromEntries(Object.entries(pool.profiles).map(([name, profile]) => [name, profileBoundary(profile)])) };
}

function poolFromProposal(state, proposal, poolName) {
  poolIdentity(poolName);
  validatePolicy(proposal);
  for (const name of Object.keys(proposal.pools)) poolIdentity(name);
  if (state.policy.authorization !== "aw" || proposal.authorization !== "aw") throw queueError("work_queue_pool_not_installed", "only AW-managed pools can be installed automatically");
  if (canonical(policyWithoutPools(state.policy)) !== canonical(policyWithoutPools(proposal))) throw queueError("work_queue_policy_proposal_mismatch", "automatic pool installation cannot change installed queue policy");
  for (const [name, pool] of Object.entries(state.policy.pools)) {
    if (!Object.hasOwn(proposal.pools, name) || canonical(poolBoundary(proposal.pools[name])) !== canonical(poolBoundary(pool)))
      throw queueError("work_queue_policy_proposal_mismatch", "automatic pool installation cannot change an installed pool");
  }
  if (!Object.hasOwn(proposal.pools, poolName)) throw queueError("work_queue_pool_not_installed", "the requested pool is absent from the compiled queue policy");
  const pool = structuredClone(proposal.pools[poolName]);
  for (const [name, profile] of Object.entries(pool.profiles)) {
    let approved;
    for (const [installedName, installedPool] of Object.entries(state.policy.pools)) {
      if (!Object.hasOwn(installedPool.profiles, name)) continue;
      const installed = installedPool.profiles[name];
      const deployment = state.deployments?.get(installedName)?.get(name);
      const candidates = deployment ? [deployment.revisions[deployment.current_ref], ...Object.values(deployment.revisions)].filter(revision => revision?.available).map(revision => revision.profile) : [installed];
      approved = candidates.find(candidate => candidate && canonical(profileBoundary(candidate)) === canonical(profileBoundary(profile)));
      if (approved) break;
    }
    if (!approved || approved.effect_scope !== state.repository || Object.hasOwn(approved, "principal"))
      throw queueError("work_queue_worker_not_compiler_approved", "new pool routes must match installed repository-scoped AW worker authority");
    pool.profiles[name] = structuredClone(approved);
  }
  return pool;
}

async function installWorkQueuePools(options, trustedContext, poolNames) {
  if (!["administrator", "producer", "dispatcher"].includes(trustedContext.role)) return;
  poolNames = [...new Set(poolNames.map(poolIdentity))];
  const configured = { ...options, githubClient: options.githubClient || options.queueClient };
  let latest = await loadQueue({ ...configured, policyProposal: undefined, initializationContext: undefined });
  if (!latest.projection.policy) return latest;
  const missing = [...new Set(poolNames)].filter(name => !Object.hasOwn(latest.projection.policy.pools, name));
  if (!missing.length) return latest;

  const proposal = policyProposalFor(configured);
  if (!proposal) throw queueError("work_queue_pool_not_installed", "the requested pool is absent from the installed Policy and no compiled pool proposal is available");
  const candidates = missing.map(poolName => [poolName, poolFromProposal(latest.projection, proposal, poolName)]);
  const provisioning = { githubClient: configured.githubClient, owner: options.context.repo.owner, repo: options.context.repo.repo };
  const verified = new Set();
  for (const [, pool] of candidates) {
    for (const profile of Object.values(pool.profiles)) {
      const route = canonical(profile);
      if (verified.has(route)) continue;
      await verifyWorkerRoute({ ...provisioning, profile });
      verified.add(route);
    }
  }

  for (const [poolName, pool] of candidates) {
    const operation = { kind: "PoolInstall", pool: poolName, config: pool };
    const request = requestForIntent(trustedContext, `pool-install:${digest(poolName)}`, "pool_install", { operations: [operation] });
    await (configured.publishWorkQueueRequest || store.publishWorkQueueRequest)({
      githubClient: configured.queueClient || configured.githubClient,
      owner: options.context.repo.owner,
      repo: options.context.repo.repo,
      ...(options.branch === undefined ? {} : { branch: options.branch }),
      context: trustedContext,
      actor: actorFromContext(trustedContext),
      request,
      generateOperations: state => {
        if (!state.policy || state.policy.authorization !== "aw") throw queueError("work_queue_pool_not_installed", "automatic installation requires an AW-managed queue");
        if (Object.hasOwn(state.policy.pools, poolName)) {
          if (canonical(state.policy.pools[poolName]) !== canonical(pool)) throw queueError("work_queue_policy_proposal_mismatch", "the pool was installed with a different definition");
          return { operations: [], idempotent: true };
        }
        const installedPool = poolFromProposal(state, proposal, poolName);
        if (canonical(installedPool) !== canonical(pool)) throw queueError("work_queue_policy_proposal_mismatch", "the pool proposal changed during installation");
        return { operations: [operation] };
      },
      core: options.core,
    });
    latest = await loadQueue({ ...configured, policyProposal: undefined, initializationContext: undefined });
  }
  return latest;
}

module.exports = { installWorkQueuePools };
