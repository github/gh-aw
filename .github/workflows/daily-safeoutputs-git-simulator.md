---
private: true
emoji: "🧪"
description: Daily synthetic tester that exhaustively explores git configurations associated with create-pull-request and push-to-pull-request-branch safe outputs using local repositories, repo-memory persistence, and parallel inline sub-agents.
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
  issues: read
  pull-requests: read

sandbox:
  agent:
    id: awf
engine:
  id: claude
  model-provider: github
  model-routing:
    goal: cost
    mode: economy
strict: true
runtimes:
  python:
    version: "3.11"
tools:
  cli-proxy: true
  bash: true
  github:
    mode: gh-proxy
    toolsets: [default, repos, pull_requests]
  repo-memory:
    branch-name: memory/git-simulator
    file-glob: ["git-sim-state.json", "git-sim-strategies.md"]
    allowed-extensions: [".json", ".md"]
    max-file-size: 204800
    validation:
      script: |
        if (memoryPhase === "push" && memorySourceJobResult !== "success") {
          throw new Error("Simulator memory publication requires successful final evidence preparation");
        }
        return true;
pre-agent-steps:
  - name: Isolate simulator memory and read-only ledger signals
    uses: actions/github-script@v9
    env:
      GH_TOKEN: ${{ github.token }}
    with:
      script: |
        const fs = require("node:fs");
        const path = require("node:path");
        const { DatabaseSync } = require("node:sqlite");
        const actionsDir = path.join(process.env.RUNNER_TEMP, "gh-aw", "actions");
        const { main } = require(path.join(actionsDir, "create_ledger_projection.cjs"));
        const { isolateMemory } = require(path.join(process.env.GITHUB_WORKSPACE, "actions/setup/js/git_simulator_memory_helpers.test.cjs"));
        const ledgerName = "git-simulator-lessons";
        const ledgerWorkspace = path.join(process.env.RUNNER_TEMP, "git-simulator-private-ledger");
        fs.mkdirSync(ledgerWorkspace, { recursive: true });
        await exec.exec("git", ["init", "--quiet", ledgerWorkspace]);
        const [databasePath] = await main({
          githubClient: github, owner: context.repo.owner, repo: context.repo.repo,
          workspaceDir: ledgerWorkspace,
          ledgerConfigs: [{
            name: ledgerName, type: "notes",
            max_record_kb: 4, max_segment_kb: 100, max_patch_kb: 16
          }]
        });
        const database = new DatabaseSync(databasePath, { readOnly: true });
        let notes;
        try {
          notes = database.prepare(`
            SELECT n.id, n.subject, n.note, s.downvotes
            FROM notes n JOIN note_state s ON n.id = s.note_id
            ORDER BY n.id LIMIT 20
          `).all().map(row => ({
            ...row,
            citations: database.prepare(`
              SELECT path FROM note_citations WHERE note_id = ? ORDER BY ordinal LIMIT 4
            `).all(row.id).map(citation => citation.path)
          }));
        } finally {
          database.close();
        }
        const memoryDir = "/tmp/gh-aw/repo-memory/default";
        isolateMemory({
          memoryDir,
          workspace: process.env.GITHUB_WORKSPACE, notes,
          outputPath: "/tmp/gh-aw/agent/git-simulator-signals.json",
          privateGitDir: path.join(process.env.RUNNER_TEMP, "git-simulator-private-memory-git"),
          privateHistoryPath: path.join(process.env.RUNNER_TEMP, "git-simulator-private-producer-history.json")
        });
        // The reader does not declare tools.ledger, so no ledger write or compaction handler exists.
        const projectionRoot = path.dirname(path.dirname(databasePath));
        fs.chmodSync(projectionRoot, 0o700);
        fs.chmodSync(path.dirname(databasePath), 0o700);
        fs.rmSync(projectionRoot, { recursive: true });
        fs.rmSync(ledgerWorkspace, { recursive: true });
