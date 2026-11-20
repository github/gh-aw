---
name: safe-output-target-deferral
description: Keep supported unresolved safe-output targets ahead of authorization without changing invocation binding or handler semantics.
---

# Safe-output target deferral

Use when changing temporary target resolution or authorization for any safe
output. Apply the checks below to each affected handler; do not assume every
handler accepts the issue resolver's forms.

## Contract

- An accepted unresolved target must defer before an authority request or
  consuming decision. After production, bind the resolved invocation and obtain
  one decision; mutate only the authorized target.
- Reuse the handler's existing resolver. `resolveIssueNumber` accepts
  case-insensitive `aw_` prefixes, optional `#`, surrounding whitespace and
  matching quotes. `resolveNumberFromTemporaryId` does not accept quotes.
  Both use the existing 3-12 character alphanumeric/underscore ID validation.
- Check only supported target fields. Do not infer targets from arbitrary
  strings, suffixes, output-producing `temporary_id`/`output_id`, or unrelated
  identities. Preserve invalid-input behavior, numeric targets, configured
  selection, staging, builtin exemptions and terminal unresolved reporting.
- Keep the authorization snapshot deep-frozen. Handlers receive an independent,
  initially data-equivalent mutable message copy and snapshot-backed resolutions.
  Newly created IDs may be published for later invocations. Preserve per-call
  cache refreshes and mutation-during-authority-wait protection.
- Preserve the protocol, declaration/input/policy hashes, `scope.binding`,
  invocation identity and replay behavior. Do not authorize a replacement after
  an authorized handler defers, move all preparation before authorization, or
  use a writable message copy to expand or redirect the bound operation.

## Procedure

1. Inspect current HEAD and existing coverage before editing. Read
   `actions/setup/js/safe_output_handler_manager.cjs`,
   `actions/setup/js/temporary_id.cjs` and the affected native handler's target
   resolution. Load the `developer` skill for repository validation gates.
2. Trace every supported target alias to its actual resolver. For issue/PR
   targets include label operations, comments, updates, closing, sub-issue links,
   milestone/agent assignment, issue type/fields and project content. Discussions
   use their own number resolver. Other native handlers, project/artifact
   references, custom executors and queue/Claim adapters require separate
   evidence; do not extend their accepted forms by inference.
3. Reproduce consumer-before-producer ordering with an unprotected fake producer,
   fake authority and recording GitHub client. Keep expected repo/number literal,
   independent of the resolver. Record authority calls and mutations when the
   producer runs; assert those observations outside handlers, since manager
   error handling can catch assertions thrown inside a fake producer.
4. Share existing recognition, not another quote/whitespace/case regex. Apply
   the manager guard across the existing supported target fields; leave raw
   batch identity and the snapshot-backed map unchanged.
5. Cover accepted variants and aliases without a Cartesian matrix; verify zero
   authority calls/mutations before production, exactly one decision and the
   expected mutation afterward, permanent nonresolution, malformed IDs and
   unrelated identities. For other handlers, distinguish guard-only coverage
   from real authorization-to-effect coverage.
6. Correct directly conflicting specification wording and nearby comments.
   Do not claim universal native-handler, adapter or custom-executor guarantees.

## Verification and handoff

Run from the repository root:

```bash
export npm_config_cache="$TMPDIR/gh-aw-npm-cache"
make fmt
npm --prefix actions/setup/js run test:js -- safe_output_handler_manager_authorization.test.cjs safe_output_handler_manager_bound_temporary_ids.test.cjs temporary_id.test.cjs
npm --prefix actions/setup/js run typecheck
python3 scripts/check-instruction-prefixes.py .github/skills/safe-output-target-deferral/SKILL.md
```

Run affected handler/resolver suites when their code changes and the final
repository validation gate before committing/pushing. No live GitHub mutation or
new tooling is needed. Report the reproduced failure, minimal fix, exercised
forms/aliases, real-handler versus guard-only coverage and remaining limits.
Stop with evidence if a fix needs a protocol change, new temporary-ID capability
or broader handler redesign.
