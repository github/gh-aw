# agentdrain Package

> Drain-style log template mining and anomaly scoring for structured agent pipeline events.

## Overview

The `agentdrain` package implements an online log-template miner inspired by the Drain algorithm and adapts it to `AgentEvent` records emitted by agentic workflow stages. It converts structured events into deterministic token streams, normalizes variable values with regex-based masking, groups similar events into clusters, and returns `MatchResult` values describing the matched template, extracted parameters, and similarity score.

The package supports two related workflows: training on known-good events and analyzing new events for anomalies. `Miner` manages a single stream of events, while `Coordinator` manages one `Miner` per stage so templates from `plan`, `tool_call`, `finish`, and other stages do not interfere with each other. Miner state can be serialized with `Snapshot`/`SnapshotCluster`, and coordinators can bootstrap from embedded default weights via `LoadDefaultWeights`.

The public API is intentionally small: event flattening and tokenization helpers, configurable masking, a concurrent miner, stage-aware coordination, and anomaly scoring. Internally, the package uses a parse tree and cluster store, but those remain unexported implementation details.

## Public API

### Types

| Type | Kind | Description |
|------|------|-------------|
| `AgentEvent` | struct | Structured event with a `Stage` and key/value `Fields` used as miner input. |
| `AnomalyDetector` | struct | Evaluates `MatchResult` values and produces `AnomalyReport` values using similarity and rarity thresholds. |
| `AnomalyReport` | struct | Describes anomaly flags, normalized score, and human-readable reason text. |
| `Cluster` | struct | Represents a mined template cluster with ID, tokenized template, size, and optional stage. |
| `Config` | struct | Configures parse-tree depth, similarity threshold, wildcard token, masking rules, rarity threshold, and excluded fields. |
| `Coordinator` | struct | Owns one `Miner` per stage and provides stage-aware training, analysis, and persistence. |
| `MaskRule` | struct | Regex substitution rule applied before tokenization. |
| `Masker` | struct | Compiled ordered set of `MaskRule` values that normalizes log lines. |
| `MatchResult` | struct | Reports the cluster ID, rendered template, extracted params, similarity, and stage for a processed event. |
| `Miner` | struct | Concurrent Drain-style miner for one event stream. |
| `Snapshot` | struct | Serializable representation of a miner's config, clusters, and next cluster ID. |
| `SnapshotCluster` | struct | Serializable representation of one cluster inside a `Snapshot`. |

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `(*AnomalyDetector).Analyze` | `func (d *AnomalyDetector) Analyze(result *MatchResult, isNew bool, cluster *Cluster) *AnomalyReport` | Scores a match result and cluster context, producing anomaly flags, a normalized score, and reason text. |
| `(*Coordinator).AllClusters` | `func (c *Coordinator) AllClusters() map[string][]Cluster` | Returns a snapshot of clusters for every registered stage. |
| `(*Coordinator).AnalyzeEvent` | `func (c *Coordinator) AnalyzeEvent(evt AgentEvent) (*MatchResult, *AnomalyReport, error)` | Routes an event to its stage miner and returns both the match result and anomaly report. |
| `(*Coordinator).LoadDefaultWeights` | `func (c *Coordinator) LoadDefaultWeights() error` | Loads embedded default weights from `data/default_weights.json` unless the embedded file is empty or `{}`. |
| `(*Coordinator).LoadSnapshots` | `func (c *Coordinator) LoadSnapshots(snapshots map[string][]byte) error` | Restores per-stage miner snapshots, creating new stage miners when snapshots reference previously unknown stages. |
| `(*Coordinator).LoadWeightsJSON` | `func (c *Coordinator) LoadWeightsJSON(data []byte) error` | Restores all stage miners from a combined JSON document produced by `SaveWeightsJSON`. |
| `(*Coordinator).SaveSnapshots` | `func (c *Coordinator) SaveSnapshots() (map[string][]byte, error)` | Serializes each stage miner independently as JSON. |
| `(*Coordinator).SaveWeightsJSON` | `func (c *Coordinator) SaveWeightsJSON() ([]byte, error)` | Serializes all stage snapshots into one combined JSON blob suitable for embedding as default weights. |
| `(*Coordinator).TrainEvent` | `func (c *Coordinator) TrainEvent(evt AgentEvent) (*MatchResult, error)` | Routes an event to the miner for `evt.Stage` and updates that miner. |
| `(*Masker).Mask` | `func (m *Masker) Mask(line string) string` | Applies all configured masking rules in order. |
| `(*Miner).AnalyzeEvent` | `func (m *Miner) AnalyzeEvent(evt AgentEvent) (*MatchResult, *AnomalyReport, error)` | Performs inference, trains on the event, and returns both the resulting match and anomaly report. |
| `(*Miner).Clusters` | `func (m *Miner) Clusters() []Cluster` | Returns a snapshot of all known clusters. |
| `(*Miner).LoadJSON` | `func (m *Miner) LoadJSON(data []byte) error` | Replaces miner state from a JSON snapshot and rebuilds the parse tree. |
| `(*Miner).SaveJSON` | `func (m *Miner) SaveJSON() ([]byte, error)` | Serializes miner state to JSON. |
| `(*Miner).TrainEvent` | `func (m *Miner) TrainEvent(evt AgentEvent) (*MatchResult, error)` | Flattens an `AgentEvent`, trains on it, and propagates the event stage onto the result and cluster. |
| `DefaultConfig` | `func DefaultConfig() Config` | Returns the built-in production defaults, including masking rules and excluded fields. |
| `FlattenEvent` | `func FlattenEvent(evt AgentEvent, excludeFields []string) string` | Converts an event into a deterministic space-separated `key=value` token string with `stage=` first when present. |
| `NewAnomalyDetector` | `func NewAnomalyDetector(simThreshold float64, rareClusterThreshold int) (*AnomalyDetector, error)` | Validates thresholds and constructs an anomaly detector. |
| `NewCoordinator` | `func NewCoordinator(cfg Config, stages []string) (*Coordinator, error)` | Creates a stage-aware coordinator with one miner per supplied stage. |
| `NewMasker` | `func NewMasker(rules []MaskRule) (*Masker, error)` | Compiles regex mask rules into a reusable masker. |
| `NewMiner` | `func NewMiner(cfg Config) (*Miner, error)` | Constructs a miner with compiled mask rules, a fresh parse tree, and an empty cluster store. |
| `StageSequence` | `func StageSequence(events []AgentEvent) string` | Returns the stages from a slice of events as a single space-separated string. |
| `Tokenize` | `func Tokenize(line string) []string` | Splits a line on whitespace. |

