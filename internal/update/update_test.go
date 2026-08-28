package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		name            string
		current, latest string
		want            bool
	}{
		{"dev build is always behind", "dev", "v0.1.0", true},
		{"same version", "v0.1.0", "v0.1.0", false},
		{"patch bump", "v0.1.0", "v0.1.1", true},
		{"minor bump", "v0.1.9", "v0.2.0", true},
		{"major bump", "v0.9.9", "v1.0.0", true},
		{"latest is older", "v0.2.0", "v0.1.9", false},
		{"no v prefix", "0.1.0", "0.2.0", true},
		{"garbage latest is never newer", "v0.1.0", "nope", false},
		{"prerelease suffix ignored", "v0.1.0", "v0.2.0-rc1", true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := Newer(tt.current, tt.latest); got != tt.want {
				t.Fatalf("Newer(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

func TestAsset(t *testing.T) {
	cases := []struct {
		goos, goarch string
		want         string
	}{
		{"linux", "amd64", "arnes-linux-amd64"},
		{"darwin", "arm64", "arnes-darwin-arm64"},
		{"windows", "amd64", "arnes-windows-amd64.exe"},
	}
	for _, tt := range cases {
		if got := Asset(tt.goos, tt.goarch); got != tt.want {
			t.Errorf("Asset(%q, %q) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
		}
	}
}

// fakeRelease stands up an httptest server serving the GitHub releases/latest
// endpoint, the platform binary asset, and a matching checksums.txt.
func fakeRelease(t *testing.T, tag string, binary []byte) GitHub {
	t.Helper()
	asset := Asset(runtime.GOOS, runtime.GOARCH)
	digest := sha256.Sum256(binary)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(digest[:]), asset)

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/repos/o/n/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": tag,
			"body":     "release notes",
			"assets": []map[string]string{
				{"name": asset, "browser_download_url": srv.URL + "/dl/" + asset},
				{"name": "checksums.txt", "browser_download_url": srv.URL + "/dl/checksums.txt"},
			},
		})
	})
	mux.HandleFunc("/dl/"+asset, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(binary) })
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(checksums)) })

	return GitHub{Repo: "o/n", baseURL: srv.URL}
}

func TestGitHubLatest(t *testing.T) {
	gh := fakeRelease(t, "v0.3.0", []byte("binary"))
	rel, err := gh.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "v0.3.0" {
		t.Fatalf("Version = %q, want v0.3.0", rel.Version)
	}
	if _, ok := rel.Assets[Asset(runtime.GOOS, runtime.GOARCH)]; !ok {
		t.Fatalf("no platform asset in %v", rel.Assets)
	}
}

func TestCheck(t *testing.T) {
	gh := fakeRelease(t, "v0.3.0", []byte("bin"))

	rel, newer, err := Check(context.Background(), gh, "v0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if !newer || rel.Version != "v0.3.0" {
		t.Fatalf("newer=%v version=%q, want true/v0.3.0", newer, rel.Version)
	}

	if _, newer, _ := Check(context.Background(), gh, "v0.3.0"); newer {
		t.Fatal("equal versions must not report newer")
	}
}

func TestApplyReplacesBinary(t *testing.T) {
	newBin := []byte("#!/bin/sh\necho new\n")
	gh := fakeRelease(t, "v0.3.0", newBin)
	rel, err := gh.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "arnes")
	if err := os.WriteFile(dest, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Apply(context.Background(), rel, dest); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(newBin) {
		t.Fatalf("binary not replaced: %q", got)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(dest); fi.Mode().Perm()&0o111 == 0 {
			t.Fatalf("replacement is not executable: %v", fi.Mode())
		}
	}
}

func TestApplyRejectsBadChecksum(t *testing.T) {
	asset := Asset(runtime.GOOS, runtime.GOARCH)
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/repos/o/n/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v0.3.0",
			"assets": []map[string]string{
				{"name": asset, "browser_download_url": srv.URL + "/dl/bin"},
				{"name": "checksums.txt", "browser_download_url": srv.URL + "/dl/cs"},
			},
		})
	})
	mux.HandleFunc("/dl/bin", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("real bytes")) })
	mux.HandleFunc("/dl/cs", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", "0000000000000000000000000000000000000000000000000000000000000000", asset)
	})

	gh := GitHub{Repo: "o/n", baseURL: srv.URL}
	rel, err := gh.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "arnes")
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Apply(context.Background(), rel, dest); err == nil {
		t.Fatal("expected a checksum mismatch error")
	}
	if got, _ := os.ReadFile(dest); string(got) != "old" {
		t.Fatalf("dest must be untouched on failure, got %q", got)
	}
}
