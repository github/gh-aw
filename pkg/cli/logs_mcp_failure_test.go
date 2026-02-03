//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestExtractMCPFailuresFromSafeOutputsServer(t *testing.T) {
	// Test specific scenario from https://github.com/github/gh-aw/actions/runs/17701181429/job/50307041425
	// where safeoutputs MCP server failed to launch
	logContent := `{"type":"system","subtype":"init","cwd":"/home/runner/work/gh-aw/gh-aw","session_id":"d5d6d3c4-b04d-4b41-ba8e-6de4e93648bb","tools":["Task","Bash","Glob","Grep","LS","ExitPlanMode","Read","Edit","MultiEdit","Write","NotebookEdit","WebFetch","TodoWrite","WebSearch","BashOutput","KillBash","mcp__github__add_comment_to_pending_review","mcp__github__add_issue_comment","mcp__github__add_sub_issue","mcp__github__assign_copilot_to_issue","mcp__github__cancel_workflow_run","mcp__github__create_and_submit_pull_request_review","mcp__github__create_branch","mcp__github__create_gist","mcp__github__create_issue","mcp__github__create_or_update_file","mcp__github__create_pending_pull_request_review","mcp__github__create_pull_request","mcp__github__create_repository","mcp__github__delete_file","mcp__github__delete_pending_pull_request_review","mcp__github__delete_workflow_run_logs","mcp__github__dismiss_notification","mcp__github__download_workflow_run_artifact","mcp__github__fork_repository","mcp__github__get_code_scanning_alert","mcp__github__get_commit","mcp__github__get_dependabot_alert","mcp__github__get_discussion","mcp__github__get_discussion_comments","mcp__github__get_file_contents","mcp__github__get_global_security_advisory","mcp__github__get_issue","mcp__github__get_issue_comments","mcp__github__get_job_logs","mcp__github__get_latest_release","mcp__github__get_me","mcp__github__get_notification_details","mcp__github__get_pull_request","mcp__github__get_pull_request_comments","mcp__github__get_pull_request_diff","mcp__github__get_pull_request_files","mcp__github__get_pull_request_reviews","mcp__github__get_pull_request_status","mcp__github__get_release_by_tag","mcp__github__get_secret_scanning_alert","mcp__github__get_tag","mcp__github__get_team_members","mcp__github__get_teams","mcp__github__get_workflow_run","mcp__github__get_workflow_run_logs","mcp__github__get_workflow_run_usage","mcp__github__list_branches","mcp__github__list_code_scanning_alerts","mcp__github__list_commits","mcp__github__list_dependabot_alerts","mcp__github__list_discussion_categories","mcp__github__list_discussions","mcp__github__list_gists","mcp__github__list_global_security_advisories","mcp__github__list_issue_types","mcp__github__list_issues","mcp__github__list_notifications","mcp__github__list_org_repository_security_advisories","mcp__github__list_pull_requests","mcp__github__list_releases","mcp__github__list_repository_security_advisories","mcp__github__list_secret_scanning_alerts","mcp__github__list_sub_issues","mcp__github__list_tags","mcp__github__list_workflow_jobs","mcp__github__list_workflow_run_artifacts","mcp__github__list_workflow_runs","mcp__github__list_workflows","mcp__github__manage_notification_subscription","mcp__github__manage_repository_notification_subscription","mcp__github__mark_all_notifications_read","mcp__github__merge_pull_request","mcp__github__push_files","mcp__github__remove_sub_issue","mcp__github__reprioritize_sub_issue","mcp__github__request_copilot_review","mcp__github__rerun_failed_jobs","mcp__github__rerun_workflow_run","mcp__github__run_workflow","mcp__github__search_code","mcp__github__search_issues","mcp__github__search_orgs","mcp__github__search_pull_requests","mcp__github__search_repositories","mcp__github__search_users","mcp__github__submit_pending_pull_request_review","mcp__github__update_gist","mcp__github__update_issue","mcp__github__update_pull_request","mcp__github__update_pull_request_branch","ListMcpResourcesTool","ReadMcpResourceTool"],"mcp_servers":[{"name":"github","status":"connected"},{"name":"safeoutputs","status":"failed"}],"model":"claude-sonnet-4-20250514","permissionMode":"default","slash_commands":["add-dir","agents","clear","compact","config","cost","doctor","exit","help","ide","init","install-github-app","mcp","memory","migrate-installer","model","pr-comments","release-notes","resume","status","statusline","bug","review","security-review","upgrade","vim","permissions","hooks","export","logout","login","bashes","mcp__github__AssignCodingAgent","mcp__github__IssueToFixWorkflow"],"apiKeySource":"ANTHROPIC_API_KEY"}
{"type":"assistant","message":{"id":"msg_01McAF6aPVtSGqp4RFYp2ugV","type":"message","role":"assistant","model":"claude-sonnet-4-20250514","content":[{"type":"text","text":"I cannot call a ` + "`draw pelican`" + ` tool or any other missing tool, as I don't have access to tools that don't exist in my available toolset. Additionally, I don't have a ` + "`missing-tool`" + ` tool available to report this missing functionality.\\n\\nThe tools I have access to are the standard file system operations (Read, Write, Edit, etc.), GitHub API tools (mcp__github__*), search tools (Grep, Glob), and other development utilities listed in my function definitions.\\n\\nIf you need to test the missing-tool safe output functionality as described in the workflow, you would need to run the actual GitHub Actions workflow that contains the custom engine implementation shown in the markdown file."}],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":3,"cache_creation_input_tokens":39481,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":39481,"ephemeral_1h_input_tokens":0},"output_tokens":2,"service_tier":"standard"}},"parent_tool_use_id":null,"session_id":"d5d6d3c4-b04d-4b41-ba8e-6de4e93648bb"}
{"type":"result","subtype":"success","is_error":false,"duration_ms":7173,"duration_api_ms":8795,"num_turns":1,"result":"I cannot call a ` + "`draw pelican`" + ` tool or any other missing tool, as I don't have access to tools that don't exist in my available toolset. Additionally, I don't have a ` + "`missing-tool`" + ` tool available to report this missing functionality.\\n\\nThe tools I have access to are the standard file system operations (Read, Write, Edit, etc.), GitHub API tools (mcp__github__*), search tools (Grep, Glob), and other development utilities listed in my function definitions.\\n\\nIf you need to test the missing-tool safe output functionality as described in the workflow, you would need to run the actual GitHub Actions workflow that contains the custom engine implementation shown in the markdown file.","session_id":"d5d6d3c4-b04d-4b41-ba8e-6de4e93648bb","total_cost_usd":0.2987599,"usage":{"input_tokens":6,"cache_creation_input_tokens":78962,"cache_read_input_tokens":0,"output_tokens":152,"server_tool_use":{"web_search_requests":0},"service_tier":"standard"},"permission_denials":[]}`

	// Create a temporary directory structure
	tmpDir := testutil.TempDir(t, "test-*")
	runDir := filepath.Join(tmpDir, "run-17701181429")
	err := os.MkdirAll(runDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create run directory: %v", err)
	}

	// Create the log file
	logFile := filepath.Join(runDir, "test-safe-output-missing-tool.log")
	err = os.WriteFile(logFile, []byte(logContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write log file: %v", err)
	}

	// Test run information
	run := WorkflowRun{
		DatabaseID:   17701181429,
		WorkflowName: "Test Safe Output - Missing Tool",
	}

	// Test the extraction function
	failures, err := extractMCPFailuresFromRun(runDir, run, true)
	if err != nil {
		t.Fatalf("Failed to extract MCP failures: %v", err)
	}

	// Verify that we detected the safe_outputs server failure
	if len(failures) == 0 {
		t.Fatal("Expected to find MCP failures, but found none")
	}

	if len(failures) != 1 {
		t.Fatalf("Expected to find 1 MCP failure, but found %d", len(failures))
	}

	failure := failures[0]
	if failure.ServerName != "safeoutputs" {
		t.Errorf("Expected server name 'safeoutputs', got '%s'", failure.ServerName)
	}

	if failure.Status != "failed" {
		t.Errorf("Expected status 'failed', got '%s'", failure.Status)
	}

	if failure.WorkflowName != "Test Safe Output - Missing Tool" {
		t.Errorf("Expected workflow name 'Test Safe Output - Missing Tool', got '%s'", failure.WorkflowName)
	}

	if failure.RunID != 17701181429 {
		t.Errorf("Expected run ID 17701181429, got %d", failure.RunID)
	}
}

