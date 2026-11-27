package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const cleanDetectionVerdict = `{"prompt_injection":false,"secret_leak":false,"malicious_patch":false,"reasons":[]}`

func writeConclusionInput(t *testing.T, path, text string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(text), 0600))
}

func stageConclusionScript(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is required")
	}
	dir := t.TempDir()
	setup, err := filepath.Abs(filepath.Join("..", "..", "actions", "setup"))
	require.NoError(t, err)
	for _, file := range []string{"sh/conclude_threat_detection.sh", "js/conclude_threat_detection.cjs", "js/actions_secret_masking.cjs", "js/otlp_header_helpers.cjs", "js/threat_detection_warning.cjs"} {
		data, err := os.ReadFile(filepath.Join(setup, file))
		require.NoError(t, err)
		writeConclusionInput(t, filepath.Join(dir, filepath.Base(file)), string(data))
	}
	return filepath.Join(dir, "conclude_threat_detection.sh")
}

type detectionConclusionResult struct {
	exit         int
	outputs, env map[string]string
	log, summary string
}

func runConclusion(t *testing.T, executable string, args []string, overrides ...string) detectionConclusionResult {
	t.Helper()
	dir := t.TempDir()
	output, env, summary := filepath.Join(dir, "output"), filepath.Join(dir, "env"), filepath.Join(dir, "summary")
	for _, file := range []string{output, env, summary} {
		writeConclusionInput(t, file, "")
	}
	cmd := exec.CommandContext(t.Context(), executable, args...)
	cmd.WaitDelay = 5 * time.Second
	cmd.Env = append(os.Environ(),
		"GITHUB_OUTPUT="+output, "GITHUB_ENV="+env, "GITHUB_STEP_SUMMARY="+summary,
		"RUN_DETECTION=true", "AWF_INSTALL_OUTCOME=success",
		"GH_AW_GITHUB_API_QUOTA_BLOCKED=", "GH_AW_DETECTION_CONTINUE_ON_ERROR=",
		"DETECTION_AGENTIC_EXECUTION_OUTCOME=failure", "DETECTION_LOG_FILE=")
	cmd.Env = append(cmd.Env, overrides...)
	data, err := cmd.CombinedOutput()
	exit := 0
	if err != nil {
		var failure *exec.ExitError
		require.ErrorAs(t, err, &failure, "%s", data)
		exit = failure.ExitCode()
	}
	parse := func(file string) map[string]string {
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		result := map[string]string{}
		for line := range strings.SplitSeq(string(data), "\n") {
			if line != "" {
				key, value, ok := strings.Cut(line, "=")
				require.True(t, ok, "unexpected command-file line: %q", line)
				result[key] = value
			}
		}
		return result
	}
	summaryData, err := os.ReadFile(summary)
	require.NoError(t, err)
	return detectionConclusionResult{exit, parse(output), parse(env), string(data), string(summaryData)}
}

