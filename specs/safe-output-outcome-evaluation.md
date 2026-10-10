---
title: Safe Output Outcome Evaluation Specification
version: 1.0.0
status: Working Draft
date: 2026-05-15
last_updated: 2026-10-10
---

# Safe Output Outcome Evaluation Specification

Every safe output type has a measurable outcome. This spec defines the exact evaluation logic for each type: what to check, how to classify the outcome, and what OTel attributes to emit.

## Principles

1. **Same as a repository observer would check.** If this action happened on GitHub, how would an observer decide it was good from visible repository state?
2. **Direct Outcome Only.** We check whether the action stuck, not whether it caused downstream effects.
3. **Bot-aware, not provenance-perfect.** Distinguish bot/app-visible closes and edits from non-bot ones, but do not assume non-bot actors are unassisted by AI.
4. **Observation-aware.** Evaluate snapshots after an appropriate observation period. Open issues and PRs remain pending; elapsed time alone is not rejection or acceptance.

## Norms

The key words **MUST**, **MUST NOT**, and **SHOULD** in this document are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119). These requirements apply to **outcome evaluation workers** as the primary conformance target unless a different conformance target is explicitly named in the requirement.

1. A primary-object `404` for issue, PR, or comment creation is `rejected`/`strong`/`deleted`. A supplementary endpoint `404` MUST NOT imply object deletion.
2. Other API failures return `error`/`weak`/`evaluation_error`. Failure to fetch supplementary PR effort evidence does not undo authoritative merge acceptance, but MUST prevent zero-touch proof.
3. Evaluation workers MUST NOT infer acceptance from unavailable data. Collector retry and rate-limit scheduling remain future lifecycle work.

## Provenance Limits

Outcome evaluation is based on observable GitHub state and actor identity, not hidden authoring provenance.

1. Outcome evaluation workers **MUST** treat actions performed by visible bot or app identities as bot/app actions.
2. Outcome evaluation workers **MUST** treat actions performed by visible non-bot user identities as non-bot actions, even if those users may have used Copilot or another AI assistant.
3. Outcome evaluation workers **MUST NOT** infer hidden AI assistance when GitHub exposes only a normal user identity.
4. Metrics and fields that use `human_*` names are historical names. In this specification they mean actor-visible, non-bot activity unless explicit provenance metadata is available.
5. Implementations **SHOULD** prefer explicit provenance markers when available, such as bot identities, GitHub App identities, trace IDs, labels, commit trailers, or other durable metadata emitted by the workflow.
6. When GitHub surfaces an action under a GitHub App or bot identity (including app-token API writes performed on behalf of a human), outcome evaluation workers **MUST** classify that action as bot/app activity for `human_*` fields unless separate durable provenance metadata explicitly identifies a visible non-bot actor.

## Outcome Categories

Every evaluation produces one of these outcomes:

| Outcome | Meaning |
|---------|---------|
| `accepted` | The action was kept, merged, resolved, or engaged with |
| `rejected` | The action was undone, closed-as-not-planned, removed, or reverted |
| `ignored` | An action reached a state without its expected effect |
| `pending` | The object has not reached a terminal state yet |
| `lifecycle` | Closed/removed by the workflow itself (e.g., `close-older-issues`) — not a rejection |
| `lifecycle_close` | Closed by lifecycle/noop bot policy and not reopened by a visible non-bot actor |
| `unknown` | Missing execution evidence, unsupported evaluator, or no action-specific observable signal |
| `error` | API or evaluation failure prevented verification |

### Cross-runtime conformance

Go and JavaScript consume the same cases in
`pkg/cli/testdata/outcome_conformance.json`. The normalized triple
`outcome_status`, `evidence_strength`, and `signal` is authoritative; collectors
and telemetry MUST preserve it instead of reinterpreting display text.

Creation of a PR is accepted only after merge. Creation of an issue is accepted
only when closed as `completed`; an open issue remains pending even when engaged.
Non-bot follow-up requires a visible actor identity and activity after the action.
Reactions and non-bot comment follow-up are medium evidence, not proof of causal
impact. Label and milestone retention MUST verify the recorded mutation or
identity, not merely that some labels or milestone exist. A submitted review
requires its recorded review ID; arbitrary later reviews and unverified team
membership MUST NOT establish acceptance.

Unsupported evaluators return `unknown`/`none`/`unsupported_evaluator`.
Generic target existence returns `unknown`/`weak`/`target_exists_only`.
Unknown, error, and lifecycle counts MUST be included in summaries.
Zero-touch acceptance requires available comment, review, and commit evidence;
missing evidence MUST NOT default to zero human effort.

## Common OTel Attributes

Every outcome span carries these attributes:

| Attribute | Type | Description |
|-----------|------|-------------|
| `gh-aw.outcome.type` | string | Safe output type (e.g., `create_pull_request`) |
| `gh-aw.outcome.result` | string | Normalized outcome, including `unknown`, `error`, and skipped metadata outputs |
| `gh-aw.outcome.object_url` | string | GitHub URL of the affected object |
| `gh-aw.outcome.object_number` | int | Issue/PR/discussion number |
| `gh-aw.outcome.repo` | string | `owner/repo` |
| `gh-aw.outcome.source_run_id` | string | Workflow run that created this output |
| `gh-aw.outcome.source_trace_id` | string | Original OTLP trace ID |
| `gh-aw.outcome.created_at` | string | When the safe output was executed |
| `gh-aw.outcome.checked_at` | string | When this evaluation ran |
| `gh-aw.outcome.time_to_outcome_hours` | float | Hours from creation to terminal state |
| `gh-aw.outcome.human_comments` | int | Historical field name; means actor-visible non-bot comments on the object |
| `gh-aw.outcome.human_edits` | int | Historical field name; means actor-visible non-bot edits before acceptance |
| `gh-aw.outcome.zero_touch` | bool | Accepted with no actor-visible non-bot modifications |

## Implementation

The following implementation areas are responsible for evaluation data capture, outcome classification plumbing, and runtime event artifacts.

Status meanings:
- `implemented`: dedicated evaluator logic exists in both Go and JS.
- `partial`: dedicated evaluator exists in one runtime; the other relies on generic fallback logic.
- `not-started`: no dedicated evaluator exists yet; current behavior is generic/no-op only.

### Current Default Acceptance Map

This table summarizes the current runtime behavior in `pkg/cli/outcome_eval*.go`. It is intentionally about what the evaluator accepts today, not just the intended long-term spec semantics.

