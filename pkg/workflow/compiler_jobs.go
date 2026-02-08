package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/goccy/go-yaml"
)

var compilerJobsLog = logger.New("workflow:compiler_jobs")

// This file contains job building functions extracted from compiler.go
// These functions are responsible for constructing the various jobs that make up
// a compiled agentic workflow, including activation, main, safe outputs, and custom jobs.

func (c *Compiler) isActivationJobNeeded() bool {
	// Activation job is always needed to perform the timestamp check
	// It also handles:
	// 1. Command is configured (for team member checking)
	// 2. Text output is needed (for compute-text action)
	// 3. If condition is specified (to handle runtime conditions)
	// 4. Permission checks are needed (consolidated team member validation)
	return true
}

// referencesCustomJobOutputs checks if a condition string references custom jobs.
// Returns true if the condition contains "needs.<customJobName>." patterns, which includes
// both outputs (needs.job.outputs.*) and results (needs.job.result).
func (c *Compiler) referencesCustomJobOutputs(condition string, customJobs map[string]any) bool {
	if condition == "" || customJobs == nil {
		return false
	}
	for jobName := range customJobs {
		// Check for patterns like "needs.ast_grep.outputs" or "needs.ast_grep.result"
		if strings.Contains(condition, fmt.Sprintf("needs.%s.", jobName)) {
			return true
		}
	}
	return false
}

// jobDependsOnPreActivation checks if a job config has pre_activation as a dependency.
func jobDependsOnPreActivation(jobConfig map[string]any) bool {
	if needs, hasNeeds := jobConfig["needs"]; hasNeeds {
		if needsList, ok := needs.([]any); ok {
			for _, need := range needsList {
				if needStr, ok := need.(string); ok && needStr == string(constants.PreActivationJobName) {
					return true
				}
			}
		} else if needStr, ok := needs.(string); ok && needStr == string(constants.PreActivationJobName) {
			return true
		}
	}
	return false
}

// jobDependsOnAgent checks if a job config has agent as a dependency.
// Jobs that depend on agent should run AFTER the agent job, not before it.
// The jobConfig parameter is expected to be a map representing the job's YAML configuration,
// where "needs" can be either a string (single dependency) or []any (multiple dependencies).
// Returns false if "needs" is missing, malformed, or doesn't contain the agent job.
func jobDependsOnAgent(jobConfig map[string]any) bool {
	if needs, hasNeeds := jobConfig["needs"]; hasNeeds {
		if needsList, ok := needs.([]any); ok {
			for _, need := range needsList {
				if needStr, ok := need.(string); ok && needStr == string(constants.AgentJobName) {
					return true
				}
			}
		} else if needStr, ok := needs.(string); ok && needStr == string(constants.AgentJobName) {
			return true
		}
	}
	return false
}

// getCustomJobsDependingOnPreActivation returns custom job names that explicitly depend on pre_activation.
// These jobs run after pre_activation but before activation, and activation should depend on them.
func (c *Compiler) getCustomJobsDependingOnPreActivation(customJobs map[string]any) []string {
	return sliceutil.FilterMapKeys(customJobs, func(jobName string, jobConfig any) bool {
		if configMap, ok := jobConfig.(map[string]any); ok {
			return jobDependsOnPreActivation(configMap)
		}
		return false
	})
}

// getReferencedCustomJobs returns custom job names that are referenced in the given content.
// It looks for patterns like "needs.<jobName>." or "${{ needs.<jobName>." in the content.
func (c *Compiler) getReferencedCustomJobs(content string, customJobs map[string]any) []string {
	if content == "" || customJobs == nil {
		return nil
	}
	// Check for patterns like "needs.job_name." which covers:
	// - needs.job_name.outputs.X
	// - ${{ needs.job_name.outputs.X }}
	// - needs.job_name.result
	return sliceutil.FilterMapKeys(customJobs, func(jobName string, _ any) bool {
		return strings.Contains(content, fmt.Sprintf("needs.%s.", jobName))
	})
}

