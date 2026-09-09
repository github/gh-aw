package workflow

import (
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/setutil"
)

// commentOutProcessedFieldsInOnSection comments out draft, max-stack, fork, forks, names, labels, manual-approval, cooldown, stop-after, skip-if-match, skip-if-no-match, skip-roles, reaction, lock-for-agent, steps, permissions, needs, restore-memory, and stale-check fields in the on section
// These fields are processed separately and should be commented for documentation
// Exception: names fields in sections with __gh_aw_native_label_filter__ marker in frontmatter are NOT commented out
func (c *Compiler) commentOutProcessedFieldsInOnSection(yamlStr string, frontmatter map[string]any) string {
	frontmatterLog.Print("Processing 'on' section to comment out processed fields")

	nativeLabelFilterSections := collectNativeLabelFilterSections(frontmatter)
	state := newOnSectionCleanupState()
	lines := strings.Split(yamlStr, "\n")
	result := make([]string, 0, len(lines))

	for _, line := range lines {
		info := newOnSectionLine(line)
		if state.handleEventSectionEntry(info, &result) {
			continue
		}
		state.leaveEventSections(info)
		if state.shouldSkipNativeLabelFilterMarker(info) {
			continue
		}
		state.enterSubsections(info)
		state.leaveSubsections(info)
		shouldComment, commentReason := state.determineComment(info, result, nativeLabelFilterSections)
		result = state.appendLine(result, info, shouldComment, commentReason)
	}

	result = dedentTrailingOnCommentBlock(result)
	frontmatterLog.Printf("Processed 'on' section: %d input lines, %d native label filter section(s)", len(lines), len(nativeLabelFilterSections))
	return strings.Join(result, "\n")
}

type onSectionLine struct {
	raw     string
	trimmed string
	indent  int
}

func newOnSectionLine(raw string) onSectionLine {
	return onSectionLine{
		raw:     raw,
		trimmed: strings.TrimSpace(raw),
		indent:  len(raw) - len(strings.TrimLeft(raw, " 	")),
	}
}

type onSectionCleanupState struct {
	inPullRequest                bool
	inPullRequestReview          bool
	inIssues                     bool
	inDiscussion                 bool
	inIssueComment               bool
	inDeploymentStatus           bool
	inWorkflowRun                bool
	inWorkflowRunConclusionArray bool
	inForksArray                 bool
	inSkipIfMatch                bool
	inSkipIfNoMatch              bool
	inSkipIfCheckFailing         bool
	inSkipAuthorAssociations     bool
	inSkipRolesArray             bool
	inSkipBotsArray              bool
	inRolesArray                 bool
	inBotsArray                  bool
	inLabelsArray                bool
	inNeedsArray                 bool
	inGitHubApp                  bool
	inOnSteps                    bool
	inOnPermissions              bool
	commentBlockIndent           string
	inCommentBlock               bool
	currentSection               string
	currentSectionIndent         int
	deploymentStatusIndent       int
	workflowRunIndent            int
}

func newOnSectionCleanupState() *onSectionCleanupState {
	return &onSectionCleanupState{
		currentSectionIndent:   -1,
		deploymentStatusIndent: -1,
		workflowRunIndent:      -1,
	}
}

func collectNativeLabelFilterSections(frontmatter map[string]any) map[string]struct{} {
	sections := make(map[string]struct{})
	onValue, exists := frontmatter["on"]
	if !exists {
		return sections
	}
	onMap, ok := onValue.(map[string]any)
	if !ok {
		return sections
	}
	for _, sectionKey := range []string{"issues", "pull_request", "discussion", "issue_comment"} {
		sectionValue, hasSection := onMap[sectionKey]
		if !hasSection {
			continue
		}
		sectionMap, ok := sectionValue.(map[string]any)
		if !ok {
			continue
		}
		marker, hasMarker := sectionMap["__gh_aw_native_label_filter__"]
		useNative, ok := marker.(bool)
		if hasMarker && ok && useNative {
			sections[sectionKey] = struct{}{}
			frontmatterLog.Printf("Section %s uses native label filtering", sectionKey)
		}
	}
	return sections
}

