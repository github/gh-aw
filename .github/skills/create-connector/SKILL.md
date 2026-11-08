---
name: create-connector
description: Create or extend a declaration-only third-party connector, with narrow reads, separate safe outputs, optional protected triggers, and offline conformance evidence.
---

# Create a Connector

Use this skill when adding a provider or extending its declarative capabilities.
For consumption/setup only, use [the connector guide](../../aw/connectors.md).

## Load the contract, not a catalog

Read the [connector specification](../../../docs/src/content/docs/specs/connector-specification.md)
and only the closest declaration in `.github/workflows/connectors/`.
Use its `docs` references and matching `connector-documentation` XML, not a
product name, old native handler, or guessed OAuth scope as API evidence.
Read the [authoring guide](../../aw/connectors.md#author-or-extend-a-declaration)
for advanced policies. No remote skill discovery is needed.

Confirm one useful consumer scenario, exact authorized resources, needed
read evidence, desired effect, credential route, and any trigger evidence.
If the provider's API or authorization details are unavailable, mark that
capability unsupported instead of inventing an endpoint, scope, or tool.

## Create the smallest source

Create `.github/workflows/connectors/<id>.md` with `connectors.<id>` and ordinary
`import-schema`. Start from section 9 of the specification or the closest
existing provider. Its synthetic API is an offline shape example, never a
real provider or live-test target.

| Need | Declare |
| --- | --- |
| Bounded REST/fixed GraphQL read | `tools.<read>` with exact `tool`, typed `inputs`, fixed request, minimal checked projection. |
| Hosted streamable-HTTP MCP read | `mcp-servers.<registration>` with fixed URL, `readOnly: true`, exact native `tools` schemas and grants. |
| Provider mutation | `safe-outputs.<operation>` with a distinct safe-output credential and exact resource checks. |
| Native event | `triggers.<name>` with fixed `resourceOrigins`, verification profile, evidence predicates, normalization and grants; no ingress deployment. |
| Resource content/context | A selected reader. Do not add public `context` or automatic hydration. |

Set `experimental: true` for a new unproven contract. Add literal official
HTTPS `docs` URLs actually consulted. Expose resource arrays and secret NAMES
through `import-schema`; tokens never belong in `with`, source, or prompts.
Use `token`, scoped `github-app`, or audience-bound `github-oidc` only when that
route is valid for the actual provider. Neither GitHub route exchanges tokens.

Bound every string and response. Request placeholders take safe individual
segments; query/body values use `input`, `literal`, `object`, or `array`.
Resource inputs are required strings checked against exact grants before secrets.
Correlate the returned ID with the requested ID, not merely some allowed ID.
Reject partial pages when completeness matters. Project only needed evidence.
Mark human text with `sanitize: title/body`; do not sanitize IDs as body text.

Do not add per-provider runtime code, checkout/build/install hooks, dynamic
URLs/queries/scripts, automatic pagination, arbitrary patches, credential
fallbacks, or grants derived from responses. If the shared contract cannot
express a required behavior, report the gap; extend reusable infrastructure
only when the task explicitly includes that implementation.

## Supply a complete consumer and documentation

Add one matching XML comment with purpose, acquisition/renewal instructions,
phase credential names/scopes, input/grant contracts, evidence/privacy limits,
and complete consumer YAML/prompt elements. Never include secret values or
double-hyphen sequences in the outer comment. Do not embed Markdown
frontmatter delimiters inside it. Documentation is stripped from prompts,
not executed as setup.

Create a minimal consumer with normal `uses`/`with`, `workflow_dispatch`,
explicit network approval, one reader and one `staged: true`, `max: 1` effect.
For HTTP MCP prefer `{registration: [exactNativeTool]}` over the whole profile.
Specimen grants must match in YAML and prompt. Say what the read does NOT prove
and never claim staged delivery. Importing alone must select nothing.

In this repository, add a source-only fixture to
`pkg/cli/workflows/<id>-golden.md`, following the existing golden harness.
Wire the new fixture into its provider scenario/CLI compilation matrix; do not
install goldens under `.github/workflows/` or commit fixture lock files.
Update the human connector reference with honest setup and evidence limits.

## Validate offline first

Use a connector-capable compiler and the matching bundled runtime:

```bash
gh aw connector <id> setup --file .github/workflows/connectors/<id>.md
gh aw compile <consumer> --strict --no-emit
gh aw connector <id> test --phase safe-output --capability <operation> \
  --file .github/workflows/connectors/<id>.md \
  --input 'resources=["approved-id"]' \
  --args '{"resource-id":"approved-id","text":"Offline conformance example"}' \
  --node-runtime actions/setup/js/connector_runtime.cjs
```

The last command is a shape illustration: replace input, argument, and operation
names with the actual declaration's schema. `--input name=JSON` binds an import
input; `--grant collection=JSON-array` replaces a declared resource grant.
Annotated safe-output text requires the matching Node runtime and dependencies.
Setup and safe-output tests read no credentials and perform no provider writes.
An agent-phase test DOES contact the provider; do not run it as offline validation.

Add tests for import-only/no capability, phase pruning, exact native selection,
invalid arguments/grants before credentials, wrong response identity/types,
staging with zero secret/HTTP accesses, publication bounds, and Go/Node parity.
For supported triggers, add complete/nonmatching native fixtures and frozen
predicate parity; projection must report `operational: false`.
Check local/pinned imports, extracted XML examples, and deployment file closure.
Run `make compile-cli-workflows` for the golden matrix and follow the
[developer gates](../developer/SKILL.md) for code changes.

Report source, scenario, exposed methods/effect, declared credential routes,
offline evidence, and unsupported behavior. Do not claim consent, receiver
enrollment, token validity, provider delivery, or useful output quality from a
compile or mock. Live execution requires the
[debugging gates](../../aw/debug-agentic-workflow.md), explicit effect scope,
and exact reviewed published revision. Stop on authorization denial.
