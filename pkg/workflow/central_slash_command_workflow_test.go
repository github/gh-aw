//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/require"
)

func TestGenerateCentralSlashCommandWorkflow_GeneratesWorkflow(t *testing.T) {
	tmpDir := testutil.TempDir(t, "central-slash-workflow-test")
	statusComment := true
	t.Setenv("GH_AW_ACTION_MODE", "dev")
	originalVersion := compilerVersion
	originalIsRelease := isReleaseBuild
	t.Cleanup(func() {
		compilerVersion = originalVersion
		isReleaseBuild = originalIsRelease
	})
	SetVersion("c610c2a")
	SetIsRelease(false)

	data := []*WorkflowData{
		{
			WorkflowID:         "triage-issue",
			Command:            []string{"triage"},
			CommandEvents:      []string{"issue_comment", "issues"},
			CommandCentralized: true,
			AIReaction:         "eyes",
			FrontmatterEmoji:   "🤖",
			StatusComment:      &statusComment,
		},
		{
			WorkflowID:         "triage-pr",
			Command:            []string{"triage"},
			CommandEvents:      []string{"pull_request", "pull_request_comment"},
			CommandCentralized: true,
			AIReaction:         "rocket",
			StatusComment:      &statusComment,
		},
		{
			WorkflowID:         "cloclo",
			Command:            []string{"cloclo"},
			CommandEvents:      []string{"discussion_comment"},
			CommandCentralized: true,
			AIReaction:         "heart",
			StatusComment:      &statusComment,
		},
		{
			WorkflowID:                "ci-doctor",
			LabelCommand:              []string{"ci-doctor"},
			LabelCommandEvents:        []string{"pull_request"},
			LabelCommandDecentralized: true,
			AIReaction:                "eyes",
			FrontmatterEmoji:          "🏷️",
			StatusComment:             &statusComment,
		},
		{
			WorkflowID:         "daily-summary",
			Command:            []string{"summary"},
			CommandCentralized: false,
			Description:        "Summarize recent updates",
		},
	}

	require.NoError(t, GenerateCentralSlashCommandWorkflow(context.Background(), data, tmpDir, nil))

	generatedPath := filepath.Join(tmpDir, centralSlashCommandWorkflowFilename)
	content, err := os.ReadFile(generatedPath)
	require.NoError(t, err)
	text := string(content)
	lines := strings.Split(text, "\n")
	require.NotEmpty(t, lines)
	require.Contains(t, lines[0], "# gh-aw-commands: ")
	metadataJSON := strings.TrimPrefix(lines[0], "# gh-aw-commands: ")
	var metadata commandsHeaderMetadata
	require.NoError(t, json.Unmarshal([]byte(metadataJSON), &metadata))
	require.Equal(t, "v1", metadata.PayloadVersion)
	require.Equal(t, "v1", metadata.SchemaVersion)
	require.Equal(t, "dev", metadata.Compiler)
	require.Equal(t, []string{"cloclo", "triage"}, metadata.Commands)
	require.Equal(t, []string{"ci-doctor", "cloclo", "triage-issue", "triage-pr"}, metadata.Workflows)
	require.Contains(t, text, "# Routing summary (sorted):")
	require.Contains(t, text, "#   slash commands:")
	require.Contains(t, text, "#     /cloclo -> cloclo [discussion_comment] reaction=heart")
	require.Contains(t, text, "#     /triage -> triage-issue [issue_comment,issues] reaction=eyes")
	require.Contains(t, text, "#     /triage -> triage-pr [pull_request,pull_request_comment] reaction=rocket")
	require.Contains(t, text, "#   labels:")
	require.Contains(t, text, "#     ci-doctor -> ci-doctor [pull_request] reaction=eyes")

	require.Contains(t, text, "name: \"Agentic Commands\"")
	require.NotContains(t, text, "Compiler version:")
	require.Contains(t, text, "permissions: {}")
	require.Contains(t, text, "runs-on: ubuntu-slim")
	require.Contains(t, text, "timeout-minutes: 15")
	require.Contains(t, text, "    permissions:\n      actions: write\n      contents: read\n      issues: write\n      pull-requests: write\n      discussions: write")
	require.Contains(t, text, "      - name: Setup Scripts")
	require.Contains(t, text, "        uses: ./actions/setup")
	require.Contains(t, text, "          destination: ${{ runner.temp }}/gh-aw/actions")
	require.Contains(t, text, "issues:")
	require.Contains(t, text, "issue_comment:")
	require.Contains(t, text, "pull_request:")
	require.Contains(t, text, "discussion_comment:")
	require.Contains(t, text, `"triage":[{"workflow":"triage-issue","events":["issue_comment","issues"],"ai_reaction":"eyes","emoji":"🤖","status_comment":true},{"workflow":"triage-pr","events":["pull_request","pull_request_comment"],"ai_reaction":"rocket","status_comment":true}]`)
	require.Contains(t, text, `"cloclo":[{"workflow":"cloclo","events":["discussion_comment"],"ai_reaction":"heart","status_comment":true}]`)
	require.Contains(t, text, `"ci-doctor":[{"workflow":"ci-doctor","events":["pull_request"],"ai_reaction":"eyes","emoji":"🏷️","status_comment":true}]`)
	require.Contains(t, text, `GH_AW_HELP_COMMANDS`)
	require.Contains(t, text, `"command":"summary","description":"Summarize recent updates","centralized":false,"decentralized":true`)
	require.Contains(t, text, `"command":"triage","centralized":true,"decentralized":false`)
	require.Contains(t, text, `GH_AW_HELP_COMMAND_ENABLED: 'true'`)
	require.Contains(t, text, `GH_AW_SLASH_COMMAND_DOCS_URL: 'https://github.github.com/gh-aw/reference/command-triggers/'`)
	require.Contains(t, text, "GH_AW_LABEL_ROUTING")
	require.Contains(t, text, `require(path.join(actionsDir, 'setup_globals.cjs'))`)
	require.Contains(t, text, `setupGlobals(core, github, context, exec, io, getOctokit);`)
	require.Contains(t, text, `require(path.join(actionsDir, 'route_slash_command.cjs'))`)
	require.NotContains(t, text, `const routeMap = JSON.parse(process.env.GH_AW_SLASH_ROUTING || "{}");`)
	require.NotContains(t, text, `trustedAuthorAssociations`)
	require.NotContains(t, text, `isForkBasedPullRequestEvent`)
	require.NotContains(t, text, `workflow_id: route.workflow + ".lock.yml"`)
}

