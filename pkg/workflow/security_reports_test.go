//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestCodeScanningAlertsConfig tests the parsing of create-code-scanning-alert configuration
func TestCodeScanningAlertsConfig(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name           string
		frontmatter    map[string]any
		expectedConfig *CreateCodeScanningAlertsConfig
	}{
		{
			name: "basic code scanning alert configuration",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-code-scanning-alert": nil,
				},
			},
			expectedConfig: &CreateCodeScanningAlertsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 0}}, // 0 means unlimited
		},
		{
			name: "code scanning alert with max configuration",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-code-scanning-alert": map[string]any{
						"max": 50,
					},
				},
			},
			expectedConfig: &CreateCodeScanningAlertsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 50}},
		},
		{
			name: "code scanning alert with driver configuration",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-code-scanning-alert": map[string]any{
						"driver": "Custom Security Scanner",
					},
				},
			},
			expectedConfig: &CreateCodeScanningAlertsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 0}, Driver: "Custom Security Scanner"},
		},
		{
			name: "code scanning alert with max and driver configuration",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-code-scanning-alert": map[string]any{
						"max":    25,
						"driver": "Advanced Scanner",
					},
				},
			},
			expectedConfig: &CreateCodeScanningAlertsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 25}, Driver: "Advanced Scanner"},
		},
		{
			name: "no code scanning alert configuration",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-issue": nil,
				},
			},
			expectedConfig: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := compiler.extractSafeOutputsConfig(tt.frontmatter)

			if tt.expectedConfig == nil {
				if config == nil || config.CreateCodeScanningAlerts == nil {
					return // Expected no config
				}
				t.Errorf("Expected no CreateCodeScanningAlerts config, but got: %+v", config.CreateCodeScanningAlerts)
				return
			}

			if config == nil || config.CreateCodeScanningAlerts == nil {
				t.Errorf("Expected CreateCodeScanningAlerts config, but got nil")
				return
			}

			if config.CreateCodeScanningAlerts.Max != tt.expectedConfig.Max {
				t.Errorf("Expected Max=%d, got Max=%d", tt.expectedConfig.Max, config.CreateCodeScanningAlerts.Max)
			}

			if config.CreateCodeScanningAlerts.Driver != tt.expectedConfig.Driver {
				t.Errorf("Expected Driver=%s, got Driver=%s", tt.expectedConfig.Driver, config.CreateCodeScanningAlerts.Driver)
			}
		})
	}
}

