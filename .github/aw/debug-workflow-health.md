---
description: Verify whether a pushed agentic workflow fix has matching existing-run evidence, without dispatching new runs.
disable-model-invocation: true
---

# Verify Workflow Health After a Fix

Use this route for "is it healthy now?", "did the patch work?", or "can we close
the investigation?" An older green run cannot verify a new patch. For historical
failure clustering, use [existing-run analysis](analyzing-runs.md) instead.
Follow the [shared strategy](debug-agentic-workflow.md); this is existing-evidence
verification, not authorization to dispatch or rerun a workflow.

## Collect Revision-Aware Health

Use `gh aw health --json` in a matching repository. For a named workflow or a
different checkout, retain the supplied workflow and explicitly scope the repository;
do not broaden to the fleet or infer another repository from the current checkout.

```bash
gh aw health WORKFLOW --repo HOST/OWNER/REPO --json
```

Collection defaults to a two-minute budget. Set `--timeout 30s` for a shorter
check or `--timeout 0` to disable the deadline. Exhaustion returns available
evidence with `collection.partial=true` and `reason=time_budget`; it does not
verify health. Use `DEBUG=cli:health` to trace request progress and timings on
stderr. Increase the budget or narrow the scope when more evidence is needed.

Use available health evidence when the CLI is unavailable; do not assume that
an MCP equivalent exists without discovering its actual schema.

Health inventories registered AW `failure` conclusions from run-list payloads before
any job or revision lookups, then verifies one job-backed representative per
branch/event. `collection.potential_issues` are approximate, list-only signals;
they may include jobless, historical, or superseded failures. Check each
`representative_status`; do not audit a `superseded` representative.
`failure_listing_complete` covers the cheap inventory; `failure_pass_complete`
covers representative verification. Counts survive budget exhaustion even if
verification is unfinished. The later chronological pass adds successful and
pending evidence, prioritizing recently failing workflows. A truncated sample is
not a fleet failure rate or proof that remaining workflows are healthy.
The inventory keeps up to five candidates per workflow and stops when every
registered workflow reaches that count or after ten pages. The page cap marks
collection partial while leaving chronological sampling available.
It samples backward per AW toward five completed first-attempt runs, bounded by
`--days` and API quota.

## Match the Source Actually Exercised

Read `collection` and `workflows[].revisions`: compare
`gh-aw-metadata` frontmatter/body hashes, not repository commits alone.
Health also compares the workflow Markdown source's blob SHA at each supported run
with the pushed branch source, even when the lock file has not been recompiled.
It reads committed Git objects from a matching local checkout first, never dirty
files or stale tracking refs. A pushed branch head is resolved once per collection;
missing local history or blobs fall back to GitHub without fetching or deepening
the checkout. Fleet API fallback reuses directory snapshots per ref and lock
metadata per blob SHA, rather than reading each file for each run.
It stops collecting older revisions of that branch/event when the source differs
and excludes those superseded failures from investigation. `source_sha` identifies
the pushed source and the previous source in revision cohorts. Wait for evidence
from the updated source instead of auditing failures it supersedes; a source edit
alone does not prove a fix. Unrelated repository changes do not stop sampling.
`recovery_observed` is evidence, not causation. A pushed revision without runs,
missing hashes, or a sparse/quota-truncated sample is not healthy. Health clusters
cover current-revision failures; use [logs](analyzing-runs.md#collect) for historical failures.

## Report the Verification Gap

State the target workflow/repository, source revision, matching inspected runs,
collection completeness, and remaining uncertainty. Separate patch delivery,
local checks, and existing-run recovery. If no run exercised the pushed source,
report "awaiting verification" rather than healthy; do not dispatch for evidence.
