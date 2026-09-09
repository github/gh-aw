package workflow

// ========================================
// Safe Output Configuration Extraction
// ========================================
//
// ## Schema Generation Architecture
//
// MCP tool schemas for Safe Outputs are managed through a hybrid approach:
//
// ### Static Schemas (30+ built-in safe output types)
// Defined in: pkg/workflow/js/safe_outputs_tools.json
// - Embedded at compile time via //go:embed directive in pkg/workflow/js.go
// - Contains complete MCP tool definitions with inputSchema for all built-in types
// - Examples: create_issue, create_pull_request, add_comment, update_project, etc.
// - Accessed via GetSafeOutputsToolsJSON() function
//
// ### Dynamic Schema Generation (custom safe-jobs)
// Implemented in: pkg/workflow/safe_outputs_config_generation.go
// - generateCustomJobToolDefinition() builds MCP tool schemas from SafeJobConfig
// - Converts job input definitions to JSON Schema format
// - Supports type mapping (string, boolean, number, choice/enum)
// - Enforces required fields and additionalProperties: false
// - Custom job tools are merged with static tools at runtime
//
// ### Schema Filtering
// Implemented in: pkg/workflow/safe_outputs_config_generation.go
// - generateFilteredToolsJSON() filters tools based on enabled safe-outputs
// - Only includes tools that are configured in the workflow frontmatter
// - Reduces MCP gateway overhead by exposing only necessary tools
//
// ### Validation
// Implemented in: pkg/workflow/safe_outputs_tools_schema_test.go
// - TestSafeOutputsToolsJSONCompliesWithMCPSchema validates against MCP spec
// - TestEachToolHasRequiredMCPFields checks name, description, inputSchema
// - TestNoTopLevelOneOfAllOfAnyOf prevents unsupported schema constructs
//
// This architecture ensures schema consistency by:
// 1. Using embedded JSON for static schemas (single source of truth)
// 2. Programmatic generation for dynamic schemas (type-safe)
// 3. Automated validation in CI (regression prevention)
//

