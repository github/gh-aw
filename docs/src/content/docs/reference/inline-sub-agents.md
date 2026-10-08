---
title: Inline Sub-Agents
description: Define sub-agents directly inside a workflow markdown file using a level-2 heading delimiter.
sidebar:
  order: 645
---

An inline sub-agent is a named agent definition embedded directly in a workflow markdown file. Instead of creating a separate agent file, you define the agent's frontmatter and instructions in a dedicated section of the same workflow file.

## Syntax

Start a sub-agent block with a level-2 heading in the following form:

```markdown
## agent: `name`
```

When a matching `## end agent: \`name\`` marker is present, the block continues until that marker even if the block contains other `##` headings. Without an explicit end marker, the block continues until the next `##` heading or end of file.

### Explicit end marker

Add a closing heading to bound the block explicitly instead of relying on the next `##` heading or EOF:

```markdown
## agent: `name`
...
## end agent: `name`
```

```markdown
## agent: `file-summarizer`
---
description: Summarizes files
---
You are a file summarization assistant.
## end agent: `file-summarizer`
```

Use the explicit end marker when the block appears in the middle of a document — for example via an [import](/gh-aw/reference/imports/) — or when the sub-agent instructions themselves need `##` headings.

Without an explicit end marker, the block still ends at the next `##` heading or EOF.

When a sub-agent block is brought in via `{{#runtime-import ...}}` and has no explicit end marker, the runtime import resolver automatically inserts one at the implicit boundary. This prevents the imported block from swallowing later content, such as another import or the rest of the workflow body. An explicit end marker is still recommended for clarity.

### Name constraints

Names must start with a lowercase letter (`a–z`) and may contain only `a–z`, `0–9`, `_`, and `-`. Examples: `file-summarizer`, `code_reviewer`, `pr-analyst`.

### Structure

Each sub-agent block contains optional YAML frontmatter wrapped in `---` delimiters, followed by the agent instructions.

```markdown
## agent: `file-summarizer`
---
model: claude-haiku-4.5
description: Summarizes the content of a file in a few concise sentences
---
You are a file summarization assistant. When given a file path, read the file
and return a brief summary (2–4 sentences) describing its purpose and key
contents. Be concise and factual.
```

## Frontmatter fields

| Field | Required | Description |
|---|---|---|
| `model` | No | AI model to use (e.g. `claude-haiku-4.5`). Defaults to the parent workflow's model. |
| `description` | For Pi | Short description of the sub-agent's purpose. |
| `tools` | No | Pi accepts a comma-separated string or list of tool names, restricted by the parent workflow's tool policy. |

## Runtime behavior

At runtime, each inline sub-agent block is extracted to a location that the AI engine can access natively. The destination path depends on the engine:

| Engine | Destination path |
|--------|-----------------|
| `copilot` | `.github/agents/<name>.agent.md` |
| `claude` | `.claude/agents/<name>.md` |
| `codex` | `.codex/agents/<name>.md` |
| `gemini` | `.gemini/agents/<name>.md` |
| `pi` | `.pi/agents/<name>.md`, restored into the managed Pi user scope |

Pi exposes a managed `subagent` tool in the built-in CLI, SDK, and RPC modes, including bare mode. It accepts one declared agent and task: `{"agent":"file-summarizer","task":"Summarize README.md"}`. Model aliases such as `small` resolve against the parent's provider inventory; cross-provider delegation is rejected. See [Pi sub-agents](/gh-aw/engines/pi/#sub-agents) for controls and limitations.

To use a sub-agent, instruct the parent workflow's prompt to invoke it by name:

```aw wrap
## Test Requirements

15. **Sub-Agent Testing**: Use the `file-summarizer` sub-agent to summarize the
    file `.github/workflows/smoke-copilot.md`. Verify the sub-agent returns a
    brief summary (2–4 sentences). Mark this test as ❌ if the sub-agent is
    unavailable or returns an error.
```

## Example: File Summarization Sub-Agent

The following excerpt shows a full workflow that defines and uses an inline sub-agent.

```aw wrap
---
on:
  workflow_dispatch:

engine: copilot
---

# File Summary Task

Use the `file-summarizer` sub-agent to summarize `README.md` and add a comment
to the current pull request with the result.

## agent: `file-summarizer`
---
model: claude-haiku-4.5
description: Summarizes the content of a file in a few concise sentences
---
You are a file summarization assistant. When given a file path, read the file
and return a brief summary (2–4 sentences) describing its purpose and key
contents. Be concise and factual.
```

The sub-agent block at the bottom is extracted before the workflow runs and has no effect on the parent workflow's instructions.

## Example: Multiple Sub-Agents in One Workflow

A workflow file may contain multiple sub-agent blocks. Each starts with `## agent: \`name\`` and ends at a matching `## end agent: \`name\`` marker, the next `##` heading, or EOF.

```aw wrap
## agent: `summarizer`
---
model: claude-haiku-4.5
description: Summarizes files concisely
---
Summarize the given file in 2–4 sentences.

## agent: `reviewer`
---
model: claude-sonnet-4.5
description: Reviews code for quality issues
---
Review the given code for bugs, style issues, and potential improvements.
```

## Learn More

- [Importing Copilot Agent Files](/gh-aw/reference/copilot-custom-agents/) for agents stored in `.github/agents/`
- [DeterministicOps](/gh-aw/patterns/deterministic-ops/) for combining deterministic steps with AI reasoning
- [Markdown](/gh-aw/reference/markdown/) for workflow markdown syntax
- [Workflow Structure](/gh-aw/reference/workflow-structure/) for overall file organization
- [Frontmatter](/gh-aw/reference/frontmatter/) for YAML configuration options
