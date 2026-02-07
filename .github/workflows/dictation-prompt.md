---
name: Dictation Prompt Generator
description: Generates optimized prompts for voice dictation and speech-to-text workflows
on:
  workflow_dispatch:
  schedule:
    - cron: "0 6 * * 0"  # Weekly on Sundays at 6 AM UTC

permissions:
  contents: read
  issues: read
  pull-requests: read

engine: copilot

network: defaults

imports:
  - shared/mood.md
  - shared/reporting.md

tools:
  edit:
  bash:
    - "*"
  github:
    toolsets: [default]

safe-outputs:
  create-pull-request:
    expires: 2d
    title-prefix: "[docs] "
    labels: [documentation, automation]
    draft: false
    auto-merge: true

timeout-minutes: 10
---

# Dictation Prompt Generator

Extract technical vocabulary from documentation files and create a concise dictation instruction file for fixing speech-to-text errors and improving text clarity.

## Your Mission

Create a concise dictation instruction file at `skills/dictation/SKILL.md` that:
1. Contains a glossary of approximately 1000 project-specific terms extracted from documentation
2. Provides instructions for fixing speech-to-text errors (ambiguous terms, spacing, hyphenation)
3. Provides instructions for "agentifying" text: removing filler words (humm, you know, um, uh, like, etc.), improving clarity, and making text more professional
4. Does NOT include planning guidelines or examples (keep it short and focused on error correction and text cleanup)
5. Includes guidelines to NOT plan or provide examples, just focus on fixing speech-to-text errors and improving text quality.

## Task Steps

### 1. Scan Documentation for Project-Specific Glossary

Scan documentation files in `docs/src/content/docs/` to extract approximately 1000 project-specific technical terms (950-1050 acceptable).

**Focus areas:**
- Configuration: safe-outputs, permissions, tools, cache-memory, toolset, frontmatter
- Engines: copilot, claude, codex, custom
- Bot mentions: @copilot (for GitHub issue assignment)
- Commands: compile, audit, logs, mcp, recompile
- GitHub concepts: workflow_dispatch, pull_request, issues, discussions
- Repository-specific: agentic workflows, gh-aw, activation, MCP servers
- File formats: markdown, lockfile (.lock.yml), YAML
- Tool types: edit, bash, github, playwright, web-fetch, web-search
- Operations: fmt, lint, test-unit, timeout-minutes, runs-on

**Exclude**: makefile, Astro, starlight (tooling-specific, not user-facing)

### 2. Create the Dictation Instructions File

Create `skills/dictation/SKILL.md` with:
- Frontmatter with name and description fields
- Title: Dictation Instructions
- Technical Context: Brief description of gh-aw
- Project Glossary: ~1000 terms, alphabetically sorted, one per line
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

### 3. Create Pull Request

Use the create-pull-request tool to submit your changes with:
- Title: "[docs] Update dictation skill instructions"
- Description explaining the changes made to skills/dictation/SKILL.md

## Guidelines

- Scan only `docs/src/content/docs/**/*.md` files
- Extract ~1000 terms (950-1050 acceptable)
- Exclude tooling-specific terms (makefile, Astro, starlight)
- Prioritize frequently used project-specific terms
- Alphabetize the glossary
- No descriptions in glossary (just term names)
- Focus on fixing speech-to-text errors, not planning or examples

## Success Criteria

- ✅ File `skills/dictation/SKILL.md` exists
- ✅ Contains proper SKILL.md frontmatter (name, description)
- ✅ Contains ~1000 project-specific terms (950-1050 acceptable)
- ✅ Terms extracted from documentation only
- ✅ Focuses on fixing speech-to-text errors
- ✅ Includes instructions for removing filler words and improving text clarity
- ✅ Pull request created with changes
