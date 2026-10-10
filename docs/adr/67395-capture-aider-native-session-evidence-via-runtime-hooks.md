# ADR-67395: Capture Aider Native Session Evidence via Runtime Hooks

**Date**: 2026-10-10
**Status**: Draft
**Deciders**: pelikhan (PR author), gh-aw maintainers

---

### Context

The Aider engine adapter in `.github/workflows/shared/aider.md` builds an inline Python entrypoint that drives `aider` in-process and emits canonical `gh-aw` session events (`session.init`, `assistant.message`, `tool.*`, `session.result`). Until now that adapter only observed Aider's *display* layer (`InputOutput.assistant_output`), so the recorded transcript was the pretty-printed terminal rendering rather than the provider reply. Reasoning content, structured refusals, `finish_reason == "length"` truncation, provider response identifiers, and exact token/cost accounting were all available from the underlying LiteLLM completion object but were discarded before reaching `agent-session.jsonl` / `aw_session.jsonl`. Separately, the shared log-parser dispatch in `actions/setup/js/engine_log_parser.cjs` could bypass Aider's declared parser, so Actions-side rendering and CLI-side reconstruction of the same run could disagree, and historical runs that failed during provider startup (e.g. rate limits) produced no session evidence at all.

### Decision

We will capture Aider session evidence from Aider's **native runtime objects** by monkey-patching `Coder.show_send_output` and `Coder.calculate_and_show_tokens_and_cost` inside the workflow entrypoint, instead of inferring it from formatted terminal output. The hook reads the raw completion (`choices[].message.content`, `reasoning_content`/`reasoning`, `refusal`, `finish_reason`, `id`, `model`, `created`, `usage`) and emits distinct `assistant.message`, `assistant.reasoning` and `assistant.refusal` events, preserving empty/null/false/zero values verbatim; `assistant_output` is suppressed while a hooked response is being rendered so the display path no longer duplicates provider text. Observed per-response usage is normalised into the canonical accounting schema and summed into a single terminal `session.result`, and one native cost snapshot is published only when Aider itself reports a priced model. We will also prefer the engine's declared parser in shared dispatch and recover attributable historical startup failures from raw logs as `session.error` entries, without fabricating counts, identifiers, conversation content or terminal success.

Refusal-bearing and content-filtered responses emit `assistant.refusal` instead of an `assistant.message` answer. Events interpreted by downstream session, workflow, and guardrail consumers require explicit `sourceEngine: "aider"` attribution because shell stdout shares the log stream; only opaque unknown extension types may remain source-less. Empty-output engine attribution uses runner metadata or the configured engine, not agent-produced `workflow.info` events. Empty display-layer warning calls do not create assistant messages or increment turns; explicitly observed empty provider replies remain preserved.

### Alternatives Considered

#### Alternative 1: Keep parsing Aider's rendered terminal output

Continue to derive all events from `InputOutput.assistant_output` and stdout text, extending the regex/heuristic layer to recover reasoning, refusals and token counts. This requires no coupling to Aider internals and keeps working across Aider versions that change their Python API. It was rejected because the information simply is not present in the rendered output: reasoning and refusal fields never reach the terminal, `finish_reason` truncation is invisible, and token/cost figures would have to be re-tokenised or scraped from a human-oriented usage banner — i.e. fabricated or approximate accounting, which the gh-aw session schema treats as unacceptable.

#### Alternative 2: Use Aider's `--analytics`/log file or a LiteLLM callback instead of patching `Coder`

Aider can emit analytics events, and LiteLLM supports success/failure callbacks that receive the full response object. This would avoid depending on `Coder` method signatures. It was rejected because analytics is explicitly disabled in the pinned profile (`--analytics-disable`) and is aggregate/privacy-oriented rather than per-response, while a LiteLLM callback sits below Aider and would lose the coder-level context (turn boundaries, `usage_report`, `total_cost`, model pricing info) needed to attribute events to a session and to publish a single consistent terminal result. It remains a plausible future fallback if the `Coder` API proves unstable.

#### Alternative 3: Reconstruct the session entirely post-hoc in the Node log parser

Leave the Python entrypoint unchanged and do all enrichment in `engine_log_parser.cjs` after the run. Rejected for the same reason as Alternative 1 — the raw log does not contain the native fields — although this PR does adopt a narrow version of it for historical runs: startup-framed `litellm.*Error` diagnostics are recovered as `session.error` only, with no attempt to rebuild conversation or accounting.

### Consequences

#### Positive

- Session records now carry provider-truth evidence: unformatted reply text, separate reasoning, structured refusals, length-limited partials and native response/model identifiers.
- Token and cost accounting is exact and observed rather than estimated, and is published once in a terminal `session.result` with an explicit `process.exit`/exception-derived status.
- Actions-side and CLI-side reconstruction agree because shared dispatch prefers the engine's declared parser; unknown canonical extension events are retained rather than dropped.
- Historical startup failures (e.g. provider rate limits) that previously produced zero evidence now yield attributable `session.error` diagnostics.

#### Negative

- The adapter is now coupled to private-ish Aider internals (`Coder.show_send_output`, `Coder.calculate_and_show_tokens_and_cost`, `coder.usage_report`, `coder.total_cost`). An upstream refactor in a future Aider release can silently degrade capture, so the engine version pin (`aider 0.86.2`) becomes load-bearing.
- The inline Python entrypoint grew substantially (~200 added lines embedded in `.github/workflows/shared/aider.md` and replicated into five generated `.lock.yml` files), which is harder to read, lint and unit-test than ordinary Go/JS source.
- Capture is only validated for the pinned non-streaming profile (`--no-stream`); streaming responses are outside response-hook coverage.
- The historical-log recovery path is heuristic (banner + `Repo-map:` preamble + `litellm.*Error` framing) and will not generalise to arbitrary unframed failure text.

#### Neutral

- Evidence for reasoning, refusal, partial replies and edge values (empty/null/false/zero) is synthetic in `pkg/workflow/aider_session_capture_test.go`; only smoke-success and downstream-failure paths are backed by sampled real runs under `pkg/workflow/testdata/aider_session/`.
- Turn counting moves from the display callback to the completion hook, so `numTurns` now reflects provider responses rather than rendered messages — comparable in practice but not byte-identical to prior runs.
- All Aider-consuming workflows must be recompiled (`make recompile`) whenever the shared adapter changes, since the entrypoint is inlined into each lock file.
- The shared-dispatcher change is deliberately kept as a separate commit from the Aider-owned changes to keep the engine-agnostic prerequisite reviewable on its own.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
