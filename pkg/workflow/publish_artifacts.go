package workflow

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
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

const artifactStagingTransferDirExpr = "${{ runner.temp }}/gh-aw/safeoutputs/upload-artifacts-transfer/"

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
func generateSafeOutputsArtifactStagingUpload(builder *strings.Builder, data *WorkflowData, pinAction func(string) string) error {
	if data.SafeOutputs == nil || !usesSafeOutputsArtifactStaging(data.SafeOutputs) {
		return nil
	}

	publishArtifactsLog.Print("Generating safe-outputs artifact staging upload step")

	prefix := artifactPrefixExprForDownstreamJob(data)
	configJSON, runtimePolicyValues, err := safeOutputsArtifactStagingConfig(data)
	if err != nil {
		return err
	}

	builder.WriteString("      - name: Prepare safe-outputs artifact staging\n")
	builder.WriteString("        id: prepare_safe_outputs_artifact_staging\n")
	builder.WriteString("        if: always() && " + secretRedactionSuccessCondition(data) + "\n")
	fmt.Fprintf(builder, "        uses: %s\n", pinAction("actions/github-script")) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	builder.WriteString("        env:\n")
	fmt.Fprintf(builder, "          GH_AW_UPLOAD_ARTIFACT_CONFIG: %q\n", configJSON) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	runtimePolicyKeys := make([]string, 0, len(runtimePolicyValues))
	for key := range runtimePolicyValues {
		runtimePolicyKeys = append(runtimePolicyKeys, key)
	}
	slices.Sort(runtimePolicyKeys)
	for _, key := range runtimePolicyKeys {
		fmt.Fprintf(builder, "          %s: %q\n", key, runtimePolicyValues[key]) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	}
	fmt.Fprintf(builder, "          GH_AW_UPLOAD_ARTIFACT_STAGING_DIR: %s\n", artifactStagingDirExpr)          //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(builder, "          GH_AW_UPLOAD_ARTIFACT_TRANSFER_DIR: %s\n", artifactStagingTransferDirExpr) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	builder.WriteString("        with:\n")
	builder.WriteString("          script: |\n")
	builder.WriteString("            const { setupGlobals } = require(`${process.env.RUNNER_TEMP}/gh-aw/actions/setup_globals.cjs`);\n")
	builder.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	builder.WriteString("            const { prepare } = require(`${process.env.RUNNER_TEMP}/gh-aw/actions/prepare_upload_artifact_staging.cjs`);\n")
	builder.WriteString("            const result = await prepare({ core });\n")
	builder.WriteString("            core.info(`Prepared ${result.count} safe-output staging file(s)`);\n")
	builder.WriteString("      # Upload safe-outputs upload-artifact staging for the upload_artifact job\n")
	builder.WriteString("      - name: Upload upload-artifact staging\n")
	builder.WriteString("        if: always() && " + secretRedactionSuccessCondition(data) + " && steps.prepare_safe_outputs_artifact_staging.outcome == 'success'\n")
	fmt.Fprintf(builder, "        uses: %s\n", pinAction("actions/upload-artifact")) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	builder.WriteString("        with:\n")
	fmt.Fprintf(builder, "          name: %s%s\n", prefix, SafeOutputsUploadArtifactStagingArtifactName) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(builder, "          path: %s\n", artifactStagingTransferDirExpr)                         //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	builder.WriteString("          retention-days: 1\n")
	builder.WriteString("          if-no-files-found: ignore\n")
	return nil
}

func safeOutputsArtifactStagingConfig(data *WorkflowData) (string, map[string]string, error) {
	config := make(map[string]any)
	runtimePolicyValues := make(map[string]string)
	nextRuntimePolicyValue := 0
	encodePatterns := func(patterns []string) []string {
		encoded := make([]string, len(patterns))
		for i, pattern := range patterns {
			encoded[i] = encodeArtifactStagingRuntimePolicy(pattern, runtimePolicyValues, &nextRuntimePolicyValue)
		}
		return encoded
	}
	if artifactConfig := data.SafeOutputs.UploadArtifact; artifactConfig != nil {
		handlerConfig := make(map[string]any)
		maxSizeBytes := artifactConfig.MaxSizeBytes
		if maxSizeBytes <= 0 {
			maxSizeBytes = defaultArtifactMaxSizeBytes
		}
		handlerConfig["max-size-bytes"] = maxSizeBytes
		if len(artifactConfig.AllowedPaths) > 0 {
			handlerConfig["allowed-paths"] = encodePatterns(artifactConfig.AllowedPaths)
		}
		if artifactConfig.Filters != nil {
			if len(artifactConfig.Filters.Include) > 0 {
				handlerConfig["filters-include"] = encodePatterns(artifactConfig.Filters.Include)
			}
			if len(artifactConfig.Filters.Exclude) > 0 {
				handlerConfig["filters-exclude"] = encodePatterns(artifactConfig.Filters.Exclude)
			}
		}
		config["upload_artifact"] = handlerConfig
	}
	for name, script := range data.SafeOutputs.Scripts {
		if script != nil {
			if files := connectorStagedFilePolicies(script.connectorInputSchema); len(files) > 0 {
				config[stringutil.NormalizeSafeOutputIdentifier(name)] = map[string]any{"staged_files": files}
			}
		}
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		return "", nil, fmt.Errorf("cannot serialize safe-output staging configuration: %w", err)
	}
	return string(configJSON), runtimePolicyValues, nil
}

func encodeArtifactStagingRuntimePolicy(pattern string, values map[string]string, nextIndex *int) string {
	var encoded strings.Builder
	for offset := 0; offset < len(pattern); {
		start := strings.Index(pattern[offset:], "${{")
		if start < 0 {
			encoded.WriteString(pattern[offset:])
			break
		}
		start += offset
		end := strings.Index(pattern[start+3:], "}}")
		if end < 0 {
			encoded.WriteString(pattern[offset:])
			break
		}
		end += start + 3
		expression := strings.TrimSpace(pattern[start+3 : end])
		if expression == "" {
			encoded.WriteString(pattern[offset : end+2])
			offset = end + 2
			continue
		}
		key := fmt.Sprintf("GH_AW_UPLOAD_ARTIFACT_RUNTIME_POLICY_%d", *nextIndex)
		placeholder := "__" + key + "__"
		for strings.Contains(pattern, placeholder) || values[key] != "" {
			(*nextIndex)++
			key = fmt.Sprintf("GH_AW_UPLOAD_ARTIFACT_RUNTIME_POLICY_%d", *nextIndex)
			placeholder = "__" + key + "__"
		}
		(*nextIndex)++
		values[key] = "${{ " + expression + " }}"
		encoded.WriteString(pattern[offset:start])
		encoded.WriteString(placeholder)
		offset = end + 2
	}
	return encoded.String()
}

func usesSafeOutputsArtifactStaging(config *SafeOutputsConfig) bool {
	return len(safeOutputsArtifactStagingTypes(config)) != 0
}

func safeOutputsArtifactStagingTypes(config *SafeOutputsConfig) []string {
	if config == nil {
		return nil
	}
	var types []string
	if config.UploadArtifact != nil {
		types = append(types, "upload_artifact")
	}
	for name, script := range config.Scripts {
		if script != nil && len(connectorStagedFilePolicies(script.connectorInputSchema)) != 0 {
			types = append(types, stringutil.NormalizeSafeOutputIdentifier(name))
		}
	}
	slices.Sort(types)
	return slices.Compact(types)
}
