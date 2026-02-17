package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const apiURL = "https://api.openai.com/v1/chat/completions"

// Client is a minimal OpenAI API client (no SDK, pure stdlib)
type Client struct {
	apiKey string
	model  string
	http   *http.Client
}

func NewClient(apiKey, model string) *Client {
	if model == "" {
		model = "gpt-4o-mini"
	}
	return &Client{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// History entry from the browser
type HistoryEntry struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Ask sends a question to OpenAI with conversation history
func (c *Client) Ask(query string, history []HistoryEntry) (string, error) {
	// Build message array
	messages := []message{
		{
			Role: "system",
			Content: `You are UnityMind, an expert Unity game development assistant. 
You specialize in Unity 2D and 3D game development, C# scripting, Unity Editor, 
physics, animation, UI, audio, scene management, performance optimization, 
and the Unity ecosystem.

Guidelines:
- Give clear, practical, copy-paste-ready code examples when relevant
- Always specify the Unity version considerations if important
- Prefer Unity's built-in solutions before suggesting third-party assets
- Format code blocks with triple backticks and 'csharp' language tag
- Be concise but complete
- If you reference Unity documentation, mention the specific Manual or ScriptReference page`,
		},
	}

	// Add conversation history (last 6 messages max to save tokens)
	start := 0
	if len(history) > 6 {
		start = len(history) - 6
	}
	for _, h := range history[start:] {
		messages = append(messages, message{Role: h.Role, Content: h.Content})
	}

	// Add the current question
	messages = append(messages, message{Role: "user", Content: query})

	reqBody := chatRequest{
		Model:       c.model,
		Messages:    messages,
		MaxTokens:   1024,
		Temperature: 0.3, // Low temp = more accurate, less hallucination
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal error: %w", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("request error: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read error: %w", err)
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBytes, &chatResp); err != nil {
		return "", fmt.Errorf("parse error: %w", err)
	}

	if chatResp.Error != nil {
		return "", fmt.Errorf("API error (%s): %s", chatResp.Error.Type, chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no choices returned")
	}

	answer := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	if answer == "" {
		return "", fmt.Errorf("empty response")
	}

	return answer, nil
}
