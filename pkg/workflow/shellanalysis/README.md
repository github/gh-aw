# shellanalysis Package

`shellanalysis` identifies Bash constructs that may evaluate dynamic code.
It supports run-step expression sanitization and does not execute shell source.

`HasShellCodeEvaluation` inspects a parsed Bash syntax tree.
`LiteralWord` performs quote removal only and reports whether a word is static.
