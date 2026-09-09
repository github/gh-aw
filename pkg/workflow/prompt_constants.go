package workflow

import (
	_ "embed"

	"github.com/github/gh-aw/pkg/constants"
)

// Prompt file paths at runtime (copied by setup action to ${RUNNER_TEMP}/gh-aw/prompts)
const (
	promptsDir                              = constants.GhAwRootDirShell + "/prompts"
	prContextPromptFile                     = "pr_context_prompt.md"
	prContextPushToPRBranchGuidanceFile     = "pr_context_push_to_pr_branch_guidance.md"
	tempFolderPromptFile                    = "temp_folder_prompt.md"
	playwrightPromptFile                    = "playwright_prompt.md"
	playwrightAWFPromptFile                 = "playwright_awf_prompt.md"
	markdownPromptFile                      = "markdown.md"
	xpiaPromptFile                          = "xpia.md"
	cacheMemoryPromptFile                   = "cache_memory_prompt.md"
	cacheMemoryPromptMultiFile              = "cache_memory_prompt_multi.md"
	repoMemoryPromptFile                    = "repo_memory_prompt.md"
	repoMemoryPromptMultiFile               = "repo_memory_prompt_multi.md"
	safeOutputsPromptFile                   = "safe_outputs_prompt.md"
	safeOutputsCreatePRFile                 = "safe_outputs_create_pull_request.md"
	safeOutputsSteeringIssueFile            = "safe_outputs_steering_issue.md"
	safeOutputsPushToBranchFile             = "safe_outputs_push_to_pr_branch.md"
	safeOutputsCommentMemoryFile            = "safe_outputs_comment_memory.md"
	safeOutputsAutoCreateIssueFile          = "safe_outputs_auto_create_issue.md"
	githubMCPToolsPromptFile                = "github_mcp_tools_prompt.md"
	githubMCPToolsWithSafeOutputsPromptFile = "github_mcp_tools_with_safeoutputs_prompt.md"
	mcpCLIToolsPromptFile                   = "mcp_cli_tools_prompt.md"
	mcpCLIToolsWithSafeOutputsPromptFile    = "mcp_cli_tools_with_safeoutputs_prompt.md"
	cliProxyPromptFile                      = "cli_proxy_prompt.md"
	cliProxyWithSafeOutputsPromptFile       = "cli_proxy_with_safeoutputs_prompt.md"
)

// GitHub context prompt is kept embedded because it contains GitHub Actions expressions
// that need to be extracted at compile time. Moving this to a runtime file would require
// reading and parsing the file during compilation, which is more complex.
//
//go:embed prompts/github_context_prompt.md
var githubContextPromptText string