func (s *onSectionCleanupState) inEventSection() bool {
	return s.inPullRequest || s.inPullRequestReview || s.inIssues || s.inDiscussion || s.inIssueComment
}

func (s *onSectionCleanupState) handleEventSectionEntry(info onSectionLine, result *[]string) bool {
	section, ok := s.detectEventSection(info)
	if !ok {
		return false
	}
	s.activateEventSection(section, info.indent)
	*result = append(*result, info.raw)
	return true
}

func (s *onSectionCleanupState) detectEventSection(info onSectionLine) (string, bool) {
	if s.inOnPermissions || s.inOnSteps || s.inSkipAuthorAssociations {
		return "", false
	}
	if info.indent != 2 && info.indent != 4 {
		return "", false
	}
	switch info.trimmed {
	case "pull_request:", "pull_request_review:", "issues:", "discussion:", "issue_comment:", "deployment_status:", "workflow_run:":
		return strings.TrimSuffix(info.trimmed, ":"), true
	default:
		return "", false
	}
}

func (s *onSectionCleanupState) activateEventSection(section string, indent int) {
	frontmatterLog.Printf("Entering event section %q at indent %d", section, indent)
	s.resetTopLevelExtensionState()
	s.inCommentBlock = false
	s.commentBlockIndent = ""
	s.inPullRequest = section == "pull_request"
	s.inPullRequestReview = section == "pull_request_review"
	s.inIssues = section == "issues"
	s.inDiscussion = section == "discussion"
	s.inIssueComment = section == "issue_comment"
	s.inDeploymentStatus = section == "deployment_status"
	s.inWorkflowRun = section == "workflow_run"
	s.inWorkflowRunConclusionArray = false
	s.inForksArray = false
	s.currentSection, s.currentSectionIndent = "", -1
	if s.inEventSection() {
		s.currentSection, s.currentSectionIndent = section, indent
	}
	s.deploymentStatusIndent = -1
	if section == "deployment_status" {
		s.deploymentStatusIndent = indent
	}
	s.workflowRunIndent = -1
	if section == "workflow_run" {
		s.workflowRunIndent = indent
	}
}

func (s *onSectionCleanupState) resetTopLevelExtensionState() {
	s.inSkipRolesArray = false
	s.inSkipBotsArray = false
	s.inRolesArray = false
	s.inBotsArray = false
	s.inLabelsArray = false
	s.inNeedsArray = false
	s.inSkipIfMatch = false
	s.inSkipIfNoMatch = false
	s.inSkipIfCheckFailing = false
	s.inSkipAuthorAssociations = false
}

func (s *onSectionCleanupState) leaveEventSections(info onSectionLine) {
	s.leaveCurrentEventSection(info)
	s.leaveDeploymentStatusSection(info)
	s.leaveWorkflowRunSection(info)
}

func (s *onSectionCleanupState) leaveCurrentEventSection(info onSectionLine) {
	if !s.inEventSection() || info.trimmed == "" || strings.HasPrefix(info.trimmed, "#") {
		return
	}
	if s.currentSectionIndent >= 0 && info.indent <= s.currentSectionIndent {
		s.inPullRequest = false
		s.inPullRequestReview = false
		s.inIssues = false
		s.inDiscussion = false
		s.inIssueComment = false
		s.inForksArray = false
		s.currentSection = ""
		s.currentSectionIndent = -1
	}
}

func (s *onSectionCleanupState) leaveDeploymentStatusSection(info onSectionLine) {
	if !s.inDeploymentStatus || info.trimmed == "" || strings.HasPrefix(info.trimmed, "#") {
		return
	}
	if s.deploymentStatusIndent >= 0 && info.indent <= s.deploymentStatusIndent {
		s.inDeploymentStatus = false
		s.deploymentStatusIndent = -1
	}
}

