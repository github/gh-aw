package cli

import (
	"encoding/json"
	"math"
	"math/big"
	"strconv"
	"strings"
)

type harborEvaluationStats struct {
	NTrials        int                            `json:"n_trials"`
	NErrors        int                            `json:"n_errors"`
	RewardStats    map[string]map[string][]string `json:"reward_stats"`
	Metrics        []any                          `json:"metrics"`
	PassAtK        map[string]float64             `json:"pass_at_k"`
	ExceptionStats map[string][]string            `json:"exception_stats"`
}

// Match Harbor JobStats.increment: every imported record is completed, but only
// records with actual verifier rewards count as evaluated trials.
func harborJobStats(results []map[string]any) map[string]any {
	evals := make(map[string]*harborEvaluationStats)
	for _, trial := range results {
		group, valid := harborEvaluationGroup(trial)
		if !valid {
			continue
		}
		stats := evals[group]
		if stats == nil {
			stats = &harborEvaluationStats{
				RewardStats: make(map[string]map[string][]string), Metrics: []any{},
				PassAtK: make(map[string]float64), ExceptionStats: make(map[string][]string),
			}
			evals[group] = stats
		}
		verifier, ok := trial["verifier_result"].(map[string]any)
		if !ok {
			continue
		}
		rewards, ok := verifier["rewards"].(map[string]json.Number)
		if !ok || rewards == nil {
			continue
		}
		trialName, ok := trial["trial_name"].(string)
		if !ok || trialName == "" {
			continue
		}
		stats.NTrials++
		for metric, value := range rewards {
			if stats.RewardStats[metric] == nil {
				stats.RewardStats[metric] = make(map[string][]string)
			}
			key := harborRewardStatsKey(value)
			stats.RewardStats[metric][key] = append(stats.RewardStats[metric][key], trialName)
		}
	}
	return map[string]any{
		"n_completed_trials": len(results), "n_errored_trials": 0, "evals": evals,
	}
}

func harborEvaluationGroup(trial map[string]any) (string, bool) {
	agent, ok := trial["agent_info"].(map[string]any)
	if !ok {
		return "", false
	}
	group, ok := agent["name"].(string)
	if !ok || group == "" {
		return "", false
	}
	if model, ok := agent["model_info"].(map[string]any); ok {
		if modelName, ok := model["name"].(string); ok && modelName != "" {
			group += "__" + modelName
		}
	}
	return group + "__adhoc", true
}

// Python reward-stat dictionaries equate integral floats and integers. Canonical
// keys preserve that grouping without rounding original integer reward values.
func harborRewardStatsKey(number json.Number) string {
	if !strings.ContainsAny(string(number), ".eE") {
		if integer, ok := new(big.Int).SetString(string(number), 10); ok {
			return integer.String()
		}
	}
	value, _ := number.Float64() // Eligibility already rejects non-finite/out-of-range numbers.
	if value == 0 {
		return "0"
	}
	if math.Trunc(value) == value {
		return strconv.FormatFloat(value, 'f', 0, 64)
	}
	return strconv.FormatFloat(value, 'g', -1, 64)
}
