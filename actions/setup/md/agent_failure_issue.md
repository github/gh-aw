### What happened

**{workflow_name}** {failure_summary}.

{run_links_context}

**Branch:** `{branch}`

{pull_request_context}

### Next step

Review the failure details below for a targeted fix, or **assign this issue to a coding agent** to investigate.

### Failure details

{secret_verification_context}{copilot_org_billing_error_context}{copilot_agent_not_found_context}{credential_auth_error_context}{inference_access_error_context}{mcp_policy_error_context}{model_not_supported_error_context}{model_endpoint_mismatch_context}{http_400_response_error_context}{ai_credits_rate_limit_error_context}{unknown_model_ai_credits_context}{missing_model_pricing_context}{shell_expansion_guard_rejected_context}{app_token_minting_failed_context}{lockdown_check_failed_context}{oauth_token_check_failed_context}{stale_lock_file_failed_context}{activation_failed_context}{daily_ai_credits_exceeded_context}{daily_ai_credits_guardrail_error_context}{assignment_errors_context}{assign_copilot_failure_context}{skill_install_failure_context}{create_discussion_errors_context}{code_push_failure_context}{repo_memory_validation_context}{push_repo_memory_failure_context}{missing_data_context}{missing_tool_context}{permission_denied_context}{tool_denials_exceeded_context}{missing_safe_outputs_context}{engine_failure_context}{timeout_context}{fork_context}

{runtime_evidence_context}

{optimize_token_consumption_context}

<details>
<summary>Copy a debugging prompt</summary>

Paste this into your coding agent:

```text
Debug the agentic workflow "{workflow_id}" failure.
Follow https://raw.githubusercontent.com/github/gh-aw/main/debug.md
Failed run: {run_url}
```

If the repository includes the `agentic-workflows` skill, load it before investigating.

</details>

<details>
<summary>Manage failure notifications</summary>

To stop creating failure issues for this workflow, set this in its frontmatter and recompile:

```yaml
safe-outputs:
  report-failure-as-issue: false
```

This only disables issue reporting; it does not fix the failure.

</details>
