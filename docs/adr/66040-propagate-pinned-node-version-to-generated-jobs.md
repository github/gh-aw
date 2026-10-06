# ADR-66040: Propagate the Pinned Node.js Runtime Version to Every Generated Job

**Date**: 2026-10-06
**Status**: Draft
**Deciders**: gh-aw maintainers (pending review; PR author: app/copilot-swe-agent)

---

### Context

gh-aw compiles a single workflow markdown file into several GitHub Actions jobs (agent, threat `detection`, `evals`). Only the agent job goes through `DetectRuntimeRequirements` / `applyRuntimeOverrides`, so only it honored `runtimes.node.version` from frontmatter; the detection and evals jobs emitted the compiler default `constants.DefaultNodeVersion` (`node-version: '24'`). Issue #65939 reported that a repository pinning `runtimes.node.version: "24.21.0"` therefore ran different Node.js versions across jobs of the same workflow, and the documented workaround (declaring a custom `Setup Node.js` step under `safe-outputs.threat-detection.steps`) fails compilation with `duplicate step 'Setup Node.js' found in job 'detection'`. A prior change (#64498) had already established a precedent for this class of problem by post-processing the generated `Setup Node.js` step to honor `runtimes.node.action-repo` / `action-version`, but it explicitly left `version` out of scope. This PR touches `pkg/workflow/` with ~123 added lines in business-logic directories across 7 files, most of which are regression tests.

### Decision

We will extend the existing post-generation rewrite path rather than thread a version parameter through runtime detection: `rewriteNodeSetupStepUses` becomes `rewriteNodeSetupStep`, which rewrites both the `uses:` line and the `node-version:` line of a `Setup Node.js` step, and `applyNodeSetupActionOverride` becomes `applyNodeSetupOverrides`, applied to engine-bundled installation steps in the detection and evals jobs. A new `resolveNodeSetupVersion(data)` resolves the version with the same precedence already used for the action override: the merged `Runtimes["node"]` map wins outright (returning empty — i.e. the default — when it omits `version`), and typed frontmatter (`ParsedFrontmatter.RuntimesTyped.Node.Version`) is consulted only when no merged `node` entry exists. Non-string scalars are normalized via `fmt.Sprint`, so `version: 22` and `version: "22"` behave identically. The primary driver is consistency with the already-shipped action-override mechanism, which keeps the fix localized to `pkg/workflow/nodejs.go` and its three call sites.

### Alternatives Considered

#### Alternative 1: Pass the resolved version into `GenerateNodeJsSetupStep()`

This is the approach suggested in issue #65939: give the generator a version argument so the step is emitted correctly the first time instead of being rewritten afterwards. It is arguably the cleaner design and was a close call. It was rejected for this change because `GenerateNodeJsSetupStep()` is called from many sites that do not have a `*WorkflowData` in hand, and because engine-bundled install steps (`engine.GetInstallationSteps`) emit their own `Setup Node.js` step that the generator never produces — those would still require a rewrite pass, leaving two mechanisms instead of one.

#### Alternative 2: Route the detection and evals jobs through `DetectRuntimeRequirements` / `applyRuntimeOverrides`

Making the auxiliary jobs share the agent job's runtime-resolution pipeline would fix the version mismatch structurally and prevent the whole class of divergence (including future runtimes such as Python or Go). It was rejected as out of scope and too risky for a bug fix: those jobs deliberately bypass runtime detection because they run a fixed, engine-specific toolchain on a fresh runner, and routing them through the full pipeline would change their emitted steps broadly and churn every generated `.lock.yml`.

#### Alternative 3: Allow a user-declared `Setup Node.js` step in `threat-detection.steps`

Relaxing the duplicate-step validation would let authors override the version themselves. Rejected because it pushes a compiler-level consistency guarantee onto every workflow author, does not help the `evals` job, and weakens a validation rule that exists to prevent genuinely conflicting steps.

### Consequences

#### Positive
- A workflow that pins `runtimes.node.version` now emits the same `node-version` in the agent, `detection`, and `evals` jobs, closing #65939.
- The fix reuses an established mechanism, so action-repo and version overrides share one resolution precedence and one rewrite helper — fewer places to diverge later.
- Regression coverage was added for all three engines (`copilot`, `claude`, `codex`), for merged-vs-typed precedence, for numeric versions, and for engine-bundled installer steps.

#### Negative
- The design remains a string-level post-processing pass over generated YAML (matching `uses:` / `node-version:` line prefixes), which is brittle to formatting changes in `Setup Node.js` steps and must be kept in sync with every engine's bundled installer.
- Only Node.js is covered. Other runtimes (Python, Go, uv, …) still diverge between the agent job and the auxiliary jobs, so the underlying structural gap identified in Alternative 2 persists as technical debt.
- The "merged `Runtimes` map wins outright" precedence means a merged `node` entry that sets only `action-version` silently resets the typed `version` back to the default — intentional and tested, but surprising to read.

#### Neutral
- The debug log line that previously announced the action override was dropped when the helper was generalized.
- Renaming `applyNodeSetupActionOverride` → `applyNodeSetupOverrides` and `rewriteNodeSetupStepUses` → `rewriteNodeSetupStep` is internal to `pkg/workflow`; no exported API changes.
- Existing workflows that do not set `runtimes.node.version` keep emitting `constants.DefaultNodeVersion`, so no generated lock files change by default.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
