//go:build integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

// JobArtifacts holds upload and download information for a job
type JobArtifacts struct {
	Uploads   []*ArtifactUpload
	Downloads []*ArtifactDownload
}

// TestGenerateArtifactsReference compiles all agentic workflows and generates
// a reference document mapping artifacts to file paths per job.
// This document is meant to be used by agents to generate file paths in JavaScript and Go.
func TestGenerateArtifactsReference(t *testing.T) {
	// Find all workflow markdown files
	workflowsDir := filepath.Join("..", "..", ".github", "workflows")
	entries, err := os.ReadDir(workflowsDir)
	require.NoError(t, err, "Failed to read workflows directory")

	// Collect workflow files (exclude campaign files and lock files)
	var workflowFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".md") &&
			!strings.HasSuffix(name, ".lock.yml") &&
			!strings.Contains(name, ".campaign.") {
			workflowFiles = append(workflowFiles, filepath.Join(workflowsDir, name))
		}
	}

	t.Logf("Found %d workflow files to process", len(workflowFiles))

	// Map to store artifacts per workflow
	workflowArtifacts := make(map[string]map[string]*JobArtifacts) // workflow -> job -> artifacts

	// Compile each workflow and extract artifact information
	// Use dry-run mode (noEmit) so we don't write lock.yml files
	compiler := NewCompiler()
	compiler.SetNoEmit(true) // Enable dry-run mode - validate without generating lock files
	successCount := 0

	for _, workflowPath := range workflowFiles {
		workflowName := filepath.Base(workflowPath)

		// Parse the workflow
		workflowData, err := compiler.ParseWorkflowFile(workflowPath)
		if err != nil {
			t.Logf("Warning: Failed to parse %s: %v", workflowName, err)
			continue
		}

		// Try to compile the workflow
		err = compiler.CompileWorkflowData(workflowData, workflowPath)
		if err != nil {
			// Some workflows may fail compilation for various reasons (missing permissions, etc.)
			// We'll skip these for the artifact analysis
			t.Logf("Warning: Failed to compile %s: %v", workflowName, err)
			continue
		}

		// Read the compiled lock file to extract artifact information
		lockPath := stringutil.MarkdownToLockFile(workflowPath)
		lockContent, err := os.ReadFile(lockPath)
		if err != nil {
			t.Logf("Warning: Failed to read lock file for %s: %v", workflowName, err)
			continue
		}

		// Parse the lock file to extract artifact steps
		jobs := extractArtifactsFromYAML(string(lockContent), workflowName, t)

		if len(jobs) > 0 {
			workflowArtifacts[workflowName] = jobs
			successCount++
		}
	}

	t.Logf("Successfully analyzed %d workflows with artifacts", successCount)

	// Build a global summary of artifacts by job name
	artifactsByJob := buildArtifactsSummary(workflowArtifacts)

	// Generate the markdown reference document
	markdown := generateArtifactsMarkdown(workflowArtifacts, artifactsByJob)

	// Write to scratchpad/artifacts.md
	specsDir := filepath.Join("..", "..", "specs")
	err = os.MkdirAll(specsDir, 0755)
	require.NoError(t, err, "Failed to create specs directory")

	artifactsPath := filepath.Join(specsDir, "artifacts.md")
	err = os.WriteFile(artifactsPath, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write artifacts.md")

	t.Logf("Generated artifacts reference at %s", artifactsPath)
}

