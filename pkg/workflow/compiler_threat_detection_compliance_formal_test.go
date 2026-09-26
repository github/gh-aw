//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	formalRuleIDPattern = regexp.MustCompile(`^CTR-\d{3}$`)
	formalTestIDPattern = regexp.MustCompile(`^T-CTR-\d{3}$`)
	formalRuleEntry     = regexp.MustCompile(`(?m)^- \*\*(CTR-\d{3}) `)
	formalTestEntry     = regexp.MustCompile(`(?m)^\| \*\*(T-CTR-\d{3})\*\* \| (CTR-\d{3}) `)
	formalMapEntry      = regexp.MustCompile(`(?m)^\| (CTR-\d{3}) \| (T-CTR-\d{3}) \|$`)
)

func formalSpecSection(t *testing.T, filename, start, end string) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "locate formal compliance test")
	data, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "..", "specs", filename))
	require.NoError(t, err)

	section := strings.SplitN(string(data), start, 2)
	require.Len(t, section, 2, "find %q in %s", start, filename)
	if end == "" {
		return section[1]
	}
	section = strings.SplitN(section[1], end, 2)
	require.Len(t, section, 2, "find %q in %s", end, filename)
	return section[0]
}

func formalActiveRules(t *testing.T) map[string]struct{} {
	t.Helper()
	section := formalSpecSection(t, "compiler-threat-detection-spec.md", "### 5.1 Core Rule Catalog", "### 5.2")
	rules := make(map[string]struct{})
	for _, match := range formalRuleEntry.FindAllStringSubmatch(section, -1) {
		rules[match[1]] = struct{}{}
	}
	return rules
}

func formalCatalogTests(t *testing.T) map[string]string {
	t.Helper()
	section := formalSpecSection(t, "compiler-threat-detection-spec.md", "### 8.1 Test ID Catalog", "### 8.2")
	tests := make(map[string]string)
	for _, match := range formalTestEntry.FindAllStringSubmatch(section, -1) {
		tests[match[1]] = match[2]
	}
	return tests
}

func formalComplianceMap(t *testing.T) map[string]string {
	t.Helper()
	section := formalSpecSection(t, filepath.Join("compiler-threat-detection-compliance", "README.md"), "# Compiler Threat Detection Compliance Map", "")
	mapping := make(map[string]string)
	for _, match := range formalMapEntry.FindAllStringSubmatch(section, -1) {
		mapping[match[1]] = match[2]
	}
	return mapping
}

func formalHasUniqueTestIDs(mapping map[string]string) bool {
	seen := make(map[string]struct{}, len(mapping))
	for _, testID := range mapping {
		if _, duplicate := seen[testID]; duplicate {
			return false
		}
		seen[testID] = struct{}{}
	}
	return true
}

func TestFormal_RuleTestIDBijection(t *testing.T) {
	rules := formalActiveRules(t)
	tests := formalCatalogTests(t)
	mapping := formalComplianceMap(t)

	require.Len(t, mapping, len(rules))
	require.Len(t, tests, len(rules))
	require.True(t, formalHasUniqueTestIDs(mapping))

	for ruleID, testID := range mapping {
		_, active := rules[ruleID]
		require.Truef(t, active, "compliance map contains inactive rule %q", ruleID)
		require.Equal(t, ruleID, tests[testID], "test ID %q must map to rule %q", testID, ruleID)
	}
}

func TestFormal_TestIDFormatWellFormed(t *testing.T) {
	mapping := formalComplianceMap(t)
	for ruleID, testID := range mapping {
		require.Regexp(t, formalRuleIDPattern, ruleID)
		require.Regexp(t, formalTestIDPattern, testID)
	}
	// Test IDs are allocated from a single sequence shared with the Section 8.2
	// optimizer protocol catalog, so a test ID number need not match its rule ID
	// number; only uniqueness is required.
	require.True(t, formalHasUniqueTestIDs(mapping))
}

func TestFormal_NoOrphanTestID(t *testing.T) {
	rules := formalActiveRules(t)
	for testID, ruleID := range formalCatalogTests(t) {
		_, active := rules[ruleID]
		require.Truef(t, active, "test ID %q has no active rule", testID)
	}
}

func TestFormal_ActiveRuleCoverageComplete(t *testing.T) {
	rules := formalActiveRules(t)
	mapping := formalComplianceMap(t)
	require.Len(t, mapping, len(rules))

	for ruleID := range rules {
		_, mapped := mapping[ruleID]
		require.Truef(t, mapped, "active rule %q has no required test ID", ruleID)
	}
}

func TestFormal_RepositoryDispatchAppAuthorizationRule(t *testing.T) {
	rules := formalActiveRules(t)
	mapping := formalComplianceMap(t)
	tests := formalCatalogTests(t)

	require.Contains(t, rules, "CTR-029")
	require.Equal(t, "T-CTR-044", mapping["CTR-029"])
	require.Equal(t, "CTR-029", tests["T-CTR-044"])
}

func TestFormal_EdgeCase_DuplicateTestIDViolatesBijection(t *testing.T) {
	mapping := map[string]string{"CTR-001": "T-CTR-001", "CTR-002": "T-CTR-001"}
	require.False(t, formalHasUniqueTestIDs(mapping))
}
