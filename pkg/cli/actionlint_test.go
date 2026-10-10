//go:build !integration

package cli

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAndDisplayActionlintOutput(t *testing.T) {
	tests := []struct {
		name           string
		stdout         string
		verbose        bool
		expectedOutput []string
		expectError    bool
		expectedCount  int
		expectedKinds  map[string]int
	}{
		{
			name: "single error",
			stdout: `[
{"message":"label \"ubuntu-slim\" is unknown. available labels are \"ubuntu-latest\", \"ubuntu-22.04\", \"ubuntu-20.04\", \"windows-latest\", \"windows-2022\", \"windows-2019\", \"macos-latest\", \"macos-13\", \"macos-12\", \"macos-11\". if it is a custom label for self-hosted runner, set list of labels in actionlint.yaml config file","filepath":".github/workflows/test.lock.yml","line":10,"column":14,"kind":"runner-label","snippet":"    runs-on: ubuntu-slim\n             ^~~~~~~~~~~","end_column":24}
]`,
			expectedOutput: []string{
				".github/workflows/test.lock.yml:10:14: error: [runner-label] label \"ubuntu-slim\" is unknown",
				"runs-on: ubuntu-slim",
				"^",
			},
			expectError:   false,
			expectedCount: 1,
			expectedKinds: map[string]int{"runner-label": 1},
		},
		{
			name: "multiple errors",
			stdout: `[
{"message":"label \"ubuntu-slim\" is unknown. available labels are \"ubuntu-latest\", \"ubuntu-22.04\", \"ubuntu-20.04\", \"windows-latest\", \"windows-2022\", \"windows-2019\", \"macos-latest\", \"macos-13\", \"macos-12\", \"macos-11\". if it is a custom label for self-hosted runner, set list of labels in actionlint.yaml config file","filepath":".github/workflows/test.lock.yml","line":10,"column":14,"kind":"runner-label","snippet":"    runs-on: ubuntu-slim\n             ^~~~~~~~~~~","end_column":24},
{"message":"shellcheck reported issue in this script: SC2086:info:1:8: Double quote to prevent globbing and word splitting","filepath":".github/workflows/test.lock.yml","line":25,"column":9,"kind":"shellcheck","snippet":"        run: |\n        ^~~~","end_column":12}
]`,
			expectedOutput: []string{
				".github/workflows/test.lock.yml:10:14: error: [runner-label] label \"ubuntu-slim\" is unknown",
				".github/workflows/test.lock.yml:25:9: error: [shellcheck] shellcheck reported issue",
			},
			expectError:   false,
			expectedCount: 2,
			expectedKinds: map[string]int{"runner-label": 1, "shellcheck": 1},
		},
		{
			name:           "no errors - empty output",
			stdout:         "",
			expectedOutput: []string{},
			expectError:    false,
			expectedCount:  0,
			expectedKinds:  map[string]int{},
		},
		{
			name:        "invalid JSON",
			stdout:      `{invalid json}`,
			expectError: true,
		},
		{
			name: "multiple errors from multiple files",
			stdout: `[
{"message":"label \"ubuntu-slim\" is unknown","filepath":".github/workflows/test1.lock.yml","line":10,"column":14,"kind":"runner-label","snippet":"    runs-on: ubuntu-slim\n             ^~~~~~~~~~~","end_column":24},
{"message":"shellcheck reported issue","filepath":".github/workflows/test2.lock.yml","line":25,"column":9,"kind":"shellcheck","snippet":"        run: |\n        ^~~~","end_column":12}
]`,
			expectedOutput: []string{
				".github/workflows/test1.lock.yml:10:14: error: [runner-label]",
				".github/workflows/test2.lock.yml:25:9: error: [shellcheck]",
			},
			expectError:   false,
			expectedCount: 2,
			expectedKinds: map[string]int{"runner-label": 1, "shellcheck": 1},
		},
		{
			name: "errors from three files",
			stdout: `[
{"message":"error 1","filepath":".github/workflows/a.lock.yml","line":10,"column":1,"kind":"error","snippet":"test","end_column":5},
{"message":"error 2","filepath":".github/workflows/b.lock.yml","line":20,"column":1,"kind":"error","snippet":"test","end_column":5},
{"message":"error 3","filepath":".github/workflows/c.lock.yml","line":30,"column":1,"kind":"error","snippet":"test","end_column":5}
]`,
			expectedOutput: []string{
				".github/workflows/a.lock.yml:10:1",
				".github/workflows/b.lock.yml:20:1",
				".github/workflows/c.lock.yml:30:1",
			},
			expectError:   false,
			expectedCount: 3,
			expectedKinds: map[string]int{"error": 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var count int
			var kinds map[string]int
			var err error

			output := testutil.CaptureStderr(t, func() {
				count, kinds, err = parseAndDisplayActionlintOutput(tt.stdout, tt.verbose)
			})

			if tt.expectError {
				require.Error(t, err, "should return error for invalid input")
			} else {
				require.NoError(t, err, "should not return error for valid input")
				assert.Equal(t, tt.expectedCount, count, "error count should match expected")
				if tt.expectedKinds != nil {
					assert.Equal(t, tt.expectedKinds, kinds, "error kinds should match expected")
				}
				for _, expected := range tt.expectedOutput {
					assert.Contains(t, output, expected,
						"output should contain %q", expected)
				}
			}
		})
	}
}

