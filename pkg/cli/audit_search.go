package cli

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/github/gh-aw/pkg/agentdrain"
	"github.com/github/gh-aw/pkg/logger"
)

var auditSearchLog = logger.New("cli:audit_search")

const (
	defaultAuditSearchLimit = 5
	maxAuditSearchTextBytes = 16 * 1024
	auditSearchExcerptBytes = 600
)

// AuditEvidenceSearch is query-specific retrieval, not a completeness or threat assessment.
type AuditEvidenceSearch struct {
	RunID          int64                `json:"run_id"`
	Source         string               `json:"source"`
	Query          string               `json:"query"`
	IndexedEvents  int                  `json:"indexed_events"`
	MatchingEvents int                  `json:"matching_events"`
	TextBytes      int                  `json:"text_bytes"`
	Truncated      bool                 `json:"truncated"`
	Matches        []AuditEvidenceMatch `json:"matches"`
}

// AuditEvidenceMatch represents an event or an automatically selected repetition group.
type AuditEvidenceMatch struct {
	Score               float64                `json:"bm25_score"`
	Stage               string                 `json:"stage"`
	ClusterID           int                    `json:"cluster_id,omitempty"`
	Template            string                 `json:"template,omitempty"`
	Occurrences         int                    `json:"occurrences"`
	MatchingOccurrences int                    `json:"matching_occurrences"`
	AnomalyScore        float64                `json:"anomaly_score,omitempty"`
	Evidence            AuditEvidenceExcerpt   `json:"evidence"`
	References          []AuditEvidenceRef     `json:"references"`
	Context             []AuditEvidenceExcerpt `json:"context,omitempty"`
}

type AuditEvidenceExcerpt struct {
	Reference AuditEvidenceRef `json:"reference"`
	Text      string           `json:"text"`
	Truncated bool             `json:"truncated,omitempty"`
}

type auditSearchDocument struct {
	ref       AuditEvidenceRef
	text      string
	outcome   string
	stage     string
	clusterID int
	anomaly   float64
}

func validateAuditSearch(query string, limit int) error {
	if len(query) > 1024 || len(auditSearchTokens(query)) == 0 {
		return errors.New("--search requires a query containing letters or digits, at most 1024 bytes")
	}
	if limit < 1 || limit > 20 {
		return errors.New("--search-limit must be between 1 and 20")
	}
	return nil
}

// Preserve exact identifiers as well as their components, without stemming paths or error codes.
func auditSearchTokens(text string) []string {
	var tokens []string
	for word := range strings.FieldsFuncSeq(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("/._:-", r)
	}) {
		word = strings.Trim(word, "/._:-")
		if word == "" {
			continue
		}
		tokens = append(tokens, word)
		parts := strings.FieldsFunc(word, func(r rune) bool { return strings.ContainsRune("/._:-", r) })
		if len(parts) > 1 {
			tokens = append(tokens, parts...)
		}
	}
	return tokens
}

type auditSearchRank struct {
	index int
	score float64
}

// Exact repeats share one BM25 document so retries cannot distort document frequency.
func rankAuditEvidence(ctx context.Context, docs []auditSearchDocument, query string) ([]auditSearchRank, error) {
	queryTerms := make(map[string]struct{})
	for _, term := range auditSearchTokens(query) {
		queryTerms[term] = struct{}{}
	}
	type documentTerms struct {
		index  int
		length int
		freq   map[string]int
	}
	var corpus []documentTerms
	seen := make(map[string]struct{})
	df := make(map[string]int)
	totalLength := 0
	sortedTerms := slices.Sorted(maps.Keys(queryTerms))
	for i, doc := range docs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key := doc.stage + "\n" + doc.text
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		tokens := auditSearchTokens(doc.text + " " + doc.ref.Type)
		freq := auditSearchQueryFrequencies(tokens, queryTerms)
		for term := range freq {
			df[term]++
		}
		corpus = append(corpus, documentTerms{i, len(tokens), freq})
		totalLength += len(tokens)
	}
	if len(corpus) == 0 || totalLength == 0 {
		return nil, nil
	}
	avgLength := float64(totalLength) / float64(len(corpus))
	var ranked []auditSearchRank
	for _, doc := range corpus {
		score := 0.0
		// Stable summation order keeps ties deterministic across invocations.
		for _, term := range sortedTerms {
			score += auditSearchBM25Term(doc.freq[term], df[term], len(corpus), doc.length, avgLength)
		}
		if score > 0 {
			ranked = append(ranked, auditSearchRank{doc.index, score})
		}
	}
	slices.SortStableFunc(ranked, func(a, b auditSearchRank) int {
		if a.score > b.score {
			return -1
		}
		if a.score < b.score {
			return 1
		}
		return a.index - b.index
	})
	return ranked, nil
}

