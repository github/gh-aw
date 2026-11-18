# ADR-78621: Isolate Copilot SDK Bootstrap from Repository Package Policy

**Date**: 2026-11-17
**Status**: Draft
**Deciders**: pelikhan (PR author) — [TODO: verify]

---

### Context

Compiled workflows using the Copilot SDK engine installed `@github/copilot-sdk` and `undici` with `npm install` run inside `${GITHUB_WORKSPACE}`, i.e. against the repository under test. Repositories that enforce strict pnpm `devEngines` policies or use `catalog:` / `workspace:` dependency protocols reject that npm invocation, so the SDK bootstrap fails for reasons unrelated to the workflow (see #77386). The SDK is infrastructure owned by gh-aw, not a dependency of the user's application, and the install tree must be visible inside the sandbox and survive Node runtime switches (NVX).

### Decision

We will install the SDK dependencies (normal and inline Node drivers) into a dedicated gh-aw-owned prefix, `${RUNNER_TEMP}/gh-aw/engine-cli/copilot-sdk`, via `npm install --prefix ... --ignore-scripts --no-save`, instead of in the checkout. The driver resolves modules from that prefix by exporting its `node_modules` on `NODE_PATH` (CommonJS) and by registering a scoped ESM resolve hook (`copilot_sdk_module_resolution*.cjs`) that falls back to the prefix only for `@github/copilot-sdk` and `undici` when normal resolution fails. This keeps application-local packages authoritative and leaves application manifests and lockfiles untouched.

### Alternatives Considered

#### Alternative 1: Keep installing in the workspace and relax or bypass the repository's package policy

Pass flags or environment overrides so npm tolerates pnpm `devEngines` and `catalog:`/`workspace:` protocols. Rejected: it weakens repository-defined policy, is fragile across npm/pnpm versions, and still mutates the user's tree.

#### Alternative 2: Bundle the SDK with the setup action

Ship the SDK and `undici` pre-installed in the setup action. Avoids a runtime install, but increases the action's size and release coupling to SDK versions, and does not fit the existing pinned-version install flow. [TODO: verify]

#### Alternative 3: Set only `NODE_PATH` for the isolated prefix

Simplest approach, but ESM `import` ignores `NODE_PATH`, so the inline/ESM driver would not find the SDK; a resolution hook was needed.

### Consequences

#### Positive
- SDK bootstrap no longer depends on or modifies the repository's package manager configuration, manifests, or lockfiles.
- Install location lives under the sandbox-mounted setup tree, so SDK loading is visible inside the sandbox.
- Resolution fallback is scoped to two packages, so application-local versions are not overridden.
- Regression tests assert unchanged manifests/lockfiles, no application install, and sandbox-visible loading.

#### Negative
- Adds custom module-resolution code (`registerHooks` / `register` fallback) that depends on Node's evolving module hook APIs.
- Roughly 100 generated `.lock.yml` files change, increasing review noise and requiring recompilation.
- A repository that deliberately provides its own SDK version is not used as the install source for the bootstrap prefix.

#### Neutral
- Package versions stay pinned (`@github/copilot-sdk` and `undici@6.28.0`); only the install location changes.
- The Python SDK install path (`.gh-aw/copilot-sdk/python`) is unchanged.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
