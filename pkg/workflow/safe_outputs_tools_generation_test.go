//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateCustomJobToolDefinitionBasic tests the basic structure of a generated custom tool.
func TestGenerateCustomJobToolDefinitionBasic(t *testing.T) {
	jobConfig := &SafeJobConfig{
		Description: "My custom job",
		Inputs: map[string]*InputDefinition{
			"env": {
				Type:        "choice",
				Description: "Environment to deploy to",
				Options:     []string{"staging", "production"},
				Required:    true,
			},
			"message": {
				Type:        "string",
				Description: "Optional message",
			},
		},
	}

	tool := generateCustomJobToolDefinition("deploy_app", jobConfig)

	assert.Equal(t, "deploy_app", tool["name"], "Tool name should match")
	assert.Equal(t, "My custom job", tool["description"], "Description should match")

	inputSchema, ok := tool["inputSchema"].(map[string]any)
	require.True(t, ok, "inputSchema should be present")
	assert.Equal(t, "object", inputSchema["type"], "inputSchema type should be object")
	assert.Equal(t, false, inputSchema["additionalProperties"], "additionalProperties should be false")

	required, ok := inputSchema["required"].([]string)
	require.True(t, ok, "required should be a []string")
	assert.Contains(t, required, "env", "env should be required")

	properties, ok := inputSchema["properties"].(map[string]any)
	require.True(t, ok, "properties should be present")

	envProp, ok := properties["env"].(map[string]any)
	require.True(t, ok, "env property should exist")
	assert.Equal(t, "string", envProp["type"], "choice type maps to string")
	assert.Equal(t, []string{"staging", "production"}, envProp["enum"], "enum values should match")
}

func TestDispatchWorkflowWorkQueueSelector(t *testing.T) {
	normal := generateDispatchWorkflowTool("worker", map[string]any{}, nil, false)
	normalProps := normal["inputSchema"].(map[string]any)["properties"].(map[string]any)
	assert.NotContains(t, normalProps, "work_queue")

	withoutContext := generateDispatchWorkflowTool("worker", map[string]any{}, nil, true)
	assert.NotContains(t, withoutContext["inputSchema"].(map[string]any)["properties"].(map[string]any), "work_queue")

	queue := generateDispatchWorkflowTool("worker", map[string]any{
		"aw_context":            map[string]any{"type": "string", "required": true},
		"work_queue_claim":      map[string]any{"type": "string", "required": true},
		"work_queue_assignment": map[string]any{"type": "string", "required": true},
	}, nil, true)
	assert.NotContains(t, queue["inputSchema"].(map[string]any)["properties"].(map[string]any), "aw_context")
	assert.NotContains(t, queue["inputSchema"].(map[string]any)["required"], "aw_context")
	assert.NotContains(t, queue["inputSchema"].(map[string]any)["properties"].(map[string]any), "work_queue_claim")
	assert.NotContains(t, queue["inputSchema"].(map[string]any)["required"], "work_queue_claim")
	props := queue["inputSchema"].(map[string]any)["properties"].(map[string]any)
	assert.NotContains(t, props, "work_queue_assignment")
	assert.NotContains(t, props, "work_queue", "agent-specific Work selectors cannot bypass the installed scheduler")
}

// TestGenerateCustomJobToolDefinitionDefaultDescription tests that a default description is used when none provided.
func TestGenerateCustomJobToolDefinitionDefaultDescription(t *testing.T) {
	jobConfig := &SafeJobConfig{}
	tool := generateCustomJobToolDefinition("my_job", jobConfig)
	assert.Equal(t, "Execute the my_job custom job", tool["description"], "Default description should be set")
}

// TestGenerateCustomJobToolDefinitionBooleanInput tests boolean input type mapping.
func TestGenerateCustomJobToolDefinitionBooleanInput(t *testing.T) {
	jobConfig := &SafeJobConfig{
		Inputs: map[string]*InputDefinition{
			"dry_run": {
				Type:     "boolean",
				Required: false,
			},
		},
	}

	tool := generateCustomJobToolDefinition("run_job", jobConfig)
	inputSchema := tool["inputSchema"].(map[string]any)
	properties := inputSchema["properties"].(map[string]any)

	dryRunProp, ok := properties["dry_run"].(map[string]any)
	require.True(t, ok, "dry_run property should exist")
	assert.Equal(t, "boolean", dryRunProp["type"], "boolean type should map to boolean")
}

