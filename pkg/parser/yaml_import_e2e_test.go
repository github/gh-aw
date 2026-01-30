//go:build !integration

package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestYAMLWorkflowE2EImport(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a YAML workflow with multiple jobs
	yamlWorkflow := `name: CI Workflow
on:
  push:
    branches: [main]
  pull_request:

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Run linter
        run: npm run lint
  
  test:
    runs-on: ubuntu-latest
    needs: lint
    steps:
      - uses: actions/checkout@v3
      - name: Run tests
        run: npm test
  
  build:
    runs-on: ubuntu-latest
    needs: test
    steps:
      - uses: actions/checkout@v3
      - name: Build
        run: npm run build`

	yamlFile := filepath.Join(tmpDir, "ci-workflow.yml")
	err := os.WriteFile(yamlFile, []byte(yamlWorkflow), 0644)
	require.NoError(t, err, "Should create YAML workflow file")

	// Create a markdown workflow that imports the YAML workflow
	mdWorkflow := `---
name: Main Workflow
on: issue_comment
imports:
  - ci-workflow.yml
jobs:
  deploy:
    runs-on: ubuntu-latest
    needs: build
    steps:
      - name: Deploy
        run: echo "Deploying..."
---

# Main Workflow

This workflow imports a YAML workflow and adds additional jobs.`

	mdFile := filepath.Join(tmpDir, "main-workflow.md")
	err = os.WriteFile(mdFile, []byte(mdWorkflow), 0644)
	require.NoError(t, err, "Should create markdown workflow file")

	// Extract frontmatter and process imports
	result, err := ExtractFrontmatterFromContent(mdWorkflow)
	require.NoError(t, err, "Should extract frontmatter")

	importsResult, err := ProcessImportsFromFrontmatterWithManifest(result.Frontmatter, tmpDir, nil)
	require.NoError(t, err, "Should process imports")

	// Verify that jobs were imported
	assert.NotEmpty(t, importsResult.MergedJobs, "Should have merged jobs from YAML workflow")

	// Parse the merged jobs JSON and merge all lines
	allJobs := make(map[string]any)
	lines := []string{}
	for _, line := range []string{importsResult.MergedJobs} {
		if line != "" && line != "{}" {
			lines = append(lines, line)
		}
	}

	// Since we might have multiple JSON objects on separate lines, merge them
	for _, line := range lines {
		// Split by newlines in case there are multiple JSON objects
		for _, jsonLine := range []string{line} {
			if jsonLine == "" || jsonLine == "{}" {
				continue
			}
			var jobs map[string]any
			if err := json.Unmarshal([]byte(jsonLine), &jobs); err == nil {
				for k, v := range jobs {
					allJobs[k] = v
				}
			}
		}
	}

	// Verify all three jobs from YAML workflow were imported
	assert.Contains(t, allJobs, "lint", "Should contain lint job from YAML workflow")
	assert.Contains(t, allJobs, "test", "Should contain test job from YAML workflow")
	assert.Contains(t, allJobs, "build", "Should contain build job from YAML workflow")

	// Verify job details
	lintJob, ok := allJobs["lint"].(map[string]any)
	require.True(t, ok, "lint job should be a map")
	assert.Equal(t, "ubuntu-latest", lintJob["runs-on"], "lint job should have correct runs-on")

	testJob, ok := allJobs["test"].(map[string]any)
	require.True(t, ok, "test job should be a map")
	assert.Equal(t, "ubuntu-latest", testJob["runs-on"], "test job should have correct runs-on")

	// Verify job dependencies
	if needs, ok := testJob["needs"].(string); ok {
		assert.Equal(t, "lint", needs, "test job should depend on lint")
	} else if needsArr, ok := testJob["needs"].([]any); ok {
		assert.Contains(t, needsArr, "lint", "test job should depend on lint")
	}
}

func TestYAMLWorkflowImportWithServices(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a YAML workflow with services
	yamlWorkflow := `name: Database Test Workflow
on: push

jobs:
  db-test:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:13
        env:
          POSTGRES_PASSWORD: password
        ports:
          - 5432:5432
      redis:
        image: redis:alpine
        ports:
          - 6379:6379
    steps:
      - uses: actions/checkout@v3
      - name: Run database tests
        run: npm run test:db`

	yamlFile := filepath.Join(tmpDir, "db-test.yml")
	err := os.WriteFile(yamlFile, []byte(yamlWorkflow), 0644)
	require.NoError(t, err, "Should create YAML workflow file")

	// Create a markdown workflow that imports the YAML workflow
	mdWorkflow := `---
name: Main Workflow
on: issue_comment
imports:
  - db-test.yml
---

# Main Workflow

This workflow imports a YAML workflow with services.`

	mdFile := filepath.Join(tmpDir, "main-workflow.md")
	err = os.WriteFile(mdFile, []byte(mdWorkflow), 0644)
	require.NoError(t, err, "Should create markdown workflow file")

	// Extract frontmatter and process imports
	result, err := ExtractFrontmatterFromContent(mdWorkflow)
	require.NoError(t, err, "Should extract frontmatter")

	importsResult, err := ProcessImportsFromFrontmatterWithManifest(result.Frontmatter, tmpDir, nil)
	require.NoError(t, err, "Should process imports")

	// Verify that jobs were imported
	assert.NotEmpty(t, importsResult.MergedJobs, "Should have merged jobs from YAML workflow")

	// Verify that services were imported
	assert.NotEmpty(t, importsResult.MergedServices, "Should have merged services from YAML workflow")
	assert.Contains(t, importsResult.MergedServices, "db-test_postgres", "Should contain prefixed postgres service")
	assert.Contains(t, importsResult.MergedServices, "db-test_redis", "Should contain prefixed redis service")
}
