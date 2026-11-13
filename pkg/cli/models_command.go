package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/modelsdev"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

const defaultModelsRefreshCount = 20

// NewModelsCommand creates the models command.
func NewModelsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "models",
		Short: "List AWF models, aliases, and observed automation models",
		Long: `List model data to help pick a model alias or explicit model name.

Outputs three sections:
- AWF models advertised by configured endpoints in recent workflow runs
- Built-in model aliases and their resolution order
- Models observed in local automation logs and AWF reflect artifacts

By default, the command attempts a lightweight log refresh focused on firewall artifacts
so recent awf-reflect data can be discovered before reporting.`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` models
  ` + string(constants.CLIExtensionPrefix) + ` models --json
  ` + string(constants.CLIExtensionPrefix) + ` models --logs-dir .github/aw/logs
  ` + string(constants.CLIExtensionPrefix) + ` models --refresh-count 50
  ` + string(constants.CLIExtensionPrefix) + ` models --refresh-observed=false`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runModelsCommand(cmd)
		},
	}

	addJSONFlag(cmd)
	cmd.Flags().String("logs-dir", defaultLogsOutputDir, "Directory containing downloaded logs/artifacts")
	cmd.Flags().Bool("refresh-observed", true, "Attempt to refresh local observed-model artifacts before reporting")
	cmd.Flags().Int("refresh-count", defaultModelsRefreshCount, "Maximum number of recent runs to inspect when refreshing observed models")
	addRepoFlag(cmd)
	return cmd
}

type modelCatalogRow struct {
	Provider string `json:"provider" console:"header:Provider"`
	Model    string `json:"model" console:"header:Model"`
}

type modelAliasRow struct {
	Alias   string `json:"alias" console:"header:Alias"`
	Targets string `json:"targets" console:"header:Resolution Order"`
}

type observedModelRow struct {
	Provider    string `json:"provider" console:"header:Provider"`
	Model       string `json:"model" console:"header:Model"`
	Sources     string `json:"sources" console:"header:Sources"`
	Occurrences int    `json:"occurrences" console:"header:Seen"`
	InCatalog   bool   `json:"in_catalog" console:"header:AWF Inventory"`
	AliasHints  string `json:"alias_hints,omitempty" console:"header:Alias Hints,omitempty"`
}

type modelsReport struct {
	Catalog         []modelCatalogRow  `json:"catalog"`
	CatalogComplete *bool              `json:"catalog_complete,omitempty"`
	Aliases         []modelAliasRow    `json:"aliases"`
	Observed        []observedModelRow `json:"observed"`
	Warnings        []string           `json:"warnings,omitempty"`
}

type modelsReportOptions struct {
	logsDir         string
	refreshObserved bool
	refreshCount    int
	repoOverride    string
}

const maxAliasHints = 6

func runModelsCommand(cmd *cobra.Command) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	logsDir, _ := cmd.Flags().GetString("logs-dir")
	refreshObserved, _ := cmd.Flags().GetBool("refresh-observed")
	refreshCount, _ := cmd.Flags().GetInt("refresh-count")
	repoOverride, _ := cmd.Flags().GetString("repo")

	report := buildModelsReport(cmd.Context(), modelsReportOptions{
		logsDir:         logsDir,
		refreshObserved: refreshObserved,
		refreshCount:    refreshCount,
		repoOverride:    repoOverride,
	})

	if jsonOutput {
		jsonBytes, err := marshalIndentJSONOrWrap(report, "models report")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(jsonBytes))
		return nil
	}

	fmt.Fprintln(os.Stdout, "AWF Model Inventory")
	if len(report.Catalog) == 0 {
		fmt.Fprintln(os.Stdout, "No AWF models found in recent workflow inventories.")
	} else {
		fmt.Fprint(os.Stdout, console.RenderStructStdout(report.Catalog))
	}
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Model Aliases")
	fmt.Fprint(os.Stdout, console.RenderStructStdout(report.Aliases))
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Observed Models")
	if len(report.Observed) == 0 {
		fmt.Fprintln(os.Stdout, "No observed models found in local logs/artifacts.")
	} else {
		fmt.Fprint(os.Stdout, console.RenderStructStdout(report.Observed))
	}
	for _, warning := range report.Warnings {
		fmt.Fprintln(os.Stderr, warning)
	}
	return nil
}

func buildModelsReport(ctx context.Context, opts modelsReportOptions) modelsReport {
	warnings := make([]string, 0)
	if opts.refreshObserved {
		if err := refreshObservedArtifacts(ctx, opts.logsDir, opts.refreshCount, opts.repoOverride); err != nil {
			warnings = append(warnings, "observed-model refresh failed: "+err.Error())
		}
	}

	aliasRows, aliasMap := buildModelAliasRows()
	observedRows, catalogRows, catalogComplete, observedWarnings := collectObservedModelRows(opts.logsDir, aliasMap)
	warnings = append(warnings, observedWarnings...)

	return modelsReport{
		Catalog:         catalogRows,
		CatalogComplete: catalogComplete,
		Aliases:         aliasRows,
		Observed:        observedRows,
		Warnings:        warnings,
	}
}

