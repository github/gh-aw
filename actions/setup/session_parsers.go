package setup

import "embed"

// SessionParserSources contains the runtime parsers and their transitive
// dependencies, not test fixtures. The CLI uses the same sources as Actions.
//
//go:embed js/session_cli.cjs js/unified_session.cjs js/unified_session_payload.cjs js/unified_session_render.cjs
//go:embed js/memory_save_results.cjs
//go:embed js/durable_session_record.cjs
//go:embed js/steering_helpers.cjs js/jsonl_helpers.cjs
//go:embed js/schema-validation.cjs js/json_schema_validation.cjs js/memory_schema_contract.cjs
//go:embed js/add_labels_schema.cjs
//go:embed js/skill_inventory.cjs js/markdown_metadata.cjs js/error_recovery.cjs js/github_rate_limit_logger.cjs
//go:embed js/github_api_quota.cjs js/github_api_diagnostics.cjs
//go:embed js/unified_session_order.cjs js/unified_session_otel.cjs
//go:embed js/trace_graders.cjs js/grader_session.cjs js/grader_artifacts.cjs js/trajectory_graders.cjs js/builtin_graders.json js/working_set_metrics.cjs
//go:embed js/custom_graders.cjs js/custom_grader_worker.cjs
//go:embed js/operational_value_grader.cjs js/operational_value_worker.cjs
//go:embed js/workflow_job_durations.cjs
//go:embed js/mcp_configuration.cjs js/symlink_guard.cjs
//go:embed js/mcp_rate_limit.cjs
//go:embed js/parse_claude_log.cjs js/parse_codex_log.cjs js/parse_copilot_log.cjs js/parse_gemini_log.cjs
//go:embed js/parse_custom_log.cjs js/parse_pi_log.cjs js/parse_opencode_log.cjs js/parse_goose_log.cjs
//go:embed js/parse_agy_log.cjs
//go:embed js/agent_session.cjs js/agent_session_render.cjs js/claude_session.cjs
//go:embed js/provider_refusal.cjs
//go:embed js/codex_session.cjs js/codex_log_framing.cjs js/copilot_session.cjs
//go:embed js/copilot_workflow_events.cjs js/dynamic_workflow_session.cjs
//go:embed js/subagent_session_render.cjs
//go:embed js/gemini_session.cjs
//go:embed js/pi_session.cjs js/pi_session_redaction.cjs js/session_artifact.cjs
//go:embed js/log_parser_bootstrap.cjs js/log_parser_format.cjs js/log_parser_shared.cjs
//go:embed js/protected_command_output.cjs
//go:embed js/log_parser_step_summary_builder.cjs js/markdown_unfencing.cjs
//go:embed js/report_ast.cjs js/sanitize_content_core.cjs js/markdown_code_region_balancer.cjs
//go:embed js/temporary_id.cjs
//go:embed js/diagnostic_sanitization.cjs js/sanitize_content.cjs js/missing_info_formatter.cjs
//go:embed js/ephemerals.cjs js/project_timezone.cjs
//go:embed js/add_mask_redaction.cjs js/json_string_scanner.cjs js/mask_value_matcher.cjs js/redact_secrets.cjs js/safe_output_redaction.cjs js/safe_outputs_tools.json js/safe_outputs_config_redact.cjs js/safe_output_manifest.cjs js/safe_output_outcomes.cjs js/safe_outputs_status.cjs
//go:embed js/otlp_header_helpers.cjs js/otlp.cjs js/work_queue_telemetry.cjs
//go:embed js/work_queue_claim_scope.cjs js/work_queue_codec.cjs js/lock_file_headers.cjs js/work_queue_resource_scope.cjs
//go:embed js/work_queue_host.cjs
//go:embed js/aw_context.cjs js/experiment_helpers.cjs js/finish_work_queue_claim.cjs
//go:embed js/webhook_context.cjs js/provider_issue_webhook.cjs js/webhook_resource.cjs
//go:embed js/connector_webhook.cjs js/event_predicate.cjs
//go:embed js/gateway_command_webhook.cjs js/slash_command_matcher.cjs
//go:embed js/github_notification_webhook.cjs js/github_notification_webhooks.json
//go:embed js/messages_core.cjs
//go:embed js/work_queue_mcp_server.cjs js/work_queue_replay.cjs js/work_queue_intents.cjs
//go:embed js/work_queue_issue_contract.cjs
//go:embed js/work_queue_binding.cjs js/work_queue_policy.cjs js/work_queue_store.cjs
//go:embed js/work_queue_auth_cache.cjs
//go:embed js/work_queue_git_transport.cjs js/work_queue_git_objects.cjs js/git_auth_env.cjs
//go:embed js/work_queue_policy_config.cjs
//go:embed js/work_queue_pool_install.cjs
//go:embed js/work_queue_checked_transport.cjs
//go:embed js/work_queue_summary_renderer.cjs
//go:embed js/work_queue_memory.cjs js/work_queue_logging.cjs
//go:embed js/work_queue_native.cjs js/work_queue_control_receipts.cjs js/work_queue_delivery.cjs
//go:embed js/work_queue_native_creation_delivery.cjs
//go:embed js/dispatch_workflow_transport.cjs
//go:embed js/work_queue_effect_resource.cjs js/work_queue_effect_client.cjs js/work_queue_claim_adapters.cjs
//go:embed js/work_queue_declared_verification.cjs js/work_queue_dependency_resolver.cjs js/work_queue_dispatch.cjs
//go:embed js/work_queue_dispatch_credential.cjs js/work_queue_provisioning.cjs js/work_queue_reconciler.cjs
//go:embed js/work_queue_read_retry.cjs
//go:embed js/work_queue_expiration.cjs
//go:embed js/work_queue_deployment.cjs js/work_queue_deployment_control.cjs
//go:embed js/work_queue_git_tree_adapter.cjs js/work_queue_graphql_adapter.cjs js/work_queue_rest_adapter.cjs
//go:embed js/work_queue_graph.cjs js/work_queue_limits.cjs js/work_queue_scheduler.cjs js/work_queue_expiration.cjs
//go:embed js/work_queue_indexes.cjs
//go:embed js/mcp_server_core.cjs js/mcp_dependencies_manager.cjs js/mcp_enhanced_errors.cjs js/mcp_logger.cjs
//go:embed js/levenshtein_distance.cjs
//go:embed js/safe_output_names.cjs
//go:embed js/mcp_handler_go.cjs js/mcp_handler_javascript.cjs js/mcp_handler_process.cjs
//go:embed js/mcp_handler_python.cjs js/mcp_handler_shell.cjs
//go:embed js/glob_pattern_helpers.cjs js/handler_auth.cjs js/invocation_context_helpers.cjs
//go:embed js/read_buffer.cjs js/repo_helpers.cjs js/safe_output_helpers.cjs js/staged_preview.cjs
//go:embed js/constants.cjs js/error_codes.cjs js/error_helpers.cjs js/model_costs.cjs js/shim.cjs
//go:embed js/agent_execution.cjs js/agent_error_patterns.cjs js/harness_error_patterns.cjs js/harness_crash_signals.cjs js/harness_retry_record.cjs
//go:embed js/harness_retry_session.cjs
//go:embed js/engine_log_parser.cjs
//go:embed js/routing_analytics.cjs js/resolve_model_alias.cjs
//go:embed js/parse_kiro_log.cjs
//go:embed js/parse_deepseek_log.cjs
//go:embed js/parse_pydantic_log.cjs
//go:embed js/write_gate_observability.cjs js/write_gate_decisions.cjs js/write_gate_inventory.cjs
//go:embed js/pending_writes.cjs js/pending_writes_files.cjs js/pending_writes_client.cjs js/pending_writes_plan.cjs js/pending_writes_code.cjs
//go:embed js/safe_output_dependencies.cjs js/safe_output_execution_metadata.cjs js/safe_output_submission.cjs
//go:embed js/git_helpers.cjs js/git_auth_helpers.cjs js/git_patch_utils.cjs js/commit_sha_helpers.cjs
//go:embed js/generate_git_bundle.cjs js/generate_git_patch.cjs js/extra_empty_commit.cjs
//go:embed js/push_signed_commits.cjs js/push_to_pull_request_branch.cjs
//go:embed js/resolve_transport_paths.cjs js/transport_integrity.cjs js/manifest_file_helpers.cjs js/patch_path_helpers.cjs
//go:embed js/object_pack_fetch.cjs js/object_pack_limits.cjs js/object_pack_transport.cjs
//go:embed js/create_pull_request_helpers.cjs js/pr_helpers.cjs js/pr_lifecycle.cjs js/template_branch.cjs
//go:embed js/get_base_branch.cjs js/normalize_branch_name.cjs js/find_repo_checkout.cjs js/checkout_manifest.cjs js/workflow_file_exclusions.cjs
//go:embed js/code_scanning_helpers.cjs js/ledger_builtin.cjs js/ledger_store.cjs js/ledger_transactions.cjs
//go:embed js/execution_provenance.cjs js/workflow_metadata_helpers.cjs js/actions_secret_masking.cjs js/update_activation_comment.cjs
//go:embed js/firewall_blocked_domains.cjs js/gateway_difc_filtered.cjs js/limit_enforcement_helpers.cjs
//go:embed js/messages_footer.cjs js/messages_run_status.cjs js/missing_messages_helper.cjs js/generate_footer.cjs
//go:embed js/report_markdown.cjs js/report_redaction.cjs js/report_size.cjs js/issue_report.cjs js/render_template.cjs
//go:embed js/github_api_helpers.cjs js/model_aliases.cjs js/model_attribution.cjs js/compact_numbers.cjs
//go:embed js/templatable.cjs js/is_truthy.cjs js/threat_detection_warning.cjs js/work_queue_git_effects.cjs
var SessionParserSources embed.FS

// GitHubNotificationWebhooks is the source catalog shared by compiler and runtime.
//
//go:embed js/github_notification_webhooks.json
var GitHubNotificationWebhooks []byte
