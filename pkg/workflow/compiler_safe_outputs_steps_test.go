//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildConsolidatedSafeOutputStep tests individual step building
func TestBuildConsolidatedSafeOutputStep(t *testing.T) {
	tests := []struct {
		name             string
		config           SafeOutputStepConfig
		checkContains    []string
		checkNotContains []string
	}{
		{
			name: "basic step with inline script",
			config: SafeOutputStepConfig{
				StepName: "Test Step",
				StepID:   "test_step",
				Script:   "console.log('test');",
				Token:    "${{ github.token }}",
			},
			checkContains: []string{
				"name: Test Step",
				"id: test_step",
				"uses: actions/github-script@",
				"GH_AW_AGENT_OUTPUT",
				"github-token:",
				"setupGlobals",
			},
		},
		{
			name: "step with script name (file mode)",
			config: SafeOutputStepConfig{
				StepName:   "Create Issue",
				StepID:     "create_issue",
				ScriptName: "create_issue_handler",
				Token:      "${{ github.token }}",
			},
			checkContains: []string{
				"name: Create Issue",
				"id: create_issue",
				"setupGlobals",
				"require('/opt/gh-aw/actions/create_issue_handler.cjs')",
				"await main();",
			},
			checkNotContains: []string{
				"console.log", // Should not inline script
			},
		},
		{
			name: "step with condition",
			config: SafeOutputStepConfig{
				StepName:  "Conditional Step",
				StepID:    "conditional",
				Script:    "console.log('test');",
				Token:     "${{ github.token }}",
				Condition: BuildEquals(BuildStringLiteral("test"), BuildStringLiteral("test")),
			},
			checkContains: []string{
				"if: 'test' == 'test'",
			},
		},
		{
			name: "step with custom env vars",
			config: SafeOutputStepConfig{
				StepName: "Step with Env",
				StepID:   "env_step",
				Script:   "console.log('test');",
				Token:    "${{ github.token }}",
				CustomEnvVars: []string{
					"          CUSTOM_VAR: \"value\"\n",
					"          ANOTHER_VAR: \"value2\"\n",
				},
			},
			checkContains: []string{
				"CUSTOM_VAR: \"value\"",
				"ANOTHER_VAR: \"value2\"",
			},
		},
		{
			name: "step with copilot token",
			config: SafeOutputStepConfig{
				StepName:        "Copilot Step",
				StepID:          "copilot",
				Script:          "console.log('test');",
				Token:           "${{ secrets.COPILOT_GITHUB_TOKEN }}",
				UseCopilotToken: true,
			},
			checkContains: []string{
				"github-token:",
			},
		},
		{
			name: "step with agent token",
			config: SafeOutputStepConfig{
				StepName:      "Agent Step",
				StepID:        "agent",
				Script:        "console.log('test');",
				Token:         "${{ secrets.AGENT_TOKEN }}",
				UseAgentToken: true,
			},
			checkContains: []string{
				"github-token:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			workflowData := &WorkflowData{
				Name:        "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{},
			}

			steps := compiler.buildConsolidatedSafeOutputStep(workflowData, tt.config)

			require.NotEmpty(t, steps)

			stepsContent := strings.Join(steps, "")

			for _, expected := range tt.checkContains {
				assert.Contains(t, stepsContent, expected, "Expected to find: "+expected)
			}

			for _, notExpected := range tt.checkNotContains {
				assert.NotContains(t, stepsContent, notExpected, "Should not contain: "+notExpected)
			}
		})
	}
}

