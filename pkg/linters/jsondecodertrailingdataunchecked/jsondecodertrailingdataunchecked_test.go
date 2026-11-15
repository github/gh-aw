//go:build !integration

package jsondecodertrailingdataunchecked_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/jsondecodertrailingdataunchecked"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), jsondecodertrailingdataunchecked.Analyzer, "jsondecodertrailingdataunchecked")
}
