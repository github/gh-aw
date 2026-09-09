//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadRepoConfig_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "missing aw.json should return default config without error")
	assert.False(t, cfg.MaintenanceDisabled, "maintenance should be enabled by default")
	assert.Nil(t, cfg.Maintenance, "maintenance config should be nil when file is absent")
}

func TestLoadRepoConfig_MaintenanceFalse(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"maintenance": false}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "valid aw.json should load without error")
	assert.True(t, cfg.MaintenanceDisabled, "maintenance should be disabled")
	assert.Nil(t, cfg.Maintenance, "maintenance config should be nil when disabled")
}

func TestLoadRepoConfig_MaintenanceWithStringRunsOn(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"maintenance": {"runs_on": "custom-runner"}}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "valid aw.json should load without error")
	assert.False(t, cfg.MaintenanceDisabled, "maintenance should not be disabled")
	require.NotNil(t, cfg.Maintenance, "maintenance config should be set")
	assert.Equal(t, RunsOnValue{"custom-runner"}, cfg.Maintenance.RunsOn, "string runs_on should be normalised to a single-element RunsOnValue")
}

func TestLoadRepoConfig_MaintenanceWithArrayRunsOn(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"maintenance": {"runs_on": ["self-hosted", "linux"]}}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "valid aw.json should load without error")
	assert.False(t, cfg.MaintenanceDisabled, "maintenance should not be disabled")
	require.NotNil(t, cfg.Maintenance, "maintenance config should be set")
	assert.Equal(t, RunsOnValue{"self-hosted", "linux"}, cfg.Maintenance.RunsOn, "array runs_on should be deserialised as RunsOnValue")
}

func TestLoadRepoConfig_EmptyObject(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "empty aw.json should load without error")
	assert.False(t, cfg.MaintenanceDisabled, "maintenance should be enabled by default")
	assert.Nil(t, cfg.Maintenance, "maintenance config should be nil when not specified")
	assert.True(t, cfg.IsHelpCommandEnabled(), "help command should be enabled by default")
}

func TestLoadRepoConfig_StrictTrue(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"strict": true}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "aw.json should accept strict: true")
	assert.True(t, cfg.Strict, "strict mode should be enforced")
}

