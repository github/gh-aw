# ADR-73529: Per-Input `maxLength` for Custom Safe-Output String Inputs

**Date**: 2026-10-31
**Status**: Draft
**Deciders**: Unknown — [TODO: verify] (PR author / gh-aw maintainers)

---

### Context

Custom safe-output jobs (`safe-outputs.jobs.<name>.inputs`) are exposed to the agent as MCP tools, and every string input is validated at runtime by `schema-validation.cjs` against a fixed default limit of 10,240 bytes (`MAX_STRING_INPUT_BYTES`). Some workflows need to pass larger structured values (for example, generated reports) through a custom safe-output job, and the fixed limit forces the agent to shorten or truncate those payloads. The existing workaround, `artifacts:`, moves the payload out of the safe-output item, so later stages that inspect the item, such as threat detection and post-processing, cannot see it. The runtime validator already honors a JSON Schema `maxLength` on tool input properties (measured in characters), so the missing piece is a way for workflow authors to set it (issue #73520).

### Decision

We will add an optional `maxLength` property to custom safe-output job input definitions. It accepts integers from 1 to 1,000,000, is valid only for `string` inputs or inputs with no `type`, and the frontmatter schema rejects it for `boolean`, `number`, `choice`, and `environment` inputs. The compiler stores it as `InputDefinition.MaxLength` (`*int`) and emits it as the MCP tool property `maxLength`. That value then replaces the default 10,240-byte limit for that input. Inputs that omit `maxLength` keep the existing byte-based default unchanged. We chose this approach because it reuses the existing runtime `maxLength` handling and keeps the decision explicit and per-input.

### Alternatives Considered

#### Alternative 1: Raise the global default string limit

Increase `MAX_STRING_INPUT_BYTES` for all custom job inputs. This is the simplest change, but every existing workflow would silently accept much larger agent outputs. That increases the surface for prompt-injected bulk data and raises token and detection costs for workflows that never needed it. We rejected it in favor of an opt-in per input.

#### Alternative 2: Require `artifacts:` for large payloads

Keep the 10 KB limit and direct authors to write large payloads to files under `/tmp/gh-aw/` and declare them in `artifacts:`. This path already exists and has secret redaction, but artifact contents are not part of the safe-output item. Threat detection and post-processing would therefore not inspect them, which defeats the purpose for structured values that must stay in the item. The documentation keeps `artifacts:` as the recommended path for files and directories.

#### Alternative 3: Byte-based limit (`max-bytes`) instead of characters

Express the override in bytes to match the default unit. We did not choose this because the runtime validator's existing `maxLength` path counts characters, as JSON Schema does. A byte-based override would need new runtime logic. As a result, the default (bytes) and the override (characters) use different units. [TODO: verify this was deliberately considered]

### Consequences

#### Positive
- Authors can allow large string payloads for a specific input without affecting other inputs or workflows.
- Large values stay in the safe-output item, so threat detection and post-processing can still inspect them.
- No runtime change is needed: the compiler emits the standard `maxLength`, which `schema-validation.cjs` already enforces.
- The change is backward compatible because inputs without `maxLength` keep the 10,240-byte default.

#### Negative
- The units differ: the default limit is measured in bytes, while `maxLength` is measured in characters. With multi-byte text, a `maxLength` of N can allow more than N bytes.
- A per-input ceiling of 1,000,000 characters allows very large agent outputs, which can increase safe-output artifact size, detection cost, and the amount of untrusted data passed to custom jobs.
- The type restriction is enforced in two places: the JSON Schema `allOf`/`if`/`then` rule and a type check in `populateCustomJobToolProperties`. Both must stay in sync.

#### Neutral
- `parseInputMaxLength` silently ignores non-positive, non-integer, or unsupported numeric values (it returns `nil`) and relies on schema validation to reject them first.
- A new WASM golden fixture (`custom-safe-output-max-length`) and documentation in `custom-safe-outputs.md` were added to describe and lock in the behavior.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
