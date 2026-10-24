# ADR-71297: Require Valid Grype Output and Per-Image Scan Status

**Date**: 2026-10-23
**Status**: Proposed
**Deciders**: pelikhan, PR #71297 reviewers

---

### Context

`gh aw compile --grype` runs Grype (via Docker) against container images in lock-file manifests. In `pkg/cli/grype.go`, `grypeRunOnImage` parsed JSON only if stdout started with `{`, allowing empty output, non-JSON output, or output without a `matches` array to be cached and reported as a successful scan with zero findings. The `static-analysis-report` workflow checked only for the `Running grype` marker, which did not establish that every planned image scan completed. Investigation for issue #71044 confirmed this parsing and reporting reliability gap, but did not establish the cause of the historical Serena scan dropout. Missing findings or lower counts alone do not establish CVE remediation.

### Decision

The CLI will accept a scan result only when stdout decodes as a JSON object with a non-null `matches` array and the process exits with code 0 or 1; `matches: []` remains valid, while invalid-output errors are cached rather than converted into zero findings. To make coverage machine-checkable, the CLI will log each planned image reference before scanning and emit `Grype scan completed for <ref>: N vulnerability finding(s)` or `Grype scan failed for <ref>: <error>`, using a pinned reference when available and the image tag otherwise. The `static-analysis-report` workflow will compare planned and completed occurrence counts for each reference, reject any failed scan, and require an explicit zero-container marker when there are no plans. Reporting integrity is the driver: zero findings requires successful completion evidence, not merely absent output, while non-strict compilation may still continue after scanner errors.

### Alternatives Considered

#### Alternative 1: Keep lenient parsing and add only reporting caveats

We could keep the existing parse logic, where non-`{` output is ignored, and only tell the reporting agent to be careful. This change would be smaller and could not break existing compiles. We rejected it because the CLI would still cache and return zero-finding results for broken scans. That undermines every later consumer, not just one workflow.

#### Alternative 2: Emit a structured JSON status artifact instead of log lines

The CLI could write a per-image status file in JSON, and the workflow could validate it. That would avoid depending on the presentation of stderr messages, but would require a new output file and plumbing in both the compile command and the workflow. This change instead reuses the existing `compile-output.txt` capture and tool-marker approach, with a contract test for the workflow gate. A structured artifact remains an alternative, not a committed follow-up.

#### Alternative 3: Fail compilation on any Grype error, regardless of strict mode

Any invalid scan could stop compilation. This would prevent downstream consumers from overlooking scanner failures, but would change the existing non-strict behavior that allows compilation to continue. The change preserves that behavior and adds a completeness gate specifically to the reporting pipeline.

### Consequences

#### Positive
- Empty or malformed output, and missing or null `matches`, are cached as errors rather than clean scan results.
- Planned image references and completed or failed statuses make coverage auditable, including cached results, and distinguish "unknown/incomplete" from "zero findings".
- The completeness gate is tested by running the script itself (`grype_workflow_contract_test.go`), and the parser is tested across exit codes 0, 1, and 2.

#### Negative
- Grype or Docker output that used to pass silently now fails. In strict mode it also fails compilation, and in the static-analysis-report workflow it fails the job. This may surface new CI failures.
- The workflow gate depends on the wording and formatting of stderr status messages, creating a contract between `grype.go` and the Python regexes in `static-analysis-report.md`. Changes to either side require corresponding contract-test updates.
- The contract test parses workflow markdown frontmatter and rewrites a hard-coded log path, which couples Go tests to the workflow file layout.

#### Neutral
- Existing exit-code semantics are kept: exit 1 is accepted with valid output because Grype uses it for findings, while other non-zero exit codes are errors.
- The change does not explain past Serena scan dropouts and does not claim that any CVEs were fixed.
- The validation checks the expected output structure, not whether Grype detected every vulnerability or whether an image is vulnerability-free.
- The generated `static-analysis-report.lock.yml` must stay aligned with the workflow source when the gate changes.

---

*Proposed for review in PR #71297; acceptance remains with the deciders.*
