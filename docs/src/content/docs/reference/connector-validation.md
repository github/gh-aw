---
title: Connectors
description: Declarative third-party services with independently selected triggers, tools, context, and safe outputs.
---

## Workflow configuration

Import a connector Markdown file using the existing `imports` / `uses` / `with`
syntax, then select its APIs in the normal workflow sections:

```aw wrap
---
imports:
  - uses: .github/aw/connectors/example.md
    with:
      connection: .github/aw/connections/example.yaml
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
tools:
  tracker: [get-issue]
context:
  tracker:
    issue-details: {issue: ISSUE-1}
safe-outputs:
  tracker-add-comment:
---

Investigate the approved issue and request a comment with the findings.
```

`with` is validated and substituted by the ordinary `import-schema` machinery;
there is no separate connector import or parameter system. Nested imports,
defaults, and `imports.aw` use the same existing rules. The connection path
remains relative to the consuming repository root and contains approved resource
grants and secret names, not credentials.

The connector Markdown declares registration only:

```aw wrap
---
import-schema:
  connection:
    type: string
    required: true
connectors:
  tracker:
    definition: .github/aw/connectors/example.yaml
    connection: ${{ github.aw.import-inputs.connection }}
---

Use only the explicitly selected tracker capabilities.
```

For a self-contained, cross-repository Markdown import, replace `definition` with
`spec`, containing the complete versioned `ConnectorSpec` object. The shipped
`.github/aw/connectors/example.md` demonstrates all four capability types inline.
It needs no companion definition file. Path-based definitions, including the
eight provider wrappers, require their referenced YAML in the consuming
repository. Cross-repository Markdown imports use the normal pinned
`owner/repo/path@ref` import syntax.

Importing alone enables nothing. `tools`, `context`, and `safe-outputs` are
independently optional. Built-in APIs can coexist in the same sections.
Connector triggers and safe outputs are always prefixed with their registered
connector id and a hyphen, for example `tracker-issue-created` and
`tracker-add-comment`. The id is the registration key (`tracker`), not the
provider's authority identifier (`spec.id`). Ambiguous prefixes across imported
ids and collisions with built-in GitHub names are rejected. Unqualified
`safe-outputs.add-comment` remains the built-in GitHub operation, even when the
connector declares an operation with the same local name. Qualified entries
accept null or `{}`; behavior and input schemas come from the connector spec.

`on: {tracker-issue-created: null}` selects a declared connector trigger, but
compilation currently fails closed because authenticated custom ingress is not
operational. Use an ordinary GitHub trigger for the working examples.
Unknown capabilities, duplicate registrations, out-of-scope context, and
selection inside an imported registration are rejected. Only the main workflow
selects authority.

## Direct registration

The lower-level `connectors` form remains available for direct registration
without an import. Its capability parts are independently optional:

```aw wrap
---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
connectors:
  tracker:
    definition: .github/aw/connectors/example.yaml
    connection: .github/aw/connections/example.yaml
    tools: [get-issue]
    context:
      issue-details:
        issue: ISSUE-1
    safe-outputs: [add-comment]
---

Investigate the approved issue using the supplied context.
Request an add-comment safe output with the findings.
```

