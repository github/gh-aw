# ADR-79888: Install TypeScript LSP from a Pinned npm Lockfile

**Date**: 2026-11-21
**Status**: Proposed
**Deciders**: Not recorded; maintainer approval is not documented.
**Related PR**: [#79888](https://github.com/github/gh-aw/pull/79888)

---

### Context

Copilot workflows that declare the built-in `lsp.typescript` entry get a compiler-generated install step. The previous step installed `typescript@5.8.3` and `typescript-language-server@4.3.3` with `npm install -g`. Exact top-level versions do not enforce a lockfile or verify tarballs against checked-in integrity hashes. Code scanning raised alerts 1082–1084 against this pattern. The replacement must preserve the zero-config install, use the runner's configured npm registry, keep the existing install-script and cooldown policies, and make the binaries available inside the agent sandbox and WSL shells.

### Decision

Keep TypeScript `5.8.3` and `typescript-language-server` `4.3.3` so the default toolchain does not change. Embed `package.json` and the complete npm lockfile in the compiler and run `npm ci` in `${RUNNER_TEMP}/gh-aw/engine-cli/typescript-lsp`. This installs the locked dependency graph and checks package tarballs against the lockfile's SHA-512 hashes. Keep the runner's configured registry rather than requiring direct access to npmjs. If the locked install fails, fail the step instead of falling back to an unlocked install.

Users can omit `lsp.typescript.version` or explicitly select the bundled server version. For another version, configure the server under `lsp.typescript-custom`, install it from a checked-in npm project with `npm ci` in `pre-agent-steps`, and point `command` to the installed executable. This lets each workflow choose its own version without relaxing the built-in install's lockfile requirement.

### Alternatives Considered

#### Alternative 1: Keep `npm install -g` with exact top-level pins

Keeping the global install would avoid migration work, but exact top-level pins do not enforce the full dependency graph or lockfile integrity checks. Using the global install only as a fallback would still discard those guarantees whenever the locked install failed.

#### Alternative 2: Resolve arbitrary built-in versions at install time

Generating a lockfile on the runner would preserve arbitrary version overrides, but the dependency graph would be resolved anew on each run. Running `npm ci` afterward would enforce only that run's resolution, not a checked-in graph. The custom entry preserves version choice while keeping dependency resolution out of workflow runs.

#### Alternative 3: Require a custom entry for all TypeScript users

Removing the built-in entry would eliminate the compiler's bundled-lockfile maintenance, at the cost of requiring every TypeScript workflow to manage its own npm project and install step. Keeping the built-in entry preserves the zero-config path for users who do not need a different version.

### Consequences

#### Positive
- The lockfile pins the full dependency graph and provides SHA-512 integrity checks for downloaded packages.
- The install runs `npm ci --prefix` in a dedicated directory rather than a global install, which isolates it from other global npm state.
- The default `lsp.typescript` configuration keeps working without workflow changes, and the fix addresses code-scanning alerts 1082–1084 on the compiler side.

#### Negative
- Breaking change: requests for a server version other than `4.3.3` fail validation at compile time and require the custom migration described above. The changeset is marked `major`.
- Upgrading TypeScript or `typescript-language-server` now requires regenerating the embedded lockfile and releasing a new compiler version.
- The PR defers regeneration of the compiled workflows for `eslint-miner`, `jsweep`, and `smoke-copilot` until after SDK integration.

#### Neutral
- Only `package.json` and `package-lock.json` are written at runtime. `--replace-registry-host=npmjs` lets npm use the runner's configured registry for canonical npmjs lockfile URLs without bundling a project `.npmrc`.
- `prependTypeScriptLSPPath` re-adds the `node_modules/.bin` directory to `PATH` in the Copilot command, because sandbox and WSL shells can rebuild `PATH` after `GITHUB_PATH` is applied.
- The install step retains the existing npm lifecycle-script and minimum-release-age settings.
