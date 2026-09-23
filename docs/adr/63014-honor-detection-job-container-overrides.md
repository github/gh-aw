# ADR-63014: Honor detection job container overrides

**Date**: 2026-09-23
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

Threat-detection jobs are derived from the parent workflow configuration, but they previously dropped `sandbox.agent.images` and `container_pins` while building the detection-specific workflow data. The PR description and diff show that this caused detection jobs to fall back to the default `ghcr.io` AWF registry even when the main agent workflow had been configured to use mirrored or private container images. This created inconsistent runtime behavior between the main workflow and threat-detection jobs and could break repositories that require private-registry routing or pinned mirrored images. The change is focused on how detection-job workflow data inherits container-related configuration from the parent workflow.

### Decision

We will propagate `sandbox.agent.images` and `container_pins` into threat-detection workflow data and clone both maps before assigning them to the derived detection configuration. When `container_pins` redirects a required default AWF image, the detection job will translate the mappings into a complete `container.images` runtime manifest. Threat-detection jobs will therefore use the same image-role overrides and repository container-pin mappings as the parent workflow during both pre-pull and runtime execution. We chose this because the PR evidence shows the architectural intent is configuration consistency across workflow variants, while cloning prevents the derived detection configuration from mutating the parent workflow state.

### Alternatives Considered

#### Alternative 1: Keep threat-detection jobs on the default AWF registry

This preserves the previous behavior and avoids carrying more configuration into derived detection workflow data. It was not chosen because the PR shows that defaulting to `ghcr.io` breaks expected parity with the parent workflow and ignores explicit private-registry or mirrored-image configuration already supplied by repository authors.

#### Alternative 2: Propagate overrides by sharing the parent maps directly

This would let detection jobs see the same image and pin data with less copying logic. It was not chosen because the tests in this PR explicitly validate that detection workflow data must not alias the parent maps; sharing references would make later mutation in derived workflow construction able to leak back into the parent configuration.

### Consequences

#### Positive
- Threat-detection jobs now honor the same mirrored or private container image configuration as the primary workflow jobs.
- Detection pre-pull steps use mapped container references instead of silently falling back to the default AWF registry.
- Detection runtime configuration uses mapped references instead of starting default-registry images.
- Cloning the image and pin mappings preserves isolation between parent workflow data and derived detection workflow data.

#### Negative
- The threat-detection workflow builder now has extra configuration-copying logic that must stay synchronized with future sandbox configuration changes.
- Additional tests and cloning introduce a small maintenance cost whenever container override behavior evolves.

#### Neutral
- The change does not introduce a new registry-selection feature; it extends existing override mechanisms into detection jobs.
- Runtime behavior only changes for workflows that already set `sandbox.agent.images` or `container_pins`; workflows using defaults continue to behave the same.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
