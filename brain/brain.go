// Package brain synthesizes human-readable answers from doc content + built-in Unity knowledge.
// Priority: built-in template knowledge FIRST, then synthesize from docs.
// No API calls needed — all answers are pattern-matched and generated locally.
package brain

import (
	"fmt"
	"strings"
	"unicode"

	"unitymind/search"
)

// HistoryEntry is a past conversation message
type HistoryEntry struct {
	Role    string
	Content string
}

// Synthesize is the main entry point.
// It first tries to match a built-in answer template (instant, no doc needed),
// then falls back to synthesizing from the doc content passed in.
func Synthesize(query string, results []search.Result, history []HistoryEntry) string {
	q := strings.ToLower(strings.TrimSpace(query))

	// Very short/vague queries get a helpful prompt
	if len(strings.Fields(q)) <= 1 && q != "how" {
		return fmt.Sprintf("Could you give me a bit more detail? For example: *\"How do I use %s in Unity?\"* or *\"Write me a script for %s\"*", q, q)
	}
	if q == "how" || q == "how do i" || q == "help" || q == "what" {
		return "Sure! What do you need help with? For example:\n- *\"How do I play a sound effect?\"*\n- *\"Write a script to move with Rigidbody2D\"*\n- *\"What is a Coroutine?\"*"
	}

	// ── Step 1: Try built-in knowledge base first ──────────────────────────
	if answer := builtinAnswer(q, query); answer != "" {
		return answer
	}

	// ── Step 2: Synthesize from doc content ───────────────────────────────
	if len(results) == 0 {
		return "I couldn't find anything specific about that. Try rephrasing, or click 🔄 to refresh the docs index."
	}
	intent := detectIntent(q)
	topic := extractTopic(q)
	ctx := buildContext(results)
	return synthesizeFromDocs(intent, q, topic, ctx, results)
}

// ── Built-in Knowledge Base ───────────────────────────────────────────────────
// These give perfect answers regardless of what the search engine returned.
// Covers the 30 most common Unity questions.

