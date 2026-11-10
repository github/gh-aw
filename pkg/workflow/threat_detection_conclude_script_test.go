package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func isolatedPath(t *testing.T, binDir string) string {
	t.Helper()

	dirnamePath, err := exec.LookPath("dirname")
	if err != nil {
		t.Fatalf("failed to locate dirname: %v", err)
	}
	if err := os.Symlink(dirnamePath, filepath.Join(binDir, "dirname")); err != nil {
		t.Fatalf("failed to make dirname available in test PATH: %v", err)
	}
	return binDir
}

func TestConcludeThreatDetectionScript_ThreatKinds(t *testing.T) {
	for _, tt := range []struct {
		name, result, reason, wantKinds string
	}{
		{"multiple findings", `{"prompt_injection":true,"secret_leak":false,"malicious_patch":true,"reasons":["PRIVATE_TEXT"]}`, "threat_detected", "prompt_injection,malicious_patch"},
		{"secret leak", `{"prompt_injection":false,"secret_leak":true,"malicious_patch":false}`, "threat_detected", "secret_leak"},
		{"invalid boolean", `{"prompt_injection":"true","secret_leak":false,"malicious_patch":true}`, "threat_detected", ""},
		{"malformed", `not-json PRIVATE_TEXT`, "threat_detected", ""},
		{"tooling error", `{"prompt_injection":true,"secret_leak":true,"malicious_patch":true}`, "agent_failure", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, policy := range []string{"true", "false"} {
				tmpDir := t.TempDir()
				resultFile := filepath.Join(tmpDir, "detection_result.json")
				outputFile := filepath.Join(tmpDir, "github_output")
				binDir := filepath.Join(tmpDir, "bin")
				if err := os.MkdirAll(binDir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(resultFile, []byte(tt.result), 0644); err != nil {
					t.Fatal(err)
				}
				stub := "#!/bin/bash\necho conclusion=failure >> \"$GITHUB_OUTPUT\"\necho reason=" + tt.reason + " >> \"$GITHUB_OUTPUT\"\nexit 1\n"
				if err := os.WriteFile(filepath.Join(binDir, "threat-detect"), []byte(stub), 0755); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command("bash", filepath.Join("..", "..", "actions", "setup", "sh", "conclude_threat_detection.sh"), resultFile)
				cmd.Env = append(os.Environ(), "RUN_DETECTION=true", "GH_AW_DETECTION_CONTINUE_ON_ERROR="+policy, "GITHUB_OUTPUT="+outputFile, "PATH="+binDir+":"+os.Getenv("PATH"))
				out, err := cmd.CombinedOutput()
				wantErr := policy == "false" || tt.reason == "threat_detected"
				if (err != nil) != wantErr {
					t.Fatalf("exit error = %v, want error %v; output: %s", err, wantErr, out)
				}
				output, err := os.ReadFile(outputFile)
				if err != nil {
					t.Fatal(err)
				}
				if got := lastKeyValue(string(output), "threat_kinds"); got != tt.wantKinds {
					t.Errorf("threat_kinds = %q, want %q", got, tt.wantKinds)
				}
				if strings.Contains(string(output)+string(out), "PRIVATE_TEXT") {
					t.Fatal("category outputs or diagnostics exposed generated text")
				}
			}
		})
	}
}

// TestConcludeThreatDetectionScript_MissingBinaryWarnMode verifies that the
// script's sole remaining shell-side special case — threat-detect missing
// from PATH — warns and exits 0 when GH_AW_DETECTION_CONTINUE_ON_ERROR is not
// exactly "false" (warn mode).
func TestConcludeThreatDetectionScript_MissingBinaryWarnMode(t *testing.T) {
	for _, continueOnError := range []string{"true", "yes"} {
		t.Run(continueOnError, func(t *testing.T) {
			tmpDir := t.TempDir()
			scriptPath := filepath.Join("..", "..", "actions", "setup", "sh", "conclude_threat_detection.sh")
			outputFile := filepath.Join(tmpDir, "github_output.txt")
			resultFile := filepath.Join(tmpDir, "detection_result.json")
			binDir := filepath.Join(tmpDir, "bin")
			if err := os.MkdirAll(binDir, 0755); err != nil {
				t.Fatalf("failed to create test bin directory: %v", err)
			}

			cmd := exec.Command("bash", scriptPath, resultFile)
			cmd.Env = append(os.Environ(),
				"RUN_DETECTION=true",
				"GH_AW_DETECTION_CONTINUE_ON_ERROR="+continueOnError,
				"GITHUB_OUTPUT="+outputFile,
				"PATH="+isolatedPath(t, binDir),
			)

			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("script should exit 0 in warn mode when threat-detect is missing: %v\nOutput: %s", err, out)
			}

			outputData, err := os.ReadFile(outputFile)
			if err != nil {
				t.Fatalf("failed to read GITHUB_OUTPUT: %v", err)
			}
			outputText := string(outputData)
			if !strings.Contains(outputText, "conclusion=warning") || !strings.Contains(outputText, "success=false") || !strings.Contains(outputText, "reason=agent_failure") {
				t.Fatalf("expected warning failure outputs in GITHUB_OUTPUT, got: %s", outputText)
			}
			if !strings.Contains(string(out), "threat-detect binary not found on PATH") {
				t.Fatalf("expected warning message about missing binary, got: %s", out)
			}
		})
	}
}

