---
"gh-aw": major
---

Replace the native Jira, Linear, and Azure DevOps tools, safe-output handlers,
and gateway issue triggers with the shared declarative connector runtime.
Provider Markdown declarations are editable files under
`.github/workflows/connectors/<provider>.md`, consumed through ordinary local or
pinned `owner/repo/path@ref` imports. No provider definitions are compiled in.

Jira workflows must import the declaration, explicitly grant cloud IDs, issue
keys, projects, and exact queries as needed, and select read profiles and write
effects independently. Store separate encoded Basic credentials for personal MCP
reads and REST writes, or use the declared service-account read credential.
Legacy `auth`/`allowed` and automatic email/token environment injection are
rejected. Connector safe-output selections support per-operation `max` budgets
and `staged` previews in addition to workflow-wide staging.

The connector preserves issue creation, issue updates, ADF comments, and additive
labels. Updates currently require both summary and description. Explicit
temporary references support create-to-update/comment/label chaining through
execution-local authenticated receipts, without granting arbitrary real issue
keys. Seven formerly exposed read methods remain unavailable; no parallel
native implementation remains. Generic safe-output trusted inputs and bounded
preflights can resolve author-owned resource identifiers without exposing them
to the agent. Imported trigger selections are wired to the protected webhook
gateway; receiver authentication, enrollment and deployment remain external. See the
Connectors reference for migration inputs, boundaries, and limitations.

Linear consumers must replace native toolsets and safe outputs with explicit
connector imports and selections. Trusted resource preflights preserve the
smoke workflow's secret-derived team and project-slug destination without
granting those inputs to the agent. Broad native MCP toolsets, friendly-name
chaining, and legacy destination options are not implicit compatibility paths.
Per-operation `max` and `staged` remain explicit supported controls.

Azure DevOps consumers must import the connector and use separate read/write
Entra credentials; automatic PAT/system-token wiring is removed. Hosted MCP
currently exposes project metadata, while the declared mutations remain bounded
to title changes, comments, assignment, and fixed related links. Other legacy
work-item fields, attachments, and temporary-ID options are not drop-in
replacements. The gateway opt-in, App-provenance and workflow-version admission
gates apply to all imported triggers. Unsupported or unenrolled receivers remain
nonactivating; compilation does not install provider webhooks.

Connector credentials now select explicit stored-token, GitHub App installation
token, or GitHub OIDC identity-token routes. Minting is job- and phase-local;
App permissions and OIDC audiences are explicit, and staged writes do not mint.
There is no automatic provider token exchange or credential fallback.
