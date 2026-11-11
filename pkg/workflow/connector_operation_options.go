package workflow

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/sliceutil"
)

const (
	// Match the conservative native Linear default, not arbitrary custom scripts:
	// one attempted proposal per selected operation unless the workflow opts in.
	connectorDefaultMax = 1
	connectorMaximumMax = 100
)

// ConnectorOperationOptions holds workflow-owned delivery controls and replay data.
type ConnectorOperationOptions struct {
	Max     int              `json:"max"`
	Staged  *TemplatableBool `json:"staged,omitempty"`
	Samples []map[string]any `json:"samples,omitempty"`
}

func parseConnectorOperationOptions(value any) (ConnectorOperationOptions, error) {
	options := ConnectorOperationOptions{Max: connectorDefaultMax}
	if value == nil {
		return options, nil
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return options, errors.New("connector operation options must be null or an object containing max, staged and samples")
	}
	for _, name := range sliceutil.SortedKeys(fields) {
		switch name {
		case "max":
			// Accept integral numeric scalars without coercing strings or truncating.
			data, err := json.Marshal(fields[name])
			if err != nil {
				return options, fmt.Errorf("max must be an integer from 1 to %d", connectorMaximumMax)
			}
			var numeric float64
			if err := json.Unmarshal(data, &numeric); err != nil || numeric < 1 || numeric > connectorMaximumMax || numeric != float64(int(numeric)) {
				return options, fmt.Errorf("max must be an integer from 1 to %d", connectorMaximumMax)
			}
			options.Max = int(numeric)
		case "staged":
			data, err := json.Marshal(fields[name])
			if err != nil || fields[name] == nil {
				return options, errors.New("staged must be a boolean or GitHub Actions expression")
			}
			var staged TemplatableBool
			if err := json.Unmarshal(data, &staged); err != nil {
				return options, fmt.Errorf("staged must be a boolean or GitHub Actions expression: %w", err)
			}
			options.Staged = &staged
		case "samples":
			samples := fields[name]
			switch value := samples.(type) {
			case map[string]any:
			case []any:
				for _, item := range value {
					if _, ok := item.(map[string]any); !ok {
						return options, errors.New("samples must be an object or an array of objects")
					}
				}
			default:
				return options, errors.New("samples must be an object or an array of objects")
			}
			options.Samples = parseSamplesValue(samples)
		default:
			return options, fmt.Errorf("unknown connector operation option %q; allowed options are max, staged and samples", name)
		}
	}
	return options, nil
}

func (config *WorkflowConnector) operationOptions(operation string) ConnectorOperationOptions {
	if options, ok := config.SafeOutputOptions[operation]; ok {
		return options
	}
	return ConnectorOperationOptions{Max: connectorDefaultMax}
}

// Read the already-frozen snapshot, not mutable declarations or agent arguments.
func connectorOperationSecrets(plan *connectors.Plan) (map[string][]string, error) {
	encoded, err := plan.Encode()
	if err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	var snapshot struct {
		Spec       connectors.ConnectorSpec `json:"spec"`
		Connection connectors.Connection    `json:"connection"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	result := map[string][]string{}
	for _, name := range plan.SafeOutputNames() {
		op := snapshot.Spec.SafeOutputs[name]
		secrets := []string{snapshot.Connection.Credentials[op.Credential].Secret}
		for _, source := range op.TrustedInputs {
			if source.Secret != "" {
				secrets = append(secrets, source.Secret)
			}
		}
		slices.Sort(secrets)
		result[name] = slices.Compact(secrets)
	}
	return result, nil
}

// An empty condition means always live; false means no selected live consumer.
// Expressions compare their string value exactly as isStagedMode does.
func connectorCredentialLiveCondition(config *WorkflowConnector, secret string, global *TemplatableBool, forced bool) (string, bool, error) {
	if forced || templatableBoolIsTrue(global) {
		return "", false, nil
	}
	operations, err := connectorOperationSecrets(config.SafeOutputs)
	if err != nil {
		return "", false, err
	}
	var terms []string
	alwaysLive := false
	for _, operation := range config.SafeOutputs.SafeOutputNames() {
		if !slices.Contains(operations[operation], secret) {
			continue
		}
		staged := config.operationOptions(operation).Staged
		if templatableBoolIsTrue(staged) {
			continue
		}
		if staged == nil || !isExpression(staged.String()) {
			alwaysLive = true
			break
		}
		terms = append(terms, "format('{0}', "+stripExpressionWrapper(staged.String())+") != 'true'")
	}
	if !alwaysLive && len(terms) == 0 {
		return "", false, nil
	}
	condition := ""
	if !alwaysLive {
		slices.Sort(terms)
		terms = slices.Compact(terms)
		condition = "(" + strings.Join(terms, " || ") + ")"
	}
	if global != nil && isExpression(global.String()) {
		globalCondition := "format('{0}', " + stripExpressionWrapper(global.String()) + ") != 'true'"
		if condition == "" {
			condition = globalCondition
		} else {
			condition = globalCondition + " && " + condition
		}
	}
	return condition, true, nil
}

func connectorStagedValue(value any) *TemplatableBool {
	data, err := json.Marshal(value)
	if err != nil || value == nil {
		return nil
	}
	var staged TemplatableBool
	if err := json.Unmarshal(data, &staged); err != nil {
		return nil
	}
	return &staged
}

func connectorCredentialMintCondition(c *Compiler, data *WorkflowData, config *WorkflowConnector, secret, phase string) (string, string, bool) {
	if phase != "safe-output" {
		return "", "", true
	}
	live, enabled, err := connectorCredentialLiveCondition(config, secret, data.SafeOutputs.Staged, c.dryRun || c.trialMode || c.forceStaged)
	if err != nil || !enabled {
		// Expansion has already validated this same frozen snapshot; fail closed
		// if it nevertheless cannot be read while generating mint steps.
		return "", "", false
	}
	if live == "" {
		return "", "", true
	}
	return "        if: ${{ env.GH_AW_CONNECTOR_STAGED != 'true' }}\n", "${{ !(" + live + ") }}", true
}
