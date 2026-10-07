# ADR-66648: Require Terminal Stdin for Interactive Prompts

**Date**: 2026-10-07
**Status**: Draft
**Deciders**: pelikhan [TODO: verify full decider list]

---

### Context

`pkg/console` exposes interactive prompts (`ConfirmAction`, `ShowInteractiveList`, `PromptSecretInput`, and the `huh`-based form runner). Until now these helpers used `tty.IsStderrTerminal()` as a single proxy for "we are interactive": when stderr was not a terminal they silently fell back to reading plain text from `os.Stdin`. Stdin and stderr are independent file descriptors, so in CI, piped, or redirected invocations (`echo y | gh aw ...`, `gh aw ... < file`) the fallback would consume whatever happened to be on stdin and treat it as a deliberate user answer. That makes destructive confirmations answerable by accident and makes non-interactive runs behave non-deterministically. The package also has a WASM build that must keep compiling and continue reporting interactivity as unsupported. This change addresses #66574.

### Decision

We will detect stdin's terminal state explicitly and require it before any prompt reads user input. A new `tty.IsStdinTerminal()` is added for both native and WASM builds (WASM always reports non-terminal), and every interactive entry point in `pkg/console` checks it first: when stdin is not a TTY the call returns a clear error (`interactive input not available (stdin is not a TTY)`) instead of reading piped data. The `huh` form path additionally continues to require a terminal stderr for rendering; the text fallbacks remain available when stdin is a terminal but stderr is not. The primary driver is correctness and safety of confirmation prompts, not ergonomics.

### Alternatives Considered

#### Alternative 1: Keep the stderr-only check and accept piped stdin as valid input

This is the pre-change behaviour and required no code at all; it also lets scripts "answer" prompts by piping `y`. It was rejected because the stderr check does not describe stdin at all, so the fallback reads arbitrary piped content — including unrelated data a user redirected for another purpose — as a confirmation answer. Treating that as consent is unsafe for destructive operations.

#### Alternative 2: Add an explicit non-interactive flag (e.g. `--yes` / `--non-interactive`) instead of TTY detection

Callers would declare intent explicitly and prompts would error only when the flag is absent and input is unavailable. This was a close call and remains complementary rather than contradictory, but it was not chosen here because it requires touching every command surface that can prompt, does not fix the underlying incorrect TTY proxy inside `pkg/console`, and leaves the accidental-stdin-consumption bug in place for callers that forget the flag.

### Consequences

#### Positive
- Interactive prompts can no longer consume piped or redirected stdin as if it were a deliberate user answer, removing an accidental-confirmation hazard for destructive commands.
- Failures are explicit and self-describing (`stdin is not a TTY`) instead of silently succeeding with whatever byte stream was attached, which makes CI and script failures diagnosable.
- `tty` gains a stdin accessor symmetrical with the existing stdout/stderr helpers, so future callers have one correct way to ask the question on both native and WASM builds.

#### Negative
- This is a behaviour change for any existing script or workflow that answered prompts by piping input (`echo y | gh aw ...`); those invocations now fail with an error and must be reworked around a non-interactive flag or code path.
- The confirm/list text fallbacks become reachable in a narrower set of environments (terminal stdin, non-terminal stderr), so that path gets less real-world exercise while still needing to be maintained.
- Tests can no longer drive the prompt helpers through an `os.Stdin` pipe, so the existing table-driven coverage of the text fallbacks via the public entry points was replaced with assertions that the non-TTY case errors; the fallback parsing itself must be covered through the internal helpers instead.

#### Neutral
- `pkg/tty/tty.go` and `pkg/tty/tty_wasm.go` both gain `IsStdinTerminal()`, and the `pkg/tty` and `pkg/console` README API tables were updated in the same change to document the new contract.
- Call sites in `confirm.go`, `input.go`, `list.go`, and `prompt_form.go` each perform their own guard rather than sharing a single wrapper, so a future helper could centralise the check without changing the decision.
- Behaviour on WASM is unchanged in effect: `IsStdinTerminal()` returns false there, which matches the existing "interactivity unsupported" reporting.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
