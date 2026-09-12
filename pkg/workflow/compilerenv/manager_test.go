package compilerenv

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestManager_WithInjectedGetter smoke-tests all Resolve* methods through a
// single injected EnvGetter map so the DI pattern is exercised end-to-end.
func TestManager_WithInjectedGetter(t *testing.T) {
	env := map[string]string{
		DefaultMaxTurns:           "20",
		DefaultTimeoutMinutes:     "45",
		DefaultMaxTurnCacheMisses: "9",
		DefaultDetectionModel:     "gpt-5.5-mini",
		DefaultEvalsModel:         "gpt-5.5-nano",
		DefaultUTC:                "-08:00",
	}
	m := New(func(key string) string { return env[key] })

	t.Run("ResolveDefaultMaxTurns", func(t *testing.T) {
		assert.Equal(t, "20", m.ResolveDefaultMaxTurns("7"), "injected env should override fallback")
	})

	t.Run("ResolveDefaultTimeoutMinutes", func(t *testing.T) {
		assert.Equal(t, 45, m.ResolveDefaultTimeoutMinutes(20))
	})

	t.Run("ResolveDefaultMaxTurnCacheMisses", func(t *testing.T) {
		assert.Equal(t, 9, m.ResolveDefaultMaxTurnCacheMisses(5))
	})

	t.Run("ResolveDefaultDetectionModel", func(t *testing.T) {
		assert.Equal(t, "gpt-5.5-mini", m.ResolveDefaultDetectionModel(""))
	})
	t.Run("ResolveDefaultEvalsModel", func(t *testing.T) {
		assert.Equal(t, "gpt-5.5-nano", m.ResolveDefaultEvalsModel(""))
	})

	t.Run("ResolveDefaultUTC", func(t *testing.T) {
		assert.Equal(t, "-08:00", m.ResolveDefaultUTC("+00:00"))
	})
}

func TestNew_NilGetterUsesProcessEnv(t *testing.T) {
	t.Setenv(DefaultMaxTurns, "12")
	m := New(nil)
	assert.Equal(t, "12", m.ResolveDefaultMaxTurns("7"))
}

// TestManager_FallbackWhenEnvEmpty confirms that Manager methods return the
// fallback value when the injected getter returns empty strings.
func TestManager_FallbackWhenEnvEmpty(t *testing.T) {
	m := New(func(string) string { return "" })

	assert.Equal(t, "7", m.ResolveDefaultMaxTurns("7"))
	assert.Equal(t, 20, m.ResolveDefaultTimeoutMinutes(20))
	assert.Equal(t, 5, m.ResolveDefaultMaxTurnCacheMisses(5))
	assert.Equal(t, "gpt-5.5-mini", m.ResolveDefaultDetectionModel("gpt-5.5-mini"))
	assert.Equal(t, "gpt-5.5-nano", m.ResolveDefaultEvalsModel("gpt-5.5-nano"))
	assert.Equal(t, "+00:00", m.ResolveDefaultUTC("+00:00"))
}

// TestManager_PolicyModelsAllowed exercises ResolvePolicyModelsAllowed on a
// Manager with an injected getter.
func TestManager_PolicyModelsAllowed(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		env := map[string]string{PolicyModelsAllowed: "gpt-5,claude-sonnet"}
		m := New(func(key string) string { return env[key] })

		got, ok := m.ResolvePolicyModelsAllowed()
		assert.True(t, ok)
		assert.Equal(t, []string{"gpt-5", "claude-sonnet"}, got)
	})

	t.Run("deduplicates repeated entries", func(t *testing.T) {
		env := map[string]string{PolicyModelsAllowed: "gpt-5,gpt-5,claude-sonnet,gpt-5"}
		m := New(func(key string) string { return env[key] })

		got, ok := m.ResolvePolicyModelsAllowed()
		assert.True(t, ok)
		assert.Equal(t, []string{"gpt-5", "claude-sonnet"}, got)
	})
}