func TestExtractMCPFailuresFromLogFileDirectly(t *testing.T) {
	// Test the direct log file parsing with the exact content
	logContent := `{"type":"system","subtype":"init","cwd":"/home/runner/work/gh-aw/gh-aw","session_id":"d5d6d3c4-b04d-4b41-ba8e-6de4e93648bb","mcp_servers":[{"name":"github","status":"connected"},{"name":"safeoutputs","status":"failed"}],"model":"claude-sonnet-4-20250514"}`

	run := WorkflowRun{
		DatabaseID:   17701181429,
		WorkflowName: "Test Safe Output - Missing Tool",
	}

	// Create a temporary file for this test
	tmpFile := filepath.Join(testutil.TempDir(t, "test-*"), "test.log")
	err := os.WriteFile(tmpFile, []byte(logContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write temporary log file: %v", err)
	}

	failures, err := extractMCPFailuresFromLogFile(tmpFile, run, true)
	if err != nil {
		t.Fatalf("Failed to extract MCP failures from log file: %v", err)
	}

	if len(failures) != 1 {
		t.Fatalf("Expected to find 1 MCP failure, but found %d", len(failures))
	}

	failure := failures[0]
	if failure.ServerName != "safeoutputs" {
		t.Errorf("Expected server name 'safeoutputs', got '%s'", failure.ServerName)
	}

	if failure.Status != "failed" {
		t.Errorf("Expected status 'failed', got '%s'", failure.Status)
	}
}
