package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCopyEnvFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := filepath.Join(home, "code")
	writeFile(t, filepath.Join(root, "app", ".env"), "SECRET=1")
	writeFile(t, filepath.Join(root, "app", "sub", ".env.local"), "SECRET=2")
	writeFile(t, filepath.Join(root, "app", "node_modules", "pkg", ".env"), "ignored")
	writeFile(t, filepath.Join(root, "py", ".env", "bin", "python"), "venv")
	writeFile(t, filepath.Join(root, "app", "main.go"), "package main")
	if err := os.MkdirAll(filepath.Join(root, "link"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "app", ".env"), filepath.Join(root, "link", ".env")); err != nil {
		t.Fatal(err)
	}

	// Output inside the scanned root must not be picked up again
	out := filepath.Join(root, "out")
	locs := []Location{{Path: root, Ignore: []string{"node_modules"}}}

	for range 2 {
		if err := CopyEnvFiles(context.Background(), locs, out, DefaultEnvPatterns); err != nil {
			t.Fatal(err)
		}
	}

	want := map[string]string{
		"code/app/.env":           "SECRET=1",
		"code/app/sub/.env.local": "SECRET=2",
	}
	got := map[string]string{}
	err := filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			if perm := info.Mode().Perm(); perm != 0700 && path != out {
				t.Errorf("%s: dir mode %o, want 700", path, perm)
			}
			return nil
		}
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Errorf("%s: file mode %o, want 600", path, perm)
		}
		rel, _ := filepath.Rel(out, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != len(want) {
		t.Fatalf("copied %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestEnvDestPath(t *testing.T) {
	cases := []struct{ path, home, want string }{
		{"/Users/me/app/.env", "/Users/me", "app/.env"},
		{"/opt/app/.env", "/Users/me", "opt/app/.env"},
		{"/Users/meow/.env", "/Users/me", "Users/meow/.env"},
	}
	for _, c := range cases {
		if got := envDestPath(c.path, c.home); got != c.want {
			t.Errorf("envDestPath(%q, %q) = %q, want %q", c.path, c.home, got, c.want)
		}
	}
}
