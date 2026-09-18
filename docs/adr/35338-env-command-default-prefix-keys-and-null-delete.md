# ADR-35338: `env` Command, `default_*` YAML Keys, Null-for-Delete, and Update Confirmation

**Date**: 2026-05-28
**Status**: Draft
**Deciders**: PR author (pelikhan), reviewers of PR #35338


---

## Part 1 — Narrative (Human-Friendly)

### Context

[ADR-35286](35286-compiler-managed-enterprise-env-controls.md) introduced the `gh aw defaults` command pair (`get` / `update`) backed by a flat YAML file whose keys carried a `default_` prefix (e.g. `default_max_ai_credits`, `default_model_copilot`) that mirrored the `GH_AW_DEFAULT_*` GitHub Actions variable names. At the same time, `defaults update` performed a mutating batch operation — upserting or deleting GitHub Actions variables at repo, org, or enterprise scope — with no preview and no confirmation step, so a typo or unintended file content silently overwrote shared org-wide configuration. The original ADR's normative section did not constrain either the delete semantics or the update UX, so refining both without superseding the parent ADR is in scope.

### Decision

We will (1) rename the command from `defaults` to `env`, (2) keep the `default_`-prefixed YAML keys as established in ADR-35286, (3) switch the delete signal from empty string to explicit `null` — a field set to `null` (or absent) deletes the variable while a non-null string value sets it, and (4) make `gh aw env update` render a structured preview (scope, target, file, per-field action) and require interactive confirmation by default, with `--yes` / `-y` to bypass the prompt in automation. The `GH_AW_DEFAULT_*` GitHub Actions variable names themselves are unchanged. The confirmation gate is implemented as a small seam (`confirmAction func(...)`) so the path is unit-testable without driving a real TTY.

### Alternatives Considered

#### Alternative 1: Keep `default_*` keys as an accepted alias for backward compatibility

Continue reading non-prefixed keys alongside the `default_*` keys (or canonicalize one to the other on read), so any user with a checked-in defaults file from the ADR-35286 era keeps working. This was rejected because the `gh aw defaults` command is brand new in PR #35286 (one commit prior to this PR) and has no documented release; the audience that could have adopted the legacy shape is effectively only the PR author. Keeping an alias would lock in two key spellings forever for zero real benefit and would dilute the new docs with "either of these works" caveats. A clean break now is cheaper than an alias forever.

#### Alternative 2: Confirmation behind an opt-in `--confirm` flag instead of default-on with `--yes` bypass

Leave the existing zero-prompt behavior and add a `--confirm` flag for users who want a preview. This was rejected because `defaults update` mutates GitHub Actions variables at potentially enterprise scope — the blast radius of an accidental run is significantly larger than typical CLI side effects, and the convention in `gh` and most modern destructive CLIs (e.g. `terraform apply`, `gh release delete`) is "confirm by default, `--yes` to skip." Defaulting to safe and letting CI opt out matches user expectations and shrinks the failure mode where a typo silently wipes shared config.

#### Alternative 3: Diff-style preview (current → new value per field) instead of action-style preview

Render the preview as a two-column "before / after" diff by first fetching the current value of each `GH_AW_DEFAULT_*` variable from the target scope, then showing per-field deltas. This was rejected because it would require an extra `gh api` round-trip per field on every `update` invocation (seven reads minimum), adding latency and a new failure mode (read fails, write would have succeeded), for a UX gain that is mostly cosmetic — the user can read the file they just edited. The chosen action-style preview (`set` / `delete` per field with the new value) is unambiguous about what will happen without needing the current state.

### Consequences

#### Positive

- The on-disk file format stays aligned with the `GH_AW_DEFAULT_*` variable names by using explicit `default_`-prefixed keys.
- `defaults update` now has a confirmation gate on a destructive, often org- or enterprise-scoped operation; accidental mass mutations from typos or wrong-file invocations are blocked by default.
- The console preview shows scope, target, file, and per-field action through the shared `console.RenderStruct` helper, so the surface matches other table-style CLI surfaces in this repo.
- The `--yes` / `-y` flag preserves the non-interactive path for CI without forcing users to remember a different flag name (`gh` uses `--yes` for the same purpose).
- The confirmation seam (`confirmAction` parameter on `confirmDefaultsUpdate`) allows the path to be unit-tested without a TTY, so the "skips when --yes / errors on cancel / calls action by default" behaviors are all covered by `TestConfirmDefaultsUpdate`.

#### Negative

- Hard breaking change in the file format: any defaults file written with non-prefixed keys is silently treated as empty on read — every field becomes the empty string, which `defaults update` interprets as "delete the variable." A user re-running an old file with `--yes` would wipe their org defaults. The `TestDefaultsFileYAMLDoesNotReadLegacyKeys` test explicitly nails down this behavior, so it is by design, but the migration burden is real even if the population of affected users is small.
- CI workflows that call `gh aw defaults update` must add `--yes` or the command will hang waiting for stdin and eventually fail; this is documented in `docs/src/content/docs/reference/cost-management.md` but still requires a one-line change everywhere the command is wired up.
- One more decision point for users to learn: `default_*` keys and confirmation vs. `--yes`. The docs need to stay in sync with the binary forever.