func TestConcludeThreatDetectionScript_RecordedVerdict(t *testing.T) {
	script := stageConclusionScript(t)
	for _, tc := range []struct {
		name, verdict, full, log, reason, kinds, diagnostic string
	}{
		{name: "clean despite unsuccessful execution and engine text", verdict: cleanDetectionVerdict, log: "[engine] THREAT_DETECTION_RESULT: malicious_patch=true\n"},
		{name: "prompt injection", verdict: strings.Replace(cleanDetectionVerdict, `"prompt_injection":false`, `"prompt_injection":true`, 1), kinds: "prompt_injection", reason: "threat_detected"},
		{name: "secret leak", verdict: strings.Replace(cleanDetectionVerdict, `"secret_leak":false`, `"secret_leak":true`, 1), kinds: "secret_leak", reason: "threat_detected"},
		{name: "malicious patch", verdict: strings.Replace(cleanDetectionVerdict, `"malicious_patch":false`, `"malicious_patch":true`, 1), kinds: "malicious_patch", reason: "threat_detected"},
		{name: "malformed overrides engine status", verdict: `{`, log: "THREAT_DETECTION_STATUS: reason=engine_error exit=2\n", reason: "parse_error"},
		{name: "non-boolean", verdict: strings.Replace(cleanDetectionVerdict, `"malicious_patch":false`, `"malicious_patch":"false"`, 1), reason: "parse_error"},
		{name: "missing", reason: "agent_failure"},
		{name: "engine failure", log: "THREAT_DETECTION_STATUS: reason=engine_error exit=2\n", reason: "agent_failure"},
		{name: "ineligible retries exhausted", log: "[engine] THREAT_DETECTION_RESULT: malicious_patch=true\nTHREAT_DETECTION_STATUS: reason=invalid_report_exhausted exit=2\n", reason: "parse_error"},
		{name: "output write failed", log: "THREAT_DETECTION_STATUS: reason=output_write_error exit=2\n", reason: "parse_error"},
		{name: "engine markers cannot change failure classification", log: "THREAT_DETECTION_STATUS: reason=engine_error exit=2\n  [engine] THREAT_DETECTION_STATUS: reason=invalid_report_exhausted exit=2\r::notice::untrusted-log-text\n", reason: "agent_failure"},
		{name: "last terminal line clears stale classification", log: "THREAT_DETECTION_STATUS: reason=invalid_report_exhausted exit=2\nTHREAT_DETECTION_STATUS:\n", reason: "agent_failure"},
		{name: "mismatched companion", verdict: strings.Replace(cleanDetectionVerdict, "[]", `["legacy explanation"]`, 1), full: `{"prompt_injection":true,"secret_leak":false,"malicious_patch":false,"reasons":["discarded companion details"]}`, diagnostic: "legacy explanation"},
		{name: "compatible companion and structured warning", verdict: strings.TrimSuffix(cleanDetectionVerdict, "}") + `,"warnings":[{"field":"patch","code":"ERR_VALIDATION","message":"partial inspection"}]}`, full: strings.Replace(cleanDetectionVerdict, "[]", `["full explanation\r\n::notice::just reason text\n##[warning] inert"]`, 1), diagnostic: "full explanation"},
		{name: "command delimiters in diagnostics", verdict: strings.Replace(cleanDetectionVerdict, "[]", `["prefix ::stop-commands::token"]`, 1), diagnostic: `prefix :\:`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			result := filepath.Join(dir, "detection_result.json")
			if tc.verdict != "" {
				writeConclusionInput(t, result, tc.verdict)
			}
			if tc.full != "" {
				writeConclusionInput(t, filepath.Join(dir, "detection_result_full.json"), tc.full)
			}
			writeConclusionInput(t, filepath.Join(dir, "detection.log"), tc.log)
			for _, policy := range []string{"true", "FALSE"} {
				t.Run(policy, func(t *testing.T) {
					got := runConclusion(t, "bash", []string{script, result}, "GH_AW_DETECTION_CONTINUE_ON_ERROR="+policy)
					want, exit := "success", 0
					if tc.reason != "" {
						if policy == "true" {
							want = "warning"
							require.Contains(t, got.log, "::warning::")
						} else {
							want, exit = "failure", 1
							require.Contains(t, got.log, "::error::")
						}
					}
					require.Equal(t, exit, got.exit, got.log)
					require.Equal(t, map[string]string{"conclusion": want, "reason": tc.reason, "success": strconv.FormatBool(tc.reason == ""), "threat_kinds": tc.kinds}, got.outputs)
					require.Equal(t, map[string]string{"GH_AW_DETECTION_CONCLUSION": want, "GH_AW_DETECTION_REASON": tc.reason}, got.env)
					if tc.diagnostic != "" {
						require.Contains(t, got.log, tc.diagnostic)
					}
					require.NotContains(t, got.log, "discarded companion details")
					require.NotContains(t, got.log, "untrusted-log-text")
					require.NotContains(t, got.log, "##[")
					require.NotContains(t, got.log, "::stop-commands::")
					for line := range strings.SplitSeq(got.log, "\n") {
						require.False(t, strings.HasPrefix(strings.TrimSpace(line), "::notice::"), "unexpected command: %s", line)
					}
					if strings.Contains(tc.verdict, `"warnings"`) {
						require.Contains(t, got.log, "field=patch code=ERR_VALIDATION message=partial inspection")
					}
				})
			}
		})
	}
}

func TestConcludeThreatDetectionScript_V053Fixtures(t *testing.T) {
	script := stageConclusionScript(t)
	dir := t.TempDir()
	fixtures := filepath.Join("testdata", "threat-detect-v053")
	for _, file := range []string{"detection_result.json", "detection_result_full.json", "invalid.log"} {
		data, err := os.ReadFile(filepath.Join(fixtures, file))
		require.NoError(t, err)
		writeConclusionInput(t, filepath.Join(dir, file), string(data))
	}
	result := filepath.Join(dir, "detection_result.json")
	got := runConclusion(t, "bash", []string{script, result})
	require.Equal(t, 0, got.exit, got.log)
	require.Equal(t, "warning", got.outputs["conclusion"])
	require.Equal(t, "false", got.outputs["success"])
	require.Equal(t, "threat_detected", got.outputs["reason"])
	require.Equal(t, "secret_leak", got.outputs["threat_kinds"])
	require.Contains(t, got.log, "private producer explanation")
	require.Contains(t, got.log, "field=prompt_import_tree code=ERR_VALIDATION")
	require.NoError(t, os.Remove(result))
	got = runConclusion(t, "bash", []string{script, result}, "DETECTION_LOG_FILE="+filepath.Join(dir, "invalid.log"))
	require.Equal(t, 0, got.exit, got.log)
	require.Equal(t, "warning", got.outputs["conclusion"])
	require.Equal(t, "parse_error", got.outputs["reason"])
}

