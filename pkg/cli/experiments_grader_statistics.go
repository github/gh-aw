package cli

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/github/gh-aw/pkg/workflow"
	"gonum.org/v1/gonum/stat"
	"gonum.org/v1/gonum/stat/distuv"
)

// MetricComparison is one control-versus-variant comparison of grader observations.
type MetricComparison struct {
	ControlVariant         string   `json:"control_variant"`
	Variant                string   `json:"variant"`
	AnalysisType           string   `json:"analysis_type"`
	Delta                  float64  `json:"delta"`
	PValue                 *float64 `json:"p_value,omitempty"`
	ProbabilitySuperiority *float64 `json:"probability_superiority,omitempty"`
	Error                  string   `json:"error,omitempty"`
}

func meanGraderObservations(observations []GraderMetricObservation) float64 {
	if len(observations) == 0 {
		return 0
	}
	sum := 0.0
	for _, observation := range observations {
		sum += observation.Value
	}
	return sum / float64(len(observations))
}

func graderObservationMetricType(cfg *workflow.ExperimentConfig, set *graderMetricObservationSet) string {
	if cfg != nil && (cfg.AnalysisType == "proportion_test" || cfg.AnalysisType == "bayesian_ab") {
		return "binary"
	}
	hasObservation := false
	for _, observations := range set.ByVariant {
		for _, observation := range observations {
			hasObservation = true
			if !observation.Binary {
				return "continuous"
			}
		}
	}
	if hasObservation {
		return "binary"
	}
	return ""
}

func computeGraderMetricComparisons(
	cfg *workflow.ExperimentConfig,
	set *graderMetricObservationSet,
	variantNames []string,
	metricType string,
) []MetricComparison {
	if len(variantNames) < 2 {
		return nil
	}
	control := variantNames[0]
	if cfg != nil && len(cfg.Variants) > 0 && slices.Contains(variantNames, cfg.Variants[0]) {
		control = cfg.Variants[0]
	}
	analysisType := ""
	if cfg != nil {
		analysisType = cfg.AnalysisType
	}
	if analysisType == "" {
		if metricType == "binary" {
			analysisType = "proportion_test"
		} else {
			analysisType = "t_test"
		}
	}

	controlValues := graderObservationValues(set.ByVariant[control])
	comparisons := make([]MetricComparison, 0, len(variantNames)-1)
	for _, variant := range variantNames {
		if variant == control {
			continue
		}
		variantValues := graderObservationValues(set.ByVariant[variant])
		comparison := MetricComparison{
			ControlVariant: control,
			Variant:        variant,
			AnalysisType:   analysisType,
			Delta:          meanValues(variantValues) - meanValues(controlValues),
		}
		switch analysisType {
		case "t_test":
			pValue, err := welchTTest(controlValues, variantValues)
			setComparisonPValue(&comparison, pValue, err)
		case "mann_whitney":
			pValue, err := mannWhitneyTest(controlValues, variantValues)
			setComparisonPValue(&comparison, pValue, err)
		case "proportion_test":
			pValue, err := twoProportionTest(controlValues, variantValues)
			setComparisonPValue(&comparison, pValue, err)
		case "bayesian_ab":
			probability, err := betaBinomialProbability(controlValues, variantValues)
			if err != nil {
				comparison.Error = err.Error()
			} else {
				if set.Direction == "lower_is_better" {
					// betaBinomialProbability always computes P(variant > control); for a
					// lower-is-better metric "superiority" means the variant is smaller, so
					// invert to report P(variant < control).
					probability = 1 - probability
				}
				comparison.ProbabilitySuperiority = &probability
			}
		default:
			comparison.Error = fmt.Sprintf("unsupported analysis type %q", analysisType)
		}
		comparisons = append(comparisons, comparison)
	}
	return comparisons
}

func setComparisonPValue(comparison *MetricComparison, pValue float64, err error) {
	if err != nil {
		comparison.Error = err.Error()
		return
	}
	comparison.PValue = &pValue
}

func graderObservationValues(observations []GraderMetricObservation) []float64 {
	values := make([]float64, len(observations))
	for i, observation := range observations {
		values[i] = observation.Value
	}
	return values
}

func meanValues(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	return stat.Mean(values, nil)
}

