# ADR-73256: Block Vulnerable Claude Code Versions and Track Engine Packages in Dependabot Manifests

**Date**: 2026-07-23
**Status**: Draft
**Deciders**: pelikhan, Copilot coding agent [TODO: verify]

---

### Context

Claude Code release `2.1.313` has been identified as unsafe. Workflows can still pick it up through an explicit `engine.version` override (including spellings such as `v2.1.313`, `=2.1.313`, or zero-padded components), through a threat-detection engine override, or indirectly through dynamic npm selectors (dist-tags, ranges, or `${{ }}` expressions) that resolve to it at install time. Without a guard, npm would execute that release's lifecycle scripts before any later check could stop it. Separately, `gh aw compile --dependabot` only scanned `npx`/`pip install`/`go install` invocations, so the agentic engine CLIs themselves (Copilot, Claude, Codex, Gemini, Pi, and imported engine definitions) were invisible to Dependabot, leaving no automated signal when an engine version needs an update.

### Decision

We will enforce the block in two layers. At compile time, `validateBlockedEngineVersions` normalizes the effective Claude version (default or override, for both the main engine and the threat-detection engine) and rejects `2.1.313` with a validation error. At run time, whenever the Claude version is not a literal, unblocked semver, the generated npm install step first resolves the selector with `npm view ... version --json` and fails if it resolves to the blocked release, before `npm install` runs any lifecycle scripts. We will also compute each workflow's effective engine installations (agent and detection engine, using the same configuration inheritance as execution) and add those packages to the generated npm, pip, and Go Dependabot manifests, preserving multiple npm versions of the same package through `npm:` aliases (`gh-aw-engine-<sha256>`).

### Alternatives Considered

#### Alternative 1: Compile-time validation only

Reject the blocked version only when it appears literally in frontmatter. This is simpler and adds no shell to the generated lock files, but it does not cover dist-tags, semver ranges, or runtime expressions, which the compiler cannot resolve statically. It was not chosen because a selector such as `latest` or `^2.1.0` could still install the blocked release and execute its lifecycle scripts.

#### Alternative 2: Runtime guard only (no compile-time error)

Rely solely on the pre-install `npm view` check. This covers every selector form, but users with a literal blocked pin would only learn about the problem when a workflow run fails, and the threat-detection override would also fail late. It was not chosen because a compile-time error gives faster, clearer feedback for the common literal-pin case.

#### Alternative 3: Track engine versions outside Dependabot manifests

Keep engine versions out of the generated `package.json`/`requirements.txt`/`go.mod` and rely on compiler upgrades to move default pins. This avoids alias entries and manifest noise, but leaves user overrides and imported engine definitions without any automated update signal. It was not chosen because the PR's goal is to give engine packages the same Dependabot visibility as other runtime tools.

### Consequences

#### Positive
- The blocked Claude release cannot be selected by literal override, equivalent spellings, threat-detection override, or a dynamic selector, and its lifecycle scripts cannot run.
- Engine CLIs, both built-in and imported, at default or overridden versions are now visible to Dependabot alongside other runtime dependencies.
- npm aliasing preserves distinct engine versions used by different workflows or MCP tools instead of silently collapsing them.

#### Negative
- The blocked version is hardcoded in two places (a Go regex and an inline Node.js check in the generated shell), so adding or removing blocked versions requires coordinated code changes and recompiling every affected workflow.
- Guarded Claude install steps now require a network `npm view` call and inline Node.js, which makes the generated lock files longer and adds a runtime dependency on registry metadata.
- Python and Go manifests keep last-version-wins semantics because they have no alias mechanism, so conflicting engine versions across workflows are not all tracked.
- Runtime expressions and custom `engine.command` overrides cannot be represented statically and are omitted from manifests. The PR notes that an external detector's standard Copilot install is omitted when a custom Copilot command is configured.

#### Neutral
- Updating a generated manifest does not change the installed engine; users must change `engine.version` (or upgrade the compiler) and recompile.
- Engine installation dependencies are computed once during `ParseWorkflowFile` and cached on `WorkflowData`.
- `parseNpmPackage` was simplified with `strings.Cut`, with no intended behavior change.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
