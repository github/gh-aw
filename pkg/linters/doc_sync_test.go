//go:build !integration

package linters_test

import (
	"bufio"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/linters"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	docBulletRe   = regexp.MustCompile(`^//\s+-\s+([a-z0-9_-]+)\s+—`)
	readmeTableRe = regexp.MustCompile(`^\|\s+` + "`" + `([a-z0-9_-]+)` + "`" + `\s+\|`)
	linterFlagsRe = regexp.MustCompile(`LINTER_FLAGS=["']([^"']+)["']`)
)

var notYetEnforced = map[string]string{
	"blankassigncomma":            "existing production violations need remediation before enforcement; nolint suppression already works",
	"bufferresetbeforereuse":      "new correctness analyzer needs an enforcement-readiness audit before native CI enables it",
	"bufioscannererunchecked":     "existing scanner loops need Err handling before native CI enables it",
	"closeerrorunchecked":         "existing production violations need remediation before enforcement; nolint suppression already works",
	"deferrerrorunchecked":        "new deferred-error analyzer needs an enforcement-readiness audit before native and WebAssembly CI enable it",
	"errorfwrapv":                 "requires an enforcement audit after the recent false-positive fix (#51928)",
	"errormessage":                "dedicated lint-error-messages CI job is intentionally advisory (continue-on-error per #54800)",
	"excessivefuncparams":         "existing production violations need remediation before enforcement; nolint suppression already works",
	"formatintitoa":               "existing FormatInt conversions need remediation before CI enforcement",
	"fprintferrorunchecked":       "existing production violations need remediation before enforcement; nolint suppression already works",
	"goroutineresourceleak":       "requires an enforcement-readiness audit before native and WebAssembly CI enable it",
	"hardcodedfilepath":           "requires an enforcement audit after the same-package constant fix (#52428)",
	"largefunc":                   "existing production violations need remediation before enforcement; nolint suppression already works",
	"lenstringzero":               "requires an enforcement audit after the diagnostic-message fix (#54717)",
	"manualpathconcat":            "existing production violations need remediation",
	"osisnotexist":                "existing os error predicates need case-by-case review before CI enforcement",
	"packagelevelmutableslicemap": "existing production violations need remediation",
	"reflectdeepequalusage":       "existing reflect.DeepEqual uses need case-by-case review before CI enforcement",
	"seenmapbool":                 "existing production violations need remediation",
	"sprintferrdot":               "has not yet completed an enforcement-readiness audit",
	"ssljson":                     "has not yet completed an enforcement-readiness audit",
	"stringsconcatloop":           "has not yet completed an enforcement-readiness audit",
	"typeassertionnil":            "needs a cross-platform enforcement-readiness audit before CI enables it",
	"typeassertionokdiscarded":    "existing production violations need remediation before enforcement; nolint suppression already works",
	"unchecked_deferredclose":     "existing deferred Close() findings need remediation before CI enforcement",
	"uncheckedsliceindex":         "existing production violations need remediation before enforcement; nolint suppression already works",
	"walkdirignoredeerror":        "existing discarded WalkDir errors in audit_threat_detection.go and model_routing.go need remediation before CI enforcement",
}

// TestDocGo_CountMatchesBullets validates that the "All N active analyzers:"
// header count in doc.go matches the actual number of bullet entries.
// This prevents the header from silently drifting from the bullet list
// (as seen in gh-aw#40436, gh-aw#45185, gh-aw#46131).
func TestDocGo_CountMatchesBullets(t *testing.T) {
	t.Parallel()
	f, err := os.Open("doc.go")
	require.NoError(t, err, "doc.go must be present in pkg/linters")
	defer f.Close() //nolint:errcheck

	headerRe := regexp.MustCompile(`// All (\d+) active analyzers:`)
	var headerCount int
	var bulletCount int
	var foundHeader bool

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if m := headerRe.FindStringSubmatch(line); m != nil {
			n, parseErr := strconv.Atoi(m[1])
			require.NoError(t, parseErr)
			headerCount = n
			foundHeader = true
		}
		if strings.HasPrefix(line, "//   - ") {
			bulletCount++
		}
	}
	require.NoError(t, scanner.Err())
	require.True(t, foundHeader, "doc.go must contain an '// All N active analyzers:' header")

	assert.Equal(t, headerCount, bulletCount,
		"doc.go header says %d analyzers but %d bullet entries were found; "+
			"update the header or add/remove the missing bullets",
		headerCount, bulletCount)
}

