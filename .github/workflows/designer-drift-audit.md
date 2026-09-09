---
private: true
emoji: 🔍
description: Daily audit that detects drift between aw reference docs and the workflow designer skill/agent files
features:
  gh-aw-detection: true
on:
  schedule: daily on weekdays
permissions:
  contents: read
  issues: read
  pull-requests: read # required by pull_requests


tools:
  github:
    mode: gh-proxy
    toolsets: [default]
steps:
  - name: Extract reference doc metadata
    run: |
      mkdir -p /tmp/gh-aw/data

      # Core reference docs the designer depends on
      REFS=(
        ".github/aw/syntax.md"
        ".github/aw/safe-outputs.md"
        ".github/aw/network.md"
        ".github/aw/patterns.md"
        ".github/aw/subagents.md"
        ".github/aw/token-optimization.md"
        ".github/aw/triggers.md"
        ".github/aw/create-agentic-workflow.md"
      )

      # Extract key sections from each reference doc:
      # - H2/H3 headings (structural outline)
      # - YAML frontmatter field names
      # - Table rows containing mapping keywords
      for ref in "${REFS[@]}"; do
        if [ -f "$ref" ]; then
          basename=$(basename "$ref" .md)
          {
            echo "=== $ref ==="
            # Headings
            grep -n '^##' "$ref" | head -50
            echo "---"
            # Frontmatter keys (between --- delimiters)
            sed -n '/^---$/,/^---$/p' "$ref" | grep -E '^\w+:' | head -30
            echo "---"
            # Table rows with config keywords
            grep -E '^\|.*\|' "$ref" | grep -ivE '^\|[\s-]+\|' | head -80
            echo "==="
          } > "/tmp/gh-aw/data/ref-${basename}.txt"
        fi
      done

  - name: Extract designer file metadata
    run: |
      # Skill file
      SKILL=".github/aw/designer.md"
      if [ -f "$SKILL" ]; then
        {
          echo "=== SKILL.md ==="
          grep -n '^##' "$SKILL" | head -50
          echo "---"
          # Decision heuristic tables
          grep -E '^\|.*\|' "$SKILL" | grep -ivE '^\|[\s-]+\|' | head -150
          echo "---"
          # Explicit references to .github/aw/ files
          grep -n '\.github/aw/' "$SKILL" || true
          echo "==="
        } > /tmp/gh-aw/data/designer-skill.txt
      fi

      # Agent file
      AGENT=".github/agents/interactive-agent-designer.agent.md"
      if [ -f "$AGENT" ]; then
        {
          echo "=== interactive-agent-designer.agent.md ==="
          grep -n '^##' "$AGENT" | head -50
          echo "---"
          grep -E '^\|.*\|' "$AGENT" | grep -ivE '^\|[\s-]+\|' | head -60
          echo "---"
          grep -n '\.github/aw/' "$AGENT" || true
          echo "==="
        } > /tmp/gh-aw/data/designer-agent.txt
      fi

  - name: Collect recent changes to reference docs
    run: |
      # Commits in the last 7 days that touched reference docs
      git log --oneline --since="7 days ago" -- \
        .github/aw/syntax.md \
        .github/aw/safe-outputs.md \
        .github/aw/network.md \
        .github/aw/patterns.md \
        .github/aw/subagents.md \
        .github/aw/token-optimization.md \
        .github/aw/triggers.md \
        .github/aw/create-agentic-workflow.md \
        > /tmp/gh-aw/data/recent-ref-commits.txt 2>/dev/null || true

      # Commits in the last 7 days that touched designer files
      git log --oneline --since="7 days ago" -- \
        .github/aw/designer.md \
        .github/agents/interactive-agent-designer.agent.md \
        > /tmp/gh-aw/data/recent-designer-commits.txt 2>/dev/null || true

      # New or changed safe output types (from validation config in Go)
      grep -oP '"(\w+)":\s*\{' pkg/workflow/safe_outputs_validation_config.go \
        | sed 's/": {//' | tr -d '"' | sort \
        > /tmp/gh-aw/data/all-safe-output-types.txt 2>/dev/null || true
