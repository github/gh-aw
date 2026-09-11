//go:build !integration

package workflow

import (
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractOTLPEndpointDomain verifies hostname extraction from OTLP endpoint URLs.
func TestExtractOTLPEndpointDomain(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		expected string
	}{
		{
			name:     "empty endpoint returns empty string",
			endpoint: "",
			expected: "",
		},
		{
			name:     "GitHub Actions expression returns empty string",
			endpoint: "${{ secrets.OTLP_ENDPOINT }}",
			expected: "",
		},
		{
			name:     "inline expression returns empty string",
			endpoint: "https://${{ secrets.HOST }}:4317",
			expected: "",
		},
		{
			name:     "HTTPS URL without port",
			endpoint: "https://traces.example.com",
			expected: "traces.example.com",
		},
		{
			name:     "HTTPS URL with port",
			endpoint: "https://traces.example.com:4317",
			expected: "traces.example.com",
		},
		{
			name:     "HTTP URL with path",
			endpoint: "http://otel-collector.internal:4318/v1/traces",
			expected: "otel-collector.internal",
		},
		{
			name:     "gRPC URL",
			endpoint: "grpc://traces.example.com:4317",
			expected: "traces.example.com",
		},
		{
			name:     "subdomain",
			endpoint: "https://otel.collector.corp.example.com:4317",
			expected: "otel.collector.corp.example.com",
		},
		{
			name:     "invalid URL (no scheme) returns empty string",
			endpoint: "traces.example.com:4317",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractOTLPEndpointDomain(tt.endpoint)
			assert.Equal(t, tt.expected, got, "extractOTLPEndpointDomain(%q)", tt.endpoint)
		})
	}
}

// TestGetOTLPEndpointEnvValue verifies endpoint value extraction from FrontmatterConfig.
func TestGetOTLPEndpointEnvValue(t *testing.T) {
	tests := []struct {
		name     string
		config   *FrontmatterConfig
		expected string
	}{
		{
			name:     "nil config returns empty string",
			config:   nil,
			expected: "",
		},
		{
			name:     "nil observability returns empty string",
			config:   &FrontmatterConfig{},
			expected: "",
		},
		{
			name: "nil OTLP returns empty string",
			config: &FrontmatterConfig{
				Observability: &ObservabilityConfig{},
			},
			expected: "",
		},
		{
			name: "empty string endpoint returns empty string",
			config: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{Endpoint: ""},
				},
			},
			expected: "",
		},
		{
			name: "static URL endpoint (string form)",
			config: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{Endpoint: "https://traces.example.com:4317"},
				},
			},
			expected: "https://traces.example.com:4317",
		},
		{
			name: "secret expression endpoint (string form)",
			config: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{Endpoint: "${{ secrets.OTLP_ENDPOINT }}"},
				},
			},
			expected: "${{ secrets.OTLP_ENDPOINT }}",
		},
		{
			name: "object form returns empty string (only string form handled by this function)",
			config: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{Endpoint: map[string]any{"url": "https://traces.example.com:4317"}},
				},
			},
			expected: "",
		},
		{
			name: "nil endpoint returns empty string",
			config: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{Endpoint: nil},
				},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getOTLPEndpointEnvValue(tt.config)
			assert.Equal(t, tt.expected, got, "getOTLPEndpointEnvValue")
		})
	}
}

func TestGetOTLPIfMissingMode(t *testing.T) {
	t.Run("uses parsed frontmatter value", func(t *testing.T) {
		got := getOTLPIfMissingMode(&FrontmatterConfig{
			Observability: &ObservabilityConfig{
				OTLP: &OTLPConfig{IfMissing: "ignore"},
			},
		}, nil)
		assert.Equal(t, "ignore", got)
	})

	t.Run("returns warn for parsed if-missing warn", func(t *testing.T) {
		got := getOTLPIfMissingMode(&FrontmatterConfig{
			Observability: &ObservabilityConfig{
				OTLP: &OTLPConfig{IfMissing: "warn"},
			},
		}, nil)
		assert.Equal(t, "warn", got)
	})

	t.Run("returns error for parsed if-missing error", func(t *testing.T) {
		got := getOTLPIfMissingMode(&FrontmatterConfig{
			Observability: &ObservabilityConfig{
				OTLP: &OTLPConfig{IfMissing: "error"},
			},
		}, nil)
		assert.Equal(t, "error", got)
	})

	t.Run("falls back to raw frontmatter if-missing value", func(t *testing.T) {
		got := getOTLPIfMissingMode(nil, map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{
					"if-missing": "ignore",
				},
			},
		})
		assert.Equal(t, "ignore", got)
	})

	t.Run("falls back to raw frontmatter warn value", func(t *testing.T) {
		got := getOTLPIfMissingMode(nil, map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{
					"if-missing": "warn",
				},
			},
		})
		assert.Equal(t, "warn", got)
	})

	t.Run("returns empty for invalid raw frontmatter value", func(t *testing.T) {
		got := getOTLPIfMissingMode(nil, map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{
					"if-missing": "ignor",
				},
			},
		})
		assert.Empty(t, got)
	})

	t.Run("returns empty when unset", func(t *testing.T) {
		got := getOTLPIfMissingMode(nil, map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{},
			},
		})
		assert.Empty(t, got)
	})
}

func TestGetOTLPGitHubApp(t *testing.T) {
	t.Run("returns parsed github-app config", func(t *testing.T) {
		got := getOTLPGitHubApp(&FrontmatterConfig{
			Observability: &ObservabilityConfig{
				OTLP: &OTLPConfig{
					GitHubApp: &OTLPGitHubAppConfig{
						Audience: "https://collector.example.com",
					},
				},
			},
		}, nil)
		require.NotNil(t, got)
		assert.Equal(t, "https://collector.example.com", got.Audience)
	})

	t.Run("returns raw github-app config", func(t *testing.T) {
		got := getOTLPGitHubApp(nil, map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{
					"github-app": map[string]any{
						"audience": "api://AzureADTokenExchange",
					},
				},
			},
		})
		require.NotNil(t, got)
		assert.Equal(t, "api://AzureADTokenExchange", got.Audience)
	})

	t.Run("returns nil when github-app is missing", func(t *testing.T) {
		got := getOTLPGitHubApp(nil, map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{},
			},
		})
		assert.Nil(t, got)
	})

	t.Run("returns nil for invalid raw structure", func(t *testing.T) {
		assert.Nil(t, getOTLPGitHubApp(nil, map[string]any{
			"observability": "invalid",
		}))
		assert.Nil(t, getOTLPGitHubApp(nil, map[string]any{
			"observability": map[string]any{
				"otlp": "invalid",
			},
		}))
		assert.Nil(t, getOTLPGitHubApp(nil, map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{
					"github-app": "invalid",
				},
			},
		}))
	})
}

func TestHasOTLPGitHubOIDCAuth(t *testing.T) {
	assert.True(t, hasOTLPGitHubOIDCAuth(nil, map[string]any{
		"observability": map[string]any{
			"otlp": map[string]any{
				"workload-identity": map[string]any{
					"provider": "google",
					"audience": "//iam.googleapis.com/projects/123/locations/global/workloadIdentityPools/pool/providers/github",
				},
			},
		},
	}))

	assert.True(t, hasOTLPGitHubOIDCAuth(&FrontmatterConfig{
		Observability: &ObservabilityConfig{
			OTLP: &OTLPConfig{
				GitHubApp: &OTLPGitHubAppConfig{},
			},
		},
	}, nil))

	assert.True(t, hasOTLPGitHubOIDCAuth(nil, map[string]any{
		"observability": map[string]any{
			"otlp": map[string]any{
				"github-app": map[string]any{},
			},
		},
	}))

	assert.False(t, hasOTLPGitHubOIDCAuth(nil, map[string]any{
		"observability": map[string]any{
			"otlp": map[string]any{},
		},
	}))

	assert.False(t, hasOTLPGitHubOIDCAuth(nil, map[string]any{
		"observability": map[string]any{
			"otlp": map[string]any{
				"github-app": map[string]any{
					"app-id":      "${{ vars.APP_ID }}",
					"private-key": "${{ secrets.APP_PRIVATE_KEY }}",
				},
			},
		},
	}))
}

func TestGetOTLPGitHubAppTokenConfig(t *testing.T) {
	got := getOTLPGitHubAppTokenConfig(map[string]any{
		"observability": map[string]any{
			"otlp": map[string]any{
				"github-app": map[string]any{
					"app-id":      "${{ vars.APP_ID }}",
					"private-key": "${{ secrets.APP_PRIVATE_KEY }}",
				},
			},
		},
	})
	require.NotNil(t, got)
	assert.Equal(t, "${{ vars.APP_ID }}", got.AppID)
	assert.Equal(t, "${{ secrets.APP_PRIVATE_KEY }}", got.PrivateKey)
}

