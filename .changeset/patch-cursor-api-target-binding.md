---
"gh-aw": patch
---

Let users bind Cursor to its upstream with `engine.api-target`, defaulting to
`api2.cursor.sh`. Add a declarative `execution.api-target-env-var` binding so
custom engines can expose the standard API-target field without requiring users
to configure gateway-specific environment variables. The driver continues to
discover and use only the AWF gateway endpoint.

Exchange Cursor API keys for session tokens on the trusted runner before AWF
starts, using a paired credential preparation script and provider variable.
The session token stays in the sidecar and Cursor receives a placeholder,
avoiding the native authentication exchange failure observed in the smoke run.
