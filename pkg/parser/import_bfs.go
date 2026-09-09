// Package parser provides functions for parsing and processing workflow markdown files.
// import_bfs.go implements the BFS traversal core for processing workflow imports.
// It orchestrates queue seeding, the BFS loop, queue item dispatch, and result assembly
// using the importAccumulator to collect results across all imported files.
package parser

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/setutil"
)

// processImportsFromFrontmatterWithManifestAndSource is the internal implementation that includes source tracking.
func processImportsFromFrontmatterWithManifestAndSource(frontmatter map[string]any, baseDir string, cache *ImportCache, workflowFilePath string, yamlContent string) (*ImportsResult, error) {
	importsField, exists := frontmatter["imports"]
	if !exists {
		return &ImportsResult{}, nil
	}
	parserLog.Print("Processing imports from frontmatter with recursive BFS")
	importSpecs, err := parseImportSpecsFromField(importsField)
	if err != nil {
		return nil, err
	}
	if len(importSpecs) == 0 {
		return &ImportsResult{}, nil
	}
	parserLog.Printf("Found %d direct imports to process", len(importSpecs))
	state := newImportBFSState()
	if err := seedInitialImportQueue(importSpecs, baseDir, cache, workflowFilePath, yamlContent, state); err != nil {
		return nil, err
	}
	if err := processImportQueue(baseDir, cache, workflowFilePath, yamlContent, state); err != nil {
		return nil, err
	}
	parserLog.Printf("Completed BFS traversal. Processed %d imports in total", len(state.processedOrder))
	priorities := make(map[string]int, len(state.processedOrder))
	for fullPath, item := range state.processedItems {
		priorities[fullPath] = item.priority
	}
	topologicalOrder, err := topologicalSortImports(state.processedOrder, state.dependencies, state.importPaths, priorities, workflowFilePath)
	if err != nil {
		return nil, err
	}
	for _, fullPath := range topologicalOrder {
		item := state.processedItems[fullPath]
		if item.frontmatter != nil {
			state.acc.extractOrderedStepFields(item.frontmatter)
		}
	}
	displayOrder := make([]string, 0, len(topologicalOrder))
	for _, fullPath := range topologicalOrder {
		displayOrder = append(displayOrder, state.importPaths[fullPath])
	}
	parserLog.Printf("Sorted imports in topological order: %v", displayOrder)
	return state.acc.toImportsResult(displayOrder), nil
}

type nestedImportEntry struct {
	path   string
	inputs map[string]any
}

type importBFSState struct {
	queue          []importQueueItem
	visited        map[string]struct{}
	visitedInputs  map[string]map[string]any
	processedOrder []string
	processedItems map[string]importQueueItem
	dependencies   map[string][]string
	importPaths    map[string]string
	acc            *importAccumulator
}

func newImportBFSState() *importBFSState {
	return &importBFSState{
		visited:        make(map[string]struct{}),
		visitedInputs:  make(map[string]map[string]any),
		processedItems: make(map[string]importQueueItem),
		dependencies:   make(map[string][]string),
		importPaths:    make(map[string]string),
		acc:            newImportAccumulator(),
	}
}

func parseImportSpecsFromField(importsField any) ([]ImportSpec, error) {
	switch v := importsField.(type) {
	case []any:
		return parseImportSpecsFromArray(v)
	case []string:
		return importSpecsFromStringSlice(v), nil
	case map[string]any:
		return parseImportSpecsFromObject(v)
	default:
		return nil, errors.New("imports field must be an array or an object with an 'aw' subfield")
	}
}

func parseImportSpecsFromObject(importsObject map[string]any) ([]ImportSpec, error) {
	awAny, hasAW := importsObject["aw"]
	if !hasAW {
		return nil, nil
	}
	switch awVal := awAny.(type) {
	case []any:
		specs, err := parseImportSpecsFromArray(awVal)
		if err != nil {
			return nil, fmt.Errorf("imports.aw: %w", err)
		}
		return specs, nil
	case []string:
		return importSpecsFromStringSlice(awVal), nil
	default:
		return nil, errors.New("imports.aw must be an array of strings or objects")
	}
}

func importSpecsFromStringSlice(paths []string) []ImportSpec {
	specs := make([]ImportSpec, 0, len(paths))
	for _, s := range paths {
		specs = append(specs, ImportSpec{Path: s})
	}
	return specs
}