// extractArtifactsFromYAML parses compiled YAML and extracts artifact upload/download information
func extractArtifactsFromYAML(yamlContent string, workflowName string, t *testing.T) map[string]*JobArtifacts {
	// Parse YAML
	var workflow map[string]interface{}
	err := yaml.Unmarshal([]byte(yamlContent), &workflow)
	if err != nil {
		t.Logf("Warning: Failed to parse YAML for %s: %v", workflowName, err)
		return nil
	}

	// Get jobs
	jobsRaw, ok := workflow["jobs"].(map[string]interface{})
	if !ok {
		return nil
	}

	result := make(map[string]*JobArtifacts)

	for jobName, jobDataRaw := range jobsRaw {
		jobData, ok := jobDataRaw.(map[string]interface{})
		if !ok {
			continue
		}

		steps, ok := jobData["steps"].([]interface{})
		if !ok {
			continue
		}

		jobArtifacts := &JobArtifacts{}
		hasArtifacts := false

		for _, stepRaw := range steps {
			step, ok := stepRaw.(map[string]interface{})
			if !ok {
				continue
			}

			uses, ok := step["uses"].(string)
			if !ok {
				continue
			}

			// Check for upload-artifact
			if strings.Contains(uses, "actions/upload-artifact@") {
				upload := &ArtifactUpload{
					JobName: jobName,
				}

				// Extract 'with' parameters
				withParams, ok := step["with"].(map[string]interface{})
				if ok {
					if name, ok := withParams["name"].(string); ok {
						upload.Name = name
					}
					// Handle path which could be a string or multiline string
					if pathStr, ok := withParams["path"].(string); ok {
						// Split by newlines and trim whitespace
						lines := strings.Split(pathStr, "\n")
						for _, line := range lines {
							line = strings.TrimSpace(line)
							if line != "" {
								upload.Paths = append(upload.Paths, line)
							}
						}
					}
				}

				if upload.Name != "" {
					jobArtifacts.Uploads = append(jobArtifacts.Uploads, upload)
					hasArtifacts = true
				}
			}

			// Check for download-artifact
			if strings.Contains(uses, "actions/download-artifact@") {
				download := &ArtifactDownload{
					JobName: jobName,
				}

				// Extract 'with' parameters
				withParams, ok := step["with"].(map[string]interface{})
				if ok {
					if name, ok := withParams["name"].(string); ok {
						download.Name = name
					}
					if pattern, ok := withParams["pattern"].(string); ok {
						download.Pattern = pattern
					}
					if pathStr, ok := withParams["path"].(string); ok {
						download.Path = pathStr
					}
					if merge, ok := withParams["merge-multiple"].(bool); ok {
						download.MergeMultiple = merge
					}
				}

				// Try to infer dependencies from job needs
				if needs, ok := jobData["needs"].([]interface{}); ok {
					for _, need := range needs {
						if needStr, ok := need.(string); ok {
							download.DependsOn = append(download.DependsOn, needStr)
						}
					}
				} else if needStr, ok := jobData["needs"].(string); ok {
					download.DependsOn = []string{needStr}
				}

				if download.Name != "" || download.Pattern != "" {
					jobArtifacts.Downloads = append(jobArtifacts.Downloads, download)
					hasArtifacts = true
				}
			}
		}

		if hasArtifacts {
			result[jobName] = jobArtifacts
		}
	}

	return result
}

// ArtifactSummary holds merged artifact information for a job across all workflows
type ArtifactSummary struct {
	JobName   string
	Uploads   map[string]*ArtifactUploadInfo   // artifact name -> upload info
	Downloads map[string]*ArtifactDownloadInfo // artifact name/pattern -> download info
}

// ArtifactUploadInfo holds merged upload information
type ArtifactUploadInfo struct {
	ArtifactName string
	Paths        map[string]bool // unique paths across all workflows
	Workflows    []string        // workflows that upload this artifact
}

// ArtifactDownloadInfo holds merged download information
type ArtifactDownloadInfo struct {
	Identifier    string          // artifact name or pattern
	DownloadPaths map[string]bool // unique download paths
	Workflows     []string        // workflows that download this
	MergeMultiple bool
}

// buildArtifactsSummary creates a summary of artifacts by job name, merging duplicates
func buildArtifactsSummary(workflowArtifacts map[string]map[string]*JobArtifacts) map[string]*ArtifactSummary {
	summary := make(map[string]*ArtifactSummary)

	for workflowName, jobs := range workflowArtifacts {
		for jobName, artifacts := range jobs {
			// Get or create job summary
			if summary[jobName] == nil {
				summary[jobName] = &ArtifactSummary{
					JobName:   jobName,
					Uploads:   make(map[string]*ArtifactUploadInfo),
					Downloads: make(map[string]*ArtifactDownloadInfo),
				}
			}
			jobSummary := summary[jobName]

			// Merge uploads
			for _, upload := range artifacts.Uploads {
				if upload.Name == "" {
					continue
				}

				if jobSummary.Uploads[upload.Name] == nil {
					jobSummary.Uploads[upload.Name] = &ArtifactUploadInfo{
						ArtifactName: upload.Name,
						Paths:        make(map[string]bool),
						Workflows:    []string{},
					}
				}
				uploadInfo := jobSummary.Uploads[upload.Name]

				// Add paths
				for _, path := range upload.Paths {
					uploadInfo.Paths[path] = true
				}

				// Add workflow if not already present
				if !artifactContainsWorkflow(uploadInfo.Workflows, workflowName) {
					uploadInfo.Workflows = append(uploadInfo.Workflows, workflowName)
				}
			}

			// Merge downloads
			for _, download := range artifacts.Downloads {
				identifier := download.Name
				if identifier == "" {
					identifier = download.Pattern
				}
				if identifier == "" {
					continue
				}

				if jobSummary.Downloads[identifier] == nil {
					jobSummary.Downloads[identifier] = &ArtifactDownloadInfo{
						Identifier:    identifier,
						DownloadPaths: make(map[string]bool),
						Workflows:     []string{},
						MergeMultiple: download.MergeMultiple,
					}
				}
				downloadInfo := jobSummary.Downloads[identifier]

				// Add download path
				if download.Path != "" {
					downloadInfo.DownloadPaths[download.Path] = true
				}

				// Add workflow if not already present
				if !artifactContainsWorkflow(downloadInfo.Workflows, workflowName) {
					downloadInfo.Workflows = append(downloadInfo.Workflows, workflowName)
				}
			}
		}
	}

	return summary
}

