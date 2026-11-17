# ADR-78624: Preserve Indentation-Sensitive Prompt Chunks in Compiled YAML

**Date**: 2026-11-17
**Status**: Draft
**Deciders**: pelikhan (PR author); Copilot coding agent (implementation) [TODO: verify]

---

### Context

gh-aw compiles a workflow's Markdown prompt into `GH_AW_PROMPT_CONTENT_NNNN` environment values, split into chunks of about 20.9 KB. The chunks are re-marshaled with goccy/go-yaml under `DefaultMarshalOptions`, which emits multiline strings as bare `|` literal block scalars. If a chunk's first content line starts with spaces or tabs (for example, a continuation line of a Markdown list item), YAML infers the wrong block indentation and the generated lock file becomes invalid. Whether this happens depends on where a chunk boundary falls (issue #76963, still open). The lock file must remain valid YAML, prompt text must survive unchanged, and compilation should fail rather than write a broken lock file.

### Decision

We will emit multiline strings that literal block style cannot represent safely as double-quoted scalars. A new `marshalYAMLWithOptions` wrapper in `pkg/workflow/yaml_options.go` applies this rule when a string has no content after its leading newlines, starts a content line with a space or tab, contains a CR, or ends with a space, tab, or blank line. Prompt steps are also round-trip checked (`validatePromptContentRoundTrip`) before emission, so any altered `GH_AW_PROMPT_CONTENT_*` value fails compilation. The primary driver is correctness: the compiler guarantees that emitted prompt text matches the intended text.

### Alternatives Considered

#### Alternative 1: Double-quote every multiline string

This is the simplest way to avoid literal-block inference, and it needs no predicate. It was not chosen because it would change every multiline value in every generated lock file. The implementation keeps literal style for ordinary multiline strings and falls back to the original marshal path when nothing needs repair [TODO: verify the PR discussion for the stated rationale].

#### Alternative 2: Change the shared marshal options

Removing `yaml.UseLiteralStyleIfMultiline(true)` from `DefaultMarshalOptions` would avoid the bare-literal problem. It was not chosen because `DefaultMarshalOptions` is shared by many emitters, so the change would alter output far beyond the affected prompt chunks. The PR leaves `DefaultMarshalOptions` unchanged.

#### Alternative 3: Move chunk boundaries away from indented lines

Adjusting `splitContentIntoChunks` so that no chunk starts with indentation would avoid this input. It was not chosen because the failure depends on where boundaries fall and reappears whenever prompt text changes (issue #76963). The new regression test deliberately keeps an indented chunk start and checks the marshaled output, so the fix does not depend on chunk layout.

### Consequences

#### Positive
- Multiline values that would break literal block YAML are quoted, so generated lock files parse for the cases covered by the new tests.
- `validatePromptContentRoundTrip` turns a prompt-text mismatch into a compile error instead of silent corruption.
- Ordinary multiline values keep literal style, and output without repairs is unchanged.
- Regression tests cover leading spaces and tabs, leading newlines, CRLF, trailing whitespace, and parsing with both goccy and yaml.v3.

#### Negative
- Marshaling a document with multiline strings now encodes to a node and walks its AST before the real encode. Prompt steps are also parsed again after encoding. The compile-time cost is not measured in the PR [TODO: verify].
- The repair depends on goccy/go-yaml AST and token internals (`ast.Filter`, `token.DoubleQuoteType`). A library upgrade could change this behavior.
- Only the call sites converted in this PR use the wrapper. Other direct `yaml.MarshalWithOptions` calls remain, for example in `trigger_parser.go`, `tools.go`, `compiler_workflow_call.go`, and `codemod/`. The guarantee does not cover them [TODO: verify whether any can carry multiline user content].

#### Neutral
- Values that need repair are written as escaped double-quoted scalars, which are harder to read in lock files than literal blocks.
- The trailing-blank-line check (`\n\n` suffix) changes output only when another unsafe string appears in the same marshal call, because otherwise the original value is re-marshaled. Whether that check is needed is [TODO: verify].
- The round-trip check covers only `GH_AW_PROMPT_CONTENT_*` env keys. Other values are not verified by it.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
