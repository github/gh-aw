# ADR-66957: Use AWF-Local Gateway Hostnames for Network-Isolated Agents

**Date**: 2026-10-08
**Status**: Draft
**Deciders**: SivaKesava1 (PR #66957 author) [TODO: verify additional deciders]

---

### Context

Agentic engines (Claude, Copilot BYOK, Gemini, and universal LLM-consumer engines) are configured at compile time with a bootstrap LLM gateway base URL (`ANTHROPIC_BASE_URL`, `COPILOT_PROVIDER_BASE_URL`, `GEMINI_API_BASE_URL`). That URL was unconditionally emitted as `http://host.docker.internal:<gatewayPort>`. When the Agentic Workflow Firewall (AWF) network isolation profile is active, the agent container runs on an isolated Docker network where `host.docker.internal` is not reachable; the API proxy is reachable only by its service hostname `api-proxy`. Claude's harness performs a pre-flight connectivity check against the bootstrap URL *before* it discovers the real runtime endpoint via the AWF `/reflect` call, so the unreachable host caused the run to fail before discovery could correct it (see #66788). Non-isolated and host-access profiles still require `host.docker.internal`, so the fix must be topology-aware rather than a global rename.

### Decision

We will select the LLM gateway hostname based on the workflow's network topology in a single shared helper in `pkg/workflow/llm_provider.go`: emit `api-proxy` when `isAWFNetworkIsolationEnabled(workflowData)` is true, and retain `host.docker.internal` otherwise. All engines that build gateway URLs (Claude, Copilot BYOK, Gemini, universal LLM-consumer) consume this shared selection rather than hardcoding a host, and Claude's harness continues to overwrite the bootstrap URL with the endpoint discovered through AWF `/reflect`.

### Alternatives Considered

#### Alternative 1: Always emit `api-proxy`

Simplest possible change: replace the hardcoded `host.docker.internal` with `api-proxy` everywhere. Rejected because non-isolated and host-access firewall profiles do not resolve the `api-proxy` service name; this would move the breakage from isolated runs to every other profile.

#### Alternative 2: Rely solely on runtime endpoint discovery (`/reflect`)

Let the bootstrap URL stay wrong and have each harness replace it at runtime. Considered because discovery already exists for Claude and keeps compile-time output topology-agnostic. Rejected because Claude's pre-flight check runs before discovery, and the other engines (Copilot BYOK, Gemini, universal provider) have no equivalent discovery step, so the bootstrap value must be correct for them.

#### Alternative 3: Add a host alias so `host.docker.internal` resolves inside the isolated network

Configure `extra_hosts`/DNS in the isolated compose topology so the existing URL keeps working. Rejected as it widens the isolation boundary the firewall is meant to enforce and makes the network contract implicit rather than explicit in the compiled workflow.

### Consequences

#### Positive
- Isolated AWF runs get a reachable bootstrap gateway URL, so Claude's pre-flight check succeeds and #66788 is resolved.
- Host selection lives in one shared helper, so every engine stays consistent and future gateway consumers inherit the behaviour automatically.
- Compiled lock files now make the expected network topology explicit and testable (tests assert `api-proxy` for isolated profiles and `host.docker.internal` otherwise).

#### Negative
- Gateway URL generation now depends on `workflowData`, coupling URL construction to workflow configuration and requiring that parameter to be threaded through engine call sites.
- Compiled `.lock.yml` outputs and golden files change, so downstream pinned snapshots and any external tooling that matched `host.docker.internal` must be regenerated.
- A second code path means isolation-specific regressions can only be caught by profile-specific tests; a wrong `isAWFNetworkIsolationEnabled` result silently produces an unreachable URL.

#### Neutral
- `api-proxy` becomes a de facto stable service-name contract between the compiler and the AWF compose topology.
- The `smoke-claude-on-copilot` workflow keeps its two-day schedule but now performs a model-generated observation before returning `noop`, and its lock file was regenerated as part of this change.
- Behaviour for non-isolated and host-access profiles is unchanged.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