func TestGenerateCentralSlashCommandWorkflow_PinsSetupActionWithResolver(t *testing.T) {
	tmpDir := testutil.TempDir(t, "central-slash-workflow-action-pin-test")
	t.Setenv("GH_AW_ACTION_MODE", "action")
	originalVersion := compilerVersion
	originalIsRelease := isReleaseBuild
	t.Cleanup(func() {
		compilerVersion = originalVersion
		isReleaseBuild = originalIsRelease
	})
	SetVersion("v1.2.3")
	SetIsRelease(true)

	const setupSHA = "0123456789abcdef0123456789abcdef01234567"
	cache := NewActionCache(tmpDir)
	require.True(t, cache.Set("github/gh-aw-actions/setup", "v1.2.3", setupSHA))
	resolver := NewActionResolver(cache)
	data := []*WorkflowData{
		{
			WorkflowID:         "without-resolver",
			Command:            []string{"triage"},
			CommandEvents:      []string{"issue_comment"},
			CommandCentralized: true,
		},
		{
			WorkflowID:         "with-resolver",
			Command:            []string{"triage"},
			CommandEvents:      []string{"pull_request_comment"},
			CommandCentralized: true,
			ActionResolver:     resolver,
		},
	}

	require.NoError(t, GenerateCentralSlashCommandWorkflow(context.Background(), data, tmpDir, nil))
	content, err := os.ReadFile(filepath.Join(tmpDir, centralSlashCommandWorkflowFilename))
	require.NoError(t, err)
	text := string(content)
	require.Contains(t, text, "        uses: github/gh-aw-actions/setup@"+setupSHA+" # v1.2.3")
	require.NotContains(t, text, "        uses: github/gh-aw-actions/setup@v1.2.3")
}

