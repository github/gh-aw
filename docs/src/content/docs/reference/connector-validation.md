---
title: Connector Validation
description: Offline validation of experimental external connector definitions.
---

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

## Validation boundary

Validation does not contact a provider, resolve secrets, approve a connection,
register subscriptions, or execute safe outputs. A declared verification profile
does not provide an authenticated webhook receiver. External gateway sources
still require independently approved enrollment and admission.

Provider definitions describe capabilities; connection grants and workflow
bindings select authority separately. Successful definition validation does not
establish that a credential has the required provider permissions or that an
external MCP service supports unattended authentication.