// TestConcludeThreatDetectionScript_MissingBinaryStrictMode verifies the
// guard hard-fails (exit 1) when GH_AW_DETECTION_CONTINUE_ON_ERROR is
// exactly "false" (strict mode).
func TestConcludeThreatDetectionScript_MissingBinaryStrictMode(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join("..", "..", "actions", "setup", "sh", "conclude_threat_detection.sh")
	outputFile := filepath.Join(tmpDir, "github_output.txt")
	resultFile := filepath.Join(tmpDir, "detection_result.json")
	binDir := filepath.Join(tmpDir, "bin")

	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("failed to create test bin directory: %v", err)
	}

	cmd := exec.Command("bash", scriptPath, resultFile)
	cmd.Env = append(os.Environ(),
		"RUN_DETECTION=true",
		"GH_AW_DETECTION_CONTINUE_ON_ERROR=false",
		"GITHUB_OUTPUT="+outputFile,
		"PATH="+isolatedPath(t, binDir),
	)

	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("script should exit non-zero in strict mode when threat-detect is missing, output: %s", out)
	}
	if !strings.Contains(string(out), "ERR_SYSTEM:") {
		t.Fatalf("expected ERR_SYSTEM message, got: %s", out)
	}
	if !strings.Contains(string(out), "threat-detect binary not found on PATH") {
		t.Fatalf("expected message about missing binary, got: %s", out)
	}
	outputData, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read GITHUB_OUTPUT: %v", err)
	}
	outputText := string(outputData)
	if !strings.Contains(outputText, "conclusion=failure") || !strings.Contains(outputText, "success=false") || !strings.Contains(outputText, "reason=agent_failure") {
		t.Fatalf("expected strict failure outputs in GITHUB_OUTPUT, got: %s", outputText)
	}
}

// TestConcludeThreatDetectionScript_InvokesThreatDetectConclude verifies the
// script delegates to `threat-detect conclude` with both --result-file and
// an explicit --detection-log, defaulting the log path to
// <result-file-dir>/detection.log.
func TestConcludeThreatDetectionScript_InvokesThreatDetectConclude(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join("..", "..", "actions", "setup", "sh", "conclude_threat_detection.sh")
	resultFile := filepath.Join(tmpDir, "detection_result.json")
	outputFile := filepath.Join(tmpDir, "github_output.txt")
	envFile := filepath.Join(tmpDir, "github_env.txt")
	callLog := filepath.Join(tmpDir, "call.log")
	binDir := filepath.Join(tmpDir, "bin")

	if err := os.WriteFile(resultFile, []byte(`{"conclusion":"success"}`), 0644); err != nil {
		t.Fatalf("failed to write result file: %v", err)
	}
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("failed to create bin dir: %v", err)
	}

	stubPath := filepath.Join(binDir, "threat-detect")
	stub := "#!/usr/bin/env bash\n" +
		"echo \"$*\" >> \"$CALL_LOG\"\n" +
		"echo \"conclusion=success\" >> \"$GITHUB_OUTPUT\"\n"
	if err := os.WriteFile(stubPath, []byte(stub), 0755); err != nil {
		t.Fatalf("failed to write threat-detect stub: %v", err)
	}

	cmd := exec.Command("bash", scriptPath, resultFile)
	cmd.Env = append(os.Environ(),
		"RUN_DETECTION=true",
		"GITHUB_OUTPUT="+outputFile,
		"GITHUB_ENV="+envFile,
		"CALL_LOG="+callLog,
		"PATH="+binDir+":"+os.Getenv("PATH"),
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script failed: %v\nOutput: %s", err, out)
	}

	callData, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("failed to read call log: %v", err)
	}
	expectedLog := filepath.Join(tmpDir, "detection.log")
	if !strings.Contains(string(callData), "conclude --result-file "+resultFile+" --detection-log "+expectedLog) {
		t.Fatalf("expected threat-detect conclude invocation with default detection log, got: %s", callData)
	}
}

// TestConcludeThreatDetectionScript_PropagatesThreatDetectFailure verifies that a
// threat-detect conclude failure that never recorded a conclusion still fails
// closed in strict mode, with explicit agent_failure outputs so safe_outputs
// never sees an empty conclusion.
func TestConcludeThreatDetectionScript_PropagatesThreatDetectFailure(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join("..", "..", "actions", "setup", "sh", "conclude_threat_detection.sh")
	outputFile := filepath.Join(tmpDir, "github_output.txt")
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("failed to create bin directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "threat-detect"), []byte("#!/bin/bash\nexit 1\n"), 0755); err != nil {
		t.Fatalf("failed to write threat-detect stub: %v", err)
	}

	cmd := exec.Command("bash", scriptPath, filepath.Join(tmpDir, "detection_result.json"))
	cmd.Env = append(os.Environ(),
		"RUN_DETECTION=true",
		"GH_AW_DETECTION_CONTINUE_ON_ERROR=false",
		"GITHUB_OUTPUT="+outputFile,
		"PATH="+binDir+":"+os.Getenv("PATH"),
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("script should propagate threat-detect failure, output: %s", out)
	}
	outputData, readErr := os.ReadFile(outputFile)
	if readErr != nil {
		t.Fatalf("failed to read GITHUB_OUTPUT: %v", readErr)
	}
	if got := lastKeyValue(string(outputData), "conclusion"); got != "failure" {
		t.Errorf("conclusion = %q, want failure; GITHUB_OUTPUT: %s", got, outputData)
	}
	if got := lastKeyValue(string(outputData), "reason"); got != "agent_failure" {
		t.Errorf("reason = %q, want agent_failure", got)
	}
}

