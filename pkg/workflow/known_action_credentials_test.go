//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestDetectKnownCredentialLeakingActions verifies that known credential-leaking actions
// are correctly identified from a steps list.
func TestDetectKnownCredentialLeakingActions(t *testing.T) {
	tests := []struct {
		name     string
		steps    []any
		expected map[string]struct{}
	}{
		{
			name:     "no steps",
			steps:    []any{},
			expected: nil,
		},
		{
			name: "no known actions",
			steps: []any{
				map[string]any{"name": "Setup Node", "uses": "actions/setup-node@v4"},
			},
			expected: nil,
		},
		{
			name: "google-github-actions/auth detected",
			steps: []any{
				map[string]any{"uses": "google-github-actions/auth@v2"},
			},
			expected: map[string]struct{}{"GH_AW_CLEAN_GCP": {}},
		},
		{
			name: "aws-actions/configure-aws-credentials detected",
			steps: []any{
				map[string]any{"uses": "aws-actions/configure-aws-credentials@v4"},
			},
			expected: map[string]struct{}{"GH_AW_CLEAN_AWS": {}},
		},
		{
			name: "azure/login detected",
			steps: []any{
				map[string]any{"uses": "azure/login@v2"},
			},
			expected: map[string]struct{}{"GH_AW_CLEAN_AZURE": {}},
		},
		{
			name: "docker/login-action detected",
			steps: []any{
				map[string]any{"uses": "docker/login-action@v3"},
			},
			expected: map[string]struct{}{"GH_AW_CLEAN_DOCKER": {}},
		},
		{
			name: "actions/checkout without ssh-key not detected",
			steps: []any{
				map[string]any{"uses": "actions/checkout@v4"},
			},
			expected: nil,
		},
		{
			name: "actions/checkout with ssh-key detected",
			steps: []any{
				map[string]any{
					"uses": "actions/checkout@v4",
					"with": map[string]any{"ssh-key": "${{ secrets.DEPLOY_KEY }}"},
				},
			},
			expected: map[string]struct{}{"GH_AW_CLEAN_SSH": {}},
		},
		{
			name: "actions/checkout with empty ssh-key not detected",
			steps: []any{
				map[string]any{
					"uses": "actions/checkout@v4",
					"with": map[string]any{"ssh-key": ""},
				},
			},
			expected: nil,
		},
		{
			name: "actions/checkout with whitespace-only ssh-key not detected",
			steps: []any{
				map[string]any{
					"uses": "actions/checkout@v4",
					"with": map[string]any{"ssh-key": "   "},
				},
			},
			expected: nil,
		},
		{
			name: "multiple known actions detected",
			steps: []any{
				map[string]any{"uses": "aws-actions/configure-aws-credentials@v4"},
				map[string]any{"uses": "docker/login-action@v3"},
			},
			expected: map[string]struct{}{
				"GH_AW_CLEAN_AWS":    {},
				"GH_AW_CLEAN_DOCKER": {},
			},
		},
		{
			name: "all known actions detected",
			steps: []any{
				map[string]any{"uses": "google-github-actions/auth@v2"},
				map[string]any{"uses": "aws-actions/configure-aws-credentials@v4"},
				map[string]any{"uses": "azure/login@v2"},
				map[string]any{"uses": "docker/login-action@v3"},
				map[string]any{
					"uses": "actions/checkout@v4",
					"with": map[string]any{"ssh-key": "${{ secrets.DEPLOY_KEY }}"},
				},
			},
			expected: map[string]struct{}{
				"GH_AW_CLEAN_GCP":    {},
				"GH_AW_CLEAN_AWS":    {},
				"GH_AW_CLEAN_AZURE":  {},
				"GH_AW_CLEAN_DOCKER": {},
				"GH_AW_CLEAN_SSH":    {},
			},
		},
		{
			name: "action without version suffix matches",
			steps: []any{
				map[string]any{"uses": "azure/login"},
			},
			expected: map[string]struct{}{"GH_AW_CLEAN_AZURE": {}},
		},
		{
			name: "action with commit SHA matches",
			steps: []any{
				map[string]any{"uses": "aws-actions/configure-aws-credentials@abc1234def5678"},
			},
			expected: map[string]struct{}{"GH_AW_CLEAN_AWS": {}},
		},
		{
			name: "action with inline comment stripped",
			steps: []any{
				map[string]any{"uses": "docker/login-action@abc1234 # v3"},
			},
			expected: map[string]struct{}{"GH_AW_CLEAN_DOCKER": {}},
		},
		{
			name: "run step (no uses) ignored",
			steps: []any{
				map[string]any{"name": "Run script", "run": "echo hello"},
			},
			expected: nil,
		},
		{
			name: "partial prefix does not match",
			steps: []any{
				map[string]any{"uses": "some-org/configure-aws-credentials-extra@v1"},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectKnownCredentialLeakingActions(tt.steps)
			assert.Equal(t, tt.expected, result, "unexpected detection result")
		})
	}
}