### Constants

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| `AnomalyMaxScore` | untyped numeric constant | `2.0` | Maximum raw anomaly weight before normalization into the `[0,1]` score range. |
| `AnomalyWeightLow` | untyped numeric constant | `0.7` | Weight added when a known cluster matches below the similarity threshold. |
| `AnomalyWeightNew` | untyped numeric constant | `1.0` | Weight added when analysis creates a brand-new template cluster. |
| `AnomalyWeightRare` | untyped numeric constant | `0.3` | Weight added when the matched cluster size is at or below the rare-cluster threshold. |

## Usage Examples

Examples below are taken from the package's spec tests and reflect the current public API.

```go
cfg := agentdrain.DefaultConfig()
miner, err := agentdrain.NewMiner(cfg)
if err != nil {
	panic(err)
}

evt := agentdrain.AgentEvent{
	Stage:  "plan",
	Fields: map[string]string{"action": "start", "step": "1"},
}
result, err := miner.TrainEvent(evt)
if err != nil {
	panic(err)
}
fmt.Println(result.ClusterID)
```

```go
cfg := agentdrain.DefaultConfig()
coord, err := agentdrain.NewCoordinator(cfg, []string{"plan", "tool_call", "finish"})
if err != nil {
	panic(err)
}

evt := agentdrain.AgentEvent{
	Stage:  "plan",
	Fields: map[string]string{"action": "evaluate", "step": "1"},
}
result, report, err := coord.AnalyzeEvent(evt)
if err != nil {
	panic(err)
}
fmt.Println(result.Stage, report.AnomalyScore)
```

```go
flat := agentdrain.FlattenEvent(
	agentdrain.AgentEvent{
		Stage: "tool_call",
		Fields: map[string]string{
			"session_id": "abc-123",
			"action":     "start",
		},
	},
	[]string{"session_id"},
)
fmt.Println(flat)
// Output: stage=tool_call action=start
```

```go
masker, err := agentdrain.NewMasker([]agentdrain.MaskRule{{
	Name:        "number_test",
	Pattern:     `\d+`,
	Replacement: "<NUM>",
}})
if err != nil {
	panic(err)
}
fmt.Println(masker.Mask("step 42 completed"))
```

## Design Decisions

`FlattenEvent` is deterministic by design: it emits `stage=` first when present, sorts remaining field keys alphabetically, and omits explicitly excluded fields. This makes clustering stable across Go map iteration order and allows saved weights to remain reusable.

`Miner.AnalyzeEvent` performs inference before training, then trains on the same event and scores the resulting cluster with `AnomalyDetector`. The anomaly flags intentionally treat “new template” and “low similarity” as mutually exclusive so a brand-new cluster is not double-counted as both conditions.

`Coordinator` isolates miners by stage. This prevents templates from unrelated workflow phases from merging into the same cluster space and supports persistence as either per-stage snapshots or one combined weights document. The embedded default-weights mechanism provides an opt-in pre-trained baseline without exposing embedding details through additional API surface.

## Dependencies

Internal package dependencies include `pkg/logger` for debug logging, `pkg/setutil` for exclusion-set membership checks, and `pkg/sliceutil` for collection helpers used while flattening events. The package also embeds `data/default_weights.json` for coordinator bootstrapping.

External dependencies for production code are limited to the Go standard library, including `encoding/json`, `regexp`, `sort`, `strings`, `sync`, and `embed` support.

## Thread Safety

`Miner` is safe for concurrent use. It protects mutable state with an internal `sync.RWMutex`; training and load operations take the write lock, while cluster snapshots and JSON save operations take the read lock.

`Coordinator` is also safe for concurrent use. It protects its stage-to-miner map with its own `sync.RWMutex` and relies on each contained `Miner` for per-stage concurrency control.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
