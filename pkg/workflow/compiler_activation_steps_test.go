//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newActivationStepsTestCompiler(version string) *Compiler {
	if version == "" {
		version = "dev"
	}
	compiler := NewCompiler(WithVersion(version))
	compiler.SetActionMode(ActionModeDev)
	return compiler
}

func newActivationStepsTestContext(data *WorkflowData) *activationJobBuildContext {
	if data == nil {
		data = &WorkflowData{}
	}
	return &activationJobBuildContext{
		data:         data,
		lockFilename: "test.lock.yml",
		outputs:      map[string]string{},
	}
}

func TestActivationStepsAddReactionStep(t *testing.T) {
	compiler := newActivationStepsTestCompiler("")

	t.Run("adds reaction step", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{
			AIReaction: "eyes",
		})
		ctx.hasReaction = true
		ctx.reactionIssues = true

		compiler.addActivationReactionStep(ctx)

		steps := strings.Join(ctx.steps, "")
		assert.Contains(t, steps, "Add eyes reaction for immediate feedback")
		assert.Contains(t, steps, "id: react")
		assert.Contains(t, steps, "uses: actions/github-script")
		assert.Contains(t, steps, "GH_AW_REACTION: \"eyes\"")
		assert.Contains(t, steps, "github-token: ${{ secrets.GITHUB_TOKEN }}")
		assert.Contains(t, steps, "add_reaction.cjs")
	})

	t.Run("skips when reaction is disabled", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{AIReaction: "eyes"})

		compiler.addActivationReactionStep(ctx)

		assert.Empty(t, ctx.steps)
	})
}

func TestActivationStepsAddSecretValidationStep(t *testing.T) {
	compiler := newActivationStepsTestCompiler("")
	engine, err := compiler.getAgenticEngine("copilot")
	require.NoError(t, err)

	ctx := newActivationStepsTestContext(&WorkflowData{AI: "copilot"})
	ctx.engine = engine

	compiler.addActivationSecretValidationStep(ctx)

	steps := strings.Join(ctx.steps, "")
	assert.Contains(t, steps, "id: validate-secret")
	assert.Contains(t, steps, "validate_multi_secret.sh")
	assert.Equal(t, "${{ steps.validate-secret.outputs.verification_result }}", ctx.outputs["secret_verification_result"])
}

func TestActivationStepsAddSafeOutputSecretValidationSteps(t *testing.T) {
	compiler := newActivationStepsTestCompiler("")
	engine, err := compiler.getAgenticEngine("copilot")
	require.NoError(t, err)

	ctx := newActivationStepsTestContext(&WorkflowData{
		AI: "copilot",
		SafeOutputs: &SafeOutputsConfig{
			JiraCreateIssue:   &JiraSafeOutputConfig{},
			LinearCreateIssue: &LinearCreateIssueConfig{},
			CreateWorkItems:   &CreateWorkItemConfig{},
		},
	})
	ctx.engine = engine

	compiler.addActivationSecretValidationStep(ctx)

	steps := strings.Join(ctx.steps, "")
	assert.Contains(t, steps, "JIRA_USER_EMAIL: ${{ secrets.JIRA_USER_EMAIL }}")
	assert.Contains(t, steps, "JIRA_API_TOKEN: ${{ secrets.JIRA_API_TOKEN }}")
	assert.Contains(t, steps, "LINEAR_API_KEY: ${{ secrets.LINEAR_API_KEY }}")
	assert.Contains(t, steps, "AZURE_DEVOPS_EXT_PAT: ${{ secrets.AZURE_DEVOPS_EXT_PAT }}")
	assert.Contains(t, steps, "'Jira safe outputs'")
	assert.Contains(t, steps, "'Linear safe outputs'")
	assert.Contains(t, steps, "'Azure DevOps safe outputs'")
	assert.Contains(t, ctx.outputs["secret_verification_result"], "steps.validate-safe-output-secret-4.outcome == 'failure'")
}

