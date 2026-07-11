package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

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

// parseStart builds a command with the start flags bound and parses argv so
// Changed() reflects exactly what was "passed" — mirrors production wiring.
func parseStart(t *testing.T, argv ...string) (*cobra.Command, *startOptions) {
	t.Helper()
	o := &startOptions{}
	cmd := &cobra.Command{Use: "test"}
	bindStartFlags(cmd.Flags(), o)
	if err := cmd.ParseFlags(argv); err != nil {
		t.Fatalf("ParseFlags(%v): %v", argv, err)
	}
	return cmd, o
}

// Preference flags persist to config.json; privacy/mode flags never do.
func TestPersistPreferencesSavesOnlySettings(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // config dir resolves under here

	cmd, o := parseStart(t,
		"--voice", "Daniel", "--rate", "180", "--volume", "0.7",
		"--narrator", "--verbatim", "--skip", "foo",
	)
	persistPreferences(cmd, o)

	saved, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Preferences persisted.
	if saved.Macos.Voice != "Daniel" || saved.Macos.Rate != 180 || saved.Macos.Volume != 0.7 {
		t.Errorf("preferences not persisted: %+v", saved.Macos)
	}
	// Privacy/mode flags must NOT persist.
	if saved.Narrator.Enabled {
		t.Error("--narrator must not be persisted (privacy/data-egress)")
	}
	if len(saved.TextProcessor.Skip) != 0 {
		t.Errorf("--skip must not be persisted, got %v", saved.TextProcessor.Skip)
	}
	// The filtering defaults are untouched (still on) in the saved file.
	if !saved.TextProcessor.Dedupe || !saved.TextProcessor.FilterFiller {
		t.Error("saved file should keep the on-by-default filtering")
	}
}

// With no preference flags set, nothing is written (no surprise config file).
func TestPersistPreferencesNoopWithoutFlags(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd, o := parseStart(t, "--narrator") // a non-preference flag only
	persistPreferences(cmd, o)

	dir, _ := config.ConfigDir()
	if _, err := os.Stat(filepath.Join(dir, "config.json")); !os.IsNotExist(err) {
		t.Errorf("no preference flags => config.json must not be created (stat err = %v)", err)
	}
}
