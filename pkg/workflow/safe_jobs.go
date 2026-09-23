package workflow

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var safeJobsLog = logger.New("workflow:safe_jobs")

// SafeJobConfig defines a safe job configuration with GitHub Actions job properties
type SafeJobConfig struct {
	// Standard GitHub Actions job properties
	Name           string            `yaml:"name,omitempty"`
	Description    string            `yaml:"description,omitempty"`
	RunsOn         string            `yaml:"runs-on,omitempty"`
	If             string            `yaml:"if,omitempty"`
	Needs          []string          `yaml:"needs,omitempty"`
	Steps          []any             `yaml:"steps,omitempty"`
	Env            map[string]string `yaml:"env,omitempty"`
	Permissions    map[string]string `yaml:"permissions,omitempty"`
	RawPermissions any               `yaml:"-"`

	// Additional safe-job specific properties
	Inputs      map[string]*InputDefinition `yaml:"inputs,omitempty"`
	GitHubToken string                      `yaml:"github-token,omitempty"`
	Output      string                      `yaml:"output,omitempty"`
	Max         int                         `yaml:"max,omitempty"` // Maximum number of times this output type may be emitted per run (default: 1)
	// Artifacts declares additional agent-job filesystem paths (files, directories, or globs
	// rooted under /tmp/gh-aw/) that this job's steps depend on. The compiler persists these
	// paths in the unified "agent" artifact upload so the job can read them after the agent
	// job completes. Without declaring a path here, any file the agent writes outside the
	// compiler's built-in known paths is silently dropped and never reaches this job.
	Artifacts      []string `yaml:"artifacts,omitempty"`
	runsOnError    error    `yaml:"-"`
	artifactsError error    `yaml:"-"`
}

