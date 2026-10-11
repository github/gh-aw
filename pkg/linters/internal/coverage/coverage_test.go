package coverage

import (
	"go/token"
	"os"
	"sync"
	"testing"

	xcov "golang.org/x/tools/cover"
	"golang.org/x/tools/go/analysis"
)

// resetForTest resets singleton state between tests.
func resetForTest() {
	once = sync.Once{}
	index = nil
}

// fsetHandle wraps a token.FileSet and one registered file for convenience.
type fsetHandle struct {
	fset *token.FileSet
	file *token.File
}

// newFset creates a FileSet with a single file of the given name and size.
func newFset(t *testing.T, filename string, size int) *fsetHandle {
	t.Helper()
	fset := token.NewFileSet()
	f := fset.AddFile(filename, -1, size)
	// Register enough lines so lineStart() doesn't panic.
	lines := make([]byte, size)
	for i := range lines {
		if i > 0 && i%20 == 0 {
			lines[i] = '\n'
		}
	}
	f.SetLinesForContent(lines)
	return &fsetHandle{fset: fset, file: f}
}

// lineStart returns the token.Pos for the start of the given 1-based line.
func (h *fsetHandle) lineStart(line int) token.Pos {
	return h.file.LineStart(line)
}

func (h *fsetHandle) lineColumn(line, column int) token.Pos {
	return h.file.LineStart(line) + token.Pos(column-1)
}

// writeTempProfile writes a coverage profile to a temp file and returns the path.
func writeTempProfile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "coverage*.out")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

// profileWithCount returns a minimal coverage profile where the given file's
// line 2 has the specified hit count.
func profileWithCount(profileFilename string, count int) string {
	return "mode: count\n" +
		profileFilename + ":2.1,2.80 1 " + itoa(count) + "\n"
}

// itoa converts a non-negative int to decimal string.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// TestNoProfile verifies that ShouldApply is permissive when no env var is set.
func TestNoProfile(t *testing.T) {
	resetForTest()
	t.Setenv(envVar, "")

	fh := newFset(t, "somefile.go", 100)
	pass := &analysis.Pass{Fset: fh.fset}

	if !ShouldApply(pass, fh.lineStart(1), 1) {
		t.Fatal("expected true (permissive fallback) when no profile is set")
	}
}

// TestThresholdZeroAlwaysApplies verifies that threshold=0 disables gating.
func TestThresholdZeroAlwaysApplies(t *testing.T) {
	resetForTest()
	t.Setenv(envVar, writeTempProfile(t, profileWithCount("github.com/org/repo/pkg/foo/foo.go", 0)))

	fh := newFset(t, "/abs/path/github.com/org/repo/pkg/foo/foo.go", 400)
	pass := &analysis.Pass{Fset: fh.fset}

	if !ShouldApply(pass, fh.lineStart(2), 0) {
		t.Fatal("expected true when threshold=0 (gating disabled)")
	}
}

// TestBelowThreshold verifies that a line with count < threshold is suppressed.
func TestBelowThreshold(t *testing.T) {
	resetForTest()
	t.Setenv(envVar, writeTempProfile(t, profileWithCount("github.com/org/repo/pkg/foo/foo.go", 0)))

	fh := newFset(t, "/abs/path/github.com/org/repo/pkg/foo/foo.go", 400)
	pass := &analysis.Pass{Fset: fh.fset}

	if ShouldApply(pass, fh.lineStart(2), 1) {
		t.Fatal("expected false (suppress) when hit count < threshold")
	}
}

// TestAtThreshold verifies that a line exactly at the threshold is reported.
func TestAtThreshold(t *testing.T) {
	resetForTest()
	t.Setenv(envVar, writeTempProfile(t, profileWithCount("github.com/org/repo/pkg/foo/foo.go", 3)))

	fh := newFset(t, "/abs/path/github.com/org/repo/pkg/foo/foo.go", 400)
	pass := &analysis.Pass{Fset: fh.fset}

	if !ShouldApply(pass, fh.lineStart(2), 3) {
		t.Fatal("expected true (report) when hit count == threshold")
	}
}

// TestAboveThreshold verifies that a line above the threshold is reported.
func TestAboveThreshold(t *testing.T) {
	resetForTest()
	t.Setenv(envVar, writeTempProfile(t, profileWithCount("github.com/org/repo/pkg/foo/foo.go", 10)))

	fh := newFset(t, "/abs/path/github.com/org/repo/pkg/foo/foo.go", 400)
	pass := &analysis.Pass{Fset: fh.fset}

	if !ShouldApply(pass, fh.lineStart(2), 3) {
		t.Fatal("expected true (report) when hit count > threshold")
	}
}

// TestProfileLoadError verifies permissive fallback when the profile path is invalid.
func TestProfileLoadError(t *testing.T) {
	resetForTest()
	t.Setenv(envVar, "/nonexistent/path/to/coverage.out")

	fh := newFset(t, "foo.go", 100)
	pass := &analysis.Pass{Fset: fh.fset}

	if !ShouldApply(pass, fh.lineStart(1), 1) {
		t.Fatal("expected true (permissive fallback) on profile load error")
	}
}