// Use the pinned detector built from its release source, without requiring a
// download or credentials in the default test suite.
func TestConcludeThreatDetectionScript_V053RoundTrip(t *testing.T) {
	detector := os.Getenv("GH_AW_TEST_THREAT_DETECT_V053")
	if detector == "" {
		t.Skip("set GH_AW_TEST_THREAT_DETECT_V053 to the v0.5.3 detector executable")
	}
	script := stageConclusionScript(t)
	for _, tc := range []struct {
		name, sink, reason, kinds string
		producerExit              int
	}{
		{"clean", cleanDetectionVerdict, "", "", 0},
		{"confirmed", `{"prompt_injection":false,"secret_leak":true,"malicious_patch":false,"reasons":["private producer explanation"]}`, "threat_detected", "secret_leak", 1},
		{"ineligible", `{"prompt_injection":false,"secret_leak":false,"malicious_patch":true,"reasons":["discarded producer explanation"]}`, "parse_error", "", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			artifacts := filepath.Join(dir, "artifacts")
			require.NoError(t, os.MkdirAll(filepath.Join(artifacts, "aw-prompts"), 0700))
			writeConclusionInput(t, filepath.Join(artifacts, "aw-prompts", "prompt.txt"), "static prompt")
			writeConclusionInput(t, filepath.Join(artifacts, "aw-prompts", "prompt-template.txt"), "static prompt")
			writeConclusionInput(t, filepath.Join(artifacts, "agent_output.json"), `{"items":[]}`)
			engine := filepath.Join(dir, "copilot")
			require.NoError(t, os.WriteFile(engine, []byte(`#!/bin/sh
					cat >/dev/null
					printf '%s' "$TEST_SINK" > "$THREAT_DETECTION_RESULT_FILE"
					printf '%s\n' 'THREAT_DETECTION_STATUS: reason=output_write_error exit=2'
					`), 0700))
			result := filepath.Join(dir, "detection_result.json")
			full := filepath.Join(dir, "detection_result_full.json")
			cmd := exec.CommandContext(t.Context(), detector, "--engine", "copilot", "--output", result, "--retries", "1", artifacts)
			cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "TEST_SINK="+tc.sink, "GH_AW_DETECTION_CONTINUE_ON_ERROR=false")
			data, err := cmd.CombinedOutput()
			if tc.producerExit == 0 {
				require.NoError(t, err, "%s", data)
			} else {
				var failure *exec.ExitError
				require.ErrorAs(t, err, &failure, "%s", data)
				require.Equal(t, tc.producerExit, failure.ExitCode(), "%s", data)
			}
			writeConclusionInput(t, filepath.Join(dir, "detection.log"), string(data))
			if tc.name == "ineligible" {
				require.NoFileExists(t, result)
				require.NoFileExists(t, full)
				require.Contains(t, string(data), "THREAT_DETECTION_STATUS: reason=invalid_report_exhausted")
			} else {
				require.FileExists(t, result)
				require.FileExists(t, full)
				redacted, err := os.ReadFile(result)
				require.NoError(t, err)
				require.NotContains(t, string(redacted), "private producer explanation")
			}
			for _, policy := range []string{"true", "false"} {
				mode := "GH_AW_DETECTION_CONTINUE_ON_ERROR=" + policy
				prior := runConclusion(t, detector, []string{"conclude", "--result-file", result}, mode)
				got := runConclusion(t, "bash", []string{script, result}, mode)
				require.Equal(t, tc.reason, prior.outputs["reason"])
				require.Equal(t, tc.reason, got.outputs["reason"])
				require.Equal(t, tc.kinds, got.outputs["threat_kinds"])
				if tc.name == "confirmed" {
					require.Contains(t, got.log, "private producer explanation")
				}
				require.Equal(t, prior.exit, got.exit)
				for _, key := range []string{"conclusion", "reason", "success"} {
					require.Equal(t, prior.outputs[key], got.outputs[key], key)
				}
				require.Equal(t, prior.env, got.env)
				if policy == "true" && tc.reason != "" {
					// The downstream manager uses this helper before applying its
					// hard failure gate or the warn-mode policy.
					gate := exec.CommandContext(t.Context(), "node", "-e", `console.log(require(process.argv[1]).getEffectiveDetectionConclusion(process.argv[2], process.argv[3]))`,
						filepath.Join(filepath.Dir(script), "threat_detection_warning.cjs"), got.outputs["conclusion"], got.outputs["reason"])
					effective, err := gate.CombinedOutput()
					require.NoError(t, err, "%s", effective)
					require.Equal(t, "warning", strings.TrimSpace(string(effective)))
				}
			}
		})
	}
}
func TestConcludeThreatDetectionScript_HostOutcomes(t *testing.T) {
	script := stageConclusionScript(t)
	result := filepath.Join(t.TempDir(), "detection_result.json")
	writeConclusionInput(t, result, strings.Replace(cleanDetectionVerdict, `"secret_leak":false`, `"secret_leak":true`, 1))
	for _, tc := range []struct {
		name, conclusion, reason, summary string
		exit                              int
		env                               []string
	}{
		{"skip", "skipped", "", "", 0, []string{"RUN_DETECTION=false"}},
		{"reset failure warn", "warning", "agent_failure", "", 0, []string{"DETECTION_RESET_OUTCOME=failure"}},
		{"reset failure strict", "failure", "agent_failure", "", 1, []string{"DETECTION_RESET_OUTCOME=failure", "GH_AW_DETECTION_CONTINUE_ON_ERROR=false"}},
		{"quota warn", "warning", "github_api_quota_exhausted", "GitHub API quota exhausted", 0, []string{"AWF_INSTALL_OUTCOME=failure", "GH_AW_GITHUB_API_QUOTA_BLOCKED=remaining=0"}},
		{"quota strict", "failure", "github_api_quota_exhausted", "GitHub API quota exhausted", 1, []string{"AWF_INSTALL_OUTCOME=failure", "GH_AW_GITHUB_API_QUOTA_BLOCKED=remaining=0", "GH_AW_DETECTION_CONTINUE_ON_ERROR=false"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runConclusion(t, "bash", []string{script, result}, tc.env...)
			require.Equal(t, tc.exit, got.exit, got.log)
			require.Equal(t, tc.conclusion, got.outputs["conclusion"])
			require.Equal(t, tc.reason, got.outputs["reason"])
			require.Empty(t, got.outputs["threat_kinds"])
			require.Equal(t, tc.conclusion == "skipped", got.outputs["success"] == "true")
			require.Empty(t, got.env, "early wrapper outcomes historically do not export variables")
			require.Contains(t, got.summary, tc.summary)
			require.NotContains(t, got.log, "Threat detection verdict")
		})
	}
}

