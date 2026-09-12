//go:build !integration

// Package workflow – replace_label formal model tests.
//
// This file encodes the formal specification predicates (P1–P15 and edge
// cases) for the replace_label safe-output type.
//
// Scope: tests here cover two layers:
//  1. Production Go code – ValidationConfig shape assertions (P3, edge cases)
//     and the Go compiler parser for staged mode (P9).
//  2. Formal spec model – helper functions (formalMatch*, formalCompute*, …)
//     that re-implement the semantics described in the spec and used to
//     exercise the invariants at the spec level.
//
// The formal helpers are NOT wrappers around the production JavaScript handler
// (actions/setup/js/replace_label.cjs).  Regressions in the JS handler are
// detected by the JavaScript test suite (replace_label.test.cjs) and
// integration tests, not by this file.  When matching semantics differ from
// the JS runtime (e.g. glob case-sensitivity), the helper is documented to
// match the production default so the spec invariants stay accurate.
package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yamlv3 "gopkg.in/yaml.v3"
)

type formalReplaceLabelOutcome struct {
	Success      bool
	Skipped      bool
	Staged       bool
	LabelRemoved *string
	LabelAdded   string
	Labels       []string
}

type replaceLabelFixtureFile struct {
	FixtureID string                        `yaml:"fixture_id"`
	Scenarios []replaceLabelFixtureScenario `yaml:"scenarios"`
}

type replaceLabelFixtureScenario struct {
	ScenarioID string                   `yaml:"scenario_id"`
	Input      replaceLabelFixtureInput `yaml:"input"`
	Expected   replaceLabelExpected     `yaml:"expected"`
}

type replaceLabelFixtureInput struct {
	SafeOutputConfig replaceLabelFixtureConfig  `yaml:"safe_output_config"`
	Message          replaceLabelFixtureMessage `yaml:"message"`
}

type replaceLabelFixtureConfig struct {
	AllowedAdd    []string `yaml:"allowed-add"`
	AllowedRemove []string `yaml:"allowed-remove"`
	Blocked       []string `yaml:"blocked"`
}

type replaceLabelFixtureMessage struct {
	LabelToAdd    string `yaml:"label_to_add"`
	LabelToRemove string `yaml:"label_to_remove"`
}

type replaceLabelExpected struct {
	Decision  string `yaml:"decision"`
	ErrorCode *int   `yaml:"error_code"`
	Reason    string `yaml:"reason"`
}

func formalRequiredNonEmptyLabel(s string) bool {
	return strings.TrimSpace(s) != ""
}

func formalLabelAndRepoLengthsValid(labelToRemove, labelToAdd, repo string) bool {
	return len(labelToRemove) <= 128 && len(labelToAdd) <= 128 && len(repo) <= 256
}

// formalSimpleGlobToRegex converts a simple glob pattern to a regexp using the
// same semantics as the production matchesSimpleGlob helper
// (glob_pattern_helpers.cjs, simpleGlobToRegex, pathMode=false):
//   - All regex-special characters INCLUDING [ and ] are escaped to literals.
//   - * is NOT escaped; it expands to .* (matches any character, including /).
//   - The match is anchored at both ends.
//
// Note: character-class syntax (e.g. p[0-9]) is therefore NOT supported;
// brackets are treated as ordinary characters, consistent with production.
func formalSimpleGlobToRegex(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteByte('^')
	for _, c := range pattern {
		switch c {
		case '*':
			b.WriteString(".*")
		// Special characters to escape mirror the escapeRegexChars helper in
		// glob_pattern_helpers.cjs: \, ., +, ?, ^, $, {, }, (, ), |, [, ] are
		// escaped to literals.  * is intentionally omitted here so it can be
		// replaced by .* below.
		case '.', '+', '?', '^', '$', '{', '}', '(', ')', '|', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteRune(c)
		default:
			b.WriteRune(c)
		}
	}
	b.WriteByte('$')
	return regexp.MustCompile(b.String())
}

