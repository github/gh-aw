package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/spf13/cobra"
)

// updateJSONLEvent is the versioned public update/upgrade event contract.
// File events contain hashes, never file contents or diffs.
type updateJSONLEvent struct {
	SchemaVersion int      `json:"schema_version"`
	Command       string   `json:"command"`
	Event         string   `json:"event"`
	Repository    string   `json:"repository,omitempty"`
	Path          string   `json:"path,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	Operation     string   `json:"operation,omitempty"`
	Before        string   `json:"before,omitempty"`
	After         string   `json:"after,omitempty"`
	URL           string   `json:"url,omitempty"`
	Status        string   `json:"status,omitempty"`
	Error         string   `json:"error,omitempty"`
	Codemods      []string `json:"codemods,omitempty"`
	ChangedFiles  *int     `json:"changed_files,omitempty"`
	MetadataOnly  bool     `json:"metadata_only,omitempty"`
}

type updateJSONLKey struct{}
type updateJSONLRepoKey struct{}

type updateJSONLReporter struct {
	mu           sync.Mutex
	writer       io.Writer
	command      string
	err          error
	changed      int
	changedPaths map[string]struct{}
	worker       bool
	events       []updateJSONLEvent
	result       *orgRolloutResult
}

func updateReporter(ctx context.Context) *updateJSONLReporter {
	if ctx == nil {
		return nil
	}
	if reporter, ok := ctx.Value(updateJSONLKey{}).(*updateJSONLReporter); ok {
		return reporter
	}
	return nil
}

func withUpdateRepository(ctx context.Context, repo string) context.Context {
	return context.WithValue(ctx, updateJSONLRepoKey{}, repo)
}

func emitUpdateEvent(ctx context.Context, event updateJSONLEvent) error {
	reporter := updateReporter(ctx)
	if reporter == nil {
		return nil
	}
	if event.Repository == "" {
		if repository, ok := ctx.Value(updateJSONLRepoKey{}).(string); ok {
			event.Repository = repository
		}
	}
	return reporter.emit(event)
}

func (r *updateJSONLReporter) emit(event updateJSONLEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	event.SchemaVersion, event.Command = 1, r.command
	if r.worker {
		r.events = append(r.events, event)
	} else {
		data, err := json.Marshal(event)
		if err == nil {
			data = append(data, '\n')
			var written int
			written, err = r.writer.Write(data)
			if err == nil && written != len(data) {
				err = io.ErrShortWrite
			}
		}
		if err != nil {
			r.err = fmt.Errorf("writing --jsonl output: %w", err)
			return r.err
		}
	}
	if event.Event == "change" && slices.Contains([]string{"workflow", "lockfile", "artifact"}, event.Kind) {
		if event.Path != "" {
			if r.changedPaths == nil {
				r.changedPaths = make(map[string]struct{})
			}
			pathKey := event.Repository + "\x00" + event.Path
			if _, exists := r.changedPaths[pathKey]; !exists {
				r.changedPaths[pathKey] = struct{}{}
				r.changed++
			}
		}
	}
	return nil
}

func (r *updateJSONLReporter) recordError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err == nil {
		r.err = err
	}
}

// Wrap RunE rather than redirecting stdout. All human diagnostics retain their
// existing stderr destination, including when a machine-output write fails.
func addUpdateJSONLFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("jsonl", false, "Report update results as versioned JSON Lines on stdout")
	cmd.Flags().Bool("jsonl-continuation", false, "Continue a JSON Lines upgrade after relaunch")
	_ = cmd.Flags().MarkHidden("jsonl-continuation")
	state := &updateJSONLCommand{run: cmd.RunE, validateArgs: cmd.Args}
	cmd.Args, cmd.RunE = state.args, state.execute
}

type updateJSONLCommand struct {
	run             func(*cobra.Command, []string) error
	validateArgs    cobra.PositionalArgs
	usageAfterError *bool
}

func (s *updateJSONLCommand) args(cmd *cobra.Command, args []string) error {
	if s.usageAfterError != nil {
		cmd.SilenceUsage = *s.usageAfterError
		s.usageAfterError = nil
	}
	jsonl, _ := cmd.Flags().GetBool("jsonl")
	if !jsonl && s.validateArgs != nil {
		return s.validateArgs(cmd, args)
	}
	return nil
}

func (s *updateJSONLCommand) execute(cmd *cobra.Command, args []string) (runErr error) {
	enabled, _ := cmd.Flags().GetBool("jsonl")
	continuation, _ := cmd.Flags().GetBool("jsonl-continuation")
	if !enabled {
		if continuation {
			return errors.New("--jsonl-continuation requires --jsonl")
		}
		return s.run(cmd, args)
	}
	originalContext, originalSilenceUsage := cmd.Context(), cmd.SilenceUsage
	defer func() { s.restore(cmd, originalContext, originalSilenceUsage, runErr) }()
	cmd.SilenceUsage = true
	worker, _ := cmd.Flags().GetBool("rollout-worker")
	reporter := &updateJSONLReporter{writer: updateJSONLWriter(cmd), command: cmd.Name(), worker: worker}
	ctx := context.WithValue(cmd.Context(), updateJSONLKey{}, reporter)
	if repo, _ := cmd.Flags().GetString("repo"); repo != "" {
		ctx = withUpdateRepository(ctx, repo)
	}
	cmd.SetContext(ctx)
	defer func() { runErr = reporter.complete(ctx, continuation, runErr) }()
	if !worker && !continuation {
		if err := emitUpdateEvent(ctx, updateJSONLEvent{Event: "command_started"}); err != nil {
			return err
		}
	}
	for _, name := range []string{"interactive", "audit", "json"} {
		if flag := cmd.Flags().Lookup(name); flag != nil && flag.Value.String() == "true" {
			return fmt.Errorf("--jsonl cannot be combined with --%s", name)
		}
	}
	if s.validateArgs != nil {
		if err := s.validateArgs(cmd, args); err != nil {
			return err
		}
	}
	return s.run(cmd, args)
}

func updateJSONLWriter(cmd *cobra.Command) io.Writer {
	writer := cmd.OutOrStdout()
	if stderr, ok := writer.(*os.File); ok && stderr == os.Stderr {
		return os.Stdout
	}
	return writer
}

func (s *updateJSONLCommand) restore(cmd *cobra.Command, ctx context.Context, silenceUsage bool, runErr error) {
	cmd.SetContext(ctx)
	if runErr == nil {
		cmd.SilenceUsage = silenceUsage
	} else {
		// Cobra renders automatic usage after RunE returns. Keep that human
		// text off a JSON writer, restoring the setting on command reuse.
		s.usageAfterError = &silenceUsage
	}
}

func (r *updateJSONLReporter) complete(ctx context.Context, continuation bool, runErr error) error {
	var exit *ExitCodeError
	if errors.As(runErr, &exit) && exit.Code == 0 {
		runErr = nil
	}
	runErr = errors.Join(runErr, r.err)
	status, message := "success", ""
	if runErr != nil {
		status, message = "error", runErr.Error()
	}
	if r.worker {
		result := orgRolloutResult{Status: "unchanged"}
		if r.result != nil {
			result = *r.result
		}
		if runErr != nil {
			result.Status, result.Error = "failed", message
		}
		result.Events = r.events
		runErr = errors.Join(runErr, emitOrgRolloutResult(result))
	} else if !continuation {
		runErr = errors.Join(runErr, emitUpdateEvent(ctx, updateJSONLEvent{
			Event: "command_completed", Status: status, Error: message, ChangedFiles: &r.changed,
		}))
	}
	return errors.Join(runErr, r.err)
}

// Observe the actual net filesystem change, not proposed edits, pre-existing
// dirty files, or a parsed human success message.
func observeUpdateChanges(ctx context.Context, dir string, targets ...string) (func() error, error) {
	if updateReporter(ctx) == nil {
		return func() error { return nil }, nil
	}
	if dir == "" {
		dir = getWorkflowsDir()
	}
	workflowPrefix, err := updateJSONLWorkflowPrefix(dir)
	if err != nil {
		reportUpdateWarning(ctx, "change_observation", err)
		return func() error { return nil }, nil
	}
	before, err := captureUpdatePRFiles(ctx, dir, targets...)
	if err != nil {
		reportUpdateWarning(ctx, "change_observation", err)
		return func() error { return nil }, nil
	}
	return func() error {
		after, err := captureUpdatePRFiles(ctx, dir, targets...)
		if err != nil {
			reportUpdateWarning(ctx, "change_observation", err)
			return nil
		}
		return reportUpdateFileChanges(ctx, workflowPrefix, before, after)
	}, nil
}

func reportUpdateFileChanges(ctx context.Context, workflowPrefix string, before, after map[string]updatePRFile) error {
	for _, path := range updateJSONLFilePaths(before, after) {
		old, hadOld := before[path]
		next, hasNext := after[path]
		if hadOld && hasNext && old.hash == next.hash {
			continue
		}
		kind := "artifact"
		if strings.HasSuffix(path, ".lock.yml") {
			kind = "lockfile"
		} else if strings.HasSuffix(path, ".md") && (old.source != "" || next.source != "" || strings.HasPrefix(path, workflowPrefix) || workflowPrefix == "./") {
			kind = "workflow"
		}

		oldHash, nextHash := updateJSONLFileHash(old, hadOld), updateJSONLFileHash(next, hasNext)
		if err := emitUpdateEvent(ctx, updateJSONLEvent{
			Event: "change", Path: path, Kind: kind, Operation: updateChangeOperation(hadOld, hasNext),
			Before: oldHash, After: nextHash, MetadataOnly: old.metadataOnly || next.metadataOnly,
		}); err != nil {
			return err
		}
		for _, metadata := range []struct{ kind, before, after string }{
			{"source", old.source, next.source}, {"source_sha", old.sourceSHA, next.sourceSHA}, {"compiler", old.compiler, next.compiler},
		} {
			if metadata.before != metadata.after {
				if err := emitUpdateEvent(ctx, updateJSONLEvent{
					Event: "change", Path: path, Kind: metadata.kind,
					Operation: updateChangeOperation(metadata.before != "", metadata.after != ""),
					Before:    metadata.before, After: metadata.after,
				}); err != nil {
					return err
				}
			}
		}
		if err := emitUpdatePinChanges(ctx, path, old.pins, next.pins); err != nil {
			return err
		}
	}
	return nil
}

func updateJSONLWorkflowPrefix(dir string) (string, error) {
	physicalDir, err := summaryPhysicalPath(dir)
	if err != nil {
		return "", err
	}
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		gitRoot, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	root, err := summaryPhysicalPath(gitRoot)
	if err != nil {
		return "", err
	}
	relativeDir, err := filepath.Rel(root, physicalDir)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(relativeDir) + "/", nil
}

func updateJSONLRelativePath(path string) string {
	root, err := gitutil.FindGitRoot()
	if err != nil {
		root, err = os.Getwd()
	}
	if err != nil {
		return filepath.ToSlash(path)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}

func updateJSONLFilePaths(before, after map[string]updatePRFile) []string {
	paths := make([]string, 0, len(before)+len(after))
	for path := range before {
		paths = append(paths, path)
	}
	for path := range after {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

func updateJSONLFileHash(file updatePRFile, present bool) string {
	if !present {
		return ""
	}
	return fmt.Sprintf("%x", file.hash)
}

func updateChangeOperation(before, after bool) string {
	if !before {
		return "added"
	}
	if !after {
		return "removed"
	}
	return "modified"
}

func emitUpdatePinChanges(ctx context.Context, path string, before, after map[string]updatePRPin) error {
	keys := make([]string, 0, len(before)+len(after))
	for key := range before {
		keys = append(keys, key)
	}
	for key := range after {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range slices.Compact(keys) {
		old, hadOld := before[key]
		next, hasNext := after[key]
		if old == next {
			continue
		}
		kind := strings.ToLower(next.kind)
		if !hasNext {
			kind = strings.ToLower(old.kind)
		}

		oldRef, nextRef := "", ""
		if hadOld {
			oldRef = old.repo + "@" + old.version + "@" + old.sha
		}
		if hasNext {
			nextRef = next.repo + "@" + next.version + "@" + next.sha
		}
		if err := emitUpdateEvent(ctx, updateJSONLEvent{Event: "change", Path: path, Kind: kind,
			Operation: updateChangeOperation(hadOld, hasNext), Before: oldRef, After: nextRef}); err != nil {
			return err
		}
	}
	return nil
}

func reportUpdateWarning(ctx context.Context, phase string, err error) {
	if reporter := updateReporter(ctx); reporter != nil {
		reporter.recordError(emitUpdateEvent(ctx, updateJSONLEvent{Event: "warning", Kind: phase, Error: err.Error()}))
	}
}

func reportUpdateRepositoryCompletion(ctx context.Context, repo string, err error) error {
	event := updateJSONLEvent{Event: "repository_completed", Repository: repo, Status: "success"}
	if err != nil {
		event.Status, event.Error = "failed", err.Error()
	}
	return emitUpdateEvent(ctx, event)
}

func reportUpdatePreview(ctx context.Context, preview orgRepoPreview, status string) error {
	if len(preview.Workflows) == 0 {
		return emitUpdateEvent(ctx, updateJSONLEvent{Event: "repository_preview", Repository: preview.Repo, Status: status})
	}
	for _, wf := range preview.Workflows {
		if err := emitUpdateEvent(ctx, updateJSONLEvent{
			Event: "repository_preview", Repository: preview.Repo, Path: wf.Path,
			Kind: "source", Before: wf.CurrentRef, After: wf.LatestRef, Status: status,
		}); err != nil {
			return err
		}
	}
	return nil
}
