package workflow

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var compilerYamlGradersLog = logger.New("workflow:compiler_yaml_graders")

const graderPayloadChunkSize = 12 * 1024

// generateGradersStep emits an always() post-agent step that runs deterministic
// graders. The step executes after secret redaction / summary steps and before
// the unified artifact upload so results are included in the agent artifact.
//
// The step is only emitted when graders are configured (graders: in frontmatter).
func (c *Compiler) generateGradersStep(yaml *strings.Builder, data *WorkflowData) {
	if data.Graders == nil || !data.Graders.HasGraders() {
		return
	}

	compilerYamlGradersLog.Printf("Generating graders step with %d enabled graders", len(data.Graders.EnabledGraderIDs()))

	// Build the manifest JSON that the JS runtime will consume.
	manifest := buildGraderManifest(data.Graders)
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		compilerYamlGradersLog.Printf("Failed to marshal grader manifest: %v", err)
		return
	}
	manifestB64 := base64.StdEncoding.EncodeToString(manifestJSON)

	// Build execution spec (scripts) separately, base64 encoded for safety.
	execSpec := buildGraderExecSpec(data.Graders)
	execJSON, err := json.Marshal(execSpec)
	if err != nil {
		compilerYamlGradersLog.Printf("Failed to marshal grader exec spec: %v", err)
		return
	}
	execB64 := base64.StdEncoding.EncodeToString(execJSON)

	yaml.WriteString("      - name: Run graders\n")
	yaml.WriteString("        if: always()\n")
	yaml.WriteString("        continue-on-error: true\n")
	fmt.Fprintf(yaml, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	yaml.WriteString("            const { main } = require('" + SetupActionDestination + "/trace_graders.cjs');\n")
	invocation := fmt.Sprintf("            await main('%s', '%s');\n", manifestB64, execB64)
	if len(invocation) <= MaxExpressionSize {
		yaml.WriteString(invocation)
	} else {
		writeChunkedGraderPayload(yaml, "graderManifestB64", manifestB64)
		writeChunkedGraderPayload(yaml, "graderExecB64", execB64)
		yaml.WriteString("            await main(graderManifestB64, graderExecB64);\n")
	}
	if operationalValueGrader, ok := data.Graders.Graders["operational-value"]; ok && (operationalValueGrader.Enabled == nil || *operationalValueGrader.Enabled) {
		yaml.WriteString("        env:\n")
		yaml.WriteString("          GH_TOKEN: ${{ github.token }}\n")
		yaml.WriteString("          GH_AW_RUN_CREATED_AT: ${{ needs.activation.outputs.run_created_at }}\n")
	}

	compilerYamlGradersLog.Print("Generated graders step")
}

func writeChunkedGraderPayload(yaml *strings.Builder, variable, payload string) {
	fmt.Fprintf(yaml, "            const %s = [\n", variable)
	for payload != "" {
		chunkSize := min(len(payload), graderPayloadChunkSize)
		fmt.Fprintf(yaml, "              '%s',\n", payload[:chunkSize])
		payload = payload[chunkSize:]
	}
	yaml.WriteString("            ].join('');\n")
}

// graderManifestEntry represents a single grader in the serialized manifest.
// The manifest is an object {version:1, graders:[...]} for stable schema.
type graderManifestEntry struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Source      string         `json:"source"` // "builtin", "inline", or "operational-value"
	Enabled     bool           `json:"enabled"`
	Unit        string         `json:"unit,omitempty"`
	Direction   string         `json:"direction,omitempty"`
	Threshold   *float64       `json:"threshold,omitempty"`
	Max         *float64       `json:"max,omitempty"`
	Min         *float64       `json:"min,omitempty"`
	Digest      string         `json:"digest,omitempty"` // SHA-256 of inline script or operational-value evaluator
	Run         string         `json:"run,omitempty"`
	Config      map[string]any `json:"config,omitempty"`
}

// graderManifest is the top-level manifest object written to disk.
type graderManifest struct {
	Version int                   `json:"version"`
	Graders []graderManifestEntry `json:"graders"`
}

// graderExecEntry carries trusted executable content for a custom grader, keyed by ID.
type graderExecEntry struct {
	ID     string `json:"id"`
	Script string `json:"script,omitempty"`
	Run    string `json:"run,omitempty"`
}

