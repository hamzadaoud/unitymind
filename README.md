# 🎮 UnityMind

**A local-first Unity game development assistant that runs as a single tiny executable.**

No cloud dependency. No subscriptions. Works offline. Runs on hardware as old as a 0.6GHz CPU with 100MB RAM.

---

## ✨ Features

- **Local-first search** — Indexes Unity documentation on your machine. Answers in milliseconds.
- **Live docs fallback** — If your local index doesn't have the answer, fetches from `docs.unity3d.com` live.
- **AI fallback (optional)** — Set your own OpenAI API key to get GPT answers when docs don't suffice.
- **Single `.exe`** — No Python, no Node.js, no Docker. Just double-click and it opens in your browser.
- **Ultra lightweight** — ~10MB binary, ~25MB RAM usage at runtime.
- **Cross-platform** — Windows x64/ARM64, Linux x64/ARM (Raspberry Pi), compiled from the same code.
- **Covers Unity 2D and 3D** — Physics, animation, UI, scripting, shaders, NavMesh, audio, and more.

---

## 🚀 Quick Start (Windows)

### Option A — Download Release (easiest)
1. Go to [Releases](../../releases) and download `UnityMind.exe`
2. Put it in a folder (e.g. `C:\Tools\UnityMind\`)
3. Double-click `UnityMind.exe`
4. Your browser opens automatically at `http://localhost:7331`

### Option B — Build from Source
**Requirements:** [Go 1.21+](https://go.dev/dl/)

```batch
git clone https://github.com/YOUR_USERNAME/unitymind.git
cd unitymind
build.bat
UnityMind.exe
```

---

## 🖥️ System Requirements

| Component | Minimum | Recommended |
|-----------|---------|-------------|
| CPU       | 0.6 GHz (x86 or ARM) | Any modern CPU |
| RAM       | 100 MB  | 512 MB+ |
| Disk      | 100 MB  | 500 MB (for doc cache) |
| OS        | Windows 7+, Linux | Windows 10+, Linux |
| Network   | Not required | Required for live docs + AI |

---

## ⚙️ Configuration

On first launch, `config.json` is created in the same folder as the `.exe`:

```json
{
  "openai_key": "",
  "openai_model": "gpt-4o-mini",
  "port": 7331,
  "auto_update_docs": true
}
```

You can also configure everything from the in-app **Settings** panel (⚙️ button).

### OpenAI Key (optional)
UnityMind works great without OpenAI. But if you want AI-powered answers as a last resort:
1. Get a key at [platform.openai.com](https://platform.openai.com)
2. Paste it in Settings → OpenAI API Key
3. It's stored locally in `config.json` — never sent anywhere except OpenAI's API

---

## 🔍 How It Works

```
Your Question
     │
     ▼
┌─────────────────────────────────┐
│  1. Local Index Search (instant)│  ← BM25 search over cached Unity docs
│     Score > 0.3? → Answer ✓     │
└───────────────┬─────────────────┘
                │ Not found
                ▼
┌─────────────────────────────────┐
│  2. Live Unity Docs Search      │  ← Fetches docs.unity3d.com
│     Results? → Answer ✓         │
└───────────────┬─────────────────┘
                │ Still not found
                ▼
┌─────────────────────────────────┐
│  3. OpenAI API (if key set)     │  ← Your own API key, your cost
│     → Answer ✓                  │
└─────────────────────────────────┘
```

---

## 🏗 Project Structure

```
unitymind/
├── main.go              ← HTTP server, routing, browser launch
├── search/
│   └── search.go        ← BM25 search engine (zero dependencies)
├── docs/
│   └── manager.go       ← Unity doc fetcher and HTML parser
├── openai/
│   └── client.go        ← OpenAI API client (stdlib only)
├── ui/
│   └── index.html       ← Embedded chat interface
├── cache/
│   └── docs_index.json  ← Local doc index (auto-generated)
├── config.json          ← User settings (auto-generated)
├── go.mod
├── build.bat            ← Windows build script (all platforms)
└── README.md
```

---

## 🤝 Contributing

This project is open source and welcomes contributions! Ideas:
- Add more Unity doc pages to the `coreDocs` list in `docs/manager.go`
- Improve the search ranking algorithm in `search/search.go`
- Add syntax highlighting to code blocks in the UI
- Add conversation export feature
- Add Unity version selector

---

## 📄 License

MIT License — free to use, modify, and distribute.

---

## 🙏 Credits

Built with ❤️ using:
- [Go](https://go.dev) — the language that makes this tiny and fast
- [Unity Documentation](https://docs.unity3d.com) — the knowledge source
- [OpenAI API](https://openai.com) — optional AI fallback
- Claude (Anthropic) — helped design and generate this codebase

---

*UnityMind is not affiliated with Unity Technologies.*
