package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	mu      sync.Mutex
	writer  io.Writer
	command string
	err     error
	changed int
	worker  bool
	events  []updateJSONLEvent
	result  *orgRolloutResult
}

func updateReporter(ctx context.Context) *updateJSONLReporter {
	if ctx == nil {
		return nil
	}
	reporter, _ := ctx.Value(updateJSONLKey{}).(*updateJSONLReporter)
	return reporter
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
		event.Repository, _ = ctx.Value(updateJSONLRepoKey{}).(string)
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
		r.changed++
	}
	return nil
}

// Wrap RunE rather than redirecting stdout. All human diagnostics retain their
// existing stderr destination, including when a machine-output write fails.
func addUpdateJSONLFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("jsonl", false, "Report update results as versioned JSON Lines on stdout")
	cmd.Flags().Bool("jsonl-continuation", false, "Continue a JSON Lines upgrade after relaunch")
	_ = cmd.Flags().MarkHidden("jsonl-continuation")
	run := cmd.RunE
	validateArgs := cmd.Args
	var usageAfterError *bool
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		if usageAfterError != nil {
			cmd.SilenceUsage = *usageAfterError
			usageAfterError = nil
		}
		jsonl, _ := cmd.Flags().GetBool("jsonl")
		if !jsonl && validateArgs != nil {
			return validateArgs(cmd, args)
		}
		return nil
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) (runErr error) {
		enabled, _ := cmd.Flags().GetBool("jsonl")
		continuation, _ := cmd.Flags().GetBool("jsonl-continuation")
		if !enabled {
			if continuation {
				return errors.New("--jsonl-continuation requires --jsonl")
			}
			return run(cmd, args)
		}
		originalContext, originalSilenceUsage := cmd.Context(), cmd.SilenceUsage
		defer func() {
			cmd.SetContext(originalContext)
			if runErr == nil {
				cmd.SilenceUsage = originalSilenceUsage
			} else {
				// Cobra renders automatic usage after RunE returns. Keep that
				// human text off a caller-supplied JSON writer until execution
				// finishes; restore the setting when this command is reused.
				usageAfterError = &originalSilenceUsage
			}
		}()
		cmd.SilenceUsage = true
		worker, _ := cmd.Flags().GetBool("rollout-worker")
		reporter := &updateJSONLReporter{writer: cmd.OutOrStdout(), command: cmd.Name(), worker: worker}
		ctx := context.WithValue(cmd.Context(), updateJSONLKey{}, reporter)
		if repo, _ := cmd.Flags().GetString("repo"); repo != "" {
			ctx = withUpdateRepository(ctx, repo)
		}
		cmd.SetContext(ctx)
		defer func() {
			var exit *ExitCodeError
			if errors.As(runErr, &exit) && exit.Code == 0 {
				runErr = nil
			}
			runErr = errors.Join(runErr, reporter.err)
			status := "success"
			message := ""
			if runErr != nil {
				status, message = "error", runErr.Error()
			}
			if worker {
				result := orgRolloutResult{Status: "unchanged"}
				if reporter.result != nil {
					result = *reporter.result
				}
				if runErr != nil {
					result.Status, result.Error = "failed", message
				}
				result.Events = reporter.events
				runErr = errors.Join(runErr, emitOrgRolloutResult(result))
			} else if !continuation {
				runErr = errors.Join(runErr, emitUpdateEvent(ctx, updateJSONLEvent{
					Event: "command_completed", Status: status, Error: message, ChangedFiles: &reporter.changed,
				}))
			}
			runErr = errors.Join(runErr, reporter.err)
		}()
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
		if validateArgs != nil {
			if err := validateArgs(cmd, args); err != nil {
				return err
			}
		}
		return run(cmd, args)
	}
}

// Observe the actual net filesystem change, not proposed edits, pre-existing
// dirty files, or a parsed human success message.
func observeUpdateChanges(ctx context.Context, dir string) (func() error, error) {
	if updateReporter(ctx) == nil {
		return func() error { return nil }, nil
	}
	if dir == "" {
		dir = getWorkflowsDir()
	}
	physicalDir, err := summaryPhysicalPath(dir)
	if err != nil {
		return nil, err
	}
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return nil, err
	}
	root, err := summaryPhysicalPath(gitRoot)
	if err != nil {
		return nil, err
	}
	relativeDir, err := filepath.Rel(root, physicalDir)
	if err != nil {
		return nil, err
	}
	workflowPrefix := filepath.ToSlash(relativeDir) + "/"
	before, err := captureUpdatePRFiles(ctx, dir)
	if err != nil {
		return nil, err
	}
	return func() error {
		after, err := captureUpdatePRFiles(ctx, dir)
		if err != nil {
			return err
		}
		paths := make([]string, 0, len(before)+len(after))
		for path := range before {
			paths = append(paths, path)
		}
		for path := range after {
			paths = append(paths, path)
		}
		slices.Sort(paths)
		for _, path := range slices.Compact(paths) {
			old, hadOld := before[path]
			next, hasNext := after[path]
			if hadOld && hasNext && old.hash == next.hash {
				continue
			}
			kind := "artifact"
			if strings.HasSuffix(path, ".lock.yml") {
				kind = "lockfile"
			} else if strings.HasSuffix(path, ".md") && (old.source != "" || next.source != "" || strings.HasPrefix(path, workflowPrefix) || relativeDir == ".") {
				kind = "workflow"
			}

			oldHash, nextHash := "", ""
			if hadOld {
				oldHash = fmt.Sprintf("%x", old.hash)
			}
			if hasNext {
				nextHash = fmt.Sprintf("%x", next.hash)
			}
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
	}, nil
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
	_ = emitUpdateEvent(ctx, updateJSONLEvent{Event: "warning", Kind: phase, Error: err.Error()})
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
