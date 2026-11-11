package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
)

const (
	repositoryConnectorDirectory        = constants.WorkflowsDirSlash + "connectors/"
	repositoryConnectorFixtureDirectory = "pkg/cli/workflows"
)

type repositoryConnectorDocument struct {
	path      string
	ids       []string
	imports   []string
	connector bool
	workflow  bool
}

// Discovery inspects local authoring files only. It never resolves remote refs,
// substitutes input values, validates provider access or selects capabilities.
func writeRepositoryConnectorGuidance(output io.Writer, root string) error {
	documents, err := repositoryConnectorDocuments(root)
	if err != nil {
		return err
	}
	var text strings.Builder
	for _, document := range documents {
		if !document.connector {
			continue
		}
		if text.Len() == 0 {
			text.WriteString("\nLocal connector declarations/wrappers (inspect before importing):\n")
		}
		fmt.Fprintf(&text, "  %s", connectorConsoleText(document.path)) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
		if len(document.ids) > 0 {
			fmt.Fprintf(&text, " (ids: %s)", connectorConsoleText(strings.Join(document.ids, ", "))) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
		}
		text.WriteByte('\n')
	}
	var consumers []string
	for _, document := range documents {
		if document.workflow && repositoryUsesConnector(document.path, documents, "", map[string]bool{}) {
			label := document.path
			if strings.HasSuffix(label, "-golden.md") {
				label += " (golden example; replace specimen bindings)"
			}
			consumers = append(consumers, label)
		}
	}
	if len(consumers) > 0 {
		text.WriteString("\nConnector consumer workflows (inspect imports/with and explicit selections):\n")
		for _, consumer := range consumers {
			fmt.Fprintf(&text, "  %s\n", connectorConsoleText(consumer)) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
		}
	}
	if text.Len() == 0 {
		return nil
	}
	const guidance = "\nUse gh aw connector <id> setup --file PATH for declaration-derived phase credentials, scopes, bindings and endpoints.\n" +
		"Use editable local declarations or reviewed pinned imports; no built-in provider catalog or automatic installation.\n" +
		"Importing enables nothing. Select exact reads/native methods and staged safe outputs separately; keep exact approved resource bindings.\n" +
		"Secret names only in with/--input, never values. Token acquisition/refresh and provider permissions remain external.\n" +
		"This discovery did not read/write credentials, change tools/network/grants, or activate webhook ingress.\n"
	_, err = io.WriteString(output, text.String()+guidance)
	return err
}

func repositoryConnectorDocuments(root string) (documents []repositoryConnectorDocument, resultErr error) {
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open connector discovery root: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, dir.Close()) }()
	for _, directory := range []string{constants.WorkflowsDir, repositoryConnectorFixtureDirectory} {
		err = fs.WalkDir(dir.FS(), directory, func(path string, entry fs.DirEntry, walkErr error) error {
			if errors.Is(walkErr, fs.ErrNotExist) && path == directory {
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !entry.Type().IsRegular() || !strings.HasSuffix(path, ".md") {
				return nil
			}
			if directory == repositoryConnectorFixtureDirectory && !strings.HasSuffix(path, "-golden.md") {
				return nil
			}
			content, err := readConnectorSetupFile(dir, path)
			if err != nil {
				return err
			}
			parsed, err := parser.ExtractFrontmatterFromContent(string(content))
			if err != nil {
				return fmt.Errorf("cannot inspect connector setup metadata in %s; correct its frontmatter", connectorConsoleText(path))
			}
			declarations, ok := parsed.Frontmatter["connectors"].(map[string]any)
			if !ok && parsed.Frontmatter["connectors"] != nil {
				return fmt.Errorf("cannot inspect %s: connectors must be an object", connectorConsoleText(path))
			}
			_, workflow := parsed.Frontmatter["on"]
			documents = append(documents, repositoryConnectorDocument{
				path: path, ids: slices.Sorted(maps.Keys(declarations)),
				imports:   repositoryConnectorImportPaths(parsed.Frontmatter["imports"]),
				connector: len(declarations) > 0 || strings.HasPrefix(path, repositoryConnectorDirectory),
				workflow:  workflow,
			})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("discover repository connectors: %w", err)
		}
	}
	return documents, nil
}

func readConnectorSetupFile(root *os.Root, path string) (content []byte, resultErr error) {
	file, err := root.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open connector setup source %s: %w", connectorConsoleText(path), err)
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("connector setup source must be a regular file")
	}
	content, err = io.ReadAll(io.LimitReader(file, connectors.MaxDocumentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > connectors.MaxDocumentBytes {
		return nil, fmt.Errorf("connector setup source %s exceeds 256 KiB", connectorConsoleText(path))
	}
	return content, nil
}

func repositoryConnectorImportPaths(value any) []string {
	if object, ok := value.(map[string]any); ok {
		value = object["aw"]
	}
	var paths []string
	switch entries := value.(type) {
	case []string:
		return entries
	case []any:
		for _, entry := range entries {
			switch imported := entry.(type) {
			case string:
				paths = append(paths, imported)
			case map[string]any:
				if uses, ok := imported["uses"].(string); ok {
					paths = append(paths, uses)
				} else if path, ok := imported["path"].(string); ok {
					paths = append(paths, path)
				}
			}
		}
	}
	return paths
}

func repositoryUsesConnector(path string, documents []repositoryConnectorDocument, id string, visited map[string]bool) bool {
	if visited[path] {
		return false
	}
	visited[path] = true
	for _, document := range documents {
		if document.path != path {
			continue
		}
		if document.connector && (id == "" || slices.Contains(document.ids, id)) {
			return true
		}
		for _, imported := range document.imports {
			if isWorkflowSpecFormat(imported) {
				source, _, _ := strings.Cut(imported, "@")
				if strings.Contains(source, "/connectors/") && (id == "" || filepath.Base(source) == id+".md") {
					return true
				}
				continue
			}
			imported, _, _ = strings.Cut(imported, "#")
			resolved := filepath.Join(filepath.Dir(path), imported)
			if strings.HasPrefix(imported, "/") || strings.HasPrefix(imported, constants.GithubDir) {
				resolved = strings.TrimPrefix(imported, "/")
			}
			if repositoryUsesConnector(filepath.ToSlash(filepath.Clean(resolved)), documents, id, visited) {
				return true
			}
		}
	}
	return false
}

func writeConnectorSetupExamples(output io.Writer, root, id string) error {
	documents, err := repositoryConnectorDocuments(root)
	if err != nil {
		return err
	}
	var text strings.Builder
	for _, document := range documents {
		if !document.workflow || !strings.HasSuffix(document.path, "-golden.md") ||
			!repositoryUsesConnector(document.path, documents, id, map[string]bool{}) {
			continue
		}
		if text.Len() == 0 {
			text.WriteString("\nLocal golden consumers (inspect; replace all specimen resource bindings before use):\n")
		}
		fmt.Fprintf(&text, "  %s\n", connectorConsoleText(document.path)) //nolint:fprintferrorunchecked // strings.Builder cannot fail.
	}
	_, err = io.WriteString(output, text.String())
	return err
}
