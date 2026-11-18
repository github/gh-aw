package cli

import (
	"errors"
	"slices"
	"strings"
	"time"
)

type valueWindowPolicy struct {
	Version             int    `json:"version"`
	Baseline            string `json:"baseline"`
	BeforeDays          int    `json:"before_days"`
	AfterDays           int    `json:"after_days"`
	OutcomeLagDays      int    `json:"outcome_lag_days"`
	DurabilityDays      int    `json:"durability_days"`
	SampleEveryHours    int    `json:"sample_every_hours"`
	TrendEveryDays      int    `json:"trend_every_days"`
	MinimumBefore       int    `json:"minimum_before_samples"`
	MinimumAfter        int    `json:"minimum_after_samples"`
	IsolationBeforeDays int    `json:"isolation_before_days"`
	IsolationAfterDays  int    `json:"isolation_after_days"`
	Overlap             string `json:"overlap"`
}

func defaultValueWindowPolicy() valueWindowPolicy {
	return valueWindowPolicy{
		Version: 1, Baseline: "exact_parent", BeforeDays: 7, AfterDays: 7,
		DurabilityDays: 7, SampleEveryHours: 24, TrendEveryDays: 7,
		MinimumBefore: 7, MinimumAfter: 7, IsolationBeforeDays: 7, IsolationAfterDays: 7, Overlap: "workflow",
	}
}

func resolveValueWindowPolicy(recipe *valueRecipe) (valueWindowPolicy, error) {
	return resolveValuePolicy(recipe, "exact_parent")
}

func resolveValuePolicy(recipe *valueRecipe, baseline string) (valueWindowPolicy, error) {
	policy := defaultValueWindowPolicy()
	policy.Baseline = baseline
	if recipe != nil && len(recipe.Window) != 0 {
		if err := decodeValueDeclaration(recipe.Window, &policy); err != nil {
			return policy, err
		}
	}
	return policy, validateValueWindowPolicy(policy)
}

func validateValueWindowPolicy(p valueWindowPolicy) error {
	if p.Version != 1 || !slices.Contains([]string{"exact_parent", "paired_observation"}, p.Baseline) ||
		p.Overlap != "workflow" && p.Overlap != "changed_files" {
		return errors.New("window requires version 1, baseline exact_parent or paired_observation, and overlap workflow or changed_files")
	}
	for _, days := range []int{p.BeforeDays, p.AfterDays, p.DurabilityDays, p.TrendEveryDays} {
		if days < 1 || days > 365 {
			return errors.New("window before_days, after_days, durability_days, and trend_every_days must be between 1 and 365")
		}
	}
	for _, days := range []int{p.OutcomeLagDays, p.IsolationBeforeDays, p.IsolationAfterDays} {
		if days < 0 || days > 365 {
			return errors.New("window lag and isolation days must be between 0 and 365")
		}
	}
	if p.SampleEveryHours < 1 || p.SampleEveryHours > 24 || 24%p.SampleEveryHours != 0 {
		return errors.New("window sample_every_hours must be a positive divisor of 24")
	}
	before, after := p.BeforeDays*24/p.SampleEveryHours, p.AfterDays*24/p.SampleEveryHours
	if before+after > 1024 || p.MinimumBefore < 1 || p.MinimumBefore > before ||
		p.MinimumAfter < 1 || p.MinimumAfter > after || p.DurabilityDays < p.OutcomeLagDays {
		return errors.New("window requires at most 1024 sample slots, attainable positive minimum samples, and durability covering outcome lag")
	}
	return nil
}

func (p valueWindowPolicy) resolved() valueWindowPolicy {
	if p.Version == 0 {
		return defaultValueWindowPolicy()
	}
	return p
}

func valueDays(days int) time.Duration {
	return time.Duration(days) * 24 * time.Hour
}

func prepareValueOverlap(git valueGit, definition valueMeasurementDefinition, outputs []valueOutput, cutoff time.Time) error {
	for i := range outputs {
		output := &outputs[i]
		if output.Merged.IsZero() || output.Merged.After(cutoff) {
			continue
		}
		sha, err := resolveValueMerge(git.ctx, definition, *output)
		if err != nil {
			return err
		}
		files, err := git.run("diff-tree", "--no-commit-id", "--name-only", "-r", "-z", sha+"^1", sha)
		if err != nil {
			return err
		}
		output.SHA = sha
		output.Files = strings.Split(strings.TrimSuffix(files, "\x00"), "\x00")
	}
	return nil
}

func valueOverlapReason(window *valueWindow, outputs []valueOutput) string {
	policy := window.Policy.resolved()
	for _, other := range outputs {
		delta := other.Merged.Sub(window.MergedAt)
		if other.PR == window.PR || other.Merged.IsZero() ||
			delta < -valueDays(policy.IsolationBeforeDays) || delta > valueDays(policy.IsolationAfterDays) {
			continue
		}
		if policy.Overlap == "workflow" {
			return "shared_workflow_merge_window"
		}
		if len(window.ChangedFiles) == 0 || len(other.Files) == 0 {
			return "overlap_evidence_missing"
		}
		if slices.ContainsFunc(window.ChangedFiles, func(file string) bool { return slices.Contains(other.Files, file) }) {
			return "overlapping_changed_files"
		}
	}
	return ""
}
