// Package coverage provides helpers for coverage-aware perf linter gating.
//
// When the GH_AW_LINT_COVERAGE_PROFILE environment variable points to a Go
// coverage profile (produced by "go test -covermode=count -coverprofile=<path>"),
// [ShouldApply] returns false for code positions whose recorded hit count is
// below the configured threshold, suppressing findings on cold paths.
//
// When the variable is unset (the default), every call to [ShouldApply]
// returns true so all gated linters behave exactly as before.
package coverage

import (
	"go/token"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"sync"

	xcov "golang.org/x/tools/cover"
	"golang.org/x/tools/go/analysis"
)

const envVar = "GH_AW_LINT_COVERAGE_PROFILE"

// profileIndex holds the lazily loaded coverage profiles indexed by filename.
type profileIndex struct {
	profiles map[string]*xcov.Profile // key: Profile.FileName (package-qualified path)
}

// modulePrefix returns the module path of the running binary's main module
// (e.g. "github.com/github/gh-aw"), or "" if it cannot be determined. This is
// used to strip the module-qualified prefix from coverage profile keys so
// they can be matched against real OS absolute paths, which have no notion
// of the module's import path (e.g. GitHub Actions checkouts under
// "/home/runner/work/<repo>/<repo>/...").
func modulePrefix() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok || bi.Main.Path == "" {
		return ""
	}
	return bi.Main.Path
}

var (
	once  sync.Once
	index *profileIndex // nil means no profile loaded (permissive fallback)
)

// load loads the profile at most once per process from GH_AW_LINT_COVERAGE_PROFILE.
// On any error (env unset, bad path, parse failure) it leaves index nil so
// callers fall back to permissive behaviour.
func load() {
	once.Do(func() {
		path := os.Getenv(envVar) //nolint:osgetenvlibrary
		if path == "" {
			return
		}
		profiles, err := xcov.ParseProfiles(path)
		if err != nil {
			return
		}
		m := make(map[string]*xcov.Profile, len(profiles))
		for _, p := range profiles {
			m[p.FileName] = p
		}
		index = &profileIndex{profiles: m}
	})
}

// findProfile looks up the profile entry that corresponds to the given on-disk
// filename. Coverage profile keys are module-qualified paths such as
// "github.com/github/gh-aw/pkg/foo/foo.go", while pass.Fset returns real OS
// absolute paths (e.g. "/home/runner/work/gh-aw/gh-aw/pkg/foo/foo.go" on a
// standard GitHub Actions checkout). Since the module's import path has no
// relation to the on-disk checkout directory name, we strip the running
// binary's main module path from each profile key (when present) before
// matching, so comparisons use the module-relative path shared by both
// forms. We still fall back to matching the raw key as-is, to support
// profiles built from a different module than the running binary.
func (idx *profileIndex) findProfile(filename string) *xcov.Profile {
	norm := filepath.ToSlash(filename)
	modPrefix := modulePrefix()
	keys := make([]string, 0, len(idx.profiles))
	for key := range idx.profiles {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	var best *xcov.Profile
	bestExact := false
	bestLength := -1
	bestKey := ""
	consider := func(key, candidate string, p *xcov.Profile) {
		exact := norm == candidate
		if !exact && !strings.HasSuffix(norm, "/"+candidate) {
			return
		}
		if exact && !bestExact ||
			exact == bestExact && (len(candidate) > bestLength ||
				len(candidate) == bestLength && (bestKey == "" || key < bestKey)) {
			best = p
			bestExact = exact
			bestLength = len(candidate)
			bestKey = key
		}
	}

	for _, key := range keys {
		p := idx.profiles[key]
		normKey := filepath.ToSlash(key)
		consider(key, normKey, p)
		if modPrefix != "" {
			if rel, ok := strings.CutPrefix(normKey, modPrefix+"/"); ok {
				consider(key, rel, p)
			}
		}
	}
	return best
}

// hitCount returns the execution count for the given 1-based line and column.
// When column data is unavailable, the last block spanning the line wins.
func hitCount(p *xcov.Profile, line, column int) int {
	count, lineCount := 0, 0
	hasLineBlock, hasColumnInfo, hasColumnMatch := false, false, false
	for _, b := range p.Blocks {
		if b.StartLine > line || line > b.EndLine {
			continue
		}
		hasLineBlock = true
		lineCount = b.Count
		if b.StartCol <= 0 || b.EndCol <= 0 {
			continue
		}
		hasColumnInfo = true
		if column > 0 && (line != b.StartLine || column >= b.StartCol) &&
			(line != b.EndLine || column < b.EndCol) {
			count = b.Count
			hasColumnMatch = true
		}
	}
	if hasColumnMatch {
		return count
	}
	if hasLineBlock && !hasColumnInfo {
		return lineCount
	}
	return count
}

// ShouldApply reports whether a linter finding at pos should be reported.
//
//   - When threshold is 0, coverage gating is disabled and the function
//     always returns true.
//   - When no coverage profile is loaded (GH_AW_LINT_COVERAGE_PROFILE is
//     unset or the file cannot be parsed), the function always returns true
//     (permissive fallback).
//   - Otherwise it returns true only when the recorded hit count for the
//     position's line is >= threshold.
func ShouldApply(pass *analysis.Pass, pos token.Pos, threshold int) bool {
	if threshold == 0 {
		return true
	}
	load()
	if index == nil {
		return true
	}
	position := pass.Fset.Position(pos)
	if !position.IsValid() {
		return true
	}
	p := index.findProfile(position.Filename)
	if p == nil {
		return true
	}
	return hitCount(p, position.Line, position.Column) >= threshold
}

// RegisterHotThresholdFlag registers a -hot-threshold flag on the given
// analyzer and returns a pointer to the flag value. The default value is 1
// (gate on any recorded execution). Pass 0 to disable gating entirely.
//
// This function must be called from an init() function, not from the analyzer
// Run function, to avoid an analyzer initialisation cycle.
func RegisterHotThresholdFlag(a *analysis.Analyzer) *int {
	v := new(int)
	*v = 1
	a.Flags.IntVar(v, "hot-threshold", 1, "minimum coverage hit count to report a finding (0 = always report)")
	return v
}
