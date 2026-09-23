# ADR-62957: Enable native web search for Copilot engine

**Date**: 2026-09-23
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

gh-aw already exposes `tools.web-search` across engines, but the Copilot engine previously declared that capability unsupported even though Copilot CLI and the Copilot SDK runtime both expose a built-in `web_search` tool. The PR description and diff show that this mismatch caused workflows with `engine: copilot` and `tools: web-search:` to compile with an unsupported-engine warning and without the permission needed to invoke search, especially affecting repositories that do not want or cannot use GitHub repository tools or a separate MCP search integration. The change spans Copilot engine capability metadata, CLI argument generation, SDK tool capability wiring, documentation, and tests. The decision is whether gh-aw should continue treating Copilot web search as unsupported unless supplied by external MCPs, or recognize and wire Copilot's native built-in search tool directly.

### Decision

We will treat `tools: web-search:` as a native, opt-in capability for the Copilot engine and compile it to Copilot's built-in `web_search` permission and tool registration. gh-aw will advertise Copilot web-search support in engine capability metadata, preserve Copilot built-in tool schemas when `web-search` is enabled, propagate the capability into the SDK tool configuration, and remove the unsupported-engine warning path for Copilot while keeping unsupported-engine coverage on Gemini. We chose this because the PR evidence shows Copilot already has the necessary built-in tool, and wiring gh-aw to that native capability is simpler and less permission-heavy than forcing users through GitHub repository tooling or third-party MCP search integrations.

### Alternatives Considered

#### Alternative 1: Keep Copilot web search unsupported and require external MCP search integration

This was the previous behavior and remains a viable fallback because MCP-based search works across engines that do not expose a native search primitive. It was not chosen because the PR shows Copilot already exposes a built-in `web_search` tool, so continuing to warn and withhold permission would preserve a false limitation and force extra configuration for a capability the runtime already supports.

#### Alternative 2: Enable Copilot web search implicitly for all Copilot workflows

This was a realistic option because it would reduce user configuration and always make the built-in search tool available. It was not chosen because the existing tool model is explicit and opt-in, and the diff intentionally preserves that contract by granting `--allow-tool web_search` only when workflows declare `tools: web-search:` so authors do not receive unexpected search permissions.

### Consequences

#### Positive
- Copilot workflows that declare `tools: web-search:` now compile to a usable native `web_search` permission instead of a warning and a non-functional configuration.
- Repositories can enable web search on Copilot without also enabling GitHub repository tools or adding a separate MCP search server.
- CLI-mode and SDK-mode Copilot executions stay aligned because capability advertisement, built-in tool registration, and permission parity are updated together and covered by tests.

#### Negative
- gh-aw now relies on Copilot's built-in `web_search` behavior and naming contract, so upstream Copilot tool changes could require follow-up updates.
- The Copilot engine logic becomes slightly more complex because built-in MCP suppression, tool argument mapping, and SDK capability derivation must all special-case `web-search`.

#### Neutral
- Web search remains disabled by default for Copilot and is only enabled when workflows explicitly declare `tools: web-search:`.
- Documentation and integration tests now distinguish Copilot's native support from Gemini's unsupported-engine warning path.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
