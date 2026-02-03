package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var safeJobsLog = logger.New("workflow:safe_jobs")

// SafeJobConfig defines a safe job configuration with GitHub Actions job properties
type SafeJobConfig struct {
	// Standard GitHub Actions job properties
	Name        string            `yaml:"name,omitempty"`
	Description string            `yaml:"description,omitempty"`
	RunsOn      any               `yaml:"runs-on,omitempty"`
	If          string            `yaml:"if,omitempty"`
	Needs       []string          `yaml:"needs,omitempty"`
	Steps       []any             `yaml:"steps,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
	Permissions map[string]string `yaml:"permissions,omitempty"`

	// Additional safe-job specific properties
	Inputs      map[string]*InputDefinition `yaml:"inputs,omitempty"`
	GitHubToken string                      `yaml:"github-token,omitempty"`
	Output      string                      `yaml:"output,omitempty"`
}

// HasSafeJobsEnabled checks if any safe-jobs are enabled at the top level
func HasSafeJobsEnabled(safeJobs map[string]*SafeJobConfig) bool {
	return len(safeJobs) > 0
}

// parseSafeJobsConfig parses safe-jobs configuration from a jobs map.
// This function expects a map of job configurations directly (from safe-outputs.jobs).
// The top-level "safe-jobs" key is NOT supported - only "safe-outputs.jobs" is valid.
func (c *Compiler) parseSafeJobsConfig(jobsMap map[string]any) map[string]*SafeJobConfig {
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

		// Parse runs-on
		if runsOn, exists := jobConfig["runs-on"]; exists {
			safeJob.RunsOn = runsOn
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

		// Parse output
		if output, exists := jobConfig["output"]; exists {
			if outputStr, ok := output.(string); ok {
				safeJob.Output = outputStr
			}
		}

		// Parse inputs using the unified parsing function
		if inputs, exists := jobConfig["inputs"]; exists {
			if inputsMap, ok := inputs.(map[string]any); ok {
				safeJob.Inputs = ParseInputDefinitions(inputsMap)
			}
		}

		safeJobsLog.Printf("Parsed safe-job configuration: name=%s, has_steps=%v, has_inputs=%v", jobName, len(safeJob.Steps) > 0, len(safeJob.Inputs) > 0)
		result[jobName] = safeJob
	}

	return result
}

// buildSafeJobs creates custom safe-output jobs defined in SafeOutputs.Jobs
func (c *Compiler) buildSafeJobs(data *WorkflowData, threatDetectionEnabled bool) ([]string, error) {
	if data.SafeOutputs == nil || len(data.SafeOutputs.Jobs) == 0 {
		return nil, nil
	}

	safeJobsLog.Printf("Building %d safe-jobs, threatDetectionEnabled=%v", len(data.SafeOutputs.Jobs), threatDetectionEnabled)
	var safeJobNames []string
	for jobName, jobConfig := range data.SafeOutputs.Jobs {
		// Normalize job name to use underscores for consistency
		normalizedJobName := stringutil.NormalizeSafeOutputIdentifier(jobName)

		job := &Job{
			Name: normalizedJobName,
		}

		// Set custom job name if specified
		if jobConfig.Name != "" {
			job.DisplayName = jobConfig.Name
		}

		// Safe-jobs should depend on agent job (always) AND detection job (if enabled)
		job.Needs = append(job.Needs, string(constants.AgentJobName))
		if threatDetectionEnabled {
			job.Needs = append(job.Needs, string(constants.DetectionJobName))
		}

		// Add any additional dependencies from the config
		job.Needs = append(job.Needs, jobConfig.Needs...)

		// Set runs-on
		if jobConfig.RunsOn != nil {
			if runsOnStr, ok := jobConfig.RunsOn.(string); ok {
				job.RunsOn = fmt.Sprintf("runs-on: %s", runsOnStr)
			} else if runsOnList, ok := jobConfig.RunsOn.([]any); ok {
				// Handle array format
				var runsOnItems []string
				for _, item := range runsOnList {
					if itemStr, ok := item.(string); ok {
						runsOnItems = append(runsOnItems, fmt.Sprintf("      - %s", itemStr))
					}
				}
				if len(runsOnItems) > 0 {
					job.RunsOn = fmt.Sprintf("runs-on:\n%s", strings.Join(runsOnItems, "\n"))
				}
			}
		} else {
			job.RunsOn = "runs-on: ubuntu-latest" // Default
		}

		// Set if condition - combine safe output type check with user-provided condition
		// Custom safe jobs should only run if the agent output contains the job name (tool call)
		// Use normalized job name to match the underscore format in output_types
		safeOutputCondition := BuildSafeOutputType(normalizedJobName) // min=0 means check for the tool in output_types

		if jobConfig.If != "" {
			// If user provided a custom condition, combine it with the safe output type check
			userConditionStr := c.extractExpressionFromIfString(jobConfig.If)
			userCondition := &ExpressionNode{Expression: userConditionStr}
			job.If = BuildAnd(safeOutputCondition, userCondition).Render()
		} else {
			// Otherwise, just use the safe output type check
			job.If = safeOutputCondition.Render()
		}

		// Build job steps
		var steps []string

		// Add step to download agent output artifact using shared helper
		downloadSteps := buildArtifactDownloadSteps(ArtifactDownloadConfig{
			ArtifactName: constants.AgentOutputArtifactName,
			DownloadPath: "/opt/gh-aw/safe-jobs/",
			SetupEnvStep: false, // We'll handle env vars separately to add job-specific ones
			StepName:     "Download agent output artifact",
		})
		steps = append(steps, downloadSteps...)

		// the download artifacts always creates a folder, then unpacks in that folder
		agentOutputArtifactFilename := fmt.Sprintf("/opt/gh-aw/safe-jobs/%s", constants.AgentOutputFilename)

		// Add environment variables step with GH_AW_AGENT_OUTPUT and job-specific env vars
		steps = append(steps, "      - name: Setup Safe Job Environment Variables\n")
		steps = append(steps, "        run: |\n")
		steps = append(steps, "          find \"/opt/gh-aw/safe-jobs/\" -type f -print\n")
		// Configure GH_AW_AGENT_OUTPUT to point to downloaded artifact file
		steps = append(steps, fmt.Sprintf("          echo \"GH_AW_AGENT_OUTPUT=%s\" >> \"$GITHUB_ENV\"\n", agentOutputArtifactFilename))

		// Add job-specific environment variables
		if jobConfig.Env != nil {
			for key, value := range jobConfig.Env {
				steps = append(steps, fmt.Sprintf("          echo \"%s=%s\" >> \"$GITHUB_ENV\"\n", key, value))
			}
		}

		// Add custom steps from the job configuration
		if len(jobConfig.Steps) > 0 {
			for _, step := range jobConfig.Steps {
				if stepMap, ok := step.(map[string]any); ok {
					// Convert to typed step for action pinning
					typedStep, err := MapToStep(stepMap)
					if err != nil {
						return nil, fmt.Errorf("failed to convert step to typed step for safe job %s: %w", jobName, err)
					}

					// Apply action pinning using type-safe version
					pinnedStep := ApplyActionPinToTypedStep(typedStep, data)

					// Convert back to map for YAML generation
					stepYAML, err := c.convertStepToYAML(pinnedStep.ToMap())
					if err != nil {
						return nil, fmt.Errorf("failed to convert step to YAML for safe job %s: %w", jobName, err)
					}
					steps = append(steps, stepYAML)
				}
			}
		}

		job.Steps = steps

		// Set permissions if specified
		if len(jobConfig.Permissions) > 0 {
			// Build Permissions struct from map
			perms := NewPermissions()
			for perm, level := range jobConfig.Permissions {
				perms.Set(PermissionScope(perm), PermissionLevel(level))
			}
			job.Permissions = perms.RenderToYAML()
		}

		// Add the job to the job manager
		if err := c.jobManager.AddJob(job); err != nil {
			safeJobsLog.Printf("Failed to add safe-job %s: %v", normalizedJobName, err)
			return nil, fmt.Errorf("failed to add safe job %s: %w", jobName, err)
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
					c := &Compiler{} // Create a temporary compiler instance for parsing
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
	for name, config := range base {
		result[name] = config
	}

	// Add additional safe-jobs, checking for conflicts
	for name, config := range additional {
		if _, exists := result[name]; exists {
			return nil, fmt.Errorf("safe-job name conflict: '%s' is defined in both main workflow and included files", name)
		}
		result[name] = config
	}

	return result, nil
}
