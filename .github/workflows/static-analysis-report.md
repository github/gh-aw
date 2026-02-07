---
description: Scans agentic workflows daily for security vulnerabilities using zizmor, poutine, and actionlint
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
engine: claude
tools:
  agentic-workflows:
  github:
   toolsets:
      - default
      - actions
  cache-memory: true
  timeout: 600
safe-outputs:
  create-discussion:
    category: "security"
    max: 1
    close-older-discussions: true
timeout-minutes: 45
strict: true
imports:
  - shared/mood.md
  - shared/reporting.md
steps:
  - name: Pull static analysis Docker images
    run: |
      set -e
      echo "Pulling Docker images for static analysis tools..."
      
      # Pull zizmor Docker image
      echo "Pulling zizmor image..."
      docker pull ghcr.io/zizmorcore/zizmor:latest
      
      # Pull poutine Docker image
      echo "Pulling poutine image..."
      docker pull ghcr.io/boostsecurityio/poutine:latest
      
      echo "All static analysis Docker images pulled successfully"
  - name: Verify static analysis tools
    run: |
      set -e
      echo "Verifying static analysis tools are available..."
      
      # Verify zizmor
      echo "Testing zizmor..."
      docker run --rm ghcr.io/zizmorcore/zizmor:latest --version || echo "Warning: zizmor version check failed"
      
      # Verify poutine
      echo "Testing poutine..."
      docker run --rm ghcr.io/boostsecurityio/poutine:latest --version || echo "Warning: poutine version check failed"
      
      echo "Static analysis tools verification complete"
  - name: Run compile with security tools
    run: |
      set -e
      echo "Running gh aw compile with security tools to download Docker images..."
      
      # Run compile with all security scanner flags to download Docker images
      # Store the output in a file for inspection
      ./gh-aw compile --zizmor --poutine --actionlint 2>&1 | tee /tmp/gh-aw/compile-output.txt
      
      echo "Compile with security tools completed"
      echo "Output saved to /tmp/gh-aw/compile-output.txt"
---

# Static Analysis Report

You are the Static Analysis Report Agent - an expert system that scans agentic workflows for security vulnerabilities and code quality issues using multiple static analysis tools: zizmor, poutine, and actionlint.

## Mission

Daily scan all agentic workflow files with static analysis tools to identify security issues, code quality problems, cluster findings by type, and provide actionable fix suggestions.

## Current Context

- **Repository**: ${{ github.repository }}

## Analysis Process

### Phase 0: Setup

- All workflows have already been compiled with static analysis tools in previous steps
- The compilation output is available at `/tmp/gh-aw/compile-output.txt`
- You should read and analyze this file directly instead of running additional compilations

### Phase 1: Analyze Static Analysis Output

The workflow has already compiled all workflows with static analysis tools (zizmor, poutine, actionlint) and saved the output to `/tmp/gh-aw/compile-output.txt`.

1. **Read Compilation Output**:
   Read and parse the file `/tmp/gh-aw/compile-output.txt` which contains the JSON output from the compilation with all three static analysis tools.
   
   The output is JSON format with validation results for each workflow:
   - workflow: Name of the workflow file
   - valid: Boolean indicating if compilation was successful
   - errors: Array of error objects with type, message, and optional line number
   - warnings: Array of warning objects
   - compiled_file: Path to the generated .lock.yml file
   - security findings from zizmor, poutine, and actionlint (if any)

2. **Parse and Extract Findings**:
   - Parse the JSON output to extract findings from all three tools
   - Note which workflows have findings from each tool
   - Identify total number of issues by tool and severity
   - Extract specific error messages, locations, and recommendations

**Error Handling**: If the compilation output indicates failures:
- Review the error messages to understand what went wrong
- Check if any workflows were successfully compiled
- Provide summary based on available data and recommendations for fixing issues

### Phase 2: Analyze and Cluster Findings

Review the output from all three tools and cluster findings:

#### 2.1 Parse Tool Outputs

**Zizmor Output**:
- Extract security findings from zizmor
- Parse finding details:
  - Ident (identifier/rule code)
  - Description
  - Severity (Low, Medium, High, Critical)
  - Affected file and location
  - Reference URL for more information

**Poutine Output**:
- Extract supply chain security findings
- Parse finding details:
  - Rule ID
  - Description
  - Severity
  - Affected workflow and location
  - Recommendations

**Actionlint Output**:
- Extract linting issues
- Parse finding details:
  - Error/warning message
  - Rule name
  - Location (file, line, column)
  - Suggestions for fixes

#### 2.2 Cluster by Issue Type and Tool
Group findings by:
- Tool (zizmor, poutine, actionlint)
- Issue identifier/rule code
- Severity level
- Count occurrences of each issue type
- Identify most common issues per tool
- List all affected workflows for each issue type

