# ADR-79888: Install TypeScript LSP from a Pinned npm Lockfile

**Date**: 2026-11-21
**Status**: Draft
**Deciders**: pelikhan (PR author) [TODO: verify other deciders]

---

### Context

Workflows that set `engine: copilot` and declare the built-in `lsp.typescript` entry receive an install step that the compiler generates automatically. The previous step ran `npm install -g --ignore-scripts typescript@5.8.3 typescript-language-server@4.3.3`. Only the two top-level packages were pinned, and npm does not enforce a lockfile, so transitive dependencies could resolve differently on each run. Code scanning raised alerts 1082–1084 against this pattern. The replacement must keep the zero-config experience, honor the runner's npm registry and the existing install-script and cooldown policies, and keep the server binaries on `PATH` inside the agent sandbox and WSL shells.

### Decision

We will install the built-in TypeScript language server from a lockfile embedded in the compiler, using `npm ci` into an isolated directory under `${RUNNER_TEMP}/gh-aw/engine-cli/typescript-lsp`. The primary driver is supply-chain integrity and reproducible installs, which addresses code-scanning alerts 1082–1084. The built-in `lsp.typescript` entry will accept only the bundled server version (`4.3.3`); other versions must move to `lsp.typescript-custom` with a checked-in npm lockfile.

### Alternatives Considered

#### Alternative 1: Keep `npm install -g` with exact top-level pins

This is the status quo and the smallest change. It was rejected because pinning `typescript` and `typescript-language-server` does not pin their transitive dependencies. The PR body states that `npm install -g` does not enforce a lockfile, which is the gap behind alerts 1082–1084. This was not a close call, because the option leaves the flagged gap open.

#### Alternative 2: Resolve the requested version at install time

Accept any `version` value and let the runner resolve it, for example with `npm install --package-lock-only` followed by `npm ci`. This would keep arbitrary version overrides working. It was rejected because each run would resolve dependencies against the registry at that moment, which reintroduces the unpinned-resolution problem. [TODO: verify. The PR does not discuss this option; the rationale is inferred from the diff.]

#### Alternative 3: Remove the built-in TypeScript entry

Require users to add their own `steps:` install and custom `lsp` key, as already required for languages outside the built-in table. This would eliminate the bundled lockfile and its maintenance. It was rejected because it gives up the zero-config path that the built-in table provides. [TODO: verify. The PR keeps the built-in entry but does not record this trade-off.]

### Consequences

#### Positive
- Transitive dependencies are fixed by a lockfile whose entries carry SHA-512 integrity hashes, so repeated installs resolve the same packages.
- The install runs `npm ci --prefix` in a dedicated directory rather than a global install, which isolates it from other global npm state.
- The default `lsp.typescript` configuration keeps working without workflow changes, and the fix addresses code-scanning alerts 1082–1084 on the compiler side.

#### Negative
- Breaking change: `lsp.typescript.version` values other than `4.3.3` now fail validation at compile time. Users must move the entry to `lsp.typescript-custom` and install from a checked-in npm project with `npm ci` in `steps`. The changeset is marked `major`.
- Upgrading TypeScript or `typescript-language-server` now requires regenerating the embedded lockfile and releasing a new compiler version.
- Technical debt: generated lock files for `eslint-miner`, `jsweep`, and `smoke-copilot` were intentionally left unchanged in this draft and need one regeneration after SDK integration, per the PR body.

#### Neutral
- The source directory's `.npmrc`, which points at an internal registry, is not embedded. Only `package.json` and `package-lock.json` are written at runtime, and `--replace-registry-host=npmjs` maps canonical npmjs lockfile URLs to the runner's configured registry.
- `prependTypeScriptLSPPath` re-adds the `node_modules/.bin` directory to `PATH` in the Copilot command, because sandbox and WSL shells can rebuild `PATH` after `GITHUB_PATH` is applied.
- The shared `appendNpmLSPInstallPolicy` helper keeps the `--ignore-scripts` and cooldown environment policy for the new install step.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