func seedInitialImportQueue(importSpecs []ImportSpec, baseDir string, cache *ImportCache, workflowFilePath string, yamlContent string, state *importBFSState) error {
	for index, importSpec := range importSpecs {
		if err := seedSingleImportSpec(importSpec, index, baseDir, cache, workflowFilePath, yamlContent, state); err != nil {
			return err
		}
	}
	return nil
}

func seedSingleImportSpec(importSpec ImportSpec, priority int, baseDir string, cache *ImportCache, workflowFilePath string, yamlContent string, state *importBFSState) error {
	importPath := importSpec.Path
	filePath, sectionName := splitPathAndSection(importPath)
	fullPath, err := resolveSeedImportPath(filePath, importPath, baseDir, cache, workflowFilePath, yamlContent)
	if err != nil {
		if isRepositoryImport(importPath) {
			parserLog.Printf("Detected repository import: %s", importPath)
			state.acc.repositoryImports = append(state.acc.repositoryImports, importPath)
			return nil
		}
		return err
	}
	origin, err := detectRemoteImportOrigin(filePath)
	if err != nil {
		return err
	}
	return enqueueImportPath(state, importPath, fullPath, sectionName, baseDir, importSpec.Inputs, origin, priority)
}

func resolveSeedImportPath(filePath, importPath, baseDir string, cache *ImportCache, workflowFilePath string, yamlContent string) (string, error) {
	fullPath, err := ResolveIncludePath(filePath, baseDir, cache)
	if err != nil {
		return "", formatInitialImportResolveError(filePath, importPath, workflowFilePath, yamlContent, err)
	}
	if err := validateNoLockYMLImport(fullPath, importPath, workflowFilePath, yamlContent); err != nil {
		return "", err
	}
	return fullPath, nil
}

func formatInitialImportResolveError(filePath, importPath, workflowFilePath, yamlContent string, resolveErr error) error {
	if workflowFilePath != "" && yamlContent != "" {
		line, column := findImportItemLocation(yamlContent, importPath)
		importErr := &ImportError{
			ImportPath: importPath,
			FilePath:   workflowFilePath,
			Line:       line,
			Column:     column,
			Cause:      resolveErr,
		}
		return FormatImportError(importErr, yamlContent)
	}
	return fmt.Errorf("failed to resolve import '%s': %w", filePath, resolveErr)
}

func validateNoLockYMLImport(fullPath, importPath, workflowFilePath, yamlContent string) error {
	if !strings.HasSuffix(strings.ToLower(fullPath), ".lock.yml") {
		return nil
	}
	lockErr := errors.New("cannot import .lock.yml files. Lock files are compiled outputs from gh-aw. Import the source .md file instead")
	if workflowFilePath != "" && yamlContent != "" {
		line, column := findImportItemLocation(yamlContent, importPath)
		importErr := &ImportError{ImportPath: importPath, FilePath: workflowFilePath, Line: line, Column: column, Cause: lockErr}
		return FormatImportError(importErr, yamlContent)
	}
	return fmt.Errorf("cannot import .lock.yml files: '%s'. Lock files are compiled outputs from gh-aw. Import the source .md file instead", importPath)
}

func detectRemoteImportOrigin(filePath string) (*remoteImportOrigin, error) {
	if !IsWorkflowSpec(filePath) {
		return nil, nil
	}
	origin, err := parseRemoteOrigin(filePath)
	if err != nil {
		return nil, fmt.Errorf("invalid workflowspec ref in %q: %w", filePath, err)
	}
	if origin != nil {
		importLog.Printf("Tracking remote origin for workflowspec: %s/%s@%s", origin.Owner, origin.Repo, origin.Ref)
	}
	return origin, nil
}

func enqueueImportPath(state *importBFSState, importPath, fullPath, sectionName, baseDir string, inputs map[string]any, origin *remoteImportOrigin, priority int) error {
	if !setutil.Contains(state.visited, fullPath) {
		state.visited[fullPath] = struct{}{}
		state.visitedInputs[fullPath] = inputs
		state.queue = append(state.queue, importQueueItem{
			importPath: importPath, fullPath: fullPath, sectionName: sectionName, baseDir: baseDir, inputs: inputs, remoteOrigin: origin, priority: priority,
		})
		parserLog.Printf("Queued import: %s (resolved to %s)", importPath, fullPath)
		return nil
	}
	if err := checkImportInputsConsistency(importPath, state.visitedInputs[fullPath], inputs); err != nil {
		return err
	}
	parserLog.Printf("Skipping duplicate import: %s (already visited)", importPath)
	return nil
}

