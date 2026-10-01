// @ts-check
/// <reference types="@actions/github-script" />
// @safe-outputs-exempt SEC-005: the maintenance apply job only writes the configured ledger branch in the current GitHub Actions repository context.
"use strict";

// Trusted Agentic Maintenance apply step for one ledger. It never executes user JavaScript.
// It treats the plan artifact as hostile input, revalidates it against the latest ledger branch
// state, and publishes the full transition in one commit guarded by expectedHeadOid. Concurrent
// appends only add new shards, so they are preserved; a moved head triggers a full refetch and
// revalidation rather than a blind overwrite.

const fs = require("node:fs");
const path = require("node:path");
const { fetchLedgerBranch } = require("./create_ledger_projection.cjs");
const { execGitSync } = require("./git_helpers.cjs");
const { materializeSnapshot, parseCompactionConfig, prepareApply, readPlanFile, validatePlan } = require("./ledger_compaction.cjs");
const { writeSummary } = require("./ledger_compaction_plan.cjs");

const MAX_ATTEMPTS = 5;
const COMMIT_MUTATION = `mutation($input: CreateCommitOnBranchInput!) {
  createCommitOnBranch(input: $input) { commit { oid } }
}`;

/** @param {number} ms */
function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

/**
 * @param {{config?: string, planFile?: string, githubClient?: any, owner?: string, repo?: string, serverHost?: string, token?: string, now?: Date, retryDelayMs?: number}} [options]
 */
async function main(options = {}) {
  const config = parseCompactionConfig(options.config);
  const planFile = options.planFile || process.env.GH_AW_LEDGER_COMPACTION_PLAN_FILE;
  if (!planFile) throw new TypeError("Missing ledger compaction plan path");
  const githubClient = options.githubClient || github;
  const owner = options.owner || context.repo.owner;
  const repo = options.repo || context.repo.repo;
  const serverHost = options.serverHost || new URL(process.env.GITHUB_SERVER_URL || "https://github.com").host;
  const token = options.token || process.env.GH_TOKEN;
  const retryDelayMs = options.retryDelayMs ?? 2000;
  /** @type {Record<string, string | number>} */
  const summary = { ledger: config.name, result: "rejected" };
  core.setOutput("result", "rejected");
  try {
    const plan = validatePlan(readPlanFile(planFile), config);
    Object.assign(summary, { trigger: plan.trigger, plan_id: plan.plan_id, validation: "plan schema valid" });
    for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt++) {
      summary.attempts = attempt;
      const scratch = fs.mkdtempSync(path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw-ledger-compaction-apply-"));
      try {
        const workspaceDir = path.join(scratch, "repo");
        const sourceDir = path.join(scratch, "source");
        execGitSync(["init", "-q", workspaceDir], { stdio: "pipe" });
        const refName = `refs/gh-aw/ledger-compaction/${config.name}`;
        const exists = await fetchLedgerBranch({ githubClient, owner, repo, branchName: config.branch, refName, workspaceDir, serverHost, token });
        if (!exists) return finish(summary, "stale", `ledger branch ${config.branch} no longer exists`);
        const head = execGitSync(["rev-parse", refName], { cwd: workspaceDir, stdio: "pipe" }).trim();
        materializeSnapshot({ workspaceDir, refName, sourceDir, config });
        const prepared = prepareApply({ plan, sourceDir, config, now: options.now });
        if (prepared.status === "rejected") throw new TypeError(`Rejected ledger compaction plan: ${prepared.reason}`);
        if (prepared.status !== "ready") return finish(summary, prepared.status, prepared.reason);
        Object.assign(summary, {
          validation: prepared.reason,
          source_segments: prepared.stats.sourceSegments,
          source_records: prepared.stats.sourceRecords,
          replacement_records: prepared.stats.replacementRecords,
          bytes_before: prepared.stats.bytesBefore,
          bytes_after: prepared.stats.bytesAfter,
          preserved_segments: prepared.stats.unrelatedSegments,
        });
        try {
          const response = await githubClient.graphql(COMMIT_MUTATION, {
            input: {
              branch: { repositoryNameWithOwner: `${owner}/${repo}`, branchName: config.branch },
              expectedHeadOid: head,
              message: { headline: `Compact ledger ${config.name}`, body: `Agentic Maintenance ledger compaction plan ${plan.plan_id}` },
              fileChanges: {
                additions: prepared.additions.map(addition => ({ path: addition.path, contents: addition.contents.toString("base64") })),
                deletions: prepared.deletions,
              },
            },
          });
          const oid = response && response.createCommitOnBranch && response.createCommitOnBranch.commit && response.createCommitOnBranch.commit.oid;
          if (oid) summary.commit = oid;
          return finish(summary, "applied", "compaction committed atomically");
        } catch (error) {
          // Never overwrite: refetch and revalidate. If the commit actually landed, the next
          // attempt observes it and reports already_applied.
          core.warning(`Ledger compaction commit attempt ${attempt} failed; revalidating against the latest ledger state`);
          core.debug(error instanceof Error ? error.message : String(error));
          summary.retried = attempt;
          if (attempt === MAX_ATTEMPTS) throw new Error(`Ledger compaction could not be applied after ${MAX_ATTEMPTS} attempts; the ledger was left unchanged and maintenance can retry`);
          await sleep(retryDelayMs * attempt);
        }
      } finally {
        fs.rmSync(scratch, { recursive: true, force: true });
      }
    }
    return finish(summary, "failed", "unreachable");
  } catch (error) {
    summary.result = error instanceof TypeError ? "rejected" : "failed";
    summary.reason = error instanceof Error ? error.message : "apply failed";
    core.setOutput("result", summary.result);
    throw error;
  } finally {
    await writeSummary("Ledger compaction apply", summary);
  }
}

/**
 * @param {Record<string, string | number>} summary
 * @param {string} result
 * @param {string} reason
 */
function finish(summary, result, reason) {
  summary.result = result;
  summary.reason = reason;
  core.setOutput("result", result);
  if (result === "stale" || result === "conflict") {
    core.warning(`Ledger compaction plan for ${summary.ledger} was not applied (${result}): ${reason}. The ledger is unchanged; the next maintenance run will plan from current state.`);
  }
  return { status: result, summary };
}

if (require.main === module) {
  main().catch(error => {
    core.setFailed(error instanceof Error ? error.message : "Ledger compaction apply failed");
  });
}

module.exports = { main };
