package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var compilerSafeOutputJobsLog = logger.New("workflow:compiler_safe_output_jobs")

// buildSafeOutputsJobs builds all safe output jobs based on the configuration in data.SafeOutputs.
// It creates a consolidated safe_outputs job containing all safe output operations as steps,
// plus the threat detection job (if enabled), custom safe-jobs, and conclusion job.
func (c *Compiler) buildSafeOutputsJobs(data *WorkflowData, jobName, markdownPath string) error {
	if data.SafeOutputs == nil {
		compilerSafeOutputJobsLog.Print("No safe outputs configured, skipping safe outputs jobs")
		return nil
	}
	compilerSafeOutputJobsLog.Print("Building safe outputs jobs (consolidated mode)")

	// Track whether threat detection job is enabled
	threatDetectionEnabled := false

	// Build threat detection job if enabled
	if data.SafeOutputs.ThreatDetection != nil {
		compilerSafeOutputJobsLog.Print("Building threat detection job")
		detectionJob, err := c.buildThreatDetectionJob(data, jobName)
		if err != nil {
			return fmt.Errorf("failed to build detection job: %w", err)
		}
		if err := c.jobManager.AddJob(detectionJob); err != nil {
			return fmt.Errorf("failed to add detection job: %w", err)
		}
		compilerSafeOutputJobsLog.Printf("Successfully added threat detection job: %s", constants.DetectionJobName)
		threatDetectionEnabled = true
	}

	// Track safe output job names to establish dependencies for conclusion job
	var safeOutputJobNames []string

	// Build consolidated safe outputs job containing all safe output operations as steps
	consolidatedJob, consolidatedStepNames, err := c.buildConsolidatedSafeOutputsJob(data, jobName, markdownPath)
	if err != nil {
		return fmt.Errorf("failed to build consolidated safe outputs job: %w", err)
	}
	if consolidatedJob != nil {
		if err := c.jobManager.AddJob(consolidatedJob); err != nil {
			return fmt.Errorf("failed to add consolidated safe outputs job: %w", err)
		}
		safeOutputJobNames = append(safeOutputJobNames, consolidatedJob.Name)
		compilerSafeOutputJobsLog.Printf("Added consolidated safe outputs job with %d steps: %v", len(consolidatedStepNames), consolidatedStepNames)
	}

	// Build safe-jobs if configured
	// Safe-jobs should depend on agent job (always) AND detection job (if threat detection is enabled)
	// These custom safe-jobs should also be included in the conclusion job's dependencies
	safeJobNames, err := c.buildSafeJobs(data, threatDetectionEnabled)
	if err != nil {
		return fmt.Errorf("failed to build safe-jobs: %w", err)
	}
	// Add custom safe-job names to the list of safe output jobs
	safeOutputJobNames = append(safeOutputJobNames, safeJobNames...)
	compilerSafeOutputJobsLog.Printf("Added %d custom safe-job names to conclusion dependencies", len(safeJobNames))

	// Build upload_assets job as a separate job if configured
	// This needs to be separate from the consolidated safe_outputs job because it requires:
	// 1. Git configuration for pushing to orphaned branches
	// 2. Checkout with proper credentials
	// 3. Different permissions (contents: write)
	if data.SafeOutputs != nil && data.SafeOutputs.UploadAssets != nil {
		compilerSafeOutputJobsLog.Print("Building separate upload_assets job")
		uploadAssetsJob, err := c.buildUploadAssetsJob(data, jobName, threatDetectionEnabled)
		if err != nil {
			return fmt.Errorf("failed to build upload_assets job: %w", err)
		}
		if err := c.jobManager.AddJob(uploadAssetsJob); err != nil {
			return fmt.Errorf("failed to add upload_assets job: %w", err)
		}
		safeOutputJobNames = append(safeOutputJobNames, uploadAssetsJob.Name)
		compilerSafeOutputJobsLog.Printf("Added separate upload_assets job")
	}

	// Build conclusion job if add-comment is configured OR if command trigger is configured with reactions
	// This job runs last, after all safe output jobs (and push_repo_memory if configured), to update the activation comment on failure
	// The buildConclusionJob function itself will decide whether to create the job based on the configuration
	conclusionJob, err := c.buildConclusionJob(data, jobName, safeOutputJobNames)
	if err != nil {
		return fmt.Errorf("failed to build conclusion job: %w", err)
	}
	if conclusionJob != nil {
		// If push_repo_memory job exists, conclusion should depend on it
		// Check if the job was already created (it's created in buildJobs)
		if _, exists := c.jobManager.GetJob("push_repo_memory"); exists {
			conclusionJob.Needs = append(conclusionJob.Needs, "push_repo_memory")
			compilerSafeOutputJobsLog.Printf("Added push_repo_memory dependency to conclusion job")
		}
		if err := c.jobManager.AddJob(conclusionJob); err != nil {
			return fmt.Errorf("failed to add conclusion job: %w", err)
		}
	}

	return nil
}
