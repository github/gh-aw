# ADR-76272: Decouple Gemini Proxy Upstream from Agent Egress Policy

**Date**: 2026-11-09
**Status**: Draft
**Deciders**: Unknown [TODO: verify. PR author is copilot-swe-agent; the gate actor is pelikhan.]

---

### Context

Gemini workflows run behind the AWF API proxy, which forwards model calls to the upstream host `generativelanguage.googleapis.com`. The compiler prewarms a single cached allow-list (`CachedAllowedDomainsStr`) and reuses it both for sanitization and as the agent firewall `network.allowDomains`. That cache included the default Gemini upstream, so a Gemini workflow could reach the model API directly without the documented `gemini` domain-set opt-in. This builds on [ADR-55461](55461-centralize-engine-default-domain-sets.md), which made explicit `network.allowed` opt-in the rule for engine domains. The finding is tracked in issue #76219. Proxy routing, explicit custom Gemini targets (`GEMINI_API_BASE_URL`), and BYOK behavior must keep working.

### Decision

We will keep two egress policies separate. The agent firewall policy, computed by `computeAllowedDomainsForFirewall` and cached in `CachedAllowedDomainsStr`, will include only explicitly configured hosts: `network.allowed`, engine domain sets, and a custom `GEMINI_API_BASE_URL` host. The sanitization policy, computed by `computeAllowedDomainsForSanitization`, will add the Gemini proxy upstream on top of the firewall policy. The default upstream stays in the AWF API-proxy target configuration for routing, but it is no longer added to agent `network.allowDomains` unless the workflow opts in with `gemini` or the hostname. The primary driver is least-privilege agent egress.

### Alternatives Considered

#### Alternative 1: Require `gemini` opt-in before configuring the proxy upstream

Making the opt-in a precondition for any Gemini proxy configuration would keep a single policy and make proxied and direct access equivalent. It was not chosen because it would break gateway routing for workflows that do not opt in. The added tests still expect the proxy target to be the default host when the network is empty or GitHub-only, with direct access disabled. This was not a close call. [TODO: verify with PR author]

#### Alternative 2: Keep one cache and strip the Gemini host when emitting the AWF config

Filtering the host out of `network.allowDomains` in `awf_config_build.go` would touch only one location. It was not chosen because the same cached value feeds sanitization, the firewall config, and the Gemini execution step. A filter at emission time would have to tell compiler-injected hosts apart from user-requested ones. The cache would also keep describing a mixed policy. [TODO: verify rationale with PR author]

### Consequences

#### Positive
- Agent containers no longer get implicit direct access to `generativelanguage.googleapis.com`. Direct access now requires `gemini` or the hostname in `network.allowed`, as documented.
- The `CachedAllowedDomainsStr` contract is narrower: it holds only the agent firewall policy, and its comment now says so.
- `TestGeminiProxyTargetDoesNotGrantAgentEgress` covers empty, GitHub-only, opt-in, custom-target, and cold/warm cache cases. The two Gemini lock files drop the host from emitted `allowDomains`.

#### Negative
- Gemini workflows that relied on implicit direct egress will lose it on upgrade and must add `gemini` to `network.allowed`. [TODO: verify no other in-repo workflow depends on direct egress]
- There are now two domain computations, and contributors must pick the right one. The sanitization variant is not cached.
- Validation was static: emitted-config and unit tests only. No live Gemini request was made. [TODO: verify whether a live smoke run is planned]

#### Neutral
- Sanitization still allows the default Gemini host, so agent output that links to it is still accepted.
- Compiler prewarm now returns errors from `computeAllowedDomainsForFirewall`. Previously they were discarded with `_, _ =`.
- `docs/src/content/docs/reference/network.md` documents the revised behavior.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
