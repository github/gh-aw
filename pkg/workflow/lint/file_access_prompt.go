package lint

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// fileAccessInstruction is one prompt sentence or fenced shell line that
// implies file access (FAL-X*).
type fileAccessInstruction struct {
	line int
	// shell marks fenced shell commands. Their writes are governed by bash
	// allowlists, which prompt-tool validation already reports.
	shell          bool
	write          bool
	targets        []string
	docker         bool
	cacheMemoryIDs []string
	repoMemory     bool
}

const (
	// fileAccessRuntimeRoot is the agent runtime directory that holds memory mounts.
	fileAccessRuntimeRoot = "/tmp/gh-aw/"
	// fileAccessDockerSocket is the host Docker socket hidden by the agent sandbox.
	fileAccessDockerSocket = "/var/run/docker.sock"
)

var (
	fileAccessWriteVerb = regexp.MustCompile(`(?i)(?:^|\b(?:then|and)\s+|,\s+)(?:(?:please|you\s+(?:must|should|will)|must|then|finally|also|next|always|now)[\s,]+)*(?:create|write|edit|modify|update|save|append|overwrite|store|install)\b`)
	fileAccessNoun      = regexp.MustCompile(`(?i)\b(?:files?|director(?:y|ies)|folders?)\b`)
	fileAccessNegation  = regexp.MustCompile(`(?i)\b(?:not|never|don't|do\s+not|doesn't|avoid|without|cannot|can't|mustn't|shouldn't|no)\b`)
	fileAccessDocker    = regexp.MustCompile(`(?i)\b(?:run|use|execute|start|build|launch|pull|spawn)\s+(?:(?:a|an|the|new|local|your)\s+)*docker(?:-compose)?(?:[\s)` + "`" + `'",]|$)|` + "`" + `docker(?:-compose)?\s`)
	// fileAccessDaemonless lists Docker CLI commands that query registries without the socket.
	fileAccessDaemonless   = regexp.MustCompile(`(?i)\bdocker\s+manifest\b`)
	fileAccessIllustration = regexp.MustCompile(`(?i)\b(?:such\s+as|e\.g\b|for\s+example|for\s+instance)`)
	fileAccessMemoryDir    = regexp.MustCompile(fileAccessRuntimeRoot + `(?:cache-memory(?:-[A-Za-z0-9_-]+)?|repo-memory\b)`)
	fileAccessBlockStart   = regexp.MustCompile(`^\s*(?:#{1,6}\s|[-*+]\s|\d+[.)]\s|\|)`)
	fileAccessCacheDir     = regexp.MustCompile(fileAccessRuntimeRoot + `cache-memory(-[A-Za-z0-9_-]+)?`)
	fileAccessRepoDir      = regexp.MustCompile(fileAccessRuntimeRoot + `repo-memory\b`)
	fileAccessSentence     = regexp.MustCompile(`[.!?;]\s+`)
	fileAccessListLead     = regexp.MustCompile(`^\s*(?:>\s*)*(?:[-*+]|\d+[.)])?\s*(?:\[[ xX]\]\s*)?`)
)

var shellFenceLanguages = []string{"bash", "sh", "shell", "zsh", "console", "shell-session"}

func fileAccessInstructions(ctx context.Context, markdown string) ([]fileAccessInstruction, error) {
	text := strings.ReplaceAll(strings.ReplaceAll(markdown, "\r\n", "\n"), "\r", "\n")
	var instructions []fileAccessInstruction
	var fenceMarker byte
	var fenceLength int
	shellFence := false
	inComment := false
	var block proseBlock
	flush := func() {
		instructions = append(instructions, block.instructions()...)
		block = proseBlock{}
	}
	lineNumber := 0
	for line := range strings.Lines(text) {
		if err := ctx.Err(); err != nil {
			return instructions, err
		}
		line = strings.TrimSuffix(line, "\n")
		lineNumber++
		marker, length, rest, isFence := markdownFence(line)
		if fenceMarker != 0 {
			if isFence && marker == fenceMarker && length >= fenceLength && strings.TrimSpace(rest) == "" {
				fenceMarker = 0
				continue
			}
			if shellFence {
				if instruction, ok := shellFenceInstruction(line, lineNumber); ok {
					instructions = append(instructions, instruction)
				}
			} else if instruction, ok := memoryInstruction(line, lineNumber); ok {
				instructions = append(instructions, instruction)
			}
			continue
		}
		if isFence {
			flush()
			fenceMarker, fenceLength = marker, length
			language := strings.ToLower(strings.TrimSpace(rest))
			if fields := strings.Fields(language); len(fields) > 0 {
				language = strings.Trim(fields[0], "{}")
			}
			shellFence = slices.Contains(shellFenceLanguages, language)
			continue
		}
		line, inComment = stripHTMLComments(line, inComment)
		if strings.TrimSpace(line) == "" || fileAccessBlockStart.MatchString(line) {
			flush()
		}
		block.add(line, lineNumber)
	}
	flush()
	return instructions, nil
}

