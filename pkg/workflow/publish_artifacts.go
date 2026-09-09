package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
)

var publishArtifactsLog = logger.New("workflow:publish_artifacts")

// defaultArtifactMaxUploads is the default maximum number of upload_artifact tool calls allowed per run.
const defaultArtifactMaxUploads = 1

// defaultArtifactMaxSizeBytes is the default maximum total upload size (100 MB).
const defaultArtifactMaxSizeBytes int64 = 104857600

// artifactStagingDirExpr is the GitHub Actions expression form of the staging directory.
// `actions/upload-artifact` and `actions/download-artifact` do not expand shell variables
// in their `path:` inputs, so we must use ${{ runner.temp }} here.
const artifactStagingDirExpr = "${{ runner.temp }}/gh-aw/safeoutputs/upload-artifacts/"

// SafeOutputsUploadArtifactStagingArtifactName is the artifact that carries the staging directory
// from the main agent job to the upload_artifact job.
const SafeOutputsUploadArtifactStagingArtifactName = "safe-outputs-upload-artifacts"

// ArtifactFiltersConfig holds include/exclude glob patterns for artifact file selection.
type ArtifactFiltersConfig struct {
	Include []string `yaml:"include,omitempty"` // Glob patterns for files to include
	Exclude []string `yaml:"exclude,omitempty"` // Glob patterns for files to exclude
}

// ArtifactDefaultsConfig holds default request settings applied when the model does not
// specify a value explicitly.
type ArtifactDefaultsConfig struct {
	IfNoFiles string `yaml:"if-no-files,omitempty"` // Behavior when no files match: "error" or "ignore"
}

// UploadArtifactConfig holds configuration for the upload-artifact safe output type.
type UploadArtifactConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	MaxUploads           int                     `yaml:"max-uploads,omitempty"`    // Max upload_artifact tool calls allowed (default: 1)
	RetentionDays        *string                 `yaml:"retention-days,omitempty"` // Fixed retention period in days (templatable int; agent cannot override)
	SkipArchive          *string                 `yaml:"skip-archive,omitempty"`   // Fixed skip-archive flag (templatable bool; agent cannot override)
	MaxSizeBytes         int64                   `yaml:"max-size-bytes,omitempty"` // Max total bytes per upload (default: 100 MB)
	AllowedPaths         []string                `yaml:"allowed-paths,omitempty"`  // Glob patterns restricting which paths the model may upload
	Filters              *ArtifactFiltersConfig  `yaml:"filters,omitempty"`        // Default include/exclude filters applied on top of allowed-paths
	Defaults             *ArtifactDefaultsConfig `yaml:"defaults,omitempty"`       // Default values injected when the model omits a field
}

