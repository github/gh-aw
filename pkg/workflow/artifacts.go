package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var artifactsLog = logger.New("workflow:artifacts")

// ArtifactDownloadConfig holds configuration for building artifact download steps
type ArtifactDownloadConfig struct {
	ArtifactName     string // Name of the artifact to download (e.g., "agent-output", "prompt")
	ArtifactFilename string // Filename inside the artifact directory (e.g., "agent_output.json", "prompt.txt")
	DownloadPath     string // Path where artifact will be downloaded (e.g., "/tmp/gh-aw/safeoutputs/")
	SetupEnvStep     bool   // Whether to add environment variable setup step
	EnvVarName       string // Environment variable name to set (e.g., "GH_AW_AGENT_OUTPUT")
	StepName         string // Optional custom step name (defaults to "Download {artifact} artifact")
	IfCondition      string // Optional conditional expression for the step (e.g., "needs.agent.outputs.has_patch == 'true'")
}

// buildArtifactDownloadSteps creates steps to download a GitHub Actions artifact
// This is a generalized helper that can be used across different contexts (safe-outputs, safe-jobs, threat-detection)
func buildArtifactDownloadSteps(config ArtifactDownloadConfig) []string {
	artifactsLog.Printf("Building artifact download steps: artifact=%s, path=%s, setupEnv=%v",
		config.ArtifactName, config.DownloadPath, config.SetupEnvStep)

	var steps []string

	// Use provided step name or generate default
	stepName := config.StepName
	if stepName == "" {
		stepName = fmt.Sprintf("Download %s artifact", config.ArtifactName)
		artifactsLog.Printf("Using default step name: %s", stepName)
	}

	// Add step to download artifact
	steps = append(steps, fmt.Sprintf("      - name: %s\n", stepName))
	// Add conditional if specified
	if config.IfCondition != "" {
		steps = append(steps, fmt.Sprintf("        if: %s\n", config.IfCondition))
		artifactsLog.Printf("Added conditional: %s", config.IfCondition)
	}
	steps = append(steps, "        continue-on-error: true\n")
	steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/download-artifact")))
	steps = append(steps, "        with:\n")
	steps = append(steps, fmt.Sprintf("          name: %s\n", config.ArtifactName))
	steps = append(steps, fmt.Sprintf("          path: %s\n", config.DownloadPath))

	// Add environment variable setup if requested
	if config.SetupEnvStep {
		artifactsLog.Printf("Adding environment variable setup step: %s=%s%s",
			config.EnvVarName, config.DownloadPath, config.ArtifactFilename)
		steps = append(steps, "      - name: Setup agent output environment variable\n")
		steps = append(steps, "        run: |\n")
		steps = append(steps, fmt.Sprintf("          mkdir -p %s\n", config.DownloadPath))
		steps = append(steps, fmt.Sprintf("          find \"%s\" -type f -print\n", config.DownloadPath))
		// When downloading a single artifact by name with download-artifact@v4,
		// artifacts are extracted directly to {download-path}, not {download-path}/{artifact-name}/
		// The actual filename is specified in ArtifactFilename
		artifactPath := fmt.Sprintf("%s%s", config.DownloadPath, config.ArtifactFilename)
		steps = append(steps, fmt.Sprintf("          echo \"%s=%s\" >> \"$GITHUB_ENV\"\n", config.EnvVarName, artifactPath))
	}

	return steps
}
