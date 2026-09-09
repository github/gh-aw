package cli

import (
	"encoding/json"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var botsCodemodLog = logger.New("cli:codemod_bots")

// getBotsToOnBotsCodemod creates a codemod for moving top-level 'bots' to 'on.bots'
func getBotsToOnBotsCodemod() Codemod {
	codemod := newMoveTopLevelKeyToOnBlockCodemod(moveToOnBlockConfig{
		ID:           "bots-to-on-bots",
		Name:         "Move bots to on.bots",
		Description:  "Moves the top-level 'bots' field to 'on.bots' as per the new frontmatter structure",
		IntroducedIn: "0.10.0",
		FieldKey:     "bots",
		IsInlineSingle: func(v string) bool {
			return strings.HasPrefix(v, "[")
		},
		Log: botsCodemodLog,
	})
	baseApply := codemod.Apply
	codemod.Apply = func(content string, frontmatter map[string]any) (string, bool, error) {
		topBots, hasTopBots := frontmatter["bots"]
		onMap, hasOnMap := frontmatter["on"].(map[string]any)
		onBots, hasOnBots := onMap["bots"]
		if !hasTopBots || !hasOnMap || !hasOnBots {
			return baseApply(content, frontmatter)
		}

		mergedBots, ok := mergeLegacyBots(onBots, topBots)
		if !ok {
			return content, false, nil
		}
		return applyFrontmatterLineTransform(content, func(lines []string) ([]string, bool) {
			return mergeLegacyBotsLines(lines, mergedBots)
		})
	}
	return codemod
}

func mergeLegacyBots(onBots, topBots any) ([]string, bool) {
	merged := make([]string, 0)
	seen := make(map[string]struct{})
	for _, value := range []any{onBots, topBots} {
		bots, ok := value.([]any)
		if !ok {
			return nil, false
		}
		for _, botValue := range bots {
			bot, ok := botValue.(string)
			if !ok {
				return nil, false
			}
			if _, exists := seen[bot]; !exists {
				seen[bot] = struct{}{}
				merged = append(merged, bot)
			}
		}
	}
	return merged, true
}

func mergeLegacyBotsLines(lines []string, bots []string) ([]string, bool) {
	topBotsStart, topBotsEnd := findBotsBlock(lines, 0, len(lines), 0, false)
	onStart := -1
	for i, line := range lines {
		if isTopLevelKey(line) && strings.HasPrefix(strings.TrimSpace(line), "on:") {
			onStart = i
			break
		}
	}
	if topBotsStart == -1 || onStart == -1 {
		return lines, false
	}

	onEnd := len(lines)
	for i := onStart + 1; i < len(lines); i++ {
		if isTopLevelKey(lines[i]) {
			onEnd = i
			break
		}
	}
	onBotsStart, onBotsEnd := findBotsBlock(lines, onStart+1, onEnd, len(getIndentation(lines[onStart])), true)
	if onBotsStart == -1 {
		return lines, false
	}
	onBotsIndent := getIndentation(lines[onBotsStart])
	topBlock := lines[topBotsStart : topBotsEnd+1]
	onBlock := lines[onBotsStart : onBotsEnd+1]
	comments := append(collectComments(lines, topBotsStart, topBotsEnd), collectComments(lines, onBotsStart, onBotsEnd)...)
	itemComments := mergeBotItemComments(topBlock, onBlock)
	isInline := isInlineBotsValue(lines[onBotsStart]) && len(itemComments) == 0 && len(comments) == 0
	declaration := buildBotsDeclaration(onBotsIndent, bots, itemComments, lines[onBotsStart], isInline)
	if len(comments) > 0 {
		declaration = append(append([]string{}, comments...), declaration...)
	}

	result := make([]string, 0, len(lines))
	for i, line := range lines {
		if (i >= topBotsStart && i <= topBotsEnd) || (i >= onBotsStart && i <= onBotsEnd) {
			continue
		}
		result = append(result, line)
		if i == onStart {
			result = append(result, declaration...)
		}
	}
	return result, true
}

func buildBotsDeclaration(indent string, bots []string, itemComments map[string]string, originalLine string, inline bool) []string {
	if inline {
		encodedBots, err := json.Marshal(bots)
		if err != nil {
			return nil
		}
		line := indent + "bots: " + string(encodedBots)
		if comment := trailingComment(originalLine); comment != "" {
			line += " " + comment
		}
		return []string{line}
	}
	out := []string{indent + "bots:"}
	for _, bot := range bots {
		line := indent + "  - " + bot
		if comment, ok := itemComments[bot]; ok && comment != "" {
			line += " " + comment
		}
		out = append(out, line)
	}
	return out
}

func trailingComment(line string) string {
	trimmed := strings.TrimSpace(line)
	inSingleQuoted := false
	inDoubleQuoted := false
	for i := range trimmed {
		ch := trimmed[i]
		switch ch {
		case '\'':
			if !inDoubleQuoted {
				inSingleQuoted = !inSingleQuoted
			}
		case '"':
			if !inSingleQuoted {
				inDoubleQuoted = !inDoubleQuoted
			}
		case '#':
			if !inSingleQuoted && !inDoubleQuoted {
				return strings.TrimSpace(trimmed[i:])
			}
		}
	}
	return ""
}

func collectComments(lines []string, start, end int) []string {
	comments := make([]string, 0)
	for i := start; i <= end; i++ {
		if i >= len(lines) {
			break
		}
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			comments = append(comments, lines[i])
		}
	}
	return comments
}

