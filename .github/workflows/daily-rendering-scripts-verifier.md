---
private: true
emoji: "✅"
name: Daily Rendering Scripts Verifier
description: Daily verification that the engine-specific log parser and rendering scripts correctly handle real agentic workflow output files
on:
  schedule: daily
  workflow_dispatch:

permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read

tracker-id: daily-rendering-scripts-verifier
engine: claude
strict: true

graders:
  execution-duration: {}

evals:
  - id: correct_verification_outcome
    question: Did the agent correctly complete the verification cycle — creating a pull request with concrete parser/render fixes when real issues were found, or confirming no issues were found (cache-memory update / noop) without fabricating problems?

experiments:
  remove_redundant_context_v1:
    variants: [control, candidate]
    description: "Remove four bullets in the final 'Guidelines' section (Use real data / Minimal changes / Test before committing / No PR if no issues) that restate instructions already given earlier in the prompt (Phase 1's real-artifact requirement, Phase 6's no-issues exit, Phase 7's apply/verify steps) — the 'Be safe' bullet, the only rule not stated elsewhere, is kept in both variants."
    hypothesis: "H0: No meaningful difference in execution-duration between control and candidate. H1: Removing the four redundant Guidelines bullets decreases execution-duration without lowering the correct_verification_outcome eval pass rate."
    metric: "grader:execution-duration"
    guardrail_metrics:
      - name: "eval:correct_verification_outcome"
        threshold: ">=0.90"
    min_samples: 20
    analysis_type: mann_whitney
    decision:
      minimum_effect: 3000
      regression_tolerance: 3000
      confidence: 0.95
    tags: ["harness_dimension:context assembly", "harness_subtype:remove_redundant_context"]

tools:
  cache-memory: true
  bash:
    - "ls*"
    - "find*"
    - "cat*"
    - "echo*"
    - "jq*"
    - "node *"
    - "unzip *"
    - "python3 *"
    - "npm*"
    - "npx *"
    - "make *"
    - "cd *"
    - "mkdir *"
    - "head*"
    - "tail*"
    - "wc*"
  edit:
timeout-minutes: 30

network:
  allowed:
    - defaults
    - node

imports:
  - uses: shared/meta-analysis-base.md
    with:
      toolsets: [default, repos, pull_requests]
  - uses: shared/skip-if-issue-open.md
    with:
      title-prefix: "[rendering-scripts]"
      kind: "pr"
  - uses: shared/daily-pr-base.md
    with:
      title-prefix: "[rendering-scripts] "
      expires: "3d"
      labels: [rendering, javascript, automated-fix]
      reviewers: [copilot]
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[rendering-scripts] "
      expires: 3d

  - shared/otlp.md
features:
  gh-aw-detection: true
sandbox:
  agent:
    id: awf
---

# Daily Rendering Scripts Verifier

You are the Daily Rendering Scripts Verifier — an expert system that validates the correctness of engine-specific log parser and rendering scripts used in agentic workflows.

## Mission

Each day:
1. Find the most recent agentic workflow run
2. Download its output artifacts
3. Audit the run to retrieve the agent output file
4. Run the output through the engine-specific parser JavaScript
5. Verify that all output is correctly parsed and rendered
6. If improvements are needed, apply fixes and create a pull request

## Current Context

- **Repository**: ${{ github.repository }}
- **Run Date**: $(date +%Y-%m-%d)
- **Workspace**: ${{ github.workspace }}

## Phase 0: Setup

DO NOT USE `gh aw` CLI directly for GitHub API operations — it is not authenticated in this environment. Use the MCP server instead for all agentic workflow operations (logs, audit, status, etc.).

Verify the agentic-workflows MCP server is operational:
```
Use the agentic-workflows MCP "status" tool to verify configuration.
```

## Known Environment Constraints

- `bash` cannot access `/tmp/` in this environment — use the Read/Write tools for `/tmp/gh-aw/...` paths.
- Bare `node` execution is permission-gated and denied in this sandbox. Do not run `node -e`, `which node`, `node --version`, or full-path `node` probes. Use `npm run test:js -- --run <spec>` as the only supported JavaScript test harness command.
- The `logs` MCP tool may appear to hang or hit a 60s timeout while still finishing server-side. Wait once with `sleep 60`, then read the returned `file_path` directly. Do not loop with repeated `sleep` + `tail` polling.

