---
name: Auditor
description: Hourly, budgeted health discovery and targeted audits of newly observed current-revision failures.
intent: Reduce recurring agentic workflow failures without duplicating work already under review.
on:
  schedule: hourly
permissions:
  contents: read
  actions: read
  pull-requests: read
  copilot-requests: write
concurrency:
  group: auditor-${{ github.repository }}
  cancel-in-progress: false
timeout-minutes: 40
network:
  allowed: [defaults, go, node]
tools:
  bash: ["*"]
  edit:
  github:
    mode: gh-proxy
    toolsets: [pull_requests]
safe-outputs:
  create-pull-request:
    title-prefix: "[auditor] "
    draft: true
    max: 1
    max-patch-files: 30
    allowed-files:
      - "pkg/**"
      - "cmd/**"
      - "internal/**"
      - "actions/**"
      - "scripts/**"
      - "schemas/**"
      - ".github/workflows/**/*.md"
    excluded-files:
      - "**/*.lock.yml"
      - "scripts/auditor-collection.cjs"
    github-token-for-extra-empty-commit: none
    protected-files: request-review
if: github.event_name == 'schedule' && needs.collect.outputs.has_new_failures == 'true'
jobs:
  collect:
    name: Collect failure evidence
    if: github.event_name == 'schedule'
    runs-on: ubuntu-latest
    timeout-minutes: 20
    permissions:
      contents: read
      actions: read
      pull-requests: read
    outputs:
      has_new_failures: ${{ steps.collect.outputs.has_new_failures }}
    steps:
      - name: Checkout trusted workflow source
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - name: Install gh-aw CLI
        uses: ./actions/setup-cli
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
      - name: Restore health observations and audit cache
        uses: actions/cache/restore@55cc8345863c7cc4c66a329aec7e433d2d1c52a9 # v6.1.0
        with:
          path: ${{ runner.temp }}/auditor-cache
          key: auditor-health-v2-${{ github.repository_id }}-${{ github.ref_name }}-${{ github.run_id }}-${{ github.run_attempt }}
          restore-keys: |
            auditor-health-v2-${{ github.repository_id }}-${{ github.ref_name }}-
      - name: Collect revision-aware health and bounded audits
        id: collect
        uses: actions/github-script@3a2844b7e9c422d3c10d287c895573f7108da1b3 # v9.0.0
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          CACHE_DIR: ${{ runner.temp }}/auditor-cache
          DATA_DIR: ${{ runner.temp }}/auditor-data
        with:
          retries: 0
          script: |
            await require('./scripts/auditor-collection.cjs')({github, context, core});
      - name: Upload compact collection
        uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2
        with:
          name: auditor-collection
          path: ${{ runner.temp }}/auditor-data
          if-no-files-found: error
          retention-days: 2
      - name: Save successful health observations and audit cache
        uses: actions/cache/save@55cc8345863c7cc4c66a329aec7e433d2d1c52a9 # v6.1.0
        with:
          path: ${{ runner.temp }}/auditor-cache
          key: auditor-health-v2-${{ github.repository_id }}-${{ github.ref_name }}-${{ github.run_id }}-${{ github.run_attempt }}
steps:
  - name: Download this run's compact collection
    uses: actions/download-artifact@9000827ccba6bdab643e8b6fd33ac0654aef8333 # v8.0.2
    with:
      name: auditor-collection
      path: /tmp/gh-aw/auditor
  - name: Setup Go for focused regression tests
    uses: actions/setup-go@v7.0.0
    with:
      go-version-file: go.mod
      cache: true
  - name: Setup Node.js for focused regression tests
    uses: actions/setup-node@v7.1.0
    with:
      node-version: "24"
---

# Auditor

Read `/tmp/gh-aw/auditor/manifest.json` and its `health_file` first. Stop with `noop`
if either is missing, invalid or incomplete, there are no **new** audited failures,
or the open-PR listing is incomplete. Collection failure is never an empty result.
Health is a bounded current-revision sample, not an exhaustive completion census.
Do not interpret complete collection as complete fleet coverage or proof of health.

## Evidence and selection

The trusted `collect` job alone runs `gh aw health --json` over the preceding
24 hours, reuses compatible cached audit evidence, and feeds at most three new
`failure_investigation.selected_runs` into `gh aw audit --stdin --no-baseline`.
Each audit receives one run ID, preventing accidental multi-run diff output.
Read the manifest's relative audit reports before inspecting source. Do not
rescan health, rerun logs, download all artifacts, broaden the window, or perform
further remote audits. Paths embedded in health/audit reports belong to the
collection runner; only manifest-relative files were transferred.

Health excludes superseded revisions, lifecycle-resolved runs and intentional
failures using its existing algorithm. Potential issues and metadata-only
signals are unknown until their selected audit supplies evidence. Cluster counts
refer to sampled unique runs, overlap, and must not be summed. A shared symptom
is not a proven shared root cause.

