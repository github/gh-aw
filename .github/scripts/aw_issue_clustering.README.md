# AW essential issue clustering

The daily Copilot workflow in
[`aw-issue-clustering.md`](../workflows/aw-issue-clustering.md) maintains up to ten
assignable summary issues and one **AW Essential 10** discussion in `audits`.
Save the issue filter **`is:issue is:open label:aw-essential`** to view the queue.
The label is created automatically by the write-isolated publisher.
The manual dispatch input `staged` previews the proposed queue without mutating
issues, labels, or discussions; the daily schedule publishes normally.

## Evidence and ranking

The collector paginates the entire open backlog, not the latest 100 or a search
API's first 1,000 results. An input must have bot authorship and a gh-aw metadata
marker, or a legacy generated footer linking a run in this repository. Labels,
title prefixes, or a bot author alone are insufficient. Human issues, WIP issues,
group containers, and this workflow's own outputs cannot become cluster members.
New issues created after workflow-run start wait for the next pass.
Closed source issues are ignored for evidence, seeds, new assignments, and
deferrals regardless of their closure reason. Closed essential summaries are
read only to identify completed work for cleanup; they are not candidates.
Frozen assigned scopes may retain historical references to closed sources,
which do not count toward eligible backlog coverage.

AW discussions updated in the last 14 days, including closed reports and every
deep-report briefing, supplement the issue evidence. Older discussions explicitly
linked from open AW issues are also fetched. Full dossiers stay on disk; the
agent starts from excerpts, references, and 30 deterministic TF-IDF/medoid seeds.
Nonexistent linked discussions generate an explicit warning and cannot be cited;
authentication, rate-limit, or other collection errors fail the run.
Semantic synthesis may split or merge seeds, but every eligible issue must appear
once in an actionable cluster or an explained deferral.

Impact, confidence, and effort use 1-5 scales. Priority is
`impact * confidence * log2(1 + source_count) / effort`; key order breaks ties.
Repeated reports are corroboration, not additional affected issues. A shortfall
below ten requires a reason, rather than padded or unrelated assignments.

## Assignment lifecycle

Assign a summary to freeze its scope. Unassign it to allow daily refreshes.
Stable keys retain issue identity while members overlap; merges/splits retire only
unassigned summaries. Operator text outside the generated island, labels,
assignees, and comments are preserved. Summaries do not auto-expire.
At publication, provenance-verified sources closed after run start are removed
from unassigned candidates; empty candidates retire with an explicit shortfall.
Assigned scopes remain frozen. Summaries assigned or edited after run start use
their live trusted metadata, not the agent's stale replacement. Coverage and
duplicate-membership checks still apply after this reconciliation. This accounts
for expiry without claiming that an auto-closed source was actually fixed.

Closing a summary as **completed** suppresses its unchanged source findings and
queues them for trusted cleanup. Each run first performs this cleanup in a
write-isolated deterministic job, before collecting evidence or recomputing the
queue. It refetches each completed summary and linked source and closes the source
as completed only if its AW provenance and activity timestamp are unchanged.
The collector then reads the cleaned backlog. Cleanup failures stop recomputation;
the job summary lists the sources closed (or previewed in staged mode).
After validating the complete plan, the publisher repeats cleanup for summaries
completed during analysis.
Human issues, WIP/group containers, revived findings, and sources still linked
to open assigned summaries cannot be cleanup targets. A reopened or edited
summary cancels its pending cleanup. No comments, labels or bodies are rewritten
on source issues.

Newer source activity makes those findings eligible again. Workflow retirement
uses **not planned**, so reorganizing the queue does not mark its sources fixed
or close them. The collector tracks completed summaries and pending source
closures in `index.json`; cleanup-only runs submit an empty plan rather than
`noop`. Staged mode previews source closures without writing.
There are never more than ten open owned summaries; assigned work counts toward
that limit. The discussion provides the current rank even for frozen assignments. If GitHub
rejects editing its existing bot-authored body, the publisher posts the current
queue as a new comment on that discussion instead.

The publisher independently refetches GitHub evidence rather than trusting
agent-editable snapshot files. It validates the full plan before writing, checks
ownership and concurrent edits, retires superseded summaries before creating
replacements, and honors staged mode. GitHub does not provide a multi-resource
transaction: an API failure is surfaced, may leave fewer than ten summaries,
and the next successful pass reconciles the queue without creating an eleventh.

## JavaScript collection and validation

Collection and publication run with the injected `github`, `context`, and `core`
objects from `actions/github-script`. The collector and publisher share the same
provenance, pagination and plan validation code; no Python or subprocess GitHub
client is needed.

The agent writes the full plan to
`/tmp/gh-aw/agent/aw-issue-clustering/plan.json`, validates it locally, and calls
`publish_essential_issues` once with
`plan_path: "agent/aw-issue-clustering/plan.json"`. That declared artifact is
downloaded relative to `GH_AW_AGENT_OUTPUT`; the publisher accepts only this
fixed relative path and refetches trusted evidence before any writes. Plans are
not constrained by the tool's 10 KB string-input limit, so findings, frozen
metadata and individual deferral reasons must not be clipped to fit it.

## Local validation

```bash
node --test .github/scripts/test_aw_issue_clustering*.cjs
node .github/scripts/aw_issue_clustering_publish.cjs \
  --repo github/gh-aw \
  --corpus /tmp/gh-aw/agent/aw-issue-clustering/corpus.json \
  --plan /tmp/gh-aw/agent/aw-issue-clustering/plan.json
```

The last command validates only, using the evidence collected by the workflow.
Writes run in the `actions/github-script` safe-output job using
`.github/scripts/aw_issue_clustering_publish.cjs`. Collection needs issues,
discussions and workflow-run read access. No npm dependencies, embedding API,
or separate cache database are required.
