package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const determinismTestIterations = 10
const authPlaceholder = "******"

func TestFormal_EndpointFormNormalization(t *testing.T) {
	t.Run("string object and array normalize to ordered entries", func(t *testing.T) {
		stringForm := map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{
					"endpoint": "https://string.example.com:4317",
				},
			},
		}
		assert.Equal(t,
			[]otlpEndpointEntry{{URL: "https://string.example.com:4317"}},
			collectAllOTLPEndpoints(stringForm),
		)

		objectForm := map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{
					"endpoint": map[string]any{"url": "https://object.example.com:4317"},
				},
			},
		}
		assert.Equal(t,
			[]otlpEndpointEntry{{URL: "https://object.example.com:4317"}},
			collectAllOTLPEndpoints(objectForm),
		)

		arrayForm := map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{
					"endpoint": []any{
						map[string]any{"url": "https://first.example.com:4317"},
						map[string]any{"url": "https://second.example.com:4317"},
					},
				},
			},
		}
		assert.Equal(t,
			[]otlpEndpointEntry{
				{URL: "https://first.example.com:4317"},
				{URL: "https://second.example.com:4317"},
			},
			collectAllOTLPEndpoints(arrayForm),
		)
	})

	t.Run("empty and absent normalize to empty", func(t *testing.T) {
		assert.Empty(t, collectAllOTLPEndpoints(nil))
		assert.Empty(t, collectAllOTLPEndpoints(map[string]any{}))
		assert.Empty(t, collectAllOTLPEndpoints(map[string]any{"observability": map[string]any{}}))
	})
}

func TestFormal_HeaderMapDeterminism(t *testing.T) {
	headers := map[string]any{"z": "3", "a": "1", "m": "2"}
	want := "a=1,m=2,z=3"

	for range determinismTestIterations {
		assert.Equal(t, want, normalizeOTLPHeadersForEndpoint(headers, "https://example.com:4317"))
	}
}

func TestFormal_SentryAuthHeaderRewrite(t *testing.T) {
	normalizedSentryHeaders := normalizeOTLPHeadersForEndpoint("Authorization="+authPlaceholder, "https://o0.ingest.sentry.io/api/0/envelope/")
	normalizedNonSentryHeaders := normalizeOTLPHeadersForEndpoint("Authorization="+authPlaceholder, "https://otlp.example.com:4317")
	normalizedSentryMixedHeaders := normalizeOTLPHeadersForEndpoint(
		map[string]any{"Authorization": authPlaceholder, "X-Tenant": "acme"},
		"https://o0.ingest.sentry.io/api/0/envelope/",
	)

	assert.Equal(t, "x-sentry-auth="+authPlaceholder, normalizedSentryHeaders)
	assert.Equal(t, "Authorization="+authPlaceholder, normalizedNonSentryHeaders)
	assert.Equal(t, "x-sentry-auth="+authPlaceholder+",X-Tenant=acme", normalizedSentryMixedHeaders)
}

func TestFormal_IfMissingPolicyValidation(t *testing.T) {
	assert.Equal(t, "error", normalizeOTLPIfMissingMode("error"))
	assert.Equal(t, "warn", normalizeOTLPIfMissingMode("WARN"))
	assert.Equal(t, "ignore", normalizeOTLPIfMissingMode(" Ignore "))
}

func TestFormal_ServiceNameFormation(t *testing.T) {
	assert.Equal(t, "gh-aw", otelServiceName(nil))
	assert.Equal(t, "gh-aw.repo-triage-weekly", otelServiceName(&WorkflowData{WorkflowID: "repo-triage-weekly", Name: "Sample Name"}))
	assert.Equal(t, "gh-aw.repo-triage-weekly", otelServiceName(&WorkflowData{WorkflowID: "Repo Triage/Weekly", Name: "Sample Name"}))
	assert.Equal(t, "gh-aw.workflow-name", otelServiceName(&WorkflowData{Name: "Workflow Name"}))
}

