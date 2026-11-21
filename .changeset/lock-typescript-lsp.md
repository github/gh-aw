---
"gh-aw": major
---

Install the built-in TypeScript language server from a bundled npm lockfile.

**Breaking change:** The experimental `lsp.typescript.version` option accepts only the bundled language server version (`4.3.3`). Omit the option to use the default. To use another version, rename the entry to `lsp.typescript-custom`, remove `version`, install the packages from a checked-in npm project with `npm ci` in `pre-agent-steps`, and point `command` to the installed executable.
