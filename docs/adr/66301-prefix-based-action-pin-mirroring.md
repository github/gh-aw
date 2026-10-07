# ADR-66301: Rewrite Action Repositories via Prefix Mappings After SHA Resolution

**Date**: 2026-10-06
**Status**: Draft
**Deciders**: pelikhan [TODO: verify full decider list]

---

### Context

`aw.json` `action_pins` is documented as the mechanism that lets enterprises redirect `uses:` references to internal mirrors, but it only took effect on one of the two resolution paths in `pkg/workflow/action_pins.go`. The package-level `getActionPin` and `(*Compiler).getActionPin` read embedded/cached pins directly, so roughly 80 compiler emission sites (checkout, artifacts, github-script, safe-output jobs, ledger, cache-memory, and the generated maintenance/slash-command workflows) still produced `uses: actions/*` lines — 26 of 57 lines in a reproduction lock file (see #66185). In an organization that only allows internal mirrors, those lock files fail. Additionally, `action_pins` keys are exact `owner/repo@version` strings, so mirroring an entire action family requires one entry per action per version and must be rewritten on every gh-aw upgrade that bumps an action.

### Decision

We will route every compiler-generated action reference through `actionpins.ResolveLatestActionPin` so that mappings apply uniformly at all emission sites, and we will add an optional `action_pin_prefixes` map to `aw.json` that rewrites only the *repository* portion of an already-resolved reference. Prefix rewriting happens **after** SHA pinning (`resolve.go` defers `applyActionPinPrefix` on the original repo), so the upstream SHA and `# version` comment are preserved verbatim and no network lookup against the mirror is needed. The longest matching prefix wins, and exact `action_pins` entries continue to take precedence over prefixes. The primary driver is correctness and maintenance cost: version-independent mirroring that survives gh-aw action upgrades.

### Alternatives Considered

#### Alternative 1: Keep exact `action_pins` only and expand its coverage

Fix the bug half of #66185 (make all emission sites consult `action_pins`) without introducing prefix mappings. This was a close call because it is the smaller change and keeps a single configuration concept. It was rejected because the operational burden remains: each mirrored action needs an entry per version, and every gh-aw release that bumps an embedded action version silently reverts users to public `actions/*`.

#### Alternative 2: Resolve the mirror repository directly (look up the mirror's own SHA)

Treat `my-org/actions-checkout` as a first-class action and resolve its tags/SHA through the normal pin resolution path. Rejected because it requires network access to the mirror from the compiling environment — frequently a private, unreachable host for the exact enterprise users this feature targets — and because mirrors are expected to contain the same commits, making a second resolution redundant and a source of drift between the comment and the SHA.

#### Alternative 3: Post-process the generated lock file with a textual repository substitution

Run a regex rewrite over the final `.lock.yml`. Rejected because it operates outside the pin model, cannot express exact-over-prefix precedence, and would rewrite incidental `actions/` occurrences in scripts or comments.

### Consequences

#### Positive
- Mirrored references now cover all compiler-generated jobs and the generated maintenance/slash-command/auto-update workflows, satisfying the acceptance criterion of zero residual `uses: actions/` lines.
- A single `{"actions/": "my-org/actions-"}` entry mirrors the whole family and remains correct across gh-aw action version bumps.
- SHA and version comments stay identical to upstream, keeping lock files auditable against the public action history.

#### Negative
- gh-aw now assumes the mirror contains the upstream SHA; if it does not, the failure surfaces only at workflow run time on GitHub, not at compile time.
- A second configuration key (`action_pin_prefixes`) with precedence rules relative to `action_pins` increases the surface users must understand and that the compiler must keep consistent.
- Routing every emission site through `ResolveLatestActionPin` touches many generator files (22 files in this PR), widening the blast radius of future changes to pin resolution.

#### Neutral
- `PinContext` gains `PrefixMappings`, and the resolved-reference rewrite is exposed as `ApplyResolvedActionPinPrefix` for callers that pin before rendering.
- The schema (`pkg/parser/schemas/repo_config_schema.json`), repo config struct, and `WorkflowData` all carry the new field; reference docs and the glossary were updated alongside.
- Prefix application emits the same one-time informational message as exact mappings, so existing log-based expectations are unchanged in shape.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
