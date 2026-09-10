# ADR-59971: Add Frontmatter Format Command

**Date**: 2026-09-10
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

`gh-aw` workflows store behavior in YAML frontmatter embedded in Markdown files, and this PR adds a CLI path that rewrites that frontmatter. The diff shows repeated requirements to preserve Markdown body content and comments, while also normalizing indentation, key naming via existing codemods, and deterministic field ordering. It also adds support for formatting individual workflows, all workflows in the default directory, and files under a custom directory. The architectural question is how workflow frontmatter normalization should be implemented without introducing a separate configuration format or breaking user-authored comments.

### Decision

We will add a dedicated `gh aw format` command that applies the existing codemod pipeline and then rewrites workflow frontmatter through a YAML AST-based normalizer while preserving comments and the Markdown body. We decided to integrate formatting into the CLI command set and operate directly on workflow Markdown files because the PR evidence shows the desired behavior is repository-local, repeatable, and aligned with other `gh aw` authoring commands. This approach makes frontmatter canonicalization an explicit authoring workflow instead of an incidental side effect of compile or validation paths.

### Alternatives Considered

#### Alternative 1: Reformat frontmatter implicitly during compile or validation

The project could normalize workflow frontmatter whenever users run compile, validate, or related commands. This was considered because those commands already parse workflows and would have access to the same frontmatter data. It was not chosen because the diff introduces a standalone `format` command and targeted CLI tests, indicating a need for an explicit write operation that users can run on demand without coupling source-file mutation to read-oriented commands.

#### Alternative 2: Implement formatting with text-based string rewriting only

Another option would be to format the frontmatter with line-oriented replacements or regex-style transforms and avoid YAML AST handling. This was considered because it could be simpler for a narrow subset of field migrations. It was not chosen because the implementation and tests emphasize preserving comments, handling nested mappings, and avoiding false delimiters inside block scalars, which are stronger fits for structured YAML node processing than plain text rewriting.

#### Alternative 3: Leave frontmatter style unmanaged and rely only on codemods

The team could keep workflow syntax migrations limited to existing codemods and avoid introducing a canonical formatting pass. This was considered because it minimizes new CLI surface area and avoids file rewrite logic. It was not chosen because the PR explicitly adds deterministic ordering, indentation normalization, and round-trip coverage, showing that consistent formatting is now treated as part of workflow maintainability rather than an optional manual convention.

### Consequences

#### Positive
- Workflow authors get a dedicated command to canonicalize frontmatter structure across one file, many files, or custom workflow directories.
- Existing codemods can be reused before formatting, allowing syntax migrations and canonical layout to happen in a single operation.
- YAML-node-based normalization can preserve comments and Markdown content while still enforcing deterministic ordering and indentation.

#### Negative
- The CLI surface area grows, adding another authoring command that maintainers must document, test, and support.
- Formatting now depends on careful frontmatter parsing and YAML encoding behavior, which can introduce edge cases around delimiters, block scalars, or comment retention.
- Rewriting files in place may create larger diffs for users when canonical ordering changes multiple fields at once.

#### Neutral
- The command is grouped with other development-oriented CLI commands rather than changing execution behavior.
- Test coverage now includes comment preservation, round-tripping, block scalar handling, field ordering, and command registration.
- The formatter works on Markdown workflow files directly and does not introduce a new persisted intermediate representation.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
