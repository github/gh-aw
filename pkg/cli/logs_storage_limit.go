// This file provides disk-usage limiting for the logs command.
package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
)

const bytesPerMegabyte int64 = 1024 * 1024
const maxReportedLogsCacheFiles = 20

var errLogsStorageLimitReached = errors.New("logs storage limit reached")

type logsStorageLimit struct {
	outputDir      string
	maxBytes       int64
	pruneOlderRuns bool
	// mu guards usedBytes/initialized bookkeeping only. It is held briefly around
	// the pre-download budget check and the post-download usage update, never
	// across the download itself, so concurrent downloads (e.g. from
	// downloadRunArtifactsConcurrent's pool) can still run in parallel; only the
	// shared byte counter is serialized. This means the budget can overshoot by up
	// to the combined size of the in-flight downloads that were admitted before
	// the limit was reached, which is an accepted trade-off for a soft cap.
	mu        sync.Mutex
	reached   atomic.Bool
	initErr   error
	usedBytes int64
	active    int
	completed map[string]struct{}
}

type logsFolderSize struct {
	name string
	size int64
}

type logsFileSize struct {
	path string
	size int64
}

type logsPruneCandidate struct {
	path string
	size int64
}

var prunableLogsCacheDirectories = map[string]struct{}{
	"agent":         {},
	"aw-mcp":        {},
	"mcp-logs":      {},
	"mcp-scripts":   {},
	"pi-agent-dir":  {},
	"proxy-logs":    {},
	"sandbox":       {},
	"workflow-logs": {},
}

var essentialLogsCacheFiles = map[string]struct{}{
	constants.AgentOutputFilename.String():      {},
	constants.TokenUsageFilename.String():       {},
	"aw_info.json":                              {},
	constants.EvalsResultFilename.String():      {},
	constants.GithubRateLimitsFilename.String(): {},
	constants.GraderManifestFilename.String():   {},
	constants.GraderResultsFilename.String():    {},
	jobsAPIResponseFileName:                     {},
	runAPIResponseFileName:                      {},
	runSummaryFileName:                          {},
	"safe-output-items.jsonl":                   {},
	constants.SafeOutputsFilename.String():      {},
	constants.TemporaryIdMapFilename.String():   {},
}

func newLogsStorageLimit(outputDir string, maxStorageMB int, pruneOlderRuns bool) *logsStorageLimit {
	if maxStorageMB <= 0 {
		return nil
	}
	limit := &logsStorageLimit{
		outputDir:      outputDir,
		maxBytes:       int64(maxStorageMB) * bytesPerMegabyte,
		pruneOlderRuns: pruneOlderRuns,
		completed:      make(map[string]struct{}),
	}
	limit.initErr = limit.initialize()
	return limit
}

func validateMaxStorageMB(maxStorageMB int) error {
	if maxStorageMB < 0 || int64(maxStorageMB) > math.MaxInt64/bytesPerMegabyte {
		return fmt.Errorf("invalid --max-storage value %d: expected a non-negative number of MB", maxStorageMB)
	}
	return nil
}

func logsDirectorySize(path string) (int64, error) {
	var total int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if info == nil || !info.Mode().IsRegular() {
			return nil
		}
		if info.Size() > math.MaxInt64-total {
			return errors.New("logs directory size exceeds supported maximum")
		}
		total += info.Size()
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	return total, err
}

func compareLogsFolderSizeDesc(a, b logsFolderSize) int {
	if a.size == b.size {
		return cmp.Compare(a.name, b.name)
	}
	return cmp.Compare(b.size, a.size)
}

func compareLogsFileSizeDesc(a, b logsFileSize) int {
	if a.size == b.size {
		return cmp.Compare(a.path, b.path)
	}
	return cmp.Compare(b.size, a.size)
}