func TestGenerateCentralSlashCommandWorkflow_DeletesWhenUnused(t *testing.T) {
	tmpDir := testutil.TempDir(t, "central-slash-workflow-delete-test")
	generatedPath := filepath.Join(tmpDir, centralSlashCommandWorkflowFilename)
	require.NoError(t, os.WriteFile(generatedPath, []byte("stale"), 0644))

	data := []*WorkflowData{
		{
			WorkflowID:         "regular",
			Command:            []string{"regular"},
			CommandEvents:      []string{"issue_comment"},
			CommandCentralized: false,
		},
	}

	require.NoError(t, GenerateCentralSlashCommandWorkflow(context.Background(), data, tmpDir, nil))
	_, err := os.Stat(generatedPath)
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))
}

func TestGenerateCentralSlashCommandWorkflow_GeneratesForDecentralizedLabelsOnly(t *testing.T) {
	tmpDir := testutil.TempDir(t, "central-label-workflow-test")
	data := []*WorkflowData{
		{
			WorkflowID:                "ci-doctor",
			LabelCommand:              []string{"ci-doctor"},
			LabelCommandEvents:        []string{"pull_request"},
			LabelCommandDecentralized: true,
		},
	}

	require.NoError(t, GenerateCentralSlashCommandWorkflow(context.Background(), data, tmpDir, nil))
	content, err := os.ReadFile(filepath.Join(tmpDir, centralSlashCommandWorkflowFilename))
	require.NoError(t, err)
	text := string(content)
	require.Contains(t, text, "GH_AW_LABEL_ROUTING")
	require.Contains(t, text, `"ci-doctor":[{"workflow":"ci-doctor","events":["pull_request"]}]`)
	require.Contains(t, text, "pull_request:")
	require.Contains(t, text, "types: [labeled]")
	require.Contains(t, text, "#   slash commands:")
	require.Contains(t, text, "#     (none)")
	require.Contains(t, text, "#   labels:")
	require.Contains(t, text, "#     ci-doctor -> ci-doctor [pull_request]")
}

func TestGenerateCentralSlashCommandWorkflow_IncludesPullRequestsPermissionForIssueCommentRoutes(t *testing.T) {
	tmpDir := testutil.TempDir(t, "central-slash-workflow-issue-comment-perms")
	data := []*WorkflowData{
		{
			WorkflowID:         "triage",
			Command:            []string{"triage"},
			CommandEvents:      []string{"issue_comment"},
			CommandCentralized: true,
		},
	}

	require.NoError(t, GenerateCentralSlashCommandWorkflow(context.Background(), data, tmpDir, nil))
	content, err := os.ReadFile(filepath.Join(tmpDir, centralSlashCommandWorkflowFilename))
	require.NoError(t, err)
	text := string(content)
	require.Contains(t, text, "issue_comment:")
	require.Contains(t, text, "pull-requests: write")
}

func TestCollectCentralLabelCommandRoutes_IncludesSlashCentralizedLabelCommands(t *testing.T) {
	data := []*WorkflowData{
		{
			WorkflowID:         "triage",
			Command:            []string{"triage"},
			CommandEvents:      []string{"issue_comment"},
			CommandCentralized: true,
			LabelCommand:       []string{"triage"},
			LabelCommandEvents: []string{"issues"},
			AIReaction:         "eyes",
		},
	}

	_, labelRoutesByCommand, mergedEvents := collectCentralCommandRoutes(data)
	require.Equal(t, []slashCommandRoute{
		{Workflow: "triage", Events: []string{"issues"}, AIReaction: "eyes"},
	}, labelRoutesByCommand["triage"])
	require.ElementsMatch(t, []string{"labeled"}, typeSetKeys(mergedEvents["issues"]))
}

