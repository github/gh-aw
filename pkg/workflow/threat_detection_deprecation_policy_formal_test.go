//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// This formal test suite verifies the rule deprecation lifecycle in
// spec §5.4 (Deprecation Policy) against the actual specification
// artifacts: the Section 5.1 rule catalog, the Section 7.1 baseline rule
// mapping, the Section 8.1 test ID catalog, the Section 10 change log, and
// the rule/test crosswalk in specs/compiler-threat-detection-compliance.
//
// §5.4 uses these annotation conventions (documented in the compliance
// README) so that the obligations are mechanically verifiable:
//
//	Section 5.1 catalog entry: - **CTR-NNN Name** [Deprecated in vX.Y.Z: reason]: ...
//	Section 7.1 mapping row:   | CTR-NNN Name [Deprecated in vX.Y.Z] | — | — |
//	Section 8.1 / crosswalk:   the row is annotated with [DEPRECATED]
//
// The verifier below runs against the real artifacts (so a non-conforming
// deprecation in the specification fails this suite) and against fixtures
// that exercise a deprecated row, since no rule is deprecated today.

var (
	ctrCatalogEntryPattern    = regexp.MustCompile(`^-\s+\*\*(CTR-\d{3})\b`)
	ctrTableRuleRowPattern    = regexp.MustCompile(`^\|\s*(CTR-\d{3})\b`)
	ctrTestCatalogRowPattern  = regexp.MustCompile(`^\|\s*\*{0,2}(T-CTR-\d{3})\*{0,2}`)
	ctrTestCatalogRulePattern = regexp.MustCompile(`(CTR-\d{3})`)
	ctrDeprecationNotice      = regexp.MustCompile(`\[Deprecated in (v\d+\.\d+\.\d+)\s*:\s*([^\]]*)\]`)
	ctrDeprecationAnnotation  = regexp.MustCompile(`\[Deprecated in (v\d+\.\d+\.\d+)[^\]]*\]`)
)

const ctrDeprecatedTestMarker = "[DEPRECATED]"

type ctrCatalogEntry struct {
	RuleID string
	Raw    string
}

type ctrMappingRow struct {
	RuleID         string
	Implementation string
	Tests          string
	Raw            string
}

type ctrTestCatalogRow struct {
	TestID string
	RuleID string
	Raw    string
}

type ctrCrosswalkRow struct {
	RuleID string
	TestID string
	Raw    string
}

type ctrSpecArtifacts struct {
	catalog   map[string]ctrCatalogEntry
	mapping   map[string]ctrMappingRow
	tests     []ctrTestCatalogRow
	crosswalk []ctrCrosswalkRow
	changeLog string
}

// specSection returns the body of the markdown section introduced by heading,
// stopping at the next heading of the same or a higher level.
func specSection(markdown, heading string) string {
	headingLevel := strings.Count(strings.Fields(heading)[0], "#")
	var body []string
	inSection := false
	for line := range strings.SplitSeq(markdown, "\n") {
		if !inSection {
			if strings.HasPrefix(line, heading) {
				inSection = true
			}
			continue
		}
		if fields := strings.Fields(line); len(fields) > 0 && strings.Trim(fields[0], "#") == "" {
			if strings.Count(fields[0], "#") <= headingLevel {
				break
			}
		}
		body = append(body, line)
	}
	return strings.Join(body, "\n")
}

func tableCells(row string) []string {
	trimmed := strings.Trim(strings.TrimSpace(row), "|")
	cells := strings.Split(trimmed, "|")
	for i, cell := range cells {
		cells[i] = strings.TrimSpace(cell)
	}
	return cells
}