// computeLogsCacheStats walks the logs cache directory once, computing the total
// size, per top-level folder sizes, the fileLimit largest files (by size, then
// relative path), and the total file count. Doing this in a single pass avoids
// the redundant traversals that would result from measuring the total size, the
// per-folder sizes, and the largest files independently.
func computeLogsCacheStats(path string, fileLimit int) (int64, []logsFolderSize, []logsFileSize, int, error) {
	folderSizes, err := initialLogsFolderSizes(path)
	if err != nil {
		return 0, nil, nil, 0, err
	}
	if folderSizes == nil {
		return 0, nil, nil, 0, nil
	}

	var totalSize int64
	var files []logsFileSize
	fileCount := 0
	err = filepath.Walk(path, func(filePath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if info == nil || !info.Mode().IsRegular() {
			return nil
		}
		if info.Size() > math.MaxInt64-totalSize {
			return errors.New("logs directory size exceeds supported maximum")
		}
		totalSize += info.Size()

		relativePath, relErr := filepath.Rel(path, filePath)
		if relErr != nil {
			return relErr
		}
		fileCount++
		if top, rest, ok := strings.Cut(relativePath, string(filepath.Separator)); ok && rest != "" {
			folderSizes[top] += info.Size()
		}
		if fileLimit > 0 {
			files = append(files, logsFileSize{path: relativePath, size: info.Size()})
		}
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil, nil, 0, nil
	}
	if err != nil {
		return 0, nil, nil, 0, err
	}

	folders := make([]logsFolderSize, 0, len(folderSizes))
	for name, size := range folderSizes {
		folders = append(folders, logsFolderSize{name: name, size: size})
	}
	slices.SortFunc(folders, compareLogsFolderSizeDesc)

	slices.SortFunc(files, compareLogsFileSizeDesc)
	if len(files) > fileLimit {
		files = files[:fileLimit]
	}

	return totalSize, folders, files, fileCount, nil
}

func initialLogsFolderSizes(path string) (map[string]int64, error) {
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	folderSizes := make(map[string]int64, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			folderSizes[entry.Name()] = 0
		}
	}
	return folderSizes, nil
}

func (l *logsStorageLimit) runDownload(ctx context.Context, storagePath string, download func() error) error {
	return l.runDownloadWithPruning(ctx, storagePath, download, true, false)
}

func (l *logsStorageLimit) runDownloadDeferred(ctx context.Context, storagePath string, download func() error) error {
	return l.runDownloadWithPruning(ctx, storagePath, download, false, false)
}

func (l *logsStorageLimit) runDownloadDeferredReserved(ctx context.Context, storagePath string, download func() error) error {
	return l.runDownloadWithPruning(ctx, storagePath, download, false, true)
}

func (l *logsStorageLimit) runDownloadWithPruning(ctx context.Context, storagePath string, download func() error, markCompleted, reserved bool) error {
	if l == nil {
		return download()
	}

	select {
	case <-ctx.Done():
		return contextCause(ctx)
	default:
	}

	if !reserved {
		if err := l.reserve(storagePath); err != nil {
			return err
		}
	}

	sizeBefore, err := logsDirectorySize(storagePath)
	if err != nil {
		return fmt.Errorf("failed to measure logs storage path %q: %w", storagePath, err)
	}
	downloadErr := download()
	sizeAfter, sizeErr := logsDirectorySize(storagePath)
	if sizeErr != nil {
		return errors.Join(downloadErr, fmt.Errorf("failed to measure logs storage: %w", sizeErr))
	}
	pruneErr := l.recordUsage(storagePath, sizeAfter-sizeBefore, markCompleted)
	return errors.Join(downloadErr, pruneErr)
}