// TestAddRepoParameterIfNeededCreatesIssueWithRepos tests that repo param is added for create_issue
// when allowed_repos is configured.
func TestAddRepoParameterIfNeededCreatesIssueWithRepos(t *testing.T) {
	tool := map[string]any{
		"name": "create_issue",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{"type": "string"},
			},
		},
	}

	safeOutputs := &SafeOutputsConfig{
		CreateIssues: &CreateIssuesConfig{
			AllowedRepos:   []string{"org/repo1", "org/repo2"},
			TargetRepoSlug: "org/repo1",
		},
	}

	addRepoParameterIfNeeded(tool, "create_issue", safeOutputs)

	inputSchema := tool["inputSchema"].(map[string]any)
	properties := inputSchema["properties"].(map[string]any)

	repoProp, ok := properties["repo"].(map[string]any)
	require.True(t, ok, "repo property should be added")
	assert.Equal(t, "string", repoProp["type"], "repo type should be string")
	assert.Contains(t, repoProp["description"].(string), "org/repo1", "description should include default repo")
}

// TestAddRepoParameterIfNeededNoAllowedRepos tests that repo param is NOT added when no allowed_repos.
func TestAddRepoParameterIfNeededNoAllowedRepos(t *testing.T) {
	tool := map[string]any{
		"name": "create_issue",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{"type": "string"},
			},
		},
	}

	safeOutputs := &SafeOutputsConfig{
		CreateIssues: &CreateIssuesConfig{},
	}

	addRepoParameterIfNeeded(tool, "create_issue", safeOutputs)

	inputSchema := tool["inputSchema"].(map[string]any)
	properties := inputSchema["properties"].(map[string]any)

	_, hasRepo := properties["repo"]
	assert.False(t, hasRepo, "repo property should NOT be added when no allowed_repos")
}

// TestAddRepoParameterIfNeededWildcardTargetRepo tests that repo param is added for update_issue
// when target-repo is "*" (wildcard), even without allowed-repos.
func TestAddRepoParameterIfNeededWildcardTargetRepo(t *testing.T) {
	tool := map[string]any{
		"name": "update_issue",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{"type": "string"},
			},
		},
	}

	safeOutputs := &SafeOutputsConfig{
		UpdateIssues: &UpdateIssuesConfig{
			UpdateEntityConfig: UpdateEntityConfig{
				SafeOutputTargetConfig: SafeOutputTargetConfig{
					TargetRepoSlug: "*",
				},
			},
		},
	}

	addRepoParameterIfNeeded(tool, "update_issue", safeOutputs)

	inputSchema := tool["inputSchema"].(map[string]any)
	properties := inputSchema["properties"].(map[string]any)

	repoProp, ok := properties["repo"].(map[string]any)
	require.True(t, ok, "repo property should be added when target-repo is wildcard")
	assert.Equal(t, "string", repoProp["type"], "repo type should be string")
	assert.Contains(t, repoProp["description"].(string), "Any repository can be targeted", "description should indicate any repo allowed")
}

// TestAddRepoParameterIfNeededSpecificTargetRepoNoAllowedRepos tests that repo param is NOT added
// for update_issue when target-repo is a specific repo but allowed-repos is empty.
// The handler automatically routes to the configured target-repo, so the agent doesn't need to
// specify repo in the tool schema.
func TestAddRepoParameterIfNeededSpecificTargetRepoNoAllowedRepos(t *testing.T) {
	tool := map[string]any{
		"name": "update_issue",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{"type": "string"},
			},
		},
	}

	safeOutputs := &SafeOutputsConfig{
		UpdateIssues: &UpdateIssuesConfig{
			UpdateEntityConfig: UpdateEntityConfig{
				SafeOutputTargetConfig: SafeOutputTargetConfig{
					TargetRepoSlug: "org/target-repo",
				},
			},
		},
	}

	addRepoParameterIfNeeded(tool, "update_issue", safeOutputs)

	inputSchema := tool["inputSchema"].(map[string]any)
	properties := inputSchema["properties"].(map[string]any)

	_, hasRepo := properties["repo"]
	assert.False(t, hasRepo, "repo parameter should NOT be added when target-repo is specific and no allowed-repos")
}

func TestAddRepoParameterIfNeededClosePullRequestWithAllowedRepos(t *testing.T) {
	tool := map[string]any{
		"name": "close_pull_request",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"body": map[string]any{"type": "string"},
			},
		},
	}

	safeOutputs := &SafeOutputsConfig{
		ClosePullRequests: &ClosePullRequestsConfig{
			SafeOutputTargetConfig: SafeOutputTargetConfig{
				TargetRepoSlug: "org/default-repo",
				AllowedRepos:   []string{"org/other-repo"},
			},
		},
	}

	addRepoParameterIfNeeded(tool, "close_pull_request", safeOutputs)

	inputSchema := tool["inputSchema"].(map[string]any)
	properties := inputSchema["properties"].(map[string]any)

	repoProp, ok := properties["repo"].(map[string]any)
	require.True(t, ok, "repo property should be added")
	assert.Equal(t, "string", repoProp["type"], "repo type should be string")
	assert.Contains(t, repoProp["description"].(string), "org/default-repo", "description should include default repo")
}