func auditSearchBM25Term(frequency, documentFrequency, documents, length int, averageLength float64) float64 {
	if frequency == 0 {
		return 0
	}
	tf := float64(frequency)
	idf := math.Log1p((float64(documents-documentFrequency) + 0.5) / (float64(documentFrequency) + 0.5))
	return idf * tf * 2.2 / (tf + 1.2*(0.25+0.75*float64(length)/averageLength))
}

func auditSearchQueryFrequencies(terms []string, queryTerms map[string]struct{}) map[string]int {
	freq := make(map[string]int)
	for _, term := range terms {
		if _, ok := queryTerms[term]; ok {
			freq[term]++
		}
	}
	return freq
}

func searchAuditSession(ctx context.Context, runDir string, runID int64, query string, limit int) (*AuditEvidenceSearch, error) {
	if limit == 0 {
		limit = defaultAuditSearchLimit
	}
	if err := validateAuditSearch(query, limit); err != nil {
		return nil, err
	}
	auditSearchLog.Printf("Searching audit session: run_id=%d, dir=%s, limit=%d", runID, runDir, limit)
	docs, source, err := readAuditSearchSession(ctx, runDir)
	if err != nil {
		return nil, err
	}
	ranked, err := rankAuditEvidence(ctx, docs, query)
	if err != nil {
		return nil, err
	}
	auditSearchLog.Printf("Ranked audit evidence: source=%s, documents=%d, ranked=%d", source, len(docs), len(ranked))
	clusters, err := groupAuditEvidence(ctx, docs, ranked, query, limit)
	if err != nil {
		return nil, err
	}
	return selectAuditEvidence(docs, clusters, ranked, runID, source, query, limit), nil
}

func auditEvidenceClusterKey(doc auditSearchDocument) string {
	if doc.clusterID == 0 {
		return doc.stage + "\n" + doc.text
	}
	return fmt.Sprintf("%s:%d", doc.stage, doc.clusterID)
}

func clusterAuditEvidence(ctx context.Context, docs []auditSearchDocument) (map[string]agentdrain.Cluster, error) {
	stages := make(map[string]struct{})
	for _, doc := range docs {
		stages[doc.stage] = struct{}{}
	}
	auditSearchLog.Printf("Clustering audit evidence: documents=%d, stages=%d", len(docs), len(stages))
	if len(stages) > 256 {
		return nil, errors.New("unified session exceeds the 256-stage search input limit")
	}
	coordinator, err := agentdrain.NewCoordinator(agentdrain.DefaultConfig(), slices.Sorted(maps.Keys(stages)))
	if err != nil {
		return nil, err
	}
	for i := range docs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		doc := &docs[i]
		result, report, err := coordinator.AnalyzeEvent(agentdrain.AgentEvent{
			Stage:  doc.stage,
			Fields: map[string]string{"text": auditSearchClip(doc.text, 2048)},
		})
		if err != nil {
			return nil, fmt.Errorf("cluster unified session line %d: %w", doc.ref.Line, err)
		}
		doc.clusterID = result.ClusterID
		doc.anomaly = report.AnomalyScore
	}
	clusters := make(map[string]agentdrain.Cluster)
	for stage, entries := range coordinator.AllClusters() {
		for _, cluster := range entries {
			clusters[fmt.Sprintf("%s:%d", stage, cluster.ID)] = cluster
		}
	}
	return partitionAuditEvidenceOutcomes(docs, clusters), nil
}

