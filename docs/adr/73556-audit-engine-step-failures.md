# ADR-73556: Audit Engine Step Failures

**Date**: 2026-10-31
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

`gh aw audit` primarily infers workflow health from run and job conclusions, but compiler-emitted evals judge and threat-detection engine steps run with `continue-on-error: true`. When one of those steps fails during a pre-flight check, GitHub's jobs API can still report the step and job as successful, leaving a green run indistinguishable from a run whose engine actually completed. The downloaded job logs and evals execution artifacts contain the missing evidence, including non-zero process exits, health-check errors, and all-`UNKNOWN` results. The audit must expose this failure without changing the compiler's `continue-on-error` behavior, the existing generic `errors` section, or non-audit `gh aw logs` behavior.

### Decision

We will add a dedicated `engine_step_failed` audit finding for compiler-emitted engine and judge steps in the `evals` and `detection` jobs. The audit will identify these steps through shared constants, inspect cached job metadata and downloaded logs within each step's timestamp window, and use evals execution evidence to detect a judge that never started; it will report the first health-check error or non-zero process-exit error regardless of the overall run conclusion. We will also add an `evals` summary to structured and console audit output, reuse the existing eval-result path discovery, version the audit cache and schemas, and document the new finding in the debug playbook.

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
- Shared step-name constants and compiled-lock tests keep the audit selector aligned with compiler-emitted workflow steps.

#### Negative
- Audit logic must correlate jobs API timestamps with flat or nested log layouts and can miss a failure when logs or execution artifacts are unavailable or malformed.
- The audit JSON contract and cache version change, requiring schema updates and regeneration of stale cached audit data for downstream consumers.
- The additional evidence parsing increases maintenance and test surface in the CLI, including edge cases where neighboring steps log within the same second.

#### Neutral
- The `continue-on-error` settings, compiler step outcomes, existing `threat_detection_job_failed` and `workflow_succeeded` logic, and the generic `errors` section remain unchanged.
- The new finding is produced only for the compiler-emitted evals and detection engine/judge steps; ordinary agent engine steps and unrelated continue-on-error steps remain out of scope.
- `gh aw logs` without `--audit` remains unchanged, while `gh aw logs --audit` benefits from the shared audit data and cache versioning.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