// TestConcludeThreatDetectionScript_ConcludeExitWithoutConclusion verifies that a
// threat-detect conclude that exits non-zero before writing a conclusion is
// reported as an unavailable detector instead of leaving the outputs empty, which
// safe_outputs would otherwise treat as unrestricted: warn mode concludes
// warning/agent_failure and exits 0, strict mode fails closed.
func TestConcludeThreatDetectionScript_ConcludeExitWithoutConclusion(t *testing.T) {
	tests := []struct {
		name            string
		continueOnError string
		stub            string
		wantErr         bool
		wantConclusion  string
	}{
		{name: "warn mode exit 2", continueOnError: "true", stub: "#!/usr/bin/env bash\nexit 2\n", wantConclusion: "warning"},
		{name: "strict mode exit 2", continueOnError: "false", stub: "#!/usr/bin/env bash\nexit 2\n", wantErr: true, wantConclusion: "failure"},
		{name: "warn mode empty conclusion", continueOnError: "true", stub: "#!/usr/bin/env bash\necho \"conclusion=\" >> \"$GITHUB_OUTPUT\"\nexit 2\n", wantConclusion: "warning"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			scriptPath := filepath.Join("..", "..", "actions", "setup", "sh", "conclude_threat_detection.sh")
			outputFile := filepath.Join(tmpDir, "github_output.txt")
			binDir := filepath.Join(tmpDir, "bin")
			if err := os.MkdirAll(binDir, 0755); err != nil {
				t.Fatalf("failed to create bin directory: %v", err)
			}
			if err := os.WriteFile(filepath.Join(binDir, "threat-detect"), []byte(tt.stub), 0755); err != nil {
				t.Fatalf("failed to write threat-detect stub: %v", err)
			}

			cmd := exec.Command("bash", scriptPath, filepath.Join(tmpDir, "detection_result.json"))
			cmd.Env = append(os.Environ(),
				"RUN_DETECTION=true",
				"THREAT_DETECT_INSTALL_OUTCOME=success",
				"GH_AW_DETECTION_CONTINUE_ON_ERROR="+tt.continueOnError,
				"GITHUB_OUTPUT="+outputFile,
				"PATH="+binDir+":"+os.Getenv("PATH"),
			)
			out, err := cmd.CombinedOutput()
			if tt.wantErr && err == nil {
				t.Fatalf("script should fail closed in strict mode, output: %s", out)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("script should exit 0 in warn mode: %v\nOutput: %s", err, out)
			}
			outputData, readErr := os.ReadFile(outputFile)
			if readErr != nil {
				t.Fatalf("failed to read GITHUB_OUTPUT: %v", readErr)
			}
			if got := lastKeyValue(string(outputData), "conclusion"); got != tt.wantConclusion {
				t.Errorf("conclusion = %q, want %s; GITHUB_OUTPUT: %s", got, tt.wantConclusion, outputData)
			}
			if got := lastKeyValue(string(outputData), "success"); got != "false" {
				t.Errorf("success = %q, want false", got)
			}
			if got := lastKeyValue(string(outputData), "reason"); got != "agent_failure" {
				t.Errorf("reason = %q, want agent_failure", got)
			}
			if !strings.Contains(string(out), "threat-detect conclude exited with status 2 without reporting a conclusion") {
				t.Errorf("expected message about missing conclusion, got: %s", out)
			}
		})
	}
}

