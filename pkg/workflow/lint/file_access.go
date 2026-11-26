package lint

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
)

// FileAccessRule simulates the agent's effective file-access capabilities and
// predicts prompt instructions that the sandbox or tool configuration will
// block. The behavior is specified in specs/file-access-lint-spec.md.
func FileAccessRule() Rule {
	return Rule{
		ID:          "file-access",
		Description: "Predict prompt file-access instructions that the sandbox or tool configuration will block",
		Severity:    Warning, Targets: AWAST, Check: checkFileAccess,
	}
}

// triState models configuration that imports may still supply.
type triState uint8

const (
	stateUnknown triState = iota
	stateEnabled
	stateDisabled
)

// fileAccessModel is the simulated capability model (FAL-M*).
type fileAccessModel struct {
	sandbox triState
	// write is the agent's ability to create or modify file contents through
	// the edit tool or a write-capable shell command.
	write       triState
	writeSource RelatedLocation
	// cacheMemory lists configured cache-memory IDs; nil with known=false means unknown.
	cacheMemory      map[string]struct{}
	cacheMemoryKnown bool
	repoMemory       triState
	// protectedBlocked is true when every configured PR writer blocks protected files.
	protectedBlocked bool
	protectedExclude []string
	protectedSource  string
	engine           string
}

var readOnlySystemPrefixes = []string{"/usr", "/opt", "/bin", "/sbin", "/lib", "/lib64", "/etc", "/boot"}

// writeCapableCommands lists shell stems that can create or modify file
// contents without redirection. Redirection is not modeled (FAL-M4).
var writeCapableCommands = []string{
	"tee", "cp", "mv", "touch", "mkdir", "sed", "awk", "dd", "install", "ln", "truncate", "patch",
	"python", "python3", "node", "perl", "ruby", "sh", "bash", "zsh", "go", "make", "npm", "npx",
}

func checkFileAccess(ctx context.Context, input Input) ([]Diagnostic, error) {
	if err := integrityInput(ctx, input); err != nil || input.Document == nil {
		return nil, err
	}
	model, err := buildFileAccessModel(ctx, input)
	if err != nil {
		return nil, err
	}
	instructions, err := fileAccessInstructions(ctx, input.Document.Markdown)
	if err != nil {
		return nil, err
	}
	var diagnostics []Diagnostic
	for _, instruction := range instructions {
		if err := ctx.Err(); err != nil {
			return diagnostics, err
		}
		diagnostics = append(diagnostics, model.evaluate(instruction)...)
	}
	return diagnostics, nil
}

func buildFileAccessModel(ctx context.Context, input Input) (*fileAccessModel, error) {
	frontmatter := input.Document.Frontmatter
	pending, err := authoredImportsPending(input)
	if err != nil {
		return nil, err
	}
	model := &fileAccessModel{engine: fileAccessEngine(frontmatter)}

	// FAL-M1: sandbox state.
	if model.sandbox, err = authoredSandboxState(frontmatter, pending); err != nil {
		return nil, err
	}

	safeOutputs, err := policyMapping(frontmatter["safe-outputs"], "safe-outputs")
	if err != nil {
		return nil, err
	}
	prWriters := pullRequestWriters(safeOutputs)

	// FAL-M2..M4: effective edit and shell write capability.
	if input.ResolvedTools != nil {
		model.write, model.writeSource, err = resolvedWriteCapability(ctx, input.ResolvedTools)
	} else {
		tools, mapErr := policyMapping(frontmatter["tools"], "tools")
		if mapErr != nil {
			return nil, mapErr
		}
		model.write, model.writeSource, err = authoredWriteCapability(ctx, tools, model.sandbox, len(prWriters) > 0, pending)
	}
	if err != nil {
		return nil, err
	}

	// FAL-M5: memory mounts. Compiler-resolved tools include imports.
	if err := model.applyMemory(input, pending); err != nil {
		return nil, err
	}

	// FAL-M6: protected files. Imports may add a permissive PR writer.
	if !pending {
		if err := model.applyProtectedFiles(ctx, prWriters); err != nil {
			return nil, err
		}
	}
	return model, nil
}

