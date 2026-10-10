package cli

import "fmt"

func generateSubagentModelUnavailableFindings(runDir string) []AuditFinding {
	attribution, _, err := readSessionModelRouting(runDir)
	if err != nil {
		auditReportLog.Printf("failed to read sub-agent model availability: %v", err)
		return nil
	}
	if attribution == nil {
		return nil
	}
	var findings []AuditFinding
	seen := make(map[sessionSubagentModelUnavailable]struct{})
	for _, model := range attribution.UnavailableSubagentModels {
		if _, duplicate := seen[model]; duplicate {
			continue
		}
		seen[model] = struct{}{}
		findings = append(findings, AuditFinding{
			Code:        AuditFindingSubagentModelUnavailable,
			Category:    "tooling",
			Severity:    "medium",
			Title:       "Sub-agent Model Unavailable",
			Description: fmt.Sprintf("Sub-agent %s declared model %s was unavailable; used inherited session model %s", model.AgentName, model.DeclaredModel, model.Model),
			Impact:      "The sub-agent used the session model instead of its declared model. This fallback is a warning, not a delegation failure.",
		})
	}
	return findings
}