func (s *onSectionCleanupState) leaveWorkflowRunSection(info onSectionLine) {
	if !s.inWorkflowRun || info.trimmed == "" || strings.HasPrefix(info.trimmed, "#") {
		return
	}
	if s.workflowRunIndent >= 0 && info.indent <= s.workflowRunIndent {
		s.inWorkflowRun = false
		s.inWorkflowRunConclusionArray = false
		s.workflowRunIndent = -1
	}
}

func (s *onSectionCleanupState) shouldSkipNativeLabelFilterMarker(info onSectionLine) bool {
	return s.inEventSection() && strings.Contains(info.trimmed, "__gh_aw_native_label_filter__:")
}

func (s *onSectionCleanupState) enterSubsections(info onSectionLine) {
	s.enterArraySections(info)
	s.enterObjectSections(info)
}

func (s *onSectionCleanupState) enterArraySections(info onSectionLine) {
	if s.inPullRequest && strings.HasPrefix(info.trimmed, "forks:") {
		s.inForksArray = true
	}
	if !s.inEventSection() && strings.HasPrefix(info.trimmed, "skip-roles:") {
		s.inSkipRolesArray = true
	}
	if !s.inEventSection() && strings.HasPrefix(info.trimmed, "skip-bots:") {
		s.inSkipBotsArray = true
	}
	if !s.inEventSection() && strings.HasPrefix(info.trimmed, "roles:") {
		s.inRolesArray = true
	}
	if !s.inEventSection() && strings.HasPrefix(info.trimmed, "bots:") {
		s.inBotsArray = true
	}
	if !s.inEventSection() && !s.inOnSteps && !s.inOnPermissions && info.indent == 2 && info.trimmed == "labels:" {
		s.inLabelsArray = true
	}
	if !s.inEventSection() && !s.inOnSteps && !s.inOnPermissions && info.indent == 2 && strings.HasPrefix(info.trimmed, "needs:") {
		s.inNeedsArray = true
	}
	if !s.inEventSection() && strings.HasPrefix(info.trimmed, "steps:") {
		s.inOnSteps = true
	}
	if !s.inEventSection() && !s.inOnPermissions && strings.HasPrefix(info.trimmed, "permissions:") {
		s.inOnPermissions = true
	}
}

func (s *onSectionCleanupState) enterObjectSections(info onSectionLine) {
	if !s.inEventSection() && !s.inSkipIfMatch && ((strings.HasPrefix(info.trimmed, "skip-if-match:") && info.trimmed == "skip-if-match:") ||
		(strings.HasPrefix(info.trimmed, "# skip-if-match:") && strings.Contains(info.trimmed, "pre-activation job"))) {
		s.inSkipIfMatch = true
	}
	if !s.inEventSection() && !s.inSkipIfNoMatch && ((strings.HasPrefix(info.trimmed, "skip-if-no-match:") && info.trimmed == "skip-if-no-match:") ||
		(strings.HasPrefix(info.trimmed, "# skip-if-no-match:") && strings.Contains(info.trimmed, "pre-activation job"))) {
		s.inSkipIfNoMatch = true
	}
	if !s.inEventSection() && !s.inSkipIfCheckFailing && (info.trimmed == "skip-if-check-failing:" ||
		(strings.HasPrefix(info.trimmed, "# skip-if-check-failing:") && strings.Contains(info.trimmed, "pre-activation job"))) {
		s.inSkipIfCheckFailing = true
	}
	if !s.inEventSection() && !s.inSkipAuthorAssociations && strings.HasPrefix(info.trimmed, "skip-author-associations:") && info.trimmed == "skip-author-associations:" {
		s.inSkipAuthorAssociations = true
	}
	if !s.inEventSection() && !s.inGitHubApp && ((strings.HasPrefix(info.trimmed, "github-app:") && info.trimmed == "github-app:") ||
		(strings.HasPrefix(info.trimmed, "# github-app:") && strings.Contains(info.trimmed, "pre-activation job"))) {
		s.inGitHubApp = true
	}
}

