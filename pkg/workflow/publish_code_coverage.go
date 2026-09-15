package workflow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
)

var publishCodeCoverageLog = logger.New("workflow:publish_code_coverage")

// defaultCodeCoverageWaitForProcessingTimeout is the default number of seconds
// actions/upload-code-coverage waits for GitHub to finish processing an upload
// before failing the step. Matches the action's own documented default.
const defaultCodeCoverageWaitForProcessingTimeout = 160

// codeCoverageStagingDirExpr is the GitHub Actions expression form of the staging directory
// where the agent stages the coverage report file before calling the upload_code_coverage tool.
// actions/upload-artifact and actions/download-artifact do not expand shell variables in
// their `path:` inputs, so we must use ${{ runner.temp }} here (mirrors artifactStagingDirExpr).
const codeCoverageStagingDirExpr = "${{ runner.temp }}/gh-aw/safeoutputs/upload-code-coverage/"

// codeCoverageDownloadPath is the location where the upload_code_coverage job
// downloads the staging artifact before invoking actions/upload-code-coverage.
const codeCoverageDownloadPath = "/tmp/gh-aw/upload-code-coverage/"

// SafeOutputsUploadCodeCoverageStagingArtifactName is the artifact that carries the staging
// directory (containing the staged coverage report file) from the agent job to the
// upload_code_coverage job.
const SafeOutputsUploadCodeCoverageStagingArtifactName = "safe-outputs-upload-code-coverage"

// UploadCodeCoverageConfig holds configuration for uploading a code coverage report via
// actions/upload-code-coverage from agent output.
type UploadCodeCoverageConfig struct {
	BaseSafeOutputConfig     `yaml:",inline"`
	FailOnError              *bool `yaml:"fail-on-error,omitempty"`               // Fixed fail-on-error input for actions/upload-code-coverage (default: true); agent cannot override
	WaitForProcessingTimeout int   `yaml:"wait-for-processing-timeout,omitempty"` // Fixed wait-for-processing-timeout in seconds (default: 160); agent cannot override
}

// parseUploadCodeCoverageConfig handles upload-code-coverage configuration
func (c *Compiler) parseUploadCodeCoverageConfig(outputMap map[string]any) *UploadCodeCoverageConfig {
	configData, exists := outputMap["upload-code-coverage"]
	if !exists {
		return nil
	}

	// Explicit false disables upload-code-coverage (e.g. when passed via import-inputs)
	if b, ok := configData.(bool); ok && !b {
		publishCodeCoverageLog.Print("upload-code-coverage explicitly set to false, skipping")
		return nil
	}

	publishCodeCoverageLog.Print("Parsing upload-code-coverage configuration")
	trueVal := true
	config := &UploadCodeCoverageConfig{
		FailOnError:              &trueVal,
		WaitForProcessingTimeout: defaultCodeCoverageWaitForProcessingTimeout,
	}

	if configMap, ok := configData.(map[string]any); ok {
		if failOnError, exists := configMap["fail-on-error"]; exists {
			if b, ok := failOnError.(bool); ok {
				config.FailOnError = &b
			}
		}

		if timeout, exists := configMap["wait-for-processing-timeout"]; exists {
			if timeoutInt, ok := typeutil.ParseIntValue(timeout); ok && timeoutInt >= 0 {
				config.WaitForProcessingTimeout = timeoutInt
			}
		}

		// Parse common base fields with default max of 1 (single coverage report per run)
		c.parseBaseSafeOutputConfig(configMap, &config.BaseSafeOutputConfig, 1)
	} else if configData == nil {
		// Handle null case ("upload-code-coverage:" with no value): use defaults, max 1
		config.Max = defaultIntStr(1)
	}

	publishCodeCoverageLog.Printf("Parsed upload-code-coverage config: fail_on_error=%v, wait_for_processing_timeout=%d",
		*config.FailOnError, config.WaitForProcessingTimeout)
	return config
}

