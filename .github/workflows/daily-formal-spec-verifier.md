---
private: true
emoji: "🔬"
name: Daily Formal Spec Verifier
description: >
  Picks one specification from specs/ daily, incrementally adopts executable
  TLA+/TLC or Koka models where appropriate, and generates a Go testify unit-test
  suite covering the inferred behaviors. Other formal notation stays illustrative.
  Results are delivered as a validated test-suite pull request.
intent: Keep specification-derived implementation tests grounded in reproducible evidence while incrementally adopting executable models where they add value.
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
  copilot-requests: write
tracker-id: daily-formal-spec-verifier
engine:
  id: copilot
  copilot-sdk: true
max-tool-denials: 3
strict: true
timeout-minutes: 25
runs-on: ubuntu-24.04

network:
  allowed: [defaults, go]

imports:
  - shared/tla-plus.md
  - shared/koka.md
  - shared/lock-safe-pull-request.md
  - shared/activation-app.md
  - shared/reporting.md
  - shared/otlp.md

sandbox:
  agent:
    id: awf
tools:
  cli-proxy: true
  github:
    mode: gh-proxy
    toolsets: [default, issues, pull_requests]
  cache-memory: true
  repo-memory:
    branch-name: memory/formal-spec-verifier
    file-glob: ["*.md", "*.json"]
    max-file-size: 65536
  edit: null
  bash:
    - "gofmt -w:*"
    - "go test:*"
    - "find specs -type f -name \"*.md\" | sort"
    - "cat /tmp/gh-aw/cache-memory/formal-spec-verifier/rotation.json"
    - "cat specs/*.md"
    - "find . -name \"*_test.go\" -path \"*/pkg/*\" | head -20"
    - "cat pkg/workflow/*.go | head -200"
    - "cat pkg/cli/*.go"
    - "find specs -type f -name \"*.tla\""
    - "find specs -type f -name \"*.kk\""
    - "sha256sum *"
    - "node .github/scripts/formal_spec_check.cjs:*"
    - "cat /tmp/gh-aw/agent/formal-spec-verifier/*"

steps:
    - name: Validate fixture-based TLA+ installer
      run: bash .github/scripts/install_tla_runtime_test.sh

post-steps:
  - name: Upload formal verification evidence
    if: always()
    uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2
    with:
      name: formal-spec-verifier-${{ github.run_id }}-${{ github.run_attempt }}
      path: /tmp/gh-aw/agent/formal-spec-verifier/
      retention-days: 14
      if-no-files-found: ignore

safe-outputs:
  create-pull-request:
    exclude-lock-yml: true
    title-prefix: "[formal-spec] "
    expires: 7d
    labels: [automation, formal-verification, testing, specifications]
    max: 1
  mentions: false
  allowed-github-references: []
  max-bot-mentions: 1
features:
  gh-aw-detection: true
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily Formal Spec Verifier 🔬

Use only commands listed under `tools.bash`, one standalone command per call. Pipelines are permitted only when the complete pipeline is explicitly allowlisted. Do not append commands, redirections, or subshells to an allowed command. If a required operation is blocked, stop and report it instead of probing command variants.

On the first tool denial, stop that operation and use a known permitted alternative:
native file inspection for source and notes, or a standalone command below. Do not
retry `go version`, `git log`, Python, or another ungranted discovery command.
A workflow tool denial is not a GitHub authentication error; do not request new
credentials or broader permissions. If a required capability has no permitted
alternative, call `report_incomplete` before exhausting the three-denial budget.

| Operation | Permitted standalone invocation |
|---|---|
| Read source or prior notes | Built-in file inspection tools, using the full path |
| Check existing work | `gh pr list --state open --search "formal-spec" --json number,title` |
| Format the generated test | `gofmt -w pkg/workflow/example_formal_test.go` (use the actual test path) |
| Run focused tests | `go test ./pkg/workflow -run '^TestSelectedSpec' -count=1` (use the actual package and selector) |
| Run a bounded formal check | `node .github/scripts/formal_spec_check.cjs sany specs/example.md Model.tla` (see the model contract below) |