func (s *onSectionCleanupState) leaveSubsections(info onSectionLine) {
	s.leaveObjectSections(info)
	s.leaveArraySections(info)
	s.leaveStepsAndPermissions(info)
}

func (s *onSectionCleanupState) leaveObjectSections(info onSectionLine) {
	if s.inSkipIfMatch && isLeavingTopLevelObject(info, "skip-if-match:", "# skip-if-match:") {
		s.inSkipIfMatch = false
	}
	if s.inSkipIfNoMatch && isLeavingTopLevelObject(info, "skip-if-no-match:", "# skip-if-no-match:") {
		s.inSkipIfNoMatch = false
	}
	if s.inSkipIfCheckFailing && isLeavingTopLevelObject(info, "skip-if-check-failing:", "# skip-if-check-failing:") {
		s.inSkipIfCheckFailing = false
	}
	if s.inSkipAuthorAssociations && isLeavingTopLevelObject(info, "skip-author-associations:", "# skip-author-associations:") {
		s.inSkipAuthorAssociations = false
	}
	if s.inGitHubApp && isLeavingTopLevelObject(info, "github-app:", "# github-app:") {
		s.inGitHubApp = false
	}
}

func (s *onSectionCleanupState) leaveArraySections(info onSectionLine) {
	if s.inForksArray && s.inPullRequest && isLeavingArray(info, "forks:", 4) {
		s.inForksArray = false
	}
	if s.inSkipRolesArray && isLeavingArray(info, "skip-roles:", 2) {
		s.inSkipRolesArray = false
	}
	if s.inSkipBotsArray && isLeavingArray(info, "skip-bots:", 2) {
		s.inSkipBotsArray = false
	}
	if s.inRolesArray && isLeavingArray(info, "roles:", 2) {
		s.inRolesArray = false
	}
	if s.inBotsArray && isLeavingArray(info, "bots:", 2) {
		s.inBotsArray = false
	}
	if s.inLabelsArray && isLeavingArray(info, "labels:", 2) {
		s.inLabelsArray = false
	}
	if s.inNeedsArray && isLeavingArray(info, "needs:", 2) {
		s.inNeedsArray = false
	}
}

func (s *onSectionCleanupState) leaveStepsAndPermissions(info onSectionLine) {
	if s.inOnSteps && isLeavingArray(info, "steps:", 2) {
		s.inOnSteps = false
	}
	if s.inOnPermissions && isLeavingTopLevelObject(info, "permissions:", "# permissions:") {
		s.inOnPermissions = false
	}
}

func isLeavingTopLevelObject(info onSectionLine, key, commentedKey string) bool {
	return info.trimmed != "" &&
		!strings.HasPrefix(info.trimmed, key) &&
		!strings.HasPrefix(info.trimmed, commentedKey) &&
		info.indent == 2 &&
		!strings.HasPrefix(info.trimmed, "#")
}

func isLeavingArray(info onSectionLine, key string, indent int) bool {
	return info.trimmed != "" &&
		info.indent == indent &&
		!strings.HasPrefix(info.trimmed, "-") &&
		!strings.HasPrefix(info.trimmed, key) &&
		!strings.HasPrefix(info.trimmed, "#")
}

func (s *onSectionCleanupState) determineComment(info onSectionLine, result []string, native map[string]struct{}) (bool, string) {
	if !s.inEventSection() {
		if shouldComment, reason := s.determineTopLevelComment(info); shouldComment {
			return true, reason
		}
	}
	return s.determineEventSectionComment(info, result, native)
}

func (s *onSectionCleanupState) determineTopLevelComment(info onSectionLine) (bool, string) {
	if s.inEventSection() {
		return false, ""
	}
	for _, fn := range []func(onSectionLine) (bool, string){
		s.commentSimpleTopLevelField,
		s.commentConditionalObjectField,
		s.commentRoleAndLabelField,
		s.commentStepPermissionAndAppField,
	} {
		if shouldComment, reason := fn(info); shouldComment {
			return true, reason
		}
	}
	return false, ""
}