// buildJobs creates all jobs for the workflow and adds them to the job manager.
// This function orchestrates the building of all job types by delegating to focused helper functions.
func (c *Compiler) buildJobs(data *WorkflowData, markdownPath string) error {
	compilerJobsLog.Printf("Building jobs for workflow: %s", markdownPath)

	// Try to read frontmatter to determine event types for safe events check
	var frontmatter map[string]any
	if content, err := os.ReadFile(markdownPath); err == nil {
		if result, err := parser.ExtractFrontmatterFromContent(string(content)); err == nil {
			frontmatter = result.Frontmatter
		}
	}

	// Extract lock filename for timestamp check
	lockFilename := filepath.Base(stringutil.MarkdownToLockFile(markdownPath))

	// Build pre-activation and activation jobs
	_, activationJobCreated, err := c.buildPreActivationAndActivationJobs(data, frontmatter, lockFilename)
	if err != nil {
		return err
	}

	// Build main workflow job
	if err := c.buildMainJobWrapper(data, activationJobCreated); err != nil {
		return err
	}

	// Build safe outputs jobs if configured
	if err := c.buildSafeOutputsJobs(data, string(constants.AgentJobName), markdownPath); err != nil {
		return fmt.Errorf("failed to build safe outputs jobs: %w", err)
	}

	// Build additional custom jobs from frontmatter jobs section
	if len(data.Jobs) > 0 {
		compilerJobsLog.Printf("Building %d custom jobs from frontmatter", len(data.Jobs))
	}
	if err := c.buildCustomJobs(data, activationJobCreated); err != nil {
		return fmt.Errorf("failed to build custom jobs: %w", err)
	}

	// Build memory management jobs (repo-memory and cache-memory)
	if err := c.buildMemoryManagementJobs(data); err != nil {
		return err
	}

	compilerJobsLog.Print("Successfully built all jobs for workflow")
	return nil
}

// buildPreActivationAndActivationJobs builds the pre-activation and activation jobs if needed.
// Returns whether each job was created.
func (c *Compiler) buildPreActivationAndActivationJobs(data *WorkflowData, frontmatter map[string]any, lockFilename string) (preActivationJobCreated bool, activationJobCreated bool, err error) {
	// Determine if permission checks or stop-time checks are needed
	needsPermissionCheck := c.needsRoleCheck(data, frontmatter)
	hasStopTime := data.StopTime != ""
	hasSkipIfMatch := data.SkipIfMatch != nil
	hasSkipIfNoMatch := data.SkipIfNoMatch != nil
	hasCommandTrigger := len(data.Command) > 0
	compilerJobsLog.Printf("Job configuration: needsPermissionCheck=%v, hasStopTime=%v, hasSkipIfMatch=%v, hasSkipIfNoMatch=%v, hasCommand=%v", needsPermissionCheck, hasStopTime, hasSkipIfMatch, hasSkipIfNoMatch, hasCommandTrigger)

	// Build pre-activation job if needed (combines membership checks, stop-time validation, skip-if-match check, skip-if-no-match check, and command position check)
	if needsPermissionCheck || hasStopTime || hasSkipIfMatch || hasSkipIfNoMatch || hasCommandTrigger {
		compilerJobsLog.Print("Building pre-activation job")
		preActivationJob, err := c.buildPreActivationJob(data, needsPermissionCheck)
		if err != nil {
			return false, false, fmt.Errorf("failed to build %s job: %w", constants.PreActivationJobName, err)
		}
		if err := c.jobManager.AddJob(preActivationJob); err != nil {
			return false, false, fmt.Errorf("failed to add %s job: %w", constants.PreActivationJobName, err)
		}
		compilerJobsLog.Printf("Successfully added pre-activation job: %s", constants.PreActivationJobName)
		preActivationJobCreated = true
	}

	// Determine if we need to add workflow_run repository safety check
	var workflowRunRepoSafety string
	if c.hasWorkflowRunTrigger(frontmatter) {
		workflowRunRepoSafety = c.buildWorkflowRunRepoSafetyCondition()
		compilerJobsLog.Print("Adding workflow_run repository safety check")
	}

	// Build activation job if needed (preamble job that handles runtime conditions)
	if c.isActivationJobNeeded() {
		compilerJobsLog.Print("Building activation job")
		activationJob, err := c.buildActivationJob(data, preActivationJobCreated, workflowRunRepoSafety, lockFilename)
		if err != nil {
			return preActivationJobCreated, false, fmt.Errorf("failed to build activation job: %w", err)
		}
		if err := c.jobManager.AddJob(activationJob); err != nil {
			return preActivationJobCreated, false, fmt.Errorf("failed to add activation job: %w", err)
		}
		compilerJobsLog.Print("Successfully added activation job")
		activationJobCreated = true
	}

	return preActivationJobCreated, activationJobCreated, nil
}

