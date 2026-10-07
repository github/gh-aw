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

The universe contains two externally admitted root Works. Each has a provisioned worker profile; equal priority, dependency readiness, capacity, run binding and resource authorization are simplified assumptions. `Dispatch` projects the first compatible worker assignment from one protected prefix request, not the complete native dispatch set. Homogeneous batching is permitted only when `Batch` is true; otherwise the assignment is a singleton. Different profiles cannot share an assignment. Actual mixed-profile probes produce two separate dispatches; leaving the other modeled Work queued is projection, not a claim about scheduler selection. This two-Work projection does not exercise all three dispatch slots, profile trust-domain/share-key restrictions, assignment byte bounds or queue fairness/recovery algorithms.

`birth` records original membership and `original` must remain equal to it. Implicit Claim selection is allowed only for an original singleton, regardless of how many members later finish or cancel. `Stage` projects one output family per Claim: miner PR, refiner issue or monster remediation assignment. Its count is **not** the aggregate of every output type. Installed Work contracts are external, so modeled `OutputMin`/`OutputMax` are scenario assumptions, not contracts inferred from the workflow prompts. A separate no-write scenario models explicit no-write Work.

`Complete` does not produce a Result or readback evidence. `Effect` requires Completion and an active Claim. `Readback` abstracts successful independent verification of the **whole** immutable contract, including additional output families, memory, resource receipts and queue controls when required. Unavailable readback can remain unverified forever; job success or an agent assertion cannot replace it. `Result` requires that evidence and the modeled family's cardinality. Cancellation does not undo already applied writes, but blocks later writes and settlement in this abstraction.

`Admit` represents an external authorized producer. Publishing an issue is not admission. `NoAutomaticDAG` tracks the explicit admitted set; its negative control injects unauthorized admission during an effect. The supplied workflows do not install an automatic miner → refiner → monster DAG. This absence is a deployment assumption, not a claim that externally provisioned policies cannot admit graphs or that workers can never submit authorized continuations.

`Precheck` is run-wide for monster, whereas output scope is per Claim. ESLint exit zero marks `lintClean`, including warning-only diagnostics. Installation, build and tool failures do not mark it. `Quality` intentionally does not gate authority or Result: build/test/low-false-positive requirements and the monster prompt's three-total-assignments limit are agent obligations rather than runtime properties established by this model.

## Checked properties

The safety configurations check types, immutable original membership, homogeneous batches, installed policy, unambiguous original Claim selection, Completion before writes, independent readback before Result, per-Claim result cardinality, warning-only exit-zero behavior, explicit admission and assignment size. With only two Works, `BoundedAssignment <= 3` is a finite-universe bound, not validation of the full three-slot dispatcher budget. `Spec` permits stuttering; deadlock checking is disabled for terminal or stalled workers. No fairness assumption or temporal progress property is checked.

Each negative configuration changes exactly one abstraction rule and requires the **named** invariant violation, not merely a nonzero TLC exit:

| Mutation                                                           | Expected diagnostic        |
| ------------------------------------------------------------------ | -------------------------- |
| Permit an implicit selector for an original multi-Claim assignment | `ScopedIntents`            |
| Shrink original membership after cancellation                      | `ImmutableMembership`      |
| Mix worker profiles in a batch                                     | `HomogeneousBatch`         |
| Admit/dispatch without installed policy                            | `PolicyRequired`           |
| Write before Completion                                            | `EffectRequiresCompletion` |
| Settle without independent readback                                | `ResultRequiresReadback`   |
| Verify an under-minimum output count                               | `ResultCardinality`        |
| Treat warning-only lint as non-clean                               | `WarningExitZero`          |
| Admit another Work as an issue/effect side effect                  | `NoAutomaticDAG`           |

Witness configurations keep `Fault = "none"` and add `NeverWitness` alongside all safety invariants. Its deliberate failure demonstrates reachable warning-only clean scans, Completion without Result, two Claims with three outputs each, a Result despite failed quality obligations, tool failure without a clean flag, independently verified Result and explicit no-write Result. These are bounded existential witnesses, not liveness proofs or evidence that production agents actually choose those behaviors.

## Actual code comparison

The checker parses workflow frontmatter using the installed YAML parser, finds the TypeScript plugin object through its syntax tree, builds the **actual** library entrypoint with its unchanged package scripts/tsconfig and compares its exports with the actual executed CJS config. It does not treat matching strings as a refinement proof.

Build and scan fixtures reuse existing source/dependencies via symlinks as build inputs and emit compiled files only in the artifact directory. The checker runs the actual `npm run lint:setup-js` and `lint:setup-js:changed` scripts against inert source samples. ESLint never executes the samples. A separate fixture executes the actual monster precheck shell after rebasing its agent-output paths; an npm stub supplies measured lint exit/log results or explicit installation/build/tool failure. No `npm ci`, installation, workflow/CI run, API write or network request is performed.