func TestActivationStepsAddOAuthTokenCheckStep(t *testing.T) {
	compiler := newActivationStepsTestCompiler("")

	t.Run("uses default copilot token secret", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{})

		compiler.addActivationOAuthTokenCheckStep(ctx)

		steps := strings.Join(ctx.steps, "")
		assert.Contains(t, steps, "Check for OAuth tokens")
		assert.Contains(t, steps, "id: check-oauth-tokens")
		assert.Contains(t, steps, "check_oauth_tokens.sh")
		assert.Contains(t, steps, constants.CopilotGitHubToken+": ${{ secrets."+constants.CopilotGitHubToken+" }}")
		assert.Contains(t, steps, constants.EnvVarGitHubToken+": ${{ secrets."+constants.EnvVarGitHubToken+" }}")
		assert.Contains(t, steps, constants.EnvVarGitHubMCPServerToken+": ${{ secrets."+constants.EnvVarGitHubMCPServerToken+" }}")
	})

	t.Run("uses engine env override for copilot token", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{
			EngineConfig: &EngineConfig{
				Env: map[string]string{
					constants.CopilotGitHubToken: "${{ secrets.CUSTOM_COPILOT_TOKEN }}",
				},
			},
		})

		compiler.addActivationOAuthTokenCheckStep(ctx)

		assert.Contains(t, strings.Join(ctx.steps, ""), constants.CopilotGitHubToken+": ${{ secrets.CUSTOM_COPILOT_TOKEN }}")
	})

	t.Run("omits COPILOT_GITHUB_TOKEN when copilot-requests write permission is set", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{
			Permissions: "permissions:\n  copilot-requests: write\n",
		})

		compiler.addActivationOAuthTokenCheckStep(ctx)

		steps := strings.Join(ctx.steps, "")
		assert.Contains(t, steps, "Check for OAuth tokens")
		assert.NotContains(t, steps, constants.CopilotGitHubToken)
		assert.Contains(t, steps, constants.EnvVarGitHubToken+": ${{ secrets."+constants.EnvVarGitHubToken+" }}")
		assert.Contains(t, steps, constants.EnvVarGitHubMCPServerToken+": ${{ secrets."+constants.EnvVarGitHubMCPServerToken+" }}")
	})
}

func TestActivationStepsAddCrossRepoGuidanceStep(t *testing.T) {
	compiler := newActivationStepsTestCompiler("")

	t.Run("adds workflow_call guidance", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{
			On: "\"on\":\n  workflow_call:\n",
		})

		compiler.addActivationCrossRepoGuidanceStep(ctx)

		steps := strings.Join(ctx.steps, "")
		assert.Contains(t, steps, "Print cross-repo setup guidance")
		assert.Contains(t, steps, "resolve-host-repo.outputs.target_repo != github.repository")
		assert.Contains(t, steps, "cross-repo workflow_call")
	})

	t.Run("skips for inlined imports", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{
			On:             "\"on\":\n  workflow_call:\n",
			InlinedImports: true,
		})

		compiler.addActivationCrossRepoGuidanceStep(ctx)

		assert.Empty(t, ctx.steps)
	})
}

func TestActivationStepsAddRepositoryAndOutputSteps(t *testing.T) {
	t.Run("adds repository and output steps", func(t *testing.T) {
		originalIsRelease := isReleaseBuild
		isReleaseBuild = true
		t.Cleanup(func() { isReleaseBuild = originalIsRelease })

		compiler := newActivationStepsTestCompiler("v1.2.3")
		ctx := newActivationBuildContext(&WorkflowData{}, false, "", "test.lock.yml")

		err := compiler.addActivationRepositoryAndOutputSteps(ctx)

		require.NoError(t, err)
		steps := strings.Join(ctx.steps, "")
		assert.Contains(t, steps, "Checkout .github and .agents folders")
		assert.Contains(t, steps, "Check workflow lock file")
		assert.Contains(t, steps, "Check compile-agentic version")
		assert.Equal(t, `""`, ctx.outputs["comment_id"])
		assert.Equal(t, `""`, ctx.outputs["comment_repo"])
	})

	t.Run("returns text output errors", func(t *testing.T) {
		compiler := newActivationStepsTestCompiler("")
		ctx := newActivationBuildContext(&WorkflowData{
			NeedsTextOutput: true,
			Model:           "/bad-provider",
			EngineConfig: &EngineConfig{
				ID: "pi",
			},
		}, false, "", "test.lock.yml")

		err := compiler.addActivationRepositoryAndOutputSteps(ctx)

		require.Error(t, err)
	})
}

