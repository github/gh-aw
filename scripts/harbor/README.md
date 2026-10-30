# Use gh-aw graders as Harbor verifiers

This bridge runs explicitly selected gh-aw graders over a redacted unified
session and publishes Harbor rewards. It does not execute workflows or convert
ATIF back into a session. The included task calibrates a verifier against
synthetic recorded evidence, not a fresh agent or a real GitHub write.

## Run locally

Build `gh-aw` with `make build`. Python 3.9+ and Node.js 20+ with permission
support are required; Bash evaluators may also need their declared tools.
From the repository root:

```bash
python3 scripts/harbor/verify.py \
  --gh-aw "$PWD/gh-aw" \
  --config scripts/harbor/task/tests/mapping.json \
  --session scripts/harbor/task/environment/aw_session.jsonl \
  --output-dir /tmp/gh-aw-harbor-verifier
```

The output directory is verifier-owned. Each invocation removes only
`reward.json`, `reward.txt`, and `gh-aw-graders.json` from that directory.
Use a distinct directory per concurrent trial.

`reward.json` contains `{"reward": 1}` for the reference fixture.
`gh-aw-graders.json` retains resolved definitions, original results,
implementation identity, input/config/workflow/binary hashes, and coverage.
Input is capped at 50 MiB, each stdout/stderr stream at 1 MiB, and each
grader process group at 130 seconds. Only `PATH` is inherited by the CLI;
its established runtime further restricts grader workers.

## Select rewards explicitly

`mapping.json` is trusted configuration, not an agent artifact. Version 1 accepts
only `version`, `workflow`, and `metrics`. The workflow is resolved relative to
the mapping file and must stay within its directory. Each metric requires:

| Field | Meaning |
|---|---|
| `grader` | Enabled grader ID from the trusted workflow |
| `reward` | Unique Harbor reward key, using lowercase kebab-case |
| `mapping` | `threshold` for binary acceptance, or `raw` for the numeric measurement |
| `required` | Whether absent evidence makes the entire evaluation unscorable |

Select at least one required metric. Duplicate grader/reward IDs, unknown fields,
unsupported versions, and missing definitions are errors. No defaults are
automatically exposed as rewards.

Threshold mapping requires finite `threshold`, a supported `direction`, and a
matching boolean `passed` from the resolved grader definition/result. A native
`status: "pass"` with `passed: null` means computation succeeded, **not** task
success. Use `raw` for such measurements or configure an acceptance threshold.
Raw values retain their units; there is no implicit normalization or aggregation.
Extra operational-value Bash metrics remain diagnostics.

| Outcome | Exit | Publication |
|---|---|---|
| Valid measurement, including threshold failure | 0 | Numeric rewards; a measured failure maps to `0` |
| Required unavailable evidence | 2 | Diagnostic status `unavailable`; no reward file |
| Optional unavailable evidence | 0 | Reward key omitted, coverage false; required rewards still published |
| Invalid configuration/input, timeout, or grader error | 1 | Diagnostic status `error`; no reward file |

Rewards are written atomically, after all required metrics are valid. A failed
optional evaluator is still an error, not missing coverage. Successful partial
results are retained only as diagnostics when another required metric is absent.

## Package a separate Harbor verifier

The reference task uses Harbor **0.25.0**, task schema 1.4, and its default
verifier. That verifier parses reward files rather than relying on `test.sh`'s
exit code. With no reward file it raises `RewardFileNotFoundError`, classified
as `verifier_failed`, `reward_valid = false`, and no retry. Thus an unavailable
measurement is not a valid zero reward. Inspect `gh-aw-graders.json` to
distinguish missing evidence from evaluator failure; report their counts
separately from task failures.

Prepare the verifier image from the repository root:

```bash
make build-linux
docker build -f scripts/harbor/Dockerfile \
  --build-arg BINARY=gh-aw-linux-amd64 \
  -t gh-aw-harbor-verifier:local .
```

Use the arm64 binary and matching Docker platform on arm64 hosts. Before a
benchmark, push the built image and replace the local tag in
`task/task.toml` with its immutable registry digest. Record the repository
revision and verifier image digest with results: the image freezes imported
workflow definitions, evaluator code, package versions, and transitive resources.
The diagnostic hashes supplement, not replace, that image identity.

Install the optional host-side Harbor dependency in an isolated Python 3.12+
environment, then run the calibration:

```bash
python -m pip install -r scripts/harbor/requirements-test.txt
harbor run -p scripts/harbor/task -a nop
```

The task explicitly transfers `/app/evidence/aw_session.jsonl` into a separate,
network-disabled verifier. The verifier image bundles `/tests/test.sh`, the
mapping, workflow, and bridge. Do not mount evidence over `/tests`, the binary,
or other grading code. Trusted definitions must not come from the agent's
checkout. Keep production credentials and oracle material out of the agent
image and verifier environment.

For a real recorded run, replace the synthetic fixture with redacted evidence
collected through a trusted pipeline and freeze its grading contract.
Agent-written events claiming success are not authenticated proof of delivery.
Staged requests cannot satisfy an executed-effect criterion. The sample grader
checks target-bound recorded comment results, not comment quality. Its
`expectNoop` configuration supports an explicit skipped/no-URL criterion; it
does not treat missing events as a successful no-op.

Built-in and JavaScript graders still compute only over the supplied session.
Filesystem assertions belong in trusted verifier code. Existing operational-value
Bash remains trusted code, not a filesystem/network sandbox. Reward publication
is the bridge's job; never add file writes to pure grader scripts or custom
steps to conclusion.

ATIF export is independent: retain `aw_session.jsonl` for grading and use ATIF
for supported trajectory viewing. Neither export nor this bridge implies a
fresh gh-aw runtime inside Harbor.

## Check changes

```bash
make build
python3 scripts/harbor/test_verify.py
go test ./pkg/cli -run 'TestGraderDefinitionEnvelopeContract|TestRunGraderFromUnifiedSession' -count=1
python scripts/harbor/test_harbor.py
```

The first Python suite has no dependencies and uses the built binary for the
reference evidence matrix, including deterministic repeats. The last suite
requires the pinned Harbor dependency: it runs Harbor's real verifier and error
policy against a filesystem/exec stand-in, without Docker or inference. It is
not a container-isolation test. Run the calibration task above to exercise
container startup, explicit artifact handoff, and offline verification.
