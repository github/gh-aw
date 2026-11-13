package cli

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/github/gh-aw/pkg/console"
)

type logsCollectionStats struct {
	discoveredRuns    atomic.Int64
	downloadedReports atomic.Int64
	cachedReports     atomic.Int64
	// downloadCount, totalDownloadNanos, maxDownloadNanos, totalDownloadBytes, and
	// maxDownloadBytes track artifact-download timing and size across runs that were
	// actually downloaded this invocation (not served from the on-disk cache), so an
	// avg/max summary can be rendered at the end of the run.
	downloadCount      atomic.Int64
	totalDownloadNanos atomic.Int64
	maxDownloadNanos   atomic.Int64
	totalDownloadBytes atomic.Int64
	maxDownloadBytes   atomic.Int64
}

func (s *logsCollectionStats) recordDiscovered(count int) {
	if s != nil {
		s.discoveredRuns.Add(int64(count))
	}
}

func (s *logsCollectionStats) recordResult(result DownloadResult) {
	if s == nil {
		return
	}
	if result.Cached && !result.Skipped {
		s.cachedReports.Add(1)
		return
	}
	if !result.Skipped && result.Error == nil {
		s.downloadedReports.Add(1)
		if result.Run.DownloadDuration > 0 {
			s.recordDownloadStats(result.Run.DownloadDuration, result.Run.DownloadSizeBytes)
		}
	}
}

// recordDownloadStats accumulates per-run download duration and size so that
// renderLogsDownloadStatsSummary can report avg/max values at the end of the run.
func (s *logsCollectionStats) recordDownloadStats(duration time.Duration, sizeBytes int64) {
	if s == nil {
		return
	}
	s.downloadCount.Add(1)
	s.totalDownloadNanos.Add(duration.Nanoseconds())
	s.totalDownloadBytes.Add(sizeBytes)
	atomicMaxInt64(&s.maxDownloadNanos, duration.Nanoseconds())
	atomicMaxInt64(&s.maxDownloadBytes, sizeBytes)
}

// atomicMaxInt64 atomically sets *addr to value if value is greater than the
// current contents, using a compare-and-swap retry loop.
func atomicMaxInt64(addr *atomic.Int64, value int64) {
	for {
		current := addr.Load()
		if value <= current {
			return
		}
		if addr.CompareAndSwap(current, value) {
			return
		}
	}
}

func renderLogsCollectionStats(stats *logsCollectionStats) {
	if stats == nil {
		return
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf(
		"Runs: %d discovered; reports: %d downloaded, %d skipped because cached analyses were reused",
		stats.discoveredRuns.Load(), stats.downloadedReports.Load(), stats.cachedReports.Load(),
	)))
}

// gitHubAPIRateLimitCostEstimate sums the core GitHub API requests consumed
// across one or more rate-limit reports (Start/End snapshots taken around the
// logs command). Returns ok=false when no populated report is available.
func gitHubAPIRateLimitCostEstimate(reports []*GitHubAPIRateLimitReport) (int, bool) {
	var total int
	var found bool
	for _, report := range reports {
		if report == nil || report.Start == nil || report.End == nil {
			continue
		}
		diff := report.End.Used - report.Start.Used
		if diff < 0 || report.End.Reset != report.Start.Reset {
			// The rate-limit window reset mid-run (Used wrapped back down, or the
			// reset timestamp itself moved even though Used happened to still be
			// >= Start.Used); fall back to the ending value as a lower-bound
			// approximation rather than mixing counters from different windows.
			diff = report.End.Used
		}
		total += diff
		found = true
	}
	return total, found
}

// formatDownloadByteSize renders a byte count in a compact human-readable form
// (B, KB, MB, GB) for the end-of-run download stats summary.
func formatDownloadByteSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(bytes)/float64(div), "KMGTPE"[exp]) //nolint:uncheckedsliceindex // exp is bounded by the int64 byte range.
}

// renderLogsDownloadStatsSummary prints an end-of-run informational line
// summarizing per-run artifact download duration and size (avg/max across runs
// actually downloaded this invocation), plus an estimated GitHub API rate-limit
// cost per run derived from the provided rate-limit reports. It is a no-op when
// no runs were downloaded or stats were not tracked.
func renderLogsDownloadStatsSummary(stats *logsCollectionStats, reports ...*GitHubAPIRateLimitReport) {
	if stats == nil {
		return
	}
	count := stats.downloadCount.Load()
	if count == 0 {
		return
	}
	avgDuration := time.Duration(stats.totalDownloadNanos.Load() / count)
	maxDuration := time.Duration(stats.maxDownloadNanos.Load())
	avgSize := stats.totalDownloadBytes.Load() / count
	maxSize := stats.maxDownloadBytes.Load()
	msg := fmt.Sprintf(
		"Download stats: avg %s (max %s) per run; avg size %s (max %s) per run",
		avgDuration.Round(time.Millisecond), maxDuration.Round(time.Millisecond),
		formatDownloadByteSize(avgSize), formatDownloadByteSize(maxSize),
	)
	if calls, ok := gitHubAPIRateLimitCostEstimate(reports); ok {
		msg += fmt.Sprintf("; GitHub API cost estimate: ~%.1f requests/run", float64(calls)/float64(count))
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(msg))
}