func TestRepoTargetAccessorsMatchHandlerMetadata(t *testing.T) {
	accessors := getRepoTargetAccessors()
	for _, handler := range safeOutputHandlers {
		if !isRepoTargetHandler(handler) {
			continue
		}

		t.Run(handler.ToolName, func(t *testing.T) {
			config := &SafeOutputsConfig{}
			output := reflect.ValueOf(config).Elem().FieldByName(handler.StructField)
			require.True(t, output.IsValid(), "handler struct field must exist")

			output.Set(reflect.New(output.Type().Elem()))
			output = output.Elem()
			output.FieldByName("AllowedRepos").Set(reflect.ValueOf([]string{handler.ToolName + "/allowed"}))
			output.FieldByName("TargetRepoSlug").SetString(handler.ToolName + "/target")

			accessor, ok := accessors[handler.ToolName]
			require.True(t, ok, "repo target handler must have an accessor")
			targetConfig := accessor(config)
			require.NotNil(t, targetConfig)
			assert.Equal(t, []string{handler.ToolName + "/allowed"}, targetConfig.allowedRepos)
			assert.Equal(t, handler.ToolName+"/target", targetConfig.targetRepoSlug)
		})
	}
}

func TestParseUpdateIssuesConfigWithWildcardTargetRepo(t *testing.T) {
	compiler := &Compiler{}
	outputMap := map[string]any{
		"update-issue": map[string]any{
			"target-repo": "*",
		},
	}

	result := compiler.parseUpdateIssuesConfig(outputMap)
	require.NotNil(t, result, "parseUpdateIssuesConfig should return non-nil for wildcard target-repo")
	assert.Equal(t, "*", result.TargetRepoSlug, "TargetRepoSlug should be '*'")
}

// TestGenerateDispatchWorkflowToolBasic tests basic dispatch workflow tool generation.
func TestGenerateDispatchWorkflowToolBasic(t *testing.T) {
	workflowInputs := map[string]any{
		"environment": map[string]any{
			"description": "Target environment",
			"type":        "choice",
			"options":     []any{"staging", "production"},
			"required":    true,
		},
	}

	tool := generateDispatchWorkflowTool("deploy-app", workflowInputs, nil, false)

	assert.Equal(t, "deploy_app", tool["name"], "Tool name should be normalized")
	assert.Equal(t, "deploy-app", tool["_workflow_name"], "Internal workflow name should be preserved")
	assert.Contains(t, tool["description"].(string), "deploy-app", "Description should mention workflow name")

	inputSchema, ok := tool["inputSchema"].(map[string]any)
	require.True(t, ok, "inputSchema should be present")

	properties, ok := inputSchema["properties"].(map[string]any)
	require.True(t, ok, "properties should be present")

	envProp, ok := properties["environment"].(map[string]any)
	require.True(t, ok, "environment property should exist")
	assert.Equal(t, "string", envProp["type"], "choice maps to string")
	assert.Equal(t, []any{"staging", "production"}, envProp["enum"], "enum values should match")
}

// TestGenerateDispatchWorkflowToolEmptyInputs tests dispatch workflow tool with no inputs.
func TestGenerateDispatchWorkflowToolEmptyInputs(t *testing.T) {
	tool := generateDispatchWorkflowTool("simple-workflow", make(map[string]any), nil, false)

	assert.Equal(t, "simple_workflow", tool["name"], "Name should be normalized")

	inputSchema := tool["inputSchema"].(map[string]any)
	properties := inputSchema["properties"].(map[string]any)
	assert.Empty(t, properties, "Properties should be empty for workflow with no inputs")

	_, hasRequired := inputSchema["required"]
	assert.False(t, hasRequired, "required field should not be present when no required inputs")
}

