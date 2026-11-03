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

// TestRuntimeImportExpressionValidation tests the expression validation in runtime_import.cjs
func TestRuntimeImportExpressionValidation(t *testing.T) {
	tests := []struct {
		name        string
		expression  string
		expectSafe  bool
		description string
	}{
		{
			name:        "safe expression github.actor",
			expression:  "github.actor",
			expectSafe:  true,
			description: "Core GitHub context property",
		},
		{
			name:        "safe expression github.repository",
			expression:  "github.repository",
			expectSafe:  true,
			description: "Core GitHub context property",
		},
		{
			name:        "safe expression github.event.issue.number",
			expression:  "github.event.issue.number",
			expectSafe:  true,
			description: "Event context property",
		},
		{
			name:        "safe expression needs.build.outputs.version",
			expression:  "needs.build.outputs.version",
			expectSafe:  true,
			description: "Job dependency output",
		},
		{
			name:        "safe expression steps.test.outputs.result",
			expression:  "steps.test.outputs.result",
			expectSafe:  true,
			description: "Step output",
		},
		{
			name:        "unsafe expression env.NODE_VERSION",
			expression:  "env.NODE_VERSION",
			expectSafe:  false,
			description: "Environment variables are unavailable to restricted runtime fragments",
		},
		{
			name:        "safe expression inputs.version",
			expression:  "inputs.version",
			expectSafe:  true,
			description: "Workflow call input",
		},
		{
			name:        "safe expression github.event.inputs.branch",
			expression:  "github.event.inputs.branch",
			expectSafe:  true,
			description: "Workflow dispatch input",
		},
		{
			name:        "unsafe expression secrets.TOKEN",
			expression:  "secrets.TOKEN",
			expectSafe:  false,
			description: "Secret access not allowed",
		},
		{
			name:        "unsafe expression runner.os",
			expression:  "runner.os",
			expectSafe:  false,
			description: "Runner context not allowed",
		},
		{
			name:        "unsafe expression github.token",
			expression:  "github.token",
			expectSafe:  false,
			description: "Token access not allowed",
		},
		{
			name:        "unsafe expression vars.MY_VAR",
			expression:  "vars.MY_VAR",
			expectSafe:  false,
			description: "Variables not allowed",
		},
		// Compound expression forms added by spec-enforcer fix
		{
			name:        "standalone string literal",
			expression:  "'full-sweep (enforce_all)'",
			expectSafe:  true,
			description: "Standalone string literal is safe",
		},
		{
			name:        "standalone boolean literal",
			expression:  "true",
			expectSafe:  true,
			description: "Standalone boolean literal is safe",
		},
		{
			name:        "comparison expression with safe property",
			expression:  "github.event.inputs.enforce_all == 'true'",
			expectSafe:  true,
			description: "Comparison with safe LHS property is allowed",
		},
		{
			name:        "AND compound safe × safe (no literals)",
			expression:  "github.actor && github.repository",
			expectSafe:  true,
			description: "AND compound: both sides are safe properties",
		},
		{
			name:        "AND compound with literal RHS is now refused",
			expression:  "github.event.inputs.enforce_all == 'true' && 'full-sweep (enforce_all)'",
			expectSafe:  false,
			description: "AND with literal operand is refused (literal sub-expression in conjunction)",
		},
		{
			name:        "AND compound with literal LHS is refused",
			expression:  "'prefix' && github.actor",
			expectSafe:  false,
			description: "AND with literal on left is refused",
		},
		{
			name:        "ternary-style AND/OR chain is refused (literal in AND)",
			expression:  "github.event.inputs.enforce_all == 'true' && 'full-sweep (enforce_all)' || 'round-robin'",
			expectSafe:  false,
			description: "Ternary-style AND/OR chain with literal operand is refused",
		},
		{
			name:        "OR fallback pattern is still allowed",
			expression:  "github.event.inputs.enforce_all || 'round-robin'",
			expectSafe:  true,
			description: "OR fallback: safe property || literal is allowed",
		},
		{
			name:        "literal on left of OR is refused",
			expression:  "'default' || github.actor",
			expectSafe:  false,
			description: "Literal on left of OR is refused (always truthy, dead right side)",
		},
		{
			name:        "unsafe AND compound with secrets on right",
			expression:  "github.actor && secrets.TOKEN",
			expectSafe:  false,
			description: "secrets.TOKEN in AND right side must be blocked",
		},
		{
			name:        "unsafe OR compound with secrets on right",
			expression:  "github.actor == 'value' || secrets.TOKEN",
			expectSafe:  false,
			description: "secrets.TOKEN in OR right side must be blocked",
		},
		{
			name:        "unsafe comparison with secrets on right",
			expression:  "github.actor == secrets.TOKEN",
			expectSafe:  false,
			description: "secrets.TOKEN on comparison RHS must be blocked",
		},
		{
			name:        "malformed comparison with trailing unsafe content",
			expression:  "github.actor == 'value' |\x0f secrets.TOKEN",
			expectSafe:  false,
			description: "Malformed comparisons must not pass via partial comparison matching",
		},
		{
			name:        "unsafe compound with secrets",
			expression:  "secrets.TOKEN == 'x' && github.actor || github.repository",
			expectSafe:  false,
			description: "secrets.TOKEN in compound expression must be blocked",
		},
	}

	// Find node executable
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skipf("Node.js not found, skipping runtime_import tests: %v", err)
	}

	// Get absolute path to runtime_import.cjs
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	runtimeImportPath := filepath.Join(wd, "../../actions/setup/js/runtime_import.cjs")
	if _, err := os.Stat(runtimeImportPath); os.IsNotExist(err) {
		t.Fatalf("runtime_import.cjs not found at %s", runtimeImportPath)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test script that calls isSafeExpression
			testScript := `
const { isSafeExpression } = require('` + runtimeImportPath + `');
const expr = process.argv[2];
const result = isSafeExpression(expr);
console.log(JSON.stringify({ safe: result }));
`
			// Write test script to temp file
			tmpFile, err := os.CreateTemp("", "test-expr-*.js")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.WriteString(testScript); err != nil {
				t.Fatalf("Failed to write test script: %v", err)
			}
			tmpFile.Close()

			// Run the test script
			cmd := exec.Command(nodePath, tmpFile.Name(), tt.expression)
			// Use Output() to only capture stdout, avoiding stderr like [one-shot-token]
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("Failed to run test script: %v\nOutput: %s", err, output)
			}

			// Parse the result
			var result struct {
				Safe bool `json:"safe"`
			}
			if err := json.Unmarshal(output, &result); err != nil {
				t.Fatalf("Failed to parse result: %v\nOutput: %s", err, output)
			}

			if result.Safe != tt.expectSafe {
				t.Errorf("isSafeExpression(%q) = %v, want %v (%s)", tt.expression, result.Safe, tt.expectSafe, tt.description)
			}
		})
	}
}