func TestLoadRepoConfig_StrictFalseRejected(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"strict": false}`)

	_, err := LoadRepoConfig(dir)
	assert.Error(t, err, "aw.json should reject strict: false")
}

func TestLoadRepoConfig_HelpCommandFalse(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"help_command": false}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "valid aw.json should load without error")
	require.NotNil(t, cfg.HelpCommand, "help_command should be set")
	assert.False(t, *cfg.HelpCommand, "help_command should be false when explicitly set")
	assert.False(t, cfg.IsHelpCommandEnabled(), "help command should be disabled when help_command is false")
}

func TestLoadRepoConfig_HelpCommandTrue(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"help_command": true}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "valid aw.json should load without error")
	require.NotNil(t, cfg.HelpCommand, "help_command should be set")
	assert.True(t, *cfg.HelpCommand, "help_command should be true when explicitly set")
	assert.True(t, cfg.IsHelpCommandEnabled(), "help command should be enabled when help_command is true")
}

func TestLoadRepoConfig_MaintenanceEmptyObject(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"maintenance": {}}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "aw.json with empty maintenance object should load without error")
	assert.False(t, cfg.MaintenanceDisabled, "maintenance should not be disabled")
	require.NotNil(t, cfg.Maintenance, "maintenance config should be set")
	assert.Empty(t, cfg.Maintenance.RunsOn, "runs_on should be empty when not specified")
}

func TestLoadRepoConfig_ActionFailureIssueExpires(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"maintenance": {"action_failure_issue_expires": 72}}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "valid aw.json should load without error")
	require.NotNil(t, cfg.Maintenance, "maintenance config should be set")
	assert.Equal(t, 72, cfg.Maintenance.ActionFailureIssueExpires, "action_failure_issue_expires should be parsed from aw.json")
	assert.Equal(t, 72, cfg.ActionFailureIssueExpiresHours(), "accessor should return configured expiration")
	assert.True(t, cfg.IsActionFailureIssueExpiresExplicit(), "explicit action_failure_issue_expires should be flagged as explicit")
}

func TestLoadRepoConfig_ActionFailureIssueExpiresNotExplicitWhenUnset(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"maintenance": {"runs_on": "ubuntu-latest"}}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "valid aw.json should load without error")
	require.NotNil(t, cfg.Maintenance, "maintenance config should be set")
	assert.Equal(t, DefaultActionFailureIssueExpiresHours, cfg.ActionFailureIssueExpiresHours(), "accessor should fall back to default")
	assert.False(t, cfg.IsActionFailureIssueExpiresExplicit(), "action_failure_issue_expires should not be flagged explicit when absent from aw.json")
}

func TestLoadRepoConfig_MaintenanceCompileConfig(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"maintenance": {"compile": {"create_pull_request_github_token": "MAINTENANCE_TOKEN"}}}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "valid aw.json should load without error")
	require.NotNil(t, cfg.Maintenance, "maintenance config should be set")
	require.NotNil(t, cfg.Maintenance.Compile, "compile config should be set")
	assert.Equal(t, "MAINTENANCE_TOKEN", cfg.Maintenance.Compile.CreatePullRequestGitHubToken)
}

func TestLoadRepoConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	writeAWJSONRaw(t, dir, `not-json`)

	_, err := LoadRepoConfig(dir)
	assert.Error(t, err, "invalid JSON should return an error")
}

func TestLoadRepoConfig_SchemaViolation(t *testing.T) {
	dir := t.TempDir()
	// "maintenance: true" is not allowed by the schema (only false or object)
	writeAWJSON(t, dir, `{"maintenance": true}`)

	_, err := LoadRepoConfig(dir)
	assert.Error(t, err, "schema violation should return an error")
}

func TestLoadRepoConfig_ContainerPinsRequireSHA256Digest(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tests := []struct {
		name    string
		mapping string
		wantErr bool
	}{
		{
			name:    "object with valid image and digest accepted",
			mapping: `{"image":"registry.acme.com/image:v1","digest":"sha256:` + digest + `"}`,
		},
		{
			name:    "missing digest field rejected",
			mapping: `{"image":"registry.acme.com/image:v1"}`,
			wantErr: true,
		},
		{
			name:    "missing image field rejected",
			mapping: `{"digest":"sha256:` + digest + `"}`,
			wantErr: true,
		},
		{
			name:    "short digest rejected",
			mapping: `{"image":"registry.acme.com/image:v1","digest":"sha256:abc123"}`,
			wantErr: true,
		},
		{
			name:    "digest without sha256 prefix rejected",
			mapping: `{"image":"registry.acme.com/image:v1","digest":"` + digest + `"}`,
			wantErr: true,
		},
		{
			name:    "image with digest component rejected",
			mapping: `{"image":"registry.acme.com/image:v1@sha256:` + digest + `","digest":"sha256:` + digest + `"}`,
			wantErr: true,
		},
		{
			name:    "flat string value rejected",
			mapping: `"registry.acme.com/image:v1@sha256:` + digest + `"`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeAWJSON(t, dir, `{"container_pins":{"ghcr.io/owner/image:v1":`+tt.mapping+`}}`)

			_, err := LoadRepoConfig(dir)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestLoadRepoConfig_ContainerPinsKeyNoDigestAllowed verifies that container_pins
// keys (source images) must not contain a digest — only tag-based references are
// valid source keys. A key like "image@sha256:..." is rejected by the schema.
func TestLoadRepoConfig_ContainerPinsKeyNoDigestAllowed(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	const goodValue = `{"image":"registry.acme.com/image:v1","digest":"sha256:` + digest + `"}`

	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{
			name: "plain image:tag key accepted",
			key:  `"ghcr.io/owner/image:v1"`,
		},
		{
			name: "image without tag accepted",
			key:  `"alpine"`,
		},
		{
			name:    "digest-pinned key rejected",
			key:     `"ghcr.io/owner/image:v1@sha256:` + digest + `"`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeAWJSON(t, dir, `{"container_pins":{`+tt.key+`:`+goodValue+`}}`)

			_, err := LoadRepoConfig(dir)
			if tt.wantErr {
				require.Error(t, err, "digest-pinned source keys should be rejected by schema")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestLoadRepoConfig_ContainerPinsObjectFields verifies that the parsed
// ContainerPins map stores the image and digest fields separately.
func TestLoadRepoConfig_ContainerPinsObjectFields(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"container_pins":{"ghcr.io/owner/image:v1":{"image":"registry.acme.com/image:v1","digest":"sha256:`+digest+`"}}}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err)
	require.Len(t, cfg.ContainerPins, 1)
	target := cfg.ContainerPins["ghcr.io/owner/image:v1"]
	assert.Equal(t, "registry.acme.com/image:v1", target.Image)
	assert.Equal(t, "sha256:"+digest, target.Digest)
}

func TestLoadRepoConfig_LabelTriggersDisable(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"maintenance": {"label_triggers": false}}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "valid aw.json should load without error")
	require.NotNil(t, cfg.Maintenance, "maintenance config should be set")
	require.NotNil(t, cfg.Maintenance.LabelTriggers, "label_triggers should be set")
	assert.False(t, *cfg.Maintenance.LabelTriggers, "label_triggers should be false when explicitly set")
	assert.False(t, cfg.Maintenance.IsLabelTriggerEnabled(), "setting label_triggers: false explicitly opts out — label-triggered jobs should not be included")
}

func TestLoadRepoConfig_LabelTriggers_DefaultFalse(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"maintenance": {}}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "valid aw.json should load without error")
	require.NotNil(t, cfg.Maintenance, "maintenance config should be set")
	assert.Nil(t, cfg.Maintenance.LabelTriggers, "label_triggers should be nil when not specified")
	assert.False(t, cfg.Maintenance.IsLabelTriggerEnabled(), "label triggers should be disabled by default (nil = false)")
}

func TestLoadRepoConfig_LabelTriggers_ExplicitTrue(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"maintenance": {"label_triggers": true}}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "valid aw.json should load without error")
	require.NotNil(t, cfg.Maintenance, "maintenance config should be set")
	require.NotNil(t, cfg.Maintenance.LabelTriggers, "label_triggers should be set")
	assert.True(t, *cfg.Maintenance.LabelTriggers, "label_triggers should be true when explicitly set")
	assert.True(t, cfg.Maintenance.IsLabelTriggerEnabled(), "label_triggers: true keeps label-triggered jobs enabled")
}

func TestLoadRepoConfig_DisabledJobs(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"maintenance": {"disabled_jobs": ["close-expired-entities", "label-apply-safe-outputs"]}}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "valid aw.json should load without error")
	require.NotNil(t, cfg.Maintenance, "maintenance config should be set")
	require.Len(t, cfg.Maintenance.DisabledJobs, 2, "disabled_jobs should be parsed")
	assert.True(t, cfg.Maintenance.IsJobDisabled("close-expired-entities"), "hyphenated job name should match")
	assert.True(t, cfg.Maintenance.IsJobDisabled("label_apply_safe_outputs"), "underscored lookup should match hyphen/underscore equivalently")
	assert.False(t, cfg.Maintenance.IsJobDisabled("close.expired.entities"), "periods should not match hyphenated job names")
	assert.False(t, cfg.Maintenance.IsJobDisabled("create_labels"), "unlisted jobs should remain enabled")
}

func TestLoadRepoConfig_DisabledJobsRejectsInvalidOrDuplicateValues(t *testing.T) {
	tests := []struct {
		name     string
		awJSON   string
		contains string
	}{
		{
			name:     "literal duplicate rejected by schema",
			awJSON:   `{"maintenance": {"disabled_jobs": ["apply_safe_outputs", "apply_safe_outputs"]}}`,
			contains: "disabled_jobs",
		},
		{
			name:     "normalization-equivalent duplicate rejected",
			awJSON:   `{"maintenance": {"disabled_jobs": ["close-expired-entities", "close_expired_entities"]}}`,
			contains: "duplicate maintenance.disabled_jobs entries",
		},
		{
			name:     "unknown job rejected",
			awJSON:   `{"maintenance": {"disabled_jobs": ["apply_safe_outputz"]}}`,
			contains: "unrecognized maintenance.disabled_jobs entry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeAWJSON(t, dir, tt.awJSON)

			_, err := LoadRepoConfig(dir)
			require.Error(t, err)
			require.ErrorContains(t, err, tt.contains)
		})
	}
}

// TestLoadRepoConfig_UnknownProperty tests that unknown properties are rejected.
func TestLoadRepoConfig_UnknownProperty(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"unknown_property": "value"}`)

	_, err := LoadRepoConfig(dir)
	assert.Error(t, err, "unknown property should fail schema validation (additionalProperties: false)")
}