func processImportQueue(baseDir string, cache *ImportCache, workflowFilePath string, yamlContent string, state *importBFSState) error {
	for len(state.queue) > 0 {
		item := state.queue[0]
		state.queue = state.queue[1:]
		if err := processQueueItem(item, baseDir, cache, workflowFilePath, yamlContent, state); err != nil {
			return err
		}
	}
	return nil
}

func processQueueItem(item importQueueItem, baseDir string, cache *ImportCache, workflowFilePath string, yamlContent string, state *importBFSState) error {
	parserLog.Printf("Processing import from queue: %s", item.fullPath)
	maps.Copy(state.acc.importInputs, item.inputs)
	state.processedOrder = append(state.processedOrder, item.fullPath)
	state.importPaths[item.fullPath] = item.importPath
	state.processedItems[item.fullPath] = item
	handled, err := handleAgentImportItem(item, state)
	if handled || err != nil {
		return err
	}
	handled, err = handleYAMLWorkflowImportItem(item, state)
	if handled || err != nil {
		return err
	}
	return handleStandardImportItem(item, baseDir, cache, workflowFilePath, yamlContent, state)
}

func handleAgentImportItem(item importQueueItem, state *importBFSState) (bool, error) {
	fullPathSlash := filepath.ToSlash(item.fullPath)
	isAgentFile := strings.Contains(fullPathSlash, "/.github/agents/") && strings.HasSuffix(strings.ToLower(fullPathSlash), ".md")
	if !isAgentFile {
		return false, nil
	}
	if state.acc.agentFile != "" {
		parserLog.Printf("Multiple agent files found: %s and %s", state.acc.agentFile, item.importPath)
		return true, fmt.Errorf("multiple agent files found in imports: '%s' and '%s'. Only one agent file is allowed per workflow", state.acc.agentFile, item.importPath)
	}
	importRelPath := assignAgentFilePath(state.acc, fullPathSlash, item.importPath, item.fullPath)
	if len(item.inputs) == 0 {
		state.acc.importPaths = append(state.acc.importPaths, importRelPath)
		state.acc.promptImports = append(state.acc.promptImports, PromptImportEntry{ImportPath: importRelPath})
		parserLog.Printf("Added agent import path for runtime-import: %s", importRelPath)
		return true, nil
	}
	parserLog.Printf("Agent file has inputs - will be inlined instead of runtime-imported")
	markdownContent, err := processIncludedFileWithVisited(item.fullPath, item.sectionName, false, state.visited)
	if err != nil {
		return true, fmt.Errorf("failed to process markdown from agent file '%s': %w", item.fullPath, err)
	}
	appendMarkdownWithSeparator(&state.acc.markdownBuilder, markdownContent)
	state.acc.promptImports = append(state.acc.promptImports, PromptImportEntry{Markdown: markdownContent})
	return true, nil
}

func assignAgentFilePath(acc *importAccumulator, fullPathSlash, importPath, fullPath string) string {
	if idx := strings.Index(fullPathSlash, "/.github/"); idx >= 0 {
		acc.agentFile = fullPathSlash[idx+1:]
	} else {
		acc.agentFile = fullPathSlash
	}
	parserLog.Printf("Found agent file: %s (resolved to: %s)", fullPath, acc.agentFile)
	acc.agentImportSpec = importPath
	parserLog.Printf("Agent import specification: %s", acc.agentImportSpec)
	return acc.agentFile
}

func handleYAMLWorkflowImportItem(item importQueueItem, state *importBFSState) (bool, error) {
	if !isYAMLWorkflowFile(item.fullPath) {
		return false, nil
	}
	parserLog.Printf("Detected YAML workflow file: %s", item.fullPath)
	jobsOrStepsData, servicesJSON, err := processYAMLWorkflowImport(item.fullPath)
	if err != nil {
		return true, fmt.Errorf("failed to process YAML workflow '%s': %w", item.importPath, err)
	}
	appendYAMLImportJobsOrSteps(state.acc, item.importPath, item.fullPath, jobsOrStepsData)
	appendYAMLImportServices(state.acc, item.importPath, servicesJSON)
	return true, nil
}