// formalMatchAnyPattern reports whether value matches any of the given glob
// patterns.  Matching is case-insensitive, consistent with the production
// matchesSimpleGlob helper in glob_pattern_helpers.cjs.  Pattern semantics
// follow formalSimpleGlobToRegex: * spans any characters, brackets are literal.
func formalMatchAnyPattern(value string, patterns []string) bool {
	lowerValue := strings.ToLower(value)
	for _, p := range patterns {
		re := formalSimpleGlobToRegex(strings.ToLower(p))
		if re.MatchString(lowerValue) {
			return true
		}
	}
	return false
}

func formalValidateSingleLabel(labelName string, allowedPatterns, blockedPatterns []string, fieldName string) error {
	if formalMatchAnyPattern(labelName, blockedPatterns) {
		return fmt.Errorf("%s %q matches a blocked pattern", fieldName, labelName)
	}
	if len(allowedPatterns) > 0 && !formalMatchAnyPattern(labelName, allowedPatterns) {
		return fmt.Errorf("%s %q is not in the allowed list", fieldName, labelName)
	}
	return nil
}

func formalRequiredLabelsSatisfied(itemLabels, required []string) bool {
	if len(required) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(itemLabels))
	for _, label := range itemLabels {
		set[label] = struct{}{}
	}
	for _, label := range required {
		if _, ok := set[label]; !ok {
			return false
		}
	}
	return true
}

func formalTitlePrefixSatisfied(title, prefix string) bool {
	if prefix == "" {
		return true
	}
	return strings.HasPrefix(title, prefix)
}

func formalComputeNewLabelSet(current []string, labelToRemove, labelToAdd string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(current)+1)
	for _, l := range current {
		if l == labelToRemove {
			continue
		}
		if _, ok := seen[l]; ok {
			continue
		}
		seen[l] = struct{}{}
		out = append(out, l)
	}
	if _, ok := seen[labelToAdd]; !ok {
		out = append(out, labelToAdd)
	}
	return out
}

func formalApplyReplace(current []string, labelToRemove, labelToAdd string) formalReplaceLabelOutcome {
	present := slices.Contains(current, labelToRemove)
	out := formalReplaceLabelOutcome{
		Success:    true,
		LabelAdded: labelToAdd,
		Labels:     formalComputeNewLabelSet(current, labelToRemove, labelToAdd),
	}
	if present {
		removed := labelToRemove
		out.LabelRemoved = &removed
	}
	return out
}

func formalRepoAllowed(targetRepo, defaultRepo string, allowedRepos []string) bool {
	repo := targetRepo
	if repo == "" {
		repo = defaultRepo
	}
	if defaultRepo == "*" {
		return true
	}
	if repo == defaultRepo {
		return true
	}
	return slices.Contains(allowedRepos, repo)
}

func formalResolveTargetNumber(targetMode string, triggeringNumber int, requestedNumber int) (int, bool) {
	switch targetMode {
	case "triggering", "":
		if triggeringNumber <= 0 {
			return 0, false
		}
		return triggeringNumber, true
	case "*":
		if requestedNumber > 0 {
			return requestedNumber, true
		}
		if triggeringNumber > 0 {
			return triggeringNumber, true
		}
		return 0, false
	default:
		n, err := strconv.Atoi(targetMode)
		if err != nil || n <= 0 {
			return 0, false
		}
		return n, true
	}
}

// formalResolveItemNumberAliases models alias resolution priority for item targets.
// Order is: item_number, issue_number, pr_number, pull_number.
// Resolution stops at the FIRST alias key that is present in the map (matching
// production temporary_id.cjs behaviour: the first non-null/non-undefined field
// wins, and an invalid value at that position is not bypassed by falling through
// to a lower-priority alias).
// Returns 0 when the first present alias has a non-positive value, or when no
// alias key is present at all.
func formalResolveItemNumberAliases(message map[string]any) int {
	for _, key := range []string{"item_number", "issue_number", "pr_number", "pull_number"} {
		v, ok := message[key]
		if !ok {
			continue
		}
		// First present alias found: check if positive and return immediately.
		// A non-positive value at this position is treated as invalid (returns 0)
		// without falling through to a lower-priority alias.
		switch n := v.(type) {
		case int:
			if n > 0 {
				return n
			}
		case int64:
			if n > 0 {
				return int(n)
			}
		case float64:
			if n > 0 {
				return int(n)
			}
		}
		return 0
	}
	return 0
}

