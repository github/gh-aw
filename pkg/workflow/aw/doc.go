// Package aw defines the source AST for agentic workflow Markdown files.
//
// Document separates YAML frontmatter from the Markdown prompt and retains
// source locations for diagnostics. Parse uses the shared frontmatter parser;
// Emit preserves unchanged source verbatim, including comments, shorthands,
// line endings, and prompt whitespace. Edited frontmatter is serialized
// deterministically without losing fields that are not modeled by a typed view.
//
// Frontmatter retains the complete AW configuration, including engine, tools,
// imports, safe outputs, triggers, and custom jobs. Schema validation, import
// resolution, and typed configuration parsing remain the compiler's job.
// Runner selections retain scalar, label-sequence, and group/labels mapping
// values, including expression strings, at the root and in custom jobs.
//
// workflow.Compiler.ParseWorkflowDocument accepts this source AST, and
// callers then lower the resolved data through the existing compiler.
// The AW source AST and the native Actions execution AST are separate stages;
// AW-specific fields are never emitted as native Actions workflow properties.
package aw
