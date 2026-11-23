package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// Sources are scoped to one scanner result, never shared across compilation.
type scannerSources map[string]*scannerSource

type scannerSource struct {
	lines     []string
	offset    int
	digest    string
	locations []scannerSourceLocation
	steps     map[string]string
	nodes     map[string]string
}

type scannerSourceLocation struct {
	path        string
	value       any
	digest      string
	line        int
	column      int
	endLine     int
	endColumn   int
	contentLine int
	run         bool
}

func isScannerWorkflowFile(file string) bool {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".yml", ".yaml", ".md":
		return true
	default:
		return false
	}
}

func (sources scannerSources) load(file string) *scannerSource {
	if !isScannerWorkflowFile(file) {
		return nil
	}
	if source, found := sources[file]; found {
		return source
	}
	sources[file] = nil
	content, err := os.ReadFile(file)
	if err != nil {
		compileDevelopmentLog.Print("Scanner source unavailable; retaining coordinate identity")
		return nil
	}
	source := &scannerSource{
		digest: scannerValueDigest(string(content)), steps: make(map[string]string), nodes: make(map[string]string),
	}
	sources[file] = source
	if err := source.parse(file, content); err != nil {
		// Invalid workflows still produce scanner findings. Do not match them
		// structurally or expose source values in a parse-error diagnostic.
		source.locations = nil
		source.nodes = nil
		compileDevelopmentLog.Print("Scanner AST unavailable; retaining content and coordinate identity")
	}
	return source
}

func (source *scannerSource) parse(file string, content []byte) error {
	var root any
	var err error
	if strings.EqualFold(filepath.Ext(file), ".md") {
		content, root, err = source.indexAW(content)
	} else {
		root, err = source.indexActions(content)
	}
	if err != nil {
		return err
	}
	source.lines = strings.Split(string(content), "\n")
	syntax, err := parser.ParseBytes(content, 0)
	if err != nil {
		return err
	}
	for _, doc := range syntax.Docs {
		if err := source.indexNode(doc.Body, "$", root); err != nil {
			return err
		}
	}
	return nil
}

func (source *scannerSource) indexAW(content []byte) ([]byte, any, error) {
	document, err := aw.Parse(string(content))
	if err != nil {
		return nil, nil, err
	}
	blocks, err := aw.ExecutionBlocks(context.Background(), document)
	if err != nil {
		return nil, nil, err
	}
	for _, block := range blocks {
		for _, field := range aw.StepSections {
			var fields []map[string]any
			for _, step := range block.Steps {
				if step.Section == field {
					fields = append(fields, step.Fields)
				}
			}
			path := scannerAWBlockPath(block.Path)
			if err := source.indexSteps(scannerChildPath(path, field), fields); err != nil {
				return nil, nil, err
			}
		}
	}
	source.offset = max(document.Location.FrontmatterStart-1, 0)
	return []byte(strings.Join(document.Location.FrontmatterLines, "\n")), document.Frontmatter, nil
}

func scannerAWBlockPath(block string) string {
	path := "$"
	if block == "safe-outputs" {
		return scannerChildPath(path, block)
	}
	for _, section := range []string{"safe-outputs.jobs", "safe-jobs", "jobs"} {
		if id, found := strings.CutPrefix(block, section+"."); found {
			for key := range strings.SplitSeq(section, ".") {
				path = scannerChildPath(path, key)
			}
			return scannerChildPath(path, id)
		}
	}
	return path
}

func (source *scannerSource) indexActions(content []byte) (yaml.MapSlice, error) {
	document, err := actions.Parse(content)
	if err != nil {
		return nil, err
	}
	for _, job := range document.Jobs {
		fields := make([]map[string]any, 0, len(job.Steps))
		for _, step := range job.Steps {
			raw, ok := step.(actions.RawStep)
			if !ok {
				return nil, errors.New("parsed scanner step is not an authored step")
			}
			fields = append(fields, raw.Fields)
		}
		path := scannerChildPath(scannerChildPath(scannerChildPath("$", "jobs"), string(job.ID)), "steps")
		if err := source.indexSteps(path, fields); err != nil {
			return nil, err
		}
	}
	return actions.Lower(document)
}

