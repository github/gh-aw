//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepoMemoryAppTargetAndCondition(t *testing.T) {
	app := &GitHubAppConfig{}
	for _, tc := range []struct {
		name      string
		memory    RepoMemoryEntry
		level     PermissionLevel
		target    string
		condition string
	}{
		{name: "default target", memory: RepoMemoryEntry{ID: "notes", GitHubApp: app}, level: PermissionRead, target: "${{ github.repository }}"},
		{name: "static wiki", memory: RepoMemoryEntry{ID: "notes", TargetRepo: "owner/repo", Wiki: true, GitHubApp: app}, level: PermissionWrite, target: "owner/repo.wiki"},
		{name: "expression without app", memory: RepoMemoryEntry{ID: "notes", TargetRepo: "${{ inputs.repo }}"}, level: PermissionRead, target: "${{ inputs.repo }}"},
		{name: "read override", memory: RepoMemoryEntry{ID: "notes", TargetRepo: "${{ inputs.repo }}", GitHubAppRead: app}, level: PermissionRead, target: "${{ steps.repo-memory-app-read-target-notes.outputs.owner }}/${{ steps.repo-memory-app-read-target-notes.outputs.repository }}", condition: "steps.repo-memory-app-read-target-notes.outcome == 'success'"},
		{name: "write override with wiki", memory: RepoMemoryEntry{ID: "notes", TargetRepo: "${{ inputs.repo }}", Wiki: true, GitHubAppWrite: app}, level: PermissionWrite, target: "${{ steps.repo-memory-app-target-notes.outputs.owner }}/${{ steps.repo-memory-app-target-notes.outputs.repository }}.wiki", condition: "steps.repo-memory-app-target-notes.outcome == 'success'"},
		{name: "opposite override", memory: RepoMemoryEntry{ID: "notes", TargetRepo: "${{ inputs.repo }}", GitHubAppWrite: app}, level: PermissionRead, target: "${{ inputs.repo }}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.target, repoMemoryTargetRepo(tc.memory, tc.level))
			assert.Equal(t, tc.condition, repoMemoryAppTargetCondition(tc.memory, tc.level))
		})
	}
}

func TestRepoMemoryAppReadExpressionGatesCloneAndMint(t *testing.T) {
	memory := newDefaultRepoMemoryEntry("test", "memory")
	memory.TargetRepo = "${{ inputs.repo }}"
	memory.GitHubAppRead = &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}"}
	data := &WorkflowData{RepoMemoryConfig: &RepoMemoryConfig{Memories: []RepoMemoryEntry{memory}}}

	var steps strings.Builder
	NewCompiler().generateRepoMemorySteps(&steps, data, "agent")
	generated := steps.String()
	condition := "steps.repo-memory-app-read-target-default.outcome == 'success'"
	require.Equal(t, 2, strings.Count(generated, condition), "both mint and clone must be gated on target resolution")
	assert.Contains(t, generated, "TARGET_REPO: ${{ steps.repo-memory-app-read-target-default.outputs.owner }}/${{ steps.repo-memory-app-read-target-default.outputs.repository }}")
	assert.Contains(t, generated, "GH_TOKEN: ${{ steps.repo-memory-app-read-token-default.outputs.token }}")
}