// buildMainJobWrapper builds the main workflow job and adds it to the job manager.
func (c *Compiler) buildMainJobWrapper(data *WorkflowData, activationJobCreated bool) error {
	compilerJobsLog.Print("Building main job")
	mainJob, err := c.buildMainJob(data, activationJobCreated)
	if err != nil {
		return fmt.Errorf("failed to build main job: %w", err)
	}
	if err := c.jobManager.AddJob(mainJob); err != nil {
		return fmt.Errorf("failed to add main job: %w", err)
	}
	compilerJobsLog.Printf("Successfully added main job: %s", string(constants.AgentJobName))
	return nil
}

// buildMemoryManagementJobs builds memory management jobs (push_repo_memory and update_cache_memory).
// These jobs handle artifact-based memory persistence to git branches and GitHub Actions cache.
func (c *Compiler) buildMemoryManagementJobs(data *WorkflowData) error {
	threatDetectionEnabledForSafeJobs := data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil

	// Build push_repo_memory job if repo-memory is configured
	pushRepoMemoryJobName, err := c.buildPushRepoMemoryJobWrapper(data, threatDetectionEnabledForSafeJobs)
	if err != nil {
		return err
	}

	// Build update_cache_memory job if cache-memory is configured and threat detection is enabled
	updateCacheMemoryJobName, err := c.buildUpdateCacheMemoryJobWrapper(data, threatDetectionEnabledForSafeJobs)
	if err != nil {
		return err
	}

	// Update conclusion job dependencies
	if err := c.updateConclusionJobDependencies(pushRepoMemoryJobName, updateCacheMemoryJobName); err != nil {
		return err
	}

	return nil
}

// buildPushRepoMemoryJobWrapper builds the push_repo_memory job if repo-memory is configured.
// Returns the job name if created, empty string otherwise.
func (c *Compiler) buildPushRepoMemoryJobWrapper(data *WorkflowData, threatDetectionEnabled bool) (string, error) {
	if data.RepoMemoryConfig == nil || len(data.RepoMemoryConfig.Memories) == 0 {
		return "", nil
	}

	compilerJobsLog.Print("Building push_repo_memory job")
	pushRepoMemoryJob, err := c.buildPushRepoMemoryJob(data, threatDetectionEnabled)
	if err != nil {
		return "", fmt.Errorf("failed to build push_repo_memory job: %w", err)
	}

	if pushRepoMemoryJob == nil {
		return "", nil
	}

	// Add detection dependency if threat detection is enabled
	if threatDetectionEnabled {
		pushRepoMemoryJob.Needs = append(pushRepoMemoryJob.Needs, string(constants.DetectionJobName))
		compilerJobsLog.Print("Added detection dependency to push_repo_memory job")
	}

	if err := c.jobManager.AddJob(pushRepoMemoryJob); err != nil {
		return "", fmt.Errorf("failed to add push_repo_memory job: %w", err)
	}

	compilerJobsLog.Printf("Successfully added push_repo_memory job: %s", pushRepoMemoryJob.Name)
	return pushRepoMemoryJob.Name, nil
}

// buildUpdateCacheMemoryJobWrapper builds the update_cache_memory job if cache-memory is configured.
// Returns the job name if created, empty string otherwise.
func (c *Compiler) buildUpdateCacheMemoryJobWrapper(data *WorkflowData, threatDetectionEnabled bool) (string, error) {
	if data.CacheMemoryConfig == nil || len(data.CacheMemoryConfig.Caches) == 0 {
		return "", nil
	}

	if !threatDetectionEnabled {
		return "", nil
	}

	compilerJobsLog.Print("Building update_cache_memory job")
	updateCacheMemoryJob, err := c.buildUpdateCacheMemoryJob(data, threatDetectionEnabled)
	if err != nil {
		return "", fmt.Errorf("failed to build update_cache_memory job: %w", err)
	}

	if updateCacheMemoryJob == nil {
		return "", nil
	}

	if err := c.jobManager.AddJob(updateCacheMemoryJob); err != nil {
		return "", fmt.Errorf("failed to add update_cache_memory job: %w", err)
	}

	compilerJobsLog.Printf("Successfully added update_cache_memory job: %s", updateCacheMemoryJob.Name)
	return updateCacheMemoryJob.Name, nil
}

