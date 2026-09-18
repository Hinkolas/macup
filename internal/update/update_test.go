package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func makeArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("archive")
	sum := sha256.Sum256(data)
	sums := []byte(hex.EncodeToString(sum[:]) + "  macup_darwin_arm64.tar.gz\n" +
		"deadbeef  macup_darwin_amd64.tar.gz\n")

	if err := verifyChecksum(data, sums, "macup_darwin_arm64.tar.gz"); err != nil {
		t.Errorf("valid checksum rejected: %v", err)
	}
	if err := verifyChecksum(data, sums, "macup_darwin_amd64.tar.gz"); err == nil {
		t.Error("mismatching checksum accepted")
	}
	if err := verifyChecksum(data, sums, "missing.tar.gz"); err == nil {
		t.Error("missing checksum entry accepted")
	}
}

func TestExtractBinary(t *testing.T) {
	archive := makeArchive(t, map[string]string{"README.md": "readme", "macup": "binary"})
	got, err := extractBinary(archive)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "binary" {
		t.Errorf("got %q, want %q", got, "binary")
	}

	if _, err := extractBinary(makeArchive(t, map[string]string{"README.md": "readme"})); err == nil {
		t.Error("archive without binary accepted")
	}
}

func TestReplaceFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "macup")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := replaceFile(target, []byte("new")); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("got %q, want %q", got, "new")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp file left behind: %v", entries)
	}
}
