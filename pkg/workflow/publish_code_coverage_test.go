//go:build !integration

package workflow

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseUploadCodeCoverageConfig(t *testing.T) {
	c := &Compiler{}

	tests := []struct {
		name     string
		input    map[string]any
		expected *UploadCodeCoverageConfig
	}{
		{
			name: "upload-code-coverage config with custom values",
			input: map[string]any{
				"upload-code-coverage": map[string]any{
					"fail-on-error":               false,
					"wait-for-processing-timeout": 60,
					"github-token":                "${{ secrets.CUSTOM_TOKEN }}",
				},
			},
			expected: &UploadCodeCoverageConfig{
				FailOnError:              boolPtr(false),
				WaitForProcessingTimeout: 60,
				BaseSafeOutputConfig:     BaseSafeOutputConfig{GitHubToken: "${{ secrets.CUSTOM_TOKEN }}", Max: strPtr("1")},
			},
		},
		{
			name: "upload-code-coverage config with defaults (empty map)",
			input: map[string]any{
				"upload-code-coverage": map[string]any{},
			},
			expected: &UploadCodeCoverageConfig{
				FailOnError:              boolPtr(true),
				WaitForProcessingTimeout: defaultCodeCoverageWaitForProcessingTimeout,
				BaseSafeOutputConfig:     BaseSafeOutputConfig{Max: strPtr("1")},
			},
		},
		{
			name: "upload-code-coverage config null value uses defaults",
			input: map[string]any{
				"upload-code-coverage": nil,
			},
			expected: &UploadCodeCoverageConfig{
				FailOnError:              boolPtr(true),
				WaitForProcessingTimeout: defaultCodeCoverageWaitForProcessingTimeout,
				BaseSafeOutputConfig:     BaseSafeOutputConfig{Max: strPtr("1")},
			},
		},
		{
			name: "upload-code-coverage explicitly disabled",
			input: map[string]any{
				"upload-code-coverage": false,
			},
			expected: nil,
		},
		{
			name:     "no upload-code-coverage config",
			input:    map[string]any{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.parseUploadCodeCoverageConfig(tt.input)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Fatalf("expected %+v, got nil", tt.expected)
			}

			if result.FailOnError == nil || tt.expected.FailOnError == nil || *result.FailOnError != *tt.expected.FailOnError {
				t.Errorf("FailOnError = %v, want %v", result.FailOnError, tt.expected.FailOnError)
			}
			if result.WaitForProcessingTimeout != tt.expected.WaitForProcessingTimeout {
				t.Errorf("WaitForProcessingTimeout = %d, want %d", result.WaitForProcessingTimeout, tt.expected.WaitForProcessingTimeout)
			}
			if result.GitHubToken != tt.expected.GitHubToken {
				t.Errorf("GitHubToken = %q, want %q", result.GitHubToken, tt.expected.GitHubToken)
			}
			gotMax := ""
			if result.Max != nil {
				gotMax = *result.Max
			}
			wantMax := ""
			if tt.expected.Max != nil {
				wantMax = *tt.expected.Max
			}
			if gotMax != wantMax {
				t.Errorf("Max = %q, want %q", gotMax, wantMax)
			}
		})
	}
}

func TestUploadCodeCoverageExperimentalWarning(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			UploadCodeCoverage: &UploadCodeCoverageConfig{},
		},
	}

	c := NewCompiler()
	var buf bytes.Buffer
	c.emitExperimentalFeatureWarningsTo(data, &buf)

	expectedMessage := "Using experimental feature: upload-code-coverage"
	if !strings.Contains(buf.String(), expectedMessage) {
		t.Fatalf("expected warning containing %q, got stderr:\n%s", expectedMessage, buf.String())
	}
	if c.GetWarningCount() == 0 {
		t.Fatal("expected warning count > 0")
	}
}

