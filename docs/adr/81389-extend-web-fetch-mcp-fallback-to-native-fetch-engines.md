# ADR-81389: Extend the Sandboxed Web-Fetch MCP Fallback to Native-Fetch Engines and Allow Curl for gpclean

**Date**: 2026-11-26
**Status**: Draft
**Deciders**: pelikhan (PR author); gh-aw maintainers [TODO: verify reviewers]
**Related**: ADR-80037 (base web-fetch MCP fallback), issue #81218 (gpclean incomplete result)

---

### Context

ADR-80037 added a stateless stdio `web_fetch` MCP server that runs inside the AWF sandbox. It was enabled for engines without native web fetch (Copilot CLI in offline BYOK mode, Pi, Claude on GitHub inference, Goose, and OpenCode). ADR-80037 also states that engines with native fetch keep their current behavior.

The GPL Dependency Cleaner (`gpclean`) reported an incomplete result in #81218 because its required independent license checks were unavailable. Its `minimal_toolset` experiment variant allowed neither web-fetch nor `curl`, so those checks could not be completed. The PR body frames the fix as a gpclean change, but the diff also changes how the fallback is compiled for other engines.

Claude on Anthropic inference, Codex, and Gemini already use native fetch when AWF is disabled. Agy declared no fallback. ADR-80037's constraints still apply: fetch egress must stay under the AWF firewall, and the capability must fail closed when no proxy is configured.

### Decision

We will extend the same AWF-sandboxed `web_fetch` server to the following engines when AWF is enabled:

- **Claude on Anthropic inference**: adds `mcp__web-fetch__web_fetch` to the allowed tools, alongside native `WebFetch`.
- **Codex and Gemini**: register the server in their MCP configuration. Codex passes proxy and CA-bundle variables through `env_vars`. Gemini adds `HTTP_PROXY`, `HTTPS_PROXY`, `SSL_CERT_FILE`, `SSL_CERT_DIR`, and `CURL_CA_BUNDLE` to its shell environment.
- **Agy**: declares the `WebFetchFallback` capability.

Without AWF, Claude on Anthropic, Codex, and Gemini keep native fetch only. Copilot, Pi, Agy, and Claude on GitHub inference fail at compile time without AWF.

For gpclean, we will allow `curl` in the `minimal_toolset` variant, use GitHub search and allowed HTTPS pages for research, and call `report_incomplete` when a required license check cannot be completed. A module is not marked checked in that case, and the run does not report a clean result.

The server is still not added to the MCP gateway, and `web-fetch` remains a reserved server name.

### Alternatives Considered

#### Alternative 1: Keep the fallback limited to ADR-80037 engines and let gpclean report incomplete

This is the smallest change because it adds no engine compiler paths. It was not chosen because gpclean cannot finish its license checks in the restricted variant, and the same incomplete result will recur (#81218).

#### Alternative 2: Allow `curl` in gpclean's bash allowlist only, with no MCP server

This would unblock gpclean without touching any engine compiler. It is not the only mechanism in this PR because bash `curl` has no bounded pagination, response size cap, or the server's redirect and protocol checks. ADR-80037 relies on those checks for untrusted page content. Curl is allowed in gpclean as a complement, not a replacement.

#### Alternative 3: Host the fetch tool in the MCP gateway

This was rejected in ADR-80037. The gateway runs outside the agent's AWF sandbox, so its egress would follow different rules from the agent's own traffic.

### Consequences

#### Positive
- gpclean can complete its license checks in the restricted variant. Checks that cannot be completed are reported as incomplete, not as clean.
- Engines with native fetch get a bounded, paginated fallback that uses the same AWF firewall path.
- Fetch egress still goes through the AWF proxy, and the server is still absent from the gateway configuration.

#### Negative
- **Divergence from ADR-80037**: its consequence "Engines with native fetch keep their current behavior" no longer holds for Claude on Anthropic, Codex, Gemini, and Agy when AWF is enabled. ADR-80037 should be amended or superseded in part when this ADR is accepted.
- Gemini and Codex pass proxy and CA-bundle variables into the sandbox. The forwarded names are fixed in code, and no credential variables are added, but this is a new environment path to review.
- `curl` in the `minimal_toolset` variant is a general HTTPS client inside the sandbox. The only egress limit is the AWF network allowlist, which the PR body says still applies. [TODO: verify that the allowlist covers the license and module hosts gpclean needs.]
- Without AWF, `tools.web-fetch` now fails at compile time for Agy and Claude on GitHub inference, in addition to Copilot and Pi. It compiles to native fetch alone for Claude on Anthropic, Codex, and Gemini.
- **Scope gap**: the PR body describes only the gpclean change. The engine compilers, gateway configuration, Gemini environment, and docs changes are not described there. [TODO: author should update the PR body or split the PR.]

#### Neutral
- The `web-fetch` server name stays reserved, as in ADR-80037.
- `gpclean.lock.yml` is regenerated to match the workflow source.
- `docs/src/content/docs/reference/engines.md` and `tools.md` are updated to describe the new per-engine behavior.

---

*ADR drafted by the Design Decision Gate from PR #81389 evidence. Review and finalize before changing status from Draft to Accepted.*
