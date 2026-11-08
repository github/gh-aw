//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type formalConformanceRegistryRow struct {
	TestID      string
	Requirement string
	TestFile    string
}

func formalConformanceRegistryRepositoryRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

func formalConformanceRegistryReadFile(t *testing.T, relativePath string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(formalConformanceRegistryRepositoryRoot(t), relativePath))
	require.NoError(t, err)
	return string(content)
}

func formalConformanceRegistryBaselineRows(t *testing.T) []formalConformanceRegistryRow {
	t.Helper()

	content := formalConformanceRegistryReadFile(t, "specs/awf-config-sources-compliance/README.md")
	var rows []formalConformanceRegistryRow
	for line := range strings.SplitSeq(content, "\n") {
		if !strings.HasPrefix(line, "| T-DR-") {
			continue
		}

		cells := strings.Split(line, "|")
		require.Len(t, cells, 7, "registry row: %s", line)
		rows = append(rows, formalConformanceRegistryRow{
			TestID:      strings.TrimSpace(cells[1]),
			Requirement: strings.TrimSpace(cells[2]),
			TestFile:    strings.Trim(strings.TrimSpace(cells[5]), "`"),
		})
	}

	require.NotEmpty(t, rows)
	return rows
}

func formalConformanceRegistryParseSeriesID(id string, prefix string) (int, bool) {
	if !strings.HasPrefix(id, prefix) {
		return 0, false
	}
	numeric := strings.TrimPrefix(id, prefix)
	if len(numeric) < 3 {
		return 0, false
	}
	for _, r := range numeric {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	value, err := strconv.Atoi(numeric)
	if err != nil {
		return 0, false
	}
	return value, true
}

func formalConformanceRegistryParsePlainID(id string) (int, bool) {
	if strings.HasPrefix(id, "T-DR-SAFE-") {
		return 0, false
	}
	return formalConformanceRegistryParseSeriesID(id, "T-DR-")
}

func formalConformanceRegistryIsWellFormedFinalID(id string) bool {
	if strings.HasPrefix(id, "T-DR-SAFE-") {
		_, ok := formalConformanceRegistryParseSeriesID(id, "T-DR-SAFE-")
		return ok
	}
	_, ok := formalConformanceRegistryParsePlainID(id)
	return ok
}

func formalConformanceRegistryNextPlainID(rows []formalConformanceRegistryRow) string {
	max := 0
	for _, row := range rows {
		value, ok := formalConformanceRegistryParsePlainID(row.TestID)
		if !ok {
			continue
		}
		if value > max {
			max = value
		}
	}
	return fmt.Sprintf("T-DR-%03d", max+1)
}

func formalConformanceRegistryHasUniqueIDs(rows []formalConformanceRegistryRow) bool {
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if _, exists := seen[row.TestID]; exists {
			return false
		}
		seen[row.TestID] = struct{}{}
	}
	return true
}

func formalConformanceRegistryHasRequirementReference(row formalConformanceRegistryRow) bool {
	return strings.TrimSpace(row.Requirement) != "" && strings.Contains(row.Requirement, "§")
}

func formalConformanceRegistryHasImplementationFile(row formalConformanceRegistryRow) bool {
	return strings.HasPrefix(row.TestFile, "pkg/workflow/") && strings.HasSuffix(row.TestFile, "_test.go")
}

func formalConformanceRegistryRouteTestFile(spansDriftOutputAndSchema bool) string {
	if spansDriftOutputAndSchema {
		return "pkg/workflow/awf_config_drift_test.go"
	}
	return "pkg/workflow/awf_config_safeguards_formal_test.go"
}

func formalConformanceRegistryHasSpecCrossReference(specContent, id string) bool {
	for offset := 0; ; {
		index := strings.Index(specContent[offset:], id)
		if index < 0 {
			return false
		}
		index += offset
		end := index + len(id)
		if (index == 0 || !formalConformanceRegistryIDCharacter(specContent[index-1])) &&
			(end == len(specContent) || !formalConformanceRegistryIDCharacter(specContent[end])) {
			return true
		}
		offset = end
	}
}

func formalConformanceRegistryIDCharacter(character byte) bool {
	return character >= 'A' && character <= 'Z' ||
		character >= 'a' && character <= 'z' ||
		character >= '0' && character <= '9' ||
		character == '-'
}

func formalConformanceRegistrySeriesDisjoint(id string) bool {
	plain := strings.HasPrefix(id, "T-DR-") && !strings.HasPrefix(id, "T-DR-SAFE-")
	safe := strings.HasPrefix(id, "T-DR-SAFE-")
	return plain != safe
}

func TestFormalConformanceRegistry_P1_TestIDMonotonicity(t *testing.T) {
	next := formalConformanceRegistryNextPlainID(formalConformanceRegistryBaselineRows(t))
	assert.Equal(t, "T-DR-012", next)

	nextValue, ok := formalConformanceRegistryParsePlainID(next)
	require.True(t, ok)
	assert.Equal(t, 12, nextValue)
}

func TestFormalConformanceRegistry_P1_EmptyRegistryStartsAtOne(t *testing.T) {
	assert.Equal(t, "T-DR-001", formalConformanceRegistryNextPlainID(nil))
}

