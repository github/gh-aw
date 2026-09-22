# ADR-62755: Stage external threat detection for ARC/DinD

**Date**: 2026-09-22
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

External threat detection currently assumes the runner process and Docker daemon share the same filesystem view for the threat-detection working directory and installed `threat-detect` binary. The PR description and diff show this assumption breaks on ARC/DinD runners, where the daemon sees only the shared `RUNNER_TEMP` volume rather than the runner's private root filesystem. This PR updates workflow compilation and supporting shell logic so the detector binary and prepared inputs are staged onto the shared volume, the detection directory is mounted read-write at the daemon-visible path, and detector outputs are collected back before existing artifact and conclusion steps run. The decision is whether to keep the current direct host-path execution model or introduce explicit staging and collection for ARC/DinD topologies.

### Decision

We will stage external threat-detection inputs and the `threat-detect` binary under `${RUNNER_TEMP}/gh-aw` for ARC/DinD executions, mount the staged detection directory read-write into AWF, and collect only detector output files back into the canonical host-side detection directory after execution. gh-aw will preserve existing non-ARC behavior and leave existing conclusion semantics unchanged while compiling ARC/DinD detection runs to use rewritten shared-volume paths. We chose this approach because it directly addresses the split-filesystem constraint visible in the PR while preserving downstream consumers that still read the canonical host-side detection directory.

The ARC/DinD detector installation uses `--rootless`. The installer publishes the exact verified executable path as a step output; staging consumes that path instead of selecting an executable from `PATH`. Codex configuration is prepared inside the detection directory and uses a writable home under the shared detection mount. The parent runtime mount remains read-only.

An unconditional reset step removes inherited host-side result files and clears the dedicated shared staging directory before preparation and installation. This prevents a failed or cancelled installation from leaving an old verdict for downstream consumers. Staging clears the shared directory again before copying current inputs, including optional files, and collection explicitly requires installation success and a non-skipped execution. Neither cleanup nor collection manufactures a verdict or changes conclusion policy.

### Alternatives Considered

#### Alternative 1: Keep using the canonical host detection directory directly inside ARC/DinD runs

This was the pre-change behavior and is the simplest model because it avoids new staging scripts and post-processing steps. It was not chosen because the PR evidence shows ARC/DinD runners do not share the runner root filesystem with the Docker daemon, so the detector binary and prepared inputs are not reliably visible at the canonical host path during AWF execution.

#### Alternative 2: Change all threat-detection consumers to read and write only from the shared `RUNNER_TEMP` path

This was a realistic alternative because it would avoid the collect-back step and make the shared-volume path the sole source of truth for ARC/DinD runs. It was not chosen because the PR explicitly preserves existing artifact, log, execution-evidence, and conclusion consumers at the canonical detection directory, and the diff adds collection logic that copies only detector outputs back without overwriting host-owned files.

### Consequences

#### Positive
- External threat detection now works on ARC/DinD topologies where the runner and Docker daemon do not share the same root filesystem.
- Existing host-side consumers can continue reading logs, execution evidence, and detection outputs from the canonical detection directory.
- The compiled workflow keeps conclusion ordering and `continue-on-error` behavior unchanged while adding ARC/DinD-specific staging and collection.

#### Negative
- The implementation becomes more complex because compilation now depends on staging scripts, ARC-specific mount rewriting, and a post-execution collection step.
- ARC/DinD detection runs now depend on the shared `RUNNER_TEMP` volume being available and writable for both staged inputs and collected outputs.
- The system must carefully avoid stale or unsafe files, which adds cleanup and copy constraints to the shell helper.

#### Neutral
- Non-ARC threat-detection execution remains on the existing direct-path behavior and does not use the new staging helper.
- New regression tests cover compiler output and shell-level round trips for success, failure, spaced paths, stale-result and optional-input cleanup, verified binary selection, and rejected symlink directories.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
