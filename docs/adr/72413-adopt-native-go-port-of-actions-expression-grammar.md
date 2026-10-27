# ADR-72413: Adopt a Native Go Port of the Official Actions Expression Grammar

**Date**: 2026-10-26
**Status**: Proposed
**Deciders**: Unconfirmed; maintainer review and acceptance are pending.

---

### Context

gh-aw parses GitHub Actions expressions (`${{ ... }}`) in many places: compiler validation, expression extraction, secret extraction, template-injection checks, import-input substitution, frontmatter hashing, and several CLI codemods. Before this change, these consumers used a mix of handwritten parsers and regular expressions. They disagreed on quoting, interpolation boundaries, and property references. For example, some helpers accepted double-quoted or backtick strings, and some matched reference-like text inside string literals. Security allowlists and frontmatter-hash parity between Go and JavaScript depend on these decisions being consistent. Production compilation must not require Node.js or add new Go module dependencies.

### Decision

Adopt one shared package, `pkg/workflow/actionslanguageservice`, to align expression consumers with the official grammar without adding a production Node.js requirement or Go module dependencies. It is a native Go port of the expression lexer/parser and the workflow schema/template reader from `actions/languageservices`, pinned at revision `4043eda158e16579cc5fb1b0b07a4bce2a76f0b5`; the upstream `workflow-v1.0.json` schema and MIT license are retained unchanged. Compiler, parser, and CLI consumers use its quote-aware interpolation scanner, AST traversal (`References`, `ReferencePattern`), and source-range rewriting while retaining existing security allowlists and runtime environment-mapping conventions.

Check conformance against the official `@actions/expressions` and `@actions/workflow-parser` packages at version `0.3.61`: ordinary Go tests replay 1,215 recorded cases offline, boundary tests exercise limits, and CI runs a live comparison through `make test-actions-parser-conformance`. This is a bounded syntax/template-reader compatibility commitment, not a claim of full runner or expression-evaluation equivalence.

### Alternatives Considered

#### Alternative 1: Keep and harden the handwritten and regex-based parsers

Fix each disagreement in the existing helpers and add shared regression tests, avoiding a large port and a vendored schema. This remains viable for isolated bugs, but it leaves gh-aw responsible for independently reconstructing the Actions grammar across security-sensitive consumers. A shared upstream-derived grammar with an official reference oracle offers a clearer compatibility target than continued local hardening.

#### Alternative 2: Use the official JavaScript parsers directly

Run `@actions/expressions` and `@actions/workflow-parser` through Node.js, or embed a JavaScript runtime in-process. Direct use avoids grammar translation and reduces port maintenance, but a Node.js bridge violates the production runtime constraint; embedding a runtime adds integration and dependency costs, including for WASM. Retain the official packages as a test-only oracle instead: running the oracle in one Node.js process can amortize conformance-test overhead without making production compilation depend on it.

#### Alternative 3: Port the full upstream workflow converter and expression evaluator

A complete port would also cover workflow conversion and expression evaluation semantics, bringing broader upstream behavior and a larger maintenance and testing surface. Defer that scope because this migration needs syntax, AST, and template-reader validation, not a runner implementation. The port deliberately excludes the converter, expression evaluation, and fetching reusable workflows; template-reader conformance must not be presented as coverage of those features.

### Consequences

#### Positive
- Every expression consumer shares one grammar. Quoting, interpolation boundaries, and reference detection behave the same in the compiler, parser, CLI, and frontmatter hash.
- Reference checks use AST access nodes, including single-quoted bracket notation. Reference-like text inside string literals no longer causes false matches or missed matches in security validation.
- Conformance can be measured against upstream through recorded cases offline and live comparisons in CI.
- The reported migration check compiled all 349 repository workflows without generated-output drift.
- The PR removes a large amount of regex and handwritten parsing code, for example in `expression_parser.go`, `expression_extraction.go`, and `secret_extraction.go`.

#### Negative
- This is a breaking syntax change. Double-quoted and backtick string literals and JavaScript-style equality spellings, which some helpers accepted before, are now rejected. Workflows that relied on them must be updated.
- The repository now contains and maintains a vendored port: about 2,800 lines of schema and a 26,000-line conformance corpus. Updating to a newer upstream revision requires porting the changes again and re-recording the corpus.
- CI now needs Node.js and npm dependencies (`conformance/package-lock.json`) for the live comparison suite.
- Validation is not fully green: standard lint passed, but custom Go linter findings and macOS/Bash/path-fixture test failures remain unresolved in the reported validation. Their resolution or explicit maintainer disposition is required before acceptance; passing conformance and no-drift checks do not clear them.
- Recorded conformance is finite: upstream reader fixtures larger than 100,000 bytes are excluded, with local boundary tests covering expression length and depth limits. Passing the corpus is not proof of equivalence for all inputs.

#### Neutral
- gh-aw's separate experiment-comparison shorthand is normalized before Actions parsing, so it is still supported.
- `--validate` passes the generated YAML through the ported schema and template reader. This adds to the existing JSON Schema, dependency, and security checks and does not replace them.
- Go/JavaScript frontmatter-hash parity relies on shared interpolation-boundary test vectors in `pkg/parser/testdata/frontmatter_hash_interpolation_vectors.json`.

---

*Proposed for maintainer review. Decider identities, approval, and disposition of outstanding validation findings have not been confirmed.*