// TestManager_PolicyModelsBlocked exercises ResolvePolicyModelsBlocked on a
// Manager with an injected getter.
func TestManager_PolicyModelsBlocked(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		env := map[string]string{PolicyModelsBlocked: "gpt-5-pro,claude-opus"}
		m := New(func(key string) string { return env[key] })

		got, ok := m.ResolvePolicyModelsBlocked()
		assert.True(t, ok)
		assert.Equal(t, []string{"gpt-5-pro", "claude-opus"}, got)
	})

	t.Run("comma and newline separators", func(t *testing.T) {
		env := map[string]string{PolicyModelsBlocked: "gpt-5-pro,\nclaude-opus"}
		m := New(func(key string) string { return env[key] })

		got, ok := m.ResolvePolicyModelsBlocked()
		assert.True(t, ok)
		assert.Equal(t, []string{"gpt-5-pro", "claude-opus"}, got)
	})

	t.Run("deduplicates repeated entries", func(t *testing.T) {
		env := map[string]string{PolicyModelsBlocked: "gpt-5-pro,\nclaude-opus,\ngpt-5-pro"}
		m := New(func(key string) string { return env[key] })

		got, ok := m.ResolvePolicyModelsBlocked()
		assert.True(t, ok)
		assert.Equal(t, []string{"gpt-5-pro", "claude-opus"}, got)
	})
}

func TestBuildDefaultMaxTurnsExpression(t *testing.T) {
	assert.Equal(t,
		"${{ vars.GH_AW_DEFAULT_MAX_TURNS || '' }}",
		BuildDefaultMaxTurnsExpression(),
	)
}

func TestBuildDefaultOTLPEndpointExpression(t *testing.T) {
	assert.Equal(t,
		"${{ secrets.GH_AW_DEFAULT_OTLP_ENDPOINT || vars.GH_AW_DEFAULT_OTLP_ENDPOINT }}",
		BuildDefaultOTLPEndpointExpression(),
	)
}

func TestBuildTimeoutMinutesExpression(t *testing.T) {
	assert.Equal(t,
		"${{ fromJSON(vars.GH_AW_DEFAULT_TIMEOUT_MINUTES || '20') }}",
		BuildTimeoutMinutesExpression(DefaultTimeoutMinutes, 20),
	)
	assert.Equal(t,
		"${{ fromJSON(vars.GH_AW_DEFAULT_AGENT_JOB_TIMEOUT_MINUTES || '60') }}",
		BuildTimeoutMinutesExpression(DefaultAgentJobTimeoutMinutes, 60),
	)
	assert.Equal(t,
		"${{ fromJSON(vars.GH_AW_DEFAULT_DETECTION_JOB_TIMEOUT_MINUTES || '10') }}",
		BuildTimeoutMinutesExpression(DefaultDetectionJobTimeoutMinutes, 10),
	)
}

func TestBuildDefaultDetectionMaxAICreditsExpression(t *testing.T) {
	assert.Equal(t,
		"${{ vars.GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS || '400' }}",
		BuildDefaultDetectionMaxAICreditsExpression("400"),
	)
}

func TestBuildDefaultEvalsMaxAICreditsExpression(t *testing.T) {
	assert.Equal(t,
		"${{ vars.GH_AW_DEFAULT_EVALS_MAX_AI_CREDITS || '400' }}",
		BuildDefaultEvalsMaxAICreditsExpression("400"),
	)
}

func TestBuildDefaultMaxDailyAICreditsExpression(t *testing.T) {
	assert.Equal(t,
		"${{ vars.GH_AW_DEFAULT_MAX_DAILY_AI_CREDITS || '5000' }}",
		BuildDefaultMaxDailyAICreditsExpression("5000"),
	)
}