// parseSafeJobsConfig parses safe-jobs configuration from a jobs map.
// This function expects a map of job configurations directly (from safe-outputs.jobs).
// The top-level "safe-jobs" key is NOT supported - only "safe-outputs.jobs" is valid.
func (c *Compiler) parseSafeJobsConfig(jobsMap map[string]any) map[string]*SafeJobConfig { //nolint:largefunc // Sequential field-by-field parsing of the safe-job config map; splitting would obscure the direct mapping to frontmatter keys.
	if jobsMap == nil {
		return nil
	}

	safeJobsLog.Printf("Parsing %d safe-jobs from jobs map", len(jobsMap))
	result := make(map[string]*SafeJobConfig)

	for jobName, jobValue := range jobsMap {
		jobConfig, ok := jobValue.(map[string]any)
		if !ok {
			continue
		}

		safeJob := &SafeJobConfig{}

		// Parse name
		if name, exists := jobConfig["name"]; exists {
			if nameStr, ok := name.(string); ok {
				safeJob.Name = nameStr
			}
		}

		// Parse description
		if description, exists := jobConfig["description"]; exists {
			if descStr, ok := description.(string); ok {
				safeJob.Description = descStr
			}
		}

		// Parse runs-on using the shared custom-job parser.
		runsOnJob := &Job{}
		if err := c.extractCustomJobRunsOn(runsOnJob, jobName, jobConfig); err != nil {
			safeJob.runsOnError = err
		} else if isEmptySafeJobRunsOn(jobConfig["runs-on"]) {
			safeJob.RunsOn = ""
		} else {
			safeJob.RunsOn = runsOnJob.RunsOn
		}

		// Parse if condition
		if ifCond, exists := jobConfig["if"]; exists {
			if ifStr, ok := ifCond.(string); ok {
				safeJob.If = c.extractExpressionFromIfString(ifStr)
			}
		}

		// Parse needs
		if needs, exists := jobConfig["needs"]; exists {
			if needsList, ok := needs.([]any); ok {
				for _, need := range needsList {
					if needStr, ok := need.(string); ok {
						safeJob.Needs = append(safeJob.Needs, needStr)
					}
				}
			} else if needStr, ok := needs.(string); ok {
				safeJob.Needs = append(safeJob.Needs, needStr)
			}
		}

		// Parse steps
		if steps, exists := jobConfig["steps"]; exists {
			if stepsList, ok := steps.([]any); ok {
				safeJob.Steps = stepsList
			}
		}

		// Parse env
		if env, exists := jobConfig["env"]; exists {
			if envMap, ok := env.(map[string]any); ok {
				safeJob.Env = make(map[string]string)
				for key, value := range envMap {
					if valueStr, ok := value.(string); ok {
						safeJob.Env[key] = valueStr
					}
				}
			}
		}

		// Parse permissions
		if permissions, exists := jobConfig["permissions"]; exists {
			safeJob.RawPermissions = permissions
			if permMap, ok := permissions.(map[string]any); ok {
				safeJob.Permissions = make(map[string]string)
				for key, value := range permMap {
					if valueStr, ok := value.(string); ok {
						safeJob.Permissions[key] = valueStr
					}
				}
			}
		}

		// Parse github-token
		if token, exists := jobConfig["github-token"]; exists {
			if tokenStr, ok := token.(string); ok {
				safeJob.GitHubToken = tokenStr
			}
		}

		// Parse output (also accept "agent-output" as alias)
		if output, exists := jobConfig["output"]; exists {
			if outputStr, ok := output.(string); ok {
				safeJob.Output = outputStr
			}
		} else if agentOutput, exists := jobConfig["agent-output"]; exists {
			if agentOutputStr, ok := agentOutput.(string); ok {
				safeJob.Output = agentOutputStr
			}
		}

		// Parse inputs using the unified parsing function
		if inputs, exists := jobConfig["inputs"]; exists {
			if inputsMap, ok := inputs.(map[string]any); ok {
				safeJob.Inputs = ParseInputDefinitions(inputsMap)
			}
		}

		// Parse max (optional; controls how many times this output type may be emitted per run)
		if maxVal, exists := jobConfig["max"]; exists {
			maxInt := 0
			switch v := maxVal.(type) {
			case int:
				maxInt = v
			case int64:
				maxInt = int(v)
			case uint64:
				maxInt = int(v)
			case float64:
				if v != float64(int(v)) {
					safeJobsLog.Printf("Warning: ignoring non-integer max for safe-job %q: %v", jobName, v)
				} else {
					maxInt = int(v)
				}
			default:
				safeJobsLog.Printf("Warning: ignoring non-numeric max for safe-job %q: %T", jobName, maxVal)
			}
			if maxInt > 0 {
				safeJob.Max = maxInt
			}
		}

		// Parse artifacts: additional agent-job filesystem paths this job depends on.
		// Each entry must be rooted under /tmp/gh-aw/ so the compiler's secret redaction
		// step (which only scans that subtree) covers it before upload.
		if artifacts, exists := jobConfig["artifacts"]; exists {
			if artifactsList, ok := artifacts.([]any); ok {
				for _, artifact := range artifactsList {
					artifactStr, ok := artifact.(string)
					if !ok {
						safeJob.artifactsError = fmt.Errorf("safe-job %q: artifacts entries must be strings", jobName)
						break
					}
					if err := validateSafeJobArtifactPath(artifactStr); err != nil {
						safeJob.artifactsError = fmt.Errorf("safe-job %q: %w", jobName, err)
						break
					}
					safeJob.Artifacts = append(safeJob.Artifacts, artifactStr)
				}
			} else {
				safeJob.artifactsError = fmt.Errorf("safe-job %q: artifacts must be an array of strings", jobName)
			}
		}

		safeJobsLog.Printf("Parsed safe-job configuration: name=%s, has_steps=%v, has_inputs=%v, max=%d", jobName, len(safeJob.Steps) > 0, len(safeJob.Inputs) > 0, safeJob.Max)
		result[jobName] = safeJob
	}

	return result
}

