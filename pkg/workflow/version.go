package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var versionLog = logger.New("workflow:version")

// compilerVersion holds the version of the compiler, set at runtime.
// This is used to include version information in generated workflow headers.
var compilerVersion = "dev"

// isReleaseBuild indicates whether this binary was built as a release.
// This is set at build time via -X linker flag and used to determine
// if version information should be included in generated workflows.
var isReleaseBuild = false

// SetVersion sets the compiler version for inclusion in generated workflow headers.
// Only non-dev versions are included in the generated headers.
func SetVersion(v string) {
	versionLog.Printf("Setting compiler version: %s", v)
	compilerVersion = v
}

// GetVersion returns the current compiler version.
func GetVersion() string {
	return compilerVersion
}

// SetIsRelease sets whether this binary was built as a release.
func SetIsRelease(release bool) {
	versionLog.Printf("Setting release build flag: %v", release)
	isReleaseBuild = release
}

// IsRelease returns whether this binary was built as a release.
func IsRelease() bool {
	return isReleaseBuild
}

// IsReleasedVersion checks if a version string represents a released build.
// It relies on the isReleaseBuild flag set at build time via -X linker flag.
func IsReleasedVersion(version string) bool {
	return isReleaseBuild
}