#### Neutral

- The `GH_AW_DEFAULT_*` GitHub Actions variable names are unchanged; the override chain documented in ADR-35286 (Model Override Chain, Max-AI-Credits Override) is untouched, so no compiler or YAML-generation behavior is affected.
- The `defaultsBinding` struct gains a `fieldName` field so the preview renderer can show the file-side key (`default_max_turns`) rather than the GitHub variable name (`GH_AW_DEFAULT_MAX_TURNS`); the binding list remains the single source of truth for the seven managed variables.
- File permissions for the generated YAML now go through `constants.FilePermPublic` rather than an inline `0o644` literal — a small consistency cleanup that comes along for the ride.
- A new `displayName()` method on `defaultsTarget` and two new preview row types (`defaultsUpdatePreview`, `defaultsUpdateRow`) are added strictly for rendering; they have no behavior beyond formatting.

---

## Part 2 — Normative Specification (RFC 2119)

> The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this section are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

### Defaults File Format

1. The `defaults.yml` file consumed by `gh aw env get` and `gh aw env update` **MUST** use the YAML keys `default_max_ai_credits`, `default_max_turns`, `default_timeout_minutes`, `default_detection_model`, `default_model_copilot`, `default_model_claude`, `default_model_codex`.
2. The `defaultsFile` struct **MUST** declare `yaml:"default_*"` tags for managed defaults; non-prefixed keys **MUST NOT** be read on unmarshal.
3. The `gh aw env get` subcommand **MUST** serialize using only the `default_*` keys.
4. Each entry in `defaultsBindings` **MUST** carry both its `envName` (the `GH_AW_DEFAULT_*` GitHub Actions variable name) and its `fieldName` (the file-side key) so the update preview can label rows with the file-side key.
5. The `GH_AW_DEFAULT_*` GitHub Actions variable names themselves **MUST NOT** be changed by this ADR.

### Update Command UX

1. `gh aw env update` **MUST** render a preview of the planned mutation — scope, target display name, source file path, and a per-field action/value table — to stderr before any mutation is performed.
2. `gh aw env update` **MUST** require an interactive confirmation before applying mutations, unless `--yes` / `-y` is provided.
3. When the user declines confirmation, `gh aw env update` **MUST** return an error matching `"defaults update cancelled"` and **MUST NOT** perform any upsert or delete against the target scope.
4. The `--yes` / `-y` flag **MUST** bypass the confirmation prompt and **MUST** be the only built-in mechanism for non-interactive automation.
5. The confirmation step **MUST** be invoked through an injectable function value (the `confirmAction` parameter on `confirmDefaultsUpdate`) so it can be unit-tested without a TTY; the production wiring **MUST** use `console.ConfirmAction`.
6. An empty (or whitespace-only) value for any managed field in the input file **MUST** be interpreted as "delete the corresponding `GH_AW_DEFAULT_*` variable from the target scope," matching the behavior established in ADR-35286.

### Preview Rendering

1. The preview header **MUST** be rendered via `console.RenderStruct(defaultsUpdatePreview{...})` and include `Scope`, `Target`, `File`, and `Fields` columns.
2. The per-field table **MUST** be rendered via `console.RenderStruct` over `[]defaultsUpdateRow` with `Field`, `Action`, and `Value` columns.
3. The `Action` cell **MUST** be `"set"` for non-empty values and `"delete"` for empty values.
4. When `--yes` is provided, the preview **MUST** still be rendered before the "Skipping confirmation because --yes was provided." info message; the flag suppresses only the prompt, not the preview.

### Documentation

1. The reference page `docs/src/content/docs/reference/cost-management.md` **MUST** show the trimmed-key shape in its examples and **MUST** document the confirmation behavior and `--yes` override.
2. The reference page `docs/src/content/docs/reference/compiler-enterprise-environment-controls.md` **MUST** mention that the defaults file uses the trimmed keys.
3. The reference page `docs/src/content/docs/reference/environment-variables.md` **MUST** mention that `gh aw defaults` manages `GH_AW_DEFAULT_*` variables via trimmed YAML keys.

### Conformance

An implementation is considered conformant with this ADR if it satisfies all **MUST** and **MUST NOT** requirements above. Failure to meet any **MUST** or **MUST NOT** requirement constitutes non-conformance. This ADR refines but does not supersede [ADR-35286](35286-compiler-managed-enterprise-env-controls.md); the normative sections of ADR-35286 remain in force.

---

*This is a DRAFT ADR generated by the [Design Decision Gate](https://github.com/github/gh-aw/actions/runs/26549106345) workflow. The PR author must review, complete, and finalize this document before the PR can merge.*
