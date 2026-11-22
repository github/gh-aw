---
"gh-aw": major
---

Migrate the bundled npm SDK and Copilot CLI extensions to `@microsoft/copilot-sdk` v2.

**Breaking change:** Existing `engine.copilot-sdk` pins for `@github/copilot-sdk` no longer install, and custom drivers and CLI extensions using the v1 npm API must migrate.

**Migration guide:**
- For each custom pin, replace the dependency with an exact `@microsoft/copilot-sdk` version, regenerate the npm v3 lockfile, and check in both files under `.github/aw/copilot-sdk/<version>/<variant>/`. Update the workflow's `version` and `sha256` using the SHA-256 of the manifest bytes, a NUL byte, and the lockfile bytes (`{ cat package.json && printf '\0' && cat package-lock.json; } | shasum -a 256 -b`). Alternatively, use `copilot-sdk: true` for the bundled SDK.
- For custom JavaScript and TypeScript drivers, import `@microsoft/copilot-sdk` and use the v2 `CopilotClient`, TCP connection, and session options shown in the [driver specification](https://github.github.com/gh-aw/specs/copilot-sdk-driver-specification/).
- For CLI extensions, import `@microsoft/copilot-sdk/extension` and migrate `joinSession` options to `canvases.definitions`, `tools.definitions`, `hooks.handlers`, and `permissions.onRequest` as applicable.
