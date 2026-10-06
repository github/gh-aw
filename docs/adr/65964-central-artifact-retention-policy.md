# ADR-65964: Centralize Artifact Retention as a Repository/Enterprise Policy Resolved at Compile Time

**Date**: 2026-02-06
**Status**: Draft
**Deciders**: Unknown (PR author: pelikhan)

---

### Context

Every generated gh-aw workflow uploads artifacts (agent logs, safe-output payloads, maintenance results, user-declared `upload-artifact` steps), and each upload step historically carried its own `retention-days` value — or none at all, falling back to the repository's Actions default. That made retention an attribute of individual emitters rather than a property that a repository, organization, or enterprise administrator could set once. Administrators who needed a shorter retention window (for compliance or storage cost) or a longer one (for incident forensics) had no single place to express it, and there was no way to change retention for already-generated workflows without touching every emitter. PR #65964 touches `pkg/workflow/` and `pkg/cli/` (~611 added lines in business-logic directories) across 344 files, the bulk of which are the 321 regenerated `.lock.yml` files plus compiler golden fixtures.

### Decision

We will introduce a single `artifact_retention_days` setting in `.github/workflows/aw.json` (accepting an integer 1–400 or a single-line GitHub Actions expression) and resolve it in a fixed precedence order: repository config setting, then the runtime Actions variable `vars.GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS`, then the pre-existing per-artifact `retention-days` value. Resolution happens in the compiler (`pkg/workflow/artifact_retention.go`) as a post-generation pass that rewrites only the `with:` inputs of recognized `actions/upload-artifact` steps — including mirrored/GHES pins declared via `action_pins` — leaving all other YAML byte-for-byte intact. The precedence is encoded directly into the emitted expression `${{ vars.GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS || <fallback> }}`, so an unset repository policy preserves today's behavior exactly, and `gh aw env` gains `default_artifact_retention_days` for repository, organization, and enterprise management. The primary driver is making retention an administrable policy with zero behavior change when no policy is configured.

### Alternatives Considered

#### Alternative 1: Thread a retention parameter through every artifact-emitting code path

Each place that emits an upload step could read the config and pass `retention-days` explicitly. This was a genuine candidate — it is the most conventional approach and avoids post-processing generated YAML. It was rejected because gh-aw emits upload steps from many unrelated sites (agent job, safe-output handler, maintenance workflows, user-authored custom steps that the compiler only passes through), and user-declared steps would still be unreachable. A single post-generation pass covers all of them uniformly; the PR reports auditing all 3,445 upload steps across 322 generated workflows as evidence of that uniformity.

#### Alternative 2: Resolve retention purely at runtime via an Actions variable

Emit only `${{ vars.GH_AW_DEFAULT_ARTIFACT_RETENTION_DAYS }}` everywhere and let administrators set the variable at the org/enterprise level, with no compile-time repository setting and no lock-file churn. This was attractive because it would have avoided regenerating 321 lock files. It was rejected because the value would then be invisible in the committed workflow, undiscoverable during review, and unvalidatable at compile time — an out-of-range or malformed variable would only surface as a failing Actions run. The chosen design keeps the runtime variable as the middle precedence tier while still validating the checked-in value and rejecting invalid runtime values in the safe-output handler.

#### Alternative 3: Rely on the GitHub repository-level default retention setting alone

GitHub already exposes a repository/org artifact retention default, and `actions/upload-artifact` honors it when `retention-days` is unset. Rejected because gh-aw emitters already set explicit per-artifact values that override that default, and the platform setting cannot express gh-aw-specific policy or be managed through `gh aw env` alongside the other gh-aw settings.

### Consequences

#### Positive
- Retention becomes one administrable knob at repository, organization, or enterprise scope instead of a per-emitter detail, and `gh aw env` surfaces it alongside other gh-aw settings.
- Coverage is uniform: generated uploads, maintenance artifacts, custom upload steps, and mirrored/GHES upload actions are all rewritten by the same pass, so no emitter can silently escape the policy.
- When no policy is configured, the emitted expression falls back to the previous per-artifact value, so existing repositories observe no behavior change.
- Invalid values are caught early — the schema bounds the checked-in value to 1–400, and invalid runtime values are rejected in the safe-output handler rather than failing opaquely mid-run.

#### Negative
- The post-generation pass parses and rewrites generated YAML with hand-rolled indentation and line-range logic (six-space step indentation, `leadingSpaces` checks). This is coupled to the compiler's current emission style and will break or silently no-op if that formatting changes.
- Recognizing upload steps depends on matching `actions/upload-artifact` or a configured `action_pins` mirror; an unrecognized mirror or a dynamically constructed `uses:` value will be skipped without the policy applying, and that gap is not self-announcing.
- Every upload step now carries a `${{ vars.… || … }}` expression instead of a literal, making generated lock files slightly noisier and retention harder to read at a glance.
- A large one-time diff (321 regenerated lock files plus golden fixtures) makes the substantive change harder to review and will conflict with any concurrently open workflow PR.

#### Neutral
- GitHub's own retention limits still apply; this does not change Actions cache eviction or git-backed storage.
- The `/etc/hosts` upload test was made portable to macOS as part of this change, without altering upload protections.
- The PR reports that the full repository gate is not green: pre-existing custom-linter violations and unrelated macOS path/architecture/toolchain failures remain. [TODO: verify these are unrelated to this change before moving status to Accepted.]

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
