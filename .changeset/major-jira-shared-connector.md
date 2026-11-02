---
"gh-aw": major
---

Replace Jira's native tools, safe-output handlers, and gateway issue trigger with
the shared declarative connector runtime. Provider Markdown declarations are
embedded in the compiler and available through ordinary quoted
`@builtin:connectors/<provider>.md` imports.

Jira workflows must import the declaration, explicitly grant cloud IDs, issue
keys, projects, and exact queries as needed, and select read profiles and write
effects independently. Store separate encoded Basic credentials for personal MCP
reads and REST writes, or use the declared service-account read credential.
Legacy `auth`/`allowed`, automatic email/token environment injection, and
per-effect `max`/`staged` options are rejected.

The connector preserves issue creation, issue updates, ADF comments, and additive
labels. Updates currently require both summary and description. Explicit
temporary references support create-to-update/comment/label chaining through
execution-local authenticated receipts, without granting arbitrary real issue
keys. Seven formerly exposed read methods remain unavailable; no parallel
native implementation remains. Generic safe-output trusted inputs and bounded
preflights can resolve author-owned resource identifiers without exposing them
to the agent. Custom Jira ingress remains unavailable and fails closed. See the
Connectors reference for migration inputs, boundaries, and limitations.