// TestConcludeThreatDetectionScript_HonorsDetectionLogOverride verifies that
// an explicit DETECTION_LOG_FILE environment variable overrides the default
// <result-file-dir>/detection.log path passed to --detection-log.
func TestConcludeThreatDetectionScript_HonorsDetectionLogOverride(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join("..", "..", "actions", "setup", "sh", "conclude_threat_detection.sh")
	resultFile := filepath.Join(tmpDir, "detection_result.json")
	outputFile := filepath.Join(tmpDir, "github_output.txt")
	callLog := filepath.Join(tmpDir, "call.log")
	binDir := filepath.Join(tmpDir, "bin")
	customLog := filepath.Join(tmpDir, "custom-detection.log")

	if err := os.WriteFile(resultFile, []byte(`{"conclusion":"success"}`), 0644); err != nil {
		t.Fatalf("failed to write result file: %v", err)
	}
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("failed to create bin dir: %v", err)
	}

	stubPath := filepath.Join(binDir, "threat-detect")
	stub := "#!/usr/bin/env bash\n" +
		"echo \"$*\" >> \"$CALL_LOG\"\n" +
		"echo \"conclusion=success\" >> \"$GITHUB_OUTPUT\"\n"
	if err := os.WriteFile(stubPath, []byte(stub), 0755); err != nil {
		t.Fatalf("failed to write threat-detect stub: %v", err)
	}

	cmd := exec.Command("bash", scriptPath, resultFile)
	cmd.Env = append(os.Environ(),
		"RUN_DETECTION=true",
		"GITHUB_OUTPUT="+outputFile,
		"CALL_LOG="+callLog,
		"DETECTION_LOG_FILE="+customLog,
		"PATH="+binDir+":"+os.Getenv("PATH"),
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script failed: %v\nOutput: %s", err, out)
	}

	callData, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("failed to read call log: %v", err)
	}
	if !strings.Contains(string(callData), "--detection-log "+customLog) {
		t.Fatalf("expected threat-detect conclude invocation with overridden detection log, got: %s", callData)
	}
}

// TestConcludeThreatDetectionScript_FailedInstallDoesNotInvokeDetector verifies
// that a non-success threat-detect install outcome reports the agent_failure
// conclusion directly, without executing any binary found on PATH. A binary
// present after a failed install is either unverified or the install script's
// fail-closed placeholder, and the placeholder cannot report a conclusion itself.
func TestConcludeThreatDetectionScript_FailedInstallDoesNotInvokeDetector(t *testing.T) {
	tests := []struct {
		name            string
		continueOnError string
		installOutcome  string
		wantErr         bool
		wantConclusion  string
	}{
		{name: "warn mode failure", continueOnError: "true", installOutcome: "failure", wantErr: false, wantConclusion: "conclusion=warning"},
		{name: "warn mode cancelled", continueOnError: "true", installOutcome: "cancelled", wantErr: false, wantConclusion: "conclusion=warning"},
		{name: "strict mode failure", continueOnError: "false", installOutcome: "failure", wantErr: true, wantConclusion: "conclusion=failure"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			scriptPath := filepath.Join("..", "..", "actions", "setup", "sh", "conclude_threat_detection.sh")
			resultFile := filepath.Join(tmpDir, "detection_result.json")
			outputFile := filepath.Join(tmpDir, "github_output.txt")
			callLog := filepath.Join(tmpDir, "call.log")
			binDir := filepath.Join(tmpDir, "bin")

			if err := os.MkdirAll(binDir, 0755); err != nil {
				t.Fatalf("failed to create bin dir: %v", err)
			}
			// Stand in for an unverified binary left on PATH (preinstalled copy or
			// the install script's fail-closed placeholder).
			stub := "#!/usr/bin/env bash\necho \"$*\" >> \"$CALL_LOG\"\n"
			if err := os.WriteFile(filepath.Join(binDir, "threat-detect"), []byte(stub), 0755); err != nil {
				t.Fatalf("failed to write threat-detect stub: %v", err)
			}

			cmd := exec.Command("bash", scriptPath, resultFile)
			cmd.Env = append(os.Environ(),
				"RUN_DETECTION=true",
				"THREAT_DETECT_INSTALL_OUTCOME="+tt.installOutcome,
				"GH_AW_DETECTION_CONTINUE_ON_ERROR="+tt.continueOnError,
				"GITHUB_OUTPUT="+outputFile,
				"CALL_LOG="+callLog,
				"PATH="+binDir+":"+os.Getenv("PATH"),
			)

			out, err := cmd.CombinedOutput()
			if tt.wantErr && err == nil {
				t.Fatalf("script should exit non-zero in strict mode, output: %s", out)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("script should exit 0 in warn mode: %v\nOutput: %s", err, out)
			}

			if _, statErr := os.Stat(callLog); statErr == nil {
				t.Fatal("script must not invoke threat-detect when installation did not succeed")
			}

			outputData, err := os.ReadFile(outputFile)
			if err != nil {
				t.Fatalf("failed to read GITHUB_OUTPUT: %v", err)
			}
			outputText := string(outputData)
			if !strings.Contains(outputText, tt.wantConclusion) || !strings.Contains(outputText, "success=false") || !strings.Contains(outputText, "reason=agent_failure") {
				t.Fatalf("expected %q with agent_failure outputs, got: %s", tt.wantConclusion, outputText)
			}
			if !strings.Contains(string(out), "threat-detect installation did not complete verification") {
				t.Fatalf("expected message about incomplete installation, got: %s", out)
			}
		})
	}
}

