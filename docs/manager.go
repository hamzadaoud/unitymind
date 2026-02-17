package docs

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"unitymind/search"
)

// DocLink is a title+URL pair returned to the UI
type DocLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Manager handles fetching Unity documentation
type Manager struct {
	cacheDir string
	client   *http.Client
}

func NewManager(cacheDir string) *Manager {
	return &Manager{
		cacheDir: cacheDir,
		client:   &http.Client{Timeout: 12 * time.Second},
	}
}

// ── Keyword → specific doc URL mapping ───────────────────────────────────────
// Instead of trusting Unity's search page (which returns junk),
// we map keywords directly to the exact doc pages that answer them.
// This is the "smart routing" layer.

type docRoute struct {
	keywords []string // any of these in the query triggers this route
	urls     []string // fetch these pages (in order)
}

var routes = []docRoute{
	// Audio
	{
		keywords: []string{"sound", "audio", "music", "audiosource", "audioclip", "play sound", "sfx", "sound effect", "background music"},
		urls: []string{
			"https://docs.unity3d.com/Manual/AudioOverview.html",
			"https://docs.unity3d.com/ScriptReference/AudioSource.html",
			"https://docs.unity3d.com/ScriptReference/AudioSource.PlayOneShot.html",
		},
	},
	// Movement / Rigidbody 2D
	{
		keywords: []string{"rigidbody2d", "move 2d", "movement 2d", "2d movement", "2d player", "player 2d", "platformer"},
		urls: []string{
			"https://docs.unity3d.com/Manual/RigidbodiesOverview.html",
			"https://docs.unity3d.com/ScriptReference/Rigidbody2D.html",
			"https://docs.unity3d.com/ScriptReference/Rigidbody2D.MovePosition.html",
		},
	},
	// Movement / Rigidbody 3D
	{
		keywords: []string{"rigidbody", "move 3d", "movement 3d", "3d movement", "physics movement", "addforce"},
		urls: []string{
			"https://docs.unity3d.com/Manual/RigidbodiesOverview.html",
			"https://docs.unity3d.com/ScriptReference/Rigidbody.html",
			"https://docs.unity3d.com/ScriptReference/Rigidbody.AddForce.html",
		},
	},
	// Transform movement
	{
		keywords: []string{"transform move", "translate", "move gameobject", "move object", "move player"},
		urls: []string{
			"https://docs.unity3d.com/ScriptReference/Transform.html",
			"https://docs.unity3d.com/ScriptReference/Transform.Translate.html",
		},
	},
	// Collision 2D
	{
		keywords: []string{"collision 2d", "collider 2d", "oncollisionenter2d", "ontriggerenter2d", "trigger 2d"},
		urls: []string{
			"https://docs.unity3d.com/Manual/CollidersOverview.html",
			"https://docs.unity3d.com/ScriptReference/MonoBehaviour.OnCollisionEnter2D.html",
			"https://docs.unity3d.com/ScriptReference/MonoBehaviour.OnTriggerEnter2D.html",
		},
	},
	// Collision 3D
	{
		keywords: []string{"collision", "collider", "oncollisionenter", "ontriggerenter", "trigger", "detect collision"},
		urls: []string{
			"https://docs.unity3d.com/Manual/CollidersOverview.html",
			"https://docs.unity3d.com/ScriptReference/MonoBehaviour.OnCollisionEnter.html",
			"https://docs.unity3d.com/ScriptReference/MonoBehaviour.OnTriggerEnter.html",
		},
	},
	// Coroutines
	{
		keywords: []string{"coroutine", "waitforseconds", "ienumerator", "startcoroutine", "delay", "wait seconds"},
		urls: []string{
			"https://docs.unity3d.com/Manual/Coroutines.html",
			"https://docs.unity3d.com/ScriptReference/MonoBehaviour.StartCoroutine.html",
			"https://docs.unity3d.com/ScriptReference/WaitForSeconds.html",
		},
	},
	// Animation
	{
		keywords: []string{"animator", "animation", "animat", "state machine", "blend tree", "settrigger", "setbool"},
		urls: []string{
			"https://docs.unity3d.com/Manual/AnimatorControllers.html",
			"https://docs.unity3d.com/ScriptReference/Animator.html",
			"https://docs.unity3d.com/ScriptReference/Animator.SetTrigger.html",
		},
	},
	// Scene loading
	{
		keywords: []string{"load scene", "loadscene", "scenemanager", "change scene", "next scene", "scene transition"},
		urls: []string{
			"https://docs.unity3d.com/Manual/MultiSceneEditing.html",
			"https://docs.unity3d.com/ScriptReference/SceneManagement.SceneManager.html",
			"https://docs.unity3d.com/ScriptReference/SceneManagement.SceneManager.LoadScene.html",
		},
	},
	// Prefabs & Instantiate
	{
		keywords: []string{"prefab", "instantiate", "spawn", "create object"},
		urls: []string{
			"https://docs.unity3d.com/Manual/Prefabs.html",
			"https://docs.unity3d.com/ScriptReference/Object.Instantiate.html",
		},
	},
	// Input
	{
		keywords: []string{"input", "keyboard", "mouse", "getkey", "getaxis", "button press", "input system"},
		urls: []string{
			"https://docs.unity3d.com/Manual/Input.html",
			"https://docs.unity3d.com/ScriptReference/Input.html",
			"https://docs.unity3d.com/ScriptReference/Input.GetAxis.html",
		},
	},
	// UI / Canvas
	{
		keywords: []string{"ui", "canvas", "button", "text", "slider", "image", "ugui", "ui element"},
		urls: []string{
			"https://docs.unity3d.com/Manual/UISystem.html",
			"https://docs.unity3d.com/ScriptReference/UI.Button.html",
		},
	},
	// Camera
	{
		keywords: []string{"camera", "main camera", "follow camera", "cinemachine"},
		urls: []string{
			"https://docs.unity3d.com/Manual/CamerasOverview.html",
			"https://docs.unity3d.com/ScriptReference/Camera.html",
		},
	},
	// NavMesh / AI
	{
		keywords: []string{"navmesh", "pathfinding", "ai", "navmeshagent", "navigation", "enemy follow"},
		urls: []string{
			"https://docs.unity3d.com/Manual/Navigation.html",
			"https://docs.unity3d.com/ScriptReference/AI.NavMeshAgent.html",
		},
	},
	// Raycasting
	{
		keywords: []string{"raycast", "ray", "linecast", "physics.raycast", "shooting", "hit detection"},
		urls: []string{
			"https://docs.unity3d.com/ScriptReference/Physics.Raycast.html",
			"https://docs.unity3d.com/ScriptReference/Physics2D.Raycast.html",
		},
	},
	// Saving / PlayerPrefs
	{
		keywords: []string{"save", "load", "playerprefs", "persist", "store data", "high score", "settings save"},
		urls: []string{
			"https://docs.unity3d.com/ScriptReference/PlayerPrefs.html",
			"https://docs.unity3d.com/ScriptReference/JsonUtility.html",
		},
	},
	// Destroy
	{
		keywords: []string{"destroy", "delete object", "remove object", "despawn"},
		urls: []string{
			"https://docs.unity3d.com/ScriptReference/Object.Destroy.html",
		},
	},
	// Object pooling
	{
		keywords: []string{"object pool", "pooling", "pool"},
		urls: []string{
			"https://docs.unity3d.com/ScriptReference/Pool.ObjectPool_1.html",
		},
	},
	// Lighting
	{
		keywords: []string{"light", "lighting", "bake", "shadow", "global illumination"},
		urls: []string{
			"https://docs.unity3d.com/Manual/LightingInUnity.html",
			"https://docs.unity3d.com/ScriptReference/Light.html",
		},
	},
	// Sprites / 2D
	{
		keywords: []string{"sprite", "spriterenderer", "sprite sheet", "2d art"},
		urls: []string{
			"https://docs.unity3d.com/Manual/Sprites.html",
			"https://docs.unity3d.com/ScriptReference/SpriteRenderer.html",
		},
	},
	// Tilemap
	{
		keywords: []string{"tilemap", "tile", "tilelayer"},
		urls: []string{
			"https://docs.unity3d.com/Manual/Tilemap.html",
			"https://docs.unity3d.com/ScriptReference/Tilemaps.Tilemap.html",
		},
	},
	// ScriptableObject
	{
		keywords: []string{"scriptableobject", "scriptable object", "data container", "so asset"},
		urls: []string{
			"https://docs.unity3d.com/Manual/class-ScriptableObject.html",
			"https://docs.unity3d.com/ScriptReference/ScriptableObject.html",
		},
	},
	// Time / deltaTime
	{
		keywords: []string{"time.deltatime", "deltatime", "framerate", "fps independent", "time scale"},
		urls: []string{
			"https://docs.unity3d.com/ScriptReference/Time.html",
		},
	},
	// Update / FixedUpdate
	{
		keywords: []string{"update vs fixedupdate", "fixedupdate", "lateupdate", "monobehaviour lifecycle", "execution order"},
		urls: []string{
			"https://docs.unity3d.com/Manual/ExecutionOrder.html",
			"https://docs.unity3d.com/ScriptReference/MonoBehaviour.FixedUpdate.html",
		},
	},
	// Tags & Layers
	{
		keywords: []string{"tag", "layer", "comparetag", "layermask"},
		urls: []string{
			"https://docs.unity3d.com/Manual/Tags.html",
			"https://docs.unity3d.com/ScriptReference/GameObject.CompareTag.html",
		},
	},
	// GetComponent
	{
		keywords: []string{"getcomponent", "find component", "access component"},
		urls: []string{
			"https://docs.unity3d.com/ScriptReference/Component.GetComponent.html",
		},
	},
	// Events / Delegates
	{
		keywords: []string{"unityevent", "event", "delegate", "action", "callback"},
		urls: []string{
			"https://docs.unity3d.com/Manual/UnityEvents.html",
			"https://docs.unity3d.com/ScriptReference/Events.UnityEvent.html",
		},
	},
	// Build
	{
		keywords: []string{"build", "publish", "export", "release", "build settings", "platform"},
		urls: []string{
			"https://docs.unity3d.com/Manual/BuildSettings.html",
		},
	},
	// Shader / Material
	{
		keywords: []string{"shader", "material", "shadergraph", "urp shader", "hdrp"},
		urls: []string{
			"https://docs.unity3d.com/Manual/Shaders.html",
			"https://docs.unity3d.com/ScriptReference/Material.html",
		},
	},
}

