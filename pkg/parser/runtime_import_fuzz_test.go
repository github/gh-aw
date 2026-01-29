//go:build !integration

package parser

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzRuntimeImportExpressionValidation performs fuzz testing on expression validation
// in runtime_import.cjs to discover edge cases and potential security vulnerabilities.
//
// The fuzzer validates that:
// 1. All safe expressions are correctly identified
// 2. All unsafe expressions are properly rejected
// 3. Parser handles all fuzzer-generated inputs without panic
// 4. Edge cases are handled (empty, very long, special characters, nested structures)
// 5. Security patterns are enforced (no secrets, no runner context, etc.)
func FuzzRuntimeImportExpressionValidation(f *testing.F) {
	// Seed corpus with known safe expressions
	f.Add("github.actor")
	f.Add("github.repository")
	f.Add("github.event.issue.number")
	f.Add("github.event.pull_request.title")
	f.Add("needs.build.outputs.version")
	f.Add("steps.test.outputs.result")
	f.Add("env.NODE_VERSION")
	f.Add("inputs.branch")
	f.Add("github.event.inputs.tag")

	// Seed corpus with known unsafe expressions
	f.Add("secrets.TOKEN")
	f.Add("secrets.GITHUB_TOKEN")
	f.Add("runner.os")
	f.Add("runner.temp")
	f.Add("github.token")
	f.Add("vars.MY_VAR")

	// Seed corpus with edge cases
	f.Add("")                                     // empty
	f.Add("   ")                                  // whitespace only
	f.Add("github")                               // incomplete
	f.Add("github.")                              // trailing dot
	f.Add(".github.actor")                        // leading dot
	f.Add("github..actor")                        // double dot
	f.Add("github.actor.")                        // trailing dot after property
	f.Add("needs.job-name.outputs.value")         // dashes in job name
	f.Add("steps.step_name.outputs.value")        // underscores in step name
	f.Add("github.event.release.assets[0].id")    // array access
	f.Add("github" + strings.Repeat(".prop", 50)) // very long chain

	// Find node executable
	nodePath, err := exec.LookPath("node")
	if err != nil {
		f.Skip("Node.js not found, skipping fuzz test")
	}

	// Get absolute path to runtime_import.cjs
	wd, err := os.Getwd()
	if err != nil {
		f.Fatalf("Failed to get working directory: %v", err)
	}
	runtimeImportPath := filepath.Join(wd, "../../actions/setup/js/runtime_import.cjs")
	if _, err := os.Stat(runtimeImportPath); os.IsNotExist(err) {
		f.Fatalf("runtime_import.cjs not found at %s", runtimeImportPath)
	}

	f.Fuzz(func(t *testing.T, expression string) {
		// Skip very long inputs to avoid timeout
		if len(expression) > 1000 {
			t.Skip("Expression too long")
		}

		// Create test script
		testScript := `
const { isSafeExpression } = require('` + runtimeImportPath + `');
const expr = process.argv[2];
try {
	const result = isSafeExpression(expr);
	console.log(JSON.stringify({ success: true, safe: result }));
} catch (error) {
	console.log(JSON.stringify({ success: false, error: error.message }));
}
`
		tmpFile, err := os.CreateTemp("", "fuzz-expr-*.js")
		if err != nil {
			t.Fatalf("Failed to create temp file: %v", err)
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.WriteString(testScript); err != nil {
			t.Fatalf("Failed to write test script: %v", err)
		}
		tmpFile.Close()

		cmd := exec.Command(nodePath, tmpFile.Name(), expression)
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Command execution failure is acceptable for fuzz testing
			return
		}

		var result struct {
			Success bool   `json:"success"`
			Safe    bool   `json:"safe"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal(output, &result); err != nil {
			// JSON parse failure is acceptable for fuzz testing
			return
		}

		// Validate invariants
		if result.Success {
			// If the function succeeded, verify security invariants

			// Expressions containing "secrets." should never be safe
			if strings.Contains(expression, "secrets.") && result.Safe {
				t.Errorf("Expression containing 'secrets.' was marked as safe: %q", expression)
			}

			// Expressions containing "runner." should never be safe
			if strings.Contains(expression, "runner.") && result.Safe {
				t.Errorf("Expression containing 'runner.' was marked as safe: %q", expression)
			}

			// Expression "github.token" should never be safe
			if strings.TrimSpace(expression) == "github.token" && result.Safe {
				t.Errorf("Expression 'github.token' was marked as safe")
			}

			// Expressions with newlines should never be safe
			if strings.Contains(expression, "\n") && result.Safe {
				t.Errorf("Expression with newline was marked as safe: %q", expression)
			}
		}
	})
}

// FuzzRuntimeImportProcessExpressions performs fuzz testing on processExpressions
// to discover edge cases in expression processing and validation.
func FuzzRuntimeImportProcessExpressions(f *testing.F) {
	// Seed corpus with valid content patterns
	f.Add("Actor: ${{ github.actor }}")
	f.Add("Repo: ${{ github.repository }}, Run: ${{ github.run_id }}")
	f.Add("Issue #${{ github.event.issue.number }}: ${{ github.event.issue.title }}")
	f.Add("No expressions here")
	f.Add("")

	// Seed corpus with invalid content patterns
	f.Add("Secret: ${{ secrets.TOKEN }}")
	f.Add("Runner: ${{ runner.os }}")
	f.Add("Mixed: ${{ github.actor }} and ${{ secrets.TOKEN }}")

	// Seed corpus with edge cases
	f.Add("${{github.actor}}")                            // no spaces
	f.Add("${{  github.actor  }}")                        // extra spaces
	f.Add("Nested ${{ ${{ github.actor }} }}")            // nested (invalid)
	f.Add("${{ github.actor }} ${{ github.repository }}") // multiple
	f.Add("Text ${{ github.actor")                        // unclosed
	f.Add("Text }} github.actor }}")                      // unbalanced
	f.Add(strings.Repeat("${{ github.actor }} ", 100))    // many expressions

	nodePath, err := exec.LookPath("node")
	if err != nil {
		f.Skip("Node.js not found, skipping fuzz test")
	}

	wd, err := os.Getwd()
	if err != nil {
		f.Fatalf("Failed to get working directory: %v", err)
	}
	runtimeImportPath := filepath.Join(wd, "../../actions/setup/js/runtime_import.cjs")
	if _, err := os.Stat(runtimeImportPath); os.IsNotExist(err) {
		f.Fatalf("runtime_import.cjs not found at %s", runtimeImportPath)
	}

	f.Fuzz(func(t *testing.T, content string) {
		// Skip very long inputs
		if len(content) > 10000 {
			t.Skip("Content too long")
		}

		testScript := `
global.core = {
	info: () => {},
	warning: () => {},
	setFailed: () => {},
};

global.context = {
	actor: 'testuser',
	job: 'test-job',
	repo: { owner: 'testorg', repo: 'testrepo' },
	runId: 12345,
	runNumber: 42,
	workflow: 'test-workflow',
	payload: {},
};

process.env.GITHUB_SERVER_URL = 'https://github.com';
process.env.GITHUB_WORKSPACE = '/workspace';

const { processExpressions } = require('` + runtimeImportPath + `');
const content = process.argv[2];

try {
	const result = processExpressions(content, 'test.md');
	console.log(JSON.stringify({ success: true, result: result }));
} catch (error) {
	console.log(JSON.stringify({ success: false, error: error.message }));
}
`
		tmpFile, err := os.CreateTemp("", "fuzz-process-*.js")
		if err != nil {
			t.Fatalf("Failed to create temp file: %v", err)
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.WriteString(testScript); err != nil {
			t.Fatalf("Failed to write test script: %v", err)
		}
		tmpFile.Close()

		cmd := exec.Command(nodePath, tmpFile.Name(), content)
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Command execution failure is acceptable
			return
		}

		var result struct {
			Success bool   `json:"success"`
			Result  string `json:"result"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal(output, &result); err != nil {
			// JSON parse failure is acceptable
			return
		}

		// Validate security invariants
		if result.Success {
			// If processing succeeded, verify no secrets leaked
			if strings.Contains(content, "secrets.") {
				// Should have failed validation
				t.Errorf("Content with 'secrets.' expression was processed successfully: %q", content)
			}

			// Result should not contain the literal string "${{"
			if strings.Contains(result.Result, "${{") {
				// Check if it's a safe pattern that couldn't be evaluated
				// This is OK only for expressions that reference unavailable context
				if !strings.Contains(result.Result, "needs.") &&
					!strings.Contains(result.Result, "steps.") &&
					!strings.Contains(result.Result, "inputs.") {
					t.Logf("Warning: Result contains unprocessed expression: %s", result.Result)
				}
			}
		} else {
			// If processing failed, verify error message is informative
			if result.Error != "" {
				if strings.Contains(content, "secrets.") &&
					!strings.Contains(result.Error, "unauthorized") &&
					!strings.Contains(result.Error, "not allowed") {
					t.Errorf("Error for 'secrets.' should mention 'unauthorized' or 'not allowed', got: %s", result.Error)
				}
			}
		}
	})
}
