package lint

import (
	"context"

	"github.com/github/gh-aw/pkg/workflow/taint"
)

func SecretLeakRule() Rule {
	return Rule{
		ID:           "secret-leak",
		ThreatRuleID: "CTR-017",
		Description:  "Detect secret values flowing to disclosure sinks",
		Severity:     Error,
		Targets:      ActionsAST | AWAST,
		Check: func(ctx context.Context, input Input) ([]Diagnostic, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			var result taint.Result
			var err error
			rule := taint.SecretLeakRule()
			rule.SafeOutputJobs = input.SafeOutputJobs
			isSink := rule.IsSink
			rule.IsSink = func(site taint.Site) bool {
				return site.Kind != taint.OTLPAttribute && isSink(site)
			}
			if input.JavaScript != nil {
				options := *input.JavaScript
				options.Context = ctx
				rule.JavaScript = &options
			}
			if input.Workflow == nil && input.Document != nil {
				result, err = taint.AnalyzeAuthoredDocument(ctx, input.Document, rule)
			} else if input.Document != nil {
				result, err = taint.AnalyzeDocument(input.Document, input.Workflow, rule)
			} else {
				result, err = taint.Analyze(input.Workflow, rule)
			}
			diagnostics := make([]Diagnostic, 0, len(result.Findings)+len(result.Gaps))
			for _, finding := range result.Findings {
				related := make([]RelatedLocation, 0, len(finding.Sources))
				for _, source := range finding.Sources {
					related = append(related, RelatedLocation{Path: source.Path, Symbol: source.Reference})
				}
				diagnostics = append(diagnostics, Diagnostic{
					Path: finding.Site.Path, Line: finding.Site.Line,
					Related: related,
					Message: "Secret-tainted value reaches " + string(finding.Site.Kind) +
						"; remove the disclosure and pass credentials only to authentication inputs",
				})
			}
			for _, gap := range result.Gaps {
				diagnostics = append(diagnostics, Diagnostic{
					Severity: Warning, Path: gap.Path, Line: gap.Line,
					Message: "Incomplete secret-leak analysis: " + gap.Reason,
				})
			}
			return diagnostics, err
		},
	}
}