Rows marked `evalGenericSticky` fallback are generic existence checks, not type-specific acceptance logic.

| Output type | Current evaluator | `accepted` at a glance |
|-------------|-------------------|------------------------|
| `create_pull_request` | `evalCreatePullRequest` | merged |
| `create_issue` | `evalCreateIssue` | closed as completed |
| `add_comment` | `evalAddComment` | reacted to or replied to |
| `add_labels` | `evalAddLabels` | recorded added-label delta retained |
| `add_reviewer` | dedicated review-request evaluator | recorded reviewer submitted a review |
| `update_issue` | dedicated retained-update evaluator | intended edit still matches current issue state |
| `update_pull_request` | dedicated retained-update evaluator | intended edit still matches current PR state |
| `close_issue` | `evalCloseSticky` | retained visible non-bot close |
| `close_pull_request` | `evalCloseSticky` | retained visible non-bot close, unmerged |
| `close_discussion` | `evalCloseDiscussion` | none yet |
| `create_discussion` | `evalCreateDiscussion` | none yet |
| `update_discussion` | `evalUpdateDiscussion` | none yet (pending GraphQL) |
| `create_pull_request_review_comment` | `evalReviewComment` | none yet |
| `submit_pull_request_review` | dedicated review evaluator | review affected PR lifecycle |
| `reply_to_pull_request_review_comment` | `evalGenericSticky` fallback | none; existence is weak unknown |
| `resolve_pull_request_review_thread` | `evalResolveThread` | none yet |
| `push_to_pull_request_branch` | `evalPushToPRBranch` | recorded commits verified in merged history |
| `mark_pull_request_as_ready_for_review` | `evalMarkReady` | reviewed |
| `assign_to_agent` | `evalAssignToAgent` | attributable post-assignment agent PR merged |
| `dispatch_workflow` | `evalDispatchWorkflow` | workflow run completed with success |
| `autofix_code_scanning_alert` | `evalGenericSticky` fallback | none; existence is weak unknown |
| `create_code_scanning_alert` | `evalGenericSticky` fallback | none; existence is weak unknown |
| `link_sub_issue` | `evalGenericSticky` fallback | none; existence is weak unknown |
| `hide_comment` | `evalHideComment` | none yet |
| `assign_milestone` | `evalAssignMilestone` | recorded milestone identity retained |
| `replace_label` | `evalReplaceLabel` | label replacement retained |
| `update_project` | `evalGenericSticky` fallback | none; existence is weak unknown |
| `update_release` | `evalGenericSticky` fallback | none; existence is weak unknown |
| `noop` | explicit skip | skipped |
| `missing_tool` | explicit skip | skipped |

### Implementation locations

The Go dispatcher is `pkg/cli/outcome_eval.go`; dedicated evaluators are split
across `outcome_eval_{pr,issue,comment,label,generic,agent,review,update,workflow}.go`.
Common actor and evidence helpers live in `outcome_eval_evidence.go`.

The JavaScript dispatcher and primary action evaluators are
`actions/setup/js/outcome_action_evaluators.cjs`. Review and retained-update
evaluators live in `outcome_review_evaluators.cjs`, with common evidence helpers
in `outcome_evidence.cjs`. `evaluate_outcomes.cjs` collects runs and preserves
their typed results; `emit_outcome_spans.cjs` exports them.

Discussion, review-thread, review-comment creation, and comment-hiding
evaluators remain unsupported in both runtimes. Fallback outputs have no
implemented acceptance rule. Collection revisit/retry policy and per-outcome
AIC attribution are separate remaining work; cross-runtime conformance does
not complete those features.

---

## 1. `create_pull_request`

**Question:** Was the PR merged?

**API:** `GET /repos/{owner}/{repo}/pulls/{number}`

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| `merged == true` | `accepted` |
| `state == "closed"` and `merged == false` | `rejected` |
| `state == "open"` | `pending` |

**Extra signals:**
- `human_edits`: historical field name; count actor-visible non-bot commits pushed by users other than the PR author after creation
- `human_comments`: historical field name; count actor-visible non-bot comments on the PR
- `zero_touch`: accepted with complete supplementary evidence and no post-action visible non-bot comments, submitted reviews, or edits
- `time_to_outcome_hours`: `merged_at - created_at` or `closed_at - created_at`

**Additional OTel attributes:**

| Attribute | Type | Description |
|-----------|------|-------------|
| `ghaw.outcome.pr.merged` | bool | Whether PR was merged |
| `ghaw.outcome.pr.review_count` | int | Number of reviews submitted |
| `ghaw.outcome.pr.additions` | int | Lines added |
| `ghaw.outcome.pr.deletions` | int | Lines deleted |

---

## 2. `create_issue`

**Question:** Was the issue resolved or dismissed?

**API:** `GET /repos/{owner}/{repo}/issues/{number}`

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| `state == "closed"` and `state_reason == "completed"` | `accepted` |
| `state == "closed"` and `state_reason == "not_planned"` and closed by bot | `lifecycle` |
| `state == "closed"` and `state_reason == "not_planned"` and closed by a visible non-bot actor | `rejected` |
| `state == "open"` and has non-bot comments | `pending` (engaged) |
| `state == "open"` and no non-bot comments | `pending` |

**Bot detection:** check the latest close event in `GET /repos/{owner}/{repo}/issues/{number}/events`. Honor bot actor types and known bot logins; a missing actor leaves evaluation unverifiable.

**Extra signals:**
- `human_comments`: historical field name; non-bot comments
- Reactions on the issue body

**Additional OTel attributes:**

| Attribute | Type | Description |
|-----------|------|-------------|
| `ghaw.outcome.issue.state_reason` | string | `completed` or `not_planned` |
| `ghaw.outcome.issue.closed_by` | string | Username that closed the issue |
| `ghaw.outcome.issue.closed_by_bot` | bool | Whether a bot closed it |

---

## 3. `add_comment`

**Question:** Did anyone respond or react?

**API:** `GET /repos/{owner}/{repo}/issues/comments/{comment_id}`

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| Comment has replies (subsequent comments referencing it) or reactions > 0 | `accepted` |
| Comment exists, no visible non-bot follow-up, no reactions | `pending` |
| Comment was deleted (404) | `rejected` |
| Minimized state | Not currently evaluated |

**Extra signals:**
- Reaction count and types
- Reply count (comments posted after this one on the same issue/PR)

**Additional OTel attributes:**