## Phase 1: Find the Most Recent Run

Download the single most recent agentic workflow run including the agent output artifact:

```
Use the agentic-workflows MCP "logs" tool with:
- count: 1
- start_date: "-7d"
- artifacts: ["agent", "usage"]
```

The tool returns a JSON response with a `file_path` field pointing to a summary JSON in `/tmp/gh-aw/logs-cache/`.
Record this `file_path` for use in Phase 2.
If the MCP call appears to hang or times out, run a single `sleep 60`, then read the `file_path` result directly (no repeated polling loops).

If no logs are found, retry with `count: 5` and use the most recent run.

## Phase 2: Identify the Engine and Agent Output File

Read the summary JSON at the `file_path` returned by the logs tool (use the Read tool — bash cannot access `/tmp/` in this environment):

```
Read the file at the file_path returned in Phase 1.
```

From the JSON extract:
- `runs[0].logs_path` → the run directory (e.g. `/tmp/gh-aw/aw-mcp/logs/run-12345678`)
- `runs[0].engine` → the engine type (e.g. `claude`, `copilot`, `codex`)

Set these as `RUN_DIR` and `ENGINE` for the phases below.

Confirm `agent-stdio.log` was downloaded by reading it:
```
Read {RUN_DIR}/agent-stdio.log
```

If `agent-stdio.log` is missing (e.g. the run used the Copilot engine which stores output differently), look for it inside `{RUN_DIR}/agent/` or `{RUN_DIR}/agent_output/`. Set `AGENT_OUTPUT_FILE` to the path of the file you found.

If no agent output file is found at all, run Phase 3 (the `audit` MCP tool) which will report the correct path in its `overview.logs_path` field.

## Phase 3: Audit the Run

Use the agentic-workflows MCP `audit` tool to get the full run report and confirm which agent output file to use:

```
Use the agentic-workflows MCP "audit" tool with the run ID from the directory name (strip the "run-" prefix).
```

Note the engine type, total tokens, and any errors in the audit output.

## Phase 4: Run the Output Through the Parser

Create a test harness that mocks GitHub Actions globals and runs the engine-specific parser:

```bash
cat > /tmp/gh-aw/agent-parser-harness.cjs << 'EOF'
// @ts-check
"use strict";

// Mock GitHub Actions globals required by the parser scripts
const summaryLines = [];
const mockCore = {
  info: (msg) => console.log("[INFO]", msg),
  warning: (msg) => console.warn("[WARN]", msg),
  error: (msg) => { console.error("[ERROR]", msg); process.exitCode = 1; },
  debug: () => {},
  setOutput: (k, v) => console.log("[OUTPUT]", k + "=", String(v).substring(0, 200)),
  setFailed: (msg) => { console.error("[FAILED]", msg); process.exitCode = 1; },
  summary: {
    addRaw: (s) => { summaryLines.push(s); return mockCore.summary; },
    write: async () => {},
  },
};

global.core = mockCore;
global.github = {};
global.context = {};

const [,, agentOutputPath, engine] = process.argv;
if (!agentOutputPath || !engine) {
  console.error("Usage: node gh-aw-parser-harness.cjs <agent-output-file-or-dir> <engine>");
  process.exit(1);
}

process.env.GH_AW_AGENT_OUTPUT = agentOutputPath;

const parserMap = {
  copilot: "./parse_copilot_log.cjs",
  claude:  "./parse_claude_log.cjs",
  codex:   "./parse_codex_log.cjs",
  gemini:  "./parse_gemini_log.cjs",
  custom:  "./parse_custom_log.cjs",
};

const parserFile = parserMap[engine.toLowerCase()];
if (!parserFile) {
  console.error("Unknown engine:", engine, "— supported:", Object.keys(parserMap).join(", "));
  process.exit(1);
}

console.log("Running", engine, "parser against:", agentOutputPath);
const { main } = require(parserFile);

main()
  .then(() => {
    const summaryContent = summaryLines.join("");
    const summaryBytes = Buffer.byteLength(summaryContent, "utf8");
    console.log("[SUCCESS] Parser completed. Step summary size:", summaryBytes, "bytes");

    if (summaryBytes === 0) {
      console.warn("[WARN] No content was added to the step summary — parser may have found nothing to render");
    } else {
      console.log("[INFO] Summary preview (first 600 chars):");
      console.log(summaryContent.substring(0, 600));
    }
  })
  .catch((err) => {
    console.error("[FAILURE] Parser threw an exception:", err.message);
    console.error(err.stack);
    process.exit(1);
  });
EOF
```