func TestActivationStepsAddCheckoutAndBaseRestoreStep(t *testing.T) {
	compiler := newActivationStepsTestCompiler("")

	t.Run("adds checkout and save-base steps", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{})

		compiler.addActivationCheckoutAndBaseRestoreStep(ctx)

		steps := strings.Join(ctx.steps, "")
		assert.Contains(t, steps, "Checkout .github and .agents folders")
		assert.Contains(t, steps, "Save agent config folders for base branch restoration")
		assert.Contains(t, steps, "save_base_github_folders.sh")
	})

	t.Run("skips when checkout is disabled by action tag", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{
			Features: map[string]any{
				"action-tag": "v1.2.3",
			},
		})

		compiler.addActivationCheckoutAndBaseRestoreStep(ctx)

		assert.Empty(t, ctx.steps)
	})
}

func TestActivationStepsAddLockFileStep(t *testing.T) {
	compiler := newActivationStepsTestCompiler("")

	t.Run("adds stale lock step", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{
			ActivationGitHubToken: "${{ secrets.CUSTOM_TOKEN }}",
			StaleCheckFull:        true,
		})

		compiler.addActivationLockFileStep(ctx)

		steps := strings.Join(ctx.steps, "")
		assert.Contains(t, steps, "Check workflow lock file")
		assert.Contains(t, steps, "id: check-lock-file")
		assert.Contains(t, steps, "GH_AW_WORKFLOW_FILE: \"test.lock.yml\"")
		assert.Contains(t, steps, "GH_AW_STALE_CHECK_FULL: \"true\"")
		assert.Contains(t, steps, "github-token: ${{ secrets.CUSTOM_TOKEN }}")
		assert.Contains(t, steps, "check_workflow_timestamp_api.cjs")
	})

	t.Run("skips when stale check is disabled", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{StaleCheckDisabled: true})

		compiler.addActivationLockFileStep(ctx)

		assert.Empty(t, ctx.steps)
	})
}

func TestActivationStepsAddVersionCheckStep(t *testing.T) {
	t.Run("adds version check for release builds", func(t *testing.T) {
		originalIsRelease := isReleaseBuild
		isReleaseBuild = true
		t.Cleanup(func() { isReleaseBuild = originalIsRelease })

		compiler := newActivationStepsTestCompiler("v1.2.3")
		ctx := newActivationStepsTestContext(&WorkflowData{})

		compiler.addActivationVersionCheckStep(ctx)

		steps := strings.Join(ctx.steps, "")
		assert.Contains(t, steps, "Check compile-agentic version")
		assert.Contains(t, steps, "GH_AW_COMPILED_VERSION: \"v1.2.3\"")
		assert.Contains(t, steps, "GH_AW_BLOCKED_VERSION_REPORT_AS_ISSUE: \"true\"")
		assert.Contains(t, steps, "GH_AW_WORKFLOW_NAME: \"\"")
		assert.Contains(t, steps, "check_version_updates.cjs")
	})

	t.Run("disables blocked version issue reporting when failure issues are disabled", func(t *testing.T) {
		originalIsRelease := isReleaseBuild
		isReleaseBuild = true
		t.Cleanup(func() { isReleaseBuild = originalIsRelease })

		compiler := newActivationStepsTestCompiler("v1.2.3")
		ctx := newActivationStepsTestContext(&WorkflowData{
			SafeOutputs: &SafeOutputsConfig{ReportFailureAsIssue: templatableBoolPtr("false")},
		})

		compiler.addActivationVersionCheckStep(ctx)

		steps := strings.Join(ctx.steps, "")
		assert.Contains(t, steps, "GH_AW_BLOCKED_VERSION_REPORT_AS_ISSUE: \"false\"")
	})

	t.Run("disables blocked version issue reporting when on.report-blocked-version is false", func(t *testing.T) {
		originalIsRelease := isReleaseBuild
		isReleaseBuild = true
		t.Cleanup(func() { isReleaseBuild = originalIsRelease })

		compiler := newActivationStepsTestCompiler("v1.2.3")
		ctx := newActivationStepsTestContext(&WorkflowData{
			ReportBlockedVersionDisabled: true,
		})

		compiler.addActivationVersionCheckStep(ctx)

		steps := strings.Join(ctx.steps, "")
		assert.Contains(t, steps, "GH_AW_BLOCKED_VERSION_REPORT_AS_ISSUE: \"false\"")
	})

	t.Run("report-blocked-version: false overrides a templated report-failure-as-issue value", func(t *testing.T) {
		originalIsRelease := isReleaseBuild
		isReleaseBuild = true
		t.Cleanup(func() { isReleaseBuild = originalIsRelease })

		compiler := newActivationStepsTestCompiler("v1.2.3")
		ctx := newActivationStepsTestContext(&WorkflowData{
			ReportBlockedVersionDisabled: true,
			SafeOutputs:                  &SafeOutputsConfig{ReportFailureAsIssue: templatableBoolPtr("${{ inputs.report-failure-as-issue }}")},
		})

		compiler.addActivationVersionCheckStep(ctx)

		steps := strings.Join(ctx.steps, "")
		assert.Contains(t, steps, "GH_AW_BLOCKED_VERSION_REPORT_AS_ISSUE: \"false\"")
	})

	t.Run("skips version check for dev builds", func(t *testing.T) {
		compiler := newActivationStepsTestCompiler("dev")
		ctx := newActivationStepsTestContext(&WorkflowData{})

		compiler.addActivationVersionCheckStep(ctx)

		assert.Empty(t, ctx.steps)
	})
}

