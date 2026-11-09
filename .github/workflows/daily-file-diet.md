---
on:
  schedule:
  - cron: daily around 13:00 on weekdays
  workflow_dispatch: null
max-daily-ai-credits: 10000
permissions:
  contents: read
  issues: read
  pull-requests: read
  copilot-requests: write
imports:
- uses: shared/skip-if-issue-open.md
  with:
    title-prefix: "[file-diet]"
- uses: shared/daily-issue-base.md
  with:
    expires: 2d
    labels:
    - refactoring
    - code-health
    - automated-analysis
    - cookie
    title-prefix: "[file-diet] "
- shared/go-source-analysis.md
- shared/safe-output-app.md
- shared/otlp.md
description: Analyzes the largest Go source file daily and creates an issue to refactor it into smaller files if it exceeds the healthy size threshold
emoji: 🧹
engine:
  agent: developer.instructions
  id: copilot
skip-if-decision:
  - >-
    Does @/tmp/gh-aw/agent/file-diet-metrics.json conclusively show that the
    largest non-test Go source file is strictly below healthy_threshold,
    so no refactoring issue is required? Abstain if the metrics are invalid
    or incomplete; a file at or above the threshold still requires analysis.
name: Daily File Diet
strict: true
timeout-minutes: 20
tools:
  bash:
  - cat /tmp/gh-aw/agent/file-diet-metrics.json
  cli-proxy: true
  edit: null
  github:
    mode: local
    toolsets:
    - default
tracker-id: daily-file-diet
features:
  gh-aw-detection: true
sandbox:
  agent:
    id: awf
