# ADR-78311: Pin Setup Action and Built-in Containers to Compiler Version

**Date**: 2026-11-16
**Status**: Draft
**Deciders**: pelikhan (PR author); gh-aw maintainers

---

### Context

Compiled lock files depend on two kinds of runtime artifacts that ship with each gh-aw release: the `github/gh-aw-actions/setup` action and the built-in container images the compiler injects (gh-aw-firewall images, gh-aw-mcpg, gh-aw-router, gh-aw-node, and github-mcp-server). The compiler embeds the setup action version and container digests that it was built and tested with. Before this change, `gh aw update` and `gh aw upgrade` treated these like any other dependency. The setup action moved to the latest release, capped at the CLI version by ADR-26827, it was never moved below a newer existing entry, and its workflow `uses:` references were skipped when `--no-release-bump` was set. Built-in images were re-resolved from the registry, so a moving tag could produce a digest that the running compiler never shipped. As a result, running `update` and then `compile` could produce a lock file that differs from the one `compile` produces on its own, and a workflow could run against a runtime that does not match its compiler.

### Decision

For release builds of the compiler, `gh aw update` and `gh aw upgrade` will pin the setup action to exactly the running compiler's release tag and its resolved SHA, in both `actions-lock.json` and workflow Markdown `uses:` references. This pin skips the latest-release lookup, the native-action cap, the downgrade guard, and `--no-release-bump`. The setup action can therefore move up or down to match the compiler. Dev and other non-release builds keep the previous release-based behavior. Built-in container images will use the compiler's embedded digest pins (`actionpins.GetContainerPin`) instead of registry resolution. If no embedded pin exists, the image falls back to registry resolution. `gh aw compile --force-refresh-container-pins` still refreshes built-in images from the registry, because it is the path used to refresh the trusted pins themselves.

This narrows ADR-26827 for the setup action only. Other native actions in `github/gh-aw` and `github/gh-aw-actions` still follow the ADR-26827 cap.

### Alternatives Considered

#### Alternative 1: Keep latest-release behavior with the ADR-26827 cap

The setup action would keep moving to the latest release up to the CLI version, and built-in images would keep resolving from the registry. This needs no change, but the cap only prevents moving ahead of the CLI. It does not move an entry back down to the CLI version, and it does not apply under `--no-release-bump`. Built-in digests can still drift from the compiler. It was rejected because it does not guarantee that `update` followed by `compile` matches a plain `compile`.

#### Alternative 2: Warn on mismatch but keep the existing pins

`update` would report when the setup action or a built-in digest differs from the compiler's version but would not change it. This avoids surprising downgrades, but most users would not act on the warning, and the mismatch would remain. It was rejected for the same reason ADR-26827 rejected warn-only behavior: the runtime and compiler are a matched pair.

#### Alternative 3: Opt-in flag for compiler pinning

A new flag, such as `--pin-to-compiler`, would enable the new behavior only when requested. This keeps the default unchanged but makes the correct result depend on users knowing about the flag, and it adds another CLI option to document and test. It was rejected because the matched pair should be the default.

#### Alternative 4: Resolve built-in images from the registry and compare with the embedded pins

`update` would query the registry and only accept a digest that matches the embedded pin, warning or failing otherwise. This detects tag drift but still needs network access and registry tooling for images whose correct digest is already known. When the two differ, the embedded pin is the expected value anyway. It was rejected as extra cost with no change in outcome.

### Consequences

#### Positive
- On release builds, `compile`, then `update`, then `compile` produces a byte-identical lock file. An end-to-end test covers this.
- The setup action and built-in images always match the compiler that generated the lock file, including after a CLI downgrade.
- Built-in images no longer need Docker Buildx or `crane` during `update`, which removes registry failures for those images.

#### Negative
- Users cannot get a newer setup action or a newer built-in image digest through `update` without upgrading the CLI. This includes hotfix releases.
- The setup action can be moved to an older version when the CLI is older than the pinned entry. This is intended, but it can surprise users who expected `update` to only move forward.
- If the compiler's tag SHA cannot be resolved, the setup action is reported as a failed update and left unchanged instead of falling back to the latest release.
- The built-in image list is hard-coded in `isBuiltinContainerImage`. Adding a new built-in image requires a code change.

#### Neutral
- Dev and other non-release builds keep the previous setup action behavior, so local development is unchanged.
- Other gh-aw native actions and third-party actions keep their existing update rules.
- `compile --force-refresh-container-pins` remains the way to refresh built-in pins from the registry.

---

*ADR created by Copilot from PR #78311. Review and finalize before changing status from Draft to Accepted.*
