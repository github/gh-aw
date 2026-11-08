package agentdrain

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/github/gh-aw/pkg/logger"
)

var persistLog = logger.New("agentdrain:persist")

// Snapshot is the serializable representation of a Miner's state.
type Snapshot struct {
	Config   Config            `json:"config"`
	Clusters []SnapshotCluster `json:"clusters"`
	NextID   int               `json:"next_id"`
}

// SnapshotCluster is the serializable form of a single Cluster.
type SnapshotCluster struct {
	ID       int      `json:"id"`
	Template []string `json:"template"`
	Size     int      `json:"size"`
	Stage    string   `json:"stage"`
}

// SaveJSON serializes the miner's current state to JSON bytes.
func (m *Miner) SaveJSON() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	persistLog.Printf("Saving miner state: clusters=%d", len(m.store.clusters))
	snap := Snapshot{
		Config:   m.cfg,
		NextID:   m.store.nextID,
		Clusters: make([]SnapshotCluster, 0, len(m.store.clusters)),
	}
	for _, c := range m.store.clusters {
		snap.Clusters = append(snap.Clusters, SnapshotCluster{
			ID:       c.ID,
			Template: slices.Clone(c.Template),
			Size:     c.Size,
			Stage:    c.Stage,
		})
	}
	return json.Marshal(snap)
}

// LoadJSON restores miner state from JSON bytes produced by SaveJSON.
// The existing state is replaced; the parse tree is rebuilt from the snapshot.
func (m *Miner) LoadJSON(data []byte) error {
	persistLog.Printf("Loading miner state: bytes=%d", len(data))
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		persistLog.Printf("Failed to decode miner snapshot JSON: %v", err)
		return fmt.Errorf("agentdrain: LoadJSON: %w", err)
	}
	persistLog.Printf("Decoded miner snapshot: clusters=%d, next_id=%d, mask_rules=%d", len(snap.Clusters), snap.NextID, len(snap.Config.MaskRules))

	masker, err := NewMasker(snap.Config.MaskRules)
	if err != nil {
		persistLog.Printf("Failed to rebuild masker from snapshot config: %v", err)
		return fmt.Errorf("agentdrain: LoadJSON: %w", err)
	}
	detector, err := NewAnomalyDetector(snap.Config.SimThreshold, snap.Config.RareClusterThreshold)
	if err != nil {
		persistLog.Printf("Failed to rebuild anomaly detector from snapshot config: %v", err)
		return fmt.Errorf("agentdrain: LoadJSON: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.cfg = snap.Config
	m.masker = masker
	m.detector = detector
	m.store = newClusterStore()
	m.tree = newParseTree()
	m.store.nextID = snap.NextID

	for _, sc := range snap.Clusters {
		c := &Cluster{
			ID:       sc.ID,
			Template: slices.Clone(sc.Template),
			Size:     sc.Size,
			Stage:    sc.Stage,
		}
		m.store.clusters[c.ID] = c
		m.tree.addCluster(c.Template, c.ID, m.cfg.Depth, m.cfg.MaxChildren, m.cfg.ParamToken)
	}
	persistLog.Printf("Loaded miner state: clusters=%d", len(snap.Clusters))
	return nil
}