func appendYAMLImportJobsOrSteps(acc *importAccumulator, importPath, fullPath, jobsOrStepsData string) {
	if isCopilotSetupStepsFile(fullPath) {
		if jobsOrStepsData != "" {
			acc.copilotSetupStepsBuilder.WriteString(jobsOrStepsData + "\n")
			parserLog.Printf("Added copilot-setup steps (will be inserted at start): %s", importPath)
		}
		return
	}
	if jobsOrStepsData != "" && jobsOrStepsData != "{}" {
		acc.jobsBuilder.WriteString(jobsOrStepsData + "\n")
		parserLog.Printf("Added jobs from YAML workflow: %s", importPath)
	}
}

func appendYAMLImportServices(acc *importAccumulator, importPath, servicesJSON string) {
	if servicesJSON == "" || servicesJSON == "{}" {
		return
	}
	var services map[string]any
	if err := json.Unmarshal([]byte(servicesJSON), &services); err != nil {
		return
	}
	servicesWrapper := map[string]any{"services": services}
	servicesYAML, err := yaml.Marshal(servicesWrapper)
	if err != nil {
		return
	}
	acc.servicesBuilder.WriteString(string(servicesYAML) + "\n")
	parserLog.Printf("Added services from YAML workflow: %s", importPath)
}

func handleStandardImportItem(item importQueueItem, baseDir string, cache *ImportCache, workflowFilePath string, yamlContent string, state *importBFSState) error {
	content, err := readFileFunc(item.fullPath)
	if err != nil {
		return fmt.Errorf("failed to read imported file '%s': %w", item.fullPath, err)
	}
	result, parseErr := extractImportFrontmatterForNested(content, item)
	item.content = content
	if parseErr != nil {
		parserLog.Printf("Failed to extract frontmatter from %s: %v", item.fullPath, parseErr)
	} else if result.Frontmatter != nil {
		item.frontmatter = result.Frontmatter
		if err := enqueueNestedImports(result.Frontmatter, item, baseDir, cache, workflowFilePath, yamlContent, state); err != nil {
			return err
		}
	}
	state.processedItems[item.fullPath] = item
	return state.acc.extractImportFields(content, item, state.visited, false)
}

func extractImportFrontmatterForNested(content []byte, item importQueueItem) (*FrontmatterResult, error) {
	result, err := extractFrontmatterForImport(item.fullPath, content)
	if err != nil || result == nil {
		return result, err
	}
	inputsWithDefaults := applyImportSchemaDefaultsFromFrontmatter(result.Frontmatter, item.inputs)
	if len(inputsWithDefaults) == 0 {
		return result, nil
	}
	origContent := string(content)
	substituted := substituteImportInputsInContent(origContent, inputsWithDefaults)
	if substituted == origContent {
		return result, nil
	}
	if reparse, rerr := ExtractFrontmatterFromContent(substituted); rerr == nil {
		result = reparse
	}
	return result, nil
}

func extractFrontmatterForImport(fullPath string, content []byte) (*FrontmatterResult, error) {
	if strings.HasPrefix(fullPath, BuiltinPathPrefix) {
		return ExtractFrontmatterFromBuiltinFile(fullPath, content)
	}
	return ExtractFrontmatterFromContent(string(content))
}

func enqueueNestedImports(frontmatter map[string]any, item importQueueItem, baseDir string, cache *ImportCache, workflowFilePath string, yamlContent string, state *importBFSState) error {
	importsField, hasImports := frontmatter["imports"]
	if !hasImports {
		return nil
	}
	importSpecs, err := parseImportSpecsFromField(importsField)
	if err != nil {
		return err
	}
	nestedImports := nestedEntriesFromSpecs(importSpecs)
	for _, nestedEntry := range nestedImports {
		if err := enqueueNestedImportEntry(nestedEntry, item, baseDir, cache, workflowFilePath, yamlContent, state); err != nil {
			return err
		}
	}
	return nil
}

func nestedEntriesFromSpecs(specs []ImportSpec) []nestedImportEntry {
	nestedImports := make([]nestedImportEntry, 0, len(specs))
	for _, spec := range specs {
		nestedImports = append(nestedImports, nestedImportEntry{path: spec.Path, inputs: spec.Inputs})
	}
	return nestedImports
}