post-steps:
  - name: Restore producer evidence and validate final memory
    id: restore_simulator_evidence
    if: ${{ always() && steps.redact_secrets.outcome == 'success' && steps.validate_recovery_envelopes.outcome == 'success' }}
    uses: actions/github-script@v9
    with:
      script: |
        const path = require("node:path");
        const actionsDir = path.join(process.env.RUNNER_TEMP, "gh-aw", "actions");
        const { setupGlobals } = require(path.join(actionsDir, "setup_globals.cjs"));
        setupGlobals(core, github, context, exec, io, getOctokit);
        const { restoreProducerHistory, validateProducerUpload } = require(path.join(process.env.GITHUB_WORKSPACE, "actions/setup/js/git_simulator_memory_helpers.test.cjs"));
        const memoryDir = "/tmp/gh-aw/repo-memory/default";
        restoreProducerHistory({
          memoryDir, workspace: process.env.GITHUB_WORKSPACE,
          privateGitDir: path.join(process.env.RUNNER_TEMP, "git-simulator-private-memory-git"),
          privateHistoryPath: path.join(process.env.RUNNER_TEMP, "git-simulator-private-producer-history.json")
        });
        const { processFile, extractMCPGatewayTokens, MCP_GATEWAY_CONFIG_PATHS } = require(path.join(actionsDir, "redact_secrets.cjs"));
        const { getOTLPSecretValues } = require(path.join(actionsDir, "otlp_header_helpers.cjs"));
        const secrets = extractMCPGatewayTokens(MCP_GATEWAY_CONFIG_PATHS);
        for (const name of ["git-sim-state.json", "git-sim-strategies.md"]) {
          processFile(path.join(memoryDir, name), secrets, getOTLPSecretValues());
        }
        validateProducerUpload({ memoryDir, core });
        const { main } = require(path.join(actionsDir, "validate_recovery_envelopes.cjs"));
        main({ roots: [memoryDir], core });
  - name: Replace staged memory with reconstructed producer evidence
    if: ${{ always() && steps.restore_simulator_evidence.outcome == 'success' }}
    uses: actions/upload-artifact@v7
    with:
      name: repo-memory-default
      path: /tmp/gh-aw/repo-memory/default
      overwrite: true
      retention-days: 1
      if-no-files-found: error
safe-outputs:
  create-issue:
    title-prefix: "[git-sim] "
    labels: ["git-simulator", "safe-outputs", "automated"]
    max: 4
    close-older-issues: false
    deduplicate-by-title: 3
  steer: true
checkout:
  fetch: ["*"]
  fetch-depth: 0
timeout-minutes: 45
models:
  default-ai-credits-pricing:
    input: 5.0
    output: 25.0
evals:
  - id: git_scenarios_simulated
    question: Did the agent simulate git configurations associated with create-pull-request and push-to-pull-request-branch using local repositories only?
  - id: simulator_findings_reported
    question: Did the agent report the simulator results and any systematic git-state issues it found?
features:
  gh-aw-detection: true
---

# Daily Safe Outputs Git Simulator

You are the **Safe Outputs Git Simulator** — a systematic exhaustive tester for git operations in the agentic workflow framework. Explore the full configuration space using local-only Git repositories and report measurable git-state behavior associated with `create-pull-request` and `push-to-pull-request-branch`. Never invoke either safe output: they create externally visible GitHub resources and are not probes.

## Current Context

- **Repository**: ${{ github.repository }}
- **Run Date**: $(date +%Y-%m-%d)
- **Run ID**: ${{ github.run_id }}

## Configuration Space (Z3-style)

The exploration space is defined by these orthogonal dimensions:

```
SIZE    := [tiny, small, medium, large, huge]
           → stuff.md entries: [0, 10, 100, 1000, 5000]
HISTORY := [none, shallow, medium, deep]
           → history.md entries: [0, 5, 50, 500]
FILES   := [single, few, many, batch]
           → patch file count: [1, 5, 20, 100]
PATCH   := [micro, small, medium, large, xlarge]
           → total patch size: [1, 50, 200, 1000, 4000] KB
BRANCH  := [clean, ahead, diverged]
           → branch state relative to base
COMMIT  := [single, multi, merge_msg]
           → commit structure: 1 commit, 3 commits, merge-style commit message
```

