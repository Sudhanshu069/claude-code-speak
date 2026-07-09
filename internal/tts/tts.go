// Package tts converts text to a playable audio buffer through a small Provider
// interface plus a name->factory registry, mirroring Node src/tts.js. The
// player derives the temp-file extension from the format string a provider
// returns, so a new provider only needs to return a known Format* constant.
package tts

import (
	"context"
	"errors"
	"fmt"

	"github.com/Sudhanshu069/claude-says/internal/config"
)

// Audio format strings returned by Synthesize; the player derives the temp-file
// extension from these.
const (
	FormatAIFF = "aiff" // macOS `say`
	FormatWAV  = "wav"  // Google LINEAR16 (RIFF/WAV container)
	FormatMP3  = "mp3"  // ElevenLabs
)

// Provider converts text to a playable audio buffer. Small by design.
type Provider interface {
	// Synthesize renders text to audio. It returns the raw bytes and one of the
	// Format* constants. It must honor ctx cancellation/deadline.
	Synthesize(ctx context.Context, text string) (audio []byte, format string, err error)
	// Validate checks credentials/connectivity. nil == ok.
	Validate(ctx context.Context) error
}

// VoiceLister is an OPTIONAL capability used by the `voices` subcommand.
// Providers that cannot enumerate voices simply don't implement it.
type VoiceLister interface {
	Voices(ctx context.Context) ([]Voice, error)
}

// Voice is one selectable voice.
type Voice struct {
	ID       string // provider-native id (voice name / voice_id)
	Name     string // human label
	Language string // BCP-47 tag when known, else ""
}

// ErrUnknownProvider is returned by New for an unregistered provider name.
var ErrUnknownProvider = errors.New("unknown TTS provider")

// ErrHTTPStatus is a sentinel for non-2xx network responses. The response body
// is deliberately NOT included (it can leak account/voice details into logs).
type ErrHTTPStatus struct {
	Provider string
	Code     int
}

// Error implements error.
func (e *ErrHTTPStatus) Error() string {
	return fmt.Sprintf("%s API error %d", e.Provider, e.Code)
}

type factory func(*config.Config) (Provider, error)

var registry = map[string]factory{
	"macos":      newMacOS,
	"google":     newGoogle,
	"elevenlabs": newElevenLabs,
}

// providerOrder is the CLI display order, preserving Node's insertion order
// (google, elevenlabs, macos) instead of the random map / alphabetical order.
// List filters this by registry so a stale name here can never appear.
var providerOrder = []string{"google", "elevenlabs", "macos"}

// New builds the provider named by cfg.Provider (default "macos").
func New(cfg *config.Config) (Provider, error) {
	name := cfg.Provider
	if name == "" {
		name = "macos"
	}
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q (available: %v)", ErrUnknownProvider, name, List())
	}
	return f(cfg)
}

// List returns the registered provider names in Node's display order (see
// providerOrder), with any provider not listed there appended defensively so a
// new registration can never silently vanish from the CLI.
func List() []string {
	seen := make(map[string]bool, len(registry))
	names := make([]string, 0, len(registry))
	for _, name := range providerOrder {
		if _, ok := registry[name]; ok {
			names = append(names, name)
			seen[name] = true
		}
	}
	for name := range registry {
		if !seen[name] {
			names = append(names, name)
		}
	}
	return names
}