// TestInjectOTLPConfig verifies that injectOTLPConfig correctly modifies WorkflowData.
func TestInjectOTLPConfig(t *testing.T) {
	newCompiler := func() *Compiler { return &Compiler{} }

	t.Run("falls back to enterprise defaults when OTLP is not configured", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{},
		}
		c.injectOTLPConfig(wd)
		assert.Nil(t, wd.NetworkPermissions, "NetworkPermissions should remain nil for expression endpoints")
		assert.True(t, wd.OTLPUsesEnterpriseDefaults, "enterprise defaults should be flagged")
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_ENDPOINT: ${{ secrets.GH_AW_DEFAULT_OTLP_ENDPOINT || vars.GH_AW_DEFAULT_OTLP_ENDPOINT }}")
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_HEADERS: ${{ secrets.GH_AW_DEFAULT_OTLP_HEADERS }}")
		assert.Contains(t, wd.Env, "GH_AW_OTLP_IF_MISSING: ignore", "unset enterprise defaults must be a no-op")
	})

	t.Run("falls back to enterprise defaults when ParsedFrontmatter is nil", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{}
		c.injectOTLPConfig(wd)
		assert.Nil(t, wd.NetworkPermissions, "NetworkPermissions should remain nil for expression endpoints")
		assert.True(t, wd.OTLPUsesEnterpriseDefaults, "enterprise defaults should be flagged")
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_ENDPOINT: ${{ secrets.GH_AW_DEFAULT_OTLP_ENDPOINT || vars.GH_AW_DEFAULT_OTLP_ENDPOINT }}")
	})

	t.Run("injects env vars when endpoint is a secret expression", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{Endpoint: "${{ secrets.OTLP_ENDPOINT }}"},
				},
			},
		}
		c.injectOTLPConfig(wd)

		// NetworkPermissions.Allowed should NOT be populated (can't resolve expression)
		if wd.NetworkPermissions != nil {
			assert.Empty(t, wd.NetworkPermissions.Allowed, "Allowed should be empty for expression endpoints")
		}

		// Env should contain the OTEL vars
		require.NotEmpty(t, wd.Env, "Env should be set")
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_ENDPOINT: ${{ secrets.OTLP_ENDPOINT }}", "should contain endpoint var")
		assert.Contains(t, wd.Env, "OTEL_SERVICE_NAME: gh-aw", "should contain service name")
	})

	t.Run("injects if-missing env var when if-missing is set to ignore", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{
						Endpoint:  "${{ secrets.OTLP_ENDPOINT }}",
						IfMissing: "ignore",
					},
				},
			},
		}
		c.injectOTLPConfig(wd)
		require.NotEmpty(t, wd.Env)
		assert.Contains(t, wd.Env, "GH_AW_OTLP_IF_MISSING: ignore")
	})

	t.Run("injects if-missing env var when if-missing is set to warn", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{
						Endpoint:  "${{ secrets.OTLP_ENDPOINT }}",
						IfMissing: "warn",
					},
				},
			},
		}
		c.injectOTLPConfig(wd)
		require.NotEmpty(t, wd.Env)
		assert.Contains(t, wd.Env, "GH_AW_OTLP_IF_MISSING: warn")
	})

	t.Run("allows Google workload identity hosts even for expression endpoints", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			RawFrontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": "${{ secrets.OTLP_ENDPOINT }}",
						"workload-identity": map[string]any{
							"provider": "google",
							"audience": "projects/123/locations/global/workloadIdentityPools/pool/providers/github",
						},
					},
				},
			},
		}
		c.injectOTLPConfig(wd)

		require.NotNil(t, wd.NetworkPermissions, "NetworkPermissions should be created")
		assert.Contains(t, wd.NetworkPermissions.Allowed, "sts.googleapis.com")
		assert.Contains(t, wd.NetworkPermissions.Allowed, "iamcredentials.googleapis.com")
	})

	t.Run("adds domain to new NetworkPermissions and injects env vars for static URL", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{Endpoint: "https://traces.example.com:4317"},
				},
			},
		}
		c.injectOTLPConfig(wd)

		require.NotNil(t, wd.NetworkPermissions, "NetworkPermissions should be created")
		assert.Contains(t, wd.NetworkPermissions.Allowed, "traces.example.com", "should contain OTLP domain")

		require.NotEmpty(t, wd.Env, "Env should be set")
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_ENDPOINT: https://traces.example.com:4317")
		assert.Contains(t, wd.Env, "OTEL_SERVICE_NAME: gh-aw")
		assert.True(t, strings.HasPrefix(wd.Env, "env:"), "Env should start with 'env:'")
	})

	t.Run("injects OTEL_RESOURCE_ATTRIBUTES with gh-aw context and engine id", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			AI: "copilot",
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{Endpoint: "https://traces.example.com:4317"},
				},
			},
		}
		c.injectOTLPConfig(wd)

		assert.Contains(
			t,
			wd.Env,
			"OTEL_RESOURCE_ATTRIBUTES: 'gh-aw.workflow.name=unknown,gh-aw.repository=${{ github.repository }},gh-aw.run.id=${{ github.run_id }},github.run_id=${{ github.run_id }},gh-aw.engine.id=copilot'",
		)
	})

	t.Run("injects OTEL_RESOURCE_ATTRIBUTES without engine id when unavailable", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{Endpoint: "https://traces.example.com:4317"},
				},
			},
		}
		c.injectOTLPConfig(wd)

		assert.Contains(
			t,
			wd.Env,
			"OTEL_RESOURCE_ATTRIBUTES: 'gh-aw.workflow.name=unknown,gh-aw.repository=${{ github.repository }},gh-aw.run.id=${{ github.run_id }},github.run_id=${{ github.run_id }}'",
		)
		assert.NotContains(t, wd.Env, "gh-aw.engine.id=")
	})

	t.Run("percent-encodes OTEL_RESOURCE_ATTRIBUTES engine id value", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			AI: `copilot,eq=uals\slash`,
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{Endpoint: "https://traces.example.com:4317"},
				},
			},
		}
		c.injectOTLPConfig(wd)

		assert.Contains(
			t,
			wd.Env,
			`gh-aw.engine.id=copilot%2Ceq%3Duals%5Cslash`,
		)
	})

	t.Run("percent-encodes OTEL_RESOURCE_ATTRIBUTES workflow name value", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			Name: "triage weekly,run\\v2",
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{Endpoint: "https://traces.example.com:4317"},
				},
			},
		}
		c.injectOTLPConfig(wd)

		assert.Contains(t, wd.Env, `gh-aw.workflow.name=triage%20weekly%2Crun%5Cv2`)
	})

	t.Run("percent-encodes apostrophes in OTEL_RESOURCE_ATTRIBUTES values", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			Name: "owner's workflow",
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{Endpoint: "https://traces.example.com:4317"},
				},
			},
		}
		c.injectOTLPConfig(wd)

		assert.Contains(t, wd.Env, "OTEL_RESOURCE_ATTRIBUTES:", "resource attributes should be injected")
		assert.Contains(
			t,
			wd.Env,
			"OTEL_RESOURCE_ATTRIBUTES: 'gh-aw.workflow.name=owner%27s%20workflow,gh-aw.repository=${{ github.repository }},gh-aw.run.id=${{ github.run_id }},github.run_id=${{ github.run_id }}'",
			"resource attributes should remain fully single-quoted after percent-encoding",
		)
	})

	t.Run("appends custom OTLP resource attributes", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			AI:   "copilot",
			Name: "triage weekly",
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{
						Endpoint: "https://traces.example.com:4317",
						ResourceAttributes: map[string]string{
							"my.actor":       "${{ github.actor }}",
							"my.target repo": "owner/repo,weekly",
						},
					},
				},
			},
		}
		c.injectOTLPConfig(wd)

		assert.Contains(
			t,
			wd.Env,
			"OTEL_RESOURCE_ATTRIBUTES: 'gh-aw.workflow.name=triage%20weekly,gh-aw.repository=${{ github.repository }},gh-aw.run.id=${{ github.run_id }},github.run_id=${{ github.run_id }},gh-aw.engine.id=copilot,my.actor=${{ github.actor }},my.target%20repo=owner%2Frepo%2Cweekly'",
		)
	})

	t.Run("appends domain to existing NetworkPermissions.Allowed", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{Endpoint: "https://traces.example.com:4317"},
				},
			},
			NetworkPermissions: &NetworkPermissions{
				Allowed: []string{"api.github.com", "pypi.org"},
			},
		}
		c.injectOTLPConfig(wd)

		assert.Contains(t, wd.NetworkPermissions.Allowed, "api.github.com", "existing domains should remain")
		assert.Contains(t, wd.NetworkPermissions.Allowed, "pypi.org", "existing domains should remain")
		assert.Contains(t, wd.NetworkPermissions.Allowed, "traces.example.com", "OTLP domain should be appended")
	})

	t.Run("appends OTEL vars to existing Env block", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{Endpoint: "https://traces.example.com"},
				},
			},
			Env: "env:\n  MY_VAR: hello",
		}
		c.injectOTLPConfig(wd)

		assert.Contains(t, wd.Env, "MY_VAR: hello", "existing env var should remain")
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_ENDPOINT: https://traces.example.com")
		assert.Contains(t, wd.Env, "OTEL_SERVICE_NAME: gh-aw")
		// Should still be a single env: block
		assert.Equal(t, 1, strings.Count(wd.Env, "env:"), "should have exactly one env: key")
	})

	t.Run("preserves user-defined OTEL_SERVICE_NAME and does not inject duplicate", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{Endpoint: "https://traces.example.com"},
				},
			},
			Env: "env:\n  OTEL_SERVICE_NAME: my-service",
		}
		c.injectOTLPConfig(wd)

		assert.Contains(t, wd.Env, "OTEL_SERVICE_NAME: my-service", "user-defined service name should be preserved")
		assert.Equal(t, 1, strings.Count(wd.Env, "OTEL_SERVICE_NAME:"), "OTEL_SERVICE_NAME should appear exactly once")
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_ENDPOINT: https://traces.example.com", "endpoint should still be injected")
	})

	t.Run("preserves user-defined OTEL_EXPORTER_OTLP_ENDPOINT and does not inject duplicate", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{Endpoint: "https://traces.example.com"},
				},
			},
			Env: "env:\n  OTEL_EXPORTER_OTLP_ENDPOINT: https://user-collector.example.com",
		}
		c.injectOTLPConfig(wd)

		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_ENDPOINT: https://user-collector.example.com", "user-defined endpoint should be preserved")
		assert.Equal(t, 1, strings.Count(wd.Env, "OTEL_EXPORTER_OTLP_ENDPOINT:"), "OTEL_EXPORTER_OTLP_ENDPOINT should appear exactly once")
	})

	t.Run("preserves user-defined GH_AW_OTLP_ENDPOINTS and does not inject duplicate", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{Endpoint: "https://traces.example.com"},
				},
			},
			Env: "env:\n  GH_AW_OTLP_ENDPOINTS: '[]'",
		}
		c.injectOTLPConfig(wd)

		assert.Equal(t, 1, strings.Count(wd.Env, "GH_AW_OTLP_ENDPOINTS:"), "GH_AW_OTLP_ENDPOINTS should appear exactly once")
	})

	t.Run("OTEL_SERVICE_NAME includes sanitized workflow ID when available", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			WorkflowID: "Repo Triage/Weekly",
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{Endpoint: "https://otel.corp.com"},
				},
			},
		}
		c.injectOTLPConfig(wd)
		assert.Contains(t, wd.Env, "OTEL_SERVICE_NAME: gh-aw.repo-triage-weekly", "service name should include sanitized workflow ID")
	})

	t.Run("injects OTEL_EXPORTER_OTLP_HEADERS when headers are configured", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{
						Endpoint: "https://traces.example.com",
						Headers:  "Authorization=Bearer tok,X-Tenant=acme",
					},
				},
			},
		}
		c.injectOTLPConfig(wd)
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_HEADERS: Authorization=Bearer tok,X-Tenant=acme", "headers var should be injected")
	})

	t.Run("injects OTEL_EXPORTER_OTLP_HEADERS for secret expression", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{
						Endpoint: "https://traces.example.com",
						Headers:  "${{ secrets.OTLP_HEADERS }}",
					},
				},
			},
		}
		c.injectOTLPConfig(wd)
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_HEADERS: ${{ secrets.OTLP_HEADERS }}", "headers var should support secret expressions")
	})

	t.Run("does not inject OTEL_EXPORTER_OTLP_HEADERS when headers not configured", func(t *testing.T) {
		c := newCompiler()
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{Endpoint: "https://traces.example.com"},
				},
			},
		}
		c.injectOTLPConfig(wd)
		assert.NotContains(t, wd.Env, "OTEL_EXPORTER_OTLP_HEADERS", "headers var should not appear when unconfigured")
	})
}