func TestLoadRepoConfig_InvalidActionFailureIssueExpires(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"maintenance": {"action_failure_issue_expires": 0}}`)

	_, err := LoadRepoConfig(dir)
	assert.Error(t, err, "action_failure_issue_expires must be >= 1")
}

func TestLoadRepoConfig_InvalidMaintenanceCompileGitHubTokenSecret(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"maintenance": {"compile": {"create_pull_request_github_token": "bad-secret"}}}`)

	_, err := LoadRepoConfig(dir)
	assert.Error(t, err, "create_pull_request_github_token must be a valid secret name")
}

func TestLoadRepoConfig_GHESTrue(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"ghes": true}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "valid aw.json with ghes: true should load without error")
	assert.True(t, cfg.GHES, "GHES should be true when set in aw.json")
}

func TestLoadRepoConfig_GHESFalse(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"ghes": false}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "valid aw.json with ghes: false should load without error")
	assert.False(t, cfg.GHES, "GHES should be false when explicitly set to false")
}

func TestLoadRepoConfig_GHESDefault(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err)
	assert.False(t, cfg.GHES, "GHES should default to false when not present in aw.json")
}

func TestLoadRepoConfig_GHESWithMaintenance(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"ghes": true, "maintenance": {"runs_on": "self-hosted"}}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "aw.json with ghes and maintenance should load without error")
	assert.True(t, cfg.GHES, "GHES should be true")
	require.NotNil(t, cfg.Maintenance, "maintenance config should be set")
	assert.Equal(t, RunsOnValue{"self-hosted"}, cfg.Maintenance.RunsOn)
}

