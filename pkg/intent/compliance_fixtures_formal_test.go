//go:build !integration

package intent_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yamlv3 "gopkg.in/yaml.v3"

	"github.com/github/gh-aw/pkg/intent"
)

type complianceFixture struct {
	FixtureID string `yaml:"fixture_id"`
	Input     struct {
		Artifact             fixtureArtifact             `yaml:"artifact"`
		SuggestedAttribution *fixtureExpectedAttribution `yaml:"suggested_attribution"`
		UnmatchedLabels      []string                    `yaml:"unmatched_labels"`
	} `yaml:"input"`
	Expected fixtureExpected `yaml:"expected"`
}

type fixtureArtifact struct {
	Kind                string                 `yaml:"kind"`
	Number              int                    `yaml:"number"`
	ExplicitIntent      *fixtureExplicitIntent `yaml:"explicit_intent"`
	LinkedClosingIssues []fixtureClosingIssue  `yaml:"linked_closing_issues"`
	Labels              []string               `yaml:"labels"`
}

type fixtureExplicitIntent struct {
	Key    string `yaml:"key"`
	Source string `yaml:"source"`
}

type fixtureClosingIssue struct {
	Number         int    `yaml:"number"`
	InferredIntent string `yaml:"inferred_intent"`
}

type fixtureExpected struct {
	Attribution fixtureExpectedAttribution `yaml:"attribution"`
	Policy      fixtureExpectedPolicy      `yaml:"policy"`
}

type fixtureExpectedAttribution struct {
	Source    string   `yaml:"source"`
	Status    string   `yaml:"status"`
	IntentKey *string  `yaml:"intent_key"`
	Labels    []string `yaml:"labels"`
}

type fixtureExpectedPolicy struct {
	Autonomy              string `yaml:"autonomy"`
	WriteScope            string `yaml:"write_scope"`
	HumanApprovalRequired bool   `yaml:"human_approval_required"`
	AutoMergeAllowed      bool   `yaml:"auto_merge_allowed"`
	MaxAttempts           int    `yaml:"max_attempts"`
}

func TestFormalFixture_ExplicitIntentWinsOverLinkedIssues(t *testing.T) {
	t.Parallel()
	fixture := loadIntentComplianceFixture(t, "explicit-intent-wins.yaml")

	rec := fixtureResolver(fixture).ResolvePullRequest(buildFixturePullRequest(fixture))

	assertFixtureAttribution(t, fixture.Expected.Attribution, rec)
}

func TestFormalFixture_AmbiguousRootIssueSet(t *testing.T) {
	t.Parallel()
	fixture := loadIntentComplianceFixture(t, "ambiguous-root-closing-issues.yaml")

	rec := fixtureResolver(fixture).ResolvePullRequest(buildFixturePullRequest(fixture))

	assertFixtureAttribution(t, fixture.Expected.Attribution, rec)
}

func TestFormalFixture_UnlinkedPullRequestFailsClosed(t *testing.T) {
	t.Parallel()
	fixture := loadIntentComplianceFixture(t, "unlinked-pr-fail-closed.yaml")

	rec := fixtureResolver(fixture).ResolvePullRequest(buildFixturePullRequest(fixture))

	assertFixtureAttribution(t, fixture.Expected.Attribution, rec)
}

func TestFormalFixture_AmbiguousResolvesToSafestPolicy(t *testing.T) {
	t.Parallel()
	fixture := loadIntentComplianceFixture(t, "ambiguous-root-closing-issues.yaml")

	rec := fixtureResolver(fixture).ResolvePullRequest(buildFixturePullRequest(fixture))
	policy := fixturePolicyCompiler(t).Compile(rec, intent.RepositoryContext{})

	assertFixturePolicy(t, fixture.Expected.Policy, policy)
}

func TestFormalFixture_UnlinkedResolvesToSafestPolicy(t *testing.T) {
	t.Parallel()
	fixture := loadIntentComplianceFixture(t, "unlinked-pr-fail-closed.yaml")

	rec := fixtureResolver(fixture).ResolvePullRequest(buildFixturePullRequest(fixture))
	policy := fixturePolicyCompiler(t).Compile(rec, intent.RepositoryContext{})

	assertFixturePolicy(t, fixture.Expected.Policy, policy)
}

func TestFormalFixture_MappedExplicitStatusIsNotFailClosed(t *testing.T) {
	t.Parallel()
	fixture := loadIntentComplianceFixture(t, "explicit-intent-wins.yaml")

	rec := matchingResolver().ResolvePullRequest(buildFixturePullRequest(fixture))
	policy := fixturePolicyCompiler(t).Compile(rec, intent.RepositoryContext{})

	assertFixturePolicy(t, fixture.Expected.Policy, policy)
	assert.NotEqual(t, "propose_only", policy.Autonomy, "mapped explicit intent must not be forced to fail-closed autonomy")
	assert.NotEqual(t, "none", policy.WriteScope, "mapped explicit intent must not be forced to fail-closed write scope")
	assert.False(t, policy.HumanApprovalRequired, "mapped explicit intent should not require fail-closed approval gating")
}

