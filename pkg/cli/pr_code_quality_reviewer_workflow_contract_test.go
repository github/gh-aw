//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// explicitMainAgentModelPattern matches a top-level, provider-qualified model
// declaration (for example "model: openai/gpt-5.4") in the workflow frontmatter.
var explicitMainAgentModelPattern = regexp.MustCompile(`(?m)^model: \S+/\S+$`)

func TestPRCodeQualityReviewerWorkflowSubAgentModelContract(t *testing.T) {
	t.Parallel()
	repoRoot, err := gitutil.FindGitRoot()
	if err != nil {
		t.Skipf("Skipping test: not in a git repository: %v", err)
	}

	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "pr-code-quality-reviewer.md")
	content, err := os.ReadFile(workflowPath)
	require.NoError(t, err, "Should read pr-code-quality-reviewer workflow")

	text := string(content)
	assert.Regexp(t, explicitMainAgentModelPattern, text, "Main agent should use an explicit provider-qualified model")
	assert.Contains(t, text, "## agent: `grumpy-coder`", "Workflow should define the grumpy-coder sub-agent")
	assert.Contains(t, text, "model: small", "Sub-agent should use the portable small alias")
	assert.NotContains(t, text, "model: inherited", "Sub-agent should not inherit an unsupported tier-specific model")
}

func TestPRCodeQualityReviewerWorkflowPublicationContract(t *testing.T) {
	t.Parallel()
	repoRoot, err := gitutil.FindGitRoot()
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "pr-code-quality-reviewer.md"))
	require.NoError(t, err)

	text := string(content)
	_, publication, ok := strings.Cut(text, "## skill: `review-publication`")
	require.True(t, ok, "Publication safeguards must live in the extracted skill")
	assert.Contains(t, publication, "Keep publication commands stdout-only")
	assert.Contains(t, publication, "Do not write temporary payload files, use shell redirection or heredocs")
	assert.Contains(t, publication, "Send each safe-output command separately from analysis commands")
	assert.Contains(t, publication, "without widening permissions")
	assert.Contains(t, publication, "then emit `report_incomplete` with the concrete blocker")
	assert.Contains(t, publication, "do not call `noop` or claim publication")
	assert.Contains(t, publication, `| safeoutputs create_pull_request_review_comment .`)
	assert.Contains(t, publication, `| safeoutputs submit_pull_request_review .`)
	assert.Contains(t, publication, "Make at most one corrected `printf` pipeline attempt")
	assert.Contains(t, publication, "If an inline-comment publication fails, stop publishing further comments and follow the same recovery path")
	assert.Contains(t, publication, "If no transport can run, state that no terminal safe output was emitted")
	assert.Contains(t, publication, "distinguish an unavailable CLI from an unallowed command or file write")
	assert.Contains(t, publication, "Encode every apostrophe in JSON string values as `\\u0027` before shell single-quoting")
	assert.Contains(t, publication, "never use shell double quotes")
	assert.Contains(t, publication, "Escape control characters as `\\n`, `\\t`, `\\r`, or `\\u00XX`")
	assert.NotContains(t, text, "name the exact allowlist entry needed: `tools.bash: - safeoutputs`")
}

func TestPRCodeQualityReviewerWorkflowToolContract(t *testing.T) {
	t.Parallel()
	repoRoot, err := gitutil.FindGitRoot()
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "pr-code-quality-reviewer.md"))
	require.NoError(t, err)

	text := string(content)
	mainAndSpecialist, publication, ok := strings.Cut(text, "## skill: `review-publication`")
	require.True(t, ok)
	assert.NotContains(t, text, "command -v safeoutputs", "Publication must not depend on an unallowed availability probe")
	assert.NotContains(t, text, "    - command:*\n")
	assert.Contains(t, publication, "Do not prepend availability probes or other commands")
	assert.Contains(t, mainAndSpecialist, "run exactly `wc -c /tmp/gh-aw/agent/pr-review-comments.json`")
	assert.Contains(t, mainAndSpecialist, "Use `glob` to check whether")
	assert.Contains(t, mainAndSpecialist, "do not execute repository code, tests, package managers, or build commands")
}

