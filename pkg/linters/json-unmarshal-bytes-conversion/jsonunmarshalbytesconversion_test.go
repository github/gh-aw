//go:build !integration

package jsonunmarshalbytesconversion_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	jsonunmarshalbytesconversion "github.com/github/gh-aw/pkg/linters/json-unmarshal-bytes-conversion"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), jsonunmarshalbytesconversion.Analyzer, "jsonunmarshalbytesconversion")
}