// TestGenerateDispatchWorkflowToolRequiredSorted tests that the required array is always sorted.
// This ensures idempotent output regardless of map iteration order.
func TestGenerateDispatchWorkflowToolRequiredSorted(t *testing.T) {
	workflowInputs := map[string]any{
		"tracker_issue": map[string]any{
			"description": "Dashboard issue number to reference",
			"type":        "string",
			"required":    true,
		},
		"flag_key": map[string]any{
			"description": "The LaunchDarkly flag key to clean up",
			"type":        "string",
			"required":    true,
		},
		"optional_param": map[string]any{
			"description": "An optional parameter",
			"type":        "string",
			"required":    false,
		},
	}

	// Run multiple times to catch non-determinism from map iteration
	for i := range 10 {
		tool := generateDispatchWorkflowTool("cleanup-worker", workflowInputs, nil, false)

		inputSchema, ok := tool["inputSchema"].(map[string]any)
		require.True(t, ok, "inputSchema should be present (iteration %d)", i)

		required, ok := inputSchema["required"].([]string)
		require.True(t, ok, "required should be []string (iteration %d)", i)

		assert.Equal(t, []string{"flag_key", "tracker_issue"}, required,
			"required array should be sorted alphabetically (iteration %d)", i)
	}
}

// TestGenerateDispatchWorkflowToolWithAllowedRefs tests that a 'ref' parameter is injected
// into the tool schema when allowed-refs is configured. This ensures the agent can supply
// a target ref that is validated against the configured glob patterns by the runtime handler.
func TestGenerateDispatchWorkflowToolWithAllowedRefs(t *testing.T) {
	workflowInputs := map[string]any{
		"model": map[string]any{
			"description": "Model to run",
			"type":        "string",
			"required":    true,
		},
	}
	allowedRefs := []string{"silencer/*", "refs/heads/main"}

	tool := generateDispatchWorkflowTool("t3000-unit-tests", workflowInputs, allowedRefs, false)

	assert.Equal(t, "t3000_unit_tests", tool["name"], "Tool name should be normalized")

	inputSchema, ok := tool["inputSchema"].(map[string]any)
	require.True(t, ok, "inputSchema should be present")

	properties, ok := inputSchema["properties"].(map[string]any)
	require.True(t, ok, "properties should be present")

	// ref property should be injected
	refProp, ok := properties["ref"].(map[string]any)
	require.True(t, ok, "ref property should exist when allowed-refs is configured")
	assert.Equal(t, "string", refProp["type"], "ref property should be a string")
	assert.Contains(t, refProp["description"].(string), "silencer/*", "ref description should mention allowed patterns")
	assert.Contains(t, refProp["description"].(string), "refs/heads/main", "ref description should mention all allowed patterns")
	assert.Contains(t, refProp["description"].(string), "pull request head", "ref description should explain the PR comment fallback")

	// ref should not be in required (it is optional)
	required, hasRequired := inputSchema["required"].([]string)
	if hasRequired {
		assert.NotContains(t, required, "ref", "ref should not be required")
	}

	// description should mention the allowed patterns
	desc := tool["description"].(string)
	assert.Contains(t, desc, "silencer/*", "tool description should mention allowed ref patterns")
}

// TestGenerateDispatchWorkflowToolNoRefWithoutAllowedRefs tests that no 'ref' property
// is added when allowed-refs is not configured (nil or empty).
func TestGenerateDispatchWorkflowToolNoRefWithoutAllowedRefs(t *testing.T) {
	workflowInputs := map[string]any{
		"platform": map[string]any{
			"description": "Target platform",
			"type":        "string",
			"required":    true,
		},
	}

	for _, allowedRefs := range [][]string{nil, {}} {
		tool := generateDispatchWorkflowTool("build-workflow", workflowInputs, allowedRefs, false)

		inputSchema, ok := tool["inputSchema"].(map[string]any)
		require.True(t, ok, "inputSchema should be present")

		properties, ok := inputSchema["properties"].(map[string]any)
		require.True(t, ok, "properties should be present")

		_, hasRef := properties["ref"]
		assert.False(t, hasRef, "ref property should not be present when allowed-refs is not configured")

		required, ok := inputSchema["required"].([]string)
		require.True(t, ok, "required should be present for required workflow input")
		assert.Equal(t, []string{"platform"}, required, "required should only include workflow inputs, not ref")
	}
}

// TestComputeRequiredFieldRemovalsCloseDiscussion verifies that allow-body: false for
// close-discussion produces a required field removal for the body field.
func TestComputeRequiredFieldRemovalsCloseDiscussion(t *testing.T) {
	f := false
	removals := computeRequiredFieldRemovals(&SafeOutputsConfig{
		CloseDiscussions: &CloseDiscussionsConfig{
			AllowBody: &f,
		},
	})

	require.Contains(t, removals, "close_discussion", "expected close_discussion key")
	assert.Equal(t, []string{"body"}, removals["close_discussion"])
	assert.NotContains(t, removals, "close_issue", "close_issue should not be affected")
}

