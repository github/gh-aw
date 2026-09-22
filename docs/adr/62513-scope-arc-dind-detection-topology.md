# ADR-62513: Scope ARC/DinD detection topology to the detection runner

**Date**: 2026-09-22
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request changes how gh-aw propagates runner topology into the threat-detection job. The PR description and code diff show that the workflow-level `runner.topology: arc-dind` setting was being applied to a detection job that is normally pinned to GitHub-hosted `ubuntu-latest`, which caused ARC/DinD-specific code generation to rewrite paths under `${RUNNER_TEMP}/gh-aw`, leave `threat-detect` unstaged, and write detection output to a read-only location that later steps did not read. The design question is whether the detection job should inherit the agent job's ARC/DinD runner topology by default, or only when the detection job explicitly selects its own compatible runner.

### Decision

We will apply `runner.topology: arc-dind` to the threat-detection job only when `safe-outputs.threat-detection.runs-on` selects a non GitHub-hosted runner. An explicit GitHub-hosted label such as `ubuntu-latest` is treated like the default, because it provably cannot be the ARC runner the topology describes. gh-aw will otherwise treat the detection job as running on its default GitHub-hosted runner and will avoid ARC/DinD-specific path rewriting, tool-cache redirection, and AWF topology configuration there. We chose this because runner topology is a property of the runner executing the detection job, not a workflow-wide assumption that can safely be copied from the primary agent job.

### Alternatives Considered

#### Alternative 1: Keep propagating the workflow runner topology to detection unconditionally

This was the pre-change behavior and was the simplest implementation because detection reused the workflow's existing runner configuration. It was not chosen because the PR evidence shows that this contradicts the detection job's actual default `ubuntu-latest` runtime and produces broken ARC/DinD code generation, including missing binary staging and mismatched result paths.

#### Alternative 2: Fully support ARC/DinD detection on every topology path immediately

This was considered because some users may run detection on self-hosted ARC runners and would benefit from a complete end-to-end fix. It was not chosen in this PR because the immediate defect is incorrect topology propagation to a GitHub-hosted detection job, while broader ARC/DinD detection support still has unresolved staging and writable-mount issues that would require additional design and implementation work.

### Consequences

#### Positive
- Default threat-detection jobs on `ubuntu-latest` now generate paths and runtime setup that match the runner they actually use.
- Detection result producers and consumers now agree on the same writable output path in the default configuration.
- Regression tests now verify that ARC/DinD code generation is omitted for GitHub-hosted detection runners (default or explicit) and preserved only when the detection job declares a self-hosted runner.

#### Negative
- ARC/DinD behavior for detection is now conditional on the detection job's own `runs-on` resolving to a non GitHub-hosted runner, which adds another coupling that maintainers must understand when debugging runner-specific behavior.
- Detection jobs that explicitly run on ARC still retain previously known incomplete ARC/DinD support, so this PR narrows the bug rather than solving every related topology issue.
- Future runner-topology features will need explicit decisions about whether they apply to the agent job, the detection job, or both.

#### Neutral
- The implementation introduces helper functions to derive runner configuration specifically for the detection job.
- Documentation now states that the detection job defaults to `ubuntu-latest` and only inherits ARC/DinD topology when it declares a self-hosted runner override.
- Existing tests for ARC/DinD detection paths now set `ThreatDetection.RunsOn` explicitly to model the cases where topology inheritance is intended.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