// TestRuntimeImportProcessExpressions tests the processExpressions function
func TestRuntimeImportProcessExpressions(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
		description string
	}{
		{
			name:        "content with safe expressions",
			content:     "Actor: ${{ github.actor }}, Repo: ${{ github.repository }}",
			expectError: false,
			description: "Should process safe expressions",
		},
		{
			name:        "content with unsafe expression",
			content:     "Secret: ${{ secrets.TOKEN }}",
			expectError: true,
			description: "Should reject unsafe expressions",
		},
		{
			name:        "content with multiline expression",
			content:     "Value: ${{ \ngithub.actor \n}}",
			expectError: true,
			description: "Should reject multiline expressions",
		},
		{
			name:        "content without expressions",
			content:     "No expressions here",
			expectError: false,
			description: "Should pass through content without expressions",
		},
		{
			name:        "content with mixed safe and unsafe",
			content:     "Safe: ${{ github.actor }}, Unsafe: ${{ secrets.TOKEN }}",
			expectError: true,
			description: "Should reject if any expression is unsafe",
		},
	}

	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skipf("Node.js not found, skipping runtime_import tests: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	runtimeImportPath := filepath.Join(wd, "../../actions/setup/js/runtime_import.cjs")
	if _, err := os.Stat(runtimeImportPath); os.IsNotExist(err) {
		t.Fatalf("runtime_import.cjs not found at %s", runtimeImportPath)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test script that calls processExpressions
			testScript := `
// Mock core object for testing
global.core = {
	info: () => {},
	warning: () => {},
	setFailed: () => {},
};

// Mock context object with test data
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
			tmpFile, err := os.CreateTemp("", "test-process-*.js")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.WriteString(testScript); err != nil {
				t.Fatalf("Failed to write test script: %v", err)
			}
			tmpFile.Close()

			cmd := exec.Command(nodePath, tmpFile.Name(), tt.content)
			// Use Output() to only capture stdout, avoiding stderr like [one-shot-token]
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("Failed to run test script: %v\nOutput: %s", err, output)
			}

			var result struct {
				Success bool   `json:"success"`
				Result  string `json:"result"`
				Error   string `json:"error"`
			}
			if err := json.Unmarshal(output, &result); err != nil {
				t.Fatalf("Failed to parse result: %v\nOutput: %s", err, output)
			}

			if tt.expectError && result.Success {
				t.Errorf("processExpressions(%q) succeeded, expected error", tt.content)
			}
			if !tt.expectError && !result.Success {
				t.Errorf("processExpressions(%q) failed: %s, expected success", tt.content, result.Error)
			}

			// Verify error message contains expected keywords for unsafe expressions
			if tt.expectError && result.Error != "" {
				if !strings.Contains(result.Error, "unauthorized") && !strings.Contains(result.Error, "not allowed") {
					t.Errorf("Error message should mention 'unauthorized' or 'not allowed', got: %s", result.Error)
				}
			}
		})
	}
}

// TestRuntimeImportWithExpressions tests the full runtime import flow with expressions
func TestRuntimeImportWithExpressions(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skipf("Node.js not found, skipping runtime_import tests: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	runtimeImportPath := filepath.Join(wd, "../../actions/setup/js/runtime_import.cjs")
	if _, err := os.Stat(runtimeImportPath); os.IsNotExist(err) {
		t.Fatalf("runtime_import.cjs not found at %s", runtimeImportPath)
	}

	// Create temp directory for test files
	tempDir, err := os.MkdirTemp("", "runtime-import-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	githubDir := filepath.Join(tempDir, ".github")
	if err := os.MkdirAll(githubDir, 0755); err != nil {
		t.Fatalf("Failed to create .github directory: %v", err)
	}
	workflowsDir := filepath.Join(githubDir, "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	tests := []struct {
		name         string
		fileContent  string
		expectError  bool
		validateFunc func(t *testing.T, result string)
	}{
		{
			name: "file with safe expressions",
			fileContent: `# Test File

Actor: ${{ github.actor }}
Repository: ${{ github.repository }}
Run ID: ${{ github.run_id }}`,
			expectError: false,
			validateFunc: func(t *testing.T, result string) {
				if !strings.Contains(result, "testuser") {
					t.Errorf("Result should contain rendered actor name 'testuser', got: %s", result)
				}
				if !strings.Contains(result, "testorg/testrepo") {
					t.Errorf("Result should contain rendered repository 'testorg/testrepo', got: %s", result)
				}
			},
		},
		{
			name: "file with unsafe expression",
			fileContent: `# Test File

Secret: ${{ secrets.TOKEN }}`,
			expectError: true,
			validateFunc: func(t *testing.T, result string) {
				if !strings.Contains(result, "unauthorized") {
					t.Errorf("Error should mention 'unauthorized', got: %s", result)
				}
			},
		},
		{
			name: "file with mixed expressions",
			fileContent: `# Test File

Safe: ${{ github.actor }}
Unsafe: ${{ runner.os }}`,
			expectError: true,
			validateFunc: func(t *testing.T, result string) {
				if !strings.Contains(result, "runner.os") {
					t.Errorf("Error should mention the unsafe expression 'runner.os', got: %s", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write test file to workflows directory
			testFilePath := filepath.Join(workflowsDir, "test.md")
			if err := os.WriteFile(testFilePath, []byte(tt.fileContent), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Create test script
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
process.env.GITHUB_WORKSPACE = '` + tempDir + `';

const { processRuntimeImport } = require('` + runtimeImportPath + `');

(async () => {
	try {
		const result = await processRuntimeImport('test.md', false, '` + tempDir + `');
		console.log(JSON.stringify({ success: true, result: result }));
	} catch (error) {
		console.log(JSON.stringify({ success: false, error: error.message }));
	}
})();
`
			tmpFile, err := os.CreateTemp("", "test-import-*.js")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.WriteString(testScript); err != nil {
				t.Fatalf("Failed to write test script: %v", err)
			}
			tmpFile.Close()

			cmd := exec.Command(nodePath, tmpFile.Name())
			// Use Output() to only capture stdout, avoiding stderr like [one-shot-token]
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("Failed to run test script: %v\nOutput: %s", err, output)
			}

			var result struct {
				Success bool   `json:"success"`
				Result  string `json:"result"`
				Error   string `json:"error"`
			}
			if err := json.Unmarshal(output, &result); err != nil {
				t.Fatalf("Failed to parse result: %v\nOutput: %s", err, output)
			}

			if tt.expectError && result.Success {
				t.Errorf("processRuntimeImport succeeded, expected error")
			}
			if !tt.expectError && !result.Success {
				t.Errorf("processRuntimeImport failed: %s, expected success", result.Error)
			}

			// Run validation function
			if tt.expectError {
				tt.validateFunc(t, result.Error)
			} else {
				tt.validateFunc(t, result.Result)
			}
		})
	}
}
