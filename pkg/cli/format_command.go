package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var formatLog = logger.New("cli:format_command")

// Re-encoding reaches a fixed point after YAML node styles and comments are normalized.
const maxYAMLFormattingPasses = 10

// NewFormatCommand creates the format command.
func NewFormatCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "format [workflow]...",
		Short: "Apply codemods and normalize agentic workflow frontmatter",
		Long: `Apply all available codemods and normalize YAML frontmatter in agentic workflow files.

Formatting uses two-space indentation and deterministic field ordering while retaining comments
and Markdown content. If no workflows are specified, all Markdown files in .github/workflows are
formatted.

` + WorkflowIDExplanation,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` format
  ` + string(constants.CLIExtensionPrefix) + ` format my-workflow
  ` + string(constants.CLIExtensionPrefix) + ` format --dir custom/workflows`,
		RunE: func(cmd *cobra.Command, args []string) error {
			workflowDir, _ := cmd.Flags().GetString("dir")
			verbose, _ := cmd.Flags().GetBool("verbose")
			return runFormatCommand(args, workflowDir, verbose)
		},
	}

	cmd.Flags().StringP("dir", "d", "", "Workflow directory (default: $GH_AW_WORKFLOWS_DIR or .github/workflows)")
	cmd.ValidArgsFunction = CompleteWorkflowNames
	RegisterDirFlagCompletion(cmd, "dir")
	return cmd
}

func runFormatCommand(workflowIDs []string, workflowDir string, verbose bool) error {
	formatLog.Printf("Formatting workflows: workflowIDs=%v, workflowDir=%s", workflowIDs, workflowDir)
	files, err := resolveFormatWorkflowFiles(workflowIDs, workflowDir, verbose)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No workflow files found."))
		return nil
	}

	codemods := GetAllCodemods()
	for _, file := range files {
		if err := formatWorkflowFile(file, codemods, verbose); err != nil {
			return fmt.Errorf("failed to format %s: %w", filepath.Base(file), err)
		}
	}
	return nil
}