// TestComputeRequiredFieldRemovalsCloseIssue verifies that allow-body: false for
// close-issue produces a required field removal for the body field.
func TestComputeRequiredFieldRemovalsCloseIssue(t *testing.T) {
	f := false
	removals := computeRequiredFieldRemovals(&SafeOutputsConfig{
		CloseIssues: &CloseIssuesConfig{
			AllowBody: &f,
		},
	})

	require.Contains(t, removals, "close_issue", "expected close_issue key")
	assert.Equal(t, []string{"body"}, removals["close_issue"])
	assert.NotContains(t, removals, "close_discussion", "close_discussion should not be affected")
}

// TestComputeRequiredFieldRemovalsAllowBodyTrue verifies that allow-body: true
// does NOT produce any required field removals.
func TestComputeRequiredFieldRemovalsAllowBodyTrue(t *testing.T) {
	tr := true
	removals := computeRequiredFieldRemovals(&SafeOutputsConfig{
		CloseDiscussions: &CloseDiscussionsConfig{
			AllowBody: &tr,
		},
		CloseIssues: &CloseIssuesConfig{
			AllowBody: &tr,
		},
	})

	assert.Empty(t, removals, "no removals expected when allow-body is true")
}

// TestComputeRequiredFieldRemovalsNilConfig verifies that a nil config returns empty removals.
func TestComputeRequiredFieldRemovalsNilConfig(t *testing.T) {
	removals := computeRequiredFieldRemovals(nil)
	assert.Empty(t, removals)
}

// TestComputeRequiredFieldRemovals_BothFalse verifies that allow-body: false on both tools
// produces removals for both.
func TestComputeRequiredFieldRemovals_BothFalse(t *testing.T) {
	f := false
	removals := computeRequiredFieldRemovals(&SafeOutputsConfig{
		CloseDiscussions: &CloseDiscussionsConfig{AllowBody: &f},
		CloseIssues:      &CloseIssuesConfig{AllowBody: &f},
	})

	assert.Equal(t, []string{"body"}, removals["close_discussion"])
	assert.Equal(t, []string{"body"}, removals["close_issue"])
}

func TestComputeRequiredFieldAdditionsRequireTemporaryID(t *testing.T) {
	additions := computeRequiredFieldAdditions(&SafeOutputsConfig{
		CreateIssues:       &CreateIssuesConfig{RequireTemporaryID: true},
		CreatePullRequests: &CreatePullRequestsConfig{RequireTemporaryID: true},
	})

	require.Contains(t, additions, "create_issue")
	require.Contains(t, additions, "create_pull_request")
	assert.Equal(t, []string{"temporary_id"}, additions["create_issue"])
	assert.Equal(t, []string{"temporary_id"}, additions["create_pull_request"])
}

func TestComputeRequiredFieldAdditionsDisabledByDefault(t *testing.T) {
	additions := computeRequiredFieldAdditions(&SafeOutputsConfig{
		CreateIssues:       &CreateIssuesConfig{},
		CreatePullRequests: &CreatePullRequestsConfig{},
	})
	assert.Empty(t, additions)
}

func TestComputeRequiredFieldAdditionsSubmitPRReviewEventRequiredWhenCommentDisallowed(t *testing.T) {
	additions := computeRequiredFieldAdditions(&SafeOutputsConfig{
		SubmitPullRequestReview: &SubmitPullRequestReviewConfig{
			AllowedEvents: []string{"APPROVE"},
		},
	})

	assert.Equal(t, []string{"event"}, additions["submit_pull_request_review"])
}

func TestComputeRequiredFieldAdditionsSubmitPRReviewEventOptionalWhenCommentAllowed(t *testing.T) {
	additions := computeRequiredFieldAdditions(&SafeOutputsConfig{
		SubmitPullRequestReview: &SubmitPullRequestReviewConfig{
			AllowedEvents: []string{"COMMENT", "REQUEST_CHANGES"},
		},
	})

	assert.NotContains(t, additions, "submit_pull_request_review")
}

func TestComputeRequiredFieldAdditionsIssueIntentDefaultDisabled(t *testing.T) {
	additions := computeRequiredFieldAdditions(&SafeOutputsConfig{
		CloseIssues:   &CloseIssuesConfig{},
		AssignToUser:  &AssignToUserConfig{},
		AssignToAgent: &AssignToAgentConfig{},
	})

	assert.NotContains(t, additions, "close_issue")
	assert.NotContains(t, additions, "assign_to_user")
	assert.NotContains(t, additions, "assign_to_agent")
}

func TestComputeRequiredFieldAdditionsIssueIntentOptIn(t *testing.T) {
	enabled := true
	additions := computeRequiredFieldAdditions(&SafeOutputsConfig{
		CloseIssues:   &CloseIssuesConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{IssueIntent: &enabled}},
		AssignToUser:  &AssignToUserConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{IssueIntent: &enabled}},
		AssignToAgent: &AssignToAgentConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{IssueIntent: &enabled}},
	})

	assert.Equal(t, []string{"rationale", "confidence"}, additions["close_issue"])
	assert.Equal(t, []string{"rationale", "confidence"}, additions["assign_to_user"])
	assert.Equal(t, []string{"rationale", "confidence"}, additions["assign_to_agent"])
}

