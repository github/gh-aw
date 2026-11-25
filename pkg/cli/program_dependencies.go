package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/parser"
)

var fetchedProgramPathPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*(?:/[A-Za-z0-9_][A-Za-z0-9_.-]*)*\.(?:ts|mts|cts|js|mjs|cjs)$`)

type programUpdateBaselineKey struct{}
type programWorkersOnlyKey struct{}

type programUpdateBaseline struct {
	repo, sourceDir, latestDir, ref string
}

func withProgramUpdateBaseline(ctx context.Context, repo, sourcePath, latestPath, ref string, overwrite bool) context.Context {
	if overwrite {
		return ctx
	}
	return context.WithValue(ctx, programUpdateBaselineKey{}, programUpdateBaseline{
		repo: repo, sourceDir: path.Dir(sourcePath), latestDir: path.Dir(latestPath), ref: ref,
	})
}

func withProgramWorkersOnly(ctx context.Context) context.Context {
	return context.WithValue(ctx, programWorkersOnlyKey{}, true)
}

func skipNonProgramWorker(ctx context.Context, content []byte) (bool, error) {
	if ctx.Value(programWorkersOnlyKey{}) != true {
		return false, nil
	}
	program, err := workflowProgramPath(string(content))
	return program == "", err
}

func compileUpdatedProgramWorkers(ctx context.Context, workflowFile string, tracker *FileTracker, opts UpdateWorkflowsOptions) error {
	names := append(dispatchWorkflowNamesForCompilation(workflowFile), callWorkflowNamesForCompilation(workflowFile)...)
	seen := make(map[string]bool)
	for _, name := range names {
		worker := filepath.Join(filepath.Dir(workflowFile), name+".md")
		if worker == workflowFile || seen[worker] || !tracker.tracks(worker) {
			continue
		}
		seen[worker] = true
		content, err := os.ReadFile(worker)
		if err != nil {
			return err
		}
		program, err := workflowProgramPath(string(content))
		if err != nil {
			return err
		}
		if program == "" {
			continue
		}
		if err := compileWorkflowWithTrackingAndOptions(ctx, worker, trackedWorkflowCompileOptions{
			verbose: opts.Verbose, engineOverride: opts.EngineOverride, tracker: tracker, noGitattributes: true, approve: opts.Approve,
		}); err != nil {
			return fmt.Errorf("failed to compile updated program worker %s: %w", worker, err)
		}
	}
	return nil
}

func validateWorkerUpdate(ctx context.Context, targetDir, targetPath, remotePath string, content []byte, downloader fileDownloadFn) error {
	baseline, ok := ctx.Value(programUpdateBaselineKey{}).(programUpdateBaseline)
	if !ok {
		return nil
	}
	program, err := workflowProgramPath(string(content))
	if err != nil || program == "" {
		return err
	}
	if _, err := os.Lstat(targetPath); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	local, err := fileutil.ReadFileWithinBase(targetDir, targetPath)
	if err != nil {
		return err
	}
	relativeRemote, err := filepath.Rel(filepath.FromSlash(baseline.latestDir), filepath.FromSlash(remotePath))
	if err != nil || !filepath.IsLocal(relativeRemote) {
		return fmt.Errorf("cannot determine previous source for worker %q", remotePath)
	}
	parts := strings.SplitN(baseline.repo, "/", 2)
	if len(parts) != 2 || baseline.ref == "" {
		return fmt.Errorf("cannot determine previous source for worker %q", remotePath)
	}
	oldPath := path.Join(baseline.sourceDir, filepath.ToSlash(relativeRemote))
	previous, err := downloader(ctx, parts[0], parts[1], oldPath, baseline.ref)
	if err != nil {
		return fmt.Errorf("cannot verify previous worker %q: %w", remotePath, err)
	}
	modified := !bytes.Equal(previous, local)
	if strings.HasSuffix(remotePath, ".md") {
		modified = hasLocalModifications(string(previous), string(local), baseline.repo+"/"+oldPath+"@"+baseline.ref, filepath.Dir(targetPath), false)
	}
	if modified {
		return fmt.Errorf("worker %q has local changes; use --no-merge to overwrite", targetPath)
	}
	return nil
}

// fetchWorkflowProgram installs a program relative to the installed workflow, not
// the source repository root. downloader must use the workflow's resolved ref.
func workflowProgramPath(content string) (string, error) {
	frontmatter, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		return "", nil // Leave malformed workflow diagnostics to the compiler.
	}
	raw, exists := frontmatter.Frontmatter["program"]
	if !exists {
		return "", nil
	}
	program, ok := raw.(string)
	if !ok || len(program) > 256 || !fetchedProgramPathPattern.MatchString(program) {
		return "", fmt.Errorf("invalid program path %q: expected a relative JavaScript or TypeScript path below the workflow directory", raw)
	}
	return program, nil
}

func fetchWorkflowProgram(ctx context.Context, content string, spec *WorkflowSpec, remoteWorkflowPath, targetDir, relativeWorkflowPath string, force bool, tracker *FileTracker, downloader fileDownloadFn) error {
	program, err := workflowProgramPath(content)
	if err != nil || program == "" {
		return err
	}
	relativePath := path.Join(path.Dir(filepath.ToSlash(relativeWorkflowPath)), program)
	if !filepath.IsLocal(filepath.FromSlash(relativePath)) {
		return fmt.Errorf("program path %q escapes workflow directory", program)
	}
	parts := strings.SplitN(spec.RepoSlug, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return errors.New("cannot fetch program without a source repository")
	}
	ref := resolveRemoteWorkflowRef(ctx, spec)
	remotePath := path.Join(path.Dir(remoteWorkflowPath), program)
	data, err := downloader(ctx, parts[0], parts[1], remotePath, ref)
	if err != nil {
		return fmt.Errorf("failed to fetch program %s@%s: %w", remotePath, ref, err)
	}
	if baseline, ok := ctx.Value(programUpdateBaselineKey{}).(programUpdateBaseline); ok && force {
		localPath := filepath.Join(targetDir, filepath.FromSlash(relativePath))
		if _, statErr := os.Lstat(localPath); statErr == nil {
			local, readErr := fileutil.ReadFileWithinBase(targetDir, localPath)
			if readErr != nil {
				return fmt.Errorf("cannot safely read existing program %q: %w", relativePath, readErr)
			}
			if !bytes.Equal(local, data) {
				relativeRemote, relErr := filepath.Rel(filepath.FromSlash(baseline.latestDir), filepath.FromSlash(remotePath))
				if relErr != nil || !filepath.IsLocal(relativeRemote) {
					return fmt.Errorf("cannot determine previous source for program %q", relativePath)
				}
				oldParts := strings.SplitN(baseline.repo, "/", 2)
				if len(oldParts) != 2 || baseline.ref == "" {
					return fmt.Errorf("cannot determine previous source for program %q", relativePath)
				}
				oldPath := path.Join(baseline.sourceDir, filepath.ToSlash(relativeRemote))
				previous, previousErr := downloader(ctx, oldParts[0], oldParts[1], oldPath, baseline.ref)
				if previousErr != nil || !bytes.Equal(local, previous) {
					return fmt.Errorf("program %q has local changes or no available base; use --no-merge to overwrite", relativePath)
				}
			}
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return fmt.Errorf("cannot inspect program %q: %w", relativePath, statErr)
		}
	}
	return writeWorkflowProgram(targetDir, relativePath, data, force, tracker)
}

func copyLocalWorkflowProgram(content, sourceDir, targetDir string, force bool, tracker *FileTracker) error {
	program, err := workflowProgramPath(content)
	if err != nil || program == "" {
		return err
	}
	data, err := fileutil.ReadFileWithinBase(sourceDir, filepath.Join(sourceDir, filepath.FromSlash(program)))
	if err != nil {
		return fmt.Errorf("failed to read local program %q: %w", program, err)
	}
	return writeWorkflowProgram(targetDir, program, data, force, tracker)
}

func writeWorkflowProgram(targetDir, relativePath string, data []byte, force bool, tracker *FileTracker) error {
	root, err := os.OpenRoot(targetDir)
	if err != nil {
		return fmt.Errorf("failed to open workflow directory: %w", err)
	}
	defer root.Close()
	baseInfo, err := os.Lstat(targetDir)
	if err != nil {
		return fmt.Errorf("invalid workflow directory: %w", err)
	}
	rootInfo, err := root.Stat(".")
	if err != nil {
		return fmt.Errorf("invalid workflow directory: %w", err)
	}
	if baseInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(baseInfo, rootInfo) {
		return fmt.Errorf("invalid workflow directory %q: symlink or changed while opening", targetDir)
	}

	relativePath = filepath.FromSlash(relativePath)
	if !filepath.IsLocal(relativePath) {
		return fmt.Errorf("invalid program destination %q", relativePath)
	}
	// Refuse symlinks, including dangling ones, before creating or replacing files.
	current := ""
	for part := range strings.SplitSeq(relativePath, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, statErr := root.Lstat(current)
		if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
			return fmt.Errorf("invalid program destination %q: %w", relativePath, statErr)
		}
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("invalid program destination %q: symlink %q", relativePath, current)
		}
	}
	info, err := root.Lstat(relativePath)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if exists {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("program destination %q is not a regular file", relativePath)
		}
		existing, err := root.ReadFile(relativePath)
		if err != nil {
			return err
		}
		if bytes.Equal(existing, data) {
			return nil
		}
		if !force {
			return fmt.Errorf("program %q already exists with different content; use --force to overwrite", relativePath)
		}
	}
	if err := root.MkdirAll(filepath.Dir(relativePath), constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create program directory: %w", err)
	}
	fullPath := filepath.Join(targetDir, relativePath)
	if tracker != nil {
		if exists {
			absPath, err := filepath.Abs(fullPath)
			if err != nil {
				return err
			}
			if !slices.Contains(tracker.CreatedFiles, absPath) {
				tracker.TrackModified(fullPath)
			}
		} else {
			tracker.TrackCreated(fullPath)
		}
	}
	flags := os.O_WRONLY | os.O_CREATE
	if exists {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	file, err := root.OpenFile(relativePath, flags, constants.FilePermSensitive)
	if err != nil {
		return fmt.Errorf("failed to open program %q: %w", relativePath, err)
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("failed to write program %q: %w", relativePath, err)
	}
	return nil
}
