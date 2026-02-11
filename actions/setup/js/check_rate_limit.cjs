// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Rate limit check for per-user per-workflow triggers
 * Prevents users from triggering workflows too frequently
 */

async function main() {
  const actor = context.actor;
  const owner = context.repo.owner;
  const repo = context.repo.repo;
  const workflowId = context.workflow;
  const eventName = context.eventName;
  const runId = context.runId;

  // Get configuration from environment variables
  const maxRuns = parseInt(process.env.GH_AW_RATE_LIMIT_MAX || "5", 10);
  const windowMinutes = parseInt(process.env.GH_AW_RATE_LIMIT_WINDOW || "60", 10);
  const eventsList = process.env.GH_AW_RATE_LIMIT_EVENTS || "";

  core.info(`🔍 Checking rate limit for user '${actor}' on workflow '${workflowId}'`);
  core.info(`   Configuration: max=${maxRuns} runs per ${windowMinutes} minutes`);
  core.info(`   Current event: ${eventName}`);

  // Parse events to apply rate limiting to
  const limitedEvents = eventsList ? eventsList.split(",").map(e => e.trim()) : [];

  // If specific events are configured, check if current event should be limited
  if (limitedEvents.length > 0) {
    if (!limitedEvents.includes(eventName)) {
      core.info(`✅ Event '${eventName}' is not subject to rate limiting`);
      core.info(`   Rate limiting applies only to: ${limitedEvents.join(", ")}`);
      core.setOutput("rate_limit_ok", "true");
      return;
    }
    core.info(`   Event '${eventName}' is subject to rate limiting`);
  } else {
    core.info(`   Rate limiting applies to all programmatically triggered events`);
  }

  // Calculate time threshold
  const windowMs = windowMinutes * 60 * 1000;
  const thresholdTime = new Date(Date.now() - windowMs);
  const thresholdISO = thresholdTime.toISOString();

  core.info(`   Time window: runs created after ${thresholdISO}`);

  try {
    // Collect recent workflow runs by event type
    // This allows us to aggregate counts and short-circuit when max is exceeded
    let totalRecentRuns = 0;
    const runsPerEvent = {};

    core.info(`📊 Querying workflow runs for '${workflowId}'...`);

    // Query workflow runs (paginated if needed)
    let page = 1;
    let hasMore = true;
    const perPage = 100;

    while (hasMore && totalRecentRuns < maxRuns) {
      core.info(`   Fetching page ${page} (up to ${perPage} runs per page)...`);

      const response = await github.rest.actions.listWorkflowRuns({
        owner,
        repo,
        workflow_id: workflowId,
        per_page: perPage,
        page,
      });

      const runs = response.data.workflow_runs;
      core.info(`   Retrieved ${runs.length} runs from page ${page}`);

      if (runs.length === 0) {
        hasMore = false;
        break;
      }

      // Filter runs by actor and time window
      for (const run of runs) {
        // Stop processing if we've already exceeded the limit
        if (totalRecentRuns >= maxRuns) {
          core.info(`   Short-circuit: Already found ${totalRecentRuns} runs (>= max ${maxRuns})`);
          hasMore = false;
          break;
        }

        // Skip if run is older than the time window
        const runCreatedAt = new Date(run.created_at);
        if (runCreatedAt < thresholdTime) {
          core.info(`   Skipping run ${run.id} - created before threshold (${run.created_at})`);
          continue;
        }

        // Check if run is by the same actor
        if (run.actor?.login !== actor) {
          continue;
        }

        // Skip the current run (we're checking if we should allow THIS run)
        if (run.id === runId) {
          continue;
        }

        // Skip cancelled workflow runs (they don't count toward the rate limit)
        if (run.status === "cancelled") {
          core.info(`   Skipping run ${run.id} - cancelled (status: ${run.status})`);
          continue;
        }

        // Skip runs that completed in less than 15 seconds (treat as cancelled/failed fast)
        if (run.created_at && run.updated_at) {
          const runStart = new Date(run.created_at);
          const runEnd = new Date(run.updated_at);
          const durationSeconds = (runEnd.getTime() - runStart.getTime()) / 1000;

          if (durationSeconds < 15) {
            core.info(`   Skipping run ${run.id} - ran for less than 15s (${durationSeconds.toFixed(1)}s)`);
            continue;
          }
        }

        // If specific events are configured, only count matching events
        const runEvent = run.event;
        if (limitedEvents.length > 0 && !limitedEvents.includes(runEvent)) {
          continue;
        }

        // Count this run
        totalRecentRuns++;
        runsPerEvent[runEvent] = (runsPerEvent[runEvent] || 0) + 1;

        core.info(`   ✓ Run #${run.run_number} (${run.id}) by ${run.actor?.login} - ` + `event: ${runEvent}, created: ${run.created_at}, status: ${run.status}`);
      }

      // Check if we should fetch more pages
      if (runs.length < perPage || totalRecentRuns >= maxRuns) {
        hasMore = false;
      } else {
        page++;
      }
    }

    // Log summary by event type
    core.info(`📈 Rate limit summary for user '${actor}':`);
    core.info(`   Total recent runs in last ${windowMinutes} minutes: ${totalRecentRuns}`);
    core.info(`   Maximum allowed: ${maxRuns}`);

    if (Object.keys(runsPerEvent).length > 0) {
      core.info(`   Breakdown by event type:`);
      for (const [event, count] of Object.entries(runsPerEvent)) {
        core.info(`   - ${event}: ${count} runs`);
      }
    }

    // Check if rate limit is exceeded
    if (totalRecentRuns >= maxRuns) {
      core.warning(`⚠️ Rate limit exceeded for user '${actor}' on workflow '${workflowId}'`);
      core.warning(`   User has triggered ${totalRecentRuns} runs in the last ${windowMinutes} minutes (max: ${maxRuns})`);
      core.warning(`   Cancelling current workflow run...`);

      // Cancel the current workflow run
      try {
        await github.rest.actions.cancelWorkflowRun({
          owner,
          repo,
          run_id: runId,
        });
        core.warning(`✅ Workflow run ${runId} cancelled successfully`);
      } catch (cancelError) {
        const errorMsg = cancelError instanceof Error ? cancelError.message : String(cancelError);
        core.error(`❌ Failed to cancel workflow run: ${errorMsg}`);
        // Continue anyway - the rate limit output will still be set to false
      }

      core.setOutput("rate_limit_ok", "false");
      return;
    }

    // Rate limit not exceeded
    core.info(`✅ Rate limit check passed`);
    core.info(`   User '${actor}' has ${totalRecentRuns} runs in the last ${windowMinutes} minutes`);
    core.info(`   Remaining quota: ${maxRuns - totalRecentRuns} runs`);
    core.setOutput("rate_limit_ok", "true");
  } catch (error) {
    const errorMsg = error instanceof Error ? error.message : String(error);
    const errorStack = error instanceof Error ? error.stack : "";
    core.error(`❌ Rate limit check failed: ${errorMsg}`);
    if (errorStack) {
      core.error(`   Stack trace: ${errorStack}`);
    }

    // On error, allow the workflow to proceed (fail-open)
    // This prevents rate limiting from blocking workflows due to API issues
    core.warning(`⚠️ Allowing workflow to proceed due to rate limit check error`);
    core.setOutput("rate_limit_ok", "true");
  }
}

module.exports = { main };
