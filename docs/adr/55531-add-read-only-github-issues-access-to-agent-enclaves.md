# ADR-55531: Add Read-Only GitHub Issues Access to Agent Enclaves

**Date**: 2026-08-28
**Status**: Draft
**Deciders**: lpcox, copilot-swe-agent

---

### Context

This pull request adds a new enclave capability, `enclaves[].agent.github.cli: issues-read-v1`, for agent enclaves that need GitHub Issues data while preserving the repository's finite-disclosure and fail-closed isolation model. The diff updates the workflow schema, compiler version gates, startup scripts, and documentation so the feature is only available with AWF `v0.28.9` and mcpg `v0.4.13`, uses a dedicated compiler-owned proxy, and limits access to a closed set of REST read routes. The same PR also adjusts timeout ceilings from the earlier 600-second bucket to the newer 4,800-second bucket and introduces deferred readiness handling so the AWF-owned `awf-enclave` backend is not eagerly probed before AWF starts it.

### Decision

We will support a closed, opt-in `issues-read-v1` GitHub CLI profile for agent enclaves, implemented through a separate compiler-owned mcpg proxy that retains the upstream token and exposes only a PAT-free local handoff to AWF and the enclave. We will enforce this through schema validation, version minimums, exact allowed REST issue endpoints, repository sensitivity rules, and compiler-owned deferred readiness for the `awf-enclave` MCP backend. This decision prioritizes secure isolation and deterministic compiler control over broader or more flexible GitHub access.

### Alternatives Considered

#### Alternative 1: Give Enclaves General GitHub CLI or GraphQL Access

The PR could have allowed a broader `github.cli` mode or relied on stock `gh issue` commands without route-level restrictions.

This was considered because it would be simpler for users and would reduce special-case compiler logic. It was rejected because the diff repeatedly enforces a closed `issues-read-v1` value, documents that stock `gh issue` commands may fall back to denied GraphQL calls, and adds fail-closed behavior for all non-approved REST routes. Broader access would weaken the enclave isolation and policy guarantees this feature is trying to preserve.

#### Alternative 2: Reuse the Primary Agent's Existing GitHub Access Path

Instead of starting a dedicated compiler-owned proxy, the implementation could have shared the primary agent's GitHub connectivity or passed token-bearing configuration directly into AWF or the enclave.

This was considered because it would avoid new proxy lifecycle scripts and reduce moving pieces. It was rejected because the diff explicitly adds `start_enclave_github_proxy.sh` and `stop_enclave_github_proxy.sh`, masks and hands off only limited runtime values through `GITHUB_ENV`, excludes `MCP_GATEWAY_AGENT_ID` from the primary agent, and documents that the primary agent must not receive the proxy address, agent ID, CA path, identity, capability, PAT, or repository catalog. Reusing the primary path would contradict the isolation model implemented here.

### Consequences

#### Positive
- Agent enclaves can read GitHub Issues through a narrowly scoped, explicit capability instead of requiring broader repository or GitHub access.
- The compiler keeps control of sensitive proxy lifecycle and token placement, reducing exposure of secrets and preserving the repository's fail-closed enclave model.
- Schema, tests, and docs now encode the exact supported mode, version minimums, timeout bounds, and deferred startup behavior, making the feature more deterministic to validate and operate.

#### Negative
- The compiler and setup scripts gain additional complexity, including dedicated proxy startup/cleanup logic, deferred MCP readiness handling, and more version-gated behavior.
- Users do not get general `gh issue` compatibility; they must work within exact REST GET routes and documented profile constraints.
- The feature now depends on synchronized behavior across gh-aw, AWF `v0.28.9`, and mcpg `v0.4.13`, which increases cross-repository coupling for future changes.

#### Neutral
- Enclave timeout limits and gateway tool-timeout conversions are updated alongside this feature to reflect the newer 4,800-second finite-disclosure bucket.
- The ADR number is tied to PR `#55531`, matching the design-gate convention for draft ADR generation on implementation-heavy pull requests.
- Documentation in `.github/aw/enclaves.md`, `docs/src/content/docs/experimental/enclaves.md`, and the glossary now becomes part of the architectural contract for this enclave profile.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