func TestBuildUploadCodeCoverageJob(t *testing.T) {
	c := NewCompiler()
	data := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			UploadCodeCoverage: &UploadCodeCoverageConfig{
				FailOnError:              boolPtr(true),
				WaitForProcessingTimeout: 160,
			},
		},
	}

	job, err := c.buildUploadCodeCoverageJob(data, "agent")
	if err != nil {
		t.Fatalf("Failed to build upload_code_coverage job: %v", err)
	}
	if job == nil {
		t.Fatal("Expected non-nil job")
	}

	var stepsStrSb strings.Builder
	for _, step := range job.Steps {
		stepsStrSb.WriteString(step)
	}
	stepsStr := stepsStrSb.String()

	if !strings.Contains(stepsStr, "actions/upload-code-coverage") {
		t.Error("Expected step to reference actions/upload-code-coverage")
	}
	if !strings.Contains(stepsStr, "language: ${{ needs.safe_outputs.outputs.upload_code_coverage_language }}") {
		t.Error("Expected language input to be wired from safe_outputs job outputs")
	}
	if !strings.Contains(stepsStr, "label: ${{ needs.safe_outputs.outputs.upload_code_coverage_label }}") {
		t.Error("Expected label input to be wired from safe_outputs job outputs")
	}
	if !strings.Contains(stepsStr, "fail-on-error: true") {
		t.Error("Expected fail-on-error: true to be rendered")
	}
	if !strings.Contains(stepsStr, "wait-for-processing-timeout: 160") {
		t.Error("Expected wait-for-processing-timeout: 160 to be rendered")
	}
	if !strings.Contains(stepsStr, "# Timeout is in seconds; 160 matches actions/upload-code-coverage's documented default.") {
		t.Error("Expected wait-for-processing-timeout comment")
	}
	if !strings.Contains(stepsStr, "token: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}") {
		t.Error("Expected upload-code-coverage token fallback to be rendered")
	}
	if !strings.Contains(stepsStr, "Download upload-code-coverage staging") {
		t.Error("Expected download step for staging artifact")
	}
	if !strings.Contains(stepsStr, "id: download_upload_code_coverage_staging") {
		t.Error("Expected download step ID for staging artifact")
	}
	if strings.Contains(stepsStr, "continue-on-error: true") {
		t.Error("Expected upload-code-coverage artifact download failures to fail the job")
	}
	if !strings.Contains(stepsStr, "Verify code coverage report") {
		t.Error("Expected coverage report verification step")
	}
	if !strings.Contains(stepsStr, "COVERAGE_FILE: /tmp/gh-aw/upload-code-coverage/${{ needs.safe_outputs.outputs.upload_code_coverage_file }}") {
		t.Error("Expected verification step to receive the downloaded coverage file path")
	}
	if !strings.Contains(stepsStr, `test -s "$COVERAGE_FILE"`) {
		t.Error("Expected verification step to require the downloaded coverage file")
	}

	if job.If != "needs.safe_outputs.outputs.upload_code_coverage_file != ''" {
		t.Errorf("Unexpected job condition: %s", job.If)
	}

	foundContents := false
	foundCodeQuality := false
	for line := range strings.SplitSeq(job.Permissions, "\n") {
		if strings.Contains(line, "contents:") && strings.Contains(line, "read") {
			foundContents = true
		}
		if strings.Contains(line, "code-quality:") && strings.Contains(line, "write") {
			foundCodeQuality = true
		}
	}
	if !foundContents {
		t.Errorf("Expected contents: read permission, got: %s", job.Permissions)
	}
	if !foundCodeQuality {
		t.Errorf("Expected code-quality: write permission, got: %s", job.Permissions)
	}

	if len(job.Needs) != 2 || job.Needs[0] != "agent" || job.Needs[1] != "safe_outputs" {
		t.Errorf("Expected job.Needs = [agent, safe_outputs], got %v", job.Needs)
	}
}

func TestBuildUploadCodeCoverageJobRespectsZeroWaitTimeout(t *testing.T) {
	c := NewCompiler()
	data := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			UploadCodeCoverage: &UploadCodeCoverageConfig{
				FailOnError:              boolPtr(true),
				WaitForProcessingTimeout: 0,
			},
		},
	}

	job, err := c.buildUploadCodeCoverageJob(data, "agent")
	if err != nil {
		t.Fatalf("Failed to build upload_code_coverage job: %v", err)
	}

	var stepsStr strings.Builder
	for _, step := range job.Steps {
		stepsStr.WriteString(step)
	}
	if !strings.Contains(stepsStr.String(), "wait-for-processing-timeout: 0") {
		t.Error("Expected wait-for-processing-timeout: 0 to be rendered")
	}
}

