# ADR-60267: Validate Safe-Output Secrets Before Activation

**Date**: 2026-09-11
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The activation job already validates engine-specific credentials before starting an agentic workflow, but this pull request shows external safe-output handlers for Jira, Linear, and Azure DevOps could still fail later when their required secrets were missing. The PR adds compiler changes, shell-script messaging updates, generated workflow output changes, and tests across the activation and safe-output paths, indicating the goal is to fail early with actionable configuration errors instead of discovering missing credentials after agent execution. The implementation also preserves frontmatter and environment-based credential overrides, so the decision must account for both default secret injection and explicit user-supplied credentials. The architectural question is whether external safe-output credential validation belongs in activation-time workflow compilation/runtime setup or should remain deferred to the safe-output processing phase.

### Decision

We will validate required external safe-output credentials during activation, alongside existing engine secret checks, and surface the combined result through the activation job output. We decided to generate dedicated validation steps for Jira, Linear, and Azure DevOps safe outputs when those handlers are enabled and no explicit credential override or deployment environment is configured, and to inject the default Azure DevOps PAT secret into the safe-output processor when needed. This favors earlier, clearer workflow failure over deferring credential errors until after the agent has already run.

### Alternatives Considered

#### Alternative 1: Keep safe-output credential validation inside the safe-output processor only

The workflow could continue checking Jira, Linear, and Azure DevOps credentials only when the safe-output processor runs after agent execution. This was considered because it keeps credential knowledge localized to the safe-output stage and avoids adding more activation-job steps. It was not chosen because the PR evidence shows that delayed failure wastes agent runtime and produces less actionable feedback when required secrets are absent.

#### Alternative 2: Validate only engine credentials and document safe-output secret prerequisites

Another option would be to leave runtime behavior unchanged and rely on documentation or setup instructions to tell users which safe-output secrets must exist. This was considered because it avoids compiler changes and preserves a simpler activation pipeline. It was not chosen because the diff adds concrete validation logic, aggregated failure reporting, and tests that demonstrate missing safe-output credentials should be enforced automatically rather than left to manual configuration discipline.

#### Alternative 3: Require explicit frontmatter credentials for all external safe outputs

The system could force every Jira, Linear, and Azure DevOps safe-output configuration to declare its credential source in frontmatter and skip default secret lookup entirely. This was considered because it makes credential provenance explicit and reduces implicit defaults. It was not chosen because the PR preserves the existing default-secret behavior, adds override-aware checks, and specifically injects `${{ secrets.AZURE_DEVOPS_EXT_PAT }}` when no override is present, indicating the desired design is secure defaults with optional explicit overrides.

### Consequences

#### Positive
- Workflows fail before agent execution when enabled external safe outputs lack required credentials, reducing wasted runtime and faster feedback to maintainers.
- Validation errors now identify which component requires each missing secret, making setup issues easier to diagnose.
- Engine secret validation and safe-output secret validation share the same activation result path, giving conclusion logic one place to detect credential-guardrail failures.

#### Negative
- Activation job generation becomes more complex because it must synthesize multiple validation steps, unique step IDs, and aggregated failure expressions.
- Safe-output configuration logic is now more tightly coupled to activation-time compiler behavior, increasing maintenance cost when new handlers are added.
- Generated workflow output grows with additional validation steps and environment injection rules for external systems.

#### Neutral
- Explicit frontmatter credentials and environment-based credential resolution remain supported and suppress default validation where the PR deems those configurations authoritative.
- Azure DevOps safe outputs now receive `${{ secrets.AZURE_DEVOPS_EXT_PAT }}` by default unless `SYSTEM_ACCESSTOKEN` or `AZURE_DEVOPS_EXT_PAT` is already provided.
- Existing secret-validation helpers are reused with a generalized step-ID API rather than introducing a separate validation mechanism.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