func TestFormalConformanceRegistry_P2_TestIDNoDuplicates(t *testing.T) {
	rows := formalConformanceRegistryBaselineRows(t)
	assert.True(t, formalConformanceRegistryHasUniqueIDs(rows))

	rows = append(rows, formalConformanceRegistryRow{TestID: "T-DR-010", Requirement: "§x", TestFile: "pkg/workflow/awf_config_drift_test.go"})
	assert.False(t, formalConformanceRegistryHasUniqueIDs(rows))
}

func TestFormalConformanceRegistry_P3_TestIDFormatWellFormed(t *testing.T) {
	valid := []string{"T-DR-001", "T-DR-010", "T-DR-1000", "T-DR-SAFE-001", "T-DR-SAFE-1234"}
	invalid := []string{"t-dr-001", "T-DR-01", "T-DR-ABC", "T-DRSAFE-001", "T-DR-SAFE-1", "T-DR-SAFE-01", "T-DR-SAFE-ABC"}

	for _, id := range valid {
		assert.True(t, formalConformanceRegistryIsWellFormedFinalID(id), id)
	}
	for _, id := range invalid {
		assert.False(t, formalConformanceRegistryIsWellFormedFinalID(id), id)
	}
}

func TestFormalConformanceRegistry_P4_PlaceholderIDRejectedAsFinal(t *testing.T) {
	assert.False(t, formalConformanceRegistryIsWellFormedFinalID("T-DR-NNN"))
}

func TestFormalConformanceRegistry_P5_RowHasRequirementReference(t *testing.T) {
	for _, row := range formalConformanceRegistryBaselineRows(t) {
		assert.True(t, formalConformanceRegistryHasRequirementReference(row), row.TestID)
	}
	assert.False(t, formalConformanceRegistryHasRequirementReference(formalConformanceRegistryRow{TestID: "T-DR-011", Requirement: "required fields", TestFile: "pkg/workflow/awf_config_drift_test.go"}))
}

func TestFormalConformanceRegistry_P6_RowHasImplementationFile(t *testing.T) {
	for _, row := range formalConformanceRegistryBaselineRows(t) {
		assert.True(t, formalConformanceRegistryHasImplementationFile(row), row.TestID)
		assert.FileExists(t, filepath.Join(formalConformanceRegistryRepositoryRoot(t), row.TestFile), row.TestID)
	}
	assert.False(t, formalConformanceRegistryHasImplementationFile(formalConformanceRegistryRow{TestID: "T-DR-011", Requirement: "§3.1", TestFile: ""}))
}

func TestFormalConformanceRegistry_P7_SafeguardRowRoutingDecision(t *testing.T) {
	assert.Equal(t, "pkg/workflow/awf_config_safeguards_formal_test.go", formalConformanceRegistryRouteTestFile(false))
	assert.Equal(t, "pkg/workflow/awf_config_drift_test.go", formalConformanceRegistryRouteTestFile(true))
}

func TestFormalConformanceRegistry_P8_SpecCrossReferenceRequired(t *testing.T) {
	specContent := formalConformanceRegistryReadFile(t, "specs/awf-config-sources-spec.md")
	for _, row := range formalConformanceRegistryBaselineRows(t) {
		assert.True(t, formalConformanceRegistryHasSpecCrossReference(specContent, row.TestID), row.TestID)
	}

	assert.False(t, formalConformanceRegistryHasSpecCrossReference(specContent, "T-DR-012"))
}

func TestFormalConformanceRegistry_P9_DriftSeriesVsSafeguardSeriesDisjoint(t *testing.T) {
	assert.True(t, formalConformanceRegistrySeriesDisjoint("T-DR-010"))
	assert.True(t, formalConformanceRegistrySeriesDisjoint("T-DR-SAFE-004"))
	assert.False(t, formalConformanceRegistrySeriesDisjoint("T-DRX-010"))
}

func TestFormalConformanceRegistry_EdgeCase_FourDigitRollover(t *testing.T) {
	rows := []formalConformanceRegistryRow{{TestID: "T-DR-999", Requirement: "§x", TestFile: "pkg/workflow/awf_config_drift_test.go"}}
	next := formalConformanceRegistryNextPlainID(rows)
	assert.Equal(t, "T-DR-1000", next)
	assert.True(t, formalConformanceRegistryIsWellFormedFinalID(next))
}

func TestFormalConformanceRegistry_EdgeCase_SafeguardOnlyRegistryDoesNotAffectPlainSeries(t *testing.T) {
	rows := []formalConformanceRegistryRow{
		{TestID: "T-DR-SAFE-001", Requirement: "§8", TestFile: "pkg/workflow/awf_config_safeguards_formal_test.go"},
		{TestID: "T-DR-SAFE-004", Requirement: "§8", TestFile: "pkg/workflow/awf_config_safeguards_formal_test.go"},
	}
	assert.Equal(t, "T-DR-001", formalConformanceRegistryNextPlainID(rows))
}

func TestFormalConformanceRegistry_EdgeCase_MissingImplementationFileIsInvalid(t *testing.T) {
	row := formalConformanceRegistryRow{TestID: "T-DR-011", Requirement: "§3.1", TestFile: ""}
	assert.False(t, formalConformanceRegistryHasImplementationFile(row))
}