// TestObservabilityConfigParsing verifies that the OTLPConfig is correctly parsed
// from raw frontmatter via ParseFrontmatterConfig.
func TestObservabilityConfigParsing(t *testing.T) {
	tests := []struct {
		name             string
		frontmatter      map[string]any
		wantOTLPConfig   bool
		expectedEndpoint string
		expectedHeaders  string
	}{
		{
			name:           "no observability section",
			frontmatter:    map[string]any{},
			wantOTLPConfig: false,
		},
		{
			name:           "observability without otlp",
			frontmatter:    map[string]any{"observability": map[string]any{}},
			wantOTLPConfig: false,
		},
		{
			name: "observability with otlp endpoint",
			frontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": "https://traces.example.com:4317",
					},
				},
			},
			wantOTLPConfig:   true,
			expectedEndpoint: "https://traces.example.com:4317",
		},
		{
			name: "observability with otlp secret expression",
			frontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": "${{ secrets.OTLP_ENDPOINT }}",
					},
				},
			},
			wantOTLPConfig:   true,
			expectedEndpoint: "${{ secrets.OTLP_ENDPOINT }}",
		},
		{
			name: "observability with both otlp endpoint and config",
			frontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": "https://traces.example.com",
					},
				},
			},
			wantOTLPConfig:   true,
			expectedEndpoint: "https://traces.example.com",
		},
		{
			name: "observability with otlp endpoint and headers",
			frontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": "https://traces.example.com",
						"headers":  "Authorization=Bearer tok,X-Tenant=acme",
					},
				},
			},
			wantOTLPConfig:   true,
			expectedEndpoint: "https://traces.example.com",
			expectedHeaders:  "Authorization=Bearer tok,X-Tenant=acme",
		},
		{
			name: "observability with otlp headers as secret expression",
			frontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": "https://traces.example.com",
						"headers":  "${{ secrets.OTLP_HEADERS }}",
					},
				},
			},
			wantOTLPConfig:   true,
			expectedEndpoint: "https://traces.example.com",
			expectedHeaders:  "${{ secrets.OTLP_HEADERS }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := ParseFrontmatterConfig(tt.frontmatter)
			require.NoError(t, err, "ParseFrontmatterConfig should not fail")
			require.NotNil(t, config, "Config should not be nil")

			if !tt.wantOTLPConfig {
				if config.Observability != nil {
					assert.Nil(t, config.Observability.OTLP, "OTLP should be nil")
				}
				return
			}

			require.NotNil(t, config.Observability, "Observability should not be nil")
			require.NotNil(t, config.Observability.OTLP, "OTLP should not be nil")
			assert.Equal(t, tt.expectedEndpoint, config.Observability.OTLP.Endpoint, "Endpoint should match")
			// Normalize Headers (any) to string for comparison
			normalizedHeaders := normalizeOTLPHeadersForEndpoint(config.Observability.OTLP.Headers, "")
			assert.Equal(t, tt.expectedHeaders, normalizedHeaders, "Headers should match")
		})
	}
}

func TestObservabilityConfigParsing_OTLPResourceAttributes(t *testing.T) {
	config, err := ParseFrontmatterConfig(map[string]any{
		"observability": map[string]any{
			"otlp": map[string]any{
				"resource-attributes": map[string]any{
					"my.target-repo": "${{ github.repository }}",
					"my.event":       "repository_dispatch",
				},
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, config)
	require.NotNil(t, config.Observability)
	require.NotNil(t, config.Observability.OTLP)
	assert.Equal(t, map[string]string{
		"my.target-repo": "${{ github.repository }}",
		"my.event":       "repository_dispatch",
	}, config.Observability.OTLP.ResourceAttributes)
}

// TestInjectOTLPConfig_RawFrontmatterFallback verifies that injectOTLPConfig works
// when ParsedFrontmatter is nil (e.g. complex engine objects cause ParseFrontmatterConfig
// to fail) but the raw frontmatter contains valid OTLP configuration.
func TestInjectOTLPConfig_RawFrontmatterFallback(t *testing.T) {
	c := &Compiler{}

	t.Run("injects OTLP from raw frontmatter when ParsedFrontmatter is nil", func(t *testing.T) {
		wd := &WorkflowData{
			ParsedFrontmatter: nil, // simulates ParseFrontmatterConfig failure
			RawFrontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": "${{ secrets.GH_AW_OTEL_ENDPOINT }}",
						"headers":  "${{ secrets.GH_AW_OTEL_HEADERS }}",
					},
				},
				// Simulate complex engine object that would cause ParseFrontmatterConfig to fail.
				"engine": map[string]any{"id": "copilot", "max-continuations": 2},
			},
		}
		c.injectOTLPConfig(wd)

		require.NotEmpty(t, wd.Env, "Env should be set even without ParsedFrontmatter")
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_ENDPOINT: ${{ secrets.GH_AW_OTEL_ENDPOINT }}", "endpoint should be injected from raw")
		assert.Contains(t, wd.Env, "OTEL_SERVICE_NAME: gh-aw", "service name should be set")
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_HEADERS: ${{ secrets.GH_AW_OTEL_HEADERS }}", "headers should be injected from raw")
	})

	t.Run("uses enterprise defaults when neither raw nor parsed frontmatter has OTLP", func(t *testing.T) {
		wd := &WorkflowData{
			ParsedFrontmatter: nil,
			RawFrontmatter:    map[string]any{"name": "my-workflow"},
		}
		c.injectOTLPConfig(wd)
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_ENDPOINT: ${{ secrets.GH_AW_DEFAULT_OTLP_ENDPOINT || vars.GH_AW_DEFAULT_OTLP_ENDPOINT }}")
		assert.Nil(t, wd.NetworkPermissions, "NetworkPermissions should remain nil")
	})
}

// TestIsOTLPHeadersPresent verifies that isOTLPHeadersPresent correctly detects
// whether OTEL_EXPORTER_OTLP_HEADERS is present in the workflow env block.
func TestIsOTLPHeadersPresent(t *testing.T) {
	tests := []struct {
		name     string
		data     *WorkflowData
		expected bool
	}{
		{
			name:     "nil WorkflowData returns false",
			data:     nil,
			expected: false,
		},
		{
			name:     "empty Env returns false",
			data:     &WorkflowData{},
			expected: false,
		},
		{
			name: "Env without OTEL_EXPORTER_OTLP_HEADERS returns false",
			data: &WorkflowData{
				Env: "env:\n  OTEL_EXPORTER_OTLP_ENDPOINT: https://traces.example.com\n  OTEL_SERVICE_NAME: gh-aw",
			},
			expected: false,
		},
		{
			name: "Env with OTEL_EXPORTER_OTLP_HEADERS returns true",
			data: &WorkflowData{
				Env: "env:\n  OTEL_EXPORTER_OTLP_ENDPOINT: https://traces.example.com\n  OTEL_SERVICE_NAME: gh-aw\n  OTEL_EXPORTER_OTLP_HEADERS: Authorization=Bearer tok",
			},
			expected: true,
		},
		{
			name: "Env with secret expression headers returns true",
			data: &WorkflowData{
				Env: "env:\n  OTEL_EXPORTER_OTLP_ENDPOINT: ${{ secrets.OTLP_ENDPOINT }}\n  OTEL_SERVICE_NAME: gh-aw\n  OTEL_EXPORTER_OTLP_HEADERS: ${{ secrets.OTLP_HEADERS }}",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isOTLPHeadersPresent(tt.data)
			assert.Equal(t, tt.expected, got, "isOTLPHeadersPresent")
		})
	}
}