// reserve checks the shared budget state and protects the download path from
// pruning under a short-lived lock. It never holds the lock across the actual
// download, so concurrent downloads keep running in parallel.
func (l *logsStorageLimit) reserve(storagePath string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.initErr != nil {
		return l.initErr
	}
	cleanPath := filepath.Clean(storagePath)
	_, wasCompleted := l.completed[cleanPath]
	delete(l.completed, cleanPath)
	l.active++
	restoreAndReject := func() error {
		l.active--
		if wasCompleted {
			l.completed[cleanPath] = struct{}{}
		}
		return errLogsStorageLimitReached
	}
	if l.reached.Load() && !l.pruneOlderRuns && l.active == 1 && !wasCompleted {
		return restoreAndReject()
	}
	if l.usedBytes >= l.maxBytes {
		if err := l.pruneLocked(storagePath); err != nil {
			l.active--
			return err
		}
		if l.usedBytes >= l.maxBytes && l.active == 1 && !wasCompleted {
			return restoreAndReject()
		}
	}
	return nil
}

func (l *logsStorageLimit) initialize() error {
	size, folders, files, fileCount, err := computeLogsCacheStats(l.outputDir, maxReportedLogsCacheFiles)
	if err != nil {
		return fmt.Errorf("failed to measure logs cache: %w", err)
	}
	l.reportStartingUsage(size, folders, files, fileCount)
	l.usedBytes = size
	err = filepath.Walk(l.outputDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if info != nil && info.IsDir() && strings.HasPrefix(info.Name(), "run-") {
			l.completed[filepath.Clean(path)] = struct{}{}
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to identify completed runs: %w", err)
	}
	if l.usedBytes >= l.maxBytes {
		freed, err := pruneLogsCache(l.outputDir, l.usedBytes-l.maxBytes+1, nil)
		if err != nil {
			return fmt.Errorf("failed to prune logs cache: %w", err)
		}
		l.recordPrunedUsage(freed)
	}
	if l.usedBytes >= l.maxBytes && l.active == 0 {
		l.markReached(l.usedBytes)
	}
	return nil
}

// recordUsage applies a completed download's byte delta and removes non-essential
// data from paths that are safe to prune when the cache reaches the budget.
func (l *logsStorageLimit) recordUsage(storagePath string, delta int64, markCompleted bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.usedBytes += delta
	if markCompleted {
		cleanPath := filepath.Clean(storagePath)
		l.active--
		l.completed[cleanPath] = struct{}{}
	}
	if l.usedBytes >= l.maxBytes {
		return l.pruneLocked(storagePath)
	}
	return nil
}

func (l *logsStorageLimit) finalizeDownload(storagePath string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	cleanPath := filepath.Clean(storagePath)
	l.active--
	l.completed[cleanPath] = struct{}{}
	if l.usedBytes < l.maxBytes {
		return nil
	}
	return l.pruneLocked(storagePath)
}

func (l *logsStorageLimit) pruneLocked(storagePath string) error {
	if l.usedBytes >= l.maxBytes {
		freed, err := pruneLogsCache(l.outputDir, l.usedBytes-l.maxBytes+1, l.completed)
		if err != nil {
			return fmt.Errorf("failed to prune logs cache: %w", err)
		}
		l.recordPrunedUsage(freed)
	}
	if l.pruneOlderRuns && l.usedBytes >= l.maxBytes {
		freed, err := l.pruneOldestRunsLocked(storagePath, l.usedBytes-l.maxBytes+1)
		if err != nil {
			return fmt.Errorf("failed to prune older logs runs: %w", err)
		}
		l.recordPrunedRunUsage(freed)
	}
	if l.usedBytes >= l.maxBytes && l.active == 0 {
		l.markReached(l.usedBytes)
	}
	return nil
}

type logsRunPruneCandidate struct {
	path  string
	runID int64
	size  int64
}

func (l *logsStorageLimit) pruneOldestRunsLocked(storagePath string, bytesToFree int64) (int64, error) {
	currentRunID, ok := logsRunIDFromPath(storagePath)
	if !ok || bytesToFree <= 0 {
		return 0, nil
	}

	candidates := make([]logsRunPruneCandidate, 0, len(l.completed))
	var availableBytes int64
	for completedPath := range l.completed {
		runID, ok := logsRunIDFromPath(completedPath)
		if !ok || runID >= currentRunID {
			continue
		}
		size, err := logsDirectorySize(completedPath)
		if err != nil {
			return 0, err
		}
		candidates = append(candidates, logsRunPruneCandidate{path: completedPath, runID: runID, size: size})
		availableBytes += size
	}
	if availableBytes < bytesToFree {
		return 0, nil
	}
	slices.SortFunc(candidates, func(a, b logsRunPruneCandidate) int {
		if a.runID == b.runID {
			return cmp.Compare(a.path, b.path)
		}
		return cmp.Compare(a.runID, b.runID)
	})

	var freed int64
	for _, candidate := range candidates {
		if freed >= bytesToFree {
			break
		}
		if err := os.RemoveAll(candidate.path); err != nil {
			l.recordPrunedRunUsage(freed)
			return freed, err
		}
		delete(l.completed, candidate.path)
		freed += candidate.size
	}
	return freed, nil
}

func logsRunIDFromPath(path string) (int64, bool) {
	name := filepath.Base(filepath.Clean(path))
	value, ok := strings.CutPrefix(name, "run-")
	if !ok {
		return 0, false
	}
	runID, err := strconv.ParseInt(value, 10, 64)
	return runID, err == nil
}

func (l *logsStorageLimit) reportStartingUsage(size int64, folders []logsFolderSize, files []logsFileSize, fileCount int) {
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf(
		"Logs cache starting size: %s (maximum %s)",
		console.FormatFileSize(size), console.FormatFileSize(l.maxBytes),
	)))
	logsOrchestratorLog.Printf("Logs cache starting size: used=%d maximum=%d", size, l.maxBytes)
	for _, folder := range folders {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf(
			"Logs cache folder %s: %s",
			strconv.Quote(folder.name), console.FormatFileSize(folder.size),
		)))
		logsOrchestratorLog.Printf("Logs cache folder size: folder=%q size=%d", folder.name, folder.size)
	}
	if fileCount > len(files) {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf(
			"Logs cache files: showing %d largest of %d",
			len(files), fileCount,
		)))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf(
			"Logs cache files: %d",
			fileCount,
		)))
	}
	logsOrchestratorLog.Printf("Logs cache file count: total=%d reported=%d", fileCount, len(files))
	for _, file := range files {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf(
			"Logs cache file %s: %s",
			strconv.Quote(file.path), console.FormatFileSize(file.size),
		)))
		logsOrchestratorLog.Printf("Logs cache file size: path=%q size=%d", file.path, file.size)
	}
}