func resolveFormatWorkflowFiles(workflowIDs []string, workflowDir string, verbose bool) ([]string, error) {
	if workflowDir == "" {
		workflowDir = constants.GetWorkflowDir()
	} else {
		workflowDir = filepath.Clean(workflowDir)
	}

	if len(workflowIDs) == 0 {
		files, err := getMarkdownWorkflowFiles(workflowDir)
		if err != nil {
			return nil, err
		}
		return filterMarkdownFilesWithFrontmatter(files)
	}

	files := make([]string, 0, len(workflowIDs))
	for _, workflowID := range workflowIDs {
		file, err := resolveWorkflowFileInDir(workflowID, verbose, workflowDir)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

func formatWorkflowFile(filePath string, codemods []Codemod, verbose bool) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	formatted, appliedCodemods, err := formatWorkflowContentWithInfo(string(content), filePath, codemods)
	if err != nil {
		return err
	}
	if formatted == string(content) {
		console.LogVerbose(verbose, filepath.Base(filePath)+" is already formatted")
		return nil
	}

	if err := os.WriteFile(filePath, []byte(formatted), constants.FilePermSensitive); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	if err := scaffoldSerenaSharedWorkflowIfNeeded(filePath, appliedCodemods, formatted, verbose); err != nil {
		return fmt.Errorf("failed to scaffold shared Serena workflow: %w", err)
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(filepath.Base(filePath)))
	return nil
}

func formatWorkflowContentWithInfo(content, filePath string, codemods []Codemod) (string, []string, error) {
	fixed, appliedCodemods, err := applyFormatCodemods(content, filePath, codemods)
	if err != nil {
		return "", nil, err
	}
	formatted, err := normalizeFrontmatter(fixed)
	return formatted, appliedCodemods, err
}

func applyFormatCodemods(content, filePath string, codemods []Codemod) (string, []string, error) {
	currentContent := content
	var appliedCodemods []string

	for _, codemod := range codemods {
		currentResult, err := parser.ExtractFrontmatterFromContent(currentContent)
		if err != nil {
			return "", nil, err
		}

		var newContent string
		var applied bool
		if codemod.ApplyWithContext != nil {
			newContent, applied, err = codemod.ApplyWithContext(currentContent, currentResult.Frontmatter, filePath)
		} else {
			newContent, applied, err = codemod.Apply(currentContent, currentResult.Frontmatter)
		}
		if err != nil {
			wrappedErr := fmt.Errorf("codemod %s failed: %w", codemod.ID, err)
			if codemod.Guided {
				return "", nil, &GuidedError{Cause: wrappedErr}
			}
			return "", nil, wrappedErr
		}
		if applied {
			currentContent = newContent
			appliedCodemods = append(appliedCodemods, codemod.Name)
		}
	}

	return currentContent, appliedCodemods, nil
}

func normalizeFrontmatter(content string) (string, error) {
	frontmatter, suffix, err := splitFrontmatterForFormatting(content)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(frontmatter) == "" {
		return "---\n---" + suffix, nil
	}

	var document yaml.Node
	decoder := yaml.NewDecoder(strings.NewReader(frontmatter))
	if err := decoder.Decode(&document); err != nil {
		return "", fmt.Errorf("failed to parse frontmatter: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return "", errors.New("frontmatter must contain a single YAML document")
		}
		return "", fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	if len(document.Content) == 0 {
		return "---\n---" + suffix, nil
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return "", errors.New("frontmatter must be a YAML mapping")
	}
	orderYAMLMapping(root, constants.PriorityWorkflowFields)

	formattedYAML := ""
	for range maxYAMLFormattingPasses {
		next, err := encodeYAMLDocument(&document)
		if err != nil {
			return "", err
		}
		if next == formattedYAML {
			return "---\n" + formattedYAML + "---" + suffix, nil
		}
		formattedYAML = next
		var normalizedDocument yaml.Node
		if err := yaml.Unmarshal([]byte(formattedYAML), &normalizedDocument); err != nil {
			return "", fmt.Errorf("failed to parse encoded frontmatter: %w", err)
		}
		document = normalizedDocument
	}
	return "", fmt.Errorf("frontmatter formatting did not stabilize after %d passes", maxYAMLFormattingPasses)
}

func encodeYAMLDocument(document *yaml.Node) (string, error) {
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(document); err != nil {
		return "", fmt.Errorf("failed to encode frontmatter: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("failed to encode frontmatter: %w", err)
	}

	formattedYAML := output.String()
	formattedYAML = strings.TrimPrefix(formattedYAML, "---\n")
	formattedYAML = strings.TrimSuffix(formattedYAML, "...\n")
	if formattedYAML != "" && !strings.HasSuffix(formattedYAML, "\n") {
		formattedYAML += "\n"
	}
	return formattedYAML, nil
}

func splitFrontmatterForFormatting(content string) (string, string, error) {
	firstNewline := strings.IndexByte(content, '\n')
	if firstNewline < 0 || strings.TrimSpace(content[:firstNewline]) != "---" {
		return "", "", errors.New("workflow does not contain YAML frontmatter")
	}

	for start := firstNewline + 1; start <= len(content); {
		end := strings.IndexByte(content[start:], '\n')
		if end < 0 {
			end = len(content)
		} else {
			end += start
		}
		line := strings.TrimSuffix(content[start:end], "\r")
		if line == "---" {
			return content[firstNewline+1 : start], content[end:], nil
		}
		if end == len(content) {
			break
		}
		start = end + 1
	}
	return "", "", errors.New("frontmatter not properly closed")
}

func orderYAMLMapping(node *yaml.Node, priorityFields []string) {
	if node.Kind == yaml.MappingNode {
		type pair struct {
			key   *yaml.Node
			value *yaml.Node
		}
		pairs := make([]pair, 0, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			pairs = append(pairs, pair{key: node.Content[i], value: node.Content[i+1]})
		}
		slices.SortStableFunc(pairs, func(a, b pair) int {
			return compareYAMLKeys(a.key.Value, b.key.Value, priorityFields)
		})
		node.Content = node.Content[:0]
		for _, item := range pairs {
			node.Content = append(node.Content, item.key, item.value)
		}
	}

	for _, child := range node.Content {
		orderYAMLMapping(child, nil)
	}
}

func compareYAMLKeys(a, b string, priorityFields []string) int {
	if len(priorityFields) > 0 {
		aIndex := slices.Index(priorityFields, a)
		bIndex := slices.Index(priorityFields, b)
		switch {
		case aIndex >= 0 && bIndex >= 0:
			return aIndex - bIndex
		case aIndex >= 0:
			return -1
		case bIndex >= 0:
			return 1
		}
	}
	return strings.Compare(a, b)
}
