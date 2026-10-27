---
title: Accept schema-supported frontmatter shapes in typed parsing
description: Rationale for adapting typed frontmatter decoding without broadening shared integer syntax.
---

# ADR-71809: Accept Schema-Supported Frontmatter Shapes in Typed Parsing

**Date**: 2026-10-25
**Status**: Proposed
**Related PR**: [#71809](https://github.com/github/gh-aw/pull/71809)

## Context

The workflow compiler validates frontmatter against a JSON schema and also decodes it into `FrontmatterConfig`. Schema-valid input can fail typed decoding when a Go field represents only one of the accepted shapes. A failure causes the compiler to discard the entire typed configuration and emit a generic parse warning, potentially losing unrelated typed settings.

The reported sweep of 460 public workflows found 13 such warnings: five scalar custom safe-output job `needs` values, four cache arrays, three daily credit-limit objects, and one `20K` credit limit. The schema already accepts these forms; changing workflow syntax is not necessary.

The compatibility fix must preserve invalid-input diagnostics and existing consumers of typed thresholds. In particular, credit-specific syntax must not become valid for every field using the shared `TemplatableInt32` type.

## Decision

Adapt the normalization and decode boundary for these fields rather than relax decoding globally or change the schema:

1. Normalize scalar `safe-outputs.jobs.*.needs` strings to single-element string slices in `normalizeSafeOutputShorthands`. Clone the affected maps so the original frontmatter remains unchanged. Existing list values retain their meaning.
2. Change `FrontmatterConfig.Cache` from `map[string]any` to `any` to preserve either a single cache object or an array of cache objects. `validateFrontmatterCacheShape` rejects other non-null shapes, including non-object array entries. This is a shape check, not a replacement for schema validation of cache properties.
3. Capture `max-ai-credits` and `max-daily-ai-credits` as `json.RawMessage` through an auxiliary struct embedding an alias of `FrontmatterConfig`. Parse them with `parseFrontmatterAICLimit` into the existing `*TemplatableInt32` fields. Accept positive integers, positive numeric strings with optional K/M suffixes, GitHub Actions expressions, and the scalar `-1` disable sentinel. Normalize numeric thresholds to base-10 strings and preserve expressions.

Only the daily limit accepts an object. Its required `value` supplies the typed threshold; `-1` is not accepted inside that object. Disabling the guardrail uses scalar `max-daily-ai-credits: -1`. Other daily settings, including `backend`, `continue-on-error`, and `github-app`, continue to be read from the unchanged raw frontmatter.

Ordinary `TemplatableInt32` decoding remains unchanged. Invalid input still fails typed parsing and retains the compiler's warning path. Cache-shape and credit-limit errors add field-specific detail; this change does not replace every generic parse diagnostic.

This is the approach implemented in PR #71809, recorded as proposed pending maintainer acceptance. It makes no changes to permissions, checkout behavior, or workflow-execution authorization.

## Alternatives Considered

### Shared custom `UnmarshalJSON`

Extending `TemplatableInt32.UnmarshalJSON` would colocate decoding with the type but broaden credit-specific syntax for unrelated integer fields. Dedicated wrapper types for credits, cache, and job dependencies could avoid that problem, at the cost of changing more typed interfaces and consumers. Boundary adaptation preserves the existing threshold and dependency APIs; cache is the explicit exception.

### Lenient decoding

Skipping fields that fail decoding would preserve some unrelated settings, but silently lose valid values and hide invalid input. It would mask the representation mismatch rather than resolve it. The selected approach continues to return errors instead of silently accepting partial configuration.

### Narrowing the schema

Rejecting scalar dependencies, cache arrays, or supported credit-limit forms would align validation with the old Go representation. It would also break existing schema-valid workflows. The typed representation should adapt to the supported input contract instead.

## Consequences

The affected schema-valid forms no longer discard the typed configuration. Credit consumers retain their existing threshold API, and normalization does not mutate raw frontmatter. Existing regression tests cover supported forms, invalid-input warnings, raw-map preservation, cache round trips, and compiled output.

The tradeoff is weaker static typing for `Cache`; consumers must handle its object and array forms. The alias/raw-message decoder requires maintenance alongside the schema. Daily-limit consumers must also distinguish the derived typed threshold from auxiliary settings retained in raw frontmatter; the typed threshold is not a complete representation of the object.

The reported local compilation check covered 348 workflows with zero generic parse warnings. It also identified generated lock drift in 27 files; those lock files were not modified. This observation is compilation evidence, not live-execution validation or a guarantee that every possible schema/type mismatch has been eliminated.
