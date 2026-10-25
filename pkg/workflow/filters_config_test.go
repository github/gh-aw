//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPullRequestForkFilterTypedNilMap(t *testing.T) {
	var prConfig map[string]any
	frontmatter := map[string]any{"on": map[string]any{"pull_request": prConfig}}
	data := &WorkflowData{}
	NewCompiler().applyPullRequestForkFilter(data, frontmatter)
	assert.Equal(t, "github.event_name != 'pull_request' || github.event.pull_request.head.repo.id == github.repository_id", data.If)
}

func TestPullRequestFilterConfigUsesParsedOn(t *testing.T) {
	data := &WorkflowData{ParsedFrontmatter: &FrontmatterConfig{
		On: map[string]any{"pull_request": map[string]any{"draft": true}},
	}}
	frontmatter := map[string]any{"on": map[string]any{
		"pull_request": map[string]any{"draft": false},
	}}
	config, exists := pullRequestFilterConfig(data, frontmatter)
	assert.True(t, exists)
	assert.Equal(t, true, config["draft"])
}
