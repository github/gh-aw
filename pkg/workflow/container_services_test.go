//go:build integration

package workflow

import (
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestContainerSupport(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter string
		expected    string
	}{
		{
			name: "simple container image",
			frontmatter: `---
on:
  issues:
    types: [opened]
container: node:18
---

# Test Container Workflow

This workflow runs in a Node.js container.`,
			expected: "container: node:18",
		},
		{
			name: "container object with configuration",
			frontmatter: `---
on:
  issues:
    types: [opened]
container:
  image: node:18
  env:
    NODE_ENV: production
  ports:
    - 3000
---

# Test Container Workflow

This workflow runs in a configured Node.js container.`,
			expected: `container:
      image: node:18
      env:
        NODE_ENV: production
      ports:
        - 3000`,
		},
		{
			name: "container with credentials",
			frontmatter: `---
on:
  issues:
    types: [opened]
container:
  image: myregistry.com/myapp:latest
  credentials:
    username: ${{ secrets.REGISTRY_USERNAME }}
    password: ${{ secrets.REGISTRY_PASSWORD }}
---

# Test Container Workflow

This workflow runs in a private registry container.`,
			expected: `container:
      image: myregistry.com/myapp:latest
      credentials:
        username: ${{ secrets.REGISTRY_USERNAME }}
        password: ${{ secrets.REGISTRY_PASSWORD }}`,
		},
		{
			name: "no container specified",
			frontmatter: `---
on:
  issues:
    types: [opened]
---

# Test Workflow

This is a test without container.`,
			expected: "", // No container section should be present
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary workflow file
			tmpDir, err := os.MkdirTemp("", "workflow-container-test")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			workflowFile := tmpDir + "/test.md"
			err = os.WriteFile(workflowFile, []byte(tt.frontmatter), 0644)
			if err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			// Parse the workflow
			compiler := NewCompiler()
			workflowData, err := compiler.ParseWorkflowFile(workflowFile)
			if err != nil {
				t.Fatalf("Failed to parse workflow: %v", err)
			}

			// Check if container is correctly extracted
			if tt.expected == "" {
				if workflowData.Container != "" {
					t.Errorf("Expected no container, but got: %s", workflowData.Container)
				}
			} else {
				if !strings.Contains(workflowData.Container, strings.TrimSpace(strings.Split(tt.expected, "\n")[0])) {
					t.Errorf("Expected container to contain '%s', but got: %s", tt.expected, workflowData.Container)
				}
			}

			// Generate YAML and check if container appears in the main job
			yamlContent, err := compiler.generateYAML(workflowData, workflowFile)
			if err != nil {
				t.Fatalf("Failed to generate YAML: %v", err)
			}

			if tt.expected == "" {
				// Should not contain container section
				if strings.Contains(yamlContent, "container:") {
					t.Errorf("Expected no container in YAML, but found container section")
				}
			} else {
				// Should contain container section in the main job
				lines := strings.Split(yamlContent, "\n")
				inMainJob := false
				foundContainer := false

				for i, line := range lines {
					if strings.Contains(line, string(constants.AgentJobName)+":") {
						inMainJob = true
						continue
					}
					if inMainJob && strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && line != "  "+string(constants.AgentJobName)+":" {
						// Found next job, stop looking
						break
					}
					if inMainJob && strings.TrimSpace(line) != "" && strings.HasPrefix(strings.TrimSpace(line), "container:") {
						foundContainer = true
						// For complex container objects, check the next few lines too
						if strings.Contains(tt.expected, "image:") {
							nextLines := []string{line}
							for j := i + 1; j < len(lines) && j < i+10; j++ {
								if strings.HasPrefix(lines[j], "      ") || strings.TrimSpace(lines[j]) == "" {
									nextLines = append(nextLines, lines[j])
								} else {
									break
								}
							}
							combinedLines := strings.Join(nextLines, "\n")
							if !strings.Contains(combinedLines, "image:") {
								t.Errorf("Expected container object with image, but didn't find it in: %s", combinedLines)
							}
						}
						break
					}
				}

				if !foundContainer {
					t.Errorf("Expected container section in main job, but not found in YAML:\n%s", yamlContent)
				}
			}
		})
	}
}