// validateSafeJobArtifactPath validates a single safe-job "artifacts" entry.
//
// Two properties must hold before the path is trusted for inclusion in the unified
// agent artifact upload:
//  1. It must be rooted under /tmp/gh-aw/ so the compiler's secret-redaction step,
//     which only scans that subtree, covers it before upload. Path traversal segments
//     ("..") are rejected outright since they could otherwise be used to smuggle a
//     path that resolves outside /tmp/gh-aw/ past this prefix check.
//  2. It must not reference hidden files or directories (any path segment starting
//     with "."), because the unified "actions/upload-artifact" step does not set
//     include-hidden-files, whose default is false. A declared hidden path would
//     therefore pass validation but still be silently dropped from the artifact.
func validateSafeJobArtifactPath(path string) error {
	if strings.Contains(path, "${{") {
		return fmt.Errorf("artifact path %q must be literal and must not contain GitHub Actions expressions", path)
	}
	if strings.IndexFunc(path, unicode.IsControl) >= 0 {
		return fmt.Errorf("artifact path %q must not contain control characters", path)
	}
	if !strings.HasPrefix(path, constants.TmpGhAwDirSlash) {
		return fmt.Errorf("artifact path %q must be rooted under %q so it is covered by secret redaction", path, constants.TmpGhAwDirSlash)
	}
	for segment := range strings.SplitSeq(path, "/") {
		if segment == ".." {
			return fmt.Errorf("artifact path %q must not contain \"..\" path traversal segments", path)
		}
		if strings.HasPrefix(segment, ".") && segment != "" {
			return fmt.Errorf("artifact path %q must not reference hidden files or directories (segment %q starts with \".\"), which are silently dropped by the unified artifact upload", path, segment)
		}
	}
	if !isPathScannedBySecretRedaction(path) {
		return fmt.Errorf("artifact path %q must end with \"/\" for a directory or use a file extension covered by secret redaction", path)
	}
	return nil
}

// collectSafeJobArtifactPaths gathers all declared artifact paths from custom safe-outputs
// jobs, sorted by normalized job name for deterministic compiled output. Directory
// declarations are restricted to recursive globs for extensions covered by secret redaction.
func collectSafeJobArtifactPaths(jobs map[string]*SafeJobConfig) []string {
	if len(jobs) == 0 {
		return nil
	}

	names := make([]string, 0, len(jobs))
	for name := range jobs {
		names = append(names, name)
	}
	slices.Sort(names)

	var paths []string
	for _, name := range names {
		if job := jobs[name]; job != nil {
			for _, artifactPath := range job.Artifacts {
				if strings.HasSuffix(artifactPath, "/") {
					for _, ext := range secretRedactionScannedExtensions {
						paths = append(paths, artifactPath+"**/*"+ext)
					}
					continue
				}
				paths = append(paths, artifactPath)
			}
		}
	}
	return paths
}

func hasSafeJobArtifactPaths(data *WorkflowData) bool {
	return data != nil && data.SafeOutputs != nil && len(collectSafeJobArtifactPaths(data.SafeOutputs.Jobs)) > 0
}

func isEmptySafeJobRunsOn(value any) bool {
	if _, isObject := value.(map[string]any); isObject {
		return false
	}
	return isEmptyRunsOnValue(value)
}

