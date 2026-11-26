//go:build !integration

package linters_test

import (
	"flag"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/linters"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis"
)

func analyzerNames(analyzers []*analysis.Analyzer) []string {
	names := make([]string, 0, len(analyzers))
	for _, a := range analyzers {
		names = append(names, a.Name)
	}
	slices.Sort(names)
	return names
}

func TestTiersPartitionAllAnalyzers(t *testing.T) {
	fast := analyzerNames(linters.Fast())
	slow := analyzerNames(linters.Slow())
	require.NotEmpty(t, fast, "fast tier must not be empty")
	require.NotEmpty(t, slow, "slow tier must not be empty")

	for _, name := range slow {
		assert.NotContains(t, fast, name, "analyzer %q must belong to exactly one tier", name)
		assert.True(t, linters.IsSlow(name), "Slow() must only return slow analyzers")
	}

	union := append(slices.Clone(fast), slow...)
	slices.Sort(union)
	assert.Equal(t, analyzerNames(linters.All()), union, "fast and slow tiers must together cover every registered analyzer")
}

func TestSlowAnalyzersAreRegistered(t *testing.T) {
	registered := analyzerNames(linters.All())
	for name, reason := range linters.SlowAnalyzerReasons() {
		assert.Contains(t, registered, name, "slow analyzer %q must be registered in All()", name)
		assert.NotEmpty(t, strings.TrimSpace(reason), "slow analyzer %q must document why it is slow", name)
	}
}

func TestSelectTier(t *testing.T) {
	fastName := linters.Fast()[0].Name
	slowName := linters.Slow()[0].Name

	t.Run("all returns registry unchanged", func(t *testing.T) {
		for _, tier := range []string{"", linters.TierAll} {
			selected, err := linters.SelectTier(tier, []string{"-" + fastName, "-" + slowName})
			require.NoError(t, err)
			assert.Equal(t, analyzerNames(linters.All()), analyzerNames(selected))
		}
	})

	t.Run("unknown tier", func(t *testing.T) {
		_, err := linters.SelectTier("medium", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown linter tier")
	})

	t.Run("rejects enabling an analyzer from another tier", func(t *testing.T) {
		for _, arg := range []string{"-" + slowName, "--" + slowName, "-" + slowName + "=true", "-" + slowName + "=1"} {
			_, err := linters.SelectTier(linters.TierFast, []string{arg})
			require.Errorf(t, err, "enabling %s in the fast tier must fail", arg)
			assert.Contains(t, err.Error(), "GH_AW_LINT_TIER=slow")
		}
		_, err := linters.SelectTier(linters.TierSlow, []string{"-" + fastName})
		require.Error(t, err, "enabling a fast analyzer in the slow tier must fail")
	})

	t.Run("allows disabling and analyzer-specific flags", func(t *testing.T) {
		args := []string{"-" + slowName + "=false", "-largefunc.max-lines=80", "-test=false", "./pkg/...", "--", "-" + slowName}
		_, err := linters.SelectTier(linters.TierFast, args)
		require.NoError(t, err)
	})

	t.Run("stubs keep names and flags but do no work", func(t *testing.T) {
		selected, err := linters.SelectTier(linters.TierFast, nil)
		require.NoError(t, err)
		assert.Equal(t, analyzerNames(linters.All()), analyzerNames(selected), "every analyzer name must stay registered so shared flags parse")

		originals := make(map[string]*analysis.Analyzer)
		for _, a := range linters.All() {
			originals[a.Name] = a
		}
		for _, a := range selected {
			if !linters.IsSlow(a.Name) {
				assert.Same(t, originals[a.Name], a, "fast analyzer %q must run unchanged", a.Name)
				continue
			}
			assert.NotSame(t, originals[a.Name], a, "slow analyzer %q must be stubbed", a.Name)
			originals[a.Name].Flags.VisitAll(func(f *flag.Flag) {
				assert.NotNilf(t, a.Flags.Lookup(f.Name), "stub %q must keep flag %q", a.Name, f.Name)
			})
			assert.Empty(t, a.Requires, "stub %q must not pull in prerequisites", a.Name)
			result, runErr := a.Run(nil)
			require.NoError(t, runErr)
			assert.Nil(t, result)
		}
	})
}

// TestCITierStepsMatchTiers ensures every lint-go-custom step that selects a
// tier only enables analyzers from that tier, so neither tier silently skips
// an enforced linter.
func TestCITierStepsMatchTiers(t *testing.T) {
	workflowBytes, err := os.ReadFile("../../.github/workflows/cgo.yml")
	require.NoError(t, err, "cgo.yml must be present")

	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(workflowBytes, &workflow), "CI workflow must be valid YAML")
	job, ok := workflow.Jobs["lint-go-custom"]
	require.True(t, ok, "CI workflow must define the lint-go-custom job")

	tieredSteps := map[string]int{}
	for _, step := range job.Steps {
		tier := ""
		for field := range strings.FieldsSeq(step.Run) {
			if value, found := strings.CutPrefix(field, "LINTER_TIER="); found {
				tier = strings.Trim(value, `"'`)
			}
		}
		if tier == "" || tier == linters.TierAll {
			continue
		}
		tieredSteps[tier]++
		for _, match := range linterFlagsRe.FindAllStringSubmatch(step.Run, -1) {
			_, err := linters.SelectTier(tier, strings.Fields(match[1]))
			require.NoErrorf(t, err, "step %q must only enable %s-tier analyzers", step.Name, tier)
		}
	}
	assert.Positive(t, tieredSteps[linters.TierFast], "lint-go-custom must run the fast tier")
	assert.Positive(t, tieredSteps[linters.TierSlow], "lint-go-custom must run the slow tier")
}
