package narrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/Sudhanshu069/claude-says/internal/config"
)

// geminiTimeout bounds every Gemini request (context deadline + client Timeout).
const geminiTimeout = 10 * time.Second

// geminiMaxRespBytes caps the response body we decode. Responses are tiny
// (MaxOutputTokens: 100), so 1 MiB is generous headroom; the cap keeps a
// misbehaving or MITM'd endpoint from streaming an unbounded body into memory.
const geminiMaxRespBytes = 1 << 20

// modelRe validates the model id before it is interpolated into the request
// URL. Every real Gemini model id (e.g. "gemini-2.5-flash",
// "gemini-1.5-pro-latest") matches; rejecting anything else stops a crafted
// config value from injecting extra path segments or query parameters.
var modelRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// geminiSystemPrompt is the narrator instruction, verbatim from Node
// src/narrators/gemini.js.
const geminiSystemPrompt = `You are a concise narrator commentating on an AI coding assistant's actions in real-time.

Rules:
- Summarize what the assistant is doing in 1-2 short, conversational sentences
- Skip code snippets, file paths, and technical details
- Focus on the intent and action: "Claude is fixing the bug" not "Claude is editing line 42 of src/foo.js"
- Use present tense: "Claude is reading...", "Claude found...", "Claude is now editing..."
- Never use markdown formatting
- If the text is just a brief status update, keep your summary equally brief
- Maximum 2 sentences`

// GeminiNarrator rephrases text via the Gemini REST API. The API key is sent in
// the x-goog-api-key header (never the URL), and Narrate falls back to the input
// text on any failure.
type GeminiNarrator struct {
	http   *http.Client
	apiKey string // GEMINI_API_KEY
	model  string // default "gemini-2.5-flash"
}

// newGemini builds a GeminiNarrator from cfg.Narrator.Gemini and the
// GEMINI_API_KEY env var.
func newGemini(cfg *config.Config) (Narrator, error) {
	model := cfg.Narrator.Gemini.Model
	if model == "" {
		model = "gemini-2.5-flash"
	}
	return &GeminiNarrator{
		http:   &http.Client{Timeout: geminiTimeout},
		apiKey: os.Getenv("GEMINI_API_KEY"),
		model:  model,
	}, nil
}

// errNoAPIKey is returned by generate when GEMINI_API_KEY is unset. Narrate
// swallows it (returns input); Validate surfaces it to the setup wizard.
var errNoAPIKey = errors.New("GEMINI_API_KEY environment variable not set")

// Narrate POSTs to generativelanguage.googleapis.com/v1beta/models/{model}:generateContent
// and returns the rephrased text, or the ORIGINAL text on missing key / timeout
// / non-200 / parse failure. It never returns an error by contract.
func (n *GeminiNarrator) Narrate(ctx context.Context, text string) string {
	out, _ := n.NarrateOrErr(ctx, text)
	return out
}

// NarrateOrErr behaves like Narrate but also returns the underlying error when
// it degraded to the input verbatim, so callers can log the fallback. The
// returned string is always safe to speak (input on missing key / timeout /
// non-200 / parse failure / empty response). Implements narrator.Degrader.
func (n *GeminiNarrator) NarrateOrErr(ctx context.Context, text string) (string, error) {
	out, err := n.generate(ctx, text)
	if err != nil {
		return text, err
	}
	if out == "" {
		return text, nil
	}
	return out, nil
}

// Validate checks the API key and connectivity (surfaces real errors for setup).
// Unlike Narrate it is NOT total: it makes a real request and returns the
// underlying error so the setup wizard can report it.
func (n *GeminiNarrator) Validate(ctx context.Context) error {
	if n.apiKey == "" {
		return errNoAPIKey
	}
	out, err := n.generate(ctx, "I am reading the config file to check the settings.")
	if err != nil {
		return err
	}
	if out == "" {
		return errors.New("gemini: empty response")
	}
	return nil
}

// generate performs one generateContent request and returns the rephrased text.
// It is the single fallible core shared by Narrate (swallows errors) and
// Validate (surfaces them). The request is always bounded by a context deadline
// (Node bug #4/#24) in addition to the client Timeout backstop, and the API key
// travels in the x-goog-api-key header, never the URL. Non-2xx responses return
// an error WITHOUT the response body so it can't leak into logs.
func (n *GeminiNarrator) generate(ctx context.Context, text string) (string, error) {
	if n.apiKey == "" {
		return "", errNoAPIKey
	}
	if !modelRe.MatchString(n.model) {
		return "", fmt.Errorf("gemini: invalid model %q", n.model)
	}

	ctx, cancel := context.WithTimeout(ctx, geminiTimeout)
	defer cancel()

	payload := geminiReq{
		SystemInstruction: geminiContent{Parts: []geminiPart{{Text: geminiSystemPrompt}}},
		Contents: []geminiContent{
			{Parts: []geminiPart{{Text: "Narrate this AI assistant output:\n\n" + text}}},
		},
		GenerationConfig: geminiGenCfg{MaxOutputTokens: 100, Temperature: 0.3},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	url := "https://generativelanguage.googleapis.com/v1beta/models/" + n.model + ":generateContent"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", n.apiKey)

	resp, err := n.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Drain (bounded) for connection reuse; never fold the body into the
		// error — it can carry sensitive detail into logs.
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return "", fmt.Errorf("gemini API error %d", resp.StatusCode)
	}

	var parsed geminiResp
	if err := json.NewDecoder(io.LimitReader(resp.Body, geminiMaxRespBytes)).Decode(&parsed); err != nil {
		return "", err
	}
	if len(parsed.Candidates) > 0 && len(parsed.Candidates[0].Content.Parts) > 0 {
		return parsed.Candidates[0].Content.Parts[0].Text, nil
	}
	return "", nil
}

// Wire types.
type geminiReq struct {
	SystemInstruction geminiContent   `json:"system_instruction"`
	Contents          []geminiContent `json:"contents"`
	GenerationConfig  geminiGenCfg    `json:"generationConfig"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenCfg struct {
	MaxOutputTokens int     `json:"maxOutputTokens"`
	Temperature     float64 `json:"temperature"`
}

type geminiResp struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
}