func (s *onSectionCleanupState) commentSimpleTopLevelField(info onSectionLine) (bool, string) {
	switch {
	case strings.HasPrefix(info.trimmed, "manual-approval:"):
		return true, " # Manual approval processed as environment field in activation job"
	case strings.HasPrefix(info.trimmed, "cooldown:"):
		return true, " # Cooldown processed as run history check in pre-activation job"
	case strings.HasPrefix(info.trimmed, "stop-after:"):
		return true, " # Stop-after processed as stop-time check in pre-activation job"
	case strings.HasPrefix(info.trimmed, "restore-memory:"):
		return true, " # Restore-memory enables pre-activation memory restore"
	case strings.HasPrefix(info.trimmed, "reaction:"):
		return true, " # Reaction processed as activation job step"
	case strings.HasPrefix(info.trimmed, "github-token:"):
		return true, " # GitHub token used for reactions and status comments in activation"
	case strings.HasPrefix(info.trimmed, "stale-check:"):
		return true, " # Stale-check processed as frontmatter hash check step in activation job"
	default:
		return false, ""
	}
}

func (s *onSectionCleanupState) commentConditionalObjectField(info onSectionLine) (bool, string) {
	switch {
	case strings.HasPrefix(info.trimmed, "skip-if-match:"):
		return true, " # Skip-if-match processed as search check in pre-activation job"
	case s.inSkipIfMatch && hasAnyPrefixInLine(info.trimmed, "query:", "max:", "scope:"):
		return true, ""
	case strings.HasPrefix(info.trimmed, "skip-if-no-match:"):
		return true, " # Skip-if-no-match processed as search check in pre-activation job"
	case s.inSkipIfNoMatch && hasAnyPrefixInLine(info.trimmed, "query:", "min:", "scope:"):
		return true, ""
	case strings.HasPrefix(info.trimmed, "skip-if-check-failing:"):
		return true, " # Skip-if-check-failing processed as check status gate in pre-activation job"
	case s.inSkipIfCheckFailing && (hasAnyPrefixInLine(info.trimmed, "include:", "exclude:", "branch:", "allow-pending:") || strings.HasPrefix(info.trimmed, "-")):
		return true, ""
	case strings.HasPrefix(info.trimmed, "skip-author-associations:"):
		return true, " # Skip-author-associations compiled into pre-activation job if condition"
	case s.inSkipAuthorAssociations && info.indent > 2:
		return true, ""
	default:
		return false, ""
	}
}

func (s *onSectionCleanupState) commentRoleAndLabelField(info onSectionLine) (bool, string) {
	switch {
	case strings.HasPrefix(info.trimmed, "skip-roles:"):
		return true, " # Skip-roles processed as role check in pre-activation job"
	case s.inSkipRolesArray && strings.HasPrefix(info.trimmed, "-"):
		return true, " # Skip-roles processed as role check in pre-activation job"
	case strings.HasPrefix(info.trimmed, "skip-bots:"):
		return true, " # Skip-bots processed as bot check in pre-activation job"
	case s.inSkipBotsArray && strings.HasPrefix(info.trimmed, "-"):
		return true, " # Skip-bots processed as bot check in pre-activation job"
	case strings.HasPrefix(info.trimmed, "roles:"):
		return true, " # Roles processed as role check in pre-activation job"
	case s.inRolesArray && strings.HasPrefix(info.trimmed, "-"):
		return true, " # Roles processed as role check in pre-activation job"
	case strings.HasPrefix(info.trimmed, "bots:"):
		return true, " # Bots processed as bot check in pre-activation job"
	case s.inBotsArray && strings.HasPrefix(info.trimmed, "-"):
		return true, " # Bots processed as bot check in pre-activation job"
	default:
		return s.commentLabelAndNeedsField(info)
	}
}