// proseBlock joins wrapped lines so sentences spanning lines are analyzed
// together, while findings still report the line holding the match.
type proseBlock struct {
	text   strings.Builder
	starts []int
	lines  []int
}

func (block *proseBlock) add(line string, lineNumber int) {
	if strings.TrimSpace(line) == "" {
		return
	}
	if block.text.Len() > 0 {
		block.text.WriteByte(' ')
	}
	block.starts = append(block.starts, block.text.Len())
	block.lines = append(block.lines, lineNumber)
	block.text.WriteString(line)
}

func (block *proseBlock) lineAt(offset int) int {
	index, found := slices.BinarySearch(block.starts, offset)
	if !found {
		index--
	}
	index = max(index, 0)
	if index >= 0 && index < len(block.lines) {
		return block.lines[index]
	}
	return 0
}

func (block *proseBlock) instructions() []fileAccessInstruction {
	text := block.text.String()
	if text == "" {
		return nil
	}
	var instructions []fileAccessInstruction
	offset := 0
	for _, bounds := range append(fileAccessSentence.FindAllStringIndex(text, -1), []int{len(text), len(text)}) {
		if len(bounds) != 2 {
			continue
		}
		sentence := text[offset:bounds[0]] //nolint:uncheckedsliceindex // Index pairs are checked above.
		start := offset + len(sentence) - len(strings.TrimLeft(sentence, " \t"))
		instructions = append(instructions, proseInstructions(sentence, start, block)...)
		offset = bounds[1] //nolint:uncheckedsliceindex // Index pairs are checked above.
	}
	return instructions
}
func stripHTMLComments(line string, inComment bool) (string, bool) {
	var kept strings.Builder
	for line != "" {
		if inComment {
			end := strings.Index(line, "-->")
			if end < 0 {
				return kept.String(), true
			}
			line, inComment = line[end+3:], false
			continue
		}
		start := strings.Index(line, "<!--")
		if start < 0 {
			kept.WriteString(line)
			break
		}
		kept.WriteString(line[:start])
		line, inComment = line[start+4:], true
	}
	return kept.String(), inComment
}

func proseInstructions(sentence string, start int, block *proseBlock) []fileAccessInstruction {
	// FAL-X2: negated and illustrative sentences are not instructions.
	if fileAccessNegation.MatchString(sentence) || fileAccessIllustration.MatchString(sentence) {
		return nil
	}
	var instructions []fileAccessInstruction
	seen := make(map[string]struct{})
	for _, match := range fileAccessMemoryDir.FindAllStringIndex(sentence, -1) {
		if len(match) != 2 {
			continue
		}
		matchStart, matchEnd := match[0], match[1] //nolint:uncheckedsliceindex // Index pairs are checked above.
		line := block.lineAt(start + matchStart)
		key := fmt.Sprintf("%d:%s", line, sentence[matchStart:matchEnd])
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		if instruction, ok := memoryInstruction(sentence[matchStart:matchEnd], line); ok {
			instructions = append(instructions, instruction)
		}
	}
	clean := fileAccessListLead.ReplaceAllString(strings.ReplaceAll(sentence, "**", ""), "")
	instruction := fileAccessInstruction{line: block.lineAt(start)}
	paths := promptPathTokens(clean)
	if fileAccessWriteVerb.MatchString(clean) && (len(paths) > 0 || fileAccessNoun.MatchString(clean)) {
		instruction.write = true
		instruction.targets = writeTargets(paths)
	}
	instruction.docker = fileAccessDocker.MatchString(sentence) && !fileAccessDaemonless.MatchString(sentence) ||
		strings.Contains(sentence, fileAccessDockerSocket)
	if instruction.write || instruction.docker {
		instructions = append(instructions, instruction)
	}
	return instructions
}

