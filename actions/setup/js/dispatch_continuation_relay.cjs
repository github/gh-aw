// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Conclusion-job step that hands a recorded continuation request to the generated
 * continuation relay workflow. Producer attempts identify the exact request and
 * checkpoint artifacts after reruns.
 */

const { dispatchWorkflow } = require("./dispatch_workflow_transport.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_API } = require("./error_codes.cjs");
const { getDefaultBranch } = require("./continuation_request.cjs");

const DEFAULT_RELAY_WORKFLOW = "agentic-continuation-relay.yml";

/**
 * @param {{github?: any, context?: any, core?: any}} [options]
 */
async function main({ github = global.github, context = global.context, core = global.core } = {}) {
  const workflow = process.env.GH_AW_CONTINUATION_RELAY_WORKFLOW || DEFAULT_RELAY_WORKFLOW;
  const parentRunId = String(context.runId);
  const parentRunAttempt = String(process.env.GH_AW_PARENT_REQUEST_ATTEMPT || "");
  const checkpointRunAttempt = String(process.env.GH_AW_PARENT_CHECKPOINT_ATTEMPT || "");
  if (!/^[1-9]\d*$/.test(parentRunAttempt) || !/^[1-9]\d*$/.test(checkpointRunAttempt)) {
    core.setFailed(`${ERR_API}: Continuation request and checkpoint producer attempts are required`);
    return;
  }
  const inputs = { parent_run_id: parentRunId, parent_run_attempt: parentRunAttempt, checkpoint_run_attempt: checkpointRunAttempt };
  const { owner, repo } = context.repo;
  try {
    const branch = await getDefaultBranch(github, context);
    core.info(`Dispatching continuation relay ${workflow} on ${branch} for run ${parentRunId} attempt ${parentRunAttempt}`);
    const response = await dispatchWorkflow(github, { owner, repo, workflow_id: workflow, ref: `refs/heads/${branch}`, inputs, return_run_details: true }, { allowLegacyFallback: true, coreApi: core });
    const relayRunId = response?.data?.workflow_run_id;
    if (relayRunId) core.setOutput("relay_run_id", String(relayRunId));
    core.info(relayRunId ? `✓ Dispatched continuation relay run ${relayRunId}` : "✓ Dispatched continuation relay");
    await core.summary
      .addHeading("Continuation requested", 3)
      .addRaw(`The continuation relay \`${workflow}\` was dispatched for run ${parentRunId} (attempt ${parentRunAttempt})${relayRunId ? ` as run ${relayRunId}` : ""}.`, true)
      .write();
  } catch (error) {
    core.setFailed(
      `${ERR_API}: Failed to dispatch continuation relay ${workflow}: ${getErrorMessage(error)}. ` +
        `Ensure ${workflow} exists on the default branch (run \`gh aw compile\`), then retry with: ` +
        `gh workflow run ${workflow} -f parent_run_id=${parentRunId} -f parent_run_attempt=${parentRunAttempt} -f checkpoint_run_attempt=${checkpointRunAttempt}`
    );
  }
}

module.exports = { main, DEFAULT_RELAY_WORKFLOW };
