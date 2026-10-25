package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"
)

// NewEditCommand creates the experimental command for changing workflow frontmatter.
func NewEditCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit <workflow> [path: value]",
		Short: "Experimental: edit workflow frontmatter and recompile",
		Long: `Experimental: edit schema-validated workflow frontmatter and recompile its generated file.

The workflow-id may be a workflow name, a Markdown filename, or a path. Changes are
validated before writing. Workflows managed by a source: declaration can be edited
locally; by default, future updates will merge in those local changes (use --no-merge to override).

Edits that change nothing leave the workflow untouched. When frontmatter does change it is
re-serialized, so YAML comments, key ordering, and quoting styles are not preserved.`,
		Example: `  gh aw edit repo-assist "max-turns: 20"
  gh aw edit repo-assist --schedule "every 6h"
  gh aw edit repo-assist --set model=small --unset engine.model
  gh aw edit repo-assist --add-import shared/common.md
  gh aw edit repo-assist --add-skill shared/review`,
		Args: cobra.RangeArgs(1, 2),
		RunE: runEditCommand,
	}
	cmd.Flags().StringArray("set", nil, "Set a frontmatter path (path=value)")
	cmd.Flags().StringArray("unset", nil, "Remove a frontmatter path")
	cmd.Flags().StringArray("add", nil, "Append a value to a list (path=value)")
	cmd.Flags().StringArray("remove", nil, "Remove a value from a list (path=value)")
	cmd.Flags().StringArray("add-import", nil, "Append a workflow import path")
	cmd.Flags().StringArray("remove-import", nil, "Remove a workflow import path")
	cmd.Flags().StringArray("add-skill", nil, "Append a workflow skill")
	cmd.Flags().StringArray("remove-skill", nil, "Remove a workflow skill")
	cmd.Flags().String("schedule", "", "Set a schedule using a fuzzy schedule or cron expression; use off to remove it")
	cmd.Flags().Bool("dry-run", false, "Validate changes without writing or compiling")
	cmd.Flags().StringP("dir", "d", "", "Workflow directory (default: $GH_AW_WORKFLOWS_DIR or .github/workflows)")
	cmd.ValidArgsFunction = CompleteWorkflowNames
	RegisterDirFlagCompletion(cmd, "dir")
	return cmd
}

func runEditCommand(cmd *cobra.Command, args []string) error {
	workflowPath, err := resolveWorkflowFileInDir(args[0], false, editFlagString(cmd, "dir"))
	if err != nil {
		return err
	}
	content, err := os.ReadFile(workflowPath)
	if err != nil {
		return fmt.Errorf("read workflow: %w", err)
	}
	parsed, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		return err
	}

	changes, err := editChangesFromCommand(cmd, args)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		return errors.New("provide an assignment or an edit flag")
	}
	if _, managed := parsed.Frontmatter["source"]; managed {
		for _, change := range changes {
			if strings.Split(change.path, ".")[0] == "source" {
				return errors.New("cannot edit source for a source-managed workflow")
			}
		}
	}
	edited := false
	for _, change := range changes {
		applied, err := applyEditChange(parsed.Frontmatter, change)
		if err != nil {
			return err
		}
		edited = edited || applied
	}
	if !edited {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "workflow already matches the requested changes")
		return err
	}
	if err := parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(parsed.Frontmatter, workflowPath); err != nil {
		return fmt.Errorf("invalid edited workflow: %w", err)
	}
	updated, err := replaceFrontmatter(string(content), parsed.Frontmatter)
	if err != nil {
		return err
	}
	if editFlagBool(cmd, "dry-run") {
		_, err := fmt.Fprint(cmd.OutOrStdout(), updated)
		return err
	}

	return writeAndCompileEditedWorkflow(cmd.Context(), workflowPath, content, updated)
}

func writeAndCompileEditedWorkflow(ctx context.Context, workflowPath string, content []byte, updated string) error {
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	previousLock, lockErr := os.ReadFile(lockPath)
	lockExisted := lockErr == nil
	if lockErr != nil && !os.IsNotExist(lockErr) {
		return fmt.Errorf("read generated lock file: %w", lockErr)
	}
	if err := writeFileAtomically(workflowPath, []byte(updated)); err != nil {
		return fmt.Errorf("write workflow: %w", err)
	}
	if err := compileWorkflow(ctx, workflowPath, false, true, ""); err != nil {
		return errors.Join(fmt.Errorf("compile edited workflow: %w", err), restoreEditedWorkflow(workflowPath, content, lockPath, previousLock, lockExisted))
	}
	return nil
}

// restoreEditedWorkflow puts the workflow and its generated file back to their pre-edit state.
func restoreEditedWorkflow(workflowPath string, content []byte, lockPath string, previousLock []byte, lockExisted bool) error {
	var errs []error
	if err := writeFileAtomically(workflowPath, content); err != nil {
		errs = append(errs, fmt.Errorf("restore workflow: %w", err))
	}
	if lockExisted {
		if err := writeFileAtomically(lockPath, previousLock); err != nil {
			errs = append(errs, fmt.Errorf("restore generated lock file: %w", err))
		}
	} else if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("remove generated lock file: %w", err))
	}
	return errors.Join(errs...)
}

