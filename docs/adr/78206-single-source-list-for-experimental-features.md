# ADR-78206: Single Source List for Experimental Features

**Date**: 2026-11-16
**Status**: Draft
**Deciders**: pelikhan (PR author); gh-aw maintainers

---

### Context

gh-aw marks some features as experimental in three separate places: compiler notices that start with "Using experimental feature:", docs pages and the docs sidebar's Experimental group, and the `experimental:` badges on cracktro deck slides. Each place was maintained by hand, so the three could disagree about which features are experimental. Compiler notice wording is user-visible output and must not change. A developer new to the codebase needs one authoritative place that answers "is this feature experimental, and where is that stated?"

### Decision

We will record experimental features in one data file, `pkg/workflow/data/experimental_features.json`, and treat it as the source of truth. The compiler embeds this file and builds its experimental-feature notices from it, while the checks that decide when a feature is used stay in Go code. The docs and cracktro deck keep their own hand-written text, and Go and Playwright tests fail when they disagree with the file. The primary driver is preventing drift between the three places without changing compiler output.

### Alternatives Considered

#### Alternative 1: Keep three independent lists and rely on review

Each place would stay hand-maintained, and reviewers would catch mismatches. This is the status quo, and it leaves the drift risk in place. It needs no new file or tests, but it was rejected because preventing drift is the problem this change is meant to solve.

#### Alternative 2: Generate the docs sidebar group and deck badges from the list

A build step could write the Experimental sidebar group and the deck's badge labels from the JSON, so the consumers could not disagree at all. This would prevent drift instead of detecting it, but it would add a generation step to the docs and deck build. The PR does not record why generation was not chosen [TODO: verify]. As implemented, the deck still hardcodes its badge strings, and `docs/tests/talk.spec.ts` catches any drift.

#### Alternative 3: Keep the list only in Go code

A Go table in `pkg/workflow` could hold the list, and the Go tests could check the docs against it. This was not sufficient because the deck test is a TypeScript Playwright spec that reads the list directly. It would need a second, hand-copied list or a generated file, so a JSON file that both Go and TypeScript can read was chosen.

### Consequences

#### Positive
- Feature status is recorded once. Compiler notices, docs group membership, and deck badges are checked against the same entries.
- Tests check both directions: a compiler check without a list entry fails, and a list entry with a compiler warning but no check fails. The docs sidebar Experimental group must match the `docs_group` entries.
- Compiler notice text is unchanged, and the JSON is embedded in the binary, so the compiler needs no runtime file access.

#### Negative
- The deck still hardcodes its badge strings, so drift is detected by a test rather than prevented. The Playwright spec has not been run for this PR because `docs/node_modules` was not installed, so this check is unverified.
- The docs check only requires the word "experimental" to appear on each listed page. This is weaker than checking that the page states the feature's status.
- The sidebar check parses `docs/astro.config.mjs` with a regular expression, so reformatting that file can break the test without any behavior change.
- `experimentalFeatureWarning` panics when an ID is missing from the list or has no `compiler_warning`. A mismatch therefore fails at compile time instead of returning an error; tests guard against this, but the panic path remains.
- Adding a compiler-warning feature needs two edits: an entry in the JSON and a predicate in the `experimentalFeatureChecks` table in `compiler_validators.go`.

#### Neutral
- Some features are experimental in the docs and deck but have no compiler notice (`experiments`, `model-router`, `microvm-runtime`, `qmd`, and the docs Experimental group). Whether the compiler should warn for them is deferred to a follow-up.
- The JSON file's `$comment` field documents the schema. Every field except `id` and `name` is optional.
- Three docs pages that lacked an experimental note gained one: `experimental/qmd.md`, `reference/rate-limiting-controls.md` (`user-rate-limit`), and `reference/agent-runtimes.md` (microVM runtimes).

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