Total cells: 5 × 4 × 4 × 5 × 3 × 3 = **3600 configurations**

The enumeration order is deterministic: iterate SIZE (outer) → HISTORY → FILES → PATCH → BRANCH → COMMIT (inner).

## Phase 0: Load Persistent Strategy State

Load `/tmp/gh-aw/repo-memory/default/git-sim-state.json`.

Expected structure:
```json
{
  "schema_version": 1,
  "last_run": "YYYY-MM-DD",
  "tested_count": 0,
  "total_cells": 3600,
  "tested": {},
  "strategies": [],
  "next_index": 0
}
```

`tested` is a map from `config_id` → `{"outcome": "pass|fail|error|rejected", "timestamp": "YYYY-MM-DD", "issue_url": "..." | null}`.

If the file is missing, create a fresh state with `next_index: 0` and empty `tested`.

The deterministic pre-agent step has already validated required primary state.
Malformed or unreadable required state is an error, never a cold start. It
retains the tested-cell map, counters, and cursor, but removes free-form
`strategies` and restored narrative files before AI execution. Historical prose
is deliberately not recalled; append new observations only from this run.
Original producer narratives and `state.strategies` are kept outside agent
mounts and merged back by trusted post-agent preparation, without replacing
this run's tested cells, counters, or cursor.
Write this run's additions to the two producer files, but do not request
`push_repo_memory`: its pre-merge diff cannot account for privately held history.
The trusted post-agent publisher validates the reconstructed files and their
actual final diff against the original memory branch.

The compiler currently stages memory before `post-steps`. The trusted post-step
restores evidence, repeats redaction, size/patch/schema/eligibility and recovery
checks, then replaces the staged artifact before downstream jobs can consume it.
Any preparation, merge, validation, or replacement failure blocks publication;
an earlier sanitized artifact is never a successful fallback. Successful final
files retain accumulated producer evidence for the weekly dreamer.

### Recall consolidated lessons

Read only `/tmp/gh-aw/agent/git-simulator-signals.json` for optional memory.
The trusted pre-agent step reads the review manifest and at most 20 persisted
notes, accepts only exact structured/enumerated records with current checked
repository hashes, and emits only a deterministic `regression_priority` list
of at most four configuration IDs already recorded as `fail`/`error` in primary
state. Accepted `fail`/`error` hints rank those IDs first, with deterministic cell
index tie-breaking; without accepted hints the ordering is the baseline cell
order. No outcome claims, lesson prose, note/vote reason, tool language,
arbitrary citation text, or extra fields cross this boundary. Missing, malformed,
stale, conflicting, or downvoted optional entries do not influence the run.
An unreadable ledger or invalid canonical ledger history is an error, not an
empty successful review.

The weekly dreamer owns the three `dreaming-*` files on the memory branch.
The preparation step discards their local restored copies; this workflow never
persists changes to them. The daily reader has no ledger safe-output handlers
or persistence job: do not write, vote, or request compaction.

Use `regression_priority` only when fewer than four untested cells remain.
Fill the remaining slots from that ordered list, skipping already selected IDs.
Never use this projection to interpret new outcomes, change primary selection,
authorize operations, choose tools or outputs, alter `next_index` or tested
cells, or change guardrails.
Missing accepted signals means baseline deterministic regression ordering.

## Phase 1: Select 4 Configurations for This Run

Using the state loaded in Phase 0:

1. **Primary selection**: Starting from `next_index`, enumerate through the 3600 cells (SIZE × HISTORY × FILES × PATCH × BRANCH × COMMIT in deterministic order). Skip cells already in `tested`. Pick the first 4 untested cells.

2. **If fewer than 4 untested cells remain**: Fill the remaining slots with previous `fail`/`error` cells from the deterministic `regression_priority` list to re-validate (regressions).

