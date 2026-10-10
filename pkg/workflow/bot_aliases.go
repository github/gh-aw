package workflow

import (
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var botAliasesLog = logger.New("workflow:bot_aliases")

// copilotBotSet is a fast-lookup set built from constants.CopilotBotNames.
// Any entry in this set triggers expansion to the full CopilotBotNames list.
var copilotBotSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(constants.CopilotBotNames))
	for _, name := range constants.CopilotBotNames {
		set[name] = struct{}{}
	}
	return set
}()

// expandBotNames expands any entry found in constants.CopilotBotNames to the
// full set of Copilot identifiers. Other entries are passed through unchanged.
// Duplicates are removed from the result.
//
// A nil or empty input slice is returned as-is. The nil/empty distinction is
// preserved so callers can distinguish "no bots configured" (nil) from "bots
// field present but empty" ([]string{}).
//
// The recognized identifiers are defined in constants.CopilotBotNames.
func expandBotNames(bots []string) []string {
	if len(bots) == 0 {
		return bots
	}
	needsExpansion := false
	for _, b := range bots {
		if setutil.Contains(copilotBotSet, b) {
			needsExpansion = true
			break
		}
	}
	if !needsExpansion {
		return bots
	}
	// Pre-allocate with the worst-case capacity: every entry is a copilot
	// identifier that expands to len(constants.CopilotBotNames) entries.
	expanded := make([]string, 0, len(bots)*len(constants.CopilotBotNames))
	for _, b := range bots {
		if setutil.Contains(copilotBotSet, b) {
			expanded = append(expanded, constants.CopilotBotNames...)
		} else {
			expanded = append(expanded, b)
		}
	}
	result := sliceutil.Deduplicate(expanded)
	botAliasesLog.Printf("Expanded bot names: input=%d, output=%d", len(bots), len(result))
	return result
}

func hasCopilotBotName(bots []string) bool {
	for _, bot := range bots {
		if setutil.Contains(copilotBotSet, bot) {
			return true
		}
	}
	return false
}
