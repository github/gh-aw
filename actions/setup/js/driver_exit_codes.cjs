// @ts-check
"use strict";

// Exit code the SDK driver uses to report a main-session model verification failure (substitution
// or unverifiable evidence) so the harness stops without retrying, as it does in CLI mode.
const COPILOT_MODEL_VERIFICATION_EXIT_CODE = 79;

// Exit code for a session stopped by a workflow tool permission denial; keep distinct from 79.
const WORKFLOW_TOOL_PERMISSION_DENIED_EXIT_CODE = 80;

module.exports = { COPILOT_MODEL_VERIFICATION_EXIT_CODE, WORKFLOW_TOOL_PERMISSION_DENIED_EXIT_CODE };