func builtinAnswer(q, raw string) string {
	switch {

	// ── AUDIO ────────────────────────────────────────────────────────────────
	case matchAny(q, "play sound", "sound effect", "audio", "audiosource", "play music", "sfx", "play clip", "music"):
		if isCodeRequest(q) {
			return `Here's how to **play a sound effect** in Unity:

` + "```csharp" + `
using UnityEngine;

public class SoundExample : MonoBehaviour
{
    // Drag your AudioClip into this in the Inspector
    public AudioClip coinSound;
    public AudioClip backgroundMusic;

    private AudioSource audioSource;

    void Start()
    {
        audioSource = GetComponent<AudioSource>();

        // Play background music on loop
        audioSource.clip = backgroundMusic;
        audioSource.loop = true;
        audioSource.Play();
    }

    // Call this from anywhere to play a one-shot SFX
    public void PlayCoin()
    {
        // PlayOneShot is best for sound effects — doesn't interrupt other sounds
        audioSource.PlayOneShot(coinSound);
    }

    // Play at a specific position in 3D space
    public void PlayAtPosition(Vector3 pos)
    {
        AudioSource.PlayClipAtPoint(coinSound, pos);
    }
}
` + "```" + `

**Setup steps:**
1. Add an **AudioSource** component to your GameObject
2. Assign your AudioClip in the Inspector (drag the audio file onto the field)
3. Attach this script to the same GameObject
4. Call ` + "`PlayCoin()`" + ` from wherever you need it (e.g. ` + "`OnCollisionEnter`" + `)

**AudioSource vs AudioClip:**
- **AudioClip** = the sound file (.wav, .mp3, .ogg)
- **AudioSource** = the speaker that plays it (one per GameObject)
- Use ` + "`PlayOneShot`" + ` for SFX (fire and forget), ` + "`audioSource.Play()`" + ` for music/loops`
		}
		return `**Playing sounds in Unity** uses two components:

- **AudioClip** — the actual sound file you import (.wav, .mp3, .ogg)
- **AudioSource** — the component that plays it (attached to a GameObject)

**Quick setup:**
1. Add an **AudioSource** component to your GameObject
2. In the Inspector, drag your audio file into the *AudioClip* slot
3. Call ` + "`audioSource.Play()`" + ` in your script, or use ` + "`audioSource.PlayOneShot(clip)`" + ` for sound effects

**Key methods:**
- ` + "`audioSource.Play()`" + ` — plays the assigned clip
- ` + "`audioSource.PlayOneShot(clip)`" + ` — plays a clip once without interrupting others (best for SFX)
- ` + "`AudioSource.PlayClipAtPoint(clip, position)`" + ` — plays at a world position (great for 3D games)
- ` + "`audioSource.Stop()`" + ` / ` + "`audioSource.Pause()`" + `

Want me to write the full C# script for this?`

	// ── RIGIDBODY 2D MOVEMENT ─────────────────────────────────────────────────
	case matchAny(q, "rigidbody2d", "move 2d", "2d movement", "2d player move", "movement 2d", "platformer move"):
		return `Here's a **Rigidbody2D movement** script for a 2D game:

` + "```csharp" + `
using UnityEngine;

public class PlayerMovement2D : MonoBehaviour
{
    public float moveSpeed = 5f;
    public float jumpForce = 10f;

    private Rigidbody2D rb;
    private bool isGrounded;

    void Start()
    {
        rb = GetComponent<Rigidbody2D>();
    }

    void FixedUpdate()
    {
        // Horizontal movement (A/D or arrow keys)
        float moveX = Input.GetAxisRaw("Horizontal");
        rb.linearVelocity = new Vector2(moveX * moveSpeed, rb.linearVelocity.y);
    }

    void Update()
    {
        // Jump
        if (Input.GetKeyDown(KeyCode.Space) && isGrounded)
        {
            rb.AddForce(Vector2.up * jumpForce, ForceMode2D.Impulse);
        }
    }

    void OnCollisionEnter2D(Collision2D col)
    {
        if (col.gameObject.CompareTag("Ground"))
            isGrounded = true;
    }

    void OnCollisionExit2D(Collision2D col)
    {
        if (col.gameObject.CompareTag("Ground"))
            isGrounded = false;
    }
}
` + "```" + `

**Setup:**
1. Add **Rigidbody2D** + **Collider2D** to your player
2. Tag your ground objects as *"Ground"*
3. Attach this script to the player
4. Tune ` + "`moveSpeed`" + ` and ` + "`jumpForce`" + ` in the Inspector

**Why ` + "`FixedUpdate`" + ` for movement?** Physics runs at a fixed timestep (50/sec) — putting movement in ` + "`Update`" + ` makes it framerate-dependent and jittery.`

	// ── RIGIDBODY 3D MOVEMENT ─────────────────────────────────────────────────
	case matchAny(q, "rigidbody move", "3d movement", "move 3d", "3d player", "addforce move") && !matchAny(q, "2d"):
		return `Here's a **Rigidbody 3D movement** script:

` + "```csharp" + `
using UnityEngine;

public class PlayerMovement3D : MonoBehaviour
{
    public float moveSpeed = 5f;
    public float jumpForce = 8f;

    private Rigidbody rb;

    void Start()
    {
        rb = GetComponent<Rigidbody>();
    }

    void FixedUpdate()
    {
        float moveX = Input.GetAxisRaw("Horizontal");
        float moveZ = Input.GetAxisRaw("Vertical");

        Vector3 direction = new Vector3(moveX, 0, moveZ).normalized;
        rb.MovePosition(rb.position + direction * moveSpeed * Time.fixedDeltaTime);
    }

    void Update()
    {
        if (Input.GetKeyDown(KeyCode.Space))
            rb.AddForce(Vector3.up * jumpForce, ForceMode.Impulse);
    }
}
` + "```" + `

**Setup:** Add **Rigidbody** + **Collider** to your player, attach this script. That's it.`

	// ── TRANSFORM MOVEMENT ────────────────────────────────────────────────────
	case matchAny(q, "transform move", "move gameobject", "move object without physics", "move without rigidbody", "translate"):
		return `Here's movement using **Transform** (no physics — good for non-physics objects):

` + "```csharp" + `
using UnityEngine;

public class MoveWithTransform : MonoBehaviour
{
    public float speed = 5f;

    void Update()
    {
        float h = Input.GetAxis("Horizontal");
        float v = Input.GetAxis("Vertical");

        // Always multiply by Time.deltaTime to stay framerate-independent
        transform.Translate(new Vector3(h, 0, v) * speed * Time.deltaTime);
    }
}
` + "```" + `

⚠️ **Transform vs Rigidbody:** Transform movement bypasses physics completely — objects won't push each other or respond to gravity. Use **Rigidbody** if you need real physics interactions.`

	// ── COROUTINES ────────────────────────────────────────────────────────────
	case matchAny(q, "coroutine", "waitforseconds", "ienumerator", "startcoroutine", "delay", "wait second", "wait for"):
		return `**Coroutines** let you pause code and resume it later — without freezing the game:

` + "```csharp" + `
using UnityEngine;
using System.Collections;

public class CoroutineExample : MonoBehaviour
{
    void Start()
    {
        StartCoroutine(CountDown(3));
    }

    IEnumerator CountDown(int seconds)
    {
        Debug.Log("Starting countdown...");

        for (int i = seconds; i > 0; i--)
        {
            Debug.Log(i + "...");
            yield return new WaitForSeconds(1f); // wait 1 second, game keeps running
        }

        Debug.Log("Go!");
    }

    // Other useful yield options:
    IEnumerator OtherExamples()
    {
        yield return null;                          // wait one frame
        yield return new WaitForFixedUpdate();      // wait for next physics step
        yield return new WaitUntil(() => isReady);  // wait until condition is true
        yield return new WaitWhile(() => isLoading);// wait while condition is true
    }

    bool isReady = false;
    bool isLoading = true;

    void StopExample()
    {
        StopCoroutine(CountDown(3)); // stop a specific coroutine
        StopAllCoroutines();         // stop all on this GameObject
    }
}
` + "```" + `

**When to use coroutines:** Timed events, spawning waves, fade effects, anything that needs "wait X seconds then do Y" without blocking the game.`

	// ── COLLISION ─────────────────────────────────────────────────────────────
	case matchAny(q, "collision", "oncollisionenter", "detect collision", "collide", "hit detection") && !matchAny(q, "2d"):
		return `**3D Collision detection** in Unity:

` + "```csharp" + `
using UnityEngine;

public class CollisionExample : MonoBehaviour
{
    // Called when this object physically hits another
    void OnCollisionEnter(Collision collision)
    {
        Debug.Log("Hit: " + collision.gameObject.name);

        if (collision.gameObject.CompareTag("Enemy"))
        {
            Debug.Log("Hit an enemy!");
            // Deal damage, play sound, etc.
        }
    }

    void OnCollisionStay(Collision collision) { /* called every frame while touching */ }
    void OnCollisionExit(Collision collision) { /* called when they separate */ }

    // Trigger zone (Collider must have "Is Trigger" checked)
    void OnTriggerEnter(Collider other)
    {
        Debug.Log("Entered trigger: " + other.name);
    }

    void OnTriggerExit(Collider other) { }
}
` + "```" + `

**Requirements:** At least one object needs a **Rigidbody**. Both need **Colliders**.

**Collision vs Trigger:**
- **Collision** → physical bounce (walls, floors, enemies)
- **Trigger** → invisible zone, objects pass through (pickups, damage areas)`

	// ── COLLISION 2D ─────────────────────────────────────────────────────────
	case matchAny(q, "collision 2d", "oncollisionenter2d", "2d collision", "trigger 2d", "ontriggerenter2d"):
		return `**2D Collision detection:**

` + "```csharp" + `
using UnityEngine;

public class Collision2DExample : MonoBehaviour
{
    void OnCollisionEnter2D(Collision2D collision)
    {
        Debug.Log("Hit: " + collision.gameObject.name);

        if (collision.gameObject.CompareTag("Enemy"))
            TakeDamage();
    }

    void OnCollisionExit2D(Collision2D collision) { }
    void OnCollisionStay2D(Collision2D collision) { }

    // Trigger (enable "Is Trigger" on the Collider2D)
    void OnTriggerEnter2D(Collider2D other)
    {
        if (other.CompareTag("Coin"))
        {
            Destroy(other.gameObject); // collect the coin
            AddScore(10);
        }
    }

    void TakeDamage() { }
    void AddScore(int n) { }
}
` + "```" + `

Needs **Rigidbody2D** on at least one object, and **Collider2D** on both.`

	// ── SCENE LOADING ─────────────────────────────────────────────────────────
	case matchAny(q, "load scene", "loadscene", "change scene", "next scene", "scenemanager", "scene transition"):
		return `**Loading scenes** with SceneManager:

` + "```csharp" + `
using UnityEngine;
using UnityEngine.SceneManagement;

public class SceneLoader : MonoBehaviour
{
    // Load by name
    public void GoToMainMenu()
    {
        SceneManager.LoadScene("MainMenu");
    }

    // Load by build index (set order in File > Build Settings)
    public void LoadNextLevel()
    {
        int current = SceneManager.GetActiveScene().buildIndex;
        SceneManager.LoadScene(current + 1);
    }

    // Restart current scene
    public void Restart()
    {
        SceneManager.LoadScene(SceneManager.GetActiveScene().name);
    }

    // Load without destroying the current scene
    public void LoadAdditive(string sceneName)
    {
        SceneManager.LoadScene(sceneName, LoadSceneMode.Additive);
    }
}
` + "```" + `

⚠️ **Important:** Add your scenes to **File → Build Settings** first, or ` + "`LoadScene`" + ` will fail at runtime.`

	// ── INSTANTIATE / SPAWN ───────────────────────────────────────────────────
	case matchAny(q, "instantiate", "spawn", "create prefab", "spawn object", "create object"):
		return `**Spawning objects** with Instantiate:

` + "```csharp" + `
using UnityEngine;

public class Spawner : MonoBehaviour
{
    public GameObject enemyPrefab;
    public Transform spawnPoint;

    void Update()
    {
        if (Input.GetKeyDown(KeyCode.Space))
            SpawnEnemy();
    }

    void SpawnEnemy()
    {
        // Basic spawn at a position with rotation
        Instantiate(enemyPrefab, spawnPoint.position, spawnPoint.rotation);
    }

    void SpawnWithReference()
    {
        // Keep a reference to do things with it after spawning
        GameObject newEnemy = Instantiate(enemyPrefab, transform.position, Quaternion.identity);
        newEnemy.GetComponent<Enemy>().health = 100;
        newEnemy.transform.parent = transform; // make it a child of this object
    }
}
` + "```" + `

**Setup:** Create a Prefab (drag a GameObject from the Hierarchy into the Project panel), then drag it into the ` + "`enemyPrefab`" + ` slot in the Inspector.`

	// ── DESTROY ───────────────────────────────────────────────────────────────
	case matchAny(q, "destroy object", "destroy gameobject", "remove object", "despawn", "delete object"):
		return `**Destroying GameObjects:**

` + "```csharp" + `
using UnityEngine;

public class DestroyExample : MonoBehaviour
{
    void Examples()
    {
        // Destroy this GameObject immediately
        Destroy(gameObject);

        // Destroy after a delay (seconds)
        Destroy(gameObject, 3f);

        // Destroy a specific other object
        Destroy(otherObject);

        // Destroy just a component (not the whole object)
        Destroy(GetComponent<Rigidbody>());
    }

    public GameObject otherObject;
}
` + "```" + `

**Tip:** If you're spawning and destroying many objects frequently (bullets, particles), use **Object Pooling** instead — it's much faster than constantly creating/destroying.`

	// ── INPUT ─────────────────────────────────────────────────────────────────
	case matchAny(q, "input", "keyboard", "key press", "getkey", "getaxis", "mouse click", "mouse button", "detect input"):
		return `**Reading input** in Unity:

` + "```csharp" + `
using UnityEngine;

public class InputExample : MonoBehaviour
{
    void Update()
    {
        // ── Keyboard ────────────────────────────────────
        if (Input.GetKeyDown(KeyCode.Space))      // pressed this frame
            Debug.Log("Space pressed");

        if (Input.GetKey(KeyCode.LeftShift))      // held down
            Debug.Log("Shift held");

        if (Input.GetKeyUp(KeyCode.Space))         // released this frame
            Debug.Log("Space released");

        // ── Axes (smooth, -1 to 1) ──────────────────────
        float h = Input.GetAxis("Horizontal");    // A/D or arrow keys
        float v = Input.GetAxis("Vertical");      // W/S or arrow keys

        // Raw axis (no smoothing, exactly -1, 0, or 1)
        float rawH = Input.GetAxisRaw("Horizontal");

        // ── Mouse ───────────────────────────────────────
        if (Input.GetMouseButtonDown(0))          // left click
            Debug.Log("Left click");
        if (Input.GetMouseButtonDown(1))          // right click
            Debug.Log("Right click");

        Vector3 mousePos = Input.mousePosition;   // screen position
    }
}
` + "```" + `

**Tip:** For new projects, consider using Unity's **Input System** package (Install via Package Manager) — it's more powerful and supports gamepads natively.`

	// ── SAVE / PLAYERPREFS ────────────────────────────────────────────────────
	case matchAny(q, "save game", "playerprefs", "save data", "load data", "high score save", "save setting"):
		return `**Saving and loading data** with PlayerPrefs:

` + "```csharp" + `
using UnityEngine;

public class SaveSystem : MonoBehaviour
{
    // ── Save ─────────────────────────────────────────────
    public void SaveGame(int score, float volume, string playerName)
    {
        PlayerPrefs.SetInt("HighScore", score);
        PlayerPrefs.SetFloat("Volume", volume);
        PlayerPrefs.SetString("PlayerName", playerName);
        PlayerPrefs.Save(); // flush to disk immediately
    }

    // ── Load ─────────────────────────────────────────────
    public void LoadGame()
    {
        int score      = PlayerPrefs.GetInt("HighScore", 0);       // 0 = default
        float volume   = PlayerPrefs.GetFloat("Volume", 1f);
        string name    = PlayerPrefs.GetString("PlayerName", "Player");

        Debug.Log($"Welcome back {name}! Best score: {score}");
    }

    public bool HasSaveData() => PlayerPrefs.HasKey("HighScore");

    public void DeleteSave() => PlayerPrefs.DeleteAll();
}
` + "```" + `

**PlayerPrefs is good for:** settings, high scores, simple flags.
**For complex save data** (inventory, level progress) use ` + "`JsonUtility`" + ` + ` + "`File.WriteAllText`" + ` to save a JSON file instead.`

	// ── NAVMESH / AI ──────────────────────────────────────────────────────────
	case matchAny(q, "navmesh", "pathfinding", "enemy follow", "ai follow", "navmeshagent", "navigation"):
		return `**NavMesh pathfinding** for enemy AI:

` + "```csharp" + `
using UnityEngine;
using UnityEngine.AI;

public class EnemyAI : MonoBehaviour
{
    public Transform player;
    public float chaseRange = 10f;
    public float stopDistance = 2f;

    private NavMeshAgent agent;

    void Start()
    {
        agent = GetComponent<NavMeshAgent>();
    }

    void Update()
    {
        float dist = Vector3.Distance(transform.position, player.position);

        if (dist < chaseRange)
        {
            // Chase the player
            agent.SetDestination(player.position);

            // Stop when close enough
            agent.isStopped = dist < stopDistance;
        }
        else
        {
            agent.isStopped = true;
        }
    }
}
` + "```" + `

**Setup:**
1. **Window → AI → Navigation** → bake your NavMesh (click *Bake*)
2. Add **NavMeshAgent** component to your enemy
3. Attach this script and drag the player Transform in

For 2D games, NavMesh doesn't work natively — use a pathfinding plugin like **A* Pathfinding Project** (free) instead.`

	// ── RAYCAST ───────────────────────────────────────────────────────────────
	case matchAny(q, "raycast", "ray cast", "shoot ray", "line cast", "hit detection ray", "physics.raycast"):
		return `**Raycasting** — shoot an invisible ray to detect what's in front of you:

` + "```csharp" + `
using UnityEngine;

public class RaycastExample : MonoBehaviour
{
    public float range = 10f;
    public LayerMask hitLayers; // set in Inspector

    void Update()
    {
        // Shoot a ray forward from this object
        Ray ray = new Ray(transform.position, transform.forward);
        RaycastHit hit;

        if (Physics.Raycast(ray, out hit, range, hitLayers))
        {
            Debug.Log("Hit: " + hit.collider.name);
            Debug.Log("Distance: " + hit.distance);
            Debug.Log("Point: " + hit.point);

            // Apply damage if it's an enemy
            if (hit.collider.CompareTag("Enemy"))
                hit.collider.GetComponent<Enemy>().TakeDamage(10);
        }

        // Draw the ray in the Scene view for debugging
        Debug.DrawRay(transform.position, transform.forward * range, Color.red);
    }

    // Raycast from mouse position (for click-to-move, RTS games, etc.)
    void MouseRaycast()
    {
        Ray mouseRay = Camera.main.ScreenPointToRay(Input.mousePosition);
        if (Physics.Raycast(mouseRay, out RaycastHit hit))
            Debug.Log("Clicked on: " + hit.collider.name);
    }
}
` + "```" + `

Use **LayerMask** in the Inspector to control what the ray can and can't hit.`

	// ── ANIMATION ─────────────────────────────────────────────────────────────
	case matchAny(q, "animator", "animation state", "settrigger", "setbool", "setfloat", "animate", "animation script"):
		return `**Controlling animations** from script:

` + "```csharp" + `
using UnityEngine;

public class AnimationController : MonoBehaviour
{
    private Animator animator;
    private Rigidbody2D rb;

    void Start()
    {
        animator = GetComponent<Animator>();
        rb = GetComponent<Rigidbody2D>();
    }

    void Update()
    {
        float speed = Mathf.Abs(rb.linearVelocity.x);

        // Drive animations with parameters set in the Animator window
        animator.SetFloat("Speed", speed);
        animator.SetBool("IsGrounded", IsGrounded());

        if (Input.GetKeyDown(KeyCode.Space))
            animator.SetTrigger("Jump"); // one-shot trigger
    }

    bool IsGrounded() => true; // replace with your ground check

    // Play a specific state directly
    void PlayDeath()
    {
        animator.Play("Death");
    }
}
` + "```" + `

**Setup in Animator window:**
1. Create parameters (Float, Bool, Trigger) matching the names above
2. Make transitions between states driven by those parameters
3. The script sets the values — the Animator handles which clip plays`

	// ── UI BUTTON ─────────────────────────────────────────────────────────────
	case matchAny(q, "ui button", "button click", "onclick", "canvas button", "make button"):
		return `**UI Button setup** in Unity:

` + "```csharp" + `
using UnityEngine;
using UnityEngine.UI;

public class UIExample : MonoBehaviour
{
    public Button startButton;
    public Button quitButton;
    public Text scoreText; // or TMP_Text if using TextMeshPro

    void Start()
    {
        // Assign button functions in script
        startButton.onClick.AddListener(OnStartClicked);
        quitButton.onClick.AddListener(OnQuitClicked);
    }

    void OnStartClicked()
    {
        Debug.Log("Start!");
        UnityEngine.SceneManagement.SceneManager.LoadScene("GameScene");
    }

    void OnQuitClicked()
    {
        Application.Quit();
    }

    public void UpdateScore(int score)
    {
        scoreText.text = "Score: " + score;
    }
}
` + "```" + `

**Tip:** You can also assign functions directly in the Button's **OnClick** event in the Inspector — no code needed for simple cases. For text, prefer **TextMeshPro** (TMP) over the legacy Text component.`

	// ── CAMERA FOLLOW ─────────────────────────────────────────────────────────
	case matchAny(q, "camera follow", "follow camera", "camera track", "smooth camera", "camera player"):
		return `**Smooth camera follow** script:

` + "```csharp" + `
using UnityEngine;

public class CameraFollow : MonoBehaviour
{
    public Transform target;       // drag your player here
    public float smoothSpeed = 5f;
    public Vector3 offset = new Vector3(0, 2, -10); // 2D: (0,0,-10), 3D: adjust as needed

    void LateUpdate()
    {
        // LateUpdate ensures the player has moved before we follow
        Vector3 desiredPos = target.position + offset;
        transform.position = Vector3.Lerp(transform.position, desiredPos, smoothSpeed * Time.deltaTime);
    }
}
` + "```" + `

**Tip:** For more advanced camera behaviour (camera zones, shake, zoom), use **Cinemachine** — it's free, built into Unity, and saves you writing all this yourself. Add it via Package Manager.`

	// ── OBJECT POOLING ────────────────────────────────────────────────────────
	case matchAny(q, "object pool", "pooling", "pool object", "pool system"):
		return `**Object Pooling** — reuse objects instead of Instantiate/Destroy (way faster for bullets, particles, enemies):

` + "```csharp" + `
using UnityEngine;
using System.Collections.Generic;

public class ObjectPool : MonoBehaviour
{
    public static ObjectPool Instance;
    public GameObject prefab;
    public int initialSize = 20;

    private Queue<GameObject> pool = new Queue<GameObject>();

    void Awake()
    {
        Instance = this;
        // Pre-spawn objects and deactivate them
        for (int i = 0; i < initialSize; i++)
        {
            GameObject obj = Instantiate(prefab);
            obj.SetActive(false);
            pool.Enqueue(obj);
        }
    }

    // Get an object from the pool
    public GameObject Get(Vector3 position, Quaternion rotation)
    {
        GameObject obj = pool.Count > 0 ? pool.Dequeue() : Instantiate(prefab);
        obj.transform.SetPositionAndRotation(position, rotation);
        obj.SetActive(true);
        return obj;
    }

    // Return an object to the pool instead of destroying it
    public void Return(GameObject obj)
    {
        obj.SetActive(false);
        pool.Enqueue(obj);
    }
}
` + "```" + `

**Usage:** ` + "`ObjectPool.Instance.Get(pos, rot)`" + ` to spawn, ` + "`ObjectPool.Instance.Return(obj)`" + ` instead of Destroy. Unity 2021+ also has a built-in ` + "`UnityEngine.Pool.ObjectPool<T>`" + `.`

	// ── SCRIPTABLEOBJECT ─────────────────────────────────────────────────────
	case matchAny(q, "scriptableobject", "scriptable object", "so asset"):
		return `**ScriptableObjects** — data containers that live as assets in your Project:

` + "```csharp" + `
using UnityEngine;

// This attribute adds a menu item to create this asset
[CreateAssetMenu(fileName = "NewWeapon", menuName = "Game/Weapon")]
public class WeaponData : ScriptableObject
{
    public string weaponName = "Sword";
    public int damage = 25;
    public float fireRate = 0.5f;
    public Sprite icon;
    public AudioClip fireSound;
}
` + "```" + `

**Create an asset:** Right-click in Project panel → *Create → Game → Weapon*

**Use it in a script:**
` + "```csharp" + `
public class PlayerWeapon : MonoBehaviour
{
    public WeaponData weapon; // drag the asset here in Inspector

    void Shoot()
    {
        Debug.Log("Firing " + weapon.weaponName + " for " + weapon.damage + " damage");
        audioSource.PlayOneShot(weapon.fireSound);
    }
}
` + "```" + `

**Why ScriptableObjects?** Share data between multiple objects without duplicating it. Change stats in one place and every object that uses it updates automatically.`

	// ── UPDATE vs FIXEDUPDATE ─────────────────────────────────────────────────
	case matchAny(q, "update vs fixedupdate", "fixedupdate vs update", "when to use fixedupdate", "difference update fixedupdate"):
		return `**Update vs FixedUpdate** — one of the most important things to get right:

| | ` + "`Update()`" + ` | ` + "`FixedUpdate()`" + ` |
|---|---|---|
| **Runs** | Every rendered frame | Fixed physics timestep (50/sec default) |
| **Framerate** | Varies (30, 60, 144 FPS...) | Always consistent |
| **Use for** | Input, UI, game logic | Physics, Rigidbody, forces |
| **Time ref** | ` + "`Time.deltaTime`" + ` | ` + "`Time.fixedDeltaTime`" + ` |

` + "```csharp" + `
void Update()
{
    // ✅ Input detection
    if (Input.GetKeyDown(KeyCode.Space))
        jump = true;

    // ✅ UI updates, camera logic
    // ❌ DON'T put Rigidbody movement here
}

void FixedUpdate()
{
    // ✅ Physics movement
    rb.AddForce(Vector2.up * jumpForce);

    // ✅ MovePosition, velocity changes
    // ❌ DON'T read Input.GetKeyDown here — you'll miss presses
}
` + "```" + `

**Golden rule:** Input goes in ` + "`Update`" + `, physics goes in ` + "`FixedUpdate`" + `.`

	// ── SINGLETON ─────────────────────────────────────────────────────────────
	case matchAny(q, "singleton", "gamemanager singleton", "static instance", "dontdestroyonload"):
		return `**Singleton pattern** for a GameManager:

` + "```csharp" + `
using UnityEngine;

public class GameManager : MonoBehaviour
{
    // The one and only instance
    public static GameManager Instance { get; private set; }

    public int score = 0;
    public int lives = 3;

    void Awake()
    {
        // If there's already an instance, destroy this duplicate
        if (Instance != null && Instance != this)
        {
            Destroy(gameObject);
            return;
        }

        Instance = this;
        DontDestroyOnLoad(gameObject); // survive scene loads
    }

    public void AddScore(int points)
    {
        score += points;
    }
}
` + "```" + `

**Access it from anywhere:**
` + "```csharp" + `
GameManager.Instance.AddScore(100);
int currentLives = GameManager.Instance.lives;
` + "```" + `

Only put one GameManager in your first scene — ` + "`DontDestroyOnLoad`" + ` keeps it alive across all scene loads.`

	// ── LERP / SMOOTH ─────────────────────────────────────────────────────────
	case matchAny(q, "lerp", "smooth move", "smooth rotation", "slerp", "movetowards"):
		return `**Lerp** — smoothly interpolate between two values:

` + "```csharp" + `
using UnityEngine;

public class LerpExamples : MonoBehaviour
{
    public Transform target;
    public float speed = 3f;

    void Update()
    {
        // Smooth position (exponential ease — feels natural)
        transform.position = Vector3.Lerp(
            transform.position,
            target.position,
            speed * Time.deltaTime
        );

        // Smooth rotation
        transform.rotation = Quaternion.Slerp(
            transform.rotation,
            target.rotation,
            speed * Time.deltaTime
        );

        // Move at constant speed (no ease)
        transform.position = Vector3.MoveTowards(
            transform.position,
            target.position,
            speed * Time.deltaTime  // moves exactly this many units per second
        );

        // Smooth a single float (e.g. health bar)
        float currentHP = 80f;
        float targetHP = 100f;
        float displayed = Mathf.Lerp(currentHP, targetHP, speed * Time.deltaTime);
    }
}
` + "```" + `

**Lerp tip:** ` + "`Lerp(current, target, t)`" + ` where t=0 is current, t=1 is target. Using ` + "`speed * Time.deltaTime`" + ` as t gives you a nice ease-out feel.`

	default:
		return "" // No built-in answer — fall through to doc synthesis
	}
}