func TestFormalFixture_PolicyDeterminismAcrossRepeatedResolution(t *testing.T) {
	t.Parallel()
	fixture := loadIntentComplianceFixture(t, "explicit-intent-wins.yaml")
	pr := buildFixturePullRequest(fixture)
	resolver := matchingResolver()
	compiler := fixturePolicyCompiler(t)

	rec1 := resolver.ResolvePullRequest(pr)
	rec2 := resolver.ResolvePullRequest(pr)
	policy1 := compiler.Compile(rec1, intent.RepositoryContext{})
	policy2 := compiler.Compile(rec2, intent.RepositoryContext{})

	assertJSONEqual(t, rec1, rec2, "attribution must be byte-identical across repeated resolution")
	assertJSONEqual(t, policy1, policy2, "policy must be byte-identical across repeated compilation")
}

func TestFormalFixture_SingleSourcePerRecordAcrossResolverFixtures(t *testing.T) {
	t.Parallel()

	for _, fixtureName := range []string{
		"explicit-intent-wins.yaml",
		"ambiguous-root-closing-issues.yaml",
		"unlinked-pr-fail-closed.yaml",
		"multi-root-policy.yaml",
		"unmapped-labels-fail-closed.yaml",
	} {
		t.Run(fixtureName, func(t *testing.T) {
			fixture := loadIntentComplianceFixture(t, fixtureName)
			rec := fixtureResolver(fixture).ResolvePullRequest(buildFixturePullRequest(fixture))

			assert.NotEmpty(t, string(rec.Source), "every fixture must resolve to exactly one attribution source")
			assert.Equal(t, fixture.Expected.Attribution.Source, string(rec.Source))
		})
	}
}

func TestFormalFixture_AmbiguousOrderIndependence(t *testing.T) {
	t.Parallel()
	fixture := loadIntentComplianceFixture(t, "ambiguous-root-closing-issues.yaml")
	resolver := matchingResolver()
	compiler := fixturePolicyCompiler(t)

	pr := buildFixturePullRequest(fixture)
	reversed := buildFixturePullRequest(fixture)
	reversed.ClosingIssues[0], reversed.ClosingIssues[1] = reversed.ClosingIssues[1], reversed.ClosingIssues[0]

	rec1 := resolver.ResolvePullRequest(pr)
	rec2 := resolver.ResolvePullRequest(reversed)
	policy1 := compiler.Compile(rec1, intent.RepositoryContext{})
	policy2 := compiler.Compile(rec2, intent.RepositoryContext{})

	assertJSONEqual(t, rec1, rec2, "ambiguous resolution must be independent of closing-issue ordering")
	assertJSONEqual(t, policy1, policy2, "ambiguous policy must be independent of closing-issue ordering")
}

func TestFormalFixture_UnlinkedWithEmptyLabelsSlice(t *testing.T) {
	t.Parallel()
	fixture := loadIntentComplianceFixture(t, "unlinked-pr-fail-closed.yaml")
	pr := buildFixturePullRequest(fixture)
	pr.Labels = []string{}

	rec := fixtureResolver(fixture).ResolvePullRequest(pr)

	assertFixtureAttribution(t, fixture.Expected.Attribution, rec)
}

func TestFormalFixture_ExplicitIntentOverridesSingleClosingIssue(t *testing.T) {
	t.Parallel()
	fixture := loadIntentComplianceFixture(t, "explicit-intent-wins.yaml")
	pr := buildFixturePullRequest(fixture)
	pr.ClosingIssues = pr.ClosingIssues[:1]

	rec := fixtureResolver(fixture).ResolvePullRequest(pr)

	assertFixtureAttribution(t, fixture.Expected.Attribution, rec)
}

func TestFormalFixture_MultiRootPolicyFailsClosed(t *testing.T) {
	t.Parallel()
	fixture := loadIntentComplianceFixture(t, "multi-root-policy.yaml")

	rec := fixtureResolver(fixture).ResolvePullRequest(buildFixturePullRequest(fixture))
	assertFixtureAttribution(t, fixture.Expected.Attribution, rec)
	assertFixturePolicy(t, fixture.Expected.Policy, fixturePolicyCompiler(t).Compile(rec, intent.RepositoryContext{}))
}

func TestFormalFixture_SuggestedAttributionDoesNotSelectPolicy(t *testing.T) {
	t.Parallel()
	fixture := loadIntentComplianceFixture(t, "suggested-attribution-fail-closed.yaml")
	suggestion := fixture.Input.SuggestedAttribution
	require.NotNil(t, suggestion)

	rec := intent.IntentRecord{
		Status: intent.AttributionStatus(suggestion.Status),
	}

	assertFixturePolicy(t, fixture.Expected.Policy, fixturePolicyCompiler(t).Compile(rec, intent.RepositoryContext{}))
}

