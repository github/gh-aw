---
description: Generation checklist for workflow IDs, triggers, tools, network, outputs and scope.
disable-model-invocation: true
---

# Workflow Design Checklist

Use after the [creator](create-agentic-workflow.md) selects implementation mode.
Preserve its safety/mode rules. Complete these decisions before writing source;
then apply the [validation contract](create-agentic-workflow-validation.md).

## 1. Pick the workflow ID

Derive kebab-case from the name. Check `.github/workflows/<workflow-id>.md`;
choose a more specific ID if it exists. Never overwrite another workflow.

## 2. Derive architecture, then choose the trigger

Persist canonical `intent:`; derive architecture from its activation conditions,
required effects, evidence and inverse/no-op cases. Choose the smallest trigger
that satisfies it. Use the [Decision Matrix](triggers.md#decision-matrix) and
[trigger details](create-agentic-workflow-trigger-details.md) for scenario mappings.

Apply the [memory reuse checkpoint](designer.md#memory-reuse-checkpoint) before
selecting tools or persistence; follow [memory policy](memory.md#scheduled-dreaming).

- Changed-file reviews: `pull_request` with scoped `paths:`. Backend reviews scope
  migrations/schema/API contracts; design reviews scope tokens/theme/assets;
  compliance reviews scope manifests/policy/control docs.
- CI-linked work or artifact consumers: `workflow_run` scoped to named workflows
  with `types: [completed]` and the required conclusion. Do not race producers.
- External deployment incidents: `deployment_status`; recurring reports/audits:
  `schedule` plus `workflow_dispatch` for reruns/backfills.
- Keep known CI failures attached to the affected PR with `add-comment`;
  use deduplicated `create-issue` for unowned or repository-wide incidents.
- Reports name window, grouping and dedup key. For incidents derive stable keys
  from workflow/job/normalized failure; search existing open issues first.
  Coverage analysis needs an explicit evidence-source fallback.

### Choose the previous-result strategy

Choose by outcome, not a universal recurring-workflow default:

- **Wait:** use `on.skip-if-match` while the prior issue/PR remains open;
  resume after closure/merge when only one active item should exist.
- **Replace:** use `create-issue.close-older-issues: true`, with `close-older-key`
  when needed; use `add-comment.hide-older-comments: true` for rolling PR updates.
- **Keep:** preserve distinct results/history; require prior-output review and
  a distinct purpose for every new issue, PR or comment.

## 3. Keep permissions read-only

Follow [constraints](workflow-constraints.md). Never grant the agent
`issues: write`, `pull-requests: write` or `contents: write`.
For Copilot inference recommend `permissions: { copilot-requests: write }`;
follow [scope/caveats](syntax-core.md). Explain safe-output routing if direct
writes are requested.

### 4. Select tools

- Sandboxed workflows enable `bash` and `edit` by default; omit redundant declarations.
- Codex cannot enforce a nonempty per-command `tools.bash` allowlist. Use
  `tools.bash: false` or an engine supporting command allowlists.
- Prefer `tools.github.mode: gh-proxy` and `gh` for GitHub reads. Add
  `tools.cli-proxy: true` and mounted `mcp-clis` only for non-GitHub MCP access.
- Use `toolsets:`; add `playwright` only for browser/visual tasks. Prefer topic
  references over embedded tutorials. Declare skills/plugins at top level.

```yaml
tools:
  github:
    mode: gh-proxy
    toolsets: [default]
  cli-proxy: true
```

Omit `cli-proxy: true` for GitHub-only reads.

## 5. Infer network access

Inspect repository manifests rather than asking the ecosystem.
Follow [manifest mapping](network.md#inferring-ecosystem-from-repository-files).
Never use `network: defaults` alone for install/build/test workflows.

For policy/compliance audits limited to repository inspection and GitHub
`gh-proxy` reads, enforce "never probe externally" structurally with
`network.allowed: []`; omit external web/MCP tools. For Claude/Codex explicit
network settings also disable provider-hosted web tools unless enabled separately;
follow [hosted-web policy](network.md#hosted-web-policy-networkhosted-web).

```yaml
network:
  allowed: []
```

## 6. Configure safe outputs

Map mutations to dedicated outputs:
issues -> `create-issue`; comments -> `add-comment`; PRs -> `create-pull-request`;
labels -> `add-labels`; downloads -> `upload-artifact`;
embeddable assets -> `upload-asset`; release notes -> `update-release`.

- Restrict `create-pull-request.allowed-files`.
- Never substitute shell/API/GitHub writes for safe outputs.
- Require `noop` with a brief reason for no action, duplicates or insufficient evidence.
- Issue bodies must be meaningful, 20-65000 characters, not placeholders.
- Keep agent-job scopes read-only; safe-output jobs own write permissions.

## 7. Choose who can trigger

Team-only is the default; recommend `roles: all` for community entrypoints.
Prefer `pull_request`. Use `pull_request_target` only when base-repository context
is necessary; default `checkout: false`. Never check out or execute PR-head code.
Follow [checkout guidance](https://github.github.com/gh-aw/reference/checkout/#pull_request_target-checkout).

## 8. Bound cost and context

Follow [high-volume triage](workflow-patterns.md#high-volume-triage-and-escalation-pattern):
cheap deterministic checks before escalation; explicit `noop`/safe outputs for
known, duplicate, stale and low-value cases; frontier reasoning only for ambiguous
or high-value inputs. Preprocess large data in deterministic `steps:` into compact
`/tmp/gh-aw/` files. Bound [sub-agents](subagents.md); load context on demand and
follow [token guidance](token-optimization.md).

## 9. Omit redundant defaults and verify

Usually omit `engine: copilot`, unrestricted bash, edit and default timeouts.
Apply [output/validation checks](create-agentic-workflow-validation.md); compile
source, fix all errors and include its generated lock in the PR.