func (l *logsStorageLimit) recordPrunedUsage(freed int64) {
	if freed <= 0 {
		return
	}
	l.usedBytes -= freed
	if l.usedBytes < 0 {
		l.usedBytes = 0
	}
	if l.usedBytes < l.maxBytes {
		l.reached.Store(false)
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf(
		"Pruned %s of non-essential logs cache data", console.FormatFileSize(freed),
	)))
	logsOrchestratorLog.Printf("Pruned non-essential logs cache data: freed=%d remaining=%d", freed, l.usedBytes)
}

func (l *logsStorageLimit) recordPrunedRunUsage(freed int64) {
	if freed <= 0 {
		return
	}
	l.usedBytes -= freed
	if l.usedBytes < 0 {
		l.usedBytes = 0
	}
	if l.usedBytes < l.maxBytes {
		l.reached.Store(false)
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf(
		"Pruned %s by removing older logs runs", console.FormatFileSize(freed),
	)))
	logsOrchestratorLog.Printf("Pruned older logs runs: freed=%d remaining=%d", freed, l.usedBytes)
}

func pruneLogsCache(path string, bytesToFree int64, completedPaths map[string]struct{}) (int64, error) {
	if bytesToFree <= 0 {
		return 0, nil
	}

	var candidates []logsPruneCandidate
	err := filepath.Walk(path, func(candidatePath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if info == nil || !info.Mode().IsRegular() {
			return nil
		}
		if !isInCompletedLogsCachePath(candidatePath, completedPaths) {
			return nil
		}
		relativePath, err := filepath.Rel(path, candidatePath)
		if err != nil {
			return err
		}
		if isPrunableLogsCacheFile(relativePath) {
			candidates = append(candidates, logsPruneCandidate{path: candidatePath, size: info.Size()})
		}
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	slices.SortFunc(candidates, func(a, b logsPruneCandidate) int {
		if a.size == b.size {
			return cmp.Compare(a.path, b.path)
		}
		return cmp.Compare(b.size, a.size)
	})
	var freed int64
	for _, candidate := range candidates {
		if freed >= bytesToFree {
			break
		}
		if err := os.Remove(candidate.path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return freed, err
		}
		freed += candidate.size
		if err := invalidatePrunedArtifactMarkers(candidate.path, path); err != nil {
			return freed, err
		}
		removeEmptyLogsCacheParents(filepath.Dir(candidate.path), path)
	}
	return freed, nil
}

func isInCompletedLogsCachePath(path string, completedPaths map[string]struct{}) bool {
	// A nil map means no active-path filtering is requested; an empty map means
	// filtering is enabled and all active paths are protected.
	if completedPaths == nil {
		return true
	}
	for completedPath := range completedPaths {
		relativePath, err := filepath.Rel(completedPath, path)
		if err == nil && relativePath != ".." && !filepath.IsAbs(relativePath) &&
			!strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func isPrunableLogsCacheFile(relativePath string) bool {
	base := filepath.Base(relativePath)
	if _, essential := essentialLogsCacheFiles[base]; essential {
		return false
	}
	parts := splitLogsCachePath(relativePath)
	for _, part := range parts {
		if _, prunable := prunableLogsCacheDirectories[part]; prunable {
			return true
		}
	}
	return false
}

func invalidatePrunedArtifactMarkers(prunedPath, root string) error {
	if slices.Contains(splitLogsCachePath(prunedPath), "workflow-logs") {
		return nil
	}
	root = filepath.Clean(root)
	for dir := filepath.Dir(prunedPath); ; dir = filepath.Dir(dir) {
		markerDir := filepath.Join(dir, downloadedArtifactsMarkerDir)
		entries, err := os.ReadDir(markerDir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() || !isAgentArtifactMarker(entry.Name()) {
					continue
				}
				if err := os.Remove(filepath.Join(markerDir, entry.Name())); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if dir == root || dir == "." || dir == filepath.Dir(dir) {
			return nil
		}
	}
}

func isAgentArtifactMarker(name string) bool {
	return name == string(ArtifactSetAll) ||
		artifactNameMatchesBase(name, constants.AgentArtifactName.String()) ||
		artifactNameMatchesBase(name, constants.AgentOutputFallbackArtifactName.String())
}

func splitLogsCachePath(path string) []string {
	var parts []string
	for path != "." && path != string(filepath.Separator) {
		dir, base := filepath.Split(path)
		if base != "" {
			parts = append(parts, base)
		}
		path = filepath.Clean(dir)
	}
	return parts
}

func removeEmptyLogsCacheParents(path, root string) {
	root = filepath.Clean(root)
	for path = filepath.Clean(path); path != root && path != "."; path = filepath.Dir(path) {
		if err := os.Remove(path); err != nil {
			return
		}
	}
}

func (l *logsStorageLimit) markReached(size int64) {
	if !l.reached.CompareAndSwap(false, true) {
		return
	}
	message := fmt.Sprintf(
		"Logs storage limit reached (%s used; maximum %s). Stopping new downloads.",
		console.FormatFileSize(size), console.FormatFileSize(l.maxBytes),
	)
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(message))
	logsOrchestratorLog.Printf("Logs storage limit reached: used=%d, maximum=%d", size, l.maxBytes)
}

func (l *logsStorageLimit) isReached() bool {
	return l != nil && l.reached.Load()
}
