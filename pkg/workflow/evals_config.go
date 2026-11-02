// Package workflow - BinEval evaluation configuration types and parser.
package workflow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var evalsConfigLog = logger.New("workflow:evals_config")

// EvalDefinition represents a single binary evaluation question in a BinEval workflow.
// Questions share one Decisions request and receive YES, NO, or UNKNOWN choices.
type EvalDefinition struct {
	ID       string
	Question string
}

// EvalsConfig holds the configuration for BinEval-style evaluations declared in workflow
// frontmatter. Evaluations run after the agent, in parallel with safe-outputs.
type EvalsConfig struct {
	// Questions is the ordered list of binary evaluation questions.
	Questions []EvalDefinition
	// Model is the Decisions-compatible model for the entire batch.
	// When empty, copilot/gpt-6-luna-decisions is used, subject to GH_AW_DEFAULT_EVALS_MODEL.
	Model string
	// RunsOn allows overriding the runner for the evals job.
	RunsOn string
}

// HasEvals returns true when the config contains at least one evaluation question.
func (ec *EvalsConfig) HasEvals() bool {
	return ec != nil && len(ec.Questions) > 0
}

// parseEvalsFromFrontmatter extracts and validates the evals configuration from the
// raw frontmatter map. Returns nil when the evals field is absent.
//
// Supported forms:
//
//	# Shorthand — plain list
//	evals:
//	  - id: builds
//	    question: Does the generated code compile?
//
//	# Extended — object with questions list and optional model/runs-on
//	evals:
//	  questions:
//	    - id: builds
//	      question: Does the generated code compile?
//	  model: copilot/gpt-6-luna-decisions
func (c *Compiler) parseEvalsFromFrontmatter(frontmatter map[string]any) (*EvalsConfig, error) {
	raw, exists := frontmatter["evals"]
	if !exists || raw == nil {
		return nil, nil
	}

	cfg := &EvalsConfig{}

	switch v := raw.(type) {
	case []any:
		// Shorthand form: plain list of questions
		questions, err := parseEvalDefinitions(v)
		if err != nil {
			return nil, fmt.Errorf("evals: %w", err)
		}
		cfg.Questions = questions

	case map[string]any:
		// Extended form: object with questions and optional model/runs-on
		if questionsRaw, ok := v["questions"]; ok {
			questionsList, ok := questionsRaw.([]any)
			if !ok {
				return nil, fmt.Errorf("evals.questions must be a list of question objects, got %T. Example:\nevals:\n  questions:\n    - id: readme\n      question: Does the README explain setup?", questionsRaw)
			}
			questions, err := parseEvalDefinitions(questionsList)
			if err != nil {
				return nil, fmt.Errorf("evals.questions: %w", err)
			}
			cfg.Questions = questions
		}

		// Parse optional top-level model (default for all questions)
		if modelRaw, ok := v["model"]; ok {
			modelStr, ok := modelRaw.(string)
			if !ok {
				return nil, fmt.Errorf("evals.model must be a string, got %T. Example:\nevals:\n  model: copilot/gpt-6-luna-decisions", modelRaw)
			}
			cfg.Model = strings.TrimSpace(modelStr)
		}

		// Parse optional runs-on override
		if runsOnRaw, ok := v["runs-on"]; ok {
			cfg.RunsOn = renderRunsOnSnippet(runsOnRaw)
		}

	default:
		return nil, errors.New("evals must be a list of questions or an object with a questions list. Example:\nevals:\n  - id: readme\n    question: Does the README explain setup?")
	}

	if err := validateEvals(cfg); err != nil {
		return nil, err
	}

	evalsConfigLog.Printf("Parsed %d eval definitions (Decisions model: %q)", len(cfg.Questions), cfg.Model)
	return cfg, nil
}

// parseEvalDefinitions converts a []any YAML list into []EvalDefinition.
func parseEvalDefinitions(items []any) ([]EvalDefinition, error) {
	defs := make([]EvalDefinition, 0, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("evals item %d must be an object with 'id' and 'question' fields, got a different type. Example:\nevals:\n  - id: readme\n    question: Does the README explain setup?", i)
		}
		def, err := parseEvalDefinition(m, i)
		if err != nil {
			return nil, err
		}
		defs = append(defs, def)
	}
	return defs, nil
}

// parseEvalDefinition converts a single map entry into an EvalDefinition.
func parseEvalDefinition(m map[string]any, idx int) (EvalDefinition, error) {
	idRaw, hasID := m["id"]
	questionRaw, hasQuestion := m["question"]

	if !hasID {
		return EvalDefinition{}, fmt.Errorf("evals item %d is missing the required 'id' field. Example:\nevals:\n  - id: readme\n    question: Does the README explain setup?", idx)
	}
	if !hasQuestion {
		return EvalDefinition{}, fmt.Errorf("evals item %d is missing the required 'question' field. Example:\nevals:\n  - id: readme\n    question: Does the README explain setup?", idx)
	}

	id, ok := idRaw.(string)
	if !ok || strings.TrimSpace(id) == "" {
		return EvalDefinition{}, fmt.Errorf("evals item %d has an 'id' that is not a non-empty string. Example:\nevals:\n  - id: readme\n    question: Does the README explain setup?", idx)
	}

	question, ok := questionRaw.(string)
	if !ok || strings.TrimSpace(question) == "" {
		return EvalDefinition{}, fmt.Errorf("evals item %d has a 'question' that is not a non-empty string. Example:\nevals:\n  - id: readme\n    question: Does the README explain setup?", idx)
	}

	def := EvalDefinition{
		ID:       strings.TrimSpace(id),
		Question: strings.TrimSpace(question),
	}

	if _, ok := m["model"]; ok {
		return EvalDefinition{}, fmt.Errorf("evals question %q cannot override the model: all questions share one Decisions request. Set evals.model for the entire batch", id)
	}

	return def, nil
}

// validateEvals checks for duplicate IDs and non-empty questions after parsing.
func validateEvals(cfg *EvalsConfig) error {
	if cfg == nil {
		return nil
	}
	if len(cfg.Questions) == 0 {
		return errors.New("evals requires at least one question when configured. Example:\nevals:\n  - id: readme\n    question: Does the README explain setup?")
	}
	if len(cfg.Questions) > 400 {
		return errors.New("evals supports at most 400 questions in one Decisions request. Reduce the question list to 400 or fewer")
	}

	seen := make(map[string]struct{}, len(cfg.Questions))
	for i, q := range cfg.Questions {
		if _, dup := seen[q.ID]; dup {
			return fmt.Errorf("evals has a duplicate id %q at index %d. Use a unique 'id' for each question. Example:\nevals:\n  - id: readme\n    question: Does the README explain setup?\n  - id: security\n    question: Are secrets handled safely?", q.ID, i)
		}
		seen[q.ID] = struct{}{}

		if strings.TrimSpace(q.Question) == "" {
			return fmt.Errorf("evals question for id %q must be non-empty. Example:\nevals:\n  - id: %s\n    question: Does the README explain setup?", q.ID, q.ID)
		}
	}
	return nil
}

// ParseEvalsFromFrontmatter extracts and validates the evals configuration from the
// raw frontmatter map. Returns nil when the evals field is absent or invalid.
// This is a public standalone convenience wrapper around the compiler method.
func ParseEvalsFromFrontmatter(frontmatter map[string]any) (*EvalsConfig, error) {
	var c Compiler
	return c.parseEvalsFromFrontmatter(frontmatter)
}