| Attribute | Type | Description |
|-----------|------|-------------|
| `ghaw.outcome.comment.reactions` | int | Total reaction count |
| `ghaw.outcome.comment.replies` | int | Subsequent comments on same thread |
| `ghaw.outcome.comment.minimized` | bool | Whether the comment was hidden |

---

## 4. `add_labels`

**Question:** Did the labels stick?

**API:** `GET /repos/{owner}/{repo}/issues/{number}/labels`

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| All added labels still present | `accepted` |
| Some labels removed | `rejected` (partial) |
| All labels removed | `rejected` |

**Additional OTel attributes:**

| Attribute | Type | Description |
|-----------|------|-------------|
| `ghaw.outcome.labels.added` | int | Labels the workflow added |
| `ghaw.outcome.labels.retained` | int | Labels still present |
| `ghaw.outcome.labels.removed` | int | Labels that were removed |

**API failure safeguards (`add_labels`):** failures return
`error`/`weak`/`evaluation_error`, never acceptance based on missing data.
Recorded before-state and the actual added-label delta are required. Automatic
retry scheduling remains a collector lifecycle concern, not an implemented
classification rule.

---

## 5. `add_reviewer`

**Question:** Did the reviewer actually review?

**API:** `GET /repos/{owner}/{repo}/pulls/{number}/reviews`

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| At least one review submitted by the assigned reviewer | `accepted` |
| No review from assigned reviewer but PR still open | `pending` |
| No review and PR closed/merged | `ignored` |

**Additional OTel attributes:**

| Attribute | Type | Description |
|-----------|------|-------------|
| `ghaw.outcome.reviewer.reviewed` | bool | Whether the assigned reviewer submitted a review |
| `ghaw.outcome.reviewer.review_state` | string | `approved`, `changes_requested`, `commented` |

---

## 6. `update_issue`

**Question:** Did the edit stick?

**API:** `GET /repos/{owner}/{repo}/issues/{number}`

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| Changed fields still match the persisted `after_state` snapshot | `accepted` |
| Changed fields match the persisted `before_state` snapshot again | `rejected` |
| Changed fields differ from both `before_state` and `after_state` | `rejected` |
| Required `before_state` / `after_state` snapshots are missing | `unknown` |

**Detection:** compare the current issue fields against the execution-time `before_state` and `after_state` snapshots captured in the safe-output manifest. Compare `title`, normalized `body_hash`, `state`, `labels`, and `assignees`.

---

## 7. `update_pull_request`

**Question:** Did the edit stick?

Same retained-update logic as `update_issue` but on a PR object, using persisted execution-time snapshots instead of `updated_at`.

**API:** `GET /repos/{owner}/{repo}/pulls/{number}`

**Detection:** compare the current PR fields against the execution-time `before_state` and `after_state` snapshots. Compare `title`, normalized `body_hash`, `state`, `base`, `draft`, and `head_sha`. If the retained state later merges, classify that acceptance as stronger evidence.

---

## 8. `close_issue`

**Question:** Did it stay closed?

**API:** `GET /repos/{owner}/{repo}/issues/{number}`

**Evaluation order:** first check the current issue state. If the issue is currently open (meaning it was reopened after a prior close), classify as `rejected` regardless of prior close actor. Only currently closed issues use actor-based classification below.

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| Issue still closed and close actor is `github-actions[bot]` or configured lifecycle bot | `lifecycle_close` |
| Issue still closed and latest close actor is a visible non-bot user | `accepted` |
| Issue reopened | `rejected` |

---

## 9. `close_pull_request`

**Question:** Did it stay closed?

**API:** `GET /repos/{owner}/{repo}/pulls/{number}`

**Evaluation order:** first check the current PR state. If the PR is currently open (meaning it was reopened after a prior close), classify as `rejected` regardless of prior close actor. Only currently closed PRs use actor-based classification below.

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| PR still closed and close actor is `github-actions[bot]` or configured lifecycle bot | `lifecycle_close` |
| PR still closed, unmerged, and latest close actor is a visible non-bot user | `accepted` |
| PR merged after the close action | `rejected` |
| PR reopened | `rejected` |

---

## 10. `close_discussion`

**Proposed rule only:** current runtimes return `unknown`/`none`/`unsupported_evaluator`. The conditions below are not implemented acceptance rules.

**Question:** Did it stay closed?

**API:** GraphQL `repository.discussion(number:)` → `closed`

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| Discussion still closed | `accepted` |
| Discussion reopened | `rejected` |

---

## 11. `create_discussion`

**Proposed rule only:** current runtimes return `unknown` with no evidence.

**Question:** Did anyone engage?

**API:** GraphQL `repository.discussion(number:)` → `comments.totalCount`, `answer`

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| Has replies or is marked as answered | `accepted` |
| No replies, not answered | `ignored` |

**Additional OTel attributes:**

| Attribute | Type | Description |
|-----------|------|-------------|
| `ghaw.outcome.discussion.replies` | int | Reply count |
| `ghaw.outcome.discussion.answered` | bool | Whether marked as answered |

---

## 12. `update_discussion`

**Question:** Did the edit stick?

Same logic as `update_issue` but via GraphQL on the discussion body.

---

## 13. `create_pull_request_review_comment`

**Proposed rule only:** current runtimes return `unknown` with no evidence.

**Question:** Was the thread resolved or engaged?

**API:** GraphQL `pullRequest.reviewThreads` filtered to the comment's thread

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| Thread resolved | `accepted` |
| Thread has replies | `accepted` |
| Thread not resolved, no replies | `ignored` |

---

## 14. `submit_pull_request_review`

**Question:** Was the feedback addressed?

**API:** `GET /repos/{owner}/{repo}/pulls/{number}/commits` (check for commits after review)

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| Recorded changes-requested review followed by commits and merge | `accepted` (medium evidence) |
| Review dismissed | `rejected` |
| No new commits and PR still open | `pending` |
| Recorded approval/comment review retained on merged PR | `accepted` |

---

## 15. `reply_to_pull_request_review_comment`

**Proposed rule only:** current fallback verifies only target existence and returns weak `unknown`.

**Question:** Was the conversation advanced?

**API:** GraphQL `pullRequest.reviewThreads` → check thread state

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| Thread resolved after reply | `accepted` |
| Further replies from others | `accepted` |
| No further activity | `ignored` |

---

## 16. `resolve_pull_request_review_thread`

**Proposed rule only:** current runtimes return `unknown` with no evidence.

**Question:** Did it stay resolved?

**API:** GraphQL `pullRequest.reviewThreads` → `isResolved`

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| Thread still resolved | `accepted` |
| Thread unresolved | `rejected` |

