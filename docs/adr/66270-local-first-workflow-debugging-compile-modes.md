# ADR-66270: Support Local-First Workflow Debugging with Compile-Time `--dev` and `--environment`

**Date**: 2026-02-13
**Status**: Draft
**Deciders**: gh-aw maintainers (pending review; PR author: pelikhan)

---

### Context

Debugging an agentic workflow in `gh-aw` has historically meant dispatching it on GitHub Actions, because only the hosted run exercises the real lock file, approvals, secrets, and runner behavior. That loop is slow, requires repository dispatch permissions that Copilot cloud agents explicitly do not have, and tempts authors into running unreviewed workflows against production environments to see what breaks. At the same time, `gh aw compile` exposed its strictness, staging, and scanner checks as a scattered set of independent flags, so "compile the way CI compiles" was an undocumented incantation that was easy to get wrong or silently downgrade (for example, through the compile MCP wrapper). PR #66270 touches 30 files across `cmd/`, `pkg/cli`, `pkg/workflow`, the instruction routers under `.github/aw/`, and the reference docs, adding ~542 lines in business-logic directories. The constraint is that any local-reproduction story must be honest: it cannot pretend to reproduce GitHub Actions credentials, approval gates, or runner semantics.

### Decision

We will make local compilation the primary workflow-debugging surface, backed by two new compile-time controls and a single shared instruction router. First, `gh aw compile --dev` becomes a compile-only "strict preflight" mode: `applyDevelopmentCompileMode` turns on strict validation, staging, image/model/analysis checks, and every scanner at once, `validateDevelopmentCompileMode` rejects bypass options (`--no-emit`, `--watch`, `--approve`, `--allow-action-refs`), and `enforceDevelopmentDiagnostics` promotes warnings to failures and preserves them in JSON diagnostics so the MCP wrapper cannot silently weaken the requested checks. Second, `gh aw compile --environment NAME` rewrites the `environment:` field on every generated job, including approval jobs, with a literal, validated, `strconv.Quote`-escaped name; reusable-workflow caller jobs (`job.Uses != ""`) fail with an explicit error because GitHub Actions forbids an environment on those jobs. Third, the installed and embedded debugging instructions are unified behind `.github/aw/local-debug.md`, which separates local diagnosis/patching from bounded, human-validated edit/run/audit loops. The primary driver is safety-through-explicitness: make the strict local loop one flag, and make live verification an explicitly reviewed, environment-scoped act rather than an accidental one.

### Alternatives Considered

#### Alternative 1: Keep composing the existing individual compile flags and document the recipe

The strict combination (`--strict --staged --validate --validate-images --zizmor --poutine --syft --grype --yamllint --shellcheck --models …`) already existed; we could have written it down in `compilation-process.md` and stopped there. This was a genuine close call because it adds no new API surface and no new failure modes. It was rejected because a documented recipe has no enforcement: nothing prevents a caller — in particular the compile MCP tool invoked by an agent — from dropping a scanner or re-enabling `--allow-action-refs` while still claiming a "dev" compile. Making the mode a first-class flag that *rejects* bypass options is the only version that holds under agent-driven invocation.

#### Alternative 2: Build a local runner/emulator for agentic workflows (act-style)

Instead of improving compilation, we could have invested in locally executing the generated lock file, so authors see real job behavior without GitHub. It was considered because it is the most faithful reproduction and would cover runtime bugs that compilation cannot see. It was rejected because the fidelity would be a lie in exactly the places that matter: OIDC and `GITHUB_TOKEN` minting, environment approval gates, organization/enterprise shared secrets, and runner images cannot be reproduced locally. A partial emulator would encourage authors to trust a result that does not transfer, which is the failure mode this PR is trying to eliminate.

#### Alternative 3: Model the environment override in workflow frontmatter instead of as a CLI flag

The target environment could have been a field in the workflow markdown, compiled like any other setting. It was rejected because the override is a *debugging-time* concern, not a property of the workflow: putting it in frontmatter invites it into committed lock files, where a stale `environment: staging` would silently change production approval routing. A CLI flag keeps the override at the compile invocation that requested it and keeps the checked-in source unchanged.

### Consequences

#### Positive

- One flag (`--dev`) reproduces the full strict check set locally, so authors and agents get CI-equivalent diagnostics without dispatching a run — which also respects the Copilot-cloud no-dispatch rule.
- `--dev` is tamper-resistant: bypass options are rejected and warnings are preserved in JSON diagnostics, so an MCP caller cannot downgrade the checks it claims to have run.
- `--environment` makes live verification explicitly scoped and reviewable, including on approval jobs, instead of relying on authors remembering to hand-edit lock files.
- The reusable-workflow caller case fails loudly with actionable guidance rather than emitting a lock file that GitHub Actions would reject at run time.
- Unifying installed and embedded routers on `local-debug.md` removes drift between the two instruction copies.

#### Negative

- Two new public CLI/MCP surfaces must be kept working and documented forever; `--dev` in particular is a bundle whose membership will need revisiting every time a new check is added.
- `--dev` is slower and noisier than a plain compile, so authors may avoid it, and warning-as-error will fail on pre-existing findings unrelated to the change in flight (this PR already reports the progress gate blocked by pre-existing custom-linter findings).
- `--environment` does not isolate secrets: authorized repository/organization/enterprise shared secrets remain in job scope, so the flag can create a false sense of containment if read as sandboxing.
- Staging does not neutralize custom scripts or MCP side effects, so a `--dev` compile is still not proof that a workflow is safe to run.
- Replacing approval-job environments is a sharp edge that requires human review; misuse could route an approval to a weaker gate.

#### Neutral

- A protected test environment is recommended, not mandatory; the tooling does not enforce it.
- Compilation grants no execution authorization — `--dev` and `--environment` deliberately change nothing about who may dispatch a workflow.
- Failed `--dev` compilations can leave diagnostic artifacts on disk; these are diagnostics, not approved test inputs.
- Validation for this PR was formatting/build, impacted unit tests, focused compiler/CLI/MCP/environment tests, router-consistency tests, and tabletop multi-model instruction simulations — no hosted workflow was dispatched.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
