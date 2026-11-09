package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
)

type GHAWMarkdownMetadataRecord struct {
	SHA256     string            `json:"sha256"`
	Kind       string            `json:"kind"`
	Value      json.RawMessage   `json:"value"`
	References map[string]string `json:"references,omitempty"`
	Error      string            `json:"error,omitempty"`
}

type GHAWMarkdownMetadata struct {
	Version int                          `json:"version"`
	Records []GHAWMarkdownMetadataRecord `json:"records"`
}

// collectMarkdownMetadata records author syntax before expression substitution.
// The source is an exact lookup key, so stale repository files cannot acquire
// metadata by matching only a name or path.
func (c *Compiler) collectMarkdownMetadata(data *WorkflowData) *GHAWMarkdownMetadata {
	records := map[string]GHAWMarkdownMetadataRecord{}
	scan := func(content, documentKind string) {
		scanMarkdownMetadata(records, content, documentKind)
	}
	if data.FrontmatterYAML != "" {
		addMarkdownMetadataRecord(records, data.FrontmatterYAML, "model-pins")
	}
	scan(data.RawMarkdown, "model-pins")
	for _, content := range []string{data.MarkdownContent, data.MainWorkflowMarkdown, data.ImportedMarkdown} {
		scan(content, "artifact")
	}
	for _, content := range data.CompiledPromptMarkdown {
		scan(content, "artifact")
	}
	for _, agent := range data.SubAgents {
		scan(agent.Content, "artifact")
	}
	root := markdownMetadataRoot(c.markdownPath)
	scanRuntimeImportMetadata(root, data, scan)
	scanImportedDocumentMetadata(root, c.markdownPath, data, scan)
	scanRepositoryArtifactMetadata(root, data.Skills, scan)
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := &GHAWMarkdownMetadata{Version: 2, Records: make([]GHAWMarkdownMetadataRecord, 0, len(keys))}
	for _, key := range keys {
		result.Records = append(result.Records, records[key])
	}
	return result
}

func addMarkdownMetadataRecord(records map[string]GHAWMarkdownMetadataRecord, source, kind string) {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	hash := sha256.Sum256([]byte(source))
	digest := hex.EncodeToString(hash[:])
	key := kind + ":" + digest
	if _, exists := records[key]; exists {
		return
	}
	value, err := parser.CompileMarkdownMetadata(source)
	if kind == "model-pins" && err == nil {
		value = projectModelPinMetadata(value)
	}
	encoded, encodeErr := json.Marshal(value)
	record := GHAWMarkdownMetadataRecord{SHA256: digest, Kind: kind, Value: encoded}
	if err != nil {
		record.Error = err.Error()
	} else if encodeErr != nil {
		record.Error = "Markdown metadata is not JSON-compatible"
	}
	records[key] = record
}

func scanMarkdownMetadata(records map[string]GHAWMarkdownMetadataRecord, content, documentKind string) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	for _, block := range parser.ExtractMarkdownMetadataBlocks(content) {
		kind := "artifact"
		if block.Document {
			kind = documentKind
		}
		addMarkdownMetadataRecord(records, block.Source, kind)
		if block.Error != "" {
			hash := sha256.Sum256([]byte(block.Source))
			key := kind + ":" + hex.EncodeToString(hash[:])
			record := records[key]
			record.Error = block.Error
			records[key] = record
		}
	}
}

func markdownMetadataRoot(markdownPath string) string {
	root := filepath.Dir(markdownPath)
	if root == "." || root == "" {
		if cwd, err := os.Getwd(); err == nil {
			root = cwd
		} else {
			workflowLog.Printf("Cannot resolve Markdown metadata working directory: %v", err)
		}
	}
	sourceRoot := root
	for {
		if info, err := os.Stat(filepath.Join(root, constants.GithubDir)); err == nil && info.IsDir() {
			break
		}
		if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			root = sourceRoot
			break
		}
		root = parent
	}
	return root
}

