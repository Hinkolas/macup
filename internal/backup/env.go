package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// DefaultEnvPatterns match .env files and their variants like .env.local
var DefaultEnvPatterns = []string{".env", ".env.*"}

// CopyEnvFiles searches the given locations for regular files whose name
// matches one of the patterns and copies them uncompressed into outputDir,
// readable only by the current user. Copies keep their path relative to the
// home directory (or their absolute path when outside of it).
func CopyEnvFiles(ctx context.Context, locations []Location, outputDir string, patterns []string) error {
	for _, p := range patterns {
		if _, err := filepath.Match(p, ""); err != nil {
			return fmt.Errorf("invalid pattern %q: %w", p, err)
		}
	}

	out, err := normalizePath(outputDir)
	if err != nil {
		return err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home dir: %w", err)
	}
	home = filepath.Clean(home)

	var warnings []string
	copied := 0

	for _, loc := range locations {
		root, err := normalizePath(loc.Path)
		if err != nil {
			return err
		}

		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if err != nil {
				// A missing root or unreadable entry doesn't stop the other files
				warnings = append(warnings, err.Error())
				if d != nil && d.IsDir() && path != root {
					return filepath.SkipDir
				}
				return nil
			}

			if path != root && slices.Contains(loc.Ignore, d.Name()) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			if d.IsDir() {
				// Never pick up our own copies when the output is inside a root
				if path == out {
					return filepath.SkipDir
				}
				return nil
			}

			if !d.Type().IsRegular() || !matchesAny(d.Name(), patterns) {
				return nil
			}

			rel := envDestPath(path, home)
			if err := copyFilePrivate(path, filepath.Join(out, rel)); err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", path, err))
				return nil
			}
			copied++
			fmt.Printf("✓ %s\n", rel)
			return nil
		})
		if err != nil {
			return err
		}
	}

	fmt.Printf("\n✓ %d .env file(s) copied to %s\n", copied, outputDir)

	if len(warnings) > 0 {
		fmt.Printf("\n⚠ %d path(s) skipped:\n", len(warnings))
		for _, w := range warnings {
			fmt.Printf("  - %s\n", w)
		}
	}

	return nil
}

// matchesAny reports whether name matches one of the glob patterns
func matchesAny(name string, patterns []string) bool {
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}
	return false
}

// envDestPath returns where a file is stored inside the output directory:
// relative to home when below it, otherwise its absolute path without the
// leading separator
func envDestPath(path, home string) string {
	if rel, err := filepath.Rel(home, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel
	}
	return strings.TrimPrefix(path, string(filepath.Separator))
}

// copyFilePrivate copies src to dst with owner-only permissions, keeping the
// original modification time. The copy is written to a temporary file first
// so an interrupted run never leaves a truncated file in place.
func copyFilePrivate(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}

	tmp, err := os.OpenFile(dst+".tmp", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	_, err = io.Copy(tmp, in)
	err = errors.Join(err, tmp.Close())
	if err == nil {
		err = os.Chtimes(tmp.Name(), info.ModTime(), info.ModTime())
	}
	if err == nil {
		err = os.Rename(tmp.Name(), dst)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}
