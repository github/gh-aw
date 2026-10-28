package lint

import (
	"context"
	"errors"
	"path"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/checkoutpolicy"
	"mvdan.cc/sh/v3/syntax"
)

func ArtifactExecutionRule() Rule {
	return Rule{
		ID: "artifact-execution", Description: "Review cross-run artifact workspace extraction and direct execution on workflow_run",
		Severity: Warning, Targets: ActionsAST | AWAST, Check: checkArtifactExecution,
	}
}

type artifactDownload struct {
	directory string
	path      string
}

func checkArtifactExecution(ctx context.Context, input Input) ([]Diagnostic, error) {
	if err := integrityInput(ctx, input); err != nil {
		return nil, err
	}
	if _, found, err := authoredTrigger(ctx, executionPolicyOn(input), "workflow_run"); err != nil || !found {
		return nil, err
	}
	blocks, err := executionPolicyBlocks(ctx, input)
	failures := []error{err}
	var diagnostics []Diagnostic
	for _, block := range blocks {
		var downloads []artifactDownload
		for _, step := range block.Steps {
			if err := ctx.Err(); err != nil {
				return diagnostics, errors.Join(append(failures, err)...)
			}
			download, found, err := crossRunArtifact(ctx, step)
			failures = append(failures, err)
			if found {
				downloads = append(downloads, download)
				if download.directory == "." || download.directory == "" {
					message := "Cross-run artifact extraction may overwrite workspace code or configuration; extract into a dedicated literal directory and consume validated data rather than executable content"
					if download.directory == "" {
						message = "Cross-run artifact extraction path is dynamic or unsupported; use a dedicated literal directory and review downstream execution separately"
					}
					diagnostics = append(diagnostics, Diagnostic{Path: download.path, Message: message})
				}
			}
			if len(downloads) == 0 {
				continue
			}
			foundDiagnostics, err := artifactStepExecution(ctx, input, block, step, downloads)
			diagnostics = append(diagnostics, foundDiagnostics...)
			failures = append(failures, err)
		}
	}
	return diagnostics, errors.Join(failures...)
}

