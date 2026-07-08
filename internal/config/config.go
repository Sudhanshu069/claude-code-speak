// Package config loads and persists claude-says configuration from
// ~/.claude-says/config.json, mirroring the Node src/config.js behaviour:
// a fresh DefaultConfig() overlaid with the saved JSON (safe merge for free),
// owner-only (0600) files, atomic writes, and the socket kept inside the
// per-user config dir rather than world-writable /tmp.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config reproduces Node DEFAULT_CONFIG field-for-field (camelCase JSON tags),
// plus a TextProcessor.FlushDelay that the Node code only had as a constructor
// default (1500ms).
type Config struct {
	Provider      string              `json:"provider"`
	Macos         MacosConfig         `json:"macos"`
	Google        GoogleConfig        `json:"google"`
	ElevenLabs    ElevenLabsConfig    `json:"elevenlabs"`
	Playback      PlaybackConfig      `json:"playback"`
	TextProcessor TextProcessorConfig `json:"textProcessor"`
	Narrator      NarratorConfig      `json:"narrator"`
}

// MacosConfig configures the macOS `say` provider.
type MacosConfig struct {
	Voice string `json:"voice"`
	Rate  int    `json:"rate"`
}

// GoogleConfig configures the Google Cloud TTS REST provider.
type GoogleConfig struct {
	Voice           string `json:"voice"`
	LanguageCode    string `json:"languageCode"`
	AudioEncoding   string `json:"audioEncoding"`
	SampleRateHertz int    `json:"sampleRateHertz"`
}

// ElevenLabsConfig configures the ElevenLabs provider.
type ElevenLabsConfig struct {
	VoiceID string `json:"voiceId"`
	ModelID string `json:"modelId"`
}

// PlaybackConfig configures audio playback (currently only "afplay").
type PlaybackConfig struct {
	Method string `json:"method"`
}

// TextProcessorConfig configures sentence buffering/splitting.
type TextProcessorConfig struct {
	MinChunkLength int `json:"minChunkLength"`
	MaxChunkLength int `json:"maxChunkLength"`
	FlushDelay     int `json:"flushDelay"` // ms; Node text-processor.js default 1500
}

// NarratorConfig configures the optional LLM narrator.
type NarratorConfig struct {
	Enabled  bool         `json:"enabled"`
	Provider string       `json:"provider"`
	Gemini   GeminiConfig `json:"gemini"`
}

// GeminiConfig configures the Gemini narrator.
type GeminiConfig struct {
	Model string `json:"model"`
}

// DefaultConfig mirrors Node DEFAULT_CONFIG (config.js) plus FlushDelay=1500.
func DefaultConfig() Config {
	return Config{
		Provider: "macos",
		Macos: MacosConfig{
			Voice: "Samantha",
			Rate:  200,
		},
		Google: GoogleConfig{
			Voice:           "en-US-Neural2-D",
			LanguageCode:    "en-US",
			AudioEncoding:   "LINEAR16",
			SampleRateHertz: 24000,
		},
		ElevenLabs: ElevenLabsConfig{
			VoiceID: "21m00Tcm4TlvDq8ikWAM",
			ModelID: "eleven_turbo_v2_5",
		},
		Playback: PlaybackConfig{
			Method: "afplay",
		},
		TextProcessor: TextProcessorConfig{
			MinChunkLength: 10,
			MaxChunkLength: 500,
			FlushDelay:     1500,
		},
		Narrator: NarratorConfig{
			Enabled:  false,
			Provider: "gemini",
			Gemini: GeminiConfig{
				Model: "gemini-2.5-flash",
			},
		},
	}
}

// Load builds DefaultConfig(), then json.Unmarshal-overlays the saved file on
// top of it. Go's encoding/json only sets fields present in the JSON, leaving
// absent nested fields at their default — the idiomatic equivalent of Node's
// deepMerge. A missing or unparseable file returns DefaultConfig().
func Load() (Config, error) {
	cfg := DefaultConfig()
	path, err := ConfigFile()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		// Mirror Node's try/catch returning defaults on parse error.
		return DefaultConfig(), nil
	}
	return cfg, nil
}

// Save writes JSON to ConfigFile(): MkdirAll 0700, temp file + os.Rename
// (atomic), os.Chmod 0600 best-effort. Config may hold API keys, so the file is
// owner-only. Mirrors Node saveConfig (config.js): the write is the contract,
// the chmod is best-effort hardening that must never fail the save.
func (c Config) Save() error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path, err := ConfigFile()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	// Write to a temp file in the same dir, then rename over the target so a
	// reader never observes a partially written config. Create the temp file
	// 0600 up front so secrets are never briefly world-readable.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}

	// Best-effort: tighten a pre-existing file that may have been created
	// world-readable (rename preserves the temp file's mode, but a prior save
	// under a different umask/uid could have left the target loose). EPERM on a
	// file owned by another uid must not fail the save.
	_ = os.Chmod(path, 0o600)
	return nil
}

// ConfigDir returns ~/.claude-says.
func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude-says"), nil
}

// ConfigFile returns ~/.claude-says/config.json.
func ConfigFile() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// SocketPath returns ~/.claude-says/claude-says.sock.
func SocketPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "claude-says.sock"), nil
}