// TestGenerateOTLPHeadersMaskStep verifies that generateOTLPHeadersMaskStep
// emits a step that delegates to mask_otlp_headers.sh.
func TestGenerateOTLPHeadersMaskStep(t *testing.T) {
	step := generateOTLPHeadersMaskStep()

	assert.Contains(t, step, "- name: Mask OTLP telemetry headers", "should have the masking step name")
	assert.Contains(t, step, "mask_otlp_headers.sh", "should delegate to the mask_otlp_headers.sh script")
	assert.Contains(t, step, "${RUNNER_TEMP}/gh-aw/actions/", "should reference the runtime actions directory")
}

// TestInjectOTLPConfig_HeadersPresenceAfterInjection verifies that
// isOTLPHeadersPresent returns the expected value after injectOTLPConfig runs.
func TestInjectOTLPConfig_HeadersPresenceAfterInjection(t *testing.T) {
	t.Run("isOTLPHeadersPresent returns true after headers are injected", func(t *testing.T) {
		c := &Compiler{}
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{
						Endpoint: "https://traces.example.com",
						Headers:  "Authorization=Bearer tok",
					},
				},
			},
		}
		c.injectOTLPConfig(wd)
		assert.True(t, isOTLPHeadersPresent(wd), "isOTLPHeadersPresent should return true after headers are injected")
	})

	t.Run("isOTLPHeadersPresent returns false when no headers are configured", func(t *testing.T) {
		c := &Compiler{}
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{
						Endpoint: "https://traces.example.com",
					},
				},
			},
		}
		c.injectOTLPConfig(wd)
		assert.False(t, isOTLPHeadersPresent(wd), "isOTLPHeadersPresent should return false when no headers are configured")
	})
}

// TestIsOTLPAttributesPresent verifies that isOTLPAttributesPresent correctly detects
// whether GH_AW_OTLP_ATTRIBUTES is present in the workflow env block.
func TestIsOTLPAttributesPresent(t *testing.T) {
	tests := []struct {
		name     string
		data     *WorkflowData
		expected bool
	}{
		{
			name:     "nil WorkflowData returns false",
			data:     nil,
			expected: false,
		},
		{
			name:     "empty Env returns false",
			data:     &WorkflowData{},
			expected: false,
		},
		{
			name: "Env without GH_AW_OTLP_ATTRIBUTES returns false",
			data: &WorkflowData{
				Env: "env:\n  OTEL_EXPORTER_OTLP_ENDPOINT: https://traces.example.com\n  OTEL_SERVICE_NAME: gh-aw",
			},
			expected: false,
		},
		{
			name: "Env with GH_AW_OTLP_ATTRIBUTES returns true",
			data: &WorkflowData{
				Env: `env:
  OTEL_EXPORTER_OTLP_ENDPOINT: https://traces.example.com
  OTEL_SERVICE_NAME: gh-aw
  GH_AW_OTLP_ATTRIBUTES: '{"langfuse.session.id":"abc"}'`,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isOTLPAttributesPresent(tt.data)
			assert.Equal(t, tt.expected, got, "isOTLPAttributesPresent")
		})
	}
}

// TestGenerateOTLPAttributesMaskStep verifies that generateOTLPAttributesMaskStep
// emits a step that delegates to mask_otlp_attributes.sh.
func TestGenerateOTLPAttributesMaskStep(t *testing.T) {
	step := generateOTLPAttributesMaskStep()

	assert.Contains(t, step, "- name: Mask OTLP custom attribute values", "should have the masking step name")
	assert.Contains(t, step, "mask_otlp_attributes.sh", "should delegate to the mask_otlp_attributes.sh script")
	assert.Contains(t, step, "${RUNNER_TEMP}/gh-aw/actions/", "should reference the runtime actions directory")
}

// TestInjectOTLPConfig_AttributesPresenceAfterInjection verifies that
// isOTLPAttributesPresent returns true after injectOTLPConfig injects attributes.
func TestInjectOTLPConfig_AttributesPresenceAfterInjection(t *testing.T) {
	t.Run("isOTLPAttributesPresent returns true after attributes are injected", func(t *testing.T) {
		c := &Compiler{}
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{
						Endpoint: "https://traces.example.com",
						Attributes: map[string]string{
							"langfuse.session.id": "my-session",
						},
					},
				},
			},
		}
		c.injectOTLPConfig(wd)
		assert.True(t, isOTLPAttributesPresent(wd), "isOTLPAttributesPresent should return true after attributes are injected")
	})

	t.Run("isOTLPAttributesPresent returns false when no attributes are configured", func(t *testing.T) {
		c := &Compiler{}
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{
						Endpoint: "https://traces.example.com",
					},
				},
			},
		}
		c.injectOTLPConfig(wd)
		assert.False(t, isOTLPAttributesPresent(wd), "isOTLPAttributesPresent should return false when no attributes are configured")
	})
}

func TestOTELServiceName(t *testing.T) {
	t.Run("uses workflow-specific service name when workflow ID is present", func(t *testing.T) {
		got := otelServiceName(&WorkflowData{WorkflowID: "Repo Triage/Weekly"})
		assert.Equal(t, "gh-aw.repo-triage-weekly", got, "should use WorkflowID as service name suffix when present")
	})

	t.Run("falls back to workflow name when workflow ID is empty", func(t *testing.T) {
		got := otelServiceName(&WorkflowData{Name: "Repo Triage/Weekly"})
		assert.Equal(t, "gh-aw.repo-triage-weekly", got, "should fall back to workflow name when WorkflowID is empty")
	})

	t.Run("workflow ID takes precedence over workflow name", func(t *testing.T) {
		got := otelServiceName(&WorkflowData{
			WorkflowID: "Unique Workflow ID",
			Name:       "Shared Display Name",
		})
		assert.Equal(t, "gh-aw.unique-workflow-id", got, "should prefer WorkflowID over workflow name when both are present")
	})

	t.Run("falls back when workflow ID and name are empty", func(t *testing.T) {
		got := otelServiceName(&WorkflowData{})
		assert.Equal(t, "gh-aw", got, "should return default service name when WorkflowID and name are empty")
	})

	t.Run("falls back when workflow data is nil", func(t *testing.T) {
		got := otelServiceName(nil)
		assert.Equal(t, "gh-aw", got, "should return default service name when workflow data is nil")
	})
}

// TestInjectOTLPConfig_OTLPEndpointField verifies that injectOTLPConfig sets workflowData.OTLPEndpoint
// so that downstream code (buildMCPGatewayConfig, mcp_setup_generator) can use it as the
// single source of truth for "is OTLP configured?" without re-reading raw frontmatter.
func TestInjectOTLPConfig_OTLPEndpointField(t *testing.T) {
	c := &Compiler{}

	t.Run("sets OTLPEndpoint when endpoint is configured", func(t *testing.T) {
		wd := &WorkflowData{
			RawFrontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": "https://traces.example.com:4318",
					},
				},
			},
		}
		c.injectOTLPConfig(wd)
		assert.Equal(t, "https://traces.example.com:4318", wd.OTLPEndpoint, "OTLPEndpoint should be set to the resolved endpoint")
	})

	t.Run("sets OTLPEndpoint from enterprise defaults when OTLP is not configured", func(t *testing.T) {
		wd := &WorkflowData{
			RawFrontmatter: map[string]any{"name": "no-otlp"},
		}
		c.injectOTLPConfig(wd)
		assert.Equal(t, "${{ secrets.GH_AW_DEFAULT_OTLP_ENDPOINT || vars.GH_AW_DEFAULT_OTLP_ENDPOINT }}", wd.OTLPEndpoint, "OTLPEndpoint should prefer the enterprise default secret and fall back to the variable")
		assert.Equal(t, "${{ secrets.GH_AW_DEFAULT_OTLP_HEADERS }}", wd.OTLPHeaders, "OTLPHeaders should fall back to the enterprise default secret")
	})

	t.Run("sets OTLPEndpoint from imported observability merged into RawFrontmatter", func(t *testing.T) {
		// Simulate what compiler_orchestrator_workflow.go does when importing shared/otlp.md:
		// the imported observability JSON is decoded and injected into RawFrontmatter before injectOTLPConfig runs.
		wd := &WorkflowData{
			RawFrontmatter: map[string]any{
				// Imported observability merged in (top-level has no observability key)
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": "${{ secrets.GH_AW_OTEL_ENDPOINT }}",
						"headers":  "${{ secrets.GH_AW_OTEL_HEADERS }}",
					},
				},
			},
		}
		c.injectOTLPConfig(wd)
		assert.Equal(t, "${{ secrets.GH_AW_OTEL_ENDPOINT }}", wd.OTLPEndpoint, "OTLPEndpoint should be set from imported observability")
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_ENDPOINT:", "env var should be injected")
	})
}

