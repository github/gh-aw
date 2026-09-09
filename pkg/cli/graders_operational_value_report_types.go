package cli

import (
	"encoding/json"
	"strconv"
	"time"
)

const operationalValueReportSchemaVersion = 1

type operationalValueReportRun struct {
	ID         string    `json:"id"`
	Attempt    int       `json:"attempt"`
	CreatedAt  time.Time `json:"createdAt"`
	Conclusion string    `json:"conclusion"`
	URL        string    `json:"url"`
	SHA        string    `json:"sha"`
	Ref        string    `json:"ref"`
	EventName  string    `json:"eventName"`
}

type operationalValueReportDefinition struct {
	SchemaVersion     int                                      `json:"schemaVersion"`
	Grader            string                                   `json:"grader"`
	Repository        string                                   `json:"repository"`
	WorkflowName      string                                   `json:"workflowName"`
	SourcePath        string                                   `json:"sourcePath"`
	Adoption          operationalValueReportAdoption           `json:"adoption"`
	OperationalValue  string                                   `json:"operationalValue"`
	Evidence          operationalValueReportEvidence           `json:"evidence"`
	PrimaryMetric     operationalValueReportMetric             `json:"primaryMetric"`
	DiagnosticMetrics []operationalValueReportDiagnosticMetric `json:"diagnosticMetrics,omitempty"`
	Baseline          operationalValueReportBaseline           `json:"baseline"`
	Raw               json.RawMessage                          `json:"-"`
}

type operationalValueReportAdoption struct {
	Commit    string `json:"commit"`
	AdoptedAt string `json:"adoptedAt"`
}

type operationalValueReportEvidence struct {
	Opportunity  string   `json:"opportunity"`
	Assignment   string   `json:"assignment"`
	Accepted     string   `json:"accepted"`
	Repositories []string `json:"repositories"`
	Collection   string   `json:"collection"`
	Maturation   string   `json:"maturation"`
	ZeroRule     string   `json:"zeroRule"`
	MissingRule  string   `json:"missingRule"`
}

type operationalValueReportMetric struct {
	ID        string `json:"id"`
	Formula   string `json:"formula"`
	Direction string `json:"direction"`
}

type operationalValueReportDiagnosticMetric struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Formula     string `json:"formula"`
	Direction   string `json:"direction"`
	Aggregation string `json:"aggregation"`
}

type operationalValueReportBaseline struct {
	Mode           string           `json:"mode"`
	Value          *float64         `json:"value"`
	EvidenceCutoff *string          `json:"evidenceCutoff"`
	Provenance     []map[string]any `json:"provenance"`
}

type operationalValueReportEvaluator struct {
	WorkflowID       string
	WorkflowPath     string
	EvaluatorRun     string
	EvaluatorPath    string
	EvaluatorContent string
	EvaluatorDigest  string
	cleanup          func()
	Definition       operationalValueReportDefinition
	GraderName       string
	GraderUnit       string
	GraderDirection  string
	GraderThreshold  *float64
	GraderConfig     map[string]any
}

type operationalValueReport struct {
	SchemaVersion    int                                      `json:"schemaVersion"`
	GeneratedAt      string                                   `json:"generatedAt"`
	Repository       string                                   `json:"repository"`
	WorkflowID       string                                   `json:"workflowId"`
	WorkflowName     string                                   `json:"workflowName"`
	SourcePath       string                                   `json:"sourcePath"`
	OperationalValue string                                   `json:"operationalValue"`
	Window           operationalValueReportWindow             `json:"window"`
	Evaluator        operationalValueReportEvaluatorReference `json:"evaluator"`
	Baseline         operationalValueReportBaseline           `json:"baseline"`
	Coverage         operationalValueReportCoverage           `json:"coverage"`
	Summary          operationalValueReportSummary            `json:"summary"`
	Weekly           []operationalValueReportWeek             `json:"weekly"`
	Diagnostics      []operationalValueReportDiagnosticSeries `json:"diagnostics,omitempty"`
	Observations     []operationalValueReportObservation      `json:"observations"`
	Caveat           string                                   `json:"caveat"`
}

