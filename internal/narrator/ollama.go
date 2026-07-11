package narrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Sudhanshu069/claude-says/internal/config"
)

// ollamaTimeout bounds every request. Local generation can be slower than a
// cloud call on a cold model, but the narrator must never stall the pipeline;
// on timeout Narrate falls back to the input text like gemini.
const ollamaTimeout = 20 * time.Second

// ollamaMaxRespBytes caps the decoded response body (defense-in-depth, same as
// gemini) even though the endpoint is local.
const ollamaMaxRespBytes = 1 << 20

const (
	defaultOllamaEndpoint = "http://localhost:11434"
	defaultOllamaModel    = "llama3.2"
)

// OllamaNarrator rephrases text via a local ollama server. Nothing leaves the
// machine, so — unlike gemini — it does not redact the input. Narrate is TOTAL:
// it returns the input verbatim on any failure (server down, timeout, non-200,
// parse error).
type OllamaNarrator struct {
	http     *http.Client
	endpoint string
	model    string
}

// newOllama builds an OllamaNarrator from cfg.Narrator.Ollama, applying defaults.
func newOllama(cfg *config.Config) (Narrator, error) {
	endpoint := cfg.Narrator.Ollama.Endpoint
	if endpoint == "" {
		endpoint = defaultOllamaEndpoint
	}
	model := cfg.Narrator.Ollama.Model
	if model == "" {
		model = defaultOllamaModel
	}
	return &OllamaNarrator{
		http:     &http.Client{Timeout: ollamaTimeout},
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
	}, nil
}

// ollamaReq/ollamaResp are the subset of the /api/generate contract we use.
// stream=false makes ollama return a single JSON object rather than a stream.
type ollamaReq struct {
	Model   string        `json:"model"`
	Prompt  string        `json:"prompt"`
	System  string        `json:"system"`
	Stream  bool          `json:"stream"`
	Options ollamaOptions `json:"options"`
}

type ollamaOptions struct {
	NumPredict  int     `json:"num_predict"`
	Temperature float64 `json:"temperature"`
}

type ollamaResp struct {
	Response string `json:"response"`
}

// Narrate returns the rephrased text, or the ORIGINAL text on any failure.
// Never returns an error by contract.
func (n *OllamaNarrator) Narrate(ctx context.Context, text string) string {
	out, _ := n.NarrateOrErr(ctx, text)
	return out
}

// NarrateOrErr behaves like Narrate but also returns the underlying error when
// it degraded to the input verbatim (implements Degrader). The returned string
// is always safe to speak.
func (n *OllamaNarrator) NarrateOrErr(ctx context.Context, text string) (string, error) {
	out, err := n.generate(ctx, text)
	if err != nil {
		return text, err
	}
	if out == "" {
		return text, nil
	}
	return out, nil
}

// Validate makes a real request so the setup path can surface a down server.
func (n *OllamaNarrator) Validate(ctx context.Context) error {
	out, err := n.generate(ctx, "I am reading the config file to check the settings.")
	if err != nil {
		return err
	}
	if out == "" {
		return errors.New("ollama: empty response")
	}
	return nil
}

// generate performs one /api/generate request. Bounded by a context deadline in
// addition to the client Timeout; non-2xx bodies never fold into the error.
func (n *OllamaNarrator) generate(ctx context.Context, text string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, ollamaTimeout)
	defer cancel()

	payload := ollamaReq{
		Model:   n.model,
		System:  narratorSystemPrompt,
		Prompt:  "Narrate this AI assistant output:\n\n" + text,
		Stream:  false,
		Options: ollamaOptions{NumPredict: 100, Temperature: 0.3},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.endpoint+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return "", fmt.Errorf("ollama API error %d", resp.StatusCode)
	}

	var parsed ollamaResp
	if err := json.NewDecoder(io.LimitReader(resp.Body, ollamaMaxRespBytes)).Decode(&parsed); err != nil {
		return "", err
	}
	return strings.TrimSpace(parsed.Response), nil
}