func TestFormal_StaticDomainExtraction(t *testing.T) {
	assert.Equal(t, "traces.example.com", extractOTLPEndpointDomain("https://traces.example.com:4317"))
	assert.Empty(t, extractOTLPEndpointDomain(""))
	assert.Empty(t, extractOTLPEndpointDomain("${{ secrets.OTLP_ENDPOINT }}"))
}

func TestFormal_ExpressionProducesNoAllowlistEntry(t *testing.T) {
	assert.Empty(t, extractOTLPEndpointDomain("${{ secrets.OTLP_ENDPOINT }}"))
}

func TestFormal_TopLevelHeadersApplyToStringFormOnly(t *testing.T) {
	stringForm := map[string]any{
		"observability": map[string]any{
			"otlp": map[string]any{
				"endpoint": "https://string.example.com:4317",
				"headers":  "Authorization=" + authPlaceholder,
			},
		},
	}
	entries := collectAllOTLPEndpoints(stringForm)
	require.Len(t, entries, 1)
	assert.Equal(t, "Authorization="+authPlaceholder, entries[0].Headers)

	objectForm := map[string]any{
		"observability": map[string]any{
			"otlp": map[string]any{
				"endpoint": map[string]any{
					"url":     "https://object.example.com:4317",
					"headers": "X-Per-Entry=v",
				},
				"headers": "Authorization=" + authPlaceholder,
			},
		},
	}
	objectEntries := collectAllOTLPEndpoints(objectForm)
	require.Len(t, objectEntries, 1)
	assert.Equal(t, "X-Per-Entry=v", objectEntries[0].Headers)
}

func TestFormal_FanOutPreservesDeclarationOrder(t *testing.T) {
	frontmatter := map[string]any{
		"observability": map[string]any{
			"otlp": map[string]any{
				"endpoint": []any{
					map[string]any{"url": "https://one.example.com:4317"},
					map[string]any{"url": "https://two.example.com:4317"},
					map[string]any{"url": "https://three.example.com:4317"},
				},
			},
		},
	}

	entries := collectAllOTLPEndpoints(frontmatter)
	require.Len(t, entries, 3)
	assert.Equal(t, "https://one.example.com:4317", entries[0].URL)
	assert.Equal(t, "https://two.example.com:4317", entries[1].URL)
	assert.Equal(t, "https://three.example.com:4317", entries[2].URL)
}

func TestFormal_MirrorPathConstant(t *testing.T) {
	assert.Equal(t, "/tmp/gh-aw/otel.jsonl", constants.TmpGhAwDirSlash+constants.OtelJsonlFilename.String())
	assert.Equal(t, "/tmp/gh-aw/otel-metrics.jsonl", constants.TmpGhAwDirSlash+constants.OtelMetricsJsonlFilename.String())
}

func TestFormal_EmptyURLEntriesDiscarded(t *testing.T) {
	frontmatter := map[string]any{
		"observability": map[string]any{
			"otlp": map[string]any{
				"endpoint": []any{
					map[string]any{"url": ""},
					map[string]any{"url": "https://valid.example.com:4317"},
				},
			},
		},
	}

	assert.Equal(t, []otlpEndpointEntry{{URL: "https://valid.example.com:4317"}}, collectAllOTLPEndpoints(frontmatter))
}

func TestFormal_StringHeaderFormPreservedForNonSentry(t *testing.T) {
	assert.Equal(t,
		"Authorization="+authPlaceholder,
		normalizeOTLPHeadersForEndpoint("Authorization="+authPlaceholder, "https://otlp.example.com:4317"),
	)
}

func TestFormal_NilAndEmptyHeadersYieldEmptyString(t *testing.T) {
	assert.Empty(t, normalizeOTLPHeadersForEndpoint(nil, "https://example.com:4317"))
	assert.Empty(t, normalizeOTLPHeadersForEndpoint("", "https://example.com:4317"))
	assert.Empty(t, normalizeOTLPHeadersForEndpoint(map[string]any{}, "https://example.com:4317"))
}