// TestBuildCreateOutputCodeScanningAlertJob tests the creation of code scanning alert job
func TestBuildCreateOutputCodeScanningAlertJob(t *testing.T) {
	compiler := NewCompiler()

	// Test valid configuration
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreateCodeScanningAlerts: &CreateCodeScanningAlertsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 0}},
		},
	}

	job, err := compiler.buildCreateOutputCodeScanningAlertJob(data, "main_job", "test-workflow")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if job.Name != "create_code_scanning_alert" {
		t.Errorf("Expected job name 'create_code_scanning_alert', got '%s'", job.Name)
	}

	if job.TimeoutMinutes != 10 {
		t.Errorf("Expected timeout 10 minutes, got %d", job.TimeoutMinutes)
	}

	if len(job.Needs) != 1 || job.Needs[0] != "main_job" {
		t.Errorf("Expected dependency on 'main_job', got %v", job.Needs)
	}

	// Check that job has necessary permissions
	if !strings.Contains(job.Permissions, "security-events: write") {
		t.Errorf("Expected security-events: write permission in job, got: %s", job.Permissions)
	}

	// Check that steps include SARIF upload
	stepsStr := strings.Join(job.Steps, "")
	if !strings.Contains(stepsStr, "Upload SARIF") {
		t.Errorf("Expected SARIF upload steps in job")
	}

	if !strings.Contains(stepsStr, "codeql-action/upload-sarif") {
		t.Errorf("Expected CodeQL SARIF upload action in job")
	}

	// Test with max configuration
	dataWithMax := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreateCodeScanningAlerts: &CreateCodeScanningAlertsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 25}},
		},
	}

	jobWithMax, err := compiler.buildCreateOutputCodeScanningAlertJob(dataWithMax, "main_job", "test-workflow")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	stepsWithMaxStr := strings.Join(jobWithMax.Steps, "")
	if !strings.Contains(stepsWithMaxStr, "GH_AW_SECURITY_REPORT_MAX: 25") {
		t.Errorf("Expected max configuration in environment variables")
	}

	// Test with driver configuration
	dataWithDriver := &WorkflowData{
		Name:            "My Security Workflow",
		FrontmatterName: "My Security Workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreateCodeScanningAlerts: &CreateCodeScanningAlertsConfig{Driver: "Custom Scanner"},
		},
	}

	jobWithDriver, err := compiler.buildCreateOutputCodeScanningAlertJob(dataWithDriver, "main_job", "my-workflow")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	stepsWithDriverStr := strings.Join(jobWithDriver.Steps, "")
	if !strings.Contains(stepsWithDriverStr, "GH_AW_SECURITY_REPORT_DRIVER: Custom Scanner") {
		t.Errorf("Expected driver configuration in environment variables")
	}

	// Test with no driver configuration - should default to frontmatter name
	dataNoDriver := &WorkflowData{
		Name:            "Security Analysis Workflow",
		FrontmatterName: "Security Analysis Workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreateCodeScanningAlerts: &CreateCodeScanningAlertsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 0}}, // No driver specified
		},
	}

	jobNoDriver, err := compiler.buildCreateOutputCodeScanningAlertJob(dataNoDriver, "main_job", "security-analysis")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	stepsNoDriverStr := strings.Join(jobNoDriver.Steps, "")
	if !strings.Contains(stepsNoDriverStr, "GH_AW_SECURITY_REPORT_DRIVER: Security Analysis Workflow") {
		t.Errorf("Expected frontmatter name as default driver in environment variables, got: %s", stepsNoDriverStr)
	}

	// Test with no driver and no frontmatter name - should fallback to H1 name
	dataFallback := &WorkflowData{
		Name:            "Security Analysis",
		FrontmatterName: "", // No frontmatter name
		SafeOutputs: &SafeOutputsConfig{
			CreateCodeScanningAlerts: &CreateCodeScanningAlertsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 0}}, // No driver specified
		},
	}

	jobFallback, err := compiler.buildCreateOutputCodeScanningAlertJob(dataFallback, "main_job", "security-analysis")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	stepsFallbackStr := strings.Join(jobFallback.Steps, "")
	if !strings.Contains(stepsFallbackStr, "GH_AW_SECURITY_REPORT_DRIVER: Security Analysis") {
		t.Errorf("Expected H1 name as fallback driver in environment variables, got: %s", stepsFallbackStr)
	}

	// Check that workflow filename is passed
	if !strings.Contains(stepsWithDriverStr, "GH_AW_WORKFLOW_FILENAME: my-workflow") {
		t.Errorf("Expected workflow filename in environment variables")
	}

	// Test error case - no configuration
	dataNoConfig := &WorkflowData{SafeOutputs: nil}
	_, err = compiler.buildCreateOutputCodeScanningAlertJob(dataNoConfig, "main_job", "test-workflow")
	if err == nil {
		t.Errorf("Expected error when no SafeOutputs config provided")
	}
}

// TestParseCodeScanningAlertsConfig tests the parsing function directly
func TestParseCodeScanningAlertsConfig(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name           string
		outputMap      map[string]any
		expectedMax    int
		expectedDriver string
		expectNil      bool
	}{
		{
			name: "basic configuration",
			outputMap: map[string]any{
				"create-code-scanning-alert": nil,
			},
			expectedMax:    0,
			expectedDriver: "",
			expectNil:      false,
		},
		{
			name: "configuration with max",
			outputMap: map[string]any{
				"create-code-scanning-alert": map[string]any{
					"max": 100,
				},
			},
			expectedMax:    100,
			expectedDriver: "",
			expectNil:      false,
		},
		{
			name: "configuration with driver",
			outputMap: map[string]any{
				"create-code-scanning-alert": map[string]any{
					"driver": "Test Security Scanner",
				},
			},
			expectedMax:    0,
			expectedDriver: "Test Security Scanner",
			expectNil:      false,
		},
		{
			name: "configuration with max and driver",
			outputMap: map[string]any{
				"create-code-scanning-alert": map[string]any{
					"max":    50,
					"driver": "Combined Scanner",
				},
			},
			expectedMax:    50,
			expectedDriver: "Combined Scanner",
			expectNil:      false,
		},
		{
			name: "no configuration",
			outputMap: map[string]any{
				"other-config": nil,
			},
			expectedMax:    0,
			expectedDriver: "",
			expectNil:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := compiler.parseCodeScanningAlertsConfig(tt.outputMap)

			if tt.expectNil {
				if config != nil {
					t.Errorf("Expected nil config, got: %+v", config)
				}
				return
			}

			if config == nil {
				t.Errorf("Expected config, got nil")
				return
			}

			if config.Max != tt.expectedMax {
				t.Errorf("Expected Max=%d, got Max=%d", tt.expectedMax, config.Max)
			}

			if config.Driver != tt.expectedDriver {
				t.Errorf("Expected Driver=%s, got Driver=%s", tt.expectedDriver, config.Driver)
			}
		})
	}
}