// TestInjectOTLPConfig_OTLPHeadersField verifies that injectOTLPConfig sets workflowData.OTLPHeaders
// so that buildMCPGatewayConfig can read it directly instead of re-reading raw frontmatter.
func TestInjectOTLPConfig_OTLPHeadersField(t *testing.T) {
	c := &Compiler{}

	t.Run("sets OTLPHeaders when headers are configured (map form)", func(t *testing.T) {
		wd := &WorkflowData{
			RawFrontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": "https://traces.example.com",
						"headers":  map[string]any{"Authorization": "Bearer tok", "X-Tenant": "acme"},
					},
				},
			},
		}
		c.injectOTLPConfig(wd)
		assert.Equal(t, "Authorization=Bearer tok,X-Tenant=acme", wd.OTLPHeaders, "OTLPHeaders should be set from map form")
	})

	t.Run("sets OTLPHeaders when headers are configured (string form)", func(t *testing.T) {
		wd := &WorkflowData{
			RawFrontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": "https://traces.example.com",
						"headers":  "Authorization=Bearer tok",
					},
				},
			},
		}
		c.injectOTLPConfig(wd)
		assert.Equal(t, "Authorization=Bearer tok", wd.OTLPHeaders, "OTLPHeaders should be set from string form")
	})

	t.Run("OTLPHeaders is empty when no headers are configured", func(t *testing.T) {
		wd := &WorkflowData{
			RawFrontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{"endpoint": "https://traces.example.com"},
				},
			},
		}
		c.injectOTLPConfig(wd)
		assert.Empty(t, wd.OTLPHeaders, "OTLPHeaders should be empty when no headers are configured")
	})
}

func TestNormalizeOTLPHeadersForEndpoint(t *testing.T) {
	t.Run("rewrites Authorization header for sentry URL", func(t *testing.T) {
		gotHeaders := normalizeOTLPHeadersForEndpoint(
			map[string]any{"Authorization": "Bearer tok"},
			"https://o123.ingest.sentry.io/api/123/envelope/",
		)
		assert.Equal(t, "x-sentry-auth=Bearer tok", gotHeaders, "Sentry endpoints should use x-sentry-auth")
	})

	t.Run("rewrites Authorization header for known sentry endpoint expression", func(t *testing.T) {
		gotHeaders := normalizeOTLPHeadersForEndpoint(
			"Authorization=Bearer tok,X-Tenant=acme",
			"${{ secrets.GH_AW_OTEL_SENTRY_ENDPOINT }}",
		)
		assert.Equal(t, "x-sentry-auth=Bearer tok,X-Tenant=acme", gotHeaders, "Sentry-named endpoint expressions should use x-sentry-auth")
	})

	t.Run("rewrites Authorization header for sentry URL with additional headers", func(t *testing.T) {
		gotHeaders := normalizeOTLPHeadersForEndpoint(
			"Authorization=Bearer tok,X-Tenant=acme",
			"https://o123.ingest.sentry.io/api/123/envelope/",
		)
		assert.Equal(t, "x-sentry-auth=Bearer tok,X-Tenant=acme", gotHeaders, "Sentry endpoints should rewrite Authorization while preserving additional headers")
	})

	t.Run("preserves Authorization header for non-standard sentry endpoint expressions", func(t *testing.T) {
		gotHeaders := normalizeOTLPHeadersForEndpoint(
			"Authorization=Bearer tok,X-Tenant=acme",
			"${{ secrets.TEAM_SENTRY_PROXY_ENDPOINT }}",
		)
		assert.Equal(t, "Authorization=Bearer tok,X-Tenant=acme", gotHeaders, "Only the known Sentry endpoint expression should use x-sentry-auth")
	})

	t.Run("preserves Authorization header for grafana endpoint", func(t *testing.T) {
		gotHeaders := normalizeOTLPHeadersForEndpoint(
			map[string]any{"Authorization": "Bearer tok", "X-Scope-OrgID": "tenant"},
			"https://otlp-gateway-prod-us-central-0.grafana.net/otlp",
		)
		assert.Equal(t, "Authorization=Bearer tok,X-Scope-OrgID=tenant", gotHeaders, "Non-Sentry endpoints should keep Authorization")
	})

	t.Run("preserves Authorization header when sentry appears outside URL host", func(t *testing.T) {
		gotHeaders := normalizeOTLPHeadersForEndpoint(
			"Authorization=Bearer tok,X-Tenant=acme",
			"https://otlp-gateway-prod-us-central-0.grafana.net/sentry/proxy",
		)
		assert.Equal(t, "Authorization=Bearer tok,X-Tenant=acme", gotHeaders, "Only Sentry hosts should use x-sentry-auth")
	})
}

func TestIsGitHubActionsExpression(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{name: "valid expression", input: "${{ secrets.FOO }}", expected: true},
		{name: "valid expression with surrounding whitespace", input: "  ${{ secrets.FOO }}  ", expected: true},
		{name: "missing suffix", input: "${{ secrets.FOO }", expected: false},
		{name: "missing prefix", input: "secrets.FOO }}", expected: false},
		{name: "plain string", input: "https://o123.ingest.sentry.io/api/123/envelope/", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isGitHubActionsExpression(tt.input))
		})
	}
}

// TestInjectOTLPConfig_MapHeaders verifies that the map form for headers is supported.
func TestInjectOTLPConfig_MapHeaders(t *testing.T) {
	t.Run("injects OTEL_EXPORTER_OTLP_HEADERS from map form", func(t *testing.T) {
		c := &Compiler{}
		wd := &WorkflowData{
			RawFrontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": "https://traces.example.com",
						"headers": map[string]any{
							"Authorization": "Bearer ${{ secrets.TOKEN }}",
							"X-Tenant":      "acme",
						},
					},
				},
			},
		}
		c.injectOTLPConfig(wd)
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_HEADERS: Authorization=Bearer ${{ secrets.TOKEN }},X-Tenant=acme",
			"headers should be serialised as sorted key=value pairs")
	})

	t.Run("map form with single header", func(t *testing.T) {
		c := &Compiler{}
		wd := &WorkflowData{
			RawFrontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": "https://traces.example.com",
						"headers": map[string]any{
							"api-key": "${{ secrets.API_KEY }}",
						},
					},
				},
			},
		}
		c.injectOTLPConfig(wd)
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_HEADERS: api-key=${{ secrets.API_KEY }}")
	})

	t.Run("map form via ParsedFrontmatter fallback", func(t *testing.T) {
		c := &Compiler{}
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{
						Endpoint: "https://traces.example.com",
						Headers: map[string]any{
							"Authorization": "Bearer tok",
						},
					},
				},
			},
		}
		c.injectOTLPConfig(wd)
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_HEADERS: Authorization=Bearer tok",
			"map headers should work via ParsedFrontmatter fallback")
	})
}

// correctly parsed by ParseFrontmatterConfig.
func TestObservabilityConfigParsing_MapHeaders(t *testing.T) {
	t.Run("map headers parsed as any type", func(t *testing.T) {
		frontmatter := map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{
					"endpoint": "https://traces.example.com",
					"headers": map[string]any{
						"Authorization": "Bearer tok",
						"X-Tenant":      "acme",
					},
				},
			},
		}
		config, err := ParseFrontmatterConfig(frontmatter)
		require.NoError(t, err, "ParseFrontmatterConfig should not fail")
		require.NotNil(t, config.Observability)
		require.NotNil(t, config.Observability.OTLP)
		assert.Equal(t, "https://traces.example.com", config.Observability.OTLP.Endpoint)

		// The Headers field should hold the map as-is
		headersMap, ok := config.Observability.OTLP.Headers.(map[string]any)
		require.True(t, ok, "Headers should be a map[string]any when map form is used")
		assert.Equal(t, "Bearer tok", headersMap["Authorization"])
		assert.Equal(t, "acme", headersMap["X-Tenant"])
	})

	t.Run("string headers parsed as any string", func(t *testing.T) {
		frontmatter := map[string]any{
			"observability": map[string]any{
				"otlp": map[string]any{
					"endpoint": "https://traces.example.com",
					"headers":  "Authorization=Bearer tok",
				},
			},
		}
		config, err := ParseFrontmatterConfig(frontmatter)
		require.NoError(t, err, "ParseFrontmatterConfig should not fail")
		require.NotNil(t, config.Observability)
		require.NotNil(t, config.Observability.OTLP)
		headersStr, ok := config.Observability.OTLP.Headers.(string)
		require.True(t, ok, "Headers should be a string when string form is used")
		assert.Equal(t, "Authorization=Bearer tok", headersStr)
	})
}