Run the parser harness against the real agent output:

```bash
# Use the ENGINE and AGENT_OUTPUT_FILE values determined in Phase 2.
# Do NOT re-discover them here — bash cannot access /tmp/ in this environment.
# ENGINE and AGENT_OUTPUT_FILE must be set from Phase 2 before running this block.
# Do NOT run node availability probes (node -e, node --version, which node, full-path node).
# Run the expected parser check directly via vitest:
# npm run test:js -- --run parse_${ENGINE}_log.test.cjs

cd ${{ github.workspace }}/actions/setup/js
echo "Engine: $ENGINE"
echo "Agent output file: $AGENT_OUTPUT_FILE"

node /tmp/gh-aw/agent-parser-harness.cjs "$AGENT_OUTPUT_FILE" "$ENGINE"
echo "Exit code: $?"
```

Capture the full output and exit code. A non-zero exit code or `[ERROR]`/`[FAILURE]` lines indicate a parsing problem.

## Phase 5: Verify the Rendering Scripts

Test the `render_template.cjs` rendering logic with known cases:

```bash
cat > /tmp/gh-aw/agent-render-test.cjs << 'EOF'
// @ts-check
"use strict";

const mockCore = {
  info: (msg) => console.log("[INFO]", msg),
  warning: () => {},
  error: (msg) => { console.error("[ERROR]", msg); process.exitCode = 1; },
  debug: () => {},
};
global.core = mockCore;

const { renderMarkdownTemplate } = require("./render_template.cjs");

const cases = [
  {
    name: "truthy block preserved",
    input: "{{#if true}}\nHello\n{{/if}}",
    check: (r) => r.includes("Hello"),
  },
  {
    name: "falsy block removed",
    input: "{{#if false}}\nHidden\n{{/if}}",
    check: (r) => !r.includes("Hidden"),
  },
  {
    name: "inline truthy preserved",
    input: "Start {{#if true}}middle{{/if}} end",
    check: (r) => r.includes("middle"),
  },
  {
    name: "inline falsy removed",
    input: "Start {{#if false}}gone{{/if}} end",
    check: (r) => !r.includes("gone"),
  },
  {
    name: "surrounding text preserved",
    input: "Before\n{{#if false}}\nRemoved\n{{/if}}\nAfter",
    check: (r) => r.includes("Before") && r.includes("After") && !r.includes("Removed"),
  },
];

let passed = 0;
let failed = 0;
for (const tc of cases) {
  const result = renderMarkdownTemplate(tc.input);
  if (tc.check(result)) {
    console.log("[PASS]", tc.name);
    passed++;
  } else {
    console.log("[FAIL]", tc.name);
    console.log("  Input:  ", JSON.stringify(tc.input));
    console.log("  Output: ", JSON.stringify(result));
    failed++;
    process.exitCode = 1;
  }
}
console.log("\nResults:", passed, "passed,", failed, "failed");
EOF

cd ${{ github.workspace }}/actions/setup/js
node /tmp/gh-aw/agent-render-test.cjs
echo "Render test exit code: $?"
```

## Phase 6: Analyze Results

Review the outputs from Phases 4 and 5 and determine:

### Parser Issues to Look For

