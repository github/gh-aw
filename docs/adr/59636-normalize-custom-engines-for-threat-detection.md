# ADR-59636: Normalize custom engines for threat detection

**Date**: 2026-09-09
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

Threat detection in gh-aw accepted workflows that used custom engines, but the external `threat-detect` invocation still passed the workflow engine ID directly to `--engine`. The PR description states that `threat-detect` only accepts built-in engines, so custom-engine workflows ended with `THREAT_DETECTION_STATUS: reason=config_error` while the detection job stayed green because it was `continue-on-error`. This PR changes workflow compilation, engine definition schema, tests, and documentation so custom engines can still run threat detection by resolving to a supported built-in detector. The repository needs an explicit decision for how custom workflow engines should participate in threat detection without silently skipping analysis.

### Decision

We will normalize any custom workflow engine used for threat detection to a supported built-in detection engine, defaulting to `copilot` when no explicit override is provided. We will allow engine definitions to declare a `detection-engine`, and workflows may still override that with `safe-outputs.threat-detection.engine`. When normalization occurs, threat detection will use the selected built-in engine's own model defaults rather than inheriting an incompatible custom-provider model, and the compiler will warn when it has to fall back implicitly.

### Alternatives Considered

#### Alternative 1: Continue passing the workflow engine ID directly to threat detection

The project could keep using the workflow's engine ID as the detector engine, even for custom engines such as `aider`, `opencode`, or `pydantic-ai`. This was the simplest behavior because it required no mapping layer or schema changes. It was not chosen because the PR evidence shows this causes `config_error` for custom engines, leaving runs green but unanalysed.

#### Alternative 2: Disable threat detection for all custom engines unless users configure it manually

Another option would be to require every custom-engine workflow author to set `safe-outputs.threat-detection.engine` explicitly or turn detection off. This was considered because it avoids guessing a fallback engine and keeps engine behavior explicit. It was not chosen because it would preserve the current footgun for existing workflows and create repetitive per-workflow configuration where a safe default can be supplied centrally.

#### Alternative 3: Add built-in detector support for every custom engine ID

The project could attempt to teach threat detection to accept each custom engine identifier directly. This was considered because it would preserve one-to-one naming between workflow engines and detector engines. It was not chosen because the detector only supports built-in engines, and the PR instead introduces a simpler compatibility layer plus a declarative `detection-engine` field.

### Consequences

#### Positive
- Custom-engine workflows continue to receive threat detection instead of silently ending with `config_error`.
- Engine authors can declare a working default detector once via `detection-engine`, reducing per-workflow configuration.
- Compile-time warnings make fallback behavior visible when credentials or engine compatibility might otherwise be surprising.

#### Negative
- Threat detection may run on a different engine than the workflow's primary execution engine, which can confuse users without clear documentation.
- The compiler and runtime must maintain normalization and precedence rules between workflow overrides, engine definitions, and defaults.
- Detection results may differ from the custom engine's behavior because analysis is delegated to a built-in engine such as `copilot`.

#### Neutral
- Engine definitions and frontmatter schema now include a `detection-engine` key.
- Detection model inheritance depends on whether the engine was normalized, so built-in detector defaults are used in more cases.
- Generated lockfiles change where frontmatter hashes capture the new detection-engine declarations.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
