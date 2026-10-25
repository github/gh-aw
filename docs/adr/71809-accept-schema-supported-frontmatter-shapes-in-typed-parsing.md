# ADR-71809: Accept Schema-Supported Frontmatter Shapes in Typed Parsing

**Date**: 2026-10-25
**Status**: Draft
**Deciders**: pelikhan, [TODO: verify reviewers]

---

### Context

Workflow frontmatter is validated against a JSON schema and then decoded into the typed `FrontmatterConfig` struct (`pkg/workflow/frontmatter_types.go`) through a single `json.Unmarshal` in `decodeFrontmatterConfig`. Several forms that the schema accepts did not fit that struct's Go types: a scalar `needs:` on a custom safe-output job, `cache:` as an array of cache objects, and `max-ai-credits` / `max-daily-ai-credits` values written as K/M-suffixed strings, the `-1` disable sentinel, or (for the daily limit) an object such as `{value: 20K}`. If any one field failed to unmarshal, the whole typed configuration was dropped and the user saw a generic warning. A sweep of public workflows found these mismatches. The fix needed to accept valid input while still reporting real invalid input.

### Decision

We will make typed frontmatter parsing accept every shape the schema allows. Mismatched shapes are handled at the decode boundary, and the typed API stays the same for its consumers. In detail: (1) scalar `safe-outputs.jobs.*.needs` values are normalized to single-element string slices on a clone, so the raw frontmatter is not mutated; (2) `FrontmatterConfig.Cache` is widened from `map[string]any` to `any`, and a new `validateFrontmatterCacheShape` check accepts only an object or an array of objects; (3) the credit-limit fields are captured as `json.RawMessage` through an embedded-alias auxiliary struct and parsed by a dedicated `parseFrontmatterAICLimit` into the existing `*TemplatableInt32` threshold fields. Daily-guardrail settings beyond the threshold are still read from raw frontmatter. Invalid shapes return a specific, field-named error rather than being silently ignored.

### Alternatives Considered

#### Alternative 1: Custom `UnmarshalJSON` methods on the affected types

Each field type (`TemplatableInt32`, a new cache type, a new `needs` type) could implement its own `UnmarshalJSON` to accept the extra shapes. This keeps the parsing logic next to each type. It was not chosen because `TemplatableInt32` is shared with many other integer fields (`max-turns`, `max-tool-calls`, and others). Widening its syntax would let K/M suffixes and object forms into fields where the schema does not allow them. The PR explicitly says it will "leave ordinary templatable integer parsing unchanged."

#### Alternative 2: Lenient decoding (ignore or skip fields that fail to unmarshal)

The decoder could tolerate per-field type mismatches, either by decoding field by field or by suppressing errors, so that one bad field no longer drops the whole config. This would hide the symptom, but schema-valid values would still be lost. Genuinely invalid input would also be silently discarded. The PR's goal of keeping real invalid-input diagnostics rules this out.

#### Alternative 3: Narrow the JSON schema to match the Go types

The schema could be tightened to reject the forms the typed struct can't decode. This was rejected because those forms are already documented and used in public workflows. Narrowing the schema would be a breaking change for users.

### Consequences

#### Positive
- Workflows that use schema-valid shapes (scalar `needs`, array `cache`, suffixed, expression, or object credit limits) no longer lose their entire typed configuration.
- Downstream code that reads `MaxAICredits` / `MaxDailyAICredits` keeps using the existing `*TemplatableInt32` API with canonical values.
- Invalid shapes now produce specific error messages that name the field (for example, `invalid max-daily-ai-credits: ...` or `invalid cache entry at index N`).
- Regression tests cover parsing, invalid-input warnings, non-mutation of raw frontmatter, and compiled output.

#### Negative
- `FrontmatterConfig.Cache` is now `any`, so type safety is lost. Consumers must type-switch between the object and array forms.
- Credit-limit parsing lives outside the struct's normal unmarshal path, in an aux/alias wrapper in `decodeFrontmatterConfig`. That is less obvious to future maintainers and must be kept in sync if more fields need special handling.
- Daily-guardrail object settings are split: the threshold is typed, while the other object keys are still read from raw frontmatter. There are two sources of truth for one frontmatter key.

#### Neutral
- The schema stays the authoritative definition of accepted shapes. The typed layer now adapts to it.
- The normalization of safe-output job `needs` runs inside the existing `normalizeSafeOutputShorthands` pass.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