func enqueueNestedImportEntry(entry nestedImportEntry, item importQueueItem, baseDir string, cache *ImportCache, workflowFilePath string, yamlContent string, state *importBFSState) error {
	nestedImportPath := entry.path
	nestedFilePath, nestedSectionName := splitPathAndSection(nestedImportPath)
	resolvedPath, nestedRemoteOrigin, err := resolveNestedImportPathAndOrigin(item, nestedFilePath)
	if err != nil {
		return err
	}
	nestedBaseDir := determineNestedBaseDir(item, resolvedPath, baseDir)
	nestedFullPath, err := ResolveIncludePath(resolvedPath, nestedBaseDir, cache)
	if err != nil {
		return formatNestedResolveError(nestedImportPath, nestedFilePath, item, workflowFilePath, yamlContent, err)
	}
	state.dependencies[item.fullPath] = append(state.dependencies[item.fullPath], nestedFullPath)
	canonicalImportPath := canonicalizeNestedImportPath(nestedImportPath, nestedBaseDir, baseDir, nestedRemoteOrigin, nestedFullPath)
	return enqueueNestedVisitedPath(state, canonicalImportPath, nestedFullPath, nestedSectionName, baseDir, entry.inputs, nestedRemoteOrigin, item.priority)
}

func resolveNestedImportPathAndOrigin(item importQueueItem, nestedFilePath string) (string, *remoteImportOrigin, error) {
	if item.remoteOrigin != nil && !IsWorkflowSpec(nestedFilePath) {
		return resolveRemoteNestedPath(item, nestedFilePath)
	}
	if IsWorkflowSpec(nestedFilePath) {
		nestedRemoteOrigin, err := parseRemoteOrigin(nestedFilePath)
		if err != nil {
			return "", nil, fmt.Errorf("invalid workflowspec ref in %q: %w", nestedFilePath, err)
		}
		if nestedRemoteOrigin != nil {
			importLog.Printf("Nested workflowspec import detected: %s (origin: %s/%s@%s)", nestedFilePath, nestedRemoteOrigin.Owner, nestedRemoteOrigin.Repo, nestedRemoteOrigin.Ref)
		}
		return nestedFilePath, nestedRemoteOrigin, nil
	}
	return nestedFilePath, nil, nil
}

func resolveRemoteNestedPath(item importQueueItem, nestedFilePath string) (string, *remoteImportOrigin, error) {
	cleanPath := path.Clean(strings.TrimPrefix(nestedFilePath, "./"))
	if cleanPath == ".." || strings.HasPrefix(cleanPath, "../") || path.IsAbs(cleanPath) {
		return "", nil, fmt.Errorf("nested import '%s' from remote file '%s' escapes base directory", nestedFilePath, item.importPath)
	}
	basePath := item.remoteOrigin.BasePath
	if basePath == "" {
		basePath = constants.GetWorkflowDir()
	}
	basePath = path.Clean(basePath)
	resolvedPath := fmt.Sprintf("%s/%s/%s/%s@%s",
		item.remoteOrigin.Owner, item.remoteOrigin.Repo, basePath, cleanPath, item.remoteOrigin.Ref)
	nestedRemoteOrigin, err := parseRemoteOrigin(resolvedPath)
	if err != nil {
		return "", nil, fmt.Errorf("invalid workflowspec ref in %q: %w", resolvedPath, err)
	}
	importLog.Printf("Resolving nested import as remote workflowspec: %s -> %s (basePath=%s)", nestedFilePath, resolvedPath, basePath)
	return resolvedPath, nestedRemoteOrigin, nil
}

func determineNestedBaseDir(item importQueueItem, resolvedPath, baseDir string) string {
	isLocalRelative := !strings.Contains(resolvedPath, "/") || strings.HasPrefix(resolvedPath, "./")
	if item.remoteOrigin == nil && !IsWorkflowSpec(resolvedPath) && isLocalRelative {
		return filepath.Dir(item.fullPath)
	}
	return baseDir
}

func formatNestedResolveError(nestedImportPath, nestedFilePath string, item importQueueItem, workflowFilePath string, yamlContent string, resolveErr error) error {
	if workflowFilePath != "" && yamlContent != "" {
		line, column := findImportItemLocation(yamlContent, item.importPath)
		importErr := &ImportError{ImportPath: nestedImportPath, FilePath: workflowFilePath, Line: line, Column: column, Cause: resolveErr}
		return FormatImportError(importErr, yamlContent)
	}
	return fmt.Errorf("failed to resolve nested import '%s' from '%s': %w", nestedFilePath, item.fullPath, resolveErr)
}

func canonicalizeNestedImportPath(nestedImportPath, nestedBaseDir, baseDir string, nestedRemoteOrigin *remoteImportOrigin, nestedFullPath string) string {
	if nestedRemoteOrigin != nil || nestedBaseDir == baseDir {
		return nestedImportPath
	}
	rel, err := filepath.Rel(baseDir, nestedFullPath)
	if err != nil {
		return nestedImportPath
	}
	return filepath.ToSlash(rel)
}