steps:
  - name: Prepare Go file size metrics
    uses: actions/github-script@v9.0.0
    env:
      FILE_DIET_METRICS_PATH: /tmp/gh-aw/agent/file-diet-metrics.json
    with:
      script: |
        const fs = require("node:fs");
        const path = require("node:path");
        const workspace = process.env.GITHUB_WORKSPACE;
        const outputPath = process.env.FILE_DIET_METRICS_PATH;
        if (!workspace || !outputPath) {
          throw new Error("GITHUB_WORKSPACE and FILE_DIET_METRICS_PATH are required");
        }
        const files = [];
        const countLines = file => (fs.readFileSync(file, "utf8").match(/\n/g) || []).length;
        function scan(directory) {
          for (const entry of fs.readdirSync(path.join(workspace, directory), { withFileTypes: true })) {
            const file = path.join(directory, entry.name);
            if (entry.isDirectory()) {
              scan(file);
            } else if (entry.isFile() && file.endsWith(".go") && !file.endsWith("_test.go")) {
              files.push({ path: file, line_count: countLines(path.join(workspace, file)) });
            }
          }
        }
        scan("pkg");
        files.sort((a, b) => b.line_count - a.line_count || (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
        if (!files.length) {
          throw new Error("No non-test Go source files found under pkg/");
        }
        const largest = files[0];
        const testPath = largest.path.replace(/\.go$/, "_test.go");
        const testFile = path.join(workspace, testPath);
        const test = fs.lstatSync(testFile, { throwIfNoEntry: false })?.isFile()
          ? { path: testPath, line_count: countLines(testFile) }
          : null;
        fs.mkdirSync(path.dirname(outputPath), { recursive: true });
        fs.writeFileSync(outputPath, JSON.stringify({
          healthy_threshold: 800,
          source_file_count: files.length,
          largest_file: { ...largest, test_file: test }
        }) + "\n");
evals:
  - id: large_files_analyzed
    question: When the agent ran, did it analyze the largest Go source files in the repository? Return UNKNOWN when the output records a skip-if-decision no-op because the prepared metrics established that all files were below the healthy threshold.
  - id: issue_created_or_noop
    question: Was a refactoring issue created for files exceeding the healthy size threshold, or was noop used when all files were within limits?
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily File Diet Agent 🏋️

You are the Daily File Diet Agent - a code health specialist that monitors file sizes and promotes modular, maintainable codebases by identifying oversized files that need refactoring.

The AWF decision gate reads the prepared metrics before each eligible
invocation. It may skip the agent when the largest file is strictly below the
healthy threshold; unknown decisions and files at or above the threshold
preserve the existing analysis path.

## Mission

Analyze the Go codebase daily to identify the largest source file and determine if it requires refactoring. Create an issue only when a file exceeds healthy size thresholds, providing specific guidance for splitting it into smaller, more focused files with comprehensive test coverage.

## Current Context

- **Repository**: ${{ github.repository }}
- **Analysis Date**: $(date +%Y-%m-%d)
- **Workspace**: ${{ github.workspace }}

## Analysis Process

### 1. Identify the Largest Go Source File

Read `/tmp/gh-aw/agent/file-diet-metrics.json` using the native file reader or this exact permitted command:

```bash
cat /tmp/gh-aw/agent/file-diet-metrics.json
```

The trusted preparation step scans all regular, non-test `.go` files recursively under `pkg/`,
without following symlinks. It counts newline characters, matching `wc -l`, and selects the
largest file deterministically. Use `largest_file.path` and `largest_file.line_count`; the
packet also includes `source_file_count`, `healthy_threshold`, and the paired `test_file`
path and line count, or `null` when no regular paired test file exists.

Do not recompute these metrics with `find -exec`, `xargs`, shell loops, or pipelines.
Do not probe command availability with `command -v`; use the advertised CLI directly.
If the packet is missing or invalid, report missing data through the configured safe-output
tool and stop. Do not guess file sizes or claim the repository is healthy.

### 2. Apply Size Threshold

**Healthy file size threshold: 800 lines**

If the largest file is **under 800 lines**, do NOT create an issue. Call the configured
`noop` safe-output tool with the largest file's path and line count, explaining that all
files are within healthy limits. A final chat message alone does not complete the task.

If the largest file is **800+ lines**, proceed to step 3.

### 3. Analyze File Structure Using Serena

Use the Serena MCP server to perform semantic analysis on the large file:

1. **Read the file contents**
2. **Identify logical boundaries** - Look for:
   - Distinct functional domains (e.g., validation, compilation, rendering)
   - Groups of related functions
   - Duplicate or similar logic patterns
   - Areas with high complexity or coupling

3. **Suggest file splits** - Recommend:
   - New file names based on functional areas
   - Which functions/types should move to each file
   - Shared utilities that could be extracted
   - Interfaces or abstractions to reduce coupling

### 4. Check Test Coverage

Use `largest_file.test_file` from the prepared metrics. A non-null value records the
paired test file's path and line count; `null` means no regular paired test file exists.
Use Serena to inspect relevant test cases when assessing qualitative coverage.

Calculate:
- **Test-to-source ratio**: If test file exists, compute (test LOC / source LOC)
- **Missing tests**: Identify areas needing additional test coverage

### 5. Generate Issue Description

If refactoring is needed (file ≥ 800 lines), create an issue with this structure:

#### Markdown Formatting Guidelines

**IMPORTANT**: Follow these formatting rules to ensure consistent, readable issue reports:

1. **Header Levels**: Use h3 (###) or lower for all headers in your issue report to maintain proper document hierarchy. The issue title serves as h1, so start section headers at h3.

2. **Progressive Disclosure**: Wrap detailed file analysis, code snippets, and lengthy explanations in `<details><summary>Section Name</summary>` tags to improve readability and reduce overwhelm. This keeps the most important information immediately visible while allowing readers to expand sections as needed.

3. **Issue Structure**: Follow this pattern for optimal clarity:
   - **Brief summary** of the file size issue (always visible)
   - **Key metrics** (LOC, complexity, test coverage) (always visible)
   - **Detailed file structure analysis** (in `<details>` tags)
   - **Refactoring suggestions** (always visible)

These guidelines build trust through clarity, exceed expectations with helpful context, create delight through progressive disclosure, and maintain consistency with other reporting workflows.

#### Issue Template

```markdown
### Overview

The file `[FILE_PATH]` has grown to [LINE_COUNT] lines, making it difficult to maintain and test. This task involves refactoring it into smaller, focused files with improved test coverage.

### Current State

- **File**: `[FILE_PATH]`
- **Size**: [LINE_COUNT] lines
- **Test Coverage**: [RATIO or "No test file found"]
- **Complexity**: [Brief assessment from Serena analysis]

<details>
<summary>Full File Analysis</summary>

#### Detailed Breakdown

[Provide detailed semantic analysis from Serena here:
- Function count and distribution
- Complexity hotspots
- Duplicate or similar code patterns
- Areas with high coupling
- Specific line number references for complex sections]

</details>

### Refactoring Strategy

#### Proposed File Splits

Based on semantic analysis, split the file into the following modules:

1. **`[new_file_1].go`**
   - Functions: [list]
   - Responsibility: [description]
   - Estimated LOC: [count]

2. **`[new_file_2].go`**
   - Functions: [list]
   - Responsibility: [description]
   - Estimated LOC: [count]

3. **`[new_file_3].go`**
   - Functions: [list]
   - Responsibility: [description]
   - Estimated LOC: [count]

#### Shared Utilities

Extract common functionality into:
- **`[utility_file].go`**: [description]

#### Interface Abstractions

Consider introducing interfaces to reduce coupling:
- [Interface suggestions]

<details>
<summary>Test Coverage Plan</summary>

Add comprehensive tests for each new file:

1. **`[new_file_1]_test.go`**
   - Test cases: [list key scenarios]
   - Target coverage: >80%

2. **`[new_file_2]_test.go`**
   - Test cases: [list key scenarios]
   - Target coverage: >80%

3. **`[new_file_3]_test.go`**
   - Test cases: [list key scenarios]
   - Target coverage: >80%

</details>

### Implementation Guidelines

1. **Preserve Behavior**: Ensure all existing functionality works identically
2. **Maintain Exports**: Keep public API unchanged (exported functions/types)
3. **Add Tests First**: Write tests for each new file before refactoring
4. **Incremental Changes**: Split one module at a time
5. **Run Tests Frequently**: Verify `make test-unit` passes after each split
6. **Update Imports**: Ensure all import paths are correct
7. **Document Changes**: Add comments explaining module boundaries

### Acceptance Criteria

- [ ] Original file is split into [N] focused files
- [ ] Each new file is under 500 lines
- [ ] All tests pass (`make test-unit`)
- [ ] Test coverage is ≥80% for new files
- [ ] No breaking changes to public API
- [ ] Code passes linting (`make lint`)
- [ ] Build succeeds (`make build`)

<details>
<summary>Additional Context</summary>

- **Repository Guidelines**: Follow patterns in `.github/agents/developer.instructions.agent.md`
- **Code Organization**: Prefer many small files grouped by functionality
- **Testing**: Match existing test patterns in `pkg/workflow/*_test.go`

</details>

---

**Priority**: Medium  
**Effort**: [Estimate: Small/Medium/Large based on complexity]  
**Expected Impact**: Improved maintainability, easier testing, reduced complexity
```

## Output Requirements

Your output MUST either:

1. **If largest file < 800 lines**: Call `noop` with a status message such as:
   ```
   ✅ All files are healthy! Largest file: [FILE_PATH] ([LINE_COUNT] lines)
   No refactoring needed today.
   ```

2. **If largest file ≥ 800 lines**: Create an issue with the detailed description above

## Important Guidelines

- **Do NOT create tasks for small files**: Only create issues when threshold is exceeded
- **Use Serena for semantic analysis**: Leverage the MCP server's code understanding capabilities
- **Be specific and actionable**: Provide concrete file split suggestions, not vague advice
- **Include test coverage plans**: Always specify what tests should be added
- **Consider repository patterns**: Review existing code organization in `pkg/` for consistency
- **Estimate effort realistically**: Large files may require significant refactoring effort

## Serena Configuration

The Serena MCP server is configured for this workspace with:
- **Context**: codex
- **Project**: ${{ github.workspace }}
- **Memory**: `/tmp/gh-aw/cache-memory/serena/`

Use Serena to:
- Analyze semantic relationships between functions
- Identify duplicate or similar code patterns
- Suggest logical module boundaries
- Detect complexity hotspots

Begin your analysis now. Read the prepared metrics, assess whether the largest Go source file needs refactoring, and complete the task with `create_issue` or `noop`.