func (s *onSectionCleanupState) commentLabelAndNeedsField(info onSectionLine) (bool, string) {
	switch {
	case !s.inOnSteps && !s.inOnPermissions && info.indent == 2 && strings.HasPrefix(info.trimmed, "labels:"):
		return true, " # Label filtering applied via job conditions"
	case s.inLabelsArray && strings.HasPrefix(info.trimmed, "-"):
		return true, " # Label filtering applied via job conditions"
	case !s.inOnSteps && !s.inOnPermissions && info.indent == 2 && strings.HasPrefix(info.trimmed, "needs:"):
		return true, " # Needs processed as dependency in pre-activation job"
	case s.inNeedsArray && strings.HasPrefix(info.trimmed, "-"):
		return true, " # Needs processed as dependency in pre-activation job"
	default:
		return false, ""
	}
}

func (s *onSectionCleanupState) commentStepPermissionAndAppField(info onSectionLine) (bool, string) {
	switch {
	case strings.HasPrefix(info.trimmed, "steps:"):
		return true, " # Steps injected into pre-activation job"
	case s.inOnSteps:
		return true, ""
	case strings.HasPrefix(info.trimmed, "permissions:"):
		return true, " # Permissions applied to pre-activation job"
	case s.inOnPermissions:
		return true, ""
	case strings.HasPrefix(info.trimmed, "github-app:"):
		return true, " # GitHub App used to mint token for reactions and status comments in activation"
	case s.inGitHubApp && isGitHubAppNestedField(info.trimmed):
		return true, ""
	default:
		return false, ""
	}
}

