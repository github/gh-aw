---
private: true
emoji: "🔧"
name: Daily Go Function Namer
description: Analyzes one entire Go package per day using Serena to extract function names and suggest renames that improve agent discoverability, using round-robin over package directories via cache-memory
on:
  schedule: daily
  workflow_dispatch:

max-daily-ai-credits: 10000
permissions:
  contents: read
  issues: read
  pull-requests: read

tracker-id: daily-function-namer

model: openai/gpt-5.4
engine:
  id: pi
  model-provider: openai
imports:
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[function-namer] "
      expires: 3d
  - shared/mcp/serena-go.md

  - shared/otlp.md
safe-outputs:
  create-issue:
    expires: 7d
    title-prefix: "[function-namer] "
    labels: [refactoring, code-quality, automated-analysis, cookie]
    max: 1
    close-older-issues: true

sandbox:
  agent:
    id: awf
tools:
  cli-proxy: true
  cache-memory: true
  github:
    mode: gh-proxy
    toolsets: [default, issues]
  bash: true

timeout-minutes: 30
strict: true
features:
  gh-aw-detection: true
evals:
  - id: package_analyzed
    question: Did the agent analyze at least one Go package for function naming quality and discoverability?
  - id: issue_created_or_noop
    question: Was an issue created with function rename suggestions, or was noop used when no naming improvements were identified?
---

# Daily Go Function Namer

You are an AI agent that analyzes Go functions daily to improve their names for better discoverability by AI coding agents. Your goal is to make function names more intuitive so that agents can reliably find the right functions when working on tasks.

## Mission

Each day, analyze **one entire Go package** using round-robin rotation across all package directories in `pkg/`. For each package:

1. Enumerate all functions in the package with a fast `grep` sweep
2. Activate Serena for deeper semantic analysis of functions that need it
3. Evaluate each name's clarity and intent
4. Suggest renames that are clearer and more intuitive for agents
5. Create a GitHub issue with a concrete agentic implementation plan

## Context

- **Repository**: ${{ github.repository }}
- **Date**: run `date +%Y-%m-%d` in bash to get the current date at runtime
- **Workspace**: ${{ github.workspace }}
- **Cache**: `/tmp/gh-aw/cache-memory/`

## Step 1: Compute Package Selection with Code

Run this script to load the round-robin state, enumerate all Go package directories, and compute which package to analyze this run:

```bash
# Load last_package_index from cache (-1 sentinel means cache is absent/empty — pick randomly later)
LAST_INDEX=$(python3 -c "
import sys, json, os
p = '/tmp/gh-aw/cache-memory/function-namer-state.json'
if os.path.exists(p):
    try:
        d = json.load(open(p))
        print(d.get('last_package_index', -1))
    except Exception:
        print(-1)
else:
    print(-1)
")

# Enumerate all unique package directories containing non-test Go files
mapfile -t ALL_PKGS < <(find pkg -name '*.go' ! -name '*_test.go' -type f | xargs -I{} dirname {} | sort -u)
TOTAL=${#ALL_PKGS[@]}

if [ "$TOTAL" -eq 0 ]; then
  echo "ERROR: no Go packages found in pkg/" >&2
  exit 1
fi

# On cache miss, start at a random position so repeated cold starts don't always hit the same package
if [ "$LAST_INDEX" -eq -1 ]; then
  LAST_INDEX=$(( RANDOM % TOTAL ))
  echo "cache_miss=true (random start at index ${LAST_INDEX})"
fi

echo "total_packages=${TOTAL}"
echo "last_package_index=${LAST_INDEX}"

# Pick the next package in round-robin order
PKG_DIR="${ALL_PKGS[$(( LAST_INDEX % TOTAL ))]}"
NEW_INDEX=$(( (LAST_INDEX + 1) % TOTAL ))

# Collect all non-test Go files in the selected package
mapfile -t PKG_FILES < <(find "$PKG_DIR" -maxdepth 1 -name '*.go' ! -name '*_test.go' -type f | sort)
FILE_COUNT=${#PKG_FILES[@]}
if [ "$FILE_COUNT" -gt 0 ]; then
  TOTAL_FNS=$(grep -hc "^func " "${PKG_FILES[@]}" 2>/dev/null | awk '{s+=$1} END {print s+0}')
else
  TOTAL_FNS=0
fi

echo "selected_package=${PKG_DIR}"
echo "file_count=${FILE_COUNT}"
echo "new_last_package_index=${NEW_INDEX}"
echo "total_functions_approx=${TOTAL_FNS}"
echo "--- selected files ---"
printf '%s\n' "${PKG_FILES[@]}"
```

The script outputs:
- `selected_package` — the package directory to analyze this run
- `file_count` — number of Go files in the selected package
- `new_last_package_index` — value to write back to cache after the run
- `total_functions_approx` — total function count across the package files
- The list of package file paths (one per line, after `--- selected files ---`)

