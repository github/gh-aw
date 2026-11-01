# ADR-73829: Configurable Engine Release-Age Cooldown

**Date**: 2026-11-01
**Status**: Draft
**Deciders**: Unknown

---

### Context

Compiler-managed agent engine installations currently resolve npm packages and native Copilot CLI releases through several host, microVM, cache, and guest paths. The PR introduces a release-age policy so newly published packages and binaries are not activated immediately, while retaining existing version and checksum controls. The policy must cover built-in and behavior-defined engines, imported and inline engine configurations, Copilot toolcache entries, and Copilot musl downloads without applying to externally managed commands or installers. The PR validation notes also identify an unresolved bypass in which older npm versions selected by supported Node overrides may silently ignore `NPM_CONFIG_MIN_RELEASE_AGE`; enforcement must therefore fail closed or work on those versions before this decision can be accepted.

### Decision

We will expose one integer `engine.cooldown` setting for compiler-managed engine installs, defaulting to three release-age days. The compiler will validate and propagate that value to npm through `NPM_CONFIG_MIN_RELEASE_AGE`, including microVM staging, and to the Copilot installer, which will verify GitHub release publication metadata before activating cached binaries or downloading archives. Explicit zero is allowed only outside strict mode; invalid values and unavailable or invalid Copilot metadata will reject installation, while checksum verification and version pins remain independent requirements.

### Alternatives Considered

#### Alternative 1: Keep per-engine boolean cooldown metadata

Each engine definition could continue deciding independently whether its installer receives the existing boolean cooldown flag. This would preserve the current local behavior, but it would not provide a user-configurable age, would make defaults inconsistent across built-in engines, and would not naturally cover native Copilot releases or every microVM path.

#### Alternative 2: Enforce the policy only through npm

The compiler could set `NPM_CONFIG_MIN_RELEASE_AGE` for npm-based engines and leave native installers unchanged. This is simpler for npm packages, but it cannot protect Copilot CLI archives or cached Copilot binaries, so a recently published native release could bypass the intended policy.

#### Alternative 3: Rely only on version and checksum pins

The project could require immutable version and checksum pins without adding a release-age requirement. Pins improve integrity and reproducibility, but they do not create a stabilization window for newly published releases and do not address the operational risk this PR is designed to mitigate.

### Consequences

#### Positive
- Compiler-managed npm and native Copilot installations receive one explicit, reviewable release-age policy with a safe three-day default.
- The policy applies consistently across host installs, microVM staging, toolcache activation, pinned versions, and runtime version expressions while preserving checksum verification and version pins.
- Invalid configuration and missing or malformed Copilot release metadata can fail closed instead of silently bypassing the cooldown.

#### Negative
- The default policy intentionally delays access to newly published engine releases and can make installation dependent on GitHub release metadata availability for Copilot.
- Compiler, shell installer, schema, documentation, golden files, and regression tests must remain synchronized across multiple engine paths.
- Older npm versions selected through supported Node overrides may ignore `NPM_CONFIG_MIN_RELEASE_AGE` unless the outstanding enforcement gap is resolved; this is a known limitation of the current draft implementation.

#### Neutral
- `engine.cooldown` is distinct from the existing boolean `runtimes.<id>.cooldown` setting and from legacy engine-definition metadata.
- Custom `engine.command` executables, arbitrary shell installers, pre-agent steps, extensions, and other externally managed installation methods remain outside this policy's scope.
- Non-strict workflows may explicitly use zero days, while strict workflows reject an explicit zero and otherwise use the configured positive integer.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