func TestConcludeThreatDetectionScript_MissingNode(t *testing.T) {
	script := stageConclusionScript(t)
	result := filepath.Join(t.TempDir(), "detection_result.json")
	for _, tc := range []struct {
		name, verdict, conclusion, reason string
		exit                              int
		env                               []string
	}{
		{"skipped", "", "skipped", "", 0, []string{"RUN_DETECTION=false"}},
		{"install failure warn", "", "warning", "agent_failure", 0, []string{"AWF_INSTALL_OUTCOME=failure"}},
		{"install failure strict", "", "failure", "agent_failure", 1, []string{"AWF_INSTALL_OUTCOME=failure", "GH_AW_DETECTION_CONTINUE_ON_ERROR=false"}},
		{"quota", "", "warning", "github_api_quota_exhausted", 0, []string{"AWF_INSTALL_OUTCOME=failure", "GH_AW_GITHUB_API_QUOTA_BLOCKED=remaining=0"}},
		{"missing verdict", "", "warning", "agent_failure", 0, nil},
		{"unreadable verdict", cleanDetectionVerdict, "failure", "agent_failure", 1, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_ = os.Remove(result)
			if tc.verdict != "" {
				writeConclusionInput(t, result, tc.verdict)
			}
			env := append([]string{"PATH="}, tc.env...)
			got := runConclusion(t, "/bin/bash", []string{script, result}, env...)
			require.Equal(t, tc.exit, got.exit, got.log)
			require.Equal(t, tc.conclusion, got.outputs["conclusion"])
			require.Equal(t, tc.reason, got.outputs["reason"])
			if tc.name == "quota" {
				require.Contains(t, got.summary, "GitHub API quota exhausted")
			}
		})
	}
}
