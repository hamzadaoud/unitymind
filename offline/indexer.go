// Package offline handles indexing and searching of the local Unity
// offline documentation (the ~300MB ZIP from docs.unity3d.com).
// It supports both an extracted folder and reading directly from the ZIP.
package offline

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"unitymind/search"
)

// IndexProgress reports progress during indexing
type IndexProgress struct {
	Total     int
	Processed int32
	Done      bool
	Error     error
}

// Indexer handles the offline Unity documentation
type Indexer struct {
	mu       sync.Mutex
	progress IndexProgress
}

func NewIndexer() *Indexer {
	return &Indexer{}
}

// FindDocPath auto-detects where the offline docs are.
// Checks the exe directory first (handles Windows double-click), then cwd.
func FindDocPath(hints []string) string {
	// Always check the directory the exe lives in first.
	// When double-clicking on Windows the working dir may be different.
	searchDirs := []string{"."}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		if real, err := filepath.EvalSymlinks(exePath); err == nil {
			exeDir = filepath.Dir(real)
		}
		if exeDir != "." {
			searchDirs = append(searchDirs, exeDir)
		}
	}

	// Direct hint paths take top priority
	for _, h := range hints {
		if h == "" { continue }
		if info, err := os.Stat(h); err == nil {
			if info.IsDir() && hasUnityDocs(h) { return h }
			if !info.IsDir() && isZip(h) { return h }
		}
	}

	// ZIP filenames Unity ships (checked first — user said zip is next to exe)
	zipNames := []string{
		"UnityDocumentation.zip",
		"Documentation.zip",
		"unity_docs.zip",
		"unity_documentation.zip",
	}
	for _, base := range searchDirs {
		for _, name := range zipNames {
			full := filepath.Join(base, name)
			if _, err := os.Stat(full); err == nil {
				log.Printf("[offline] Auto-detected ZIP: %s", full)
				return full
			}
		}
	}

	// Extracted folder names
	folderNames := []string{
		"Documentation", "UnityDocumentation",
		"docs", "offline_docs",
		filepath.Join("cache", "docs_html"),
	}
	for _, base := range searchDirs {
		for _, name := range folderNames {
			full := filepath.Join(base, name)
			if info, err := os.Stat(full); err == nil && info.IsDir() {
				if hasUnityDocs(full) {
					log.Printf("[offline] Auto-detected folder: %s", full)
					return full
				}
			}
		}
	}
	return ""
}

func isZip(p string) bool {
	return strings.HasSuffix(strings.ToLower(p), ".zip")
}

