package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// HealthCacheEvidence distinguishes reusable observations from derived metrics.
type HealthCacheEvidence struct {
	Status          string    `json:"status"`
	ProducerVersion string    `json:"producer_version,omitempty"`
	Attempt         int       `json:"attempt,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitzero"`
}

func readHealthCache(dir string) (*RunSummary, string) {
	data, err := os.ReadFile(filepath.Join(dir, runSummaryFileName))
	if err != nil {
		reason := "unreadable"
		if errors.Is(err, fs.ErrNotExist) {
			reason = "missing"
		}
		healthLog.Printf("Health cache unavailable: dir=%s reason=%s", dir, reason)
		return nil, reason
	}
	var summary RunSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		healthLog.Printf("Health cache invalid: dir=%s", dir)
		return nil, "invalid"
	}
	return &summary, ""
}

func recordHealthCacheEvidence(run WorkflowRun, summary *RunSummary, status string, collection *HealthCollection) {
	if collection == nil {
		return
	}
	if collection.CacheEvidence == nil {
		collection.CacheEvidence = make(map[int64]HealthCacheEvidence)
	}
	if collection.cacheSummaries == nil {
		collection.cacheSummaries = make(map[healthRunAttempt]*RunSummary)
	}
	if collection.cacheSessionCompatibility == nil {
		collection.cacheSessionCompatibility = make(map[healthRunAttempt]bool)
	}
	collection.cacheSessionCompatibility[healthRunAttempt{id: run.DatabaseID, attempt: run.Attempt}] =
		status == "missing" || status == "compatible" || status == "version_mismatch_raw_jobs_only"
	evidence := HealthCacheEvidence{Status: status, Attempt: run.Attempt, UpdatedAt: run.UpdatedAt}
	if summary != nil {
		evidence.ProducerVersion = summary.CLIVersion
	}
	collection.CacheEvidence[run.DatabaseID] = evidence
}

func renderHealthCacheEvidence(w io.Writer, evidence map[int64]HealthCacheEvidence) error {
	counts := make(map[string]int)
	for _, item := range evidence {
		counts[item.Status]++
	}
	var statuses []string
	for status := range counts {
		statuses = append(statuses, status)
	}
	slices.Sort(statuses)
	for _, status := range statuses {
		if _, err := fmt.Fprintf(w, "[health_cache] status=%s runs=%d\n", status, counts[status]); err != nil {
			return err
		}
	}
	return nil
}