// TestConcludeThreatDetectionScript_AWFInstallQuotaBlocked verifies that an AWF
// install stopped by GitHub API quota exhaustion concludes as an explicit,
// deferred failure (never a pass, even in warn mode), while other AWF install
// failures keep the generic detector-unavailable handling.
func TestConcludeThreatDetectionScript_AWFInstallQuotaBlocked(t *testing.T) {
	tests := []struct {
		name            string
		continueOnError string
		quotaBlocked    string
		wantErr         bool
		wantReason      string
		wantConclusion  string
		wantMessage     string
	}{
		{name: "quota blocked warn mode", continueOnError: "true", quotaBlocked: "resource=core; limit=15000; remaining=0; used=15000; reset=2026-11-10T00:00:04Z; reason=reset_exceeds_budget", wantErr: true, wantReason: "reason=github_api_quota_exhausted", wantConclusion: "conclusion=failure", wantMessage: "GitHub API quota exhausted"},
		{name: "quota blocked strict mode", continueOnError: "false", quotaBlocked: "resource=core; limit=15000; remaining=0; used=15000; reset=unknown; reason=unknown_reset", wantErr: true, wantReason: "reason=github_api_quota_exhausted", wantConclusion: "conclusion=failure", wantMessage: "GitHub API quota exhausted"},
		{name: "non-quota install failure", continueOnError: "true", wantErr: false, wantReason: "reason=agent_failure", wantConclusion: "conclusion=warning", wantMessage: "AWF installation did not complete verification"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			scriptPath := filepath.Join("..", "..", "actions", "setup", "sh", "conclude_threat_detection.sh")
			resultFile := filepath.Join(tmpDir, "detection_result.json")
			outputFile := filepath.Join(tmpDir, "github_output.txt")
			summaryFile := filepath.Join(tmpDir, "summary.md")
			callLog := filepath.Join(tmpDir, "call.log")
			binDir := filepath.Join(tmpDir, "bin")
			if err := os.MkdirAll(binDir, 0755); err != nil {
				t.Fatalf("failed to create bin dir: %v", err)
			}
			stub := "#!/usr/bin/env bash\necho \"$*\" >> \"$CALL_LOG\"\n"
			if err := os.WriteFile(filepath.Join(binDir, "threat-detect"), []byte(stub), 0755); err != nil {
				t.Fatalf("failed to write threat-detect stub: %v", err)
			}

			cmd := exec.Command("bash", scriptPath, resultFile)
			cmd.Env = append(os.Environ(),
				"RUN_DETECTION=true",
				// A failed AWF install skips the later threat-detect install.
				"THREAT_DETECT_INSTALL_OUTCOME=skipped",
				"AWF_INSTALL_OUTCOME=failure",
				"GH_AW_GITHUB_API_QUOTA_BLOCKED="+tt.quotaBlocked,
				"GH_AW_DETECTION_CONTINUE_ON_ERROR="+tt.continueOnError,
				"GITHUB_OUTPUT="+outputFile,
				"GITHUB_STEP_SUMMARY="+summaryFile,
				"CALL_LOG="+callLog,
				"PATH="+binDir+":"+os.Getenv("PATH"),
			)

			out, err := cmd.CombinedOutput()
			if tt.wantErr && err == nil {
				t.Fatalf("script should exit non-zero, output: %s", out)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("script should exit 0: %v\nOutput: %s", err, out)
			}
			if _, statErr := os.Stat(callLog); statErr == nil {
				t.Fatal("script must not invoke threat-detect when AWF installation did not succeed")
			}
			outputData, err := os.ReadFile(outputFile)
			if err != nil {
				t.Fatalf("failed to read GITHUB_OUTPUT: %v", err)
			}
			outputText := string(outputData)
			if !strings.Contains(outputText, "success=false") || !strings.Contains(outputText, tt.wantReason) || !strings.Contains(outputText, tt.wantConclusion) {
				t.Fatalf("expected %q and %q, got: %s", tt.wantReason, tt.wantConclusion, outputText)
			}
			if !strings.Contains(string(out), tt.wantMessage) {
				t.Fatalf("expected %q in output, got: %s", tt.wantMessage, out)
			}
			if tt.quotaBlocked != "" {
				summary, err := os.ReadFile(summaryFile)
				if err != nil {
					t.Fatalf("failed to read step summary: %v", err)
				}
				if !strings.Contains(string(summary), "### GitHub API quota exhausted") || !strings.Contains(string(summary), "remaining=0") || !strings.Contains(string(summary), "not a security pass") {
					t.Fatalf("expected quota-exhausted summary with metadata, got: %s", summary)
				}
			}
		})
	}
}

// TestConcludeThreatDetectionScript_SuccessfulInstallDelegates verifies that a
// successful install outcome leaves the normal delegation path intact.
func TestConcludeThreatDetectionScript_SuccessfulInstallDelegates(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join("..", "..", "actions", "setup", "sh", "conclude_threat_detection.sh")
	resultFile := filepath.Join(tmpDir, "detection_result.json")
	outputFile := filepath.Join(tmpDir, "github_output.txt")
	callLog := filepath.Join(tmpDir, "call.log")
	binDir := filepath.Join(tmpDir, "bin")

	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("failed to create bin dir: %v", err)
	}
	stub := "#!/usr/bin/env bash\n" +
		"echo \"$*\" >> \"$CALL_LOG\"\n" +
		"echo \"conclusion=success\" >> \"$GITHUB_OUTPUT\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "threat-detect"), []byte(stub), 0755); err != nil {
		t.Fatalf("failed to write threat-detect stub: %v", err)
	}

	cmd := exec.Command("bash", scriptPath, resultFile)
	cmd.Env = append(os.Environ(),
		"RUN_DETECTION=true",
		"THREAT_DETECT_INSTALL_OUTCOME=success",
		"GITHUB_OUTPUT="+outputFile,
		"CALL_LOG="+callLog,
		"PATH="+binDir+":"+os.Getenv("PATH"),
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script failed: %v\nOutput: %s", err, out)
	}
	callData, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("failed to read call log: %v", err)
	}
	if !strings.Contains(string(callData), "conclude --result-file "+resultFile) {
		t.Fatalf("expected threat-detect conclude invocation after a successful install, got: %s", callData)
	}
}