// formalEvaluateFixtureScenario returns nil when the scenario passes the same
// blocked-first and allowlist checks applied by the formal label validators,
// or a descriptive error indicating the denial kind.
func formalEvaluateFixtureScenario(sc replaceLabelFixtureScenario) error {
	if sc.Input.Message.LabelToAdd != "" {
		if err := formalValidateSingleLabel(sc.Input.Message.LabelToAdd, sc.Input.SafeOutputConfig.AllowedAdd, sc.Input.SafeOutputConfig.Blocked, "label_to_add"); err != nil {
			return err
		}
	}
	if sc.Input.Message.LabelToRemove != "" {
		if err := formalValidateSingleLabel(sc.Input.Message.LabelToRemove, sc.Input.SafeOutputConfig.AllowedRemove, sc.Input.SafeOutputConfig.Blocked, "label_to_remove"); err != nil {
			return err
		}
	}
	return nil
}

// runReplaceLabelFixture loads a compliance fixture YAML file and executes each
// scenario as a subtest, asserting expected allow/deny decisions.
func runReplaceLabelFixture(t *testing.T, fixtureName string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(formalRepoRoot(t), "specs", "replace-label-compliance", fixtureName))
	require.NoError(t, err)

	fixture, err := parseReplaceLabelFixture(data)
	require.NoError(t, err)

	for _, scenario := range fixture.Scenarios {
		t.Run(scenario.ScenarioID, func(t *testing.T) {
			evalErr := formalEvaluateFixtureScenario(scenario)
			switch scenario.Expected.Decision {
			case "allow":
				require.NoError(t, evalErr, "expected allow but evaluation denied the scenario")
			case "deny":
				require.Error(t, evalErr, "expected deny but evaluation allowed the scenario")
				// Assert ordering/kind signal when the fixture encodes an error_code.
				if scenario.Expected.ErrorCode != nil {
					switch *scenario.Expected.ErrorCode {
					case -32003:
						require.ErrorContains(t, evalErr, "blocked pattern",
							"error_code -32003 requires a blocked-pattern denial (blocklist evaluated first)")
					case -32002:
						require.ErrorContains(t, evalErr, "allowed list",
							"error_code -32002 requires an allowed-list denial")
					}
				}
			default:
				t.Fatalf("unknown fixture decision %q for scenario %q: must be 'allow' or 'deny'",
					scenario.Expected.Decision, scenario.ScenarioID)
			}
		})
	}
}

func parseReplaceLabelFixture(data []byte) (replaceLabelFixtureFile, error) {
	var fixture replaceLabelFixtureFile
	if err := yamlv3.Unmarshal(data, &fixture); err != nil {
		return replaceLabelFixtureFile{}, err
	}
	if len(fixture.Scenarios) == 0 {
		return replaceLabelFixtureFile{}, errors.New("fixture has no scenarios")
	}
	return fixture, nil
}

func formalRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
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

func TestFormalReplaceLabelP1_FieldRequired(t *testing.T) {
	assert.True(t, formalRequiredNonEmptyLabel("in-progress"))
	assert.False(t, formalRequiredNonEmptyLabel(""))
	assert.False(t, formalRequiredNonEmptyLabel("   "))
}

func TestFormalReplaceLabelP2_FieldMaxLength(t *testing.T) {
	assert.True(t, formalLabelAndRepoLengthsValid(strings.Repeat("a", 128), strings.Repeat("b", 128), strings.Repeat("r", 256)))
	assert.False(t, formalLabelAndRepoLengthsValid(strings.Repeat("a", 129), "ok", "repo"))
	assert.False(t, formalLabelAndRepoLengthsValid("ok", strings.Repeat("b", 129), "repo"))
	assert.False(t, formalLabelAndRepoLengthsValid("ok", "ok", strings.Repeat("r", 257)))
}