func TestPRCodeQualityReviewerWorkflowSafeOutputConsistency(t *testing.T) {
	t.Parallel()
	repoRoot, err := gitutil.FindGitRoot()
	require.NoError(t, err)
	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "pr-code-quality-reviewer")
	source, err := os.ReadFile(workflowPath + ".md")
	require.NoError(t, err)
	lock, err := os.ReadFile(workflowPath + ".lock.yml")
	require.NoError(t, err)

	_, toolsBlock, ok := strings.Cut(string(lock), "<safe-output-tools>")
	require.True(t, ok)
	toolsBlock, _, ok = strings.Cut(toolsBlock, "</safe-output-tools>")
	require.True(t, ok)
	toolsLine := regexp.MustCompile(`(?m)^\s*Tools: ([^\n]+)`).FindStringSubmatch(toolsBlock)
	require.Len(t, toolsLine, 2)
	var promptTools []string
	for tool := range strings.SplitSeq(toolsLine[1], ", ") {
		name, _, _ := strings.Cut(tool, "(")
		promptTools = append(promptTools, name)
	}

	manifest, err := workflow.ExtractGHAWManifestFromLockFile(string(lock))
	require.NoError(t, err)
	require.NotNil(t, manifest)
	var manifestTools []string
	for _, server := range manifest.MCPServers {
		if server.Name == "safeoutputs" {
			manifestTools = server.Tools
		}
	}
	require.NotEmpty(t, manifestTools)
	assert.ElementsMatch(t, manifestTools, promptTools, "Manifest and prompt must grant the same safe-output tools")

	_, publication, ok := strings.Cut(string(source), "## skill: `review-publication`")
	require.True(t, ok)
	invocationPattern := regexp.MustCompile("(?:safeoutputs[ \\t]+|(?:emit|call)[ \\t]+`)([a-z][a-z0-9_-]*)")
	invocations := invocationPattern.FindAllStringSubmatch(publication, -1)
	require.NotEmpty(t, invocations)
	hyphenatedInvocation := invocationPattern.FindStringSubmatch("safeoutputs create-pull-request-review-comment")
	require.Len(t, hyphenatedInvocation, 2)
	for _, invocation := range append(invocations, hyphenatedInvocation) {
		name := strings.ReplaceAll(invocation[1], "-", "_")
		assert.Contains(t, promptTools, name, "Reviewer instruction references an ungranted safe-output tool")
	}
}

func TestPRCodeQualityReviewerWorkflowReviewSequencingContract(t *testing.T) {
	t.Parallel()
	repoRoot, err := gitutil.FindGitRoot()
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "pr-code-quality-reviewer.md"))
	require.NoError(t, err)
	text := string(content)
	mainAndSpecialist, publication, ok := strings.Cut(text, "## skill: `review-publication`")
	require.True(t, ok)
	main, specialist, ok := strings.Cut(mainAndSpecialist, "## agent: `grumpy-coder`")
	require.True(t, ok)

	// A pending specialist must be joined or explicitly excluded before any writes.
	_, adjudication, ok := strings.Cut(main, "### Step 3: Judge Agent-to-Agent Findings")
	require.True(t, ok)
	barrier, publishStep, ok := strings.Cut(adjudication, "### Step 4: Publish the Review")
	require.True(t, ok)
	assert.Contains(t, barrier, "use a bounded wait for completion")
	assert.Contains(t, barrier, "ending no later than 10 minutes elapsed")
	assert.Contains(t, barrier, "`completed`")
	assert.Contains(t, barrier, "`unavailable`")
	assert.Contains(t, barrier, "Empty output means no specialist findings only after completion")
	assert.Contains(t, barrier, "launch/wait fails, output is invalid, or the 10-minute deadline expires")
	assert.Contains(t, barrier, "note the concrete limitation in the overall review")
	assert.Contains(t, barrier, "Do not enter Step 4 or Step 5 until")
	assert.Contains(t, barrier, "freeze the final finding set. Then use the `review-publication` skill to publish the inline comments and one overall review")
	assert.Contains(t, barrier, "Late specialist output must not reopen adjudication")
	assert.NotContains(t, text, "if it has not yet responded, continue without it")
	assert.NotContains(t, text, "do not block if it has not responded")

	publishStep, _, ok = strings.Cut(publishStep, "### Step 6: Update PR Continuity Memory")
	require.True(t, ok)
	assert.Contains(t, publishStep, "Publish only the frozen findings and overall verdict using the `review-publication` skill")
	assert.Contains(t, publishStep, "Do not continue analysis during publication")
	assert.Contains(t, publishStep, "if it is blocked, follow the skill's bounded recovery path")

	_, comments, ok := strings.Cut(publication, "### Write Review Comments")
	require.True(t, ok)
	comments, submission, ok := strings.Cut(comments, "### Submit the Overall Review")
	require.True(t, ok)
	assert.Contains(t, comments, "Publish only the frozen, adjudicated findings")
	assert.Contains(t, comments, "before the single overall review")
	assert.Contains(t, comments, "Do not start new analysis or collect specialist output during publication")
	assert.Contains(t, submission, "Submit exactly one overall review after inline comments")
	assert.Contains(t, submission, "`submit_pull_request_review` is write-once (`max: 1`)")
	assert.Contains(t, submission, "never submit a provisional or second review")
	assert.Contains(t, submission, "Always submit before the 15-minute timeout, including when there are zero findings")
	assert.Contains(t, submission, "close any unfinished specialist pass as `unavailable`, freeze the verdict, publish inline comments, and submit once")
	assert.Contains(t, submission, "Do not bypass the completion barrier")
	assert.Contains(t, submission, "After successful submission, publication is closed")
	assert.Contains(t, submission, "do not add corrective comments, submit another review")
	assert.Contains(t, submission, "or emit `report_incomplete` because of late specialist output")

	assert.Contains(t, main, "submit-pull-request-review:\n    max: 1\n    allowed-events: [COMMENT, REQUEST_CHANGES]")
	assert.Contains(t, specialist, "do not publish comments, submit reviews, or invoke safe outputs")
}
