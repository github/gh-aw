package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var callWorkflowPermissionsLog = logger.New("workflow:call_workflow_permissions")

type workflowSourceKind string

const (
	workflowSourceKindLock     workflowSourceKind = "lock"
	workflowSourceKindYAML     workflowSourceKind = "yaml"
	workflowSourceKindMarkdown workflowSourceKind = "markdown"
)

type callWorkflowPermissionImport struct {
	permissions *Permissions
	sourcePath  string
	sourceKind  workflowSourceKind
}

// extractJobPermissionsFromParsedWorkflow extracts and merges all job-level permissions
// from a parsed GitHub Actions workflow map. Returns the union of all jobs' permissions.
//
// Limitation: only explicit per-job permissions blocks are examined. Jobs that omit
// a permissions block inherit from the workflow-level permissions key and are therefore
// not counted here. If a worker workflow relies on workflow-level permissions inheritance
// instead of declaring permissions on each job, the returned set may be incomplete and
// the call-* job could still under-grant permissions at runtime. Workers called via
// call-workflow should declare explicit per-job permissions to ensure reliable extraction.
func extractJobPermissionsFromParsedWorkflow(workflow map[string]any) *Permissions {
	merged := NewPermissions()

	jobsSection, ok := workflow["jobs"]
	if !ok {
		return merged
	}

	jobsMap, ok := jobsSection.(map[string]any)
	if !ok {
		return merged
	}

	for jobName, jobConfig := range jobsMap {
		jobMap, ok := jobConfig.(map[string]any)
		if !ok {
			continue
		}

		permsValue, hasPerms := jobMap["permissions"]
		if !hasPerms {
			callWorkflowPermissionsLog.Printf("Job '%s' has no permissions block, skipping", jobName)
			continue
		}

		jobPerms := NewPermissionsParserFromValue(permsValue).ToPermissions()
		callWorkflowPermissionsLog.Printf("Merging permissions from job '%s'", jobName)
		merged.Merge(jobPerms)
	}

	return merged
}

func extractCallWorkflowPermissionImport(workflowName, markdownPath string) (*callWorkflowPermissionImport, error) {
	fileResult, err := findWorkflowFile(workflowName, markdownPath)
	if err != nil {
		return nil, fmt.Errorf("failed to find workflow file for '%s': %w", workflowName, err)
	}

	// Priority: .lock.yml > .yml > .md
	if fileResult.lockExists {
		perms, err := extractPermissionsFromYAMLFile(fileResult.lockPath)
		if err != nil {
			return nil, err
		}
		// A same-batch Markdown target gains admission OIDC even when its
		// existing lock file predates admission support.
		if fileResult.mdExists {
			perms.Set(PermissionIdToken, PermissionWrite)
		}
		return &callWorkflowPermissionImport{
			permissions: perms,
			sourcePath:  fileResult.lockPath,
			sourceKind:  workflowSourceKindLock,
		}, nil
	}

	if fileResult.ymlExists {
		perms, err := extractPermissionsFromYAMLFile(fileResult.ymlPath)
		if err != nil {
			return nil, err
		}
		return &callWorkflowPermissionImport{
			permissions: perms,
			sourcePath:  fileResult.ymlPath,
			sourceKind:  workflowSourceKindYAML,
		}, nil
	}

	if fileResult.mdExists {
		perms, err := extractPermissionsFromMDFile(fileResult.mdPath)
		if err != nil {
			return nil, err
		}
		if perms == nil {
			return nil, nil
		}
		return &callWorkflowPermissionImport{
			permissions: perms,
			sourcePath:  fileResult.mdPath,
			sourceKind:  workflowSourceKindMarkdown,
		}, nil
	}

	// No file found — return nil so the caller omits the permissions block.
	callWorkflowPermissionsLog.Printf("No workflow file found for '%s', skipping permissions", workflowName)
	return nil, nil
}

func buildCallWorkflowPermissionsComment(workflowName string, imported *callWorkflowPermissionImport) string {
	if imported == nil || imported.permissions == nil {
		return ""
	}
	if imported.permissions.RenderToYAML() == "" {
		return ""
	}

	reviewWhat := "job-level permissions"
	if imported.sourceKind == workflowSourceKindMarkdown {
		reviewWhat = "frontmatter permissions"
	}

	return strings.Join([]string{
		fmt.Sprintf("# Imported from called workflow %q because GitHub requires the caller job to grant permissions requested by reusable workflow jobs.", workflowName),
		fmt.Sprintf("# Review the called workflow's %s in %s.", reviewWhat, renderWorkflowReviewPath(imported.sourcePath)),
	}, "\n")
}

// renderWorkflowReviewPath converts an absolute workflow path to the canonical
// repo-relative display path used in generated review comments. This assumes
// workflow files live directly in constants.GetWorkflowDir().
func renderWorkflowReviewPath(sourcePath string) string {
	return "./" + filepath.ToSlash(filepath.Join(constants.GetWorkflowDir(), filepath.Base(sourcePath)))
}

// extractPermissionsFromYAMLFile reads a .lock.yml or .yml workflow file, parses it,
// and returns the merged permissions from all its jobs.
func extractPermissionsFromYAMLFile(filePath string) (*Permissions, error) {
	workflow, err := readWorkflowYAML(filePath)
	if err != nil {
		return nil, err
	}

	perms := extractJobPermissionsFromParsedWorkflow(workflow)
	callWorkflowPermissionsLog.Printf("Extracted permissions from YAML file %s", filePath)
	return perms, nil
}

// extractPermissionsFromMDFile reads a .md workflow source and uses the frontmatter-level
// permissions field plus mandatory activation permissions as a proxy for the job
// permissions that will be generated when the worker is compiled.
func extractPermissionsFromMDFile(mdPath string) (*Permissions, error) {
	// mdPath originates from findWorkflowFile(), which validates paths via
	// isPathWithinDir() to prevent directory traversal before returning them.
	content, err := os.ReadFile(mdPath) // #nosec G304 -- path pre-validated by findWorkflowFile() via isPathWithinDir()
	if err != nil {
		return nil, fmt.Errorf("failed to read workflow source %s: %w", mdPath, err)
	}

	result, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil || result == nil {
		callWorkflowPermissionsLog.Printf("Failed to extract frontmatter from %s: %v", mdPath, err)
		return nil, nil
	}

	perms := NewPermissionsContentsRead()
	if permsValue, hasPerms := result.Frontmatter["permissions"]; hasPerms {
		perms.Merge(NewPermissionsParserFromValue(permsValue).ToPermissions())
	}
	// Admission can be enabled at runtime, so reusable caller permissions must allow OIDC even if frontmatter says otherwise.
	perms.Set(PermissionIdToken, PermissionWrite)
	callWorkflowPermissionsLog.Printf("Extracted permissions from .md source %s", mdPath)
	return perms, nil
}
