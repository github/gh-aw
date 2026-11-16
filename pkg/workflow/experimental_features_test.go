//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const experimentalDocsRoot = "../../docs/src/content/docs"

func TestExperimentalFeaturesListIsWellFormed(t *testing.T) {
	t.Parallel()
	features := ExperimentalFeatures()
	require.NotEmpty(t, features)
	ids := make(map[string]bool)
	warnings := make(map[string]bool)
	for _, feature := range features {
		assert.NotEmpty(t, feature.ID, "feature id")
		assert.NotEmpty(t, feature.Name, "feature %q name", feature.ID)
		assert.False(t, ids[feature.ID], "duplicate feature id %q", feature.ID)
		ids[feature.ID] = true
		if feature.CompilerWarning != "" {
			assert.False(t, warnings[feature.CompilerWarning], "duplicate compiler_warning %q", feature.CompilerWarning)
			warnings[feature.CompilerWarning] = true
		}
		assert.False(t, feature.DocsGroup && len(feature.Docs) == 0, "feature %q sets docs_group without docs", feature.ID)
		for _, slide := range feature.Cracktro {
			assert.NotEmpty(t, slide.Slide, "feature %q cracktro slide", feature.ID)
			assert.NotEmpty(t, slide.Badge, "feature %q cracktro badge", feature.ID)
		}
	}
}

func TestExperimentalFeatureChecksMatchList(t *testing.T) {
	t.Parallel()
	// Connector warnings are emitted separately for each alias, not by a predicate.
	checked := map[string]bool{"connectors": true}
	assert.NotPanics(t, func() { experimentalFeatureWarning("connectors") }, "connectors needs a compiler_warning")
	for _, check := range experimentalFeatureChecks {
		assert.False(t, checked[check.id], "duplicate compiler check for %q", check.id)
		checked[check.id] = true
		assert.NotPanics(t, func() { experimentalFeatureWarning(check.id) }, "compiler check %q needs a compiler_warning", check.id)
	}
	for _, feature := range ExperimentalFeatures() {
		if feature.CompilerWarning != "" {
			assert.True(t, checked[feature.ID], "feature %q has a compiler_warning but no compiler check", feature.ID)
		}
	}
}

func TestQMDExperimentalDocsFrontmatter(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile(filepath.Join(experimentalDocsRoot, "experimental", "qmd.md"))
	if os.IsNotExist(err) {
		t.Skip("docs not available")
	}
	require.NoError(t, err)
	parts := strings.SplitN(string(content), "---\n", 3)
	require.Len(t, parts, 3, "QMD page must have opening and closing frontmatter delimiters")
	assert.Empty(t, parts[0])
	var frontmatter map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &frontmatter))
	assert.Equal(t, "QMD Documentation Search", frontmatter["title"])
	assert.Contains(t, parts[2], ":::caution[Experimental]")
}

func TestExperimentalEnginesMatchList(t *testing.T) {
	t.Parallel()
	listed := make(map[string]bool)
	for _, feature := range ExperimentalFeatures() {
		if feature.Engine != "" {
			listed[feature.Engine] = true
		}
	}
	registry := NewEngineRegistry()
	for _, id := range registry.GetSupportedEngines() {
		engine, err := registry.GetEngine(id)
		require.NoError(t, err)
		assert.Equal(t, engine.IsExperimental(), listed[id], "engine %q experimental flag must match data/experimental_features.json", id)
		delete(listed, id)
	}
	assert.Empty(t, listed, "experimental_features.json lists unknown engines")
}

func experimentalDocsFile(t *testing.T, sitePath string) string {
	t.Helper()
	slug := strings.Trim(sitePath, "/")
	for _, candidate := range []string{slug + ".md", slug + ".mdx", slug + "/index.md", slug + "/index.mdx"} {
		path := filepath.Join(experimentalDocsRoot, candidate)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func TestExperimentalFeatureDocsSayExperimental(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat(experimentalDocsRoot); err != nil {
		t.Skip("docs not available")
	}
	for _, feature := range ExperimentalFeatures() {
		for _, sitePath := range feature.Docs {
			path := experimentalDocsFile(t, sitePath)
			if !assert.NotEmpty(t, path, "feature %q docs page %q not found", feature.ID, sitePath) {
				continue
			}
			content, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Regexp(t, `(?i)experimental`, string(content), "feature %q docs page %s must state the feature is experimental", feature.ID, path)
		}
	}
}

func TestExperimentalDocsSidebarGroupMatchesList(t *testing.T) {
	t.Parallel()
	config, err := os.ReadFile("../../docs/astro.config.mjs")
	if err != nil {
		t.Skip("docs not available")
	}
	group := regexp.MustCompile(`(?s)label:\s*"Experimental",.*?items:\s*\[(.*?)\]`).FindSubmatch(config)
	require.NotNil(t, group, "docs sidebar Experimental group not found")
	var sidebar []string
	for _, match := range regexp.MustCompile(`link:\s*"([^"]+)"`).FindAllSubmatch(group[1], -1) {
		sidebar = append(sidebar, string(match[1]))
	}
	var listed []string
	for _, feature := range ExperimentalFeatures() {
		if feature.DocsGroup {
			listed = append(listed, feature.Docs...)
		}
	}
	assert.ElementsMatch(t, listed, sidebar, "docs sidebar Experimental group must match docs_group entries in data/experimental_features.json")
}
