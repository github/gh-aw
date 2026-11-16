// This file loads the centralized list of experimental features.
//
// data/experimental_features.json is the single source of truth for whether a
// gh-aw feature is experimental. The compiler derives its experimental-feature
// notices from it, and tests check that the docs and the cracktro talk deck
// (docs/tests/talk.spec.ts) agree with it.

package workflow

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed data/experimental_features.json
var experimentalFeaturesJSON []byte

// ExperimentalFeature describes one entry of data/experimental_features.json.
type ExperimentalFeature struct {
	ID              string                      `json:"id"`
	Name            string                      `json:"name"`
	CompilerWarning string                      `json:"compiler_warning,omitempty"`
	Engine          string                      `json:"engine,omitempty"`
	Docs            []string                    `json:"docs,omitempty"`
	DocsGroup       bool                        `json:"docs_group,omitempty"`
	Cracktro        []ExperimentalCracktroSlide `json:"cracktro,omitempty"`
}

// ExperimentalCracktroSlide names a talk-deck slide that carries the Experimental badge.
type ExperimentalCracktroSlide struct {
	Slide string `json:"slide"`
	Badge string `json:"badge"`
}

const experimentalFeatureWarningPrefix = "Using experimental feature: "

var experimentalFeaturesByID = sync.OnceValue(func() map[string]ExperimentalFeature {
	byID := make(map[string]ExperimentalFeature)
	for _, feature := range ExperimentalFeatures() {
		byID[feature.ID] = feature
	}
	return byID
})

// ExperimentalFeatures returns the embedded experimental features in file order.
func ExperimentalFeatures() []ExperimentalFeature {
	var data struct {
		Features []ExperimentalFeature `json:"features"`
	}
	if err := json.Unmarshal(experimentalFeaturesJSON, &data); err != nil {
		panic(fmt.Sprintf("BUG: workflow: could not parse embedded experimental_features.json: %v", err))
	}
	return data.Features
}

// LookupExperimentalFeature returns the experimental feature with the given ID.
func LookupExperimentalFeature(id string) (ExperimentalFeature, bool) {
	feature, ok := experimentalFeaturesByID()[id]
	return feature, ok
}

// experimentalFeatureWarning returns the compiler notice for an experimental feature ID.
// It panics for IDs that are missing from the list or have no compiler warning, which
// tests catch before release.
func experimentalFeatureWarning(id string) string {
	feature, ok := LookupExperimentalFeature(id)
	if !ok || feature.CompilerWarning == "" {
		panic(fmt.Sprintf("BUG: workflow: experimental feature %q has no compiler_warning in data/experimental_features.json", id))
	}
	return experimentalFeatureWarningPrefix + feature.CompilerWarning
}
