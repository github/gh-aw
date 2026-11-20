package lint

import (
	"context"
	"errors"

	"github.com/github/gh-aw/pkg/workflow/taint"
)

func EnvironmentFileInjectionRule() Rule {
	return Rule{
		ID: "environment-file-injection", Description: "Detect attacker-controlled writes to runner environment and path files",
		Severity: Error, Targets: ActionsAST | AWAST,
		Check: func(ctx context.Context, input Input) ([]Diagnostic, error) {
			if err := integrityInput(ctx, input); err != nil {
				return nil, err
			}
			result, err := analyzeUntrustedFlow(ctx, input, taint.Rule{
				ID: "environment-file-injection", IsSource: templateInjectionSource, IsFilter: templateInjectionFilter,
				IsSink: func(site taint.Site) bool { return site.Kind == taint.EnvironmentFile },
			})
			var diagnostics []Diagnostic
			for _, finding := range result.Findings {
				var related []RelatedLocation
				for _, source := range finding.Sources {
					related = append(related, RelatedLocation{Path: source.Path})
				}
				diagnostics = append(diagnostics, Diagnostic{Path: finding.Site.Path, Line: finding.Site.Line, Related: related,
					Message: "Attacker-controlled data is written to GITHUB_ENV or GITHUB_PATH and can alter later steps; use step-level env or validate and constrain the complete record before writing"})
			}
			for _, gap := range result.Gaps {
				diagnostics = append(diagnostics, Diagnostic{Severity: Warning, Path: gap.Path, Line: gap.Line,
					Message: "Incomplete environment-file-injection analysis: " + gap.Reason})
			}
			return diagnostics, errors.Join(err, ctx.Err())
		},
	}
}