func hasUnityDocs(dir string) bool {
	// Look for Manual or ScriptReference subdirectories
	for _, sub := range []string{"Manual", "ScriptReference", "en/Manual", "en/ScriptReference", "Documentation/en/Manual"} {
		if info, err := os.Stat(filepath.Join(dir, sub)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// IndexPath indexes all HTML files from a path (folder or ZIP).
// Calls onProgress periodically with count of indexed pages.
// Returns all indexed results.
func (ix *Indexer) IndexPath(path string, onProgress func(done, total int)) ([]search.Result, error) {
	if strings.HasSuffix(strings.ToLower(path), ".zip") {
		return ix.indexZip(path, onProgress)
	}
	return ix.indexFolder(path, onProgress)
}

// ── ZIP Indexing ──────────────────────────────────────────────────────────────

func (ix *Indexer) indexZip(zipPath string, onProgress func(done, total int)) ([]search.Result, error) {
	log.Printf("[offline] Opening ZIP: %s", zipPath)
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("cannot open zip: %w", err)
	}
	defer r.Close()

	// First pass: find all relevant HTML files
	var targets []*zip.File
	for _, f := range r.File {
		if shouldIndex(f.Name) {
			targets = append(targets, f)
		}
	}
	log.Printf("[offline] ZIP has %d indexable HTML files", len(targets))

	var results []search.Result
	var mu sync.Mutex
	var processed int32

	// Process files (sequential for ZIP — random access is slow)
	for _, f := range targets {
		result, err := parseZipFile(f)
		if err != nil || result == nil {
			continue
		}
		mu.Lock()
		results = append(results, *result)
		mu.Unlock()

		n := int(atomic.AddInt32(&processed, 1))
		if n%50 == 0 && onProgress != nil {
			onProgress(n, len(targets))
		}
	}

	if onProgress != nil {
		onProgress(len(results), len(targets))
	}
	return results, nil
}

func parseZipFile(f *zip.File) (*search.Result, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	html := string(data)
	title := extractTitle(html)
	content := extractMainContent(html)
	if len(content) < 80 {
		return nil, nil // Skip near-empty pages
	}
	if len(content) > 12000 {
		content = content[:12000]
	}

	// Build a URL from the ZIP path (so links still work if docs are extracted)
	url := zipPathToURL(f.Name)

	return &search.Result{
		Title:   title,
		URL:     url,
		Excerpt: content,
		Score:   1.0,
	}, nil
}

// ── Folder Indexing ───────────────────────────────────────────────────────────

func (ix *Indexer) indexFolder(root string, onProgress func(done, total int)) ([]search.Result, error) {
	log.Printf("[offline] Scanning folder: %s", root)

	// Collect all HTML file paths first
	var paths []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if !info.IsDir() && shouldIndex(path) {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk error: %w", err)
	}
	log.Printf("[offline] Found %d HTML files to index", len(paths))

	if len(paths) == 0 {
		return nil, fmt.Errorf("no Unity HTML files found in %s — make sure the path contains Manual/ or ScriptReference/ folders", root)
	}

	// Process in parallel (folders are fast with random access)
	results := make([]search.Result, 0, len(paths))
	var mu sync.Mutex
	var processed int32
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8) // 8 concurrent workers

	for _, p := range paths {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result, err := parseFolderFile(path, root)
			if err != nil || result == nil {
				atomic.AddInt32(&processed, 1)
				return
			}

			mu.Lock()
			results = append(results, *result)
			mu.Unlock()

			n := int(atomic.AddInt32(&processed, 1))
			if n%100 == 0 && onProgress != nil {
				onProgress(n, len(paths))
			}
		}(p)
	}

	wg.Wait()

	if onProgress != nil {
		onProgress(len(results), len(paths))
	}

	log.Printf("[offline] Indexed %d pages successfully", len(results))
	return results, nil
}

func parseFolderFile(path, root string) (*search.Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	html := string(data)
	title := extractTitle(html)
	content := extractMainContent(html)

	if len(content) < 80 {
		return nil, nil
	}
	if len(content) > 12000 {
		content = content[:12000]
	}

	// Build URL: use online docs.unity3d.com URL so links work everywhere.
	// Fall back to local file:// if we can't determine the online path.
	absPath, _ := filepath.Abs(path)
	url := "file:///" + filepath.ToSlash(absPath)
	rel, relErr := filepath.Rel(root, path)
	if relErr == nil {
		onlineURL := folderPathToURL(rel)
		if strings.HasPrefix(onlineURL, "https://") {
			url = onlineURL
		}
	}

	return &search.Result{
		Title:   title,
		URL:     url,
		Excerpt: content,
		Score:   1.0,
	}, nil
}

// ── File Filtering ────────────────────────────────────────────────────────────

func shouldIndex(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))

	// Must be HTML
	if !strings.HasSuffix(lower, ".html") && !strings.HasSuffix(lower, ".htm") {
		return false
	}

	// Must be in Manual or ScriptReference section
	// Unity ZIP structure: Documentation/en/Manual/*.html
	//                  or: Documentation/en/ScriptReference/*.html
	inManual := strings.Contains(lower, "/manual/") || strings.HasPrefix(lower, "manual/")
	inScript := strings.Contains(lower, "/scriptreference/") || strings.HasPrefix(lower, "scriptreference/")
	if !inManual && !inScript {
		return false
	}

	// Skip nav/search/index pages
	base := strings.ToLower(filepath.Base(path))
	skipNames := []string{
		"index.html", "search.html", "toc.html", "nav.html",
		"30_search", "40_search", "docdata", "genindex",
		"search-results.html", "404.html",
	}
	for _, s := range skipNames {
		if strings.Contains(base, s) {
			return false
		}
	}
	return true
}