func scannerChildPath(parent, key string) string {
	return parent + strings.TrimPrefix((&yaml.PathBuilder{}).Root().Child(key).Build().String(), "$")
}

func scannerValueDigest(value string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

func (source *scannerSource) indexSteps(path string, steps []map[string]any) error {
	names := make(map[string]int)
	for _, step := range steps {
		if name, ok := step["name"].(string); ok {
			names[name]++
		}
	}
	for index, step := range steps {
		content, err := json.Marshal(step)
		if err != nil {
			return fmt.Errorf("fingerprinting scanner step: %w", err)
		}
		identity := "content:" + scannerValueDigest(string(content))
		if id, ok := step["id"].(string); ok && id != "" {
			identity = "id:" + scannerValueDigest(id)
		} else if name, ok := step["name"].(string); ok && name != "" && names[name] == 1 {
			identity = "name:" + scannerValueDigest(name)
		}
		source.steps[fmt.Sprintf("%s[%d]", path, index)] = path + "[" + identity + "]"
	}
	return nil
}

func (source *scannerSource) indexNode(node ast.Node, path string, semantic any) error {
	content, err := json.Marshal(semantic)
	if err != nil {
		return fmt.Errorf("fingerprinting scanner scope: %w", err)
	}
	source.nodes[path] = scannerValueDigest(string(content))
	switch value := node.(type) {
	case *ast.MappingNode:
		for _, field := range value.Values {
			if err := source.indexNode(field, path, semantic); err != nil {
				return err
			}
		}
	case *ast.MappingValueNode:
		key, ok := value.Key.(ast.ScalarNode)
		if !ok {
			return errors.New("scanner mapping key is not a scalar")
		}
		name := fmt.Sprint(key.GetValue())
		child := scannerChildPath(path, name)
		field := scannerSourceField(semantic, name)
		if value.Key.IsMergeKey() {
			field = semantic
		}
		if err := source.addLocation(value.Key, field, child, true); err != nil {
			return err
		}
		return source.indexNode(value.Value, child, field)
	case *ast.SequenceNode:
		if strings.HasSuffix(path, ".needs") {
			if err := scannerValidateNeedsSourceOrder(value, semantic); err != nil {
				return err
			}
		}
		for index, child := range value.Values {
			item, err := scannerSourceItem(semantic, index)
			if err != nil {
				return err
			}
			if err := source.indexNode(child, fmt.Sprintf("%s[%d]", path, index), item); err != nil {
				return err
			}
		}
	case *ast.AnchorNode:
		return source.indexNode(value.Value, path, semantic)
	case *ast.TagNode:
		return source.indexNode(value.Value, path, semantic)
	case ast.ScalarNode, *ast.AliasNode:
		return source.addLocation(node, semantic, path, false)
	}
	return nil
}

func scannerValidateNeedsSourceOrder(node *ast.SequenceNode, semantic any) error {
	for index, child := range node.Values {
		scalar, ok := child.(ast.ScalarNode)
		if !ok {
			return errors.New("scanner Actions needs sequence cannot be matched to source order")
		}
		item, err := scannerSourceItem(semantic, index)
		if err != nil {
			return err
		}
		resolved, ok := item.(string)
		if !ok || scalar.GetValue() != resolved {
			return errors.New("scanner Actions needs sequence cannot be matched to source order")
		}
	}
	return nil
}

func scannerSourceField(value any, key string) any {
	switch mapping := value.(type) {
	case map[string]any:
		return mapping[key]
	case yaml.MapSlice:
		for _, field := range mapping {
			if field.Key == key {
				return field.Value
			}
		}
	}
	return nil
}

func scannerSourceItem(value any, index int) (any, error) {
	switch sequence := value.(type) {
	case []any:
		if index >= 0 && index < len(sequence) {
			return sequence[index], nil
		}
	case []map[string]any:
		if index >= 0 && index < len(sequence) {
			return sequence[index], nil
		}
	case []string:
		if index >= 0 && index < len(sequence) {
			return sequence[index], nil
		}
	}
	return nil, errors.New("scanner AST sequence does not match its source")
}

func (source *scannerSource) addLocation(position ast.Node, value any, path string, key bool) error {
	tk := position.GetToken()
	if tk == nil || tk.Position == nil {
		return nil
	}
	content, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("fingerprinting scanner location: %w", err)
	}
	location := scannerSourceLocation{
		path: path, value: value, digest: scannerValueDigest(string(content)), line: tk.Position.Line, column: tk.Position.Column,
		endLine: tk.Position.Line, endColumn: tk.Position.Column + len([]rune(tk.Value)),
	}
	origin := strings.TrimSpace(tk.Origin)
	if origin != "" {
		location.endColumn = scannerTokenEndColumn(origin, location.column)
		if !key {
			location.endLine += strings.Count(origin, "\n")
		}
	}
	if stepPath, ok := strings.CutSuffix(path, ".run"); ok {
		_, location.run = source.steps[stepPath]
	}
	if literal, ok := position.(*ast.LiteralNode); ok {
		location.contentLine = literal.Value.GetToken().Position.Line
		// A block scalar owns only indented body lines, never a following
		// sibling, comment outside the scalar, or the rest of the document.
		header, ok := source.lineAt(location.line - 1)
		if !ok {
			return errors.New("scanner scalar location is outside its source")
		}
		indent := len(header) - len(strings.TrimLeft(header, " "))
		if strings.HasPrefix(strings.TrimSpace(header), "- ") {
			indent += 2
		}
		for index, line := range source.lines {
			if index < location.line {
				continue
			}
			if strings.TrimSpace(line) != "" && len(line)-len(strings.TrimLeft(line, " ")) <= indent {
				break
			}
			location.endLine = index + 1
		}
	}
	source.locations = append(source.locations, location)
	return nil
}

