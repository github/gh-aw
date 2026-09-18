# ADR-60997: Remove Effective Tokens in Favor of AI Credits

**Date**: 2026-09-15
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

gh-aw previously exposed Effective Tokens (ET) across runtime outputs, schemas, workflow contracts, environment variables, and documentation as a spend-tracking metric. This pull request replaces maintained ET surfaces with AI Credits (AIC), while explicitly preserving the migration blog as historical context and retaining provider cache semantics needed for AIC fallback pricing. The change spans runtime plumbing, generated workflows, schemas, fixtures, codemods, and documentation, which means downstream automation would otherwise continue to depend on a retired metric. The repository already contains AI Credits concepts, so the main architectural question is whether gh-aw should continue to carry ET as a first-class compatibility field or complete the migration to AIC on maintained surfaces.

### Decision

We will remove Effective Tokens from maintained gh-aw surfaces and standardize spend accounting on AI Credits. Runtime outputs, workflow/environment variable contracts, schemas, forecasting fixtures, and documentation will use AIC terminology and fields, while the migration blog remains as the historical record of the transition. We will keep only compatibility behavior that is still required to price cached provider activity for AIC fallback calculations, rather than preserving ET as a user-facing metric.

### Alternatives Considered

#### Alternative 1: Keep Effective Tokens as a legacy compatibility field alongside AI Credits

gh-aw could continue publishing both ET and AIC across outputs, schemas, and workflow contracts so existing consumers would not need to migrate immediately. This was considered because the repository still has historical ET references and dual publishing can smooth ecosystem upgrades. It was not chosen because it prolongs a retired metric, keeps schemas and generated workflows more complex, and invites drift between the legacy and canonical spend fields.

#### Alternative 2: Rename documentation and workflow labels to AI Credits, but leave ET runtime/schema plumbing in place

The project could update user-facing text while keeping the underlying ET fields, environment variables, and MCP token-delta plumbing intact. This was considered because it would reduce migration churn inside the implementation. It was not chosen because the PR evidence shows the retirement is broader than terminology alone: maintained APIs, contracts, and audit/log schemas are also being updated, so leaving ET plumbing in place would preserve hidden architectural debt and make the migration incomplete.

### Consequences

#### Positive
- gh-aw exposes one primary spend metric across runtime behavior, generated workflows, schemas, and documentation, reducing ambiguity for maintainers and integrators.
- Removing ET fields and plumbing simplifies audit/log contracts and lowers the chance that regenerated artifacts or downstream tools depend on stale metric names.
- Forecasting fixtures and workflow outputs align with the active cost model, making future budgeting and reporting features easier to extend.

#### Negative
- Existing consumers that still read ET fields, environment variables, or generated workflow outputs must migrate to AIC-based names and contracts.
- The change touches many generated and maintained surfaces at once, increasing review scope and the risk of incomplete cleanup if any ET reference is missed.
- Historical ADRs and specifications about ET become obsolete and must be retired or superseded carefully to avoid conflicting guidance.

#### Neutral
- The migration blog remains in the repository as historical context, so documentation will intentionally preserve some ET references outside maintained product surfaces.
- Provider cache semantics needed for AIC fallback pricing continue to exist even though ET is removed as a first-class external metric.
- Generated workflow artifacts will be recompiled to reflect the decision, but that regeneration does not change the underlying policy beyond the metric transition.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
