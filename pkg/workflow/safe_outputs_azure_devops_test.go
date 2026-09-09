//go:build !integration

package workflow

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractAzureDevOpsSafeOutputsConfig(t *testing.T) {
	compiler := NewCompiler()
	config := compiler.extractSafeOutputsConfig(map[string]any{
		"safe-outputs": map[string]any{
			"ado-create-work-item": map[string]any{
				"work-item-type": "Bug",
				"area-path":      `Project\Platform`,
				"allowed-tags":   []any{"agent-*"},
				"samples": []any{
					map[string]any{"title": "Sample item"},
				},
			},
			"ado-update-work-item": map[string]any{
				"target": "42",
				"title":  true,
			},
			"ado-comment-on-work-item":       map[string]any{"target": "*"},
			"ado-assign-work-item":           map[string]any{},
			"ado-link-work-items":            map[string]any{"target": "*"},
			"ado-upload-workitem-attachment": map[string]any{},
		},
	})

	require.NotNil(t, config)
	require.NotNil(t, config.CreateWorkItems)
	assert.Equal(t, "Bug", config.CreateWorkItems.WorkItemType)
	assert.Equal(t, `Project\Platform`, config.CreateWorkItems.AreaPath)
	assert.Equal(t, []string{"agent-*"}, config.CreateWorkItems.AllowedTags)
	require.Len(t, config.CreateWorkItems.Samples, 1)
	assert.Equal(t, "Sample item", config.CreateWorkItems.Samples[0]["title"])
	require.NotNil(t, config.UpdateWorkItems)
	assert.Equal(t, "42", config.UpdateWorkItems.Target)
	assert.True(t, config.UpdateWorkItems.Title)
	assert.NotNil(t, config.CommentOnWorkItems)
	assert.NotNil(t, config.AssignWorkItems)
	assert.NotNil(t, config.LinkWorkItems)
	assert.NotNil(t, config.UploadWorkItemAttachments)
}

func TestAzureDevOpsSafeOutputsUseAdoAwPublicToolNames(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreateWorkItems:           &CreateWorkItemConfig{},
			UpdateWorkItems:           &UpdateWorkItemConfig{},
			CommentOnWorkItems:        &CommentOnWorkItemConfig{},
			AssignWorkItems:           &AssignWorkItemConfig{},
			LinkWorkItems:             &LinkWorkItemsConfig{},
			UploadWorkItemAttachments: &UploadWorkItemAttachmentConfig{},
		},
	}

	enabled := computeEnabledToolNames(data)

	for _, name := range []string{
		"ado_create_work_item",
		"ado_update_work_item",
		"ado_comment_on_work_item",
		"ado_assign_work_item",
		"ado_link_work_items",
		"ado_upload_workitem_attachment",
	} {
		assert.Contains(t, enabled, name)
	}
}

func TestGenerateAzureDevOpsSafeOutputsConfig(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreateWorkItems: &CreateWorkItemConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("2")},
				WorkItemType:         "Task",
				AreaPath:             `Project\Platform`,
			},
			UpdateWorkItems: &UpdateWorkItemConfig{
				Target: "42",
				Title:  true,
			},
		},
	}

	result, err := generateSafeOutputsConfig(data)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	createConfig := parsed["ado_create_work_item"].(map[string]any)
	assert.InDelta(t, 2, createConfig["max"], 0)
	assert.Equal(t, "Task", createConfig["work_item_type"])
	assert.Equal(t, `Project\Platform`, createConfig["area_path"])
	assert.Equal(t, map[string]any{}, createConfig["artifact_link"])

	updateConfig := parsed["ado_update_work_item"].(map[string]any)
	assert.Equal(t, "42", updateConfig["target"])
	assert.Equal(t, true, updateConfig["title"])
	assert.NotContains(t, updateConfig, "status")
}

func TestAzureDevOpsToolDescriptionConstraints(t *testing.T) {
	config := &SafeOutputsConfig{
		CreateWorkItems: &CreateWorkItemConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("2")},
			WorkItemType:         "Bug",
			AreaPath:             `Project\Platform`,
		},
	}

	constraints := toolConstraintBuilders["ado_create_work_item"](config)

	assert.Contains(t, constraints, "Maximum 2 work item(s) can be created.")
	assert.Contains(t, constraints, `Work item type: "Bug".`)
	assert.Contains(t, constraints, `Area path: "Project\\Platform".`)
}
