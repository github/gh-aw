package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var compilerSafeOutputJobsLog = logger.New("workflow:compiler_safe_output_jobs")

// buildSafeOutputsJobs builds all safe output jobs based on the configuration in data.SafeOutputs.
// It creates a separate detection job (if threat detection is enabled), a consolidated safe_outputs
// job containing all safe output operations as steps, plus custom safe-jobs and the conclusion job.
// When call-workflow is configured, it also generates conditional `uses:` fan-out jobs
// (one per allowed worker workflow) that run after safe_outputs.
func (c *Compiler) buildSafeOutputsJobs(data *WorkflowData, jobName, markdownPath string) error {
	if data.SafeOutputs == nil {
		compilerSafeOutputJobsLog.Print("No safe outputs configured, skipping safe outputs jobs")
		return nil
	}
	compilerSafeOutputJobsLog.Print("Building safe outputs jobs")

	// Detection is always enabled for safe-outputs workflows unless threat-detection is explicitly
	// disabled (threat-detection: false) or the engine is disabled with no custom steps
	// (threat-detection: { engine: false } with no steps). ThreatDetection is nil only when
	// explicitly disabled. When engine is false with no custom steps, the detection job has
	// nothing to run so it is skipped entirely.
	threatDetectionEnabled := IsDetectionJobEnabled(data.SafeOutputs)

	// Build the separate detection job. Detection runs by default for all safe-outputs workflows
	// and is only skipped when ThreatDetection is nil (i.e. threat-detection: false was set).
	// The detection job runs after the agent job, downloads the agent artifact,
	// and outputs detection_success and detection_conclusion for downstream jobs.
	if threatDetectionEnabled {
		detectionJob, err := c.buildDetectionJob(data)
		if err != nil {
			return fmt.Errorf("failed to build detection job: %w", err)
		}
		if detectionJob != nil {
			if err := c.jobManager.AddJob(detectionJob); err != nil {
				return fmt.Errorf("failed to add detection job: %w", err)
			}
			compilerSafeOutputJobsLog.Print("Added separate detection job")
		}
	}

	// Track safe output job names to establish dependencies for conclusion job
	var safeOutputJobNames []string
	var preparedJobs []string
	if isWorkQueueEnabled(data) {
		c.resolveAllActions(data, markdownPath)
		var err error
		preparedJobs, err = c.buildWorkQueuePreparedAdapterJobs(data, threatDetectionEnabled)
		if err != nil {
			return fmt.Errorf("failed to build trusted Claim adapter preparation: %w", err)
		}
	}

	// Build consolidated safe outputs job containing all safe output operations as steps
	consolidatedJob, consolidatedStepNames, err := c.buildConsolidatedSafeOutputsJob(data, jobName, markdownPath)
	if err != nil {
		return fmt.Errorf("failed to build consolidated safe outputs job: %w", err)
	}
	if consolidatedJob != nil {
		addWorkQueuePreparedAdapterNeeds(consolidatedJob, preparedJobs)
		if err := c.jobManager.AddJob(consolidatedJob); err != nil {
			return fmt.Errorf("failed to add consolidated safe outputs job: %w", err)
		}
		safeOutputJobNames = append(safeOutputJobNames, consolidatedJob.Name)
		compilerSafeOutputJobsLog.Printf("Added consolidated safe outputs job with %d steps: %v", len(consolidatedStepNames), consolidatedStepNames)
	}

	// Build safe-jobs if configured
	// Safe-jobs should depend on agent job (always) AND detection job (if threat detection is enabled)
	// These custom safe-jobs should also be included in the conclusion job's dependencies
	safeJobNames := preparedJobs
	if !isWorkQueueEnabled(data) {
		safeJobNames, err = c.buildSafeJobs(data, threatDetectionEnabled)
		if err != nil {
			return fmt.Errorf("failed to build safe-jobs: %w", err)
		}
	}
	// Add custom safe-job names to the list of safe output jobs
	safeOutputJobNames = append(safeOutputJobNames, safeJobNames...)
	compilerSafeOutputJobsLog.Printf("Added %d custom safe-job names to conclusion dependencies", len(safeJobNames))

	// Build upload_assets job as a separate job if configured
	// This needs to be separate from the consolidated safe_outputs job because it requires:
	// 1. Git configuration for pushing to orphaned branches
	// 2. Checkout with proper credentials
	// 3. Different permissions (contents: write)
	if data.SafeOutputs != nil && data.SafeOutputs.UploadAssets != nil && !isWorkQueueEnabled(data) {
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

	// Build upload_code_scanning_sarif job as a separate job if create-code-scanning-alert is configured.
	// This job runs after safe_outputs and only when the safe_outputs job exported a SARIF file.
	// It is separate to avoid the checkout step (needed to restore HEAD to github.sha) from
	// interfering with other safe-output operations in the consolidated safe_outputs job.
	if data.SafeOutputs != nil && data.SafeOutputs.CreateCodeScanningAlerts != nil &&
		!isWorkQueueEnabled(data) &&
		!isHandlerStaged(templatableBoolIsTrue(data.SafeOutputs.Staged), data.SafeOutputs.CreateCodeScanningAlerts.Staged) {
		compilerSafeOutputJobsLog.Print("Building separate upload_code_scanning_sarif job")
		codeScanningJob, err := c.buildCodeScanningUploadJob(data)
		if err != nil {
			return fmt.Errorf("failed to build upload_code_scanning_sarif job: %w", err)
		}
		if err := c.jobManager.AddJob(codeScanningJob); err != nil {
			return fmt.Errorf("failed to add upload_code_scanning_sarif job: %w", err)
		}
		safeOutputJobNames = append(safeOutputJobNames, codeScanningJob.Name)
		compilerSafeOutputJobsLog.Printf("Added separate upload_code_scanning_sarif job")
	}

	// Build upload_code_coverage job as a separate job if upload-code-coverage is configured.
	// This job runs after safe_outputs and only when the safe_outputs job exported a coverage
	// file. It is separate so the real actions/upload-code-coverage action can be invoked with
	// its own dedicated permissions (code-quality: write) without affecting other safe-output
	// operations in the consolidated safe_outputs job.
	if data.SafeOutputs != nil && data.SafeOutputs.UploadCodeCoverage != nil &&
		!isWorkQueueEnabled(data) &&
		!isHandlerStaged(templatableBoolIsTrue(data.SafeOutputs.Staged), data.SafeOutputs.UploadCodeCoverage.Staged) {
		compilerSafeOutputJobsLog.Print("Building separate upload_code_coverage job")
		codeCoverageJob, err := c.buildUploadCodeCoverageJob(data, jobName)
		if err != nil {
			return fmt.Errorf("failed to build upload_code_coverage job: %w", err)
		}
		if err := c.jobManager.AddJob(codeCoverageJob); err != nil {
			return fmt.Errorf("failed to add upload_code_coverage job: %w", err)
		}
		safeOutputJobNames = append(safeOutputJobNames, codeCoverageJob.Name)
		compilerSafeOutputJobsLog.Printf("Added separate upload_code_coverage job")
	}

	// Build conditional call-workflow fan-out jobs if configured.
	// Each allowed worker gets its own `uses:` job with an `if:` condition that
	// checks whether safe_outputs selected it. Only one runs per execution.
	callWorkflowJobNames, err := c.buildCallWorkflowJobs(data, markdownPath)
	if err != nil {
		return fmt.Errorf("failed to build call-workflow fan-out jobs: %w", err)
	}
	safeOutputJobNames = append(safeOutputJobNames, callWorkflowJobNames...)
	compilerSafeOutputJobsLog.Printf("Added %d call-workflow fan-out jobs", len(callWorkflowJobNames))

	// Build dedicated unlock job if lock-for-agent is enabled
	// This job is separate from conclusion to ensure it always runs, even if other jobs fail
	// It depends on agent and detection (if enabled) to run after workflow execution completes
	unlockJob, err := c.buildUnlockJob(data, threatDetectionEnabled)
	if err != nil {
		return fmt.Errorf("failed to build unlock job: %w", err)
	}
	if unlockJob != nil {
		if err := c.jobManager.AddJob(unlockJob); err != nil {
			return fmt.Errorf("failed to add unlock job: %w", err)
		}
		compilerSafeOutputJobsLog.Print("Added dedicated unlock job")
	}

	// Build conclusion job if add-comment is configured OR if command trigger is configured with reactions
	// This job runs last, after all safe output jobs (and push_repo_memory if configured), to update the activation comment on failure
	// The buildConclusionJob function itself will decide whether to create the job based on the configuration
	conclusionJob, err := c.buildConclusionJob(data, jobName, safeOutputJobNames)
	if err != nil {
		return fmt.Errorf("failed to build conclusion job: %w", err)
	}
	if conclusionJob != nil {
		// If unlock job exists, conclusion should depend on it to run after unlock completes
		if unlockJob != nil {
			conclusionJob.Needs = append(conclusionJob.Needs, "unlock")
			compilerSafeOutputJobsLog.Printf("Added unlock job dependency to conclusion job")
		}
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

// buildCallWorkflowJobs generates one conditional `uses:` job per workflow in the
// call-workflow allowlist. Each job:
//   - depends on safe_outputs
//   - has an `if:` that checks needs.safe_outputs.outputs.call_workflow_name
//   - uses: the relative path to the worker's .lock.yml (or .yml)
//   - forwards declared workflow_call inputs in `with:` so worker steps can reference inputs.<name> directly:
//   - non-payload inputs: `fromJSON(needs.safe_outputs.outputs.call_workflow_payload).<name>`
//   - `payload` is forwarded as the raw transport only when the worker declares it
//     (GitHub Actions rejects undeclared inputs)
//   - inherits all caller secrets via `secrets: inherit`
//   - includes a job-level `permissions:` block equal to the union of the
//     caller's declared permissions and the called worker's required permissions
//   - adds a help comment explaining why imported worker permissions appear on
//     the call job and where to review them in the worker workflow source
//
// Returns the names of all generated jobs so they can be added to the conclusion
// job's `needs` list.
func (c *Compiler) buildCallWorkflowJobs(data *WorkflowData, markdownPath string) ([]string, error) {
	if data.SafeOutputs == nil || data.SafeOutputs.CallWorkflow == nil {
		return nil, nil
	}

	config := data.SafeOutputs.CallWorkflow
	if len(config.Workflows) == 0 {
		return nil, nil
	}

	compilerSafeOutputJobsLog.Printf("Building %d call-workflow fan-out jobs", len(config.Workflows))

	var jobNames []string

	for _, workflowName := range config.Workflows {
		// Build the job name: "call-{sanitized-workflow-name}"
		// sanitizeJobName normalizes underscores and periods to hyphens.
		sanitizedName := sanitizeJobName(workflowName)
		jobName := "call-" + sanitizedName

		// Determine the relative path to the worker workflow file
		workflowPath, ok := config.WorkflowFiles[workflowName]
		if !ok || workflowPath == "" {
			// Fallback: construct path from name
			workflowPath = fmt.Sprintf("./.github/workflows/%s.lock.yml", workflowName)
		}

		// Build the with: block. Forward one entry per declared workflow_call input
		// on the worker, derived from the payload, so that worker steps can reference
		// inputs.<name> directly without parsing JSON. The canonical `payload`
		// envelope is only forwarded when the worker explicitly declares a `payload`
		// input; GitHub Actions rejects a `uses:` step that passes an input the
		// called workflow does not declare, so it must not be added unconditionally.
		jobNeeds := []string{"safe_outputs"}
		with := map[string]any{}

		if markdownPath != "" {
			fileResult, findErr := findWorkflowFile(workflowName, markdownPath)
			if findErr != nil {
				compilerSafeOutputJobsLog.Printf("Warning: could not find worker workflow file for '%s': %v. "+
					"Typed inputs will not be forwarded in the with: block.", workflowName, findErr)
			} else {
				var workflowInputs map[string]any
				var inputErr error
				switch {
				case fileResult.lockExists:
					workflowInputs, inputErr = extractWorkflowCallInputs(fileResult.lockPath)
				case fileResult.ymlExists:
					workflowInputs, inputErr = extractWorkflowCallInputs(fileResult.ymlPath)
				case fileResult.mdExists:
					workflowInputs, inputErr = extractMDWorkflowCallInputs(fileResult.mdPath)
				default:
					compilerSafeOutputJobsLog.Printf("Warning: no worker file found for '%s'; "+
						"typed inputs will not be forwarded in the with: block.", workflowName)
				}
				if inputErr != nil {
					compilerSafeOutputJobsLog.Printf("Warning: could not extract workflow_call inputs for '%s': %v. "+
						"Typed inputs will not be forwarded in the with: block.", workflowName, inputErr)
				} else if workflowInputs != nil {
					typedInputCount := 0
					for inputName := range workflowInputs {
						if inputName == "payload" {
							// The worker explicitly declares the canonical payload
							// envelope input; forward the raw transport rather than a
							// fromJSON expression.
							with["payload"] = "${{ needs.safe_outputs.outputs.call_workflow_payload }}"
							continue
						}
						with[inputName] = buildCallWorkflowInputExpression(inputName)
						typedInputCount++
					}
					compilerSafeOutputJobsLog.Printf("Forwarding %d typed inputs for call-workflow job '%s'", typedInputCount, jobName)
				}

			}
		}

		callJob := &Job{
			Name:  jobName,
			Needs: jobNeeds,
			If:    fmt.Sprintf("needs.safe_outputs.outputs.call_workflow_name == '%s'", workflowName),
			Uses:  workflowPath,
			With:  with,
		}

		// Infer the minimal set of secrets required by the worker workflow so we can
		// pass them explicitly instead of using secrets: inherit. This requires the
		// worker to have been compiled with on.workflow_call.secrets declarations.
		// If the worker has not yet been compiled (no .lock.yml/.yml), or declares no
		// secrets, fall back to secrets: inherit for backward compatibility.
		if markdownPath != "" {
			workerSecrets, secretsErr := extractCallWorkflowSecrets(workflowName, markdownPath)
			if secretsErr != nil {
				compilerSafeOutputJobsLog.Printf("Warning: could not extract secrets for call-workflow job '%s': %v. "+
					"Falling back to secrets: inherit.", jobName, secretsErr)
				callJob.SecretsInherit = true
			} else if len(workerSecrets) == 0 {
				// No secrets were extracted from the worker. This can mean either the
				// worker declares no workflow_call secrets or its compiled file was not
				// found yet. Fall back to secrets: inherit for backward compatibility.
				compilerSafeOutputJobsLog.Printf("No workflow_call secrets could be extracted for worker '%s' "+
					"(worker may declare none or its compiled file may not exist yet); using secrets: inherit", workflowName)
				callJob.SecretsInherit = true
			} else {
				// Map each declared secret explicitly.
				callJob.Secrets = make(map[string]string, len(workerSecrets))
				for _, s := range workerSecrets {
					callJob.Secrets[s] = fmt.Sprintf("${{ secrets.%s }}", s)
				}
				compilerSafeOutputJobsLog.Printf("Mapped %d explicit secrets for call-workflow job '%s'", len(workerSecrets), jobName)
			}
		} else {
			callJob.SecretsInherit = true
		}

		// Compute the call-<worker> job's permission envelope as the union of:
		//   1. The caller's own declared permissions (the base scope the caller controls).
		//   2. The worker's job-level permissions (the minimum the worker needs to run).
		// GitHub validates reusable workflow calls against the caller job's declared
		// permissions and rejects the run at startup when the caller grants less than
		// the worker requires. Taking the union ensures the call job always holds a
		// sufficient grant without requiring the caller's markdown to enumerate every
		// permission the worker needs.
		callerPerms := data.CachedPermissions
		if callerPerms == nil {
			callerPerms = NewPermissionsParser(data.Permissions).ToPermissions()
		}

		effectivePerms := callerPerms
		var importedPerms *callWorkflowPermissionImport
		var permErr error
		if markdownPath != "" {
			importedPerms, permErr = extractCallWorkflowPermissionImport(workflowName, markdownPath)
			if permErr != nil {
				// Non-fatal: log and continue. The worker file may not exist yet (it may be
				// compiled in the same batch), in which case we fall back to the caller's
				// own declared permissions.
				compilerSafeOutputJobsLog.Printf("Could not extract worker permissions for call-workflow job '%s' (falling back to caller-only permissions): %v", jobName, permErr)
			} else if importedPerms != nil && importedPerms.permissions != nil {
				// Compute the union by merging caller and worker permissions into a
				// fresh map-based Permissions. Starting from a blank slate (rather
				// than a clone of callerPerms) ensures shorthand values like
				// "read-all" are correctly expanded before the worker's explicit
				// scopes are merged on top — cloning a shorthand Permissions and then
				// merging a map into it would clear the shorthand field without first
				// expanding it, silently dropping the caller's baseline grant.
				merged := NewPermissions()
				merged.Merge(callerPerms)
				merged.Merge(importedPerms.permissions)
				effectivePerms = merged
				compilerSafeOutputJobsLog.Printf("Merged caller and worker permissions for call-workflow job '%s'", jobName)
			}
		}

		if effectivePerms != nil {
			rendered := effectivePerms.RenderToYAML()
			if rendered != "" {
				callJob.PermissionsComment = buildCallWorkflowPermissionsComment(workflowName, importedPerms)
				callJob.Permissions = rendered
				compilerSafeOutputJobsLog.Printf("Set permissions on call-workflow job '%s': %s", jobName, rendered)
			}
		}

		if err := c.jobManager.AddJob(callJob); err != nil {
			return nil, fmt.Errorf("failed to add call-workflow job '%s': %w", jobName, err)
		}

		jobNames = append(jobNames, jobName)
		compilerSafeOutputJobsLog.Printf("Added call-workflow job: %s (uses: %s)", jobName, workflowPath)
	}

	return jobNames, nil
}

func buildCallWorkflowInputExpression(inputName string) string {
	payloadExpr := "fromJSON(needs.safe_outputs.outputs.call_workflow_payload)"
	if isBareActionsIdentifier(inputName) {
		return fmt.Sprintf("${{ %s.%s }}", payloadExpr, inputName)
	}

	escapedInputName := escapeActionsSingleQuotedString(inputName)
	return fmt.Sprintf("${{ %s['%s'] }}", payloadExpr, escapedInputName)
}

// isBareActionsIdentifier reports whether a name can be safely referenced via
// dot access in GitHub Actions expressions (letters/underscore followed by
// letters, digits, or underscore).
func isBareActionsIdentifier(name string) bool {
	if name == "" {
		return false
	}

	for i, r := range name {
		if i == 0 {
			if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
				return false
			}
			continue
		}

		if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}

	return true
}