func TestActivationBlockedVersionIssuePermissions(t *testing.T) {
	originalIsRelease := isReleaseBuild
	isReleaseBuild = true
	t.Cleanup(func() { isReleaseBuild = originalIsRelease })

	t.Run("adds issues write for released compiler version checks", func(t *testing.T) {
		compiler := newActivationStepsTestCompiler("v1.2.3")
		ctx := newActivationStepsTestContext(&WorkflowData{})

		permissions, err := compiler.buildActivationPermissions(ctx)

		require.NoError(t, err)
		assert.Contains(t, permissions, "issues: write")
	})

	t.Run("omits issues write when failure issue reporting is disabled", func(t *testing.T) {
		compiler := newActivationStepsTestCompiler("v1.2.3")
		ctx := newActivationStepsTestContext(&WorkflowData{
			SafeOutputs: &SafeOutputsConfig{ReportFailureAsIssue: templatableBoolPtr("false")},
		})

		permissions, err := compiler.buildActivationPermissions(ctx)

		require.NoError(t, err)
		assert.NotContains(t, permissions, "issues: write")
	})

	t.Run("omits issues write when on.report-blocked-version is false", func(t *testing.T) {
		compiler := newActivationStepsTestCompiler("v1.2.3")
		ctx := newActivationStepsTestContext(&WorkflowData{
			ReportBlockedVersionDisabled: true,
		})

		permissions, err := compiler.buildActivationPermissions(ctx)

		require.NoError(t, err)
		assert.NotContains(t, permissions, "issues: write")
	})
}

func TestActivationStepsAddSkillInstallSteps(t *testing.T) {
	compiler := newActivationStepsTestCompiler("")

	t.Run("adds skill install steps", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{
			EngineConfig: &EngineConfig{ID: "claude"},
			Skills: []string{
				"githubnext/skills@deadbeef",
			},
		})

		compiler.addActivationSkillInstallSteps(ctx)

		steps := strings.Join(ctx.steps, "")
		assert.Contains(t, steps, "Upgrade gh CLI for frontmatter skills")
		assert.Contains(t, steps, "Install frontmatter skill: githubnext/skills")
		assert.Contains(t, steps, "GH_AW_INFO_ENGINE_ID: \"claude\"")
		assert.Contains(t, steps, "GH_AW_GH_SKILL_AGENT_NAME: \"claude-code\"")
		assert.Contains(t, steps, "GH_AW_FRONTMATTER_SKILLS: \"githubnext/skills@deadbeef\"")
		assert.Contains(t, steps, "collect_skill_install_failures.cjs")
		assert.Equal(t, "${{ steps.collect-skill-install-failures.outputs.failure_count || '0' }}", ctx.outputs["skill_install_failure_count"])
		assert.Equal(t, "${{ steps.collect-skill-install-failures.outputs.errors || '' }}", ctx.outputs["skill_install_errors"])
	})

	t.Run("skips when no skills are configured", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{})

		compiler.addActivationSkillInstallSteps(ctx)

		assert.Empty(t, ctx.steps)
		assert.Empty(t, ctx.outputs)
	})
}