func TestRemoveIfExists(t *testing.T) {
	tmpDir := testutil.TempDir(t, "remove-if-exists-test")
	existingPath := filepath.Join(tmpDir, "existing.txt")
	missingPath := filepath.Join(tmpDir, "missing.txt")

	require.NoError(t, os.WriteFile(existingPath, []byte("content"), 0644))
	require.NoError(t, removeIfExists(existingPath))
	_, err := os.Stat(existingPath)
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))

	require.NoError(t, removeIfExists(missingPath))
}

func TestCollectCentralSlashCommandRoutes_UnionizesMergedEvents(t *testing.T) {
	data := []*WorkflowData{
		{
			WorkflowID:         "triage-issue",
			Command:            []string{"triage"},
			CommandEvents:      []string{"issues", "issue_comment"},
			CommandCentralized: true,
		},
		{
			WorkflowID:         "triage-pr",
			Command:            []string{"triage"},
			CommandEvents:      []string{"pull_request", "pull_request_comment"},
			CommandCentralized: true,
		},
		{
			WorkflowID:         "non-centralized",
			Command:            []string{"triage"},
			CommandEvents:      []string{"discussion"},
			CommandCentralized: false,
		},
	}

	routesByCommand, mergedEvents := collectCentralSlashCommandRoutes(data)

	require.Equal(t, []slashCommandRoute{
		{Workflow: "triage-issue", Events: []string{"issue_comment", "issues"}},
		{Workflow: "triage-pr", Events: []string{"pull_request", "pull_request_comment"}},
	}, routesByCommand["triage"])

	require.ElementsMatch(t, []string{"opened", "edited", "reopened"}, typeSetKeys(mergedEvents["issues"]))
	require.ElementsMatch(t, []string{"created", "edited"}, typeSetKeys(mergedEvents["issue_comment"]))
	require.ElementsMatch(t, []string{"opened", "edited", "reopened"}, typeSetKeys(mergedEvents["pull_request"]))
	require.NotContains(t, mergedEvents, "discussion")
}

func TestCollectCentralSlashCommandRoutes_RespectsReactionEventTargets(t *testing.T) {
	disable := false
	enable := true
	data := []*WorkflowData{
		{
			WorkflowID:                "issue-only",
			Command:                   []string{"triage"},
			CommandEvents:             []string{"issue_comment", "pull_request_comment"},
			CommandCentralized:        true,
			AIReaction:                "eyes",
			StatusComment:             &enable,
			ReactionIssues:            &enable,
			ReactionPullRequests:      &disable,
			StatusCommentIssues:       &enable,
			StatusCommentPullRequests: &disable,
		},
		{
			WorkflowID:                "pr-only-disabled",
			Command:                   []string{"triage"},
			CommandEvents:             []string{"pull_request_comment"},
			CommandCentralized:        true,
			AIReaction:                "rocket",
			StatusComment:             &enable,
			ReactionPullRequests:      &disable,
			StatusCommentPullRequests: &disable,
		},
		{
			WorkflowID:               "discussion-enabled",
			Command:                  []string{"triage"},
			CommandEvents:            []string{"discussion_comment"},
			CommandCentralized:       true,
			AIReaction:               "heart",
			StatusComment:            &enable,
			ReactionDiscussions:      &enable,
			StatusCommentDiscussions: &enable,
		},
		{
			WorkflowID:         "none-reaction",
			Command:            []string{"triage"},
			CommandEvents:      []string{"issue_comment"},
			CommandCentralized: true,
			AIReaction:         "none",
			StatusComment:      &enable,
		},
	}

	routesByCommand, _ := collectCentralSlashCommandRoutes(data)
	require.Len(t, routesByCommand["triage"], 5)
	routeReactions := map[string][]string{}
	for _, route := range routesByCommand["triage"] {
		routeReactions[route.Workflow] = append(routeReactions[route.Workflow], route.AIReaction+"|"+strconv.FormatBool(route.StatusComment)+"|"+strings.Join(route.Events, ","))
	}
	require.ElementsMatch(t, []string{"eyes|true|issue_comment", "|false|pull_request_comment"}, routeReactions["issue-only"])
	require.Equal(t, []string{"|false|pull_request_comment"}, routeReactions["pr-only-disabled"])
	require.Equal(t, []string{"heart|true|discussion_comment"}, routeReactions["discussion-enabled"])
	require.Equal(t, []string{"|true|issue_comment"}, routeReactions["none-reaction"])
}