Rank unresolved health clusters by sampled unique affected-run count, breaking
ties with first failing boundaries before aftermath. Compare typed job/step,
normalized error, failing component, engine/model, revision and configuration.
Exclude this Auditor, obsolete attempts, already-fixed default-branch behavior
and self-recovered failures. Do not invent a fix from metadata alone.

Inspect clusters in ranked order, first checking PR coverage. Select the largest
unresolved cluster whose supplied audit proves an actionable root cause. If the
largest unresolved cluster has no selected audit or insufficient evidence,
`noop` with that limitation rather than fixing a smaller convenient cluster.
`remaining_targets` and health's `remaining_clusters` are explicit audit-coverage
gaps, not evidence that unselected runs are healthy. Never retry, rerun or dispatch
workflows, upload live tests, change credentials, or weaken security gates.

## Deduplicate against every open PR

Read **all** `open_prs` in the manifest, including drafts, human PRs and PRs from
other automations; do not filter by author, prefix, label or workflow marker.
Compare titles and bodies semantically with the diagnosed cause and proposed
repair. For plausible matches, inspect the PR diff through `gh` (at most five
PRs); if ambiguity remains, stop without creating a PR.

Derive the publication fingerprint only after confirming the root cause:
`auditor-root-cause:v1:<sha256>`, using a lowercase, whitespace-normalized tuple
of repository, defective source component/path, violated invariant and repair
mechanism. Do not hash run IDs, dates, head SHAs, transient paths, raw error
messages, or query-local cluster IDs. Include the canonical tuple and
`<!-- auditor-root-cause:v1:<sha256> -->` in the PR body. Match exact fingerprints
AND semantically equivalent fixes without markers. If covered, skip that cluster
and consider the next largest unresolved cluster within the audit budget.

Immediately before publication refresh the entire open-PR listing with
`gh api repos/${{ github.repository }}/pulls?state=open&per_page=100&page=N`,
with the endpoint quoted, one page at a time, using response Link headers.
Stop after ten pages; if `rel="next"` remains, any request fails, identities
repeat, or a possible semantic match cannot be ruled out, call `noop`.
Reapply both fingerprint and semantic dedup to this fresh complete listing.
Do not mutate, close, merge, comment on or push to existing PRs. Concurrency
serializes this auditor, but cannot prevent an external author racing publication.

## Make one focused fix

Read `AGENTS.md`, `CONTRIBUTING.md` and `.github/aw/instructions.md`. Inspect the
defective source at the failed revision (using supplied evidence and bounded
read-only GitHub queries when unavailable locally), then confirm the same
defect exists in this checkout. Never fetch/pull/push authenticated git remotes:
checkout credentials are deliberately not persisted.

Fix the confirmed cause, not cascades. Add a focused regression test and follow
repository format/lint/build/test gates. Do not modify upstream-managed `source:`
workflows locally; report their upstream fix requirement with `noop`. Do not
add dependencies, unrelated cleanup, instruction changes, or generated-file-only
fixes. If workflow Markdown changes, compile and run `make recompile`, inspect
the generated result and discard unrelated generation drift. The native
`excluded-files: ["**/*.lock.yml"]` removes **all** lock YAML from PR output;
explain any required maintainer recompilation of source workflow changes in the
PR. Do not bypass the exclusion or commit a generated lock under another name.

Create at most one draft PR via `create_pull_request` only after validation and
fresh dedup pass. Use `### Summary`, `### Root cause`, `### Validation`, and
`### Coverage and limitations`. Include the stable fingerprint/tuple, source
cluster IDs, sampled unique affected count, collection window, remaining targets
and clusters, up to three linked run references, and actual test results.
Separate observations from inference and local validation from unperformed live
checks. If no safe, novel, evidenced fix is available, call `noop`.

## Usage and collection limits

Both collection and agent jobs require the scheduled event; manual dispatch
cannot activate this Auditor. Discovery uses health's revision-aware metadata
algorithm, its 1,000-request cap and a two-minute deadline, rather than paging
every run since a creation-time cursor. Failure inventory or representative-pass
incompleteness fails closed, preserving the successful observation checkpoint.

The v2 cache namespace explicitly replaces v1's incremental creation census.
Its checkpoint records successful sampled collection; it is not a completeness
watermark for all runs. Attempt/update-aware identities of audited selections
are retained for 24 hours. Only successfully audited targets become seen; excess
new selections remain eligible next hour. Cache loss can cause reinspection, but
all-author semantic PR dedup is still required. No completeness claim is made
for unfinished, unselected, out-of-window or superseded runs.

At most five health targets are proposed and three new targets are audited per
invocation, with a two-minute timeout per audit. PR listing is capped at ten
pages. Collection requires 350 remaining API requests initially and preserves
200 before and after audits in the same rate window. Partial health/audit output,
missing identities, changed attempts, invalid state or incomplete PR listing
never advances the checkpoint or activates the agent. This replaces the former
150-request logs-query allowance with health's explicit discovery budget and
bounded audit/time/reserve limits; it does not claim an absolute shared request cap.
