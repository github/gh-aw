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
assignees, and comments are preserved. Source issues are never mutated or closed.
Summaries do not auto-expire.
At publication, provenance-verified sources closed after run start are removed
from unassigned candidates; empty candidates retire with an explicit shortfall.
Assigned scopes remain frozen. This accounts for expiry without claiming that an
auto-closed source was actually fixed.

Closing a summary as **completed** suppresses its unchanged source findings.
Newer source activity makes those findings eligible again. Workflow retirement
uses **not planned**, so reorganizing the queue does not mark its sources fixed.
There are never more than ten open owned summaries; assigned work counts toward
that limit. The discussion provides the current rank even for frozen assignments.

The publisher independently refetches GitHub evidence rather than trusting
agent-editable snapshot files. It validates the full plan before writing, checks
ownership and concurrent edits, retires superseded summaries before creating
replacements, and honors staged mode. GitHub does not provide a multi-resource
transaction: an API failure is surfaced, may leave fewer than ten summaries,
and the next successful pass reconciles the queue without creating an eleventh.

## Local validation

```bash
python3 -m unittest discover -s .github/scripts -p 'test_aw_issue_clustering.py'
python3 .github/scripts/aw_issue_clustering.py \
  --repo github/gh-aw --output /tmp/gh-aw/agent/aw-issue-clustering
python3 .github/scripts/aw_issue_clustering_publish.py \
  --repo github/gh-aw \
  --corpus /tmp/gh-aw/agent/aw-issue-clustering/corpus.json \
  --plan /tmp/gh-aw/agent/aw-issue-clustering/plan.json
```

The last command validates only; writes require the safe-output job's
`--agent-output` mode. Collection requires an authenticated `gh` CLI with issues,
discussions, and (in Actions) workflow-run read access. No Python dependencies,
embedding API, or separate cache database are required.