// ── Helpers ────────────────────────────────────────────────────────────────────

func matchAny(q string, patterns ...string) bool {
	for _, p := range patterns {
		if strings.Contains(q, p) {
			return true
		}
	}
	return false
}

func isCodeRequest(q string) bool {
	return matchAny(q, "script", "code", "write", "example", "how do i", "how to", "show", "give me", "make", "create")
}

// ── Intent Detection ───────────────────────────────────────────────────────────

type Intent int

const (
	IntentHowTo    Intent = iota
	IntentWriteCode
	IntentExplain
	IntentDifference
	IntentFix
	IntentList
	IntentGeneral
)

func detectIntent(q string) Intent {
	if matchAny(q, "write", "script", "code", "give me code", "show me", "example") {
		return IntentWriteCode
	}
	if matchAny(q, "how do i", "how to", "how can i", "how do you", "how does") {
		return IntentHowTo
	}
	if matchAny(q, "error", "not working", "broken", "fix", "bug", "null", "exception") {
		return IntentFix
	}
	if matchAny(q, "difference", " vs ", "versus", "or ", "which") {
		return IntentDifference
	}
	if matchAny(q, "what is", "what are", "explain", "what does", "describe") {
		return IntentExplain
	}
	if matchAny(q, "list", "all types", "types of", "what are all") {
		return IntentList
	}
	return IntentGeneral
}

