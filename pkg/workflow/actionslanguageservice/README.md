# actionslanguageservice Package

> Go parser and source helpers for GitHub Actions expressions and workflows.

## Overview

This package implements the Actions expression grammar used by the compiler and linter. It parses syntax and tracks source ranges; it does not evaluate expressions, execute workflow code, or replace schema validation.

Expression length and nesting are bounded by `MaxExpressionLength` and `MaxExpressionDepth`. Parsing errors are value-safe and do not include the full source expression in debug logging.

## Public API

| Symbol | Description |
|--------|-------------|
| `Token`, `Tokenize` | Lexical tokens retaining original spelling and byte offsets. |
| `Expression`, `ParseSyntax`, `ParseExpression`, `ValidateExpression` | Expression AST construction and syntax validation; `ParseExpression` also checks allowed contexts. |
| `Walk`, `Reference`, `References` | Traversal and extraction of context references from an expression AST. |
| `RewriteReferences`, `WrappedExpression` | Source-preserving reference rewriting and extraction of a complete wrapped expression. |
| `Pattern`, `ReferencePattern` | Helpers for matching and rewriting expression references in source text. |
| `Interpolation`, `ExtractExpressions` | Finds expression interpolations in source while respecting quoted strings. |
| `Workflow`, `ParseWorkflow` | Parses workflow YAML and records expressions by YAML node. |
| `ActionsIdentifier` | Validates identifiers used by Actions syntax. |
| `ValidateWorkflowWithCacheMode` | Validates workflow YAML against the compiler's cache-mode schema. |

## Dependencies

The parser follows the pinned Actions language grammar and is used by workflow AST and security-analysis packages.
