package taint

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// NUL cannot occur in a filesystem path. These symbols preserve the distinction
// between an expanded runner variable and a literal filename such as '$GITHUB_ENV'.
const (
	runnerEnvTarget     = "\x00github-env"
	runnerPathTarget    = "\x00github-path"
	runnerOutputTarget  = "\x00github-output"
	runnerSummaryTarget = "\x00github-step-summary"
)

type shellOutputRoute struct {
	redirect  *syntax.Redirect
	log       bool
	discarded bool
	unknown   bool
}

func (s *shell) outputRoute(stmt *syntax.Stmt) (shellOutputRoute, bool) {
	routes := map[string]shellOutputRoute{"1": {}, "2": {log: true}}
	incomplete := false
	for _, redir := range stmt.Redirs {
		fd := "1"
		if redir.N != nil {
			fd = redir.N.Value
		}
		switch redir.Op.String() {
		case ">", ">>":
			routes[fd] = shellOutputRoute{redirect: redir}
		case "&>", "&>>":
			routes["1"] = shellOutputRoute{redirect: redir}
			routes["2"] = routes["1"]
		case ">&":
			texts, known := wordTexts(redir.Word, s.constants, s.constantValues, s.constantUnknown)
			target, exists := sliceAt(texts, 0)
			if !known || !exists || len(texts) != 1 {
				routes[fd] = shellOutputRoute{unknown: true}
				incomplete = true
				continue
			}
			if target == "-" {
				routes[fd] = shellOutputRoute{discarded: true}
				continue
			}
			source := strings.TrimSuffix(target, "-")
			if _, err := strconv.Atoi(source); err == nil {
				route, exists := routes[source]
				if !exists {
					route = shellOutputRoute{unknown: true}
					incomplete = true
				}
				routes[fd] = route
				if source != target {
					routes[source] = shellOutputRoute{discarded: true}
				}
			} else if redir.N == nil {
				// Bash's unnumbered >&filename redirects both output streams.
				routes["1"] = shellOutputRoute{redirect: redir}
				routes["2"] = routes["1"]
			} else {
				routes[fd] = shellOutputRoute{unknown: true}
				incomplete = true
			}
		}
	}
	incomplete = s.outputRoutingIncomplete(stmt, routes["2"]) || incomplete
	return routes["1"], incomplete
}

func (s *shell) outputRoutingIncomplete(stmt *syntax.Stmt, stderr shellOutputRoute) bool {
	incomplete := false
	stderrRedirected := stderr.redirect != nil || stderr.discarded || stderr.unknown
	if stderrRedirected {
		incomplete = incomplete || s.trace
		syntax.Walk(stmt.Cmd, func(node syntax.Node) bool {
			if expansion, ok := node.(*syntax.ParamExp); ok && expansion.Exp != nil {
				op := expansion.Exp.Op.String()
				incomplete = incomplete || op == "?" || op == ":?"
			}
			return true
		})
	}
	if call, ok := stmt.Cmd.(*syntax.CallExpr); ok {
		if command, exists := sliceAt(call.Args, 0); exists && literalShellWord(command) == "exec" && len(stmt.Redirs) > 0 {
			s.incompleteRouting = true
		}
	} else if stderrRedirected {
		// Nested commands may write stderr independently of the compound stdout.
		incomplete = true
	}
	return incomplete || s.incompleteRouting
}

func (s *shell) redirectOutput(stmt *syntax.Stmt, redir *syntax.Redirect, out value) {
	targets, known := s.paths(redir.Word)
	if !known && len(union(out, s.word(redir.Word))) > 0 {
		s.a.gapAt(s.path, int(redir.Pos().Line()), "dynamic file targets are not modeled")
	}
	for _, target := range targets {
		payload := s.outputName(stmt.Cmd, runnerTargetName(target))
		if payload == "" {
			for _, source := range stmt.Redirs {
				if source.Hdoc != nil {
					payload = heredocOutputName(source.Hdoc, s.constants, s.markers)
					if target == runnerOutputTarget {
						payload = "" // A heredoc can contain several output records.
					}
					if payload != "" {
						break
					}
				}
			}
		}
		s.write(target, out, payload, int(stmt.Pos().Line()), stmt.Cmd)
	}
}

func (s *shell) pathFor(path string) string {
	if specialTarget(path) {
		return path
	}
	if filepath.IsAbs(path) || s.workingDirectory == "" {
		path = filepath.Clean(path)
	} else {
		path = filepath.Clean(filepath.Join(s.workingDirectory, path))
	}
	// Keep ordinary reserved-looking filenames separate from the canonical
	// runner file keys also used by JavaScript transport.
	if runnerFileKey(path) {
		return "./" + path
	}
	return path
}

func runnerFileKey(path string) bool {
	return path == "$GITHUB_ENV" || path == "$GITHUB_PATH" ||
		path == "$GITHUB_OUTPUT" || path == "$GITHUB_STEP_SUMMARY"
}

func runnerTargetName(target string) string {
	switch target {
	case runnerEnvTarget:
		return "$GITHUB_ENV"
	case runnerPathTarget:
		return "$GITHUB_PATH"
	case runnerOutputTarget:
		return "$GITHUB_OUTPUT"
	case runnerSummaryTarget:
		return "$GITHUB_STEP_SUMMARY"
	default:
		return target
	}
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
	pattern = runnerTargetName(pattern)
	if !strings.ContainsAny(pattern, "*?[") {
		return []string{pattern}, true
	}
	var matches []string
	for file := range s.files {
		if runnerFileKey(file) {
			continue
		}
		matched, err := filepath.Match(filepath.ToSlash(pattern), filepath.ToSlash(filepath.Clean(file)))
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
	target = s.environmentFileWrite(target, sources, line)
	site := Site{Path: s.path, Line: line}
	switch target {
	case "$GITHUB_STEP_SUMMARY":
		site.Kind = Summary
		s.a.emit(site, sources)
	case "$GITHUB_OUTPUT", "$GITHUB_ENV":
		s.files[target] = union(s.files[target], sources)
		name, _, found := strings.Cut(payload, "=")
		if !found {
			name, _, _ = strings.Cut(payload, "<<")
		}
		name = strings.TrimSpace(name)
		if target == "$GITHUB_OUTPUT" {
			site.Kind = Output
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

func (s *shell) environmentFileWrite(target string, sources value, line int) string {
	switch target {
	case runnerEnvTarget, runnerPathTarget:
		s.a.emit(Site{Kind: EnvironmentFile, Path: s.path, Line: line}, sources)
	}
	return runnerTargetName(target)
}

func artifactIncludes(patterns []string, file string) bool {
	if runnerFileKey(file) {
		return false
	}
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