You are a formal methods expert and Go test engineer. Your mission is to:

1. Select one specification file from `specs/` that has not been recently processed.
2. Choose the most appropriate method for its properties: optionally execute TLA+/TLC or Koka models, or keep formal notation illustrative when executable modeling adds no value.
3. Generate a complete Go testify unit-test suite from the inferred formal model.
4. Add and validate the test suite in the relevant package and publish a pull request with the formalization and any executable-model evidence.

Follow the imported formal-methods guidance. Both pinned toolchains are prepared
before the agent starts; their availability does not require using both on every
spec. Keep the Go test suite mandatory, and migrate models one selected spec at
a time rather than converting the specification corpus.

---

## Step 1 — Rotation (cache-memory)

Load `/tmp/gh-aw/cache-memory/formal-spec-verifier/rotation.json`.

Schema:

```json
{
  "last_index": 0,
  "processed": ["specs/foo.md"],
  "last_run": "2026-01-01-12-00-00"
}
```

- On first run (file missing): initialize `{ "last_index": 0, "processed": [], "last_run": "" }`.
- Run exactly `find specs -type f -name "*.md" | sort` to get the full list. Run it separately from the cache read; do not add `head`, `wc`, `cd`, or other shell commands to it.
- Select the spec at `(last_index + 1) % len(specs)` that is **not** in `processed` from the last 14 days.
- If all specs were processed within the last 14 days, reset `processed` to `[]` and start from `last_index 0`.

---

## Step 1b — Load Prior Notes (repo-memory)

Before analyzing the selected spec, check for existing notes from previous runs:

- Notes directory: `/tmp/gh-aw/repo-memory/default/formal-spec-verifier/`
- Notes index: `/tmp/gh-aw/repo-memory/default/formal-spec-verifier/notes-index.json`
- Per-spec note files: `/tmp/gh-aw/repo-memory/default/formal-spec-verifier/<spec-slug>.md`

`notes-index.json` schema:

```json
{
  "specs": {
    "specs/foo.md": {
      "slug": "foo",
      "last_formalized": "2026-01-01-12-00-00",
      "notation": "TLA+",
      "predicate_count": 8,
      "issue_number": 1234
    }
  }
}
```

If a note file exists for the selected spec, read it and use the prior predicate list as a starting point, extending or refining it rather than starting from scratch. If no prior notes exist, proceed fresh.
Use built-in file inspection tools for note files; do not combine note and cache reads with shell commands.

Reuse the note's model paths and verification mode when present, but rerun checks
against current sources. Legacy notes without verification metadata remain valid
notation-only entries; missing metadata is not evidence of a prior model pass.

---

## Step 2 — Read and Parse the Specification

Read the selected spec file in full with built-in file inspection tools (or an allowed standalone `cat specs/*.md` command for a top-level spec). Do not combine reads or pipe them through `head`.

Use built-in file inspection tools for analysis when bash output is insufficient.
Modify only the selected specification's focused test suite, directly required
test helpers, and its executable model/configuration when justified.

Extract:

- **Purpose**: the system or component being specified.
- **Invariants**: properties that must always hold.
- **Preconditions**: conditions that must hold before an operation runs.
- **Postconditions**: guarantees the operation provides after execution.
- **State machine transitions** (if any): states, events, guards, effects.
- **Safety properties**: "bad things never happen" assertions.
- **Liveness properties**: "good things eventually happen" obligations.
- **Error / edge cases** mentioned explicitly or implied.

---

## Step 3 — Choose the Appropriate Formal Method

Produce a **Formal Model** section using the most appropriate notation(s) from:

| Notation | Best for |
|---|---|
| **TLA+ / TLC** | State machines, distributed protocols, temporal logic, bounded state-space exploration |
| **Koka** | Typed functional models, explicit effects, deterministic handlers, tested transition behavior |
| **Z3 / SMT-LIB** | Arithmetic constraints, solver-verified predicates |
| **Lean 4 / Coq** | Type-theoretic proofs, inductive data types, lemmas |
| **F\*** | Stateful effectful programs, pre/post contracts |

Rules:
- Keep Lean, Coq, F*, and Z3/SMT-LIB **pseudo-notation** clearly marked as illustrative; no runtime is installed for them.
- Prefer an existing runnable model for the selected spec. Use TLA+/TLC for temporal or concurrency properties and Koka for type/effect boundaries or handler behavior. Use both only for distinct complementary properties.
- Staying notation-only is valid when executable modeling is disproportionate or adds no useful coverage; record the reason. Do not convert existing TLA+ temporal checks to Koka or claim Koka exhausts executions.
- Annotate each predicate with the source sentence or paragraph from the spec.
- Keep the formalization compact: prefer 5–15 top-level predicates / lemmas.
- Map every formal predicate to a testable Go behavior.

### Optional executable models and gradual adoption

Inspect the selected spec's existing `.tla`/`.cfg` or `.kk` files and documented
verification wrapper. Preserve tool compatibility, expected negative controls,
and finite bounds. This workflow permits only the bounded formal-check helper
below, not arbitrary repository wrappers or direct tool commands. If an existing
model cannot use this contract without changing its semantics, keep it unchanged
and report the incompatibility. Never run the whole corpus or download another tool.

If no suitable model exists and modeling adds value, add a small model under
`specs/formal/<spec-slug>/`, where the slug derives from the complete selected
spec path relative to `specs/` (replace `/` with `-` and remove `.md`). Commit
the `.tla`/`.cfg` or `.kk` sources with the Go tests. Do not add markdown files
there that would enter the spec rotation, port production code to Koka, or
introduce a Koka dependency into the Go application's build.

Use `node .github/scripts/formal_spec_check.cjs` with one operation per call:

```text
node .github/scripts/formal_spec_check.cjs sany specs/<selected-spec>.md Model.tla
node .github/scripts/formal_spec_check.cjs tlc specs/<selected-spec>.md Model.tla Model.cfg
node .github/scripts/formal_spec_check.cjs koka specs/<selected-spec>.md main.kk
```

Replace the specification path and model basenames with actual files. The helper
derives `<spec-slug>` from the complete selected spec path, accepts models only
under `specs/formal/<spec-slug>/`, and rejects path traversal, symlinks, unknown
operations, and extra arguments. Do not edit the helper. Models needing different
paths or unsupported flags must be reported as incompatible, not run through an
ungranted `cd`, `timeout`, interpreter, or wrapper.

The helper uses only the pinned tool paths from shared setup. It runs from the
model directory, creates fresh evidence/build directories under
`/tmp/gh-aw/agent/formal-spec-verifier/<spec-slug>/`, and captures tool versions,
source hashes, exact arguments, statuses, and complete stdout/stderr. It enforces
`timeout --kill-after=30s`, at most 2 minutes per compilation or TLC invocation,
30 seconds per Koka executable, and a shared **8-minute total** formal-check
budget. TLC uses 2 workers and a 2 GiB Java heap. Leave at least 10 minutes for Go
tests and delivery; narrow the model before starting if these limits cannot fit.

For TLA+, run SANY and the documented TLC configuration, retaining exhaustion
status, checked invariants/temporal properties, state counts, and counterexamples.
For Koka, compile from the model's directory with the build directory and output
executable under the transient results path, then execute its assertions and
deterministic handlers. Include an intentional negative control for new models
and check its expected diagnostic/verdict, not just a nonzero exit code. Confirm
the sandbox has the native C compiler and headers needed by Koka's backend.
Simulate external effects with deterministic handlers; do not use live GitHub
mutations, services, or credentials as model fixtures.