func TestGenerateCentralSlashCommandWorkflow_UsesCentralizedRunsOnResolution(t *testing.T) {
	tmpDir := testutil.TempDir(t, "central-slash-workflow-runs-on-test")
	data := []*WorkflowData{
		{
			WorkflowID:         "one",
			Command:            []string{"one"},
			CommandEvents:      []string{"issue_comment"},
			CommandCentralized: true,
			RunsOnSlim:         "runs-on: ubuntu-latest",
		},
		{
			WorkflowID:         "two",
			Command:            []string{"two"},
			CommandEvents:      []string{"issue_comment"},
			CommandCentralized: true,
			SafeOutputs: &SafeOutputsConfig{
				RunsOn: "self-hosted",
			},
		},
		{
			WorkflowID:         "three",
			Command:            []string{"three"},
			CommandEvents:      []string{"issue_comment"},
			CommandCentralized: true,
			SafeOutputs: &SafeOutputsConfig{
				RunsOn: "self-hosted",
			},
		},
	}

	require.NoError(t, GenerateCentralSlashCommandWorkflow(context.Background(), data, tmpDir, nil))
	content, err := os.ReadFile(filepath.Join(tmpDir, centralSlashCommandWorkflowFilename))
	require.NoError(t, err)
	require.Contains(t, string(content), "runs-on: self-hosted")
}

func TestFormatRunsOnSnippetForInlineValue(t *testing.T) {
	tests := []struct {
		name   string
		runsOn string
		want   string
	}{
		{
			name:   "plain label",
			runsOn: "ubuntu-latest",
			want:   "ubuntu-latest",
		},
		{
			name:   "rendered string snippet",
			runsOn: "runs-on: self-hosted",
			want:   "self-hosted",
		},
		{
			name:   "rendered array snippet",
			runsOn: "runs-on:\n- self-hosted\n- linux",
			want:   "\n      - self-hosted\n      - linux",
		},
		{
			name:   "rendered object snippet",
			runsOn: "runs-on:\n  group: runner-group\n  labels:\n  - linux",
			want:   "\n      group: runner-group\n      labels:\n      - linux",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, formatRunsOnSnippetForInlineValue(tt.runsOn))
		})
	}
}

func TestBuildCommandsHeaderMetadata_UsesReleaseVersionOnlyForReleaseBuilds(t *testing.T) {
	originalVersion := compilerVersion
	originalIsRelease := isReleaseBuild
	t.Cleanup(func() {
		compilerVersion = originalVersion
		isReleaseBuild = originalIsRelease
	})

	routesByCommand := map[string][]slashCommandRoute{
		"triage": {
			{Workflow: "triage-issue", Events: []string{"issues"}},
		},
	}

	SetVersion("abc1234")
	SetIsRelease(false)
	metadata := buildCommandsHeaderMetadata(routesByCommand, nil)
	require.Equal(t, "dev", metadata.Compiler)

	SetVersion("v1.2.3")
	SetIsRelease(true)
	metadata = buildCommandsHeaderMetadata(routesByCommand, nil)
	require.Equal(t, "v1.2.3", metadata.Compiler)
}

func TestGenerateCentralSlashCommandWorkflow_DisablesHelpCommandViaRepoConfig(t *testing.T) {
	tmpDir := testutil.TempDir(t, "central-slash-workflow-help-config-test")
	disabled := false
	data := []*WorkflowData{
		{
			WorkflowID:         "triage",
			Command:            []string{"triage"},
			CommandEvents:      []string{"issue_comment"},
			CommandCentralized: true,
		},
	}

	require.NoError(t, GenerateCentralSlashCommandWorkflow(context.Background(), data, tmpDir, &RepoConfig{HelpCommand: &disabled}))
	content, err := os.ReadFile(filepath.Join(tmpDir, centralSlashCommandWorkflowFilename))
	require.NoError(t, err)
	require.Contains(t, string(content), `GH_AW_HELP_COMMAND_ENABLED: 'false'`)
}

