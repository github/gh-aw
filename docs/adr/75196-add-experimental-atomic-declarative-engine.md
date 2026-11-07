# ADR-75196: Add an Experimental Atomic Declarative Engine

**Date**: 2026-11-05
**Status**: Proposed
**Deciders**: gh-aw maintainers (review pending)

---

### Context

Atomic provides a headless coding agent with multi-provider inference and native HTTP MCP support. gh-aw already supports behavior-defined engines, so integrating Atomic does not require a dedicated Go engine. Existing compiler normalization must preserve authored gateway policies and reject unsupported native restrictions rather than silently discard them. The integration must preserve gh-aw's sandbox routing and keep upstream provider credentials out of the Atomic child process. Atomic's project configuration, extensions, and environment inheritance also need explicit limits for unattended execution.

### Decision

Add Atomic as an unsupported, experimental shared definition in `.github/workflows/shared/atomic.md`, using the existing declarative runtime and pinning `@bastani/atomic` to `0.9.71` with npm lifecycle scripts disabled. Require AWF reflection to discover the configured provider endpoint, and configure Atomic with a placeholder key rather than upstream credentials; direct-key execution is not supported. Pass the prompt on stdin, create private per-run model and MCP configuration, and launch the child with an explicit runtime environment allowlist and credential-free proxy URLs. Disable project-local configuration, saved sessions, telemetry, skills, prompt templates, and themes, while retaining bundled extensions needed for native MCP and selecting only read, bash, edit, write, and MCP tools.

Retain authored MCP gateway configuration, including GitHub integrity and repository restrictions. Validate native tool restrictions before capability normalization; Atomic rejects bash restrictions and `edit: false` because its fixed native tool profile cannot enforce them.

### Alternatives Considered

#### Alternative 1: Add a dedicated Go engine

A built-in engine could expose Atomic-specific capabilities directly through the compiler. It is not chosen because the existing declarative behaviors already cover installation, execution, provider routing, and MCP adaptation; a dedicated engine would duplicate infrastructure before the experiment establishes a need for it.

#### Alternative 2: Invoke Atomic with direct provider credentials and inherited configuration

A command wrapper could reuse Atomic's normal authentication, environment, and project settings with less adaptation. It is not chosen because upstream keys would reach the agent process and repository-local settings or execution hooks could alter unattended behavior. AWF-only inference and private configuration provide a narrower, explicit integration contract.

### Consequences

#### Positive

- Atomic can be evaluated through existing engine infrastructure without a dedicated Go engine; compiler fixes preserve gateway policies and fail closed on unsupported native restrictions.
- Provider credentials stay outside the Atomic child environment, and private per-run configuration avoids overwriting repository settings.

#### Negative

- The sample depends on Atomic's pinned CLI flags, configuration schema, and bundled MCP extension behavior; upgrades require compatibility validation.
- It does not advertise native gh-aw tool allowlists, iteration limits, Agent Plugins, custom Atomic process workflows, or structured session/token parsing.

#### Neutral

- Native HTTP MCP retains gateway authorization in private configuration and excludes CLI-mounted servers. The gateway credential is visible to the agent, so it is not an isolation boundary; environment filtering also does not hide secrets already exposed to the harness or stored in files.
- Mock-provider and MCP tests cover the integration contract, and a manually dispatched smoke workflow is provided. This record does not claim a live smoke run or authorize workflow dispatch.
