# ADR-65905: Carry Claude Dynamic Workflows Through the Activation Artifact

**Date**: 2026-02-05
**Status**: Draft
**Deciders**: Unknown (PR author: pelikhan)

---

### Context

Claude Code supports *dynamic workflows*: saved scripts and supporting resources that live under `.claude/workflows/` in the workflow repository and are invoked at runtime by the `Workflow` tool. In gh-aw's two-job model, the activation job snapshots trusted base-branch agent configuration into `/tmp/gh-aw/base/` and the agent job restores it after checkout, so that fork PR branches cannot inject malicious skills, instructions, or agent config. Two gaps prevented dynamic workflows from working: (1) when PR checkout was disabled, the activation snapshot of the `.claude` tree was never produced or restored, so the saved workflow scripts and their nested support files (including dot-prefixed resources such as `workflows/references/.context`) were absent in the agent job; and (2) Claude runs headless in gh-aw, and the `Workflow` tool was not in the default allowed-tools list, so invoking a dynamic workflow required `permission-mode: bypassPermissions`. PR #65905 touches `pkg/workflow/` (~124 added lines in business-logic directories) plus the `actions/setup/sh/` restore scripts and 70 regenerated `.lock.yml` files.

### Decision

We will deliver Claude dynamic workflows through the existing trusted-activation-artifact channel rather than through a new mechanism. Concretely: the activation job always preserves the whole `.claude` tree (recursively, including hidden nested files) even when PR checkout is disabled; the agent job restores it with a dedicated `Restore Claude workflows from activation artifact` step that `rm -rf`s the workspace `.claude` and `cp -a`s the trusted copy, gated by `canRestoreClaudeWorkflows()` so the snapshot is never copied over an *external* repository checked out at the workspace root; and `Workflow` is added to the default Claude allowed-tools list (`pkg/workflow/claude_tools.go`) so headless runs do not need `bypassPermissions`. The primary driver is security-preserving reuse: dynamic workflow content must come from the trusted base branch, never from the PR branch.

### Alternatives Considered

#### Alternative 1: Enable the `Workflow` tool via `permission-mode: bypassPermissions`

Claude already supports bypassing permission prompts wholesale, which would make dynamic workflows (and anything else) invocable headlessly with no compiler change. Rejected because it removes the permission boundary for *every* tool, not just `Workflow`, which is the opposite of gh-aw's least-privilege default. Adding a single tool to the default allowlist is a bounded change, and the tool is a no-op when no workflows are registered.

#### Alternative 2: Let the agent job read `.claude/workflows/` directly from the PR checkout

The simplest implementation would be to leave the workspace `.claude` tree alone after checkout and let Claude load whatever workflow scripts are present. This was a genuine candidate because it requires no save/restore plumbing at all. Rejected because it is a direct code-injection path: a fork PR could add or modify `.claude/workflows/*.js` and have gh-aw execute it with the agent job's privileges. The base-branch restore exists specifically to close this class of hole.

#### Alternative 3: Introduce a separate artifact dedicated to dynamic workflows

A standalone upload/download pair scoped to `.claude/workflows/` would avoid touching the general agent-config restore path. Rejected as redundant: the activation artifact already crosses the job boundary and already carries `.claude`; a second artifact adds another upload, another download, and a second ordering constraint relative to inline sub-agent and skill restores.

### Consequences

#### Positive
- Claude dynamic workflows and their nested support files are available in the agent job even when PR checkout is disabled, with content guaranteed to come from the trusted base branch.
- Headless Claude runs can invoke `Workflow` without weakening the overall permission model to `bypassPermissions`.
- `canRestoreClaudeWorkflows()` prevents the workflow repository's `.claude` tree from leaking into an unrelated repository checked out at the workspace root (including wiki and custom-steps checkouts).
- Regression coverage was added at both layers: Go tests (`pkg/workflow/pr_test.go`, `compiler_activation_job_test.go`, `claude_engine_tools_test.go`) and shell tests for the save/restore scripts.

#### Negative
- The restore step destroys the workspace `.claude` directory unconditionally (`rm -rf` then `cp -a`), so any legitimate PR-branch-authored change to `.claude` is silently discarded in the agent job; authors must land such changes on the base branch first.
- `canRestoreClaudeWorkflows()` encodes heuristics over `CustomSteps` and `CheckoutConfigs` (string matching on `${{` and `${{ github.repository }}`); these are approximations that can mis-classify unusual checkout configurations and will need maintenance as checkout options evolve.
- Adding `Workflow` to the default allowlist broadens the default tool surface for every Claude workflow in the repository, not only those that use dynamic workflows.
- The change required regenerating 70 `.lock.yml` files, making the diff large and review-expensive relative to its logical size.

#### Neutral
- Step ordering is now load-bearing: the Claude restore must run after the base-`.github` restore and before inline sub-agent/skill restores and pre-agent steps; this ordering is expressed in `compiler_yaml_ai_execution.go` and is only enforced by tests and comments.
- Documentation in `docs/src/content/docs/engines/claude.md` now records explicit headless invocation guidance and bare-mode limitations, so the feature's constraints are user-visible rather than implicit.
- The feature is Claude-specific (`engine.GetID() == "claude"`); other engines are unaffected but would need an analogous decision if they gain comparable dynamic-script support.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