// TestComputePropertyInjectionsOmittedStateReason verifies that when close-issue has no
// state-reason configured, state_reason is injected with all three supported values.
func TestComputePropertyInjectionsOmittedStateReason(t *testing.T) {
	injections := computePropertyInjections(&SafeOutputsConfig{
		CloseIssues: &CloseIssuesConfig{},
	})

	require.Contains(t, injections, "close_issue")
	require.Contains(t, injections["close_issue"], "state_reason")
	prop, ok := injections["close_issue"]["state_reason"].(map[string]any)
	require.True(t, ok, "state_reason should be a property map")
	assert.Equal(t, "string", prop["type"])
	assert.Equal(t, []string{"completed", "not_planned", "duplicate"}, prop["enum"])
}

// TestComputePropertyInjectionsListStateReason verifies that a list state-reason injects
// only the configured values into the state_reason enum.
func TestComputePropertyInjectionsListStateReason(t *testing.T) {
	injections := computePropertyInjections(&SafeOutputsConfig{
		CloseIssues: &CloseIssuesConfig{
			AllowedStateReason: []string{"not_planned", "duplicate"},
		},
	})

	require.Contains(t, injections, "close_issue")
	prop, ok := injections["close_issue"]["state_reason"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []string{"not_planned", "duplicate"}, prop["enum"])
}

// TestComputePropertyInjectionsScalarStateReason verifies that a scalar state-reason does
// NOT inject state_reason (the agent cannot choose).
func TestComputePropertyInjectionsScalarStateReason(t *testing.T) {
	injections := computePropertyInjections(&SafeOutputsConfig{
		CloseIssues: &CloseIssuesConfig{
			StateReason: "not_planned",
		},
	})

	assert.NotContains(t, injections, "close_issue", "scalar state-reason should not inject state_reason into tool schema")
}

// TestComputePropertyInjectionsNilConfig verifies that nil config returns empty map.
func TestComputePropertyInjectionsNilConfig(t *testing.T) {
	injections := computePropertyInjections(nil)
	assert.Empty(t, injections)
}

// TestComputePropertyInjectionsNilCloseIssues verifies that nil close issues returns empty map.
func TestComputePropertyInjectionsNilCloseIssues(t *testing.T) {
	injections := computePropertyInjections(&SafeOutputsConfig{})
	assert.Empty(t, injections)
}

// TestComputePropertyInjectionsNilSubmitPRReview verifies that nil submit-pull-request-review
// does not add submit_pull_request_review property injections.
func TestComputePropertyInjectionsNilSubmitPRReview(t *testing.T) {
	injections := computePropertyInjections(&SafeOutputsConfig{
		SubmitPullRequestReview: nil,
	})
	assert.NotContains(t, injections, "submit_pull_request_review")
}

// TestComputePropertyInjectionsAllowedEventsSubmitPRReview verifies that a configured
// allowed-events list narrows the submit_pull_request_review event enum in the tool schema.
func TestComputePropertyInjectionsAllowedEventsSubmitPRReview(t *testing.T) {
	injections := computePropertyInjections(&SafeOutputsConfig{
		SubmitPullRequestReview: &SubmitPullRequestReviewConfig{
			AllowedEvents: []string{"COMMENT"},
		},
	})

	require.Contains(t, injections, "submit_pull_request_review")
	prop, ok := injections["submit_pull_request_review"]["event"].(map[string]any)
	require.True(t, ok, "event should be a property map")
	assert.Equal(t, []string{"COMMENT"}, prop["enum"])
}

// TestComputePropertyInjectionsAllowedEventsMultipleSubmitPRReview verifies multiple allowed
// events are all present in the narrowed enum.
func TestComputePropertyInjectionsAllowedEventsMultipleSubmitPRReview(t *testing.T) {
	injections := computePropertyInjections(&SafeOutputsConfig{
		SubmitPullRequestReview: &SubmitPullRequestReviewConfig{
			AllowedEvents: []string{"COMMENT", "REQUEST_CHANGES"},
		},
	})

	prop, ok := injections["submit_pull_request_review"]["event"].(map[string]any)
	require.True(t, ok, "event should be a property map")
	assert.Equal(t, []string{"COMMENT", "REQUEST_CHANGES"}, prop["enum"])
}