// artifactContainsWorkflow checks if a string slice contains a value
func artifactContainsWorkflow(slice []string, value string) bool {
	for _, item := range slice {
		if item == value {
			return true
		}
	}
	return false
}

// generateArtifactsMarkdown generates a markdown document with artifact information
func generateArtifactsMarkdown(workflowArtifacts map[string]map[string]*JobArtifacts, artifactsByJob map[string]*ArtifactSummary) string {
	var sb strings.Builder

	sb.WriteString("<!-- This file was automatically generated by gh-aw. DO NOT EDIT. -->\n\n")
	sb.WriteString("# Artifact File Locations Reference\n\n")
	sb.WriteString("This document provides a reference for artifact file locations across all agentic workflows.\n")
	sb.WriteString("It is generated automatically and meant to be used by agents when generating file paths in JavaScript and Go code.\n\n")
	sb.WriteString("## Overview\n\n")
	sb.WriteString("When artifacts are uploaded, GitHub Actions strips the common parent directory from file paths.\n")
	sb.WriteString("When artifacts are downloaded, files are extracted based on the download mode:\n\n")
	sb.WriteString("- **Download by name**: Files extracted directly to `path/` (e.g., `path/file.txt`)\n")
	sb.WriteString("- **Download by pattern (no merge)**: Files in `path/artifact-name/` (e.g., `path/artifact-1/file.txt`)\n")
	sb.WriteString("- **Download by pattern (merge)**: Files extracted directly to `path/` (e.g., `path/file.txt`)\n\n")

	// Add summary section
	sb.WriteString("## Summary by Job\n\n")
	sb.WriteString("This section provides an overview of artifacts organized by job name, with duplicates merged across workflows.\n\n")

	// Sort job names for consistent output
	jobNames := make([]string, 0, len(artifactsByJob))
	for jobName := range artifactsByJob {
		jobNames = append(jobNames, jobName)
	}
	sort.Strings(jobNames)

	for _, jobName := range jobNames {
		summary := artifactsByJob[jobName]

		fmt.Fprintf(&sb, "### Job: `%s`\n\n", jobName)

		// Uploads summary
		if len(summary.Uploads) > 0 {
			sb.WriteString("**Artifacts Uploaded:**\n\n")

			// Sort artifact names
			uploadNames := make([]string, 0, len(summary.Uploads))
			for name := range summary.Uploads {
				uploadNames = append(uploadNames, name)
			}
			sort.Strings(uploadNames)

			for _, name := range uploadNames {
				info := summary.Uploads[name]
				fmt.Fprintf(&sb, "- `%s`\n", info.ArtifactName)

				// Sort and list paths
				paths := make([]string, 0, len(info.Paths))
				for path := range info.Paths {
					paths = append(paths, path)
				}
				sort.Strings(paths)

				sb.WriteString("  - **Paths**: ")
				for i, path := range paths {
					if i > 0 {
						sb.WriteString(", ")
					}
					fmt.Fprintf(&sb, "`%s`", path)
				}
				sb.WriteString("\n")

				// Sort and list workflows
				sort.Strings(info.Workflows)
				fmt.Fprintf(&sb, "  - **Used in**: %d workflow(s) - %s\n", len(info.Workflows), strings.Join(info.Workflows, ", "))
			}
			sb.WriteString("\n")
		}

		// Downloads summary
		if len(summary.Downloads) > 0 {
			sb.WriteString("**Artifacts Downloaded:**\n\n")

			// Sort identifiers
			downloadIds := make([]string, 0, len(summary.Downloads))
			for id := range summary.Downloads {
				downloadIds = append(downloadIds, id)
			}
			sort.Strings(downloadIds)

			for _, id := range downloadIds {
				info := summary.Downloads[id]
				fmt.Fprintf(&sb, "- `%s`\n", info.Identifier)

				// Sort and list download paths
				paths := make([]string, 0, len(info.DownloadPaths))
				for path := range info.DownloadPaths {
					paths = append(paths, path)
				}
				sort.Strings(paths)

				sb.WriteString("  - **Download paths**: ")
				for i, path := range paths {
					if i > 0 {
						sb.WriteString(", ")
					}
					fmt.Fprintf(&sb, "`%s`", path)
				}
				sb.WriteString("\n")

				// Sort and list workflows
				sort.Strings(info.Workflows)
				fmt.Fprintf(&sb, "  - **Used in**: %d workflow(s) - %s\n", len(info.Workflows), strings.Join(info.Workflows, ", "))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("## Workflows\n\n")

	// Sort workflow names for consistent output
	workflowNames := make([]string, 0, len(workflowArtifacts))
	for name := range workflowArtifacts {
		workflowNames = append(workflowNames, name)
	}
	sort.Strings(workflowNames)

	for _, workflowName := range workflowNames {
		jobs := workflowArtifacts[workflowName]

		fmt.Fprintf(&sb, "### %s\n\n", workflowName)

		// Sort job names
		jobNames := make([]string, 0, len(jobs))
		for jobName := range jobs {
			jobNames = append(jobNames, jobName)
		}
		sort.Strings(jobNames)

		for _, jobName := range jobNames {
			artifacts := jobs[jobName]

			fmt.Fprintf(&sb, "#### Job: `%s`\n\n", jobName)

			// Uploads
			if len(artifacts.Uploads) > 0 {
				sb.WriteString("**Uploads:**\n\n")
				for _, upload := range artifacts.Uploads {
					fmt.Fprintf(&sb, "- **Artifact**: `%s`\n", upload.Name)
					sb.WriteString("  - **Upload paths**:\n")
					for _, path := range upload.Paths {
						fmt.Fprintf(&sb, "    - `%s`\n", path)
					}

					if len(upload.NormalizedPaths) > 0 {
						sb.WriteString("  - **Paths in artifact** (after common parent stripping):\n")

						// Sort normalized paths for consistent output
						var normalizedKeys []string
						for key := range upload.NormalizedPaths {
							normalizedKeys = append(normalizedKeys, key)
						}
						sort.Strings(normalizedKeys)

						for _, key := range normalizedKeys {
							normalizedPath := upload.NormalizedPaths[key]
							fmt.Fprintf(&sb, "    - `%s` → `%s`\n", key, normalizedPath)
						}
					}
					sb.WriteString("\n")
				}
			}

			// Downloads
			if len(artifacts.Downloads) > 0 {
				sb.WriteString("**Downloads:**\n\n")
				for _, download := range artifacts.Downloads {
					if download.Name != "" {
						fmt.Fprintf(&sb, "- **Artifact**: `%s` (by name)\n", download.Name)
					} else if download.Pattern != "" {
						fmt.Fprintf(&sb, "- **Pattern**: `%s`", download.Pattern)
						if download.MergeMultiple {
							sb.WriteString(" (merge-multiple: true)\n")
						} else {
							sb.WriteString(" (merge-multiple: false)\n")
						}
					}
					fmt.Fprintf(&sb, "  - **Download path**: `%s`\n", download.Path)
					if len(download.DependsOn) > 0 {
						fmt.Fprintf(&sb, "  - **Depends on jobs**: %v\n", download.DependsOn)
					}
					sb.WriteString("\n")
				}
			}
		}
	}

	sb.WriteString("## Usage Examples\n\n")
	sb.WriteString("### JavaScript (actions/github-script)\n\n")
	sb.WriteString("```javascript\n")
	sb.WriteString("// Reading a file from a downloaded artifact\n")
	sb.WriteString("const fs = require('fs');\n")
	sb.WriteString("const path = require('path');\n\n")
	sb.WriteString("// If artifact 'build-output' was downloaded to '/tmp/artifacts'\n")
	sb.WriteString("// and contains 'dist/app.js' (after common parent stripping)\n")
	sb.WriteString("const filePath = path.join('/tmp/artifacts', 'dist', 'app.js');\n")
	sb.WriteString("const content = fs.readFileSync(filePath, 'utf8');\n")
	sb.WriteString("```\n\n")
	sb.WriteString("### Go\n\n")
	sb.WriteString("```go\n")
	sb.WriteString("// Reading a file from a downloaded artifact\n")
	sb.WriteString("import (\n")
	sb.WriteString("    \"os\"\n")
	sb.WriteString("    \"path/filepath\"\n")
	sb.WriteString(")\n\n")
	sb.WriteString("// If artifact 'build-output' was downloaded to '/tmp/artifacts'\n")
	sb.WriteString("// and contains 'dist/app.js' (after common parent stripping)\n")
	sb.WriteString("filePath := filepath.Join(\"/tmp/artifacts\", \"dist\", \"app.js\")\n")
	sb.WriteString("content, err := os.ReadFile(filePath)\n")
	sb.WriteString("```\n\n")
	sb.WriteString("## Notes\n\n")
	sb.WriteString("- This document is auto-generated from workflow analysis\n")
	sb.WriteString("- Actual file paths may vary based on the workflow execution context\n")
	sb.WriteString("- Always verify file existence before reading in production code\n")
	sb.WriteString("- Common parent directories are automatically stripped during upload\n")
	sb.WriteString("- Use `ComputeDownloadPath()` from the artifact manager for accurate path computation\n")

	return sb.String()
}
