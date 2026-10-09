---
description: Guide for defining inline sub-agents in workflow markdown files — syntax, engine placement, frontmatter fields, and best practices.
---

# Inline Sub-Agents

Define specialised agents directly in a workflow markdown file. At runtime, sub-agent sections are extracted (after `{{#runtime-import}}` macros resolve) and written to the engine-specific agents directory for the engine CLI to discover.

---

## Syntax

Define a sub-agent with a level-2 Markdown heading of the form `## agent: \`name\``:

```markdown
## agent: `file-summarizer`
---
description: Summarizes the content of a file in a few concise sentences
model: small
---
You are a file summarization assistant. When given a file path, read the
file and return a brief summary (2–4 sentences) describing its purpose
and key contents. Be concise and factual.
```

### Name rules

- Must be enclosed in backticks: `` `name` ``
- Lowercase only: `[a-z][a-z0-9_-]*`
- Examples: `` `planner` ``, `` `file-summarizer` ``, `` `code-reviewer` ``

### Block boundary

The block ends at the next `##` heading (any level-2 heading) or at EOF. Place sub-agent blocks **at the bottom** of the file, after all main workflow content. If parent instructions follow a sub-agent, close it with ``## end agent: `name` `` so those instructions remain in the main prompt.

### Frontmatter fields

Sub-agent frontmatter is preserved as authored. Use fields and model values supported by the workflow's engine; gh-aw does not translate model aliases or insert the heading's name into the frontmatter.

| Field | Required | Default | Notes |
|---|---|---|---|
| `name` | Claude: yes | — | Claude's registered agent identifier. Use the same name as the inline heading. |
| `description` | Claude: yes | — | Human-readable summary of the sub-agent's role |
| `model` | No | Engine default | Engine-native model override. For Claude, `inherit` uses the parent model; `inherited` is not the inheritance sentinel. |

For Claude, include both required fields and use its native inheritance value:

```markdown
## agent: `file-summarizer`
---
name: file-summarizer
description: Summarizes a file
model: inherit
---
Read the requested file and return a concise summary.
```

Claude silently skips project agent files without `name`, even when the filename matches the inline heading. See [Claude's sub-agent frontmatter reference](https://code.claude.com/docs/en/sub-agents#supported-frontmatter-fields).

Aliases such as `small` in the Copilot examples below are engine-specific, not portable substitutions. Other frontmatter fields are also preserved; native tool restrictions must use the engine's syntax. Sub-agents run within the parent's engine and network configuration.

---

## Engine-Specific Placement

Sub-agent files are written to the directory and with the extension each engine natively expects:

| Engine | Directory | Extension |
|---|---|---|
| Copilot (default) | `.agents/agents/` | `.agent.md` |
| Claude | `.claude/agents/` | `.md` |
| Codex | `.codex/agents/` | `.md` |
| Gemini (deprecated) | `.gemini/agents/` | `.md` |
| Agy (experimental) | `.agents/agents/` | `.agent.md` |
| Pi | `.pi/agents/` | `.md` |

The engine is detected at compile time from the `engine:` field and injected as `GH_AW_ENGINE_ID` into the interpolation step's environment.

---

## MCP Access in Sub-Agents

Sub-agents **do not have their own MCP servers** — they run in the parent's agent environment without independent tool config. For file system and shell access, enable on the parent workflow:

- **`cli-proxy: true`** — GitHub CLI proxy for authenticated `gh` calls. Recommended for any sub-agent that reads/writes repo content.
- **`tools.github.mode: gh-proxy`** — routes GitHub API calls through the gh proxy sidecar; required for private repos or the GitHub MCP toolset.

```yaml
---
engine: copilot
tools:
  github:
    mode: gh-proxy
  cli-proxy: true
---
```

---

## When to Use Sub-Agents

### 1 — Parallel specialised tasks with smaller models

Break a large workflow into parallel units handled by small/cheap models, then let the parent (large) model reason over the aggregated results:

```markdown
# Investigate: Repository Health

## Step 1 — gather data

Use the `dependency-scanner` agent to list all outdated packages.
Use the `test-coverage` agent to summarise uncovered code paths.
Use the `secret-scanner` agent to check for leaked credentials.

## Step 2 — synthesise

Combine the three reports above into a prioritised action plan.
The top item must have a linked PR draft or issue.

## agent: `dependency-scanner`
---
description: Lists outdated npm/pip/go packages
model: small
---
Run the appropriate package-manager audit command and return a
machine-readable list of outdated packages with their current and
latest versions.

## agent: `test-coverage`
---
description: Summarises low-coverage code paths
model: small
---
Read the most recent test coverage report and list the top 5 files or
functions with coverage below 60 %. Include the file path and line range.

## agent: `secret-scanner`
---
description: Checks for potential credential leaks
model: small
---
Scan staged changes and recently modified files for patterns that
resemble API keys, tokens, or passwords. Report any findings with the
file name and approximate line number.
```

The parent model orchestrates; sub-agents do the heavy lifting with `small` at lower cost.

### 2 — Reusable specialised helpers

Extract repetitive sub-tasks (file summarisation, commit-message generation, code explanation) into a named sub-agent the main prompt calls by name.

---

## Planner-Worker Pattern

Split work to control cost and keep context quality high:

- **Main/frontier agent (planner-orchestrator):** forms hypotheses, decides what evidence is needed, picks workers, synthesises conclusions
- **Worker sub-agents (usually `model: small`):** bounded retrieval, extraction, classification, verification, one-shot summarisation

Prompt workers narrowly and evidence-first (e.g. "return exact error messages and line references"), not broad analysis.

Worker outputs should be compact and structured. Do not return raw logs or large file dumps to the orchestrator.

### Bounded delegation rules

- keep delegation one level deep unless gh-aw explicitly supports and validates deeper topology
- avoid recursive or open-ended sub-agent fan-out
- cap per-run worker fan-out so high-volume events cannot trigger runaway cost
- stop early with `noop` or safe output when a cheap worker can confidently classify a known/duplicate/stale/low-value case

See also: [token-optimization.md](token-optimization.md) and [workflow-patterns.md](workflow-patterns.md).

---

## Full Example

```markdown
---
engine: copilot
tools:
  github:
    mode: gh-proxy
  cli-proxy: true
  bash:
    - "cat *"
    - "ls *"
---

# PR Review Assistant

1. Use the `diff-explainer` agent to produce a plain-English summary of the
   diff for PR #${{ github.event.pull_request.number }}.
2. Post the summary as a PR comment.

## agent: `diff-explainer`
---
description: Produces a plain-English summary of a pull request diff
model: small
---
You receive a unified diff. Describe each changed file in one sentence,
focusing on *what changed* and *why it matters*. Ignore formatting-only
changes. Return a bulleted list, one bullet per file.
```

---

## Limitations

- gh-aw does not interpret sub-agent `engine:`, `tools:`, `network:`, or `mcp-servers:` fields as workflow configuration. Preserved fields are interpreted by the engine, if supported.
- Sub-agents cannot define their own safe-output jobs.
- Sub-agent blocks must appear in the main workflow file body; they are not resolved inside imported shared files.