// buildGraderManifest constructs the manifest for the JS runtime.
func buildGraderManifest(cfg *GradersConfig) *graderManifest { //nolint:largefunc // Manifest entry assembly is intentionally kept in serialization order.
	if cfg == nil {
		return &graderManifest{Version: 1}
	}

	builtinSet := make(map[string]struct{}, len(BuiltinGraderIDs))
	for _, id := range BuiltinGraderIDs {
		builtinSet[id] = struct{}{}
	}

	ids := cfg.EnabledGraderIDs()
	// Also include disabled graders so the manifest records them
	var disabledIDs []string
	for id, g := range cfg.Graders {
		if g.Enabled != nil && !*g.Enabled {
			disabledIDs = append(disabledIDs, id)
		}
	}
	sort.Strings(disabledIDs)

	entries := make([]graderManifestEntry, 0, len(ids)+len(disabledIDs))

	addEntry := func(id string, enabled bool) {
		g := cfg.Graders[id]
		source := "builtin"
		if _, ok := builtinSet[id]; !ok {
			source = "inline"
		}
		if id == "operational-value" {
			source = "operational-value"
		}
		digest := g.ScriptDigest()
		if source == "operational-value" {
			digest = g.EvaluatorDigest()
		}
		name := g.Name
		if name == "" {
			name = id
		}
		entries = append(entries, graderManifestEntry{
			ID:          id,
			Name:        name,
			Description: g.Description,
			Source:      source,
			Enabled:     enabled,
			Unit:        g.Unit,
			Direction:   g.Direction,
			Threshold:   g.Threshold,
			Max:         g.Max,
			Min:         g.Min,
			Digest:      digest,
			Run:         g.Run,
			Config:      g.Config,
		})
	}

	for _, id := range ids {
		addEntry(id, true)
	}
	for _, id := range disabledIDs {
		addEntry(id, false)
	}

	return &graderManifest{Version: 1, Graders: entries}
}

// buildGraderExecSpec builds the execution spec: an array of {id, script}
// entries for custom graders only. This is base64-encoded to avoid JS/YAML injection.
func buildGraderExecSpec(cfg *GradersConfig) []graderExecEntry {
	if cfg == nil {
		return nil
	}
	builtinSet := make(map[string]struct{}, len(BuiltinGraderIDs))
	for _, id := range BuiltinGraderIDs {
		builtinSet[id] = struct{}{}
	}

	var specs []graderExecEntry
	for _, id := range cfg.EnabledGraderIDs() {
		g := cfg.Graders[id]
		if id == "operational-value" && g.evaluatorContent != "" {
			specs = append(specs, graderExecEntry{ID: id, Run: g.evaluatorContent})
		} else if _, ok := builtinSet[id]; !ok && g.Script != "" {
			specs = append(specs, graderExecEntry{ID: id, Script: g.Script})
		}
	}
	return specs
}

// generateGraderRedactionStep emits a lightweight redaction pass that scans grader
// output files for leaked secrets. Custom grader scripts can evaluate trace data
// that may contain credential-bearing strings. This step runs after the graders step
// and reuses the existing redact_secrets.cjs infrastructure.
func (c *Compiler) generateGraderRedactionStep(yaml *strings.Builder, yamlContent string, data *WorkflowData) {
	if data.Graders == nil || !data.Graders.HasGraders() {
		return
	}

	secretReferences := CollectSecretReferences(yamlContent)
	c.stepOrderTracker.RecordSecretRedaction("Redact grader outputs")

	yaml.WriteString("      - name: Redact grader outputs\n")
	yaml.WriteString("        if: always()\n")
	yaml.WriteString("        continue-on-error: true\n")
	fmt.Fprintf(yaml, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	yaml.WriteString("            const { redactFilesInDir } = require('" + SetupActionDestination + "/redact_secrets.cjs');\n")
	fmt.Fprintf(yaml, "            await redactFilesInDir('%s');\n", constants.GradersDir)

	if len(secretReferences) > 0 {
		yaml.WriteString("        env:\n")
		escapedRefs := make([]string, len(secretReferences))
		for i, ref := range secretReferences {
			escapedRefs[i] = escapeSingleQuoteBackslash(ref)
		}
		fmt.Fprintf(yaml, "          GH_AW_SECRET_NAMES: '%s'\n", strings.Join(escapedRefs, ","))
		for _, secretName := range secretReferences {
			escapedSecretName := escapeSingleQuoteBackslash(secretName)
			fmt.Fprintf(yaml, "          SECRET_%s: ${{ secrets.%s }}\n", escapedSecretName, secretName)
		}
	}
}

// collectGraderArtifactPaths returns artifact paths for grader output files.
func collectGraderArtifactPaths(graders *GradersConfig) []string {
	paths := []string{
		constants.GradersDirSlash + constants.GraderManifestFilename.String(),
		constants.GradersDirSlash + constants.GraderPayloadFilename.String(),
		constants.GradersDirSlash + constants.GraderResultsFilename.String(),
	}
	if graders != nil {
		grader := graders.Graders["operational-value"]
		if grader != nil && (grader.Enabled == nil || *grader.Enabled) && grader.evaluatorContent != "" {
			paths = append(paths, constants.GradersDirSlash+constants.OperationalValueEvaluatorFilename.String())
		}
	}
	return paths
}