// TestComputePropertyInjectionsNoAllowedEventsSubmitPRReview verifies that no injection
// happens when allowed-events is not configured, so the static schema's full enum applies.
func TestComputePropertyInjectionsNoAllowedEventsSubmitPRReview(t *testing.T) {
	injections := computePropertyInjections(&SafeOutputsConfig{
		SubmitPullRequestReview: &SubmitPullRequestReviewConfig{},
	})

	assert.NotContains(t, injections, "submit_pull_request_review", "no allowed-events should not inject an event enum")
}

// TestPreprocessStateReasonListSlice verifies that a []any slice is converted to allowed-state-reason.
func TestPreprocessStateReasonListSlice(t *testing.T) {
	configData := map[string]any{
		"state-reason": []any{"not_planned", "duplicate"},
	}
	result := preprocessStateReasonList(configData, configData["state-reason"], nil)
	assert.True(t, result)
	assert.NotContains(t, configData, "state-reason", "state-reason key should be removed")
	assert.Equal(t, []string{"not_planned", "duplicate"}, configData["allowed-state-reason"])
}

// TestPreprocessStateReasonListStringSlice verifies that a []string slice is converted to allowed-state-reason.
func TestPreprocessStateReasonListStringSlice(t *testing.T) {
	configData := map[string]any{
		"state-reason": []string{"completed", "not_planned"},
	}
	result := preprocessStateReasonList(configData, configData["state-reason"], nil)
	assert.True(t, result)
	assert.NotContains(t, configData, "state-reason")
	assert.Equal(t, []string{"completed", "not_planned"}, configData["allowed-state-reason"])
}

// TestPreprocessStateReasonListScalarNotConverted verifies that a scalar string is not touched.
func TestPreprocessStateReasonListScalarNotConverted(t *testing.T) {
	configData := map[string]any{
		"state-reason": "not_planned",
	}
	result := preprocessStateReasonList(configData, configData["state-reason"], nil)
	assert.False(t, result)
	assert.Equal(t, "not_planned", configData["state-reason"], "scalar should remain unchanged")
	assert.NotContains(t, configData, "state-reasons")
}

// TestPreprocessStateReasonListEmptyAnySlice verifies that an empty []any leaves configData unchanged.
// An empty list must not silently escalate to unrestricted (omitted) mode.
func TestPreprocessStateReasonListEmptyAnySlice(t *testing.T) {
	configData := map[string]any{
		"state-reason": []any{},
	}
	result := preprocessStateReasonList(configData, configData["state-reason"], nil)
	assert.False(t, result, "empty []any slice should return false")
	assert.Contains(t, configData, "state-reason", "state-reason key must be preserved")
	assert.NotContains(t, configData, "allowed-state-reason")
}

// TestPreprocessStateReasonListEmptyStringSlice verifies that an empty []string leaves configData unchanged.
func TestPreprocessStateReasonListEmptyStringSlice(t *testing.T) {
	configData := map[string]any{
		"state-reason": []string{},
	}
	result := preprocessStateReasonList(configData, configData["state-reason"], nil)
	assert.False(t, result, "empty []string slice should return false")
	assert.Contains(t, configData, "state-reason", "state-reason key must be preserved")
	assert.NotContains(t, configData, "allowed-state-reason")
}

// TestPreprocessStateReasonListAllNonStringElements verifies that a []any with no string elements
// leaves configData unchanged, preventing a silent escalation to unrestricted mode.
func TestPreprocessStateReasonListAllNonStringElements(t *testing.T) {
	configData := map[string]any{
		"state-reason": []any{42, true, nil},
	}
	result := preprocessStateReasonList(configData, configData["state-reason"], nil)
	assert.False(t, result, "all-non-string []any should return false")
	assert.Contains(t, configData, "state-reason", "state-reason key must be preserved")
	assert.NotContains(t, configData, "allowed-state-reason")
}

// TestComputePropertyInjectionsFiltersInvalidStateReasons verifies that invalid values in
// AllowedStateReason are silently dropped so only supported API values reach the tool schema.
func TestComputePropertyInjectionsFiltersInvalidStateReasons(t *testing.T) {
	injections := computePropertyInjections(&SafeOutputsConfig{
		CloseIssues: &CloseIssuesConfig{
			AllowedStateReason: []string{"not_planned", "wontfix", "done"},
		},
	})

	require.Contains(t, injections, "close_issue")
	prop, ok := injections["close_issue"]["state_reason"].(map[string]any)
	require.True(t, ok)
	// "wontfix" and "done" are invalid; only "not_planned" survives.
	assert.Equal(t, []string{"not_planned"}, prop["enum"])
}

