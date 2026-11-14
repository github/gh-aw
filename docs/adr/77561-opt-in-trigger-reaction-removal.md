# ADR-77561: Opt-in Trigger Reaction Removal on Completion

**Date**: 2026-11-13
**Status**: Draft
**Deciders**: Unknown (AI-drafted from PR #77561 by pelikhan; human deciders [TODO: verify])

---

### Context

When a workflow is triggered, the activation job adds a reaction (by default `eyes`) to the triggering issue, pull request, comment, or discussion for immediate feedback. Nothing removes that reaction when the run ends. With `status-comment: false` the conclusion job has no reaction step at all, so a succeeded, failed, or skipped run leaves the reaction in place. For comment-triggered workflows such as slash commands, a lingering `eyes` reads as "still working" and is the only visible status for people who do not open the Actions tab. Any change must be opt-in so existing workflows keep their behavior, must work without a status comment, and must not widen the permissions of the safe-output GitHub App.

### Decision

We will add an opt-in boolean, `on.reaction.remove-on-complete` (default `false`). When it is enabled, activation records the ID of the reaction it created as a job output, and the conclusion job removes that reaction through the existing `add_reaction.cjs` handler in remove mode, using the activation credential. The primary driver is a truthful completion signal with the smallest change: the cleanup reuses the existing handler, token, and permission model rather than adding a new mechanism.

### Alternatives Considered

#### Alternative 1: Completion-state reactions (`on-success` / `on-failure`)

Map each outcome to a reaction, for example `+1` on success and `confused` on failure, as suggested in issue #77508. This gives more information than removal alone. We did not choose it for this change because it needs add-and-remove logic for two reaction types, failure-state mapping across every conclusion path, and more configuration surface and API calls. It is a viable follow-up that can reuse the same cleanup path. This was a close call.

#### Alternative 2: Tie reaction updates to the status comment

Extend the existing status-comment update step so that it also changes the reaction. We rejected this because it couples reactions to the status comment, which is exactly the configuration (`status-comment: false`) that the issue reports as broken.

#### Alternative 3: Always remove the reaction

Remove the reaction on every run without a flag. We rejected this because it changes visible behavior for every existing workflow without an opt-in. Some users may rely on the persistent marker.

#### Alternative 4: Keep the per-workflow custom job

Continue the current workaround, a custom job per workflow that swaps reactions. This is the status quo, and the issue author reports that each workflow then needs its own job, permissions, and test. We rejected it because the cost is repeated in every workflow.

### Consequences

#### Positive
- Completed runs no longer leave a stale `eyes` reaction. The reaction now signals "in progress" only while the run is active, including for failed and skipped runs.
- Cleanup works independently of `status-comment`.
- The default is `false`, and the new behavior applies only to workflows that opt in.
- The change reuses `add_reaction.cjs`, the activation token, and the activation GitHub App. It does not expand safe-output App permissions.
- Expected cleanup failures are handled without failing the job. A 404 (already removed), a locked resource, and a 429 or rate-limit error are logged rather than raised.

#### Negative
- Cleanup runs only when the conclusion job runs. If the run is cancelled before conclusion, the reaction can remain. This limitation is documented.
- Discussion cleanup uses GraphQL `removeReaction` keyed by subject and content for the authenticated user, not by the recorded reaction ID. It cannot distinguish the reaction this run added from another reaction of the same content by the same credential.
- The activation job gains a `reaction_id` output, and the conclusion job's `if:` gains an `always()` branch. Both make the conclusion condition harder to reason about.
- Each enabled run makes one extra API call. With a GitHub App, the conclusion job also mints an extra token, and the activation credential needs issues, pull-requests, or discussions write scope.

#### Neutral
- The JSON schema, the trigger parser, and the trigger reference documentation gain a new key. The authentication note now mentions conclusion cleanup.
- Dry-run compilation forces cleanup off, so dry-run lock files contain no cleanup step.
- Cleanup uses the activation credential, so its reach is bounded by that credential's existing scope.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
