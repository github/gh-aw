//go:build !integration

package jsondecodertrailingdataunchecked_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	jsondecodertrailingdataunchecked "github.com/github/gh-aw/pkg/linters/json-decoder-trailing-data-unchecked"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), jsondecodertrailingdataunchecked.Analyzer, "json_decoder_trailing_data_unchecked")
}