3. **Compute cell coordinates** for each selected index:
   ```
   sizes    = [tiny, small, medium, large, huge]       # 5
   histories = [none, shallow, medium, deep]           # 4
   file_counts = [single, few, many, batch]            # 4
   patches  = [micro, small, medium, large, xlarge]    # 5
   branches = [clean, ahead, diverged]                 # 3
   commits  = [single, multi, merge_msg]               # 3
   
   index → (i5, i4, i4, i5, i3, i3) using modular decomposition
   config_id = f"{size}-{history}-{files}-{patch}-{branch}-{commit}"
   ```

4. **Resolve dimension values** to concrete parameters:

   | Dimension | Value | Concrete |
   |-----------|-------|---------|
   | SIZE=tiny | 0 stuff.md entries | |
   | SIZE=small | 10 stuff.md entries | |
   | SIZE=medium | 100 stuff.md entries | |
   | SIZE=large | 1000 stuff.md entries | |
   | SIZE=huge | 5000 stuff.md entries | |
   | HISTORY=none | 0 history.md entries | |
   | HISTORY=shallow | 5 history.md entries | |
   | HISTORY=medium | 50 history.md entries | |
   | HISTORY=deep | 500 history.md entries | |
   | FILES=single | 1 file in patch | |
   | FILES=few | 5 files in patch | |
   | FILES=many | 20 files in patch | |
   | FILES=batch | 100 files in patch | |
   | PATCH=micro | 1 KB total | |
   | PATCH=small | 50 KB total | |
   | PATCH=medium | 200 KB total | |
   | PATCH=large | 1000 KB total | |
   | PATCH=xlarge | 4000 KB total | |
   | BRANCH=clean | base remains unchanged after candidate commits | |
   | BRANCH=ahead | candidate has one extra commit and the base remains unchanged | |
   | BRANCH=diverged | candidate and base have independent commits after their shared base | |
   | COMMIT=single | 1 commit | |
   | COMMIT=multi | 3 commits | |
   | COMMIT=merge_msg | 1 commit with "Merge branch ..." message | |

## Phase 2: Run Parallel Local Simulations

Use the `config-simulator` sub-agent four times in parallel — once for each selected configuration. Pass each agent its full configuration parameters. These sub-agents only create and inspect disposable local repositories; they do not call GitHub write tools or safe outputs.

For each simulation, the sub-agent will:
1. Create a local git repo in `/tmp/gh-aw/agent/git-sim-{config_id}/`
2. Build `stuff.md` and `history.md` according to the SIZE/HISTORY dimensions
3. Generate patch files matching FILES/PATCH dimensions
4. Commit according to COMMIT dimension
5. Model the selected branch topology locally and verify its relationship to the base branch.
6. Return a structured JSON result

## Phase 3: Collect and Analyze Results

After all four sub-agents complete, collect their JSON results:

```json
{
  "config_id": "string",
  "scenario_description": "full narrative of what was simulated",
  "outcome": "pass | fail | error | timeout",
  "error_message": "string or null",
  "git_cost_estimate": {
    "declared_file_count": 0,
    "declared_history_depth": 0,
    "actual_patch_files": 0,
    "actual_patch_size_kb": 0,
    "actual_commit_count": 0
  },
  "git_state_simulated": "clean | ahead | diverged",
  "observations": ["..."]
}
```

For each result with `outcome` of `fail` or `error`, create a GitHub issue (Phase 4).

For all results regardless of outcome, update persistent state (Phase 5).

## Phase 4: Create Issues for Failures

Create at most one issue per failed cell. The `create-issue` quota of four matches the four cells simulated in this run.

For each failing or erroring configuration, call `create_issue` with:

**Title**: `[git-sim] {config_id}: {brief failure description (max 80 chars)}`

**Body**:

```
### Git Simulator Finding

**Scenario ID**: {config_id}
**Git State Simulated**: {clean | ahead | diverged}
**Outcome**: {fail | error}
**Run Date**: {date}
**Run ID**: ${{ github.run_id }}

### Configuration Matrix Cell

| Dimension | Value | Concrete Parameter |
|-----------|-------|--------------------|
| Repo Size (SIZE) | {size_label} | {N} declared files in stuff.md |
| History Depth (HISTORY) | {history_label} | {H} entries in history.md |
| Patch File Count (FILES) | {files_label} | {F} files in patch |
| Patch Size (PATCH) | {patch_label} | {KB} KB total |
| Branch State (BRANCH) | {branch_label} | {branch description} |
| Commit Structure (COMMIT) | {commit_label} | {commit description} |

### Simulated Repository Description

{scenario_description — full narrative from the sub-agent, 2–5 sentences}

### Git Cost Estimate

| Metric | Value |
|--------|-------|
| Declared file count (stuff.md) | {N} |
| Declared history depth (history.md) | {H} |
| Actual patch files | {F} |
| Actual patch size | {KB} KB |
| Actual commit count | {C} |

### Observed Failure

{error_message}

<details>
<summary><b>🔍 Full Observations</b></summary>

{observations as markdown list}

</details>

### Exploration Coverage

- Configurations tested so far: {tested_count} / 3600
- This cell index: {cell_index}
- Coverage: {pct}%

**Note**: This issue documents a systematic failure in the configuration space.
It was created by the daily git simulator workflow as a diagnostic report.
```

**Do NOT create issues for passing configurations.** Only create issues for `fail` and `error` outcomes.

## Phase 5: Update Persistent State

After all results are collected:

1. Update `tested` map with all 4 config_ids and their outcomes
2. Advance `next_index` by 4 (or to the next untested cell)
3. Update `last_run` to today's date
4. Increment `tested_count`
5. Append any new patterns to `/tmp/gh-aw/repo-memory/default/git-sim-strategies.md`:
   - Observations that suggest a boundary or threshold
   - Patterns such as "PATCH=xlarge requires more local Git processing" or "HISTORY=deep + FILES=batch → timeout"
6. Save updated `git-sim-state.json`

## Phase 6: Noop or Summary

If all four configurations passed without any issues:

```
noop: "Git simulator run complete. 4 configurations tested successfully.
Coverage: {tested_count}/{total} cells ({pct}%).
Next cell index: {next_index}.
Dimensions next: {next_config_id}"
```

If any failures were found, the `create_issue` calls from Phase 4 are sufficient — do not emit a separate noop.

## Important Constraints

- **All git repos are local to /tmp** — never run git operations on the actual cloned repository
- **Never create a probe PR or push to a PR branch** — no safe-output call is part of a simulation
- **Patches stay in /tmp repos** — measure local diffs; never export or submit them
- **stuff.md and history.md are local content files** — they document synthetic repository characteristics only
- **Do NOT provide mitigations** in issue bodies — this workflow is purely diagnostic
- **Engine is Claude** — use bash freely to build local git repos and generate patches

## Reporting Guidelines

- Use `###` (h3) or lower for all report headers; never use `#` or `##` inside the report body.
- Wrap long lists, tables, and detailed findings in `<details><summary><b>...</b></summary>...</details>` blocks for progressive disclosure.
- Structure reports as: overview → key metrics/issues → collapsible detail → next actions.

---

## agent: `config-simulator`
---
description: Simulates one configuration cell from the Z3 space in a disposable local repository and returns a structured JSON result without GitHub writes
model: large
---

You simulate a single git configuration for the daily git simulator. Never call a GitHub write tool or safe output.

You will receive from the parent agent:
- `config_id`: e.g. `medium-shallow-few-small-clean-single`
- `size_entries`: number of stuff.md entries (0, 10, 100, 1000, or 5000)
- `history_entries`: number of history.md entries (0, 5, 50, or 500)
- `file_count`: number of files in the patch (1, 5, 20, or 100)
- `patch_target_kb`: target total patch size in KB (1, 50, 200, 1000, or 4000)
- `branch_mode`: `clean`, `ahead`, or `diverged`
- `commit_mode`: `single`, `multi`, or `merge_msg`