func TestLoadRepoConfig_UTC(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"utc": "-08:00"}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "valid aw.json with utc should load without error")
	assert.Equal(t, "-08:00", cfg.UTC)
}

func TestLoadRepoConfig_InvalidUTC(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"utc": "+14:30"}`)

	_, err := LoadRepoConfig(dir)
	require.Error(t, err, "invalid timezone should return an error")
	require.ErrorContains(t, err, "must be a numeric UTC offset")
}

// TestFormatRunsOn tests the YAML serialisation of runs-on values.
func TestFormatRunsOn(t *testing.T) {
	const def = "ubuntu-slim"

	tests := []struct {
		name     string
		runsOn   RunsOnValue
		expected string
	}{
		{"nil uses default", nil, def},
		{"empty slice uses default", RunsOnValue{}, def},
		{"empty string element uses default", RunsOnValue{""}, def},
		{"single label", RunsOnValue{"custom-runner"}, "custom-runner"},
		{"single self-hosted label", RunsOnValue{"self-hosted"}, "self-hosted"},
		{"multi-label array", RunsOnValue{"self-hosted", "linux"}, `["self-hosted","linux"]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatRunsOn(tt.runsOn, def)
			assert.Equal(t, tt.expected, got, "FormatRunsOn should return expected YAML value")
		})
	}
}

func TestActionFailureIssueExpiresHours_Default(t *testing.T) {
	cfg := &RepoConfig{}
	assert.Equal(t, DefaultActionFailureIssueExpiresHours, cfg.ActionFailureIssueExpiresHours(), "default should be returned when aw.json does not set action_failure_issue_expires")
}

func TestLoadRepoConfig_AutoUpgradeEnabled(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"auto_upgrade": true}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "valid aw.json should load without error")
	require.NotNil(t, cfg.AutoUpgrade, "auto_upgrade should be set")
	assert.True(t, *cfg.AutoUpgrade, "auto_upgrade should be true")
	assert.True(t, cfg.IsAutoUpgradeEnabled(), "IsAutoUpgradeEnabled should return true")
}

func TestLoadRepoConfig_AutoUpgradeDisabled(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{"auto_upgrade": false}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "valid aw.json should load without error")
	require.NotNil(t, cfg.AutoUpgrade, "auto_upgrade should be set")
	assert.False(t, *cfg.AutoUpgrade, "auto_upgrade should be false")
	assert.False(t, cfg.IsAutoUpgradeEnabled(), "IsAutoUpgradeEnabled should return false")
}

