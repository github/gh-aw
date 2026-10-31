# ADR-73541: Track Agentic Engine Packages in Dependabot Manifests at Effective Versions

**Date**: 2026-10-31
**Status**: Draft
**Deciders**: pelikhan, Copilot (PR #73541)

---

### Context

`gh aw compile --dependabot` scans workflows for runtime tool invocations (`npx`, `pip install`, `go install`) and generates `package.json`, `requirements.txt`, and `go.mod` manifests plus `.github/dependabot.yml`. Agentic engines (Copilot CLI, Claude Code, Codex, Gemini CLI, Pi, declarative engines, threat-detection engines, and Copilot SDK packages) are installed by compiler-generated steps rather than authored commands, so they were absent from those manifests and their releases and security advisories went unmonitored. The installed engine version is not a single fixed value: it can come from a workflow's `engine.version` override, a compiler default, declarative engine installation metadata, or an imported engine definition. That definition can be lost when the batch compiler resets its engine catalog between workflows. Some versions are dynamic GitHub Actions expressions (`${{ ... }}`) that cannot be pinned in a manifest.

### Decision

We will add engine packages to the existing Dependabot manifests through a manifest-only extractor (`extractEnginePackages` in `pkg/workflow/dependabot_engines.go`). The extractor resolves each workflow's *effective* installed version: `engine.version` when set, otherwise the declarative installation version or compiler default. Built-in engines without declarative installation metadata map to known npm package names, with Copilot's release binary represented by `@github/copilot`. Detection engines and Copilot SDK packages (whose SDK version is pinned independently of `engine.version`) are also included. Packages whose name or version contains `${{` are excluded. To keep imported engine metadata available during batch compilation, `WorkflowData` stores the resolved `EngineDefinition` per workflow. Engine packages are prepended to the existing per-ecosystem dependency lists, while package validation for `npx` stays unchanged.

### Alternatives Considered

#### Alternative 1: Keep engines out of the generated manifests and document manual tracking

Users could add engine packages to their own manifests or watch engine releases manually. This avoids compiler changes but leaves the main gap in place: compiled workflows can install vulnerable or outdated engine releases without any Dependabot alert. Manually maintained entries would also drift from the version the compiler actually installs.

#### Alternative 2: Always track the gh-aw compiler's default engine versions, ignoring workflow overrides

A simpler implementation would emit one fixed entry per engine using `constants.Default*Version`. This was rejected because workflows that pin `engine.version` would get alerts for a version they do not run and miss alerts for the version they do run. It would also ignore declarative and imported engine definitions.

#### Alternative 3: Feed engine installations into the general command-extraction pipeline (e.g., synthesise `npx`/`pip install` commands)

Treating engine installs as ordinary authored commands would reuse existing extractors with little new code. It would also widen the npx package-validation surface and could change existing behavior. The PR explicitly keeps engine extraction manifest-only for that reason [TODO: verify whether this was weighed as a distinct option].

### Consequences

#### Positive
- Dependabot now covers engine releases and security alerts for npm, pip, and Go engines, including detection engines and Copilot SDK packages.
- Manifest versions match what the workflow actually installs, because they honor `engine.version`, compiler defaults, and declarative or imported engine metadata.
- Existing command-derived dependencies and npx validation behavior are preserved.

#### Negative
- Every workflow with a package-based engine now produces a `package.json`, `package-lock.json`, and `dependabot.yml`, even when it uses no authored tools. This adds generated files and Dependabot PR volume.
- `engineInstallationForManifest` hard-codes npm package names for built-in engines, so this mapping must be updated whenever an engine is added or renamed.
- Copilot is installed as a release binary but tracked through its npm package, which is an indirect proxy for the installed artifact.
- Storing `engineDefinition` on `WorkflowData` and cloning the engine catalog to resolve detection engines adds coupling to compiler-internal state.

#### Neutral
- Engines whose versions use dynamic `${{ }}` expressions, and engines run with a custom `engine.command`, are not represented in manifests.
- To resolve an engine alert, users update `engine.version` or upgrade gh-aw, then rerun `gh aw compile --dependabot`. SDK versions change only through gh-aw upgrades.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