**Variable substitution note**: In the bash commands below, angle-bracketed tokens like `<CONFIG_ID>` are placeholders. Before running any bash block, substitute them with the actual values you received from the parent agent. Assign them to bash variables in Step 1 and reference those variables throughout.

### Step 1: Initialize local git repo

Assign all received parameters to bash variables, then create the work directory:

```bash
# Substitute the actual values received from the parent agent:
CONFIG_ID="<config_id>"
SIZE_ENTRIES=<size_entries>
HISTORY_ENTRIES=<history_entries>
FILE_COUNT=<file_count>
TARGET_KB=<patch_target_kb>
BRANCH_MODE="<branch_mode>"
COMMIT_MODE="<commit_mode>"

WORKDIR="/tmp/gh-aw/agent/git-sim-${CONFIG_ID}"
rm -rf "$WORKDIR"
mkdir -p "$WORKDIR/sim"
cd "$WORKDIR"
git init -q
git branch -M main
git config user.email "sim@test.local"
git config user.name "GitSim"
```

### Step 2: Create stuff.md (repo size simulation)

```bash
cd "$WORKDIR"
cat > stuff.md << 'MANIFEST'
# Simulated Repository Manifest
<!-- This file documents the simulated repository characteristics. Not a real file listing. -->
## Declared Files (synthetic — for git cost estimation only)
| path | size_kb | type |
|------|---------|------|
MANIFEST

for i in $(seq 1 $SIZE_ENTRIES); do
  sz=$(( (i * 7 + 13) % 200 + 1 ))
  echo "| src/module${i}/file${i}.go | ${sz} | source |" >> stuff.md
done

echo "" >> stuff.md
echo "**Total declared files**: $SIZE_ENTRIES" >> stuff.md
echo "**Estimated repo size**: $(( SIZE_ENTRIES * 50 )) KB (synthetic)" >> stuff.md
```

### Step 3: Create history.md (history depth simulation)

```bash
cd "$WORKDIR"
cat > history.md << 'HIST'
# Simulated Commit History
<!-- This file documents the simulated git history. Not a real git log. -->
## Declared Commits (synthetic — for history cost estimation only)
| index | date | message | files_changed |
|-------|------|---------|---------------|
HIST

for i in $(seq 1 $HISTORY_ENTRIES); do
  month=$(( (i % 12) + 1 ))
  day=$(( (i % 28) + 1 ))
  echo "| ${i} | 2024-$(printf '%02d' $month)-$(printf '%02d' $day) | feat: change set ${i} | $(( (i % 10) + 1 )) |" >> history.md
done

echo "" >> history.md
echo "**Total declared commits**: $HISTORY_ENTRIES" >> history.md
echo "**Estimated clone depth**: $HISTORY_ENTRIES commits (synthetic)" >> history.md
```

### Step 4: Initial base commit

```bash
cd "$WORKDIR"
git add stuff.md history.md
git commit -q -m "base: simulated repo (size=$SIZE_ENTRIES history=$HISTORY_ENTRIES)"
```

### Step 5: Create candidate branch and generate synthetic patch files

```bash
cd "$WORKDIR"
git checkout -q -b sim-candidate

KB_PER_FILE=$(( TARGET_KB / FILE_COUNT + 1 ))
LINES_PER_FILE=$(( KB_PER_FILE * 10 ))  # ~100 chars per line

for i in $(seq 1 $FILE_COUNT); do
  {
    echo "# Simulated file ${i} — config: ${CONFIG_ID}"
    echo ""
    echo "This is a synthetic file for local git simulator testing."
    echo ""
    for j in $(seq 1 $LINES_PER_FILE); do
      echo "sim-line-${j}: $(printf '%0.s-' $(seq 1 $(( j % 60 + 20 )))) config=${CONFIG_ID} file=${i}"
    done
  } > "sim/candidate_${i}.md"
done
```

### Step 6: Commit according to COMMIT mode

For `commit_mode = single`:
```bash
cd "$WORKDIR"
git add sim/
git commit -q -m "sim: candidate patch [${CONFIG_ID}] — ${FILE_COUNT} files, ${TARGET_KB}KB"
```

