// Package narrator rephrases assistant text through an LLM before TTS, mirroring
// Node src/narrator.js. The Narrate method is TOTAL by type — it returns only a
// string — so a narrator failure can never propagate up and drop a sentence
// (Node bug #11 defense). Validate is separate for the setup wizard, where a
// real error IS wanted.
package narrator

import (
	"context"
	"errors"
	"fmt"

	"github.com/Sudhanshu069/claude-says/internal/config"
)

// Narrator rephrases assistant text before TTS. Narrate is TOTAL: it returns
// only a string, returning the input verbatim on any error.
type Narrator interface {
	Narrate(ctx context.Context, text string) string
	Validate(ctx context.Context) error
}

// Degrader is an optional interface a Narrator may implement to report when a
// Narrate call degraded to the input verbatim (LLM unreachable/errored). It lets
// callers log the fallback (logger.warn) while keeping Narrate itself total.
type Degrader interface {
	// NarrateOrErr behaves like Narrate but also returns the underlying error
	// when it fell back to the input text. The returned string is ALWAYS safe to
	// speak (input on any failure), so callers can ignore the error and still
	// never drop a sentence.
	NarrateOrErr(ctx context.Context, text string) (out string, err error)
}

// ErrUnknownNarrator is returned by New for an unregistered narrator name.
var ErrUnknownNarrator = errors.New("unknown narrator provider")

type factory func(*config.Config) (Narrator, error)

var registry = map[string]factory{
	"gemini": newGemini, // cloud (Google) — redacts before egress
	"ollama": newOllama, // local — nothing leaves the machine
}

// New builds the narrator named by cfg.Narrator.Provider (default "gemini").
func New(cfg *config.Config) (Narrator, error) {
	name := cfg.Narrator.Provider
	if name == "" {
		name = "gemini"
	}
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownNarrator, name)
	}
	return f(cfg)
}

// List returns the registered narrator names.
func List() []string {
	names := make([]string, 0, len(registry))
	for k := range registry {
		names = append(names, k)
	}
	return names
}
