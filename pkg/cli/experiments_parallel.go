package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

type parallelExperimentIdentity struct {
	RunID      string `json:"run_id"`
	RunAttempt string `json:"run_attempt"`
	Job        string `json:"job"`
	MatrixID   string `json:"matrix_id"`
}

type parallelExperimentConfig struct {
	ID            string   `json:"id"`
	Experiment    string   `json:"experiment"`
	Variant       string   `json:"variant"`
	Engine        *string  `json:"engine"`
	Model         *string  `json:"model"`
	Harness       *string  `json:"harness"`
	MaxAICredits  *float64 `json:"max_ai_credits"`
	TimeoutMinute *float64 `json:"timeout_minutes"`
	Admitted      *bool    `json:"admitted"`
	SkipReason    *string  `json:"skip_reason"`
}

type parallelExperimentUsage struct {
	Source           *string  `json:"source"`
	AIC              *float64 `json:"aic"`
	InputTokens      *float64 `json:"input_tokens"`
	OutputTokens     *float64 `json:"output_tokens"`
	CacheReadTokens  *float64 `json:"cache_read_tokens"`
	CacheWriteTokens *float64 `json:"cache_write_tokens"`
	AICSource        *string  `json:"aic_source"`
	Warnings         []string `json:"warnings"`
}

type parallelExperimentManifest struct {
	Version               int                        `json:"version"`
	Identity              parallelExperimentIdentity `json:"identity"`
	Configured            parallelExperimentConfig   `json:"configured"`
	Resolved              json.RawMessage            `json:"resolved"`
	Admission             json.RawMessage            `json:"admission"`
	ConfiguredAt          *string                    `json:"configured_at"`
	FinalizedAt           *string                    `json:"finalized_at"`
	State                 string                     `json:"state"`
	Admitted              *bool                      `json:"admitted"`
	Executed              *bool                      `json:"executed"`
	Completed             *bool                      `json:"completed"`
	Failed                *bool                      `json:"failed"`
	Skipped               *bool                      `json:"skipped"`
	ExecutionStartedAt    *string                    `json:"execution_started_at"`
	ExecutionEndedAt      *string                    `json:"execution_ended_at"`
	Outcomes              json.RawMessage            `json:"outcomes"`
	Assignments           json.RawMessage            `json:"assignments"`
	Session               json.RawMessage            `json:"session"`
	StagedSafeOutputs     json.RawMessage            `json:"staged_safe_outputs"`
	Evidence              json.RawMessage            `json:"evidence"`
	ConfigurationEvidence json.RawMessage            `json:"configuration_evidence"`
	Usage                 *parallelExperimentUsage   `json:"usage"`
	MissingEvidence       []string                   `json:"missing_evidence"`
}

type parallelExperimentArtifact struct {
	Path     string                     `json:"path"`
	Manifest parallelExperimentManifest `json:"manifest"`
}

type parallelExperimentGap struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type parallelExperimentReport struct {
	Scope        string                       `json:"scope"`
	Evaluation   string                       `json:"evaluation"`
	Coverage     string                       `json:"coverage"`
	Artifacts    []parallelExperimentArtifact `json:"artifacts"`
	EvidenceGaps []parallelExperimentGap      `json:"evidence_gaps"`
}

func newExperimentsParallelSubcommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "parallel <download-directory> [download-directory...]",
		Short: "Inspect downloaded parallel experiment artifacts",
		Long: `Inspect parallel-experiment-<attempt>-<id>-agent directories downloaded with
gh run download <run-id> --pattern 'parallel-experiment-*'. Pass run directories
or individual artifact directories; keep each artifact in its own directory.

This local-only report does not inspect the production ledger, discover missing
artifacts, reconcile Actions jobs, evaluate results, or sum production usage.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := collectParallelExperimentReport(args)
			if err != nil {
				return err
			}
			jsonOutput, _ := cmd.Flags().GetBool("json")
			if !jsonOutput {
				return renderParallelExperimentReport(cmd.OutOrStdout(), report)
			}
			data, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return err
		},
	}
	addJSONFlag(cmd)
	return cmd
}

func renderParallelExperimentReport(w io.Writer, report *parallelExperimentReport) error {
	if _, err := fmt.Fprintf(w, "%s\n%s; %s\n", report.Scope, report.Evaluation, report.Coverage); err != nil {
		return err
	}
	for _, artifact := range report.Artifacts {
		manifest := artifact.Manifest
		data, err := json.Marshal(manifest)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "\nArtifact %q\n%s\n", artifact.Path, data); err != nil {
			return err
		}
	}
	for _, gap := range report.EvidenceGaps {
		if _, err := fmt.Fprintf(w, "\nEvidence gap %q: %s\n", gap.Path, gap.Reason); err != nil {
			return err
		}
	}
	return nil
}

func collectParallelExperimentReport(roots []string) (*parallelExperimentReport, error) {
	report := &parallelExperimentReport{
		Scope:        "parallel experimental artifacts only; production evidence is excluded",
		Evaluation:   "not evaluated",
		Coverage:     "unknown: downloaded artifacts cannot establish complete run or job coverage",
		Artifacts:    []parallelExperimentArtifact{},
		EvidenceGaps: []parallelExperimentGap{},
	}
	seen := map[parallelExperimentIdentity]string{}
	visited := map[string]struct{}{}
	for _, root := range roots {
		info, err := os.Lstat(root)
		if err != nil {
			return nil, fmt.Errorf("inspect %q: %w", root, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%q must be a directory, not a symlink or file", root)
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() || !isParallelArtifactName(entry.Name()) {
				return nil
			}
			absolute, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			if _, ok := visited[absolute]; ok {
				return filepath.SkipDir
			}
			visited[absolute] = struct{}{}
			item := readParallelExperiment(filepath.Dir(path), entry.Name())
			if item.Error != "" {
				report.EvidenceGaps = append(report.EvidenceGaps, parallelExperimentGap{Path: path, Reason: item.Error})
				return filepath.SkipDir
			}
			manifest, err := decodeParallelExperimentManifest(path, item.Manifest)
			if err != nil {
				report.EvidenceGaps = append(report.EvidenceGaps, parallelExperimentGap{Path: path, Reason: err.Error()})
				return filepath.SkipDir
			}
			if prior, ok := seen[manifest.Identity]; ok {
				return fmt.Errorf("duplicate parallel experiment identity in %q and %q", prior, path)
			}
			seen[manifest.Identity] = path
			report.Artifacts = append(report.Artifacts, parallelExperimentArtifact{Path: path, Manifest: manifest})
			return filepath.SkipDir
		})
		if err != nil {
			return nil, fmt.Errorf("scan %q: %w", root, err)
		}
	}
	return report, nil
}

func isParallelArtifactName(name string) bool {
	return strings.HasPrefix(name, "parallel-experiment-") && strings.HasSuffix(name, "-agent")
}

func decodeParallelExperimentManifest(dir string, data json.RawMessage) (parallelExperimentManifest, error) {
	var manifest parallelExperimentManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, fmt.Errorf("invalid manifest: %w", err)
	}
	identity := manifest.Identity
	if manifest.Version != 1 {
		return manifest, fmt.Errorf("unsupported manifest version %d", manifest.Version)
	}
	if identity.RunID == "" || identity.RunAttempt == "" || identity.Job == "" || identity.MatrixID == "" ||
		manifest.Configured.ID != identity.MatrixID || manifest.Configured.Experiment == "" || manifest.Configured.Variant == "" ||
		filepath.Base(dir) != "parallel-experiment-"+identity.RunAttempt+"-"+identity.MatrixID+"-agent" {
		return manifest, errors.New("manifest identity or artifact name mismatch")
	}
	return manifest, nil
}
