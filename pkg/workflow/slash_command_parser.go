package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var slashCommandParserLog = logger.New("workflow:slash_command_parser")

// parseSlashCommandShorthand parses a string in the format "/command" and returns the command name.
// It returns an empty string if the input is not a valid slash command shorthand.
// It returns an error if the input starts with "/" but has an empty command name.
func parseSlashCommandShorthand(input string) (commandName string, isSlashCommand bool, err error) {
	// Check if it's a slash command shorthand (starts with /)
	if !strings.HasPrefix(input, "/") {
		return "", false, nil
	}

	// Extract command name (remove leading /)
	commandName = strings.TrimPrefix(input, "/")
	if commandName == "" {
		return "", true, fmt.Errorf("slash command shorthand cannot be empty after '/'")
	}

	slashCommandParserLog.Printf("Parsed slash command shorthand: /%s -> command name: %s", input, commandName)

	return commandName, true, nil
}

// expandSlashCommandShorthand takes a command name and returns a map that represents
// the expanded slash_command + workflow_dispatch configuration.
func expandSlashCommandShorthand(commandName string) map[string]any {
	return map[string]any{
		"slash_command":     commandName,
		"workflow_dispatch": nil,
	}
}