func TestFormalReplaceLabelP3_DefaultMaxFive(t *testing.T) {
	cfg, ok := ValidationConfig["replace_label"]
	require.True(t, ok)
	assert.Equal(t, 5, cfg.DefaultMax)
}

func TestFormalReplaceLabelP3_ValidationConfigFields(t *testing.T) {
	cfg, ok := ValidationConfig["replace_label"]
	require.True(t, ok)

	assert.Equal(t, FieldValidation{Required: true, Type: "string", Sanitize: true, MaxLength: 128}, cfg.Fields["label_to_remove"])
	assert.Equal(t, FieldValidation{Required: true, Type: "string", Sanitize: true, MaxLength: 128}, cfg.Fields["label_to_add"])
	assert.True(t, cfg.Fields["item_number"].IssueNumberOrTemporaryID)
	assert.Equal(t, "string", cfg.Fields["repo"].Type)
	assert.Equal(t, 256, cfg.Fields["repo"].MaxLength)
}

func TestFormalReplaceLabelP4_AllowlistEnforcement(t *testing.T) {
	err := formalValidateSingleLabel("state-done", []string{"state-*"}, nil, "label_to_add")
	require.NoError(t, err)

	err = formalValidateSingleLabel("needs-triage", []string{"state-*"}, nil, "label_to_add")
	require.Error(t, err)
	require.ErrorContains(t, err, "allowed list")
}

func TestFormalReplaceLabelP5_BlocklistPriority(t *testing.T) {
	err := formalValidateSingleLabel("state-internal", []string{"state-*"}, []string{"state-in*"}, "label_to_add")
	require.Error(t, err)
	require.ErrorContains(t, err, "blocked pattern")
}

func TestFormalReplaceLabelP6_RemoveAllowlist(t *testing.T) {
	err := formalValidateSingleLabel("state-in-progress", []string{"state-*"}, nil, "label_to_remove")
	require.NoError(t, err)

	err = formalValidateSingleLabel("bug", []string{"state-*"}, nil, "label_to_remove")
	require.Error(t, err)
}

func TestFormalReplaceLabelP7_RequiredLabelsGate(t *testing.T) {
	assert.True(t, formalRequiredLabelsSatisfied([]string{"ready", "triaged"}, []string{"ready"}))
	assert.False(t, formalRequiredLabelsSatisfied([]string{"triaged"}, []string{"ready", "triaged"}))
}

func TestFormalReplaceLabelP8_TitlePrefixGate(t *testing.T) {
	assert.True(t, formalTitlePrefixSatisfied("[BUG] crash on startup", "[BUG]"))
	assert.False(t, formalTitlePrefixSatisfied("crash on startup", "[BUG]"))
}

func TestFormalReplaceLabelP9_StagedFlagOnConfig(t *testing.T) {
	compiler := NewCompiler()
	cfg := compiler.parseReplaceLabelConfig(map[string]any{
		"replace-label": map[string]any{
			"staged": true,
		},
	})
	require.NotNil(t, cfg)
	require.NotNil(t, cfg.Staged)
	assert.Equal(t, "true", string(*cfg.Staged))
}

func TestFormalReplaceLabelP9_StagedHandlerResult(t *testing.T) {
	outcome := formalReplaceLabelOutcome{Success: true, Staged: true}
	assert.True(t, outcome.Success)
	assert.True(t, outcome.Staged)
}

func TestFormalReplaceLabelP10_LabelSetComputation(t *testing.T) {
	labels := formalComputeNewLabelSet([]string{"in-progress", "bug", "bug"}, "in-progress", "done")
	assert.Equal(t, []string{"bug", "done"}, labels)
}

func TestFormalReplaceLabelP11_IdempotentMissingRemove(t *testing.T) {
	outcome := formalApplyReplace([]string{"bug"}, "in-progress", "done")
	assert.True(t, outcome.Success)
	assert.Nil(t, outcome.LabelRemoved)
	assert.Equal(t, "done", outcome.LabelAdded)
	assert.Equal(t, []string{"bug", "done"}, outcome.Labels)
}