Record source/config hashes, tool versions, exact commands, exit statuses,
bounds, verdicts, and complete sanitized logs in that results directory. Link
the `formal-spec-verifier-<run_id>-<run_attempt>` artifact and summarize evidence
in the PR's Formal Model and Usage sections. Distinguish expected negative
controls from genuine violations, timeouts, parse errors, and tooling failures.
Once an executable check is attempted, an unexpected failure is
`report_incomplete`, never a silent fallback to a passing notation-only result.
An exhausted finite TLC model is not an unbounded proof; Koka compilation and
passing assertions do not prove temporal properties or verify the Go code.

---

## Step 4 — Generate Go Testify Unit Tests

Produce a complete Go test file that:

- Is named `<derived_package>_formal_test.go` (e.g. `workflow_formal_test.go`).
- Uses package `<package>_test`.
- Imports `github.com/stretchr/testify/assert` and `github.com/stretchr/testify/require`.
- Has one `Test*` function per formal predicate / invariant (table-driven when multiple cases share the same predicate).
- Covers **all** the safety, invariant, precondition, and postcondition predicates identified in Step 3.
- Covers at least **three** edge / error cases identified in Step 2.
- Includes a leading comment block that references the specification file and lists the formal predicates the tests encode.
- All assertions include a descriptive message string.

Use existing types, functions, and interfaces from the codebase (read relevant `.go` files in `pkg/`). Do not deliver placeholder stub interfaces or tests that merely test their own fake implementation. If no concrete implementation exists, report that blocker with `report_incomplete`.

---

## Step 5 — Validate and Create a Pull Request

Write the complete test file into its package, run `gofmt` and focused `go test` selectors,
and commit locally. Create exactly one pull request using `create_pull_request` with
`title`, `body`, and the committed local `branch`.

### Output Contract (Required)

1. Draft the title and body locally first if needed, but emit exactly one final `create_pull_request` only after implementation and validation are complete.
2. Use the configured safe-output transport. With `cli-proxy`, invoke `safeoutputs create_pull_request '<JSON>'` with a complete JSON object containing `title`, `body`, and `branch`. Inside JSON strings, encode newlines as `\n`, double quotes as `\"`, backslashes as `\\`, and apostrophes as `\u0027`; Unicode can remain literal. If a direct safe-output tool is exposed instead, call it with the same arguments.
3. Do **not** use `python3`, `node`, `jq`, or any other scripting runtime to construct or validate the pull request payload — build the body string inline. Do not redirect it to a file.
4. Never retry `create_pull_request` with empty, placeholder, or partial arguments.
5. If the quality checks below cannot be met, emit `report_incomplete` through the same transport. With `cli-proxy`, use `safeoutputs report_incomplete '{"reason":"<concrete blocker>"}'`; never substitute a prose-only failure report.

### Pull request format

Title: `[formal-spec] <SpecFileName> — Formal model & test suite — <YYYY-MM-DD>`

Use `###` or lower for all headers in your report. Never use `#` (h1) or `##` (h2) — these are reserved for the pull request title rendered by GitHub.

Wrap long sections (>5 items, detailed lists, raw data) in `<details><summary><b>Section Name</b></summary>` blocks to keep the report scannable.

Body requirements:

```markdown
### Summary

One-paragraph description of the spec and what was formalized.

### Specification

- **File**: `specs/<name>.md`
- **Focus area**: <component or protocol>
- **Formal notation used**: <TLA+ / Koka / Lean 4 / Z3 / F* / mixed>

### Formal Model

<details>
<summary><b>Predicates, invariants, and verification evidence</b></summary>

<one predicate / lemma block per invariant; distinguish illustrative notation,
TLC model checking, and Koka type/effect checking plus behavioral assertions>

<selected method and rationale, committed model paths when applicable,
observed bounded verdicts and artifact reference, or notation-only reason>

</details>

### Behavioral Coverage Map

| Predicate / Invariant | Test Function | Description |
|---|---|---|
| `<PredicateName>` | `Test<Name>` | <what it checks> |
...

### Generated Test Suite

<details>
<summary><b>📄 `<package>/<file>_formal_test.go`</b></summary>

```go
<full test file content>
```

</details>

### Usage

1. Identify the committed test file in its package directory.
2. Describe the real implementation APIs exercised.
3. Include the exact focused `go test` command and observed result.

### Context

- Spec processed: `specs/<name>.md`
- Formal notation: <list>
- Run: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
```

### Output quality bar

Before emitting `create_pull_request`, verify the body and committed tests:
- Contains all required sections (`Summary`, `Specification`, `Formal Model`, `Behavioral Coverage Map`, `Generated Test Suite`, `Usage`, `Context`).
- Has at least 5 rows in the Behavioral Coverage Map.
- The generated test file is formatted and its focused Go tests passed against real implementation APIs.
- Is at least 1200 characters long.

If these checks cannot be met, emit `report_incomplete` instead of an unvalidated pull request.

---

## Step 6 — Persist Rotation State

After `create_pull_request` is staged successfully, write updated `rotation.json` to
`/tmp/gh-aw/cache-memory/formal-spec-verifier/rotation.json`:

- Increment `last_index`.
- Append the selected spec to `processed`.
- Set `last_run` to the current timestamp in `YYYY-MM-DD-HH-MM-SS` format (no colons, no `T`, no `Z`).

Use the `write` tool (not shell redirection) to persist the file.

---

## Step 7 — Persist Formal Notes (repo-memory)

After writing the rotation state, save the formal model notes for this run so future runs
can build on them.

### Per-spec note file

Write `/tmp/gh-aw/repo-memory/default/formal-spec-verifier/<spec-slug>.md`:

```markdown
# Formal Notes: <SpecFileName>

**Last formalized**: <YYYY-MM-DD-HH-MM-SS>
**Notation**: <TLA+ / Koka / Lean 4 / Z3 / F* / mixed>
**Verification mode**: <notation-only / tlc / koka / tlc+koka>
**Model paths**: <committed source/config paths, or none>
**Checked source hashes**: <hashes from this run, or not applicable>
**Verdict and evidence**: <bounded verdicts and artifact reference, or not attempted>
**Issue**: #<number>

## Predicates

| ID | Predicate | Description |
|---|---|---|
| P1 | `<name>` | <what it asserts> |
...

## Key Invariants

<bullet list of invariants>

## Edge Cases Identified

<bullet list of edge cases>

## Notes for Future Runs

<any observations about gaps, areas that need deeper formalization, or cross-spec dependencies>
```

### Update notes index

Read `/tmp/gh-aw/repo-memory/default/formal-spec-verifier/notes-index.json` (or initialize
`{"specs": {}}` if absent), add or update the entry for the selected spec, and write the
file back.
Preserve existing fields and add `verification_mode`, `model_paths`,
`source_hashes`, and `last_verdict` for this run. This lets later rotations
incrementally reuse validated models without treating old notes as current proof.

---

## Constraints

- **Lean, F*, Z3, and Coq remain notation-only.** TLA+/TLC and Koka are optional executable checks using the preinstalled pinned tools; never install or upgrade tools from the agent.
- **No network calls** beyond what the tools provide.
- **Keep the prompt-body loop tight**: read the spec once, formalize once, write the test file once.
- Use `noop` with a brief explanation if no spec can be selected (e.g. all were processed today).
- **Before finishing, confirm you called exactly one terminal safe output:** `create_pull_request`, `report_incomplete`, or `noop`.
- **Tool budget awareness**: Reserve time for focused validation and delivery. If the quality bar or validation cannot be completed, emit `report_incomplete`; never publish a partial or unvalidated test suite.