type operationalValueReportWindow struct {
	StartAt string `json:"startAt"`
	EndAt   string `json:"endAt"`
}

type operationalValueReportEvaluatorReference struct {
	Path       string          `json:"path"`
	SHA256     string          `json:"sha256"`
	Definition json.RawMessage `json:"definition"`
}

type operationalValueReportCoverage struct {
	RunCount                  int `json:"runCount"`
	NumericCount              int `json:"numericCount"`
	MatureCount               int `json:"matureCount"`
	UnavailableCount          int `json:"unavailableCount"`
	ErrorCount                int `json:"errorCount"`
	DistinctOpportunityCount  int `json:"distinctOpportunityCount"`
	DuplicateOpportunityCount int `json:"duplicateOpportunityCount"`
	WeeklyCacheHits           int `json:"weeklyCacheHits"`
	EvaluatedCount            int `json:"evaluatedCount"`
}

type operationalValueReportSummary struct {
	Mean                    *float64 `json:"mean"`
	Median                  *float64 `json:"median"`
	Minimum                 *float64 `json:"minimum"`
	Maximum                 *float64 `json:"maximum"`
	First                   *float64 `json:"first"`
	Latest                  *float64 `json:"latest"`
	Change                  *float64 `json:"change"`
	LatestDeltaFromBaseline *float64 `json:"latestDeltaFromBaseline"`
}

type operationalValueReportWeek struct {
	WeekStart                string   `json:"weekStart"`
	WeekEnd                  string   `json:"weekEnd"`
	RunCount                 int      `json:"runCount"`
	NumericCount             int      `json:"numericCount"`
	DistinctOpportunityCount int      `json:"distinctOpportunityCount"`
	Mean                     *float64 `json:"mean"`
	Minimum                  *float64 `json:"minimum"`
	Maximum                  *float64 `json:"maximum"`
}

type operationalValueReportDiagnosticSeries struct {
	Metric  operationalValueReportDiagnosticMetric `json:"metric"`
	Summary operationalValueReportSummary          `json:"summary"`
	Weekly  []operationalValueReportDiagnosticWeek `json:"weekly"`
}

type operationalValueReportDiagnosticWeek struct {
	WeekStart    string   `json:"weekStart"`
	WeekEnd      string   `json:"weekEnd"`
	NumericCount int      `json:"numericCount"`
	Value        *float64 `json:"value"`
}

type operationalValueReportObservation struct {
	ID                string                    `json:"id"`
	Run               operationalValueReportRun `json:"run"`
	Value             *float64                  `json:"value"`
	Status            string                    `json:"status"`
	Message           string                    `json:"message,omitempty"`
	OpportunityKey    string                    `json:"opportunityKey"`
	EvidenceAt        string                    `json:"evidenceAt"`
	EvidenceCutoff    string                    `json:"evidenceCutoff"`
	MaturesAt         string                    `json:"maturesAt"`
	Mature            bool                      `json:"mature"`
	Case              map[string]any            `json:"case"`
	Provenance        []map[string]any          `json:"provenance"`
	Diagnostics       map[string]any            `json:"diagnostics,omitempty"`
	BaselineValue     *float64                  `json:"baselineValue"`
	DeltaFromBaseline *float64                  `json:"deltaFromBaseline"`
	EvaluatorDigest   string                    `json:"evaluatorDigest"`
	Source            string                    `json:"source"`
}

func operationalValueReportObservationKey(observation operationalValueReportObservation) string {
	return observation.Run.ID + ":" + strconv.Itoa(observation.Run.Attempt)
}

func operationalValueReportObservationID(repository, workflowID string, observation operationalValueReportObservation) string {
	return repository + ":" + workflowID + ":" + operationalValueReportObservationKey(observation) + ":" + observation.EvaluatorDigest
}