---

## 17. `push_to_pull_request_branch`

**Question:** Was the code accepted?

**API:** `GET /repos/{owner}/{repo}/pulls/{number}`

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| PR merged and recorded pushed commits verified in merge ancestry | `accepted` |
| PR closed without merge | `rejected` |
| PR still open | `pending` |
| PR merged but recorded commits not verified in ancestry | `unknown` |

---

## 18. `mark_pull_request_as_ready_for_review`

**Question:** Did someone review it?

**API:** `GET /repos/{owner}/{repo}/pulls/{number}/reviews`

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| At least one visible non-bot review submitted strictly after marking | `accepted` |
| No reviews but PR still open | `pending` |
| PR merged or closed with no reviews | `ignored` |

---

## 19. `assign_to_agent`

**Question:** Did the agent produce a result?

**API:**
1. `GET /repos/{owner}/{repo}/issues/{number}` → check state
2. Search for PRs from agent: `GET /repos/{owner}/{repo}/issues/{number}/timeline` → find linked PRs from `copilot-swe-agent`

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| Agent PR created and merged | `accepted` |
| Agent PR created but closed without merge | `rejected` |
| Agent PR created and still open | `pending` |
| No attributable post-assignment agent PR | `unknown` |
| Issue resolved without attributable agent PR | `unknown` |

**Additional OTel attributes:**

| Attribute | Type | Description |
|-----------|------|-------------|
| `ghaw.outcome.agent.pr_number` | int | PR number created by agent |
| `ghaw.outcome.agent.pr_merged` | bool | Whether agent's PR was merged |

---

## 20. `dispatch_workflow`

**Question:** Did the dispatched workflow succeed?

**API:** `GET /repos/{owner}/{repo}/actions/runs/{recorded_run_id}`; no guessed workflow/time-window attribution

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| Dispatched run completed with `conclusion == "success"` | `accepted` |
| Dispatched run completed with `conclusion == "failure"` | `rejected` |
| Dispatched run not found or still running | `pending` |

---

## 21. `autofix_code_scanning_alert`

**Proposed rule only:** current fallback returns weak `unknown`, not acceptance.

**Question:** Was the fix accepted?

**API:**
1. Check alert state: `GET /repos/{owner}/{repo}/code-scanning/alerts/{alert_number}`
2. Check linked PR if any

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| Alert state changed to `fixed` | `accepted` |
| Alert dismissed | `rejected` |
| Linked PR merged | `accepted` |
| Linked PR closed | `rejected` |
| Alert still open | `pending` |

---

## 22. `create_code_scanning_alert`

**Proposed rule only:** current fallback returns weak `unknown`, not acceptance.

**Question:** Was the alert triaged?

**API:** `GET /repos/{owner}/{repo}/code-scanning/alerts/{alert_number}`

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| Alert state `fixed` | `accepted` |
| Alert state `dismissed` with reason | `accepted` (triaged) |
| Alert still `open` | `pending` |

---

## 23. `link_sub_issue`

**Proposed rule only:** current fallback returns weak `unknown`, not acceptance.

**Question:** Did the link stick?

**API:** GraphQL `issue.subIssues`

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| Sub-issue link still present | `accepted` |
| Link removed | `rejected` |

---

## 24. `hide_comment`

**Proposed rule only:** current runtimes return `unknown` with no evidence.

**Question:** Did it stay hidden?

**API:** GraphQL `node(id:)` → `isMinimized`

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| Comment still minimized | `accepted` |
| Comment un-minimized | `rejected` |

---

## 25. `assign_milestone`

**Question:** Did the milestone stick?

**API:** `GET /repos/{owner}/{repo}/issues/{number}`

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| Recorded milestone number still assigned | `accepted` |
| Milestone removed or changed | `rejected` |

---

## 26. `update_project`

**Proposed rule only:** current fallback returns weak `unknown`, not acceptance.

**Question:** Did the field value stick?

**API:** GraphQL `projectV2` → field value query

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| Field value unchanged | `accepted` |
| Field value changed by someone else | `rejected` |

---

## 27. `update_release`

**Proposed rule only:** current fallback returns weak `unknown`, not acceptance.

**Question:** Did the edit stick?

**API:** `GET /repos/{owner}/{repo}/releases/{release_id}`

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| Release body/name unchanged since workflow edit | `accepted` |
| Release body/name changed by someone else | `rejected` |

---

## 28. `noop`

No outcome to evaluate. Skip.

## 29. `missing_tool`

No outcome to evaluate. Skip.

## 30. `replace_label`

**Question:** Did the label replacement stick? Specifically: is `label_to_add` present on the item and is `label_to_remove` absent?

**API:** `GET /repos/{owner}/{repo}/issues/{number}/labels`

**Evaluation:**

| Condition | Outcome |
|-----------|---------|
| `label_to_add` is present on the item AND `label_to_remove` is absent | `accepted` |
| `label_to_add` is absent from the item | `rejected` |
| `label_to_add` is present but `label_to_remove` is also still present | `rejected` (partial failure — remove did not apply) |
| API failure, including missing labeling target | `error` with weak evidence |
| `lifecycle` | N/A — `replace_label` has no lifecycle bot-close behavior |
| `lifecycle_close` | N/A — `replace_label` has no lifecycle bot-close behavior |
| `ignored` | N/A — label state is always evaluable when the item is accessible; no time-bounded engagement signal applies |

**Extra signals:**
- `label_to_add`: the label name that should be present after the replacement
- `label_to_remove`: the label name that should be absent after the replacement
- Label zero-touch attribution is not currently implemented.

**Additional OTel attributes:**

| Attribute | Type | Description |
|-----------|------|-------------|
| `ghaw.outcome.replace_label.label_to_add` | string | Label that should be present after the replacement |
| `ghaw.outcome.replace_label.label_to_remove` | string | Label that should be absent after the replacement |
| `ghaw.outcome.replace_label.add_present` | bool | Whether `label_to_add` is present at evaluation time |
| `ghaw.outcome.replace_label.remove_absent` | bool | Whether `label_to_remove` is absent at evaluation time |

**API failure safeguards (`replace_label`):**

The evaluator requires recorded before/after label state and compares the actual
replacement delta. Missing snapshots return `unknown` with no evidence.
API failures return `error`; execution-time handler retry policy in
`replace-label-spec.md` is separate from delayed outcome classification.

**References:** See [replace-label-spec.md](replace-label-spec.md) for the full definition of the `replace_label` safe-output type, including the message schema, processing model, and REST interface.

