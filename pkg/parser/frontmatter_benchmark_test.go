//go:build !integration

package parser

import (
	"testing"
)

// BenchmarkParseFrontmatter benchmarks basic YAML frontmatter parsing
func BenchmarkParseFrontmatter(b *testing.B) {
	content := `---
on: push
permissions:
  contents: read
  issues: write
engine: claude
timeout-minutes: 10
---

# Test Workflow

This is a test workflow.
`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ExtractFrontmatterFromContent(content)
	}
}

// BenchmarkParseFrontmatter_Complex benchmarks complex frontmatter with tools and MCP
func BenchmarkParseFrontmatter_Complex(b *testing.B) {
	content := `---
on:
  pull_request:
    types: [opened, synchronize, reopened]
    forks: ["org/*", "user/repo"]
permissions:
  contents: read
  issues: write
  pull-requests: write
  actions: read
engine:
  id: copilot
  max-turns: 5
  max-concurrency: 3
  model: gpt-5
mcp-servers:
  github:
    mode: remote
    toolsets: [default, actions, discussions]
    read-only: false
  playwright:
    container: "mcr.microsoft.com/playwright:v1.41.0"
    allowed-domains: ["github.com", "*.github.io"]
  cache-memory:
    - id: default
      key: memory-default-${{ github.run_id }}
    - id: session
      key: memory-session-${{ github.run_id }}
network:
  allowed:
    - defaults
    - python
    - node
    - containers
  firewall:
    version: "v1.0.0"
    log-level: debug
tools:
  edit:
  web-fetch:
  web-search:
  bash:
    - "git status"
    - "git diff"
    - "npm test"
    - "npm run lint"
safe-outputs:
  create-pull-request:
    title-prefix: "[ai] "
    labels: [automation, ai-generated]
    draft: true
  add-comment:
    max: 3
    target: "*"
  create-issue:
    title-prefix: "[bug] "
    labels: [bug, automated]
    max: 5
timeout-minutes: 30
concurrency:
  group: workflow-${{ github.event.pull_request.number }}
  cancel-in-progress: true
imports:
  - shared/security.md
  - shared/tools.md
---

# Complex Workflow

This is a complex workflow with many features.
`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ExtractFrontmatterFromContent(content)
	}
}

// BenchmarkParseFrontmatter_Minimal benchmarks minimal frontmatter
func BenchmarkParseFrontmatter_Minimal(b *testing.B) {
	content := `---
on: push
---

# Minimal Workflow

Simple workflow with minimal configuration.
`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ExtractFrontmatterFromContent(content)
	}
}

// BenchmarkParseFrontmatter_WithArrays benchmarks frontmatter with arrays
func BenchmarkParseFrontmatter_WithArrays(b *testing.B) {
	content := `---
on:
  schedule:
    - cron: "0 0 * * *"
    - cron: "0 12 * * *"
    - cron: "0 18 * * *"
permissions:
  contents: read
  issues: write
  pull-requests: write
tools:
  github:
    allowed:
      - get_repository
      - list_commits
      - get_commit
      - list_issues
      - create_issue
      - add_issue_comment
      - list_pull_requests
      - get_pull_request
  bash:
    - "echo"
    - "ls"
    - "cat"
    - "grep"
    - "awk"
    - "sed"
imports:
  - shared/tool1.md
  - shared/tool2.md
  - shared/tool3.md
  - shared/security.md
---

# Workflow with Arrays

Workflow demonstrating array handling in frontmatter.
`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ExtractFrontmatterFromContent(content)
	}
}

// BenchmarkValidateSchema benchmarks schema validation
func BenchmarkValidateSchema(b *testing.B) {
	frontmatter := map[string]any{
		"on": "push",
		"permissions": map[string]any{
			"contents":      "read",
			"issues":        "write",
			"pull-requests": "read",
		},
		"engine": "claude",
		"tools": map[string]any{
			"github": map[string]any{
				"allowed": []any{"issue_read", "add_issue_comment"},
			},
			"bash": []any{"echo", "ls"},
		},
		"timeout-minutes": 10,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateMainWorkflowFrontmatterWithSchema(frontmatter)
	}
}

// BenchmarkValidateSchema_Complex benchmarks schema validation with complex data
func BenchmarkValidateSchema_Complex(b *testing.B) {
	frontmatter := map[string]any{
		"on": map[string]any{
			"pull_request": map[string]any{
				"types": []any{"opened", "synchronize", "reopened"},
				"forks": []any{"org/*", "user/repo"},
			},
		},
		"permissions": map[string]any{
			"contents":      "read",
			"issues":        "write",
			"pull-requests": "write",
			"actions":       "read",
		},
		"engine": map[string]any{
			"id":              "copilot",
			"max-turns":       5,
			"max-concurrency": 3,
			"model":           "gpt-5",
		},
		"mcp-servers": map[string]any{
			"github": map[string]any{
				"mode":      "remote",
				"toolsets":  []any{"default", "actions", "discussions"},
				"read-only": false,
			},
			"playwright": map[string]any{
				"container":       "mcr.microsoft.com/playwright:v1.41.0",
				"allowed-domains": []any{"github.com", "*.github.io"},
			},
		},
		"network": map[string]any{
			"allowed": []any{"defaults", "python", "node"},
			"firewall": map[string]any{
				"version":   "v1.0.0",
				"log-level": "debug",
			},
		},
		"tools": map[string]any{
			"edit":       true,
			"web-fetch":  true,
			"web-search": true,
			"bash":       []any{"git status", "git diff", "npm test"},
		},
		"safe-outputs": map[string]any{
			"create-pull-requests": map[string]any{
				"title-prefix": "[ai] ",
				"labels":       []any{"automation", "ai-generated"},
				"draft":        true,
			},
			"add-comments": map[string]any{
				"max":    3,
				"target": "*",
			},
		},
		"timeout-minutes": 30,
		"concurrency": map[string]any{
			"group":              "workflow-123",
			"cancel-in-progress": true,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateMainWorkflowFrontmatterWithSchema(frontmatter)
	}
}