// ── HTML Parsing ──────────────────────────────────────────────────────────────

var (
	reTitle      = regexp.MustCompile(`(?i)<title[^>]*>(.*?)</title>`)
	reScript     = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle      = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reNav        = regexp.MustCompile(`(?is)<nav[^>]*>.*?</nav>`)
	reHeader     = regexp.MustCompile(`(?is)<header[^>]*>.*?</header>`)
	reFooter     = regexp.MustCompile(`(?is)<footer[^>]*>.*?</footer>`)
	reSidebar    = regexp.MustCompile(`(?is)<div[^>]*(?:sidebar|toc|nav|menu|breadcrumb)[^>]*>.*?</div>`)
	reComment    = regexp.MustCompile(`(?s)<!--.*?-->`)
	reTags       = regexp.MustCompile(`<[^>]+>`)
	reEntities   = regexp.MustCompile(`&[a-z]+;|&#[0-9]+;`)
	reMultiSpace = regexp.MustCompile(`[ \t]{2,}`)
	reMultiLine  = regexp.MustCompile(`\n{3,}`)
	reMain       = regexp.MustCompile(`(?is)<(?:main|article|div[^>]*(?:content|main|body)[^>]*)>(.*?)</(?:main|article|div)>`)
)

func extractTitle(html string) string {
	m := reTitle.FindStringSubmatch(html)
	if len(m) > 1 {
		t := stripTags(m[1])
		// Remove " - Unity Manual" suffix
		if i := strings.Index(t, " - Unity"); i > 0 {
			t = t[:i]
		}
		if i := strings.Index(t, " | Unity"); i > 0 {
			t = t[:i]
		}
		return strings.TrimSpace(t)
	}
	return "Unity Documentation"
}

func extractMainContent(html string) string {
	// Try to extract just the main content area
	m := reMain.FindStringSubmatch(html)
	if len(m) > 1 && len(m[1]) > 200 {
		html = m[1]
	}

	// Strip non-content elements
	html = reScript.ReplaceAllString(html, " ")
	html = reStyle.ReplaceAllString(html, " ")
	html = reNav.ReplaceAllString(html, " ")
	html = reHeader.ReplaceAllString(html, " ")
	html = reFooter.ReplaceAllString(html, " ")
	html = reSidebar.ReplaceAllString(html, " ")
	html = reComment.ReplaceAllString(html, " ")

	// Add newlines around block elements before stripping tags
	for _, tag := range []string{"p", "li", "h1", "h2", "h3", "h4", "br", "div", "tr", "pre"} {
		html = strings.ReplaceAll(html, "</"+tag+">", "\n")
		html = strings.ReplaceAll(html, "</"+strings.ToUpper(tag)+">", "\n")
	}

	text := stripTags(html)
	text = decodeEntities(text)

	// Clean up whitespace
	lines := strings.Split(text, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) > 15 {
			cleaned = append(cleaned, line)
		}
	}
	text = strings.Join(cleaned, "\n")
	text = reMultiLine.ReplaceAllString(text, "\n\n")

	return strings.TrimSpace(text)
}

func stripTags(html string) string {
	return reTags.ReplaceAllString(html, "")
}

func decodeEntities(s string) string {
	replacements := map[string]string{
		"&nbsp;":  " ",
		"&amp;":   "&",
		"&lt;":    "<",
		"&gt;":    ">",
		"&quot;":  `"`,
		"&#39;":   "'",
		"&mdash;": "—",
		"&ndash;": "–",
		"&hellip;": "...",
		"&copy;":  "©",
	}
	for entity, char := range replacements {
		s = strings.ReplaceAll(s, entity, char)
	}
	// Remove remaining entities
	s = reEntities.ReplaceAllString(s, " ")
	return s
}

// ── URL Helpers ───────────────────────────────────────────────────────────────

func zipPathToURL(zipPath string) string {
	zipPath = filepath.ToSlash(zipPath)
	// Look for Manual/ or ScriptReference/ in the path
	if i := strings.Index(zipPath, "Manual/"); i >= 0 {
		return "https://docs.unity3d.com/" + zipPath[i:]
	}
	if i := strings.Index(zipPath, "ScriptReference/"); i >= 0 {
		return "https://docs.unity3d.com/" + zipPath[i:]
	}
	return zipPath
}