// TestBuildSharedPRCheckoutSteps tests shared PR checkout step generation
func TestBuildSharedPRCheckoutSteps(t *testing.T) {
	tests := []struct {
		name          string
		safeOutputs   *SafeOutputsConfig
		trialMode     bool
		trialRepo     string
		checkContains []string
	}{
		{
			name: "create pull request only",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			checkContains: []string{
				"name: Checkout repository",
				"uses: actions/checkout@",
				"token: ${{ github.token }}",
				"persist-credentials: false",
				"fetch-depth: 1",
				"name: Configure Git credentials",
				"git config --global user.email",
				"github-actions[bot]@users.noreply.github.com",
			},
		},
		{
			name: "push to PR branch only",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			checkContains: []string{
				"name: Checkout repository",
				"name: Configure Git credentials",
			},
		},
		{
			name: "both create PR and push to PR branch",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests:      &CreatePullRequestsConfig{},
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			checkContains: []string{
				"name: Checkout repository",
				"name: Configure Git credentials",
			},
		},
		{
			name: "with GitHub App token",
			safeOutputs: &SafeOutputsConfig{
				App: &GitHubAppConfig{
					AppID:      "12345",
					PrivateKey: "test-key",
				},
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			checkContains: []string{
				"token: ${{ steps.safe-outputs-app-token.outputs.token }}",
			},
		},
		{
			name:      "trial mode with target repo",
			trialMode: true,
			trialRepo: "org/trial-repo",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			checkContains: []string{
				"repository: org/trial-repo",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			if tt.trialMode {
				compiler.SetTrialMode(true)
			}
			if tt.trialRepo != "" {
				compiler.SetTrialLogicalRepoSlug(tt.trialRepo)
			}

			workflowData := &WorkflowData{
				Name:        "Test Workflow",
				SafeOutputs: tt.safeOutputs,
			}

			steps := compiler.buildSharedPRCheckoutSteps(workflowData)

			require.NotEmpty(t, steps)

			stepsContent := strings.Join(steps, "")

			for _, expected := range tt.checkContains {
				assert.Contains(t, stepsContent, expected, "Expected to find: "+expected)
			}
		})
	}
}

// TestBuildSharedPRCheckoutStepsConditions tests conditional execution
func TestBuildSharedPRCheckoutStepsConditions(t *testing.T) {
	tests := []struct {
		name                   string
		createPR               bool
		pushToPRBranch         bool
		expectedConditionParts []string
	}{
		{
			name:                   "only create PR",
			createPR:               true,
			pushToPRBranch:         false,
			expectedConditionParts: []string{"create_pull_request"},
		},
		{
			name:                   "only push to PR branch",
			createPR:               false,
			pushToPRBranch:         true,
			expectedConditionParts: []string{"push_to_pull_request_branch"},
		},
		{
			name:                   "both operations",
			createPR:               true,
			pushToPRBranch:         true,
			expectedConditionParts: []string{"create_pull_request", "push_to_pull_request_branch", "||"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			safeOutputs := &SafeOutputsConfig{}
			if tt.createPR {
				safeOutputs.CreatePullRequests = &CreatePullRequestsConfig{}
			}
			if tt.pushToPRBranch {
				safeOutputs.PushToPullRequestBranch = &PushToPullRequestBranchConfig{}
			}

			workflowData := &WorkflowData{
				Name:        "Test Workflow",
				SafeOutputs: safeOutputs,
			}

			steps := compiler.buildSharedPRCheckoutSteps(workflowData)

			require.NotEmpty(t, steps)

			stepsContent := strings.Join(steps, "")

			for _, part := range tt.expectedConditionParts {
				assert.Contains(t, stepsContent, part, "Expected condition part: "+part)
			}
		})
	}
}

// TestBuildHandlerManagerStep tests handler manager step generation
func TestBuildHandlerManagerStep(t *testing.T) {
	tests := []struct {
		name          string
		safeOutputs   *SafeOutputsConfig
		checkContains []string
	}{
		{
			name: "basic handler manager",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
			checkContains: []string{
				"name: Process Safe Outputs",
				"id: process_safe_outputs",
				"uses: actions/github-script@",
				"GH_AW_AGENT_OUTPUT",
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
				"setupGlobals",
				"safe_output_handler_manager.cjs",
			},
		},
		{
			name: "handler manager with multiple types",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					TitlePrefix: "[Issue] ",
				},
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: 5,
					},
				},
				CreateDiscussions: &CreateDiscussionsConfig{
					Category: "general",
				},
			},
			checkContains: []string{
				"name: Process Safe Outputs",
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
		},
		// Note: create_project and create_project_status_update are now handled by
		// the project handler manager (buildProjectHandlerManagerStep), not the main handler manager
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			workflowData := &WorkflowData{
				Name:        "Test Workflow",
				SafeOutputs: tt.safeOutputs,
			}

			steps := compiler.buildHandlerManagerStep(workflowData)

			require.NotEmpty(t, steps)

			stepsContent := strings.Join(steps, "")

			for _, expected := range tt.checkContains {
				assert.Contains(t, stepsContent, expected, "Expected to find: "+expected)
			}
		})
	}
}

