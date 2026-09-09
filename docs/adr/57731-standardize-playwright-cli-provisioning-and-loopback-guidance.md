# ADR-57731: Standardize Playwright CLI Provisioning and Loopback Guidance

**Date**: 2026-09-01
**Status**: Draft
**Deciders**: Unknown

---

### Context

This pull request changes both workflow compiler behavior and Playwright documentation. In `pkg/workflow/playwright_cli.go`, the compiler now installs Chromium, Firefox, and WebKit before the agent starts instead of relying on lazy browser downloads during agent execution. The docs in `.github/aw/playwright.md`, `docs/src/content/docs/reference/playwright.md`, and `.github/aw/network.md` are updated to define the supported `playwright-cli` command surface, clarify that same-sandbox loopback access does not require `network.allowed: local`, and document screenshot publication and troubleshooting. Because this affects built-in tool provisioning, runtime constraints, and workflow authoring guidance, the implementation reflects an architectural product decision rather than a local documentation-only change.

### Decision

We will make built-in Playwright CLI workflows provision Chromium, Firefox, and WebKit before the agent starts and treat browser installation during agent execution as unsupported. We will also standardize workflow guidance around the current `@playwright/cli` interface, automatic restricted-Bash permission for `playwright-cli:*`, and loopback access to same-sandbox services without requiring `network.allowed: local`. We chose this approach because the PR consistently aligns compiler steps, generated fixtures, and user-facing documentation around a deterministic pre-provisioned Playwright model.

### Alternatives Considered

#### Alternative 1: Keep Lazy Browser Installation During Agent Execution

Continue installing only `@playwright/cli` and its skills before the run, while allowing browser binaries to be downloaded when the agent first invokes browser commands.

This was considered because it keeps startup shorter and defers work until a browser is actually used. It was not chosen because the PR evidence explicitly marks runtime installation of browsers and packages as prohibited and adds compiler install steps so browser availability is deterministic before the agent begins.

#### Alternative 2: Require Explicit Loopback and Tool Allowlist Configuration in Every Workflow

Keep documentation conservative by requiring authors to add `network.allowed: local` for localhost access and manually list `playwright-cli:*` in restricted Bash configurations.

This was considered because it makes all permissions visually explicit in workflow source. It was not chosen because the PR updates documentation and golden fixtures to reflect actual platform behavior: same-sandbox localhost already works without `local`, and the compiler automatically allows `playwright-cli:*` when restricted Bash is used.

### Consequences

#### Positive
- Playwright CLI workflows become more deterministic because supported browsers are installed before the agent starts.
- Workflow authors get one consistent set of Playwright CLI instructions across compiler docs, reference docs, and generated fixtures.
- Local testing guidance better matches the AWF sandbox model by documenting loopback access without unnecessary `local` entries.

#### Negative
- Workflow setup now performs additional browser installation steps even when only one browser may be used.
- The compiler and fixtures take on extra maintenance for browser provisioning and related generated outputs.
- Authors who relied on older MCP-style or legacy Playwright CLI examples must update their expectations and prompts.

#### Neutral
- Generated golden fixtures change to encode browser install steps, updated prompts, and restricted-tool allowlists.
- Screenshot retrieval is now documented through safe-output artifact publication rather than implied by local `/tmp` file access alone.
- Accessibility guidance remains limited to what can be supported without runtime dependency installation inside the sandbox.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
