package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var artifactsLog = logger.New("workflow:artifacts")

// ArtifactDownloadConfig holds configuration for building artifact download steps
type ArtifactDownloadConfig struct {
	ArtifactName     string // Name of the artifact to download (e.g., "agent-output", "prompt")
	FallbackArtifact string // Optional secondary artifact name; when set, both artifacts are matched and merged
	ArtifactFilename string // Filename inside the artifact directory (e.g., "agent_output.json", "prompt.txt")
	DownloadPath     string // Path where artifact will be downloaded (e.g., "/tmp/gh-aw/safeoutputs/")
	SetupEnvStep     bool   // Whether to add environment variable setup step
	EnvVarName       string // Environment variable name to set (e.g., "GH_AW_AGENT_OUTPUT")
	StepName         string // Optional custom step name (defaults to "Download {artifact} artifact")
	IfCondition      string // Optional conditional expression for the step (e.g., "needs.agent.outputs.has_patch == 'true'")
	StepID           string // Optional step ID; when set, the env-setup step is gated on this step's success
	ContinueOnError  *bool  // Optional; nil uses the default (emit continue-on-error: true). True emits true; false omits the key.
}

// buildArtifactDownloadSteps creates steps to download a GitHub Actions artifact.
// pinAction is used to resolve the download-artifact action reference; callers inside
// a Compiler method should pass c.getActionPin to honour the per-compilation GHES compat flag.
func buildArtifactDownloadSteps(config ArtifactDownloadConfig, pinAction func(string) string) []string { //nolint:largefunc // Keeps all download options and environment setup in one ordered builder.
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
	// Add step ID if specified (used to condition the env-setup step on download success)
	if config.StepID != "" {
		steps = append(steps, fmt.Sprintf("        id: %s\n", config.StepID))
		artifactsLog.Printf("Added step ID: %s", config.StepID)
	}
	// Add conditional if specified
	if config.IfCondition != "" {
		steps = append(steps, fmt.Sprintf("        if: %s\n", config.IfCondition))
		artifactsLog.Printf("Added conditional: %s", config.IfCondition)
	}
	continueOnError := true
	if config.ContinueOnError != nil {
		continueOnError = *config.ContinueOnError
	}
	if continueOnError {
		steps = append(steps, "        continue-on-error: true\n")
	}
	downloadAction := pinAction("actions/download-artifact")
	steps = append(steps, fmt.Sprintf("        uses: %s\n", downloadAction))
	steps = append(steps, "        with:\n")
	if config.FallbackArtifact != "" {
		if downloadArtifactSupportsPattern(downloadAction) {
			// Match both artifacts with a single brace-expanded pattern and merge their contents into
			// the download path. The primary artifact may be missing when its (best-effort) upload
			// failed; the fallback artifact then supplies the critical files.
			steps = append(steps, fmt.Sprintf("          pattern: \"{%s,%s}\"\n", config.ArtifactName, config.FallbackArtifact))
			steps = append(steps, "          merge-multiple: true\n")
		} else {
			steps = append(steps, fmt.Sprintf("          name: %s\n", config.ArtifactName))
		}
	} else {
		steps = append(steps, fmt.Sprintf("          name: %s\n", config.ArtifactName))
	}
	steps = append(steps, fmt.Sprintf("          path: %s\n", config.DownloadPath))

	// Add environment variable setup if requested
	if config.SetupEnvStep {
		artifactsLog.Printf("Adding environment variable setup step: %s=%s%s",
			config.EnvVarName, config.DownloadPath, config.ArtifactFilename)
		steps = append(steps, "      - name: Setup agent output environment variable\n")
		steps = append(steps, "        id: setup-agent-output-env\n")
		// Only set the env var when the artifact was actually downloaded and contains the expected file
		if config.StepID != "" {
			steps = append(steps, fmt.Sprintf("        if: steps.%s.outcome == 'success'\n", config.StepID))
			artifactsLog.Printf("Added env-setup conditional on step outcome: %s", config.StepID)
		}
		steps = append(steps, "        run: |\n")
		steps = append(steps, fmt.Sprintf("          mkdir -p %s\n", config.DownloadPath))
		steps = append(steps, fmt.Sprintf("          find \"%s\" -type f -print\n", config.DownloadPath))
		// When downloading a single artifact by name with download-artifact@v4,
		// artifacts are extracted directly to {download-path}, not {download-path}/{artifact-name}/
		// The actual filename is specified in ArtifactFilename
		artifactPath := fmt.Sprintf("%s%s", config.DownloadPath, config.ArtifactFilename)
		steps = append(steps, fmt.Sprintf("          if [ -f \"%s\" ]; then\n", artifactPath))
		steps = append(steps, fmt.Sprintf("            echo \"%s=%s\" >> \"$GITHUB_OUTPUT\"\n", config.EnvVarName, artifactPath))
		steps = append(steps, "          fi\n")
	}

	return steps
}

func downloadArtifactSupportsPattern(actionRef string) bool {
	version := extractActionVersion(actionRef)
	return !strings.HasPrefix(version, "v3") && !strings.Contains(version, "# v3.")
}

func artifactUploadOverwriteOption(prefix, actionRef string) string {
	if prefix == "" || !downloadArtifactSupportsPattern(actionRef) {
		return ""
	}
	return "          overwrite: true\n"
}

func downloadArtifactInputLines(artifactName, actionRef string) []string {
	if downloadArtifactSupportsPattern(actionRef) {
		return []string{
			fmt.Sprintf("          pattern: %s\n", artifactName),
			"          merge-multiple: true\n",
		}
	}
	return []string{fmt.Sprintf("          name: %s\n", artifactName)}
}
