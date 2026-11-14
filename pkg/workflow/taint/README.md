# taint Package

> Value-free source-to-sink analysis for Actions expressions and workflow scripts.

## Overview

`taint` tracks symbolic references from configured sources to disclosure or execution sinks. It analyzes parsed workflow structures without evaluating expressions, executing scripts, or reading secret values. Results include structured findings and coverage gaps when an input could not be fully analyzed.

## Public API

| Symbol | Description |
|--------|-------------|
| `Kind`, `Site`, `Source` | Sink category, source location, and symbolic source reference. |
| `Finding`, `CoverageGap`, `Result` | Analysis findings and explicit limits on analysis coverage. |
| `Rule` | Source, sink, and filter predicates for one analysis rule. |
| `Analyze`, `AnalyzeDocument`, `AnalyzeAuthoredDocument`, `AnalyzeAuthoredScripts` | Analyze compiled Actions workflows and/or authored AW documents. |
| `SecretLeakRule` | Built-in secret-disclosure rule. |
| `JavaScriptOptions`, `JavaScriptInput`, `JavaScriptResult`, `AnalyzeJavaScript` | Bounded JavaScript analysis using a supplied parser implementation. |
| `JavaScriptNode` | Value-free projection of the TypeScript compiler AST; unrecognized syntax is retained in `Other`. |
| `JavaScriptNode.Children` | Returns all projected child nodes, including unmodeled syntax. |
| `JavaScriptParser` | Parses one source string into a `JavaScriptNode`. |
| `JavaScriptBatchParser` | Parses independent scripts in one batch, returning per-script results and a separate batch error. |
| `JavaScriptParseResult` | Holds a parsed node or the syntax error for one script in a batch. |
| `JavaScriptMode`, `JavaScriptFast`, `JavaScriptDeep` | Selects fast authored-script traversal or deep library analysis; an empty mode preserves deep library behavior. |
| `TypeScriptParser` | Implements the parser interfaces using an explicitly installed native TypeScript compiler bridge; it does not download the compiler at runtime. |
| `PrimeAuthoredJavaScript` | Primes JavaScript analysis for a set of authored documents. |

Findings identify source paths and sink sites, not values. A `CoverageGap` must be treated as incomplete evidence, not as proof that no flow exists.
