# ADR-72413: Adopt a Native Go Port of the Official Actions Expression Grammar

**Date**: 2026-10-26
**Status**: Draft
**Deciders**: pelikhan [TODO: verify]

---

### Context

gh-aw parses GitHub Actions expressions (`${{ ... }}`) in many places: compiler validation, expression extraction, secret extraction, template-injection checks, import-input substitution, frontmatter hashing, and several CLI codemods. Before this change, these consumers used a mix of handwritten parsers and regular expressions. They disagreed on quoting, interpolation boundaries, and property references. For example, some helpers accepted double-quoted or backtick strings, and some matched reference-like text inside string literals. Security allowlists and frontmatter-hash parity between Go and JavaScript depend on these decisions being consistent. Production compilation must not require Node.js or add new Go module dependencies.

### Decision

We will replace the legacy expression parsers with one shared package, `pkg/workflow/actionslanguageservice`. It is a native Go port of the expression lexer/parser and the workflow schema/template reader from `actions/languageservices`, pinned at revision `4043eda158e16579cc5fb1b0b07a4bce2a76f0b5`. The upstream `workflow-v1.0.json` schema and the MIT license are kept unchanged. Compiler, parser, and CLI consumers will use the port's quote-aware interpolation scanner, AST traversal (`References`, `ReferencePattern`), and source-range rewriting instead of regular expressions. Existing security allowlists and the runtime environment-mapping conventions are kept. We will check conformance against the official `@actions/expressions` and `@actions/workflow-parser` packages (version `0.3.61`) in two ways. Ordinary Go tests replay 1,215 recorded cases offline, and CI runs a live comparison through `make test-actions-parser-conformance`.

### Alternatives Considered

#### Alternative 1: Keep and harden the handwritten and regex-based parsers

We could fix each disagreement in the existing helpers one at a time. This avoids a large port and a vendored schema. We rejected it because the scattered parsers had already drifted apart. Without a single grammar and a reference oracle, consumers would keep disagreeing about quoting and reference boundaries.

#### Alternative 2: Call the official JavaScript parsers at compile time

The compiler could run `@actions/expressions` and `@actions/workflow-parser` through Node.js. That would match upstream exactly with less ported code. We rejected it because production compilation, including the WASM build, must run without Node.js or npm. The PR keeps the official packages only as a test oracle.

#### Alternative 3: Port the full upstream workflow converter and expression evaluator

A complete port would also cover the converter and expression evaluation semantics. We rejected this scope because gh-aw needs only syntax, AST, and template-reader validation. The PR deliberately excludes the converter, expression evaluation, and fetching reusable workflows.

### Consequences

#### Positive
- Every expression consumer shares one grammar. Quoting, interpolation boundaries, and reference detection behave the same in the compiler, parser, CLI, and frontmatter hash.
- Reference checks use AST access nodes, including single-quoted bracket notation. Reference-like text inside string literals no longer causes false matches or missed matches in security validation.
- Conformance can be measured against upstream through recorded cases offline and live comparisons in CI.
- The PR removes a large amount of regex and handwritten parsing code, for example in `expression_parser.go`, `expression_extraction.go`, and `secret_extraction.go`.

#### Negative
- This is a breaking syntax change. Double-quoted and backtick string literals and JavaScript-style equality spellings, which some helpers accepted before, are now rejected. Workflows that relied on them must be updated.
- The repository now contains and maintains a vendored port: about 2,800 lines of schema and a 26,000-line conformance corpus. Updating to a newer upstream revision requires porting the changes again and re-recording the corpus.
- CI now needs Node.js and npm dependencies (`conformance/package-lock.json`) for the live comparison suite.
- The PR author reports that custom Go linter findings and some test failures remain open. [TODO: verify these are resolved before acceptance]

#### Neutral
- gh-aw's separate experiment-comparison shorthand is normalized before Actions parsing, so it is still supported.
- `--validate` passes the generated YAML through the ported schema and template reader. This adds to the existing JSON Schema, dependency, and security checks and does not replace them.
- Go/JavaScript frontmatter-hash parity relies on shared interpolation-boundary test vectors in `pkg/parser/testdata/frontmatter_hash_interpolation_vectors.json`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