// generateSafeOutputsCodeCoverageStagingUpload generates a step in the main agent job that
// uploads the upload-code-coverage staging directory (containing the coverage report file
// staged by the agent) so the separate upload_code_coverage job can download it and pass it
// to actions/upload-code-coverage. This step only appears when upload-code-coverage is
// configured in safe-outputs.
// pinAction resolves the upload-artifact action reference; pass c.getActionPin from Compiler methods.
func generateSafeOutputsCodeCoverageStagingUpload(builder *strings.Builder, data *WorkflowData, pinAction func(string) string) {
	if data.SafeOutputs == nil || data.SafeOutputs.UploadCodeCoverage == nil {
		return
	}

	publishCodeCoverageLog.Print("Generating safe-outputs upload-code-coverage staging upload step")

	// In workflow_call context, apply the per-invocation prefix to avoid artifact name clashes.
	prefix := artifactPrefixExprForDownstreamJob(data)

	builder.WriteString("      # Upload safe-outputs upload-code-coverage staging for the upload_code_coverage job\n")
	builder.WriteString("      - name: Upload upload-code-coverage staging\n")
	builder.WriteString("        if: always()\n")
	fmt.Fprintf(builder, "        uses: %s\n", pinAction("actions/upload-artifact"))
	builder.WriteString("        with:\n")
	fmt.Fprintf(builder, "          name: %s%s\n", prefix, SafeOutputsUploadCodeCoverageStagingArtifactName)
	fmt.Fprintf(builder, "          path: %s\n", codeCoverageStagingDirExpr)
	builder.WriteString("          retention-days: 1\n")
	builder.WriteString("          if-no-files-found: ignore\n")
}

