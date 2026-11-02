# ADR-28998: Auto-Extract Shell Injection Expressions from run: Steps into Env Vars

**Date**: 2026-04-28
**Status**: Draft
**Deciders**: pelikhan, copilot-swe-agent

---

## Part 1 — Narrative (Human-Friendly)

### Context

GitHub Actions workflows can include `run:` steps whose shell scripts contain `${{ ... }}` expressions that the Actions runner interpolates before executing the shell. If a user-controlled value (such as an issue title, PR description, or branch name) reaches a `run:` script this way, an attacker can inject arbitrary shell commands — a well-known class of vulnerability called template injection or script injection. The gh-aw compiler previously permitted this pattern without any automated remediation, leaving compiled workflows vulnerable if authors used inline expressions in `run:` fields. Addressing this at compile time rather than at review time eliminates an entire class of security defects before they can reach production.

### Decision

We will automatically sanitize `${{ ... }}` expressions found directly in `run:` step scripts by extracting each expression into a deterministically named `GH_AW_<EXPR>` environment variable in the step's `env:` block, replacing the inline occurrence with the corresponding shell variable reference, and emitting a compiler warning for every extraction. This transformation happens silently during compilation (the source YAML is never modified) rather than blocking the build with a hard error, so existing workflows compile without changes while the output is always safe.

### Alternatives Considered

#### Alternative 1: Hard compilation error on inline expressions

The compiler could reject any workflow that contains a `${{ ... }}` expression directly in a `run:` field, forcing authors to fix the pattern manually before the workflow can compile. This was explicitly considered and rejected because it would break existing workflows that have not yet been migrated and would create friction for authors who may not immediately understand the security risk. The auto-extraction approach achieves the same security outcome while remaining backward compatible.

#### Alternative 2: Lint warning with no code transformation

The compiler could emit a warning identifying the unsafe pattern but leave the YAML unchanged, relying on the author to fix it. This was rejected because it provides only advisory output; if the warning is ignored or not surfaced (e.g., in CI logs), the compiled workflow remains vulnerable. Auto-extraction guarantees safety regardless of whether the author reads the warning.

#### Alternative 3: Require explicit opt-in safe pattern via schema validation

A schema validator could enforce that all `run:` fields contain no `${{ }}` tokens and require authors to declare the safe `env:` pattern from the start. This would be the most strict approach but would require tooling changes for all authors and would not handle third-party or imported workflow snippets that authors do not control. It was deferred in favour of the transparent auto-extraction approach.

### Consequences

#### Positive
- All compiled workflows are automatically protected from shell injection attacks, regardless of author awareness.
- Backward compatibility is maintained: workflows that already use the safe `env:` pattern compile identically; workflows using the unsafe pattern compile safely with a warning.
- Compiler warnings are precise and actionable, telling authors exactly which expression was extracted and into which environment variable.
- Fuzz testing (two fuzz targets with 30+ seed corpus entries) validates that the sanitizer never panics on arbitrary input and correctly preserves heredoc content.

#### Negative
- The compiled output differs structurally from the source YAML: `env:` entries appear that the author never wrote. This can surprise authors comparing source to compiled output and may complicate debugging.
- Expressions inside quoted heredoc bodies are left unsanitized because quoting suppresses shell expansion. Unquoted bodies undergo command substitution and must be sanitized, including lines beginning with `#`. Heredoc boundaries must match Bash syntax exactly.
- The sanitizer adds a new code path in the compilation pipeline that must be maintained alongside any future changes to `run:` step rendering.

#### Neutral
- Each extracted expression increments the compiler's warning count, which may affect CI dashboards or gates that track warning totals.
- The deterministic `GH_AW_<EXPR>` naming scheme means that two different expressions that hash to the same name would collide; this is unlikely but constitutes a latent edge case.
- The transformation is applied at two call sites (`renderStepFromMap` for struct-based steps, `addCustomStepsAsIs` / `addCustomStepsWithRuntimeInsertion` for raw YAML steps), requiring both paths to remain in sync.

---

## Part 2 — Normative Specification (RFC 2119)

> The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this section are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

### Expression Extraction