// applyMemory records configured cache-memory and repo-memory mounts.
func (model *fileAccessModel) applyMemory(input Input, pending bool) error {
	tools := input.ResolvedTools
	memoryPending := false
	var err error
	if tools == nil {
		if tools, err = policyMapping(input.Document.Frontmatter["tools"], "tools"); err != nil {
			return err
		}
		memoryPending = pending
	}
	model.cacheMemory, model.cacheMemoryKnown, err = cacheMemoryIDs(tools["cache-memory"], memoryPending)
	if err != nil {
		return err
	}
	switch value, exists := tools["repo-memory"]; {
	case exists && value != false:
		model.repoMemory = stateEnabled
	case memoryPending:
		model.repoMemory = stateUnknown
	default:
		model.repoMemory = stateDisabled
	}
	return nil
}

// applyProtectedFiles records whether every pull-request writer blocks protected files.
func (model *fileAccessModel) applyProtectedFiles(ctx context.Context, prWriters []pullRequestWriter) error {
	if len(prWriters) == 0 {
		return nil
	}
	model.protectedBlocked = true
	for _, writer := range prWriters {
		source := "safe-outputs." + writer.name + ".protected-files"
		policy, exclude, err := protectedFilesPolicy(ctx, writer.config, source)
		if err != nil {
			return err
		}
		if policy != "blocked" {
			model.protectedBlocked = false
			return nil
		}
		model.protectedExclude = append(model.protectedExclude, exclude...)
		if model.protectedSource == "" {
			model.protectedSource = source
		}
	}
	return nil
}

func fileAccessEngine(frontmatter map[string]any) string {
	switch engine := frontmatter["engine"].(type) {
	case string:
		return engine
	case map[string]any:
		if id, ok := engine["id"].(string); ok {
			return id
		}
	}
	return "copilot"
}

func authoredSandboxState(frontmatter map[string]any, pending bool) (triState, error) {
	unset := stateEnabled
	if pending {
		unset = stateUnknown
	}
	switch sandbox := frontmatter["sandbox"].(type) {
	case nil:
		return unset, nil
	case string:
		return stateEnabled, nil
	case map[string]any:
		switch agent := sandbox["agent"].(type) {
		case bool:
			if !agent {
				return stateDisabled, nil
			}
			return stateEnabled, nil
		case nil:
			if _, exists := sandbox["agent"]; exists {
				return stateEnabled, nil
			}
			return unset, nil
		default:
			return stateEnabled, nil
		}
	default:
		return stateUnknown, errors.New("file-access requires sandbox to be a string or mapping")
	}
}

type pullRequestWriter struct {
	name   string
	config any
}

func pullRequestWriters(safeOutputs map[string]any) []pullRequestWriter {
	var writers []pullRequestWriter
	for _, name := range []string{"create-pull-request", "push-to-pull-request-branch"} {
		if value, exists := safeOutputs[name]; exists && value != false {
			writers = append(writers, pullRequestWriter{name: name, config: value})
		}
	}
	return writers
}

func protectedFilesPolicy(ctx context.Context, config any, location string) (string, []string, error) {
	fields, ok := config.(map[string]any)
	if !ok {
		return "request-review", nil, nil
	}
	switch value := fields["protected-files"].(type) {
	case nil:
		return "request-review", nil, nil
	case string:
		return value, nil, nil
	case map[string]any:
		policy := "request-review"
		if authored, ok := value["policy"].(string); ok && authored != "" {
			policy = authored
		}
		var exclude []string
		if raw, exists := value["exclude"]; exists && raw != nil {
			var err error
			if exclude, err = policyStrings(ctx, raw, location+".exclude"); err != nil {
				return "", nil, err
			}
		}
		return policy, exclude, nil
	default:
		return "", nil, fmt.Errorf("file-access requires a string or mapping at %s", location)
	}
}

func cacheMemoryIDs(raw any, pending bool) (map[string]struct{}, bool, error) {
	ids := make(map[string]struct{})
	switch config := raw.(type) {
	case nil:
		return ids, !pending, nil
	case bool:
		if config {
			ids["default"] = struct{}{}
		}
		return ids, !pending || config, nil
	case map[string]any:
		ids[cacheMemoryEntryID(config)] = struct{}{}
	case []any:
		for _, entry := range config {
			fields, ok := entry.(map[string]any)
			if !ok {
				return nil, false, errors.New("file-access requires tools.cache-memory list entries to be mappings")
			}
			ids[cacheMemoryEntryID(fields)] = struct{}{}
		}
	default:
		ids["default"] = struct{}{}
	}
	// Explicit configuration is authoritative only for the IDs it names;
	// imports may still add other caches.
	return ids, !pending, nil
}

