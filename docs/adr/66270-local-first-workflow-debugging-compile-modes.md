# ADR-66270: Support Local-First Workflow Debugging with Compile-Time `--dev` and `--environment`

**Date**: 2026-10-06
**Status**: Proposed
**Deciders**: gh-aw maintainers (pending review; PR author: pelikhan)

---

### Context

Only a hosted run exercises the real lock file, approvals, secrets, and runner behavior together. That loop requires repository dispatch permission and can expose production resources to unreviewed changes. Local component reproduction provides useful evidence without claiming hosted-run fidelity. Meanwhile, `gh aw compile` exposes strictness, staging, and scanner checks as independent flags that callers can accidentally omit. The constraint is that local debugging must not pretend to reproduce GitHub Actions credentials, approval gates, or runner semantics.

### Decision

Use local compilation and bounded component reproduction as the first debugging steps. `gh aw compile --dev` enables strict validation, staging, image/model/analysis checks, and all scanners, rejecting bypass options and explicitly disabled required flags. Warnings become failures; workflow-local diagnostics remain attached to their workflow, while batch diagnostics are reported separately. Development compilation disables `push_` jobs and conclusion issue/comment reporting without removing diagnostic handlers, summaries, or usage artifacts. It adds no memory-tool staging fields.

`gh aw compile --environment NAME` replaces the environment on every generated job, including approval jobs, with a validated literal name. Reusable-workflow caller jobs fail explicitly because GitHub Actions forbids an environment on them. Installed and embedded debugging instructions share `.github/aw/debug-agentic-workflow.md`, which unifies local diagnosis/patching, evidence triage, and bounded, human-validated live debugging. Neither flag authorizes workflow execution.

### Alternatives Considered

#### Alternative 1: Keep composing the existing individual compile flags and document the recipe

The strict combination (`--strict --staged --validate --validate-images --zizmor --poutine --syft --grype --yamllint --shellcheck --models …`) already existed; we could have written it down in `compilation-process.md` and stopped there. This was a genuine close call because it adds no new API surface and no new failure modes. It was rejected because a documented recipe has no enforcement: nothing prevents a caller — in particular the compile MCP tool invoked by an agent — from dropping a scanner or re-enabling `--allow-action-refs` while still claiming a "dev" compile. Making the mode a first-class flag that *rejects* bypass options is the only version that holds under agent-driven invocation.

#### Alternative 2: Build a local runner/emulator for agentic workflows (act-style)

Locally executing the generated lock file could exercise runtime behavior that compilation cannot see. This approach was excluded from this design: partial emulation does not establish equivalence for hosted token minting, environment approvals, shared secrets, or runner behavior. Prefer native component execution with explicit fixtures and mocks, and disclose the remaining hosted-only boundaries.

#### Alternative 3: Model the environment override in workflow frontmatter instead of as a CLI flag

The override is a debugging-time concern rather than a persistent source setting. A CLI flag leaves workflow markdown unchanged, but it still writes the override into generated lock files. Those files can be committed; authors must review generated changes and recompile without the override before publishing production configuration.

### Consequences

#### Positive

- One flag (`--dev`) requests the complete development check set without dispatch. Local diagnostics do not replace hosted CI or runtime verification.
- Bypass options and explicitly disabled required checks are rejected; JSON diagnostics preserve failures without treating another workflow's warning as a local defect.
- Push jobs and conclusion issue/comment reporting are disabled for development testing; diagnostic evidence remains available.
- `--environment` makes live verification explicitly scoped and reviewable, including on approval jobs, instead of relying on authors remembering to hand-edit lock files.
- The reusable-workflow caller case fails loudly with actionable guidance rather than emitting a lock file that GitHub Actions would reject at run time.
- Unifying installed and embedded routers on `debug-agentic-workflow.md` removes drift between the two instruction copies.

#### Negative

- Two new public CLI/MCP surfaces must be kept working and documented forever; `--dev` in particular is a bundle whose membership will need revisiting every time a new check is added.
- `--dev` is slower and noisier than a plain compile, so authors may avoid it, and warning-as-error will fail on pre-existing findings unrelated to the change in flight (this PR already reports the progress gate blocked by pre-existing custom-linter findings).
- `--environment` does not isolate secrets: authorized repository/organization/enterprise shared secrets remain in job scope, so the flag can create a false sense of containment if read as sandboxing.
- Staging and disabled push jobs do not neutralize other custom scripts, cache saves, or MCP side effects, so a `--dev` compile is not proof that a workflow is safe to run.
- Replacing approval-job environments is a sharp edge that requires human review; misuse could route an approval to a weaker gate.

#### Neutral

- A protected test environment is recommended, not mandatory; the tooling does not enforce it.
- Compilation grants no execution authorization — `--dev` and `--environment` deliberately change nothing about who may dispatch a workflow.
- Failed `--dev` compilations can leave diagnostic artifacts on disk; these are diagnostics, not approved test inputs.
- Validation for this PR was formatting/build, impacted unit tests, focused compiler/CLI/MCP/environment tests, router-consistency tests, and tabletop multi-model instruction simulations — no hosted workflow was dispatched.

---

*Proposed decision; acceptance remains with the maintainers.*