| Observation | Evidence and boundary |
| --- | --- |
| Assignment protocol is version 3, original payload is deeply frozen | Native `normalizeAssignment` probes; this normalization alone is not authentication |
| Singleton selection differs from original multi-Claim selection | Native scope, foreign-selector, authority-override and nested AsyncLocalStorage escape probes |
| Policy/profile installation and producer entitlement are required | Native replay/policy rejection probes; default profile has `max_claims: 1`, permitted batch has two members, mixed profiles produce separate dispatches |
| Winning Claim/native attempt/principal gate authority | `claimAuthority` probes reject unfinished, cancelled, wrong-principal and later-attempt Claims; positive completed state is a detached fixture mutation, not fabricated durable evidence |
| Count contracts are per Claim | Actual collector entrypoint accepts three issues for each of two Claims, rejects one Claim's overflow without charging its sibling, and checks a missing sibling's minimum independently. Native diagnostic delivery checks also reject invalid counts; six accepted intents do not establish six native writes |
| Completion/success cannot replace readback | Missing exact receipts/verifier remain unknown; the protected two-argument delivery facade rejects untrusted nominal success |
| CJS coverage excludes tests; generic JS/TS goals are broader | Actual resolved ESLint configs and package-script fixture scans; changed scan omits nested CJS |
| Warning-only lint exits zero | Actual library scan and precheck flag probes; `require-http-response-error-listener` is an error, other configured rules warn |
| Refiner memory is a declared git-tree adapter | Parsed script-mode adapter, canonical repository, immutable legacy base commit and `memory/eslint-refiner-runs` prefix; no remote memory verification is exercised |

`comparison.json` separates fact mismatches from documentation observations and assumptions. The README's rule-link table is compared with registered IDs as table coverage only, not a semantic audit of all rule descriptions. Prompts' final-action wording, nonduplicate issues, three total monster assignments, build/tests, quality bars and downstream remediation success are not claimed to be enforced. No installed policy/profile artifact is present in this comparison; actual deployed permissions and contracts remain outside its evidence.

The four previously omitted rule-table entries now link their existing sections: `no-async-foreach-callback`, `no-single-char-string-replace`, `no-string-fallback-for-non-string-message` and `require-http-response-error-listener`. The comparison requires linked-table coverage to match registered rules and checks that section targets exist. Tests deliberately remove these rows or break their targets; this checks reference coverage, not all rule-description semantics.

The collector probe runs its unchanged CJS entrypoint with fixture-supplied original assignment context and an artifact output root. Its legacy patch-directory existence probe is stubbed as absent without touching that directory. The probe exercises actual count partitioning, but does not authenticate a live assignment or deliver an issue.

Tests alter extracted facts, add misleading comments/body examples, rename an actual registration, and downgrade the hard HTTP rule in an executable fixture. That fixture changes the observed ESLint exit status and must fail the comparison. Trace tests also reject altered transitions and check recorded model/config/log hashes.

## Reproduce

Use existing Node dependencies in `eslint-factory/node_modules` and `actions/setup/js/node_modules`, an existing Java runtime and the official TLC jar. The driver refuses any jar except SHA-256 `936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88`. It uses at most two TLC workers, a 1 GiB Java heap, a 60-second per-case timeout (overridable up to 120 seconds) and a ten-minute total budget. JVM extracted modules, compiler outputs and all fixture files stay inside the artifact path.

```bash
export TLA2TOOLS_JAR=/absolute/path/to/existing/tla2tools.jar
export JAVA=/absolute/path/to/java
export ARTIFACTS=/absolute/path/to/session/artifacts/eslint-factory-formal-review
node specs/eslint-factory/check.mjs --artifacts="$ARTIFACTS/run"
FACTORY_ARTIFACTS="$ARTIFACTS/tests" \
FACTORY_MODEL_ARTIFACTS="$ARTIFACTS/run" \
  node --test specs/eslint-factory/compare.test.mjs
node actions/setup/js/node_modules/prettier/bin/prettier.cjs \
  --check 'specs/eslint-factory/*.mjs'
```

`--case=monster-batch` selects a single case. The driver records the current HEAD and origin/main anchor, exact command, Java/TLC/Node/dependency identities, source hashes, verdicts and state counts in `validation.json`. HEAD is an anchor, not a claim that uncommitted formalization/README changes are committed: the measured inputs are current working-tree bytes identified by hashes. Every counterexample/witness includes raw `tlc.log`, generated config, copied model, readable `trace.md` and hash/HEAD-bound `trace.json`. Trace validation checks Init, all enabled transitions/frame conditions via an independent executable replay of this finite abstraction, prior safety and the exact terminal diagnostic. That replay is not a proof about native runtime code. Changed measured sources or HEAD during validation cause failure and require rerunning.

The six safety cases exhaust their finite state graphs; any timeout or unexpected error is a failure, never reported as exhaustive. State counts and reachability trace lengths are machine-reported per run rather than unbounded verification claims.
