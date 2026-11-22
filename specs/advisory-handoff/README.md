# Advisory handoff protocol

This model describes the maintainer-driven handoff to a temporary private GitHub advisory fork. It does not run against GitHub or use real advisory data.

## Expected use

1. Run `gh aw advisory setup` once to create or reuse a personal private side repository without collaborators or pending invitations. Configure a trusted, manually dispatched workflow with an upstream-scoped read-only checkout credential.
2. Prepare and test a remediation at an exact base commit. Keep the patch and test notes in the side repository's artifact. Record the exact successful first-attempt run and artifact IDs with `gh aw advisory record <GHSA-ID>`. Use `--create-fork` only if the advisory needs a temporary private fork.
3. Run `gh aw advisory inspect <GHSA-ID> --print-patch` in a private terminal to review the untrusted patch and the destination. Without `--print-patch`, inspection shows only metadata and the digest. The digest identifies the handoff; it does not attest that the fix passed tests.
4. Run `gh aw advisory transfer <GHSA-ID> --approve <digest>`. Transfer verifies the handoff again and attempts to create only the destination branch. Open the PR from the advisory page. If transfer warns about base drift, check whether the branch already exists before rebasing and retesting.

## Model boundary

`sideReady` represents the verified side repository. Each GHSA has independent fork, record, review, transfer, and branch state. `ClaimFork` persists the local marker; `SendForkRequest` and `ProvisionFork` separate requesting the fork from its appearance. Retrying an identical record is a stuttering step.

A plan stands for an immutable verified handoff: exact patch, source coordinates, tested base, and destination. The two-advisory configuration varies patch and base for one advisory and keeps the second advisory's plan fixed to bound the state space while still allowing interleaving. The model assumes artifact and digest verification, one stable verified private fork and destination ref per GHSA, and an atomic expected-absent lease on that destination. It does not verify those assumptions, credential selection, GitHub authorization or confidentiality, artifact retention, or patch parsing. It models the recorded-artifact path, not one-off local patches. Human PR creation and publication are outside the CLI protocol.

## Properties and results

| Configuration | Property | Result |
| --- | --- | --- |
| `AdvisoryHandoff.cfg` | Creation uses the checked approval and verified private fork; a branch is never overwritten; a success report follows a matching base check. Under weak fairness and without a lost request, a claimed fork eventually appears. | Passed with TLC 2.19: 27,282 distinct states. |
| `BaseRace.cfg` | The tested base still matches at the instant the destination branch is created. | Counterexample: the base moves after the pre-push check. The post-push check can warn but cannot undo the write. The destination lease does not cover the base ref. |
| `ForkRetry.cfg` | Every claimed fork eventually appears. | Counterexample: the local claim persists but the request never reaches GitHub. Retries only poll, so progress requires an operator to create the fork in GitHub. Weak fairness cannot help once no submission is enabled. |

The base race is a consistency gap, not evidence of an unapproved or public push. Keep the warning explicit about the branch possibly existing. A strict guarantee would require an operation that checks the base and creates the destination atomically. For fork recovery, distinguish definitive rejection from an ambiguous result; an ambiguous result needs reconciliation or explicit recovery rather than an automatic repeat of the POST.

Other bottlenecks are outside the state model: fork polling can take up to five minutes, and `record`, `inspect`, and `transfer` separately download and verify the artifact. Measure those costs before changing the polling cadence or caching confidential patch bytes. An expired artifact requires a fresh run and a new review.

## Run with TLC

From this directory, set `TLA2TOOLS_JAR` to the official TLA+ Tools JAR:

```bash
for model in AdvisoryHandoff BaseRace ForkRetry; do
  java -cp "$TLA2TOOLS_JAR" tlc2.TLC \
    -metadir "$(mktemp -d "$TMPDIR/advisory-tlc.XXXXXX")" \
    -config "$model.cfg" AdvisoryHandoff.tla
done
```

The two witness configurations intentionally report violations. No live advisory validation is implied.