// buildSafeJobs creates custom safe-output jobs defined in SafeOutputs.Jobs
func (c *Compiler) buildSafeJobs(data *WorkflowData, threatDetectionEnabled bool) ([]string, error) { //nolint:largefunc // Existing orchestration of job needs, condition, steps, and permissions remains explicit and ordered.
	if data.SafeOutputs == nil || len(data.SafeOutputs.Jobs) == 0 {
		return nil, nil
	}

	safeJobsLog.Printf("Building %d safe-jobs, threatDetectionEnabled=%v", len(data.SafeOutputs.Jobs), threatDetectionEnabled)
	var safeJobNames []string

	// Collect normalized names and a config lookup map, then sort by normalized name.
	// Sorting on the normalized form (rather than the raw key) guarantees that the
	// returned safeJobNames slice and the conclusion job's needs: list are ordered
	// consistently with the names that appear in the compiled YAML, even when raw
	// names contain characters (e.g. '.', '-') that normalize differently from '_'.
	type safeJobEntry struct {
		normalizedName string
		config         *SafeJobConfig
	}
	entries := make([]safeJobEntry, 0, len(data.SafeOutputs.Jobs))
	for rawName, cfg := range data.SafeOutputs.Jobs {
		entries = append(entries, safeJobEntry{stringutil.NormalizeSafeOutputIdentifier(rawName), cfg})
	}
	slices.SortFunc(entries, func(a, b safeJobEntry) int { return strings.Compare(a.normalizedName, b.normalizedName) })

	for _, entry := range entries {
		jobConfig := entry.config
		normalizedJobName := entry.normalizedName

		job := &Job{
			Name:        normalizedJobName,
			Environment: c.indentYAMLLines(resolveSafeOutputsEnvironment(data), "    "),
		}

		// Set custom job name if specified
		if jobConfig.Name != "" {
			job.DisplayName = jobConfig.Name
		}

		// Safe-jobs depend on agent job
		job.Needs = append(job.Needs, string(constants.AgentJobName))

		// When threat detection is enabled, safe-jobs also depend on the detection job
		// so that the condition can gate on needs.detection.result == 'success'
		if threatDetectionEnabled {
			job.Needs = append(job.Needs, string(constants.DetectionJobName))
		}

		// Add any additional dependencies from the config
		job.Needs = append(job.Needs, jobConfig.Needs...)

		const defaultRunsOn = "ubuntu-latest"

		// Set runs-on, defaulting to ubuntu-latest when omitted.
		if jobConfig.runsOnError != nil {
			return nil, fmt.Errorf("invalid runs-on for safe-job '%s': %w", normalizedJobName, jobConfig.runsOnError)
		}

		// Reject invalid artifacts declarations up front with a clear, actionable error
		// rather than letting them surface later as an opaque step-ordering compiler bug.
		if jobConfig.artifactsError != nil {
			return nil, fmt.Errorf("invalid artifacts for safe-job '%s': %w", normalizedJobName, jobConfig.artifactsError)
		}
		runsOn := jobConfig.RunsOn
		if runsOn == "" {
			runsOn = "runs-on: " + defaultRunsOn
		}
		job.RunsOn = runsOn

		// Set if condition - combine safe output type check with user-provided condition
		// Custom safe jobs should only run if the agent output contains the job name (tool call)
		// Use normalized job name to match the underscore format in output_types
		safeOutputCondition := BuildSafeOutputType(normalizedJobName) // min=0 means check for the tool in output_types

		// When detection is expression-controlled the detection job may be skipped at runtime.
		// Wrap the condition with always() + detection-passed so the safe-job still runs when
		// the caller disabled threat detection for this invocation via the expression.
		baseCondition := safeOutputCondition
		if IsConditionalDetection(data.SafeOutputs) {
			baseCondition = BuildAnd(
				BuildAnd(BuildFunctionCall("always"), safeOutputCondition),
				buildDetectionPassedCondition(),
			)
		}

		if jobConfig.If != "" {
			// If user provided a custom condition, combine it with the base condition
			userConditionStr := c.extractExpressionFromIfString(jobConfig.If)
			userCondition := &ExpressionNode{Expression: userConditionStr}
			job.If = RenderCondition(BuildAnd(baseCondition, userCondition))
		} else {
			job.If = RenderCondition(baseCondition)
		}

		// Build job steps
		var steps []string

		// Add step to download agent output artifact using shared helper.
		// In workflow_call context, use the per-invocation prefix to avoid artifact name clashes.
		// Safe-jobs depend on the agent job, so the prefix comes from needs.agent.outputs.
		agentArtifactPrefix := artifactPrefixExprForAgentDownstreamJob(data)
		downloadSteps := buildArtifactDownloadSteps(ArtifactDownloadConfig{
			ArtifactName:     agentArtifactPrefix + constants.AgentArtifactName.String(),
			FallbackArtifact: agentArtifactPrefix + constants.AgentOutputFallbackArtifactName.String(),
			DownloadPath:     SafeJobsDownloadDirExpr,
			SetupEnvStep:     false, // We'll handle env vars separately to add job-specific ones
			StepName:         "Download agent output artifact",
		}, c.getActionPin)
		steps = append(steps, downloadSteps...)

		// the download artifacts always creates a folder, then unpacks in that folder

		// Add custom steps from the job configuration, injecting env vars directly so
		// user steps can access GH_AW_AGENT_OUTPUT and all job-specific env vars.
		if len(jobConfig.Steps) > 0 {
			// GH_AW_AGENT_OUTPUT uses the runner.temp Actions expression so the path is
			// resolved by the runner without requiring a $GITHUB_OUTPUT write.
			setupEnvVars := map[string]string{
				"GH_AW_AGENT_OUTPUT": SafeJobsDownloadDirExpr + constants.AgentOutputFilename.String(),
			}
			// All job-specific env vars (literal or expression-based) are injected with
			// their original values. Nothing goes through $GITHUB_OUTPUT.
			maps.Copy(setupEnvVars, jobConfig.Env)
			for _, step := range jobConfig.Steps {
				if stepMap, ok := step.(map[string]any); ok {
					// Convert to typed step for action pinning
					typedStep, err := MapToStep(stepMap)
					if err != nil {
						return nil, fmt.Errorf("failed to convert step to typed step for safe job %s: %w", normalizedJobName, err)
					}

					// Inject setup env vars so user steps can access GH_AW_AGENT_OUTPUT
					// and job-specific env vars (previously available via GITHUB_ENV).
					if typedStep.Env == nil {
						typedStep.Env = make(map[string]string)
					}
					for k, v := range setupEnvVars {
						if _, exists := typedStep.Env[k]; !exists {
							typedStep.Env[k] = v
						}
					}

					// Apply action pinning using type-safe version
					pinnedStep, err := applyActionPinToTypedStep(typedStep, data)
					if err != nil {
						return nil, fmt.Errorf("failed to pin action for step in safe job %s: %w", normalizedJobName, err)
					}

					// Convert back to map for YAML generation
					stepYAML, err := ConvertStepToYAML(pinnedStep.ToMap())
					if err != nil {
						return nil, fmt.Errorf("failed to convert step to YAML for safe job %s: %w", normalizedJobName, err)
					}
					steps = append(steps, stepYAML)
				}
			}
		}

		job.Steps = steps

		// Set permissions if specified
		if jobConfig.RawPermissions != nil {
			job.Permissions = NewPermissionsParserFromValue(jobConfig.RawPermissions).ToPermissions().RenderToYAML()
		} else if len(jobConfig.Permissions) > 0 {
			perms := NewPermissions()
			for perm, level := range jobConfig.Permissions {
				perms.Set(PermissionScope(perm), PermissionLevel(level))
			}
			job.Permissions = perms.RenderToYAML()
		}

		// Add the job to the job manager
		if err := c.jobManager.AddJob(job); err != nil {
			safeJobsLog.Printf("Failed to add safe-job %s: %v", normalizedJobName, err)
			return nil, fmt.Errorf("failed to add safe job %s: %w", normalizedJobName, err)
		}
		safeJobsLog.Printf("Created safe-job: %s with %d dependencies and %d steps", normalizedJobName, len(job.Needs), len(job.Steps))
		safeJobNames = append(safeJobNames, normalizedJobName)
	}

	safeJobsLog.Printf("Successfully built %d safe-jobs", len(safeJobNames))
	return safeJobNames, nil
}