func TestLoadRepoConfig_AutoUpgradeOmitted(t *testing.T) {
	dir := t.TempDir()
	writeAWJSON(t, dir, `{}`)

	cfg, err := LoadRepoConfig(dir)
	require.NoError(t, err, "valid aw.json should load without error")
	assert.Nil(t, cfg.AutoUpgrade, "auto_upgrade should be nil when omitted")
	assert.False(t, cfg.IsAutoUpgradeEnabled(), "IsAutoUpgradeEnabled should return false when omitted (opt-in)")
}

func TestIsAutoUpgradeEnabled_NilConfig(t *testing.T) {
	var r *RepoConfig
	assert.False(t, r.IsAutoUpgradeEnabled(), "IsAutoUpgradeEnabled should return false for nil RepoConfig")
}

func TestLoadRepoConfig_AutoUpgradeCron(t *testing.T) {
	t.Run("object form with cron enables auto_upgrade", func(t *testing.T) {
		dir := t.TempDir()
		writeAWJSON(t, dir, `{"auto_upgrade": {"cron": "0 9 * * 1"}}`)

		cfg, err := LoadRepoConfig(dir)
		require.NoError(t, err, "valid aw.json with auto_upgrade object should load without error")
		require.NotNil(t, cfg.AutoUpgrade, "auto_upgrade should be set")
		assert.True(t, *cfg.AutoUpgrade, "auto_upgrade object form should imply enabled")
		assert.True(t, cfg.IsAutoUpgradeEnabled(), "IsAutoUpgradeEnabled should return true for object form")
		assert.Equal(t, "0 9 * * 1", cfg.AutoUpgradeCron, "cron should be set from nested field")
	})

	t.Run("object form without cron uses fuzzy schedule", func(t *testing.T) {
		dir := t.TempDir()
		writeAWJSON(t, dir, `{"auto_upgrade": {}}`)

		cfg, err := LoadRepoConfig(dir)
		require.NoError(t, err)
		require.NotNil(t, cfg.AutoUpgrade, "auto_upgrade should be set")
		assert.True(t, *cfg.AutoUpgrade, "empty object should imply enabled")
		assert.Empty(t, cfg.AutoUpgradeCron, "AutoUpgradeCron should be empty when cron is omitted")
	})

	t.Run("object form loads upgrade options", func(t *testing.T) {
		dir := t.TempDir()
		writeAWJSON(t, dir, `{"auto_upgrade": {"options": ["--pre-releases"]}}`)

		cfg, err := LoadRepoConfig(dir)
		require.NoError(t, err)
		assert.Equal(t, []string{"--pre-releases"}, cfg.AutoUpgradeOptions)
	})

	t.Run("rejects unsupported upgrade options", func(t *testing.T) {
		dir := t.TempDir()
		writeAWJSON(t, dir, `{"auto_upgrade": {"options": ["--unknown"]}}`)

		_, err := LoadRepoConfig(dir)
		assert.Error(t, err, "unsupported auto-upgrade options should return an error")
	})

	t.Run("boolean true has no cron", func(t *testing.T) {
		dir := t.TempDir()
		writeAWJSON(t, dir, `{"auto_upgrade": true}`)

		cfg, err := LoadRepoConfig(dir)
		require.NoError(t, err)
		assert.Empty(t, cfg.AutoUpgradeCron, "AutoUpgradeCron should be empty when using boolean form")
	})

	t.Run("rejects invalid cron pattern", func(t *testing.T) {
		dir := t.TempDir()
		writeAWJSON(t, dir, `{"auto_upgrade": {"cron": "not-a-cron"}}`)

		// Invalid cron is rejected by JSON schema validation in LoadRepoConfig.
		_, err := LoadRepoConfig(dir)
		assert.Error(t, err, "invalid cron in auto_upgrade object should return an error")
	})

	t.Run("rejects out-of-range cron values", func(t *testing.T) {
		dir := t.TempDir()
		writeAWJSON(t, dir, `{"auto_upgrade": {"cron": "99 99 99 99 99"}}`)

		_, err := LoadRepoConfig(dir)
		assert.Error(t, err, "out-of-range cron values should return an error")
	})

	t.Run("rejects six-field cron", func(t *testing.T) {
		dir := t.TempDir()
		writeAWJSON(t, dir, `{"auto_upgrade": {"cron": "0 0 * * * *"}}`)

		_, err := LoadRepoConfig(dir)
		assert.Error(t, err, "six-field cron should return an error")
	})
}

