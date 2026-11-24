package workflow

import "time"

// SetEngineReleaseTimeLookupForTesting replaces public engine release-metadata
// lookups so tests in other packages compile deterministically without network
// access. Compilers with an injected release HTTP client keep using it. The
// returned function restores the previous lookup. Not for production use.
func SetEngineReleaseTimeLookupForTesting(lookup func(packageName, version string) (time.Time, error)) func() {
	previous := engineReleaseTimeLookup
	engineReleaseTimeLookup = func(c *Compiler, endpoint, packageName, version string) (time.Time, error) {
		if c.engineReleaseClient != nil {
			return c.fetchEngineReleaseTime(endpoint, packageName, version)
		}
		return lookup(packageName, version)
	}
	resetSharedEngineReleaseCache()
	return func() {
		engineReleaseTimeLookup = previous
		resetSharedEngineReleaseCache()
	}
}

func resetSharedEngineReleaseCache() {
	sharedEngineReleaseCache.Lock()
	defer sharedEngineReleaseCache.Unlock()
	sharedEngineReleaseCache.entries = map[string]engineReleaseMetadata{}
}
