# ADR-60682: Simplify Operational-Value Grading to One-Shot Evaluators

**Date**: 2026-09-14
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request removes the existing historical replay, maturation, baseline, cache, registry, and report machinery from gh-aw's operational-value grading flow and replaces it with a deterministic per-run evaluator contract. The implementation rewrites the operational-value designer guidance, changes evaluator verification scripts, updates tests, and introduces fixture-based validation for file-backed evaluators. It also updates the Daily File Diet operational-value evaluator to grade the current run's requested issue-or-noop decision against repository state at the run SHA. The architectural question is whether operational value in gh-aw should be modeled as historical attainment over time or as a frozen one-shot evaluation of the strongest effect observable at grading time.

### Decision

We will simplify operational-value grading to one-shot evaluators that read the current run request, inspect only evidence available at grading time, and emit an ordered metric array without replay, baselines, or retrospective contract metadata modes. We decided to freeze each workflow's evaluator bytes and SHA-256 digest with the compiled workflow so workflow-specific semantics live in the evaluator itself, while generic runtime infrastructure remains minimal. This was chosen because the PR evidence consistently shifts responsibility from centralized historical machinery to deterministic workflow-specific evaluators and explicitly treats requested safe outputs as evidence of requested actions rather than applied mutations.

### Alternatives Considered

#### Alternative 1: Preserve the existing historical replay and baseline framework

gh-aw could continue using evaluators with definition, metric, and grade-run modes plus historical reports, maturity windows, opportunity keys, and baseline comparisons. This was considered because it supports longitudinal analysis and comparable post-adoption reporting across runs. It was not chosen because the PR removes that machinery across the designer skill, verifier scripts, and Daily File Diet grader in favor of a smaller contract focused on what one run can prove at grading time.

#### Alternative 2: Keep generic runtime infrastructure and add one-shot evaluators on top

Another option would be to retain the existing replay-oriented schema and infrastructure while adding a parallel path for simpler evaluators. This was considered because it would preserve backwards compatibility for older workflows while enabling new workflows to opt into a lighter model. It was not chosen because the PR rewrites the core guidance around a single minimal evaluator form, updates verification to require only ordered metric arrays, and removes contract concepts such as baselines and maturation rather than making them optional.

#### Alternative 3: Measure operational value primarily by applied GitHub mutations

The project could define operational value only after requested issues, comments, or other mutations are actually applied downstream. This was considered because applied mutations are stronger evidence than requested actions. It was not chosen because the PR explicitly notes that grading occurs before the safe-output job applies requests and therefore the strongest attributable effect for many workflows is the verifiable requested action, not a future applied mutation.

### Consequences

#### Positive
- Operational-value evaluators become smaller and easier to reason about because they grade a single run from currently available evidence.
- Workflow-specific semantics move into frozen evaluator code and fixtures, reducing generic infrastructure and hindsight tuning risk.
- The contract more clearly separates requested safe outputs from applied mutations, which makes grading claims more honest about what was actually observed.

#### Negative
- gh-aw loses built-in historical replay, maturation, and baseline mechanisms that previously supported cross-run reporting in the generic infrastructure.
- Each workflow evaluator must now encode more domain-specific decision logic directly, which can increase authoring burden and duplication.
- Some long-term outcomes can no longer be measured directly and must be represented by weaker but observable precursor effects at grading time.

#### Neutral
- Evaluators now output an ordered metric array with the first metric treated as primary and later metrics as diagnostics.
- File-backed evaluators gain explicit semantic fixture files, while inline evaluators must carry equivalent coverage in workflow tests.
- Existing workflows adopting this model need paired workflow-and-evaluator updates validated by the new contract-change script.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
