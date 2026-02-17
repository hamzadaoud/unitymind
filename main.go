package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"unitymind/brain"
	"unitymind/docs"
	"unitymind/offline"
	"unitymind/openai"
	"unitymind/search"
)

//go:embed ui/index.html
var uiFiles embed.FS

type Config struct {
	OpenAIKey       string `json:"openai_key"`
	OpenAIModel     string `json:"openai_model"`
	Port            int    `json:"port"`
	AutoUpdate      bool   `json:"auto_update_docs"`
	LastDocUpdate   string `json:"last_doc_update"`
	OfflineDocsPath string `json:"offline_docs_path"`
}

var cfg Config
var searcher *search.Engine
var docManager *docs.Manager
var offlineIndexer *offline.Indexer
var indexingProgress int32
var indexingDone int32

func loadConfig() {
	cfg = Config{OpenAIKey: "", OpenAIModel: "gpt-4o-mini", Port: 7331, AutoUpdate: true}
	data, err := os.ReadFile("config.json")
	if err != nil { saveConfig(); return }
	json.Unmarshal(data, &cfg)
}

func saveConfig() {
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile("config.json", data, 0644)
}

func openBrowser(url string) {
	var cmd string; var args []string
	switch runtime.GOOS {
	case "windows": cmd = "cmd"; args = []string{"/c", "start", url}
	case "darwin":  cmd = "open"; args = []string{url}
	default:        cmd = "xdg-open"; args = []string{url}
	}
	exec.Command(cmd, args...).Start()
}

func waitForPort(port int) {
	addr := fmt.Sprintf("localhost:%d", port)
	for i := 0; i < 30; i++ {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil { conn.Close(); return }
		time.Sleep(100 * time.Millisecond)
	}
}

