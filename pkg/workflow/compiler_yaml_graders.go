package workflow

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

const graderPayloadChunkSize = 2 * 1024

type graderManifestEntry struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Source      string         `json:"source"`
	Enabled     bool           `json:"enabled"`
	Unit        string         `json:"unit,omitempty"`
	Direction   string         `json:"direction,omitempty"`
	Threshold   *float64       `json:"threshold,omitempty"`
	Max         *float64       `json:"max,omitempty"`
	Min         *float64       `json:"min,omitempty"`
	Config      map[string]any `json:"config,omitempty"`
	Script      string         `json:"script,omitempty"`
}

type graderManifest struct {
	Version       int                   `json:"version"`
	Graders       []graderManifestEntry `json:"graders"`
	CustomGraders []string              `json:"customGraders,omitempty"`
}

func buildGraderManifest(cfg *GradersConfig, custom bool) *graderManifest {
	manifest := &graderManifest{Version: 2, Graders: []graderManifestEntry{}}
	if cfg == nil {
		return manifest
	}
	if !custom {
		manifest.CustomGraders = cfg.CustomGraderIDs()
	}
	ids := make([]string, 0, len(cfg.Graders))
	for id := range cfg.Graders {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		g := cfg.Graders[id]
		if (g.Script != "") != custom {
			continue
		}
		if custom && g.Enabled != nil && !*g.Enabled {
			continue
		}
		meta := builtinGraderMetaByID[id]
		if slices.Contains(DefaultGraderIDs, id) && meta != nil &&
			(g.Enabled == nil || *g.Enabled) && g.Name == meta.Name &&
			g.Description == meta.Description && g.Unit == meta.Unit && g.Direction == meta.Direction &&
			equalGraderNumber(g.Threshold, meta.Threshold) && equalGraderNumber(g.Min, meta.Min) &&
			equalGraderNumber(g.Max, meta.Max) && len(g.Config) == 0 {
			continue
		}
		source := "builtin"
		if custom {
			source = "inline"
			if id == "operational-value" {
				source = "operational-value"
			}
		}
		manifest.Graders = append(manifest.Graders, graderManifestEntry{
			ID: id, Name: g.Name, Description: g.Description, Source: source, Script: g.Script,
			Enabled: g.Enabled == nil || *g.Enabled, Unit: g.Unit, Direction: g.Direction,
			Threshold: g.Threshold, Max: g.Max, Min: g.Min, Config: g.Config,
		})
	}
	return manifest
}

func equalGraderNumber(a, b *float64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func escapeGraderJSON(payload []byte) string {
	return strings.ReplaceAll(string(payload), "$", `\u0024`)
}

func writeGraderEnvironment(yaml *strings.Builder, variable string, payload []byte) {
	runes := []rune(escapeGraderJSON(payload))
	index := 0
	for len(runes) > 0 {
		chunkSize := min(len(runes), graderPayloadChunkSize)
		chunk := strings.ReplaceAll(string(runes[:chunkSize]), "'", "''")
		yaml.WriteString("          " + variable + "_" + strconv.Itoa(index) + ": '" + chunk + "'\n")
		runes = runes[chunkSize:]
		index++
	}
	yaml.WriteString("          " + variable + "_CHUNKS: '" + strconv.Itoa(index) + "'\n")
}

// Grader options travel as data to conclusion's session collection step.
func graderEnvironment(cfg *GradersConfig) (string, error) {
	return graderEnvironmentForJob(cfg, false)
}

func graderEnvironmentForJob(cfg *GradersConfig, custom bool) (string, error) {
	payload, err := json.Marshal(buildGraderManifest(cfg, custom))
	if err != nil {
		return "", err
	}
	var yaml strings.Builder
	prefix := "GH_AW_GRADER_MANIFEST"
	if custom {
		prefix = "GH_AW_CUSTOM_GRADER_MANIFEST"
	}
	writeGraderEnvironment(&yaml, prefix, payload)
	return yaml.String(), nil
}
