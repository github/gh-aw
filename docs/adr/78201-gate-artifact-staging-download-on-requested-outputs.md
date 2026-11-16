# ADR-78201: Gate Artifact Staging Download on Requested Safe Outputs and Define a Shared Browser-Need Contract

**Date**: 2026-11-14
**Status**: Draft
**Deciders**: Unknown (AI-drafted from PR #78201 by pelikhan; human deciders [TODO: verify])

---

### Context

The Daily Cracktro workflow failed required browser validation because its instructions and scoped tool permissions did not cover the full preview lifecycle (launch, loopback readiness check, screenshot capture, upload). Separately, the safe-outputs job always downloaded the upload-artifact staging artifact whenever `upload_artifact` or a connector script with staged-file policies was configured. Runs that never requested a screenshot or file output tried to download a staging artifact that was never created, which produced misleading audit errors. Any fix had to keep the fixed preview launcher, the loopback checks, and the existing validation and security controls.

### Decision

We will make the compiled staging download step conditional: it runs only when the agent output contains an `upload_artifact` item or an item for a connector script that declares staged-file policies. The compiler derives these types through a new `safeOutputsArtifactStagingTypes` helper and emits an `if:` condition for them. We will also add a shared browser-need decision and readiness contract in `.github/aw/playwright.md`, linked from the workflow design, creation, update, validation and debugging guidance, and have the Cracktro preview write screenshots into mounted upload staging. Custom-action resolution errors will now propagate instead of being swallowed. The primary driver is removing false-positive failures without weakening existing controls.

### Alternatives Considered

#### Alternative 1: Keep the unconditional download and ignore its failure

The step already uses `continue-on-error: true`, so the failure could be tolerated and filtered in audit tooling. This is the smallest change, but the audit errors would stay and tooling would need to learn to ignore them. It would also hide real staging failures when output was requested.

#### Alternative 2: Handle the missing artifact inside the handler manager

The JavaScript handler could detect a missing staging directory and skip silently. This keeps the compiled YAML unchanged, but the runner would still attempt a download and log an error, and the decision would be less visible in the compiled lock file.

#### Alternative 3: Fix only the Cracktro workflow instructions

Update only `daily-cracktro-maintainer.md` with the extra permissions and steps. This is narrower, but other workflows that use the browser would hit the same gap, so it was not chosen.

### Consequences

#### Positive
- Runs without artifact or connector file outputs no longer try to download a nonexistent staging artifact, so audit logs have fewer misleading errors.
- The staging download condition is visible in the compiled lock files.
- Browser-need guidance is shared across authoring, update and debug flows, and static-only tasks stay browser-free.
- Custom-action resolution failures now fail compilation instead of passing silently.

#### Negative
- About 16 lock files change (one added line each), which adds review noise and ties them to the compiler change.
- `uploadArtifactStagingDownloadStepCount` must stay in sync with the emitted step slice, which is a fragile coupling.
- The staging condition depends on normalized script names matching the safe-output type names emitted by the agent.

#### Neutral
- The download step keeps `continue-on-error: true`.
- Guidance tests (`test_guidance.py`) now assert the shared contract is linked from the relevant documents.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