func TestGetActionlintVersion(t *testing.T) {
	original := actionlintVersion
	defer func() { actionlintVersion = original }()

	actionlintVersion = "1.7.9"
	version, err := getActionlintVersion(context.Background())
	require.NoError(t, err, "should not error when version is cached")
	assert.Equal(t, "1.7.9", version, "should return cached version")
}

func TestDisplayActionlintSummary(t *testing.T) {
	tests := []struct {
		name                string
		stats               *ActionlintStats
		expectedContains    []string
		notExpectedContains []string
	}{
		{
			name: "summary with errors and warnings",
			stats: &ActionlintStats{
				TotalWorkflows: 5,
				TotalErrors:    10,
				TotalWarnings:  3,
				ErrorsByKind: map[string]int{
					"runner-label": 5,
					"shellcheck":   5,
				},
			},
			expectedContains: []string{
				"Actionlint Summary",
				"Checked 5 workflow(s)",
				"Found 13 issue(s)",
				"10 error(s), 3 warning(s)",
				"Issues by type:",
				"runner-label: 5",
				"shellcheck: 5",
			},
		},
		{
			name: "summary with only errors",
			stats: &ActionlintStats{
				TotalWorkflows: 3,
				TotalErrors:    7,
				TotalWarnings:  0,
				ErrorsByKind: map[string]int{
					"syntax": 7,
				},
			},
			expectedContains: []string{
				"Actionlint Summary",
				"Checked 3 workflow(s)",
				"Found 7 issue(s)",
				"7 error(s)",
				"Issues by type:",
				"syntax: 7",
			},
		},
		{
			name: "summary with no issues",
			stats: &ActionlintStats{
				TotalWorkflows: 10,
				TotalErrors:    0,
				TotalWarnings:  0,
				ErrorsByKind:   map[string]int{},
			},
			expectedContains: []string{
				"Actionlint Summary",
				"Checked 10 workflow(s)",
				"No issues found",
			},
		},
		{
			name:             "nil stats - no output",
			stats:            nil,
			expectedContains: []string{},
		},
		// Regression tests: integration failures must never produce "No issues found"
		{
			name: "integration errors only - no lint issues",
			stats: &ActionlintStats{
				TotalWorkflows:    3,
				TotalErrors:       0,
				TotalWarnings:     0,
				IntegrationErrors: 2,
				ErrorsByKind:      map[string]int{},
			},
			expectedContains: []string{
				"Actionlint Summary",
				"Checked 3 workflow(s)",
				"2 actionlint invocation(s) failed",
				"tooling or integration error",
			},
			notExpectedContains: []string{
				"No issues found",
			},
		},
		{
			name: "integration errors alongside lint issues",
			stats: &ActionlintStats{
				TotalWorkflows:    4,
				TotalErrors:       5,
				TotalWarnings:     0,
				IntegrationErrors: 1,
				ErrorsByKind:      map[string]int{"syntax": 5},
			},
			expectedContains: []string{
				"Actionlint Summary",
				"Checked 4 workflow(s)",
				"Found 5 issue(s)",
				"1 actionlint invocation(s) also failed with tooling errors",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalStats := actionlintStats
			defer func() { actionlintStats = originalStats }()
			actionlintStats = tt.stats

			output := testutil.CaptureStderr(t, displayActionlintSummary)

			for _, expected := range tt.expectedContains {
				assert.Contains(t, output, expected,
					"output should contain %q", expected)
			}
			for _, notExpected := range tt.notExpectedContains {
				assert.NotContains(t, output, notExpected,
					"output must not contain %q", notExpected)
			}
		})
	}
}