// TestBuildProjectHandlerManagerStep tests project handler manager step generation
func TestBuildProjectHandlerManagerStep(t *testing.T) {
	tests := []struct {
		name              string
		safeOutputs       *SafeOutputsConfig
		parsedFrontmatter *FrontmatterConfig
		checkContains     []string
	}{
		{
			name: "project handler manager with create_project",
			safeOutputs: &SafeOutputsConfig{
				CreateProjects: &CreateProjectsConfig{
					GitHubToken: "${{ secrets.PROJECTS_PAT }}",
					TargetOwner: "test-org",
				},
			},
			checkContains: []string{
				"name: Process Project-Related Safe Outputs",
				"id: process_project_safe_outputs",
				"uses: actions/github-script@",
				"GH_AW_AGENT_OUTPUT",
				"GH_AW_SAFE_OUTPUTS_PROJECT_HANDLER_CONFIG",
				"GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.PROJECTS_PAT }}",
				"github-token: ${{ secrets.PROJECTS_PAT }}",
				"setupGlobals",
				"safe_output_project_handler_manager.cjs",
			},
		},
		{
			name: "project handler manager with create_project_status_update",
			safeOutputs: &SafeOutputsConfig{
				CreateProjectStatusUpdates: &CreateProjectStatusUpdateConfig{
					GitHubToken: "${{ secrets.PROJECTS_PAT }}",
				},
			},
			checkContains: []string{
				"name: Process Project-Related Safe Outputs",
				"id: process_project_safe_outputs",
				"GH_AW_SAFE_OUTPUTS_PROJECT_HANDLER_CONFIG",
				"GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.PROJECTS_PAT }}",
				"github-token: ${{ secrets.PROJECTS_PAT }}",
			},
		},
		{
			name: "project handler manager without custom token uses default",
			safeOutputs: &SafeOutputsConfig{
				CreateProjects: &CreateProjectsConfig{
					TargetOwner: "test-org",
				},
			},
			checkContains: []string{
				"name: Process Project-Related Safe Outputs",
				"GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}",
				"github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}",
			},
		},
		{
			name: "project handler manager with project URL from frontmatter",
			safeOutputs: &SafeOutputsConfig{
				UpdateProjects: &UpdateProjectConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: 10,
					},
				},
			},
			parsedFrontmatter: &FrontmatterConfig{
				Project: &ProjectConfig{
					URL: "https://github.com/orgs/test-org/projects/123",
				},
			},
			checkContains: []string{
				"name: Process Project-Related Safe Outputs",
				"GH_AW_PROJECT_URL: \"https://github.com/orgs/test-org/projects/123\"",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			workflowData := &WorkflowData{
				Name:              "Test Workflow",
				SafeOutputs:       tt.safeOutputs,
				ParsedFrontmatter: tt.parsedFrontmatter,
			}

			steps := compiler.buildProjectHandlerManagerStep(workflowData)

			require.NotEmpty(t, steps)

			stepsContent := strings.Join(steps, "")

			for _, expected := range tt.checkContains {
				assert.Contains(t, stepsContent, expected, "Expected to find: "+expected)
			}
		})
	}
}

// TestStepOrderInConsolidatedJob tests that steps appear in correct order
func TestStepOrderInConsolidatedJob(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				TitlePrefix: "[Test] ",
			},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "agent", "test.md")

	require.NoError(t, err)
	require.NotNil(t, job)

	stepsContent := strings.Join(job.Steps, "")

	// Find positions of key steps
	setupPos := strings.Index(stepsContent, "name: Setup Scripts")
	downloadPos := strings.Index(stepsContent, "name: Download agent output")
	patchPos := strings.Index(stepsContent, "name: Download patch artifact")
	checkoutPos := strings.Index(stepsContent, "name: Checkout repository")
	gitConfigPos := strings.Index(stepsContent, "name: Configure Git credentials")
	handlerPos := strings.Index(stepsContent, "name: Process Safe Outputs")

	// Verify order
	if setupPos != -1 && downloadPos != -1 {
		assert.Less(t, setupPos, downloadPos, "Setup should come before download")
	}
	if downloadPos != -1 && patchPos != -1 {
		assert.Less(t, downloadPos, patchPos, "Agent output download should come before patch download")
	}
	if patchPos != -1 && checkoutPos != -1 {
		assert.Less(t, patchPos, checkoutPos, "Patch download should come before checkout")
	}
	if checkoutPos != -1 && gitConfigPos != -1 {
		assert.Less(t, checkoutPos, gitConfigPos, "Checkout should come before git config")
	}
	if gitConfigPos != -1 && handlerPos != -1 {
		assert.Less(t, gitConfigPos, handlerPos, "Git config should come before handler")
	}
}

