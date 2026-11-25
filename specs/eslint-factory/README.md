# ESLint factory bounded formal model

This directory models the factory's queue-worker lifecycle and compares its assumptions with executable local code. It is **not** a proof of production refinement, rule quality, security, scheduler fairness or unbounded liveness.

## Files and abstraction

| File               | Purpose                                                                                                                                |
| ------------------ | -------------------------------------------------------------------------------------------------------------------------------------- |
| `Factory.tla`      | Finite admission, prefix request, immutable assignment, Claim-scoped intent, Completion, effect, independent readback and Result model |
| `cases.mjs`        | Six safety configurations, nine exact negative controls and seven guarded reachability witnesses; generates TLC `.cfg` files           |
| `check.mjs`        | Pinned, time/heap/worker-bounded TLC driver and comparison runner                                                                      |
| `compare.mjs`      | Parsed YAML/TypeScript extraction, actual library build/scan commands and native CJS/AsyncLocalStorage probes                          |
| `trace.mjs`        | Strict finite-value trace parser, independent abstraction replay and readable trace rendering                                          |
| `compare.test.mjs` | Comparison sensitivity, mutated executable fixture and generated trace validation tests                                                |
| `trace.test.mjs`   | Readable trace rendering and cell-escaping test                                                                                       |

The universe contains two externally admitted root Works. `queueBranch` starts absent; the first authorized producer submit atomically creates it with the compiler-approved Policy and Work. `Installed` models availability of that compiled Policy proposal, not an administrator-seeded ledger. Dispatch requires the created branch and Policy. Each Work has a provisioned worker profile; equal priority, dependency readiness, capacity, run binding and resource authorization are simplified assumptions. `Dispatch` projects this worker activation's Dispatch from one protected prefix request, not the complete native dispatch set. As in the v4 replay rule, a Dispatch carries exactly one Claim: a request that grants several Works grants each its own Dispatch, and a mixed-profile cohort gives each profile its own run (the monster precheck runs only when the activation's Work is a monster Work). Leaving the other modeled Work queued is projection, not a claim about scheduler selection. This two-Work projection does not exercise all seven dispatch slots, assignment byte bounds or queue fairness/recovery algorithms.

`birth` records original membership and `original` must remain equal to it, even after the Claim settles and a re-read assignment would be empty. A safe-output selector may be omitted (it resolves to the one member) or name the member's own handle; a foreign handle is rejected. `Stage` projects one output family per Claim: miner PR, refiner issue or monster remediation assignment. Its count is **not** the aggregate of every output type. Modeled `OutputMin`/`OutputMax` are scenario assumptions, not the complete worker-specific contracts prepared by `eslint_factory_portfolio.cjs`. A separate no-write scenario models explicit no-write Work.

`Complete` does not produce a Result or readback evidence. `Effect` requires Completion and an active Claim. `Readback` abstracts successful independent verification of the **whole** immutable contract, including additional output families, memory, resource receipts and queue controls when required. Unavailable readback can remain unverified forever; job success or an agent assertion cannot replace it. `Result` requires that evidence and the modeled family's cardinality. Cancellation does not undo already applied writes, but blocks later writes and settlement in this abstraction.

`Admit` abstracts an authorized producer. The actual dispatcher now stages seven independent date-keyed tasks from a trusted prepared plan: one miner task plus a refiner and a monster task for each of three rotating rules read from the registry. This fixed two-Work model does not establish that daily admission behavior. `eslint_factory_portfolio.test.cjs` exercises native admission, idempotent same-day submissions, rule rotation, producer-entitlement rejection, and seven singleton grants. Publishing an issue is not admission. `NoAutomaticDAG` tracks the explicit admitted set; its negative control injects unauthorized admission during an effect. The supplied workflows still do not install an automatic miner → refiner → monster dependency chain.

`Precheck` is run-wide for monster, whereas output scope is per Claim. `scan` describes the assigned rule's diagnostics after a completed scan; `lintClean` holds only when that rule has none. Warning-only diagnostics of the assigned rule keep the scan actionable even though ESLint exits zero; other rules' diagnostics do not affect it. Installation, build and tool failures do not mark it. `Quality` intentionally does not gate authority or Result: build/test/low-false-positive requirements and the monster prompt's one-assignment-per-Claim limit are agent obligations rather than runtime properties established by this model.

The daily producer admits write-capable Work. A clean monster scan or absent miner
rule idea now cancels that Claim instead of completing it with only `noop`;
an explicitly no-write assignment can still complete without resource effects.
The finite model's no-write scenario remains an abstraction, not proof of the
daily producer's cancellation or live reservation release.