func refreshObservedArtifacts(ctx context.Context, logsDir string, refreshCount int, repoOverride string) error {
	if refreshCount <= 0 {
		refreshCount = defaultModelsRefreshCount
	}
	return DownloadWorkflowLogs(ctx, LogsDownloadOptions{
		Count:          refreshCount,
		OutputDir:      logsDir,
		RepoOverride:   repoOverride,
		ArtifactSets:   []string{string(ArtifactSetFirewall), string(ArtifactSetUsage)},
		SuppressRender: true,
	})
}

func buildModelAliasRows() ([]modelAliasRow, map[string][]string) {
	aliasMap := workflow.BuiltinModelAliases()
	aliases := make([]string, 0, len(aliasMap))
	for alias := range aliasMap {
		aliases = append(aliases, alias)
	}
	slices.Sort(aliases)

	rows := make([]modelAliasRow, 0, len(aliases))
	for _, alias := range aliases {
		rows = append(rows, modelAliasRow{Alias: alias, Targets: strings.Join(aliasMap[alias], ", ")})
	}
	return rows, aliasMap
}

type observedModelRecord struct {
	provider    string
	model       string
	sources     map[string]struct{}
	occurrences int
}

func collectObservedModelRows(logsDir string, aliasMap map[string][]string) ([]observedModelRow, []modelCatalogRow, *bool, []string) {
	warnings := make([]string, 0)
	records := make(map[string]*observedModelRecord)

	addObserved := func(provider, model, source string, occurrences int) {
		normalizedProvider := modelsdev.NormalizeProvider(provider)
		trimmedModel := strings.TrimSpace(model)
		if trimmedModel == "" {
			return
		}
		normalizedModel := strings.ToLower(trimmedModel)
		key := path.Join(normalizedProvider, normalizedModel)
		record := records[key]
		if record == nil {
			record = &observedModelRecord{provider: normalizedProvider, model: normalizedModel, sources: map[string]struct{}{}}
			records[key] = record
		}
		record.sources[source] = struct{}{}
		if occurrences <= 0 {
			occurrences = 1
		}
		record.occurrences += occurrences
	}

	// summary.json is generated from the same run-* directories that live alongside
	// it, so runs already represented in the summary are skipped when walking the
	// run directories to avoid counting their requests twice.
	summarizedRuns, summaryErr := collectObservedFromSummary(logsDir, addObserved)
	warnings = appendObservedCollectionWarning(warnings, summaryErr, "summary.json")
	warnings = appendObservedCollectionWarning(warnings, collectObservedFromRunDirs(logsDir, summarizedRuns, addObserved), "run directories")
	catalogRows, catalogComplete, reflectErr := collectObservedFromAWFReflect(logsDir, addObserved)
	warnings = appendObservedCollectionWarning(warnings, reflectErr, "awf-reflect artifacts")

	catalogIndex := make(map[string]struct{}, len(catalogRows))
	for _, row := range catalogRows {
		catalogIndex[path.Join(row.Provider, row.Model)] = struct{}{}
	}
	return buildObservedModelRows(records, aliasMap, catalogIndex), catalogRows, catalogComplete, warnings
}

// buildObservedModelRows converts collected observations into sorted report rows,
// ordered by descending occurrence count then provider and model.
func buildObservedModelRows(records map[string]*observedModelRecord, aliasMap map[string][]string, catalogIndex map[string]struct{}) []observedModelRow {
	rows := make([]observedModelRow, 0, len(records))
	for _, record := range records {
		sourceList := make([]string, 0, len(record.sources))
		for source := range record.sources {
			sourceList = append(sourceList, source)
		}
		slices.Sort(sourceList)

		rows = append(rows, observedModelRow{
			Provider:    record.provider,
			Model:       record.model,
			Sources:     strings.Join(sourceList, ", "),
			Occurrences: record.occurrences,
			InCatalog:   modelExistsInAWFCatalog(catalogIndex, record.provider, record.model),
			AliasHints:  inferAliasHints(record.provider, record.model, aliasMap),
		})
	}
	slices.SortFunc(rows, func(a, b observedModelRow) int {
		if a.Occurrences != b.Occurrences {
			if a.Occurrences > b.Occurrences {
				return -1
			}
			return 1
		}
		if cmp := strings.Compare(a.Provider, b.Provider); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Model, b.Model)
	})
	return rows
}

func modelExistsInAWFCatalog(catalogIndex map[string]struct{}, provider, model string) bool {
	_, exists := catalogIndex[path.Join(modelsdev.NormalizeProvider(provider), strings.ToLower(strings.TrimSpace(model)))]
	return exists
}

func appendObservedCollectionWarning(warnings []string, err error, scope string) []string {
	if err != nil {
		return append(warnings, "failed to parse "+scope+" for observed models: "+err.Error())
	}
	return warnings
}

