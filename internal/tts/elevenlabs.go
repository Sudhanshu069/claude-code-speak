package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Sudhanshu069/claude-says/internal/config"
)

// elevenLabsTimeout is the client Timeout backstop that FIXES Node bug #4/#24
// (the Node ElevenLabs provider had no timeout on any request).
const elevenLabsTimeout = 30 * time.Second

const elevenLabsBaseURL = "https://api.elevenlabs.io"

// errElevenLabsNotConfigured is returned when ELEVENLABS_API_KEY is absent.
var errElevenLabsNotConfigured = errors.New("elevenlabs: ELEVENLABS_API_KEY environment variable not set")

// ElevenLabsProvider synthesizes speech via the ElevenLabs REST API. Returns
// MP3. Every request is ctx-bound plus a client Timeout backstop.
type ElevenLabsProvider struct {
	http    *http.Client
	apiKey  string // ELEVENLABS_API_KEY
	voiceID string // configured; empty => lookup+cache
	modelID string

	mu            sync.Mutex
	cachedVoiceID string
}

// newElevenLabs builds an ElevenLabsProvider from cfg.ElevenLabs and the
// ELEVENLABS_API_KEY env var.
func newElevenLabs(cfg *config.Config) (Provider, error) {
	modelID := cfg.ElevenLabs.ModelID
	if modelID == "" {
		modelID = "eleven_turbo_v2_5"
	}
	return &ElevenLabsProvider{
		http:    &http.Client{Timeout: elevenLabsTimeout},
		apiKey:  os.Getenv("ELEVENLABS_API_KEY"),
		voiceID: cfg.ElevenLabs.VoiceID,
		modelID: modelID,
	}, nil
}

// elevenLabsSynthReq is the POST body for text-to-speech.
type elevenLabsSynthReq struct {
	Text          string                  `json:"text"`
	ModelID       string                  `json:"model_id"`
	VoiceSettings elevenLabsVoiceSettings `json:"voice_settings"`
}

type elevenLabsVoiceSettings struct {
	Stability       float64 `json:"stability"`
	SimilarityBoost float64 `json:"similarity_boost"`
}

// Synthesize POSTs to /v1/text-to-speech/{voiceID} with Accept: audio/mpeg.
// Returns (mp3Bytes, FormatMP3, err). Every request is ctx-bound.
func (p *ElevenLabsProvider) Synthesize(ctx context.Context, text string) ([]byte, string, error) {
	if p.apiKey == "" {
		return nil, FormatMP3, errElevenLabsNotConfigured
	}

	voiceID := p.voiceID
	if voiceID == "" {
		id, err := p.defaultVoiceID(ctx)
		if err != nil {
			return nil, FormatMP3, err
		}
		voiceID = id
	}

	body, err := json.Marshal(elevenLabsSynthReq{
		Text:    text,
		ModelID: p.modelID,
		VoiceSettings: elevenLabsVoiceSettings{
			Stability:       0.5,
			SimilarityBoost: 0.75,
		},
	})
	if err != nil {
		return nil, FormatMP3, err
	}

	url := elevenLabsBaseURL + "/v1/text-to-speech/" + voiceID
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, FormatMP3, err
	}
	req.Header.Set("Accept", "audio/mpeg")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("xi-api-key", p.apiKey)

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, FormatMP3, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Drain (bounded) so the connection can be reused; return the status
		// only — the body may carry account/voice details unfit for logs.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, FormatMP3, &ErrHTTPStatus{Provider: "elevenlabs", Code: resp.StatusCode}
	}

	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, FormatMP3, err
	}
	return audio, FormatMP3, nil
}

// Validate checks the API key and connectivity.
func (p *ElevenLabsProvider) Validate(ctx context.Context) error {
	if p.apiKey == "" {
		return errElevenLabsNotConfigured
	}
	audio, _, err := p.Synthesize(ctx, "test")
	if err != nil {
		return err
	}
	if len(audio) == 0 {
		return errors.New("elevenlabs: empty audio response")
	}
	return nil
}

// elevenLabsVoicesResp models GET /v1/voices.
type elevenLabsVoicesResp struct {
	Voices []struct {
		VoiceID string `json:"voice_id"`
		Name    string `json:"name"`
	} `json:"voices"`
}

// fetchVoices performs GET /v1/voices under ctx.
func (p *ElevenLabsProvider) fetchVoices(ctx context.Context) (*elevenLabsVoicesResp, error) {
	if p.apiKey == "" {
		return nil, errElevenLabsNotConfigured
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, elevenLabsBaseURL+"/v1/voices", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("xi-api-key", p.apiKey)

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, &ErrHTTPStatus{Provider: "elevenlabs", Code: resp.StatusCode}
	}

	var out elevenLabsVoicesResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Voices lists available voices via GET /v1/voices (implements VoiceLister).
func (p *ElevenLabsProvider) Voices(ctx context.Context) ([]Voice, error) {
	data, err := p.fetchVoices(ctx)
	if err != nil {
		return nil, err
	}
	voices := make([]Voice, 0, len(data.Voices))
	for _, v := range data.Voices {
		voices = append(voices, Voice{ID: v.VoiceID, Name: v.Name})
	}
	return voices, nil
}

// defaultVoiceID lazily resolves and caches the first available voice
// (mutex-guarded), mirroring Node's _cachedVoiceId.
func (p *ElevenLabsProvider) defaultVoiceID(ctx context.Context) (string, error) {
	p.mu.Lock()
	if p.cachedVoiceID != "" {
		id := p.cachedVoiceID
		p.mu.Unlock()
		return id, nil
	}
	p.mu.Unlock()

	data, err := p.fetchVoices(ctx)
	if err != nil {
		return "", err
	}
	if len(data.Voices) == 0 || data.Voices[0].VoiceID == "" {
		return "", errors.New("elevenlabs: no voices available")
	}
	id := data.Voices[0].VoiceID

	p.mu.Lock()
	p.cachedVoiceID = id
	p.mu.Unlock()
	return id, nil
}
