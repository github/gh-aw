//go:build !js && !wasm

package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var dependabotLog = logger.New("workflow:dependabot")

const managedDependabotIgnoreComment = "Managed by gh aw compile. Version-locked to the gh-aw compiler; do not bump."

const dependabotConfigRelativePath = ".github/dependabot.yml"

// PackageJSON represents the structure of a package.json file
type PackageJSON struct {
	Name            string            `json:"name"`
	Private         bool              `json:"private"`
	License         string            `json:"license,omitempty"`
	Dependencies    map[string]string `json:"dependencies,omitempty"`
	DevDependencies map[string]string `json:"devDependencies,omitempty"`
}

// DependabotConfig represents the structure of .github/dependabot.yml
type DependabotConfig struct {
	Version int                     `yaml:"version"`
	Updates []DependabotUpdateEntry `yaml:"updates"`
}

// DependabotUpdateEntry represents a single update configuration in dependabot.yml
type DependabotUpdateEntry struct {
	PackageEcosystem string `yaml:"package-ecosystem"`
	Directory        string `yaml:"directory"`
	Schedule         struct {
		Interval string `yaml:"interval"`
	} `yaml:"schedule"`
}

// NpmDependency represents a parsed npm package with version
type NpmDependency struct {
	Name    string
	Version string // semver range or specific version
}

// PipDependency represents a parsed pip package with version
type PipDependency struct {
	Name    string
	Version string // version specifier (e.g., ==1.0.0, >=2.0.0)
}

// GoDependency represents a parsed Go package
type GoDependency struct {
	Path    string // import path (e.g., github.com/user/repo)
	Version string // version or pseudo-version
}

// GenerateDependabotManifests generates manifest files and dependabot.yml for detected dependencies
func (c *Compiler) GenerateDependabotManifests(ctx context.Context, workflowDataList []*WorkflowData, workflowDir string, forceOverwrite bool) error {
	dependabotLog.Print("Starting Dependabot manifest generation")

	// Track which ecosystems have dependencies
	ecosystems := make(map[string]struct {
	})

	if added, err := c.generateNpmManifests(ctx, workflowDataList, workflowDir, forceOverwrite); err != nil {
		return err
	} else if added {
		ecosystems["npm"] = struct{}{}
	}

	if added, err := c.generatePipManifests(workflowDataList, workflowDir, forceOverwrite); err != nil {
		return err
	} else if added {
		ecosystems["pip"] = struct{}{}
	}

	if added, err := c.generateGoManifests(workflowDataList, workflowDir, forceOverwrite); err != nil {
		return err
	} else if added {
		ecosystems["gomod"] = struct{}{}
	}

	// If no dependencies found at all, skip
	if len(ecosystems) == 0 {
		dependabotLog.Print("No dependencies found, skipping manifest generation")
		if c.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No dependencies detected in workflows, skipping Dependabot manifest generation"))
		}
		return nil
	}

	// Generate dependabot.yml with all detected ecosystems
	dependabotPath := filepath.Join(filepath.Dir(workflowDir), "dependabot.yml")
	if err := c.generateDependabotConfig(dependabotPath, ecosystems, forceOverwrite); err != nil {
		if err := c.handleManifestGenerationError("dependabot.yml", err); err != nil {
			return err
		}
	}

	if c.verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Successfully generated Dependabot manifests"))
	}

	return nil
}

// handleManifestGenerationError reports a manifest generation failure, returning an error in
// strict mode or recording a warning and returning nil otherwise.
func (c *Compiler) handleManifestGenerationError(manifestName string, err error) error {
	if c.strictMode {
		return fmt.Errorf("failed to generate %s: %w", manifestName, err)
	}
	c.IncrementWarningCount()
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to generate %s: %v", manifestName, err)))
	return nil
}