func collectObservedFromSummary(logsDir string, addObserved func(provider, model, source string, occurrences int)) (map[int64]struct{}, error) {
	summarizedRuns := make(map[int64]struct{})
	summaryPath := filepath.Join(logsDir, "summary.json")
	content, err := os.ReadFile(summaryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return summarizedRuns, nil
		}
		return summarizedRuns, err
	}
	var payload struct {
		Runs []struct {
			RunID             int64 `json:"run_id"`
			TokenUsageSummary *struct {
				ByModel map[string]*struct {
					Provider string `json:"provider"`
					Requests int    `json:"requests"`
				} `json:"by_model"`
			} `json:"token_usage_summary"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(content, &payload); err != nil {
		return summarizedRuns, err
	}
	for _, run := range payload.Runs {
		if run.RunID > 0 {
			summarizedRuns[run.RunID] = struct{}{}
		}
		if run.TokenUsageSummary == nil {
			continue
		}
		for model, usage := range run.TokenUsageSummary.ByModel {
			provider := ""
			requests := 1
			if usage != nil {
				provider = usage.Provider
				if usage.Requests > 0 {
					requests = usage.Requests
				}
			}
			addObserved(provider, model, "summary", requests)
		}
	}
	return summarizedRuns, nil
}

func collectObservedFromRunDirs(logsDir string, summarizedRuns map[int64]struct{}, addObserved func(provider, model, source string, occurrences int)) error {
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "run-") {
			continue
		}
		if runID, parseErr := strconv.ParseInt(strings.TrimPrefix(entry.Name(), "run-"), 10, 64); parseErr == nil {
			if _, alreadyCounted := summarizedRuns[runID]; alreadyCounted {
				continue
			}
		}
		runDir := filepath.Join(logsDir, entry.Name())
		summary, err := analyzeTokenUsage(runDir, false)
		if err != nil || summary == nil {
			continue
		}
		for model, usage := range summary.ByModel {
			provider := ""
			requests := 1
			if usage != nil {
				provider = usage.Provider
				if usage.Requests > 0 {
					requests = usage.Requests
				}
			}
			addObserved(provider, model, "token-usage", requests)
		}
	}
	return nil
}

func collectObservedFromAWFReflect(logsDir string, addObserved func(provider, model, source string, occurrences int)) ([]modelCatalogRow, *bool, error) {
	catalog := make(map[string]modelCatalogRow)
	inventoryFound := false
	allInventoriesComplete := true
	walkErr := filepath.WalkDir(logsDir, func(filePath string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || d.Name() != "awf-reflect.json" {
			return nil
		}
		inventoryFound = true
		content, readErr := os.ReadFile(filePath)
		if readErr != nil {
			allInventoriesComplete = false
			return nil
		}
		var payload struct {
			ModelsFetchComplete *bool `json:"models_fetch_complete"`
			Endpoints           []struct {
				Provider string   `json:"provider"`
				Models   []string `json:"models"`
			} `json:"endpoints"`
		}
		if unmarshalErr := json.Unmarshal(content, &payload); unmarshalErr != nil {
			allInventoriesComplete = false
			return nil
		}
		if payload.ModelsFetchComplete == nil || !*payload.ModelsFetchComplete {
			allInventoriesComplete = false
		}
		for _, endpoint := range payload.Endpoints {
			for _, model := range endpoint.Models {
				addObserved(endpoint.Provider, model, "awf-reflect", 1)
				provider := modelsdev.NormalizeProvider(endpoint.Provider)
				normalizedModel := strings.ToLower(strings.TrimSpace(model))
				if normalizedModel == "" {
					continue
				}
				key := path.Join(provider, normalizedModel)
				catalog[key] = modelCatalogRow{Provider: provider, Model: normalizedModel}
			}
		}
		return nil
	})
	if walkErr != nil && !os.IsNotExist(walkErr) {
		return nil, nil, walkErr
	}
	rows := make([]modelCatalogRow, 0, len(catalog))
	for _, row := range catalog {
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b modelCatalogRow) int {
		if cmp := strings.Compare(a.Provider, b.Provider); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Model, b.Model)
	})
	if !inventoryFound {
		return rows, nil, nil
	}
	return rows, &allInventoriesComplete, nil
}

func inferAliasHints(provider, model string, aliasMap map[string][]string) string {
	modelIDs := []string{path.Join(provider, model)}
	switch provider {
	case "github-copilot":
		modelIDs = append(modelIDs, path.Join("copilot", model), path.Join("github", model), path.Join("github_models", model))
	case "copilot", "github", "github_models":
		modelIDs = append(modelIDs, path.Join("github-copilot", model))
	}
	matches := make([]string, 0)
	for alias, entries := range aliasMap {
		for _, entry := range entries {
			if !strings.Contains(entry, "/") {
				continue
			}
			for _, modelID := range modelIDs {
				if wildcardMatch(entry, modelID) {
					matches = append(matches, alias)
					break
				}
			}
		}
	}
	if len(matches) == 0 {
		return ""
	}
	slices.Sort(matches)
	if len(matches) > maxAliasHints {
		matches = matches[:maxAliasHints]
	}
	return strings.Join(matches, ", ")
}

func wildcardMatch(pattern, value string) bool {
	matched, err := filepath.Match(pattern, value)
	if err != nil {
		return false
	}
	return matched
}
