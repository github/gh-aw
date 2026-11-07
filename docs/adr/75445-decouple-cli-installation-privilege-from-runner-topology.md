# ADR-75445: Decouple CLI Installation Privilege from Runner Topology

**Date**: 2026-11-06
**Status**: Proposed
**Deciders**: gh-aw maintainers (review pending)

---

### Context

Native self-hosted runners may lack sudo or a writable `/usr/local/bin`, even when they do not use ARC/DinD. Selecting rootless Copilot CLI and threat-detect installation solely through `runner.topology: arc-dind` conflates installation privilege with Docker networking, socket, and shared-volume behavior. Requiring a topology override to install without sudo would enable unrelated runtime behavior.

Existing workflows must retain their installation defaults, and the external threat detector must execute the verified installed binary rather than a binary found incidentally on PATH or under the sandbox's HOME.

### Decision

Add `runner.installation: rootless | privileged`, resolved by the shared `useRootlessCLIInstallation` policy for Copilot CLI and threat-detect. An explicit setting takes precedence. When omitted, ARC/DinD remains rootless and other topologies remain privileged. Reject privileged installation with ARC/DinD during compilation because that topology requires binaries on its shared volume.

For native rootless external detection, validate that `THREAT_DETECT_BINARY` names a regular, executable, non-symlink file, then stage it in `${RUNNER_TEMP}/gh-aw/bin` before Copilot host setup. Remove any stale destination before copying and prepend the mounted directory to the detector's container PATH. Keep release versions, digest verification, HTTPS requirements, and platform checks unchanged.

This setting controls installer privilege only. It does not grant agent sudo access, change sandbox isolation, configure Docker access, or remove the runner's existing sandbox prerequisites.

### Alternatives Considered

#### Alternative 1: Use ARC/DinD topology to select rootless installation

This reuses an existing setting but also changes networking and shared-volume behavior on native runners. Installation privilege is independent of those choices.

#### Alternative 2: Detect sudo availability at runtime

Automatic fallback would make installation behavior depend on incidental host state rather than authored configuration. An explicit policy provides deterministic compilation and actionable validation errors.

#### Alternative 3: Always install rootlessly

This simplifies policy selection but changes established defaults and paths for existing workflows. Preserving defaults avoids requiring unrelated migrations.

### Consequences

#### Positive

- Native self-hosted runners can opt into no-sudo installation without enabling ARC/DinD behavior.
- Copilot and threat-detect use one privilege policy, and external detection uses the verified binary independently of sandbox HOME.
- Existing workflows retain their default behavior and release-integrity checks.

#### Negative

- Authors must select rootless installation explicitly on native runners that cannot support privileged installation.
- Native rootless external detection requires an additional validated copy into the mounted job directory.

#### Neutral

- Rootless CLI installation does not make the entire workflow rootless; Docker, firewall, and sandbox prerequisites still apply.
- Regression tests cover policy defaults, invalid settings, staging order, invalid detector rejection, privileged installation, and no-sudo installation.
