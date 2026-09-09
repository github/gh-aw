package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func writeGraderFiles(t *testing.T, dir string, results any, manifest any) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if results != nil {
		data, err := json.Marshal(results)
		if err != nil {
			t.Fatalf("failed to marshal results: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, constants.GraderResultsFilename.String()), data, 0o600); err != nil {
			t.Fatalf("failed to write results: %v", err)
		}
	}
	if manifest != nil {
		data, err := json.Marshal(manifest)
		if err != nil {
			t.Fatalf("failed to marshal manifest: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, constants.GraderManifestFilename.String()), data, 0o600); err != nil {
			t.Fatalf("failed to write manifest: %v", err)
		}
	}
}

func sampleGraderResults() map[string]any {
	return map[string]any{
		"version": 1,
		"results": []map[string]any{
			{"id": "turns", "name": "Turn count", "status": "pass", "value": 12, "unit": "turns", "passed": true},
			{"id": "cost", "name": "Cost", "status": "fail", "value": 3.5, "unit": "USD", "passed": false},
			{"id": "flaky", "status": "error", "value": nil, "error": "grader flaky failed"},
			{"id": "absent", "status": "unavailable", "value": nil, "message": "grader returned no value"},
		},
	}
}

func sampleGraderManifest() map[string]any {
	return map[string]any{
		"version": 1,
		"graders": []map[string]any{
			{"id": "cost", "name": "Cost", "unit": "USD", "direction": "lower_is_better", "threshold": 1.0},
		},
	}
}

func TestExtractGradersDataFromUsageArtifact(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	writeGraderFiles(t,
		filepath.Join(runDir, constants.UsageArtifactName.String(), constants.GradersDirName.String()),
		sampleGraderResults(), sampleGraderManifest())

	graders := extractGradersData(runDir)
	if graders == nil {
		t.Fatal("expected graders data to be extracted from the usage artifact")
	}
	if len(graders.Results) != 4 {
		t.Fatalf("expected 4 grader results, got %d", len(graders.Results))
	}
	if graders.Total != 4 || graders.Passed != 1 || graders.Failed != 1 || graders.ErrorCount != 1 || graders.UnavailableCount != 1 {
		t.Fatalf("unexpected status counts: %+v", graders)
	}
	data, err := json.Marshal(graders)
	if err != nil {
		t.Fatalf("failed to marshal graders data: %v", err)
	}
	jsonText := string(data)
	for _, want := range []string{`"total":4`, `"passed":1`, `"failed":1`} {
		if !strings.Contains(jsonText, want) {
			t.Fatalf("expected graders JSON to contain %s, got %s", want, jsonText)
		}
	}
	if strings.Contains(jsonText, "pass_count") || strings.Contains(jsonText, "fail_count") {
		t.Fatalf("graders JSON should use public count names, got %s", jsonText)
	}

	// Results are sorted by ID: absent, cost, flaky, turns
	cost := graders.Results[1]
	if cost.ID != "cost" {
		t.Fatalf("expected results sorted by id, got %q at index 1", cost.ID)
	}
	if cost.Value == nil || *cost.Value != 3.5 {
		t.Fatalf("expected cost value 3.5, got %v", cost.Value)
	}
	if cost.Direction != "lower_is_better" {
		t.Errorf("expected direction from manifest, got %q", cost.Direction)
	}
	if cost.Threshold == nil || *cost.Threshold != 1.0 {
		t.Errorf("expected threshold from manifest, got %v", cost.Threshold)
	}
	if formatGraderValue(cost) != "3.5USD" {
		t.Errorf("unexpected formatted value: %q", formatGraderValue(cost))
	}
	if formatGraderValue(graders.Results[0]) != "n/a" {
		t.Errorf("expected n/a for missing value, got %q", formatGraderValue(graders.Results[0]))
	}
}

func TestExtractGradersDataFromAgentArtifact(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	writeGraderFiles(t, filepath.Join(runDir, "agent", constants.GradersDirName.String()), sampleGraderResults(), nil)

	graders := extractGradersData(runDir)
	if graders == nil {
		t.Fatal("expected graders data to be extracted from the agent artifact")
	}
	if graders.Results[1].Direction != "" {
		t.Errorf("expected empty direction without a manifest, got %q", graders.Results[1].Direction)
	}
	if !runHasGraders(runDir) {
		t.Error("expected runHasGraders to report true")
	}
}

func TestExtractGradersDataMissingAndMalformed(t *testing.T) {
	t.Parallel()
	empty := t.TempDir()
	if extractGradersData(empty) != nil {
		t.Error("expected nil graders data when no results are present")
	}
	if runHasGraders(empty) {
		t.Error("expected runHasGraders to report false for an empty run dir")
	}
	if extractGradersData("") != nil {
		t.Error("expected nil graders data for an empty logs path")
	}

	malformed := t.TempDir()
	gradersDir := filepath.Join(malformed, constants.GradersDirName.String())
	if err := os.MkdirAll(gradersDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gradersDir, constants.GraderResultsFilename.String()), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("failed to write results: %v", err)
	}
	if extractGradersData(malformed) != nil {
		t.Error("expected nil graders data for malformed results")
	}
}

func TestExtractGradersDataSkipsOversizedManifest(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	gradersDir := filepath.Join(runDir, constants.UsageArtifactName.String(), constants.GradersDirName.String())
	writeGraderFiles(t, gradersDir, sampleGraderResults(), nil)
	if err := os.WriteFile(
		filepath.Join(gradersDir, constants.GraderManifestFilename.String()),
		[]byte(strings.Repeat("x", maxGraderResultsBytes+1)),
		0o600,
	); err != nil {
		t.Fatalf("failed to write oversized manifest: %v", err)
	}

	graders := extractGradersData(runDir)
	if graders == nil {
		t.Fatal("expected graders data to be extracted despite oversized manifest")
	}
	if graders.Results[1].Direction != "" || graders.Results[1].Threshold != nil {
		t.Fatalf("expected oversized manifest metadata to be ignored, got %+v", graders.Results[1])
	}
}

func TestGradersArtifactSetResolvesToUsageAgentAndFallback(t *testing.T) {
	t.Parallel()
	filter := artifactSetArtifacts[ArtifactSetGraders]
	if len(filter) != 3 {
		t.Fatalf("expected graders set to resolve to 3 artifacts, got %v", filter)
	}
	if filter[0] != constants.UsageArtifactName.String() ||
		filter[1] != constants.AgentArtifactName.String() ||
		filter[2] != constants.AgentOutputFallbackArtifactName.String() {
		t.Fatalf("unexpected graders artifact set: %v", filter)
	}
}
