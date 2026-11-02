//go:build !integration

package workflow

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// parseEvalsFromFrontmatter
// ---------------------------------------------------------------------------

func TestParseEvalsFromFrontmatter_Nil_WhenAbsent(t *testing.T) {
	c := NewCompiler()
	cfg, err := c.parseEvalsFromFrontmatter(map[string]any{})
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestParseEvalsFromFrontmatter_Nil_WhenExplicitNull(t *testing.T) {
	c := NewCompiler()
	cfg, err := c.parseEvalsFromFrontmatter(map[string]any{"evals": nil})
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestParseEvalsFromFrontmatter_ShorthandForm(t *testing.T) {
	c := NewCompiler()
	frontmatter := map[string]any{
		"evals": []any{
			map[string]any{"id": "builds", "question": "Does the code compile?"},
			map[string]any{"id": "tests", "question": "Do all tests pass?"},
		},
	}
	cfg, err := c.parseEvalsFromFrontmatter(frontmatter)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Len(t, cfg.Questions, 2)
	assert.Equal(t, "builds", cfg.Questions[0].ID)
	assert.Equal(t, "Does the code compile?", cfg.Questions[0].Question)
	assert.Equal(t, "tests", cfg.Questions[1].ID)
	assert.Empty(t, cfg.Model)
	assert.Empty(t, cfg.RunsOn)
}

func TestParseEvalsFromFrontmatter_ExtendedForm(t *testing.T) {
	c := NewCompiler()
	frontmatter := map[string]any{
		"evals": map[string]any{
			"questions": []any{
				map[string]any{"id": "focused", "question": "Is the change focused?"},
			},
			"runs-on": "ubuntu-latest",
		},
	}
	cfg, err := c.parseEvalsFromFrontmatter(frontmatter)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Len(t, cfg.Questions, 1)
	assert.Equal(t, "focused", cfg.Questions[0].ID)
	assert.NotEmpty(t, cfg.RunsOn)
}

func TestParseEvalsFromFrontmatter_ExtendedForm_WithModel(t *testing.T) {
	c := NewCompiler()
	frontmatter := map[string]any{
		"evals": map[string]any{
			"questions": []any{
				map[string]any{"id": "q1", "question": "Is it correct?"},
			},
			"model": "openai/gpt-4o-mini",
		},
	}
	cfg, err := c.parseEvalsFromFrontmatter(frontmatter)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "openai/gpt-4o-mini", cfg.Model)
}

func TestParseEvalsFromFrontmatter_PerQuestionModel(t *testing.T) {
	c := NewCompiler()
	frontmatter := map[string]any{
		"evals": []any{
			map[string]any{"id": "q1", "question": "Is it correct?", "model": "openai/gpt-4o"},
			map[string]any{"id": "q2", "question": "Is it safe?"},
		},
	}
	_, err := c.parseEvalsFromFrontmatter(frontmatter)
	require.ErrorContains(t, err, "all questions share one Decisions request")
	require.ErrorContains(t, err, "Set evals.model")
}

func TestValidateEvals_DecisionsQuestionLimit(t *testing.T) {
	cfg := &EvalsConfig{}
	for i := range 400 {
		cfg.Questions = append(cfg.Questions, EvalDefinition{ID: strconv.Itoa(i), Question: "Did it succeed?"})
	}
	require.NoError(t, validateEvals(cfg))
	cfg.Questions = append(cfg.Questions, EvalDefinition{ID: "overflow", Question: "Did it succeed?"})
	require.ErrorContains(t, validateEvals(cfg), "at most 400")
}

func TestParseEvalsFromFrontmatter_QuestionLevelNonStringModel(t *testing.T) {
	c := NewCompiler()
	_, err := c.parseEvalsFromFrontmatter(map[string]any{
		"evals": []any{
			map[string]any{"id": "q1", "question": "Is it correct?", "model": 99},
		},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "model")
	require.ErrorContains(t, err, "all questions share one Decisions request")
}

func TestParseEvalsFromFrontmatter_InvalidType(t *testing.T) {
	c := NewCompiler()
	_, err := c.parseEvalsFromFrontmatter(map[string]any{"evals": "invalid"})
	require.Error(t, err)
	require.ErrorContains(t, err, "evals")
}

func TestParseEvalsFromFrontmatter_WrongTypeQuestions(t *testing.T) {
	c := NewCompiler()
	_, err := c.parseEvalsFromFrontmatter(map[string]any{
		"evals": map[string]any{
			"questions": "not-a-list",
		},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "evals.questions")
	require.ErrorContains(t, err, "list")
}

func TestParseEvalsFromFrontmatter_NonStringModel(t *testing.T) {
	c := NewCompiler()
	_, err := c.parseEvalsFromFrontmatter(map[string]any{
		"evals": map[string]any{
			"questions": []any{
				map[string]any{"id": "q1", "question": "Is it correct?"},
			},
			"model": 42,
		},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "evals.model")
	require.ErrorContains(t, err, "string")
}

// ---------------------------------------------------------------------------
// validateEvals
// ---------------------------------------------------------------------------

func TestValidateEvals_RejectsEmptyQuestions(t *testing.T) {
	err := validateEvals(&EvalsConfig{Questions: []EvalDefinition{}})
	require.Error(t, err)
	require.ErrorContains(t, err, "at least one question")
}

func TestValidateEvals_RejectsDuplicateID(t *testing.T) {
	cfg := &EvalsConfig{
		Questions: []EvalDefinition{
			{ID: "builds", Question: "Does it compile?"},
			{ID: "builds", Question: "Different question but same ID"},
		},
	}
	err := validateEvals(cfg)
	require.Error(t, err)
	require.ErrorContains(t, err, "duplicate id")
	require.ErrorContains(t, err, "builds")
}

func TestValidateEvals_RejectsEmptyQuestion(t *testing.T) {
	// parseEvalDefinition trims and rejects empty, so only need to cover the validator branch
	cfg := &EvalsConfig{
		Questions: []EvalDefinition{
			{ID: "q1", Question: "  "},
		},
	}
	err := validateEvals(cfg)
	require.Error(t, err)
	require.ErrorContains(t, err, "non-empty")
}

func TestValidateEvals_AcceptsValidQuestions(t *testing.T) {
	cfg := &EvalsConfig{
		Questions: []EvalDefinition{
			{ID: "q1", Question: "First question?"},
			{ID: "q2", Question: "Second question?"},
		},
	}
	require.NoError(t, validateEvals(cfg))
}

func TestValidateEvals_NilConfig(t *testing.T) {
	require.NoError(t, validateEvals(nil))
}

// ---------------------------------------------------------------------------
// parseEvalDefinition
// ---------------------------------------------------------------------------

func TestParseEvalDefinition_MissingID(t *testing.T) {
	_, err := parseEvalDefinition(map[string]any{"question": "Something?"}, 0)
	require.Error(t, err)
	require.ErrorContains(t, err, "id")
}

func TestParseEvalDefinition_MissingQuestion(t *testing.T) {
	_, err := parseEvalDefinition(map[string]any{"id": "q1"}, 0)
	require.Error(t, err)
	require.ErrorContains(t, err, "question")
}

func TestParseEvalDefinition_EmptyID(t *testing.T) {
	_, err := parseEvalDefinition(map[string]any{"id": "  ", "question": "Something?"}, 0)
	require.Error(t, err)
	require.ErrorContains(t, err, "id")
}

func TestParseEvalDefinition_TrimsWhitespace(t *testing.T) {
	def, err := parseEvalDefinition(map[string]any{"id": "  q1  ", "question": "  Is it OK?  "}, 0)
	require.NoError(t, err)
	assert.Equal(t, "q1", def.ID)
	assert.Equal(t, "Is it OK?", def.Question)
}

// ---------------------------------------------------------------------------
// HasEvals
// ---------------------------------------------------------------------------

func TestHasEvals_NilConfig(t *testing.T) {
	var cfg *EvalsConfig
	assert.False(t, cfg.HasEvals())
}

func TestHasEvals_EmptyQuestions(t *testing.T) {
	cfg := &EvalsConfig{Questions: []EvalDefinition{}}
	assert.False(t, cfg.HasEvals())
}

func TestHasEvals_NonEmpty(t *testing.T) {
	cfg := &EvalsConfig{Questions: []EvalDefinition{{ID: "q1", Question: "ok?"}}}
	assert.True(t, cfg.HasEvals())
}
