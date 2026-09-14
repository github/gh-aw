# Operational Value Metric Patterns

Use these patterns as reasoning examples, not fixed metrics. The workflow Markdown remains authoritative.

For every pattern, adopt the workflow and evaluator together before the first scored run. Keep the evaluator unchanged while workflow intent and acceptance criteria are unchanged. When those semantics change, replace the evaluator at the same path in the same commit; Git history and the archived digest retain the old pair. Corrections are prospective and never rewrite prior results. Keep the metric ID unless the measured outcome itself changes.

## Threshold maintenance

Example goal: inspect a repository, identify the largest eligible file, and request refactoring when it exceeds a declared threshold.

- Unit: one repository scan.
- Independent evidence: eligible files and line counts from the checkout being graded.
- Expected decision: request work for the deterministically selected file, otherwise explicitly noop.
- Primary metric: `large-file-triage-decision`.
- Function: `1` when the requested issue/noop matches the independently computed decision; `0` when it contradicts that decision; `null` when the checkout or output request is unavailable.
- Avoid: waiting for the file to shrink, querying whether an issue was later created, or scoring repository health itself.

## Event-driven routing

Example goal: respond to a failed deployment by requesting one incident issue unless a matching incident is already open.

- Unit: one failure event.
- Independent evidence: event fields plus one narrowly scoped lookup for the deduplication condition when it is not already materialized.
- Expected decision: request an incident or explicitly noop because a matching incident exists.
- Primary metric: `deployment-incident-routing`.
- Function: `1` for the correct branch with required event facts in the request; `0` for the wrong branch or missing required content; `null` when event or deduplication evidence is unavailable.
- Avoid: claiming the issue was created before safe-output application or claiming the root-cause hypothesis is true without independent evidence.

## Classification and triage

Example goal: label eligible issues under a fixed policy.

- Unit: one issue or one explicitly bounded issue batch.
- Independent evidence: issue state at the workflow snapshot and deterministic policy constraints from the Markdown.
- Primary metric when semantics are fully specified: `correct-issue-triage`.
- Primary metric when semantics require human judgment: `eligible-issue-triage-policy-conformance`.
- Function: score independently verifiable eligibility, coverage, allowed labels, and required explanation. Do not call a label semantically correct unless a deterministic rule or independent truth establishes it.
- Avoid: dividing self-approved labels by labels the same agent proposed.

## Audit and reporting

Example goal: summarize grader results or workflow health accurately.

- Unit: one report over one fixed input snapshot.
- Independent evidence: the complete source dataset and the requested report content.
- Primary metric: `grader-audit-accuracy` or another domain-specific accuracy name.
- Function: independently recompute required totals and sections, then score exact agreement or the proportion of required facts represented correctly.
- Avoid: using the health of the audited systems as the workflow score. A report can be fully valuable while accurately reporting widespread failure.

## Release preparation

Example goal: publish or update a release for a requested semantic version change.

- Unit: one release request.
- Independent evidence: requested bump, current version, changelog or changesets, required build results, and local dependency pins.
- Primary metric: `release-request-valid` when grading precedes publication; use `release-published` only when publication is already proven.
- Function: `1` when the independently derived version and required release content match the requested action; `0` for a contradictory or incomplete request; `null` when required build or version evidence is unavailable.
- Avoid: looking up a release that may have been created by another actor or assuming an update request was applied.

## Proposed code change

Example goal: simplify, refactor, lint, or repair code and request a pull request.

- Unit: the explicitly selected file, finding, or bounded sample for this run.
- Independent evidence: original source, requested patch, scope constraints, and results from every required validation command.
- Primary metric: name the exact proven property, such as `yamllint-fix-request-valid` or `selected-tests-parallelized-safely`.
- Function: compare the requested patch with deterministic requirements and require the specified checks to pass; return `0` for a contradictory patch or failed required check and `null` when source, patch, or validation evidence is unavailable.
- Avoid: scoring lines changed, warnings mentioned, tasks attempted, subjective readability, repository-wide improvement from a sample, merge, or freedom from regressions beyond the checks performed.

## Research and recommendations

Example goal: surface repository-relevant research opportunities.

- Unit: one bounded research scan.
- Independent evidence: source identity, extracted claim, repository gap, and a concrete proposed action.
- Primary metric: `research-opportunity-evidence-completeness` when relevance cannot be independently judged.
- Function: score only explicit, verifiable acceptance criteria from the Markdown, such as valid citation, supported claim, identified repository surface, deduplication, and actionable next step.
- Avoid: treating a model's relevance score, confidence, ranking, or number of papers as operational value.

