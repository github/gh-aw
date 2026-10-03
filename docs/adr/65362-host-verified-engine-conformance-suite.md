# ADR-65362: Verify Engine Configuration with Host-Asserted Conformance Workflows

**Date**: 2026-10-03
**Status**: Draft
**Deciders**: pelikhan (PR author), gh-aw maintainers *[TODO: verify]*

---

### Context

gh-aw supports many agentic engines: five built-in engines (`claude`, `codex`, `copilot`, `gemini`, plus the Copilot-backed profiles) and eight import-based engines (`aider`, `crush`, `cursor`, `deepseek-harness`, `goose`, `kiro`, `opencode`, `pi`, `pydantic-ai`). The existing smoke workflows (`smoke-*.md`) rely on the agent itself reporting whether its configuration works, so a misconfigured engine can still produce a green run by simply claiming success. There was no host-side check that `engine.env` propagation, shell execution, filesystem fixtures, and the MCP tool transport actually function per engine. At the same time the repository still carried an obsolete GenAIScript `custom` engine sample and catalog entry that no longer reflects a supported path.

### Decision

We will add a shared, import-based conformance suite (`.github/workflows/shared/engine-conformance.md`) plus 13 manual-only `engine-conformance-<engine-id>` workflows, one per supported engine, whose pass/fail outcome is decided by host-side JavaScript assertions rather than by agent self-reporting. The shared import generates per-run random fixtures in a `pre-agent-steps` step, exposes a `conformance-challenge` MCP script that issues a second nonce and records a host-side receipt, and runs a `post-steps` assertion program that compares the agent's typed `result.json` against the host's `expected.json` and receipt. The primary driver is trustworthiness of the signal: evidence is produced and validated outside the agent's control, and missing, malformed, oversized, symlinked, or mismatched evidence fails the run. We will also remove the obsolete GenAIScript `custom` engine sample, catalog entry, docs references, and conformance exception.

### Alternatives Considered

#### Alternative 1: Extend the existing `smoke-*` workflows with stronger prompts

Each engine already has a smoke workflow, so the cheapest option was to ask the agent in those prompts to self-check its environment and report results. This was rejected because the verdict would still be produced by the component under test; a confused or hallucinating agent can report success without having exercised `engine.env`, bash, or the MCP transport. It also would not produce machine-checkable artifacts for triage.

#### Alternative 2: A dedicated Go CLI command / unit-test harness for engine configuration

A new `gh aw` subcommand or Go test harness could validate engine configuration locally without spending model credits. It was genuinely considered — it is faster and deterministic — but it can only validate compiled lock-file content, not the end-to-end runtime behaviour on a GitHub Actions runner (actual CLI installation, real environment propagation, real MCP gateway round-trip). The PR explicitly avoids adding a new CLI command or runtime implementation for this reason. Go tests are still used (`engine_conformance_test.go`, `engine_definition_test.go`) for the static/regression portion.

#### Alternative 3: A single parameterized workflow with a matrix over engines

One workflow with a job matrix would avoid 13 near-duplicate markdown files and 13 generated lock files. It was rejected because each engine needs an independent installation and execution path, engine-specific imports (`shared/aider.md`, `shared/cursor.md`, …), engine-specific models, permissions (`copilot-requests: write`), and prompt affordances (e.g. Aider's single-turn SEARCH/REPLACE instructions), which the per-workflow compilation model expresses more directly.

### Consequences

#### Positive
- Engine configuration failures are detected by host-side assertions, so a green run means `engine.env`, shell execution, fixture I/O, and the MCP CLI transport were genuinely exercised.
- Evidence is deterministic and inspectable: a step summary plus a two-day `report.json` artifact make failures triageable without re-running.
- Tamper resistance is built in — nonce mismatch, symlinked or oversized evidence, and wrong value types all fail rather than pass silently.
- Removing the dead GenAIScript `custom` engine reduces catalog and documentation drift; catalog-test lookups are kept local so the removal regression does not depend on live GitHub requests.

#### Negative
- Significant duplication: 13 workflow markdown files and ~13 generated lock files (the bulk of the diff) must be regenerated whenever the shared import changes.
- Running the suite consumes real model credits on every engine (capped at five credits and a ten-minute timeout each), so it is manual-dispatch only and will not run on every PR — regressions can go unnoticed between dispatches.
- Coverage is partial: the probe verifies the gateway/CLI tool path but not native MCP clients, SDK/driver profiles, plugins, permission-denial enforcement, or the provider's actual model selection.
- Adding a new engine now carries an extra obligation to add and compile a matching conformance workflow.

#### Neutral
- Safe outputs are staged, so conformance runs never publish issues or comments.
- Workflows are `workflow_dispatch` only, with `max-turns: 30` where supported and `gh-aw-detection: false`.
- The suite reuses existing engine authentication paths and the already SHA-pinned `actions/upload-artifact` v7.0.1; no new actions or secrets are introduced.
- Per-engine prompt text diverges (notably Aider's single-turn instructions) even though the assertion contract is shared.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
