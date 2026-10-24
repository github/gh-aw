---
"gh-aw": major
---

Run built-in Actions/AW AST lint rules during compilation by default, before emitting the generated lock file, including in no-emit mode.

Compilation now fails on AST lint errors that previously required a separate `gh aw lint` invocation. Fix the reported findings or use `gh aw compile --no-lint` to retain the previous lint behavior. Existing compiler safety validation remains active; dry-run compilation does not permit this opt-out.

`add`, `update`, and `validate` also lint whenever they recompile. Only `compile` exposes `--no-lint`; fix findings before using other recompiling commands.

Compile-time lint is Actions/AW AST-only and does not require a native JavaScript parser. Use standalone `gh aw lint --javascript-module <module-path>` for deeper authored JavaScript driver and custom-grader checks. Missing-parser coverage warnings do not block compiler dry-run.