---

## Derived Metrics

From the outcome evaluations above, compute:

| Metric | Formula | Description | Go aggregation owner | OTel emission owner |
|--------|---------|-------------|----------------------|---------------------|
| `acceptance_rate` | accepted / (accepted + rejected) | How often actions are kept | `pkg/cli/outcome_eval.go` (`ComputeOutcomeSummary`) | `actions/setup/js/emit_outcome_spans.cjs` (`buildSummaryAttributes`) |
| `waste_rate` | rejected / total | How often actions are undone | `pkg/cli/outcome_eval.go` (`ComputeOutcomeSummary`) | `actions/setup/js/emit_outcome_spans.cjs` (`buildSummaryAttributes`) |
| `ignore_rate` | ignored / total | How often actions get no response | `pkg/cli/outcome_eval.go` (`ComputeOutcomeSummary`) | `actions/setup/js/emit_outcome_spans.cjs` (`buildSummaryAttributes`) |
| `zero_touch_rate` | zero_touch / accepted | How often accepted actions need no actor-visible non-bot edits | `pkg/cli/outcome_eval.go` (`ComputeOutcomeSummary`) | `actions/setup/js/emit_outcome_spans.cjs` (`buildSummaryAttributes`) |
| `time_to_outcome` | median(time_to_outcome_hours) | How fast outcomes resolve | `pkg/cli/outcome_eval.go` (`ComputeOutcomeSummary`) | `actions/setup/js/emit_outcome_spans.cjs` (`buildSummaryAttributes`) |
| `cost_per_accepted_outcome` | total_run_cost / accepted_count | Efficiency metric | `pkg/cli/outcome_eval.go` (`ComputeOutcomeSummary`) | `actions/setup/js/emit_outcome_spans.cjs` (`buildSummaryAttributes`) |

## Implementation Priority

Start with the 5 highest-value, lowest-effort types:

1. `create_pull_request` — cleanest signal, most valuable
2. `create_issue` — common, needs bot-aware close detection
3. `add_comment` — very common, engagement signal
4. `add_labels` — simple binary check
5. `assign_to_agent` — important for delegation workflows

These cover the majority of safe output usage. Add the rest incrementally.

---

## Conformance

### Conformance Test Table

The table below specifies one conformance test row per safe-output type. Each row defines the expected OTel attribute value emitted by a correct evaluator, the pass condition (what must be true for `accepted`), and the fail condition (what signals `rejected`). Implementations **MUST** satisfy the pass condition and **MUST** not emit `accepted` when the fail condition is observed.

| Output type | Expected `ghaw.outcome.type` OTel attribute | Pass condition | Fail condition |
|---|---|---|---|
| `create_pull_request` | `create_pull_request` | PR exists in open or merged state; was not closed-as-not-planned or reverted within the evaluation window | PR closed-as-not-planned, reverted, or deleted within the evaluation window |
| `create_issue` | `create_issue` | Issue exists in open state, or was closed by a visible non-bot action (not bot policy) within the evaluation window | Issue closed-as-not-planned by a visible non-bot actor within the evaluation window, or deleted |
| `add_comment` | `add_comment` | Comment exists on the target object at evaluation time | Comment was deleted or hidden by a visible non-bot actor within the evaluation window |
| `add_labels` | `add_labels` | At least one of the bot-applied labels is still present on the target object at evaluation time | All bot-applied labels were removed by a visible non-bot actor within the evaluation window |
| `add_reviewer` | `add_reviewer` | Requested reviewer is still listed as a requested reviewer, or has already submitted a review | Reviewer request was removed by a visible non-bot actor before any review was submitted |
| `update_issue` | `update_issue` | Updated field(s) (title, body, assignee) match the values the bot submitted at evaluation time | Updated field(s) were reverted to pre-bot values by a visible non-bot actor within the evaluation window |
| `update_pull_request` | `update_pull_request` | Updated field(s) (title, body, base branch) match the values the bot submitted at evaluation time | Updated field(s) were reverted to pre-bot values by a visible non-bot actor within the evaluation window |
| `close_issue` | `close_issue` | Issue remains closed at evaluation time | Issue was reopened by a visible non-bot actor within the evaluation window |
| `close_pull_request` | `close_pull_request` | PR remains closed (not merged) at evaluation time | PR was reopened or merged after the bot closed it within the evaluation window |
| `close_discussion` | `close_discussion` | Discussion remains closed at evaluation time | Discussion was reopened by a visible non-bot actor within the evaluation window |
| `create_discussion` | `create_discussion` | Discussion exists and has not been deleted or locked within the evaluation window | Discussion was deleted or permanently locked (preventing any responses) within the evaluation window |
| `update_discussion` | `update_discussion` | Updated field(s) (title, body, category) match the values the bot submitted at evaluation time | Updated field(s) were reverted to pre-bot values by a visible non-bot actor within the evaluation window |
| `create_pull_request_review_comment` | `create_pull_request_review_comment` | Review comment exists on the PR diff at evaluation time | Review comment was deleted by a visible non-bot actor within the evaluation window |
| `submit_pull_request_review` | `submit_pull_request_review` | PR review record exists with the submitted state (APPROVED, CHANGES_REQUESTED, COMMENT) at evaluation time | Review was dismissed by a visible non-bot actor within the evaluation window |
| `reply_to_pull_request_review_comment` | `reply_to_pull_request_review_comment` | Reply comment exists in the review thread at evaluation time | Reply comment was deleted by a visible non-bot actor within the evaluation window |
| `resolve_pull_request_review_thread` | `resolve_pull_request_review_thread` | Review thread remains resolved at evaluation time | Thread was re-opened (un-resolved) by a visible non-bot actor within the evaluation window |
| `push_to_pull_request_branch` | `push_to_pull_request_branch` | The pushed commit SHA is still present in the PR branch history at evaluation time | The commit was force-pushed out of the branch history by a visible non-bot actor within the evaluation window |
| `mark_pull_request_as_ready_for_review` | `mark_pull_request_as_ready_for_review` | PR is no longer in draft state at evaluation time | PR was converted back to draft by a visible non-bot actor within the evaluation window |
| `assign_to_agent` | `assign_to_agent` | Assignment record exists on the target issue/PR at evaluation time | Assignment was removed by a visible non-bot actor before the assigned agent acted on it |
| `dispatch_workflow` | `dispatch_workflow` | The dispatched workflow run exists and reached a terminal state (success or failure) within the evaluation window | The dispatched workflow run was cancelled before reaching a terminal state; or no corresponding run record is found |
| `autofix_code_scanning_alert` | `autofix_code_scanning_alert` | Code scanning alert is in a fixed or dismissed state at evaluation time | Alert was re-opened or the fix commit was reverted within the evaluation window |
| `create_code_scanning_alert` | `create_code_scanning_alert` | Alert record exists in the repository's code scanning results at evaluation time | Alert was immediately dismissed (within the evaluation window) with no investigation action |
| `link_sub_issue` | `link_sub_issue` | Sub-issue link exists on the parent issue at evaluation time | Sub-issue link was removed by a visible non-bot actor within the evaluation window |
| `hide_comment` | `hide_comment` | Comment is minimized (hidden) at evaluation time | Comment was un-hidden by a visible non-bot actor within the evaluation window |
| `assign_milestone` | `assign_milestone` | Milestone assignment is present on the target issue/PR at evaluation time | Milestone assignment was removed by a visible non-bot actor within the evaluation window |
| `update_project` | `update_project` | Project item field(s) match the values the bot submitted at evaluation time | Project item field(s) were reverted to pre-bot values by a visible non-bot actor within the evaluation window |
| `update_release` | `update_release` | Release field(s) (name, body, tag, draft status) match the values the bot submitted at evaluation time | Release field(s) were reverted by a visible non-bot actor, or the release was deleted within the evaluation window |
| `noop` | `noop` | Evaluation is skipped; no outcome is computed | N/A — `noop` always results in `ignored` |
| `missing_tool` | `missing_tool` | Evaluation is skipped; no outcome is computed | N/A — `missing_tool` always results in `ignored` |
| `replace_label` | `replace_label` | `label_to_add` is present on the target item AND `label_to_remove` is absent at evaluation time | `label_to_add` is absent, or `label_to_remove` is still present, or the item was deleted within the evaluation window |