func extractTopic(q string) string {
	known := map[string]string{
		"rigidbody2d": "Rigidbody2D", "rigidbody": "Rigidbody",
		"audiosource": "AudioSource", "audio": "Audio", "sound": "Audio",
		"animator": "Animator", "animation": "Animation",
		"coroutine": "Coroutine", "navmesh": "NavMesh",
		"raycast": "Raycast", "tilemap": "Tilemap",
		"sprite": "Sprite", "shader": "Shader",
		"canvas": "Canvas", "ui": "UI", "button": "Button",
		"camera": "Camera", "light": "Lighting",
		"prefab": "Prefab", "scene": "Scene",
		"input": "Input", "collider": "Collider",
		"collision": "Collision", "trigger": "Trigger",
		"scriptableobject": "ScriptableObject",
		"playerprefs": "PlayerPrefs", "save": "Saving",
		"transform": "Transform", "physics": "Physics",
	}
	for key, name := range known {
		if strings.Contains(q, key) {
			return name
		}
	}
	words := strings.Fields(q)
	for i, w := range words {
		if w == "with" || w == "using" || w == "for" || w == "about" {
			if i+1 < len(words) {
				return strings.Title(words[i+1])
			}
		}
	}
	return "this topic"
}

// ── Doc-based synthesis (fallback when no built-in answer) ────────────────────

