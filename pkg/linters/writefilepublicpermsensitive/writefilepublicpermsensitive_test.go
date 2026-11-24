//go:build !integration

package writefilepublicpermsensitive_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/writefilepublicpermsensitive"
)

func TestWriteFilePublicPermSensitive(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), writefilepublicpermsensitive.Analyzer, "example")
}