// updateConclusionJobDependencies updates the conclusion job to depend on memory management jobs if they exist.
func (c *Compiler) updateConclusionJobDependencies(pushRepoMemoryJobName, updateCacheMemoryJobName string) error {
	conclusionJob, exists := c.jobManager.GetJob("conclusion")
	if !exists {
		return nil
	}

	if pushRepoMemoryJobName != "" {
		conclusionJob.Needs = append(conclusionJob.Needs, pushRepoMemoryJobName)
		compilerJobsLog.Printf("Added push_repo_memory dependency to conclusion job")
	}

	if updateCacheMemoryJobName != "" {
		conclusionJob.Needs = append(conclusionJob.Needs, updateCacheMemoryJobName)
		compilerJobsLog.Printf("Added update_cache_memory dependency to conclusion job")
	}

	return nil
}

// buildSafeOutputsJobs is now in compiler_safe_output_jobs.go
// buildPreActivationJob, buildActivationJob, and buildMainJob are now in compiler_activation_jobs.go

// extractJobsFromFrontmatter extracts job configuration from frontmatter
// This now uses the structured extraction helper for consistency
func (c *Compiler) extractJobsFromFrontmatter(frontmatter map[string]any) map[string]any {
	return ExtractMapField(frontmatter, "jobs")
}

// buildCustomJobs creates custom jobs defined in the frontmatter jobs section
func (c *Compiler) buildCustomJobs(data *WorkflowData, activationJobCreated bool) error {
	compilerJobsLog.Printf("Building %d custom jobs", len(data.Jobs))
	for jobName, jobConfig := range data.Jobs {
		// Skip jobs.pre-activation (or pre_activation) as it's handled specially in buildPreActivationJob
		if jobName == string(constants.PreActivationJobName) || jobName == "pre-activation" {
			compilerJobsLog.Printf("Skipping jobs.%s (handled in buildPreActivationJob)", jobName)
			continue
		}

		if configMap, ok := jobConfig.(map[string]any); ok {
			job := &Job{
				Name: jobName,
			}

			// Extract job dependencies
			hasExplicitNeeds := false
			if needs, hasNeeds := configMap["needs"]; hasNeeds {
				hasExplicitNeeds = true
				if needsList, ok := needs.([]any); ok {
					for _, need := range needsList {
						if needStr, ok := need.(string); ok {
							job.Needs = append(job.Needs, needStr)
						}
					}
				} else if needStr, ok := needs.(string); ok {
					// Single dependency as string
					job.Needs = append(job.Needs, needStr)
				}
			}

			// If no explicit needs and activation job exists, automatically add activation as dependency
			// This ensures custom jobs wait for workflow validation before executing
			if !hasExplicitNeeds && activationJobCreated {
				job.Needs = append(job.Needs, string(constants.ActivationJobName))
				compilerJobsLog.Printf("Added automatic dependency: custom job '%s' now depends on '%s'", jobName, string(constants.ActivationJobName))
			}

			// Extract other job properties
			if runsOn, hasRunsOn := configMap["runs-on"]; hasRunsOn {
				if runsOnStr, ok := runsOn.(string); ok {
					job.RunsOn = fmt.Sprintf("runs-on: %s", runsOnStr)
				}
			}

			if ifCond, hasIf := configMap["if"]; hasIf {
				if ifStr, ok := ifCond.(string); ok {
					job.If = c.extractExpressionFromIfString(ifStr)
				}
			}

			// Extract permissions
			if permissions, hasPermissions := configMap["permissions"]; hasPermissions {
				if permsMap, ok := permissions.(map[string]any); ok {
					// Use goccy/go-yaml to marshal permissions
					yamlBytes, err := yaml.Marshal(permsMap)
					if err != nil {
						return fmt.Errorf("failed to convert permissions to YAML for job '%s': %w", jobName, err)
					}
					// Indent the YAML properly for job-level permissions
					permsYAML := string(yamlBytes)
					lines := strings.Split(strings.TrimSpace(permsYAML), "\n")
					var formattedPerms strings.Builder
					formattedPerms.WriteString("permissions:\n")
					for _, line := range lines {
						formattedPerms.WriteString("      " + line + "\n")
					}
					job.Permissions = formattedPerms.String()
				}
			}

			// Extract outputs for custom jobs
			if outputs, hasOutputs := configMap["outputs"]; hasOutputs {
				if outputsMap, ok := outputs.(map[string]any); ok {
					job.Outputs = make(map[string]string)
					for key, val := range outputsMap {
						if valStr, ok := val.(string); ok {
							job.Outputs[key] = valStr
						} else {
							compilerJobsLog.Printf("Warning: output '%s' in job '%s' has non-string value (type: %T), ignoring", key, jobName, val)
						}
					}
				}
			}

			// Check if this is a reusable workflow call
			if uses, hasUses := configMap["uses"]; hasUses {
				if usesStr, ok := uses.(string); ok {
					compilerJobsLog.Printf("Custom job '%s' is a reusable workflow call: %s", jobName, usesStr)
					job.Uses = usesStr

					// Extract with parameters for reusable workflow
					if with, hasWith := configMap["with"]; hasWith {
						if withMap, ok := with.(map[string]any); ok {
							job.With = withMap
						}
					}

					// Extract secrets for reusable workflow
					if secrets, hasSecrets := configMap["secrets"]; hasSecrets {
						if secretsMap, ok := secrets.(map[string]any); ok {
							job.Secrets = make(map[string]string)
							for key, val := range secretsMap {
								if valStr, ok := val.(string); ok {
									// Validate that the secret value is a proper GitHub Actions expression
									// Note: We don't pass the key to validateSecretsExpression to prevent
									// CodeQL from detecting sensitive data flow to error messages/logs
									if err := validateSecretsExpression(valStr); err != nil {
										return err
									}
									job.Secrets[key] = valStr
								}
							}
						}
					}
				}
			} else {
				// Add basic steps if specified (only for non-reusable workflow jobs)
				if steps, hasSteps := configMap["steps"]; hasSteps {
					if stepsList, ok := steps.([]any); ok {
						for _, step := range stepsList {
							if stepMap, ok := step.(map[string]any); ok {
								// Convert to typed step for action pinning
								typedStep, err := MapToStep(stepMap)
								if err != nil {
									return fmt.Errorf("failed to convert step to typed step for job '%s': %w", jobName, err)
								}

								// Apply action pinning using type-safe version
								pinnedStep := ApplyActionPinToTypedStep(typedStep, data)

								// Convert back to map for YAML generation
								stepYAML, err := c.convertStepToYAML(pinnedStep.ToMap())
								if err != nil {
									return fmt.Errorf("failed to convert step to YAML for job '%s': %w", jobName, err)
								}
								job.Steps = append(job.Steps, stepYAML)
							}
						}
					}
				}
			}

			if err := c.jobManager.AddJob(job); err != nil {
				return fmt.Errorf("failed to add custom job '%s': %w", jobName, err)
			}
			compilerJobsLog.Printf("Successfully added custom job '%s' with %d needs dependencies", jobName, len(job.Needs))
		}
	}

	compilerJobsLog.Print("Completed building all custom jobs")
	return nil
}