// TestHandlerManagerOrderWithProjects tests that project handler manager comes before general handler manager
func TestHandlerManagerOrderWithProjects(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreateProjects: &CreateProjectsConfig{
				GitHubToken: "${{ secrets.PROJECTS_PAT }}",
				TargetOwner: "test-org",
			},
			CreateIssues: &CreateIssuesConfig{
				TitlePrefix: "[Test] ",
			},
			AssignToAgent: &AssignToAgentConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: 1,
				},
			},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "agent", "test.md")

	require.NoError(t, err)
	require.NotNil(t, job)

	stepsContent := strings.Join(job.Steps, "")

	// Find positions of handler steps
	projectHandlerPos := strings.Index(stepsContent, "name: Process Project-Related Safe Outputs")
	generalHandlerPos := strings.Index(stepsContent, "name: Process Safe Outputs")
	assignAgentPos := strings.Index(stepsContent, "name: Assign To Agent")

	// Verify all steps are present
	assert.NotEqual(t, -1, projectHandlerPos, "Project handler manager step should be present")
	assert.NotEqual(t, -1, generalHandlerPos, "General handler manager step should be present")
	assert.NotEqual(t, -1, assignAgentPos, "Assign to agent step should be present")

	// Verify correct order: Project Handler → General Handler → Assign To Agent
	assert.Less(t, projectHandlerPos, generalHandlerPos, "Project handler should come before general handler")
	assert.Less(t, generalHandlerPos, assignAgentPos, "General handler should come before assign to agent")
}

// TestStepWithoutCondition tests step building without condition
func TestStepWithoutCondition(t *testing.T) {
	compiler := NewCompiler()

	config := SafeOutputStepConfig{
		StepName: "Test Step",
		StepID:   "test",
		Script:   "console.log('test');",
		Token:    "${{ github.token }}",
	}

	workflowData := &WorkflowData{
		Name:        "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{},
	}

	steps := compiler.buildConsolidatedSafeOutputStep(workflowData, config)

	stepsContent := strings.Join(steps, "")

	// Should not have an 'if' line
	assert.NotContains(t, stepsContent, "if:")
}

// TestGitHubTokenPrecedence tests GitHub token selection logic
func TestGitHubTokenPrecedence(t *testing.T) {
	tests := []struct {
		name              string
		useAgentToken     bool
		useCopilotToken   bool
		token             string
		expectedInContent string
	}{
		{
			name:              "standard token",
			useAgentToken:     false,
			useCopilotToken:   false,
			token:             "${{ github.token }}",
			expectedInContent: "github-token:",
		},
		{
			name:              "copilot token",
			useAgentToken:     false,
			useCopilotToken:   true,
			token:             "${{ secrets.COPILOT_GITHUB_TOKEN }}",
			expectedInContent: "github-token:",
		},
		{
			name:              "agent token",
			useAgentToken:     true,
			useCopilotToken:   false,
			token:             "${{ secrets.AGENT_TOKEN }}",
			expectedInContent: "github-token:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			config := SafeOutputStepConfig{
				StepName:        "Test Step",
				StepID:          "test",
				Script:          "console.log('test');",
				Token:           tt.token,
				UseCopilotToken: tt.useCopilotToken,
				UseAgentToken:   tt.useAgentToken,
			}

			workflowData := &WorkflowData{
				Name:        "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{},
			}

			steps := compiler.buildConsolidatedSafeOutputStep(workflowData, config)

			stepsContent := strings.Join(steps, "")

			assert.Contains(t, stepsContent, tt.expectedInContent)
		})
	}
}

// TestScriptNameVsInlineScript tests the two modes of script inclusion
func TestScriptNameVsInlineScript(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:        "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{},
	}

	// Test inline script mode
	t.Run("inline script", func(t *testing.T) {
		config := SafeOutputStepConfig{
			StepName: "Inline Test",
			StepID:   "inline",
			Script:   "console.log('inline script');",
			Token:    "${{ github.token }}",
		}

		steps := compiler.buildConsolidatedSafeOutputStep(workflowData, config)
		stepsContent := strings.Join(steps, "")

		assert.Contains(t, stepsContent, "setupGlobals")
		assert.Contains(t, stepsContent, "console.log")
		// Inline scripts now include setupGlobals require statement
		assert.Contains(t, stepsContent, "require")
		// Inline scripts should not call await main()
		assert.NotContains(t, stepsContent, "await main()")
	})

	// Test file mode
	t.Run("file mode", func(t *testing.T) {
		config := SafeOutputStepConfig{
			StepName:   "File Test",
			StepID:     "file",
			ScriptName: "test_handler",
			Token:      "${{ github.token }}",
		}

		steps := compiler.buildConsolidatedSafeOutputStep(workflowData, config)
		stepsContent := strings.Join(steps, "")

		assert.Contains(t, stepsContent, "setupGlobals")
		assert.Contains(t, stepsContent, "require('/opt/gh-aw/actions/test_handler.cjs')")
		assert.Contains(t, stepsContent, "await main()")
		assert.NotContains(t, stepsContent, "console.log")
	})
}