func selectAuditEvidence(docs []auditSearchDocument, clusters map[string]agentdrain.Cluster, ranked []auditSearchRank, runID int64, source, query string, limit int) *AuditEvidenceSearch {
	search := &AuditEvidenceSearch{RunID: runID, Source: source, Query: query, IndexedEvents: len(docs), Matches: []AuditEvidenceMatch{}}
	groups := make(map[string]int)
	// Count all occurrences, including exact duplicates excluded from BM25 statistics.
	matching := make(map[string]int)
	for _, hit := range ranked {
		doc := docs[hit.index] //nolint:uncheckedsliceindex // ranked indexes come from ranging over this document slice
		matching[doc.stage+"\n"+doc.text] = hit.index
	}
	counts := make(map[string]int)
	for _, doc := range docs {
		if _, ok := matching[doc.stage+"\n"+doc.text]; ok {
			counts[auditEvidenceClusterKey(doc)]++
			search.MatchingEvents++
		}
	}
	for _, hit := range ranked {
		doc := docs[hit.index] //nolint:uncheckedsliceindex // ranked indexes come from ranging over this document slice
		key := auditEvidenceClusterKey(doc)
		if _, ok := groups[key]; ok {
			continue
		}
		if len(search.Matches) >= limit {
			search.Truncated = true
			continue
		}
		cluster := clusters[key]
		match := AuditEvidenceMatch{
			Score: hit.score, Stage: doc.stage, ClusterID: doc.clusterID,
			Occurrences: cluster.Size, MatchingOccurrences: counts[key], AnomalyScore: doc.anomaly,
			Template: auditSearchClip(strings.Join(cluster.Template, " "), auditSearchExcerptBytes),
			Evidence: auditEvidenceExcerpt(doc, query), References: []AuditEvidenceRef{doc.ref},
		}
		for i := hit.index - 1; i <= hit.index+1; i++ {
			if i >= 0 && i < len(docs) && i != hit.index {
				match.Context = append(match.Context, auditEvidenceExcerpt(docs[i], query))
			}
		}
		size := len(match.Template) + len(match.Evidence.Text)
		for _, neighbor := range match.Context {
			size += len(neighbor.Text)
		}
		if search.TextBytes+size > maxAuditSearchTextBytes {
			search.Truncated = true
			continue
		}
		groups[key] = len(search.Matches)
		search.Matches = append(search.Matches, match)
		search.TextBytes += size
	}
	for _, doc := range docs {
		group, selected := groups[auditEvidenceClusterKey(doc)]
		_, matched := matching[doc.stage+"\n"+doc.text]
		if selected && matched {
			match := &search.Matches[group] //nolint:uncheckedsliceindex // groups records only indexes appended to search.Matches
			if len(match.References) < 3 && doc.ref.Line != match.Evidence.Reference.Line {
				match.References = append(match.References, doc.ref)
			}
		}
	}
	return search
}

func auditEvidenceExcerpt(doc auditSearchDocument, query string) AuditEvidenceExcerpt {
	text := safeModelRoutingText(doc.text)
	start := 0
	terms := auditSearchTokens(query)
	slices.SortStableFunc(terms, func(a, b string) int { return len(b) - len(a) })
	for _, term := range terms {
		if pos := strings.Index(strings.ToLower(text), term); pos >= 0 {
			// Lowercasing can change UTF-8 byte lengths; map back to the source.
			lowerOffset := 0
			for offset, r := range text {
				if lowerOffset >= pos {
					start = max(0, offset-100)
					break
				}
				lowerOffset += utf8.RuneLen(unicode.ToLower(r))
			}
			break
		}
	}
	for start > 0 && start < len(text) && !utf8.RuneStart(text[start]) { //nolint:uncheckedsliceindex // both byte bounds are checked in this condition
		start--
	}
	excerpt := auditSearchClip(text[start:], auditSearchExcerptBytes)
	return AuditEvidenceExcerpt{Reference: doc.ref, Text: excerpt, Truncated: start > 0 || len(excerpt) < len(text)}
}

func auditSearchClip(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && limit < len(text) && !utf8.RuneStart(text[limit]) { //nolint:uncheckedsliceindex // both byte bounds are checked in this condition
		limit--
	}
	return text[:limit]
}

func renderConsoleEvidenceSearch(search *AuditEvidenceSearch) {
	if search == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "  evidence_search: query=%q source=%s events=%d matching=%d groups=%d truncated=%t (retrieval only)\n",
		safeModelRoutingText(search.Query), safeModelRoutingText(search.Source), search.IndexedEvents, search.MatchingEvents, len(search.Matches), search.Truncated)
	for _, match := range search.Matches {
		fmt.Fprintf(os.Stderr, "    bm25=%.3f occurrences=%d matching=%d line=%d",
			match.Score, match.Occurrences, match.MatchingOccurrences, match.Evidence.Reference.Line)
		if match.ClusterID != 0 {
			fmt.Fprintf(os.Stderr, " cluster=%s:%d anomaly=%.3f", safeModelRoutingText(match.Stage), match.ClusterID, match.AnomalyScore)
		}
		fmt.Fprintf(os.Stderr, "\n      %s\n", match.Evidence.Text)
		for _, neighbor := range match.Context {
			fmt.Fprintf(os.Stderr, "      context line=%d: %s\n", neighbor.Reference.Line, neighbor.Text)
		}
	}
}