func TestBuildModelOverrideExpression(t *testing.T) {
	assert.Equal(
		t,
		"${{ vars.GH_AW_MODEL_AGENT_CODEX || vars.GH_AW_DEFAULT_MODEL_CODEX || 'gpt-5.4' }}",
		BuildModelOverrideExpression("GH_AW_MODEL_AGENT_CODEX", "GH_AW_DEFAULT_MODEL_CODEX", "gpt-5.4"),
	)
	assert.Equal(
		t,
		"${{ vars.GH_AW_MODEL_AGENT_CLAUDE || vars.GH_AW_DEFAULT_MODEL_CLAUDE || '' }}",
		BuildModelOverrideExpressionEmptyFallback("GH_AW_MODEL_AGENT_CLAUDE", "GH_AW_DEFAULT_MODEL_CLAUDE"),
	)
}

func TestResolveDefaultMaxTurns(t *testing.T) {
	t.Run("unset uses fallback", func(t *testing.T) {
		t.Setenv(DefaultMaxTurns, "")
		assert.Equal(t, "7", ResolveDefaultMaxTurns("7"))
	})

	t.Run("invalid uses fallback", func(t *testing.T) {
		t.Setenv(DefaultMaxTurns, "abc")
		assert.Equal(t, "7", ResolveDefaultMaxTurns("7"))
	})

	t.Run("zero uses fallback", func(t *testing.T) {
		t.Setenv(DefaultMaxTurns, "0")
		assert.Equal(t, "7", ResolveDefaultMaxTurns("7"))
	})

	t.Run("valid value overrides fallback", func(t *testing.T) {
		t.Setenv(DefaultMaxTurns, "15")
		assert.Equal(t, "15", ResolveDefaultMaxTurns("7"))
	})
}

func TestResolveDefaultTimeoutMinutes(t *testing.T) {
	t.Run("unset uses fallback", func(t *testing.T) {
		t.Setenv(DefaultTimeoutMinutes, "")
		assert.Equal(t, 20, ResolveDefaultTimeoutMinutes(20))
	})

	t.Run("invalid uses fallback", func(t *testing.T) {
		t.Setenv(DefaultTimeoutMinutes, "abc")
		assert.Equal(t, 20, ResolveDefaultTimeoutMinutes(20))
	})

	t.Run("zero uses fallback", func(t *testing.T) {
		t.Setenv(DefaultTimeoutMinutes, "0")
		assert.Equal(t, 20, ResolveDefaultTimeoutMinutes(20))
	})

	t.Run("valid value overrides fallback", func(t *testing.T) {
		t.Setenv(DefaultTimeoutMinutes, "45")
		assert.Equal(t, 45, ResolveDefaultTimeoutMinutes(20))
	})
}

func TestResolveDefaultMaxTurnCacheMisses(t *testing.T) {
	t.Run("unset uses fallback", func(t *testing.T) {
		t.Setenv(DefaultMaxTurnCacheMisses, "")
		assert.Equal(t, 5, ResolveDefaultMaxTurnCacheMisses(5))
	})

	t.Run("invalid uses fallback", func(t *testing.T) {
		t.Setenv(DefaultMaxTurnCacheMisses, "abc")
		assert.Equal(t, 5, ResolveDefaultMaxTurnCacheMisses(5))
	})

	t.Run("zero uses fallback", func(t *testing.T) {
		t.Setenv(DefaultMaxTurnCacheMisses, "0")
		assert.Equal(t, 5, ResolveDefaultMaxTurnCacheMisses(5))
	})

	t.Run("valid value overrides fallback", func(t *testing.T) {
		t.Setenv(DefaultMaxTurnCacheMisses, "9")
		assert.Equal(t, 9, ResolveDefaultMaxTurnCacheMisses(5))
	})
}

func TestParsePositiveIntEnvVar_OverflowRegression(t *testing.T) {
	// 2^31 = 2147483648, one above MaxInt32 (2^31-1): fits in int64 but overflows int32.
	// On 32-bit platforms (strconv.IntSize == 32) strconv.Atoi rejects this
	// value, so the function must fall back to the default — the original
	// CWE-190 silent-overflow scenario.  On 64-bit platforms it parses
	// successfully, proving no over-restriction.
	const bigVal = "2147483648"
	t.Setenv(DefaultTimeoutMinutes, bigVal)
	if strconv.IntSize == 32 {
		assert.Equal(t, 20, ResolveDefaultTimeoutMinutes(20), "overflow value must fall back on 32-bit")
	} else {
		assert.Equal(t, 2147483648, ResolveDefaultTimeoutMinutes(20), "value fits on 64-bit, must parse")
	}
}

