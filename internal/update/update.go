// Package update checks GitHub Releases for a newer arnes and, on request,
// replaces the running binary with it in place.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// DefaultRepo is the GitHub "owner/name" arnes releases are published to.
const DefaultRepo = "MauricioJC3/arnes_ng"

// Release is a published GitHub release.
type Release struct {
	Version string            // the tag, e.g. "v0.2.0"
	Notes   string            // the release body
	Assets  map[string]string // asset file name -> download URL
}

// Source yields the latest release.
type Source interface {
	Latest(ctx context.Context) (Release, error)
}

// GitHub reads releases from the public GitHub REST API. The zero value works:
// Repo defaults to DefaultRepo and a 15s HTTP client is used.
type GitHub struct {
	Repo    string
	Client  *http.Client
	baseURL string // test seam; defaults to https://api.github.com
}

func (g GitHub) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// Latest implements Source.
func (g GitHub) Latest(ctx context.Context) (Release, error) {
	repo := g.Repo
	if repo == "" {
		repo = DefaultRepo
	}
	base := g.baseURL
	if base == "" {
		base = "https://api.github.com"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/repos/%s/releases/latest", base, repo), nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := g.client().Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github: %s", resp.Status)
	}

	var payload struct {
		TagName string `json:"tag_name"`
		Body    string `json:"body"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Release{}, err
	}
	if payload.TagName == "" {
		return Release{}, fmt.Errorf("github: respuesta sin tag_name")
	}

	rel := Release{Version: payload.TagName, Notes: payload.Body, Assets: map[string]string{}}
	for _, a := range payload.Assets {
		rel.Assets[a.Name] = a.URL
	}
	return rel, nil
}

// Check fetches the latest release and reports whether it is newer than current.
func Check(ctx context.Context, src Source, current string) (rel Release, newer bool, err error) {
	rel, err = src.Latest(ctx)
	if err != nil {
		return Release{}, false, err
	}
	return rel, Newer(current, rel.Version), nil
}

// Newer reports whether the version tag latest is greater than current. A
// current that is not semver (e.g. the "dev" build tag) counts as older than
// any real release.
func Newer(current, latest string) bool {
	lc, ok := parseSemver(latest)
	if !ok {
		return false
	}
	cc, ok := parseSemver(current)
	if !ok {
		return true
	}
	for i := range 3 {
		if cc[i] != lc[i] {
			return lc[i] > cc[i]
		}
	}
	return false
}

// parseSemver reads "v1.2.3" (or "1.2.3"), ignoring any -prerelease / +build
// suffix. It returns false when the core is not three dotted integers.
func parseSemver(s string) ([3]int, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

// Asset is the release asset file name for a platform. It must match the matrix
// in .github/workflows/release.yml.
func Asset(goos, goarch string) string {
	name := fmt.Sprintf("arnes-%s-%s", goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// SelfPath returns the absolute path of the running binary, symlinks resolved.
func SelfPath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return p, nil
}

// Apply downloads the release asset for the current platform, verifies its
// SHA-256 against the release's checksums.txt (when present), and atomically
// replaces the binary at dest. On any failure before the swap, dest is left
// untouched.
func Apply(ctx context.Context, rel Release, dest string) error {
	asset := Asset(runtime.GOOS, runtime.GOARCH)
	url, ok := rel.Assets[asset]
	if !ok {
		return fmt.Errorf("el release %s no trae binario para %s/%s", rel.Version, runtime.GOOS, runtime.GOARCH)
	}

	// A temp file in the destination directory keeps the final rename atomic
	// (same filesystem).
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".arnes-update-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename below succeeds

	sum := sha256.New()
	if err := download(ctx, url, io.MultiWriter(tmp, sum)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if csURL, ok := rel.Assets["checksums.txt"]; ok {
		want, err := checksumFor(ctx, csURL, asset)
		if err != nil {
			return err
		}
		if got := hex.EncodeToString(sum.Sum(nil)); got != want {
			return fmt.Errorf("checksum no coincide para %s (esperado %s, obtenido %s)", asset, want, got)
		}
	}

	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}

	// Windows cannot rename over a running executable; move it aside first.
	if runtime.GOOS == "windows" {
		_ = os.Remove(dest + ".old")
		if err := os.Rename(dest, dest+".old"); err != nil {
			return err
		}
	}
	return os.Rename(tmpName, dest)
}

func download(ctx context.Context, url string, dst io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("descarga %s: %s", url, resp.Status)
	}
	_, err = io.Copy(dst, resp.Body)
	return err
}

// checksumFor downloads a `sha256sum`-style file and returns the hex digest
// listed for asset.
func checksumFor(ctx context.Context, url, asset string) (string, error) {
	var buf strings.Builder
	if err := download(ctx, url, &buf); err != nil {
		return "", err
	}
	sc := bufio.NewScanner(strings.NewReader(buf.String()))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("checksums.txt no tiene una entrada para %s", asset)
}
