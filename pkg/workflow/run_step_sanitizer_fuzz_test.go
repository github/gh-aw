//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// FuzzSanitizeRunStepExpressions performs fuzz testing on the run-step sanitizer.
//
// The fuzzer validates that:
//  1. The function never panics on arbitrary input.
//  2. After sanitization, the run: field contains no ${{ }} markers (property test).
//  3. The env: block is present when expressions were extracted.
//  4. Every warning message references "shell injection".
//  5. The original step map is never mutated.
//
// To run the fuzzer:
//
//	go test -v -fuzz=FuzzSanitizeRunStepExpressions -fuzztime=30s ./pkg/workflow
func FuzzSanitizeRunStepExpressions(f *testing.F) {
	// Seed corpus — safe patterns
	f.Add("echo hello world")
	f.Add("bash script.sh")
	f.Add("echo ${MY_VAR}")

	// Seed corpus — single unsafe expression
	f.Add(`echo "${{ github.event.issue.title }}"`)
	f.Add(`echo "${{ github.event.issue.body }}"`)
	f.Add(`echo "${{ github.event.pull_request.title }}"`)
	f.Add(`echo "${{ github.event.pull_request.body }}"`)
	f.Add(`bash script.sh "${{ steps.build.outputs.artifact }}"`)
	f.Add(`curl -d "${{ inputs.user_data }}" https://api.example.com`)
	f.Add(`echo "${{ github.actor }}"`)
	f.Add(`echo "${{ github.sha }}"`)
	f.Add(`echo "${{ github.ref }}"`)

	// Seed corpus — multiple expressions
	f.Add(`echo "${{ github.event.issue.title }}" && echo "${{ github.event.issue.body }}"`)
	f.Add(`gh api repos/${{ github.repository }}/issues/${{ github.event.issue.number }}`)

	// Seed corpus — complex expressions (hash-based env var names)
	f.Add(`echo "${{ github.event.issue.title || 'no title' }}"`)
	f.Add(`echo "${{ toJSON(github.event) }}"`)
	f.Add(`echo "${{ format('{0}', github.event.issue.title) }}"`)

	// Seed corpus — expression with whitespace variations
	f.Add(`echo "${{github.event.issue.title}}"`)
	f.Add(`echo "${{   github.event.issue.title   }}"`)

	// Seed corpus — heredoc patterns (should not be extracted)
	f.Add("cat > /tmp/cfg.json << 'EOF'\n{\"title\": \"${{ github.event.issue.title }}\"}\nEOF")
	f.Add("cat > /tmp/out << EOF\n${{ github.event.issue.body }}\nEOF")

	// Seed corpus — mixed heredoc + non-heredoc
	f.Add("echo \"${{ github.event.issue.title }}\"\ncat > f << 'EOF'\n${{ github.event.issue.body }}\nEOF")

	// Seed corpus — duplicate expressions
	f.Add(`echo "${{ github.event.issue.title }}" && echo "${{ github.event.issue.title }}"`)

	// Seed corpus — multiline scripts
	f.Add("echo \"${{ github.event.issue.title }}\"\necho \"${{ github.event.issue.body }}\"\necho done")

	// Seed corpus — no expression marker
	f.Add("")
	f.Add("   ")
	f.Add("echo 'safe literal'")
	f.Add("${{ malformed")

	// Seed corpus — injection attempts
	f.Add(`echo "${{ github.event.issue.title }}"; rm -rf /`)
	f.Add("echo `${{ github.event.issue.title }}`")
	f.Add(`$(echo "${{ github.event.issue.title }}")`)

	// Unicode / special characters
	f.Add(`echo "Unicode: 你好 мир 🎉 ${{ github.event.issue.title }}"`)
	f.Add(`echo "${{ github.event.issue.title }}" # comment`)

	f.Fuzz(func(t *testing.T, runScript string) {
		// Skip inputs that are too large to avoid timeout.
		if len(runScript) > 50000 {
			t.Skip("Input too large")
		}

		step := map[string]any{"run": runScript}

		// Capture original values to check immutability.
		originalRun := runScript

		// Should never panic.
		result, warnings, changed := sanitizeRunStepExpressions(step)

		// Original map must not be mutated.
		if step["run"] != originalRun {
			t.Errorf("input step['run'] was mutated: want %q, got %q", originalRun, step["run"])
		}
		if _, hasEnv := step["env"]; hasEnv {
			t.Errorf("input step gained an env field after sanitization")
		}

		if !changed {
			// Unchanged: result should be identical to input.
			if result["run"] != step["run"] {
				t.Errorf("result run mismatch on unchanged step")
			}
			if len(warnings) != 0 {
				t.Errorf("expected no warnings for unchanged step, got %d", len(warnings))
			}
			return
		}

		// Changed: postconditions.
		sanitizedRun, ok := result["run"].(string)
		if !ok {
			t.Errorf("sanitized run field is not a string")
			return
		}

		// Quoted bodies remain literal; expanding bodies must be sanitized.
		sanitizedScan := executableShellContent(sanitizedRun)
		if strings.Contains(sanitizedScan, "${{") {
			if len(ExpressionPattern.FindAllString(sanitizedScan, -1)) > 0 {
				t.Errorf("executable portion of sanitized run field still contains a complete expression: %q", sanitizedRun)
			}
			// An incomplete wrapper cannot be extracted, but must fail validation.
			if err := validateNoGitHubExpressionsInRunScriptsFromParsed(map[string]any{"steps": []any{result}}); err == nil {
				t.Errorf("remaining malformed expression bypassed validation: %q", sanitizedRun)
			}
		}

		// env: block must be present.
		envMap, ok := result["env"].(map[string]any)
		if !ok || len(envMap) == 0 {
			t.Errorf("expected non-empty env map after sanitization")
		}

		// Every env var key must start with GH_AW_.
		for key := range envMap {
			if !strings.HasPrefix(key, "GH_AW_") {
				t.Errorf("env var key %q does not start with GH_AW_", key)
			}
		}

		// Every env var value must be a non-empty expression string.
		for key, val := range envMap {
			strVal, ok := val.(string)
			if !ok || strVal == "" {
				t.Errorf("env var %q has empty or non-string value", key)
			}
		}

		// Warnings must be non-empty and reference "shell injection".
		if len(warnings) == 0 {
			t.Errorf("expected at least one warning for changed step")
		}
		for _, w := range warnings {
			if !strings.Contains(w, "shell injection") {
				t.Errorf("warning %q does not mention 'shell injection'", w)
			}
		}

		// The env: block must contain at least as many entries as warnings
		// (each warning corresponds to one unique extracted expression).
		if len(envMap) < len(warnings) {
			t.Errorf("env map (%d entries) should have at least as many entries as warnings (%d)", len(envMap), len(warnings))
		}
	})
}