// writeFileAtomically writes content through a sibling temporary file so a failed
// write never truncates the destination.
func writeFileAtomically(path string, content []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if _, err := file.Write(content); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Close(); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.Chmod(tempPath, mode); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

type editChange struct {
	kind, path string
	value      any
}

func editChangesFromCommand(cmd *cobra.Command, args []string) ([]editChange, error) {
	var changes []editChange
	if len(args) == 2 {
		change, err := parseEditAssignment(args[1], ":")
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	for _, name := range []string{"set", "add", "remove"} {
		for _, assignment := range editFlagStrings(cmd, name) {
			change, err := parseEditAssignment(assignment, "=")
			if err != nil {
				return nil, err
			}
			change.kind = name
			changes = append(changes, change)
		}
	}
	for _, path := range editFlagStrings(cmd, "unset") {
		changes = append(changes, editChange{kind: "unset", path: path})
	}
	for _, importPath := range editFlagStrings(cmd, "add-import") {
		changes = append(changes, editChange{kind: "add", path: "imports", value: importPath})
	}
	for _, importPath := range editFlagStrings(cmd, "remove-import") {
		changes = append(changes, editChange{kind: "remove", path: "imports", value: importPath})
	}
	for _, skill := range editFlagStrings(cmd, "add-skill") {
		changes = append(changes, editChange{kind: "add", path: "skills", value: skill})
	}
	for _, skill := range editFlagStrings(cmd, "remove-skill") {
		changes = append(changes, editChange{kind: "remove", path: "skills", value: skill})
	}
	if schedule := editFlagString(cmd, "schedule"); schedule != "" {
		change, err := scheduleChange(schedule)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func scheduleChange(schedule string) (editChange, error) {
	schedule = strings.TrimSpace(schedule)
	if strings.EqualFold(schedule, "off") {
		return editChange{kind: "unset", path: "on.schedule"}, nil
	}
	_, _, err := parser.ParseSchedule(schedule)
	if err != nil {
		return editChange{}, fmt.Errorf("invalid schedule: %w", err)
	}
	return editChange{kind: "set", path: "on.schedule", value: schedule}, nil
}

func parseEditAssignment(assignment, separator string) (editChange, error) {
	path, rawValue, ok := strings.Cut(assignment, separator)
	path, rawValue = strings.TrimSpace(path), strings.TrimSpace(rawValue)
	if !ok || path == "" || rawValue == "" {
		return editChange{}, fmt.Errorf("invalid assignment %q; expected path%svalue", assignment, separator)
	}
	var value map[string]any
	if err := yaml.Unmarshal([]byte("value: "+rawValue), &value); err != nil {
		return editChange{}, fmt.Errorf("parse value for %q: %w", path, err)
	}
	return editChange{kind: "set", path: path, value: value["value"]}, nil
}

// applyEditChange applies a change and reports whether the frontmatter was modified.
func applyEditChange(frontmatter map[string]any, change editChange) (bool, error) {
	change, err := normalizeEditChange(frontmatter, change)
	if err != nil {
		return false, err
	}
	parent, key, err := editChangeParent(frontmatter, change.path, change.kind != "unset")
	if err != nil {
		return false, err
	}
	if parent == nil {
		// The target does not exist, so an unset is a no-op and the original
		// frontmatter representation is preserved.
		return false, nil
	}
	return applyEditChangeToParent(parent, key, change)
}

func normalizeEditChange(frontmatter map[string]any, change editChange) (editChange, error) {
	if change.kind == "set" && change.path == "on.schedule" {
		if schedule, ok := change.value.(string); ok {
			var err error
			change, err = scheduleChange(schedule)
			if err != nil {
				return editChange{}, err
			}
		}
	}
	if change.path == "imports" && (change.kind == "add" || change.kind == "remove") {
		if _, objectImports := frontmatter["imports"].(map[string]any); objectImports {
			change.path = "imports.aw"
		}
	}
	return change, nil
}

// editChangeParent walks changePath and returns the map holding its last segment.
// When create is false, a missing or non-object ancestor yields a nil parent so the
// caller can treat the change as a no-op instead of rewriting the frontmatter.
func editChangeParent(frontmatter map[string]any, changePath string, create bool) (map[string]any, string, error) {
	path := strings.Split(changePath, ".")
	if slices.Contains(path, "") {
		return nil, "", fmt.Errorf("invalid frontmatter path %q", changePath)
	}
	parent := frontmatter
	for _, part := range path[:len(path)-1] {
		child, ok := parent[part].(map[string]any)
		if !ok {
			if !create {
				return nil, "", nil
			}
			if parent[part] == nil {
				child = map[string]any{}
			} else if part == "on" {
				expanded, err := expandTriggers(parent[part])
				if err != nil {
					return nil, "", fmt.Errorf("cannot edit %q: %w", changePath, err)
				}
				child = expanded
			} else {
				return nil, "", fmt.Errorf("cannot edit %q because %q is not an object", changePath, part)
			}
			parent[part] = child
		}
		parent = child
	}
	return parent, path[len(path)-1], nil
}

// expandTriggers converts the shorthand forms accepted by the on: field into the
// equivalent trigger object so a trigger can be edited without changing semantics.
func expandTriggers(triggers any) (map[string]any, error) {
	switch value := triggers.(type) {
	case map[string]any:
		return value, nil
	case string:
		return expandTriggerString(value)
	case []any:
		expanded := make(map[string]any, len(value))
		for _, trigger := range value {
			name, ok := trigger.(string)
			if !ok {
				return nil, errors.New("on contains a non-string trigger")
			}
			expanded[name] = nil
		}
		return expanded, nil
	}
	return nil, fmt.Errorf("on is not a trigger object, got %T", triggers)
}

func expandTriggerString(triggers string) (map[string]any, error) {
	triggers = strings.TrimSpace(triggers)
	if _, _, err := parser.ParseSchedule(triggers); err == nil {
		return map[string]any{"schedule": triggers, "workflow_dispatch": nil}, nil
	}
	trigger, err := workflow.ParseTriggerShorthand(triggers)
	if err != nil {
		return nil, fmt.Errorf("on shorthand %q is not a recognized trigger: %w", triggers, err)
	}
	if trigger != nil {
		if len(trigger.Conditions) > 0 {
			return nil, fmt.Errorf("expand the on shorthand %q into its object form before editing it", triggers)
		}
		return trigger.ToYAMLMap(), nil
	}
	if strings.ContainsAny(triggers, " \t/") {
		return nil, fmt.Errorf("expand the on shorthand %q into its object form before editing it", triggers)
	}
	return map[string]any{triggers: nil}, nil
}

// applyEditChangeToParent applies a change to parent and reports whether it modified it.
func applyEditChangeToParent(parent map[string]any, key string, change editChange) (bool, error) {
	switch change.kind {
	case "set":
		existing, exists := parent[key]
		if exists && reflect.DeepEqual(existing, change.value) {
			return false, nil
		}
		parent[key] = change.value
	case "unset":
		if _, exists := parent[key]; !exists {
			return false, nil
		}
		delete(parent, key)
	case "add":
		values, ok := parent[key].([]any)
		if !ok && parent[key] != nil {
			return false, fmt.Errorf("cannot add to %q because it is not a list", change.path)
		}
		if slices.ContainsFunc(values, func(value any) bool { return reflect.DeepEqual(value, change.value) }) {
			return false, nil
		}
		parent[key] = append(values, change.value)
	case "remove":
		values, ok := parent[key].([]any)
		if !ok {
			return false, fmt.Errorf("cannot remove from %q because it is not a list", change.path)
		}
		remaining := slices.DeleteFunc(slices.Clone(values), func(value any) bool { return reflect.DeepEqual(value, change.value) })
		if len(remaining) == len(values) {
			return false, nil
		}
		parent[key] = remaining
	default:
		return false, fmt.Errorf("unsupported edit operation %q", change.kind)
	}
	return true, nil
}

func replaceFrontmatter(content string, frontmatter map[string]any) (string, error) {
	encoded, err := yaml.MarshalWithOptions(frontmatter, yaml.Indent(2), yaml.IndentSequence(true))
	if err != nil {
		return "", fmt.Errorf("encode frontmatter: %w", err)
	}
	firstLineEnd := strings.IndexByte(content, '\n')
	if firstLineEnd < 0 || strings.TrimSpace(content[:firstLineEnd]) != "---" {
		return "", errors.New("workflow must begin with YAML frontmatter")
	}
	bodyStart := firstLineEnd + 1
	for lineStart := bodyStart; lineStart <= len(content); {
		lineEnd := strings.IndexByte(content[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(content)
		} else {
			lineEnd += lineStart
		}
		if strings.TrimSpace(content[lineStart:lineEnd]) == "---" {
			if lineEnd < len(content) {
				lineEnd++
			}
			return "---\n" + string(encoded) + "---\n" + content[lineEnd:], nil
		}
		if lineEnd == len(content) {
			break
		}
		lineStart = lineEnd + 1
	}
	return "", errors.New("frontmatter not properly closed")
}

func editFlagString(cmd *cobra.Command, name string) string {
	value, _ := cmd.Flags().GetString(name)
	return value
}

func editFlagStrings(cmd *cobra.Command, name string) []string {
	value, _ := cmd.Flags().GetStringArray(name)
	return value
}

func editFlagBool(cmd *cobra.Command, name string) bool {
	value, _ := cmd.Flags().GetBool(name)
	return value
}