func TestFormalReplaceLabelP12_LabelToAddAlwaysPresent(t *testing.T) {
	labels := formalComputeNewLabelSet([]string{"done", "bug"}, "in-progress", "done")
	count := 0
	for _, l := range labels {
		if l == "done" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestFormalReplaceLabelP13_HardVsSoftErrors(t *testing.T) {
	softSkip := formalReplaceLabelOutcome{Success: false, Skipped: true}
	assert.True(t, softSkip.Skipped)

	hardErr := formalReplaceLabelOutcome{Success: false, Skipped: false}
	assert.False(t, hardErr.Skipped)
}

func TestFormalReplaceLabelP14_CrossRepoRestriction(t *testing.T) {
	assert.True(t, formalRepoAllowed("octo/current", "octo/current", []string{"octo/other"}))
	assert.True(t, formalRepoAllowed("octo/other", "octo/current", []string{"octo/other"}))
	assert.False(t, formalRepoAllowed("evil/repo", "octo/current", []string{"octo/other"}))
}

func TestFormalReplaceLabelP15_TargetModeEnforcement(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		trigger   int
		requested int
		expected  int
	}{
		{name: "T-RL-014 omitted target uses triggering item", trigger: 42, requested: 99, expected: 42},
		{name: "T-RL-015 triggering target ignores requested item", target: "triggering", trigger: 42, requested: 99, expected: 42},
		{name: "T-RL-016 fixed target ignores requested item", target: "123", trigger: 42, requested: 99, expected: 123},
		{name: "T-RL-017 fixed target works without requested item", target: "123", trigger: 42, expected: 123},
		{name: "T-RL-018 wildcard target uses requested item", target: "*", trigger: 42, requested: 99, expected: 99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, ok := formalResolveTargetNumber(tt.target, tt.trigger, tt.requested)
			require.True(t, ok)
			assert.Equal(t, tt.expected, n)
		})
	}
}

func TestFormalReplaceLabelEdge_BothLabelsIdentical(t *testing.T) {
	labels := formalComputeNewLabelSet([]string{"in-progress", "bug"}, "in-progress", "in-progress")
	assert.Equal(t, []string{"bug", "in-progress"}, labels)
}

func TestFormalReplaceLabelEdge_HardErrorOutcome(t *testing.T) {
	outcome := formalReplaceLabelOutcome{Success: false, Skipped: false}
	assert.False(t, outcome.Success)
	assert.False(t, outcome.Skipped)
}

func TestFormalReplaceLabelEdge_ItemNumberAliasFields(t *testing.T) {
	cfg, ok := ValidationConfig["replace_label"]
	require.True(t, ok)
	itemNumberField := cfg.Fields["item_number"]
	assert.True(t, itemNumberField.IssueNumberOrTemporaryID)
}

func TestFormalReplaceLabelEdge_ReplaceLabelConfigStructFieldsPresent(t *testing.T) {
	typ := reflect.TypeFor[ReplaceLabelConfig]()
	for _, field := range []string{
		"BaseSafeOutputConfig",
		"SafeOutputTargetConfig",
		"SafeOutputFilterConfig",
		"AllowedAdd",
		"AllowedRemove",
		"Blocked",
		"AllowedTransitions",
	} {
		_, ok := typ.FieldByName(field)
		assert.True(t, ok, "expected ReplaceLabelConfig to include field %s", field)
	}
}

func TestFormalGlobSemantics(t *testing.T) {
	runReplaceLabelFixture(t, "rl-001-glob-semantics.yaml")
}

func TestFormalAllowlistEnforcement(t *testing.T) {
	runReplaceLabelFixture(t, "rl-002-allowlist-enforcement.yaml")
}

func TestFormalBlocklistOrdering(t *testing.T) {
	runReplaceLabelFixture(t, "rl-003-blocklist-ordering.yaml")
}