### Sync Follow-ups: Safe-Output Section-to-Test Mapping

| Section | Output type | Compliance test file(s) | Coverage status |
|---|---|---|---|
| §1 | `create_pull_request` | `pkg/cli/outcome_eval_formal_test.go` | covered |
| §2 | `create_issue` | `pkg/cli/outcome_eval_formal_test.go` | covered |
| §3 | `add_comment` | `pkg/cli/outcome_eval_formal_test.go` | covered |
| §4 | `add_labels` | `pkg/cli/outcome_eval_formal_test.go`, `pkg/cli/outcome_eval_test.go` | covered |
| §5 | `add_reviewer` | `pkg/cli/outcome_eval_test.go` | covered |
| §6 | `update_issue` | `pkg/cli/outcome_eval_update_test.go` | covered |
| §7 | `update_pull_request` | `pkg/cli/outcome_eval_update_test.go` | covered |
| §8 | `close_issue` | `pkg/cli/outcome_eval_formal_test.go` | covered |
| §9 | `close_pull_request` | `pkg/cli/outcome_eval_formal_test.go` | covered |
| §10 | `close_discussion` | not-started | not-started |
| §11 | `create_discussion` | not-started | not-started |
| §12 | `update_discussion` | `pkg/cli/outcome_eval_workflow_test.go` | covered |
| §13 | `create_pull_request_review_comment` | not-started | not-started |
| §14 | `submit_pull_request_review` | `pkg/cli/outcome_eval_test.go` | covered |
| §15 | `reply_to_pull_request_review_comment` | not-started | not-started |
| §16 | `resolve_pull_request_review_thread` | not-started | not-started |
| §17 | `push_to_pull_request_branch` | not-started | not-started |
| §18 | `mark_pull_request_as_ready_for_review` | not-started | not-started |
| §19 | `assign_to_agent` | not-started | not-started |
| §20 | `dispatch_workflow` | `pkg/cli/outcome_eval_workflow_test.go` | covered |
| §21 | `autofix_code_scanning_alert` | not-started | not-started |
| §22 | `create_code_scanning_alert` | not-started | not-started |
| §23 | `link_sub_issue` | not-started | not-started |
| §24 | `hide_comment` | not-started | not-started |
| §25 | `assign_milestone` | not-started | not-started |
| §26 | `update_project` | not-started | not-started |
| §27 | `update_release` | not-started | not-started |
| §28 | `noop` | `pkg/cli/outcome_eval_test.go` | covered |
| §29 | `missing_tool` | `pkg/cli/outcome_eval_test.go` | covered |
| §30 | `replace_label` | `pkg/cli/outcome_eval_update_test.go`, `pkg/workflow/replace_label_formal_test.go` | covered |

### Structure: Safe-Output Section-to-Implementation Mapping

Each numbered safe-output-type section above corresponds to a dedicated configuration/compilation implementation file under `pkg/workflow/`. This mapping is distinct from the Compliance test mapping above; it maps specification sections to the Go source that defines and compiles the output type (shared cross-cutting logic such as `safe_output_handlers.go` and `compiler_safe_outputs_job.go` is omitted for brevity since it applies to all types).

| Section | Output type | Implementation file(s) |
|---|---|---|
| §1 | `create_pull_request` | `pkg/workflow/create_pull_request.go` |
| §2 | `create_issue` | `pkg/workflow/create_issue.go` |
| §3 | `add_comment` | `pkg/workflow/add_comment.go` |
| §4 | `add_labels` | `pkg/workflow/add_labels.go` |
| §5 | `add_reviewer` | `pkg/workflow/add_reviewer.go` |
| §6 | `update_issue` | `pkg/workflow/update_issue.go` |
| §7 | `update_pull_request` | `pkg/workflow/update_pull_request.go` |
| §8 | `close_issue` | `pkg/workflow/close_entity_helpers.go` |
| §9 | `close_pull_request` | `pkg/workflow/close_entity_helpers.go` |
| §10 | `close_discussion` | `pkg/workflow/close_entity_helpers.go` |
| §11 | `create_discussion` | `pkg/workflow/create_discussion.go` |
| §12 | `update_discussion` | `pkg/workflow/update_discussion.go` |
| §13 | `create_pull_request_review_comment` | `pkg/workflow/create_pr_review_comment.go` |
| §14 | `submit_pull_request_review` | `pkg/workflow/submit_pr_review.go` |
| §15 | `reply_to_pull_request_review_comment` | `pkg/workflow/reply_to_pr_review_comment.go` |
| §16 | `resolve_pull_request_review_thread` | `pkg/workflow/resolve_pr_review_thread.go` |
| §17 | `push_to_pull_request_branch` | `pkg/workflow/push_to_pull_request_branch.go`, `pkg/workflow/push_to_pull_request_branch_validation.go` |
| §18 | `mark_pull_request_as_ready_for_review` | `pkg/workflow/mark_pull_request_as_ready_for_review.go` |
| §19 | `assign_to_agent` | `pkg/workflow/assign_to_agent.go` |
| §20 | `dispatch_workflow` | `pkg/workflow/dispatch_workflow.go`, `pkg/workflow/dispatch_workflow_validation.go`, `pkg/workflow/dispatch_workflow_file_resolver.go` |
| §21 | `autofix_code_scanning_alert` | `pkg/workflow/autofix_code_scanning_alert.go` |
| §22 | `create_code_scanning_alert` | `pkg/workflow/create_code_scanning_alert.go` |
| §23 | `link_sub_issue` | `pkg/workflow/link_sub_issue.go` |
| §24 | `hide_comment` | `pkg/workflow/hide_comment.go` |
| §25 | `assign_milestone` | `pkg/workflow/assign_milestone.go` |
| §26 | `update_project` | `pkg/workflow/update_project.go` |
| §27 | `update_release` | `pkg/workflow/update_release.go` |
| §28 | `noop` | `pkg/workflow/noop.go` |
| §29 | `missing_tool` | `pkg/workflow/missing_issue_reporting.go` |
| §30 | `replace_label` | `pkg/workflow/replace_label.go` |