// buildUploadCodeCoverageJob creates a dedicated job that uploads the code coverage report
// staged by the agent to GitHub's code coverage API via actions/upload-code-coverage.
//
// This is a separate job (not a step inside safe_outputs) so that the real, third-party
// action can be invoked with a literal `uses:` step and its own dedicated permissions
// (code-quality: write), without affecting the consolidated safe_outputs job.
//
// The job:
//   - depends on the agent job (to download the staged coverage file artifact) and on
//     safe_outputs (for the file/language/label metadata recorded by the
//     upload_code_coverage handler)
//   - runs only when the safe_outputs job exported a coverage file
//     (the job condition checks that upload_code_coverage_file is non-empty)
//   - does not perform a checkout: actions/upload-code-coverage resolves the commit/ref/PR
//     number itself from the triggering event context
func (c *Compiler) buildUploadCodeCoverageJob(data *WorkflowData, mainJobName string) (*Job, error) {
	publishCodeCoverageLog.Print("Building upload_code_coverage job")

	if data.SafeOutputs == nil || data.SafeOutputs.UploadCodeCoverage == nil {
		return nil, errors.New("safe-outputs.upload-code-coverage configuration is required")
	}
	cfg := data.SafeOutputs.UploadCodeCoverage

	// Artifact prefix for workflow_call context: this job depends directly on the agent job,
	// so it must use the agent-job-relative prefix expression (mirrors upload_assets).
	agentArtifactPrefix := artifactPrefixExprForAgentDownstreamJob(data)
	permissions := NewPermissionsContentsReadCodeQualityWritePRRead()

	var steps []string

	// Download the coverage staging artifact produced by the agent job.
	const downloadStepID = "download_upload_code_coverage_staging"
	continueOnDownloadError := false
	downloadSteps := buildArtifactDownloadSteps(ArtifactDownloadConfig{
		ArtifactName:    agentArtifactPrefix + SafeOutputsUploadCodeCoverageStagingArtifactName,
		DownloadPath:    codeCoverageDownloadPath,
		StepName:        "Download upload-code-coverage staging",
		StepID:          downloadStepID,
		ContinueOnError: &continueOnDownloadError,
	}, c.getActionPin)
	steps = append(steps, downloadSteps...)

	// The local coverage file path is the downloaded staging directory plus the
	// staging-relative path recorded by the upload_code_coverage handler.
	localCoveragePath := codeCoverageDownloadPath + "${{ needs." + string(constants.SafeOutputsJobName) + ".outputs.upload_code_coverage_file }}"

	failOnError := "true"
	if cfg.FailOnError != nil && !*cfg.FailOnError {
		failOnError = "false"
	}
	waitForProcessingTimeout := cfg.WaitForProcessingTimeout

	var coverageToken string
	effectiveStaticToken := cfg.GitHubToken
	if effectiveStaticToken == "" && data.SafeOutputs != nil {
		effectiveStaticToken = data.SafeOutputs.GitHubToken
	}
	if effectiveStaticToken != "" {
		coverageToken = resolveSafeOutputGitHubToken(effectiveStaticToken)
	} else {
		var coverageApp *GitHubAppConfig
		if cfg.GitHubApp != nil {
			coverageApp = cfg.GitHubApp
		} else if data.SafeOutputs != nil {
			coverageApp = data.SafeOutputs.GitHubApp
		}
		if coverageApp != nil {
			const appTokenStepID = "upload-code-coverage-app-token"
			for _, step := range c.buildGitHubAppTokenMintStep(coverageApp, permissions, "") {
				step = strings.ReplaceAll(step, "safe-outputs-app-token-owner", appTokenStepID+"-owner")
				step = strings.ReplaceAll(step, "safe-outputs-app-token", appTokenStepID)
				steps = append(steps, step)
			}
			if coverageApp.shouldIgnoreMissingKey() {
				coverageToken = combineTokenExpressions(
					fmt.Sprintf("${{ steps.%s.outputs.token }}", appTokenStepID),
					resolveSafeOutputGitHubToken(""),
				)
			} else {
				coverageToken = fmt.Sprintf("${{ steps.%s.outputs.token }}", appTokenStepID)
			}
		} else {
			coverageToken = resolveSafeOutputGitHubToken("")
		}
	}

	steps = append(steps, "      - name: Verify code coverage report\n")
	steps = append(steps, "        env:\n")
	steps = append(steps, fmt.Sprintf("          COVERAGE_FILE: %s\n", localCoveragePath))
	steps = append(steps, "        run: |\n")
	steps = append(steps, "          test -s \"$COVERAGE_FILE\"\n")
	steps = append(steps, "      - name: Upload code coverage report\n")
	steps = append(steps, fmt.Sprintf("        id: %s\n", constants.UploadCodeCoverageJobName))
	steps = append(steps, fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/upload-code-coverage")))
	steps = append(steps, "        with:\n")
	steps = append(steps, fmt.Sprintf("          file: %s\n", localCoveragePath))
	steps = append(steps, fmt.Sprintf("          language: ${{ needs.%s.outputs.upload_code_coverage_language }}\n", constants.SafeOutputsJobName))
	steps = append(steps, fmt.Sprintf("          label: ${{ needs.%s.outputs.upload_code_coverage_label }}\n", constants.SafeOutputsJobName))
	steps = append(steps, fmt.Sprintf("          fail-on-error: %s\n", failOnError))
	steps = append(steps, "          # Timeout is in seconds; 160 matches actions/upload-code-coverage's documented default.\n")
	steps = append(steps, fmt.Sprintf("          wait-for-processing-timeout: %d\n", waitForProcessingTimeout))
	steps = append(steps, fmt.Sprintf("          token: %s\n", coverageToken))

	// The job only runs when the safe_outputs job exported a non-empty coverage file path.
	jobCondition := fmt.Sprintf("needs.%s.outputs.upload_code_coverage_file != ''", constants.SafeOutputsJobName)

	job := &Job{
		Name:           string(constants.UploadCodeCoverageJobName),
		If:             jobCondition,
		RunsOn:         c.formatFrameworkJobRunsOn(data),
		Environment:    c.indentYAMLLines(resolveSafeOutputsEnvironment(data), "    "),
		Permissions:    permissions.RenderToYAML(),
		TimeoutMinutes: 10,
		Steps:          steps,
		Needs:          []string{mainJobName, string(constants.SafeOutputsJobName)},
	}

	publishCodeCoverageLog.Print("Built upload_code_coverage job")
	return job, nil
}