func TestFormal_InvalidIfMissingFallsBackToDefault(t *testing.T) {
	for _, mode := range []string{"fail", "silent", "skip", "abort"} {
		assert.Empty(t, normalizeOTLPIfMissingMode(mode))
	}

	workflowData := &WorkflowData{
		RawFrontmatter: map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{
					"endpoint":   "https://traces.example.com:4317",
					"if-missing": "fail",
				},
			},
		},
		ParsedFrontmatter: &FrontmatterConfig{
			Observability: &ObservabilityConfig{
				OTLP: &OTLPConfig{
					Endpoint:  "https://traces.example.com:4317",
					IfMissing: "fail",
				},
			},
		},
	}
	(&Compiler{}).injectOTLPConfig(workflowData)
	assert.NotContains(t, workflowData.Env, "GH_AW_OTLP_IF_MISSING")

	validWorkflowData := &WorkflowData{
		RawFrontmatter: map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{
					"endpoint":   "https://traces.example.com:4317",
					"if-missing": "warn",
				},
			},
		},
		ParsedFrontmatter: &FrontmatterConfig{
			Observability: &ObservabilityConfig{
				OTLP: &OTLPConfig{
					Endpoint:  "https://traces.example.com:4317",
					IfMissing: "warn",
				},
			},
		},
	}
	(&Compiler{}).injectOTLPConfig(validWorkflowData)
	assert.Contains(t, validWorkflowData.Env, "GH_AW_OTLP_IF_MISSING")
	assert.Contains(t, validWorkflowData.Env, "warn")
}

func TestFormal_AbsentObservabilityProducesNoEndpoints(t *testing.T) {
	assert.Empty(t, collectAllOTLPEndpoints(nil))
	assert.Empty(t, collectAllOTLPEndpoints(map[string]any{}))
	assert.Empty(t, collectAllOTLPEndpoints(map[string]any{"observability": nil}))
}

// P16 — SecretRefResourceAttributeRejected
// validateOTLPResourceAttributes must reject any resource-attribute value that
// references secrets.* or vars.* and must accept literal string values and nil
// workflow data without error.
func TestFormal_SecretRefResourceAttributeRejected(t *testing.T) {
	// Nil workflow data is safe — no attributes to reject.
	assert.NoError(t, validateOTLPResourceAttributes(nil))

	// Literal string values are accepted.
	literalData := &WorkflowData{
		RawFrontmatter: map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{
					"resource-attributes": map[string]any{
						"deployment.environment": "production",
						"team.name":              "platform",
					},
				},
			},
		},
	}
	assert.NoError(t, validateOTLPResourceAttributes(literalData))

	// References to secrets.* must be rejected.
	secretsData := &WorkflowData{
		RawFrontmatter: map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{
					"resource-attributes": map[string]any{
						"auth.token": "${{ secrets.MY_TOKEN }}",
					},
				},
			},
		},
	}
	require.Error(t, validateOTLPResourceAttributes(secretsData))

	// References to vars.* must also be rejected.
	varsData := &WorkflowData{
		RawFrontmatter: map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{
					"resource-attributes": map[string]any{
						"config.value": "${{ vars.SOME_VAR }}",
					},
				},
			},
		},
	}
	require.Error(t, validateOTLPResourceAttributes(varsData))
}

// P17 — CustomAttributesResourceAttributesIndependent
// collectOTLPCustomAttributes and collectOTLPResourceAttributes must read from
// their respective `otlp.attributes` and `otlp.resource-attributes` keys
// independently; writing to one must not affect the other.
func TestFormal_CustomAttributesResourceAttributesIndependent(t *testing.T) {
	frontmatter := map[string]any{
		"observability": map[string]any{
			"otlp": map[string]any{
				"attributes": map[string]any{
					"span.key": "span-value",
				},
				"resource-attributes": map[string]any{
					"resource.key": "resource-value",
				},
			},
		},
	}

	customAttrs := collectOTLPCustomAttributes(frontmatter)
	resourceAttrs := collectOTLPResourceAttributes(frontmatter)

	// Each collector returns only its own entries.
	require.Len(t, customAttrs, 1)
	assert.Equal(t, "span-value", customAttrs["span.key"])
	assert.NotContains(t, customAttrs, "resource.key")

	require.Len(t, resourceAttrs, 1)
	assert.Equal(t, "resource-value", resourceAttrs["resource.key"])
	assert.NotContains(t, resourceAttrs, "span.key")

	// Absent sibling does not bleed into the other field.
	onlyCustom := map[string]any{
		"observability": map[string]any{
			"otlp": map[string]any{
				"attributes": map[string]any{"k": "v"},
			},
		},
	}
	assert.Nil(t, collectOTLPResourceAttributes(onlyCustom))

	onlyResource := map[string]any{
		"observability": map[string]any{
			"otlp": map[string]any{
				"resource-attributes": map[string]any{"k": "v"},
			},
		},
	}
	assert.Nil(t, collectOTLPCustomAttributes(onlyResource))
}