Use these values directly for the rest of the workflow. Do **not** re-derive or re-compute them manually.

**On cold start** (`/tmp/gh-aw/cache-memory/function-namer-state.json` missing): treat this as expected initialization, not a failure. Do **not** call `missing_data` for a missing state file on first run or cold cache; run the Step 1 script as written, accept `LAST_INDEX=-1`, and continue.

## Step 2: Enumerate All Functions in the Package

Before invoking Serena, run a fast `grep` sweep across all files in the selected package to build a complete function inventory. This minimizes Serena tool calls by giving you the full picture upfront:

```bash
# Fast enumeration — one pass over all package files
grep -n "^func " "${PKG_FILES[@]}" | awk -F: '{printf "%-50s line %-5s %s\n", $1, $2, $3}'
```

This produces a list of every function/method definition with its file and line number. Use this inventory to decide which functions need deeper Serena analysis.

## Step 3: Activate Serena

Activate the Serena project to enable Go semantic analysis:

```
Tool: activate_project
Args: { "path": "${{ github.workspace }}" }
```

## Step 4: Analyze Each File in the Package with Serena

For each of the files in the selected package (output by Step 1), perform a full function name analysis. Use the function inventory from Step 2 to guide which functions need deeper Serena investigation.

### 4.1 Get All Symbols

```
Tool: get_symbols_overview
Args: { "file_path": "<relative/path/to/file.go>" }
```

This returns all functions, methods, and types defined in the file.

### 4.2 Read Function Implementations

For each function identified in 4.1, read enough of the implementation to understand its behavior:

```
Tool: read_file
Args: { "file_path": "<file.go>", "start_line": N, "end_line": M }
```

For small files you may read the entire file:

```bash
cat <path/to/file.go>
```

### 4.3 Evaluate Function Names

For each function, assess its name against these criteria:

**Rename candidates — names that hurt agent discoverability:**
- Generic verbs without context: `process()`, `handle()`, `run()`, `execute()`, `generate()`
- Implementation-focused names: `useGoroutine()`, `callHTTP()`, `doLoop()`
- Abbreviations that obscure intent: `genSO()`, `mkCfg()`, `bldYAML()`, `chk()`
- Names that mismatch actual behavior
- Names that would cause an agent to overlook this function when searching for its capability

**Names to keep — these are already discoverable:**
- Verb + noun describing the exact action: `compileWorkflowMarkdown()`, `validateFrontmatterConfig()`
- Standard Go interface methods: `String()`, `Error()`, `ServeHTTP()`, `MarshalJSON()`
- Constructors following Go convention: `NewCompiler()`, `NewMCPConfig()`
- Short unexported names used as closures or immediately-invoked helpers

### 4.4 Propose Renames

For each function that would benefit from a clearer name:

1. Propose a new name in Go naming conventions (camelCase for unexported, PascalCase for exported)
2. Explain why the new name is more discoverable for an agent
3. Find all call sites using Serena:

```
Tool: find_referencing_symbols
Args: { "symbol_name": "<currentName>", "file_path": "pkg/..." }
```

**Rename examples:**
| Current | Suggested | Reason |
|---|---|---|
| `process()` | `compileWorkflowMarkdown()` | Specifies what is processed |
| `generate()` | `generateGitHubActionsYAML()` | Describes the output |
| `handle()` | `handleMCPToolRequest()` | Adds missing context |
| `mkCfg()` | `buildMCPServerConfig()` | Readable and specific |
| `run()` | `executeDockerContainer()` | Concrete action |

**Only suggest renames where the improvement is clear and meaningful.** Quality over quantity — two well-justified suggestions are better than ten marginal ones.

## Step 5: Update Cache State

After completing the analysis, save the updated round-robin position. Use the `new_last_package_index` value from Step 1 and a filesystem-safe timestamp (`YYYY-MM-DD`):

```bash
cat > /tmp/gh-aw/cache-memory/function-namer-state.json << 'CACHE_EOF'
{
  "last_package_index": <new_package_index>,
  "analyzed_packages": [
    <previous entries, pruned to last 30>,
    {"package": "pkg/workflow", "analyzed_at": "2026-03-13"}
  ]
}
CACHE_EOF
```

Where `<new_package_index>` is the `new_last_package_index` value output by Step 1, and the `analyzed_packages` list contains one entry per package actually analyzed.

Use relative paths (e.g., `pkg/workflow`) matching the output of the `find pkg` command.

Prune `analyzed_packages` to the most recent 30 entries to prevent unbounded growth.

If the state file was missing at the start of the run, initialize it from scratch here instead of reporting missing cache data.

## Step 6: Create Issue with Agentic Plan

If any rename suggestions were found across the analyzed package, create a GitHub issue.

Use h3 (`###`) or lower for all headers in the issue body. Never use h1 (`#`) or h2 (`##`) — these are reserved for the issue title.

If **no improvements were found**, emit `noop` and exit:

```json
{"noop": {"message": "No rename suggestions found for package <pkg>. All analyzed functions have clear, descriptive names."}}
```

Otherwise, create an issue with this structure:

---

**Title**: `Go function rename plan: <package>` (e.g., `Go function rename plan: pkg/workflow`)

**Body**:

```markdown
### 🏷️ Go Function Rename Plan

**Package Analyzed**: `<package>`
**Analysis Date**: <YYYY-MM-DD>
**Round-Robin Position**: package <package_index> of <total> total packages
**Functions Analyzed**: <total_functions_approx> functions across <file_count> files

### Why This Matters

When AI coding agents search for functions to complete a task, they rely on function
names to understand what code does. Clear, descriptive names increase the likelihood
that an agent will find the right function instead of reimplementing existing logic.
Functions in the same package also call each other, so reviewing them together gives
better context for rename decisions.

### Rename Suggestions

#### `<file1>`

| Current Name | Suggested Name | Reason |
|---|---|---|
| `oldName()` | `newName()` | Describes the specific action rather than the generic verb |

**All functions in this file** (for reference):
- `functionA()` — ✅ Clear, no change needed
- `oldName()` — ⚠️ Rename suggested (see table above)

#### `<file2>`

<!-- Same structure, or: "No renames needed for this file." -->

#### `<fileN>`

<!-- Repeat for each file in the package. -->

---

<details>
<summary>🤖 Agentic Implementation Plan</summary>

### Agentic Implementation Plan

This issue is designed to be assigned to a coding agent. The agent should implement
all rename suggestions below in a single pull request.

### Prerequisites

- [ ] Read each rename suggestion and verify it is accurate by reviewing the function body
- [ ] Check for any Go interface constraints that prevent renaming (e.g., must match interface method name)

### Implementation Steps

For **each** rename suggestion, follow this sequence:

#### 1. Rename the function in `<file>`

```go
// Old
func oldName(args) returnType {

// New
func newName(args) returnType {
```

#### 2. Update all call sites

Use `grep` to find every caller and update the reference:

```bash
grep -rn "oldName" pkg/ --include="*.go"
```

Also check test files:

```bash
grep -rn "oldName" pkg/ --include="*_test.go"
```

#### 3. Verify compilation after each rename

```bash
make build
```

#### 4. Run tests after all renames are complete

```bash
make test-unit
make lint
```

### Commit Convention

Each rename should be a focused commit:

```
refactor: rename <oldName> to <newName> for clarity
```

### Validation Checklist

- [ ] All renames implemented
- [ ] All call sites updated (Go files and test files)
- [ ] `make build` passes with no errors
- [ ] `make test-unit` passes
- [ ] `make lint` passes
- [ ] PR description explains the agent-discoverability rationale

### Notes for the Agent

- This is a **pure rename refactor** — behavior must not change, only names
- If a rename causes unexpected complexity (e.g., name conflicts, interface constraints),
  skip it and leave a comment in the PR explaining why
- Follow existing naming conventions documented in `AGENTS.md`
- Unexported functions used only as closures or immediately-invoked can be skipped

</details>

---

*Generated by the Daily Go Function Namer workflow*
*Run: ${{ github.run_id }}*
```

---

## Analysis Guidelines

### Focus on Agent Discoverability

The primary question is: **"Would an AI coding agent find this function when given a task description?"**

Ask yourself:
- If an agent is asked to "compile a workflow", would it find `compileWorkflowMarkdown()` faster than `process()`?
- If an agent is asked to "validate frontmatter", would it find `validateFrontmatterConfig()` rather than `check()`?
- If an agent is asked to "generate a YAML file", would it find `generateGitHubActionsWorkflow()` instead of `generate()`?

### What to Skip

Do NOT suggest renames for:
- Functions that already have clear, specific names
- Standard Go interface implementations (`String()`, `Error()`, `ServeHTTP()`, `MarshalJSON()`)
- Constructor functions following Go convention (`New*`, `Make*`)
- Functions where the rename would be minor or stylistic (e.g., `makeConfig` → `createConfig`)
- Private single-letter functions used as immediate callbacks or closures

### Quality Bar

Only include a rename suggestion if you are confident it would measurably improve an agent's ability to find the function. When in doubt, leave the function as-is.

---

**Important**: If no action is needed after completing your analysis, you **MUST** call the `noop` safe-output tool. Failing to call any safe-output tool is the most common cause of workflow failures.

```json
{"noop": {"message": "No rename suggestions found for package <pkg>. All analyzed functions already have clear, descriptive names that support agent discoverability."}}
```

### Output Format

- Use `###` (h3) or lower for all report headers; never use `#` or `##` inside the report body.
- Wrap long lists, tables, and detailed findings in `<details><summary><b>...</b></summary>...</details>` blocks to reduce scrolling.
- Structure reports as: overview → key metrics/issues → collapsible detail → next actions.