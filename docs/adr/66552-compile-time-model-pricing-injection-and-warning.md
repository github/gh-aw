# ADR-66552: Inject Catalog Model Pricing at Compile Time and Warn on Unpriced Models

**Date**: 2026-10-06
**Status**: Draft
**Deciders**: gh-aw maintainers (pending review; PR author: pelikhan)

---

### Context

Workflows compiled by `gh aw` run their inference through the agentic workflow firewall (AWF) API proxy, which needs an AI-credits price for the configured model. When a model is absent from the pricing table (for example newly released names such as `gpt-6.1-sol`), the proxy rejects every request with HTTP 400 and retries cannot help, so the workflow is fully blocked at run time (issue #66492, clustering sources #66425 and #66459). Until now the compiler only registered a model pricing resolver when a model inventory was loaded or in dry-run mode (`config.activeModels != nil || config.DryRun`), so normal compilation emitted no pricing overlay and gave no diagnostic. The failure therefore surfaced late, in CI, as an opaque proxy error rather than at authoring time. New model names will keep appearing, so the fix has to be a recurring compile-time check rather than a one-off pricing table edit.

### Decision

We will resolve model pricing from the local `gh aw` model catalog on every compilation and inject the exact matched prices into the compiled AWF pricing overlay, and we will emit a compile-time warning whenever the configured model resolves to no price and no fallback. The pricing resolver (`SetModelPricingResolver` → `findExactModelPricing`) and the configured-model validator are now registered unconditionally in `createAndConfigureCompiler`, instead of only under inventory/dry-run conditions. Lookup uses *exact* normalized model-ID matching rather than the existing prefix-based `findModelPricing`, because charging a near-miss model at a sibling model's rate is worse than warning. The warning is suppressed when the author supplies `models.providers.<provider>.models.<model>.cost`, sets `models.default-ai-credits-pricing`, or maps the model (transitively, via `ModelMappings`) onto a priced model, and it is skipped for dynamic or expression-valued model names (`auto`, `${{ ... }}`).

### Alternatives Considered

#### Alternative 1: Fail compilation hard on an unpriced model

Turning the missing price into a compilation error would guarantee the broken configuration never reaches CI. We rejected it because the local catalog inevitably lags behind newly released models, and the AWF proxy may well have pricing the client does not know about; a hard error would block legitimate workflows on stale client data. A warning that names the four concrete remedies (per-model cost, default pricing, alias mapping, different model) keeps authors unblocked while still being actionable. This was a close call and could be revisited once catalog freshness is guaranteed.

#### Alternative 2: Keep the problem at run time and rely on a proxy-side default price

Letting the proxy apply a default AI-credits rate for unknown models would fix the HTTP 400 without any compiler change. We rejected it because it is a server-side change outside this repository's control, it silently bills unknown models at an invented rate, and it leaves the author with no signal that their model is unrecognized. The per-workflow `models.default-ai-credits-pricing` escape hatch gives the same safety valve under explicit author control.

#### Alternative 3: Reuse the existing prefix-based `findModelPricing` for injection

The existing resolver already matches models by prefix, so reusing it would have required no new lookup function and would have "priced" more models automatically. We rejected it because prefix matching maps `gpt-6.1-sol-preview` onto `gpt-6.1-sol` pricing, producing confidently wrong costs; `findExactModelPricing` was added so that an inexact match degrades into a visible warning rather than a hidden mispricing.

### Consequences

#### Positive
- A blocking run-time failure (HTTP 400 from the AWF API proxy) becomes an actionable compile-time diagnostic that names four concrete remedies.
- Known catalog models, including the reported `gpt-6.1-sol`, now compile with exact prices in the AWF pricing overlay, so previously blocked workflows run without manual pricing frontmatter.
- The check is generic over the catalog, so future unpriced model names are caught automatically instead of requiring a new bug report each time.

#### Negative
- Pricing resolution and validation now run on every compilation, adding catalog initialization work and a small latency cost to the common path that previously skipped it entirely.
- Compiled lock files now embed concrete prices from the client's local catalog, so a stale catalog can bake outdated rates into the overlay until recompilation.
- Exact matching means legitimately new or variant model names (e.g. `-preview` suffixes) will warn even when the proxy knows their price, creating some false-positive noise.

#### Neutral
- `TestCreateAndConfigureCompiler_DoesNotRegisterModelPricingResolverByDefault` is inverted into `TestCreateAndConfigureCompiler_RegistersModelPricingResolver`, codifying the new default.
- Alias resolution walks `ModelMappings` recursively with a visited set, so cyclic mappings resolve to "no pricing" (and therefore warn) rather than looping.
- The warning text is part of the compiler's user-facing output and will need updating if the frontmatter keys (`models.default-ai-credits-pricing`, `models.providers.*.models.*.cost`) are ever renamed.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
