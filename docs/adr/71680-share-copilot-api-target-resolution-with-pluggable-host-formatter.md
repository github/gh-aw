# ADR-71680: Preserve HTTP Only for Copilot BYOK Provider Targets

**Date**: 2026-10-24
**Status**: Draft
**Deciders**: Unknown; human review and acceptance are pending.

---

### Context

The compiler derives the Copilot API target that is passed to AWF's api-proxy and that is also used for host-only consumers such as firewall allowlists. Resolution follows a precedence chain: explicit `engine.api-target`, then `GITHUB_COPILOT_BASE_URL`, then a literal BYOK `COPILOT_PROVIDER_BASE_URL`. Host extraction strips the URL scheme, so a BYOK URL such as `http://host.docker.internal:11434` previously reached the API proxy config as `host.docker.internal:11434`, losing the protocol information needed for plain-HTTP backends such as Ollama. Existing API proxy formatting preserves `http://` only when the effective AWF version supports HTTP API targets (`awfSupportsHTTPAPITargets`). This change must retain host-only extraction for `GITHUB_COPILOT_BASE_URL` and firewall consumers, and preserve the literal-only restriction on the BYOK fallback.

### Decision

The proposed design shares the Copilot precedence chain through the internal `resolveCopilotAPITarget(workflowData, preserveProviderHTTP)` function. Explicit `engine.api-target` values remain unchanged, and `GITHUB_COPILOT_BASE_URL` retains host-only extraction for both consumers; only the literal `COPILOT_PROVIDER_BASE_URL` fallback may retain `http://` in `BuildAWFConfigJSON`, subject to the existing AWF version gate. The resolver excludes `${{ ... }}` provider expressions and extracts the literal provider host once before formatting it; `GetCopilotAPITarget` passes `false` to retain host-only output. This confines HTTP preservation to BYOK routing without changing transport for GitHub-token-authenticated requests.

### Alternatives Considered

#### Alternative 1: Change `GetCopilotAPITarget` to always preserve the scheme

Making the existing function return `http://host:port` everywhere would have been the smallest diff. It was not chosen because host-only consumers (firewall allowlists, `GetCopilotAllowlistTargets`) expect bare hostnames. Older AWF versions also do not accept scheme-prefixed targets, so this would have broken legacy compatibility.

#### Alternative 2: Duplicate the precedence chain in `BuildAWFConfigJSON`

The API proxy builder could re-implement the explicit-target / `GITHUB_COPILOT_BASE_URL` / BYOK fallback chain inline and format the provider fallback there. This avoids a formatting flag, but the two copies of the precedence rules could drift apart when either entry point changes.

#### Alternative 3: Post-process the host-only result to re-add the scheme

The caller could take the bare host from `GetCopilotAPITarget` and look up which env var it came from to decide whether to prepend `http://`. This was not chosen because it needs the caller to reconstruct which precedence branch matched, which spreads the resolution logic across call sites.

### Consequences

#### Positive
- Literal BYOK HTTP fallbacks (for example Ollama at `http://host.docker.internal:11434`) receive a scheme-preserving target on supported AWF versions, so the api-proxy can route over plain HTTP.
- Target precedence is defined in one place and shared by host-only and API proxy consumers.
- AWF version gating is reused through `awfSupportsHTTPAPITargets`, so legacy AWF versions still receive bare hosts.

#### Negative
- `resolveCopilotAPITarget` takes a formatting flag; callers must enable it only when the effective AWF version supports HTTP targets.
- HTTP scheme preservation must remain limited to the literal provider fallback, not the GitHub-token-authenticated base URL.
- `GetCopilotAPITarget` and the API proxy config can return different strings for the same literal HTTP provider fallback (bare host vs `http://host`). Future callers must pick the right entry point.

#### Neutral
- Compiled lock files that use HTTP BYOK providers change only in the target's scheme (for example `daily-byok-ollama-test.lock.yml`).
- Regression coverage must distinguish the literal provider fallback from `GITHUB_COPILOT_BASE_URL`, and cover expressions, explicit precedence, HTTPS, and legacy AWF behavior.
- The ADR does not establish whether this bug caused the 503 failures seen in upstream runs. That remains unproven.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