1. The compiler **MUST** extract every `${{ ... }}` expression found directly in a `run:` field (outside quoted heredoc bodies and shell comments), including unquoted heredoc bodies, into a dedicated entry in the step's `env:` block before the compiled YAML is written.
2. The compiler **MUST NOT** modify the source YAML on disk; extraction applies only to the in-memory representation used during compilation.
3. The compiler **MUST NOT** extract `${{ ... }}` expressions inside syntactically valid quoted heredoc bodies, including quoted bodies nested in expanding heredocs. The shell parser **MUST** match the complete closing delimiter, apply tab stripping only for `<<-`, ignore unquoted backslash-newline continuations when determining delimiter quotation, and grant no exemption on a parse failure or a boundary disagreement with Bash. Such unsupported scripts **MUST** remain unchanged by the sanitizer and **MUST** be scanned conservatively by validation. Opening commands and commands after the closing delimiter **MUST** remain subject to extraction. Comment filtering **MUST** preserve full-script quote state. Quoted heredocs suppress shell expansion, but are not a general-purpose transport for arbitrary untrusted multiline content: Actions interpolation still occurs before Bash parses the script and can introduce a closing delimiter.
4. The generated environment variable name **MUST** follow the pattern `GH_AW_<NORMALIZED_EXPR>` where `<NORMALIZED_EXPR>` is a deterministic, uppercase, alphanumeric-and-underscore transformation of the expression content.
5. Implementations **MUST** replace the inline `${{ ... }}` occurrence in the shell script with the corresponding shell variable reference (`$GH_AW_...`) after extraction.

### Compiler Warnings

1. The compiler **MUST** emit a human-readable warning to stderr for every expression that is extracted, stating the original expression, the step name, and the generated environment variable name.
2. The compiler **MUST** increment its internal warning counter for each such extraction so that callers and CI gates that track warning totals reflect the sanitization activity.
3. Warning messages **MUST** reference "shell injection" so that authors understand the security rationale.

### Generated MCP Configuration

1. Authored expressions in MCP URLs, arguments, mounts, environment values and headers **MUST NOT** be interpolated directly into the shell heredoc that writes gateway JSON. Mount transport **MUST** apply to every supported JSON engine renderer and reuse the same step bindings for the gateway mount allowlist. Expressions not covered by existing secret/environment passthrough **MUST** use deterministic `GH_AW_MCP_EXPR_<SHA256>` step-environment bindings with the full uppercase SHA-256 digest of the trimmed expression and a canonical `${{ expression }}` binding value.
2. The gateway startup script **MUST** resolve these placeholders after JSON parsing and before URL validation. Substitution **MUST** be single-pass, preserve runtime values as data, and fail explicitly when a compiler-owned binding is absent.
3. Existing unescaped `$NAME` and `${NAME}` references **MUST** retain runner-environment expansion through `GH_AW_MCP_ENV_<NAME>` placeholders; missing legacy values retain their previous empty-string behavior. Escaped unbraced `\$NAME` **MUST** become literal `$NAME` data, while escaped braced gateway references remain deferred. Values combining Actions secret/environment expressions with transported expressions or runner references **MUST** transport all their Actions expressions as data, without weakening the runtime reference check.
4. JSON strings **MUST** be encoded before escaping expanding-heredoc metacharacters, so quotes, backslashes, newlines, dollar signs and backticks cannot become shell source or corrupt JSON.
5. Because the pinned gateway expands environment references again, startup **MUST** reject any string containing `${NAME}` after compiler-expression or legacy runner-reference substitution, including references assembled across a substitution boundary. The error **MUST NOT** include the runtime value or credential name. Literal placeholder data cannot safely pass through this gateway version; strings combining transported data and remaining authored gateway references are also rejected. Authored gateway passthrough without transported substitutions retains its existing behavior.

### Error Handling

1. If the sanitization of a raw YAML string fails (e.g., the YAML cannot be parsed), the compiler **MUST** log the error and return the original unsanitized string rather than failing the build.
2. The sanitizer **MUST NOT** mutate the input `env:` map of the original step; it **MUST** produce a new map for the sanitized step.

### Fuzz and Property Testing

1. The sanitizer implementation **MUST** expose fuzz test entry points (`FuzzSanitizeRunStepExpressions` and `FuzzSanitizeCustomStepsYAML`) that verify: no panic on arbitrary input, no `${{ }}` tokens remaining in the non-heredoc portion of sanitized output, all generated env keys prefixed with `GH_AW_`, and warnings containing "shell injection".
2. Fuzz tests **SHOULD** include a seed corpus of at least 30 entries covering representative expressions, heredoc patterns, and edge cases.

### Conformance

An implementation is considered conformant with this ADR if it satisfies all **MUST** and **MUST NOT** requirements above. Failure to meet any **MUST** or **MUST NOT** requirement constitutes non-conformance.

---

*This is a DRAFT ADR generated by the [Design Decision Gate](https://github.com/github/gh-aw/actions/runs/25082700747) workflow. The PR author must review, complete, and finalize this document before the PR can merge.*
