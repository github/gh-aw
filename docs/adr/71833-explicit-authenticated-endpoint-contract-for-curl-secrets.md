# ADR-71833: Safe-Output Integration Boundaries for Secret Consumption

**Date**: 2026-10-25
**Status**: Proposed
**Deciders**: Not formally recorded; pending maintainer acceptance of PR #71833.

---

### Context

The secret-flow analyzer (`pkg/workflow/taint`) reports CTR-017 when symbolic secrets reach modeled disclosure sinks, including network requests. Safe-output jobs exist to integrate third-party services, which consume credentials through webhook destinations, headers, request fields, and SDK calls. Treating those integrations like ordinary jobs produces false positives; secret names or curl argument positions cannot establish the intended trust boundary. A related gap meant stdin uploads were not consistently tracked as request bodies outside that boundary.

### Decision

Treat declared safe-output jobs as trusted credential consumers for network integrations, including endpoints, headers, request bodies, and SDK calls, without an opt-in comment. The compiler supplies the normalized IDs of resolved `safe-outputs.jobs`, including imports and legacy `safe-jobs`; authored and companion AW analysis uses actual integration declarations. Ordinary jobs and native Actions workflows without AW integration provenance receive no exemption based on names or comments.

The boundary follows job provenance, not a curl-only grammar, a direct-binding shape, or variable immutability. Secrets remain tainted: explicit logs, xtrace, summaries, workflow outputs, artifacts, and known publication sinks remain checked, including when they occur alongside a network operation. This is a lint policy for intended credential consumption, not blanket suppression, recipient verification, or proof that arbitrary SDK code is safe; existing analysis coverage limitations still apply.

### Alternatives Considered

#### Alternative 1: Heuristic trust based on variable/secret name or argument position

Treat secrets named like `*WEBHOOK*`/`*SLACK*` or appearing in the URL position of curl as authenticated endpoints automatically. This would fix existing workflows without author changes. It was rejected because names and positions do not prove how the value is consumed, so a heuristic could silently hide real leaks.

#### Alternative 2: Disable or suppress CTR-017 per workflow/step

Rely on the existing broad lint disable or a step-level suppression. This is simpler, but it turns off all secret-leak checking for the scope, including logs, summaries, outputs, and publication. The integration network-consumption boundary instead preserves those checks without requiring workflow source edits.

#### Alternative 3: Restrict consumption to immutable curl destinations

Require a direct secret-bound, unmodified variable as the entire quoted curl destination, with a finite option allow-list. This is locally auditable, but excludes legitimate headers, composed credentials, other HTTP clients, and SDKs. Modeling curl's full option surface would add maintenance without fixing that mismatch; the declared integration job is the intended boundary instead.

#### Alternative 4: Require an authenticated-endpoint comment

The initial implementation required `# gh-aw: authenticated-endpoint NAME`. This was replaced because declaring a safe-output integration already establishes intended credential consumption; another opt-in imposes unnecessary source edits. The linter no longer interprets this comment.

### Consequences

#### Positive
- Removes CTR-017 false positives for intended network credential use through endpoints, headers, request fields, and SDKs in declared integrations.
- The existing safe-output integration declaration supplies the boundary without extra comments or workflow source edits.
- Preserves explicit logging, summary, output, artifact, and publication checks; ordinary jobs retain conservative network checks.
- Closes stdin-upload tracking gaps outside the integration boundary.

#### Negative
- The integration boundary asserts intent only. It does not verify the secret's real host, protocol, recipient, or SDK behavior; a secret sent to an unintended network recipient within an integration may not be reported.
- Standalone lint does not resolve imported declarations; compiler lint uses resolved imported integration jobs.
- Authors remain responsible for client configuration, recipient trust, and runtime network isolation. Unmodeled helper or SDK code is not made analyzable by the integration declaration.

#### Neutral
- The policy applies across supported network clients rather than maintaining a curl-only credential-destination allow-list.
- The contract is documented in `docs/src/content/docs/reference/workflow-linters.md`.
- Job provenance must remain consistent across authored, companion, and compiler analysis; generated job names alone cannot establish the integration boundary.

---

*Prepared for review in PR #71833. Proposed records the design under review, not maintainer acceptance or merge approval.*