func TestActivationStepsAddTextOutputStep(t *testing.T) {
	compiler := newActivationStepsTestCompiler("")

	t.Run("adds text output step", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{
			NeedsTextOutput: true,
			Bots:            []string{"dependabot[bot]"},
			EngineConfig:    &EngineConfig{ID: "copilot"},
			SafeOutputs: &SafeOutputsConfig{
				AllowedDomains: []string{"docs.example.com"},
			},
		})

		err := compiler.addActivationTextOutputStep(ctx)

		require.NoError(t, err)
		steps := strings.Join(ctx.steps, "")
		assert.Contains(t, steps, "Compute current body text")
		assert.Contains(t, steps, "id: sanitized")
		assert.Contains(t, steps, "GH_AW_ALLOWED_BOTS: \"dependabot[bot]\"")
		assert.Contains(t, steps, "GH_AW_ALLOWED_DOMAINS:")
		assert.Contains(t, steps, "docs.example.com")
		assert.Contains(t, steps, "github.com")
		assert.Contains(t, steps, "localhost")
		assert.Contains(t, steps, "compute_text.cjs")
		assert.Equal(t, "${{ steps.sanitized.outputs.text }}", ctx.outputs["text"])
		assert.Equal(t, "${{ steps.sanitized.outputs.title }}", ctx.outputs["title"])
		assert.Equal(t, "${{ steps.sanitized.outputs.body }}", ctx.outputs["body"])
	})

	t.Run("skips when text output is not needed", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{})

		err := compiler.addActivationTextOutputStep(ctx)

		require.NoError(t, err)
		assert.Empty(t, ctx.steps)
	})

	t.Run("returns sanitization errors", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{
			NeedsTextOutput: true,
			Model:           "/bad-provider",
			EngineConfig: &EngineConfig{
				ID: "pi",
			},
		})

		err := compiler.addActivationTextOutputStep(ctx)

		require.Error(t, err)
	})
}

func TestActivationStepsComputeActivationSanitizationDomains(t *testing.T) {
	compiler := newActivationStepsTestCompiler("")

	t.Run("uses expanded allowed domains when configured", func(t *testing.T) {
		domains, err := compiler.computeActivationSanitizationDomains(&WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			SafeOutputs: &SafeOutputsConfig{
				AllowedDomains: []string{"docs.example.com"},
			},
		})

		require.NoError(t, err)
		assert.Contains(t, domains, "docs.example.com")
		assert.Contains(t, domains, "github.com")
		assert.Contains(t, domains, "localhost")
	})

	t.Run("uses base allowed domains when safe outputs are absent", func(t *testing.T) {
		domains, err := compiler.computeActivationSanitizationDomains(&WorkflowData{
			EngineConfig:       &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{Allowed: []string{"copilot"}},
		})

		require.NoError(t, err)
		assert.Contains(t, domains, "api.github.com")
	})

	t.Run("returns errors for malformed models", func(t *testing.T) {
		_, err := compiler.computeActivationSanitizationDomains(&WorkflowData{
			Model: "/bad-provider",
			EngineConfig: &EngineConfig{
				ID: "pi",
			},
		})

		require.Error(t, err)
	})
}

