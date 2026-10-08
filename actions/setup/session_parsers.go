package setup

import "embed"

// SessionParserSources contains the runtime parsers and their transitive
// dependencies, not test fixtures. The CLI uses the same sources as Actions.
//
//go:embed js/session_cli.cjs js/unified_session.cjs js/unified_session_payload.cjs js/unified_session_render.cjs
//go:embed js/parse_claude_log.cjs js/parse_codex_log.cjs js/parse_copilot_log.cjs js/parse_gemini_log.cjs
//go:embed js/parse_custom_log.cjs js/parse_pi_log.cjs js/parse_opencode_log.cjs js/parse_goose_log.cjs
//go:embed js/agent_session.cjs js/agent_session_render.cjs js/claude_session.cjs
//go:embed js/provider_refusal.cjs
//go:embed js/codex_session.cjs js/codex_log_framing.cjs js/copilot_session.cjs
//go:embed js/copilot_workflow_events.cjs js/dynamic_workflow_session.cjs
//go:embed js/subagent_session_render.cjs
//go:embed js/gemini_session.cjs
//go:embed js/pi_session.cjs js/pi_session_redaction.cjs js/session_artifact.cjs
//go:embed js/log_parser_bootstrap.cjs js/log_parser_format.cjs js/log_parser_shared.cjs
//go:embed js/log_parser_step_summary_builder.cjs js/markdown_unfencing.cjs
//go:embed js/add_mask_redaction.cjs js/redact_secrets.cjs js/safe_output_manifest.cjs
//go:embed js/work_queue_claim_scope.cjs js/work_queue_codec.cjs js/work_queue_resource_scope.cjs
//go:embed js/aw_context.cjs js/experiment_helpers.cjs js/finish_work_queue_claim.cjs
//go:embed js/work_queue_mcp_server.cjs js/work_queue_replay.cjs js/work_queue_intents.cjs
//go:embed js/work_queue_binding.cjs js/work_queue_policy.cjs js/work_queue_store.cjs
//go:embed js/work_queue_summary_renderer.cjs
//go:embed js/work_queue_memory.cjs
//go:embed js/work_queue_native.cjs js/work_queue_control_receipts.cjs js/work_queue_delivery.cjs
//go:embed js/work_queue_effect_resource.cjs js/work_queue_effect_client.cjs js/work_queue_claim_adapters.cjs
//go:embed js/work_queue_declared_verification.cjs js/work_queue_dependency_resolver.cjs js/work_queue_dispatch.cjs
//go:embed js/work_queue_dispatch_credential.cjs js/work_queue_provisioning.cjs js/work_queue_reconciler.cjs
//go:embed js/work_queue_git_tree_adapter.cjs js/work_queue_graphql_adapter.cjs js/work_queue_rest_adapter.cjs
//go:embed js/work_queue_graph.cjs js/work_queue_limits.cjs js/work_queue_scheduler.cjs js/work_queue_yaml.cjs
//go:embed js/mcp_server_core.cjs js/mcp_dependencies_manager.cjs js/mcp_enhanced_errors.cjs js/mcp_logger.cjs
//go:embed js/mcp_handler_go.cjs js/mcp_handler_javascript.cjs js/mcp_handler_process.cjs
//go:embed js/mcp_handler_python.cjs js/mcp_handler_shell.cjs js/mcp_scripts_validation.cjs
//go:embed js/glob_pattern_helpers.cjs js/handler_auth.cjs js/invocation_context_helpers.cjs
//go:embed js/read_buffer.cjs js/repo_helpers.cjs js/safe_output_helpers.cjs js/staged_preview.cjs
//go:embed js/constants.cjs js/error_codes.cjs js/error_helpers.cjs js/model_costs.cjs js/models.json js/shim.cjs
//go:embed js/agent_execution.cjs js/agent_error_patterns.cjs js/harness_error_patterns.cjs js/harness_crash_signals.cjs
//go:embed js/engine_log_parser.cjs
//go:embed js/parse_kiro_log.cjs
//go:embed js/parse_deepseek_log.cjs
//go:embed js/parse_pydantic_log.cjs
var SessionParserSources embed.FS