func cacheMemoryEntryID(fields map[string]any) string {
	if id, ok := fields["id"].(string); ok && id != "" {
		return id
	}
	return "default"
}

func resolvedWriteCapability(ctx context.Context, tools map[string]any) (triState, RelatedLocation, error) {
	if edit, exists := tools["edit"]; exists && edit != false {
		return stateEnabled, RelatedLocation{}, nil
	}
	writes, err := shellCanWrite(ctx, tools["bash"], "tools.bash")
	if err != nil || writes {
		return stateEnabled, RelatedLocation{}, err
	}
	return stateDisabled, RelatedLocation{Path: "tools.edit", Symbol: "edit"}, nil
}

// authoredWriteCapability mirrors the compiler's default tool resolution:
// the sandbox enables edit and unrestricted bash; PR safe outputs add edit and
// Git commands. Explicit declarations take precedence over both defaults.
func authoredWriteCapability(ctx context.Context, tools map[string]any, sandbox triState, prWriter, pending bool) (triState, RelatedLocation, error) {
	edit, editDeclared := tools["edit"]
	if editDeclared && edit != false {
		return stateEnabled, RelatedLocation{}, nil
	}
	if !editDeclared && (sandbox == stateEnabled || prWriter) {
		return stateEnabled, RelatedLocation{}, nil
	}
	bash, bashDeclared := tools["bash"]
	if !bashDeclared && sandbox == stateEnabled {
		return stateEnabled, RelatedLocation{}, nil
	}
	// nil keeps the default safe commands, none of which write; false removes bash.
	writes, err := shellCanWrite(ctx, bash, "tools.bash")
	if err != nil || writes {
		return stateEnabled, RelatedLocation{}, err
	}
	if pending || sandbox == stateUnknown {
		return stateUnknown, RelatedLocation{}, nil
	}
	if editDeclared {
		return stateDisabled, RelatedLocation{Path: "tools.edit", Symbol: "edit"}, nil
	}
	return stateDisabled, RelatedLocation{Path: "sandbox.agent", Symbol: "edit"}, nil
}

func shellCanWrite(ctx context.Context, raw any, location string) (bool, error) {
	switch value := raw.(type) {
	case nil, bool:
		return value == true, nil
	}
	commands, err := policyStrings(ctx, raw, location)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(commands, shellCommandWrites), nil
}

func shellCommandWrites(command string) bool {
	command = strings.TrimSpace(command)
	if command == "*" || command == ":*" {
		return true
	}
	head, rest, _ := strings.Cut(command, " ")
	stem := strings.TrimSuffix(head, ":*")
	if stem == "git" {
		// Only unrestricted Git or git apply writes arbitrary content.
		rest = strings.TrimSpace(rest)
		return rest == "" || strings.HasPrefix(rest, "*") || strings.HasPrefix(rest, "apply")
	}
	return slices.Contains(writeCapableCommands, stem)
}