func TestBuildHelpCommandEntries(t *testing.T) {
	data := []*WorkflowData{
		{
			WorkflowID:         "triage",
			Command:            []string{"triage", "helpful"},
			CommandCentralized: true,
			Description:        "Triage work items",
		},
		{
			WorkflowID:         "triage-inline",
			Command:            []string{"triage"},
			CommandCentralized: false,
		},
		{
			WorkflowID:         "summary",
			Command:            []string{"summary"},
			CommandCentralized: false,
			Description:        "Summarize latest updates",
		},
		{
			WorkflowID:   "label-triage",
			LabelCommand: []string{"triage-label", "urgent"},
			Description:  "Triage via label",
		},
		{
			WorkflowID:   "label-duplicate",
			LabelCommand: []string{"triage-label"},
		},
	}

	require.Equal(t, []helpCommandEntry{
		{Command: "helpful", Description: "Triage work items", Centralized: true, SourceFile: "triage"},
		{Command: "summary", Description: "Summarize latest updates", Decentralized: true, SourceFile: "summary"},
		{Command: "triage", Description: "Triage work items", Centralized: true, Decentralized: true, SourceFile: "triage"},
		{Command: "triage-label", Description: "Triage via label", Label: true, SourceFile: "label-triage"},
		{Command: "urgent", Description: "Triage via label", Label: true, SourceFile: "label-triage"},
	}, buildHelpCommandEntries(data))
}

func TestBuildHelpCommandEntries_ConflictingDescriptions(t *testing.T) {
	// Two workflows register the same command with different descriptions — first-wins.
	data := []*WorkflowData{
		{WorkflowID: "deploy-a", Command: []string{"deploy"}, CommandCentralized: true, Description: "Deploy service A"},
		{WorkflowID: "deploy-b", Command: []string{"deploy"}, CommandCentralized: true, Description: "Deploy service B"},
	}
	entries := buildHelpCommandEntries(data)
	require.Len(t, entries, 1)
	require.Equal(t, "Deploy service A", entries[0].Description, "first description should win on conflict")
	require.Equal(t, "deploy-a", entries[0].SourceFile, "source file should be from first workflow")
}

func TestBuildHelpCommandEntries_ReservedHelpCommandName(t *testing.T) {
	// The 'help' command name is reserved for the builtin handler; it should still be
	// included in entries so the metadata accurately reflects the registered commands.
	data := []*WorkflowData{
		{WorkflowID: "custom-help", Command: []string{"help"}, CommandCentralized: true},
	}
	entries := buildHelpCommandEntries(data)
	require.Len(t, entries, 1)
	require.Equal(t, "help", entries[0].Command)
}

func typeSetKeys(typeSet map[string]struct{}) []string {
	out := make([]string, 0, len(typeSet))
	for key := range typeSet {
		out = append(out, key)
	}
	return out
}

func TestGenerateCentralSlashCommandWorkflow_CheckoutDoesNotPersistCredentials(t *testing.T) {
	tmpDir := testutil.TempDir(t, "central-slash-workflow-persist-creds")
	data := []*WorkflowData{
		{
			WorkflowID:         "triage",
			Command:            []string{"triage"},
			CommandEvents:      []string{"issue_comment"},
			CommandCentralized: true,
		},
	}

	require.NoError(t, GenerateCentralSlashCommandWorkflow(context.Background(), data, tmpDir, nil))
	content, err := os.ReadFile(filepath.Join(tmpDir, centralSlashCommandWorkflowFilename))
	require.NoError(t, err)
	text := string(content)

	require.Contains(t, text, "      - name: Checkout repository\n        uses: "+getActionPin("actions/checkout")+"\n        with:\n          persist-credentials: false\n")
	require.NotContains(t, text, "persist-credentials: true")
}
