package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var reactionsLog = logger.New("workflow:reactions")

// validReactions defines the set of valid reaction values
var validReactions = map[string]bool{
	"+1":       true,
	"-1":       true,
	"laugh":    true,
	"confused": true,
	"heart":    true,
	"hooray":   true,
	"rocket":   true,
	"eyes":     true,
	"none":     true,
}

// isValidReaction checks if a reaction value is valid according to the schema
func isValidReaction(reaction string) bool {
	return validReactions[reaction]
}

// getValidReactions returns the list of valid reaction entries
func getValidReactions() []string {
	reactions := make([]string, 0, len(validReactions))
	for reaction := range validReactions {
		reactions = append(reactions, reaction)
	}
	return reactions
}

// parseReactionValue converts a reaction value from YAML to a string.
// YAML parsers may return +1 and -1 as integers, so this function handles
// both string and numeric types.
func parseReactionValue(value any) (string, error) {
	reactionsLog.Printf("Parsing reaction value: type=%T, value=%v", value, value)

	switch v := value.(type) {
	case string:
		reactionsLog.Printf("Parsed string reaction: %s", v)
		return v, nil
	case int:
		result, err := intToReactionString(int64(v))
		if err != nil {
			reactionsLog.Printf("Failed to parse int reaction: %v", err)
		}
		return result, err
	case int64:
		result, err := intToReactionString(v)
		if err != nil {
			reactionsLog.Printf("Failed to parse int64 reaction: %v", err)
		}
		return result, err
	case uint64:
		if v == 1 {
			reactionsLog.Print("Parsed uint64 reaction: +1")
			return "+1", nil
		}
		reactionsLog.Printf("Invalid uint64 reaction value: %d", v)
		return "", fmt.Errorf("invalid reaction value '%d': must be one of %v", v, getValidReactions())
	case float64:
		// YAML may parse +1 and -1 as float64
		if v == 1.0 {
			reactionsLog.Print("Parsed float64 reaction: +1")
			return "+1", nil
		}
		if v == -1.0 {
			reactionsLog.Print("Parsed float64 reaction: -1")
			return "-1", nil
		}
		reactionsLog.Printf("Invalid float64 reaction value: %f", v)
		return "", fmt.Errorf("invalid reaction value '%v': must be one of %v", v, getValidReactions())
	default:
		reactionsLog.Printf("Invalid reaction type: %T", value)
		return "", fmt.Errorf("invalid reaction type: expected string, got %T", value)
	}
}

// intToReactionString converts an integer to a reaction string.
// Only 1 (+1) and -1 are valid integer values for reactions.
func intToReactionString(v int64) (string, error) {
	switch v {
	case 1:
		return "+1", nil
	case -1:
		return "-1", nil
	default:
		return "", fmt.Errorf("invalid reaction value '%d': must be one of %v", v, getValidReactions())
	}
}
