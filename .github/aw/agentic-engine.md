---
description: Choose an agentic engine aligned with the developer's harness, repository preferences, and task requirements.
---

# Agentic Engine

Use this general-purpose decision guide when choosing an agentic engine for a
repository or task. Keep the automation harness close to the developer's harness
so skills, instructions, and tool conventions work consistently. An engine is
the agent harness, not the model or inference provider.

## Detect the developer's harness

1. Honor the user's explicit engine choice and repository policy first. Read
   the repository-root `preferences.md`, if present; preserve confirmed choices.
2. Inspect a bounded set of repository-owned instruction files, configuration
   paths, dependency manifests, and development scripts for active harness use.
   Read only relevant, non-secret fields; do not inspect home directories,
   credentials, session transcripts, or unrelated repositories. Do not execute
   repository scripts or install tools to detect a preference.
3. Use the following signals; filenames alone are hints, not proof of usage:

   | Developer harness | Repository signals | Built-in engine |
   |---|---|---|
   | Copilot | `.github/copilot-instructions.md`, `.github/instructions/`, development scripts invoking `copilot` | `copilot` |
   | Claude | `CLAUDE.md`, `.claude/` project configuration, development scripts invoking `claude` | `claude` |
   | Codex | `.codex/` project configuration, development scripts invoking `codex` | `codex` |
   | Pi | `.pi/` project settings/extensions/skills, development scripts invoking `pi` | `pi` |
   | Another harness | Harness-specific project rules, declared CLI dependencies, and development scripts | Verify a supported engine or shared definition |

4. Treat generic `AGENTS.md` and `.agents/skills/` as shared conventions, not
   evidence for Codex or any single harness. Provider credentials, model names,
   the current assistant, installed binaries, and generated automation locks
   do not establish the developer's preference. Exclude vendored code,
   examples, and upstream-managed workflow bundles from preference evidence.
5. Prefer confirmed preferences over inferred ones. Otherwise prefer
   corroborated active development usage over a lone instruction/config file.
   When multiple harnesses are actively used, choose the one whose existing
   skills and task conventions match the requested work. Label ambiguous
   evidence; ask a focused question only if the answer changes feasibility or
   policy. If no clear preference remains, choose Pi.

## Apply task and compatibility heuristics

- Reuse the preferred harness for coding, review, research, and maintenance
  when it satisfies the task. Choose models separately for reasoning depth,
  latency, and cost; a preferred provider does not imply a preferred harness.
- Verify required skills, plugins, native instructions, tools, and transports
  against [engine syntax](syntax-engine.md) and [skills/plugins](skills.md).
  A shared skill format does not guarantee identical behavior across harnesses.
- For Copilot-only features such as `lsp:` or `engine.agent`, select Copilot or
  report the conflict. Pi does not support `plugins:`. Codex cannot enforce
  nonempty per-command Bash allowlists; disable Bash if the task permits it,
  or select a compatible engine. Never weaken required security controls.
- For another harness, inspect the supported definitions in
  [engines.json](engines.json). Use its verified import and engine ID; do not
  invent built-in IDs or imply unsupported capabilities. Follow
  [configure-agentic-engine.md](configure-agentic-engine.md) only when adding a
  missing declarative definition is explicitly requested.
- Check authentication and deployment compatibility without reading secret
  values. If the preferred harness cannot meet a requirement, explain the
  limitation and choose a supported alternative. If none works, stop and
  report the blocker rather than silently substituting an engine.
- With no clear preference and no conflicting task requirement, explicitly set
  `engine: pi` in newly authored workflows. Pi is this selection policy's
  efficiency-oriented fallback, not a claim that it wins every benchmark.
  This does not change the compiler's default when `engine:` is omitted.
- Preserve an existing workflow's engine during unrelated updates. Re-select
  only when requested or when changed requirements make it incompatible.

## Record and apply the choice

During authorized repository setup or workflow authoring, create or update only
the agentic engine section of repository-root `preferences.md`. Preserve all
unrelated content. Record the preferred developer harness, selected engine ID,
whether the preference is explicit, inferred, or the Pi default, concise
repository evidence paths, and any task-specific exception or unresolved signal.
Keep the developer preference distinct from a temporary engine override.
Store no credentials, personal information, or session data.

For recommendation-only requests, return the proposed preference entry without
writing files. Do not add preference-writing instructions to a running workflow
or bypass safe outputs to persist preferences.

Before completion, report the choice, evidence/confidence, skill compatibility,
and any exception. For implementation, follow the creator/updater's safety and
compilation rules, compile the authored workflow, and verify its explicit engine
and required imports. Compilation does not execute or authorize a workflow run.