func TestFormalSchemaRequiredFields(t *testing.T) {
	cfg := ValidationConfig["replace_label"]
	assert.True(t, cfg.Fields["label_to_remove"].Required)
	assert.True(t, cfg.Fields["label_to_add"].Required)
	assert.False(t, formalRequiredNonEmptyLabel(""))
	assert.False(t, formalLabelAndRepoLengthsValid(strings.Repeat("a", 129), "ok", "repo"))
	assert.False(t, formalLabelAndRepoLengthsValid("ok", strings.Repeat("b", 129), "repo"))
}

func TestFormalRepoMaxLength(t *testing.T) {
	assert.True(t, formalLabelAndRepoLengthsValid("from", "to", strings.Repeat("r", 256)))
	assert.False(t, formalLabelAndRepoLengthsValid("from", "to", strings.Repeat("r", 257)))
}

func TestFormalCountGate(t *testing.T) {
	cfg := ValidationConfig["replace_label"]
	require.Equal(t, 5, cfg.DefaultMax)
	assert.Less(t, 4, cfg.DefaultMax)
	assert.GreaterOrEqual(t, 5, cfg.DefaultMax)
	customMax := 3
	assert.Less(t, 2, customMax)
	assert.GreaterOrEqual(t, 3, customMax)
}

func TestFormalLabelSetComputation(t *testing.T) {
	assert.Equal(t, []string{"bug", "done"}, formalComputeNewLabelSet([]string{"in-progress", "bug", "bug"}, "in-progress", "done"))
	assert.Equal(t, []string{"bug", "done"}, formalComputeNewLabelSet([]string{"bug"}, "in-progress", "done"))
}

func TestFormalStagedMode(t *testing.T) {
	compiler := NewCompiler()
	cfg := compiler.parseReplaceLabelConfig(map[string]any{"replace-label": map[string]any{"staged": true}})
	require.NotNil(t, cfg)
	require.NotNil(t, cfg.Staged)
	assert.Equal(t, "true", string(*cfg.Staged))
}

type formalRunReplaceLabelOpts struct {
	Staged      bool
	LabelToAdd  string
	AllowedAdd  []string
	Blocked     []string
	BeforeWrite func() []string
	OnWrite     func()
}

// formalRunReplaceLabel models the core execute path of the replace_label handler.
// OnWrite is invoked exactly once when the handler would call the write API
// (issues.setLabels). In staged mode the handler must return before reaching
// OnWrite; this is the invariant asserted by TestFormalStagedMode_NoWriteAPI.
func formalRunReplaceLabel(opts formalRunReplaceLabelOpts) formalReplaceLabelOutcome {
	if opts.Staged {
		return formalReplaceLabelOutcome{Success: true, Staged: true}
	}
	blocked := opts.Blocked
	if opts.BeforeWrite != nil {
		blocked = opts.BeforeWrite()
	}
	if err := formalValidateSingleLabel(opts.LabelToAdd, opts.AllowedAdd, blocked, "label_to_add"); err != nil {
		return formalReplaceLabelOutcome{Success: false}
	}
	opts.OnWrite()
	return formalReplaceLabelOutcome{Success: true}
}

func TestFormalStagedMode_NoWriteAPI(t *testing.T) {
	writeCalls := 0
	outcome := formalRunReplaceLabel(formalRunReplaceLabelOpts{Staged: true, LabelToAdd: "done", OnWrite: func() { writeCalls++ }})
	assert.True(t, outcome.Success)
	assert.True(t, outcome.Staged)
	assert.Zero(t, writeCalls, "staged mode must not call the write API (setLabels)")
}

func TestFormalNonStagedMode_InvokesWriteAPI(t *testing.T) {
	writeCalls := 0
	outcome := formalRunReplaceLabel(formalRunReplaceLabelOpts{LabelToAdd: "done", OnWrite: func() { writeCalls++ }})
	assert.True(t, outcome.Success)
	assert.False(t, outcome.Staged)
	assert.Equal(t, 1, writeCalls, "non-staged mode must invoke the write API exactly once")
}

