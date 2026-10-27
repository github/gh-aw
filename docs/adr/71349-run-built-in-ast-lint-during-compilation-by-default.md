# ADR-71349: Run Built-in Actions/AW AST Lint During Compilation by Default

**Date**: 2026-10-23
**Status**: Accepted
**Deciders**: pelikhan

---

### Context

gh-aw has an in-process Actions/AW AST lint registry (see [ADR-69813](69813-in-process-ast-lint-registry-with-secret-leak-taint-analysis.md) and [ADR-70026](70026-map-ast-lint-rules-to-compiler-threat-ids.md)) that includes a `template-injection` rule. Until this PR, that registry ran only when someone invoked `gh aw lint` separately, so `gh aw compile` could emit lock files containing patterns the linter would have flagged. The trigger was the Copilot smoke workflow. It sent an agent-generated message to `haiku-printer`, which interpolated that message straight into Bash. Shell substitutions or injected syntax could then run on the downstream runner, and the existing AST rule would have caught it if compilation had run the linter. Other compile-time analyzers are mostly opt-in flags (`--zizmor`, `--poutine`, `--actionlint`). Shellcheck is already default-on ([ADR-49762](49762-default-on-shellcheck-for-run-step-linting-in-compile-pipeline.md)).

### Decision

The built-in Actions/AW AST lint registry runs as a default step of CLI compilation. The step runs inside `generateAndValidateYAML`, after YAML generation and before lock-file emission, and it also runs with `--no-emit`. It checks both the generated Actions graph and the authored AW source document. Error-severity diagnostics fail compilation. Warnings go to stderr and count toward the warning total, except the diagnostics excluded below. The step honors active `threat-detection` suppressions. Users can opt out with `--no-lint` (MCP: `skip_lint`). The opt-out disables only this lint pass, not compiler validation or requested external scanners. Dry-run compilation rejects it. Low-level Go callers keep their existing opt-in behavior through `workflow.WithLint(true)`. The main reason for this decision is security: injection findings should block lock-file generation instead of depending on a separate, easily forgotten lint run.

The default also applies whenever `add`, `update`, or `validate` recompiles workflows, including both direct and file-tracked add compilation. Only `compile` exposes `--no-lint`; users must fix findings before using other recompiling commands.

For migration, run `gh aw compile --no-emit` to identify findings, correct the authored workflow source, then rerun the original command. `gh aw compile --no-lint` is a temporary compatibility escape hatch, not a persistent setting or a way to bypass lint in `add`, `update`, or `validate`.

Compile-time lint is Actions/AW AST-only and does not introduce a mandatory native JavaScript parser dependency. Deeper authored JavaScript driver and custom-grader checks require standalone `gh aw lint --javascript-module <module-path>`. Warning-level `secret-leak` diagnostics and unavailable-parser `grader-file-io` coverage warnings are hidden during compilation: neither is printed or counted toward compiler warning totals, and neither blocks compiler dry-run. Successful compilation is not evidence of complete JavaScript analysis.

### Alternatives Considered

#### Alternative 1: Keep linting opt-in via `--lint` or standalone `gh aw lint`

This matches the existing opt-in scanner flags and avoids a breaking change. It was rejected because the haiku-printer incident shows that opt-in analysis gets skipped in practice. The rule that would have caught the injection already existed but never ran during compilation.

#### Alternative 2: Fold the lint rules into existing compiler validation (no opt-out)

Making the AST rules part of mandatory compiler validation would give the strongest guarantee. It was not chosen because lint rules change faster and have a higher false-positive risk than core validation. Users need a narrow escape hatch (`--no-lint`) that leaves the core compiler safety checks in place.

#### Alternative 3: Enable linting by default in the low-level `workflow.Compiler` API as well

This would give library callers the same protection, but would also change every existing library caller's compilation behavior without an explicit choice. The decision preserves that API boundary: default-on applies in the CLI/MCP setup path (`createAndConfigureCompiler`) and direct add compiler constructions, while Go callers opt in with `WithLint(true)`.

### Consequences

#### Positive
- Injection and other AST lint errors now block lock-file generation, closing the gap that let the haiku-printer injection through.
- `--no-emit` and MCP compile also run the lint, so validation-only flows get the same coverage.
- The opt-out is narrow and auditable. It cannot be used with dry-run, and it does not weaken compiler validation or external scanners.

#### Negative
- This is a breaking (major) change. Workflows that compiled before may now fail until their findings are fixed. `compile` alone offers `--no-lint`; other recompiling commands require fixes.
- Compilation does a bit more work: it parses the generated YAML into an Actions AST and runs the rule registry for every workflow.
- CLI and Go API defaults now differ (CLI on, `workflow.Compiler` off), which can surprise library callers.

#### Neutral
- Native Actions YAML and downstream dispatch targets still need standalone `gh aw lint`. Compilation does not lint dispatch targets recursively.
- Actions AST parse/emit had to keep `cache-mode`, and authored-environment analysis had to accept sandbox shorthands, so that the default lint runs cleanly on existing workflows.
