// Package update checks GitHub releases for newer versions of macup and
// replaces the running binary with a released one.
package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	repo       = "Hinkolas/macup"
	binaryName = "macup"
	// maxAssetSize bounds downloads so a bad response can't exhaust memory
	maxAssetSize = 100 << 20
)

var client = &http.Client{Timeout: 2 * time.Minute}

// errNotFound is returned by get for 404 responses
var errNotFound = errors.New("not found")

// Latest returns the tag of the latest published (non-prerelease) release
func Latest(ctx context.Context) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	body, err := get(ctx, url, "application/vnd.github+json")
	if errors.Is(err, errNotFound) {
		return "", errors.New("no published release found")
	}
	if err != nil {
		return "", fmt.Errorf("fetch latest release: %w", err)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("parse latest release: %w", err)
	}
	if release.TagName == "" {
		return "", errors.New("latest release has no tag")
	}
	return release.TagName, nil
}

// Apply downloads the release with the given tag, verifies its checksum and
// atomically replaces the currently running executable with it
func Apply(ctx context.Context, tag string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("no release builds for %s", runtime.GOOS)
	}

	asset := fmt.Sprintf("%s_%s_%s.tar.gz", binaryName, runtime.GOOS, runtime.GOARCH)
	baseURL := fmt.Sprintf("https://github.com/%s/releases/download/%s", repo, tag)

	archive, err := get(ctx, baseURL+"/"+asset, "")
	if err != nil {
		return fmt.Errorf("download %s: %w", asset, err)
	}
	sums, err := get(ctx, baseURL+"/checksums.txt", "")
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	if err := verifyChecksum(archive, sums, asset); err != nil {
		return err
	}

	binary, err := extractBinary(archive)
	if err != nil {
		return err
	}

	target, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current executable: %w", err)
	}
	if target, err = filepath.EvalSymlinks(target); err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}

	if err := replaceFile(target, binary); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("no permission to replace %s; re-run with sudo or reinstall via install.sh", target)
		}
		return err
	}
	return nil
}

func get(ctx context.Context, url, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("GET %s: %w", url, errNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAssetSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxAssetSize {
		return nil, fmt.Errorf("GET %s: response too large", url)
	}
	return body, nil
}

// verifyChecksum checks data against the entry for name in a GoReleaser
// checksums.txt ("<sha256>  <name>" per line)
func verifyChecksum(data, sums []byte, name string) error {
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])

	scanner := bufio.NewScanner(bytes.NewReader(sums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[1] == name {
			if !strings.EqualFold(fields[0], actual) {
				return fmt.Errorf("checksum mismatch for %s", name)
			}
			return nil
		}
	}
	return fmt.Errorf("no checksum found for %s", name)
}

// extractBinary returns the contents of the macup binary in a .tar.gz archive
func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s not found in archive", binaryName)
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == binaryName {
			return io.ReadAll(io.LimitReader(tr, maxAssetSize))
		}
	}
}

// replaceFile writes data next to target and renames it over target, so the
// swap is atomic and a failed upgrade never leaves a partial binary behind
func replaceFile(target string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+binaryName+"-upgrade-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, target)
}