// shouldAddCheckoutStep returns true if the workflow requires a checkout step.
// A checkout is needed when the workflow uses custom agent files, accesses local
// actions in dev/script mode, or needs to reference .github directory content.
//
// The checkout step is skipped in the following cases:
//   - Custom steps already contain a checkout action
//   - Workflow is in release mode without a custom agent file
//
// The checkout step is always added when:
//   - A custom agent file is specified (via imports)
//   - Running in dev or script mode (requires .github and .actions access)
//   - Action mode is uninitialized (defaults to requiring checkout)
func (c *Compiler) shouldAddCheckoutStep(data *WorkflowData) bool {
	// Check condition 1: If custom steps already contain checkout, don't add another one
	if data.CustomSteps != "" && ContainsCheckout(data.CustomSteps) {
		log.Print("Skipping checkout step: custom steps already contain checkout")
		return false // Custom steps already have checkout
	}

	// Check condition 2: If custom agent file is specified (via imports), checkout is required
	if data.AgentFile != "" {
		log.Printf("Adding checkout step: custom agent file specified: %s", data.AgentFile)
		return true // Custom agent file requires checkout to access the file
	}

	// Check condition 3: Only skip checkout in explicit release mode without agent file
	// Dev mode, script mode, and uninitialized mode all need checkout for .github and .actions access
	if c.actionMode.IsRelease() {
		// In release mode without agent file, checkout is not needed
		log.Print("Skipping checkout step: release mode without agent file")
		return false
	}

	// Default: add checkout for dev/script mode and uninitialized mode
	log.Printf("Adding checkout step: %s mode requires .github and .actions access", c.actionMode)
	return true
}
