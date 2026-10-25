//go:build !integration

package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitialWorkflowCheckoutPreservesImportOrder(t *testing.T) {
	result := &parser.FrontmatterResult{Frontmatter: map[string]any{
		"checkout":    map[string]any{"repository": "owner/main", "path": "main"},
		"permissions": map[string]any{"contents": "none"},
	}}
	imports := &parser.ImportsResult{
		MergedCheckout: "{\"repository\":\"owner/first\",\"path\":\"first\"}\n\n{\"repository\":\"owner/second\",\"path\":\"second\"}",
	}
	var data WorkflowData
	err := populateInitialWorkflowCheckout(&data, result, &toolsProcessingResult{}, imports)
	require.NoError(t, err)
	require.Len(t, data.CheckoutConfigs, 3)
	assert.Equal(t, "owner/main", data.CheckoutConfigs[0].Repository)
	assert.Equal(t, "owner/first", data.CheckoutConfigs[1].Repository)
	assert.Equal(t, "owner/second", data.CheckoutConfigs[2].Repository)
	assert.True(t, data.CheckoutSkipDefault)
}

func TestInitialWorkflowCheckoutDisabledSkipsInvalidImports(t *testing.T) {
	result := &parser.FrontmatterResult{Frontmatter: map[string]any{"checkout": false}}
	imports := &parser.ImportsResult{MergedCheckout: "invalid JSON"}
	var data WorkflowData
	err := populateInitialWorkflowCheckout(&data, result, &toolsProcessingResult{}, imports)
	require.NoError(t, err)
	assert.True(t, data.CheckoutDisabled)
	assert.True(t, data.CheckoutExplicitlyDisabled)
	assert.Nil(t, data.CheckoutConfigs)
}

func TestInitialWorkflowCheckoutUsesParsedConfiguration(t *testing.T) {
	result := &parser.FrontmatterResult{Frontmatter: map[string]any{
		"checkout":    false,
		"permissions": map[string]any{"contents": "none"},
	}}
	configs := []*CheckoutConfig{{Repository: "owner/parsed"}}
	tools := &toolsProcessingResult{parsedFrontmatter: &FrontmatterConfig{
		CheckoutConfigs: configs,
	}}
	var data WorkflowData
	err := populateInitialWorkflowCheckout(&data, result, tools, &parser.ImportsResult{})
	require.NoError(t, err)
	assert.Equal(t, configs, data.CheckoutConfigs)
	assert.False(t, data.CheckoutDisabled)
	assert.False(t, data.CheckoutSkipDefault)
}

func TestInitialWorkflowCheckoutErrorsPreservePartialMerge(t *testing.T) {
	result := &parser.FrontmatterResult{Frontmatter: map[string]any{}}
	imports := &parser.ImportsResult{
		MergedCheckout: "{\"repository\":\"owner/first\"}\ninvalid JSON",
	}
	var data WorkflowData
	err := populateInitialWorkflowCheckout(&data, result, &toolsProcessingResult{}, imports)
	require.ErrorContains(t, err, "invalid imported checkout JSON:")
	require.Len(t, data.CheckoutConfigs, 1)
	assert.Equal(t, "owner/first", data.CheckoutConfigs[0].Repository)
}

func TestInitialWorkflowCheckoutFinalizationAfterImports(t *testing.T) {
	result := &parser.FrontmatterResult{Frontmatter: map[string]any{"on": "pull_request_target"}}
	var data WorkflowData
	imports := &parser.ImportsResult{MergedCheckout: "{\"repository\":\"owner/imported\"}"}
	err := populateInitialWorkflowCheckout(&data, result, &toolsProcessingResult{}, imports)
	require.NoError(t, err)
	NewCompiler().finalizeInitialWorkflowCheckout(&data, result)
	assert.True(t, data.IsPullRequestTarget)
	assert.False(t, data.CheckoutDisabled)
}