// Explicit no-output skips must not invoke an unverified preinstalled detector,
// which could inspect payloads that did not pass redaction.
func TestConcludeThreatDetectionScript_SkippedDetectionDoesNotInvokeBinary(t *testing.T) {
	for _, warnMode := range []string{"false", "true"} {
		t.Run("warn="+warnMode, func(t *testing.T) {
			dir := t.TempDir()
			scriptPath := filepath.Join("..", "..", "actions", "setup", "sh", "conclude_threat_detection.sh")
			resultFile := filepath.Join(dir, "detection_result.json")
			outputFile := filepath.Join(dir, "github_output.txt")
			callLog := filepath.Join(dir, "call.log")
			binDir := filepath.Join(dir, "bin")
			if err := os.MkdirAll(binDir, 0755); err != nil {
				t.Fatalf("failed to create bin dir: %v", err)
			}
			if err := os.WriteFile(resultFile, []byte("synthetic-unredacted-payload\n"), 0600); err != nil {
				t.Fatalf("failed to write synthetic payload: %v", err)
			}
			stub := "#!/usr/bin/env bash\n" +
				"echo invoked > \"$CALL_LOG\"\n" +
				"cat \"$PAYLOAD_FILE\"\n" +
				"exit 99\n"
			if err := os.WriteFile(filepath.Join(binDir, "threat-detect"), []byte(stub), 0755); err != nil {
				t.Fatalf("failed to write threat-detect stub: %v", err)
			}
			cmd := exec.Command("bash", scriptPath, resultFile)
			cmd.Env = append(os.Environ(),
				"RUN_DETECTION=false",
				"THREAT_DETECT_INSTALL_OUTCOME=skipped",
				"GH_AW_DETECTION_CONTINUE_ON_ERROR="+warnMode,
				"GITHUB_OUTPUT="+outputFile,
				"CALL_LOG="+callLog,
				"PAYLOAD_FILE="+resultFile,
				"PATH="+binDir+":"+os.Getenv("PATH"),
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("explicit skip should succeed without invoking the detector: %v\nOutput: %s", err, out)
			}
			if _, err := os.Stat(callLog); !os.IsNotExist(err) {
				t.Fatalf("preinstalled detector must not run on an explicit skip: %v", err)
			}
			if len(out) != 0 {
				t.Fatalf("explicit skip must not print payloads or diagnostics: %s", out)
			}
			output, err := os.ReadFile(outputFile)
			if err != nil {
				t.Fatalf("failed to read conclusion outputs: %v", err)
			}
			if string(output) != "threat_kinds=\nconclusion=skipped\nsuccess=true\nreason=\n" {
				t.Fatalf("unexpected explicit skip outputs: %s", output)
			}
		})
	}
}