// evaluate applies FAL-R1..R5 to one instruction.
func (model *fileAccessModel) evaluate(instruction fileAccessInstruction) []Diagnostic {
	var diagnostics []Diagnostic
	report := func(message string, related ...RelatedLocation) {
		diagnostics = append(diagnostics, Diagnostic{Path: "prompt", Line: instruction.line, Message: message, Related: related})
	}
	// FAL-R1: writes require edit or a write-capable shell command.
	if instruction.write && !instruction.shell && model.write == stateDisabled {
		report("Prompt asks the agent to write files, but neither the edit tool nor a write-capable bash command is available; enable tools.edit or remove the instruction",
			model.writeSource)
	}
	if model.sandbox == stateEnabled {
		// FAL-R2: system paths are mounted read-only inside the sandbox.
		for _, target := range instruction.targets {
			if prefix := readOnlySystemPrefix(target); prefix != "" {
				report(fmt.Sprintf("Prompt asks the agent to write under %s, which the agent sandbox mounts read-only; write under the workspace or /tmp instead", prefix),
					RelatedLocation{Path: "sandbox.agent", Symbol: "read-only-mount"})
				break
			}
		}
		// FAL-R3: the Docker socket is hidden inside the sandbox.
		if instruction.docker {
			report("Prompt asks the agent to use Docker, but the agent sandbox hides the Docker socket; run containers in a custom job or setup step instead",
				RelatedLocation{Path: "sandbox.agent", Symbol: "docker-socket"})
		}
	}
	// FAL-R4: memory directories exist only when configured.
	for _, id := range instruction.cacheMemoryIDs {
		if _, configured := model.cacheMemory[id]; model.cacheMemoryKnown && !configured {
			location := "tools.cache-memory"
			if id == "default" {
				report("Prompt uses /tmp/gh-aw/cache-memory/, but tools.cache-memory is not configured; enable cache-memory or remove the path", RelatedLocation{Path: location, Symbol: id})
			} else {
				report(fmt.Sprintf("Prompt uses /tmp/gh-aw/cache-memory-%s/, but no tools.cache-memory entry has id %s; add the entry or correct the path", id, id), RelatedLocation{Path: location, Symbol: id})
			}
		}
	}
	if instruction.repoMemory && model.repoMemory == stateDisabled {
		report("Prompt uses /tmp/gh-aw/repo-memory, but tools.repo-memory is not configured; enable repo-memory or remove the path",
			RelatedLocation{Path: "tools.repo-memory", Symbol: "repo-memory"})
	}
	// FAL-R5: protected files fail when every PR writer blocks them.
	if model.protectedBlocked {
		for _, target := range instruction.targets {
			if protectedFile(target, model.engine, model.protectedExclude) {
				report(fmt.Sprintf("Prompt asks the agent to modify protected file %s, but protected-files is blocked for every pull-request safe output; the change will be rejected", target),
					RelatedLocation{Path: model.protectedSource, Symbol: "blocked"})
				break
			}
		}
	}
	return diagnostics
}

func readOnlySystemPrefix(target string) string {
	if !strings.HasPrefix(target, "/") {
		return ""
	}
	cleaned := path.Clean(target)
	for _, prefix := range readOnlySystemPrefixes {
		if cleaned == prefix || strings.HasPrefix(cleaned, prefix+"/") {
			return prefix
		}
	}
	return ""
}

var protectedManifests = []string{
	"package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "npm-shrinkwrap.json",
	"bun.lockb", "bunfig.toml", "deno.json", "deno.jsonc", "deno.lock", "go.mod", "go.sum",
	"requirements.txt", "Pipfile", "Pipfile.lock", "pyproject.toml", "setup.py", "setup.cfg", "uv.lock",
	"Gemfile", "Gemfile.lock", "pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle",
	"settings.gradle.kts", "gradle.properties", "mix.exs", "mix.lock", "stack.yaml", "stack.yaml.lock",
	"global.json", "NuGet.Config", "Directory.Packages.props", "CODEOWNERS", "DESIGN.md",
}

var protectedTopLevelDocs = []string{"README.md", "CONTRIBUTING.md", "SECURITY.md", "CODE_OF_CONDUCT.md"}

// protectedFile mirrors the default protected set for repository-relative paths.
func protectedFile(target, engine string, exclude []string) bool {
	relative, ok := workspaceRelativePath(target)
	if !ok {
		return false
	}
	for _, entry := range exclude {
		entry = strings.TrimPrefix(entry, "./")
		if relative == entry || path.Base(relative) == entry || strings.HasSuffix(entry, "/") && strings.HasPrefix(relative, entry) {
			return false
		}
	}
	base := path.Base(relative)
	top, _, nested := strings.Cut(relative, "/")
	switch {
	case slices.Contains(protectedManifests, base):
		return true
	case !nested && slices.Contains(protectedTopLevelDocs, base):
		return true
	case nested && strings.HasPrefix(top, "."):
		// Covers .github/, .agents/, .githooks/, .husky/, .claude/ and .codex/.
		return true
	}
	switch engine {
	case "claude":
		return base == "CLAUDE.md"
	case "copilot", "codex":
		return base == "AGENTS.md"
	}
	return false
}

func workspaceRelativePath(target string) (string, bool) {
	for _, prefix := range []string{"$GITHUB_WORKSPACE/", "${GITHUB_WORKSPACE}/", "${{ github.workspace }}/"} {
		target = strings.TrimPrefix(target, prefix)
	}
	if strings.HasPrefix(target, "/") || strings.HasPrefix(target, "~") || strings.HasPrefix(target, "$") {
		return "", false
	}
	cleaned := path.Clean(target)
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", false
	}
	return cleaned, true
}