func TestDocGo_AnalyzersMatchREADME(t *testing.T) {
	t.Parallel()
	docSet := parseDocBulletSet(t)
	readmeSet := parseReadmeSubpackageSet(t)
	assert.Equal(t, sortedKeys(docSet), sortedKeys(readmeSet),
		"doc.go analyzer bullets and README Subpackages table must list the same analyzers; update both files together")
}

func TestDocSurfacesMatchRegistryAndSpecList(t *testing.T) {
	t.Parallel()
	registrySet := make(map[string]struct{})
	for _, analyzer := range linters.All() {
		registrySet[analyzer.Name] = struct{}{}
	}

	docAsAnalyzerNames := normalizedDocSlugSetToAnalyzerNames(parseDocBulletSet(t))
	readmeSlugs := parseReadmeSubpackageSet(t)
	readmeAsAnalyzerNames := normalizedDocSlugSetToAnalyzerNames(readmeSlugs)

	assert.Equal(t, sortedKeys(registrySet), sortedKeys(docAsAnalyzerNames),
		"doc.go analyzer bullets must match linters.All(); update pkg/linters/doc.go when adding/removing linters")
	assert.Equal(t, sortedKeys(registrySet), sortedKeys(readmeAsAnalyzerNames),
		"README Subpackages analyzer entries must match linters.All(); update pkg/linters/README.md when adding/removing linters")

	documentedLabels := make(map[string]struct{})
	for _, d := range documentedAnalyzers() {
		documentedLabels[d.label] = struct{}{}
	}
	assert.Equal(t, sortedKeys(readmeSlugs), sortedKeys(documentedLabels),
		"documentedAnalyzers() labels in spec_test.go must match README Subpackages table labels exactly")
}

func TestCIEnforcedLintersMatchRegistry(t *testing.T) {
	t.Parallel()
	workflowBytes, err := os.ReadFile("../../.github/workflows/cgo.yml")
	require.NoError(t, err, "cgo.yml must be present")

	native, wasm := parseCIEnforcedLinterSets(t, workflowBytes)
	enforced := make(map[string]struct{})
	for _, target := range []map[string]struct{}{native, wasm} {
		for name := range target {
			enforced[name] = struct{}{}
		}
	}

	registry := make(map[string]struct{})
	for _, analyzer := range linters.All() {
		registry[analyzer.Name] = struct{}{}
		_, enforcedInCI := enforced[analyzer.Name]
		_, intentionallyExcluded := notYetEnforced[analyzer.Name]
		assert.Truef(t, enforcedInCI || intentionallyExcluded,
			"analyzer %q is in linters.All() but neither cgo.yml LINTER_FLAGS nor notYetEnforced; "+
				"add it to CI or document why it is not ready", analyzer.Name)
	}

	for name, reason := range notYetEnforced {
		assert.Containsf(t, registry, name, "notYetEnforced analyzer %q must be registered", name)
		assert.NotContainsf(t, enforced, name, "remove %q from notYetEnforced once CI enforces it", name)
		assert.NotEmptyf(t, reason, "notYetEnforced analyzer %q must have a reason", name)
	}
	for name := range enforced {
		assert.Containsf(t, registry, name, "CI-enforced analyzer %q must be registered", name)
	}
}