// TestCollectAllOTLPEndpoints verifies that endpoint entries are correctly parsed from
// the polymorphic `endpoint` field (string, object, or array).
func TestCollectAllOTLPEndpoints(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		wantEntries []otlpEndpointEntry
	}{
		{
			name:        "empty frontmatter returns empty slice",
			frontmatter: map[string]any{},
			wantEntries: nil,
		},
		{
			name: "string form: single URL",
			frontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": "https://traces.example.com:4317",
					},
				},
			},
			wantEntries: []otlpEndpointEntry{
				{URL: "https://traces.example.com:4317"},
			},
		},
		{
			name: "string form: single URL with top-level headers",
			frontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": "https://traces.example.com:4317",
						"headers":  "Authorization=Bearer tok",
					},
				},
			},
			wantEntries: []otlpEndpointEntry{
				{URL: "https://traces.example.com:4317", Headers: "Authorization=Bearer tok"},
			},
		},
		{
			name: "string form: single URL with top-level headers (map form)",
			frontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": "https://traces.example.com:4317",
						"headers":  map[string]any{"Authorization": "Bearer tok"},
					},
				},
			},
			wantEntries: []otlpEndpointEntry{
				{URL: "https://traces.example.com:4317", Headers: "Authorization=Bearer tok"},
			},
		},
		{
			name: "object form: single endpoint with per-endpoint headers",
			frontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": map[string]any{
							"url":     "https://traces.example.com:4317",
							"headers": map[string]any{"X-API-Key": "key1"},
						},
					},
				},
			},
			wantEntries: []otlpEndpointEntry{
				{URL: "https://traces.example.com:4317", Headers: "X-API-Key=key1"},
			},
		},
		{
			name: "object form: single endpoint without headers",
			frontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": map[string]any{
							"url": "https://traces.example.com:4317",
						},
					},
				},
			},
			wantEntries: []otlpEndpointEntry{
				{URL: "https://traces.example.com:4317"},
			},
		},
		{
			name: "array form: multiple endpoints",
			frontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": []any{
							map[string]any{"url": "https://primary.example.com:4317"},
							map[string]any{"url": "https://secondary.example.com:4317", "headers": map[string]any{"X-API-Key": "key2"}},
						},
					},
				},
			},
			wantEntries: []otlpEndpointEntry{
				{URL: "https://primary.example.com:4317"},
				{URL: "https://secondary.example.com:4317", Headers: "X-API-Key=key2"},
			},
		},
		{
			name: "array form: sentry endpoint rewrites Authorization while grafana keeps it",
			frontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": []any{
							map[string]any{
								"url":     "${{ secrets.GH_AW_OTEL_SENTRY_ENDPOINT }}",
								"headers": map[string]any{"Authorization": "Bearer sentry-token"},
							},
							map[string]any{
								"url":     "${{ secrets.GH_AW_OTEL_GRAFANA_ENDPOINT }}",
								"headers": map[string]any{"Authorization": "Bearer grafana-token"},
							},
						},
					},
				},
			},
			wantEntries: []otlpEndpointEntry{
				{URL: "${{ secrets.GH_AW_OTEL_SENTRY_ENDPOINT }}", Headers: "x-sentry-auth=Bearer sentry-token"},
				{URL: "${{ secrets.GH_AW_OTEL_GRAFANA_ENDPOINT }}", Headers: "Authorization=Bearer grafana-token"},
			},
		},
		{
			name: "array form: entries with empty URL are skipped",
			frontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": []any{
							map[string]any{"url": ""},
							map[string]any{"url": "https://valid.example.com:4317"},
						},
					},
				},
			},
			wantEntries: []otlpEndpointEntry{
				{URL: "https://valid.example.com:4317"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectAllOTLPEndpoints(tt.frontmatter)
			assert.Equal(t, tt.wantEntries, got, "endpoint entries")
		})
	}
}

// TestEncodeOTLPEndpoints verifies JSON serialisation of endpoint entries.
func TestEncodeOTLPEndpoints(t *testing.T) {
	t.Run("empty slice returns empty string", func(t *testing.T) {
		assert.Empty(t, encodeOTLPEndpoints(nil))
		assert.Empty(t, encodeOTLPEndpoints([]otlpEndpointEntry{}))
	})

	t.Run("single entry without headers", func(t *testing.T) {
		encoded := encodeOTLPEndpoints([]otlpEndpointEntry{{URL: "https://traces.example.com:4317"}})
		assert.JSONEq(t, `[{"url":"https://traces.example.com:4317"}]`, encoded)
	})

	t.Run("single entry with headers", func(t *testing.T) {
		encoded := encodeOTLPEndpoints([]otlpEndpointEntry{{URL: "https://traces.example.com:4317", Headers: "Authorization=Bearer tok"}})
		assert.JSONEq(t, `[{"url":"https://traces.example.com:4317","headers":"Authorization=Bearer tok"}]`, encoded)
	})

	t.Run("multiple entries", func(t *testing.T) {
		encoded := encodeOTLPEndpoints([]otlpEndpointEntry{
			{URL: "https://primary.example.com:4317", Headers: "Authorization=Bearer tok1"},
			{URL: "https://secondary.example.com:4317", Headers: "Authorization=Bearer tok2"},
		})
		assert.JSONEq(t, `[{"url":"https://primary.example.com:4317","headers":"Authorization=Bearer tok1"},{"url":"https://secondary.example.com:4317","headers":"Authorization=Bearer tok2"}]`, encoded)
	})
}

// TestAllOTLPHeaders verifies that allOTLPHeaders concatenates headers from all entries.
func TestAllOTLPHeaders(t *testing.T) {
	t.Run("empty entries returns empty string", func(t *testing.T) {
		assert.Empty(t, allOTLPHeaders(nil))
	})

	t.Run("entries without headers returns empty string", func(t *testing.T) {
		entries := []otlpEndpointEntry{{URL: "https://a.example.com"}, {URL: "https://b.example.com"}}
		assert.Empty(t, allOTLPHeaders(entries))
	})

	t.Run("single entry with headers", func(t *testing.T) {
		entries := []otlpEndpointEntry{{URL: "https://a.example.com", Headers: "Authorization=Bearer tok"}}
		assert.Equal(t, "Authorization=Bearer tok", allOTLPHeaders(entries))
	})

	t.Run("multiple entries with headers are comma-joined", func(t *testing.T) {
		entries := []otlpEndpointEntry{
			{URL: "https://a.example.com", Headers: "Authorization=Bearer tok1"},
			{URL: "https://b.example.com", Headers: "X-API-Key=key2"},
		}
		assert.Equal(t, "Authorization=Bearer tok1,X-API-Key=key2", allOTLPHeaders(entries))
	})

	t.Run("entries without headers are skipped", func(t *testing.T) {
		entries := []otlpEndpointEntry{
			{URL: "https://a.example.com", Headers: "Authorization=Bearer tok1"},
			{URL: "https://b.example.com"},
			{URL: "https://c.example.com", Headers: "X-API-Key=key3"},
		}
		assert.Equal(t, "Authorization=Bearer tok1,X-API-Key=key3", allOTLPHeaders(entries))
	})
}