// routeQuery finds the best matching doc URLs for a query
func routeQuery(query string) []string {
	q := strings.ToLower(query)
	bestScore := 0
	var bestURLs []string

	for _, route := range routes {
		score := 0
		for _, kw := range route.keywords {
			if strings.Contains(q, kw) {
				// Longer keyword match = higher confidence
				score += len(strings.Fields(kw))
			}
		}
		if score > bestScore {
			bestScore = score
			bestURLs = route.urls
		}
	}
	return bestURLs
}

// ── Core doc list (fallback fetcher) ─────────────────────────────────────────

var coreDocs = []string{
	"https://docs.unity3d.com/Manual/ScriptingSection.html",
	"https://docs.unity3d.com/Manual/CreatingAndUsingScripts.html",
	"https://docs.unity3d.com/Manual/ControllingGameObjectsComponents.html",
	"https://docs.unity3d.com/Manual/EventSystem.html",
	"https://docs.unity3d.com/Manual/Coroutines.html",
	"https://docs.unity3d.com/Manual/ExecutionOrder.html",
	"https://docs.unity3d.com/Manual/RigidbodiesOverview.html",
	"https://docs.unity3d.com/Manual/CollidersOverview.html",
	"https://docs.unity3d.com/Manual/Physics2DReference.html",
	"https://docs.unity3d.com/Manual/Unity2D.html",
	"https://docs.unity3d.com/Manual/Sprites.html",
	"https://docs.unity3d.com/Manual/Tilemap.html",
	"https://docs.unity3d.com/Manual/Animator.html",
	"https://docs.unity3d.com/Manual/LightingInUnity.html",
	"https://docs.unity3d.com/Manual/AnimationSection.html",
	"https://docs.unity3d.com/Manual/AnimatorControllers.html",
	"https://docs.unity3d.com/Manual/UISystem.html",
	"https://docs.unity3d.com/Manual/AudioOverview.html",
	"https://docs.unity3d.com/ScriptReference/AudioSource.html",
	"https://docs.unity3d.com/Manual/MultiSceneEditing.html",
	"https://docs.unity3d.com/ScriptReference/SceneManagement.SceneManager.html",
	"https://docs.unity3d.com/Manual/Input.html",
	"https://docs.unity3d.com/Manual/Prefabs.html",
	"https://docs.unity3d.com/Manual/Navigation.html",
	"https://docs.unity3d.com/Manual/BuildSettings.html",
	"https://docs.unity3d.com/ScriptReference/Rigidbody2D.html",
	"https://docs.unity3d.com/ScriptReference/Rigidbody.html",
	"https://docs.unity3d.com/ScriptReference/PlayerPrefs.html",
	"https://docs.unity3d.com/ScriptReference/Physics.Raycast.html",
	"https://docs.unity3d.com/ScriptReference/MonoBehaviour.OnCollisionEnter.html",
	"https://docs.unity3d.com/ScriptReference/MonoBehaviour.OnTriggerEnter.html",
	"https://docs.unity3d.com/Manual/UnityEvents.html",
	"https://docs.unity3d.com/Manual/Tags.html",
	"https://docs.unity3d.com/ScriptReference/Time.html",
	"https://docs.unity3d.com/ScriptReference/Object.Instantiate.html",
	"https://docs.unity3d.com/ScriptReference/Object.Destroy.html",
	"https://docs.unity3d.com/Manual/class-ScriptableObject.html",
	"https://docs.unity3d.com/Manual/OptimizingGraphicsPerformance.html",
	"https://docs.unity3d.com/Manual/MobileOptimizationGraphicsMethods.html",
}

