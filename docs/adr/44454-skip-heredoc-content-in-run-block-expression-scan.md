# ADR-44454: Exempt Only Proven Quoted Heredoc Bodies from Run-Block Expression Scanning

**Date**: 2026-07-09
**Status**: Draft
**Deciders**: Unknown

---

### Context

Historically, `CompileSimpleWorkflow` regressed ~3x in execution time (5.67ms → 14.6ms) with ~12x more allocations after a change that introduced MCP tool support. Workflows using MCP tools generated `run:` steps that piped JSON configuration through shell heredocs using a unique compiler-generated delimiter (e.g., `cat << GH_AW_MCP_CONFIG_01641c1bd0fd81fa_EOF | "$GH_AW_NODE" ...`). These heredoc bodies contained `${{ toJSON(...) }}` expressions that were not present in `allowedRunScriptExpressionRegex`. The text-scanning path (Path B) of `validateTemplateInjection` — `walkRunBlockLines` / `scanRunContentExpressions` — visited every line in multiline run blocks without awareness of heredoc boundaries, causing it to flag heredoc-embedded expressions as disallowed on every compilation. This set `hasDisallowed=true` unconditionally, triggering a full `yaml.Unmarshal` (~434 MB of allocations per 100-iteration benchmark run) even though the parsed path (Path A, `validateNoGitHubExpressionsInRunScriptsFromParsed`) already called `removeHeredocContent` and never raised an error for these expressions. Those measurements motivated the original optimization; they are not benchmarks of the parser-based security fix.

### Decision

The original decision skipped all heredoc bodies using delimiter heuristics. Security alert 1057 exposed an unsafe assumption: Bash executes command substitutions in unquoted bodies, so those bodies cannot be exempt from expression scanning.

Both scanning paths now share the existing Bash parser to identify quoted heredoc bodies after masking Actions expressions without changing byte offsets. Path B removes YAML block indentation before parsing. Only quoted bodies are skipped; unquoted bodies are scanned even on lines beginning with `#`. Complete delimiter matching, quoting, multiple redirections, and `<<-` tab stripping follow Bash syntax. Opening commands remain visible. A parse failure grants no heredoc or comment exemption.

Quoted inner heredocs remain protected inside expanding outer bodies. Comment spans come from the full-script parse so enclosing quote state is preserved. Delimiter quotation excludes unquoted backslash-newline continuations. Quoted-body exemptions also require agreement with the first literal closing delimiter line; parser boundary disagreements are treated as unsupported syntax. Escapes in ANSI-C-quoted or double-quoted delimiter parts are also unsupported because Bash may decode them differently from the parser; ordinary single-quoted literal backslashes remain supported. For malformed or unsupported heredoc syntax, sanitization leaves the script unchanged and validation scans all expressions. Parser failure logs never include script tokens.

The `steps-run-secrets-to-env` source codemod shares these Bash region decisions instead of applying line-based comment rules inside heredocs. It preserves quoted bodies, migrates hash-prefixed expanding-body expressions, and reports unproven boundaries as guided errors without writing partial workflow changes.

Quoted-body preservation is a shell-expansion compatibility rule, not a guarantee for arbitrary Actions values. Actions interpolation precedes Bash parsing, so an untrusted multiline value can introduce a closing delimiter and executable trailing commands. Environment indirection remains the appropriate transport for untrusted data.

Generated MCP JSON no longer relies on exempting expanding heredocs. Expressions are transported through step environment bindings and resolved after JSON parsing. The distinct gateway re-expansion boundary, compile-time diagnostics, and migration constraints are specified in [ADR-28998](28998-auto-extract-shell-injection-expressions-into-env-vars.md#generated-mcp-configuration).

### Alternatives Considered

#### Alternative 1: Expand `allowedRunScriptExpressionRegex` to include `toJSON(...)` patterns

The immediate trigger was `toJSON(steps.determine-automatic-lockdown.outputs.visibility)` not matching the allowlist. Adding this specific pattern (or a broader `toJSON(...)` rule) to the regex would have eliminated the false positive for this case. However, this is a band-aid: any future expression pattern generated inside heredoc content that is not in the allowlist would re-trigger the regression. It also conflates "this expression is safe in heredoc context" with "this expression is allowed in general shell context," which are distinct security questions. Rejected because it does not fix the structural root cause.

#### Alternative 2: Cache `yaml.Unmarshal` results per workflow content hash

The expensive `yaml.Unmarshal` call on the `hasDisallowed` fallback path could be memoized using the raw YAML content as a cache key. This would avoid repeated parsing across multiple compilations of the same workflow. However, it introduces state (cache invalidation, memory pressure), does not fix the false-positive detection that triggers the fallback, and masks the underlying issue rather than correcting it. Rejected because caching a path that should not be taken is worse than avoiding the path entirely.

#### Alternative 3: Keep delimiter heuristics or fix only one scanning path

Delimiter suffixes and line-local quote or comment rules cannot establish Bash expansion semantics, particularly for nested heredocs and continued delimiters. Fixing only the sanitizer would leave independent validator bypasses; fixing only validation would reject otherwise migratable scripts. Rejected in favor of shared parser-backed boundaries for sanitization, both validator paths, and source migration.

### Consequences

#### Positive
- Path B scan semantics match Path A: both skip only quoted heredoc bodies and detect expressions in expanding unquoted bodies.
- Shared regression tests cover sanitization, parsed validators, the raw-YAML scan, exact delimiters, quoted/custom delimiters, mismatches, and tab stripping.
- The source codemod applies the same boundaries, preserving quoted data while migrating executable expressions.

#### Negative
- Scripts containing heredoc operators require shell parsing. Unsupported or malformed scripts are scanned conservatively rather than silently exempted.
- Previously exempt unquoted bodies can now produce extraction warnings or validation errors. The source codemod provides migration where boundaries are proven; unsupported boundaries require manual correction.
- Proven quoted bodies retain the pre-shell Actions delimiter-injection limitation described above.

#### Neutral
- The sanitizer and both validator paths share `executableShellContent`; the former delimiter heuristic has been removed.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
