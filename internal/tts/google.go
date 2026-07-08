package tts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/Sudhanshu069/claude-code-speak/internal/config"
)

// googleTimeout is the per-request client Timeout backstop (Node bug #4/#24).
const googleTimeout = 30 * time.Second

const (
	googleSynthURL = "https://texttospeech.googleapis.com/v1/text:synthesize"
	googleScope    = "https://www.googleapis.com/auth/cloud-platform"
)

// errGoogleNotConfigured is returned when neither GOOGLE_API_KEY nor
// Application Default Credentials are available.
var errGoogleNotConfigured = errors.New("google: no credentials (set GOOGLE_API_KEY or configure Application Default Credentials)")

// GoogleProvider synthesizes speech via the Google Cloud TTS REST API (chosen
// over the heavy gRPC SDK). Auth prefers a GOOGLE_API_KEY env var, else
// Application Default Credentials. Returns WAV (LINEAR16 in a RIFF container).
type GoogleProvider struct {
	http            *http.Client
	tokenSource     oauth2.TokenSource // ADC; nil when apiKey is used
	apiKey          string             // GOOGLE_API_KEY fallback (X-Goog-Api-Key)
	languageCode    string
	voice           string
	audioEncoding   string
	sampleRateHertz int
}

// newGoogle resolves auth: prefer GOOGLE_API_KEY, else ADC via
// google.FindDefaultCredentials(ctx, cloud-platform scope). Missing credentials
// are NOT fatal here — construction still succeeds and Synthesize/Validate
// return a clear not-configured error, so `New` never panics on absent creds.
func newGoogle(cfg *config.Config) (Provider, error) {
	p := &GoogleProvider{
		http:            &http.Client{Timeout: googleTimeout},
		apiKey:          os.Getenv("GOOGLE_API_KEY"),
		languageCode:    cfg.Google.LanguageCode,
		voice:           cfg.Google.Voice,
		audioEncoding:   cfg.Google.AudioEncoding,
		sampleRateHertz: cfg.Google.SampleRateHertz,
	}
	if p.languageCode == "" {
		p.languageCode = "en-US"
	}
	if p.voice == "" {
		p.voice = "en-US-Neural2-D"
	}
	if p.audioEncoding == "" {
		p.audioEncoding = "LINEAR16"
	}
	if p.sampleRateHertz == 0 {
		p.sampleRateHertz = 24000
	}
	if p.apiKey == "" {
		if creds, err := google.FindDefaultCredentials(context.Background(), googleScope); err == nil && creds != nil {
			p.tokenSource = creds.TokenSource
		}
	}
	return p, nil
}

// Synthesize POSTs to texttospeech.googleapis.com/v1/text:synthesize and
// base64-decodes response.audioContent. Returns (bytes, FormatWAV, err).
func (p *GoogleProvider) Synthesize(ctx context.Context, text string) ([]byte, string, error) {
	if p.apiKey == "" && p.tokenSource == nil {
		return nil, FormatWAV, errGoogleNotConfigured
	}

	body, err := json.Marshal(googleSynthReq{
		Input: googleInput{Text: text},
		Voice: googleVoice{
			LanguageCode: p.languageCode,
			Name:         p.voice,
		},
		AudioConfig: googleAudio{
			AudioEncoding:   p.audioEncoding,
			SampleRateHertz: p.sampleRateHertz,
		},
	})
	if err != nil {
		return nil, FormatWAV, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleSynthURL, bytes.NewReader(body))
	if err != nil {
		return nil, FormatWAV, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Auth: API key in the X-Goog-Api-Key header (never the URL, to keep it out
	// of logs), else an OAuth bearer token from ADC.
	if p.apiKey != "" {
		req.Header.Set("X-Goog-Api-Key", p.apiKey)
	} else {
		tok, err := p.tokenSource.Token()
		if err != nil {
			return nil, FormatWAV, err
		}
		tok.SetAuthHeader(req)
	}

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, FormatWAV, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, FormatWAV, &ErrHTTPStatus{Provider: "google", Code: resp.StatusCode}
	}

	var out googleSynthResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, FormatWAV, err
	}
	audio, err := base64.StdEncoding.DecodeString(out.AudioContent)
	if err != nil {
		return nil, FormatWAV, err
	}
	return audio, FormatWAV, nil
}

// Validate synthesizes a short test phrase.
func (p *GoogleProvider) Validate(ctx context.Context) error {
	if p.apiKey == "" && p.tokenSource == nil {
		return errGoogleNotConfigured
	}
	audio, _, err := p.Synthesize(ctx, "test")
	if err != nil {
		return err
	}
	if len(audio) == 0 {
		return errors.New("google: empty audio response")
	}
	return nil
}

// Wire types for the REST call.
type googleSynthReq struct {
	Input       googleInput `json:"input"`
	Voice       googleVoice `json:"voice"`
	AudioConfig googleAudio `json:"audioConfig"`
}

type googleInput struct {
	Text string `json:"text"`
}

type googleVoice struct {
	LanguageCode string `json:"languageCode"`
	Name         string `json:"name"`
}

type googleAudio struct {
	AudioEncoding   string `json:"audioEncoding"`
	SampleRateHertz int    `json:"sampleRateHertz"`
}

type googleSynthResp struct {
	AudioContent string `json:"audioContent"` // base64
}
