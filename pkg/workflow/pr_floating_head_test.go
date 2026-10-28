//go:build !integration

package workflow

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckoutAllowFloatingHead(t *testing.T) {
	for _, value := range []bool{false, true} {
		t.Run(strconv.FormatBool(value), func(t *testing.T) {
			configs, err := ParseCheckoutConfigs(map[string]any{"allow-floating-head": value})
			require.NoError(t, err)
			require.Len(t, configs, 1)
			assert.Equal(t, value, configs[0].AllowFloatingHead)
		})
	}
	for _, value := range []any{"true", "${{ inputs.floating }}", 1, nil} {
		_, err := ParseCheckoutConfigs(map[string]any{"allow-floating-head": value})
		require.ErrorContains(t, err, "checkout.allow-floating-head must be a literal boolean")
	}
}

func TestPRCheckoutFloatingHeadEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name    string
		configs []*CheckoutConfig
		enabled bool
	}{
		{"default", nil, false},
		{"disabled", []*CheckoutConfig{{AllowFloatingHead: false}}, false},
		{"enabled", []*CheckoutConfig{{AllowFloatingHead: true}}, true},
		{"list", []*CheckoutConfig{nil, {}, {AllowFloatingHead: true}, {AllowFloatingHead: true}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, useRequire := range []bool{false, true} {
				var output strings.Builder
				writePRReadyForReviewCheckoutAction(&output, &WorkflowData{CheckoutConfigs: tc.configs}, useRequire)
				assert.Equal(t, tc.enabled, strings.Contains(output.String(), `GH_AW_ALLOW_FLOATING_HEAD: "true"`))
				assert.LessOrEqual(t, strings.Count(output.String(), "GH_AW_ALLOW_FLOATING_HEAD"), 1)
			}
		})
	}
}

func TestFloatingHeadDoesNotEnableDisabledPRCheckout(t *testing.T) {
	disabled := false
	data := &WorkflowData{
		Permissions:     "contents: read",
		CheckoutConfigs: []*CheckoutConfig{{AllowFloatingHead: true, PullRequest: &disabled}},
	}
	assert.False(t, ShouldGeneratePRCheckoutStep(data))
	data.CheckoutConfigs[0].PullRequest = nil
	data.IsPullRequestTarget = true
	assert.False(t, ShouldGeneratePRCheckoutStep(data))
}