// parseCTRSpecArtifacts extracts the §5.1 catalog, §7.1 mapping, and §8.1 test
// catalog from the specification, the version history from the separate
// changelog document, and the compliance README crosswalk.
func parseCTRSpecArtifacts(spec, changeLog, complianceREADME string) ctrSpecArtifacts {
	artifacts := ctrSpecArtifacts{
		catalog:   map[string]ctrCatalogEntry{},
		mapping:   map[string]ctrMappingRow{},
		changeLog: changeLog,
	}

	for line := range strings.SplitSeq(specSection(spec, "### 5.1 Core Rule Catalog"), "\n") {
		if match := ctrCatalogEntryPattern.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			artifacts.catalog[match[1]] = ctrCatalogEntry{RuleID: match[1], Raw: strings.TrimSpace(line)}
		}
	}

	for line := range strings.SplitSeq(specSection(spec, "### 7.1 Baseline Rule Mapping"), "\n") {
		match := ctrTableRuleRowPattern.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		cells := tableCells(line)
		row := ctrMappingRow{RuleID: match[1], Raw: strings.TrimSpace(line)}
		if len(cells) > 1 {
			row.Implementation = cells[1]
		}
		if len(cells) > 2 {
			row.Tests = cells[2]
		}
		artifacts.mapping[match[1]] = row
	}

	for line := range strings.SplitSeq(specSection(spec, "### 8.1 Test ID Catalog"), "\n") {
		match := ctrTestCatalogRowPattern.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		cells := tableCells(line)
		row := ctrTestCatalogRow{TestID: match[1], Raw: strings.TrimSpace(line)}
		if len(cells) > 1 {
			if ruleMatch := ctrTestCatalogRulePattern.FindStringSubmatch(cells[1]); ruleMatch != nil {
				row.RuleID = ruleMatch[1]
			}
		}
		artifacts.tests = append(artifacts.tests, row)
	}

	for line := range strings.SplitSeq(complianceREADME, "\n") {
		match := ctrTableRuleRowPattern.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		cells := tableCells(line)
		row := ctrCrosswalkRow{RuleID: match[1], Raw: strings.TrimSpace(line)}
		if len(cells) > 1 {
			if testMatch := ctrTestCatalogRowPattern.FindStringSubmatch("|" + cells[1]); testMatch != nil {
				row.TestID = testMatch[1]
			}
		}
		artifacts.crosswalk = append(artifacts.crosswalk, row)
	}

	return artifacts
}