// TestFilenameMatchingSuffix verifies that absolute on-disk paths ending with
// the profile key are matched correctly.
func TestFilenameMatchingSuffix(t *testing.T) {
	resetForTest()
	profileKey := "github.com/org/repo/pkg/foo/foo.go"
	t.Setenv(envVar, writeTempProfile(t, profileWithCount(profileKey, 5)))

	absFile := "/some/absolute/path/" + profileKey
	fh := newFset(t, absFile, 400)
	pass := &analysis.Pass{Fset: fh.fset}

	if !ShouldApply(pass, fh.lineStart(2), 1) {
		t.Fatal("expected true: filename suffix should match profile key")
	}
}

// TestMultiBlockProfileLookup verifies that when multiple blocks cover the
// same line, the last block's count is used.
func TestMultiBlockProfileLookup(t *testing.T) {
	resetForTest()
	profileKey := "github.com/org/repo/pkg/foo/foo.go"
	content := "mode: count\n" +
		profileKey + ":2.1,2.10 1 0\n" +
		profileKey + ":2.11,2.20 1 5\n"
	t.Setenv(envVar, writeTempProfile(t, content))

	absFile := "/some/absolute/path/" + profileKey
	fh := newFset(t, absFile, 400)
	pass := &analysis.Pass{Fset: fh.fset}

	if ShouldApply(pass, fh.lineColumn(2, 5), 1) {
		t.Fatal("expected false: block containing column 5 has count=0")
	}
	if !ShouldApply(pass, fh.lineColumn(2, 15), 3) {
		t.Fatal("expected true: block containing column 15 has count=5, threshold=3")
	}
}

func TestFilenameMatchingPrefersMostSpecificSuffix(t *testing.T) {
	profiles := map[string]*xcov.Profile{
		"a/foo.go": {
			FileName: "a/foo.go",
			Blocks:   []xcov.ProfileBlock{{StartLine: 2, EndLine: 2, Count: 1}},
		},
		"pkg/a/foo.go": {
			FileName: "pkg/a/foo.go",
			Blocks:   []xcov.ProfileBlock{{StartLine: 2, EndLine: 2, Count: 5}},
		},
	}
	idx := &profileIndex{profiles: profiles}

	for range 100 {
		got := idx.findProfile("/checkout/pkg/a/foo.go")
		if got != profiles["pkg/a/foo.go"] {
			var gotName string
			if got != nil {
				gotName = got.FileName
			}
			t.Fatalf("findProfile() = %q, want most-specific profile %q", gotName, "pkg/a/foo.go")
		}
	}
}

// TestFilenameMatchingActionsStyleAbsolutePath verifies the regression fix:
// a coverage profile key module-qualified as "<module>/pkg/foo/foo.go" (as
// produced by "go test -coverprofile") must match a real OS absolute path
// under a GitHub Actions style checkout directory (e.g.
// "/home/runner/work/gh-aw/gh-aw/pkg/foo/foo.go"), even though the checkout
// directory name bears no textual relation to the module's import path.
func TestFilenameMatchingActionsStyleAbsolutePath(t *testing.T) {
	resetForTest()

	mod := modulePrefix()
	if mod == "" {
		t.Skip("could not determine module path via debug.ReadBuildInfo")
	}

	profileKey := mod + "/pkg/foo/foo.go"
	t.Setenv(envVar, writeTempProfile(t, profileWithCount(profileKey, 5)))

	// Actions-style checkout: /home/runner/work/<repo>/<repo>/..., which
	// shares no suffix with the module-qualified profile key other than the
	// module-relative portion ("pkg/foo/foo.go").
	absFile := "/home/runner/work/gh-aw/gh-aw/pkg/foo/foo.go"
	fh := newFset(t, absFile, 400)
	pass := &analysis.Pass{Fset: fh.fset}

	if !ShouldApply(pass, fh.lineStart(2), 1) {
		t.Fatal("expected true: Actions-style absolute path should match module-qualified profile key")
	}
}

// TestUnmatchedFilenameIsPermissive verifies that a file not in the profile
// does not suppress findings.
func TestUnmatchedFilenameIsPermissive(t *testing.T) {
	resetForTest()
	profileKey := "github.com/org/repo/pkg/foo/foo.go"
	t.Setenv(envVar, writeTempProfile(t, profileWithCount(profileKey, 0)))

	fh := newFset(t, "/some/other/path/bar.go", 400)
	pass := &analysis.Pass{Fset: fh.fset}

	if !ShouldApply(pass, fh.lineStart(2), 1) {
		t.Fatal("expected true (permissive) for file not in profile")
	}
}

// TestRegisterHotThresholdFlag verifies flag registration and default value.
func TestRegisterHotThresholdFlag(t *testing.T) {
	a := &analysis.Analyzer{Name: "testanalyzer"}
	p := RegisterHotThresholdFlag(a)
	if p == nil {
		t.Fatal("expected non-nil pointer from RegisterHotThresholdFlag")
	}
	if *p != 1 {
		t.Fatalf("expected default hot-threshold=1, got %d", *p)
	}
	f := a.Flags.Lookup("hot-threshold")
	if f == nil {
		t.Fatal("expected -hot-threshold flag to be registered on the analyzer")
	}
}
