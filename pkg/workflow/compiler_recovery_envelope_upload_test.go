//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRecoveryEnvelopeValidationFollowsAllSecretMasks(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{SecretMasking: &SecretMaskingConfig{Steps: []map[string]any{
		{"id": "custom_mask", "run": "echo masking"},
		{"run": "echo second mask"},
	}}}
	var output strings.Builder
	compiler.generateTrackedSecretRedactionStep(&output, "", data)
	content := output.String()
	assert.Less(t, strings.Index(content, "id: custom_mask"), strings.Index(content, "id: validate_recovery_envelopes"))
	assert.Less(t, strings.Index(content, "id: redact_secrets_custom_1"), strings.Index(content, "id: validate_recovery_envelopes"))
	assert.Contains(t, content, "if: always() && "+secretMaskingSuccessCondition(data))
	assert.Contains(t, content, "validate_recovery_envelopes.cjs")
	assert.Contains(t, secretRedactionSuccessCondition(data), "steps.validate_recovery_envelopes.outcome == 'success'")
}

func TestRecoveryEnvelopeValidationGatesStagingUploads(t *testing.T) {
	data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{
		UploadAssets:       &UploadAssetsConfig{},
		UploadArtifact:     &UploadArtifactConfig{},
		UploadCodeCoverage: &UploadCodeCoverageConfig{},
	}}
	pin := func(action string) string { return action + "@test-pin" }
	for _, generate := range []func(*strings.Builder, *WorkflowData, func(string) string){
		generateSafeOutputsAssetsArtifactUpload,
		generateSafeOutputsArtifactStagingUpload,
		generateSafeOutputsCodeCoverageStagingUpload,
	} {
		var output strings.Builder
		generate(&output, data, pin)
		assert.Contains(t, output.String(), "if: always() && "+secretRedactionSuccessCondition(data))
	}
}

func TestFinalRecoveryEnvelopeValidationGatesBothAgentUploads(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{CreatePullRequests: &CreatePullRequestsConfig{}}}
	var output strings.Builder
	compiler.generateAgentOutputFallbackUpload(&output, data, "", false)
	compiler.generateUnifiedArtifactUpload(&output, []string{"/tmp/gh-aw/aw-*.bundle"}, "", data, false)
	assert.Equal(t, 2, strings.Count(output.String(), "steps.validate_final_recovery_envelopes.outcome == 'success'"))
}

func TestRecoveryEnvelopeValidationGatesMemoryUploads(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs:      &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
		RepoMemoryConfig: &RepoMemoryConfig{Memories: []RepoMemoryEntry{{ID: "notes"}}},
		CacheMemoryConfig: &CacheMemoryConfig{Caches: []CacheMemoryEntry{
			{ID: "notes"},
		}},
		DriveMemoryConfig: &DriveMemoryConfig{Drives: []DriveMemoryEntry{{ID: "notes"}}},
	}

	pin := func(action string) string { return action + "@test-pin" }
	for _, test := range []struct {
		generate func(*strings.Builder, *WorkflowData, func(string) string)
		id       string
		root     string
	}{
		{generateRepoMemoryArtifactUpload, "validate_recovery_repo_notes", "/tmp/gh-aw/repo-memory/notes"},
		{generateCacheMemoryArtifactUpload, "validate_recovery_cache_notes", cacheMemoryDirFor("notes")},
		{generateDriveMemoryPersistence, "validate_recovery_drive_notes", "${{ github.workspace }}/" + driveMemoryMountPathFor("notes")},
	} {
		t.Run(test.id, func(t *testing.T) {
			var output strings.Builder
			test.generate(&output, data, pin)
			content := output.String()
			assert.Contains(t, content, "id: "+test.id+"\n")
			assert.Contains(t, content, "steps."+test.id+".outcome == 'success'")
			assert.Contains(t, content, "GH_AW_RECOVERY_ADDITIONAL_ROOTS: '[\""+test.root+"\"]'")
			assert.Contains(t, content, secretRedactionSuccessCondition(data))
		})
	}
}

func TestRecoveryArtifactScanRoots(t *testing.T) {
	assert.Equal(t, []string{
		"/tmp/gh-aw",
		"${{ runner.temp }}/gh-aw/claims",
		"${{ env.ARTIFACT_ROOT }}",
		"relative/custom",
	}, recoveryArtifactScanRoots([]string{
		"/tmp/gh-aw/aw-*.bundle",
		"${{ runner.temp }}/gh-aw/claims/*/aw-*.bundle",
		"${{ env.ARTIFACT_ROOT }}/**/*.bundle",
		"relative/custom/*.bundle",
		"!/tmp/gh-aw/excluded",
	}))
}
