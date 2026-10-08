package cli

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/semverutil"
)

const (
	minZizmorVersion  = "1.30.1"
	minPoutineVersion = "1.1.6"
)

var scannerVersionPattern = regexp.MustCompile(`(?i)\bv?[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9a-z.-]+)?\b`)

// localScannerPath returns a compatible local scanner, or empty when Docker
// should be used instead. An unrecognized version is not assumed compatible.
func localScannerPath(ctx context.Context, name string) string {
	var minimum string
	switch name {
	case "zizmor":
		minimum = minZizmorVersion
	case "poutine":
		minimum = minPoutineVersion
	default:
		return ""
	}

	path, err := fileutil.ResolveExecutablePath(name)
	if err != nil {
		return ""
	}
	versionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(versionCtx, path, "--version").CombinedOutput()
	if err != nil {
		return ""
	}
	version := scannerVersionPattern.FindString(strings.TrimSpace(string(output)))
	if !semverutil.IsValid(version) || semverutil.Compare(version, minimum) < 0 {
		return ""
	}
	return path
}