// TestDetectKnownCredentialLeakingActionsFromYAML verifies YAML parsing for the detection.
func TestDetectKnownCredentialLeakingActionsFromYAML(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected map[string]struct{}
	}{
		{
			name:     "empty string",
			yaml:     "",
			expected: nil,
		},
		{
			name:     "wrapped steps YAML with known action",
			yaml:     "steps:\n  - uses: aws-actions/configure-aws-credentials@v4\n",
			expected: map[string]struct{}{"GH_AW_CLEAN_AWS": {}},
		},
		{
			name:     "bare sequence YAML with known action",
			yaml:     "- uses: google-github-actions/auth@v2\n",
			expected: map[string]struct{}{"GH_AW_CLEAN_GCP": {}},
		},
		{
			name:     "wrapped steps YAML with no known actions",
			yaml:     "steps:\n  - uses: actions/setup-node@v4\n",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectKnownCredentialLeakingActionsFromYAML(tt.yaml)
			assert.Equal(t, tt.expected, result, "unexpected detection result")
		})
	}
}

// TestGenerateCredentialsCleanerStep verifies the merged credentials cleaner step generation.
func TestGenerateCredentialsCleanerStep(t *testing.T) {
	compiler := NewCompiler()

	t.Run("no known actions - only git credentials script, no env block", func(t *testing.T) {
		steps := compiler.generateCredentialsCleanerStep(nil)
		require.NotNil(t, steps, "expected non-nil steps")

		content := strings.Join(steps, "")
		assert.Contains(t, content, "Clean credentials", "expected step name")
		assert.NotContains(t, content, "continue-on-error: true", "cleanup must fail closed")
		assert.Contains(t, content, "verify_git_credentials.sh", "cleanup must verify its postcondition")
		assert.Contains(t, content, "clean_git_credentials.sh", "expected git cleaner script")
		assert.NotContains(t, content, "clean_known_action_credentials.sh", "known-action script must not appear")
		assert.NotContains(t, content, "env:", "env block must not appear when no known actions detected")
	})

	t.Run("empty map - same as nil", func(t *testing.T) {
		steps := compiler.generateCredentialsCleanerStep(map[string]struct{}{})
		require.NotNil(t, steps, "expected non-nil steps")

		content := strings.Join(steps, "")
		assert.Contains(t, content, "clean_git_credentials.sh", "expected git cleaner script")
		assert.NotContains(t, content, "clean_known_action_credentials.sh", "known-action script must not appear")
	})

	t.Run("generates merged step for single known action", func(t *testing.T) {
		steps := compiler.generateCredentialsCleanerStep(map[string]struct{}{
			"GH_AW_CLEAN_AWS": {},
		})
		require.NotNil(t, steps, "expected non-nil steps")

		content := strings.Join(steps, "")
		assert.Contains(t, content, "Clean credentials", "expected step name")
		assert.NotContains(t, content, "continue-on-error: true", "cleanup must fail closed")
		assert.Contains(t, content, "verify_git_credentials.sh", "cleanup must verify its postcondition")
		assert.Contains(t, content, `GH_AW_CLEAN_AWS: "true"`, "expected AWS env var")
		assert.Contains(t, content, "clean_git_credentials.sh", "expected git cleaner script")
		assert.Contains(t, content, "clean_known_action_credentials.sh", "expected known-action script")
		assert.NotContains(t, content, "GH_AW_CLEAN_GCP", "unexpected GCP env var")
	})

	t.Run("generates merged step for multiple known actions", func(t *testing.T) {
		steps := compiler.generateCredentialsCleanerStep(map[string]struct{}{
			"GH_AW_CLEAN_GCP":    {},
			"GH_AW_CLEAN_DOCKER": {},
		})
		require.NotNil(t, steps, "expected non-nil steps")

		content := strings.Join(steps, "")
		assert.Contains(t, content, `GH_AW_CLEAN_GCP: "true"`, "expected GCP env var")
		assert.Contains(t, content, `GH_AW_CLEAN_DOCKER: "true"`, "expected Docker env var")
		assert.NotContains(t, content, "GH_AW_CLEAN_AWS", "unexpected AWS env var")
	})

	t.Run("env vars are in deterministic order", func(t *testing.T) {
		steps := compiler.generateCredentialsCleanerStep(map[string]struct{}{
			"GH_AW_CLEAN_SSH":    {},
			"GH_AW_CLEAN_GCP":    {},
			"GH_AW_CLEAN_DOCKER": {},
			"GH_AW_CLEAN_AWS":    {},
			"GH_AW_CLEAN_AZURE":  {},
		})
		require.NotNil(t, steps, "expected non-nil steps")

		content := strings.Join(steps, "")
		gcpPos := strings.Index(content, "GH_AW_CLEAN_GCP")
		awsPos := strings.Index(content, "GH_AW_CLEAN_AWS")
		azurePos := strings.Index(content, "GH_AW_CLEAN_AZURE")
		dockerPos := strings.Index(content, "GH_AW_CLEAN_DOCKER")
		sshPos := strings.Index(content, "GH_AW_CLEAN_SSH")

		// Verify the order matches knownCredentialLeakingActions: GCP, AWS, AZURE, DOCKER, SSH
		assert.Less(t, gcpPos, awsPos, "GCP should come before AWS")
		assert.Less(t, awsPos, azurePos, "AWS should come before AZURE")
		assert.Less(t, azurePos, dockerPos, "AZURE should come before DOCKER")
		assert.Less(t, dockerPos, sshPos, "DOCKER should come before SSH")
	})

	t.Run("proper YAML indentation for job step level", func(t *testing.T) {
		steps := compiler.generateCredentialsCleanerStep(map[string]struct{}{
			"GH_AW_CLEAN_GCP": {},
		})
		require.NotNil(t, steps, "expected non-nil steps")
		assert.True(t, strings.HasPrefix(steps[0], "      - name:"),
			"expected 6-space indentation for step name")
	})
}

