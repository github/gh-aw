// This file provides command-line interface functionality for gh-aw.
// This file (audit_report_graders.go) reads the grader results produced by the agent job
// (grader_results.json, mirrored into the usage artifact by the conclusion job) and exposes
// them for display in the audit command and in the audit JSON report.

package cli

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var gradersDataLog = logger.New("cli:audit_report_graders")

// GraderResult is a single grader outcome surfaced in the audit report.
//
// The operational-value fields (Source, Implementation, Observation, Diagnostics,
// BaselineValue, DeltaFromBaseline) mirror the grader result contract written to
// grader_results.json so that `gh aw logs --json` and the cached JSONL records can
// reproduce a grader result without re-reading the artifact. Field names for those
// keys intentionally match the artifact contract.
type GraderResult struct {
	ID                string                `json:"id"`
	Name              string                `json:"name,omitempty"`
	Status            string                `json:"status"`
	Value             *float64              `json:"value,omitempty"`
	Unit              string                `json:"unit,omitempty"`
	Passed            *bool                 `json:"passed,omitempty"`
	Direction         string                `json:"direction,omitempty"`
	Threshold         *float64              `json:"threshold,omitempty"`
	Message           string                `json:"message,omitempty"`
	Error             string                `json:"error,omitempty"`
	Source            string                `json:"source,omitempty"`
	Implementation    *GraderImplementation `json:"implementation,omitempty"`
	Observation       map[string]any        `json:"observation,omitempty"`
	Diagnostics       map[string]any        `json:"diagnostics,omitempty"`
	BaselineValue     *float64              `json:"baselineValue,omitempty"`
	DeltaFromBaseline *float64              `json:"deltaFromBaseline,omitempty"`
}

// GraderImplementation identifies the grader runtime that produced a result.
type GraderImplementation struct {
	ID      string `json:"id,omitempty"`
	Version int    `json:"version,omitempty"`
	Digest  string `json:"digest,omitempty"`
}

// GradersData aggregates the grader results recorded for a single workflow run.
type GradersData struct {
	Version          int            `json:"version,omitempty"`
	Results          []GraderResult `json:"results"`
	Total            int            `json:"total"`
	Passed           int            `json:"passed"`
	Failed           int            `json:"failed"`
	ErrorCount       int            `json:"error_count"`
	UnavailableCount int            `json:"unavailable_count"`
}

// graderArtifactFullResult mirrors the on-disk shape of a grader_results.json entry.
// It extends the subset parsed for experiment observations with the display fields
// (name, unit, passed, message, error) used by the audit report.
type graderArtifactFullResult struct {
	ID                string                `json:"id"`
	Name              string                `json:"name"`
	Status            string                `json:"status"`
	Value             json.RawMessage       `json:"value"`
	Unit              string                `json:"unit"`
	Passed            *bool                 `json:"passed"`
	Message           string                `json:"message"`
	Error             string                `json:"error"`
	Source            string                `json:"source"`
	Implementation    *GraderImplementation `json:"implementation"`
	Observation       map[string]any        `json:"observation"`
	Diagnostics       map[string]any        `json:"diagnostics"`
	BaselineValue     json.RawMessage       `json:"baselineValue"`
	DeltaFromBaseline json.RawMessage       `json:"deltaFromBaseline"`
}

type graderArtifactFullDocument struct {
	Version int                        `json:"version"`
	Results []graderArtifactFullResult `json:"results"`
}

type graderManifestEntry struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Unit      string   `json:"unit"`
	Direction string   `json:"direction"`
	Threshold *float64 `json:"threshold"`
}

type graderManifestDocument struct {
	Graders []graderManifestEntry `json:"graders"`
}

// graderArtifactDirCandidates returns the directories, relative to a run's log directory,
// that may contain grader output files. The order reflects preference: the compact usage
// artifact first (it is downloaded for every run), then the dedicated graders artifact,
// then the unified agent artifact, then a flattened run root.
func graderArtifactDirCandidates(runDir string) []string {
	usage := constants.UsageArtifactName.String()
	graders := constants.GradersDirName.String()
	candidates := []string{
		filepath.Join(runDir, usage, graders),
		filepath.Join(runDir, graders),
		filepath.Join(runDir, "agent", graders),
		runDir,
	}

	// workflow_call runs prefix artifact names with a hash, e.g. "{hash}-usage".
	entries, err := os.ReadDir(runDir)
	if err != nil {
		return candidates
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, "-"+usage) {
			candidates = append(candidates, filepath.Join(runDir, name, graders))
		}
		if strings.HasSuffix(name, "-"+graders) {
			candidates = append(candidates, filepath.Join(runDir, name))
		}
	}
	return candidates
}

