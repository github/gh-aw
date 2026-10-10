// Sanitized excerpts of agent/agent-stdio.log from existing Smoke Kiro runs:
// success: https://github.com/github/gh-aw/actions/runs/37862725262 (agent artifact 11587670588)
// failure: https://github.com/github/gh-aw/actions/runs/37549851664 (agent artifact 11452383642)
// Text/commands/paths are replacements; CLI version, tool labels/statuses,
// multiline/truncated preview shapes, ordering, and orphan failures are retained.
// Both processes exited zero; the failed workflow's log-redaction step failed.
// These excerpts do not expose tool outputs, reasoning, refusals, or usage.
// Truncated command previews retain the observed 200-byte display boundary.

const preview = content => content.padEnd(197, "x") + "...";

const success = `kiro-cli 2.27.1
[kiro-harness] Kiro CLI verification completed in 10ms
[kiro-harness] Kiro CLI execution started

[tool] Running: printf 'example'
[tool] status: Completed

[tool] Running: make example
[tool] Running: ${preview("node -e '\nconst example = false;\nconsole.log(example);\nconsole.log(\"truncated preview")}
[tool] status: Completed
[tool] status: Completed

[tool] Searching symbols: exampleSymbol
[tool] status: Completed

[tool] AWS: sts get-caller-identity
[tool] status: Completed
Checking the example.Example inspected.

Results:
- Example: PASS

[tool] Running: ${preview("cat <<'EOF' > example.md\n## Example\n\nThe example document is truncated in the preview")}
[tool] status: Completed
[tool] Running: cat example.md
[tool] status: Completed
Example complete.

No additional action is needed.
[kiro-harness] Kiro CLI execution completed in 137243ms
[kiro-harness] Cleaned up Kiro CLI installation; total duration=139617ms; exit code=0
[entrypoint] Cleanup infrastructure
Process exiting with code: 0
`;

const failure = `kiro-cli 2.27.1
[kiro-harness] Kiro CLI verification completed in 10ms
[kiro-harness] Kiro CLI execution started

[tool] Running: printf 'one'
[tool] Running: printf 'two'
[tool] Running: ${preview("node -e 'console.log(\"truncated preview")}
[tool] status: Completed
[tool] status: Completed
[tool] status: Completed

[tool] Running: make example
[tool] Searching symbols: exampleSymbol
[tool] status: Completed
[tool] status: Completed

[tool] status: Failed

[tool] status: Failed
The example check failed. No substitute was attempted.

[tool] Running: ${preview("cat <<'EOF' > example.md\n## Example results\n\n| Check | Result |\n|---|---|\n| Example | FAIL |\nThe remaining document is truncated in the preview")}
[tool] status: Completed
[tool] Running: cat example.md
[tool] status: Completed
Example recorded.

- Overall: FAIL
[kiro-harness] Kiro CLI execution completed in 100996ms
[kiro-harness] Cleaned up Kiro CLI installation; total duration=103220ms; exit code=0
Process exiting with code: 0
`;

module.exports = { success, failure };