// extractSafeJobsFromFrontmatter extracts safe-jobs configuration from frontmatter.
// Only checks the safe-outputs.jobs location. The top-level "safe-jobs" syntax is NOT supported.
func extractSafeJobsFromFrontmatter(frontmatter map[string]any) map[string]*SafeJobConfig {
	// Check location: safe-outputs.jobs
	if safeOutputs, exists := frontmatter["safe-outputs"]; exists {
		if safeOutputsMap, ok := safeOutputs.(map[string]any); ok {
			if jobs, exists := safeOutputsMap["jobs"]; exists {
				if jobsMap, ok := jobs.(map[string]any); ok {
					c := NewCompiler() // Create a temporary compiler instance for parsing
					return c.parseSafeJobsConfig(jobsMap)
				}
			}
		}
	}

	return make(map[string]*SafeJobConfig)
}

// mergeSafeJobs merges safe-jobs from multiple sources and detects name conflicts
func mergeSafeJobs(base map[string]*SafeJobConfig, additional map[string]*SafeJobConfig) (map[string]*SafeJobConfig, error) {
	if additional == nil {
		return base, nil
	}

	if base == nil {
		base = make(map[string]*SafeJobConfig)
	}

	result := make(map[string]*SafeJobConfig)

	// Copy base safe-jobs
	maps.Copy(result, base)

	// Add additional safe-jobs, checking for conflicts
	for name, config := range additional {
		if _, exists := result[name]; exists {
			return nil, fmt.Errorf("safe-job name conflict: '%s' is defined in both main workflow and included files", name)
		}
		result[name] = config
	}

	return result, nil
}
