package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/logger"
)

var updateDisplayLog = logger.New("cli:update_display")

type updateFailureGroup struct {
	Workflows string `console:"header:Workflows,maxlen:60"`
	Reason    string `console:"header:Reason,maxlen:100"`
}

// showUpdateSummary displays a compact summary of workflow updates.
func showUpdateSummary(successfulUpdates []string, failedUpdates []updateFailure, noCompile bool) {
	updateDisplayLog.Printf("Rendering update summary: %d succeeded, %d failed", len(successfulUpdates), len(failedUpdates))
	fmt.Fprintln(os.Stderr, "")

	if len(successfulUpdates) > 0 {
		action := "Updated"
		if !noCompile {
			action = "Updated and compiled"
		}
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("%s %d workflow(s)", action, len(successfulUpdates))))
		for _, name := range successfulUpdates {
			fmt.Fprintln(os.Stderr, console.FormatListItem(name))
		}
		fmt.Fprintln(os.Stderr, "")
	}

	if len(failedUpdates) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("%d workflow(s) could not be updated", len(failedUpdates))))
		fmt.Fprint(os.Stderr, console.RenderStruct(groupUpdateFailures(failedUpdates)))
	}
}

func groupUpdateFailures(failures []updateFailure) []updateFailureGroup {
	grouped := make(map[string][]string)
	for _, failure := range failures {
		reason := compactUpdateFailureReason(failure.Error)
		grouped[reason] = append(grouped[reason], failure.Name)
	}

	reasons := make([]string, 0, len(grouped))
	for reason := range grouped {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)

	result := make([]updateFailureGroup, 0, len(reasons))
	for _, reason := range reasons {
		names := grouped[reason]
		sort.Strings(names)
		result = append(result, updateFailureGroup{
			Workflows: strings.Join(names, ", "),
			Reason:    reason,
		})
	}
	return result
}

func compactUpdateFailureReason(message string) string {
	switch {
	case errorutil.IsAuthError(message) && errorutil.IsRateLimitError(message):
		return "SAML-restricted authenticated access; anonymous GitHub API fallback is rate-limited"
	case errorutil.IsAuthError(message):
		return "GitHub API access is restricted by authentication or SAML"
	case errorutil.IsRateLimitError(message):
		return "GitHub API rate limit exceeded"
	}

	line := strings.TrimSpace(strings.SplitN(message, "\n", 2)[0])
	if line == "" {
		return "Unknown update error"
	}
	return line
}