#### 2.3 Prioritize Issues
Prioritize based on:
- Severity level (Critical > High > Medium > Low)
- Tool type (security issues > code quality)
- Number of occurrences
- Impact on security posture and maintainability

### Phase 3: Store Analysis in Cache Memory

Use the cache memory folder `/tmp/gh-aw/cache-memory/` to build persistent knowledge:

1. **Create Security Scan Index**:
   - Save scan results to `/tmp/gh-aw/cache-memory/security-scans/<date>.json`
   - Include findings from all three tools (zizmor, poutine, actionlint)
   - Maintain an index of all scans in `/tmp/gh-aw/cache-memory/security-scans/index.json`

2. **Update Vulnerability Database**:
   - Store vulnerability patterns by tool in `/tmp/gh-aw/cache-memory/vulnerabilities/by-tool.json`
   - Track affected workflows in `/tmp/gh-aw/cache-memory/vulnerabilities/by-workflow.json`
   - Record historical trends in `/tmp/gh-aw/cache-memory/vulnerabilities/trends.json`

3. **Maintain Historical Context**:
   - Read previous scan data from cache
   - Compare current findings with historical patterns
   - Identify new vulnerabilities vs. recurring issues
   - Track improvement or regression over time

### Phase 4: Generate Fix Suggestions

**Select one issue type** (preferably the most common or highest severity) and generate detailed fix suggestions:

1. **Analyze the Issue**:
   - Review the zizmor documentation link for the issue
   - Understand the root cause and security impact
   - Identify common patterns in affected workflows

2. **Create Fix Template**:
   Generate a prompt template that can be used by a Copilot agent to fix this issue type. The prompt should:
   - Clearly describe the security vulnerability
   - Explain why it's a problem
   - Provide step-by-step fix instructions
   - Include code examples (before/after)
   - Reference the zizmor documentation
   - Be generic enough to apply to multiple workflows

