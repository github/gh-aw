# ADR-66310: Reject Unavailable Native Copilot Web Tools at Compile Time

**Date**: 2026-10-06
**Status**: Draft
**Deciders**: gh-aw maintainers (pending review; PR author: pelikhan)

---

### Context

`gh aw` compiles Copilot workflows to run behind the agentic workflow firewall (AWF) in offline BYOK mode (`COPILOT_OFFLINE=true` with `COPILOT_PROVIDER_BASE_URL` pointing at the api-proxy sidecar). Copilot CLI disables all of its native web tools in that mode. Until now, declaring `tools.web-fetch` or `tools.web-search` for the Copilot engine compiled cleanly, emitted `--allow-tool web_fetch` / `--allow-tool web_search`, enabled the built-in MCP tool schema, and gated `web-search` on a CLI version check — but the tools never appeared in the agent's catalog (issue #65043: a Copilot CLI 1.0.91 probe showed 17 catalog tools with neither `web_fetch` nor `web_search`, and zero firewall egress). The failure was silent: workflows advertised a capability the agent could not use, and authors only discovered it from agent output at run time. Copilot SDK mode is different: it ships a custom proxy-aware `web_fetch` replacement that an SDK 1.0.16 / CLI 1.0.91 probe confirmed is visible and functional through the proxy.

### Decision

We will reject unavailable native Copilot web tools during compilation instead of granting permissions for tools that do not exist at run time. `tools.web-search` is a compilation error for the Copilot engine in all modes, and `tools.web-fetch` is a compilation error for Copilot CLI mode; both are rejected regardless of strict mode or network configuration, because availability is a property of the offline BYOK runtime rather than a policy knob. Copilot SDK mode (`engine.copilot-sdk: true`) keeps `tools.web-fetch`, served by the custom proxy-aware implementation, and the SDK tool-config contract now throws if native `web_search` is ever admitted. Validation runs against the merged tool set and the resolved engine configuration so that imported/SDK settings behave identically in file and string compilation, and the now-dead native permission plumbing (web-tool `--allow-tool` flags, `web-search` CLI version gating, conditional built-in MCP enabling) is removed.

### Alternatives Considered

#### Alternative 1: Silently drop the permissions and emit a warning

Keep accepting `web-fetch:` / `web-search:` for Copilot, stop emitting the `--allow-tool` flags, and print a compiler warning. This was a genuine contender: it is non-breaking, so existing workflows keep compiling and no migration is required. It was rejected because the observable behaviour would still be "declared capability, absent tool" — exactly the silent failure reported in #65043 — and compiler warnings are routinely unread in CI output. A hard error is the only signal that forces the author to choose a working alternative.

#### Alternative 2: Transparently substitute an MCP fetch/search server

Issue #65043 suggested falling back to an MCP fetch server routed through the firewall, as gh-aw already does for engines without a native fetch tool. This preserves the author's intent and keeps `web-fetch:` portable across engines. It was rejected for this change because an implicit fallback silently adds a container, network egress surface, and (for search) a third-party provider plus credentials that the workflow never declared — a security and supply-chain decision that must stay explicit in the workflow source. It remains open as a follow-up if an explicit opt-in form is designed.

#### Alternative 3: Gate on Copilot CLI version or on `COPILOT_OFFLINE`

Keep the native tools for configurations where offline mode is not in effect, extending the existing `web-search` version gate. Rejected because gh-aw always compiles Copilot runs into the offline BYOK + api-proxy topology, so the "online" branch would be unreachable dead code that still has to be maintained and tested.

### Consequences

#### Positive
- A misconfiguration that previously surfaced only as confused agent behaviour at run time is now caught deterministically at `gh aw compile` time, with a message pointing to the supported alternatives.
- Removing the native web-tool permissions, the `web-search` version gate, and the conditional built-in MCP enabling deletes code paths that could never succeed, shrinking the Copilot engine surface and its test matrix.
- Validating the merged tool set against the resolved engine configuration makes imported SDK settings behave consistently between file-based and string-based compilation.
- Engine capability documentation (`engines.md`, `tools.md`, `web-search.md`, `glossary.md`, `copilot.md`) now matches the runtime reality instead of overstating Copilot support.

#### Negative
- This is a breaking change for existing workflows: any Copilot workflow declaring `web-search:`, or `web-fetch:` in CLI mode, stops compiling and must be migrated to an MCP server, to SDK mode, or to another engine. It ships as a `patch` changeset rather than a major bump.
- Authors lose a one-line way to express web access for Copilot; the supported replacements (MCP server configuration, or switching engines) are more verbose and, for search, require third-party credentials.
- The rule is unconditional, so a future Copilot topology that re-enables native web tools would require reverting this validation rather than flipping a flag.
- Copilot's behaviour now differs by mode (`web-fetch` allowed in SDK mode, rejected in CLI mode), which is an extra distinction authors must learn.

#### Neutral
- The Copilot smoke golden fixture and several compiler tests were regenerated/inverted to assert the absence of `--allow-tool web_fetch` / `web_search`; that regeneration also absorbed pre-existing compiler-output drift.
- The SDK tool-config contract check (`copilot_sdk_tool_config.cjs`) now fails loudly on native `web_search`, converting a silent capability mismatch into a test/runtime assertion.
- No change for the Claude, Codex, Gemini, or Pi engines; their web-tool behaviour and defaults are untouched.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