func scanRuntimeImportMetadata(root string, data *WorkflowData, scan func(string, string)) {
	var scanRuntimeImports func(string)
	seenImports := map[string]struct{}{}
	scanRuntimeImports = func(content string) {
		for _, ref := range extractRuntimeImportReferences(removeXMLComments(content)) {
			for _, candidate := range runtimeImportAnalysisCandidatePaths(ref.importPath, root) {
				key := fmt.Sprintf("%s:%d-%d", candidate, ref.startLine, ref.endLine)
				if _, seen := seenImports[key]; seen || !runtimeImportAnalysisRealPathAllowed(candidate, root) {
					continue
				}
				raw, err := parser.ReadFile(candidate)
				if err != nil {
					workflowLog.Printf("Skipping unavailable runtime-import metadata %s: %v", candidate, err)
					continue
				}
				seenImports[key] = struct{}{}
				imported := string(raw)
				if ref.startLine > 0 || ref.endLine > 0 {
					imported = applyRuntimeImportLineRangeForAnalysis(imported, ref.startLine, ref.endLine)
				}
				relative, err := filepath.Rel(root, candidate)
				if err != nil {
					workflowLog.Printf("Cannot resolve runtime-import metadata path %s: %v", candidate, err)
					continue
				}
				slash := strings.TrimPrefix(filepath.ToSlash(relative), constants.GithubDir)
				kind := "model-pins"
				if strings.HasPrefix(slash, "agents/") || strings.HasPrefix(slash, "skills/") || strings.HasPrefix(slash, ".agents/agents/") || strings.HasPrefix(slash, ".agents/skills/") {
					kind = "artifact"
				}
				scan(imported, kind)
				scanRuntimeImports(imported)
				break
			}
		}
	}
	for _, content := range append([]string{data.RawMarkdown, data.MarkdownContent, data.MainWorkflowMarkdown, data.ImportedMarkdown}, data.CompiledPromptMarkdown...) {
		scanRuntimeImports(content)
	}
}

func scanImportedDocumentMetadata(root, markdownPath string, data *WorkflowData, scan func(string, string)) {
	for file, content := range data.ImportedMetadataDocuments {
		kind := "model-pins"
		if strings.Contains(filepath.ToSlash(file), "/agents/") || strings.HasSuffix(file, "SKILL.md") {
			kind = "artifact"
		}
		scan(content, kind)
	}
	for _, imported := range append(append([]string{}, data.ImportedFiles...), data.ImportPaths...) {
		imported, _, _ = strings.Cut(imported, "#")
		candidates := []string{filepath.Join(root, imported), filepath.Join(filepath.Dir(markdownPath), imported)}
		if filepath.IsAbs(imported) {
			candidates = []string{imported}
		}
		for _, file := range candidates {
			if !runtimeImportAnalysisRealPathAllowed(file, root) {
				continue
			}
			if content, err := os.ReadFile(file); err == nil {
				kind := "model-pins"
				if strings.Contains(filepath.ToSlash(file), "/agents/") || strings.HasSuffix(file, "SKILL.md") {
					kind = "artifact"
				}
				scan(string(content), kind)
				break
			} else {
				workflowLog.Printf("Skipping unavailable imported Markdown metadata %s: %v", file, err)
			}
		}
	}
}

func scanRepositoryArtifactMetadata(root string, skills []string, scan func(string, string)) {
	directories := []string{activationSymlinkAgentDir, activationSymlinkSkillDir, ".agents/agents", ".agents/skills", ".claude/agents", ".claude/skills", ".codex/agents", ".codex/skills", ".gemini/agents", ".gemini/skills", ".pi/agents", ".pi/skills", "skills"}
	for _, skill := range skills {
		if spec := parseSkillRefSpec(skill); spec.isLocal {
			directories = append(directories, spec.trimmed)
		}
	}
	for _, directory := range directories {
		err := filepath.WalkDir(filepath.Join(root, directory), func(file string, entry fs.DirEntry, entryErr error) error {
			if entryErr != nil {
				if !errors.Is(entryErr, fs.ErrNotExist) {
					workflowLog.Printf("Skipping unavailable artifact metadata path %s: %v", file, entryErr)
				}
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if entry.IsDir() {
				if entry.Name() == "node_modules" || entry.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(entry.Name(), ".md") {
				if content, err := os.ReadFile(file); err == nil {
					scan(string(content), "artifact")
				} else {
					workflowLog.Printf("Skipping unreadable artifact metadata %s: %v", file, err)
				}
			}
			return nil
		})
		if err != nil {
			workflowLog.Printf("Cannot scan artifact metadata directory %s: %v", directory, err)
		}
	}
}