func (m *Manager) FetchCoreDocs() ([]search.Result, error) {
	results := make([]search.Result, 0, len(coreDocs))
	for _, u := range coreDocs {
		r, err := m.fetchPage(u)
		if err != nil {
			continue
		}
		results = append(results, r)
		time.Sleep(100 * time.Millisecond)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("could not fetch any docs (offline?)")
	}
	return results, nil
}

// SearchLive routes the query to specific known Unity doc pages
// instead of trusting Unity's search page (which returns generic nav junk).
func (m *Manager) SearchLive(query string) ([]search.Result, error) {
	// Step 1: try our keyword router first
	urls := routeQuery(query)

	// Step 2: if no route matched, fall back to Unity's search API
	if len(urls) == 0 {
		urls = m.unitySearchAPI(query)
	}

	if len(urls) == 0 {
		return nil, fmt.Errorf("no matching docs for: %s", query)
	}

	// Fetch and parse matched pages
	results := make([]search.Result, 0, len(urls))
	for i, u := range urls {
		if i >= 3 {
			break
		}
		r, err := m.fetchPage(u)
		if err != nil {
			continue
		}
		results = append(results, r)
		time.Sleep(100 * time.Millisecond)
	}
	return results, nil
}

// unitySearchAPI tries to get specific page links from Unity's search endpoint
func (m *Manager) unitySearchAPI(query string) []string {
	searchURL := "https://docs.unity3d.com/search/?q=" + url.QueryEscape(query)
	resp, err := m.client.Get(searchURL)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	links := extractLinks(string(body), "https://docs.unity3d.com")
	// Filter out generic/homepage links
	var specific []string
	for _, l := range links {
		if strings.Contains(l, "/Manual/") || strings.Contains(l, "/ScriptReference/") {
			base := l[strings.LastIndex(l, "/")+1:]
			if base != "index.html" && base != "" && !strings.HasPrefix(base, "Unity-") {
				specific = append(specific, l)
			}
		}
	}
	return specific
}