func TestFormalFixture_UnmappedLabelsFailClosed(t *testing.T) {
	t.Parallel()
	fixture := loadIntentComplianceFixture(t, "unmapped-labels-fail-closed.yaml")

	rec := fixtureResolver(fixture).ResolvePullRequest(buildFixturePullRequest(fixture))
	assertFixtureAttribution(t, fixture.Expected.Attribution, rec)
	assertFixturePolicy(t, fixture.Expected.Policy, fixturePolicyCompiler(t).Compile(rec, intent.RepositoryContext{}))
}

func loadIntentComplianceFixture(t *testing.T, fixtureName string) complianceFixture {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(intentComplianceFixtureDir(t), fixtureName))
	require.NoError(t, err)

	var fixture complianceFixture
	require.NoError(t, yamlv3.Unmarshal(data, &fixture))
	return fixture
}

func fixtureResolver(fixture complianceFixture) intent.Resolver {
	if len(fixture.Input.UnmatchedLabels) == 0 {
		return matchingResolver()
	}

	unmatched := make(map[string]struct{}, len(fixture.Input.UnmatchedLabels))
	for _, label := range fixture.Input.UnmatchedLabels {
		unmatched[label] = struct{}{}
	}
	return intent.Resolver{
		ResolverVersion: "formal-v1",
		MatchLabels: func(labels []string) []string {
			matched := make([]string, 0, len(labels))
			for _, label := range labels {
				if _, isUnmatched := unmatched[label]; !isUnmatched {
					matched = append(matched, label)
				}
			}
			return matched
		},
	}
}

func intentComplianceFixtureDir(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)

	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "specs", "intent-attribution-compliance")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	t.Fatalf("failed to locate repository root from %s", file)
	return ""
}

func buildFixturePullRequest(fixture complianceFixture) intent.PullRequestData {
	pr := intent.PullRequestData{
		NodeID: fmt.Sprintf("PR_fixture_%d", fixture.Input.Artifact.Number),
		URL:    fmt.Sprintf("https://github.com/github/gh-aw/pull/%d", fixture.Input.Artifact.Number),
		Labels: append([]string(nil), fixture.Input.Artifact.Labels...),
	}

	if fixture.Input.Artifact.ExplicitIntent != nil {
		intentKey := fixture.Input.Artifact.ExplicitIntent.Key
		pr.ExplicitIntent = &intent.IntentRecord{
			Status:    intent.AttributionMapped,
			Source:    intent.AttributionSource(fixture.Input.Artifact.ExplicitIntent.Source),
			Rule:      "fixture_explicit_intent",
			IntentKey: &intentKey,
		}
	}

	for _, issue := range fixture.Input.Artifact.LinkedClosingIssues {
		labels := []string(nil)
		if issue.InferredIntent != "" {
			labels = []string{issue.InferredIntent}
		}
		pr.ClosingIssues = append(pr.ClosingIssues, intent.RootReference{
			NodeID: fmt.Sprintf("I_fixture_%d", issue.Number),
			Type:   "issue",
			URL:    fmt.Sprintf("https://github.com/github/gh-aw/issues/%d", issue.Number),
			Labels: labels,
		})
	}

	return pr
}

func fixturePolicyCompiler(t *testing.T) intent.PolicyCompiler {
	t.Helper()

	autoMerge := true

	return intent.PolicyCompiler{
		Rules: []intent.PolicyRule{{
			ID: "fixture-permissive-wildcard",
			Set: intent.ExecutionPolicy{
				Autonomy:              "bounded",
				WriteScope:            "any_branch",
				HumanApprovalRequired: false,
				AutoMergeAllowed:      &autoMerge,
				MaxAttempts:           10,
			},
		}},
	}
}

func assertFixtureAttribution(t *testing.T, expected fixtureExpectedAttribution, rec intent.IntentRecord) {
	t.Helper()

	assert.Equal(t, expected.Status, string(rec.Status))
	assert.Equal(t, expected.Source, string(rec.Source))
	assert.Equal(t, expected.IntentKey, rec.IntentKey)
	if expected.Labels != nil {
		assert.Equal(t, expected.Labels, rec.Labels)
	} else if expected.IntentKey == nil {
		assert.Empty(t, rec.Labels, "fixtures without an intent key must not resolve through blended label attribution")
	}
}

func assertFixturePolicy(t *testing.T, expected fixtureExpectedPolicy, policy intent.ExecutionPolicy) {
	t.Helper()

	assert.Equal(t, expected.Autonomy, policy.Autonomy)
	assert.Equal(t, expected.WriteScope, policy.WriteScope)
	assert.Equal(t, expected.HumanApprovalRequired, policy.HumanApprovalRequired)
	require.NotNil(t, policy.AutoMergeAllowed)
	assert.Equal(t, expected.AutoMergeAllowed, *policy.AutoMergeAllowed)
	assert.Equal(t, expected.MaxAttempts, policy.MaxAttempts)
}

func assertJSONEqual(t *testing.T, left, right any, msg string) {
	t.Helper()

	leftJSON, err := json.Marshal(left)
	require.NoError(t, err)
	rightJSON, err := json.Marshal(right)
	require.NoError(t, err)
	assert.JSONEq(t, string(leftJSON), string(rightJSON), msg)
}