func shellFenceInstruction(line string, lineNumber int) (fileAccessInstruction, bool) {
	instruction, _ := memoryInstruction(line, lineNumber)
	instruction.shell = true
	command := strings.TrimSpace(line)
	command = strings.TrimSpace(strings.TrimPrefix(command, "$"))
	command = strings.TrimSpace(strings.TrimPrefix(command, "sudo "))
	instruction.docker = (strings.HasPrefix(command, "docker ") || strings.HasPrefix(command, "docker-compose ")) &&
		!fileAccessDaemonless.MatchString(command) || strings.Contains(command, fileAccessDockerSocket)
	return instruction, instruction.docker || len(instruction.cacheMemoryIDs) > 0 || instruction.repoMemory
}

func memoryInstruction(line string, lineNumber int) (fileAccessInstruction, bool) {
	instruction := fileAccessInstruction{line: lineNumber}
	for _, match := range fileAccessCacheDir.FindAllString(line, -1) {
		suffix := strings.TrimPrefix(match, fileAccessRuntimeRoot+"cache-memory")
		id := strings.TrimRight(strings.TrimPrefix(suffix, "-"), "-_")
		if id == "" {
			id = "default"
		}
		if !slices.Contains(instruction.cacheMemoryIDs, id) {
			instruction.cacheMemoryIDs = append(instruction.cacheMemoryIDs, id)
		}
	}
	instruction.repoMemory = fileAccessRepoDir.MatchString(line)
	return instruction, len(instruction.cacheMemoryIDs) > 0 || instruction.repoMemory
}

type promptPath struct {
	value    string
	previous string
}

// promptPathTokens returns path-like tokens (FAL-X3). Bare words must be
// absolute or explicitly relative; code spans may also hold relative paths
// and file names, which avoids treating prose such as "and/or" as paths.
func promptPathTokens(sentence string) []promptPath {
	var paths []promptPath
	previous := ""
	inCode := false
	for part := range strings.SplitSeq(sentence, "`") {
		if inCode && strings.Contains(part, "{{") {
			// Expression code spans hold values, not paths.
			inCode = !inCode
			continue
		}
		for word := range strings.FieldsSeq(part) {
			token := strings.TrimRight(strings.TrimLeft(word, `("'[<`), `)"'.,:;!?]>`)
			if pathToken(token, inCode) {
				paths = append(paths, promptPath{value: token, previous: previous})
			}
			previous = strings.ToLower(strings.Trim(word, `(),.:;"'`))
		}
		inCode = !inCode
	}
	return paths
}

func pathToken(token string, inCode bool) bool {
	if token == "" || strings.Contains(token, "://") || strings.Contains(token, "{{") || strings.HasPrefix(token, "@") {
		return false
	}
	if strings.HasPrefix(token, "/") || strings.HasPrefix(token, "./") || strings.HasPrefix(token, "~/") ||
		strings.HasPrefix(token, "$GITHUB_WORKSPACE/") || strings.HasPrefix(token, "${GITHUB_WORKSPACE}/") {
		return len(token) > 1 && !strings.ContainsAny(token, " \t")
	}
	if !inCode || strings.ContainsAny(token, "=()|&<>") {
		return false
	}
	if strings.Contains(token, "/") {
		return true
	}
	extension := path.Ext(token)
	base := strings.TrimSuffix(token, extension)
	return strings.IndexFunc(strings.TrimPrefix(extension, "."), unicode.IsLetter) == 0 && len(base) >= 2
}

// writeTargets prefers paths introduced by a destination preposition.
func writeTargets(paths []promptPath) []string {
	if len(paths) == 0 {
		return nil
	}
	for _, candidate := range paths {
		switch candidate.previous {
		case "to", "into", "in", "at", "under", "inside":
			return []string{candidate.value}
		}
	}
	return []string{paths[0].value} //nolint:uncheckedsliceindex // Empty paths return above.
}