func TestBuildUploadCodeCoverageJobUsesSafeOutputsGitHubTokenFallback(t *testing.T) {
	c := NewCompiler()
	data := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			GitHubToken: "${{ secrets.SAFE_OUTPUTS_TOKEN }}",
			UploadCodeCoverage: &UploadCodeCoverageConfig{
				FailOnError:              boolPtr(true),
				WaitForProcessingTimeout: 160,
			},
		},
	}

	job, err := c.buildUploadCodeCoverageJob(data, "agent")
	if err != nil {
		t.Fatalf("Failed to build upload_code_coverage job: %v", err)
	}

	var stepsStr strings.Builder
	for _, step := range job.Steps {
		stepsStr.WriteString(step)
	}
	if !strings.Contains(stepsStr.String(), "token: ${{ secrets.SAFE_OUTPUTS_TOKEN }}") {
		t.Error("Expected safe-outputs.github-token fallback token to be rendered")
	}
}

func TestBuildUploadCodeCoverageJobMintsGitHubAppToken(t *testing.T) {
	c := NewCompiler()
	data := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			UploadCodeCoverage: &UploadCodeCoverageConfig{
				FailOnError:              boolPtr(true),
				WaitForProcessingTimeout: 160,
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubApp: &GitHubAppConfig{
						AppID:      "${{ vars.APP_ID }}",
						PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
					},
				},
			},
		},
	}

	job, err := c.buildUploadCodeCoverageJob(data, "agent")
	if err != nil {
		t.Fatalf("Failed to build upload_code_coverage job: %v", err)
	}

	var stepsStr strings.Builder
	for _, step := range job.Steps {
		stepsStr.WriteString(step)
	}
	out := stepsStr.String()
	if !strings.Contains(out, "id: upload-code-coverage-app-token") {
		t.Error("Expected upload-code-coverage app token mint step ID")
	}
	if !strings.Contains(out, "uses: actions/create-github-app-token@") {
		t.Error("Expected GitHub App token mint action step")
	}
	if !strings.Contains(out, "token: ${{ steps.upload-code-coverage-app-token.outputs.token }}") {
		t.Error("Expected upload action token to use minted GitHub App token")
	}
}

func TestBuildUploadCodeCoverageJobMissingConfig(t *testing.T) {
	c := NewCompiler()
	data := &WorkflowData{
		Name:        "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{},
	}

	_, err := c.buildUploadCodeCoverageJob(data, "agent")
	if err == nil {
		t.Fatal("Expected error when upload-code-coverage configuration is missing")
	}
}

func TestGenerateSafeOutputsCodeCoverageStagingUpload(t *testing.T) {
	c := NewCompiler()
	data := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			UploadCodeCoverage: &UploadCodeCoverageConfig{
				FailOnError: boolPtr(true),
			},
		},
	}

	var builder strings.Builder
	generateSafeOutputsCodeCoverageStagingUpload(&builder, data, c.getActionPin)
	out := builder.String()

	if !strings.Contains(out, SafeOutputsUploadCodeCoverageStagingArtifactName) {
		t.Errorf("Expected staging artifact name %q in output, got: %s", SafeOutputsUploadCodeCoverageStagingArtifactName, out)
	}
	if !strings.Contains(out, "actions/upload-artifact") {
		t.Error("Expected staging upload step to use actions/upload-artifact")
	}
	if !strings.Contains(out, "if: always() && steps.redact_secrets.outcome == 'success'") {
		t.Error("Expected staging upload step to require successful secret redaction")
	}
}

func TestGenerateSafeOutputsCodeCoverageStagingUploadNoConfig(t *testing.T) {
	data := &WorkflowData{
		Name:        "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{},
	}

	var builder strings.Builder
	generateSafeOutputsCodeCoverageStagingUpload(&builder, data, func(string) string { return "" })
	if builder.Len() != 0 {
		t.Errorf("Expected no output when upload-code-coverage is not configured, got: %s", builder.String())
	}
}
