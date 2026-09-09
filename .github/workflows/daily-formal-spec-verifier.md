---
private: true
emoji: "🔬"
name: Daily Formal Spec Verifier
description: >
  Picks one specification from specs/ daily, formalizes its properties using
  Lean/F*/Z3/Coq/TLA+ notation (as a reasoning methodology, no runtime dependency),
  and generates a Go testify unit-test suite covering all inferred behaviors.
  Results are published as a GitHub issue.
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

network:
  allowed: [defaults, go]

imports:
  - uses: shared/daily-issue-base.md
    with:
      title-prefix: "[formal-spec] "
      expires: 7d
      labels: [automation, formal-verification, testing, specifications]
      assignees: [copilot]
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
    - "find specs -type f -name \"*.md\" | sort"
    - "cat specs/*.md"
    - "find . -name \"*_test.go\" -path \"*/pkg/*\" | head -20"
    - "cat pkg/workflow/*.go | head -200"
    - "cat pkg/cli/*.go"
    - "sed *"

safe-outputs:
  mentions: false
  allowed-github-references: []
  max-bot-mentions: 1
features:
  gh-aw-detection: true
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily Formal Spec Verifier 🔬

You are a formal methods expert and Go test engineer. Your mission is to:

1. Select one specification file from `specs/` that has not been recently processed.
2. Analyze it using formal verification reasoning (Lean 4 / F* / Z3 / Coq / TLA+ methodologies — as a notation and reasoning discipline, **not** as code to be executed).
3. Generate a complete Go testify unit-test suite from the inferred formal model.
4. Publish the formalization + test suite as a GitHub issue.

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
- Run `find specs -type f -name '*.md' | sort` to get the full list.
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

---

## Step 2 — Read and Parse the Specification

Read the selected spec file in full with `bash`.

Use built-in file inspection tools only for read-only analysis when bash output is insufficient. Do not modify repository files.

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

## Step 3 — Formalize Using Verification Language Notation

Produce a **Formal Model** section using the most appropriate notation(s) from:

| Notation | Best for |
|---|---|
| **TLA+** | State machines, distributed protocols, temporal logic |
| **Z3 / SMT-LIB** | Arithmetic constraints, solver-verified predicates |
| **Lean 4 / Coq** | Type-theoretic proofs, inductive data types, lemmas |
| **F\*** | Stateful effectful programs, pre/post contracts |

Rules:
- Use **pseudo-notation** clearly marked as illustrative (no actual compilation required).
- Annotate each predicate with the source sentence or paragraph from the spec.
- Keep the formalization compact: prefer 5–15 top-level predicates / lemmas.
- Map every formal predicate to a testable Go behavior.

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

Use existing types, functions, and interfaces from the codebase where possible (read relevant `.go` files in `pkg/` with bash). When no concrete implementation exists yet, write the test against a **minimal stub interface** at the top of the test file, clearly marked `// stub — replace with real implementation`.

---

## Step 5 — Create Issue

Create exactly one issue using the `create_issue` safe output.

### Output Contract (Required)

1. Draft the title and body locally first if needed, but emit exactly one final `create_issue` safe output only after the full payload is complete.
2. Do **not** use `bash`, `cli-proxy`, or the `safeoutputs` CLI to create the issue or inspect the tool schema. Emit the safe output directly with `title` and `body` arguments.
3. Do **not** use `python3`, `node`, `jq`, or any other scripting runtime to construct or validate the issue payload — build the body string inline and emit the safe output directly.
4. Never retry `create_issue` with empty, placeholder, or partial arguments.
5. If the quality checks below cannot be met, emit `report_incomplete` directly as a safe output instead of `create_issue`.

### Issue format

Title: `[formal-spec] <SpecFileName> — Formal model & test suite — <YYYY-MM-DD>`

Use `###` or lower for all headers in your report. Never use `#` (h1) or `##` (h2) — these are reserved for the issue title rendered by GitHub.

Wrap long sections (>5 items, detailed lists, raw data) in `<details><summary><b>Section Name</b></summary>` blocks to keep the report scannable.

Body requirements:

```markdown
### Summary

One-paragraph description of the spec and what was formalized.

### Specification

- **File**: `specs/<name>.md`
- **Focus area**: <component or protocol>
- **Formal notation used**: <TLA+ / Lean 4 / Z3 / F* / mixed>

### Formal Model

<details>
<summary><b>Predicates and invariants (illustrative notation)</b></summary>

<one predicate / lemma block per invariant>

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

1. Copy the test file to the appropriate package directory.
2. Replace any `// stub` interfaces with real implementations.
3. Run: `go test ./... -run Formal`

### Context

- Spec processed: `specs/<name>.md`
- Formal notation: <list>
- Run: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
```

### Output quality bar

Before emitting `create_issue`, verify the body:
- Contains all required sections (`Summary`, `Specification`, `Formal Model`, `Behavioral Coverage Map`, `Generated Test Suite`, `Usage`, `Context`).
- Has at least 5 rows in the Behavioral Coverage Map.
- The generated test file has no obvious syntax errors — **visual review only**: scan for mismatched braces, missing imports, and undefined identifiers. Do **not** run `go build`, `go test`, `go version`, `python3`, `node`, `jq`, or any compiler or runtime to verify.
- Is at least 1200 characters long.

If these checks cannot be met, emit `report_incomplete` directly as a safe output instead of `create_issue`.

---

## Step 6 — Persist Rotation State

After `create_issue` succeeds, write updated `rotation.json` to
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
**Notation**: <TLA+ / Lean 4 / Z3 / F* / mixed>
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

---

## Constraints

- **No runtime dependency on Lean, F*, Z3, Coq, or TLA+.** All formal notation is illustrative only — embedded as quoted blocks in the issue body.
- **No network calls** beyond what the tools provide.
- **Keep the prompt-body loop tight**: read the spec once, formalize once, write the test file once.
- Use `noop` with a brief explanation if no spec can be selected (e.g. all were processed today).
- **Before finishing, confirm you called exactly one terminal safe output:** `create_issue`, `report_incomplete`, or `noop`.
- **Tool budget awareness**: If you are approaching the tool call limit, emit a partial result immediately rather than continuing to gather more data. Use this priority order: (1) if the formalization and test suite sections are both substantially complete (at least 5 predicates and 3 test functions), emit a partial `create_issue` with what is available; (2) otherwise, emit `report_incomplete` immediately.