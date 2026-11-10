# checkoutpolicy Package

> Predicates for assessing checkout inputs in `pull_request_target` workflows.

## Public API

| Function | Description |
|----------|-------------|
| `MatchesExpression` | Accepts one complete Actions expression whose reference exactly matches the expected context path. |
| `Base` | Accepts supported base-repository and base-ref forms. |
| `LiteralPair` | Reports whether a repository/ref pair is eligible for exact allowlist matching. |
| `LiteralRepository` | Validates a literal `owner/repository` selector. |
| `LiteralRef` | Validates a literal ref while excluding expression, wildcard, and `refs/pull/` forms. |

These are input predicates, not authorization decisions. In particular, callers must separately reject additional fetches and wiki options, and must verify allowlist membership.
