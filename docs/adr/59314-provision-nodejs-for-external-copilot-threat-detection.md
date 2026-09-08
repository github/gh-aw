# ADR-59314: Provision Node.js for external Copilot threat detection

**Date**: 2026-09-08
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

External Copilot threat-detection jobs currently install the AWF binary and then the GitHub Copilot CLI, but some runners do not provide a usable ambient Node.js runtime inside the chroot. The PR description identifies startup failures on ARC and DinD environments when that implicit runtime assumption does not hold. This change updates compiled detection jobs to stage Node.js before Copilot CLI installation, while preserving existing deduplication for engines that already provision Node.js and propagating inherited custom engine commands into the detector context. The repository needs an explicit decision on whether external Copilot-based threat detection should treat Node.js as an explicitly provisioned runtime dependency instead of relying on ambient runner state.

### Decision

We will explicitly provision Node.js in external Copilot threat-detection jobs before installing the GitHub Copilot CLI. We will rely on the shared tool-cache redirection and daemon-visible staging flow already used for ARC and DinD compatibility, and we will keep setup deduplication so Claude, Codex, and custom-command configurations do not receive redundant Node provisioning or unwanted standard Copilot CLI installation. This makes the detector runtime self-contained and consistent across runner environments where ambient Node.js availability is unreliable.

### Alternatives Considered

#### Alternative 1: Continue relying on ambient runner Node.js installations

The project could keep threat-detection workflows dependent on whatever Node.js runtime happens to be available on the host runner. This was considered because it keeps compiled jobs shorter and avoids another setup action in the detector pipeline. It was not chosen because the PR explicitly fixes startup failures caused by missing or unusable ambient Node.js inside chrooted ARC and DinD environments.

#### Alternative 2: Bundle or stage Node.js indirectly through Copilot CLI installation only

Another option would be to leave runtime provisioning implicit within Copilot CLI installation scripts or binary staging instead of adding an explicit setup step. This was considered because it could hide the runtime dependency behind existing installation logic. It was not chosen because the diff shows the failure mode is specifically about runtime availability before Copilot starts, and the PR also needs setup ordering guarantees plus deduplication across other engines and custom commands.

#### Alternative 3: Require all detector engines to provision Node.js uniformly

The workflows could inject Node.js setup into every detector engine path regardless of whether the engine already provisions or requires it. This was considered because it would create a single uniform runtime bootstrap pattern. It was not chosen because the PR description and regression tests specifically preserve deduplication and verify that Claude and Codex do not receive duplicate Node setup.

### Consequences

#### Positive
- External Copilot threat-detection jobs become resilient on ARC, DinD, and other environments where ambient Node.js is unavailable inside the execution boundary.
- Runtime setup ordering becomes explicit and testable, reducing detector startup regressions.
- Custom detector commands can inherit the same runtime assumptions without forcing installation of the standard Copilot CLI when a custom command is configured.

#### Negative
- Compiled workflows gain another setup action and corresponding manifest updates, increasing workflow size and maintenance surface.
- The compiler must maintain deduplication logic so Node.js setup is added only where appropriate.
- Future changes to Node runtime expectations now require coordinated updates across detector generation and regression coverage.

#### Neutral
- Threat-detection job compilation now treats Node.js as a first-class runtime dependency for external Copilot-based detectors.
- Generated lockfiles and manifests will change whenever this setup behavior changes.
- Validation of detector behavior now includes runtime bootstrap ordering and custom-command inheritance scenarios.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