type docContext struct {
	MainContent string
	KeyPoints   []string
	Methods     []string
}

func buildContext(results []search.Result) docContext {
	ctx := docContext{}
	allText := ""
	for i, r := range results {
		if i >= 3 { break }
		allText += r.Excerpt + "\n\n"
	}
	ctx.MainContent = allText
	ctx.KeyPoints = extractKeyPoints(allText)
	ctx.Methods = extractMethods(allText)
	return ctx
}

func extractKeyPoints(text string) []string {
	var points []string
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) > 50 && len(line) < 250 && strings.Contains(line, ".") && !strings.Contains(line, "http") {
			points = append(points, line)
			if len(points) >= 5 { break }
		}
	}
	return points
}

func extractMethods(text string) []string {
	var methods []string
	seen := map[string]bool{}
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.'
	})
	for _, w := range words {
		if strings.Contains(w, ".") {
			parts := strings.Split(w, ".")
			if len(parts) == 2 && len(parts[1]) > 2 && !seen[w] && unicode.IsUpper(rune(parts[0][0])) {
				seen[w] = true
				methods = append(methods, w)
				if len(methods) >= 6 { break }
			}
		}
	}
	return methods
}

func synthesizeFromDocs(intent Intent, q, topic string, ctx docContext, results []search.Result) string {
	sb := &strings.Builder{}

	switch intent {
	case IntentExplain:
		fmt.Fprintf(sb, "**%s in Unity:**\n\n", topic)
	case IntentHowTo:
		fmt.Fprintf(sb, "Here's how to work with **%s** in Unity:\n\n", topic)
	case IntentWriteCode:
		fmt.Fprintf(sb, "Here's what I found about **%s** from the docs:\n\n", topic)
	default:
		fmt.Fprintf(sb, "**%s** — from the Unity docs:\n\n", topic)
	}

	written := 0
	for _, p := range ctx.KeyPoints {
		clean := cleanSentence(p)
		if len(clean) > 30 {
			sb.WriteString(clean)
			sb.WriteString("\n\n")
			written++
			if written >= 3 { break }
		}
	}

	if written == 0 {
		// Last resort: take a clean slice of the raw content
		content := ctx.MainContent
		if len(content) > 600 { content = content[:600] }
		sb.WriteString(cleanSentence(content))
		sb.WriteString("\n\n")
	}

	if len(ctx.Methods) > 0 {
		sb.WriteString("**Key API:** `")
		sb.WriteString(strings.Join(ctx.Methods[:min(4, len(ctx.Methods))], "`, `"))
		sb.WriteString("`\n\n")
	}

	if len(results) > 0 {
		sb.WriteString("Check the linked docs below for the full details.")
	}

	return sb.String()
}

func cleanSentence(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}

func min(a, b int) int {
	if a < b { return a }
	return b
}
