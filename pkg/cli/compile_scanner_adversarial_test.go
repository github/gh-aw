//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/scanfindings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const scannerArtifactWorkflow = `name: Artifact
on: workflow_dispatch
permissions: read-all
jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - id: checkout
        uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683
      - id: artifact
        uses: actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02
        with:
          path: .
          include-hidden-files: true
`

func TestDryRunZizmorDifferentialBindsSourceAndSink(t *testing.T) {
	before := scannerArtifactWorkflow
	for _, test := range []struct {
		name, after string
		added       int
	}{
		{"unchanged", before, 0},
		{"comments and line shifts", "# header\n" + strings.Replace(before, "      - id: checkout", "      # checkout comment\n      - id: checkout", 1), 0},
		{"source ID changed", strings.Replace(before, "id: checkout", "id: privileged_checkout", 1), 1},
		{"credential changed with stable source ID", strings.Replace(before,
			"      - id: artifact", "        with:\n          repository: example/private-release\n          token: ${{ secrets.RELEASE_PAT }}\n      - id: artifact", 1), 1},
		{"sink input changed with stable sink ID", strings.Replace(before, "path: .", "path: /", 1), 1},
		{"unrelated step inserted", strings.Replace(before, "    steps:\n", "    steps:\n      - run: echo safe\n", 1), 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "artifact.yml")
			baseline := zizmorAdversarialSnapshot(t, path, before)
			current := zizmorAdversarialSnapshot(t, path, test.after)
			diff, err := (&dryRunScannerBaseline{results: map[string]error{"zizmor": baseline}}).filter("zizmor", current)
			require.Error(t, err, "existing high findings remain blocking; the baseline only classifies them")
			require.NotNil(t, diff)
			assert.Equal(t, test.added, diff.Added)
			assert.Equal(t, 1-test.added, diff.Existing)
		})
	}
}

func TestDryRunZizmorNativeCredentialReplacement(t *testing.T) {
	scanner, err := exec.LookPath("zizmor")
	if err != nil {
		t.Skip("native zizmor is not installed")
	}
	path := filepath.Join(t.TempDir(), "artifact.yml")
	snapshot := func(content string) error {
		require.NoError(t, os.WriteFile(path, []byte(content), 0600))
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		output, scanErr := exec.CommandContext(ctx, scanner, "--offline", "--persona", "auditor", "--format", "json", path).Output()
		if scanErr != nil {
			var exitErr *exec.ExitError
			require.ErrorAs(t, scanErr, &exitErr)
			require.GreaterOrEqual(t, exitErr.ExitCode(), 10)
			require.LessOrEqual(t, exitErr.ExitCode(), 14)
		}
		var native []json.RawMessage
		require.NoError(t, json.Unmarshal(output, &native))
		var selected []json.RawMessage
		for _, raw := range native {
			var finding zizmorFinding
			require.NoError(t, json.Unmarshal(raw, &finding))
			if finding.Ident == "artipacked" {
				selected = append(selected, raw)
			}
		}
		require.Len(t, selected, 1, "the native scanner must report the source/sink exploit")
		output, marshalErr := json.Marshal(selected)
		require.NoError(t, marshalErr)
		var findings []scanfindings.Finding
		var parseErr error
		captureStderrForGuardPolicyReportTest(func() {
			_, _, parseErr = parseAndDisplayZizmorOutputWithOptions(string(output), "", false, zizmorResultOptions{dryRun: true, findings: &findings})
		})
		require.NoError(t, parseErr)
		require.Len(t, findings, 1)
		return withScannerFindings(errors.New("native zizmor findings"), findings)
	}
	baseline := snapshot(scannerArtifactWorkflow)
	current := snapshot(strings.Replace(scannerArtifactWorkflow, "      - id: artifact",
		"        with:\n          repository: example/private-release\n          token: ${{ secrets.RELEASE_PAT }}\n      - id: artifact", 1))
	diff, err := (&dryRunScannerBaseline{results: map[string]error{"zizmor": baseline}}).filter("zizmor", current)
	require.Error(t, err)
	require.NotNil(t, diff)
	assert.Equal(t, 1, diff.Added)
	assert.Zero(t, diff.Existing)
}

func zizmorAdversarialSnapshot(t *testing.T, path, content string) error {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	var locations []any
	steps := 0
	for index, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "      - ") {
			continue
		}
		step := steps
		steps++
		if !strings.Contains(line, "id: ") {
			continue
		}
		kind := "Primary"
		if strings.Contains(line, "id: artifact") {
			kind = "Related"
		}
		locations = append(locations, map[string]any{
			"symbolic": map[string]any{
				"key":  map[string]any{"Local": map[string]any{"verbatim_path": path}},
				"kind": kind,
				"route": map[string]any{"route": []any{
					map[string]any{"Key": "jobs"}, map[string]any{"Key": "publish"},
					map[string]any{"Key": "steps"}, map[string]any{"Index": step},
				}},
			},
			"concrete": map[string]any{"location": map[string]any{
				"start_point": map[string]int{"row": index, "column": 8},
			}},
		})
	}
	output, err := json.Marshal([]any{map[string]any{
		"ident": "artipacked", "desc": "credential persistence through GitHub Actions artifacts",
		"determinations": map[string]string{"severity": "High"}, "locations": locations,
	}})
	require.NoError(t, err)
	var findings []scanfindings.Finding
	captureStderrForGuardPolicyReportTest(func() {
		_, _, err = parseAndDisplayZizmorOutputWithOptions(string(output), "", false, zizmorResultOptions{
			dryRun: true, findings: &findings,
		})
	})
	require.NoError(t, err)
	require.Len(t, findings, 1, "source and sink form one diagnostic, not independently matchable findings")
	return withScannerFindings(errors.New("zizmor findings"), findings)
}

func TestDryRunScannerDifferentialJoinedResults(t *testing.T) {
	t.Parallel()
	existing := scanfindings.Finding{RuleID: "existing", Severity: scanfindings.SeverityHigh, File: "image"}
	added := scanfindings.Finding{RuleID: "added", Severity: scanfindings.SeverityHigh, File: "image"}
	oldFinding := withScannerFindings(errors.New("findings"), []scanfindings.Finding{existing})
	newFinding := withScannerFindings(errors.New("findings"), []scanfindings.Finding{added})
	failure := errors.New("scanner failed after partial output")
	for _, test := range []struct {
		name    string
		current error
	}{
		{"operational error after findings", errors.Join(oldFinding, failure)},
		{"operational error before findings", errors.Join(failure, oldFinding)},
		{"nested operational error", fmt.Errorf("scanner: %w", errors.Join(oldFinding, failure))},
	} {
		t.Run(test.name, func(t *testing.T) {
			baseline := &dryRunScannerBaseline{results: map[string]error{"test": oldFinding}}
			_, err := baseline.filter("test", test.current)
			require.ErrorIs(t, err, failure, "partial findings must not hide a scanner failure")
			baseline.results["test"] = test.current
			_, err = baseline.filter("test", nil)
			require.ErrorIs(t, err, failure, "baseline failures must also remain fatal")
		})
	}
	diff, err := (&dryRunScannerBaseline{results: map[string]error{"test": oldFinding}}).filter(
		"test", errors.Join(oldFinding, newFinding),
	)
	require.Error(t, err)
	require.NotNil(t, diff)
	assert.Equal(t, 1, diff.Existing)
	assert.Equal(t, 1, diff.Added, "findings in subsequent joined errors must not disappear")
	_, err = (&dryRunScannerBaseline{results: map[string]error{}}).filter(
		"test", &scannerFindingsError{err: errors.New("unusable findings")},
	)
	require.Error(t, err, "an empty finding wrapper must not turn an error into success")
}

func TestScannerStepLabelsCannotHideChangedConfiguration(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"id: checkout", "name: Checkout", "uses: actions/checkout@v4"} {
		t.Run(field, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "checkout.yml")
			before := "jobs:\n  publish:\n    runs-on: ubuntu-latest\n    steps:\n      - id: checkout\n        name: Checkout\n        uses: actions/checkout@v4\n"
			offset := strings.Index(before, field)
			require.NotEqual(t, -1, offset)
			line := strings.Count(before[:offset], "\n") + 1
			finding := scanfindings.Finding{File: path, Line: line, Column: 9, RuleID: "credentials", Severity: scanfindings.SeverityHigh}
			require.NoError(t, os.WriteFile(path, []byte(before), 0600))
			identity := make(scannerSources).findingIdentity(finding)
			require.NoError(t, os.WriteFile(path, []byte(before+"        with:\n          token: ${{ secrets.RELEASE_PAT }}\n"), 0600))
			assert.NotEqual(t, identity, make(scannerSources).findingIdentity(finding))
		})
	}
}

func TestDryRunZizmorRejectsPartialLocationLoss(t *testing.T) {
	valid := `{"ident":"existing","determinations":{"severity":"Low"},"locations":[{"symbolic":{"key":{"Local":{"verbatim_path":"missing.yml"}}},"concrete":{"location":{"start_point":{"row":1}}}}]}`
	for _, invalid := range []string{
		`{"ident":"new","determinations":{"severity":"High"},"locations":[]}`,
		`{"ident":"new","locations":[{"symbolic":{},"concrete":{"location":{"start_point":{"row":1}}}}]}`,
		`{"ident":"new","locations":[{"symbolic":{"key":{"Local":{"verbatim_path":"missing.yml"}},"route":{"route":[{"Index":-1}]}}}]}`,
		`{"ident":"new","locations":[{"symbolic":{"key":{"Local":{"verbatim_path":"missing.yml"}},"route":{"route":[{"Key":"jobs","Index":0}]}}}]}`,
		`{"ident":"new","locations":[{"symbolic":{"key":{"Local":{"verbatim_path":"missing.yml"}},"route":{"route":[{}]}}}]}`,
		`{"ident":"new","locations":[{"symbolic":{"key":{"Local":{"verbatim_path":"missing.yml"}},"route":{}}}]}`,
	} {
		t.Run(invalid, func(t *testing.T) {
			var parseErr error
			captureStderrForGuardPolicyReportTest(func() {
				_, _, parseErr = parseAndDisplayZizmorOutputWithOptions("["+valid+","+invalid+"]", "", false, zizmorResultOptions{dryRun: true})
			})
			require.Error(t, parseErr, "usable baseline findings cannot make malformed new findings disappear")
		})
	}

}

func TestDryRunZizmorCrossFileLocations(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.yml")
	sink := filepath.Join(dir, "sink.yml")
	sourceContent := "jobs:\n  publish:\n    runs-on: ubuntu-latest\n    steps:\n      - id: checkout\n        uses: actions/checkout@v4\n"
	require.NoError(t, os.WriteFile(source, []byte(sourceContent), 0600))
	require.NoError(t, os.WriteFile(sink, []byte("jobs:\n  publish:\n    runs-on: ubuntu-latest\n    steps:\n      - id: artifact\n        uses: actions/upload-artifact@v4\n"), 0600))
	location := func(file, role string) string {
		return fmt.Sprintf(`{"symbolic":{"kind":%q,"key":{"Local":{"verbatim_path":%q}},"route":{"route":[{"Key":"jobs"},{"Key":"publish"},{"Key":"steps"},{"Index":0}]}},"concrete":{"location":{"start_point":{"row":4,"column":8}}}}`, role, file)
	}
	output := fmt.Sprintf(`[{"ident":"artipacked","determinations":{"severity":"High"},"locations":[%s,%s]}]`, location(source, "Primary"), location(sink, "Related"))
	snapshot := func() error {
		var findings []scanfindings.Finding
		var err error
		captureStderrForGuardPolicyReportTest(func() {
			_, _, err = parseAndDisplayZizmorOutputWithOptions(output, "", false, zizmorResultOptions{dryRun: true, findings: &findings})
		})
		require.NoError(t, err)
		require.Len(t, findings, 2)
		for _, finding := range findings {
			require.Len(t, finding.Locations, 2)
			assert.Equal(t, source, finding.Locations[0].File)
			assert.Equal(t, sink, finding.Locations[1].File)
		}
		return withScannerFindings(errors.New("cross-file findings"), findings)
	}
	baseline := snapshot()
	require.NoError(t, os.WriteFile(source, []byte(sourceContent+"        with:\n          token: ${{ secrets.RELEASE_PAT }}\n"), 0600))
	diff, err := (&dryRunScannerBaseline{results: map[string]error{"zizmor": baseline}}).filter("zizmor", snapshot())
	require.Error(t, err)
	assert.Equal(t, 2, diff.Added, "a changed source must invalidate both file projections, including the unchanged sink")
	assert.Zero(t, diff.Existing)
}
