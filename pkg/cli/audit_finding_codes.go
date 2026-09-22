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