// findGraderFile returns the first existing path for filename across the known grader
// artifact directories, or "" when the file is not present.
func findGraderFile(runDir, filename string) string {
	if runDir == "" {
		return ""
	}
	for _, dir := range graderArtifactDirCandidates(runDir) {
		candidate := filepath.Join(dir, filename)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// runHasGraders reports whether a run's output directory contains grader results.
func runHasGraders(runDir string) bool {
	return findGraderFile(runDir, constants.GraderResultsFilename.String()) != ""
}

// extractGradersData reads grader results (and the grader manifest, when available) from
// a run's log directory and returns a populated GradersData, or nil when no grader
// results are present or the results file is unusable.
func extractGradersData(logsPath string) *GradersData {
	if logsPath == "" {
		return nil
	}

	resultsPath := findGraderFile(logsPath, constants.GraderResultsFilename.String())
	if resultsPath == "" {
		gradersDataLog.Printf("No grader results found in: %s", logsPath)
		return nil
	}
	gradersDataLog.Printf("Reading grader results from: %s", resultsPath)

	info, err := os.Stat(resultsPath)
	if err != nil {
		return nil
	}
	if info.Size() > maxGraderResultsBytes {
		gradersDataLog.Printf("Grader results too large (%d bytes), skipping: %s", info.Size(), resultsPath)
		return nil
	}
	data, err := os.ReadFile(resultsPath) // #nosec G304 -- path resolved beneath the run's logs directory
	if err != nil {
		gradersDataLog.Printf("Failed to read grader results: %v", err)
		return nil
	}

	var doc graderArtifactFullDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		gradersDataLog.Printf("Failed to parse grader results: %v", err)
		return nil
	}
	if len(doc.Results) == 0 {
		return nil
	}

	manifest := loadGraderManifestEntries(logsPath)
	graders := &GradersData{Version: doc.Version}
	for _, result := range doc.Results {
		graders.Results = append(graders.Results, buildGraderResult(result, manifest[result.ID]))
	}
	slices.SortFunc(graders.Results, func(a, b GraderResult) int { return strings.Compare(a.ID, b.ID) })
	countGraderStatuses(graders)
	gradersDataLog.Printf("Parsed %d grader result(s)", len(graders.Results))
	return graders
}

// loadGraderManifestEntries reads the grader manifest, when present, keyed by grader ID.
// The manifest supplies declared metadata (direction, threshold) that the results file
// does not repeat. A missing or malformed manifest is not an error.
func loadGraderManifestEntries(logsPath string) map[string]graderManifestEntry {
	entries := make(map[string]graderManifestEntry)
	manifestPath := findGraderFile(logsPath, constants.GraderManifestFilename.String())
	if manifestPath == "" {
		return entries
	}
	info, err := os.Stat(manifestPath)
	if err != nil {
		return entries
	}
	if info.Size() > maxGraderResultsBytes {
		gradersDataLog.Printf("Grader manifest too large (%d bytes), skipping: %s", info.Size(), manifestPath)
		return entries
	}
	data, err := os.ReadFile(manifestPath) // #nosec G304 -- path resolved beneath the run's logs directory
	if err != nil {
		gradersDataLog.Printf("Failed to read grader manifest: %v", err)
		return entries
	}
	var manifest graderManifestDocument
	if err := json.Unmarshal(data, &manifest); err != nil {
		gradersDataLog.Printf("Failed to parse grader manifest: %v", err)
		return entries
	}
	for _, entry := range manifest.Graders {
		if entry.ID != "" {
			entries[entry.ID] = entry
		}
	}
	return entries
}

func buildGraderResult(result graderArtifactFullResult, manifest graderManifestEntry) GraderResult {
	summary := GraderResult{
		ID:             result.ID,
		Name:           result.Name,
		Status:         result.Status,
		Unit:           result.Unit,
		Passed:         result.Passed,
		Message:        result.Message,
		Error:          result.Error,
		Direction:      manifest.Direction,
		Threshold:      manifest.Threshold,
		Source:         result.Source,
		Implementation: result.Implementation,
		Observation:    result.Observation,
		Diagnostics:    result.Diagnostics,
	}
	if summary.Name == "" {
		summary.Name = manifest.Name
	}
	// The declared unit is only a fallback: a unit recorded by the grader runtime is
	// authoritative and must survive serialization unchanged.
	if summary.Unit == "" {
		summary.Unit = manifest.Unit
	}
	if value, ok := parseGraderValue(result.Value); ok {
		summary.Value = &value
	}
	if value, ok := parseGraderValue(result.BaselineValue); ok {
		summary.BaselineValue = &value
	}
	if value, ok := parseGraderValue(result.DeltaFromBaseline); ok {
		summary.DeltaFromBaseline = &value
	}
	return summary
}

// parseGraderValue decodes a grader value, accepting numbers and booleans (booleans are
// normalized to 1/0 exactly as experiment observations do). Null, missing, and non-finite
// values are reported as absent.
func parseGraderValue(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		var boolValue bool
		if err := json.Unmarshal(raw, &boolValue); err != nil {
			return 0, false
		}
		if boolValue {
			return 1, true
		}
		return 0, true
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

func countGraderStatuses(graders *GradersData) {
	graders.Total = len(graders.Results)
	for _, result := range graders.Results {
		switch result.Status {
		case "pass":
			graders.Passed++
		case "fail":
			graders.Failed++
		case "unavailable":
			graders.UnavailableCount++
		default:
			graders.ErrorCount++
		}
	}
}

// formatGraderValue renders a grader value with its unit for console output.
func formatGraderValue(result GraderResult) string {
	if result.Value == nil {
		return "n/a"
	}
	text := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", *result.Value), "0"), ".")
	if text == "" || text == "-" {
		text = "0"
	}
	if result.Unit != "" {
		text += result.Unit
	}
	return text
}