func TestCIEnforcedLintersMatchTargets(t *testing.T) {
	t.Parallel()
	workflowBytes, err := os.ReadFile("../../.github/workflows/cgo.yml")
	require.NoError(t, err, "cgo.yml must be present")

	native, wasm := parseCIEnforcedLinterSets(t, workflowBytes)
	require.NotEmpty(t, native, "cgo.yml must enforce custom linters on native Go code")
	require.NotEmpty(t, wasm, "cgo.yml must enforce custom linters on WebAssembly code")

	assert.Empty(t, linterSetDifference(native, wasm),
		"native and wasm CI must enforce the same custom linters; mirror dedicated steps as well as bulk flags")
	assert.Empty(t, linterSetDifference(wasm, native),
		"wasm CI must not enforce custom linters missing from native CI")
}

func parseCIEnforcedLinterSets(t *testing.T, workflowBytes []byte) (map[string]struct{}, map[string]struct{}) {
	t.Helper()
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Run string            `yaml:"run"`
				Env map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(workflowBytes, &workflow), "CI workflow must be valid YAML")
	job, ok := workflow.Jobs["lint-go-custom"]
	require.True(t, ok, "CI workflow must define the lint-go-custom job")

	native := make(map[string]struct{})
	wasm := make(map[string]struct{})
	for _, step := range job.Steps {
		activeLines := slices.DeleteFunc(strings.Split(step.Run, "\n"), func(line string) bool {
			return strings.HasPrefix(strings.TrimSpace(line), "#")
		})
		run := strings.Join(activeLines, "\n")
		goos, goarch := step.Env["GOOS"], step.Env["GOARCH"]
		for field := range strings.FieldsSeq(run) {
			key, value, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch key {
			case "GOOS":
				goos = strings.Trim(value, `"'`)
			case "GOARCH":
				goarch = strings.Trim(value, `"'`)
			}
		}
		target := native
		if goos == "js" && goarch == "wasm" {
			target = wasm
		}
		for _, match := range linterFlagsRe.FindAllStringSubmatch(run, -1) {
			for flag := range strings.FieldsSeq(match[1]) {
				if flag == "-test=false" {
					continue
				}
				require.Truef(t, strings.HasPrefix(flag, "-"), "LINTER_FLAGS entry %q must be a flag", flag)
				target[strings.TrimPrefix(flag, "-")] = struct{}{}
			}
		}
	}
	return native, wasm
}

func linterSetDifference(left, right map[string]struct{}) []string {
	difference := make(map[string]struct{})
	for name := range left {
		if _, ok := right[name]; !ok {
			difference[name] = struct{}{}
		}
	}
	return sortedKeys(difference)
}

func parseDocBulletSet(t *testing.T) map[string]struct{} {
	t.Helper()

	docBytes, err := os.ReadFile("doc.go")
	require.NoError(t, err, "doc.go must be present in pkg/linters")

	docSet := make(map[string]struct{})
	for line := range strings.SplitSeq(string(docBytes), "\n") {
		if m := docBulletRe.FindStringSubmatch(line); m != nil {
			docSet[m[1]] = struct{}{}
		}
	}

	return docSet
}

func parseReadmeSubpackageSet(t *testing.T) map[string]struct{} {
	t.Helper()

	readmeBytes, err := os.ReadFile("README.md")
	require.NoError(t, err, "README.md must be present in pkg/linters")

	readmeSet := make(map[string]struct{})
	for line := range strings.SplitSeq(string(readmeBytes), "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		m := readmeTableRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if m[1] == "internal" {
			continue
		}
		readmeSet[m[1]] = struct{}{}
	}

	return readmeSet
}

func normalizedDocSlugSetToAnalyzerNames(slugSet map[string]struct{}) map[string]struct{} {
	normalized := make(map[string]struct{}, len(slugSet))
	for slug := range slugSet {
		normalized[docSlugToAnalyzerName(slug)] = struct{}{}
	}
	return normalized
}

func docSlugToAnalyzerName(slug string) string {
	switch slug {
	case "panic-in-library-code":
		return "panicinlibrarycode"
	default:
		return slug
	}
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
