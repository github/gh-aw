# ADR-73556: Audit Engine Step Failures

**Date**: 2026-10-31
**Status**: Proposed
**Deciders**: the gh-aw maintainers

---

### Context

`gh aw audit` primarily infers workflow health from run and job conclusions, but compiler-emitted evals judge and threat-detection engine steps run with `continue-on-error: true`. When one of those steps fails during a pre-flight check, GitHub's jobs API can still report the step and job as successful, leaving a green run indistinguishable from a run whose engine actually completed. The downloaded job logs and evals execution artifacts contain the missing evidence, including non-zero process exits, health-check errors, and all-`UNKNOWN` results. The audit must expose this failure without changing the compiler's `continue-on-error` behavior, the existing generic `errors` section, or non-audit `gh aw logs` behavior.

### Decision

We will add a dedicated `engine_step_failed` audit finding for compiler-emitted engine and judge steps in the `evals` and `detection` jobs, reported regardless of the overall run conclusion. The audit identifies these steps through shared constants and inspects the cached jobs API steps together with the downloaded job logs:

- **Step log window.** Nested per-step logs are read whole. Flat per-job logs are limited to the step's `started_at`..`completed_at` window and narrowed to the step's own `##[group]Run` header. Because the jobs API reports whole seconds, the header is chosen by the step's position among the jobs API steps that start in the same second, not by "last header in the start second", so a successor that starts in the same second does not take over the engine step's window.
- **Failure signal.** A step fails when its window contains a non-zero `Process completed with exit code` line. The finding quotes the first AWF health-check error, else the first `##[error]` line.
- **Sanitized error line.** The log-derived line passes through `stringutil.SanitizeErrorMessage` and additional redaction of token values, bearer credentials and URL userinfo, and control characters are replaced, before truncation. Only the redacted line reaches the finding, the cached `audit.json` and the console.
- **Evals execution evidence per invocation.** A judge that never started (`not_started`) is reported even with a clean step log. Reusable-workflow invocations upload separate `<hash>-usage` and `<hash>-evals` artifacts with the same run ID and attempt, so the evidence is matched to each evals job through the artifact prefix logged by its upload step. Without that prefix, the state is used only when every matching artifact agrees; otherwise it is unavailable.
- **Job log attribution.** A job's logs are the downloaded log files or directories named exactly after its full job name (with `/` written as `_`). A reusable-workflow job (`<caller> / <job>`) falls back to logs named after its final segment only when no exact match exists and exactly one such log exists; ambiguous logs are treated as unavailable. Both the step-log reader and the upload-prefix lookup use this rule.
- **Evals summary.** Structured and console audit output gain an `evals` section (questions, YES, NO, UNKNOWN, execution state). Results are collected per evals invocation (`evals.jsonl` at the run root or in the `evals`/`usage` artifacts, and in their `<hash>-` prefixed variants), using one copy per invocation, and the section sums the invocations. Each file is streamed line by line with a per-line limit and no total-size limit, because `evals.jsonl` restored from the evals state branch is an append-only history that also holds earlier runs' rows; only records whose `runid` matches the audited run are counted. A read error makes the summary unavailable rather than summarizing a partial file. An evals failure finding quotes only its own invocation's answers.

We will also version the audit cache and schemas, and document the new finding in the debug playbook.

### Alternatives Considered

#### Alternative 1: Change compiler-generated workflow behavior

The compiler could remove `continue-on-error` or persist each step's `steps.<id>.outcome` into artifacts so audit could read an authoritative outcome. This was considered because it would make failure detection direct, but it would change the behavior of every compiled workflow and lock file, could interfere with detection/evals cleanup and conclusion steps, and is outside this reporting-only fix.

#### Alternative 2: Fetch check-run annotations instead of inspecting downloaded logs

Audit could request annotations for the evals and detection jobs and treat failure-level annotations as the signal. This was considered because annotations contain `##[error]` messages, but it adds API calls and still would not provide the preferred health-check context or work when the existing downloaded logs already contain the necessary evidence. It also does not address the evals `not_started` execution state or provide the requested evals summary.

#### Alternative 3: Broaden generic audit error extraction to all successful runs

The existing error extractor could be changed to scan green runs and emit generic step failures. This was considered because it would reuse existing code, but it would alter the `errors` section for successful runs, would not distinguish compiler-emitted engine steps from unrelated steps, and would lose the specific job, step, execution-state, and first-health-check semantics needed by this problem.

### Consequences

#### Positive
- Green jobs no longer hide failed evals or threat-detection engine startup and execution steps when the downloaded evidence proves a non-zero exit.
- Findings name the affected job and step and preserve the most useful first diagnostic line, making proxy or engine startup failures actionable.
- Evals output distinguishes YES, NO, and UNKNOWN answers and exposes `not_started`, so an unexecuted judge is not presented as a healthy result.
- Log-derived text is redacted before it is persisted or printed; the evals summary counts only the audited run's answers, and execution evidence is attributed per reusable-workflow invocation.
- Shared step-name constants and compiled-lock tests keep the audit selector aligned with compiler-emitted workflow steps.

#### Negative
- Audit logic must correlate jobs API timestamps with flat or nested log layouts and can miss a failure when logs or execution artifacts are unavailable or malformed, or when execution evidence from several reusable-workflow invocations cannot be attributed and disagrees.
- The audit JSON contract and cache version change, requiring schema updates and regeneration of stale cached audit data for downstream consumers.
- The additional evidence parsing increases maintenance and test surface in the CLI, including edge cases where neighboring steps log within the same second.
- Redaction can hide part of a legitimate diagnostic, such as a snake_case identifier or URL credentials, in the reported error line.

#### Neutral
- The `continue-on-error` settings, compiler step outcomes, existing `threat_detection_job_failed` and `workflow_succeeded` logic, and the generic `errors` section remain unchanged.
- The new finding is produced only for the compiler-emitted evals and detection engine/judge steps; ordinary agent engine steps and unrelated continue-on-error steps remain out of scope.
- `gh aw logs` without `--audit` remains unchanged, while `gh aw logs --audit` benefits from the shared audit data and cache versioning.

---

*ADR reviewed and completed by @copilot from the generated draft.*
