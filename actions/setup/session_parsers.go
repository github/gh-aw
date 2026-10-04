package setup

import "embed"

// SessionParserSources contains the runtime parsers and their dependencies, not
// test fixtures or workflow handlers. The CLI uses the same sources as Actions.
//
//go:embed js/session_cli.cjs js/unified_session.cjs js/unified_session_payload.cjs js/unified_session_render.cjs
//go:embed js/parse_claude_log.cjs js/parse_codex_log.cjs js/parse_copilot_log.cjs js/parse_gemini_log.cjs
//go:embed js/parse_custom_log.cjs js/parse_pi_log.cjs js/parse_opencode_log.cjs
//go:embed js/agent_session.cjs js/agent_session_render.cjs js/claude_session.cjs
//go:embed js/codex_session.cjs js/codex_log_framing.cjs js/copilot_session.cjs
//go:embed js/gemini_session.cjs
//go:embed js/pi_session.cjs js/pi_session_redaction.cjs js/session_artifact.cjs
//go:embed js/log_parser_bootstrap.cjs js/log_parser_format.cjs js/log_parser_shared.cjs
//go:embed js/log_parser_step_summary_builder.cjs js/markdown_unfencing.cjs
//go:embed js/add_mask_redaction.cjs js/redact_secrets.cjs js/safe_output_manifest.cjs
//go:embed js/constants.cjs js/error_codes.cjs js/error_helpers.cjs js/model_costs.cjs js/models.json js/shim.cjs
//go:embed js/agent_execution.cjs js/agent_error_patterns.cjs js/harness_error_patterns.cjs js/harness_crash_signals.cjs
var SessionParserSources embed.FS
