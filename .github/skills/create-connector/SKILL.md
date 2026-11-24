---
name: create-connector
description: Create or extend a declaration-only third-party connector, with narrow reads, separate safe outputs, optional protected triggers, and offline conformance evidence.
---

# Create a Connector

For new providers or capabilities. For setup, use
[the connector guide](../../aw/connectors.md).

## Load the contract, not a catalog

Read the [specification](../../../docs/src/content/docs/specs/connector-specification.md),
closest declaration in `.github/workflows/connectors/`, its `docs` references
and matching `connector-documentation` XML. Product names, old handlers and
guessed scopes are not API evidence. Read the
[authoring guide](../../aw/connectors.md#author-or-extend-a-declaration) for
advanced policies. Do not discover skills remotely.

Establish one scenario, exact resources, read evidence, effect, credential route
and trigger evidence. Mark capabilities unsupported when API/auth details are
unavailable; never invent endpoints, scopes or tools.

## Create the smallest source

Create `.github/workflows/connectors/<id>.md` with `connectors.<id>` and
`import-schema`. Start from a nearby declaration or specification section 9;
its synthetic API is offline-only, never a live target.

| Need | Declare |
| --- | --- |
| Workflow inference | `inference.<profile>`: fixed endpoint/path/protocol, inference credential, model grants. Select `engine.inference`; follow specification 5.4. Never use a reader. |
| REST/fixed GraphQL read | `tools.<read>`: exact `tool`, typed inputs, fixed request, minimal checked projection. |
| Hosted HTTP MCP read | `mcp-servers.<registration>`: fixed URL, `readOnly: true`, exact native schemas/grants. |
| Provider mutation | `safe-outputs.<operation>`: distinct writer credential, exact resource checks. |
| Native event | `triggers.<name>`: fixed `resourceOrigins`, verification profile, predicates, normalization and grants; no ingress deployment. |
| Content/context | A selected reader; no public `context` or automatic hydration. |

Set `experimental: true` for new contracts. Add consulted official HTTPS `docs`.
Expose resource arrays and secret NAMES through `import-schema`; never put tokens
in `with`, source or prompts.
Describe bindings; type resource-array items explicitly as strings. Order:
metadata, endpoints, credentials, resources, inference, triggers, readers, hosted
MCP, safe outputs. Put input type/required before constraints. Reuse anchors only
for identical primitives, not independent grants/phases. Preserve scalar quoting,
anchor-before-alias and merge-before-override order. Verify nested aliases with
the compiler and bundled runtime, not only a YAML parser.
Choose one: `token`, scoped `github-app`, direct `github-oidc`, or app-only
`azure-oidc` (specification 4). Live writers need a separate client and exclusive
literal `safe-outputs.environment`. Trust only its subject. Inference: see 5.4.

Bound strings/responses; request placeholders take single safe segments.
New readers use kebab-case selectors and unprefixed snake_case `tool` names.
Preserve existing and exact hosted native names. Query/body values use
`input`, `literal`, `object`, or `array`.
Resource inputs are required strings checked against exact grants before secrets.
Correlate the returned ID with the requested ID, not merely some allowed ID.
Reject partial pages when completeness matters. Project only needed evidence.
Mark human text with `sanitize: title/body`; use `body-plain` for literal API text
or ADF text nodes so publisher markup is not sent as content. Both body formats
retain protected attribution and sizing. Do not sanitize IDs as body text.
Use `body-plain-compact` only for APIs whose documented representation collapses
consecutive ASCII spaces and newlines, so exact echoed-text checks stay intact.

No provider runtime code, checkout/build/install hooks, dynamic URLs/queries/scripts,
automatic pagination, arbitrary patches, credential fallbacks or response-derived
grants. Report unexpressible behavior; extend shared infrastructure only when
explicitly requested.

## Supply a complete consumer and documentation

Enclose the whole body in one XML/HTML comment with matching
`connector-documentation`: purpose, acquisition/renewal, phase names/scopes,
input/grant contracts, evidence/privacy limits and complete consumer YAML/prompt.
No secret values, double-hyphen sequences, nested comments or frontmatter
delimiters inside it. No instructions outside: the body becomes a system prompt.
Keep credential formats in import metadata; reference inputs from the comment.
Full-source scans still apply without exemptions. Documentation is stripped
from prompts, never executed as setup.

Create a consumer with `uses`/`with`, `workflow_dispatch`, explicit network
approval, one reader and one `staged: true`, `max: 1` effect. For inference-only
connectors, select an inference profile instead of a reader. Import-only must
not change engine routing, credentials, permissions or network. Test model grants,
unsupported protocols/engines and native/imported override conflicts. Exclude
inference credentials from reader/effect plans. Validate inference by compiling,
not with `connector test --phase inference` or live offline-test calls.
For HTTP MCP prefer `{registration: [exactNativeTool]}` over the whole profile.
Specimen grants must match in YAML and prompt. Say what the read does NOT prove
and never claim staged delivery. Importing alone must select nothing.

Add `pkg/cli/workflows/<id>-golden.md` to the scenario/CLI compilation matrix.
Never install goldens under `.github/workflows/` or commit fixture locks.
Document setup and evidence limits in the human reference.

## Validate offline first

Use a connector-capable compiler and matching bundled runtime. Run `connector
<id> setup`, `compile <consumer> --strict --no-emit`, and applicable `connector
<id> test --phase safe-output --capability NAME`, all through `gh aw`.
Use `--file` for the declaration, `--args` for schema-valid JSON arguments, and
`--node-runtime actions/setup/js/connector_runtime.cjs` for runtime parity.
`--input name=JSON` binds an import input;
`--grant collection=JSON-array` replaces a declared resource grant.
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
