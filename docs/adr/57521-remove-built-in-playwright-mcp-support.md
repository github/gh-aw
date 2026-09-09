# ADR-57521: Remove Built-in Playwright MCP Support

**Date**: 2026-08-31
**Status**: Draft
**Deciders**: pelikhan, adr-writer agent

---

### Context

This pull request removes the built-in Playwright MCP integration from `gh-aw` and makes the Playwright tool use only the CLI-based integration. The PR updates compiler behavior, generated workflow fixtures, reference docs, dependency/version tracking, and migration guidance so `tools.playwright` no longer provisions MCP-specific tooling, metadata, or container images. The PR body states that `mode: mcp` should become a compile error with actionable migration guidance and that workflows still needing MCP must configure a custom `mcp-servers` entry. Because the change affects core workflow compilation behavior and removes a supported built-in integration path, the decision should be recorded explicitly.

### Decision

We will remove built-in Playwright MCP support from `gh-aw` and make the built-in `tools.playwright` integration always use `@playwright/cli`. The compiler will reject `mode: mcp` and provide migration guidance that directs users to equivalent CLI commands or to an explicit custom `mcp-servers.playwright` configuration when MCP is still required. We chose this approach because the PR consistently removes MCP-only rendering, registration, inspection, container/image tracking, and generated tool permissions while preserving browser automation through the simpler built-in CLI path.

### Alternatives Considered

#### Alternative 1: Keep Both Built-In Modes (`cli` and `mcp`)

Retain the current dual-mode built-in integration so workflows can continue choosing between CLI-backed and MCP-backed Playwright behavior.

This was considered because it preserves backward compatibility and avoids breaking workflows that depend on `mode: mcp`. It was not chosen because the PR evidence shows the repository is intentionally removing MCP-specific compiler branches, generated tool catalogs, version tracking, and container references in favor of a single built-in integration path.

#### Alternative 2: Keep Built-In MCP Support but Mark It Deprecated

Leave the built-in MCP path in place temporarily while documenting it as deprecated and steering users toward the CLI integration.

This was considered because it would reduce migration pressure and allow a slower transition. It was not chosen because the PR body and diff both indicate an immediate removal strategy: `mode: mcp` becomes a compile error, MCP-only arguments and metadata are deleted, and workflows requiring MCP must switch to explicit custom server configuration now.

### Consequences

#### Positive
- The built-in Playwright integration becomes simpler because workflow compilation supports one built-in execution model instead of maintaining both CLI and MCP branches.
- Generated workflows and reference material no longer need built-in MCP tool registration, container handling, or image/version tracking.
- Users who still need MCP retain a supported path through explicit `mcp-servers` configuration, which makes that dependency visible in workflow source.

#### Negative
- Existing workflows that use `tools.playwright.mode: mcp` will break at compile time and must be migrated.
- The repository takes on migration-documentation work to explain equivalent CLI usage and custom MCP server setup clearly.
- Removing built-in MCP support reduces convenience for users who preferred the previous out-of-the-box MCP integration.

#### Neutral
- Version tracking shifts to `@playwright/cli` only and stops monitoring the Playwright MCP package and related container image.
- Multiple generated workflow lockfiles, fixtures, and documentation artifacts must be regenerated to reflect the new single-mode behavior.
- Custom MCP usage remains possible, but it is no longer represented as a special built-in Playwright mode.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
