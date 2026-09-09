package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
)

var operationalValueReportCacheSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type operationalValueReportWeeklyCache struct {
	SchemaVersion   int                                 `json:"schemaVersion"`
	Repository      string                              `json:"repository"`
	WorkflowID      string                              `json:"workflowId"`
	EvaluatorDigest string                              `json:"evaluatorDigest"`
	WeekStart       string                              `json:"weekStart"`
	WeekEnd         string                              `json:"weekEnd"`
	Observations    []operationalValueReportObservation `json:"observations"`
}

func operationalValueUTCWeekStart(value time.Time) time.Time {
	value = value.UTC()
	dayOffset := (int(value.Weekday()) + 6) % 7
	return time.Date(value.Year(), value.Month(), value.Day()-dayOffset, 0, 0, 0, 0, time.UTC)
}

func operationalValueReportWeeklyCachePath(cacheRoot, repository, workflowID, evaluatorDigest string, weekStart time.Time) (string, error) {
	segments := []string{workflowID, evaluatorDigest}
	owner, repo, ok := splitOperationalValueReportRepository(repository)
	if !ok {
		return "", fmt.Errorf("invalid repository for operational-value cache: %q", repository)
	}
	segments = append([]string{owner, repo}, segments...)
	for _, segment := range segments {
		if !operationalValueReportCacheSegmentPattern.MatchString(segment) {
			return "", fmt.Errorf("invalid operational-value cache path segment %q", segment)
		}
	}
	weekName := operationalValueUTCWeekStart(weekStart).Format("2006-01-02") + ".json"
	return filepath.Join(cacheRoot, owner, repo, workflowID, evaluatorDigest, weekName), nil
}

func splitOperationalValueReportRepository(repository string) (string, string, bool) {
	for index := 1; index < len(repository)-1; index++ {
		if repository[index] != '/' {
			continue
		}
		if repository[index+1:] == "" || strings.Contains(repository[index+1:], "/") {
			return "", "", false
		}
		return repository[:index], repository[index+1:], true
	}
	return "", "", false
}

func loadOperationalValueReportWeeklyCache(path, repository, workflowID, evaluatorDigest string, weekStart time.Time) ([]operationalValueReportObservation, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cannot inspect operational-value cache %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("operational-value cache %s must be a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("cannot read operational-value cache %s: %w", path, err)
	}
	var cache operationalValueReportWeeklyCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, false, fmt.Errorf("operational-value cache %s is malformed: %w", path, err)
	}
	expectedWeekStart := operationalValueUTCWeekStart(weekStart)
	if cache.SchemaVersion != operationalValueReportSchemaVersion ||
		cache.Repository != repository || cache.WorkflowID != workflowID ||
		cache.EvaluatorDigest != evaluatorDigest || cache.WeekStart != expectedWeekStart.Format(time.RFC3339) {
		return nil, false, nil
	}
	return cache.Observations, true, nil
}

func saveOperationalValueReportWeeklyCache(path, repository, workflowID, evaluatorDigest string, weekStart time.Time, observations []operationalValueReportObservation) error {
	weekStart = operationalValueUTCWeekStart(weekStart)
	cache := operationalValueReportWeeklyCache{
		SchemaVersion:   operationalValueReportSchemaVersion,
		Repository:      repository,
		WorkflowID:      workflowID,
		EvaluatorDigest: evaluatorDigest,
		WeekStart:       weekStart.Format(time.RFC3339),
		WeekEnd:         weekStart.AddDate(0, 0, 7).Format(time.RFC3339),
		Observations:    observations,
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot encode operational-value cache: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), constants.DirPermSensitive); err != nil {
		return fmt.Errorf("cannot create operational-value cache directory: %w", err)
	}
	tempFile, err := os.CreateTemp(filepath.Dir(path), ".operational-value-week-"+strconv.FormatInt(time.Now().UnixNano(), 10)+"-*")
	if err != nil {
		return fmt.Errorf("cannot create operational-value cache file: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	if err := tempFile.Chmod(constants.FilePermSensitive); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("cannot set operational-value cache permissions: %w", err)
	}
	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("cannot write operational-value cache: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("cannot close operational-value cache: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("cannot publish operational-value cache: %w", err)
	}
	return nil
}
