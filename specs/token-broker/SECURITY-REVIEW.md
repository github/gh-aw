# Token broker security review: TLA+ findings

> [!WARNING]
> **Experimental.** These findings come from the bounded, informative model in
> this directory. See [README.md](README.md).

Checked against the server validation requirements SV-01 to SV-35 in the
[Token Broker Specification](../../docs/src/content/docs/specs/token-broker-specification.md),
Section 5.4. Every finding is mitigated by a specification change, so a
conforming broker is not exposed to it.

| # | Severity | Vector | Breach | Config | Mitigation |
| --- | --- | --- | --- | --- | --- |
| 1 | 🟠 HIGH | The agent poisons a persistent self-hosted runner, and the later safe-output job's OIDC token is stolen there. | `NoCredentialDisclosure` | `DiscoverPersistentRunner` | SV-36, TM-13 |
| 2 | 🟠 HIGH | Another repository calls an approved reusable workflow and receives credentials scoped to this repository. | `RepositoryBound` | `DiscoverReusableCaller` | SV-16, TM-14 |
| 3 | 🟡 MEDIUM | An in-flight exchange records its credential after revocation and survives a 204. | `InvalidationComplete` | `DiscoverInvalidationRace` | SV-28, SV-31, Section 4.5, TM-15 |
| 4 | 🟡 MEDIUM | Cleanup does not run, and the credential stays live until it expires. | `CompletionRevokes` | `DiscoverSkippedCleanup` | SV-37, TM-5 |
| 5 | ⚪ LOW | A GitHub App writer token makes an agent-authored pull request trigger CI, which exposes repository secrets. | `NoAgentAuthoredCI` | `WitnessAgentAuthoredCI` | SV-16, SV-17, TM-16; require approval for bot-authored PR workflows |

## Verification

With all mitigations enabled, `Hardened` and `HardenedPersistentRunner` pass
every invariant and `CompletionRevokes`. Ten `Broken*` negative controls each
remove one baseline control and produce the expected breach. Reproduce with
`specs/token-broker/check.sh`.

## Residual risk

- Runner attestation is maintained by the operator.
- Approving a caller workflow approves every reusable workflow it is allowed to call.
- Secrets outside the broker remain reachable from CI triggered by bot-authored pull requests.
- The model is bounded and abstracts cryptography, policy contents, egress, and expiry.
