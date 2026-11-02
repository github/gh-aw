---
"gh-aw": major
---

Treat repository-local runtime imports as author files and remove network fetching from prompt assembly.

Local files share compiler-selected non-secret expression bindings and bounded nested imports. HTTP, HTTPS, other URI schemes, and network-path runtime imports are rejected, including optional and nested forms. Prompt assembly performs no DNS or network requests and no longer uses URL transport or disk caches. Put external URLs in ordinary prompt instructions for the agent to retrieve with its tools inside the AWF sandbox under the configured network policy; URLs are not automatically fetched. Frontmatter compile-time imports are unchanged.

Prompt generation keeps pinned `actions/github-script` with a non-secret token placeholder instead of its default token. Generated scripts scrub inherited credentials before loading prompt modules, which receive no authenticated API client. This cleanup occurs after action initialization, not before Node startup. Secret-backed prompt environment bindings are rejected, including in non-strict mode.

Prompt import trees include a `trust: author` label on the root and every local import node, including nested and cached imports. Older `untrusted` labels remain historical metadata, not permission to load remote content.

Upgrade the compiler and setup runtime together and recompile workflow locks. Older runtimes cannot consume `author_file` plans, and older locks do not supply the new local author bindings. Move credential use to authenticated steps outside prompt generation, and migrate every remote runtime import to a permitted local file or agent-side retrieval.
