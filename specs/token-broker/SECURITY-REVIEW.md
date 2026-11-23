# Token broker security review: TLA+ findings

> [!WARNING]
> **Experimental.** These findings come from the bounded, informative model in
> this directory. See [README.md](README.md).

Checked against the server validation requirements SV-01 to SV-35 in the
[Token Broker Specification](../../docs/src/content/docs/specs/token-broker-specification.md),
Section 5.4. Findings 1 to 4 are closed by specification changes, so a
conforming broker is not exposed to them. Finding 5 is outside the broker's
control and needs repository settings.

| # | Severity | Vector | Breach | Config | Mitigation |
| --- | --- | --- | --- | --- | --- |
| 1 | 🟠 HIGH | The agent poisons a persistent self-hosted runner, and the later safe-output job's OIDC token is stolen there. | `NoCredentialDisclosure` | `DiscoverPersistentRunner` | SV-36, TM-13 |
| 2 | 🟠 HIGH | Another repository calls an approved reusable workflow and receives credentials scoped to this repository. | `RepositoryBound` | `DiscoverReusableCaller` | SV-16, TM-14 |
| 3 | 🟡 MEDIUM | An in-flight exchange records its credential after revocation and survives a 204. | `InvalidationComplete` | `DiscoverInvalidationRace` | SV-28, SV-31, Section 4.5, TM-15 |
| 4 | 🟡 MEDIUM | Cleanup does not run, and the credential stays live until it expires. | `CompletionRevokes` | `DiscoverSkippedCleanup` | SV-37, TM-5 |
| 5 | ⚪ LOW | A GitHub App writer token makes an agent-authored pull request trigger CI, which exposes repository secrets. | `NoAgentAuthoredCI` | `WitnessAgentAuthoredCI` | SV-16, SV-17, TM-16; require approval for bot-authored PR workflows |

## Mitigations in the proposal

| # | Server (Token Broker Specification) | Client (gh-aw) | Operator |
| --- | --- | --- | --- |
| 1 | SV-36 denies runners the broker has not enrolled. | The compiler warns when a broker-enabled job selects a self-hosted runner. | Use GitHub-hosted or ephemeral runners. |
| 2 | SV-16 requires both the caller and the reusable workflow to be approved. | None needed: the request carries no scope. | Enrol callers explicitly. |
| 3 | SV-31 closes the job before revoking, and SV-28 re-checks closure atomically when recording. | Unconditional cleanup step. | None. |
| 4 | SV-37 revokes on job completion. | Unconditional cleanup step. | Keep token expiry short. |
| 5 | SV-16 and SV-17 deny exchanges from pull-request runs. | None. | Require approval for bot-authored PR workflows, and protect target branches. |

Rollout guidance for operators is in the Enterprise Configuration reference,
under OIDC Token Broker.

## Verification

With all mitigations enabled, `Hardened` and `HardenedPersistentRunner` pass
every invariant and `CompletionRevokes`. Ten `Broken*` negative controls each
remove one baseline control and produce the expected breach. Reproduce with
`specs/token-broker/check.sh`.

## Residual risk

- Runner attestation is maintained by the operator (TM-13; SV-36).
- Approving a caller workflow approves every reusable workflow it is allowed to call (TM-14; SV-16).
- Secrets outside the broker remain reachable from CI triggered by bot-authored pull requests (TM-16; SV-16, SV-17).
- **Model limitation, not a broker-spec residual risk:** the bounded model abstracts cryptography, policy contents, egress, and expiry.
