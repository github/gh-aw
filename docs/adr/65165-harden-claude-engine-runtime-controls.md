# ADR-65165: Harden Claude Engine Runtime Controls

**Date**: 2026-10-03
**Status**: Draft
**Deciders**: gh-aw maintainers (review pending)

---

### Context

The Claude engine currently permits behavior that makes unattended workflow execution less deterministic than the workflow contract implies. This pull request changes the Claude harness, generated workflow lock files, and regression coverage to align runtime behavior with declared permissions, retry semantics, timeout handling, and bare-mode capability loading. The PR description explicitly calls out gaps between the workflow configuration contract and Claude Code's actual permission, retry, and accounting behavior, plus a breaking default change for permission mode. Because these changes alter how a core engine enforces repository edits, restarts, and tool access across many workflows, the implementation needs an explicit architectural record.

### Decision

We will make Claude unattended execution default to deterministic, least-privilege runtime controls. Claude workflows will default to `permission-mode: dontAsk`, remove disabled native tools from the allowed set, require explicit edit permission for repository changes, resume retries from the captured session instead of replaying fresh work, and refuse fresh restarts after partial execution unless workflows explicitly opt in. We will also resolve runtime timeout expressions at execution time, preserve provider and authentication policy during detection, and explicitly load workflow-declared skills and subagents in bare mode through the workflow plugin.

### Alternatives Considered

#### Alternative 1: Preserve the Existing More Permissive Claude Runtime Defaults

This would keep defaults such as broader approval behavior and looser restart semantics, minimizing migration work for existing workflows. It was considered because it reduces immediate disruption and avoids a breaking change in permission handling. It was not chosen because the PR evidence shows that the previous defaults allowed mismatches between configured policy and actual runtime behavior, including unsafe task replay and implicit write capability assumptions.

#### Alternative 2: Shift Runtime Safety Entirely to Safe Outputs and Sandbox Boundaries

This approach would keep Claude engine behavior relatively unchanged and rely on outer sandboxing, safe outputs, and workflow authors to manage safety-sensitive operations. It was considered because those controls already exist and are useful isolation layers. It was not chosen because the PR makes clear that engine-local behavior still needs deterministic enforcement for tool permissions, restart lifecycle, timeout handling, and capability loading; outer controls alone do not prevent replay, overbroad edit behavior, or contract drift.

### Consequences

#### Positive
- Claude workflow execution more closely matches declared workflow policy, especially for repository edits, disabled tools, and approval behavior.
- Retry and restart behavior become safer by preventing accidental replay of partially completed tool work and staged outputs.
- Runtime diagnostics and operational controls improve through session resumption, timeout resolution, watchdog handling, and better accounting attribution.

#### Negative
- This is a breaking change for existing Claude workflows that relied on implicit edit capability or more permissive approval defaults.
- Workflow authors may need to recompile workflows and explicitly enable `tools.edit` or opt into fresh restarts for replay-safe scenarios.
- The engine and test surface become more complex because permission enforcement, capability loading, timeout handling, and accounting rules must stay synchronized across Go, JavaScript, and generated lock files.

#### Neutral
- Many lock file changes are regenerated artifacts of the runtime policy shift rather than distinct architectural decisions by themselves.
- Bare-mode capability loading remains supported, but the loading path is now explicit through the `gh-aw-workflow` plugin rather than ambient discovery.

---

*Draft decision record for [pull request #65165](https://github.com/github/gh-aw/pull/65165). Review before changing status to Accepted.*
