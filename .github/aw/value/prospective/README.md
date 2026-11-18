# Independent outcome collection, v1

Enroll a future cohort by setting the repository variable `VALUE_COHORT_START` to its first UTC date (`YYYY-MM-DD`) on the default branch. For Issue Triage, supply the independently collected inputs below. Freeze this protocol before inspecting the cohort's outcomes. Do not update the ten historical pilot verdicts. There is no timestamp check intended to certify outcome-blind design.

The read-only **Collect operational value evidence** CI workflow measures one UTC cohort per workflow per day. Include scheduled and manual source runs on the repository's own default branch, irrespective of conclusion, delivery or acceptance. The unit is a source run, not a successful output. The first endpoint is the run's actual head SHA and creation time; follow-up is the last first-parent default-branch commit at or before creation + 7 days (Dead Code Remover) or + 14 days (CLI Performance). Measure immutable Git inputs after the full UTC day matures, on the same runner. These are run-relative observations, **not adoption-relative or causal estimates**.

| Workflow | Independent check |
| --- | --- |
| Dead Code Remover | Pin Go 1.27.4 and `deadcode` v0.52.0. Require working native build/vet, WASM build, and main packages in **both** `./cmd/...` and `./internal/tools/...`. Count only nongenerated, private top-level ASCII functions reported unreachable whose name appears exactly once across non-test Go source, including alternate-platform sources. Exclude protected helpers and files with embed or linkname directives. Retain package-qualified candidate IDs at both SHAs. The measured population is the **baseline set**; follow-up counts its intersection with the new set. New candidates are retained separately, not silently substituted. Loss of eligibility can mean removal, renaming or new use; this is not proof of safe deletion. A successful empty inventory is observed zero; a broken analyzer is not. |
| CLI Performance | Use the same error-checking `BenchmarkCompileSimpleWorkflow` fixture at both SHAs. Run five `ns/op` replicates at each endpoint with one CPU and fixed benchtime, on the same machine and Go toolchain; retain raw samples, fixture digest and runner context. Changed fixtures, failed compilation and missing source identity are not performance gains. The median is descriptive, not a significance test. |
| Issue Triage | Before any label is applied, independently capture the complete eligible issue IDs (unlabeled, unassigned, accessible under the workflow's integrity policy), source references and original baseline labels. Have a human review the pre-action issue information **without seeing the workflow's label, comment or subsequent history**. Separately observe applied labels for the same IDs at the frozen follow-up. Unreviewed, withheld, partial or ambiguous evidence cannot be treated as an incorrect classification or zero. Do not manufacture human labels from the workflow. |

Archive the artifact before expiry. Use the definition, dated recipe and observation bundle from the same artifact:

```bash
gh aw value dead-code-remover --repo github/gh-aw \
  --value-function artifact/definition.json --recipe artifact/recipe.json \
  --evidence artifact/observations.json --logs-dir .github/aw/logs --json
```

Keep the `provenance.json` alongside the report. An incomplete collection is not a zero-delivery or zero-value day. Numbers from overlapping cohorts are not additive. Typed evidence never establishes native adoption or PR attribution.

The default-branch collector is off until enrollment. It waits until the whole source day and follow-up cutoff have passed. A day with more than 100 listed runs, a truncated listing, or a conflicting source identity fails rather than selecting favorable runs. A measurement failure produces an unavailable observation and a failed job with the complete evidence artifact when possible. A missed scheduled day is a gap; the `cohort_day` input can select that same mature day without changing its endpoints. Archive the collector revision with its artifact; version changes apply to new cohorts. The paired endpoints do not certify continuous sampling or post-adoption durability.

## Human review for Issue Triage

This path cannot be automated from a `label` event. An authorized collector must seal a complete pre-action cohort and retain the original issue material visible under the workflow's integrity policy. Do not fetch withheld content or select only issues the agent touched. Give the reviewer **only** that pre-action material. Preserve the review separately from the independently reconstructed follow-up; authenticate the evidence producers outside the report. The following are synthetic input shapes, not historical reviews:

```json
{"repository":"github.com/github/gh-aw","workflow":"issue-triage-agent","run_id":42,"run_attempt":1,"at":"2026-09-01T10:00:00Z","observed_at":"2026-09-01T10:00:00Z","complete":true,"reference":"archive:pre-action-packet","eligible_issues":[{"number":123,"labels":[],"assignees":[]}]}
```

```json
{"cohort_sha256":"SHA256_OF_EXACT_COHORT_FILE","reviewed_at":"2026-09-02T10:00:00Z","complete":true,"blind_human_review":true,"reference":"archive:sealed-human-review","classifications":{"123":"bug"}}
```

```json
{"cohort_sha256":"SHA256_OF_EXACT_COHORT_FILE","at":"2026-09-15T10:00:00Z","observed_at":"2026-09-15T10:01:00Z","complete":true,"reference":"archive:labels-at-followup","labels":{"123":["bug"]}}
```

Use the SHA-256 of `cohort.json` in both downstream files. The projector does not read tokens, query issues, solicit or simulate a review. `complete` and `blind_human_review` are producer assertions, not authentication. Missing or partial reviews must not be filled in by the workflow.

```bash
python3 scripts/value_evidence/triage.py cohort.json review.json followup.json > observations.json
gh aw value issue-triage-agent --repo github/gh-aw --recipe frozen-review.json --evidence observations.json --json
```
