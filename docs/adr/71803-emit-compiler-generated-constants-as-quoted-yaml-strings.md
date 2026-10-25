# ADR-71803: Emit Compiler-Generated Constants as Quoted YAML Strings, Not Literal-Only Expressions

**Date**: 2026-10-25
**Status**: Draft
**Deciders**: pelikhan (PR author); [TODO: verify reviewers]

---

### Context

The gh-aw compiler emits GitHub Actions YAML in which most environment bindings are Actions expressions of the form `${{ <content> }}`. For a few compiler-generated values the "expression" was really a constant: deferred prompt markers were wrapped as `${{ '__GH_AW_DEFERRED_…__' }}`, and the unconditional pre-activation output was emitted as `activated: ${{ 'true' }}`. The workflow linter's obfuscation rule flags literal-only expressions, and the public workflow corpus produced 15 such notices (14 deferred prompt markers and one pre-activation output). Workflow authors cannot repair these bindings because the compiler generates them, so either code generation or the linter had to change. Job outputs must also stay string-typed; a bare YAML `true` would become a boolean.

### Decision

We will mark compiler-generated constants explicitly with a new `IsLiteral` flag on `ExpressionMapping`. These values are emitted as Go-quoted (`%q`) YAML strings instead of being wrapped in `${{ }}`. A single helper, `ExpressionMapping.environmentValue()`, produces the binding for prompt creation (`unified_prompt_step.go`) and for interpolation/template rendering (`template.go`). Placeholder substitution (`generatePlaceholderEnvironment`) writes literal mappings through `writeYAMLEnv`. Deferred prompt rewriting sets `IsLiteral = true` and stores the bare marker. When the pre-activation `activated` output reduces to a `StringLiteralNode`, the compiler emits it as the quoted string `"true"`. Authored expressions, late job output/result bindings in the agent job, and the existing lint rules stay unchanged.

### Alternatives Considered

#### Alternative 1: Suppress or exempt compiler-generated bindings in the obfuscation linter

Add an allowlist or exemption to the linter for known generated patterns (for example, `__GH_AW_DEFERRED_*` markers and `activated: ${{ 'true' }}`). This would leave code generation untouched. It was rejected because it weakens a security-relevant lint rule, couples the linter to compiler internals, and keeps emitting expressions that do nothing.

#### Alternative 2: Infer literalness from expression content at emit time

Have the emitter detect quoted-literal content (for example, `'...'`) and unwrap it automatically, with no explicit flag. This needs no new field. It was rejected because authored expressions such as `'authored literal'` or `'first' || 'second'` must keep their expression semantics. Inferring literalness from content would silently change how authored input is handled. An explicit flag set only by the compiler limits the change to generated values.

#### Alternative 3: Emit unquoted YAML scalars

Write the constant directly (for example, `activated: true`). This was rejected because YAML would parse `true` as a boolean and change the type of job outputs and env values, which must remain strings.

### Consequences

#### Positive
- Compiled workflows produce no obfuscation findings for generated constants, so authors no longer see notices they cannot fix.
- The lint rule keeps its full strength because nothing is exempted.
- A shared `environmentValue()` helper makes prompt creation and template interpolation emit literal bindings the same way. Placeholder substitution emits the same quoted value through `writeYAMLEnv`.
- Quoted emission keeps values string-typed, and the tests check this by round-tripping through a YAML parser.

#### Negative
- `ExpressionMapping` now carries two value semantics (expression or literal), so every new emission site has to handle `IsLiteral` or call `environmentValue()`. Otherwise it reintroduces the problem or produces a wrong binding.
- Generated lock files change textually (`'__GH_AW_DEFERRED_…__'` → `"__GH_AW_DEFERRED_…__"`, `${{ 'true' }}` → `"true"`), which causes churn on recompilation. Tests and tools that match on the old text must be updated.
- Go `%q` quoting is used as YAML double-quoted quoting. This works for the tested inputs (quotes, newlines, backslashes), but it depends on the two escaping grammars staying compatible for compiler-generated content.

#### Neutral
- Late job output/result bindings in the agent job's "Resolve deferred prompt expressions" step remain `${{ … }}` expressions, so prompt rendering and substitution semantics are preserved.
- Pre-activation conditions with real checks still emit `${{ … && … }}` expressions. Only the case that reduces to a pure literal changes.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
