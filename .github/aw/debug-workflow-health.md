---
description: Verify whether a pushed agentic workflow fix has matching existing-run evidence, without dispatching new runs.
disable-model-invocation: true
---

# Check Current Workflow Health

Use this route for fleet health checks, "did the patch work?", or "can we close
the investigation?" An older green run cannot verify a new patch. For historical
failure clustering, use [existing-run analysis](analyzing-runs.md) instead.
Follow the [shared strategy](debug-agentic-workflow.md); this is existing-evidence
verification, not authorization to dispatch or rerun a workflow.

## Collect Revision-Aware Health

Use `gh aw health --fleet --json` against the identified repository. For fleet triage,
start with the last 24 hours unless the user supplied another window. Save the
full report privately and project a compact summary before reading individual
workflows. For a named workflow, pass its name or path and retain that scope.
If collecting from a source build, substitute the verified binary path from
the [bootstrap](debug-bootstrap.md#2-check-the-cli-and-run-identity).

```bash
health_dir=$(mktemp -d "$TMPDIR/gh-aw-health.XXXXXX") &&
  gh aw health --fleet --repo HOST/OWNER/REPO --start-date=-24h --timeout 2m \
    --checkpoint "$health_dir/checkpoint.json" --json
```

For a named workflow, use `gh aw health WORKFLOW --repo HOST/OWNER/REPO --json`
and inspect its own revision evidence; the fleet projection above is not its
output schema.

Fleet mode reports historical default-branch outcomes separately from sampled
current-revision statuses. On `coverage.partial=true`, report `coverage.reason`
and the uncovered scope. `not_collected` means unassessed;
`no_current_revision_runs` means no matching run was found, not that it passed.
If `next_action.type` is `resume`, reuse `--fleet --checkpoint PATH` and the same
repository without date flags. A resume preserves the original time window and
branch revision. Use `--full` only when per-workflow evidence is needed.
Report other collection limits rather than blindly retrying. Use
`DEBUG=cli:health` for request timings on stderr.

Use available health evidence when the CLI is unavailable; do not assume that
an MCP equivalent exists without discovering its actual schema.

Without `--fleet`, health scans the default branch before PR and other branches.
Check `collection.default_branch` and `default_branch_pass_complete`; PR failures
may reflect unmerged code rather than a default-branch regression. Fleet
`status_counts` without `--fleet` are not default-branch-only; check the representative run's
branch before attributing a failure to the default branch. Within each
branch pass, health inventories registered AW `failure` conclusions from run-list payloads before
any job or revision lookups, then verifies one job-backed representative per
branch/event. `collection.potential_issues` are approximate, list-only signals;
they may include jobless, historical, or superseded failures. Check each
`representative_status`; do not audit a `superseded` representative.
`failure_listing_complete` covers the cheap inventory; `failure_pass_complete`
covers representative verification. Counts survive budget exhaustion even if
verification is unfinished. The later chronological pass adds successful and
pending evidence, prioritizing recently failing workflows. A truncated sample is
not a fleet failure rate or proof that remaining workflows are healthy.
The inventory keeps up to five candidates per workflow across both passes,
prioritizing default-branch failures, and stops when every
registered workflow reaches that count or after ten pages shared across passes. The page cap marks
collection partial while leaving chronological sampling available.
It samples backward per AW toward five completed first-attempt runs, bounded by
`--days` and API quota.

## Match the Source Actually Exercised

Read `scope` and `details.workflows[].revisions` in full fleet output, or
`collection` and `workflows[].revisions` in the legacy report: compare
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