Sync procedure: when a safe-output type's implementation file is renamed, split, or removed, update the corresponding row in this table in the same change that moves the code.

### OTel Backend Unavailability

When the OTLP exporter is unavailable (e.g., endpoint unreachable, network timeout, authentication failure) during outcome evaluation, the following safeguards **MUST** apply:

1. **Graceful degradation**: Outcome evaluation workers **MUST** complete their classification logic (determining `accepted`, `rejected`, `ignored`, etc.) regardless of OTLP exporter availability. The computed outcome **MUST** be persisted to a local audit fallback log (e.g., a NDJSON file at a known path such as `/tmp/gh-aw/outcome-audit.ndjson`) before any attempt to export to OTLP. If the OTLP export fails, the local audit log entry **MUST** still be written and **MUST NOT** be discarded. This ensures the outcome is recoverable even when the telemetry backend is down.

2. **Audit fallback and retry**: When OTLP export fails, the evaluation worker **SHOULD** schedule a retry using an exponential back-off strategy (initial delay: 5 seconds; maximum delay: 5 minutes; maximum attempts: 5). If all retries are exhausted without a successful export, the worker **MUST** record the export failure in the local audit log with a `export_failed: true` flag and the final error reason. A downstream reconciliation process **SHOULD** periodically sweep the local audit log and re-attempt export for any entries marked `export_failed: true`.

### Conformance Safeguard Coverage Requirements

Conformance suites **MUST** include explicit safeguard coverage classes in addition to happy-path outcome checks:

1. **Class A (state success/failure):** validates standard accepted/rejected/ignored/pending transitions from authoritative object state.
2. **Class B (human override/lifecycle):** validates human edits, deletions, reopen events, and lifecycle outcomes where applicable.
3. **Class C (API degradation):** validates `404`, `5xx`, timeout, and rate-limit behaviors, including retry metadata and non-terminal handling.

Every safe-output type **MUST** have at least one Class A test. Types that query GitHub APIs for evaluation **MUST** also include at least one Class C test case.

---

## Formal Model

The outcome evaluation engine is encoded as a state machine with invariants using TLA+, F* pre/post contracts, and Z3/SMT-LIB arithmetic bounds.

**State space** (`OutcomeState`):

```
OutcomeState ≜ [
  type       : SafeOutputType,
  result     : OutcomeResult,
  evalError  : String ∪ {nil},
  detail     : String,
  apiStatus  : Int ∪ {nil},
  actor      : ActorIdentity,
  checkTime  : Timestamp
]
```

**TLA+ invariants** (one per state-machine guarantee):

```tla
OutcomeDomain ≜
  ∀ s ∈ OutcomeState :
    s.result ∈ {"accepted","rejected","ignored","pending","lifecycle","lifecycle_close"}
    ∨ s.result ∈ {"unknown","error"}

APIFailureNeverTerminal ≜
  ∀ s ∈ OutcomeState :
    (s.apiStatus ∈ {500,502,503,429} ∨ s.apiStatus = 403 ∧ RateLimited(s)) ⟹
      s.result ≠ "accepted" ∧ s.result ≠ "rejected"

NotFoundClassification ≜
  ∀ s ∈ OutcomeState :
    s.apiStatus = 404 ∧ persistent(s.type) ⟹ s.result = "rejected" ∧
    s.apiStatus = 404 ∧ transient(s.type) ⟹ s.result = "ignored"

BotActorProvenance ≜
  ∀ actor : ActorIdentity :
    isBotActor(actor) ↔ HasSuffix(actor.login, "[bot]") ∨ actor.login ∈ KnownBotLogins

PRMergeAcceptance ≜
  ∀ pr : PullRequest :
    pr.merged = true                   ⟹ outcome = "accepted" ∧
    pr.state = "closed" ∧ ¬pr.merged  ⟹ outcome = "rejected" ∧
    pr.state = "open"                  ⟹ outcome = "pending"

IssueBotCloseLifecycle ≜
  ∀ issue : Issue :
    issue.state = "closed" ∧ issue.stateReason = "not_planned" ∧ closedByBot  ⟹ result = "lifecycle" ∧
    issue.state = "closed" ∧ issue.stateReason = "not_planned" ∧ ¬closedByBot ⟹ result = "rejected" ∧
    issue.state = "closed" ∧ issue.stateReason = "completed"                   ⟹ result = "accepted"

CloseStickyReopenRejection ≜
  ∀ item : close_issue ∪ close_pull_request :
    current.state = "closed" ⟹ result = "accepted" ∧
    current.state = "open"   ⟹ result = "rejected"

APIErrorNotTerminal ≜
  ∀ pr : PullRequest, err : APIError :
    fetch(pr) = err ⟹ outcome = "error"

ZeroTouchRequiresNoReviews ≜
  ∀ pr : PullRequest :
    pr.zeroTouch ⟹ pr.outcome = "accepted" ∧
      pr.humanComments = 0 ∧ pr.humanReviews = 0
```

**F* pre/post contracts** (selected):

