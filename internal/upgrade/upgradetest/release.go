package upgradetest

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/upgrade"
)

// Platforms are the os/arch pairs .goreleaser.yaml builds.
var Platforms = []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64", "windows/arm64"}

// Release describes a fake ytrack release.
type Release struct {
	Version    string // without "v"
	Prerelease bool
	// Binary returns the ytrack executable for a platform ("linux/amd64");
	// nil means a marker text naming the version and platform.
	Binary func(platform string) []byte
	// Subject is the signer identity; ReleaseSubject when empty.
	Subject string
	// WrongChecksums lists a wrong digest for every archive.
	WrongChecksums bool
	// SignOther makes the bundle sign different bytes than the checksums file.
	SignOther bool
	// Unsigned leaves out the signature bundle.
	Unsigned bool
}

// BinaryFor returns the binary content of r for platform.
func (r *Release) BinaryFor(platform string) []byte {
	if r.Binary != nil {
		return r.Binary(platform)
	}
	return []byte(fmt.Sprintf("ytrack %s %s\n", r.Version, platform))
}

// Tag returns "v" + Version.
func (r *Release) Tag() string { return "v" + r.Version }

// Assets builds the release files: one archive per platform, the checksums
// file and its Sigstore bundle, signed with s.
func (r *Release) Assets(t *testing.T, s *Sigstore) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	var sums strings.Builder
	for _, p := range Platforms {
		goos, goarch, _ := strings.Cut(p, "/")
		name := upgrade.ArchiveName(r.Version, goos, goarch)
		data := archive(t, name, r.BinaryFor(p))
		files[name] = data
		sum := sha256.Sum256(data)
		digest := hex.EncodeToString(sum[:])
		if r.WrongChecksums {
			other := sha256.Sum256(append(data, 'x'))
			digest = hex.EncodeToString(other[:])
		}
		fmt.Fprintf(&sums, "%s  %s\n", digest, name)
	}
	checksums := []byte(sums.String())
	files[upgrade.ChecksumsName(r.Version)] = checksums
	if !r.Unsigned {
		subject := r.Subject
		if subject == "" {
			subject = ReleaseSubject
		}
		signed := checksums
		if r.SignOther {
			signed = append([]byte("other "), checksums...)
		}
		files[upgrade.BundleName(r.Version)] = s.Sign(signed, subject, GitHubIssuer)
	}
	return files
}

func archive(t *testing.T, name string, bin []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if strings.HasSuffix(name, ".zip") {
		zw := zip.NewWriter(&buf)
		for _, f := range []struct {
			name string
			data []byte
		}{{"LICENSE", []byte("MIT\n")}, {"ytrack.exe", bin}} {
			w, err := zw.Create(f.name)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write(f.data)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		data []byte
		mode int64
	}{{"LICENSE", []byte("MIT\n"), 0o644}, {"completions/ytrack.bash", []byte("# ytrack\n"), 0o644}, {"ytrack", bin, 0o755}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write(f.data)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Server is a fake GitHub serving the releases API and release downloads
// over TLS. Use Server.Client() as the HTTP client.
type Server struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
	releases []*Release
	assets   map[string]map[string][]byte // tag → name → data
}

// NewServer serves releases; the last one that is not a prerelease is the
// latest.
func NewServer(t *testing.T, s *Sigstore, releases ...*Release) *Server {
	t.Helper()
	srv := &Server{releases: releases, assets: map[string]map[string][]byte{}}
	for _, r := range releases {
		srv.assets[r.Tag()] = r.Assets(t, s)
	}
	srv.Server = httptest.NewTLSServer(http.HandlerFunc(srv.serve))
	t.Cleanup(srv.Close)
	return srv
}

// Requests returns the paths requested so far.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.URL.Path)
	s.mu.Unlock()
	const api = "/repos/BrainerVirus/youtrack-cli/releases/"
	switch {
	case r.URL.Path == api+"latest":
		var latest *Release
		for _, rel := range s.releases {
			if !rel.Prerelease {
				latest = rel
			}
		}
		if latest == nil {
			http.NotFound(w, r)
			return
		}
		s.writeRelease(w, latest)
	case strings.HasPrefix(r.URL.Path, api+"tags/"):
		tag := strings.TrimPrefix(r.URL.Path, api+"tags/")
		for _, rel := range s.releases {
			if rel.Tag() == tag {
				s.writeRelease(w, rel)
				return
			}
		}
		http.NotFound(w, r)
	case strings.HasPrefix(r.URL.Path, "/download/"):
		tag, name, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/download/"), "/")
		data, ok := s.assets[tag][name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) writeRelease(w http.ResponseWriter, rel *Release) {
	names := make([]string, 0, len(s.assets[rel.Tag()]))
	for n := range s.assets[rel.Tag()] {
		names = append(names, n)
	}
	sort.Strings(names)
	assets := make([]map[string]any, 0, len(names))
	for _, n := range names {
		assets = append(assets, map[string]any{
			"name":                 n,
			"browser_download_url": s.URL + "/download/" + rel.Tag() + "/" + n,
			"size":                 len(s.assets[rel.Tag()][n]),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"tag_name":   rel.Tag(),
		"html_url":   "https://github.com/BrainerVirus/youtrack-cli/releases/tag/" + rel.Tag(),
		"prerelease": rel.Prerelease,
		"draft":      false,
		"assets":     assets,
	})
}

// WriteDir lays out releases as a static mirror for the install scripts:
// <dir>/api/releases/latest (JSON) and <dir>/download/<tag>/<asset>.
func WriteDir(t *testing.T, dir string, s *Sigstore, releases ...*Release) {
	t.Helper()
	var latest *Release
	for _, rel := range releases {
		if !rel.Prerelease {
			latest = rel
		}
		for name, data := range rel.Assets(t, s) {
			p := filepath.Join(dir, "download", rel.Tag(), name)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if latest != nil {
		p := filepath.Join(dir, "api", "releases", "latest")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		b, _ := json.MarshalIndent(map[string]any{"tag_name": latest.Tag(), "prerelease": false}, "", "  ")
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