func hasAnyPrefixInLine(line string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func (s *onSectionCleanupState) determineEventSectionComment(info onSectionLine, result []string, native map[string]struct{}) (bool, string) {
	if shouldComment, reason := s.commentPullRequestAndTriggerField(info); shouldComment {
		return true, reason
	}
	if s.inWorkflowRun && !strings.HasPrefix(info.trimmed, "-") && strings.Contains(info.trimmed, ":") && !strings.HasPrefix(info.trimmed, "conclusion:") {
		s.inWorkflowRunConclusionArray = false
	}
	if s.inEventSection() {
		return s.commentLabelFilteringEventField(info, result, native)
	}
	return false, ""
}

func (s *onSectionCleanupState) commentPullRequestAndTriggerField(info onSectionLine) (bool, string) {
	switch {
	case s.inPullRequest && strings.Contains(info.trimmed, "draft:"):
		return true, " # Draft filtering applied via job conditions"
	case (s.inPullRequest || s.inPullRequestReview) && strings.HasPrefix(info.trimmed, "max-stack:"):
		return true, " # Stack filtering applied via job conditions"
	case s.inPullRequest && strings.HasPrefix(info.trimmed, "forks:"):
		return true, " # Fork filtering applied via job conditions"
	case s.inForksArray && strings.HasPrefix(info.trimmed, "-"):
		return true, " # Fork filtering applied via job conditions"
	case s.inDeploymentStatus && strings.HasPrefix(info.trimmed, "state:"):
		return true, " # State filtering compiled into if condition"
	case s.inDeploymentStatus && strings.HasPrefix(info.trimmed, "-"):
		return true, " # State filtering compiled into if condition"
	case s.inWorkflowRun && strings.HasPrefix(info.trimmed, "conclusion:"):
		s.inWorkflowRunConclusionArray = true
		return true, " # Conclusion filtering compiled into if condition"
	case s.inWorkflowRunConclusionArray && strings.HasPrefix(info.trimmed, "-"):
		return true, " # Conclusion filtering compiled into if condition"
	default:
		return false, ""
	}
}

func (s *onSectionCleanupState) commentLabelFilteringEventField(info onSectionLine, result []string, native map[string]struct{}) (bool, string) {
	if strings.HasPrefix(info.trimmed, "lock-for-agent:") {
		return true, " # Lock-for-agent processed as issue locking in activation job"
	}
	if strings.HasPrefix(info.trimmed, "names:") {
		if !setutil.Contains(native, s.currentSection) {
			return true, " # Label filtering applied via job conditions"
		}
		return false, ""
	}
	if !setutil.Contains(native, s.currentSection) && shouldCommentNamesArrayItem(result, info.trimmed) {
		return true, " # Label filtering applied via job conditions"
	}
	return false, ""
}

func shouldCommentNamesArrayItem(result []string, trimmedLine string) bool {
	if trimmedLine == "" || !strings.HasPrefix(trimmedLine, "-") {
		return false
	}
	for i := range slices.Backward(result) {
		prevTrimmed := strings.TrimSpace(result[i])
		if prevTrimmed == "" {
			continue
		}
		if strings.Contains(prevTrimmed, "names:") && strings.Contains(prevTrimmed, "# Label filtering") {
			return true
		}
		if !strings.HasPrefix(prevTrimmed, "#") || !strings.Contains(prevTrimmed, "Label filtering") {
			return false
		}
		if strings.HasPrefix(prevTrimmed, "# -") && strings.Contains(prevTrimmed, "Label filtering") {
			continue
		}
		return false
	}
	return false
}

func (s *onSectionCleanupState) appendLine(result []string, info onSectionLine, shouldComment bool, commentReason string) []string {
	if !shouldComment {
		s.inCommentBlock = false
		s.commentBlockIndent = ""
		return append(result, info.raw)
	}
	return append(result, s.renderCommentedLine(info.raw, commentReason))
}

func (s *onSectionCleanupState) renderCommentedLine(line, commentReason string) string {
	trimmed := strings.TrimLeft(line, " 	")
	if s.inForksArray && strings.HasPrefix(trimmed, "-") {
		s.inCommentBlock = false
		s.commentBlockIndent = ""
	}
	if !s.inCommentBlock && trimmed != "" {
		s.commentBlockIndent = ""
		if len(line) > len(trimmed) {
			s.commentBlockIndent = line[:len(line)-len(trimmed)]
		}
		s.inCommentBlock = true
	}
	commentedLine := s.commentBlockIndent + "# " + trimmed + commentReason
	return strings.TrimRight(commentedLine, " 	")
}

// dedentTrailingOnCommentBlock re-indents the final run of commented-out lines at the
// end of the `on:` section to column 0.
//
// A commented-out processed field (e.g. `# roles:` or `# state:`) that is the last
// thing in the `on:` section is followed, in the assembled workflow, by a top-level
// key (`permissions:`, `concurrency:`, …) at column 0. yamllint's comments-indentation
// rule flags a comment whose indentation matches neither the preceding content line nor
// the following one, so a trailing block indented under `on:` (indent 2 or deeper) is
// reported because the next real line sits at column 0. Aligning the trailing block to
// column 0 gives it a matching anchor and clears the warning.
//
// Only the final block is adjusted. Commented blocks with real `on:` content after them
// already have a matching indentation anchor below and are left untouched.
func dedentTrailingOnCommentBlock(lines []string) []string {
	// Locate the last non-blank line; the trailing block must end in a comment.
	last := len(lines) - 1
	for last >= 0 && strings.TrimSpace(lines[last]) == "" {
		last--
	}
	if last < 0 || !strings.HasPrefix(strings.TrimSpace(lines[last]), "#") {
		return lines
	}

	// Walk back over the consecutive comment lines that form the trailing block.
	start := last
	for start-1 >= 0 && strings.HasPrefix(strings.TrimSpace(lines[start-1]), "#") {
		start--
	}

	// The block must sit under the `on:` mapping (i.e. be indented) and be preceded by
	// real content, so a file that is entirely comments is left alone.
	if start == 0 {
		return lines
	}

	// Comments at the direct `on:` children level (≤2 spaces / 1 tab) always dedent
	// to column 0. Comments nested deeper — e.g. `forks:` inside `pull_request:` at
	// 4-space indent — are usually part of a nested event section that still has real
	// sibling content at the same indent (a `types:` line next to a commented
	// `# forks:`), so they keep their indentation to preserve the visual structure of
	// the `on:` block.
	//
	// The exception is a parent key whose children are *entirely* commented out — e.g.
	// a `deployment_status:` whose only descendants are `# state:` / `# - error`. There
	// the block's nearest real content line is the shallower parent key, so the comment
	// aligns with neither that key nor the column-0 top-level key that follows the `on:`
	// section, and yamllint's comments-indentation rule flags it. In that case dedent to
	// column 0 (matching the following content) to clear the warning.
	firstLineIndent := len(lines[start]) - len(strings.TrimLeft(lines[start], " \t"))
	if firstLineIndent > 2 {
		// Find the nearest preceding real (non-blank) content line — the anchor the
		// comment block visually attaches to.
		anchor := start - 1
		for anchor >= 0 && strings.TrimSpace(lines[anchor]) == "" {
			anchor--
		}
		if anchor < 0 {
			return lines
		}
		anchorIndent := len(lines[anchor]) - len(strings.TrimLeft(lines[anchor], " \t"))
		// A sibling at the same (or deeper) indent anchors the block — leave it as-is.
		// Only a strictly shallower anchor (the parent key) needs dedenting.
		if anchorIndent >= firstLineIndent {
			return lines
		}
	}

	frontmatterLog.Printf("Dedenting trailing 'on' comment block: lines %d-%d", start, last)
	for i := start; i <= last; i++ {
		lines[i] = strings.TrimLeft(lines[i], " \t")
	}
	return lines
}

// addZizmorIgnoreForWorkflowRun adds a zizmor ignore comment for workflow_run triggers
// The comment is added after the workflow_run: line to suppress dangerous-triggers warnings
// since the compiler adds proper role and fork validation to secure these triggers
func (c *Compiler) addZizmorIgnoreForWorkflowRun(yamlStr string) string {
	// Check if the YAML contains workflow_run trigger
	if !strings.Contains(yamlStr, "workflow_run:") {
		return yamlStr
	}
	frontmatterLog.Print("Adding zizmor ignore annotation for workflow_run trigger")

	lines := strings.Split(yamlStr, "\n")
	var result []string
	annotationAdded := false // Track if we've already added the annotation

	for _, line := range lines {
		result = append(result, line)

		// Skip if we've already added the annotation (prevents duplicates)
		if annotationAdded {
			continue
		}

		// Check if this is a non-comment workflow_run: key at the correct YAML level
		trimmedLine := strings.TrimSpace(line)

		// Skip if the line is a comment
		if strings.HasPrefix(trimmedLine, "#") {
			continue
		}

		// Match lines that are only 'workflow_run:' (possibly with trailing whitespace or a comment)
		// e.g., 'workflow_run:', 'workflow_run: # comment', '  workflow_run:'
		// But not 'someworkflow_run:', 'workflow_run: value', etc.
		if strings.HasPrefix(trimmedLine, "workflow_run:") {
			after := strings.TrimSpace(trimmedLine[len("workflow_run:"):])
			// Only allow if nothing or only a comment follows
			if after == "" || strings.HasPrefix(after, "#") {
				// Get the indentation of the workflow_run line
				indentation := ""
				if len(line) > len(trimmedLine) {
					indentation = line[:len(line)-len(trimmedLine)]
				}

				// Add zizmor ignore comment with proper indentation
				// The comment explains that the trigger is secured with role and fork validation
				comment := indentation + "  # zizmor: ignore[dangerous-triggers] - workflow_run trigger is secured with role and fork validation"
				result = append(result, comment)
				annotationAdded = true
			}
		}
	}

	return strings.Join(result, "\n")
}

// isGitHubAppNestedField returns true if the trimmed YAML line represents a known
// nested field or array item inside an on.github-app object.
func isGitHubAppNestedField(trimmedLine string) bool {
	githubAppFields := []string{"app-id:", "client-id:", "private-key:", "ignore-if-missing:", "owner:", "repositories:"}
	for _, field := range githubAppFields {
		if strings.HasPrefix(trimmedLine, field) {
			return true
		}
	}
	// Array items (repositories list)
	return strings.HasPrefix(trimmedLine, "-")
}