func TestServicesSupport(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter string
		expected    string
	}{
		{
			name: "simple service",
			frontmatter: `---
on:
  issues:
    types: [opened]
services:
  postgres:
    image: postgres:13
    env:
      POSTGRES_PASSWORD: postgres
---

# Test Services Workflow

This workflow uses a PostgreSQL service.`,
			expected: `services:
      postgres:
        image: postgres:13
        env:
          POSTGRES_PASSWORD: postgres`,
		},
		{
			name: "multiple services",
			frontmatter: `---
on:
  issues:
    types: [opened]
services:
  postgres:
    image: postgres:13
    env:
      POSTGRES_PASSWORD: postgres
    ports:
      - 5432:5432
  redis:
    image: redis:7
    ports:
      - 6379:6379
---

# Test Services Workflow

This workflow uses PostgreSQL and Redis services.`,
			expected: `services:
      postgres:
        image: postgres:13`,
		},
		{
			name: "no services specified",
			frontmatter: `---
on:
  issues:
    types: [opened]
---

# Test Workflow

This is a test without services.`,
			expected: "", // No services section should be present
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary workflow file
			tmpDir, err := os.MkdirTemp("", "workflow-services-test")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			workflowFile := tmpDir + "/test.md"
			err = os.WriteFile(workflowFile, []byte(tt.frontmatter), 0644)
			if err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			// Parse the workflow
			compiler := NewCompiler()
			workflowData, err := compiler.ParseWorkflowFile(workflowFile)
			if err != nil {
				t.Fatalf("Failed to parse workflow: %v", err)
			}

			// Check if services is correctly extracted
			if tt.expected == "" {
				if workflowData.Services != "" {
					t.Errorf("Expected no services, but got: %s", workflowData.Services)
				}
			} else {
				if !strings.Contains(workflowData.Services, strings.TrimSpace(strings.Split(tt.expected, "\n")[0])) {
					t.Errorf("Expected services to contain '%s', but got: %s", tt.expected, workflowData.Services)
				}
			}

			// Generate YAML and check if services appears in the main job
			yamlContent, err := compiler.generateYAML(workflowData, workflowFile)
			if err != nil {
				t.Fatalf("Failed to generate YAML: %v", err)
			}

			if tt.expected == "" {
				// Should not contain services section
				if strings.Contains(yamlContent, "services:") {
					t.Errorf("Expected no services in YAML, but found services section")
				}
			} else {
				// Should contain services section in the main job
				lines := strings.Split(yamlContent, "\n")
				inMainJob := false
				foundServices := false

				for _, line := range lines {
					if strings.Contains(line, string(constants.AgentJobName)+":") {
						inMainJob = true
						continue
					}
					if inMainJob && strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && line != "  "+string(constants.AgentJobName)+":" {
						// Found next job, stop looking
						break
					}
					if inMainJob && strings.TrimSpace(line) != "" && strings.HasPrefix(strings.TrimSpace(line), "services:") {
						foundServices = true
						break
					}
				}

				if !foundServices {
					t.Errorf("Expected services section in main job, but not found in YAML:\n%s", yamlContent)
				}
			}
		})
	}
}

func TestContainerAndServicesIndentation(t *testing.T) {
	frontmatter := `---
on:
  issues:
    types: [opened]
container:
  image: node:18
  env:
    NODE_ENV: production
services:
  postgres:
    image: postgres:13
    env:
      POSTGRES_PASSWORD: postgres
    ports:
      - 5432:5432
---

# Test Container and Services Workflow

This workflow uses both container and services.`

	// Create temporary workflow file
	tmpDir, err := os.MkdirTemp("", "workflow-container-services-indent-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	workflowFile := tmpDir + "/test.md"
	err = os.WriteFile(workflowFile, []byte(frontmatter), 0644)
	if err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Parse and generate YAML
	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	yamlContent, err := compiler.generateYAML(workflowData, workflowFile)
	if err != nil {
		t.Fatalf("Failed to generate YAML: %v", err)
	}

	// Check that container and services are properly indented within the job
	// Check for proper indentation of container section
	if !strings.Contains(yamlContent, "    container:") {
		t.Errorf("Expected 4-space indented container section, but got:\n%s", yamlContent)
	}
	if !strings.Contains(yamlContent, "      image: node:18") {
		t.Errorf("Expected 6-space indented container image, but got:\n%s", yamlContent)
	}
	if !strings.Contains(yamlContent, "        NODE_ENV: production") {
		t.Errorf("Expected 8-space indented container env, but got:\n%s", yamlContent)
	}

	// Check for proper indentation of services section
	if !strings.Contains(yamlContent, "    services:") {
		t.Errorf("Expected 4-space indented services section, but got:\n%s", yamlContent)
	}
	if !strings.Contains(yamlContent, "      postgres:") {
		t.Errorf("Expected 6-space indented service name, but got:\n%s", yamlContent)
	}
	if !strings.Contains(yamlContent, "        image: postgres:13") {
		t.Errorf("Expected 8-space indented service image, but got:\n%s", yamlContent)
	}
	if !strings.Contains(yamlContent, "          POSTGRES_PASSWORD: postgres") {
		t.Errorf("Expected 10-space indented service env, but got:\n%s", yamlContent)
	}
	if !strings.Contains(yamlContent, "        - 5432:5432") {
		t.Errorf("Expected 8-space indented service ports, but got:\n%s", yamlContent)
	}
}
