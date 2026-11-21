---
"gh-aw": major
---

Install TypeScript `6.0.3` and TypeScript Language Server `6.1.0` from a bundled npm lockfile.

**Breaking change:** The experimental `lsp.typescript.version` option accepts only the bundled language server version (`6.1.0`). Omit the option to use the new default. To keep the former TypeScript `5.8.3` and server `4.3.3` or use another version, rename the entry to a key not used by a built-in server and remove `version`. Install the packages from a checked-in npm project with `npm ci` in `pre-agent-steps`, and point `command` to the installed executable using a workspace-relative path.