```fstar
val evaluateWithAPIError :
  item:CreatedItemReport → err:APIError →
  Tot OutcomeReport
  (requires err.status ∈ {500, 502, 503, 429} ∨ RateLimited err)
  (ensures fun r → r.Result ≠ OutcomeAccepted ∧ r.Result ≠ OutcomeRejected)

val labelRetentionMonotonicity :
  before:list string → after:list string → current:list string →
  Tot retainedStateComparison
  (requires Subset before after)
  (ensures fun c →
    Subset after current ⟹ c.Retained ≠ [] ∧
    ¬Subset after current ⟹ c.Reverted ≠ [] ∨ c.Replaced ≠ [])

val compareUpdateSnapshot :
  before:state → after:state → current:state → fields:list string →
  Tot retainedStateComparison
  (ensures fun c →
    current = after  ⟹ c.Retained = c.Changed ∧
    current = before ⟹ c.Reverted = c.Changed ∧
    current ≠ before ∧ current ≠ after ⟹ c.Replaced = c.Changed)

val evaluateOutcome :
  item:CreatedItemReport → transportOK:bool →
  Tot OutcomeReport
  (requires True)
  (ensures fun r →
    r.Type ≠ "" ∧ r.Result ∈ KnownOutcomeResults ∧
    normalizeOutcomeEvaluation(r).OutcomeStatus ≠ "" ∧
    normalizeOutcomeEvaluation(r).EvidenceStrength ≠ "")
```

**Z3/SMT-LIB bounds** (derived metrics zero-safety):

```smt2
(declare-const accepted Int)
(declare-const rejected Int)
(declare-const total    Int)
(assert (>= accepted 0))
(assert (>= rejected 0))
(assert (>= total (+ accepted rejected)))
(assert (=> (> (+ accepted rejected) 0)
            (= acceptance_rate (/ accepted (+ accepted rejected)))))
(assert (=> (> total 0)
            (= waste_rate (/ rejected total))))
(assert (=> (= (+ accepted rejected) 0) (= acceptance_rate 0.0)))
(assert (=> (= total 0) (= waste_rate 0.0)))
(check-sat) ; sat — formulas are consistent and division-by-zero safe
```

---

## Behavioral Coverage Map

| Predicate / Invariant | Test Function | Description |
|---|---|---|
| `P1` OutcomeDomain | `TestFormalOutcomeDomainInvariant` | All OutcomeResult values are within the six defined strings |
| `P2` No-Terminal-Under-API-Failure | `TestFormalAPIFailurePending` | 5xx and rate-limit responses yield `pending`/`error`, never `accepted`/`rejected` |
| `P3` 404-Terminal-Classification | `TestFormal404Classification` | 404 on persistent object → `rejected`; on transient → `ignored` |
| `P4` Bot-Actor-Provenance | `TestFormalBotActorProvenance` | Bot identity → bot action; user identity → non-bot action |
| `P5` PR-Merge-Acceptance | `TestFormalPRMergeAcceptance` | merged=true→accepted; closed+!merged→rejected; open→pending |
| `P6` Issue-Bot-Close-Lifecycle | `TestFormalIssueBotCloseLifecycle` | Bot closes not_planned→lifecycle; human→rejected; completed→accepted |
| `P7` Label-Stickiness-Monotonicity | `TestFormalLabelStickiness` | All labels retained→accepted; any removal→rejected |
| `P8` Update-Snapshot-Comparison | `TestFormalUpdateSnapshotComparison` | current=after→accepted; current=before or diverged→rejected |
| `P9` CloseSticky-Reopen-Rejection | `TestFormalCloseStickyReopenRejection` | Reopened object→rejected; lifecycle bot closed→lifecycle_close |
| `P10` Derived-Metrics-Consistency | `TestFormalDerivedMetricsConsistency` | acceptance_rate and waste_rate formulas; division-by-zero safety |
| `P11` OTel-Graceful-Degradation | `TestFormalOTelGracefulDegradation` | OTLP failure still writes audit log; outcome not discarded |
| `P12` Conformance-Class-Coverage | `TestFormalConformanceClassCoverage` | Class A/C test existence invariant structure |
| `P14` API-Error-Not-Terminal | `TestFormalAPIErrorNotTerminal` | An authoritative PR fetch error produces `error`, never a terminal outcome |
| `P15` Zero-Touch-Requires-No-Reviews | `TestFormalZeroTouchRequiresNoReviews` | `zero_touch` requires zero non-bot comments and zero reviews |

`P13` covers the worker's configurable evaluation delay and is intentionally outside this in-process evaluator suite.

---

## Generated Test Suite

The 14 test functions above are implemented in
`pkg/cli/outcome_eval_formal_test.go` using the Go `testify` library.
All tests carry the `//go:build !integration` tag so they run in the default
unit-test suite without any special flags.

Each test function:

- maps to exactly one predicate/invariant in the coverage map above;
- calls production code directly (no stubs beyond the established `*GHAPIGet` variable pattern);
- uses `assert`/`require` calls whose failure messages quote the predicate identifier and the violated invariant clause;
- is independently runnable with `go test -run <TestFunctionName>`.

Run the full formal suite:

```sh
go test ./pkg/cli/ -run 'TestFormalOutcomeDomainInvariant|TestFormalAPIFailurePending|TestFormal404Classification|TestFormalBotActorProvenance|TestFormalPRMergeAcceptance|TestFormalIssueBotCloseLifecycle|TestFormalLabelStickiness|TestFormalUpdateSnapshotComparison|TestFormalCloseStickyReopenRejection|TestFormalDerivedMetricsConsistency|TestFormalOTelGracefulDegradation|TestFormalConformanceClassCoverage|TestFormalAPIErrorNotTerminal|TestFormalZeroTouchRequiresNoReviews' -v
```

### Formal Notation Cross-References

| Notation | Predicates | Purpose |
|---|---|---|
| TLA+ state-machine invariants | P1, P4, P5, P6, P9 | State transition correctness |
| F* pre/post contracts | P2, P3, P7, P8, P11, P12 | Function-level contracts |
| Z3/SMT-LIB arithmetic bounds | P10 | Division-by-zero safety for derived metrics |

---

## Change Log

### Version 1.0.1 (Working Draft, 2026-08-01)

- Clarified provenance-resolution rules for `human_*` metrics when GitHub Apps act on behalf of humans.
- Added a section-to-test cross-reference table for all 30 safe-output types, marking uncovered rows as `not-started`.
- See also: `specs/otel-observability-spec.md` §13 (Outcome Evaluation) and §19 (Change Log).
