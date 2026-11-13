---
private: true
emoji: "🎙️"
name: Dictation Prompt Generator
description: Generates optimized prompts for voice dictation and speech-to-text workflows
on:
  workflow_dispatch:
  schedule:
    - cron: "weekly on sunday around 6:00"  # ~6 AM UTC on Sundays (scattered)

permissions:
  contents: read
  issues: read
  pull-requests: read

  copilot-requests: write


engine:
  id: copilot
  copilot-sdk: true

max-tool-denials: 3
network: defaults

imports:
  - shared/pr-safe-output-defaults.md
  - shared/reporting.md

  - shared/otlp.md
tools:
  cli-proxy: true
  edit:
  bash:
    - "*"
  github:
    mode: gh-proxy
    toolsets: [default]

safe-outputs:
  create-pull-request:
    title-prefix: "[docs] "
    labels: [documentation, automation]
    auto-merge: true

timeout-minutes: 10
evals:
  - id: dictation_prompt_generated
    question: Did the agent generate an optimized prompt for voice dictation and speech-to-text workflows?
  - id: prompt_update_created
    question: Did the agent create a pull request containing the generated dictation prompt update?
---

# Dictation Prompt Generator

Extract technical vocabulary from documentation files and create a concise dictation instruction file for fixing speech-to-text errors and improving text clarity.

## Required Safe Output (Must Do)

Before you finish, you **MUST** call exactly one safe-output tool:
- Use `create_pull_request` if you made meaningful updates to `DICTATION.md`.
- Use `noop` if no meaningful update is needed after analysis.
- Use `report_incomplete` only if a blocker prevented completion.

Do **not** end with prose-only output. A safe-output tool call is required for successful workflow completion.

## Your Mission

Create a concise dictation instruction file at `DICTATION.md` that:
1. Contains a glossary of exactly 256 project-specific terms extracted from documentation
2. Provides instructions for fixing speech-to-text errors (ambiguous terms, spacing, hyphenation)
3. Provides instructions for "agentifying" text: removing filler words (humm, you know, um, uh, like, etc.), improving clarity, and making text more professional
4. Does NOT include planning guidelines or examples (keep it short and focused on error correction and text cleanup)
5. Includes guidelines to NOT plan or provide examples, just focus on fixing speech-to-text errors and improving text quality.

## Task Steps

### 1. Run NLP Word-Frequency Histogram

Run the following Python script to compute a word-frequency histogram of code-formatted tokens across all documentation files. Use the output as the **primary source** for selecting the 256 glossary terms — prefer tokens with high frequency that are project-specific (not generic English words).

```bash
python3 - <<'EOF'
import re
from pathlib import Path
from collections import Counter

docs = Path("docs/src/content/docs")
tokens = Counter()

for md_file in docs.rglob("*.md"):
    text = md_file.read_text(errors="replace")
    # Collect backtick-quoted technical tokens
    tokens.update(re.findall(r'`([^`\n]+)`', text))
    # Also collect hyphenated/dotted/underscored identifiers
    tokens.update(re.findall(r'\b([\w][\w\-\.]{2,}[\w])\b', text))

print("Frequency histogram — top 500 project tokens:")
for tok, n in tokens.most_common(500):
    if len(tok) > 2:
        print(f"  {n:5d}  {tok}")
EOF
```

### 2. Scan Documentation for Project-Specific Glossary

Use `search` to efficiently discover documentation covering different areas of the project, then read the returned files to extract vocabulary. This is more targeted than scanning all files with `find`:

- `search("workflow configuration frontmatter engine permissions")` — core workflow concepts
- `search("safe-outputs create-pull-request tools MCP server")` — tools and integrations
- `search("compilation CLI commands audit logs")` — CLI and developer tools
- `search("network sandbox runtime activation triggers")` — advanced features

Read each returned file path for its content, then also scan any remaining documentation files in `docs/src/content/docs/` to ensure broad coverage.

**Focus areas for extraction:**
- Configuration: safe-outputs, permissions, tools, cache-memory, toolset, frontmatter
- Engines: @copilot, claude, codex, custom
- Bot mentions: @copilot (for GitHub issue assignment)
- Commands: compile, audit, logs, mcp, recompile
- GitHub concepts: workflow_dispatch, pull_request, issues, discussions
- Repository-specific: agentic workflows, gh-aw, activation, MCP servers
- File formats: markdown, lockfile (.lock.yml), YAML
- Tool types: edit, bash, github, playwright, web-fetch, web-search
- Operations: fmt, lint, test-unit, timeout-minutes, runs-on

**Exclude**: makefile, Astro, starlight (tooling-specific, not user-facing)

### 3. Create the Dictation Instructions File

Create `DICTATION.md` with:
- Frontmatter with name and description fields
- Title: Dictation Instructions
- Technical Context: Brief description of gh-aw
- Project Glossary: 256 terms, alphabetically sorted, one per line
- Fix Speech-to-Text Errors: Common misrecognitions → correct terms
- Clean Up and Improve Text: Instructions for removing filler words and improving clarity
- Guidelines: General instructions as follows

```markdown
You do not have enough background information to plan or provide code examples.
- do NOT generate code examples
- do NOT plan steps
- focus on fixing speech-to-text errors and improving text quality
- remove filler words (humm, you know, um, uh, like, basically, actually, etc.)
- improve clarity and make text more professional
- maintain the user's intended meaning
```

### 4. Create Pull Request

Use the create-pull-request tool to submit your changes with:
- Title: "[docs] Update dictation skill instructions"
- Description explaining the changes made to DICTATION.md

## Guidelines

- Scan only `docs/src/content/docs/**/*.md` files
- Extract 256 terms (240-270 acceptable)
- Exclude tooling-specific terms (makefile, Astro, starlight)
- Prioritize frequently used project-specific terms (use NLP histogram from Step 1)
- Alphabetize the glossary
- No descriptions in glossary (just term names)
- Focus on fixing speech-to-text errors, not planning or examples

## Success Criteria

- ✅ File `DICTATION.md` exists
- ✅ Contains proper frontmatter (name, description)
- ✅ Contains 256 project-specific terms (240-270 acceptable)
- ✅ Terms extracted from documentation only
- ✅ Focuses on fixing speech-to-text errors
- ✅ Includes instructions for removing filler words and improving text clarity
- ✅ Pull request created with changes