func folderPathToURL(rel string) string {
	rel = filepath.ToSlash(rel)
	// Strip leading "en/" or "Documentation/en/"
	for _, prefix := range []string{"Documentation/en/", "en/", "documentation/en/"} {
		if strings.HasPrefix(strings.ToLower(rel), strings.ToLower(prefix)) {
			rel = rel[len(prefix):]
			break
		}
	}
	if strings.HasPrefix(rel, "Manual/") || strings.HasPrefix(rel, "ScriptReference/") {
		return "https://docs.unity3d.com/" + rel
	}
	return "https://docs.unity3d.com/" + rel
}

func firstSentences(text string, n int) string {
	count := 0
	var sb strings.Builder
	for _, r := range text {
		sb.WriteRune(r)
		if r == '.' || r == '!' || r == '?' {
			count++
			if count >= n {
				break
			}
		}
	}
	return strings.TrimSpace(sb.String())
}

// ── NLU: Query Understanding ──────────────────────────────────────────────────
// This lives here so it can use the same Unity domain knowledge
// as the indexer without a circular import.

// ParsedQuery is the result of understanding a user's question
type ParsedQuery struct {
	Raw         string   // original text
	Normalized  string   // lowercased, cleaned
	Keywords    []string // important terms extracted
	APISymbols  []string // Unity API names found (Rigidbody2D, etc.)
	IsCodeReq   bool     // user wants runnable code
	IsExplain   bool     // user wants explanation
	IsFix       bool     // user has a bug/error
	IsCompare   bool     // comparing two things
	Context2D   bool     // 2D specific
	Context3D   bool     // 3D specific
	SearchTerms []string // final terms to search with (expanded)
}

// stopwords to remove from keyword extraction
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "in": true,
	"to": true, "of": true, "and": true, "or": true, "for": true,
	"on": true, "with": true, "this": true, "that": true, "it": true,
	"be": true, "as": true, "at": true, "by": true, "we": true,
	"how": true, "do": true, "i": true, "you": true, "can": true,
	"what": true, "from": true, "are": true, "use": true, "used": true,
	"my": true, "me": true, "get": true, "make": true, "create": true,
	"want": true, "need": true, "help": true, "using": true, "does": true,
	"would": true, "should": true, "could": true, "will": true, "please": true,
	"just": true, "also": true, "way": true, "some": true, "give": true,
	"show": true, "tell": true, "write": true, "let": true, "set": true,
	"put": true, "try": true, "work": true, "works": true,
}

