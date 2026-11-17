# ADR-78465: Separate Direct Model API Egress from Engine Transport Bundles

**Date**: 2026-11-17
**Status**: Draft
**Deciders**: Unknown [TODO: verify. PR author is gh-aw-bot; the gate actor is pelikhan.]

---

### Context

Engine domain sets (`copilot`, `codex`, `gemini`, `pi`, `claude`) were bundles of gateway/GitHub transport hosts and direct vendor model API hosts (`api.githubcopilot.com`, `api.openai.com`, `generativelanguage.googleapis.com`). Normal inference runs through the AWF API proxy, so the agent container does not need direct access to those hosts, yet adding an engine set granted it. Earlier work ([ADR-76272](76272-decouple-gemini-proxy-from-agent-egress.md)) decoupled the Gemini proxy upstream from agent egress, and [ADR-55461](55461-centralize-engine-default-domain-sets.md) centralized engine default sets. The domains audit tracked in issue #78353 found the remaining direct-egress leaks. Custom targets (`ANTHROPIC_BASE_URL`, `engine.api-target`, `GEMINI_API_BASE_URL`) and Codex external detection must keep working.

### Decision

We will make the engine transport sets (`engine-defaults` in `ecosystem_domains.json`) contain only gateway and GitHub transport hosts, and move direct model API hosts into explicit opt-in vendor sets: `copilot-vendor` (adds `api.githubcopilot.com`), `codex-vendor` (adds `api.openai.com`), and the new `gemini-vendor`. Claude no longer implicitly gets `api.anthropic.com`; it must be listed literally. Pi drops its provider-to-domain map (`pi-provider-domains`) and uses only the gateway baseline. The driver is least-privilege agent egress: the proxy path is the default, and direct vendor access should be a deliberate choice.

### Alternatives Considered

#### Alternative 1: Keep vendor API hosts in the engine sets

Leaving the current bundles would avoid any migration cost. It was rejected because it grants the agent direct model API egress that the proxy architecture is meant to make unnecessary, and it contradicts the audit finding.

#### Alternative 2: Add direct hosts dynamically based on the effective provider

This is how Claude and Pi worked: the compiler added `api.anthropic.com` or a provider domain depending on configuration. It keeps existing workflows working, but the result is implicit and hard to reason about, and it was the source of the leak. Explicit vendor sets give one uniform opt-in model across engines.

#### Alternative 3: Remove direct-egress opt-ins entirely

Forbidding direct vendor access would be the strictest option. It was not chosen because some workflows intentionally need direct access (for example, a custom target alongside the vendor API), and external Codex detection requires `api.openai.com`.

### Consequences

#### Positive
- The agent firewall no longer allows direct model API hosts by default, which narrows the egress surface.
- All engines share one mental model: engine set = gateway/GitHub transport, vendor set or literal domain = direct provider access.
- Domain resolution gets simpler because the Pi provider map and the Claude special-case merge are removed.
- Regression tests assert both that transport sets exclude vendor hosts and that vendor sets include them.

#### Negative
- This is a breaking change: workflows that relied on an engine set (or Claude's default target) for direct API egress must add `*-vendor` or a literal domain.
- Codex keeps a special case that appends `api.openai.com` for external detection, so the "transport-only" rule has an exception.
- `copilot-vendor` now mixes the main API host with plan-specific and telemetry hosts, so opting in is broader than a single host.

#### Neutral
- The `docs/src/content/docs/reference/network.md` reference is updated to document the new vendor sets.
- Claude and Pi have no dedicated vendor set; they rely on a literal domain or the matching shared vendor set.
- Lock files for existing workflows may need recompiling if their allowlists change.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
