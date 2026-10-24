// Package taint tracks symbolic sources through the Actions execution AST.
// It never evaluates expressions, executes scripts, or reads secret values.
package taint

import (
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/rhysd/actionlint"
)

type Kind string

const (
	Log           Kind = "log"
	Summary       Kind = "summary"
	Output        Kind = "output"
	Display       Kind = "display"
	ActionInput   Kind = "action-input"
	WorkflowInput Kind = "workflow-input"
	Artifact      Kind = "artifact"
	Network       Kind = "network"
	OTLPAttribute Kind = "otlp-attribute"
	Prompt        Kind = "prompt"
)

// Site identifies a sink without including its potentially sensitive contents.
// Line is relative to the run script; Path is an AST property path.
type Site struct {
	Kind  Kind
	Path  string
	Line  int
	Uses  string
	Input string
}

type Source struct {
	Reference string
	Path      string
}

type Finding struct {
	RuleID  string
	Site    Site
	Sources []Source
}

// CoverageGap identifies incomplete analysis without including script contents.
// Line, when present, is relative to the run script.
type CoverageGap struct {
	Path   string
	Reason string
	Line   int
}

type Result struct {
	Findings []Finding
	Gaps     []CoverageGap
}

// Rule separates sources, disclosure sinks, and value-removing filters.
// Filters must apply to the entire expression, not merely one of its children.
// ActionSources names credential outputs produced by a known action.
type Rule struct {
	ID                 string
	IsSource           func(reference string) bool
	IsSink             func(Site) bool
	IsFilter           func(actionlint.ExprNode) bool
	ActionSources      func(uses string) []string
	JavaScript         *JavaScriptOptions
	IsJavaScriptFilter func(*JavaScriptNode) bool
}

type value []Source

func union(values ...value) value {
	var result value
	for _, items := range values {
		for _, item := range items {
			if !slices.Contains(result, item) {
				result = append(result, item)
			}
		}
	}
	slices.SortFunc(result, func(a, b Source) int {
		if order := strings.Compare(a.Path, b.Path); order != 0 {
			return order
		}
		return strings.Compare(a.Reference, b.Reference)
	})
	return result
}

type scope map[string]value

func (s scope) hasSources() bool {
	for _, sources := range s {
		if len(sources) > 0 {
			return true
		}
	}
	return false
}

func (s scope) lookup(ref string) value {
	ref = strings.ToLower(ref)
	var result value
	for key, sources := range s {
		if ref == key || strings.HasPrefix(key, ref+".") || wildcardMatch(ref, key) || wildcardMatch(key, ref) {
			result = union(result, sources)
		}
	}
	return result
}

func wildcardMatch(pattern, key string) bool {
	p, k := strings.Split(pattern, "."), strings.Split(key, ".")
	if len(p) != len(k) {
		return false
	}
	return slices.EqualFunc(p, k, func(a, b string) bool { return a == "*" || a == b })
}

type analyzer struct {
	rule          Rule
	result        Result
	promptScope   scope
	failure       error
	literalScript bool
}

func (a *analyzer) emit(site Site, sources value) {
	if len(sources) == 0 || !a.rule.IsSink(site) {
		return
	}
	for i, finding := range a.result.Findings {
		if finding.Site == site {
			a.result.Findings[i].Sources = union(finding.Sources, sources)
			return
		}
	}
	a.result.Findings = append(a.result.Findings, Finding{RuleID: a.rule.ID, Site: site, Sources: sources})
}

func (a *analyzer) gap(path, reason string) {
	a.gapAt(path, 0, reason)
}

func (a *analyzer) gapAt(path string, line int, reason string) {
	for i, gap := range a.result.Gaps {
		if gap.Path == path && gap.Reason == reason {
			if line > 0 && (gap.Line == 0 || line < gap.Line) {
				a.result.Gaps[i].Line = line
			}
			return
		}
	}
	a.result.Gaps = append(a.result.Gaps, CoverageGap{Path: path, Reason: reason, Line: line})
}

func (a *analyzer) source(ref, path string, bindings scope) value {
	result := bindings.lookup(ref)
	if a.rule.IsSource(strings.ToLower(ref)) {
		result = union(result, value{{Reference: ref, Path: path}})
	}
	return result
}

func jobPath(id actions.JobID) string {
	return "jobs." + string(id)
}