// Unity API symbol map: lowercase alias → canonical Unity type
var unitySymbols = map[string][]string{
	"rigidbody2d":      {"Rigidbody2D", "Physics2D", "MovePosition", "AddForce", "velocity"},
	"rigidbody":        {"Rigidbody", "Physics", "AddForce", "MovePosition", "velocity"},
	"collider2d":       {"Collider2D", "OnCollisionEnter2D", "OnTriggerEnter2D"},
	"collider":         {"Collider", "OnCollisionEnter", "OnTriggerEnter"},
	"gameobject":       {"GameObject", "Instantiate", "Destroy", "FindObjectOfType"},
	"transform":        {"Transform", "Translate", "Rotate", "position", "rotation"},
	"animator":         {"Animator", "AnimatorController", "SetTrigger", "SetBool", "SetFloat"},
	"animation":        {"Animation", "AnimationClip", "Animator"},
	"coroutine":        {"Coroutine", "StartCoroutine", "StopCoroutine", "IEnumerator", "WaitForSeconds"},
	"prefab":           {"Prefab", "Instantiate", "PrefabUtility"},
	"scene":            {"SceneManager", "LoadScene", "GetActiveScene", "SceneManagement"},
	"canvas":           {"Canvas", "CanvasScaler", "GraphicRaycaster"},
	"ui":               {"Canvas", "Button", "Text", "Image", "Slider", "Toggle", "InputField"},
	"button":           {"Button", "onClick", "UnityEvent"},
	"audiosource":      {"AudioSource", "AudioClip", "PlayOneShot", "Play"},
	"audio":            {"AudioSource", "AudioClip", "AudioMixer", "AudioListener"},
	"camera":           {"Camera", "main", "WorldToScreenPoint", "ScreenToWorldPoint"},
	"navmesh":          {"NavMesh", "NavMeshAgent", "NavMeshSurface", "SetDestination"},
	"navmeshagent":     {"NavMeshAgent", "SetDestination", "remainingDistance", "isStopped"},
	"input":            {"Input", "GetAxis", "GetKey", "GetMouseButton", "InputSystem"},
	"inputsystem":      {"InputSystem", "PlayerInput", "InputAction"},
	"tilemap":          {"Tilemap", "TilemapRenderer", "TileBase", "SetTile"},
	"sprite":           {"Sprite", "SpriteRenderer", "SpriteAtlas"},
	"shader":           {"Shader", "Material", "ShaderGraph"},
	"material":         {"Material", "Shader", "SetColor", "SetFloat"},
	"light":            {"Light", "LightType", "Directional", "Baked"},
	"scriptableobject": {"ScriptableObject", "CreateAssetMenu", "CreateInstance"},
	"playerprefs":      {"PlayerPrefs", "SetInt", "SetFloat", "SetString", "GetInt"},
	"raycast":          {"Physics.Raycast", "Physics2D.Raycast", "RaycastHit", "LayerMask"},
	"physics":          {"Physics", "Rigidbody", "Collider", "Raycast", "OverlapSphere"},
	"physics2d":        {"Physics2D", "Rigidbody2D", "Collider2D", "Raycast"},
	"invoke":           {"Invoke", "InvokeRepeating", "CancelInvoke"},
	"lerp":             {"Lerp", "Vector3.Lerp", "Mathf.Lerp", "MoveTowards"},
	"instantiate":      {"Instantiate", "Prefab", "GameObject"},
	"destroy":          {"Destroy", "DestroyImmediate", "Despawn"},
	"monobehaviour":    {"MonoBehaviour", "Start", "Update", "Awake", "OnEnable"},
	"update":           {"Update", "FixedUpdate", "LateUpdate", "Time.deltaTime"},
	"fixedupdate":      {"FixedUpdate", "Rigidbody", "Physics", "Time.fixedDeltaTime"},
	"collision":        {"OnCollisionEnter", "OnCollisionStay", "OnCollisionExit", "Collision"},
	"trigger":          {"OnTriggerEnter", "OnTriggerStay", "OnTriggerExit", "Collider"},
	"movement":         {"Rigidbody", "Transform", "Translate", "MovePosition", "velocity"},
	"move":             {"Rigidbody", "Transform", "Translate", "MovePosition", "velocity"},
	"jump":             {"Rigidbody2D", "AddForce", "velocity", "ForceMode2D.Impulse"},
	"gravity":          {"Rigidbody", "Physics", "gravityScale", "useGravity"},
	"rotation":         {"Transform", "Quaternion", "Rotate", "Euler", "LookAt"},
	"spawn":            {"Instantiate", "Prefab", "ObjectPool"},
	"pool":             {"ObjectPool", "Instantiate", "Destroy"},
	"save":             {"PlayerPrefs", "JsonUtility", "BinaryFormatter", "File"},
	"load":             {"SceneManager", "LoadScene", "Resources.Load", "AssetBundle"},
	"singleton":        {"MonoBehaviour", "DontDestroyOnLoad", "Instance"},
	"event":            {"UnityEvent", "Action", "EventSystem", "delegate"},
	"delegate":         {"Action", "Func", "delegate", "UnityEvent"},
	"interface":        {"IEnumerator", "IComparable", "interface"},
	"abstract":         {"abstract", "MonoBehaviour", "ScriptableObject"},
	"coroutines":       {"Coroutine", "StartCoroutine", "IEnumerator", "WaitForSeconds"},
}