func TestActivationStepsAddStatusCommentStep(t *testing.T) {
	compiler := newActivationStepsTestCompiler("")

	t.Run("adds status comment step", func(t *testing.T) {
		statusComment := true
		ctx := newActivationStepsTestContext(&WorkflowData{
			Name:             "test-workflow",
			StatusComment:    &statusComment,
			FrontmatterEmoji: "🤖",
			TrackerID:        "tracker-1234",
			LockForAgent:     true,
			SafeOutputs: &SafeOutputsConfig{
				Messages: &SafeOutputMessagesConfig{
					RunStarted: "started",
				},
			},
		})
		ctx.statusCommentIssues = true

		err := compiler.addActivationStatusCommentStep(ctx)

		require.NoError(t, err)
		steps := strings.Join(ctx.steps, "")
		assert.Contains(t, steps, "Add comment with workflow run link")
		assert.Contains(t, steps, "id: add-comment")
		assert.Contains(t, steps, "GH_AW_WORKFLOW_NAME: \"test-workflow\"")
		assert.Contains(t, steps, "GH_AW_WORKFLOW_EMOJI: \"🤖\"")
		assert.Contains(t, steps, "GH_AW_TRACKER_ID: \"tracker-1234\"")
		assert.Contains(t, steps, "GH_AW_LOCK_FOR_AGENT: \"true\"")
		assert.Contains(t, steps, "GH_AW_SAFE_OUTPUT_MESSAGES:")
		assert.Contains(t, steps, "started")
		assert.Contains(t, steps, "add_workflow_run_comment.cjs")
		assert.NotContains(t, steps, "github-token:")
		assert.Equal(t, "${{ steps.add-comment.outputs.comment-id }}", ctx.outputs["comment_id"])
		assert.Equal(t, "${{ steps.add-comment.outputs.comment-url }}", ctx.outputs["comment_url"])
		assert.Equal(t, "${{ steps.add-comment.outputs.comment-repo }}", ctx.outputs["comment_repo"])
	})

	t.Run("skips when status comments are disabled", func(t *testing.T) {
		statusComment := false
		ctx := newActivationStepsTestContext(&WorkflowData{
			StatusComment: &statusComment,
		})

		err := compiler.addActivationStatusCommentStep(ctx)

		require.NoError(t, err)
		assert.Empty(t, ctx.steps)
	})
}

func TestActivationStepsAddSafeOutputMessagesEnv(t *testing.T) {
	t.Run("adds serialized messages env with message value", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{
			SafeOutputs: &SafeOutputsConfig{
				Messages: &SafeOutputMessagesConfig{
					RunFailure: "failed",
				},
			},
		})

		addActivationSafeOutputMessagesEnv(ctx)

		combined := strings.Join(ctx.steps, "")
		assert.Contains(t, combined, "GH_AW_SAFE_OUTPUT_MESSAGES:")
		assert.Contains(t, combined, "failed")
	})

	t.Run("skips when messages are absent", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{})

		addActivationSafeOutputMessagesEnv(ctx)

		assert.Empty(t, ctx.steps)
	})
}

func TestActivationStepsAddIssueLockStep(t *testing.T) {
	compiler := newActivationStepsTestCompiler("")

	t.Run("adds issue lock step", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{
			LockForAgent: true,
			AIReaction:   "eyes",
		})

		compiler.addActivationIssueLockStep(ctx)

		steps := strings.Join(ctx.steps, "")
		assert.Contains(t, steps, "Lock issue for agentic workflow")
		assert.Contains(t, steps, "id: lock-issue")
		assert.Contains(t, steps, "lock-issue.cjs")
		assert.Equal(t, "${{ steps.lock-issue.outputs.locked }}", ctx.outputs["issue_locked"])
	})

	t.Run("skips when lock-for-agent is disabled", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{})

		compiler.addActivationIssueLockStep(ctx)

		assert.Empty(t, ctx.steps)
	})
}

func TestActivationStepsEnsureActivationCommentOutputs(t *testing.T) {
	t.Run("adds missing outputs", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{})

		ensureActivationCommentOutputs(ctx)

		assert.Equal(t, `""`, ctx.outputs["comment_id"])
		assert.Equal(t, `""`, ctx.outputs["comment_repo"])
	})

	t.Run("preserves existing outputs", func(t *testing.T) {
		ctx := newActivationStepsTestContext(&WorkflowData{})
		ctx.outputs["comment_id"] = "existing-id"
		ctx.outputs["comment_repo"] = "existing-repo"

		ensureActivationCommentOutputs(ctx)

		assert.Equal(t, "existing-id", ctx.outputs["comment_id"])
		assert.Equal(t, "existing-repo", ctx.outputs["comment_repo"])
	})
}