// P18 — MergePrecedenceBaseWinsOverOverride
// mergeOTLPStringMaps must give base values priority over override values when
// the same key exists in both maps. Disjoint keys from both maps must appear in
// the result.
func TestFormal_MergePrecedenceBaseWinsOverOverride(t *testing.T) {
	base := map[string]string{
		"shared.key":    "base-value",
		"base-only.key": "base-only-value",
	}
	override := map[string]string{
		"shared.key":        "override-value",
		"override-only.key": "override-only-value",
	}

	merged := mergeOTLPStringMaps(base, override)

	// Base wins on collision.
	assert.Equal(t, "base-value", merged["shared.key"])
	// Disjoint keys from both sides are present.
	assert.Equal(t, "base-only-value", merged["base-only.key"])
	assert.Equal(t, "override-only-value", merged["override-only.key"])
	assert.Len(t, merged, 3)
}

// P19 — MergeOfEmptyMapsYieldsNil
// mergeOTLPStringMaps must return nil (not an allocated empty map) when both
// inputs are nil or empty, so callers can rely on nil as a sentinel for
// "no attributes configured".
func TestFormal_MergeOfEmptyMapsYieldsNil(t *testing.T) {
	assert.Nil(t, mergeOTLPStringMaps(nil, nil))
	assert.Nil(t, mergeOTLPStringMaps(map[string]string{}, nil))
	assert.Nil(t, mergeOTLPStringMaps(nil, map[string]string{}))
	assert.Nil(t, mergeOTLPStringMaps(map[string]string{}, map[string]string{}))
}

// P20 — MetricResourceCardinalityBound
// High-cardinality per-run/per-user identifiers (gh-aw.run.id, gh-aw.run.uuid,
// user.id, session.id, trace.id, job.id, span.id, git.commit.sha, pr.number,
// issue.number, actor.id, url, conversation.id) must be excluded from default
// metric dimensions to prevent unbounded label growth. Stable, bounded
// attributes such as service.name and gh-aw.workflow.name must be allowed.
//
// Pending: no production metric-cardinality filter exists in pkg/workflow yet.
// Wire this predicate to the real filter once
// https://github.com/github/gh-aw/issues is addressed.
// See also specs/otel-observability-spec.md §metric-cardinality and ADR-49809.
func TestFormal_MetricResourceCardinalityBound(t *testing.T) {
	t.Skip("pending: no production metricAttributeRegistry implementation in pkg/workflow; " +
		"replace t.Skip with assertions against the real filter once it lands")
}

// P21 — InstrumentationScopeNaming
// The core instrumentation scope must be "gh-aw" and the MCP gateway scope
// must be "gh-aw-mcpg". The two scopes must be distinct so traces from each
// component can be filtered independently.
//
// Pending: no production instrumentation-scope resolver exists in pkg/workflow yet.
// Wire this predicate to the real resolver once it is implemented.
// See specs/otel-observability-spec.md §instrumentation-scope and ADR-49809.
func TestFormal_InstrumentationScopeNaming(t *testing.T) {
	t.Skip("pending: no production instrumentationScopeResolver implementation in pkg/workflow; " +
		"replace t.Skip with assertions against the real resolver once it lands")
}
