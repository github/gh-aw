---
"gh-aw": major
---

Treat repository-local runtime imports as author files and make prompt generation credential-free.

Local files share compiler-selected non-secret expression bindings and bounded nested imports. Public HTTPS content remains static text without expressions, directives, or inline definitions. Prompt generation keeps pinned `actions/github-script` with a non-secret token placeholder instead of its default token. Generated scripts scrub inherited credentials before loading prompt modules, which receive no authenticated API client. This cleanup occurs after action initialization, not before Node startup. Secret-backed prompt environment bindings are rejected, including in non-strict mode.

Prompt import trees include a `trust` label on the root and every import node: `author` for local sources and `untrusted` for external HTTPS content, including nested and cached imports.

Upgrade the compiler and setup runtime together and recompile workflow locks. Older runtimes cannot consume `author_file` plans, and older locks do not supply the new local author bindings. Move credential use to authenticated steps outside prompt generation, and keep remote imported content static.