func welchTTest(control, variant []float64) (float64, error) {
	if len(control) < 2 || len(variant) < 2 {
		return 0, errors.New("t_test requires at least two usable observations per variant")
	}
	controlMean, controlVariance := stat.MeanVariance(control, nil)
	variantMean, variantVariance := stat.MeanVariance(variant, nil)
	controlTerm := controlVariance / float64(len(control))
	variantTerm := variantVariance / float64(len(variant))
	standardErrorSquared := controlTerm + variantTerm
	difference := variantMean - controlMean
	if standardErrorSquared == 0 {
		var controlValue, variantValue float64
		for _, value := range control {
			controlValue = value
			break
		}
		for _, value := range variant {
			variantValue = value
			break
		}
		if controlValue == variantValue {
			return 1, nil
		}
		return 0, nil
	}
	t := difference / math.Sqrt(standardErrorSquared)
	numerator := standardErrorSquared * standardErrorSquared
	denominator := controlTerm*controlTerm/float64(len(control)-1) +
		variantTerm*variantTerm/float64(len(variant)-1)
	if denominator == 0 {
		return 0, errors.New("t_test variance is degenerate")
	}
	degreesOfFreedom := numerator / denominator
	distribution := distuv.StudentsT{Mu: 0, Sigma: 1, Nu: degreesOfFreedom}
	return clampProbability(2 * (1 - distribution.CDF(math.Abs(t)))), nil
}

type rankedValue struct {
	value   float64
	variant bool
}

func mannWhitneyTest(control, variant []float64) (float64, error) {
	if len(control) == 0 || len(variant) == 0 {
		return 0, errors.New("mann_whitney requires at least one usable observation per variant")
	}
	ranked := make([]rankedValue, 0, len(control)+len(variant))
	for _, value := range control {
		ranked = append(ranked, rankedValue{value: value})
	}
	for _, value := range variant {
		ranked = append(ranked, rankedValue{value: value, variant: true})
	}
	slices.SortFunc(ranked, func(a, b rankedValue) int {
		if a.value < b.value {
			return -1
		}
		if a.value > b.value {
			return 1
		}
		return 0
	})

	variantRankSum := 0.0
	tieCorrection := 0.0
	for start := 0; start < len(ranked); {
		end := start + 1
		for end < len(ranked) && ranked[end].value == ranked[start].value {
			end++
		}
		averageRank := (float64(start+1) + float64(end)) / 2
		for i := start; i < end; i++ {
			if ranked[i].variant {
				variantRankSum += averageRank
			}
		}
		tieSize := float64(end - start)
		tieCorrection += tieSize*tieSize*tieSize - tieSize
		start = end
	}

	nControl := float64(len(control))
	nVariant := float64(len(variant))
	u := variantRankSum - nVariant*(nVariant+1)/2
	meanU := nControl * nVariant / 2
	total := nControl + nVariant
	varianceU := nControl * nVariant / 12 * ((total + 1) - tieCorrection/(total*(total-1)))
	if varianceU <= 0 {
		if u == meanU {
			return 1, nil
		}
		return 0, nil
	}
	z := (u - meanU) / math.Sqrt(varianceU)
	normal := distuv.UnitNormal
	return clampProbability(2 * (1 - normal.CDF(math.Abs(z)))), nil
}

func twoProportionTest(control, variant []float64) (float64, error) {
	if len(control) == 0 || len(variant) == 0 {
		return 0, errors.New("proportion_test requires at least one usable observation per variant")
	}
	if !areBinaryValues(control) || !areBinaryValues(variant) {
		return 0, errors.New("proportion_test requires boolean or numeric 0/1 grader values")
	}
	controlSuccesses := sumValues(control)
	variantSuccesses := sumValues(variant)
	controlN := float64(len(control))
	variantN := float64(len(variant))
	pooled := (controlSuccesses + variantSuccesses) / (controlN + variantN)
	variance := pooled * (1 - pooled) * (1/controlN + 1/variantN)
	difference := variantSuccesses/variantN - controlSuccesses/controlN
	if variance == 0 {
		if difference == 0 {
			return 1, nil
		}
		return 0, nil
	}
	z := difference / math.Sqrt(variance)
	return clampProbability(2 * (1 - distuv.UnitNormal.CDF(math.Abs(z)))), nil
}

func betaBinomialProbability(control, variant []float64) (float64, error) {
	if len(control) == 0 || len(variant) == 0 {
		return 0, errors.New("bayesian_ab requires at least one usable observation per variant")
	}
	if !areBinaryValues(control) || !areBinaryValues(variant) {
		return 0, errors.New("bayesian_ab requires boolean or numeric 0/1 grader values")
	}
	controlSuccesses := sumValues(control)
	variantSuccesses := sumValues(variant)
	controlPosterior := distuv.Beta{
		Alpha: controlSuccesses + 1,
		Beta:  float64(len(control)) - controlSuccesses + 1,
	}
	variantPosterior := distuv.Beta{
		Alpha: variantSuccesses + 1,
		Beta:  float64(len(variant)) - variantSuccesses + 1,
	}
	const intervals = 4096
	sum := 0.0
	for i := range intervals {
		x := (float64(i) + 0.5) / intervals
		sum += variantPosterior.Prob(x) * controlPosterior.CDF(x)
	}
	return clampProbability(sum / intervals), nil
}

func areBinaryValues(values []float64) bool {
	for _, value := range values {
		if value != 0 && value != 1 {
			return false
		}
	}
	return true
}

func sumValues(values []float64) float64 {
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	return sum
}

func clampProbability(value float64) float64 {
	return min(1, max(0, value))
}