func TestConcludeThreatDetectionScript_SkippedDetectionWithoutBinary(t *testing.T) {
	for _, tt := range []struct {
		name            string
		runDetection    string
		setRunDetection bool
		installOutcome  string
		continueOnError string
		wantErr         bool
		wantConclusion  string
		wantSuccess     string
	}{
		{name: "strict mode explicit skip", runDetection: "false", setRunDetection: true, installOutcome: "skipped", continueOnError: "false", wantConclusion: "conclusion=skipped", wantSuccess: "success=true"},
		{name: "warn mode explicit skip", runDetection: "false", setRunDetection: true, installOutcome: "skipped", continueOnError: "true", wantConclusion: "conclusion=skipped", wantSuccess: "success=true"},
		{name: "unset guard output", installOutcome: "skipped", continueOnError: "false", wantErr: true, wantConclusion: "conclusion=failure", wantSuccess: "success=false"},
		{name: "empty guard output", runDetection: "", setRunDetection: true, installOutcome: "skipped", continueOnError: "false", wantErr: true, wantConclusion: "conclusion=failure", wantSuccess: "success=false"},
		{name: "unexpected guard output", runDetection: "unexpected", setRunDetection: true, installOutcome: "skipped", continueOnError: "false", wantErr: true, wantConclusion: "conclusion=failure", wantSuccess: "success=false"},
		{name: "empty guard output warn mode", runDetection: "", setRunDetection: true, installOutcome: "skipped", continueOnError: "true", wantConclusion: "conclusion=warning", wantSuccess: "success=false"},
		{name: "explicit skip without skipped install", runDetection: "false", setRunDetection: true, continueOnError: "false", wantErr: true, wantConclusion: "conclusion=failure", wantSuccess: "success=false"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			binDir := filepath.Join(tmpDir, "bin")
			if err := os.MkdirAll(binDir, 0755); err != nil {
				t.Fatalf("failed to create bin directory: %v", err)
			}
			outputFile := filepath.Join(tmpDir, "github_output.txt")
			cmd := exec.Command("bash", filepath.Join("..", "..", "actions", "setup", "sh", "conclude_threat_detection.sh"), filepath.Join(tmpDir, "detection_result.json"))
			cmd.Env = make([]string, 0, len(os.Environ())+5)
			for _, env := range os.Environ() {
				if !strings.HasPrefix(env, "RUN_DETECTION=") {
					cmd.Env = append(cmd.Env, env)
				}
			}
			if tt.setRunDetection {
				cmd.Env = append(cmd.Env, "RUN_DETECTION="+tt.runDetection)
			}
			cmd.Env = append(cmd.Env,
				"THREAT_DETECT_INSTALL_OUTCOME="+tt.installOutcome,
				"DETECTION_AGENTIC_EXECUTION_OUTCOME=skipped",
				"GH_AW_DETECTION_CONTINUE_ON_ERROR="+tt.continueOnError,
				"GITHUB_OUTPUT="+outputFile,
				"PATH="+isolatedPath(t, binDir),
			)
			out, err := cmd.CombinedOutput()
			if tt.wantErr && err == nil {
				t.Fatalf("script should exit non-zero for an unavailable detector: %s", out)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("script should exit 0: %v\nOutput: %s", err, out)
			}
			outputData, err := os.ReadFile(outputFile)
			if err != nil {
				t.Fatalf("failed to read GITHUB_OUTPUT: %v", err)
			}
			outputText := string(outputData)
			if !strings.Contains(outputText, tt.wantConclusion) || !strings.Contains(outputText, tt.wantSuccess) {
				t.Fatalf("unexpected detector conclusion outputs: %s", outputData)
			}
			if tt.wantSuccess == "success=true" && !strings.Contains(outputText, "reason=\n") {
				t.Fatalf("expected an empty reason for skipped detection, got: %s", outputData)
			}
			if tt.wantSuccess == "success=false" && !strings.Contains(outputText, "reason=agent_failure") {
				t.Fatalf("expected unavailable detector reason, got: %s", outputData)
			}
			if tt.wantSuccess == "success=true" && strings.Contains(string(out), "threat-detect binary not found") {
				t.Fatalf("skipped detection should not report a missing detector: %s", out)
			}
		})
	}
}

// threatDetectEngineFailureStub mimics the outputs `threat-detect conclude`
// v0.5.2 wrote in run 39191157830 (detection engine returned HTTP 400
// model_not_supported, no detection_result.json): reason=agent_failure,
// conclusion=failure, success=false, exit 1.
func threatDetectEngineFailureStub(reason string) string {
	return "#!/usr/bin/env bash\n" +
		"echo \"reason=" + reason + "\" >> \"$GITHUB_OUTPUT\"\n" +
		"echo \"GH_AW_DETECTION_REASON=" + reason + "\" >> \"$GITHUB_ENV\"\n" +
		"echo \"::error::ERR_SYSTEM: ❌ Detection result file not found\"\n" +
		"echo \"conclusion=failure\" >> \"$GITHUB_OUTPUT\"\n" +
		"echo \"GH_AW_DETECTION_CONCLUSION=failure\" >> \"$GITHUB_ENV\"\n" +
		"echo \"success=false\" >> \"$GITHUB_OUTPUT\"\n" +
		"exit 1\n"
}

// lastKeyValue returns the last value written for key in a GitHub Actions
// key=value file, mirroring how the runner resolves duplicate keys.
func lastKeyValue(text, key string) string {
	value := ""
	for line := range strings.SplitSeq(text, "\n") {
		if v, ok := strings.CutPrefix(line, key+"="); ok {
			value = v
		}
	}
	return value
}

