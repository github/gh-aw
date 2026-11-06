//go:build !js && !wasm

package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/sliceutil"
	"go.yaml.in/yaml/v3"
)

func (c *Compiler) generateDependabotConfigForDirectories(filename string, directories map[string][]string) error {
	root, err := readDependabotNode(filename)
	if err != nil {
		return err
	}
	updates := dependabotNodeField(root, "updates")
	if updates == nil {
		updates = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		root.Content = append(root.Content, dependabotScalar("updates"), updates)
	} else if updates.Kind != yaml.SequenceNode {
		return errors.New("dependabot.yml: updates must be an array")
	}
	changed := false
	for _, ecosystem := range sliceutil.SortedKeys(directories) {
		for _, directory := range directories[ecosystem] {
			covered, err := dependabotDirectoryCovered(updates, ecosystem, directory)
			if err != nil {
				return err
			}
			if covered {
				continue
			}
			entry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
				dependabotScalar("package-ecosystem"), dependabotScalar(ecosystem),
				dependabotScalar("directory"), dependabotScalar(directory),
				dependabotScalar("schedule"), {
					Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
						dependabotScalar("interval"), dependabotScalar("weekly"),
					},
				},
			}}
			updates.Content = append(updates.Content, entry)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	content, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("failed to marshal dependabot.yml: %w", err)
	}
	if err := os.WriteFile(filename, content, constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write dependabot.yml: %w", err)
	}
	if c.fileTracker != nil {
		c.fileTracker.TrackCreated(filename)
	}
	if c.verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Updated .github/dependabot.yml"))
	}
	return nil
}

func readDependabotNode(filename string) (*yaml.Node, error) {
	content, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
			dependabotScalar("version"), {Kind: yaml.ScalarNode, Tag: "!!int", Value: "2"},
		}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read dependabot.yml: %w", err)
	}
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("failed to parse dependabot.yml: %w", err)
	}
	var fields map[string]any
	if err := document.Decode(&fields); err != nil {
		return nil, fmt.Errorf("invalid dependabot.yml mapping: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("dependabot.yml must contain a single document")
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode { //nolint:uncheckedsliceindex // Short-circuiting proves the single root exists.
		return nil, errors.New("dependabot.yml must contain a mapping")
	}
	root := document.Content[0] //nolint:uncheckedsliceindex // The document was verified to have exactly one root above.
	version := dependabotNodeField(root, "version")
	if version == nil || version.Value != "2" {
		return nil, errors.New("dependabot.yml requires version: 2")
	}
	return root, nil
}

func dependabotNodeField(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key { //nolint:uncheckedsliceindex // The loop bounds check both mapping nodes.
			return node.Content[i+1] //nolint:uncheckedsliceindex // The loop bounds check both mapping nodes.
		}
	}
	return nil
}

func dependabotScalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func dependabotDirectoryCovered(updates *yaml.Node, ecosystem, directory string) (bool, error) {
	for _, entry := range updates.Content {
		var update struct {
			Ecosystem   string   `yaml:"package-ecosystem"`
			Directory   string   `yaml:"directory"`
			Directories []string `yaml:"directories"`
		}
		if err := entry.Decode(&update); err != nil {
			return false, fmt.Errorf("dependabot.yml: invalid update entry: %w", err)
		}
		if update.Ecosystem != ecosystem {
			continue
		}
		if update.Directory == directory {
			return true, nil
		}
		for _, pattern := range update.Directories {
			matched, err := doublestar.Match(pattern, directory)
			if err != nil {
				return false, fmt.Errorf("dependabot.yml: invalid directories pattern %q: %w", pattern, err)
			}
			if matched {
				return true, nil
			}
		}
	}
	return false, nil
}