type ChatRequest struct {
	Message string `json:"message"`
	History []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"history"`
}

type ChatResponse struct {
	Answer     string         `json:"answer"`
	Source     string         `json:"source"`
	Links      []docs.DocLink `json:"links"`
	Elapsed    string         `json:"elapsed"`
	Understood string         `json:"understood"`
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { http.Error(w, "POST only", 405); return }
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(ChatResponse{Answer: "Invalid request.", Source: "error"}); return
	}

	start := time.Now()
	raw := strings.TrimSpace(req.Message)
	if raw == "" {
		json.NewEncoder(w).Encode(ChatResponse{Answer: "Ask me anything about Unity!", Source: "error"}); return
	}

	// Step 0: Understand the query with NLU
	pq := offline.UnderstandQuery(raw)
	searchQuery := pq.EnhancedQuery()
	understood := pq.Summary()

	brainHistory := make([]brain.HistoryEntry, len(req.History))
	for i, h := range req.History {
		brainHistory[i] = brain.HistoryEntry{Role: h.Role, Content: h.Content}
	}

	// Step 1: Local index search (enhanced + raw fallback)
	results := searcher.Search(searchQuery, 5)
	if len(results) == 0 || results[0].Score < 0.4 {
		rawResults := searcher.Search(raw, 5)
		if len(rawResults) > 0 && (len(results) == 0 || rawResults[0].Score > results[0].Score) {
			results = rawResults
		}
	}
	elapsed := time.Since(start)

	if len(results) > 0 && results[0].Score >= 0.4 {
		json.NewEncoder(w).Encode(ChatResponse{
			Answer:     brain.Synthesize(raw, results, brainHistory),
			Source:     "local_docs",
			Links:      toLinks(results),
			Elapsed:    elapsed.Round(time.Millisecond).String(),
			Understood: understood,
		})
		return
	}

	// Step 2: Live docs
	liveResults, err := docManager.SearchLive(raw)
	elapsed = time.Since(start)
	if err == nil && len(liveResults) > 0 {
		searcher.AddResults(liveResults)
		go searcher.SaveCache("cache/docs_index.json")
		json.NewEncoder(w).Encode(ChatResponse{
			Answer:     brain.Synthesize(raw, liveResults, brainHistory),
			Source:     "live_docs",
			Links:      toLinks(liveResults),
			Elapsed:    elapsed.Round(time.Millisecond).String(),
			Understood: understood,
		})
		return
	}

	// Step 3: OpenAI fallback
	if cfg.OpenAIKey != "" {
		client := openai.NewClient(cfg.OpenAIKey, cfg.OpenAIModel)
		oaHistory := make([]openai.HistoryEntry, len(req.History))
		for i, h := range req.History { oaHistory[i] = openai.HistoryEntry{Role: h.Role, Content: h.Content} }
		aiAnswer, err := client.Ask(raw, oaHistory)
		elapsed = time.Since(start)
		if err == nil {
			json.NewEncoder(w).Encode(ChatResponse{
				Answer: aiAnswer, Source: "openai",
				Elapsed: elapsed.Round(time.Millisecond).String(), Understood: understood,
			})
			return
		}
	}

	noKey := ""
	if cfg.OpenAIKey == "" { noKey = " Add an OpenAI key in ⚙️ Settings to enable AI fallback." }
	json.NewEncoder(w).Encode(ChatResponse{
		Answer:     "I couldn't find anything about that in the docs." + noKey,
		Source:     "not_found",
		Elapsed:    time.Since(start).Round(time.Millisecond).String(),
		Understood: understood,
	})
}

func toLinks(results []search.Result) []docs.DocLink {
	links := make([]docs.DocLink, 0, len(results))
	seen := map[string]bool{}
	for _, r := range results {
		if !seen[r.URL] { seen[r.URL] = true; links = append(links, docs.DocLink{Title: r.Title, URL: r.URL}) }
	}
	return links
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodGet {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"has_openai_key":    cfg.OpenAIKey != "",
			"openai_model":      cfg.OpenAIModel,
			"port":              cfg.Port,
			"last_doc_update":   cfg.LastDocUpdate,
			"doc_count":         searcher.DocCount(),
			"offline_docs_path": cfg.OfflineDocsPath,
			"indexing_progress": atomic.LoadInt32(&indexingProgress),
			"indexing_done":     atomic.LoadInt32(&indexingDone) == 1,
		})
		return
	}
	if r.Method == http.MethodPost {
		var update map[string]string
		json.NewDecoder(r.Body).Decode(&update)
		if key, ok := update["openai_key"]; ok { cfg.OpenAIKey = key }
		if model, ok := update["openai_model"]; ok { cfg.OpenAIModel = model }
		if path, ok := update["offline_docs_path"]; ok && path != cfg.OfflineDocsPath {
			cfg.OfflineDocsPath = path
			if path != "" { go indexOfflineDocs(path) }
		}
		saveConfig()
		json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
	}
}

func indexOfflineDocs(path string) {
	log.Printf("[offline] Indexing: %s", path)
	atomic.StoreInt32(&indexingDone, 0)
	atomic.StoreInt32(&indexingProgress, 0)
	results, err := offlineIndexer.IndexPath(path, func(done, total int) {
		if total > 0 {
			atomic.StoreInt32(&indexingProgress, int32(float64(done)/float64(total)*100))
		}
		if done%200 == 0 { log.Printf("[offline] %d / %d pages indexed...", done, total) }
	})
	if err != nil {
		log.Printf("[offline] Error: %v", err)
		atomic.StoreInt32(&indexingDone, 1)
		return
	}
	searcher.AddResults(results)
	searcher.SaveCache("cache/docs_index.json")
	cfg.LastDocUpdate = fmt.Sprintf("Offline docs — %d pages", len(results))
	saveConfig()
	atomic.StoreInt32(&indexingProgress, 100)
	atomic.StoreInt32(&indexingDone, 1)
	log.Printf("[offline] Done! %d pages indexed from %s", len(results), path)
}

func handleDocsUpdate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	go func() {
		results, err := docManager.FetchCoreDocs()
		if err != nil { log.Printf("[docs] Error: %v", err); return }
		searcher.AddResults(results)
		searcher.SaveCache("cache/docs_index.json")
		cfg.LastDocUpdate = time.Now().Format("2006-01-02 15:04")
		saveConfig()
		log.Printf("[docs] Refreshed: %d pages", len(results))
	}()
	json.NewEncoder(w).Encode(map[string]string{"status": "update_started"})
}

func handleIndexOffline(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	var body struct{ Path string `json:"path"` }
	json.NewDecoder(r.Body).Decode(&body)
	path := strings.TrimSpace(body.Path)
	if path == "" { path = cfg.OfflineDocsPath }
	if path == "" { path = offline.FindDocPath(nil) }
	if path == "" {
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "No offline docs path found."})
		return
	}
	cfg.OfflineDocsPath = path
	saveConfig()
	go indexOfflineDocs(path)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "indexing_started", "path": path})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":            "ok",
		"doc_count":         searcher.DocCount(),
		"version":           "1.1.0",
		"indexing_progress": atomic.LoadInt32(&indexingProgress),
		"indexing_done":     atomic.LoadInt32(&indexingDone) == 1,
	})
}

func main() {
	log.Println("╔══════════════════════════════════╗")
	log.Println("║      UnityMind v1.1.0            ║")
	log.Println("╚══════════════════════════════════╝")

	loadConfig()
	searcher = search.NewEngine()
	docManager = docs.NewManager("cache")
	offlineIndexer = offline.NewIndexer()

	if err := searcher.LoadCache("cache/docs_index.json"); err != nil {
		log.Printf("[search] No cache: %v", err)
	} else {
		log.Printf("[search] Loaded %d docs from cache.", searcher.DocCount())
	}

	// ── Offline docs detection & indexing ─────────────────────────────────────
	log.Println("[offline] Looking for UnityDocumentation.zip or extracted folder...")

	if cfg.OfflineDocsPath != "" {
		log.Printf("[offline] Config path: %s", cfg.OfflineDocsPath)
		if searcher.DocCount() >= 100 {
			log.Printf("[offline] Cache already has %d pages — skipping re-index.", searcher.DocCount())
			atomic.StoreInt32(&indexingDone, 1)
			atomic.StoreInt32(&indexingProgress, 100)
		} else {
			go indexOfflineDocs(cfg.OfflineDocsPath)
		}
	} else {
		detected := offline.FindDocPath(nil)
		if detected != "" {
			log.Printf("[offline] ✓ Found: %s — starting index...", detected)
			cfg.OfflineDocsPath = detected
			saveConfig()
			go indexOfflineDocs(detected)
		} else {
			log.Println("[offline] ✗ No offline docs found next to exe.")
			log.Println("[offline]   Put UnityDocumentation.zip next to UnityMind.exe, then restart.")
			log.Println("[offline]   Or set the path in ⚙ Settings inside the app.")
			if searcher.DocCount() == 0 {
				log.Println("[docs] Falling back: fetching core docs from internet...")
				go func() {
					results, err := docManager.FetchCoreDocs()
					if err != nil { log.Printf("[docs] Error: %v", err); return }
					searcher.AddResults(results)
					searcher.SaveCache("cache/docs_index.json")
					cfg.LastDocUpdate = time.Now().Format("2006-01-02 15:04")
					saveConfig()
					log.Printf("[docs] Fetched %d pages.", len(results))
				}()
			} else {
				log.Printf("[docs] Using cached %d pages.", searcher.DocCount())
				atomic.StoreInt32(&indexingDone, 1)
				atomic.StoreInt32(&indexingProgress, 100)
			}
		}
	}

	uiFS, _ := fs.Sub(uiFiles, "ui")
	http.Handle("/", http.FileServer(http.FS(uiFS)))
	http.HandleFunc("/api/chat", handleChat)
	http.HandleFunc("/api/config", handleConfig)
	http.HandleFunc("/api/docs/update", handleDocsUpdate)
	http.HandleFunc("/api/docs/index-offline", handleIndexOffline)
	http.HandleFunc("/api/status", handleStatus)

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("[server] http://localhost%s", addr)
	go func() {
		waitForPort(cfg.Port)
		openBrowser(fmt.Sprintf("http://localhost:%d", cfg.Port))
	}()
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("[server] Failed: %v", err)
	}
}
