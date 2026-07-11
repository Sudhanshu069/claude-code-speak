package main

import (
	"testing"

	"github.com/Sudhanshu069/claude-says/internal/config"
)

// applyOverrides is the crux of the "filtering on by default, opt out" contract.
// These assert the exact opt-out semantics so a regression can't silently change
// what the user hears.
func TestApplyOverridesFilteringDefaults(t *testing.T) {
	base := config.DefaultConfig() // Dedupe + FilterFiller default true

	t.Run("no flags keeps defaults on", func(t *testing.T) {
		got := applyOverrides(base, startOptions{})
		if !got.TextProcessor.Dedupe || !got.TextProcessor.FilterFiller {
			t.Fatalf("defaults = dedupe %v / filler %v, want both true", got.TextProcessor.Dedupe, got.TextProcessor.FilterFiller)
		}
	})

	t.Run("--no-dedupe disables only dedupe", func(t *testing.T) {
		got := applyOverrides(base, startOptions{NoDedupe: true})
		if got.TextProcessor.Dedupe {
			t.Error("--no-dedupe should turn dedupe off")
		}
		if !got.TextProcessor.FilterFiller {
			t.Error("--no-dedupe must not touch the filler filter")
		}
	})

	t.Run("--verbatim disables everything", func(t *testing.T) {
		got := applyOverrides(base, startOptions{Verbatim: true, Skip: []string{"noise"}})
		if got.TextProcessor.Dedupe || got.TextProcessor.FilterFiller {
			t.Errorf("--verbatim should disable all filtering, got dedupe %v / filler %v", got.TextProcessor.Dedupe, got.TextProcessor.FilterFiller)
		}
		if len(got.TextProcessor.Skip) != 0 {
			t.Errorf("--verbatim should ignore --skip, got %v", got.TextProcessor.Skip)
		}
	})

	t.Run("--skip adds to the config list", func(t *testing.T) {
		got := applyOverrides(base, startOptions{Skip: []string{"let me", "now i"}})
		if len(got.TextProcessor.Skip) != 2 {
			t.Fatalf("skip = %v, want two patterns", got.TextProcessor.Skip)
		}
	})
}