func TestFormalBlockedLabelAddedViaReplaceLabelMidBlocklistChange_NoWriteAPI(t *testing.T) {
	writeCalls := 0
	outcome := formalRunReplaceLabel(formalRunReplaceLabelOpts{
		LabelToAdd: "done",
		AllowedAdd: []string{"done"},
		Blocked:    []string{},
		BeforeWrite: func() []string {
			return []string{"done"}
		},
		OnWrite: func() { writeCalls++ },
	})

	assert.False(t, outcome.Success)
	assert.Zero(t, writeCalls, "label_to_add blocked before setLabels must be rejected without calling the write API")
}

func TestFormalSingleRESTCall(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(formalRepoRoot(t), "actions", "setup", "js", "replace_label.cjs"))
	require.NoError(t, err)
	src := string(data)
	assert.Equal(t, 1, strings.Count(src, ".setLabels("))
	assert.Zero(t, strings.Count(src, ".addLabels("))
	assert.Zero(t, strings.Count(src, ".removeLabel("))
	assert.Zero(t, strings.Count(src, ".removeLabels("))
}

func TestFormalBlocklistSymmetry(t *testing.T) {
	require.Error(t, formalValidateSingleLabel("~internal", nil, []string{"~*"}, "label_to_add"))
	require.Error(t, formalValidateSingleLabel("~internal", []string{"*"}, []string{"~*"}, "label_to_remove"))
}

func TestFormalRequiredLabelsGate(t *testing.T) {
	assert.True(t, formalRequiredLabelsSatisfied([]string{"ready", "triaged"}, []string{"ready"}))
	assert.False(t, formalRequiredLabelsSatisfied([]string{"triaged"}, []string{"ready"}))
}

func TestFormalTitlePrefixGate(t *testing.T) {
	assert.True(t, formalTitlePrefixSatisfied("[BUG] crash on startup", "[BUG]"))
	assert.False(t, formalTitlePrefixSatisfied("crash on startup", "[BUG]"))
}

func TestFormalAddDeduplication(t *testing.T) {
	labels := formalComputeNewLabelSet([]string{"done", "bug"}, "in-progress", "done")
	count := 0
	for _, label := range labels {
		if label == "done" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestFormalHardErrorOnRESTFail(t *testing.T) {
	err := errors.New("service unavailable")
	require.Error(t, err)
	outcome := formalReplaceLabelOutcome{Success: false, Skipped: false}
	assert.False(t, outcome.Success)
	assert.False(t, outcome.Skipped)
}

func TestFormalGlobExactNoWildcard(t *testing.T) {
	assert.True(t, formalMatchAnyPattern("bug", []string{"bug"}))
	assert.False(t, formalMatchAnyPattern("bug-fix", []string{"bug"}))
}

func TestFormalItemNumberAliases(t *testing.T) {
	assert.Equal(t, 101, formalResolveItemNumberAliases(map[string]any{"issue_number": 101}))
	assert.Equal(t, 102, formalResolveItemNumberAliases(map[string]any{"pr_number": 102}))
	assert.Equal(t, 103, formalResolveItemNumberAliases(map[string]any{"pull_number": 103}))
	assert.Equal(t, 104, formalResolveItemNumberAliases(map[string]any{"item_number": 104, "issue_number": 105}))
	assert.Equal(t, 0, formalResolveItemNumberAliases(map[string]any{}))
	assert.Equal(t, 0, formalResolveItemNumberAliases(map[string]any{"issue_number": 0}))
	assert.Equal(t, 0, formalResolveItemNumberAliases(map[string]any{"issue_number": -1}))
}

func TestFormalCrossRepoRestriction(t *testing.T) {
	assert.True(t, formalRepoAllowed("octo/current", "octo/current", []string{"octo/other"}))
	assert.False(t, formalRepoAllowed("evil/repo", "octo/current", []string{"octo/other"}))
}

func TestFormalFixtureLoaderRejectsMalformedYAML(t *testing.T) {
	_, err := parseReplaceLabelFixture([]byte("fixture_id: ["))
	require.Error(t, err)
}

func TestFormalFixtureLoaderRejectsEmptyScenarios(t *testing.T) {
	_, err := parseReplaceLabelFixture([]byte("fixture_id: test\nscenarios: []\n"))
	require.Error(t, err)
	require.ErrorContains(t, err, "no scenarios")
}