// UnderstandQuery parses a raw user query into a structured ParsedQuery
func UnderstandQuery(raw string) ParsedQuery {
	pq := ParsedQuery{Raw: raw}
	pq.Normalized = strings.ToLower(strings.TrimSpace(raw))

	// Detect context
	pq.Context2D = strings.Contains(pq.Normalized, "2d") ||
		strings.Contains(pq.Normalized, "rigidbody2d") ||
		strings.Contains(pq.Normalized, "collider2d") ||
		strings.Contains(pq.Normalized, "sprite") ||
		strings.Contains(pq.Normalized, "tilemap")

	pq.Context3D = strings.Contains(pq.Normalized, "3d") ||
		(strings.Contains(pq.Normalized, "rigidbody") && !strings.Contains(pq.Normalized, "rigidbody2d")) ||
		strings.Contains(pq.Normalized, "navmesh") ||
		strings.Contains(pq.Normalized, "shader") ||
		strings.Contains(pq.Normalized, "mesh")

	// Detect intent flags
	pq.IsCodeReq = containsAny(pq.Normalized, []string{
		"write", "script", "code", "example", "how do i", "how to",
		"show me", "give me", "can you make", "make me",
	})
	pq.IsExplain = containsAny(pq.Normalized, []string{
		"what is", "what are", "explain", "what does", "tell me",
		"describe", "definition", "what's", "whats",
	})
	pq.IsFix = containsAny(pq.Normalized, []string{
		"error", "not working", "broken", "fix", "bug", "exception",
		"null", "crash", "doesn't work", "wont work", "issue", "problem",
	})
	pq.IsCompare = containsAny(pq.Normalized, []string{
		"difference", "vs", "versus", "or ", "which", "better",
		"when to use", "compared",
	})

	// Extract keywords (non-stopword tokens)
	tokens := tokenize(pq.Normalized)
	seen := map[string]bool{}
	for _, tok := range tokens {
		if !stopWords[tok] && len(tok) >= 2 && !seen[tok] {
			seen[tok] = true
			pq.Keywords = append(pq.Keywords, tok)
		}
	}

	// Find Unity API symbols mentioned
	symbolSeen := map[string]bool{}
	for alias, symbols := range unitySymbols {
		if strings.Contains(pq.Normalized, alias) {
			for _, sym := range symbols {
				if !symbolSeen[sym] {
					symbolSeen[sym] = true
					pq.APISymbols = append(pq.APISymbols, sym)
				}
			}
		}
	}

	// Build expanded search terms
	searchSet := map[string]bool{}
	for _, kw := range pq.Keywords {
		searchSet[kw] = true
	}
	for _, sym := range pq.APISymbols {
		lower := strings.ToLower(sym)
		searchSet[lower] = true
	}
	for term := range searchSet {
		pq.SearchTerms = append(pq.SearchTerms, term)
	}

	return pq
}

// EnhancedQuery builds a single query string from a ParsedQuery
// that the search engine will score better
func (pq *ParsedQuery) EnhancedQuery() string {
	// Weight API symbols more heavily by repeating them
	parts := make([]string, 0, len(pq.SearchTerms)+len(pq.APISymbols))
	parts = append(parts, pq.Keywords...)
	parts = append(parts, pq.APISymbols...) // repeat for boost
	return strings.Join(parts, " ")
}

// Summary returns a human-readable description of what was understood
func (pq *ParsedQuery) Summary() string {
	parts := []string{}
	if pq.IsCodeReq {
		parts = append(parts, "code request")
	}
	if pq.IsExplain {
		parts = append(parts, "explanation")
	}
	if pq.IsFix {
		parts = append(parts, "debugging")
	}
	if pq.Context2D {
		parts = append(parts, "2D")
	}
	if pq.Context3D {
		parts = append(parts, "3D")
	}
	if len(pq.APISymbols) > 0 {
		parts = append(parts, "API: "+strings.Join(pq.APISymbols[:min(3, len(pq.APISymbols))], ", "))
	}
	if len(parts) == 0 {
		return "general query"
	}
	return strings.Join(parts, " · ")
}

// ── Utilities ─────────────────────────────────────────────────────────────────

func tokenize(text string) []string {
	var tokens []string
	var current strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else {
			if current.Len() >= 2 {
				tokens = append(tokens, current.String())
			}
			current.Reset()
		}
	}
	if current.Len() >= 2 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func containsAny(s string, patterns []string) bool {
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// FormatDuration formats a duration nicely
func FormatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