The separate [reservation recovery model](../work-queue/README.md#abandoned-dispatcher-reservation-recovery)
covers the full three-profile cohort after dispatcher exit. It checks
terminal-origin evidence, a prepared recovery/start-marker race, no resend or
service refund, and independent recovery of never-started siblings while miner
remains bound. `Factory.tla` does not model these queue-wide transitions or
change its original assignment authority.

## Checked properties

The safety configurations check types, immutable original membership, one Claim per Dispatch, compiled Policy availability, branch creation with first admission, Claim-scoped intents, Completion before writes, independent readback before Result, per-Claim result cardinality, the rule-scoped diagnostics gate, explicit admission and assignment size. With only two Works, `BoundedAssignment <= 3` is a finite-universe bound, not validation of the full seven-slot dispatcher budget. `Spec` permits stuttering; deadlock checking is disabled for terminal or stalled workers. No fairness assumption or temporal progress property is checked.

Each negative configuration changes exactly one abstraction rule and requires the **named** invariant violation, not merely a nonzero TLC exit:

| Mutation                                                           | Expected diagnostic        |
| ------------------------------------------------------------------ | -------------------------- |
| Accept an intent bearing a foreign Claim handle                    | `ScopedIntents`            |
| Re-read (shrink) membership after the Claim is cancelled           | `ImmutableMembership`      |
| Group both eligible Works into one Dispatch                        | `SingleClaimDispatch`      |
| Admit/dispatch without compiled Policy proposal                    | `PolicyRequired`           |
| Write before Completion                                            | `EffectRequiresCompletion` |
| Settle without independent readback                                | `ResultRequiresReadback`   |
| Verify an under-minimum output count                               | `ResultCardinality`        |
| Treat warning-only diagnostics of the assigned rule as clean       | `RuleDiagnosticsGate`      |
| Admit another Work as an issue/effect side effect                  | `NoAutomaticDAG`           |

Witness configurations keep `Fault = "none"` and add `NeverWitness` alongside all safety invariants. Its deliberate failure demonstrates reachable warning-only scans that stay actionable, Completion without Result, one Claim with three applied outputs, a Result despite failed quality obligations, tool failure without a clean flag, independently verified Result and explicit no-write Result. These are bounded existential witnesses, not liveness proofs or evidence that production agents actually choose those behaviors.

## Actual code comparison

The checker parses workflow frontmatter and compiled locks with the repository's Go YAML parser (through `scripts/markdown_metadata_test_adapter.cjs`), finds the TypeScript plugin object through its syntax tree, builds the **actual** library entrypoint with its unchanged package scripts/tsconfig and compares its exports with the actual executed CJS config. It does not treat matching strings as a refinement proof.

Build and scan fixtures reuse existing source/dependencies via symlinks as build inputs and emit compiled files only in the artifact directory. The checker runs the actual `npm run lint:setup-js` (with the unix formatter the precheck uses) and `lint:setup-js:changed` scripts against inert source samples. ESLint never executes the samples. A separate fixture executes the actual monster precheck shell after rebasing its agent-output and work-queue snapshot paths; a fixture snapshot assigns the rule, and an npm stub supplies measured lint exit/log results or explicit installation/build/tool failure. No `npm ci`, installation, workflow/CI run, API write or network request is performed.

| Observation | Evidence and boundary |
| --- | --- |
| A v4 assignment carries exactly one Claim; its payload is deeply frozen | Native `normalizeAssignment` probes reject zero or two members and unknown fields; this normalization alone is not authentication |
| Omission selects the one member; foreign selectors and agent-supplied authority fail | Native scope, foreign-selector, conflicting-ID, authority-override and nested AsyncLocalStorage escape probes. Two Dispatches reusing handle `h1` have distinct Claim identities and artifact paths |
| The compiled policy is the only policy; one Claim per Dispatch | The dispatcher lock's `GH_AW_WORK_QUEUE_POLICY` (repository bound to a fixture) passes `validatePolicyEnv` and approves every ESLint worker in pool `eslint-factory`; an unapproved profile fails submission; the actual planner grants the seven-node cohort, built from the parsed rule registry, as seven singleton Dispatches and replay rejects a grouped Dispatch with `dispatch_state_conflict`; without a policy or an allowlisted worker the dispatcher publishes nothing |
| Open Claim, bound run attempt, worker approval and effect scope gate authority | `authorizeWorkerClaim` replays the ledger from an in-memory queue branch: it requires a `completed` finish intent and the bound attempt, fails with `E004` for a withdrawn worker or foreign repository, and suppresses Claims settled by real Completion/ClaimCancellation commits |
| Count contracts are per Claim | The actual collector entrypoint runs once per singleton assignment: three issues for each of two Claims, an overflow rejected, a foreign selector rejected without counting toward the minimum, and a two-member assignment accepting nothing. Accepted intents do not establish native writes |
| Completion/success cannot replace readback | v4 has no independent delivery verifier; collector and authorization probes are not delivery evidence |
| CJS coverage excludes tests; generic JS/TS goals are broader | Actual resolved ESLint configs and package-script fixture scans; changed scan omits nested CJS |
| Warning-only lint exits zero; the precheck is rule-scoped | Actual library scan and precheck flag probes with a fixture Work snapshot: the assigned rule's warnings or errors keep the scan actionable, other rules' diagnostics leave it clean, and a Work without a rule fails the precheck. `require-http-response-error-listener` is an error, other configured rules warn |
| Refiner memory is declarative, lowering to a protected git-tree adapter | Parsed `tools.work-queue.memory` is compared with the actual compiled preparation job's adapter and immutable target. The real collector preserves structured objects and checks the compiled one-per-Claim maximum independently for two Claims; no remote memory verification is exercised |

`comparison.json` separates fact mismatches from documentation observations and assumptions. The README's rule-link table is compared with registered IDs as table coverage only, not a semantic audit of all rule descriptions. The compiled dispatcher policy is read from the checked-in lock; actual deployed permissions, credentials and contracts remain outside its evidence.

The rule-link table now covers the registered `no-dynamic-string-replacement` rule and the previously omitted entries `no-async-foreach-callback`, `no-single-char-string-replace`, `no-string-fallback-for-non-string-message` and `require-http-response-error-listener`. The comparison requires linked-table coverage to match registered rules and checks that section targets exist. Tests deliberately remove these rows or break their targets; this checks reference coverage, not all rule-description semantics.

The collector probe runs its unchanged CJS entrypoint once per singleton assignment with fixture-supplied original assignment context and an artifact output root. Its legacy patch-directory existence probe is stubbed as absent without touching that directory. The probe exercises actual per-Claim counting, but does not authenticate a live assignment or deliver an issue.

Tests alter extracted facts, add misleading comments/body examples, rename an actual registration, and downgrade the hard HTTP rule in an executable fixture. That fixture changes the observed ESLint exit status and must fail the comparison. Trace tests also reject altered transitions and check recorded model/config/log hashes.

## Non-goals

The model and checker do not enforce or prove:

- Prompt wording, nonduplicate issue creation, or the monster's one-assignment-per-Claim limit.
- Agent obligations to run builds and tests or meet quality and false-positive thresholds.
- Successful downstream remediation.
- Production refinement, deployed policy/profile permissions, scheduler fairness, or unbounded liveness.

These are agent, deployment, or operational obligations outside the bounded model.

## Sync Notes

The checks use dependencies from two existing package directories. From the repository root, install them first:

```bash
npm ci --prefix eslint-factory
npm ci --prefix actions/setup/js
```

`eslint-factory` provides TypeScript, ESLint, and Vitest; `actions/setup/js` provides the runtime modules, and the comparison parses YAML with the repository's Go parser through `scripts/markdown_metadata_test_adapter.cjs` (it needs `go`). Then run `node specs/eslint-factory/check.mjs`, `node --test specs/eslint-factory/compare.test.mjs`, and `node --test specs/eslint-factory/trace.test.mjs`. The comparison test verifies that missing rows and broken targets are detected; the trace test covers readable rendering and cell escaping only. Run `node --test specs/eslint-factory/compare.test.mjs` and `node --test specs/eslint-factory/trace.test.mjs` after changing comparison or trace logic. Run the checker and comparison test after adding, removing, or renaming a registered rule or editing the rule-link table. This checks link coverage, not rule-description semantics.

The model checker also requires Java and the existing official TLC jar. The checker defaults `JAVA` to a macOS Homebrew path, so non-macOS hosts must set `JAVA` to their Java executable; see [Reproduce](#reproduce).

## Reproduce

Use existing Node dependencies in `eslint-factory/node_modules` and `actions/setup/js/node_modules`, an existing Java runtime, and the official TLC jar. On non-macOS hosts, set `JAVA` to the Java executable because the checker default is a macOS Homebrew path. The driver refuses any jar except SHA-256 `936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88`. It uses at most two TLC workers, a 1 GiB Java heap, a 60-second per-case timeout (overridable up to 120 seconds) and a ten-minute total budget. JVM extracted modules, compiler outputs and all fixture files stay inside the artifact path.

```bash
export TLA2TOOLS_JAR=/absolute/path/to/existing/tla2tools.jar
export JAVA=/absolute/path/to/java
export ARTIFACTS=/absolute/path/to/session/artifacts/eslint-factory-formal-review
node specs/eslint-factory/check.mjs --artifacts="$ARTIFACTS/run"
FACTORY_ARTIFACTS="$ARTIFACTS/tests" \
FACTORY_MODEL_ARTIFACTS="$ARTIFACTS/run" \
  node --test specs/eslint-factory/compare.test.mjs
node --test specs/eslint-factory/trace.test.mjs
node actions/setup/js/node_modules/prettier/bin/prettier.cjs \
  --check 'specs/eslint-factory/*.mjs'
```

`--case=monster` selects a single case. The driver records the current HEAD and origin/main anchor, exact command, Java/TLC/Node/dependency identities, source hashes, verdicts and state counts in `validation.json`. HEAD is an anchor, not a claim that uncommitted formalization/README changes are committed: the measured inputs are current working-tree bytes identified by hashes. Every counterexample/witness includes raw `tlc.log`, generated config, copied model, readable `trace.md` and hash/HEAD-bound `trace.json`. Trace validation checks Init, all enabled transitions/frame conditions via an independent executable replay of this finite abstraction, prior safety and the exact terminal diagnostic. That replay is not a proof about native runtime code. Changed measured sources or HEAD during validation cause failure and require rerunning.

The six safety cases exhaust their finite state graphs; any timeout or unexpected error is a failure, never reported as exhaustive. State counts and reachability trace lengths are machine-reported per run rather than unbounded verification claims.
