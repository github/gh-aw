package workflow

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLedgerMutationSafeOutputConfig(t *testing.T) {
	withLedger := &RepoMemoryConfig{Memories: []RepoMemoryEntry{{ID: "events", Ledger: &RepoMemoryLedgerConfig{}}}}
	withoutLedger := &RepoMemoryConfig{Memories: []RepoMemoryEntry{{ID: "notes"}}}

	assert.Nil(t, buildLedgerMutationHandlerConfig(nil))
	assert.Nil(t, buildLedgerMutationHandlerConfig(withoutLedger))
	assert.Equal(t, map[string]any{"max": LedgerMutationDefaultMax}, buildLedgerMutationHandlerConfig(withLedger))

	configJSON, err := generateSafeOutputsConfig(&WorkflowData{
		SafeOutputs:      &SafeOutputsConfig{},
		RepoMemoryConfig: withLedger,
	})
	require.NoError(t, err)
	var config map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config))
	assert.Contains(t, config, "ledger_mutation")

	configJSON, err = generateSafeOutputsConfig(&WorkflowData{
		SafeOutputs:      &SafeOutputsConfig{},
		RepoMemoryConfig: withoutLedger,
	})
	require.NoError(t, err)
	config = map[string]any{}
	if configJSON != "" {
		require.NoError(t, json.Unmarshal([]byte(configJSON), &config))
	}
	assert.NotContains(t, config, "ledger_mutation")
}

func TestLedgerMutationHandlerManagerConfig(t *testing.T) {
	compiler := NewCompiler()
	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, &WorkflowData{
		SafeOutputs:      &SafeOutputsConfig{},
		RepoMemoryConfig: &RepoMemoryConfig{Memories: []RepoMemoryEntry{{ID: "events", Ledger: &RepoMemoryLedgerConfig{}}}},
	})
	require.NotEmpty(t, steps)
	assert.Contains(t, steps[0], "ledger_mutation")
}

func TestLedgerMutationValidationConfig(t *testing.T) {
	config, ok := ValidationConfig["ledger_mutation"]
	require.True(t, ok)
	assert.Equal(t, LedgerMutationDefaultMax, config.DefaultMax)
	assert.True(t, config.Fields["operation"].Required)
	assert.Equal(t, []string{"append"}, config.Fields["operation"].Enum)
	assert.True(t, config.Fields["record"].Required)

	configJSON, err := GetValidationConfigJSONWithDataSchema([]string{"ledger_mutation"}, nil, false, nil)
	require.NoError(t, err)
	assert.Contains(t, configJSON, "ledger_mutation")
}
