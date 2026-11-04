package taint

import (
	"path/filepath"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

func (s *shell) redirectOutput(stmt *syntax.Stmt, redir *syntax.Redirect, out value) {
	switch redir.Op.String() {
	case ">", ">>", "&>", "&>>":
		if redir.N != nil && redir.N.Value != "1" && !strings.HasPrefix(redir.Op.String(), "&") {
			return
		}
		target := wordText(redir.Word, s.constants)
		if !specialTarget(target) {
			targets, known := s.paths(redir.Word)
			if !known && len(union(out, s.word(redir.Word))) > 0 {
				s.a.gapAt(s.path, int(redir.Pos().Line()), "dynamic file targets are not modeled")
			}
			for _, target := range targets {
				s.write(target, out, s.outputName(stmt.Cmd, target), int(stmt.Pos().Line()), stmt.Cmd)
			}
			return
		}
		payload := s.outputName(stmt.Cmd, target)
		if payload == "" {
			for _, source := range stmt.Redirs {
				if source.Hdoc != nil {
					payload = heredocOutputName(source.Hdoc, s.constants, s.markers)
					if target == "$GITHUB_OUTPUT" {
						payload = "" // A heredoc can contain several output records.
					}
					if payload != "" {
						break
					}
				}
			}
		}
		s.write(target, out, payload, int(stmt.Pos().Line()), stmt.Cmd)
	case ">&":
		if staticWord(redir.Word) == "2" {
			s.a.emit(Site{Kind: Log, Path: s.path, Line: int(stmt.Pos().Line())}, out)
		}
	}
}

func (s *shell) pathFor(path string) string {
	if specialTarget(path) || filepath.IsAbs(path) || s.workingDirectory == "" {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(s.workingDirectory, path))
}

func (s *shell) paths(word *syntax.Word) ([]string, bool) {
	texts, known := wordTexts(word, s.constants, s.constantValues, s.constantUnknown)
	paths := make([]string, 0, len(texts))
	seen := make(map[string]struct{}, len(texts))
	for _, text := range texts {
		if text == "" || containsMarker(text, s.markers) {
			known = false
			continue
		}
		path := s.pathFor(text)
		if _, exists := seen[path]; !exists {
			paths = append(paths, path)
			seen[path] = struct{}{}
		}
	}
	return paths, known
}

func (s *shell) readPaths(pattern string) ([]string, bool) {
	if !strings.ContainsAny(pattern, "*?[") {
		return []string{pattern}, true
	}
	var matches []string
	for file := range s.files {
		matched, err := filepath.Match(filepath.ToSlash(pattern), filepath.ToSlash(file))
		if err != nil {
			return nil, false
		}
		if matched {
			matches = append(matches, file)
		}
	}
	slices.Sort(matches)
	return matches, true
}

func (s *shell) readFileWord(word *syntax.Word) value {
	targets, known := s.paths(word)
	pathSources := s.word(word)
	relevant := s.files.hasSources() || s.persisted.hasSources() || len(pathSources) > 0
	if !known && relevant {
		s.a.gapAt(s.path, int(word.Pos().Line()), "dynamic file targets are not modeled")
	}
	var sources value
	for _, target := range targets {
		files, modeled := s.readPaths(target)
		if !modeled && relevant {
			s.a.gapAt(s.path, int(word.Pos().Line()), "file patterns could not be modeled")
		}
		for _, file := range files {
			sources = union(sources, s.files[file])
		}
	}
	return sources
}

func (s *shell) write(target string, sources value, payload string, line int, command syntax.Command) {
	site := Site{Path: s.path, Line: line}
	switch target {
	case "$GITHUB_STEP_SUMMARY":
		site.Kind = Summary
		s.a.emit(site, sources)
	case "$GITHUB_OUTPUT", "$GITHUB_ENV":
		name, _, found := strings.Cut(payload, "=")
		if !found {
			name, _, _ = strings.Cut(payload, "<<")
		}
		name = strings.TrimSpace(name)
		if target == "$GITHUB_OUTPUT" {
			site.Kind = Output
			s.files[target] = union(s.files[target], sources)
			s.a.transportOutputRecord(site, name, sources, s.a.stepOutput, s.outputMayBeMultiline(command), s.outputRecordComplete(command))
		} else if name != "" {
			key := "env." + strings.ToLower(name)
			s.persisted[key] = union(s.persisted[key], sources)
			s.persisted["shell."+name] = union(s.persisted["shell."+name], sources)
		} else if len(sources) > 0 {
			s.a.gap(s.path, "dynamic GITHUB_ENV names are not modeled")
		}
	case "", "/dev/null":
	default:
		s.files[target] = union(s.files[target], sources)
	}
}

func artifactIncludes(patterns []string, file string) bool {
	included := false
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		pattern, excluded := strings.CutPrefix(pattern, "!")
		if artifactPatternMatches(pattern, file) {
			included = !excluded
		}
	}
	return included
}

func artifactPatternMatches(pattern, file string) bool {
	pattern = filepath.ToSlash(strings.TrimSuffix(pattern, "/"))
	file = filepath.ToSlash(filepath.Clean(file))
	if pattern == "" || pattern == "." || pattern == "**" {
		return true
	}
	patternParts := strings.Split(pattern, "/")
	fileParts := strings.Split(file, "/")
	type position struct{ pattern, file int }
	memo := make(map[position]bool)
	visited := make(map[position]bool)
	var match func(int, int) bool
	match = func(patternIndex, fileIndex int) bool {
		current := position{pattern: patternIndex, file: fileIndex}
		if visited[current] {
			return memo[current]
		}
		visited[current] = true
		patternPart, ok := sliceAt(patternParts, patternIndex)
		if !ok {
			memo[current] = true
			return memo[current]
		}
		if patternPart == "**" {
			memo[current] = match(patternIndex+1, fileIndex) ||
				fileIndex < len(fileParts) && match(patternIndex, fileIndex+1)
			return memo[current]
		}
		filePart, ok := sliceAt(fileParts, fileIndex)
		if !ok {
			return false
		}
		matched, err := filepath.Match(patternPart, filePart)
		memo[current] = err == nil && matched && match(patternIndex+1, fileIndex+1)
		return memo[current]
	}
	return match(0, 0)
}