func enqueueNestedVisitedPath(state *importBFSState, nestedImportPath, nestedFullPath, nestedSectionName, baseDir string, inputs map[string]any, nestedRemoteOrigin *remoteImportOrigin, priority int) error {
	if !setutil.Contains(state.visited, nestedFullPath) {
		state.visited[nestedFullPath] = struct{}{}
		state.visitedInputs[nestedFullPath] = inputs
		state.queue = append(state.queue, importQueueItem{
			importPath: nestedImportPath, fullPath: nestedFullPath, sectionName: nestedSectionName, baseDir: baseDir, inputs: inputs, remoteOrigin: nestedRemoteOrigin, priority: priority,
		})
		parserLog.Printf("Discovered nested import: %s (queued)", nestedFullPath)
		return nil
	}
	if err := checkImportInputsConsistency(nestedImportPath, state.visitedInputs[nestedFullPath], inputs); err != nil {
		return err
	}
	parserLog.Printf("Skipping already visited nested import: %s (cycle detected)", nestedFullPath)
	return nil
}

// parseImportSpecsFromArray parses an []any slice into a list of ImportSpec values.
// Each element must be a string (simple path) or a map with a required "path" or "uses"
// key and an optional "inputs" or "with" map. The "uses"/"with" form mirrors GitHub Actions
// reusable workflow syntax and is an alias for "path"/"inputs".
func parseImportSpecsFromArray(items []any) ([]ImportSpec, error) {
	var specs []ImportSpec
	for _, item := range items {
		switch importItem := item.(type) {
		case string:
			specs = append(specs, ImportSpec{Path: importItem})
		case map[string]any:
			// Accept "uses" as an alias for "path"
			pathValue, hasPath := importItem["path"]
			if !hasPath {
				pathValue, hasPath = importItem["uses"]
			}
			if !hasPath {
				return nil, errors.New("import object must have a 'path' or 'uses' field")
			}
			pathStr, ok := pathValue.(string)
			if !ok {
				return nil, errors.New("import 'path'/'uses' must be a string")
			}
			// Accept "with" as an alias for "inputs"
			var inputs map[string]any
			inputsValue, hasInputs := importItem["inputs"]
			if !hasInputs {
				inputsValue, hasInputs = importItem["with"]
			}
			if hasInputs {
				if inputsMap, ok := inputsValue.(map[string]any); ok {
					inputs = inputsMap
				} else {
					return nil, errors.New("import 'inputs'/'with' must be an object")
				}
			}
			if _, hasIf := importItem["if"]; hasIf {
				return nil, errors.New("import 'if' is no longer supported; use {{#if ...}}{{#runtime-import? ...}}{{/if}} for experiment-specific prompt imports")
			}
			specs = append(specs, ImportSpec{Path: pathStr, Inputs: inputs})
		default:
			return nil, errors.New("import item must be a string or an object with 'path'/'uses' field")
		}
	}
	return specs, nil
}

// checkImportInputsConsistency returns an error if a file that has already been imported
// is being imported again with different 'with' values. A workflow file can appear at most
// once in the import graph; when it appears multiple times the 'with' values must be identical.
func checkImportInputsConsistency(importPath string, existingInputs, newInputs map[string]any) error {
	if importInputsEqual(existingInputs, newInputs) {
		return nil
	}
	return fmt.Errorf(
		"import conflict: '%s' is imported more than once with different 'with' values.\n"+
			"An imported workflow can only be imported once per workflow.\n"+
			"  Previous 'with': %s\n"+
			"  New 'with':      %s",
		importPath,
		formatImportInputs(existingInputs),
		formatImportInputs(newInputs),
	)
}

// importInputsEqual reports whether two import input maps are deeply equal.
// Both nil and empty maps are considered equal (both represent "no inputs").
// Map key ordering does not affect the result.
func importInputsEqual(a, b map[string]any) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	// encoding/json sorts map keys deterministically, making this a safe deep-equality check.
	aJSON, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bJSON, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(aJSON, bJSON)
}

// formatImportInputs serializes an import input map to a compact JSON string for
// use in error messages. Returns "{}" if the map is nil or empty.
func formatImportInputs(inputs map[string]any) string {
	if len(inputs) == 0 {
		return "{}"
	}
	b, err := json.Marshal(inputs)
	if err != nil {
		return "<unserializable>"
	}
	return string(b)
}