safe-outputs:
  create-issue:
    title-prefix: "Designer Drift Audit"
    labels: ["drift-audit", "automated"]
    close-older-issues: true
    expires: 7d
network:
  allowed:
    - defaults
engine:
  id: codex
  model-provider: openai
model: openai/gpt-5.3-codex
---

# Designer Drift Audit

## Task

You are an auditor that checks whether the **workflow designer** files are up-to-date with the **aw reference docs**.

### Input Files

Read these pre-fetched data files:

**Reference doc outlines** (one per doc):
- `/tmp/gh-aw/data/ref-syntax.txt`
- `/tmp/gh-aw/data/ref-safe-outputs.txt`
- `/tmp/gh-aw/data/ref-network.txt`
- `/tmp/gh-aw/data/ref-patterns.txt`
- `/tmp/gh-aw/data/ref-subagents.txt`
- `/tmp/gh-aw/data/ref-token-optimization.txt`
- `/tmp/gh-aw/data/ref-triggers.txt`
- `/tmp/gh-aw/data/ref-create-agentic-workflow.txt`

**Designer file outlines**:
- `/tmp/gh-aw/data/designer-skill.txt` — the SKILL.md
- `/tmp/gh-aw/data/designer-agent.txt` — the agent file

**Change history**:
- `/tmp/gh-aw/data/recent-ref-commits.txt` — recent reference doc changes
- `/tmp/gh-aw/data/recent-designer-commits.txt` — recent designer file changes
- `/tmp/gh-aw/data/all-safe-output-types.txt` — all registered safe output types

### Audit Checks

For each reference doc, compare its outline against both designer files and check:

1. **Trigger coverage**: Are all trigger types from `triggers.md` represented in the designer's trigger mapping table? Flag any missing trigger types.

2. **Safe output coverage**: Are all safe output types from `safe-outputs.md` and `all-safe-output-types.txt` represented in the designer's safe output mapping table? Flag any missing types.

3. **Network/tool coverage**: Are all network ecosystem identifiers from `network.md` and tool modes from `syntax.md` represented in the designer's mapping tables? Flag any missing entries.

4. **Frontmatter field coverage**: Are new frontmatter fields from `syntax.md` reflected in the designer's generation template? Flag fields that exist in the syntax spec but not in the template.

5. **Pattern coverage**: Are workflow patterns from `patterns.md` referenced or guidable from the designer? Flag patterns the designer cannot recommend.

6. **Stale references**: Does the designer reference any docs, fields, or options that no longer exist in the reference docs?

7. **Recent changes without sync**: Were reference docs changed recently (see `recent-ref-commits.txt`) without corresponding designer updates (see `recent-designer-commits.txt`)? Flag these as potential drift.

### Output

If **no drift** is detected, call `noop` with reason "Designer files are in sync with reference docs."

If **drift is detected**, create an issue with:

**Title**: Use the configured title-prefix (the system adds this automatically).

**Body structure**:

```
### Designer Drift Report — <date>

### Summary
<1-2 sentence overview: X drift items found across Y categories>

### Drift Items

<details>
<summary><b>View Drift Items by Category</b></summary>

#### <Category> (e.g., "Missing Safe Output Types")
- **Source**: `<reference-doc-path>`
- **Missing from**: `SKILL.md` / `agent file` / both
- **Details**: <specific items missing>
- **Suggested fix**: <concrete action>

...repeat for each drift item...

</details>

### No Drift
<list categories that passed with ✅>

### Methodology
Compared outlines of 8 reference docs against both designer files.
Reference doc commits (last 7 days): <count>
Designer file commits (last 7 days): <count>
```

## Safe Outputs

- Use `create-issue` only when drift is found.
- Call `noop` with a short reason when everything is in sync.

## Reporting Guidelines

- Use `###` (h3) or lower for all report headers; never use `#` or `##` inside the report body.
- Wrap long lists, tables, and detailed findings in `<details><summary><b>...</b></summary>...</details>` blocks for progressive disclosure.
- Structure reports as: overview → key metrics/issues → collapsible detail → next actions.