func scannerTokenEndColumn(origin string, startColumn int) int {
	if !strings.Contains(origin, "\n") {
		return startColumn + len([]rune(origin))
	}
	endColumn := 1
	for line := range strings.SplitSeq(origin, "\n") {
		endColumn = len([]rune(line)) + 1
	}
	return endColumn
}

func (source *scannerSource) locate(line, column int) *scannerSourceLocation {
	line -= source.offset
	text, ok := source.lineAt(line - 1)
	if !ok || strings.TrimSpace(text) == "" {
		return nil
	}
	var found *scannerSourceLocation
	ambiguous := false
	for index := range source.locations {
		location := &source.locations[index]
		if line < location.line || line > location.endLine {
			continue
		}
		if line == location.line && column > 0 && (column < location.column || line == location.endLine && column >= location.endColumn) {
			continue
		}
		if found == nil || location.line > found.line || location.line == found.line && location.column > found.column {
			found = location
			ambiguous = false
			continue
		}
		if column <= 0 && location.line == found.line && location.column == found.column && found.path != location.path {
			ambiguous = true
		}
	}
	if ambiguous {
		return nil
	}
	return found
}

func (source *scannerSource) lineAt(index int) (string, bool) {
	if index >= 0 && index < len(source.lines) {
		return source.lines[index], true
	}
	return "", false
}

func (source *scannerSource) pathIdentity(path string) string {
	// Match complete prefixes without interpreting punctuation in mapping keys.
	for prefix, identity := range source.steps {
		if path == prefix || strings.HasPrefix(path, prefix+".") || strings.HasPrefix(path, prefix+"[") {
			return identity + strings.TrimPrefix(path, prefix)
		}
	}
	return path
}

func (source *scannerSource) identity(location *scannerSourceLocation, line int) string {
	path := location.path
	digest := location.digest
	for _, field := range []string{"id", "name", "uses"} {
		if stepPath, found := strings.CutSuffix(path, "."+field); found {
			if _, step := source.steps[stepPath]; step {
				path = stepPath
				digest = source.nodes[stepPath]
				break
			}
		}
	}
	if location.run && location.contentLine > 0 && line-source.offset >= location.contentLine {
		if text, ok := source.lineAt(line - source.offset - 1); ok {
			digest = scannerValueDigest(strings.TrimSpace(text))
		}
	}
	return source.pathIdentity(path) + "\x00" + digest
}
