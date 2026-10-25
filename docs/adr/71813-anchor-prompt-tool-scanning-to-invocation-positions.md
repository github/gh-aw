# ADR-71813: Anchor Prompt Tool Scanning to Invocation Positions and Track Fences Structurally

**Date**: 2026-10-25
**Status**: Draft
**Deciders**: pelikhan (PR author); reviewer acceptance has not been recorded.

---

### Context

The workflow compiler runs a heuristic scanner (`pkg/workflow/prompt_tool_validation.go`) over prompt markdown. It warns when a prompt tells the agent to use an MCP tool or shell command that the workflow's `tools:` configuration does not allow. The scanner used an unanchored regex that matched `name(arg)` anywhere in an instruction. As a result, ordinary prose such as `file(s)` and `workflow(s)` was read as an MCP call like `file(s)`. Fence tracking was also too simple. Any line that started with ```` ``` ```` or `~~~` toggled fence state. That broke on longer fences and on same-length nested fences inside Markdown output templates. Dockerfile `RUN curl ...` examples were then scanned as shell instructions. Together these issues produced six confirmed false-positive warnings across three workflows. Because the warnings are advisory, false positives make users trust them less.

### Decision

We will recognize qualified tool references (`mcp__server__tool` and `server(tool)`) only at the start of the instruction target. Further references are accepted only after explicit list separators (`,`, `and`, `or`, with optional backticks). Scanning stops at the first non-list token, so we no longer search all prose that follows. We will also track complete fence delimiters, including the full run of backticks or tildes. A fence closes only on a valid closing fence. Inside `markdown`/`md` fences, a same-length fence with an info string opens a nested example that is skipped until it closes. The main driver is precision: these warnings are only useful if they rarely fire on harmless prose or example content. Tool permissions and transport checks stay unchanged.

Parenthesized `(s)` is treated as ambiguous pluralization and ends qualified-tool list scanning; an actual tool named `s` can still be required using `mcp__server__s`. Markdown template language identifiers are recognized case-insensitively, including `Markdown` and `MD`.

### Alternatives Considered

#### Alternative 1: Keep unanchored matching and add a stop-list of plural/prose tokens

An exclusion list for patterns such as `(s)`, `(es)`, or known nouns like `file` and `workflow` would be a smaller change, but it would not distinguish invocation positions from arbitrary prose or fix fence tracking. Narrow filtering of plural suffixes can complement anchored matching when prose occurs at an invocation position; it is not a substitute for anchoring.

#### Alternative 2: Use a full CommonMark parser for fences and instruction extraction

A CommonMark parser such as goldmark could provide standard fence handling and structured access to inline code and list items. This is a design alternative, not a documented experiment: no parser prototype or comparative evaluation is recorded for this change. The selected approach retains the existing line-oriented scanner rather than introducing AST-based instruction extraction. It also deliberately handles same-length nested fences in `markdown` output templates, a non-CommonMark convention that would still require custom handling with a strict parser.

#### Alternative 3: Suppress or downgrade warnings for affected workflows

We could have made the three affected workflows quiet, for example with per-workflow opt-outs or rewritten prose. That would hide the symptom without fixing the scanner, and every new workflow with similar prose would hit the same false positives.

### Consequences

#### Positive
- Removes all six confirmed false-positive warnings across the three affected workflows, with no change to tool permission or transport validation.
- Explicit lists of qualified tools (`` `github(issue_read)`, `inventory(lookup)`, and `mcp__github__list_issues` ``) are still detected. Regression tests cover these lists, alternatives (`or`), and trailing prose.
- Fence handling now follows CommonMark length rules more closely. A longer fence is not closed by a shorter one, and a fence line with an info string never closes a fence.

#### Negative
- Qualified tool references that appear mid-sentence and not at an invocation position (for example, "Use the issue tracker via github(issue_read)") are no longer detected. This creates possible false negatives where true positives used to be found.
- The nested-fence rule (a same-length fence with an info string inside a `markdown` fence) is a custom heuristic, not standard Markdown. Prompts that rely on other nesting conventions may still be misparsed.
- The scanner state machine gains two fields (`markdownFence`, `nestedFence`) and more branches, which makes it harder to reason about.

#### Neutral
- The scanner is still heuristic and line-oriented. This decision refines it rather than replacing it with a structural parser.
- Checkout exemptions, template rendering, and other warning clusters are out of scope and unchanged.

---

*This ADR remains Draft pending reviewer acceptance.*
