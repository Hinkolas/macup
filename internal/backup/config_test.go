package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	config := "data:\n  locations:\n    - path: ~/code\n"
	writeFile(t, filepath.Join(home, ".config", "macup", "config.yaml"), config)

	// Run from elsewhere so a relative "~" directory can't be found by accident
	t.Chdir(t.TempDir())

	cfg, err := LoadConfig("~/.config/macup/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Data.Locations) != 1 || cfg.Data.Locations[0].Path != "~/code" {
		t.Fatalf("unexpected locations: %+v", cfg.Data.Locations)
	}

	out := t.TempDir()
	if err := copyConfigToBackup("~/.config/macup/config.yaml", out); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(out, "config.yaml")); err != nil || string(data) != config {
		t.Fatalf("config copy = %q, %v", data, err)
	}
}