// TestComputePropertyInjectionsAllInvalidFallsBackToFullSet verifies that when ALL configured
// values are invalid, computePropertyInjections falls back to the full supported set so the
// tool schema remains functional.
func TestComputePropertyInjectionsAllInvalidFallsBackToFullSet(t *testing.T) {
	injections := computePropertyInjections(&SafeOutputsConfig{
		CloseIssues: &CloseIssuesConfig{
			AllowedStateReason: []string{"done", "wontfix"},
		},
	})

	require.Contains(t, injections, "close_issue")
	prop, ok := injections["close_issue"]["state_reason"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, closeIssueStateReasonValues, prop["enum"])
}

func TestComputePropertyInjectionsAddsDataSchemaForBodyTypes(t *testing.T) {
	injections := computePropertyInjections(&SafeOutputsConfig{
		DataEnabled: true,
		NormalizedDataSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"verdict": map[string]any{"type": "string"},
			},
			"additionalProperties": false,
		},
	})

	for _, typeName := range dataSchemaBodyTypes {
		require.Contains(t, injections, typeName)
		prop, ok := injections[typeName]["data"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "object", prop["type"])
	}
}

func TestComputePropertyInjectionsAddsGenericDataForBodyTypes(t *testing.T) {
	injections := computePropertyInjections(&SafeOutputsConfig{DataEnabled: true})

	for _, typeName := range dataSchemaBodyTypes {
		require.Contains(t, injections, typeName)
		prop, ok := injections[typeName]["data"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "#/0/inputSchema/$defs/structured_data", prop["$ref"])
	}
}

// TestGenerateDynamicTools_DispatchWorkflow_YAMLExtension verifies that generateDynamicTools
// correctly sets the .yaml extension in WorkflowFiles when the target workflow uses a .yaml file.
func TestGenerateDynamicTools_DispatchWorkflow_YAMLExtension(t *testing.T) {
	tmpDir := t.TempDir()
	awDir := filepath.Join(tmpDir, ".github", "aw")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(awDir, 0755))
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Create a .yaml workflow (not .yml) with a workflow_dispatch trigger
	yamlWorkflow := "name: Deploy\non:\n  workflow_dispatch:\n    inputs:\n      env:\n        description: Environment\n        type: string\n"
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "deploy.yaml"), []byte(yamlWorkflow), 0644))

	markdownPath := filepath.Join(awDir, "gateway.md")

	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			DispatchWorkflow: &DispatchWorkflowConfig{
				Workflows: []string{"deploy"},
			},
		},
	}

	tools, err := generateDynamicTools(data, markdownPath)
	require.NoError(t, err)

	// WorkflowFiles must use .yaml, not .yml
	require.NotNil(t, data.SafeOutputs.DispatchWorkflow.WorkflowFiles)
	assert.Equal(t, ".yaml", data.SafeOutputs.DispatchWorkflow.WorkflowFiles["deploy"],
		"WorkflowFiles extension must be .yaml when only .yaml exists")

	// A tool must have been generated for the workflow
	require.Len(t, tools, 1, "one tool expected for the dispatch workflow")
	assert.Equal(t, "deploy", tools[0]["_workflow_name"])
}

// TestGenerateDynamicTools_CallWorkflow_YAMLExtension verifies that generateDynamicTools
// correctly sets the .yaml extension in the WorkflowFiles relative path for call_workflow.
func TestGenerateDynamicTools_CallWorkflow_YAMLExtension(t *testing.T) {
	tmpDir := t.TempDir()
	awDir := filepath.Join(tmpDir, ".github", "aw")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(awDir, 0755))
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Create a .yaml reusable workflow (workflow_call trigger)
	yamlWorkflow := "name: Worker\non:\n  workflow_call:\n    inputs:\n      task:\n        description: Task to run\n        type: string\n"
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "worker.yaml"), []byte(yamlWorkflow), 0644))

	markdownPath := filepath.Join(awDir, "gateway.md")

	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CallWorkflow: &CallWorkflowConfig{
				Workflows: []string{"worker"},
			},
		},
	}

	tools, err := generateDynamicTools(data, markdownPath)
	require.NoError(t, err)

	// WorkflowFiles relative path must use .yaml, not .yml
	require.NotNil(t, data.SafeOutputs.CallWorkflow.WorkflowFiles)
	assert.Equal(t, "./.github/workflows/worker.yaml", data.SafeOutputs.CallWorkflow.WorkflowFiles["worker"],
		"WorkflowFiles path must end in .yaml when only .yaml exists")

	// A tool must have been generated for the call workflow
	require.Len(t, tools, 1, "one tool expected for the call workflow")
	assert.Equal(t, "worker", tools[0]["_call_workflow_name"])
}
