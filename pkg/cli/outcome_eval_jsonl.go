package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/github/gh-aw/pkg/constants"
)

type lifecycleSessionEvent struct {
	Type       string                          `json:"type"`
	Timestamp  string                          `json:"timestamp,omitempty"`
	Data       lifecycleSessionEventData       `json:"data"`
	Provenance lifecycleSessionEventProvenance `json:"provenance"`
}

type lifecycleSessionEventData struct {
	SourceRunID         string `json:"sourceRunId"`
	SourceWorkflow      string `json:"sourceWorkflow,omitempty"`
	Repository          string `json:"repository,omitempty"`
	OutputType          string `json:"outputType"`
	DeliveryState       string `json:"deliveryState"`
	DeliverySignal      string `json:"deliverySignal"`
	DeliveryAt          string `json:"deliveryAt"`
	DeliveryObservedAt  string `json:"deliveryObservedAt"`
	DeliverySource      string `json:"deliverySource"`
	AdoptionState       string `json:"adoptionState"`
	AdoptionSignal      string `json:"adoptionSignal"`
	AdoptionAt          string `json:"adoptionAt,omitempty"`
	AdoptionObservedAt  string `json:"adoptionObservedAt"`
	AdoptionSource      string `json:"adoptionSource"`
	AdoptionRuleVersion string `json:"adoptionRuleVersion"`
	AcceptanceKind      string `json:"acceptanceKind,omitempty"`
	HumanVerified       bool   `json:"humanVerified"`
	AdoptionCoverage    string `json:"adoptionCoverage"`
	SurvivalState       string `json:"survivalState"`
	SurvivalSignal      string `json:"survivalSignal,omitempty"`
	SurvivalObservedAt  string `json:"survivalObservedAt,omitempty"`
	SurvivalCoverage    string `json:"survivalCoverage"`
}

type lifecycleSessionEventProvenance struct {
	Component string `json:"component"`
	Phase     string `json:"phase"`
	Path      string `json:"path"`
	Index     int    `json:"index"`
}

func writeOutcomeJSONL(dir string, runID int64, reports []OutcomeReport) error {
	if err := os.MkdirAll(dir, constants.DirPermSensitive); err != nil {
		return fmt.Errorf("create adoption directory: %w", err)
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	for _, report := range reports {
		report.OutcomeEvaluation = normalizeOutcomeEvaluation(report)
		if err := encoder.Encode(EpisodeOutcomeObservation{
			RunID: runID, RunItemCount: len(reports), OutcomeReport: report,
		}); err != nil {
			return fmt.Errorf("encode adoption observation: %w", err)
		}
	}
	filePath := filepath.Join(dir, fmt.Sprintf("adoption-%d.jsonl", runID))
	if err := writeSessionFileAtomically(filePath, buffer.Bytes()); err != nil {
		return fmt.Errorf("write adoption observations: %w", err)
	}
	if err := writeLifecycleSessionJSONL(dir, runID, reports); err != nil {
		return err
	}
	return nil
}

func writeLifecycleSessionJSONL(dir string, runID int64, reports []OutcomeReport) error {
	events := make([]lifecycleSessionEvent, 0, len(reports))
	for _, report := range reports {
		if report.Lifecycle == nil {
			continue
		}
		record := report.Lifecycle
		adoption := record.Adoption
		event := lifecycleSessionEvent{
			Type: "lifecycle.adoption", Timestamp: adoption.ObservedAt.UTC().Format(time.RFC3339Nano),
			Data: lifecycleSessionEventData{
				SourceRunID: strconv.FormatInt(record.RunID, 10), SourceWorkflow: record.Workflow,
				Repository: record.Repository, OutputType: record.Type,
				DeliveryState: record.Delivery.State, DeliverySignal: record.Delivery.Signal,
				DeliveryAt:         record.Delivery.At.UTC().Format(time.RFC3339Nano),
				DeliveryObservedAt: record.Delivery.ObservedAt.UTC().Format(time.RFC3339Nano),
				DeliverySource:     record.Delivery.Source, AdoptionState: adoption.State,
				AdoptionSignal: adoption.Signal, AdoptionObservedAt: adoption.ObservedAt.UTC().Format(time.RFC3339Nano),
				AdoptionSource: adoption.Source, AdoptionRuleVersion: adoption.RuleVersion,
				AcceptanceKind: adoption.AcceptanceKind, HumanVerified: adoption.HumanVerified,
				AdoptionCoverage: adoption.Coverage, SurvivalState: record.Survival.State,
				SurvivalSignal: record.Survival.Signal, SurvivalCoverage: record.Survival.Coverage,
			},
		}
		if !adoption.At.IsZero() {
			event.Data.AdoptionAt = adoption.At.UTC().Format(time.RFC3339Nano)
		}
		if !record.Survival.ObservedAt.IsZero() {
			event.Data.SurvivalObservedAt = record.Survival.ObservedAt.UTC().Format(time.RFC3339Nano)
		}
		events = append(events, event)
	}
	if len(events) == 0 {
		return nil
	}

	fileName := fmt.Sprintf("adoption-session-%d.jsonl", runID)
	sourcePath := fmt.Sprintf("adoption-%d.jsonl", runID)
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	if err := encoder.Encode(map[string]any{
		"type": "session.format", "data": map[string]int{"version": 1},
		"provenance": lifecycleSessionEventProvenance{Component: "collector", Phase: "adoption", Path: sourcePath, Index: 0},
	}); err != nil {
		return fmt.Errorf("encode lifecycle session header: %w", err)
	}
	for index := range events {
		events[index].Provenance = lifecycleSessionEventProvenance{
			Component: "lifecycle", Phase: "adoption", Path: sourcePath, Index: index + 1,
		}
		if err := encoder.Encode(events[index]); err != nil {
			return fmt.Errorf("encode lifecycle adoption event: %w", err)
		}
	}
	if err := writeSessionFileAtomically(filepath.Join(dir, fileName), buffer.Bytes()); err != nil {
		return fmt.Errorf("write lifecycle adoption session: %w", err)
	}
	return nil
}