// TestKnownActionCredentialCleanerInCompiledWorkflow verifies the cleanup step appears
// in compiled workflow output when known credential-leaking actions are detected.
func TestKnownActionCredentialCleanerInCompiledWorkflow(t *testing.T) {
	tmpDir := testutil.TempDir(t, "known-action-creds-test")

	tests := []struct {
		name            string
		workflowContent string
		expectCleaner   bool
		expectEnvVars   []string
	}{
		{
			name: "no known actions - no cleanup step",
			workflowContent: `---
on: push
permissions:
  contents: read
engine: copilot
steps:
  - name: Setup Node
    uses: actions/setup-node@v4
    with:
      node-version: "24"
---
Test workflow.
`,
			expectCleaner: false,
			expectEnvVars: nil,
		},
		{
			name: "aws credentials action triggers cleanup",
			workflowContent: `---
on: push
permissions:
  contents: read
engine: copilot
steps:
  - name: Configure AWS Credentials
    uses: aws-actions/configure-aws-credentials@v4
    with:
      role-to-assume: arn:aws:iam::123456789012:role/my-role
      aws-region: us-east-1
---
Test workflow.
`,
			expectCleaner: true,
			expectEnvVars: []string{`GH_AW_CLEAN_AWS: "true"`},
		},
		{
			name: "gcp auth action triggers cleanup",
			workflowContent: `---
on: push
permissions:
  contents: read
engine: copilot
steps:
  - name: Auth GCP
    uses: google-github-actions/auth@v2
    with:
      workload_identity_provider: projects/123/locations/global/workloadIdentityPools/pool/providers/provider
---
Test workflow.
`,
			expectCleaner: true,
			expectEnvVars: []string{`GH_AW_CLEAN_GCP: "true"`},
		},
		{
			name: "multiple known actions triggers targeted cleanup",
			workflowContent: `---
on: push
permissions:
  contents: read
engine: copilot
steps:
  - name: Login to Docker Hub
    uses: docker/login-action@v3
    with:
      username: myuser
      password: ${{ secrets.DOCKER_PASSWORD }}
  - name: Login to Azure
    uses: azure/login@v2
    with:
      creds: ${{ secrets.AZURE_CREDENTIALS }}
---
Test workflow.
`,
			expectCleaner: true,
			expectEnvVars: []string{
				`GH_AW_CLEAN_AZURE: "true"`,
				`GH_AW_CLEAN_DOCKER: "true"`,
			},
		},
		{
			name: "checkout without ssh-key does not trigger ssh cleanup",
			workflowContent: `---
on: push
permissions:
  contents: read
engine: copilot
steps:
  - name: Checkout
    uses: actions/checkout@v4
    with:
      persist-credentials: false
---
Test workflow.
`,
			expectCleaner: false,
			expectEnvVars: nil,
		},
		{
			name: "checkout with ssh-key triggers ssh cleanup",
			workflowContent: `---
on: push
permissions:
  contents: read
engine: copilot
steps:
  - name: Checkout with deploy key
    uses: actions/checkout@v4
    with:
      persist-credentials: false
      ssh-key: ${{ secrets.DEPLOY_KEY }}
---
Test workflow.
`,
			expectCleaner: true,
			expectEnvVars: []string{`GH_AW_CLEAN_SSH: "true"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFile := filepath.Join(tmpDir, strings.ReplaceAll(tt.name, " ", "-")+".md")
			require.NoError(t, os.WriteFile(testFile, []byte(tt.workflowContent), 0644),
				"should write test file")

			compiler := NewCompiler()
			compiler.SetSkipValidation(true)

			workflowData, err := compiler.ParseWorkflowFile(testFile)
			require.NoError(t, err, "should parse workflow file")

			lockContent, _, _, err := compiler.generateYAML(workflowData, testFile)
			require.NoError(t, err, "should generate YAML")

			if tt.expectCleaner {
				assert.Contains(t, lockContent, "Clean credentials",
					"expected cleanup step to be present")
				assert.Contains(t, lockContent, "clean_known_action_credentials.sh",
					"expected cleanup script reference")
				for _, envVar := range tt.expectEnvVars {
					assert.Contains(t, lockContent, envVar,
						"expected env var %q to be present", envVar)
				}
			} else {
				assert.NotContains(t, lockContent, "clean_known_action_credentials.sh",
					"clean_known_action_credentials.sh must not appear when no known actions are used")
			}
		})
	}
}

// TestKnownActionCleanerPrecedesAgentExecution verifies that the cleanup step runs
// before the agent execution step.
func TestKnownActionCleanerPrecedesAgentExecution(t *testing.T) {
	tmpDir := testutil.TempDir(t, "known-action-order-test")

	testContent := `---
on: push
permissions:
  contents: read
engine: copilot
steps:
  - name: Configure AWS Credentials
    uses: aws-actions/configure-aws-credentials@v4
    with:
      role-to-assume: arn:aws:iam::123456789012:role/my-role
      aws-region: us-east-1
---
Test workflow.
`
	testFile := filepath.Join(tmpDir, "test-order.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644),
		"should write test file")

	compiler := NewCompiler()
	compiler.SetSkipValidation(true)

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "should parse workflow file")

	lockContent, _, _, err := compiler.generateYAML(workflowData, testFile)
	require.NoError(t, err, "should generate YAML")

	cleanerPos := strings.Index(lockContent, "Clean credentials")
	agentPos := strings.Index(lockContent, "Execute GitHub Copilot CLI")
	if agentPos == -1 {
		agentPos = strings.Index(lockContent, "agentic_execution")
	}

	require.NotEqual(t, -1, cleanerPos, "cleanup step must be present in compiled workflow")
	require.NotEqual(t, -1, agentPos, "agent execution step must be present in compiled workflow")
	assert.Less(t, cleanerPos, agentPos,
		"cleanup step must come before agent execution step")
}

// TestMergeKnownActionEnvVars verifies the merge helper.
func TestMergeKnownActionEnvVars(t *testing.T) {
	tests := []struct {
		name     string
		a        map[string]struct{}
		b        map[string]struct{}
		expected map[string]struct{}
	}{
		{
			name:     "both nil",
			a:        nil,
			b:        nil,
			expected: nil,
		},
		{
			name:     "one nil",
			a:        map[string]struct{}{"GH_AW_CLEAN_GCP": {}},
			b:        nil,
			expected: map[string]struct{}{"GH_AW_CLEAN_GCP": {}},
		},
		{
			name:     "both populated, no overlap",
			a:        map[string]struct{}{"GH_AW_CLEAN_GCP": {}},
			b:        map[string]struct{}{"GH_AW_CLEAN_AWS": {}},
			expected: map[string]struct{}{"GH_AW_CLEAN_GCP": {}, "GH_AW_CLEAN_AWS": {}},
		},
		{
			name:     "both populated, with overlap",
			a:        map[string]struct{}{"GH_AW_CLEAN_GCP": {}, "GH_AW_CLEAN_AWS": {}},
			b:        map[string]struct{}{"GH_AW_CLEAN_AWS": {}, "GH_AW_CLEAN_AZURE": {}},
			expected: map[string]struct{}{"GH_AW_CLEAN_GCP": {}, "GH_AW_CLEAN_AWS": {}, "GH_AW_CLEAN_AZURE": {}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeKnownActionEnvVars(tt.a, tt.b)
			assert.Equal(t, tt.expected, result, "unexpected merge result")
		})
	}
}