The example selects capabilities from the repository's
[`ConnectorSpec` example](https://github.com/github/gh-aw/blob/main/pkg/connectors/testdata/example.yaml),
saved at the `definition` path. Its connection contains secret **names**, not
credential values:

```yaml
apiVersion: github.com/gh-aw/connectors/v1
kind: ConnectorConnection
connector: example.com/issues
credentials:
  reader: {secret: EXAMPLE_READ_TOKEN}
  context-reader: {secret: EXAMPLE_CONTEXT_TOKEN}
  writer: {secret: EXAMPLE_WRITE_TOKEN}
resources:
  issues: [ISSUE-1]
```

| Field under `connectors.<name>` | Meaning |
| --- | --- |
| `definition` | Repository-relative `ConnectorSpec` YAML or JSON file; mutually exclusive with `spec`. |
| `spec` | Complete inline `ConnectorSpec`; makes a Markdown import self-contained. |
| `connection` | Required repository-relative `ConnectorConnection` file with approved resource grants and secret names. |
| `on` | Optional list of declared native trigger names. Compilation currently rejects activation because no approved custom receiver ships with this prototype. |
| `tools` | Optional list of declared `mcp` read tools or `httpMcp` registrations. Ambiguous names are rejected. |
| `context` | Optional map of context recipe names to literal input argument objects. Arguments must satisfy the recipe's types and resource grants. Actions expressions are unsupported. |
| `safe-outputs` | Optional list of declared typed effect names. Selecting an effect does not perform it. |

For tools-only use, retain `definition`, `connection`, and `tools`; no context or
write credential is required. Context-only and safe-output-only configurations
work the same way. Built-in `on`, `tools`, and `safe-outputs` settings remain
available alongside connectors. Imported registrations instead use
`tools.<alias>`, `context.<alias>`, and qualified
`safe-outputs["<alias>-<operation>"]` as shown above.

The compiler validates selected names against the external definition and freezes
the selected content into phase-restricted, content-addressed runtime plans.
Paths cannot escape the repository, including through symlinks. Changing an
external definition or connection requires recompilation; runtime hosts do not
reload those files. Each encoded phase plan is limited to 65,536 bytes.

Read tools run through a constrained, containerized MCP adapter named
`connector-<name>`, not an unrestricted remote registration. Context is fetched
before agent execution and supplied as explicitly untrusted data. Write
credentials appear only in the safe-output job. Effects use existing staged
safe-output transport, with argument and resource revalidation before execution.
Trial and staged compilation use credential-free intent validation. Existing
dry-run restrictions on custom MCP hosts and effect scripts still apply.
Generated effect tool names include the alias length to avoid normalized name
collisions; for example, `tracker/add-comment` becomes
`connector_7_tracker_add_comment`.

Shared AW imports may register connectors but cannot select their capabilities.
Custom webhook activation and connector effects for work-queue participants
remain unsupported and fail compilation rather than silently dropping behavior.
No receiver, OAuth broker, subscription renewal, or provider deployment is
installed by declaring a connector.

Development mode builds the runtime in an isolated checkout of `github/gh-aw`
at the compiler's published git reference, never from the agent workspace or
an agent-produced artifact. An unversioned development compiler cannot emit
connector runtime setup. Release mode installs the pinned CLI release.

## Definition validation

`gh aw connectors validate` checks local YAML or JSON `ConnectorSpec` files.
The experimental contract version is `github.com/gh-aw/connectors/v1`.

```bash
gh aw connectors validate .github/aw/connectors/linear.yaml
gh aw connectors validate .github/aw/connectors/*.yaml --json
```

The command requires at least one file and checks every supplied definition.
It rejects unknown fields, unsupported versions, invalid capability declarations,
and violations of the shared contract's security constraints.

| Option | Behavior |
| --- | --- |
| `<file>...` | One or more local definition paths; the shell expands wildcards. |
| `--json`, `-j` | Emit a JSON array with `file`, `valid`, optional `id`, and optional `error`. |

Exit status is zero only when every definition is valid. Invalid definitions
remain in JSON output, and a validation failure is also returned to the caller.
File read and output errors are failures.

## Binding plans

`gh aw connectors plan <binding> --root <repository>` validates a
`ConnectorBinding`, its definition, its connection grants, and selected
capabilities. Referenced definition and connection paths are confined to the
repository root, which defaults to the current directory.

```bash
gh aw connectors plan .github/aw/bindings/linear.yaml --root .
```

The JSON result contains a content-addressed `digest` and `operational: false`.
The digest covers the resolved definition, connection secret references and
resource grants, and workflow selection. It is not a deployable workflow or
proof that external credentials work.

## Editor schemas

`gh aw connectors schema <kind>` prints structural JSON Schema for
`ConnectorSpec`, `ConnectorConnection`, or `ConnectorBinding`.

```bash
gh aw connectors schema ConnectorSpec
```

Structural schemas support editor completion and shape validation. The
`validate` and `plan` commands additionally enforce semantic constraints.

## Validation boundary

Validation does not contact a provider, resolve secrets, approve a connection,
register subscriptions, or execute safe outputs. A declared verification profile
does not provide an authenticated webhook receiver. External gateway sources
still require independently approved enrollment and admission.

Provider definitions describe capabilities; connection grants and workflow
bindings select authority separately. Successful definition validation does not
establish that a credential has the required provider permissions or that an
external MCP service supports unattended authentication.

## Runtime host commands

The compiler uses `gh aw connectors serve` for constrained stdio MCP reads and
`gh aw connectors invoke <context|safe-output> <operation>` for deterministic
operations. These require a compiler-frozen, single-phase plan in
`GH_AW_CONNECTOR_PLAN` and resolve only that plan's phase-specific credential
environment. They do not load mutable repository definitions or acquire tokens.

`invoke` reads one JSON argument object from stdin. Safe-output execution requires
`--allow-writes`; `--stage` instead validates and returns an intent without
resolving credentials or contacting the service. These flags are mutually
exclusive. Provider responses are bounded, redirects refused, and writes are
never automatically retried.