func TestResolveDefaultDetectionModel(t *testing.T) {
	t.Run("unset uses fallback", func(t *testing.T) {
		t.Setenv(DefaultDetectionModel, "")
		assert.Empty(t, ResolveDefaultDetectionModel(""))
	})

	t.Run("unset keeps non-empty fallback", func(t *testing.T) {
		t.Setenv(DefaultDetectionModel, "")
		assert.Equal(t, "gpt-5.5-mini", ResolveDefaultDetectionModel("gpt-5.5-mini"))
	})

	t.Run("set value overrides fallback", func(t *testing.T) {
		t.Setenv(DefaultDetectionModel, "gpt-5.5-mini")
		assert.Equal(t, "gpt-5.5-mini", ResolveDefaultDetectionModel(""))
	})
}

func TestResolveDefaultEvalsModel(t *testing.T) {
	t.Run("unset uses fallback", func(t *testing.T) {
		t.Setenv(DefaultEvalsModel, "")
		assert.Empty(t, ResolveDefaultEvalsModel(""))
	})

	t.Run("unset keeps non-empty fallback", func(t *testing.T) {
		t.Setenv(DefaultEvalsModel, "")
		assert.Equal(t, "gpt-5.5-nano", ResolveDefaultEvalsModel("gpt-5.5-nano"))
	})

	t.Run("set value overrides fallback", func(t *testing.T) {
		t.Setenv(DefaultEvalsModel, "gpt-5.5-nano")
		assert.Equal(t, "gpt-5.5-nano", ResolveDefaultEvalsModel(""))
	})
}

func TestResolveDefaultUTC(t *testing.T) {
	t.Run("unset uses fallback", func(t *testing.T) {
		t.Setenv(DefaultUTC, "")
		assert.Equal(t, "+00:00", ResolveDefaultUTC("+00:00"))
	})

	t.Run("set value overrides fallback", func(t *testing.T) {
		t.Setenv(DefaultUTC, "-08:00")
		assert.Equal(t, "-08:00", ResolveDefaultUTC("+00:00"))
	})
}

func TestResolvePolicyModelsAllowed(t *testing.T) {
	t.Run("unset returns no override", func(t *testing.T) {
		t.Setenv(PolicyModelsAllowed, "")
		got, ok := ResolvePolicyModelsAllowed()
		assert.False(t, ok)
		assert.Nil(t, got)
	})

	t.Run("comma-separated list is parsed", func(t *testing.T) {
		t.Setenv(PolicyModelsAllowed, "gpt-5, claude-sonnet, gpt-5")
		got, ok := ResolvePolicyModelsAllowed()
		assert.True(t, ok)
		assert.Equal(t, []string{"gpt-5", "claude-sonnet"}, got)
	})

	t.Run("space-separated list is rejected", func(t *testing.T) {
		t.Setenv(PolicyModelsAllowed, "gpt-5 claude-sonnet")
		got, ok := ResolvePolicyModelsAllowed()
		assert.False(t, ok)
		assert.Nil(t, got)
	})
}

func TestResolvePolicyModelsBlocked(t *testing.T) {
	t.Run("unset returns no override", func(t *testing.T) {
		t.Setenv(PolicyModelsBlocked, "")
		got, ok := ResolvePolicyModelsBlocked()
		assert.False(t, ok)
		assert.Nil(t, got)
	})

	t.Run("comma/newline-separated list is parsed", func(t *testing.T) {
		t.Setenv(PolicyModelsBlocked, "gpt-5-pro,\nclaude-opus")
		got, ok := ResolvePolicyModelsBlocked()
		assert.True(t, ok)
		assert.Equal(t, []string{"gpt-5-pro", "claude-opus"}, got)
	})
}
