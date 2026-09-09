package workflow

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/stringutil"
)

var compilerYamlHeaderLog = logger.New("workflow:compiler_yaml:header")

// generateWorkflowHeader generates the YAML header section including comments
// for description, source, imports/includes, frontmatter-hash, stop-time, and manual-approval.
// All ANSI escape codes are stripped from the output.
// The gh-aw-metadata line is placed first for easy machine parsing.
func (c *Compiler) generateWorkflowHeader(yaml *strings.Builder, data *WorkflowData, frontmatterHash string, bodyHash string, secrets []string, actions []string) error {
	// Skip the ASCII art banner in wasm/editor mode — it takes up too much space
	if c.skipHeader {
		return nil
	}

	// Add lock metadata as the very first line for easy machine parsing.
	// Single-line JSON format to minimize merge conflicts.
	if frontmatterHash != "" {
		agentInfo := AgentMetadataInfo{}
		// Agent ID: prefer EngineConfig.ID, fall back to legacy AI field
		if data.EngineConfig != nil && data.EngineConfig.ID != "" {
			agentInfo.AgentID = data.EngineConfig.ID
		} else if data.AI != "" {
			agentInfo.AgentID = data.AI
		}
		// Agent model: only include if statically configured
		if data.Model != "" {
			agentInfo.AgentModel = data.Model
		}
		if agentInfo.AgentID == "copilot" {
			agentInfo.EngineBaseURLCustomized = isCopilotCustomConfig(data)
		}
		// Detection agent info: only if threat detection has its own engine config
		if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil && data.SafeOutputs.ThreatDetection.EngineConfig != nil {
			agentInfo.DetectionAgentID = data.SafeOutputs.ThreatDetection.EngineConfig.ID
			agentInfo.DetectionAgentModel = data.SafeOutputs.ThreatDetection.Model
		}
		agentInfo.EngineVersions = collectEngineVersionsForMetadata(data, c.engineRegistry)
		agentInfo.AgentImageRunner = resolveAgentImageRunnerIdentifier(data.RawFrontmatter)
		metadata := GenerateLockMetadata(LockHashInfo{FrontmatterHash: frontmatterHash, BodyHash: bodyHash}, data.StopTime, c.effectiveStrictMode(data.RawFrontmatter), agentInfo)
		metadata.Docs = data.Docs
		if metadata.CompilerVersion == "" && c.GetActionTag() != "" {
			metadata.CompilerVersion = c.GetVersion()
		}
		metadataJSON, err := metadata.ToJSON()
		if err != nil {
			// Fallback to legacy format if JSON serialization fails
			fmt.Fprintf(yaml, "# frontmatter-hash: %s\n", frontmatterHash)
		} else {
			fmt.Fprintf(yaml, "# gh-aw-metadata: %s\n", metadataJSON)
		}
	}

	// Embed the gh-aw-manifest immediately after gh-aw-metadata for easy machine parsing.
	// The manifest records all secrets, external actions, container images, and frontmatter
	// skills detected at compile time so that subsequent compilations can perform safe update
	// enforcement.
	manifest := NewGHAWManifest(secrets, actions, data.ActionResolutionFailures, data.DockerImagePins, data.Redirect, data.Skills, data.Plugins, data.RawFrontmatter["on"])
	if data.ParsedFrontmatter != nil {
		manifest.ThreatDetectionSuppressions = data.ParsedFrontmatter.ThreatDetectionSuppressions
	} else {
		suppressions, err := parseThreatDetectionSuppressions(data.RawFrontmatter["threat-detection-suppress"])
		if err != nil {
			return fmt.Errorf("threat-detection-suppress value is not recognized, expected a list of suppression identifiers: %w", err)
		}
		manifest.ThreatDetectionSuppressions = suppressions
	}
	manifest.MemoryValidationScripts = collectMemoryValidationScripts(data)
	manifest.MCPServers = collectMCPServersForManifest(data)
	if manifestJSON, err := manifest.ToJSON(); err == nil {
		fmt.Fprintf(yaml, "# gh-aw-manifest: %s\n", manifestJSON)
	} else {
		compilerYamlHeaderLog.Printf("Failed to serialize gh-aw-manifest: %v. Safe update mode will not be available for future compilations of this workflow.", err)
	}

	// Add workflow header with logo and instructions
	sourceFile := "the corresponding .md file"
	if data.Source != "" {
		sourceFile = data.Source
	}
	header := GenerateWorkflowHeader(sourceFile, "gh-aw", "")
	yaml.WriteString(header)

	// Add description comment if provided
	if data.Description != "" {
		cleanDescription := stringutil.StripANSI(data.Description)
		// Split description into lines and prefix each with "# "
		descriptionLines := strings.SplitSeq(strings.TrimSpace(cleanDescription), "\n")
		for line := range descriptionLines {
			fmt.Fprintf(yaml, "# %s\n", strings.TrimSpace(line))
		}
	}

	// Add intent comment if provided
	if data.Intent != "" {
		if data.Description != "" {
			yaml.WriteString("#\n")
		}
		cleanIntent := stringutil.StripANSI(data.Intent)
		intentLines := strings.SplitSeq(strings.TrimSpace(cleanIntent), "\n")
		first := true
		for line := range intentLines {
			if first {
				fmt.Fprintf(yaml, "# Intent: %s\n", strings.TrimSpace(line))
				first = false
				continue
			}
			// Indent continuation lines so they stay visually attached to the intent block
			fmt.Fprintf(yaml, "#         %s\n", strings.TrimSpace(line))
		}
	}

	// Add source comment if provided
	if data.Source != "" {
		yaml.WriteString("#\n")
		cleanSource := stringutil.StripANSI(data.Source)
		// Normalize to Unix paths (forward slashes) for cross-platform compatibility
		cleanSource = filepath.ToSlash(cleanSource)
		fmt.Fprintf(yaml, "# Source: %s\n", cleanSource)
	}

	// Add manifest of imported/included files if any exist
	// Build a user-visible imports list by filtering out internal builtin engine paths
	// (e.g. "@builtin:engines/copilot.md") which are implementation details.
	var visibleImports []string
	for _, file := range data.ImportedFiles {
		if !strings.HasPrefix(file, parser.BuiltinPathPrefix) {
			visibleImports = append(visibleImports, file)
		}
	}

	if len(visibleImports) > 0 || len(data.IncludedFiles) > 0 {
		yaml.WriteString("#\n")
		yaml.WriteString("# Resolved workflow manifest:\n")

		if len(visibleImports) > 0 {
			yaml.WriteString("#   Imports:\n")
			for _, file := range visibleImports {
				cleanFile := stringutil.StripANSI(file)
				// Normalize to Unix paths (forward slashes) for cross-platform compatibility
				cleanFile = filepath.ToSlash(cleanFile)
				fmt.Fprintf(yaml, "#     - %s\n", cleanFile)
			}
		}

		if len(data.IncludedFiles) > 0 {
			yaml.WriteString("#   Includes:\n")
			for _, file := range data.IncludedFiles {
				cleanFile := stringutil.StripANSI(file)
				// Normalize to Unix paths (forward slashes) for cross-platform compatibility
				cleanFile = filepath.ToSlash(cleanFile)
				fmt.Fprintf(yaml, "#     - %s\n", cleanFile)
			}
		}
	}

	// Add inlined-imports comment to indicate the field was used at compile time
	if data.InlinedImports {
		yaml.WriteString("#\n")
		yaml.WriteString("# inlined-imports: true\n")
	}
	// Add frontmatter-declared env vars with source attribution.
	// Note: programmatically injected env vars (e.g. OTEL_* from OTLP config) are not listed here.
	if len(data.EnvSources) > 0 {
		yaml.WriteString("#\n")
		yaml.WriteString("# Frontmatter env variables:\n")
		// Sort keys for deterministic output
		keys := sliceutil.SortedKeys(data.EnvSources)
		for _, k := range keys {
			fmt.Fprintf(yaml, "#   - %s: %s\n", k, data.EnvSources[k])
		}
	}

	// Add list of secrets referenced in the workflow
	if len(secrets) > 0 {
		yaml.WriteString("#\n")
		yaml.WriteString("# Secrets used:\n")
		for _, s := range secrets {
			fmt.Fprintf(yaml, "#   - %s\n", s)
		}
	}

	// Add list of external custom actions referenced in the workflow
	if len(actions) > 0 {
		yaml.WriteString("#\n")
		yaml.WriteString("# Custom actions used:\n")
		for _, a := range actions {
			fmt.Fprintf(yaml, "#   - %s\n", a)
		}
	}

	// Add list of container images used in the workflow
	if len(data.DockerImages) > 0 {
		yaml.WriteString("#\n")
		yaml.WriteString("# Container images used:\n")
		for _, img := range data.DockerImages {
			fmt.Fprintf(yaml, "#   - %s\n", img)
		}
	}

	// Add stop-time comment if configured
	if data.StopTime != "" {
		yaml.WriteString("#\n")
		cleanStopTime := stringutil.StripANSI(data.StopTime)
		fmt.Fprintf(yaml, "# Effective stop-time: %s\n", cleanStopTime)
	}

	// Add manual-approval comment if configured
	if data.ManualApproval != "" {
		yaml.WriteString("#\n")
		cleanManualApproval := stringutil.StripANSI(data.ManualApproval)
		fmt.Fprintf(yaml, "# Manual approval required: environment '%s'\n", cleanManualApproval)
	}

	yaml.WriteString("\n")
	return nil
}
