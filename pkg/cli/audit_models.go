package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

// AuditModelCatalog contains only model metadata, never endpoint URLs or credentials.
type AuditModelCatalog struct {
	Source              string                      `json:"source"`
	ModelsFetchComplete bool                        `json:"models_fetch_complete"`
	Endpoints           []AuditModelCatalogEndpoint `json:"endpoints"`
	Warnings            []string                    `json:"warnings,omitempty"`
}

type AuditModelCatalogEndpoint struct {
	Provider   string              `json:"provider"`
	Configured bool                `json:"configured"`
	Models     []AuditCatalogModel `json:"models"`
	Warning    string              `json:"warning,omitempty"`
}

type AuditCatalogModel struct {
	ID                 string   `json:"id"`
	SupportedEndpoints []string `json:"supported_endpoints,omitempty"`
}

func buildAuditModelCatalog(runDir string) *AuditModelCatalog {
	report := &AuditModelCatalog{
		Source:    "sandbox/firewall/awf-reflect.json",
		Endpoints: []AuditModelCatalogEndpoint{},
	}
	data, err := readModelAvailabilityReflection(runDir)
	if err != nil {
		report.Warnings = []string{"Same-run model catalog is unavailable or unsafe; model absence cannot be confirmed."}
		return report
	}
	var inventory auditModelInventory
	if err := json.Unmarshal(data, &inventory); err != nil {
		report.Warnings = []string{"Same-run model catalog is invalid; model absence cannot be confirmed."}
		return report
	}
	report.ModelsFetchComplete = inventory.ModelsFetchComplete
	if !inventory.ModelsFetchComplete {
		report.Warnings = append(report.Warnings, "Model discovery did not complete; listed models may be partial and absence cannot be confirmed.")
	}
	if len(inventory.Endpoints) == 0 {
		report.Warnings = append(report.Warnings, "No provider endpoints were recorded; model availability is unknown.")
	}
	for _, endpoint := range inventory.Endpoints {
		if !modelResolutionProviderPattern.MatchString(endpoint.Provider) {
			report.Warnings = append(report.Warnings, "An endpoint has an invalid provider identifier and was omitted.")
			continue
		}
		entry := AuditModelCatalogEndpoint{
			Provider: endpoint.Provider, Configured: endpoint.Configured, Models: []AuditCatalogModel{},
		}
		if endpoint.Models == nil {
			entry.Warning = "No model inventory was recorded for this endpoint."
		}
		for _, raw := range endpoint.Models {
			id, err := reflectedModelID(raw)
			if err != nil {
				entry.Warning = "Invalid model entries were omitted; model absence cannot be confirmed."
				continue
			}
			model := AuditCatalogModel{ID: id}
			for _, route := range endpoint.RoutingModels {
				if !strings.EqualFold(route.ModelID, id) {
					continue
				}
				for _, supported := range route.SupportedEndpoints {
					if supported == "/v1/messages" || supported == "/chat/completions" || supported == "/responses" {
						model.SupportedEndpoints = append(model.SupportedEndpoints, supported)
					}
				}
			}
			slices.Sort(model.SupportedEndpoints)
			model.SupportedEndpoints = slices.Compact(model.SupportedEndpoints)
			entry.Models = append(entry.Models, model)
		}
		report.Endpoints = append(report.Endpoints, entry)
	}
	return report
}

func renderConsoleModelCatalog(catalog *AuditModelCatalog, engine *AuditEngineConfig) {
	if catalog == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "  model_catalog: source=%s models_fetch_complete=%t\n", catalog.Source, catalog.ModelsFetchComplete)
	if engine != nil {
		fmt.Fprintf(os.Stderr, "    requested_model=%s effective_model=%s effort=%s routing_status=%s\n",
			safeModelRoutingText(engine.RequestedModel), safeModelRoutingText(engine.Model),
			safeModelRoutingText(engine.ModelEffort), safeModelRoutingText(engine.ModelRoutingStatus))
	}
	for _, warning := range catalog.Warnings {
		fmt.Fprintln(os.Stderr, "    warning: "+safeModelRoutingText(warning))
	}
	for _, endpoint := range catalog.Endpoints {
		fmt.Fprintf(os.Stderr, "    provider=%s configured=%t models=%d\n",
			safeModelRoutingText(endpoint.Provider), endpoint.Configured, len(endpoint.Models))
		if endpoint.Warning != "" {
			fmt.Fprintln(os.Stderr, "      warning: "+safeModelRoutingText(endpoint.Warning))
		}
		for _, model := range endpoint.Models {
			fmt.Fprintf(os.Stderr, "      model=%s supported_endpoints=%s\n",
				safeModelRoutingText(model.ID), safeModelRoutingText(strings.Join(model.SupportedEndpoints, ",")))
		}
	}
}
