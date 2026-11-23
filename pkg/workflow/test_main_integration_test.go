//go:build integration

package workflow

import (
	"testing"
	"time"
)

// TestMain keeps integration compiler tests independent of live release
// metadata, matching the unit-test TestMain. Cooldown tests inject HTTP responses.
func TestMain(m *testing.M) {
	engineReleaseTimeLookup = func(c *Compiler, endpoint, packageName, version string) (time.Time, error) {
		if c.engineReleaseClient != nil {
			return c.fetchEngineReleaseTime(endpoint, packageName, version)
		}
		return time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), nil
	}
	m.Run()
}
