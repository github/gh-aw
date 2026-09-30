# ADR-64498: Honor node action overrides in secondary jobs

**Date**: 2026-09-30
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request changes how `gh-aw` emits `Setup Node.js` steps for threat-detection and evals jobs. The PR description shows that the main agent job already respects `runtimes.node.action-repo` and `runtimes.node.action-version`, but detection and evals jobs bypassed that path and continued to emit the default public `actions/setup-node` reference. That created a concrete failure mode for network-isolated self-hosted runners that depend on a wrapper setup action. The architectural question is whether these secondary jobs should independently keep their hardcoded setup action or resolve the same runtime override as the main job.

### Decision

We will make threat-detection and evals jobs honor the workflow's `runtimes.node` setup-action override using the same resolution rules as the main agent job. We will centralize that behavior in shared helpers that resolve the effective Node setup action and rewrite only the `uses:` line of `Setup Node.js` steps, leaving the rest of each step unchanged. We chose this because the PR evidence shows these jobs are part of the same workflow runtime contract, and emitting different Node setup actions across jobs breaks workflows that intentionally override Node setup for restricted runner environments.

### Alternatives Considered

#### Alternative 1: Keep hardcoded `actions/setup-node` in detection and evals jobs

This was realistic because the previous implementation already worked for public-network runners and required no extra helper logic. It was not chosen because the PR evidence shows it violates the existing `runtimes.node` contract by making secondary jobs behave differently from the main job, causing setup failures on self-hosted runners that rely on a custom wrapper action.

#### Alternative 2: Route detection and evals jobs through the full main-job runtime detection path

This was considered because reusing the entire existing setup pipeline could reduce special cases in how jobs obtain runtime configuration. It was not chosen because the PR implements a narrower change that preserves current job structure while applying only the missing action override, avoiding broader behavioral changes to jobs that intentionally bypass `DetectRuntimeRequirements`.

### Consequences

#### Positive
- Threat-detection, external detector, and evals jobs now use the same Node setup action override contract as the main agent job.
- Workflows on network-isolated or policy-constrained runners can supply a wrapper setup action once and have it applied consistently across secondary jobs.
- New tests cover both injected and preexisting `Setup Node.js` steps, reducing the chance of regressions in job-specific runtime setup.

#### Negative
- Node setup generation now includes extra helper logic for resolving overrides and selectively rewriting step content.
- Secondary jobs remain coupled to the naming and structure of `Setup Node.js` steps, so future refactors must preserve that contract or update the rewrite logic.

#### Neutral
- The change affects only the action reference used by `Setup Node.js`; it does not alter `runtimes.node.version`, which secondary jobs continue to source from their existing defaults.
- Existing workflows with no `runtimes.node` action override continue to emit the same setup steps as before.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
