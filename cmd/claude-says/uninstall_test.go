package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Sudhanshu069/claude-says/internal/config"
)

// runUninstall removes ~/.claude-says when it exists and honors --keep-config.
func TestRunUninstallRemovesConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir, err := config.ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"provider":"macos"}`), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	// --keep-config leaves the dir in place.
	if err := runUninstall(true); err != nil {
		t.Fatalf("runUninstall(keepConfig=true): %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("~/.claude-says removed despite --keep-config: %v", err)
	}

	// Default removes the config dir.
	if err := runUninstall(false); err != nil {
		t.Fatalf("runUninstall(keepConfig=false): %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("~/.claude-says still present after uninstall (err=%v)", err)
	}
}
