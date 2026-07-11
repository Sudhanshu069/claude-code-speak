// Package tts converts text to a playable audio buffer through a small Provider
// interface. macOS `say` is the only provider; the interface is kept so the
// daemon can inject a fake in tests and a future provider can slot back in. The
// player derives the temp-file extension from the format string Synthesize
// returns.
package tts

import (
	"context"

	"github.com/Sudhanshu069/claude-says/internal/config"
)

// Audio format strings returned by Synthesize; the player derives the temp-file
// extension from these.
const (
	FormatAIFF = "aiff" // macOS `say`
	FormatWAV  = "wav"  // generic non-AIFF container (used by the daemon tests)
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

// New builds the TTS provider. Only macOS `say` is supported now, so cfg.Provider
// is ignored — a config still selecting a removed cloud provider transparently
// falls back to macOS rather than failing at start.
func New(cfg *config.Config) (Provider, error) {
	return newMacOS(cfg)
}