// deprecatedRuleIDs reports every rule marked as deprecated in any artifact.
// A rule marked in one artifact but not another still surfaces here so that
// verifyDeprecationPolicy can report the missing annotations.
func (a ctrSpecArtifacts) deprecatedRuleIDs() []string {
	deprecated := map[string]bool{}
	for id, entry := range a.catalog {
		if strings.Contains(entry.Raw, "Deprecated") {
			deprecated[id] = true
		}
	}
	for id, row := range a.mapping {
		if strings.Contains(row.Raw, "Deprecated") {
			deprecated[id] = true
		}
	}
	for _, row := range a.tests {
		if row.RuleID != "" && strings.Contains(row.Raw, ctrDeprecatedTestMarker) {
			deprecated[row.RuleID] = true
		}
	}
	for _, row := range a.crosswalk {
		if strings.Contains(row.Raw, ctrDeprecatedTestMarker) {
			deprecated[row.RuleID] = true
		}
	}
	ids := make([]string, 0, len(deprecated))
	for id := range deprecated {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// requiredTestIDs returns the Section 8.1 test IDs that remain part of the
// required conformance gate (spec §8.3: deprecated test IDs are removed).
func (a ctrSpecArtifacts) requiredTestIDs() []string {
	var required []string
	for _, row := range a.tests {
		if strings.Contains(row.Raw, ctrDeprecatedTestMarker) {
			continue
		}
		required = append(required, row.TestID)
	}
	sort.Strings(required)
	return required
}

// verifyDeprecationPolicy returns one message per §5.4 obligation violated by
// the specification artifacts.
func (a ctrSpecArtifacts) verifyDeprecationPolicy() []string {
	var violations []string
	requiredTests := map[string]bool{}
	for _, testID := range a.requiredTestIDs() {
		requiredTests[testID] = true
	}

	for _, ruleID := range a.deprecatedRuleIDs() {
		entry, ok := a.catalog[ruleID]
		if !ok {
			violations = append(violations, ruleID+": catalog entry must be retained in Section 5.1, not deleted")
			continue
		}

		notice := ctrDeprecationNotice.FindStringSubmatch(entry.Raw)
		if notice == nil || strings.TrimSpace(notice[2]) == "" {
			violations = append(violations, ruleID+": Section 5.1 entry must carry a deprecation notice of the form [Deprecated in vX.Y.Z: reason]")
			continue
		}
		version, reason := notice[1], strings.TrimSpace(notice[2])

		row, ok := a.mapping[ruleID]
		if !ok {
			violations = append(violations, ruleID+": Section 7.1 mapping row must be retained, not deleted")
		} else {
			annotation := ctrDeprecationAnnotation.FindStringSubmatch(row.Raw)
			switch {
			case annotation == nil:
				violations = append(violations, ruleID+": Section 7.1 row must be annotated with [Deprecated in vX.Y.Z]")
			case annotation[1] != version:
				violations = append(violations, fmt.Sprintf("%s: Section 7.1 deprecation version %s does not match Section 5.1 version %s", ruleID, annotation[1], version))
			}
			if implementation := clearedMappingCell(row.Implementation); implementation != "" {
				violations = append(violations, fmt.Sprintf("%s: Section 7.1 implementation mapping must be cleared, found %q", ruleID, implementation))
			}
		}

		testIDs := 0
		for _, test := range a.tests {
			if test.RuleID != ruleID {
				continue
			}
			testIDs++
			if !strings.Contains(test.Raw, ctrDeprecatedTestMarker) {
				violations = append(violations, fmt.Sprintf("%s: Section 8.1 test ID %s must be marked %s", ruleID, test.TestID, ctrDeprecatedTestMarker))
			}
			if requiredTests[test.TestID] {
				violations = append(violations, fmt.Sprintf("%s: test ID %s must not be required for conformance after %s", ruleID, test.TestID, version))
			}
		}
		if testIDs == 0 {
			violations = append(violations, fmt.Sprintf("%s: Section 8.1 test ID rows must be retained and marked %s", ruleID, ctrDeprecatedTestMarker))
		}

		for _, crosswalk := range a.crosswalk {
			if crosswalk.RuleID == ruleID && !strings.Contains(crosswalk.Raw, ctrDeprecatedTestMarker) {
				violations = append(violations, fmt.Sprintf("%s: compliance crosswalk row must be marked %s", ruleID, ctrDeprecatedTestMarker))
			}
		}

		if !hasDeprecationChangeLogEntry(a.changeLog, ruleID, version) {
			violations = append(violations, fmt.Sprintf("%s: change log must document the deprecation with the rule ID, version %s, and rationale (%s)", ruleID, version, reason))
		}
	}

	return violations
}

// clearedMappingCell normalizes a Section 7.1 implementation cell so that an
// empty cell, a dash placeholder, or a bare deprecation annotation all count
// as cleared.
func clearedMappingCell(cell string) string {
	cell = ctrDeprecationAnnotation.ReplaceAllString(cell, "")
	cell = strings.TrimSpace(cell)
	return strings.TrimSpace(strings.Trim(cell, "-—–"))
}

func hasDeprecationChangeLogEntry(changeLog, ruleID, version string) bool {
	for line := range strings.SplitSeq(changeLog, "\n") {
		if !strings.Contains(line, ruleID) || !strings.Contains(line, version) {
			continue
		}
		if strings.Contains(strings.ToLower(line), "deprecat") {
			return true
		}
	}
	return false
}

func readCTRSpecArtifacts(t *testing.T) ctrSpecArtifacts {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	specsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "specs")
	spec, err := os.ReadFile(filepath.Join(specsDir, "compiler-threat-detection-spec.md"))
	require.NoError(t, err)
	changeLog, err := os.ReadFile(filepath.Join(specsDir, "compiler-threat-detection-changelog.md"))
	require.NoError(t, err)
	readme, err := os.ReadFile(filepath.Join(specsDir, "compiler-threat-detection-compliance", "README.md"))
	require.NoError(t, err)
	return parseCTRSpecArtifacts(string(spec), string(changeLog), string(readme))
}

func TestFormal_DeprecationPolicy_SpecArtifactsAreParsed(t *testing.T) {
	artifacts := readCTRSpecArtifacts(t)

	require.NotEmpty(t, artifacts.catalog, "Section 5.1 rule catalog must be parsed")
	require.NotEmpty(t, artifacts.mapping, "Section 7.1 baseline rule mapping must be parsed")
	require.NotEmpty(t, artifacts.tests, "Section 8.1 test ID catalog must be parsed")
	require.NotEmpty(t, artifacts.crosswalk, "compliance crosswalk must be parsed")
	require.NotEmpty(t, artifacts.changeLog, "changelog document must be parsed")

	for _, ruleID := range []string{"CTR-001", "CTR-011", "CTR-026"} {
		require.Contains(t, artifacts.catalog, ruleID)
		require.Contains(t, artifacts.mapping, ruleID)
	}
	require.Contains(t, artifacts.requiredTestIDs(), "T-CTR-001")

	for _, row := range artifacts.tests {
		require.NotEmpty(t, row.RuleID, "test catalog row %s must reference a CTR rule", row.TestID)
	}
}

func TestFormal_DeprecationPolicy_SpecArtifactsConform(t *testing.T) {
	artifacts := readCTRSpecArtifacts(t)
	require.Empty(t, artifacts.verifyDeprecationPolicy(), "specification artifacts must satisfy spec §5.4")
}

const ctrFixtureSpec = `### 5.1 Core Rule Catalog

- **CTR-001 Active Rule**: Detect an active threat class.
- **CTR-002 Retired Rule** [Deprecated in v1.2.3: the compiler feature it depended on was removed]: Detect a retired threat class.

### 5.2 Compiler Response Requirements

### 7.1 Baseline Rule Mapping

| Rule ID | Primary Implementation Areas | Test Coverage Targets |
|---------|------------------------------|-----------------------|
| CTR-001 Active Rule | ` + "`pkg/workflow/active_validation.go`" + ` | ` + "`pkg/workflow/active_validation_test.go`" + ` |
| CTR-002 Retired Rule [Deprecated in v1.2.3] | — | — |

### 7.2 Mapping Audit

### 8.1 Test ID Catalog

| Test ID | Rule | Detection Trigger | Expected Compiler Action | Stable Diagnostic ID |
|---------|------|-------------------|--------------------------|----------------------|
| **T-CTR-001** | CTR-001 Active Rule | trigger | failure | ` + "`CTR-001`" + ` |
| **T-CTR-002** [DEPRECATED] | CTR-002 Retired Rule | trigger | none | ` + "`CTR-002`" + ` |

### 8.2 Optimizer Protocol Test ID Catalog
`

const ctrFixtureChangeLog = `## Version History

### 1.2.3 (2026-01-01)

- Deprecated CTR-002 in v1.2.3 because the compiler feature it depended on was removed.
`

const ctrFixtureREADME = `| Rule ID | Test ID |
|---------|---------|
| CTR-001 | T-CTR-001 |
| CTR-002 | T-CTR-002 [DEPRECATED] |
`

func TestFormal_DeprecationPolicy_CompliantDeprecationConforms(t *testing.T) {
	artifacts := parseCTRSpecArtifacts(ctrFixtureSpec, ctrFixtureChangeLog, ctrFixtureREADME)

	require.Equal(t, []string{"CTR-002"}, artifacts.deprecatedRuleIDs())
	require.Empty(t, artifacts.verifyDeprecationPolicy())

	require.Contains(t, artifacts.catalog, "CTR-002", "deprecated rule's catalog row must be retained")
	require.Contains(t, artifacts.mapping, "CTR-002", "deprecated rule's mapping row must be retained")
	require.Equal(t, []string{"T-CTR-001"}, artifacts.requiredTestIDs(), "deprecated test IDs must leave the required conformance gate")
}

func TestFormal_DeprecationPolicy_DetectsNonConformingDeprecations(t *testing.T) {
	cases := []struct {
		name          string
		spec          string
		changeLog     string
		readme        string
		wantViolation string
	}{
		{
			name:          "catalog entry deleted",
			spec:          strings.ReplaceAll(ctrFixtureSpec, "- **CTR-002 Retired Rule** [Deprecated in v1.2.3: the compiler feature it depended on was removed]: Detect a retired threat class.\n", ""),
			readme:        ctrFixtureREADME,
			wantViolation: "catalog entry must be retained",
		},
		{
			name:          "deprecation notice missing version and reason",
			spec:          strings.ReplaceAll(ctrFixtureSpec, "** [Deprecated in v1.2.3: the compiler feature it depended on was removed]:", "** (Deprecated):"),
			readme:        ctrFixtureREADME,
			wantViolation: "must carry a deprecation notice",
		},
		{
			name:          "mapping row not annotated",
			spec:          strings.ReplaceAll(ctrFixtureSpec, "| CTR-002 Retired Rule [Deprecated in v1.2.3] |", "| CTR-002 Retired Rule |"),
			readme:        ctrFixtureREADME,
			wantViolation: "must be annotated with [Deprecated in vX.Y.Z]",
		},
		{
			name:          "mapping version mismatch",
			spec:          strings.ReplaceAll(ctrFixtureSpec, "[Deprecated in v1.2.3] |", "[Deprecated in v9.9.9] |"),
			readme:        ctrFixtureREADME,
			wantViolation: "does not match Section 5.1 version",
		},
		{
			name:          "implementation mapping not cleared",
			spec:          strings.ReplaceAll(ctrFixtureSpec, "| CTR-002 Retired Rule [Deprecated in v1.2.3] | — |", "| CTR-002 Retired Rule [Deprecated in v1.2.3] | `pkg/workflow/retired_validation.go` |"),
			readme:        ctrFixtureREADME,
			wantViolation: "implementation mapping must be cleared",
		},
		{
			name:          "test ID still required",
			spec:          strings.ReplaceAll(ctrFixtureSpec, "| **T-CTR-002** [DEPRECATED] |", "| **T-CTR-002** |"),
			readme:        ctrFixtureREADME,
			wantViolation: "must be marked [DEPRECATED]",
		},
		{
			name:          "crosswalk row not marked",
			spec:          ctrFixtureSpec,
			readme:        strings.ReplaceAll(ctrFixtureREADME, "| T-CTR-002 [DEPRECATED] |", "| T-CTR-002 |"),
			wantViolation: "crosswalk row must be marked [DEPRECATED]",
		},
		{
			name:          "change log entry missing",
			spec:          ctrFixtureSpec,
			changeLog:     strings.ReplaceAll(ctrFixtureChangeLog, "- Deprecated CTR-002 in v1.2.3 because the compiler feature it depended on was removed.", "- Routine maintenance."),
			readme:        ctrFixtureREADME,
			wantViolation: "change log must document the deprecation",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changeLog := tc.changeLog
			if changeLog == "" {
				changeLog = ctrFixtureChangeLog
			}
			violations := parseCTRSpecArtifacts(tc.spec, changeLog, tc.readme).verifyDeprecationPolicy()
			require.NotEmpty(t, violations)
			require.Contains(t, strings.Join(violations, "\n"), tc.wantViolation)
		})
	}
}

func TestFormal_DeprecationPolicy_DeprecatedTestIDsLeaveRequiredGate(t *testing.T) {
	artifacts := parseCTRSpecArtifacts(ctrFixtureSpec, ctrFixtureChangeLog, ctrFixtureREADME)

	required := artifacts.requiredTestIDs()
	require.Contains(t, required, "T-CTR-001", "active rule's test ID must remain required")
	require.NotContains(t, required, "T-CTR-002", "deprecated rule's test ID must leave the required conformance gate")
}