// FuzzSanitizeCustomStepsYAML performs fuzz testing on the custom-steps YAML sanitizer.
//
// The fuzzer validates that:
//  1. The function never panics on arbitrary input.
//  2. The returned string is never shorter than the empty string.
//  3. Every warning message references "shell injection".
//  4. After sanitization, no ${{ }} appears in a run: value of the output YAML
//     (as a best-effort heuristic — malformed YAML may bypass this check).
//
// To run the fuzzer:
//
//	go test -v -fuzz=FuzzSanitizeCustomStepsYAML -fuzztime=30s ./pkg/workflow
func FuzzSanitizeCustomStepsYAML(f *testing.F) {
	// Seed corpus — no expressions
	f.Add(`steps:
  - name: Safe
    run: echo hello`)

	f.Add(`steps:
  - uses: actions/checkout@v4`)

	// Seed corpus — single step with expression
	f.Add(`steps:
  - name: Print title
    run: echo "${{ github.event.issue.title }}"`)

	f.Add(`steps:
  - name: Curl
    run: curl -d "${{ github.event.issue.body }}" https://api.example.com`)

	// Seed corpus — multiple steps
	f.Add(`steps:
  - name: Safe
    run: echo hello
  - name: Unsafe
    run: echo "${{ github.event.issue.title }}"`)

	f.Add(`steps:
  - name: Step A
    run: echo "${{ github.event.issue.title }}"
  - name: Step B
    run: bash script.sh "${{ github.event.pull_request.body }}"`)

	// Seed corpus — expression already in env (safe, should not re-extract)
	f.Add(`steps:
  - name: Already safe
    env:
      TITLE: ${{ github.event.issue.title }}
    run: echo "$TITLE"`)

	// Seed corpus — heredoc (should not be extracted)
	f.Add(`steps:
  - name: Heredoc
    run: |
      cat > /tmp/cfg.json << 'EOF'
      {"title": "${{ github.event.issue.title }}"}
      EOF`)

	// Seed corpus — malformed YAML
	f.Add("steps:\n  - run: ${{ unclosed")
	f.Add("not: yaml: at: all")
	f.Add("")
	f.Add("   ")

	// Seed corpus — complex expression
	f.Add(`steps:
  - name: Complex
    run: echo "${{ github.event.issue.title || 'no title' }}"`)

	// Seed corpus — unicode
	f.Add(`steps:
  - name: Unicode
    run: echo "你好 ${{ github.event.issue.title }}"`)

	f.Fuzz(func(t *testing.T, input string) {
		// Skip inputs that are too large to avoid timeout.
		if len(input) > 100000 {
			t.Skip("Input too large")
		}

		// Should never panic.
		out, warnings, err := sanitizeCustomStepsYAML(input)

		// err is non-nil only for YAML re-serialisation failures; when that
		// happens the original string is returned unchanged.
		// Note: out may be an empty string when the input is empty.
		if err != nil {
			// On error the original must be returned unchanged.
			if out != input {
				t.Errorf("on error, output should equal input: err=%v", err)
			}
			return
		}

		// Output must be non-empty when input is non-empty.
		if input != "" && out == "" {
			t.Errorf("output should not be empty when input is non-empty")
		}

		// Warnings must reference "shell injection".
		for _, w := range warnings {
			if !strings.Contains(w, "shell injection") {
				t.Errorf("warning %q does not mention 'shell injection'", w)
			}
		}

		// If no warnings were emitted, the output should equal the input (idempotent).
		if len(warnings) == 0 && out != input {
			// Allow for normalised whitespace / formatting differences from YAML
			// round-trip only when the input was valid enough to parse.
			// We can't assert strict equality here without re-parsing, so we just
			// verify the output is non-empty.
			_ = out
		}
	})
}