// TestInjectOTLPConfig_MultipleEndpoints verifies the multi-endpoint injection path.
func TestInjectOTLPConfig_MultipleEndpoints(t *testing.T) {
	c := &Compiler{}

	t.Run("injects GH_AW_OTLP_ENDPOINTS for array endpoint", func(t *testing.T) {
		wd := &WorkflowData{
			RawFrontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": []any{
							map[string]any{"url": "https://primary.example.com:4317"},
							map[string]any{"url": "https://secondary.example.com:4317"},
						},
					},
				},
			},
		}
		c.injectOTLPConfig(wd)

		require.NotEmpty(t, wd.Env, "Env should be set")
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_ENDPOINT: https://primary.example.com:4317", "first endpoint should be set as primary")
		// GH_AW_OTLP_ENDPOINTS must be single-quoted so YAML parsers treat the
		// leading '[' as a string, not a sequence node.
		assert.Contains(t, wd.Env, "GH_AW_OTLP_ENDPOINTS: '[", "multi-endpoint env var should be single-quoted")
		assert.Contains(t, wd.Env, "primary.example.com", "primary endpoint should appear in GH_AW_OTLP_ENDPOINTS")
		assert.Contains(t, wd.Env, "secondary.example.com", "secondary endpoint should appear in GH_AW_OTLP_ENDPOINTS")
	})

	t.Run("escapes single quotes in GH_AW_OTLP_ENDPOINTS", func(t *testing.T) {
		wd := &WorkflowData{
			RawFrontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": []any{
							map[string]any{
								"url":     "https://primary.example.com:4317",
								"headers": map[string]any{"Authorization": "Bearer O'Reilly"},
							},
							map[string]any{"url": "https://secondary.example.com:4317"},
						},
					},
				},
			},
		}
		c.injectOTLPConfig(wd)

		assert.Contains(t, wd.Env, "GH_AW_OTLP_ENDPOINTS:", "multi-endpoint env var should be injected")
		assert.Contains(
			t,
			wd.Env,
			"GH_AW_OTLP_ENDPOINTS: '[{\"url\":\"https://primary.example.com:4317\",\"headers\":\"Authorization=Bearer O''Reilly\"}",
			"single quotes must be escaped inside GH_AW_OTLP_ENDPOINTS YAML single-quoted scalar",
		)
	})

	t.Run("adds all static endpoint domains to firewall allowlist", func(t *testing.T) {
		wd := &WorkflowData{
			RawFrontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": []any{
							map[string]any{"url": "https://primary.example.com:4317"},
							map[string]any{"url": "https://secondary.example.com:4317"},
						},
					},
				},
			},
		}
		c.injectOTLPConfig(wd)

		require.NotNil(t, wd.NetworkPermissions, "NetworkPermissions should be created")
		assert.Contains(t, wd.NetworkPermissions.Allowed, "primary.example.com")
		assert.Contains(t, wd.NetworkPermissions.Allowed, "secondary.example.com")
	})

	t.Run("sets GH_AW_OTLP_ALL_HEADERS when multiple endpoints have headers", func(t *testing.T) {
		wd := &WorkflowData{
			RawFrontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": []any{
							map[string]any{"url": "https://primary.example.com:4317", "headers": map[string]any{"Authorization": "Bearer tok1"}},
							map[string]any{"url": "https://secondary.example.com:4317", "headers": map[string]any{"Authorization": "Bearer tok2"}},
						},
					},
				},
			},
		}
		c.injectOTLPConfig(wd)

		assert.Contains(t, wd.Env, "GH_AW_OTLP_ALL_HEADERS:", "all-headers env var should be injected for multiple endpoints")
		assert.True(t, isOTLPHeadersPresent(wd), "isOTLPHeadersPresent should detect GH_AW_OTLP_ALL_HEADERS")
	})

	t.Run("rewrites sentry auth header without changing grafana auth header", func(t *testing.T) {
		wd := &WorkflowData{
			RawFrontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": []any{
							map[string]any{
								"url":     "${{ secrets.GH_AW_OTEL_SENTRY_ENDPOINT }}",
								"headers": map[string]any{"Authorization": "${{ secrets.GH_AW_OTEL_SENTRY_AUTHORIZATION }}"},
							},
							map[string]any{
								"url":     "${{ secrets.GH_AW_OTEL_GRAFANA_ENDPOINT }}",
								"headers": map[string]any{"Authorization": "${{ secrets.GH_AW_OTEL_GRAFANA_AUTHORIZATION }}"},
							},
						},
					},
				},
			},
		}
		c.injectOTLPConfig(wd)

		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_HEADERS: x-sentry-auth=${{ secrets.GH_AW_OTEL_SENTRY_AUTHORIZATION }}", "primary Sentry endpoint should use x-sentry-auth with the configured header value")
		assert.Contains(t, wd.Env, `GH_AW_OTLP_ENDPOINTS: '[{"url":"${{ secrets.GH_AW_OTEL_SENTRY_ENDPOINT }}","headers":"x-sentry-auth=${{ secrets.GH_AW_OTEL_SENTRY_AUTHORIZATION }}"},{"url":"${{ secrets.GH_AW_OTEL_GRAFANA_ENDPOINT }}","headers":"Authorization=${{ secrets.GH_AW_OTEL_GRAFANA_AUTHORIZATION }}"}]'`, "fan-out endpoints should preserve per-vendor auth headers")
	})

	t.Run("does not set GH_AW_OTLP_ALL_HEADERS for single endpoint (string form)", func(t *testing.T) {
		wd := &WorkflowData{
			RawFrontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": "https://traces.example.com:4317",
						"headers":  map[string]any{"Authorization": "Bearer tok"},
					},
				},
			},
		}
		c.injectOTLPConfig(wd)

		assert.NotContains(t, wd.Env, "GH_AW_OTLP_ALL_HEADERS", "all-headers var should not be set for single endpoint")
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_HEADERS:", "standard headers var should still be set")
	})

	t.Run("does not set GH_AW_OTLP_ALL_HEADERS for single endpoint (object form)", func(t *testing.T) {
		wd := &WorkflowData{
			RawFrontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": map[string]any{
							"url":     "https://traces.example.com:4317",
							"headers": map[string]any{"Authorization": "Bearer tok"},
						},
					},
				},
			},
		}
		c.injectOTLPConfig(wd)

		assert.NotContains(t, wd.Env, "GH_AW_OTLP_ALL_HEADERS", "all-headers var should not be set for single endpoint")
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_HEADERS:", "standard headers var should still be set")
	})

	t.Run("OTLPEndpoints field is set to JSON-encoded array", func(t *testing.T) {
		wd := &WorkflowData{
			RawFrontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": []any{
							map[string]any{"url": "https://primary.example.com:4317"},
							map[string]any{"url": "https://secondary.example.com:4317"},
						},
					},
				},
			},
		}
		c.injectOTLPConfig(wd)

		require.NotEmpty(t, wd.OTLPEndpoints, "OTLPEndpoints field should be set")
		assert.Contains(t, wd.OTLPEndpoints, "primary.example.com")
		assert.Contains(t, wd.OTLPEndpoints, "secondary.example.com")
	})

	t.Run("expression-only endpoints do not add to firewall allowlist", func(t *testing.T) {
		wd := &WorkflowData{
			RawFrontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": []any{
							map[string]any{"url": "${{ secrets.OTLP_ENDPOINT1 }}"},
							map[string]any{"url": "${{ secrets.OTLP_ENDPOINT2 }}"},
						},
					},
				},
			},
		}
		c.injectOTLPConfig(wd)

		assert.Nil(t, wd.NetworkPermissions, "expression endpoints should not add to firewall (NetworkPermissions should be nil)")
	})

	t.Run("object form: injects single endpoint with per-endpoint headers", func(t *testing.T) {
		wd := &WorkflowData{
			RawFrontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": map[string]any{
							"url":     "https://traces.example.com:4317",
							"headers": map[string]any{"Authorization": "Bearer tok"},
						},
					},
				},
			},
		}
		c.injectOTLPConfig(wd)

		require.NotEmpty(t, wd.Env)
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_ENDPOINT: https://traces.example.com:4317")
		assert.Contains(t, wd.Env, "OTEL_EXPORTER_OTLP_HEADERS: Authorization=Bearer tok")
		assert.Contains(t, wd.Env, "GH_AW_OTLP_ENDPOINTS:")
		require.NotNil(t, wd.NetworkPermissions)
		assert.Contains(t, wd.NetworkPermissions.Allowed, "traces.example.com")
	})
}

// TestIsOTLPHeadersPresent_AllHeaders verifies that isOTLPHeadersPresent detects
// GH_AW_OTLP_ALL_HEADERS in addition to OTEL_EXPORTER_OTLP_HEADERS.
func TestIsOTLPHeadersPresent_AllHeaders(t *testing.T) {
	t.Run("detects GH_AW_OTLP_ALL_HEADERS", func(t *testing.T) {
		wd := &WorkflowData{
			Env: "env:\n  OTEL_EXPORTER_OTLP_ENDPOINT: https://traces.example.com\n  GH_AW_OTLP_ALL_HEADERS: Authorization=Bearer tok1,Authorization=Bearer tok2",
		}
		assert.True(t, isOTLPHeadersPresent(wd), "should detect GH_AW_OTLP_ALL_HEADERS")
	})
}

// TestExtractRawOTLPEndpointMaps verifies that all three endpoint forms (string, object, array)
// are extracted as raw maps with original header format preserved.
func TestExtractRawOTLPEndpointMaps(t *testing.T) {
	tests := []struct {
		name string
		obs  map[string]any
		want []map[string]any
	}{
		{
			name: "nil map returns nil",
			obs:  nil,
			want: nil,
		},
		{
			name: "empty map returns nil",
			obs:  map[string]any{},
			want: nil,
		},
		{
			name: "missing otlp key returns nil",
			obs:  map[string]any{"other": "value"},
			want: nil,
		},
		{
			name: "string form without headers",
			obs: map[string]any{
				"otlp": map[string]any{
					"endpoint": "https://traces.example.com:4317",
				},
			},
			want: []map[string]any{
				{"url": "https://traces.example.com:4317"},
			},
		},
		{
			name: "string form with top-level headers preserved as map",
			obs: map[string]any{
				"otlp": map[string]any{
					"endpoint": "https://traces.example.com:4317",
					"headers":  map[string]any{"Authorization": "Bearer tok"},
				},
			},
			want: []map[string]any{
				{"url": "https://traces.example.com:4317", "headers": map[string]any{"Authorization": "Bearer tok"}},
			},
		},
		{
			name: "object form with headers",
			obs: map[string]any{
				"otlp": map[string]any{
					"endpoint": map[string]any{
						"url":     "https://traces.example.com:4317",
						"headers": map[string]any{"X-API-Key": "key1"},
					},
				},
			},
			want: []map[string]any{
				{"url": "https://traces.example.com:4317", "headers": map[string]any{"X-API-Key": "key1"}},
			},
		},
		{
			name: "array form with multiple endpoints",
			obs: map[string]any{
				"otlp": map[string]any{
					"endpoint": []any{
						map[string]any{"url": "https://primary.example.com:4317"},
						map[string]any{"url": "https://secondary.example.com:4317", "headers": map[string]any{"X-Key": "v"}},
					},
				},
			},
			want: []map[string]any{
				{"url": "https://primary.example.com:4317"},
				{"url": "https://secondary.example.com:4317", "headers": map[string]any{"X-Key": "v"}},
			},
		},
		{
			name: "array form skips entries with empty URL",
			obs: map[string]any{
				"otlp": map[string]any{
					"endpoint": []any{
						map[string]any{"url": ""},
						map[string]any{"url": "https://valid.example.com:4317"},
					},
				},
			},
			want: []map[string]any{
				{"url": "https://valid.example.com:4317"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRawOTLPEndpointMaps(tt.obs)
			assert.Equal(t, tt.want, got, "extractRawOTLPEndpointMaps")
		})
	}
}

func TestExtractRawOTLPGitHubAppMap(t *testing.T) {
	t.Run("returns shallow copy when github-app exists", func(t *testing.T) {
		obs := map[string]any{
			"otlp": map[string]any{
				"github-app": map[string]any{
					"audience": "api://AzureADTokenExchange",
				},
			},
		}

		got := extractRawOTLPGitHubAppMap(obs)
		require.NotNil(t, got)
		assert.Equal(t, "api://AzureADTokenExchange", got["audience"])

		got["audience"] = "changed"
		original := obs["otlp"].(map[string]any)["github-app"].(map[string]any)["audience"]
		assert.Equal(t, "api://AzureADTokenExchange", original)
	})

	t.Run("returns nil for invalid values", func(t *testing.T) {
		assert.Nil(t, extractRawOTLPGitHubAppMap(nil))
		assert.Nil(t, extractRawOTLPGitHubAppMap(map[string]any{}))
		assert.Nil(t, extractRawOTLPGitHubAppMap(map[string]any{
			"otlp": map[string]any{
				"github-app": "invalid",
			},
		}))
	})
}

// TestCollectOTLPCustomAttributes verifies that custom attributes are read from the
// frontmatter and returned as a map[string]string.
func TestCollectOTLPCustomAttributes(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		want        map[string]string
	}{
		{
			name:        "nil frontmatter returns nil",
			frontmatter: nil,
			want:        nil,
		},
		{
			name:        "no observability key returns nil",
			frontmatter: map[string]any{"name": "my-workflow"},
			want:        nil,
		},
		{
			name: "no otlp key in observability returns nil",
			frontmatter: map[string]any{
				"observability": map[string]any{},
			},
			want: nil,
		},
		{
			name: "no attributes key in otlp returns nil",
			frontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": "https://traces.example.com",
					},
				},
			},
			want: nil,
		},
		{
			name: "empty attributes map returns nil",
			frontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"attributes": map[string]any{},
					},
				},
			},
			want: nil,
		},
		{
			name: "string attributes are collected",
			frontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": "https://traces.example.com",
						"attributes": map[string]any{
							"langfuse.session.id": "{{ gh-aw.episode.id }}",
							"langfuse.user.id":    "{{ github.actor }}",
						},
					},
				},
			},
			want: map[string]string{
				"langfuse.session.id": "{{ gh-aw.episode.id }}",
				"langfuse.user.id":    "{{ github.actor }}",
			},
		},
		{
			name: "non-string values are silently ignored",
			frontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"attributes": map[string]any{
							"valid.key":  "valid-value",
							"number.key": 42,
							"bool.key":   true,
						},
					},
				},
			},
			want: map[string]string{
				"valid.key": "valid-value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectOTLPCustomAttributes(tt.frontmatter)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCollectOTLPResourceAttributes(t *testing.T) {
	frontmatter := map[string]any{
		"observability": map[string]any{
			"otlp": map[string]any{
				"resource-attributes": map[string]any{
					"my.actor":        "${{ github.actor }}",
					"my.target-repo":  "owner/repo",
					"ignored.numeric": 42,
				},
			},
		},
	}

	assert.Equal(t, map[string]string{
		"my.actor":       "${{ github.actor }}",
		"my.target-repo": "owner/repo",
	}, collectOTLPResourceAttributes(frontmatter))
}