func TestLoadRepoConfig_ActionPins(t *testing.T) {
	t.Run("loads action_pins mapping", func(t *testing.T) {
		dir := t.TempDir()
		writeAWJSON(t, dir, `{"action_pins": {"actions/checkout@v4": "acme-corp/checkout@v4"}}`)

		cfg, err := LoadRepoConfig(dir)
		require.NoError(t, err, "valid aw.json with action_pins should load without error")
		require.NotNil(t, cfg.ActionPins, "action_pins should be populated")
		assert.Equal(t, "acme-corp/checkout@v4", cfg.ActionPins["actions/checkout@v4"])
	})

	t.Run("allows multiple mappings", func(t *testing.T) {
		dir := t.TempDir()
		writeAWJSON(t, dir, `{"action_pins": {
			"actions/checkout@v4": "acme-corp/checkout@v4",
			"actions/setup-node@v4": "acme-corp/setup-node@v4"
		}}`)

		cfg, err := LoadRepoConfig(dir)
		require.NoError(t, err)
		assert.Len(t, cfg.ActionPins, 2)
		assert.Equal(t, "acme-corp/checkout@v4", cfg.ActionPins["actions/checkout@v4"])
		assert.Equal(t, "acme-corp/setup-node@v4", cfg.ActionPins["actions/setup-node@v4"])
	})

	t.Run("action_pins absent results in nil map", func(t *testing.T) {
		dir := t.TempDir()
		writeAWJSON(t, dir, `{}`)

		cfg, err := LoadRepoConfig(dir)
		require.NoError(t, err)
		assert.Nil(t, cfg.ActionPins, "action_pins should be nil when not specified")
	})

	t.Run("rejects key without version", func(t *testing.T) {
		dir := t.TempDir()
		writeAWJSON(t, dir, `{"action_pins": {"actions/checkout": "acme-corp/checkout@v4"}}`)

		_, err := LoadRepoConfig(dir)
		assert.Error(t, err, "key without @version should fail schema validation")
	})

	t.Run("rejects value without version", func(t *testing.T) {
		dir := t.TempDir()
		writeAWJSON(t, dir, `{"action_pins": {"actions/checkout@v4": "acme-corp/checkout"}}`)

		_, err := LoadRepoConfig(dir)
		assert.Error(t, err, "value without @version should fail schema validation")
	})
}

func TestValidateCronExpression(t *testing.T) {
	valid := []string{
		"0 9 * * 1",
		"30 5 * * 1-5",
		"0 0 * * 0",
		"*/15 * * * *",
		"0 0 1,15 * *",
		"59 23 31 12 7",
		"0 0 * * 1-5/2",
	}
	for _, expr := range valid {
		t.Run("valid: "+expr, func(t *testing.T) {
			assert.NoError(t, validateCronExpression(expr), "should accept %q", expr)
		})
	}

	invalid := []string{
		"not-a-cron",
		"99 99 99 99 99",
		"0 0 * * * *", // 6 fields
		"0 0 * *",     // 4 fields
		"60 0 * * *",  // minute out of range
		"0 24 * * *",  // hour out of range
		"0 0 0 * *",   // DOM 0 out of range
		"0 0 * 0 *",   // month 0 out of range
		"0 0 * 13 *",  // month 13 out of range
		"0 0 * * 8",   // DOW 8 out of range
		"0 0 * * 1/0", // step 0 invalid
	}
	for _, expr := range invalid {
		t.Run("invalid: "+expr, func(t *testing.T) {
			assert.Error(t, validateCronExpression(expr), "should reject %q", expr)
		})
	}
}

// writeAWJSON creates .github/workflows/aw.json with the given JSON content.
func writeAWJSON(t *testing.T, gitRoot, content string) {
	t.Helper()
	writeAWJSONRaw(t, gitRoot, content)
}

// writeAWJSONRaw creates .github/workflows/aw.json with raw (possibly invalid) content.
func writeAWJSONRaw(t *testing.T, gitRoot, content string) {
	t.Helper()
	dir := filepath.Join(gitRoot, ".github", "workflows")
	require.NoError(t, os.MkdirAll(dir, 0o755), "failed to create workflows dir")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "aw.json"), []byte(content), 0o600), "failed to write aw.json")
}