func TestInitActionlintStats(t *testing.T) {
	originalStats := actionlintStats
	defer func() { actionlintStats = originalStats }()

	initActionlintStats()

	require.NotNil(t, actionlintStats, "actionlintStats should not be nil after initialization")
	assert.Zero(t, actionlintStats.TotalWorkflows, "TotalWorkflows should start at 0")
	assert.Zero(t, actionlintStats.TotalErrors, "TotalErrors should start at 0")
	assert.Zero(t, actionlintStats.TotalWarnings, "TotalWarnings should start at 0")
	assert.Zero(t, actionlintStats.IntegrationErrors, "IntegrationErrors should start at 0")
	assert.NotNil(t, actionlintStats.ErrorsByKind, "ErrorsByKind map should be initialized")
	assert.Empty(t, actionlintStats.ErrorsByKind, "ErrorsByKind should start empty")
}

func TestGetActionlintDocsURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		kind     string
		expected string
	}{
		{
			name:     "empty kind returns base URL",
			kind:     "",
			expected: "https://github.com/rhysd/actionlint/blob/main/docs/checks.md",
		},
		{
			name:     "runner-label kind",
			kind:     "runner-label",
			expected: "https://github.com/rhysd/actionlint/blob/main/docs/checks.md#check-runner-labels",
		},
		{
			name:     "shellcheck kind",
			kind:     "shellcheck",
			expected: "https://github.com/rhysd/actionlint/blob/main/docs/checks.md#check-shellcheck-integ",
		},
		{
			name:     "pyflakes kind",
			kind:     "pyflakes",
			expected: "https://github.com/rhysd/actionlint/blob/main/docs/checks.md#check-pyflakes-integ",
		},
		{
			name:     "expression kind",
			kind:     "expression",
			expected: "https://github.com/rhysd/actionlint/blob/main/docs/checks.md#check-syntax-expression",
		},
		{
			name:     "syntax-check kind",
			kind:     "syntax-check",
			expected: "https://github.com/rhysd/actionlint/blob/main/docs/checks.md#check-unexpected-keys",
		},
		{
			name:     "generic kind with check- prefix",
			kind:     "check-job-deps",
			expected: "https://github.com/rhysd/actionlint/blob/main/docs/checks.md#check-job-deps",
		},
		{
			name:     "generic kind without check- prefix",
			kind:     "job-deps",
			expected: "https://github.com/rhysd/actionlint/blob/main/docs/checks.md#check-job-deps",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getActionlintDocsURL(tt.kind)
			assert.Equal(t, tt.expected, result, "docs URL should match expected for kind %q", tt.kind)
		})
	}
}

func TestBuildActionlintIntegrationStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		includeShellcheck bool
		includePyflakes   bool
		expected          string
	}{
		{
			name:              "both integrations enabled",
			includeShellcheck: true,
			includePyflakes:   true,
			expected:          "with shellcheck/pyflakes",
		},
		{
			name:              "only shellcheck enabled",
			includeShellcheck: true,
			includePyflakes:   false,
			expected:          "with shellcheck only",
		},
		{
			name:              "only pyflakes enabled",
			includeShellcheck: false,
			includePyflakes:   true,
			expected:          "with pyflakes only",
		},
		{
			name:              "both integrations disabled",
			includeShellcheck: false,
			includePyflakes:   false,
			expected:          "without shellcheck/pyflakes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildActionlintIntegrationStatus(tt.includeShellcheck, tt.includePyflakes)
			assert.Equal(t, tt.expected, result, "integration status should match")
		})
	}
}

func TestBuildActionlintDockerArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		opts     actionlintRunOptions
		expected []string
	}{
		{
			name: "shellcheck disabled",
			opts: actionlintRunOptions{
				IncludeShellcheck: false,
				IncludePyflakes:   true,
				IgnorePatterns:    []string{"foo", "bar"},
			},
			expected: []string{
				"run",
				"--rm",
				"-v", "/repo:/workdir",
				"-w", "/workdir",
				ActionlintImage,
				"-format", "{{json .}}",
				"-shellcheck=",
				"-ignore", "foo",
				"-ignore", "bar",
				"a.lock.yml",
				"b.lock.yml",
			},
		},
		{
			name: "pyflakes disabled",
			opts: actionlintRunOptions{
				IncludeShellcheck: true,
				IncludePyflakes:   false,
			},
			expected: []string{
				"run",
				"--rm",
				"-v", "/repo:/workdir",
				"-w", "/workdir",
				ActionlintImage,
				"-format", "{{json .}}",
				"-pyflakes=",
				"a.lock.yml",
				"b.lock.yml",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := buildActionlintDockerArgs("/repo", []string{"a.lock.yml", "b.lock.yml"}, tt.opts)
			assert.Equal(t, tt.expected, args)
		})
	}
}

func TestBuildActionlintCompilerError(t *testing.T) {
	t.Parallel()
	compilerErr := buildActionlintCompilerError(actionlintError{
		Message:  "something went wrong",
		Filepath: ".github/workflows/test.lock.yml",
		Line:     12,
		Column:   4,
		Kind:     "warning-test",
		Snippet:  "    run: echo test\n    ^~~~",
	})

	assert.Equal(t, ".github/workflows/test.lock.yml", compilerErr.Position.File)
	assert.Equal(t, 12, compilerErr.Position.Line)
	assert.Equal(t, 4, compilerErr.Position.Column)
	assert.Equal(t, "warning", compilerErr.Type)
	assert.Equal(t, []string{"    run: echo test"}, compilerErr.Context)
	assert.Contains(t, compilerErr.Message, "[warning-test] something went wrong")
	assert.Contains(t, compilerErr.Message, getActionlintDocsURL("warning-test"))
}

func TestFormatActionlintCommand(t *testing.T) {
	t.Parallel()
	args := buildActionlintDockerArgs("/tmp/repo root", []string{"a.lock.yml"}, actionlintRunOptions{
		IncludeShellcheck: false,
		IgnorePatterns:    []string{"foo bar"},
	})
	command := formatActionlintCommand(append([]string{"docker"}, args...))

	assert.Contains(t, command, `"/tmp/repo root:/workdir"`)
	assert.Contains(t, command, `-shellcheck=`)
	assert.Contains(t, command, `-ignore "foo bar"`)
	assert.Contains(t, command, `-format "{{json .}}"`)
}

func TestActionlintShouldParseOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: true,
		},
		{
			name: "lint findings exit code",
			err:  exitErrorFromCommand(t, 1),
			want: true,
		},
		{
			name: "tooling failure exit code",
			err:  exitErrorFromCommand(t, 2),
			want: false,
		},
		{
			name: "invocation failure",
			err:  assert.AnError,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, actionlintShouldParseOutput(tt.err))
		})
	}
}