func mergeBotItemComments(blocks ...[]string) map[string]string {
	commentsByBot := make(map[string]string)
	for _, block := range blocks {
		for i := range block {
			trimmed := strings.TrimSpace(block[i])
			if strings.HasPrefix(trimmed, "- ") {
				entry := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
				if entry == "" {
					continue
				}
				key, comment := splitBotEntry(entry)
				if key != "" {
					if _, ok := commentsByBot[key]; !ok && comment != "" {
						commentsByBot[key] = comment
					}
				}
			}
		}
	}
	return commentsByBot
}

func splitBotEntry(entry string) (string, string) {
	trimmed := strings.TrimSpace(entry)
	inSingleQuoted := false
	inDoubleQuoted := false
	for i := range trimmed {
		ch := trimmed[i]
		switch ch {
		case '\'':
			if !inDoubleQuoted {
				inSingleQuoted = !inSingleQuoted
			}
		case '"':
			if !inSingleQuoted {
				inDoubleQuoted = !inDoubleQuoted
			}
		case '#':
			if !inSingleQuoted && !inDoubleQuoted {
				return strings.TrimSpace(trimmed[:i]), strings.TrimSpace(trimmed[i:])
			}
		}
	}
	return trimmed, ""
}

func isInlineBotsValue(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "bots:") && !strings.Contains(trimmed, "\n") && !strings.Contains(trimmed, "- ")
}

func findBotsBlock(lines []string, start, end, indent int, nested bool) (int, int) {
	var directChildIndent = -1
	for i := start; i < end; i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lineIndent := len(getIndentation(line))
		if nested {
			if lineIndent <= indent {
				continue
			}
			if directChildIndent == -1 {
				directChildIndent = lineIndent
			}
			if lineIndent > directChildIndent {
				continue
			}
			if lineIndent != directChildIndent {
				continue
			}
		} else if lineIndent != indent {
			continue
		}
		if !strings.HasPrefix(trimmed, "bots:") {
			continue
		}
		blockEnd := i
		for j := i + 1; j < end; j++ {
			next := strings.TrimSpace(lines[j])
			if next == "" || strings.HasPrefix(next, "#") {
				blockEnd = j
				continue
			}
			nextIndent := len(getIndentation(lines[j]))
			if nextIndent <= lineIndent && isYAMLKeyLike(lines[j]) {
				break
			}
			if nextIndent > lineIndent {
				blockEnd = j
				continue
			}
			break
		}
		return i, blockEnd
	}
	return -1, -1
}

func isYAMLKeyLike(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "- ") {
		return false
	}
	return strings.Contains(trimmed, ":")
}
