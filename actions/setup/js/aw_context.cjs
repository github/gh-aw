// @ts-check
/// <reference types="@actions/github-script" />

const { readExperimentAssignments } = require("./experiment_helpers.cjs");

/**
 * Resolves the item type, item number, and comment id from the GitHub Actions
 * event payload, covering issues, pull requests, discussions, check runs,
 * check suites, PR reviews, and comment variants.
 *
 * | Event family                              | item_type     | item_number              | comment_id              |
 * |-------------------------------------------|---------------|--------------------------|-------------------------|
 * | issues, issue_comment (on issue)          | issue         | payload.issue.number     | payload.comment.id      |
 * | issue_comment (on PR), pull_request,      | pull_request  | payload.pull_request.    | payload.review.id or    |
 * | pull_request_review, pull_request_review_ |               | number or                | payload.comment.id      |
 * | comment                                   |               | payload.issue.number     |                         |
 * | discussion, discussion_comment            | discussion    | payload.discussion.      | payload.comment.id      |
 * |                                           |               | number                   |                         |
 * | check_run                                 | check_run     | payload.check_run.id     |                         |
 * | check_suite                               | check_suite   | payload.check_suite.id   |                         |
 * | push, workflow_dispatch, …                | (empty)       | (empty)                  |                         |
 *
 * Note: for `issue_comment` events GitHub places the PR data in `payload.issue`
 * with a `payload.issue.pull_request` marker.  Those events are classified as
 * `pull_request` rather than `issue`.
 *
 * @param {any} payload - GitHub Actions context.payload
 * @returns {{ item_type: string, item_number: string, comment_id: string, comment_node_id: string }}
 *   comment_node_id is only populated for discussion/discussion_comment events where
 *   payload.comment.node_id is present (GraphQL node ID needed for reply threading).
 *   It is intentionally empty for all other event types (issues, PRs, checks).
 */
function resolveItemContext(payload) {
  if (payload?.issue != null) {
    // GitHub sends `issue_comment` events for PR comments with the PR data in
    // `payload.issue` and a `payload.issue.pull_request` marker.  Detect this
    // case and classify as pull_request so callers get the correct item type.
    if (payload.issue.pull_request != null) {
      return {
        item_type: "pull_request",
        item_number: payload.issue.number != null ? String(payload.issue.number) : "",
        comment_id: payload.comment?.id != null ? String(payload.comment.id) : "",
        comment_node_id: "",
      };
    }
    return {
      item_type: "issue",
      item_number: payload.issue.number != null ? String(payload.issue.number) : "",
      comment_id: payload.comment?.id != null ? String(payload.comment.id) : "",
      comment_node_id: "",
    };
  }
  if (payload?.pull_request != null) {
    return {
      item_type: "pull_request",
      item_number: payload.pull_request.number != null ? String(payload.pull_request.number) : "",
      // pull_request_review events carry a review object; pull_request_review_comment
      // events carry a comment object.  Both are reported as comment_id.
      comment_id: payload.comment?.id != null ? String(payload.comment.id) : payload.review?.id != null ? String(payload.review.id) : "",
      comment_node_id: "",
    };
  }
  if (payload?.discussion != null) {
    return {
      item_type: "discussion",
      item_number: payload.discussion.number != null ? String(payload.discussion.number) : "",
      comment_id: payload.comment?.id != null ? String(payload.comment.id) : "",
      // comment_node_id is the GraphQL node ID of the triggering discussion comment.
      // It can be used as reply_to_id in add_comment to thread responses under
      // the triggering comment when dispatching specialist workflows.
      comment_node_id: payload.comment?.node_id != null ? String(payload.comment.node_id) : "",
    };
  }
  if (payload?.check_run != null) {
    return {
      item_type: "check_run",
      item_number: payload.check_run.id != null ? String(payload.check_run.id) : "",
      comment_id: "",
      comment_node_id: "",
    };
  }
  if (payload?.check_suite != null) {
    return {
      item_type: "check_suite",
      item_number: payload.check_suite.id != null ? String(payload.check_suite.id) : "",
      comment_id: "",
      comment_node_id: "",
    };
  }
  return { item_type: "", item_number: "", comment_id: "", comment_node_id: "" };
}

