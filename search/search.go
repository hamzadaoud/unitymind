package search

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"sync"
	"unicode"
)

// Doc is a single indexed Unity documentation page
type Doc struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
	Tags    []string `json:"tags"`
}

// Result is a ranked search hit
type Result struct {
	Title   string
	URL     string
	Excerpt string
	Score   float64
}

// Engine is the local search engine (in-memory, zero deps)
type Engine struct {
	mu   sync.RWMutex
	docs []Doc
	// inverted index: token → []doc indices
	index map[string][]int
}

func NewEngine() *Engine {
	return &Engine{
		docs:  make([]Doc, 0, 500),
		index: make(map[string][]int),
	}
}

// DocCount returns how many docs are indexed
func (e *Engine) DocCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.docs)
}

// tokenize splits text into lowercase tokens, removes stop words
func tokenize(text string) []string {
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "in": true,
		"to": true, "of": true, "and": true, "or": true, "for": true,
		"on": true, "with": true, "this": true, "that": true, "it": true,
		"be": true, "as": true, "at": true, "by": true, "we": true,
		"how": true, "do": true, "i": true, "you": true, "can": true,
		"what": true, "from": true, "are": true, "use": true, "used": true,
	}
	var tokens []string
	var current strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else {
			if current.Len() >= 2 {
				tok := current.String()
				if !stopWords[tok] {
					tokens = append(tokens, tok)
				}
			}
			current.Reset()
		}
	}
	if current.Len() >= 2 {
		tok := current.String()
		if !stopWords[tok] {
			tokens = append(tokens, tok)
		}
	}
	return tokens
}

// AddDoc indexes a single document
func (e *Engine) AddDoc(doc Doc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	// Deduplicate by URL
	for i, d := range e.docs {
		if d.URL == doc.URL {
			e.docs[i] = doc
			e.reindexDoc(i, doc)
			return
		}
	}
	idx := len(e.docs)
	e.docs = append(e.docs, doc)
	e.reindexDoc(idx, doc)
}

func (e *Engine) reindexDoc(idx int, doc Doc) {
	combined := doc.Title + " " + doc.Content + " " + strings.Join(doc.Tags, " ")
	tokens := tokenize(combined)
	seen := map[string]bool{}
	for _, tok := range tokens {
		if seen[tok] {
			continue
		}
		seen[tok] = true
		e.index[tok] = append(e.index[tok], idx)
	}
}

// AddResults adds multiple search results to the index
func (e *Engine) AddResults(results []Result) {
	for _, r := range results {
		e.AddDoc(Doc{
			ID:      r.URL,
			Title:   r.Title,
			URL:     r.URL,
			Content: r.Excerpt,
		})
	}
}

// Search finds the top-k most relevant docs for a query
func (e *Engine) Search(query string, topK int) []Result {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(e.docs) == 0 {
		return nil
	}

	tokens := tokenize(query)
	if len(tokens) == 0 {
		return nil
	}

	// BM25-lite scoring
	scores := make(map[int]float64)
	N := float64(len(e.docs))
	avgLen := e.avgDocLen()
	k1 := 1.5
	b := 0.75

	for _, tok := range tokens {
		// Exact match
		e.scoreToken(tok, tokens, scores, N, avgLen, k1, b, 1.0)
		// Prefix match (partial)
		for indexedTok := range e.index {
			if indexedTok != tok && strings.HasPrefix(indexedTok, tok) && len(tok) >= 3 {
				e.scoreToken(indexedTok, tokens, scores, N, avgLen, k1, b, 0.7)
			}
		}
	}

	// Boost score if title contains query tokens
	for idx, doc := range e.docs {
		titleLower := strings.ToLower(doc.Title)
		for _, tok := range tokens {
			if strings.Contains(titleLower, tok) {
				scores[idx] += 2.0
			}
		}
	}

	// Collect and sort
	type scoredDoc struct {
		idx   int
		score float64
	}
	var ranked []scoredDoc
	for idx, score := range scores {
		ranked = append(ranked, scoredDoc{idx, score})
	}
	// Simple insertion sort (small N, low memory)
	for i := 1; i < len(ranked); i++ {
		for j := i; j > 0 && ranked[j].score > ranked[j-1].score; j-- {
			ranked[j], ranked[j-1] = ranked[j-1], ranked[j]
		}
	}

	// Build results
	results := make([]Result, 0, topK)
	maxScore := 0.0
	if len(ranked) > 0 {
		maxScore = ranked[0].score
	}
	for i, sd := range ranked {
		if i >= topK {
			break
		}
		doc := e.docs[sd.idx]
		normalizedScore := 0.0
		if maxScore > 0 {
			normalizedScore = sd.score / maxScore
		}
		results = append(results, Result{
			Title:   doc.Title,
			URL:     doc.URL,
			Excerpt: extractExcerpt(doc.Content, tokens, 300),
			Score:   normalizedScore,
		})
	}
	return results
}

func (e *Engine) scoreToken(tok string, queryTokens []string, scores map[int]float64, N, avgLen, k1, b, boost float64) {
	postings, ok := e.index[tok]
	if !ok {
		return
	}
	df := float64(len(postings))
	idf := math.Log((N-df+0.5)/(df+0.5) + 1)
	for _, idx := range postings {
		doc := e.docs[idx]
		docLen := float64(len(tokenize(doc.Content + " " + doc.Title)))
		tf := countOccurrences(tok, doc.Content+" "+doc.Title)
		tfNorm := float64(tf) * (k1 + 1) / (float64(tf) + k1*(1-b+b*docLen/avgLen))
		scores[idx] += idf * tfNorm * boost
	}
}

func (e *Engine) avgDocLen() float64 {
	if len(e.docs) == 0 {
		return 100
	}
	total := 0
	for _, d := range e.docs {
		total += len(tokenize(d.Content + " " + d.Title))
	}
	return float64(total) / float64(len(e.docs))
}

func countOccurrences(tok, text string) int {
	count := 0
	lower := strings.ToLower(text)
	idx := 0
	for {
		i := strings.Index(lower[idx:], tok)
		if i < 0 {
			break
		}
		count++
		idx += i + len(tok)
	}
	return count
}

// extractExcerpt pulls the most relevant snippet from content
func extractExcerpt(content string, tokens []string, maxLen int) string {
	if len(content) == 0 {
		return ""
	}
	lower := strings.ToLower(content)
	bestPos := 0
	bestHits := 0
	// Slide a window to find densest token region
	windowSize := 200
	for i := 0; i < len(lower)-windowSize; i += 50 {
		end := i + windowSize
		if end > len(lower) {
			end = len(lower)
		}
		window := lower[i:end]
		hits := 0
		for _, tok := range tokens {
			if strings.Contains(window, tok) {
				hits++
			}
		}
		if hits > bestHits {
			bestHits = hits
			bestPos = i
		}
	}
	// Extract around best position
	start := bestPos
	if start > 50 {
		start -= 50
	}
	end := start + maxLen
	if end > len(content) {
		end = len(content)
	}
	excerpt := strings.TrimSpace(content[start:end])
	if start > 0 {
		excerpt = "..." + excerpt
	}
	if end < len(content) {
		excerpt = excerpt + "..."
	}
	return excerpt
}

// --- Persistence ---

type cacheFile struct {
	Docs []Doc `json:"docs"`
}

func (e *Engine) SaveCache(path string) error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	data, err := json.Marshal(cacheFile{Docs: e.docs})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (e *Engine) LoadCache(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return err
	}
	for _, doc := range cf.Docs {
		e.AddDoc(doc)
	}
	return nil
}
