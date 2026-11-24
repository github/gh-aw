# ADR-80390: Use a Full Shallow Activation Checkout for the Base Snapshot

**Date**: 2026-11-23
**Status**: Draft

## Context

The activation job loads agent configuration (`.github`, `.agents`, `.claude`, `.codex`, `.gemini`, `.pi`) with a sparse checkout. On `pull_request` events that checkout uses the PR base SHA. When the job saves a trusted base snapshot (step "Save agent config folders for base branch restoration", run by `save_base_github_folders.sh`), it resolves symlinks inside those folders. A symlink whose target lies outside the sparse set has no target on disk, and the step fails with `ENOENT`.

The reported case (issue #80360) is `.agents/skills/mui-renovate-fix`, which links to `../../skills/mui-renovate-fix`. `skills/` is not in the sparse set. Symlinks that stay inside the agent folders resolve correctly.

`resolveSymlinkExtraPaths` (`pkg/workflow/compiler_activation_job.go`) adds symlink targets to the sparse list, but it inspects only three fixed candidate symlinks. It cannot cover arbitrary symlink targets.

Forces in play:

- The snapshot must come from the trusted base ref.
- Credentials must not persist in the activation checkout.
- Symlink targets must stay confined to the repository.
- The activation job has a sparse-only expectation in spec section 3.3 and its tests. Its checkout is read-only.

## Decision

When the activation job saves a trusted base snapshot, it checks out the full repository with a shallow fetch instead of the sparse agent-folder checkout.

- `pkg/workflow/compiler_activation_steps.go`: `saveBase` (true when PR checkout is not disabled, dynamic workflows are enabled, or Copilot SDK agents are staged) selects the full checkout.
- `pkg/workflow/checkout_step_generator.go` (`generateActivationCheckoutStep`): when `fullCheckout` is set, the step is named "Checkout repository for base branch snapshot". It keeps `persist-credentials: false` and `fetch-depth: 1`, and omits `sparse-checkout` and `sparse-checkout-cone-mode`. Otherwise the existing sparse checkout is unchanged.
- Unchanged: ref selection (PR base SHA on `pull_request`, `github.sha` otherwise, resolved callee SHA for `workflow_call`), the same-repository token guard, and credential handling.
- Symlink confinement in `save_base_github_folders.sh` is unchanged and still applies to targets that are now present.
- Spec section 3.3 (`checkout-behavior-specification.md`) and `reference/checkout.md` are updated. Compiled golden files are regenerated.

## Alternatives Considered

- **Extend the sparse list with symlink targets.** This is the mechanism `resolveSymlinkExtraPaths` already uses. Generalizing it means scanning every agent folder for symlinks at compile time. The sparse list is then baked into the lock file, so a symlink added later fails again until the workflow is recompiled. Not chosen.
- **Fetch symlink targets with a second sparse checkout or the GitHub API.** This adds a checkout step and must resolve targets before they exist on disk. The PR did not explore it. Not chosen.
- **Fail closed when a symlink leaves the agent folders.** This keeps the sparse checkout, but it rejects valid configurations such as the reported one. Not chosen.
- **Keep the sparse checkout and skip the snapshot.** This removes the trusted restoration of agent configuration, which is the security control for PR-controlled agent files. Rejected.

## Consequences

Positive:

- Symlink targets anywhere in the base tree resolve during the snapshot, which removes the `ENOENT` failure.
- The snapshot still comes from the trusted base SHA, and persisted credentials stay disabled.
- No compile-time path discovery is needed, so symlink changes do not require a recompile to take effect.

Negative:

- When a snapshot is saved, the activation job downloads the full repository tree at depth 1. Checkout time and disk use grow with repository size. Not measured in this PR.
- The activation job now has read access to all base-ref files. This reverses the earlier sparse-only expectation in spec section 3.3 and its tests, and it makes symlink confinement in `save_base_github_folders.sh` the main guard against escape.
- Compiled golden files for the affected engines change. The changes are the step rename and the removed sparse block.