func crossRunArtifact(ctx context.Context, step aw.ExecutionStep) (artifactDownload, bool, error) {
	name := actionName(step.Fields["uses"])
	if name != "actions/download-artifact" && name != "dawidd6/action-download-artifact" {
		return artifactDownload{}, false, nil
	}
	with, err := policyMapping(step.Fields["with"], step.Path+".with")
	if err != nil {
		return artifactDownload{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return artifactDownload{}, false, err
	}
	runKey := "run-id"
	if name == "dawidd6/action-download-artifact" {
		runKey = "run_id"
	}
	run, explicit := with[runKey]
	if name == "actions/download-artifact" {
		if !explicit || run == nil || run == "" {
			return artifactDownload{}, false, nil
		}
	}
	if text, ok := run.(string); ok && checkoutpolicy.MatchesExpression(text, "github.run_id") {
		return artifactDownload{}, false, nil
	}
	directory := "."
	if raw, explicit := with["path"]; explicit {
		if text, ok := raw.(string); ok {
			directory = artifactLiteralPath(text)
		} else {
			directory = ""
		}
	}
	return artifactDownload{directory: directory, path: step.Path + ".with.path"}, true, nil
}

func artifactLiteralPath(text string) string {
	text = strings.TrimSpace(text)
	if checkoutpolicy.MatchesExpression(text, "github.workspace") {
		return "."
	}
	for _, prefix := range []string{"${{ github.workspace }}/", "$GITHUB_WORKSPACE/", "${GITHUB_WORKSPACE}/"} {
		text = strings.TrimPrefix(text, prefix)
	}
	if text == "$GITHUB_WORKSPACE" || text == "${GITHUB_WORKSPACE}" {
		return "."
	}
	if text == "" || strings.ContainsAny(text, "$`\\") {
		return ""
	}
	return path.Clean(text)
}

func artifactStepExecution(ctx context.Context, input Input, block aw.ExecutionBlock, step aw.ExecutionStep, downloads []artifactDownload) ([]Diagnostic, error) {
	related := func(file string) []RelatedLocation {
		var locations []RelatedLocation
		for _, download := range downloads {
			if artifactContains(download.directory, file) {
				locations = append(locations, RelatedLocation{Path: download.path})
			}
		}
		return locations
	}
	diagnostic := func(locations []RelatedLocation, path string, line int) []Diagnostic {
		if len(locations) == 0 {
			return nil
		}
		return []Diagnostic{{Path: path, Line: line, Related: locations,
			Message: "A later step may execute cross-run artifact content with workflow_run privileges; treat artifacts as untrusted data or establish independent provenance before executing them. Upstream success, branch filters and repository identity alone do not authenticate content",
		}}
	}
	if uses, ok := step.Fields["uses"].(string); ok && strings.HasPrefix(uses, "./") {
		return diagnostic(related(artifactLiteralPath(uses)), step.Path+".uses", 0), nil
	}
	directory := artifactWorkingDirectory(input, block, step)
	return policyShellCalls(ctx, input, block, step, func(call *syntax.CallExpr, _ string) ([]Diagnostic, error) {
		var locations []RelatedLocation
		for _, file := range artifactCommandFiles(call, directory) {
			locations = append(locations, related(file)...)
		}
		return diagnostic(locations, step.Path+".run", int(call.Pos().Line())), nil
	})
}

func artifactCommandFiles(call *syntax.CallExpr, directory string) []string {
	if len(call.Args) == 0 || directory == "" {
		return nil
	}
	command, operand := policyShellArgument(call, 0), policyShellArgument(call, 1)
	switch command {
	case "bash", "sh", "python", "python3", "node", "source", ".":
		if len(call.Args) > 1 {
			file := operand
			if file != "" && !strings.HasPrefix(file, "-") {
				return []string{artifactResolvePath(directory, file)}
			}
		}
	case "npm":
		if len(call.Args) > 1 {
			switch operand {
			case "ci", "install", "i", "run", "run-script", "test", "start", "exec", "rebuild":
				return []string{directory}
			}
		}
	case "npx", "make":
		if len(call.Args) == 1 || !strings.HasPrefix(operand, "-") {
			return []string{directory}
		}
	default:
		if strings.Contains(command, "/") {
			return []string{artifactResolvePath(directory, command)}
		}
	}
	return nil
}

func artifactWorkingDirectory(input Input, block aw.ExecutionBlock, step aw.ExecutionStep) string {
	if value, explicit := step.Fields["working-directory"]; explicit {
		if text, ok := value.(string); ok {
			return artifactLiteralPath(text)
		}
		return ""
	}
	var defaults []map[string]any
	if input.Workflow != nil {
		for _, job := range input.Workflow.Jobs {
			if block.Path == "jobs."+string(job.ID) {
				defaults = append(defaults, job.Defaults)
				break
			}
		}
		defaults = append(defaults, input.Workflow.Defaults)
	} else {
		for _, fields := range []map[string]any{block.Fields, input.Document.Frontmatter} {
			if value, ok := fields["defaults"].(map[string]any); ok {
				defaults = append(defaults, value)
			}
		}
	}
	for _, fields := range defaults {
		run, ok := fields["run"].(map[string]any)
		if !ok {
			continue
		}
		if value, explicit := run["working-directory"]; explicit {
			if text, ok := value.(string); ok {
				return artifactLiteralPath(text)
			}
			return ""
		}
	}
	return "."
}

func artifactResolvePath(directory, file string) string {
	file = artifactLiteralPath(file)
	if file == "" || path.IsAbs(file) {
		return file
	}
	return path.Join(directory, file)
}

func artifactContains(directory, file string) bool {
	if directory == "" || file == "" || path.IsAbs(directory) != path.IsAbs(file) {
		return false
	}
	if directory == "." {
		return file != ".." && !strings.HasPrefix(file, "../")
	}
	return file == directory || strings.HasPrefix(file, directory+"/")
}
