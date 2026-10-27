package agentdrain

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/logger"
)

var minerLog = logger.New("agentdrain:miner")

// Miner is a concurrent Drain-style log template miner.
// Use NewMiner to create an instance.
type Miner struct {
	cfg      Config
	masker   *Masker
	detector *AnomalyDetector
	tree     *parseTree
	store    *clusterStore
	mu       sync.RWMutex
}

// NewMiner creates a Miner from the given Config.
func NewMiner(cfg Config) (*Miner, error) {
	minerLog.Printf("Creating new miner: depth=%d, simThreshold=%.2f, maxChildren=%d", cfg.Depth, cfg.SimThreshold, cfg.MaxChildren)
	masker, err := NewMasker(cfg.MaskRules)
	if err != nil {
		return nil, fmt.Errorf("agentdrain: NewMiner: %w", err)
	}
	detector, err := NewAnomalyDetector(cfg.SimThreshold, cfg.RareClusterThreshold)
	if err != nil {
		return nil, fmt.Errorf("agentdrain: NewMiner: %w", err)
	}
	return &Miner{
		cfg:      cfg,
		masker:   masker,
		detector: detector,
		tree:     newParseTree(),
		store:    newClusterStore(),
	}, nil
}

// trainTokens updates the miner state for tokens. Caller must hold m.mu.
func (m *Miner) trainTokens(tokens []string, stage string) *MatchResult {
	result, _ := m.findBestMatchingCluster(tokens)
	return m.applyMatch(tokens, stage, result)
}

// applyMatch merges tokens into the cluster identified by result, or creates a
// new cluster when result is nil. result must be the inference outcome for the
// same tokens. Caller must hold m.mu.
func (m *Miner) applyMatch(tokens []string, stage string, result *MatchResult) *MatchResult {
	if result != nil {
		// Merge and update existing cluster.
		c, _ := m.store.get(result.ClusterID)
		c.Template = mergeTemplate(c.Template, tokens, m.cfg.ParamToken)
		c.Size++
		result.Template = strings.Join(c.Template, " ")
		result.Params = extractParams(tokens, c.Template, m.cfg.ParamToken)
		if stage != "" {
			result.Stage = stage
			if c.Stage == "" {
				c.Stage = stage
			}
		}
		minerLog.Printf("Train: matched existing cluster: id=%d, size=%d, similarity=%.2f", c.ID, c.Size, result.Similarity)
		return result
	}

	// Create new cluster.
	c := m.store.add(tokens, stage)
	m.tree.addCluster(tokens, c.ID, m.cfg.Depth, m.cfg.MaxChildren, m.cfg.ParamToken)
	minerLog.Printf("Train: created new cluster: id=%d, totalClusters=%d", c.ID, len(m.store.clusters))
	return &MatchResult{
		ClusterID:  c.ID,
		Template:   strings.Join(c.Template, " "),
		Params:     []string{},
		Similarity: 1.0,
		Stage:      c.Stage,
	}
}

// findBestMatchingCluster is the internal (non-locking) lookup. Must be called with mu held.
func (m *Miner) findBestMatchingCluster(tokens []string) (*MatchResult, bool) {
	candidates := m.tree.search(tokens, m.cfg.Depth, m.cfg.ParamToken)
	minerLog.Printf("findBestMatchingCluster: searching %d candidate cluster(s) for %d token(s)", len(candidates), len(tokens))
	bestSim := -1.0
	var best *Cluster
	for _, id := range candidates {
		c, ok := m.store.get(id)
		if !ok {
			continue
		}
		sim := computeSimilarity(c.Template, tokens, m.cfg.ParamToken)
		if sim > bestSim {
			bestSim = sim
			best = c
		}
	}
	if best == nil || bestSim < m.cfg.SimThreshold {
		minerLog.Printf("findBestMatchingCluster: no cluster matched (best_sim=%.2f, threshold=%.2f)", bestSim, m.cfg.SimThreshold)
		return nil, false
	}
	params := extractParams(tokens, best.Template, m.cfg.ParamToken)
	minerLog.Printf("findBestMatchingCluster: matched cluster id=%d, similarity=%.2f, params=%d", best.ID, bestSim, len(params))
	return &MatchResult{
		ClusterID:  best.ID,
		Template:   strings.Join(best.Template, " "),
		Params:     params,
		Similarity: bestSim,
		Stage:      best.Stage,
	}, true
}

// prepare flattens, masks, and tokenizes an AgentEvent.
func (m *Miner) prepare(evt AgentEvent) ([]string, error) {
	tokens := Tokenize(m.masker.Mask(FlattenEvent(evt, m.cfg.ExcludeFields)))
	if len(tokens) == 0 {
		minerLog.Printf("prepare: stage=%s produced zero tokens after masking", evt.Stage)
		return nil, errors.New("agentdrain: empty event after masking")
	}
	return tokens, nil
}

// TrainEvent flattens the AgentEvent and updates the miner.
func (m *Miner) TrainEvent(evt AgentEvent) (*MatchResult, error) {
	minerLog.Printf("TrainEvent: stage=%s", evt.Stage)
	tokens, err := m.prepare(evt)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.trainTokens(tokens, evt.Stage), nil
}

// AnalyzeEvent prepares the event once, then under a single write lock runs
// inference, updates the miner state directly, and builds an AnomalyReport.
// The write lock spans the whole operation so the isNew decision cannot become
// stale relative to the cluster the event is trained into; inference is not
// taken under a read lock because its result is reused for the mutation.
// Returns the match result and report.
func (m *Miner) AnalyzeEvent(evt AgentEvent) (*MatchResult, *AnomalyReport, error) {
	minerLog.Printf("AnalyzeEvent: stage=%s", evt.Stage)
	tokens, err := m.prepare(evt)
	if err != nil {
		return nil, nil, fmt.Errorf("agentdrain: AnalyzeEvent: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	inferResult, _ := m.findBestMatchingCluster(tokens)
	isNew := inferResult == nil
	result := m.applyMatch(tokens, evt.Stage, inferResult)
	cluster, _ := m.store.get(result.ClusterID)
	report := m.detector.Analyze(result, isNew, cluster)
	return result, report, nil
}

// Clusters returns a snapshot of all known clusters.
func (m *Miner) Clusters() []Cluster {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store.all()
}
