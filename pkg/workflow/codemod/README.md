# codemod Package

> Source-preserving transformations of authored agentic workflow frontmatter.

## Overview

`codemod` edits the authored AST from [`aw`](../aw/README.md), not compiler-generated defaults. Its operations preserve Markdown and merge frontmatter edits into the original YAML so comments, quoting, and unrelated source formatting remain intact.

## Public API

| Symbol | Description |
|--------|-------------|
| `Frontmatter` | An editing view containing the parsed `aw.Document` and its source text. |
| `Apply` | Runs a transformation atomically. On error, it returns the original content; a no-op returns byte-identical content. |
| `Frontmatter.Lookup` | Looks up an exact mapping path and distinguishes an absent field from a present `null`. |
| `Frontmatter.Edit` | Applies semantic edits to an independent document and preserves the Markdown body. |
| `Frontmatter.Set`, `Remove`, `Rename`, `PruneEmpty` | Updates authored mapping paths while retaining source syntax when possible. |
| `Frontmatter.Comment` | Adds a single-line annotation to a mapping field without replacing existing comments. |
| `Frontmatter.RewriteText` | Applies a lexical transformation to frontmatter while keeping the Markdown body outside the edit. |

When `Rename` finds that the destination already exists, that destination remains authoritative. Edits that fail validation leave the original content unchanged.
