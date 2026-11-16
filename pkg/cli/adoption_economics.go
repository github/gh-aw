package cli

import "math"

func (s *AdoptionSummary) addAIC(aic float64) {
	if aic < 0 || math.IsNaN(aic) || math.IsInf(aic, 0) {
		adoptionEvalLog.Printf("Cannot compute adoption efficiency from invalid AIC: %v", aic)
		return
	}
	if s.Accepted == 0 || aic == 0 {
		return
	}
	s.CostPerAcceptedAdoption = aic / float64(s.Accepted)
}

func loadAdoptionAIC(runDir string) (float64, error) {
	// Cached token usage is agent-only; adoption cost includes detection usage too.
	usage, err := analyzeTokenUsageAICOnly(runDir, false)
	if err != nil || usage == nil {
		return 0, err
	}
	return usage.TotalAIC, nil
}
