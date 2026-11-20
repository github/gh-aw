---
title: Token broker threat model
description: Experimental TLA+ model of the token broker threat model, used to discover attack vectors.
---

# Token broker threat model

> [!WARNING]
> **Experimental.** This model is informative and bounded. It is not a
> conformance artifact, does not replace broker negative tests, and may change
> without deprecation.

`TokenBrokerThreatModel.tla` models the
[Token Broker Specification](../../docs/src/content/docs/specs/token-broker-specification.md)
threat model (Appendix E) against the server validation requirements in
Section 5.4. TLC explores every interleaving of legitimate jobs, the broker, and
an attacker, and reports any reachable breach as a counterexample.

## Model

- **Jobs.** `agent` (reader role) and `safeout` (writer role) run in an
  approved workflow of repository `R`. `safeout` starts after `agent` ends.
  `caller` is repository `R2` calling the approved reusable workflow.
- **Attacker contexts.** `prci` is CI triggered by an agent-authored pull
  request. `foreign` is another tenant. Both can obtain broker-audience OIDC
  tokens for themselves.
- **Broker.** An exchange passes `checked`, `minted`, `recorded`, and
  `returned`. These steps are server-side and continue after the job is
  cancelled. Invalidation revokes and closes the job.
- **Attacker actions.** Read the agent environment, attack a host step that
  runs in the agent job, poison a persistent runner, replay a cloud-audience
  token, open a pull request with a stolen writer token, use a token issued
  after close, and exchange any OIDC token it holds.

| Property | Meaning |
| --- | --- |
| `NoCredentialDisclosure` | The attacker never holds a live brokered credential. |
| `ApprovedContextOnly` | Credentials are issued only to approved job contexts. |
| `RepositoryBound` | A credential scoped to `R` is issued only to jobs of `R`. |
| `ReaderInAgentJob` | The agent job only receives the reader role. |
| `InvalidationComplete` | After invalidation completes, no credential of that job is live. |
| `CompletionRevokes` | Temporal: every credential of a finished job is eventually revoked. |
| `NoAgentAuthoredCI` | Witness: no agent-authored pull request triggers CI. |

`Fixes` enables candidate hardening beyond SV-01 to SV-35. `Mutation` removes
one baseline control as a negative control. Mutations are deliberately
insecure and are never runtime options.

| Fix | Hardening | Spec |
| --- | --- | --- |
| `runner` | Deny runners not enrolled as GitHub-hosted or attested ephemeral self-hosted. | SV-36 |
| `reusable` | Require both caller and reusable workflow revisions to be approved. | SV-16 |
| `close-first` | Close the job before revoking; recording re-checks closure atomically. | SV-28, SV-31 |
| `server-close` | Close and revoke on trusted job-completion evidence. | SV-37 |

## Discovered attack vectors

Each `Discover*` configuration removes one fix from the hardened profile and
reproduces a breach that SV-01 to SV-35 alone did not prevent.

| Config | Breach | Counterexample | Spec |
| --- | --- | --- | --- |
| `DiscoverPersistentRunner` | `NoCredentialDisclosure` | The agent poisons a persistent self-hosted runner. The later safe-output job runs there, and the planted code exchanges its OIDC token for a writer credential. | TM-13 |
| `DiscoverReusableCaller` | `RepositoryBound` | `R2` calls the approved reusable workflow. The `job_workflow_ref` alternative in SV-16 accepts it, and `R2` receives a credential scoped to `R`. | TM-14 |
| `DiscoverInvalidationRace` | `InvalidationComplete` | An exchange passes SV-19 and mints. Invalidation revokes, then the exchange records, then the job is closed. The new credential survives a 204. | TM-15 |
| `DiscoverSkippedCleanup` | `CompletionRevokes` | The job ends without its cleanup step. Nothing revokes the credential before its expiry. | TM-5 |
| `WitnessAgentAuthoredCI` | `NoAgentAuthoredCI` | A writer credential opens an agent-authored pull request, which, unlike `GITHUB_TOKEN`, triggers CI. The broker denies that CI, but repository secrets are outside its control. | TM-16 |

The `Broken*` configurations confirm that each baseline control matters.

| Config | Mutation | Control removed | Breach |
| --- | --- | --- | --- |
| `BrokenEnvExclusion` | `no-env-exclusion` | Agent container excludes OIDC request variables (TM-1) | `NoCredentialDisclosure` |
| `BrokenHostStep` | `unhardened-host-step` | Hardened host git commands in the agent job (TM-1) | `NoCredentialDisclosure` |
| `BrokenAudience` | `no-audience` | SV-12 | `NoCredentialDisclosure` |
| `BrokenTenant` | `no-tenant` | SV-15 | `ApprovedContextOnly` |
| `BrokenRevision` | `no-revision` | SV-16 | `RepositoryBound` |
| `BrokenPRTrust` | `pr-trust` | SV-17 | `ApprovedContextOnly` |
| `BrokenJobIdentity` | `no-job-identity` | SV-18, SV-23 | `ReaderInAgentJob` |
| `BrokenCloseCheck` | `no-close-check` | SV-19 | `NoCredentialDisclosure` |
| `BrokenRecordOrder` | `record-after-return` | SV-28 | `InvalidationComplete` |
| `BrokenRunnerCheck` | `no-runner-check` | SV-36 | `NoCredentialDisclosure` |

`Hardened` (GitHub-hosted runner) and `HardenedPersistentRunner` enable every
fix with no mutation and check all properties. They pass.

## Limits

The model is bounded (`MaxTokens = 3`, one attempt per job, one repository
pair). It abstracts cryptography, policy contents, network egress, and token
expiry. Passing it does not show that a deployment is secure; it shows that the
modelled controls block the modelled attacks.

## Running the checks

```sh
export TLA2TOOLS_JAR=/path/to/tla2tools.jar
specs/token-broker/check.sh
```

Set `JAVA_BIN` to choose a Java runtime, `TLC_WORKERS` for parallelism,
`TLC_CONFIG_FILTER` to run one configuration, and `TLC_RESULTS_DIR` to keep
logs. Every configuration must give its expected result; the full run takes
under a minute.
