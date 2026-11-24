# ADR-80815: Bundle JavaScript threat detection in compiled workflows

**Date**: 2026-11-24
**Status**: Draft
**Deciders**: gh-aw maintainers (inferred from PR #80815 and linked issue #79610; to confirm)

---

### Context

Compiled threat-detection jobs currently run the external `threat-detect` Go binary by default. Its installer downloads a release and verifies it against version and digest pins, and updating those pins requires `scripts/update-threat-detect-pins.sh`. On ARC/DinD runners the Docker daemon cannot see the runner's private filesystem, so the binary and detection inputs must be staged onto the shared `RUNNER_TEMP` volume (see ADR-62755).

Linked issue #79610 asks to switch the default compiled detection job to the JavaScript detector shipped with the gh-aw setup action. The sandbox runs detection, and the host consumes the staged verdict through JS conclude. The issue also asks to keep the current effective engine selection, preserve isolation of network, credentials, model routing and AI credits, prevent a stale verdict from surviving skipped or failed runs, and remove detector-specific release downloads, pins and update machinery.

PR #80815 implements that cutover. Its changeset is marked `major` because it removes `safe-outputs.threat-detection.artifact-base-url`. Whether external consumers depend on that field beyond the changeset note is not inferable from current PR evidence.

### Decision

Compiled detection jobs run the JavaScript detector bundled with the setup action inside AWF by default. The `gh-aw-detection` feature flag selects the bundled JavaScript detector when it is absent or `true`. Setting it to `false` keeps the legacy inline engine path. The host reads the staged verdict through JS conclude.

The external detector's release download, install-step outputs, version and digest pins, `install_threat_detect_binary.sh`, and `scripts/update-threat-detect-pins.sh` are removed. Affected workflow locks are regenerated. Workflows that set `safe-outputs.threat-detection.artifact-base-url` must remove it before recompiling. Detection engine version and checksum settings are unchanged.

We chose this approach because it removes the release-pin and installer maintenance path. The ARC/DinD binary-staging problem also no longer applies to a separately downloaded executable. The PR adds `stage_threat_detection_runtime.cjs` for runtime staging, which was not reviewed in depth for this ADR.

### Alternatives Considered

#### Alternative 1: Keep the external `threat-detect` binary as the default

This is the pre-PR behavior. It keeps independent detector release versioning and checksum verification, and it keeps the ARC/DinD binary-staging path from ADR-62755. It was not chosen because the installer, digest pins and update script are the maintenance burden that issue #79610 targets. The binary also has to be staged across the split ARC/DinD filesystem.

#### Alternative 2: Keep the external binary as the default and make bundled JavaScript opt-in

This would limit blast radius and avoid the breaking `artifact-base-url` removal. It was not chosen because issue #79610 asks for a default cutover with smoke evidence. An opt-in path would leave both detectors and both pin systems in place indefinitely.

### Consequences

**Positive**

- There is no separate detector release download, digest pin or updater script to maintain. The JavaScript detector ships with the setup action.
- Detection no longer depends on placing a separately downloaded executable on the shared ARC/DinD volume.
- The host conclusion consumes a recorded verdict. Copilot, Claude and Codex smoke workflows now require a clean recorded verdict rather than a green warn-mode job.

**Negative**

- This is a breaking change. Workflows that set `safe-outputs.threat-detection.artifact-base-url` must remove it before recompiling, and 327 `.lock.yml` files change.
- The detector version is now tied to the setup action release rather than separately pinned.
- Live evidence is outstanding. The PR body states that no live model-backed Actions run or ARC DinD run has been observed for the assessed revision.
<parameter name="path">/home/runner/work/gh-aw/gh-aw/docs/adr/80815-bundle-javascript-threat-detection-in-compiled-workflows.md