// fetchPage downloads a doc page and extracts FULL clean text (not just 400 chars)
func (m *Manager) fetchPage(pageURL string) (search.Result, error) {
	resp, err := m.client.Get(pageURL)
	if err != nil {
		return search.Result{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return search.Result{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, pageURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return search.Result{}, err
	}

	html := string(body)
	title := extractTitle(html)
	content := stripHTML(html)
	content = cleanContent(content)

	if len(content) < 50 {
		return search.Result{}, fmt.Errorf("page too short: %s", pageURL)
	}

	// Keep up to 10000 chars — enough for the brain to synthesize a real answer
	if len(content) > 10000 {
		content = content[:10000]
	}

	return search.Result{
		Title:   title,
		URL:     pageURL,
		Excerpt: content, // full content, not just 400 chars
		Score:   1.0,
	}, nil
}

// ── HTML helpers ──────────────────────────────────────────────────────────────

var (
	reTags    = regexp.MustCompile(`<[^>]+>`)
	reScript  = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle   = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reNav     = regexp.MustCompile(`(?is)<nav[^>]*>.*?</nav>`)
	reHeader  = regexp.MustCompile(`(?is)<header[^>]*>.*?</header>`)
	reFooter  = regexp.MustCompile(`(?is)<footer[^>]*>.*?</footer>`)
	reComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	reSpaces  = regexp.MustCompile(`\s{3,}`)
	reTitle   = regexp.MustCompile(`(?i)<title[^>]*>(.*?)</title>`)
	reAnchors = regexp.MustCompile(`href="(/[^"]+)"`)
)

func extractTitle(html string) string {
	m := reTitle.FindStringSubmatch(html)
	if len(m) > 1 {
		t := strings.TrimSpace(stripHTML(m[1]))
		// Remove "- Unity Manual" etc from title
		for _, suffix := range []string{" - Unity Manual", " - Unity Scripting API", " | Unity"} {
			if i := strings.Index(t, suffix); i > 0 {
				t = t[:i]
			}
		}
		return t
	}
	return "Unity Documentation"
}

func stripHTML(html string) string {
	html = reScript.ReplaceAllString(html, " ")
	html = reStyle.ReplaceAllString(html, " ")
	html = reNav.ReplaceAllString(html, " ")
	html = reHeader.ReplaceAllString(html, " ")
	html = reFooter.ReplaceAllString(html, " ")
	html = reComment.ReplaceAllString(html, " ")
	html = reTags.ReplaceAllString(html, " ")
	r := strings.NewReplacer(
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<",
		"&gt;", ">", "&quot;", `"`, "&#39;", "'",
	)
	return r.Replace(html)
}

func cleanContent(text string) string {
	lines := strings.Split(text, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) > 20 {
			cleaned = append(cleaned, line)
		}
	}
	result := strings.Join(cleaned, "\n")
	result = reSpaces.ReplaceAllString(result, "\n\n")
	return strings.TrimSpace(result)
}

func extractLinks(html, baseURL string) []string {
	matches := reAnchors.FindAllStringSubmatch(html, -1)
	seen := map[string]bool{}
	var links []string
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		path := m[1]
		if !strings.Contains(path, "/Manual/") && !strings.Contains(path, "/ScriptReference/") {
			continue
		}
		full := baseURL + path
		if !seen[full] {
			seen[full] = true
			links = append(links, full)
		}
	}
	return links
}
