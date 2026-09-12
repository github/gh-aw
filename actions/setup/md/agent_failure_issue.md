### Workflow Failure

**Workflow:** [{workflow_name}]({workflow_source_url})  
**Branch:** {branch}  
**Run:** {run_url}{pull_request_info}

{secret_verification_context}{docker_sbx_secrets_context}{credential_auth_error_context}{inference_access_error_context}{mcp_policy_error_context}{model_not_supported_error_context}{http_400_response_error_context}{ai_credits_rate_limit_error_context}{unknown_model_ai_credits_context}{missing_model_pricing_context}{shell_expansion_guard_rejected_context}{app_token_minting_failed_context}{lockdown_check_failed_context}{oauth_token_check_failed_context}{stale_lock_file_failed_context}{daily_ai_credits_exceeded_context}{daily_ai_credits_guardrail_error_context}{assignment_errors_context}{assign_copilot_failure_context}{skill_install_failure_context}{create_discussion_errors_context}{code_push_failure_context}{repo_memory_validation_context}{push_repo_memory_failure_context}{missing_data_context}{missing_tool_context}{permission_denied_context}{tool_denials_exceeded_context}{report_incomplete_context}{missing_safe_outputs_context}{engine_failure_context}{timeout_context}{fork_context}

### Action Required

**Assign this issue to an agent** to debug and fix the issue.

{optimize_token_consumption_context}
<details>
<summary>Debug with any coding agent</summary>

Use this prompt with any coding agent (GitHub Copilot, Claude, Gemini, etc.):

````
Debug the agentic workflow failure using https://raw.githubusercontent.com/github/gh-aw/main/debug.md

The failed workflow run is at {run_url}
````

</details>

<details>
<summary>Manually invoke the agent</summary>

Debug this workflow failure using your favorite Agent CLI and the `agentic-workflows` prompt.

- Start your agent
- Load the `agentic-workflows` skill from `.github/skills/agentic-workflows/SKILL.md` or <https://github.com/github/gh-aw/blob/main/.github/skills/agentic-workflows/SKILL.md>
- Type `debug the agentic workflow {workflow_id} failure in {run_url}`

</details>

> [!TIP]
> <details>
> <summary>Stop reporting this workflow as a failure</summary>
>
> To stop a workflow from creating failure issues, set `report-failure-as-issue: false` in its frontmatter:
> ```yaml
> safe-outputs:
>   report-failure-as-issue: false
> ```
>
> </details>