// TestConcludeThreatDetectionScript_EngineFailureWarnModeAppliesWarnPolicy verifies
// that in warn mode a detection engine failure reported by threat-detect conclude
// (conclusion=failure with a tooling reason) is downgraded to conclusion=warning
// with exit 0, matching the inline parse_threat_detection_results.cjs path so the
// safe-outputs warn-mode policy (WTD1a/WTD2/WTD3) applies downstream.
func TestConcludeThreatDetectionScript_EngineFailureWarnModeAppliesWarnPolicy(t *testing.T) {
	for _, reason := range []string{"agent_failure", "parse_error"} {
		t.Run(reason, func(t *testing.T) {
			tmpDir := t.TempDir()
			scriptPath := filepath.Join("..", "..", "actions", "setup", "sh", "conclude_threat_detection.sh")
			outputFile := filepath.Join(tmpDir, "github_output.txt")
			envFile := filepath.Join(tmpDir, "github_env.txt")
			binDir := filepath.Join(tmpDir, "bin")
			if err := os.MkdirAll(binDir, 0755); err != nil {
				t.Fatalf("failed to create bin dir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(binDir, "threat-detect"), []byte(threatDetectEngineFailureStub(reason)), 0755); err != nil {
				t.Fatalf("failed to write threat-detect stub: %v", err)
			}

			cmd := exec.Command("bash", scriptPath, filepath.Join(tmpDir, "detection_result.json"))
			cmd.Env = append(os.Environ(),
				"RUN_DETECTION=true",
				"DETECTION_AGENTIC_EXECUTION_OUTCOME=failure",
				"THREAT_DETECT_INSTALL_OUTCOME=success",
				"GH_AW_DETECTION_CONTINUE_ON_ERROR=true",
				"GITHUB_OUTPUT="+outputFile,
				"GITHUB_ENV="+envFile,
				"PATH="+binDir+":"+os.Getenv("PATH"),
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("script should exit 0 in warn mode on detection engine failure: %v\nOutput: %s", err, out)
			}

			outputData, err := os.ReadFile(outputFile)
			if err != nil {
				t.Fatalf("failed to read GITHUB_OUTPUT: %v", err)
			}
			envData, err := os.ReadFile(envFile)
			if err != nil {
				t.Fatalf("failed to read GITHUB_ENV: %v", err)
			}
			if got := lastKeyValue(string(outputData), "conclusion"); got != "warning" {
				t.Errorf("conclusion = %q, want warning; GITHUB_OUTPUT: %s", got, outputData)
			}
			if got := lastKeyValue(string(outputData), "success"); got != "false" {
				t.Errorf("success = %q, want false", got)
			}
			if got := lastKeyValue(string(outputData), "reason"); got != reason {
				t.Errorf("reason = %q, want %s", got, reason)
			}
			if got := lastKeyValue(string(envData), "GH_AW_DETECTION_CONCLUSION"); got != "warning" {
				t.Errorf("GH_AW_DETECTION_CONCLUSION = %q, want warning; GITHUB_ENV: %s", got, envData)
			}
			if !strings.Contains(string(out), "::warning::Threat detection engine failed") {
				t.Errorf("expected warn-mode policy warning, got: %s", out)
			}
		})
	}
}

// TestConcludeThreatDetectionScript_EngineFailureStrictModeFailsClosed verifies that
// strict mode (continue-on-error: false) still fails the step on an engine failure.
func TestConcludeThreatDetectionScript_EngineFailureStrictModeFailsClosed(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join("..", "..", "actions", "setup", "sh", "conclude_threat_detection.sh")
	outputFile := filepath.Join(tmpDir, "github_output.txt")
	envFile := filepath.Join(tmpDir, "github_env.txt")
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("failed to create bin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "threat-detect"), []byte(threatDetectEngineFailureStub("agent_failure")), 0755); err != nil {
		t.Fatalf("failed to write threat-detect stub: %v", err)
	}

	cmd := exec.Command("bash", scriptPath, filepath.Join(tmpDir, "detection_result.json"))
	cmd.Env = append(os.Environ(),
		"RUN_DETECTION=true",
		"DETECTION_AGENTIC_EXECUTION_OUTCOME=failure",
		"THREAT_DETECT_INSTALL_OUTCOME=success",
		"GH_AW_DETECTION_CONTINUE_ON_ERROR=false",
		"GITHUB_OUTPUT="+outputFile,
		"GITHUB_ENV="+envFile,
		"PATH="+binDir+":"+os.Getenv("PATH"),
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("script should fail closed in strict mode, output: %s", out)
	}
	outputData, readErr := os.ReadFile(outputFile)
	if readErr != nil {
		t.Fatalf("failed to read GITHUB_OUTPUT: %v", readErr)
	}
	if got := lastKeyValue(string(outputData), "conclusion"); got != "failure" {
		t.Errorf("conclusion = %q, want failure", got)
	}
}

// TestConcludeThreatDetectionScript_NonToolingFailureWarnModeNotDowngraded verifies
// that only tooling-failure reasons are downgraded: a failure conclusion with any
// other reason keeps the threat-detect exit status.
func TestConcludeThreatDetectionScript_NonToolingFailureWarnModeNotDowngraded(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join("..", "..", "actions", "setup", "sh", "conclude_threat_detection.sh")
	outputFile := filepath.Join(tmpDir, "github_output.txt")
	envFile := filepath.Join(tmpDir, "github_env.txt")
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("failed to create bin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "threat-detect"), []byte(threatDetectEngineFailureStub("threat_detected")), 0755); err != nil {
		t.Fatalf("failed to write threat-detect stub: %v", err)
	}

	cmd := exec.Command("bash", scriptPath, filepath.Join(tmpDir, "detection_result.json"))
	cmd.Env = append(os.Environ(),
		"RUN_DETECTION=true",
		"THREAT_DETECT_INSTALL_OUTCOME=success",
		"GH_AW_DETECTION_CONTINUE_ON_ERROR=true",
		"GITHUB_OUTPUT="+outputFile,
		"GITHUB_ENV="+envFile,
		"PATH="+binDir+":"+os.Getenv("PATH"),
	)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("script should propagate non-tooling failure, output: %s", out)
	}
	outputData, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read GITHUB_OUTPUT: %v", err)
	}
	if got := lastKeyValue(string(outputData), "conclusion"); got != "failure" {
		t.Errorf("conclusion = %q, want failure", got)
	}
}
