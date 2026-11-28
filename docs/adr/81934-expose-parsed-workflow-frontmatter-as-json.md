# 81934. Expose parsed workflow frontmatter as JSON via `gh aw parse`

- **Status:** Draft
- **Date:** 2026-11-28
- **PR:** [#81934](https://github.com/github/gh-aw/pull/81934)

## Context

Consumers that need a workflow's authored frontmatter currently have two options: import `pkg/parser` (Go only) or compile the workflow with `gh aw compile`. The PR states that consumers need parsed frontmatter "without importing `pkg/parser` or compiling workflows." Compilation writes lock files and may touch the project, and the Go import couples every consumer to the module's internal package layout.

The repository already has a Markdown frontmatter parser, `parser.ExtractFrontmatterFromContent`, which the compiler uses. The `cmd/gh-aw` root command runs a `PersistentPreRun` hook that loads project configuration, which a parse-only read should not depend on. Callers may also want to read a workflow as it existed at a specific Git commit, not as it currently sits in the working tree.

## Decision

Add a parse-only subcommand, `gh aw parse <file|->`, that reuses the existing parser and writes the frontmatter as one JSON object to stdout:

1. The command is implemented in `pkg/cli/parse_command.go` and registered from `cmd/gh-aw/frontmatter_parse_command.go` in the `utilities` group.
2. `-` reads exact Markdown content from stdin. This lets callers pipe committed content, for example `git show <commit>:path | gh aw parse -`. The command does not invoke Git itself.
3. The output contract is the parser's frontmatter map with no envelope and no Markdown body. When `imports` is present, `resolved-imports` contains the dependency-first transitive import paths. Missing, empty, or null frontmatter yields `{}`. Nested values and scalar types are preserved, and unknown fields are retained.
4. The command overrides the root `PersistentPreRun` so it does not load project configuration.
5. Malformed YAML, unclosed frontmatter, unreadable input, unresolved or cyclic imports, and JSON write errors exit nonzero.
6. The command resolves imports using the compiler's resolver, but does not validate the workflow schema, compile, install, or execute workflows. For stdin, imports resolve from the current directory or `--base-dir`, not from the source Git revision.

Tests in `pkg/cli/parse_command_test.go` cover parser parity and the resolved import closure for file and stdin input.

## Alternatives Considered

1. **Keep the Go import as the only path.** Consumers import `pkg/parser` directly. This is rejected by the PR's stated need for access without importing `pkg/parser`. Consumers outside this Go module cannot import it at all. (Inferred: the PR does not name non-Go consumers.)
2. **Read the compiled `.lock.yml` instead.** Consumers compile and parse the output. This is rejected by the PR's stated need for access "without compiling workflows." It also depends on compiler output shape rather than the authored frontmatter. (Inferred from the PR's framing.)
3. **Add a JSON flag to an existing command.** Extend an existing `gh aw` command with an output mode. Not evaluated in the PR. The PR adds a dedicated command with its own help text and a restricted contract (no compile, no config load), which an overloaded command would have to carry through its existing hooks. (Inferred: the PR does not discuss this option.)

## Consequences

**Positive**

- Consumers get a stable, scriptable JSON interface with no Go dependency and no compilation step.
- Reusing `parser.ExtractFrontmatterFromContent` and the parity tests keeps the CLI output aligned with the parser the compiler uses.
- The stdin form lets callers read a specific committed root document; local imports are read from the filesystem.

**Negative**

- The project now owns a new public CLI surface and a JSON contract. Changes to parser output (including retained unknown fields) become visible to consumers.
- Output is unvalidated. A consumer that needs schema validation must perform it separately, and typos in field names are not reported.
- Object key order is explicitly outside the contract, so consumers must not depend on it.
- The new command appears in the root command tree, which `cmd/gh-aw/command_tree_test.go` now covers.