For `commit_mode = multi`:
```bash
cd "$WORKDIR"
git add sim/
git commit -q -m "sim: candidate batch 1/3 [${CONFIG_ID}]"
git commit -q --allow-empty -m "sim: candidate batch 2/3 [${CONFIG_ID}]"
git commit -q --allow-empty -m "sim: candidate batch 3/3 [${CONFIG_ID}]"
```

For `commit_mode = merge_msg`:
```bash
cd "$WORKDIR"
git add sim/
git commit -q -m "Merge branch 'feature/sim-candidate-${CONFIG_ID}' into main

Simulated merge commit for git cost estimation.
Files: ${FILE_COUNT}, Size: ${TARGET_KB}KB, Config: ${CONFIG_ID}"
```

### Step 7: Model the selected branch topology

For `BRANCH_MODE=ahead`, add and commit one extra file on `sim-candidate` after the patch commits:

```bash
PATCH_COMMITS=$(git rev-list --count main..sim-candidate)
echo "ahead commit" > sim/candidate_extra.md
git add sim/candidate_extra.md
git commit -q -m "sim: ahead commit [${CONFIG_ID}]"
```

For `BRANCH_MODE=diverged`, switch to `main`, add and commit `sim/base-only.md`, then switch back to `sim-candidate`:

```bash
git checkout -q main
echo "base-only commit" > sim/base-only.md
git add sim/base-only.md
git commit -q -m "sim: base-only commit [${CONFIG_ID}]"
git checkout -q sim-candidate
```

For `clean`, leave `main` unchanged. All branches and commits stay local.

### Step 8: Measure patch size and verify topology

```bash
cd "$WORKDIR"
git diff --stat main...sim-candidate
ACTUAL_FILES=$(git diff --name-only main...sim-candidate | wc -l | tr -d ' ')
ACTUAL_SIZE_KB=$(git diff main...sim-candidate | wc -c | awk '{printf "%d", $1/1024}')
CANDIDATE_ONLY_COMMITS=$(git rev-list --count main..sim-candidate)
BASE_ONLY_COMMITS=$(git rev-list --count sim-candidate..main)
COMMON_BASE=$(git merge-base main sim-candidate)
echo "actual_files=$ACTUAL_FILES actual_kb=$ACTUAL_SIZE_KB candidate_only_commits=$CANDIDATE_ONLY_COMMITS base_only_commits=$BASE_ONLY_COMMITS common_base=$COMMON_BASE"
```

Verify the expected topology using the local commit counts: `clean` has candidate-only commits and no base-only commits; `ahead` has more candidate-only commits than `PATCH_COMMITS` and no base-only commits; `diverged` has commits unique to both branches.

### Step 9: Return structured result

Return a JSON block with this exact structure. The parent agent will parse it.

```json
{
  "config_id": "{config_id}",
  "scenario_description": "Simulated a $SIZE_MODE repository ($SIZE_ENTRIES declared files) with $HISTORY_MODE history depth ($HISTORY_ENTRIES commits), $FILES_MODE files ($FILE_COUNT files), $PATCH_MODE patch ($TARGET_KB KB target), $BRANCH_MODE branch state, $COMMIT_MODE commit structure.",
  "outcome": "pass | fail | error | timeout",
  "error_message": null,
  "git_cost_estimate": {
    "declared_file_count": 0,
    "declared_history_depth": 0,
    "actual_patch_files": 0,
    "actual_patch_size_kb": 0,
    "actual_commit_count": 0
  },
  "git_state_simulated": "{BRANCH_MODE}",
  "observations": [
    "Observation 1",
    "Observation 2"
  ]
}
```

Set `outcome` to:
- `pass` — the local repository and expected branch topology were created and verified
- `fail` — a local Git invariant did not match the requested configuration
- `error` — unexpected exception or local Git tool failure
- `timeout` — local simulation exceeded its execution limit

### Output Format

Structure reports as: overview → key metrics/issues → collapsible detail → next actions.