// parseUploadArtifactConfig parses the upload-artifact key from the safe-outputs map.
func (c *Compiler) parseUploadArtifactConfig(outputMap map[string]any) *UploadArtifactConfig { //nolint:largefunc // Existing upload-artifact parsing remains centralized; Azure attachment staging only reuses its artifact channel.
	configData, exists := outputMap["upload-artifact"]
	if !exists {
		return nil
	}

	// Explicit false disables upload-artifact (e.g. when passed via import-inputs).
	if b, ok := configData.(bool); ok && !b {
		publishArtifactsLog.Print("upload-artifact explicitly set to false, skipping")
		return nil
	}

	publishArtifactsLog.Print("Parsing upload-artifact configuration")
	config := &UploadArtifactConfig{
		MaxUploads:   defaultArtifactMaxUploads,
		MaxSizeBytes: defaultArtifactMaxSizeBytes,
	}

	configMap, ok := configData.(map[string]any)
	if !ok {
		// No config map (e.g. upload-artifact: true) – use defaults.
		publishArtifactsLog.Print("upload-artifact enabled with default configuration")
		return config
	}

	// Parse max-uploads.
	if maxUploads, exists := configMap["max-uploads"]; exists {
		if v, ok := typeutil.ParseIntValue(maxUploads); ok && v > 0 {
			config.MaxUploads = v
		}
	}

	// Parse retention-days (templatable int).
	if err := preprocessIntFieldAsString(configMap, "retention-days", publishArtifactsLog); err != nil {
		publishArtifactsLog.Printf("Warning: %v", err)
	}
	if retDays, exists := configMap["retention-days"]; exists {
		if s, ok := retDays.(string); ok && s != "" {
			config.RetentionDays = &s
		}
	}

	// Parse skip-archive (templatable bool).
	if err := preprocessBoolFieldAsString(configMap, "skip-archive", publishArtifactsLog); err != nil {
		publishArtifactsLog.Printf("Warning: %v", err)
	}
	if skipArchive, exists := configMap["skip-archive"]; exists {
		if s, ok := skipArchive.(string); ok && s != "" {
			config.SkipArchive = &s
		}
	}

	// Parse max-size-bytes.
	if maxBytes, exists := configMap["max-size-bytes"]; exists {
		if v, ok := typeutil.ParseIntValue(maxBytes); ok && v > 0 {
			config.MaxSizeBytes = int64(v)
		}
	}

	// Parse allowed-paths.
	if allowedPaths, exists := configMap["allowed-paths"]; exists {
		if arr, ok := allowedPaths.([]any); ok {
			for _, p := range arr {
				if s, ok := p.(string); ok && s != "" {
					config.AllowedPaths = append(config.AllowedPaths, s)
				}
			}
		}
	}

	// Parse filters.
	if filtersData, exists := configMap["filters"]; exists {
		if filtersMap, ok := filtersData.(map[string]any); ok {
			filters := &ArtifactFiltersConfig{}
			if inc, ok := filtersMap["include"].([]any); ok {
				for _, v := range inc {
					if s, ok := v.(string); ok {
						filters.Include = append(filters.Include, s)
					}
				}
			}
			if exc, ok := filtersMap["exclude"].([]any); ok {
				for _, v := range exc {
					if s, ok := v.(string); ok {
						filters.Exclude = append(filters.Exclude, s)
					}
				}
			}
			if len(filters.Include) > 0 || len(filters.Exclude) > 0 {
				config.Filters = filters
			}
		}
	}

	// Parse defaults (if-no-files only).
	if defaultsData, exists := configMap["defaults"]; exists {
		if defaultsMap, ok := defaultsData.(map[string]any); ok {
			defaults := &ArtifactDefaultsConfig{}
			if ifNoFiles, ok := defaultsMap["if-no-files"].(string); ok && ifNoFiles != "" {
				defaults.IfNoFiles = ifNoFiles
			}
			if defaults.IfNoFiles != "" {
				config.Defaults = defaults
			}
		}
	}

	// Parse common base fields (max, github-token, staged).
	c.parseBaseSafeOutputConfig(configMap, &config.BaseSafeOutputConfig, 0)

	publishArtifactsLog.Printf("Parsed upload-artifact config: max_uploads=%d, retention_days=%v, skip_archive=%v, max_size_bytes=%d",
		config.MaxUploads, config.RetentionDays, config.SkipArchive, config.MaxSizeBytes)
	return config
}

// generateSafeOutputsArtifactStagingUpload generates a step in the main agent job that uploads
// the artifact staging directory so the safe_outputs job can download it for inline processing.
// This step only appears when upload-artifact is configured in safe-outputs.
// pinAction resolves the upload-artifact action reference; pass c.getActionPin from Compiler methods.
func generateSafeOutputsArtifactStagingUpload(builder *strings.Builder, data *WorkflowData, pinAction func(string) string) {
	if data.SafeOutputs == nil || !usesSafeOutputsArtifactStaging(data.SafeOutputs) {
		return
	}

	publishArtifactsLog.Print("Generating safe-outputs artifact staging upload step")

	prefix := artifactPrefixExprForDownstreamJob(data)

	builder.WriteString("      # Upload safe-outputs upload-artifact staging for the upload_artifact job\n")
	builder.WriteString("      - name: Upload upload-artifact staging\n")
	builder.WriteString("        if: always()\n")
	fmt.Fprintf(builder, "        uses: %s\n", pinAction("actions/upload-artifact"))
	builder.WriteString("        with:\n")
	fmt.Fprintf(builder, "          name: %s%s\n", prefix, SafeOutputsUploadArtifactStagingArtifactName)
	fmt.Fprintf(builder, "          path: %s\n", artifactStagingDirExpr)
	builder.WriteString("          retention-days: 1\n")
	builder.WriteString("          if-no-files-found: ignore\n")
}

func usesSafeOutputsArtifactStaging(config *SafeOutputsConfig) bool {
	return config != nil && (config.UploadArtifact != nil || config.UploadWorkItemAttachments != nil)
}