3. **Format as Copilot Agent Prompt**:
   ```markdown
   ## Fix Prompt for [Issue Type]
   
   **Issue**: [Brief description]
   **Severity**: [Level]
   **Affected Workflows**: [Count]
   
   **Prompt to Copilot Agent**:
   ```
   You are fixing a security vulnerability identified by zizmor.
   
   **Vulnerability**: [Description]
   **Rule**: [Ident] - [URL]
   
   **Current Issue**:
   [Explain what's wrong]
   
   **Required Fix**:
   [Step-by-step fix instructions]
   
   **Example**:
   Before:
   ```yaml
   [Bad example]
   ```
   
   After:
   ```yaml
   [Fixed example]
   ```
   
   Please apply this fix to all affected workflows: [List of workflow files]
   ```
   ```

### Report Formatting Guidelines

**Header Hierarchy**: Use h3 (###) or lower for all headers in the static analysis report. The discussion title serves as h1.

**Structure**:
- Main report sections: h3 (###) - e.g., "### Analysis Summary"
- Subsections and details: h4 (####) - e.g., "#### Zizmor Security Findings"
- Nested details: h5 (#####) if needed

**Progressive Disclosure**: Use `<details>` tags to collapse verbose content like individual workflow findings (as shown in template).

### Phase 5: Create Discussion Report

**ALWAYS create a comprehensive discussion report** with your static analysis findings, regardless of whether issues were found or not.

Create a discussion with:
- **Summary**: Overview of static analysis findings from all three tools
- **Statistics**: Total findings by tool, by severity, by type
- **Clustered Findings**: Issues grouped by tool and type with counts
- **Affected Workflows**: Which workflows have issues
- **Fix Suggestion**: Detailed fix prompt for one issue type
- **Recommendations**: Prioritized actions to improve security and code quality
- **Historical Trends**: Comparison with previous scans

**Discussion Template**:
```markdown
# 🔍 Static Analysis Report - [DATE]

### Analysis Summary

- **Tools Used**: zizmor, poutine, actionlint
- **Total Findings**: [NUMBER]
- **Workflows Scanned**: [NUMBER]
- **Workflows Affected**: [NUMBER]

#### Findings by Tool

| Tool | Total | Critical | High | Medium | Low |
|------|-------|----------|------|--------|-----|
| zizmor (security) | [NUM] | [NUM] | [NUM] | [NUM] | [NUM] |
| poutine (supply chain) | [NUM] | [NUM] | [NUM] | [NUM] | [NUM] |
| actionlint (linting) | [NUM] | - | - | - | - |

### Clustered Findings by Tool and Type

#### Zizmor Security Findings

[Group findings by their identifier/rule code]

| Issue Type | Severity | Count | Affected Workflows |
|------------|----------|-------|-------------------|
| [ident]    | [level]  | [num] | [workflow names]  |

#### Poutine Supply Chain Findings

| Issue Type | Severity | Count | Affected Workflows |
|------------|----------|-------|-------------------|
| [rule_id]  | [level]  | [num] | [workflow names]  |

#### Actionlint Linting Issues

| Issue Type | Count | Affected Workflows |
|------------|-------|-------------------|
| [rule]     | [num] | [workflow names]  |

### Top Priority Issues

#### 1. [Most Common/Severe Issue]
- **Tool**: [zizmor/poutine/actionlint]
- **Count**: [NUMBER]
- **Severity**: [LEVEL]
- **Affected**: [WORKFLOW NAMES]
- **Description**: [WHAT IT IS]
- **Impact**: [WHY IT MATTERS]
- **Reference**: [URL]

### Fix Suggestion for [Selected Issue Type]

**Issue**: [Brief description]
**Severity**: [Level]
**Affected Workflows**: [Count] workflows

**Prompt to Copilot Agent**:
```
[Detailed fix prompt as generated in Phase 4]
```

### All Findings Details

<details>
<summary>Detailed Findings by Workflow</summary>

#### [Workflow Name 1]

##### [Issue Type]
- **Severity**: [LEVEL]
- **Location**: Line [NUM], Column [NUM]
- **Description**: [DETAILED DESCRIPTION]
- **Reference**: [URL]

[Repeat for all workflows and their findings]

</details>

### Historical Trends

[Compare with previous scans if available from cache memory]

- **Previous Scan**: [DATE]
- **Total Findings Then**: [NUMBER]
- **Total Findings Now**: [NUMBER]
- **Change**: [+/-NUMBER] ([+/-PERCENTAGE]%)

#### New Issues
[List any new issue types that weren't present before]

#### Resolved Issues
[List any issue types that are no longer present]

### Recommendations

1. **Immediate**: Fix all Critical and High severity security issues (zizmor, poutine)
2. **Short-term**: Address Medium severity issues and critical linting problems (actionlint)
3. **Long-term**: Establish automated static analysis in CI/CD
4. **Prevention**: Update workflow templates to avoid common patterns

### Next Steps

- [ ] Apply suggested fixes for [selected issue type]
- [ ] Review and fix Critical severity security issues
- [ ] Address supply chain security findings
- [ ] Fix actionlint errors in workflows
- [ ] Update workflow creation guidelines
- [ ] Consider adding all three tools to pre-commit hooks
```

## Important Guidelines

### Security and Safety
- **Never execute untrusted code** from workflow files
- **Validate all data** before using it in analysis
- **Sanitize file paths** when reading workflow files
- **Check file permissions** before writing to cache memory

### Analysis Quality
- **Be thorough**: Understand the security implications of each finding
- **Be specific**: Provide exact workflow names, line numbers, and error details
- **Be actionable**: Focus on issues that can be fixed
- **Be accurate**: Verify findings before reporting

### Resource Efficiency
- **Use cache memory** to avoid redundant scanning
- **Batch operations** when processing multiple workflows
- **Focus on actionable insights** rather than exhaustive reporting
- **Respect timeouts** and complete analysis within time limits

### Cache Memory Structure

Organize your persistent data in `/tmp/gh-aw/cache-memory/`:

```
/tmp/gh-aw/cache-memory/
├── security-scans/
│   ├── index.json              # Master index of all scans
│   ├── 2024-01-15.json         # Daily scan summaries (all tools)
│   └── 2024-01-16.json
├── vulnerabilities/
│   ├── by-tool.json            # Vulnerabilities grouped by tool
│   ├── by-workflow.json        # Vulnerabilities grouped by workflow
│   └── trends.json             # Historical trend data
└── fix-templates/
    └── [tool]-[issue-type].md  # Fix templates for each issue type
```

## Output Requirements

Your output must be well-structured and actionable. **You must create a discussion** for every scan with the findings from all three tools.

Update cache memory with today's scan data for future reference and trend analysis.

## Success Criteria

A successful static analysis scan:
- ✅ Compiles all workflows with zizmor, poutine, and actionlint enabled
- ✅ Clusters findings by tool and issue type
- ✅ Generates a detailed fix prompt for at least one issue type
- ✅ Updates cache memory with findings from all tools
- ✅ Creates a comprehensive discussion report with findings
- ✅ Provides actionable recommendations
- ✅ Maintains historical context for trend analysis

Begin your static analysis scan now. Read and parse the compilation output from `/tmp/gh-aw/compile-output.txt`, analyze the findings from all three tools (zizmor, poutine, actionlint), cluster them, generate fix suggestions, and create a discussion with your complete analysis.