func TestValidateOTLPResourceAttributes(t *testing.T) {
	tests := []struct {
		name          string
		workflowData  *WorkflowData
		errorContains string
	}{
		{
			name: "allows safe expressions",
			workflowData: &WorkflowData{
				ParsedFrontmatter: &FrontmatterConfig{
					Observability: &ObservabilityConfig{
						OTLP: &OTLPConfig{
							ResourceAttributes: map[string]string{
								"my.actor": "${{ github.actor }}",
							},
						},
					},
				},
			},
		},
		{
			name: "rejects secrets expressions",
			workflowData: &WorkflowData{
				RawFrontmatter: map[string]any{
					"observability": map[string]any{
						"otlp": map[string]any{
							"resource-attributes": map[string]any{
								"api.key": "${{ secrets.OTLP_KEY }}",
							},
						},
					},
				},
			},
			errorContains: "observability.otlp.resource-attributes.api.key must not reference secrets.* or vars.*",
		},
		{
			name: "rejects vars expressions",
			workflowData: &WorkflowData{
				ParsedFrontmatter: &FrontmatterConfig{
					Observability: &ObservabilityConfig{
						OTLP: &OTLPConfig{
							ResourceAttributes: map[string]string{
								"tenant": "${{ vars.OTLP_TENANT }}",
							},
						},
					},
				},
			},
			errorContains: "observability.otlp.resource-attributes.tenant must not reference secrets.* or vars.*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOTLPResourceAttributes(tt.workflowData)
			if tt.errorContains == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.ErrorContains(t, err, tt.errorContains)
		})
	}
}

// TestInjectOTLPConfig_CustomAttributes verifies that injectOTLPConfig injects the
// GH_AW_OTLP_ATTRIBUTES env var when observability.otlp.attributes is configured.
func TestInjectOTLPConfig_CustomAttributes(t *testing.T) {
	c := &Compiler{}

	t.Run("injects GH_AW_OTLP_ATTRIBUTES when attributes are configured via ParsedFrontmatter", func(t *testing.T) {
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{
						Endpoint: "https://traces.example.com",
						Attributes: map[string]string{
							"langfuse.session.id": "{{ gh-aw.episode.id }}",
							"langfuse.user.id":    "{{ github.actor }}",
						},
					},
				},
			},
		}
		c.injectOTLPConfig(wd)
		assert.Contains(t, wd.Env, "GH_AW_OTLP_ATTRIBUTES", "should inject GH_AW_OTLP_ATTRIBUTES env var")
		assert.Contains(t, wd.Env, "langfuse.session.id", "should include the attribute key")
		assert.Contains(t, wd.Env, "gh-aw.episode.id", "should include the template value")
	})

	t.Run("injects GH_AW_OTLP_ATTRIBUTES when attributes are configured via RawFrontmatter", func(t *testing.T) {
		wd := &WorkflowData{
			RawFrontmatter: map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{
						"endpoint": "https://traces.example.com",
						"attributes": map[string]any{
							"session.id": "{{ gh-aw.episode.id }}",
							"user.id":    "{{ github.actor }}",
						},
					},
				},
			},
			ParsedFrontmatter: &FrontmatterConfig{},
		}
		c.injectOTLPConfig(wd)
		assert.Contains(t, wd.Env, "GH_AW_OTLP_ATTRIBUTES", "should inject GH_AW_OTLP_ATTRIBUTES env var")
		assert.Contains(t, wd.Env, "session.id", "should include the attribute key")
	})

	t.Run("does not inject GH_AW_OTLP_ATTRIBUTES when no attributes are configured", func(t *testing.T) {
		wd := &WorkflowData{
			ParsedFrontmatter: &FrontmatterConfig{
				Observability: &ObservabilityConfig{
					OTLP: &OTLPConfig{
						Endpoint: "https://traces.example.com",
					},
				},
			},
		}
		c.injectOTLPConfig(wd)
		assert.NotContains(t, wd.Env, "GH_AW_OTLP_ATTRIBUTES", "should not inject GH_AW_OTLP_ATTRIBUTES when no attributes are set")
	})
}

// TestMergeOTLPStringMaps verifies that mergeOTLPStringMaps correctly
// merges two attribute maps with base taking precedence.
func TestMergeOTLPStringMaps(t *testing.T) {
	t.Run("nil inputs return nil", func(t *testing.T) {
		assert.Nil(t, mergeOTLPStringMaps(nil, nil))
	})

	t.Run("base only is returned as-is", func(t *testing.T) {
		base := map[string]string{"a": "1"}
		result := mergeOTLPStringMaps(base, nil)
		assert.Equal(t, map[string]string{"a": "1"}, result)
	})

	t.Run("override only is returned as-is", func(t *testing.T) {
		override := map[string]string{"b": "2"}
		result := mergeOTLPStringMaps(nil, override)
		assert.Equal(t, map[string]string{"b": "2"}, result)
	})

	t.Run("base keys override the same key from override", func(t *testing.T) {
		base := map[string]string{"a": "base-value", "b": "base-b"}
		override := map[string]string{"a": "override-value", "c": "override-c"}
		result := mergeOTLPStringMaps(base, override)
		require.NotNil(t, result)
		assert.Equal(t, "base-value", result["a"], "base should win for key 'a'")
		assert.Equal(t, "base-b", result["b"], "base-only key 'b' should be present")
		assert.Equal(t, "override-c", result["c"], "override-only key 'c' should be present")
	})
}

// TestEncodeOTLPCustomAttributes verifies serialisation to JSON.
func TestEncodeOTLPCustomAttributes(t *testing.T) {
	t.Run("nil returns empty string", func(t *testing.T) {
		assert.Empty(t, encodeOTLPCustomAttributes(nil))
	})

	t.Run("empty map returns empty string", func(t *testing.T) {
		assert.Empty(t, encodeOTLPCustomAttributes(map[string]string{}))
	})

	t.Run("non-empty map is valid JSON", func(t *testing.T) {
		encoded := encodeOTLPCustomAttributes(map[string]string{
			"langfuse.session.id": "{{ gh-aw.episode.id }}",
		})
		assert.NotEmpty(t, encoded)
		assert.Contains(t, encoded, "langfuse.session.id")
		assert.True(t, strings.HasPrefix(encoded, "{"), "should be a JSON object")
	})
}

// TestValidateOTLPWorkloadIdentity verifies the workload identity configuration validation.
func TestValidateOTLPWorkloadIdentity(t *testing.T) {
	newData := func(workloadIdentity map[string]any, extra map[string]any) *WorkflowData {
		otlp := map[string]any{}
		if workloadIdentity != nil {
			otlp["workload-identity"] = workloadIdentity
		}
		maps.Copy(otlp, extra)
		return &WorkflowData{
			RawFrontmatter: map[string]any{
				"observability": map[string]any{"otlp": otlp},
			},
		}
	}

	t.Run("no workload identity is valid", func(t *testing.T) {
		assert.NoError(t, validateOTLPWorkloadIdentity(newData(nil, nil)))
	})

	t.Run("valid configuration", func(t *testing.T) {
		assert.NoError(t, validateOTLPWorkloadIdentity(newData(map[string]any{
			"provider": "google",
			"audience": "projects/123/locations/global/workloadIdentityPools/pool/providers/github",
		}, nil)))
	})

	t.Run("unsupported provider is rejected", func(t *testing.T) {
		err := validateOTLPWorkloadIdentity(newData(map[string]any{
			"provider": "aws",
			"audience": "some-audience",
		}, nil))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "provider must be google")
	})

	t.Run("missing audience is rejected", func(t *testing.T) {
		err := validateOTLPWorkloadIdentity(newData(map[string]any{
			"provider": "google",
		}, nil))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "audience is required")
	})

	t.Run("combining with github app credentials is rejected", func(t *testing.T) {
		err := validateOTLPWorkloadIdentity(newData(map[string]any{
			"provider": "google",
			"audience": "projects/123/locations/global/workloadIdentityPools/pool/providers/github",
		}, map[string]any{
			"github-app": map[string]any{
				"app-id":      "123",
				"private-key": "${{ secrets.APP_KEY }}",
			},
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be combined")
	})
}
