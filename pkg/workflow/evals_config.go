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
// Each question is evaluated independently and answered with YES or NO.
type EvalDefinition struct {
	ID       string
	Question string
	// Model is an optional per-question model override. When set, it takes precedence over
	// EvalsConfig.Model. Use a model alias such as "small" or a full model ID.
	Model string
}

// EvalsConfig holds the configuration for BinEval-style evaluations declared in workflow
// frontmatter. Evaluations run after safe-outputs and before the conclusion job.
type EvalsConfig struct {
	// Backend selects auto (default), llm, or decisions evaluation.
	Backend string
	// Questions is the ordered list of binary evaluation questions.
	Questions []EvalDefinition
	// Model is the default LLM model to use for evaluations. Use a model alias such as
	// "small" or a full model ID. Per-question Model fields override this value.
	// When empty, the compiler default ("evals") is used.
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
//	  model: small
func (c *Compiler) parseEvalsFromFrontmatter(frontmatter map[string]any) (*EvalsConfig, error) {
	raw, exists := frontmatter["evals"]
	if !exists || raw == nil {
		return nil, nil
	}

	cfg := &EvalsConfig{Backend: "auto"}

	switch v := raw.(type) {
	case []any:
		// Shorthand form: plain list of questions
		questions, err := parseEvalDefinitions(v)
		if err != nil {
			return nil, fmt.Errorf("evals: %w", err)
		}
		cfg.Questions = questions

	case map[string]any:
		if err := parseEvalsOptions(v, cfg); err != nil {
			return nil, err
		}
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

	default:
		return nil, errors.New("evals must be a list of questions or an object with a questions list. Example:\nevals:\n  - id: readme\n    question: Does the README explain setup?")
	}

	if err := validateEvals(cfg); err != nil {
		return nil, err
	}

	perQuestionOverrides := 0
	for _, q := range cfg.Questions {
		if q.Model != "" {
			perQuestionOverrides++
		}
	}
	evalsConfigLog.Printf("Parsed %d eval definitions (model: %q, per-question overrides: %d)", len(cfg.Questions), cfg.Model, perQuestionOverrides)
	return cfg, nil
}

func parseEvalsOptions(options map[string]any, cfg *EvalsConfig) error {
	if backendRaw, ok := options["backend"]; ok {
		backend, ok := backendRaw.(string)
		if !ok {
			return fmt.Errorf("evals.backend must be a string, got %T. Use auto, llm, or decisions", backendRaw)
		}
		cfg.Backend = strings.TrimSpace(backend)
		if cfg.Backend == "" {
			return errors.New("evals.backend must be auto, llm, or decisions")
		}
	}
	if modelRaw, ok := options["model"]; ok {
		model, ok := modelRaw.(string)
		if !ok {
			return fmt.Errorf("evals.model must be a string, got %T. Example:\nevals:\n  model: small", modelRaw)
		}
		cfg.Model = strings.TrimSpace(model)
	}
	if runsOnRaw, ok := options["runs-on"]; ok {
		cfg.RunsOn = renderRunsOnSnippet(runsOnRaw)
	}
	return nil
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

	// Optional per-question model override.
	if modelRaw, ok := m["model"]; ok {
		modelStr, ok := modelRaw.(string)
		if !ok {
			return EvalDefinition{}, fmt.Errorf("evals item %d has a 'model' that must be a string, got %T. Example:\nevals:\n  - id: readme\n    question: Does the README explain setup?\n    model: small", idx, modelRaw)
		}
		def.Model = strings.TrimSpace(modelStr)
	}

	return def, nil
}

// validateEvals checks for duplicate IDs and non-empty questions after parsing.
func validateEvals(cfg *EvalsConfig) error {
	if cfg == nil {
		return nil
	}
	switch cfg.Backend {
	case "", "auto", "llm", "decisions":
	default:
		return errors.New("evals.backend must be auto, llm, or decisions")
	}
	if cfg.Backend == "decisions" {
		if len(cfg.Questions) > 400 {
			return errors.New("evals.backend decisions supports at most 400 questions. Reduce the questions or use backend: auto or llm")
		}
		if cfg.Model != "" && cfg.Model != "gpt-6-luna" {
			return errors.New("evals.backend decisions requires model gpt-6-luna or no model. Use backend: auto or llm for other models")
		}
	}
	if len(cfg.Questions) == 0 {
		return errors.New("evals requires at least one question when configured. Example:\nevals:\n  - id: readme\n    question: Does the README explain setup?")
	}

	seen := make(map[string]struct{}, len(cfg.Questions))
	for i, q := range cfg.Questions {
		if cfg.Backend == "decisions" && q.Model != "" && q.Model != "gpt-6-luna" {
			return fmt.Errorf("evals question %q uses model %q, which is incompatible with backend decisions. Omit the model, use gpt-6-luna, or select backend: auto or llm", q.ID, q.Model)
		}
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
