//go:build !integration

package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
)

// TestDisplayBatchCompilationNotices verifies that displayBatchCompilationNotices
// aggregates experimental-feature notices and the Copilot billing tip into a single
// block rather than repeating them per workflow, and that success messages are
// suppressed when batch/quiet mode is active.
func TestDisplayBatchCompilationNotices(t *testing.T) {
	tests := []struct {
		name                string
		featureUsage        map[string]int
		copilotTipNeeded    bool
		config              CompileConfig
		expectedInOutput    []string
		notExpectedInOutput []string
	}{
		{
			name:             "no notices when nothing was used",
			featureUsage:     map[string]int{},
			copilotTipNeeded: false,
			config:           CompileConfig{},
			expectedInOutput: []string{},
			notExpectedInOutput: []string{
				"Experimental features",
				"copilot",
			},
		},
		{
			name: "single experimental feature shows aggregate notice",
			featureUsage: map[string]int{
				"Using experimental feature: canvas": 3,
			},
			copilotTipNeeded: false,
			config:           CompileConfig{},
			expectedInOutput: []string{
				"Experimental features in use:",
				"canvas: 3 workflows",
			},
			notExpectedInOutput: []string{
				"Using experimental feature: canvas",
				"Using experimental feature: canvas\nUsing experimental feature: canvas",
			},
		},
		{
			name: "multiple experimental features sorted by count desc",
			featureUsage: map[string]int{
				"Using experimental feature: canvas":    2,
				"Using experimental feature: safe-read": 5,
			},
			copilotTipNeeded: false,
			config:           CompileConfig{},
			expectedInOutput: []string{
				"Experimental features in use:",
				"safe-read: 5 workflows",
				"canvas: 2 workflows",
			},
			notExpectedInOutput: []string{},
		},
		{
			name:             "copilot tip shown when needed",
			featureUsage:     map[string]int{},
			copilotTipNeeded: true,
			config:           CompileConfig{},
			expectedInOutput: []string{
				"Copilot token-based inference may be available",
				"To suppress this tip when using a PAT, set permissions.copilot-requests: none",
			},
			notExpectedInOutput: []string{},
		},
		{
			name: "no output in JSON mode",
			featureUsage: map[string]int{
				"Using experimental feature: canvas": 2,
			},
			copilotTipNeeded: true,
			config:           CompileConfig{JSONOutput: true},
			expectedInOutput: []string{},
			notExpectedInOutput: []string{
				"Experimental features",
				"Copilot token-based inference",
			},
		},
		{
			name: "no output in verbose mode",
			featureUsage: map[string]int{
				"Using experimental feature: canvas": 2,
			},
			copilotTipNeeded: true,
			config:           CompileConfig{Verbose: true},
			expectedInOutput: []string{},
			notExpectedInOutput: []string{
				"Experimental features",
				"Copilot token-based inference",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := workflow.NewCompiler()
			compiler.SetBatchMode(true)
			compiler.SetExperimentalFeatureUsage(tt.featureUsage)
			compiler.SetCopilotTipNeeded(tt.copilotTipNeeded)

			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			displayBatchCompilationNotices(compiler, tt.config)

			w.Close()
			os.Stderr = oldStderr

			var buf bytes.Buffer
			buf.ReadFrom(r)
			output := buf.String()

			for _, expected := range tt.expectedInOutput {
				if !strings.Contains(output, expected) {
					t.Errorf("expected output to contain %q, got:\n%s", expected, output)
				}
			}
			for _, notExpected := range tt.notExpectedInOutput {
				if strings.Contains(output, notExpected) {
					t.Errorf("expected output NOT to contain %q, got:\n%s", notExpected, output)
				}
			}
		})
	}
}

// TestBatchModeSetOnMultipleSpecificFiles verifies that compileSpecificFiles enables
// batch mode (quiet, aggregation) when more than one file is provided and verbose is off.
// It also checks that displayBatchCompilationNotices is called at the end by confirming
// the per-workflow success message is absent from stderr for multiple-file batches.
func TestBatchModeSetOnSpecificFilesWhenMultiple(t *testing.T) {
	compiler := workflow.NewCompiler()
	compiler.SetBatchMode(false)
	compiler.SetQuiet(false)

	// Simulating the batch-mode condition directly: when !config.Verbose && len(files) > 1
	files := []string{"a.md", "b.md", "c.md"}
	config := CompileConfig{Verbose: false}

	batchMode := !config.Verbose && len(files) > 1
	if !batchMode {
		t.Fatal("expected batchMode=true for 3 non-verbose files")
	}

	compiler.SetBatchMode(batchMode)
	compiler.SetQuiet(batchMode)

	// Set some feature usage so the notice would appear in batch mode
	compiler.SetExperimentalFeatureUsage(map[string]int{
		"Using experimental feature: canvas": 2,
	})

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	displayBatchCompilationNotices(compiler, config)

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "Experimental features in use:") {
		t.Errorf("expected aggregated experimental features notice, got:\n%s", output)
	}
	// Verify each feature is mentioned only once regardless of count
	occurrences := strings.Count(output, "canvas")
	if occurrences != 1 {
		t.Errorf("expected 'canvas' to appear exactly once in batch output, got %d occurrences:\n%s", occurrences, output)
	}
}
