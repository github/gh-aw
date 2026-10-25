//go:build !integration

package cli

import (
	"encoding/json"
	"testing"
)

func TestInjectDockerUnavailableWarning_AddsWarningToValidResults(t *testing.T) {
	// Simulate compile output where both workflows compiled successfully.
	inputJSON := `[{"workflow":"a.md","valid":true,"errors":[],"warnings":[]},{"workflow":"b.md","valid":true,"errors":[],"warnings":[]}]`
	warningMsg := "docker is not available (cannot connect to Docker daemon). actionlint requires Docker."

	output := injectDockerUnavailableWarning(inputJSON, warningMsg)

	var results []ValidationResult
	if err := json.Unmarshal([]byte(output), &results); err != nil {
		t.Fatalf("Failed to parse injected output: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}

	for _, r := range results {
		if !r.Valid {
			t.Errorf("Workflow %s should still be valid after Docker unavailable warning", r.Workflow)
		}
		if len(r.Warnings) != 1 {
			t.Errorf("Workflow %s should have 1 warning, got %d", r.Workflow, len(r.Warnings))
			continue
		}
		if r.Warnings[0].Type != "docker_unavailable" {
			t.Errorf("Expected warning type 'docker_unavailable', got '%s'", r.Warnings[0].Type)
		}
		if r.Warnings[0].Message != warningMsg {
			t.Errorf("Expected warning message %q, got %q", warningMsg, r.Warnings[0].Message)
		}
	}
}

func TestInjectDockerUnavailableWarning_PreservesInvalidResults(t *testing.T) {
	// One workflow failed to compile; the other succeeded.
	inputJSON := `[{"workflow":"bad.md","valid":false,"errors":[{"type":"parse_error","message":"syntax error"}],"warnings":[]},{"workflow":"good.md","valid":true,"errors":[],"warnings":[]}]`
	warningMsg := "docker is not available"

	output := injectDockerUnavailableWarning(inputJSON, warningMsg)

	var results []ValidationResult
	if err := json.Unmarshal([]byte(output), &results); err != nil {
		t.Fatalf("Failed to parse injected output: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}

	// bad.md should remain invalid and still carry its original error.
	if results[0].Valid {
		t.Error("bad.md should remain invalid")
	}
	if len(results[0].Errors) != 1 || results[0].Errors[0].Type != "parse_error" {
		t.Error("bad.md should still have its original parse_error")
	}
	// good.md should be valid with the warning appended.
	if !results[1].Valid {
		t.Error("good.md should still be valid")
	}
	if len(results[1].Warnings) != 1 || results[1].Warnings[0].Type != "docker_unavailable" {
		t.Error("good.md should have the docker_unavailable warning")
	}
}

func TestInjectDockerUnavailableWarning_InvalidJSONReturnedUnchanged(t *testing.T) {
	invalidJSON := "not-valid-json"
	output := injectDockerUnavailableWarning(invalidJSON, "some warning")
	if output != invalidJSON {
		t.Errorf("Expected original output to be returned unchanged for invalid JSON, got: %s", output)
	}
}

func TestInjectShellcheckDiagnostics_AppendsWarnings(t *testing.T) {
	inputJSON := `[{"workflow":"a.md","valid":true,"errors":[],"warnings":[]}]`
	stderr := "shellcheck findings in a.lock.yml (step: lint):\nscript:1:1: warning: foo [SC1000]\n"

	output := injectShellcheckDiagnostics(inputJSON, stderr)

	var results []ValidationResult
	if err := json.Unmarshal([]byte(output), &results); err != nil {
		t.Fatalf("Failed to parse injected output: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if len(results[0].Warnings) != 1 {
		t.Fatalf("Expected 1 warning, got %d", len(results[0].Warnings))
	}
	if results[0].Warnings[0].Type != "shellcheck" {
		t.Fatalf("Expected warning type shellcheck, got %s", results[0].Warnings[0].Type)
	}
	if results[0].Warnings[0].Message == "" {
		t.Fatal("Expected non-empty warning message")
	}
}

func TestInjectShellcheckDiagnostics_IgnoresUnrelatedStderr(t *testing.T) {
	inputJSON := `[{"workflow":"a.md","valid":true,"errors":[],"warnings":[]}]`
	stderr := "diagnostic noise should not be returned"

	output := injectShellcheckDiagnostics(inputJSON, stderr)

	if output != inputJSON {
		t.Fatalf("Expected unchanged output for unrelated stderr, got: %s", output)
	}
}

func TestInjectDevelopmentShellcheckDiagnosticsKeepsStructuredResults(t *testing.T) {
	for _, inputJSON := range []string{
		`[{"scope":"batch","workflow":"shellcheck","valid":true,"errors":[],"warnings":[{"type":"shellcheck_finding","message":"advisory","severity":"info"}]}]`,
		`[{"scope":"batch","workflow":"shellcheck","valid":false,"errors":[{"type":"shellcheck_finding","message":"blocking","severity":"high"}],"warnings":[]}]`,
	} {
		output := injectDevelopmentShellcheckDiagnostics(
			inputJSON,
			"shellcheck findings in a.lock.yml:\nscript:1:3: warning: quote this [SC2086]\n",
		)
		if output != inputJSON {
			t.Fatalf("Expected structured ShellCheck results to be preserved without stderr duplicates, got: %s", output)
		}
	}
}

func TestBuildCompileErrorResults_NormalizesRequestedWorkflowNames(t *testing.T) {
	results := buildCompileErrorResults([]string{"foo", "bar.md", "nested/baz"}, "compile failed")

	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}
	if results[0].Workflow != "foo.md" {
		t.Fatalf("Expected foo.md, got %s", results[0].Workflow)
	}
	if results[1].Workflow != "bar.md" {
		t.Fatalf("Expected bar.md, got %s", results[1].Workflow)
	}
	if results[2].Workflow != "baz.md" {
		t.Fatalf("Expected baz.md, got %s", results[2].Workflow)
	}
	for _, r := range results {
		if r.Valid {
			t.Fatalf("Expected workflow %s to be invalid", r.Workflow)
		}
		if len(r.Errors) != 1 || r.Errors[0].Type != "config_error" || r.Errors[0].Message != "compile failed" {
			t.Fatalf("Unexpected error payload for workflow %s: %+v", r.Workflow, r.Errors)
		}
	}
}
