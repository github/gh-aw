# ADR-70997: Enforce Detection Gates for Custom Workflow Jobs in the Compiled Job Graph

**Date**: 2026-10-22
**Status**: Accepted
**Deciders**: pelikhan, Copilot

---

### Context

Agentic workflows can declare custom top-level `jobs` alongside the compiler-owned jobs (`activation`, `agent`, `detection`, `safe_outputs`, and so on). Threat detection runs after `agent` and decides whether the agent's output is safe to use. Before this change, a custom job could use agent results before detection finished. It could also skip a failed detection with a status-override condition such as `if: always()` or `failure()`, or sit between `agent` and `detection` in the graph. Warn-mode detection makes this harder: the `detection` job can succeed while its `detection_success` output reports unsafe results, so checking only the job result is not enough. Compiler threat specification CTR-005 (Unsafe Output Route) did not yet cover custom jobs.

### Decision

Every custom job must run either before `agent` or after a successful, clean `detection` when detection is enabled. The compiler enforces this on the resolved job graph, after typed output-derived dependencies have been lowered. The `guardCustomJobDetection` pass builds a transitive `JobDependencyGraph` from the compiled jobs and does three things:
- rejects custom jobs that are unordered relative to `agent` or that sit between `agent` and `detection`
- adds `detection` directly to the `needs` of every downstream custom job, including reusable workflow calls
- wraps the job's `if` in an outer guard that cannot be overridden: `needs.detection.result == 'success' && needs.detection.outputs.detection_success == 'true' && (<authored condition>)`

An authored downstream custom job may set `detection-gate: status-only` to report job results after detection fails, is skipped, or reports warn-mode findings. The compiler still adds `detection` to `needs` to preserve ordering, but keeps the authored condition without the outer guard. This opt-out is accepted only when the job cannot consume agent-derived data: it cannot download artifacts, call an opaque reusable workflow, reference outputs except those of detection-gated compiler-owned jobs such as `safe_outputs`, or use `GH_AW_AGENT_OUTPUT` or agent/safe-output file paths in environment values or scripts. A job using this option must also be a leaf with respect to other custom jobs.

The `custom-job-detection` lint rule provides companion diagnostics on the Actions and AW ASTs with transitive dependencies, including missing detection dependencies and conditions that bypass detection job success. It is not a substitute for the compiler's clean-output guard. Workflows with no generated detection job, including workflows that explicitly disable detection, are exempt. The contract is recorded in CTR-005 of compiler threat specification 1.0.66.

### Alternatives Considered

#### Alternative 1: Lint-only enforcement (authors must add the gate themselves)

Keep `custom-job-detection` as a lint rule that reports missing `needs: detection` or unguarded status functions, without changing the compiled output. This keeps author intent visible in the compiled YAML and avoids rewriting conditions. This option was rejected because linting authored input does not see all resolved imports or compiler-derived dependencies, and lint can be omitted. The boundary must hold in emitted workflows without requiring a separate lint invocation.

#### Alternative 2: Gate only on detection job status (`needs.detection.result == 'success'`)

Rely on the job result, or on Actions' implicit success gate, without checking the `detection_success` output. This produces a simpler guard. This option was rejected because warn-mode findings leave the `detection` job successful while its output marks the results unsafe. Downstream jobs would still receive flagged output.

#### Alternative 3: Enforce on the authored (pre-lowering) dependency graph

Validate and inject guards when custom jobs are first parsed. This option was rejected because the compiler later adds dependencies from typed output references. Checking the authored graph could miss jobs that only become downstream of `agent` after lowering. It could also misclassify jobs that sit between `agent` and `detection`. Enforcement after dependency resolution uses the graph that will actually execute.

### Consequences

#### Positive
- Downstream custom jobs, including reusable workflow calls and jobs using `always()`, `failure()`, or `cancelled()`, can no longer use agent output after detection fails, is cancelled or skipped, or reports warn-mode findings.
- The compiler and the lint rule share a transitive dependency model (`aw.JobDependencyGraph`), and CTR-005 records the contract in the threat specification with mapped tests.
- Problem graphs, such as unordered custom jobs or jobs between `agent` and `detection`, fail at compile time with a clear `jobs.<id>.needs` error.

#### Negative
- Unmarked authors cannot run post-agent custom jobs on detection failure, cancellation, skipping, or warn-mode findings, for example to send failure notifications or clean up. The `detection-gate: status-only` exception is deliberately limited to jobs that cannot consume agent-derived data.
- The compiled conditions are longer and partly redundant, for example `needs.detection.result == 'success'` appears both in the guard and in the authored condition. This made lock files across many workflows (smoke, release, and others) churn.
- This can break existing workflows: custom jobs that are neither before `agent` nor after `detection`, or that `detection` depends on while also depending on `agent`, now fail to compile. Authored `if` values must be a single valid expression, so interpolated text inside them is rejected.

#### Neutral
- Compiler-owned jobs and `safe-outputs.jobs` are exempt from this pass because they already get compiler-generated guards.
- Workflows that explicitly disable detection are exempt, so whether the gate applies depends on detection being enabled.
- The reference docs (`steps-jobs.md`, `workflow-linters.md`) and the threat specification changelog were updated to describe the contract.