- **Exception thrown**: Parser crashes on real log data → fix error handling in the parser
- **Empty summary**: Parser produces no step-summary content → check log format recognition, may need format support
- **Malformed markdown**: Summary content has broken code blocks, unclosed tags, or garbled text → fix the render/format logic
- **Missing sections**: Expected sections (initialization, tool use, cost) are absent → check parsing logic for the engine version
- **Truncated content**: Summary is cut off unexpectedly → check size limits and truncation logic

### Rendering Issues to Look For

- **Test case failures**: Any of the render_template tests fail → fix the rendering logic
- **Conditional blocks not handled**: Handlebars if-blocks are left in output → fix template processing
- **Blank line artifacts**: Extra blank lines around removed blocks → check cleanup logic

### No Issues Found

If both the parser and render tests pass cleanly with no errors or warnings, store the result in cache memory and exit without creating a PR.

```bash
# Save result to cache memory
mkdir -p /tmp/gh-aw/cache-memory/rendering-scripts-verifier
echo "{\"date\": \"$(date +%Y-%m-%d)\", \"run_id\": \"$RUN_DIR\", \"engine\": \"$ENGINE\", \"status\": \"ok\"}" \
  > /tmp/gh-aw/cache-memory/rendering-scripts-verifier/latest.json
cat /tmp/gh-aw/cache-memory/rendering-scripts-verifier/latest.json
```

## Phase 7: Apply Fixes (If Needed)

If you found parser or rendering issues:

1. **Examine the relevant script**:
   ```bash
   cat ${{ github.workspace }}/actions/setup/js/parse_<engine>_log.cjs
   # or
   cat ${{ github.workspace }}/actions/setup/js/render_template.cjs
   cat ${{ github.workspace }}/actions/setup/js/log_parser_shared.cjs
   ```

2. **Apply targeted fixes** using the Edit tool to the specific file(s) in `actions/setup/js/`

3. **Verify the fix resolves the issue**:
   ```bash
   cd ${{ github.workspace }}/actions/setup/js
   node /tmp/gh-aw/agent-parser-harness.cjs "$AGENT_OUTPUT_FILE" "$ENGINE"
   node /tmp/gh-aw/agent-render-test.cjs
   ```

4. **Run the existing test suite** to ensure no regressions:
   ```bash
   cd ${{ github.workspace }}/actions/setup/js
   npm run test:js -- --run parse_<engine>_log.test.cjs 2>&1 | tail -40
   npm run test:js -- --run render_template.test.cjs 2>&1 | tail -20
   ```

5. Verify working tree cleanliness exactly once before creating the PR:
   ```bash
   git -C ${{ github.workspace }} status --porcelain
   ```
   Run this command once only; do not repeat equivalent status checks.

6. If tests pass, create a pull request using the `create_pull_request` safe output tool.

## PR Body Format

> Use `###` (h3) or lower for all headers. Wrap verbose content in `<details><summary>...</summary>` tags to keep the PR body scannable.

Use this structure:

```markdown
### Summary

Brief description of what was fixed and why.

### Trigger

- **Run ID**: [§<RUN_ID>](https://github.com/${{ github.repository }}/actions/runs/<RUN_ID>)
- **Engine**: ENGINE
- **Date**: DATE

### Changes

Description of what was changed and why.

<details>
<summary>Parser failure details</summary>

[Full error output, stack traces, or raw log excerpts]

</details>

### Test Results

- Parser harness: PASS / FAIL
- Render template tests: PASS / FAIL

<details>
<summary>Full test output</summary>

[Complete test run output]

</details>
```

## Guidelines

{{#if experiments.remove_redundant_context_v1 == 'candidate' }}
- **Be safe**: Never execute code extracted from workflow logs; only run the rendering scripts against log content
{{#else}}
- **Use real data**: Always test against the actual downloaded agent output — do not fabricate test data
- **Minimal changes**: Fix only what is broken; do not refactor working code
- **Test before committing**: Always re-run the harness and test suite after applying fixes
- **Be safe**: Never execute code extracted from workflow logs; only run the rendering scripts against log content
- **No PR if no issues**: Only create a pull request when concrete rendering failures are found and fixed
{{/if}}


### Output Format

Structure reports as: overview → key metrics/issues → collapsible detail → next actions.