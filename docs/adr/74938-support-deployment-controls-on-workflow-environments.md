# ADR-74938: Support Deployment Controls on Workflow Environments

**Date**: 2026-11-04
**Status**: Draft
**Deciders**: gh-aw maintainers (review pending)

---

### Context

GitHub Actions environments can be used to provide secrets and protection rules without creating a deployment when their `deployment` field is set to `false`. gh-aw currently accepts environment objects only with `name` and `url`, so workflows cannot express this secrets-only use case and schema validation fails before compilation. Agentic workflows that use environments to gate write credentials therefore create deployment records even when nothing is being deployed, producing repository and pull-request noise. The compiler already passes environment YAML through, so the change must preserve the complete environment object across top-level, safe-output, and custom-job inheritance and overrides.

### Decision

We will extend the object form of top-level `environment`, `safe-outputs.environment`, and `jobs.<id>.environment` to accept an optional `deployment` value that is either a boolean or a complete GitHub Actions expression. The compiler will continue passing the complete environment object through unchanged, while schema and compilation tests will cover `false`, `true`, expressions, omitted values, inheritance, and overrides. We will document `deployment: false` for secrets-only environments and leave the CLI `--environment` override as a literal name that replaces the entire environment block.

### Alternatives Considered

#### Alternative 1: Continue creating deployments for secrets-only environments

This preserves the existing schema and compiler behavior, but it does not solve the reported use case. It leaves deployment records and failed or outdated deployment indicators for workflows whose environments only protect credentials.

#### Alternative 2: Add a gh-aw-specific setting separate from the GitHub Actions environment object

A separate setting could control deployment creation, but it would introduce a second configuration model and require compiler-specific translation. Using GitHub Actions' native `deployment` field is more interoperable, keeps the emitted environment faithful to the source, and avoids adding another override or precedence rule.

### Consequences

#### Positive

- Workflows can retain environment secrets and protection rules without creating deployment records when `deployment: false` is used.
- Boolean and expression-valued deployment controls work consistently for top-level environments, safe-output environments, and custom jobs.
- Complete-object compilation tests protect environment metadata during inheritance and overrides.

#### Negative

- The schema must be maintained in three environment locations, increasing the surface area for future schema changes.
- Users must understand that the CLI `--environment` override replaces the entire environment object and can discard settings such as `deployment`.
- Existing generated workflows and documentation need to remain compatible with omitted `deployment` values.

#### Neutral

- Omitting `deployment` preserves GitHub Actions' existing default deployment behavior.
- The compiler pass-through remains unchanged; most implementation work is schema validation, tests, and documentation rather than a new runtime abstraction.
- This decision describes configuration support and propagation; whether a particular GitHub Actions run creates a deployment remains governed by GitHub Actions semantics.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
