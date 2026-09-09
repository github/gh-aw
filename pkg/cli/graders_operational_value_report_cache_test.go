package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperationalValueReportWeeklyCacheRoundTrip(t *testing.T) {
	cacheRoot := t.TempDir()
	week := time.Date(2026, time.August, 27, 15, 0, 0, 0, time.UTC)
	path, err := operationalValueReportWeeklyCachePath(cacheRoot, "github/gh-aw", "daily-file-diet", "abc123", week)
	require.NoError(t, err)

	value := 0.75
	observations := []operationalValueReportObservation{{
		Run:             operationalValueReportRun{ID: "12345", Attempt: 2, CreatedAt: week},
		Value:           &value,
		Status:          "pass",
		Mature:          true,
		EvaluatorDigest: "abc123",
	}}
	require.NoError(t, saveOperationalValueReportWeeklyCache(path, "github/gh-aw", "daily-file-diet", "abc123", week, observations))

	loaded, hit, err := loadOperationalValueReportWeeklyCache(path, "github/gh-aw", "daily-file-diet", "abc123", week)
	require.NoError(t, err)
	require.True(t, hit)
	require.Len(t, loaded, 1)
	assert.Equal(t, "12345", loaded[0].Run.ID)
	assert.InDelta(t, 0.75, *loaded[0].Value, 0.000001)

	_, hit, err = loadOperationalValueReportWeeklyCache(path, "github/gh-aw", "daily-file-diet", "different", week)
	require.NoError(t, err)
	assert.False(t, hit)
}

func TestOperationalValueReportWeeklyCacheRejectsSymlink(t *testing.T) {
	cacheRoot := t.TempDir()
	target := cacheRoot + "/target.json"
	require.NoError(t, os.WriteFile(target, []byte("{}"), 0o600))
	link := cacheRoot + "/cache.json"
	require.NoError(t, os.Symlink(target, link))

	_, _, err := loadOperationalValueReportWeeklyCache(link, "github/gh-aw", "daily-file-diet", "abc123", time.Now())
	require.ErrorContains(t, err, "must be a regular file")
}

func TestOperationalValueUTCWeekStartUsesMonday(t *testing.T) {
	value := time.Date(2026, time.August, 30, 23, 59, 0, 0, time.FixedZone("offset", -7*60*60))
	assert.Equal(t, "2026-08-31T00:00:00Z", operationalValueUTCWeekStart(value).Format(time.RFC3339))
}

func TestOperationalValueReportWeeklyCacheUsesSensitivePermissions(t *testing.T) {
	cacheRoot := t.TempDir()
	week := time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC)
	path, err := operationalValueReportWeeklyCachePath(cacheRoot, "github/gh-aw", "daily-file-diet", "abc123", week)
	require.NoError(t, err)

	require.NoError(t, saveOperationalValueReportWeeklyCache(path, "github/gh-aw", "daily-file-diet", "abc123", week, nil))

	dirInfo, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	assert.Equal(t, constants.DirPermSensitive, dirInfo.Mode().Perm())

	fileInfo, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, constants.FilePermSensitive, fileInfo.Mode().Perm())
}
