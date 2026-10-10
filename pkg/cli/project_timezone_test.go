//go:build !integration

package cli

import (
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
	"github.com/stretchr/testify/assert"
)

func TestConfigureProjectTimezone_UsesRepoConfigOverEnv(t *testing.T) {
	originalFind := findGitRootForProjectTimezone
	originalLoad := loadRepoConfigForProjectTimezone
	t.Cleanup(func() {
		findGitRootForProjectTimezone = originalFind
		loadRepoConfigForProjectTimezone = originalLoad
		console.ResetTimeLocation()
	})

	t.Setenv(compilerenv.DefaultUTC, "+00:00")
	findGitRootForProjectTimezone = func() (string, error) { return "/repo", nil }
	loadRepoConfigForProjectTimezone = func(string) (*workflow.RepoConfig, error) {
		return &workflow.RepoConfig{UTC: "-04:00"}, nil
	}

	ConfigureProjectTimezone()

	consoleOutput := console.RenderStructStdout([]struct {
		CreatedAt time.Time `console:"header:Created"`
	}{
		{CreatedAt: time.Date(2025, 10, 28, 14, 30, 45, 0, time.UTC)},
	})

	assert.Contains(t, consoleOutput, "2025-10-28 10:30:45 UTC-04:00")
}

func TestConfigureProjectTimezone_UsesEnvFallback(t *testing.T) {
	originalFind := findGitRootForProjectTimezone
	originalLoad := loadRepoConfigForProjectTimezone
	t.Cleanup(func() {
		findGitRootForProjectTimezone = originalFind
		loadRepoConfigForProjectTimezone = originalLoad
		console.ResetTimeLocation()
	})

	t.Setenv(compilerenv.DefaultUTC, "+00:00")
	findGitRootForProjectTimezone = func() (string, error) { return "", assert.AnError }
	loadRepoConfigForProjectTimezone = func(string) (*workflow.RepoConfig, error) {
		t.Fatal("repo config should not be loaded when git root lookup fails")
		return nil, nil
	}

	ConfigureProjectTimezone()

	consoleOutput := console.RenderStructStdout([]struct {
		CreatedAt time.Time `console:"header:Created"`
	}{
		{CreatedAt: time.Date(2025, 1, 28, 14, 30, 45, 0, time.UTC)},
	})

	assert.Contains(t, consoleOutput, "2025-01-28 14:30:45 UTC+00:00")
}

func TestConfigureProjectTimezone_InvalidTimezoneResetsOverride(t *testing.T) {
	originalFind := findGitRootForProjectTimezone
	originalLoad := loadRepoConfigForProjectTimezone
	t.Cleanup(func() {
		findGitRootForProjectTimezone = originalFind
		loadRepoConfigForProjectTimezone = originalLoad
	})

	t.Setenv(compilerenv.DefaultUTC, "west")
	findGitRootForProjectTimezone = func() (string, error) { return "", assert.AnError }
	loadRepoConfigForProjectTimezone = func(string) (*workflow.RepoConfig, error) {
		t.Fatal("repo config should not be loaded when git root lookup fails")
		return nil, nil
	}
	console.SetTimeLocation(time.FixedZone("UTC-07:00", -7*60*60))
	t.Cleanup(console.ResetTimeLocation)

	ConfigureProjectTimezone()

	consoleOutput := console.RenderStructStdout([]struct {
		CreatedAt time.Time `console:"header:Created"`
	}{
		{CreatedAt: time.Date(2025, 10, 28, 14, 30, 45, 0, time.UTC)},
	})

	assert.Contains(t, consoleOutput, "2025-10-28 14:30:45")
	assert.NotContains(t, consoleOutput, "UTC-07:00")
}