## Expert review

Example goal: review a change for security or code-quality risks.

- Unit: one pull request review.
- Independent evidence: pull request diff plus deterministic rules stated by the workflow.
- Primary metric: use a narrow name such as `security-review-policy-conformance` unless independent ground truth supports correctness or recall.
- Function: score coverage of required surfaces, grounding to changed lines, policy-rule matches, deduplication, and valid requested review actions.
- Avoid: claiming vulnerability correctness, severity accuracy, precision, or recall from the review's own findings.

## Smoke test or external integration

Example goal: prove that an engine, credential, permission, or remote integration works in a declared mode.

- Unit: one declared capability test for the current target and mode.
- Independent evidence: required invocation, expected response or side effect, and explicit failure status.
- Primary metric: `mcp-authentication-success`, `engine-smoke-test-passed`, or another exact capability.
- Function: usually binary; `1` when every required assertion passes and `0` when the capability under test rejects, times out, or returns the wrong result. Use `null` only when evidence unrelated to the tested capability is unavailable.
- Avoid: measuring broad product quality, treating a partial mode set as full coverage, or assuming a requested cross-repository mutation was applied.

## User-targeted or subjective content

Example goal: summarize a supplied resource, produce creative content, or identify usability concerns.

- Unit: the exact user-supplied target and requested mode.
- Independent evidence: target identity plus objective structure, source citation, and content constraints stated by the workflow.
- Primary metric: a narrow claim such as `resource-summary-structure-complete` or `requested-theme-constraints-met`.
- Function: score only deterministic requirements and target binding. Return `null` when source material is unavailable.
- Avoid: claiming accuracy, usefulness, delight, readability, or creativity without an independent rubric and ground truth. If no objective requirement exists, report that meaningful deterministic operational value is unavailable.

## Bounded mutation set

Example goal: close, relabel, update, or consolidate a bounded set of repository objects.

- Unit: the selected set after applying the workflow's declared eligibility rules and cap to one complete snapshot.
- Independent evidence: source snapshot and full requested mutation set.
- Primary metric: `eligible-mutation-set-correct` or a domain-specific equivalent.
- Function: compare expected and observed sets, including target and action. Score full agreement as `1`; use a proportion only when partial completion is explicitly valuable; score unjustified destructive actions as `0`.
- Avoid: counting only attempted items, penalizing objects outside an intentional cap, or ignoring extra destructive mutations.

## Performance benchmark

Example goal: detect a regression against a declared performance threshold.

- Unit: one benchmark run under the workflow's declared environment, inputs, and repetitions.
- Independent evidence: complete benchmark output plus the explicit threshold and comparison rule from the workflow or referenced policy.
- Primary metric: `performance-regression-decision-correct`.
- Function: independently apply the declared rule and compare it with the requested alert/noop; return `null` when measurements are incomplete or the required environment is not established.
- Avoid: inventing a tolerance, treating natural variance as improvement, mutating a baseline during grading, or claiming long-term performance from one sample.

## External inventory

Example goal: report the current models, service capabilities, prices, or deprecations exposed by declared providers.

- Unit: one inventory over the exact provider set declared for this run.
- Independent evidence: complete captured provider responses or narrow authoritative API reads with required fields and snapshot time.
- Primary metric: `declared-provider-inventory-accuracy`.
- Function: compare every required provider record and report field against the fixed response snapshot; return `null` for unavailable, truncated, rate-limited, or incompatible source data.
- Avoid: treating design-time web research or search results as runtime truth, silently dropping a failed provider, or scoring the desirability of the inventory's contents.

## Experiment or orchestration

Example goal: run a declared agent variant or request a downstream workflow with validated inputs.

- Unit: the current variant execution or dispatch request, bound to its declared target and mode.
- Independent evidence: fixed current-run inputs, variant-independent acceptance rules, and the complete requested dispatch payload.
- Primary metric: `variant-acceptance-criteria-met` or `downstream-dispatch-request-valid`.
- Function: apply the same deterministic acceptance function to each variant, or compare the requested dispatch target and inputs with the independently expected request.
- Avoid: calling one variant better without a complete fixed comparison dataset, grading another run's future effects, or treating successful delegation as completion of the delegated task.

## Common decision rule

For every pattern, compute the expected action independently from input evidence, then compare it with the workflow's requested action:

```text
expected = decide(event, current-run inputs, repository snapshot)
observed = parse(workflow output request)
value = compare(expected, observed)
```

If `expected` cannot be computed because required evidence is missing or incomplete, return `null`. If it can be computed and `observed` is absent or contradictory, return `0`.
