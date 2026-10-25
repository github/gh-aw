package taint

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
)

// Replay only authored entry sources in the compiled graph, so their output
// bindings reach generated consumers without traversing generated JavaScript.
func analyzeFastJavaScript(ctx context.Context, document *aw.Document, compiled *actions.Workflow, rule Rule, result Result) (Result, error) {
	recorder := &javaScriptSourceRecorder{seen: make(map[[32]byte]bool)}
	options := *rule.JavaScript
	options.Parser = recorder
	discoveryRule := rule
	discoveryRule.JavaScript = &options
	_, discoveryErr := AnalyzeAuthoredDocument(ctx, document, discoveryRule)
	options = *rule.JavaScript
	options.fastEntries = make(map[string]bool)
	options.fastMatched = make(map[string][]string)
	options.fastSites = make(map[string][]string)
	options.fastConsumed = make(map[string]int)
	for _, source := range recorder.sources {
		options.fastEntries[source] = true
	}
	trackedRule := rule
	trackedRule.JavaScript = &options
	tracked, trackedErr := analyze(compiled, trackedRule, document)
	// Authored exemptions require a successful compiled-entry match. Compiler
	// normalization or unavailable generated wrappers must remain conservative.
	options.fastEntries = nil
	scripts, scriptErr := analyzeAuthoredJavaScript(ctx, document, trackedRule, true)
	for _, finding := range tracked.Findings {
		if slices.ContainsFunc(result.Findings, func(previous Finding) bool {
			return finding.Site == previous.Site && slices.Equal(finding.Sources, previous.Sources)
		}) {
			continue
		}
		scripts.Findings = slices.DeleteFunc(scripts.Findings, func(previous Finding) bool {
			return fastSameSite(finding.Site, previous.Site, options.fastSites) && slices.EqualFunc(finding.Sources, previous.Sources, func(x, y Source) bool {
				return x.Reference == y.Reference
			})
		})
		result.Findings = append(result.Findings, finding)
	}
	result.Findings = append(result.Findings, scripts.Findings...)
	result.Gaps = append(result.Gaps, tracked.Gaps...)
	result.Gaps = append(result.Gaps, scripts.Gaps...)
	return result, errors.Join(discoveryErr, trackedErr, scriptErr)
}

func fastSameSite(compiled, authored Site, locations map[string][]string) bool {
	if compiled.Kind != authored.Kind || compiled.Line != authored.Line {
		return false
	}

	for from, targets := range locations {
		suffix, ok := strings.CutPrefix(authored.Path, from)
		for _, target := range targets {
			if ok && (suffix == "" || strings.HasPrefix(suffix, ".javascript")) && compiled.Path == target+suffix {
				return true
			}
			if fastSiblingSite(compiled.Path, authored.Path, target, from) {
				return true
			}
		}
	}
	return false
}

func fastSiblingSite(compiled, authored, target, from string) bool {
	cp, ci, cs, cok := fastStepLocation(compiled)
	ap, ai, as, aok := fastStepLocation(authored)
	tp, ti, _, tok := fastStepLocation(target)
	fp, fi, _, fok := fastStepLocation(from)
	return cok && aok && tok && fok && cp == tp && ap == fp && ci-ti == ai-fi && cs == as
}

func fastStepLocation(location string) (string, int, string, bool) {
	offset := strings.LastIndex(location, "steps[")
	if offset < 0 {
		return "", 0, "", false
	}
	index, suffix, ok := strings.Cut(location[offset+len("steps["):], "]")
	number, err := strconv.Atoi(index)
	return location[:offset], number, suffix, ok && err == nil
}

func (j *javascript) recordFastEntry() {
	if j.options.fastEntries != nil {
		j.options.fastMatched[j.input.Source] = append(j.options.fastMatched[j.input.Source], j.input.Name)
		return
	}
	index := j.options.fastConsumed[j.input.Source]
	j.options.fastConsumed[j.input.Source] = index + 1
	if target, ok := sliceAt(j.options.fastMatched[j.input.Source], index); ok {
		j.fastVerified = true
		j.options.fastSites[j.input.Name] = append(j.options.fastSites[j.input.Name], target)
	}
}