func exitErrorFromCommand(t *testing.T, exitCode int) error {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=TestActionlintExitHelperSubprocess")
	cmd.Env = append(os.Environ(),
		"GH_AW_ACTIONLINT_EXIT_HELPER_PROCESS=1",
		"GH_AW_ACTIONLINT_EXIT_CODE="+strconv.Itoa(exitCode),
	)
	err := cmd.Run()
	require.Error(t, err)
	return err
}

func TestActionlintExitHelperSubprocess(t *testing.T) {
	if os.Getenv("GH_AW_ACTIONLINT_EXIT_HELPER_PROCESS") != "1" {
		return
	}

	switch os.Getenv("GH_AW_ACTIONLINT_EXIT_CODE") {
	case "1":
		os.Exit(1)
	case "2":
		os.Exit(2)
	default:
		os.Exit(0)
	}
}

func TestIsHighSeverityActionlintError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      actionlintError
		expected bool
	}{
		{
			name:     "non-shellcheck error is always high severity",
			err:      actionlintError{Kind: "syntax", Message: "unexpected token"},
			expected: true,
		},
		{
			name:     "shellcheck error severity",
			err:      actionlintError{Kind: "shellcheck", Message: "shellcheck reported issue in this script: SC2086:error:4:13: Double quote to prevent globbing"},
			expected: true,
		},
		{
			name:     "shellcheck warning severity",
			err:      actionlintError{Kind: "shellcheck", Message: "shellcheck reported issue in this script: SC2086:warning:4:13: Double quote to prevent globbing"},
			expected: true,
		},
		{
			name:     "shellcheck info severity",
			err:      actionlintError{Kind: "shellcheck", Message: "shellcheck reported issue in this script: SC2016:info:4:13: Expressions don't expand in single quotes"},
			expected: false,
		},
		{
			name:     "shellcheck style severity",
			err:      actionlintError{Kind: "shellcheck", Message: "shellcheck reported issue in this script: SC2034:style:1:1: Variable appears unused"},
			expected: false,
		},
		{
			name:     "shellcheck with unparseable message",
			err:      actionlintError{Kind: "shellcheck", Message: "some unexpected format"},
			expected: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isHighSeverityActionlintError(tt.err)
			if got != tt.expected {
				t.Errorf("isHighSeverityActionlintError() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExtractShellcheckSeverity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		message  string
		expected string
	}{
		{"shellcheck reported issue in this script: SC2016:info:4:13: msg", "info"},
		{"shellcheck reported issue in this script: SC2086:error:1:5: msg", "error"},
		{"shellcheck reported issue in this script: SC2034:style:1:1: msg", "style"},
		{"shellcheck reported issue in this script: SC2155:warning:3:1: msg", "warning"},
		{"no SC code here", ""},
	}
	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			got := extractShellcheckSeverity(tt.message)
			if got != tt.expected {
				t.Errorf("extractShellcheckSeverity() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestCountHighSeverityErrors(t *testing.T) {
	t.Parallel()
	// Mix of high and low severity
	input := `[
		{"message":"shellcheck reported issue in this script: SC2016:info:4:13: msg","filepath":"a.yml","line":1,"column":1,"kind":"shellcheck"},
		{"message":"shellcheck reported issue in this script: SC2086:error:1:5: msg","filepath":"b.yml","line":2,"column":1,"kind":"shellcheck"},
		{"message":"unexpected token","filepath":"c.yml","line":3,"column":1,"kind":"syntax"}
	]`
	got := countHighSeverityErrors(input)
	if got != 2 {
		t.Errorf("countHighSeverityErrors() = %d, want 2", got)
	}

	// All low severity
	input2 := `[
		{"message":"shellcheck reported issue in this script: SC2016:info:4:13: msg","filepath":"a.yml","line":1,"column":1,"kind":"shellcheck"},
		{"message":"shellcheck reported issue in this script: SC2034:style:1:1: msg","filepath":"b.yml","line":2,"column":1,"kind":"shellcheck"}
	]`
	got2 := countHighSeverityErrors(input2)
	if got2 != 0 {
		t.Errorf("countHighSeverityErrors() = %d, want 0", got2)
	}
}
