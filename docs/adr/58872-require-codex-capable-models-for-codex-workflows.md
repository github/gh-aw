# ADR-58872: Require Codex-capable models for Codex workflows

**Date**: 2026-09-05
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

Codex workflows in `gh-aw` can be configured to use GitHub-hosted `copilot/*` models, but the PR evidence states that general-purpose Copilot models can fail at runtime with unsupported-model or tool errors when Codex features such as custom tools are required. This change updates many workflow definitions and compiled lock files from `copilot/mai-code-1-flash-picker` to `copilot/gpt-5.3-codex`, and the changeset and PR description both say the compiler should warn when a non-Codex model is selected for a Codex workflow. The repository therefore needs an explicit architectural rule for how Codex workflows choose GitHub inference models. The decision must preserve GitHub inference routing while steering authors toward models that satisfy Codex capability requirements.

### Decision

We will allow `engine: codex` workflows to keep using GitHub-hosted `copilot/*` models, but require those selections to be Codex-capable when the workflow depends on Codex features. We will standardize affected workflows on `copilot/gpt-5.3-codex` and add compiler guidance that warns when a general-purpose Copilot model is configured for a Codex workflow, while skipping warnings for runtime expressions and known compatible models. This makes model capability compatibility an explicit workflow-authoring rule instead of a runtime surprise.

### Alternatives Considered

#### Alternative 1: Keep general-purpose Copilot models such as `copilot/mai-code-1-flash-picker`

This was the previous configuration across many workflows and keeps model selection broad. It was considered because those models are already integrated through GitHub inference. It was not chosen because the PR description explicitly says those models can fail at runtime for Codex-required capabilities such as custom tools, creating avoidable workflow breakage.

#### Alternative 2: Disallow all `copilot/*` models for `engine: codex` workflows

A stricter option would be to require only non-Copilot model identifiers for Codex workflows and reject GitHub inference model routing entirely. This would avoid ambiguity about compatibility. It was not chosen because the changeset says `engine: codex` workflows should continue to route `copilot/*` models through GitHub using Codex BYOK support, so the problem is capability selection, not the GitHub inference path itself.

#### Alternative 3: Keep current model choices and rely only on documentation

The project could document that some Copilot models may be incompatible without changing defaults or compiler behavior. This would minimize implementation work. It was not chosen because the PR demonstrates a repository-wide migration plus compiler guidance, indicating that passive documentation alone is not enough to prevent repeated runtime failures.

### Consequences

#### Positive
- Codex workflows are more likely to run successfully because their configured models now match Codex feature requirements.
- Workflow authors receive earlier feedback from compiler warnings instead of discovering incompatibilities only at runtime.
- GitHub inference support for `copilot/*` models is preserved while making compatibility expectations explicit.

#### Negative
- The repository now carries an architectural constraint that model selection for Codex workflows is narrower than generic Copilot model usage.
- Compiler logic must maintain a compatibility allowlist or equivalent guidance behavior for Codex-capable models.
- Bulk workflow migrations and recompilation updates increase maintenance churn when preferred Codex-capable models change.

#### Neutral
- Existing workflow markdown and generated `.lock.yml` files will continue to be updated together whenever the preferred Codex-capable model changes.
- Runtime expressions remain exempt from static warnings, so some compatibility checks still occur indirectly rather than being fully enforced at compile time.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
