package cli

// AuditFindingCode is a stable machine-readable identifier for an audit finding type.
// Codes are part of the structured audit output contract and must not be repurposed.
type AuditFindingCode string

const (
	AuditFindingWorkflowFailed         AuditFindingCode = "workflow_failed"
	AuditFindingWorkflowTimeout        AuditFindingCode = "workflow_timeout"
	AuditFindingHighTokenUsage         AuditFindingCode = "high_token_usage"
	AuditFindingManyIterations         AuditFindingCode = "many_iterations"
	AuditFindingMultipleErrors         AuditFindingCode = "multiple_errors"
	AuditFindingMCPServerFailures      AuditFindingCode = "mcp_server_failures"
	AuditFindingToolsNotAvailable      AuditFindingCode = "tools_not_available"
	AuditFindingBlockedNetworkRequests AuditFindingCode = "blocked_network_requests"
	AuditFindingWorkflowSucceeded      AuditFindingCode = "workflow_succeeded"
	AuditFindingDetectionJobFailed     AuditFindingCode = "threat_detection_job_failed"
	AuditFindingThreatDetected         AuditFindingCode = "threat_detected"

	AuditFindingAgenticResourceHeavy      AuditFindingCode = "agentic_resource_heavy_for_domain"
	AuditFindingAgenticOverkill           AuditFindingCode = "agentic_overkill_for_agentic"
	AuditFindingAgenticPoorControl        AuditFindingCode = "agentic_poor_agentic_control"
	AuditFindingAgenticPartiallyReducible AuditFindingCode = "agentic_partially_reducible"
	AuditFindingAgenticModelDowngrade     AuditFindingCode = "agentic_model_downgrade_available"
	AuditFindingAgenticDelegatedContext   AuditFindingCode = "agentic_delegated_context_present"
)

var agenticAuditFindingCodes = map[string]AuditFindingCode{
	"resource_heavy_for_domain": AuditFindingAgenticResourceHeavy,
	"overkill_for_agentic":      AuditFindingAgenticOverkill,
	"poor_agentic_control":      AuditFindingAgenticPoorControl,
	"partially_reducible":       AuditFindingAgenticPartiallyReducible,
	"model_downgrade_available": AuditFindingAgenticModelDowngrade,
	"delegated_context_present": AuditFindingAgenticDelegatedContext,
}

func addMissingAuditFindingCodes(findings []AuditFinding) []AuditFinding {
	coded := make([]AuditFinding, 0, len(findings))
	for _, finding := range findings {
		if finding.Code == "" {
			for kind, code := range agenticAuditFindingCodes {
				if finding.Title == prettifyAssessmentKind(kind) {
					finding.Code = code
					break
				}
			}
		}
		coded = append(coded, finding)
	}
	return coded
}