// extractSafeOutputsConfig extracts output configuration from frontmatter
//
//nolint:largefunc // Existing centralized safe-output extraction remains intentionally sequential.
func (c *Compiler) extractSafeOutputsConfig(frontmatter map[string]any) *SafeOutputsConfig {
	safeOutputsConfigLog.Print("Extracting safe-outputs configuration from frontmatter")

	var config *SafeOutputsConfig

	if output, exists := frontmatter["safe-outputs"]; exists {
		if outputMap, ok := output.(map[string]any); ok {
			safeOutputsConfigLog.Printf("Processing safe-outputs configuration with %d top-level keys", len(outputMap))
			config = &SafeOutputsConfig{}

			config.CreateWorkItems = c.parseCreateWorkItemConfig(outputMap)
			config.UpdateWorkItems = c.parseUpdateWorkItemConfig(outputMap)
			config.CommentOnWorkItems = c.parseCommentOnWorkItemConfig(outputMap)
			config.AssignWorkItems = c.parseAssignWorkItemConfig(outputMap)
			config.LinkWorkItems = c.parseLinkWorkItemsConfig(outputMap)
			config.UploadWorkItemAttachments = c.parseUploadWorkItemAttachmentConfig(outputMap)
			config.LinearCreateIssue = c.parseLinearCreateIssueConfig(outputMap)
			config.LinearAddComment = c.parseLinearAddCommentConfig(outputMap)
			config.LinearUpdateIssue = c.parseLinearUpdateIssueConfig(outputMap)

			// Handle create-issue
			issuesConfig := c.parseCreateIssuesConfig(outputMap)
			if issuesConfig != nil {
				safeOutputsConfigLog.Print("Configured create-issue output handler")
				config.CreateIssues = issuesConfig
			}

			c.extractJiraSafeOutputConfigs(outputMap, config)

			// Handle create-agent-session
			agentSessionConfig := c.parseAgentSessionConfig(outputMap)
			if agentSessionConfig != nil {
				config.CreateAgentSessions = agentSessionConfig
			}

			// Handle update-project (smart project board management)
			updateProjectConfig := c.parseUpdateProjectConfig(outputMap)
			if updateProjectConfig != nil {
				config.UpdateProjects = updateProjectConfig
			}

			// Handle create-project
			createProjectConfig := c.parseCreateProjectsConfig(outputMap)
			if createProjectConfig != nil {
				config.CreateProjects = createProjectConfig
			}

			// Handle create-project-status-update (project status updates)
			createProjectStatusUpdateConfig := c.parseCreateProjectStatusUpdateConfig(outputMap)
			if createProjectStatusUpdateConfig != nil {
				config.CreateProjectStatusUpdates = createProjectStatusUpdateConfig
			}

			// Handle create-discussion
			discussionsConfig := c.parseCreateDiscussionsConfig(outputMap)
			if discussionsConfig != nil {
				config.CreateDiscussions = discussionsConfig
			}

			// Handle close-discussion
			closeDiscussionsConfig := c.parseCloseDiscussionsConfig(outputMap)
			if closeDiscussionsConfig != nil {
				config.CloseDiscussions = closeDiscussionsConfig
			}

			// Handle close-issue
			closeIssuesConfig := c.parseCloseIssuesConfig(outputMap)
			if closeIssuesConfig != nil {
				config.CloseIssues = closeIssuesConfig
			}

			// Handle close-pull-request
			closePullRequestsConfig := c.parseClosePullRequestsConfig(outputMap)
			if closePullRequestsConfig != nil {
				config.ClosePullRequests = closePullRequestsConfig
			}

			// Handle mark-pull-request-as-ready-for-review
			markPRReadyConfig := c.parseMarkPullRequestAsReadyForReviewConfig(outputMap)
			if markPRReadyConfig != nil {
				config.MarkPullRequestAsReadyForReview = markPRReadyConfig
			}

			// Handle approve-workflow-run
			approveWorkflowRunConfig := c.parseApproveWorkflowRunConfig(outputMap)
			if approveWorkflowRunConfig != nil {
				config.ApproveWorkflowRun = approveWorkflowRunConfig
			}

			// Handle dismiss-pull-request-review (and dismiss-review alias)
			dismissPRReviewConfig := c.parseDismissPullRequestReviewConfig(outputMap)
			if dismissPRReviewConfig != nil {
				config.DismissPullRequestReview = dismissPRReviewConfig
			}

			// Handle add-comment
			commentsConfig := c.parseCommentsConfig(outputMap)
			if commentsConfig != nil {
				config.AddComments = commentsConfig
			}

			// Handle create-pull-request
			pullRequestsConfig := c.parseCreatePullRequestsConfig(outputMap)
			if pullRequestsConfig != nil {
				safeOutputsConfigLog.Print("Configured create-pull-request output handler")
				config.CreatePullRequests = pullRequestsConfig
			}

			// Handle create-pull-request-review-comment
			prReviewCommentsConfig := c.parsePullRequestReviewCommentsConfig(outputMap)
			if prReviewCommentsConfig != nil {
				config.CreatePullRequestReviewComments = prReviewCommentsConfig
			}

			// Handle submit-pull-request-review
			submitPRReviewConfig := c.parseSubmitPullRequestReviewConfig(outputMap)
			if submitPRReviewConfig != nil {
				config.SubmitPullRequestReview = submitPRReviewConfig
			}

			// Handle reply-to-pull-request-review-comment
			replyToPRReviewCommentConfig := c.parseReplyToPullRequestReviewCommentConfig(outputMap)
			if replyToPRReviewCommentConfig != nil {
				config.ReplyToPullRequestReviewComment = replyToPRReviewCommentConfig
			}

			// Handle resolve-pull-request-review-thread
			resolvePRReviewThreadConfig := c.parseResolvePullRequestReviewThreadConfig(outputMap)
			if resolvePRReviewThreadConfig != nil {
				config.ResolvePullRequestReviewThread = resolvePRReviewThreadConfig
			}

			// Handle create-code-scanning-alert
			securityReportsConfig := c.parseCodeScanningAlertsConfig(outputMap)
			if securityReportsConfig != nil {
				config.CreateCodeScanningAlerts = securityReportsConfig
			}

			// Handle autofix-code-scanning-alert
			autofixCodeScanningAlertConfig := c.parseAutofixCodeScanningAlertConfig(outputMap)
			if autofixCodeScanningAlertConfig != nil {
				config.AutofixCodeScanningAlert = autofixCodeScanningAlertConfig
			}

			// Handle create-check-run
			createCheckRunConfig := c.parseCreateCheckRunConfig(outputMap)
			if createCheckRunConfig != nil {
				config.CreateCheckRun = createCheckRunConfig
			}

			// Parse add-labels configuration
			addLabelsConfig := c.parseAddLabelsConfig(outputMap)
			if addLabelsConfig != nil {
				config.AddLabels = addLabelsConfig
			}

			// Parse remove-labels configuration
			removeLabelsConfig := c.parseRemoveLabelsConfig(outputMap)
			if removeLabelsConfig != nil {
				config.RemoveLabels = removeLabelsConfig
			}

			// Parse replace-label configuration
			replaceLabelConfig := c.parseReplaceLabelConfig(outputMap)
			if replaceLabelConfig != nil {
				config.ReplaceLabel = replaceLabelConfig
			}

			// Parse add-reviewer configuration
			addReviewerConfig := c.parseAddReviewerConfig(outputMap)
			if addReviewerConfig != nil {
				config.AddReviewer = addReviewerConfig
			}

			// Parse assign-milestone configuration
			assignMilestoneConfig := c.parseAssignMilestoneConfig(outputMap)
			if assignMilestoneConfig != nil {
				config.AssignMilestone = assignMilestoneConfig
			}

			// Handle assign-to-agent
			assignToAgentConfig := c.parseAssignToAgentConfig(outputMap)
			if assignToAgentConfig != nil {
				config.AssignToAgent = assignToAgentConfig
			}

			// Handle assign-to-user
			assignToUserConfig := c.parseAssignToUserConfig(outputMap)
			if assignToUserConfig != nil {
				config.AssignToUser = assignToUserConfig
			}

			// Handle unassign-from-user
			unassignFromUserConfig := c.parseUnassignFromUserConfig(outputMap)
			if unassignFromUserConfig != nil {
				config.UnassignFromUser = unassignFromUserConfig
			}

			// Handle update-issue
			updateIssuesConfig := c.parseUpdateIssuesConfig(outputMap)
			if updateIssuesConfig != nil {
				config.UpdateIssues = updateIssuesConfig
			}

			// Handle update-discussion
			updateDiscussionsConfig := c.parseUpdateDiscussionsConfig(outputMap)
			if updateDiscussionsConfig != nil {
				config.UpdateDiscussions = updateDiscussionsConfig
			}

			// Handle update-pull-request
			updatePullRequestsConfig := c.parseUpdatePullRequestsConfig(outputMap)
			if updatePullRequestsConfig != nil {
				config.UpdatePullRequests = updatePullRequestsConfig
			}

			// Handle merge-pull-request
			mergePullRequestConfig := c.parseMergePullRequestConfig(outputMap)
			if mergePullRequestConfig != nil {
				config.MergePullRequest = mergePullRequestConfig
			}

			// Handle push-to-pull-request-branch
			pushToBranchConfig := c.parsePushToPullRequestBranchConfig(outputMap)
			if pushToBranchConfig != nil {
				config.PushToPullRequestBranch = pushToBranchConfig
			}

			// Handle upload-asset
			uploadAssetsConfig := c.parseUploadAssetConfig(outputMap)
			if uploadAssetsConfig != nil {
				config.UploadAssets = uploadAssetsConfig
			}

			// Handle upload-artifact
			uploadArtifactConfig := c.parseUploadArtifactConfig(outputMap)
			if uploadArtifactConfig != nil {
				config.UploadArtifact = uploadArtifactConfig
			}

			// Handle upload-code-coverage
			uploadCodeCoverageConfig := c.parseUploadCodeCoverageConfig(outputMap)
			if uploadCodeCoverageConfig != nil {
				config.UploadCodeCoverage = uploadCodeCoverageConfig
			}

			// Handle update-release
			updateReleaseConfig := c.parseUpdateReleaseConfig(outputMap)
			if updateReleaseConfig != nil {
				config.UpdateRelease = updateReleaseConfig
			}

			// Handle link-sub-issue
			linkSubIssueConfig := c.parseLinkSubIssueConfig(outputMap)
			if linkSubIssueConfig != nil {
				config.LinkSubIssue = linkSubIssueConfig
			}

			// Handle hide-comment
			hideCommentConfig := c.parseHideCommentConfig(outputMap)
			if hideCommentConfig != nil {
				config.HideComment = hideCommentConfig
			}

			// Handle set-issue-type
			setIssueTypeConfig := c.parseSetIssueTypeConfig(outputMap)
			if setIssueTypeConfig != nil {
				config.SetIssueType = setIssueTypeConfig
			}

			// Handle set-issue-field
			setIssueFieldConfig := c.parseSetIssueFieldConfig(outputMap)
			if setIssueFieldConfig != nil {
				config.SetIssueField = setIssueFieldConfig
			}

			// Handle dispatch-workflow
			dispatchWorkflowConfig := c.parseDispatchWorkflowConfig(outputMap)
			if dispatchWorkflowConfig != nil {
				config.DispatchWorkflow = dispatchWorkflowConfig
			}

			// Handle dispatch_repository
			dispatchRepositoryConfig := c.parseDispatchRepositoryConfig(outputMap)
			if dispatchRepositoryConfig != nil {
				config.DispatchRepository = dispatchRepositoryConfig
			}

			// Handle call-workflow
			callWorkflowConfig := c.parseCallWorkflowConfig(outputMap)
			if callWorkflowConfig != nil {
				config.CallWorkflow = callWorkflowConfig
			}

			// Handle missing-tool (parse configuration if present, or enable by default)
			missingToolConfig := c.parseMissingToolConfig(outputMap)
			if missingToolConfig != nil {
				config.MissingTool = missingToolConfig
			} else {
				// Enable missing-tool by default if safe-outputs exists and it wasn't explicitly disabled
				if _, exists := outputMap["missing-tool"]; !exists {
					trueVal := "true"
					config.MissingTool = &MissingToolConfig{
						CreateIssue: &trueVal,
						TitlePrefix: "",
						Labels:      nil,
					}
				}
			}

			// Handle missing-data (parse configuration if present, or enable by default)
			missingDataConfig := c.parseMissingDataConfig(outputMap)
			if missingDataConfig != nil {
				config.MissingData = missingDataConfig
			} else {
				// Enable missing-data by default if safe-outputs exists and it wasn't explicitly disabled
				if _, exists := outputMap["missing-data"]; !exists {
					trueVal := "true"
					config.MissingData = &MissingDataConfig{
						CreateIssue: &trueVal,
						TitlePrefix: "",
						Labels:      nil,
					}
				}
			}

			// Handle noop (parse configuration if present, or enable by default as fallback)
			noopConfig := c.parseNoOpConfig(outputMap)
			if noopConfig != nil {
				config.NoOp = noopConfig
			} else {
				// Enable noop by default if safe-outputs exists and it wasn't explicitly disabled
				// This ensures there's always a fallback for transparency
				if _, exists := outputMap["noop"]; !exists {
					config.NoOp = &NoOpConfig{}
					config.NoOp.Max = defaultIntStr(1) // Default max
					// Implicit noop is for transparency logging only; it must not create
					// issues without a maintenance workflow to expire them, so report-as-issue
					// defaults to false here (users can opt in with an explicit noop: block).
					falseVal := "false"
					config.NoOp.ReportAsIssue = &falseVal
					config.NoOp.Implicit = true // Not authored by the user
				}
			}

			// Handle report-incomplete (parse configuration if present, or enable by default)
			reportIncompleteConfig := c.parseReportIncompleteConfig(outputMap)
			if reportIncompleteConfig != nil {
				config.ReportIncomplete = reportIncompleteConfig
			} else {
				// Enable report-incomplete by default if safe-outputs exists and it wasn't explicitly disabled.
				// This ensures agents always have a first-class channel to signal task incompletion.
				if _, exists := outputMap["report-incomplete"]; !exists {
					trueVal := defaultReportIncompleteCreateIssue(config)
					config.ReportIncomplete = &ReportIncompleteConfig{
						CreateIssue: &trueVal,
						TitlePrefix: "",
						Labels:      nil,
						Implicit:    true,
					}
				}
			}

			c.extractGlobalConfigFields(outputMap, config)
		}
	}

	// Apply default threat detection whenever safe-outputs are configured and threat-detection
	// is not explicitly disabled. Detection is always on unless threat-detection is false.
	if config != nil && config.ThreatDetection == nil {
		if output, exists := frontmatter["safe-outputs"]; exists {
			if outputMap, ok := output.(map[string]any); ok {
				if _, exists := outputMap["threat-detection"]; !exists {
					// Only apply default if threat-detection key doesn't exist
					safeOutputsConfigLog.Print("Applying default threat-detection configuration")
					config.ThreatDetection = &ThreatDetectionConfig{}
				}
			}
		}
	}

	// Force-disable threat detection when samples replay is active: the replay driver
	// emits synthetic outputs solely for deterministic end-to-end tests, and running
	// an LLM-backed detection pass would defeat that determinism.
	if config != nil && c.samplesEnabled(frontmatter) && config.ThreatDetection != nil {
		safeOutputsConfigLog.Print("Disabling threat-detection because samples replay is enabled")
		config.ThreatDetection = nil
	}

	if config != nil {
		safeOutputsConfigLog.Print("Successfully extracted safe-outputs configuration")
	} else {
		safeOutputsConfigLog.Print("No safe-outputs configuration found in frontmatter")
	}

	return config
}
