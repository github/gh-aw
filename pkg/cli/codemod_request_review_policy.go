package cli

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var requestReviewPolicyCodemodLog = logger.New("cli:codemod_request_review_policy")

func getRequestReviewPolicyCodemod() Codemod {
	return Codemod{
		ID:           "request-review-policy-normalization",
		Name:         "Normalize request_review protected-file policies",
		Description:  "Normalizes the legacy request_review protected-file policy value to request-review in pull-request safe outputs.",
		IntroducedIn: "1.0.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			if !requestReviewPolicyNeedsMigration(frontmatter) {
				return content, false, nil
			}

			newContent, applied, err := applyFrontmatterLineTransform(content, normalizeRequestReviewPolicy)
			if applied {
				requestReviewPolicyCodemodLog.Print("Normalized request_review protected-file policy")
			}
			return newContent, applied, err
		},
	}
}

func requestReviewPolicyNeedsMigration(frontmatter map[string]any) bool {
	safeOutputs, ok := frontmatter["safe-outputs"].(map[string]any)
	if !ok {
		return false
	}

	for _, handlerName := range []string{"create-pull-request", "push-to-pull-request-branch"} {
		handler, ok := safeOutputs[handlerName].(map[string]any)
		if !ok {
			continue
		}
		if policy, ok := handler["protected-files"].(string); ok && policy == "request_review" {
			return true
		}
		if policy, ok := handler["protected-files-policy"].(string); ok && policy == "request_review" {
			return true
		}
		protectedFiles, ok := handler["protected-files"].(map[string]any)
		if ok {
			if policy, ok := protectedFiles["policy"].(string); ok && policy == "request_review" {
				return true
			}
		}
	}
	return false
}

func normalizeRequestReviewPolicy(lines []string) ([]string, bool) {
	result := make([]string, 0, len(lines))
	modified := false
	state := requestReviewPolicyState{}

	for _, line := range lines {
		newLine, lineModified := normalizeRequestReviewPolicyLine(line, &state)
		result = append(result, newLine)
		modified = modified || lineModified
	}

	return result, modified
}

type requestReviewPolicyState struct {
	inSafeOutputs        bool
	safeOutputsIndent    string
	inPullRequest        bool
	handlerIndent        string
	inProtectedFiles     bool
	protectedFilesIndent string
}

func normalizeRequestReviewPolicyLine(line string, state *requestReviewPolicyState) (string, bool) {
	trimmed := strings.TrimSpace(line)
	indent := getIndentation(line)
	if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
		switch {
		case state.inSafeOutputs && hasExitedBlock(line, state.safeOutputsIndent):
			state.inSafeOutputs, state.inPullRequest, state.inProtectedFiles = false, false, false
		case state.inPullRequest && hasExitedBlock(line, state.handlerIndent):
			state.inPullRequest, state.inProtectedFiles = false, false
		case state.inProtectedFiles && hasExitedBlock(line, state.protectedFilesIndent):
			state.inProtectedFiles = false
		}
	}

	if strings.HasPrefix(trimmed, "safe-outputs:") {
		state.inSafeOutputs, state.inPullRequest, state.inProtectedFiles = true, false, false
		state.safeOutputsIndent = indent
		return line, false
	}
	if state.inSafeOutputs && isDescendant(indent, state.safeOutputsIndent) &&
		(strings.HasPrefix(trimmed, "create-pull-request:") || strings.HasPrefix(trimmed, "push-to-pull-request-branch:")) {
		state.inPullRequest, state.inProtectedFiles = true, false
		state.handlerIndent = indent
		return line, false
	}
	if state.inPullRequest && isDescendant(indent, state.handlerIndent) &&
		(strings.HasPrefix(trimmed, "protected-files:") || strings.HasPrefix(trimmed, "protected-files-policy:")) {
		newLine := strings.Replace(line, "request_review", "request-review", 1)
		state.inProtectedFiles = strings.HasPrefix(trimmed, "protected-files:") &&
			strings.HasSuffix(strings.TrimSpace(newLine), ":")
		state.protectedFilesIndent = indent
		return newLine, newLine != line
	}
	if state.inProtectedFiles && isDescendant(indent, state.protectedFilesIndent) && strings.HasPrefix(trimmed, "policy:") {
		newLine := strings.Replace(line, "request_review", "request-review", 1)
		return newLine, newLine != line
	}
	return line, false
}