/**
 * Builds a workflow-call identifier for the current workflow invocation.
 *
 * GitHub reusable workflows share the same run ID as their caller, so the
 * workflow ref is appended when available to distinguish parent and child
 * workflow invocations inside a single run.
 *
 * @param {string | number | null | undefined} runId
 * @param {string | number | null | undefined} runAttempt
 * @param {string | null | undefined} workflowRef
 * @returns {string}
 */
function buildWorkflowCallId(runId, runAttempt, workflowRef) {
  const normalizedRunId = String(runId ?? "").trim();
  if (!normalizedRunId) {
    return "";
  }

  const normalizedRunAttempt = String(runAttempt ?? "").trim() || "1";
  const normalizedWorkflowRef = typeof workflowRef === "string" ? workflowRef.trim() : "";
  const baseId = `${normalizedRunId}-${normalizedRunAttempt}`;

  return normalizedWorkflowRef ? `${baseId}:${normalizedWorkflowRef}` : baseId;
}

/**
 * @param {unknown} value
 * @returns {value is Record<string, unknown>}
 */
function isRecord(value) {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/**
 * Parse inbound aw_context from workflow inputs or repository_dispatch payload.
 *
 * Callers may deliver aw_context as a JSON string (workflow_call/workflow_dispatch)
 * or as a plain object (repository_dispatch client_payload).
 *
 * @param {unknown} raw
 * @returns {Record<string, unknown> | null}
 */
function parseInboundAwContext(raw) {
  if (raw == null) {
    return null;
  }
  if (typeof raw === "string") {
    const trimmed = raw.trim();
    if (!trimmed) {
      return null;
    }
    try {
      const parsed = JSON.parse(trimmed);
      if (isRecord(parsed)) {
        return parsed;
      }
    } catch {
      return null;
    }
    return null;
  }
  if (isRecord(raw)) {
    return raw;
  }
  return null;
}

/**
 * Resolve inbound aw_context from the current GitHub payload, if any.
 *
 * @param {any} payload
 * @returns {Record<string, unknown> | null}
 */
function readInboundAwContext(payload) {
  return parseInboundAwContext(payload?.inputs?.aw_context) || parseInboundAwContext(payload?.client_payload?.aw_context);
}

/**
 * Builds the aw_context object that identifies the calling workflow run.
 * This metadata is injected into dispatched workflows that declare an
 * aw_context input, allowing them to trace back to their caller and
 * resolve the current item (issue, pull request, discussion, check, etc.)
 * that triggered the calling workflow.
 *
 * @returns {{
 *   repo: string,
 *   run_id: string,
 *   run_attempt: string,
 *   workflow_id: string,
 *   episode_id: string,
 *   hop_id: string,
 *   parent_hop_id: string,
 *   origin_event: string,
 *   root_repo: string,
 *   root_workflow_id: string,
 *   root_run_id: string,
 *   workflow_call_id: string,
 *   time: string,
 *   actor: string,
 *   event_type: string,
 *   item_type: string,
 *   item_number: string,
 *   comment_id: string,
 *   comment_node_id: string,
 *   deployment_state: string,
 *   workflow_run_conclusion: string,
 *   otel_trace_id: string,
 *   otel_parent_span_id: string,
 *   trigger_label: string,
 *   experiments: string,
 *   allow_bot_authored_trigger_comment: boolean
 * }}
 * Properties:
 *   - item_type: Kind of entity that triggered the workflow (issue, pull_request,
 *     discussion, check_run, check_suite). Empty string for events with no item
 *     (e.g. push, workflow_dispatch).
 *   - item_number: Sequential number of the item (issue/PR/discussion) or database
 *     id (check_run/check_suite). Empty string when item_type is empty.
 *   - comment_id: ID of the triggering comment or review. Empty string when the
 *     event is not a comment/review event.
 *   - comment_node_id: GraphQL node ID of the triggering discussion comment.
 *     Only populated for discussion/discussion_comment events. Can be passed
 *     as reply_to_id in add_comment to thread responses under the triggering
 *     comment when a dispatched specialist workflow replies to a discussion.
 *   - deployment_state: The deployment status state value (e.g. "failure", "error",
 *     "success") when the workflow was triggered by a deployment_status event.
 *     Empty string for all other event types. Propagated to child workflows via
 *     workflow_call so they can identify which state triggered the parent.
 *   - workflow_run_conclusion: The conclusion of the triggering workflow_run
 *     (e.g. "failure", "success", "cancelled", "timed_out") when the workflow was
 *     triggered by a workflow_run event. Empty string for all other event types.
 *     Propagated to child workflows via workflow_call.
 *   - otel_trace_id: OTLP trace ID from the parent workflow's setup span.
 *     Empty string when OTLP is not configured or the parent setup step has
 *     not yet run.  Used by child workflow setup steps to continue the same
 *     trace as the parent (composite-action trace propagation).
 *   - otel_parent_span_id: OTLP span ID of the parent workflow's setup span.
 *     Empty string when OTLP is not configured or the parent setup step has
 *     not yet run.  Used by child workflow setup steps to link their setup
 *     span as a child of the parent's setup span for proper trace hierarchy.
 *   - trigger_label: Name of the label that triggered the workflow for labeled/unlabeled
 *     events (e.g. pull_request_target, issues, pull_request with labeled type).
 *     Empty string for events that do not carry label information.
 *   - experiments: Compact JSON string of the experiment variant assignments picked by
 *     pick_experiment.cjs for the current workflow run (e.g. `{"caveman":"yes"}`).
 *     Empty string when no experiments are declared or the assignments file cannot be read.
 *     Propagated to dispatched child workflows so they can identify which variants the
 *     parent workflow was running.
 *   - allow_bot_authored_trigger_comment: Set to `true` when the triggering event is
 *     an `issue_comment` with action `edited` and the comment was authored by a
 *     different account than `github.actor` (the bot-posted-menu / user-checks-box
 *     pattern).  Propagated to child workflows so their confused-deputy check can
 *     skip the actor-vs-comment-author mismatch guard for this known-safe scenario.
 *     `false` in all other cases.
 */
function buildAwContext() {
  const { item_type, item_number, comment_id, comment_node_id } = resolveItemContext(context.payload);
  const workflowRef = process.env.GITHUB_WORKFLOW_REF ?? "";
  const currentRepo = `${context.repo.owner}/${context.repo.repo}`;
  const currentRunId = String(process.env.GITHUB_RUN_ID ?? context.runId ?? "");
  const currentRunAttempt = String(process.env.GITHUB_RUN_ATTEMPT ?? "1");
  const currentHopId = buildWorkflowCallId(currentRunId, currentRunAttempt, workflowRef);
  const inheritedContext = readInboundAwContext(context.payload);
  const inheritedHopId = typeof inheritedContext?.hop_id === "string" ? inheritedContext.hop_id.trim() : typeof inheritedContext?.workflow_call_id === "string" ? inheritedContext.workflow_call_id.trim() : "";
  const parentHopId = typeof inheritedContext?.parent_hop_id === "string" && inheritedContext.parent_hop_id.trim() ? inheritedContext.parent_hop_id.trim() : inheritedHopId;
  const episodeId = typeof inheritedContext?.episode_id === "string" && inheritedContext.episode_id.trim() ? inheritedContext.episode_id.trim() : inheritedHopId || currentHopId;
  const originEvent =
    typeof inheritedContext?.origin_event === "string" && inheritedContext.origin_event.trim()
      ? inheritedContext.origin_event.trim()
      : typeof inheritedContext?.event_type === "string" && inheritedContext.event_type.trim()
        ? inheritedContext.event_type.trim()
        : (context.eventName ?? "");
  const rootRepo =
    typeof inheritedContext?.root_repo === "string" && inheritedContext.root_repo.trim()
      ? inheritedContext.root_repo.trim()
      : typeof inheritedContext?.repo === "string" && inheritedContext.repo.trim()
        ? inheritedContext.repo.trim()
        : currentRepo;
  const rootWorkflowId =
    typeof inheritedContext?.root_workflow_id === "string" && inheritedContext.root_workflow_id.trim()
      ? inheritedContext.root_workflow_id.trim()
      : typeof inheritedContext?.workflow_id === "string" && inheritedContext.workflow_id.trim()
        ? inheritedContext.workflow_id.trim()
        : workflowRef;
  const rootRunId =
    typeof inheritedContext?.root_run_id === "string" && inheritedContext.root_run_id.trim()
      ? inheritedContext.root_run_id.trim()
      : typeof inheritedContext?.run_id === "string" && inheritedContext.run_id.trim()
        ? inheritedContext.run_id.trim()
        : currentRunId;
  const assignments = readExperimentAssignments();
  const experimentAssignments = assignments ? JSON.stringify(assignments) : "";

  // Compute allow_bot_authored_trigger_comment ahead of the object.
  // True when the triggering event is issue_comment:edited, the comment was authored
  // by a GitHub App bot (login ends with "[bot]"), and the editor (actor) differs from
  // the comment author — the bot-posted-menu / user-checks-box pattern.
  const isIssueCommentEdited = context.eventName === "issue_comment" && context.payload?.action === "edited";
  const triggerCommentAuthor = context.payload?.comment?.user?.login;
  const triggerCommentByBot = typeof triggerCommentAuthor === "string" && triggerCommentAuthor.endsWith("[bot]");
  const allowBotAuthoredTriggerComment = isIssueCommentEdited && triggerCommentByBot && triggerCommentAuthor !== (context.actor ?? "");

  return {
    repo: currentRepo,
    run_id: String(context.runId ?? ""),
    run_attempt: currentRunAttempt,
    // GITHUB_WORKFLOW_REF provides the full workflow file path including the ref,
    // e.g. "owner/repo/.github/workflows/dispatcher.yml@refs/heads/main"
    workflow_id: workflowRef,
    // episode_id identifies the full automation session across workflow hops.
    episode_id: episodeId,
    // hop_id uniquely identifies this specific workflow invocation.
    hop_id: currentHopId,
    // parent_hop_id identifies the immediate caller when a workflow was spawned
    // by a previous automation hop.
    parent_hop_id: parentHopId,
    // origin_event captures the original GitHub event that started the episode.
    origin_event: originEvent,
    // root_* fields stay stable across all child workflow hops in the episode.
    root_repo: rootRepo,
    root_workflow_id: rootWorkflowId,
    root_run_id: rootRunId,
    // workflow_call_id uniquely identifies this specific workflow invocation,
    // including the workflow file when GitHub reuses a single run for caller
    // and callee workflow_call executions. Kept as a legacy alias of hop_id.
    workflow_call_id: currentHopId,
    time: new Date().toISOString(),
    actor: context.actor ?? "",
    event_type: context.eventName ?? "",
    item_type,
    item_number,
    comment_id,
    comment_node_id,
    // deployment_state carries the GitHub deployment_status state value when the
    // triggering event is deployment_status. Empty string for all other events.
    // Propagated to called workflows so they can access the deployment state.
    deployment_state: context.eventName === "deployment_status" ? (context.payload?.deployment_status?.state ?? "") : "",
    // workflow_run_conclusion carries the conclusion of the triggering workflow_run
    // when the event is workflow_run. Empty string for all other events.
    // Propagated to called workflows so they can access the workflow run conclusion.
    workflow_run_conclusion: context.eventName === "workflow_run" ? (context.payload?.workflow_run?.conclusion ?? "") : "",
    // Propagate the current OTLP trace ID to dispatched child workflows so that
    // composite actions share the same trace as their parent.  Empty string when
    // OTLP is not configured or the parent setup step has not run yet.
    otel_trace_id: process.env.GITHUB_AW_OTEL_TRACE_ID || "",
    // Propagate the current job's setup span ID so dispatched child workflows
    // can link their setup span as a child of this span for proper trace hierarchy.
    // Empty string when OTLP is not configured or the parent setup step has not run yet.
    otel_parent_span_id: process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID || "",
    // trigger_label is the label name from labeled/unlabeled events (pull_request_target,
    // issues, pull_request, etc.). Empty string for events without label data such as
    // workflow_dispatch, push, or schedule.
    trigger_label: context.payload?.label?.name ?? "",
    // experiments is a compact JSON string of the A/B experiment variant assignments
    // picked by pick_experiment.cjs for the current workflow run (e.g. {"caveman":"yes"}).
    // Empty string when no experiments are declared or the assignments file cannot be read.
    // Propagated to dispatched child workflows for experiment context continuity.
    experiments: experimentAssignments,
    // allow_bot_authored_trigger_comment is set to true when the triggering event is
    // issue_comment:edited, the comment was authored by a GitHub App bot (login ends
    // with "[bot]"), and the editor (actor) differs from the comment author — the
    // bot-posted-menu / user-checks-box pattern described in gh-aw issue #29480.
    // Propagated as metadata to child workflows so they can identify the trigger context.
    allow_bot_authored_trigger_comment: allowBotAuthoredTriggerComment,
  };
}

module.exports = { buildAwContext, buildWorkflowCallId, resolveItemContext, parseInboundAwContext };
