package upgrade_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/update"
	"github.com/BrainerVirus/youtrack-cli/internal/upgrade"
	"github.com/BrainerVirus/youtrack-cli/internal/upgrade/upgradetest"
)

var checksums = []byte("0123  ytrack_1.0.0_linux_amd64.tar.gz\n")

func TestVerifyBundle(t *testing.T) {
	s := upgradetest.NewSigstore(t)
	trust := s.Trust()

	for name, sign := range map[string]func([]byte, string, string) []byte{
		"a Rekor entry and a signed timestamp":      s.Sign,
		"a Rekor entry only (cosign with Rekor v1)": s.SignTlogOnly,
	} {
		t.Run("given a release workflow signature with "+name+", it verifies", func(t *testing.T) {
			if err := upgrade.VerifyBundle(sign(checksums, upgradetest.ReleaseSubject, upgradetest.GitHubIssuer), checksums, trust); err != nil {
				t.Fatal(err)
			}
		})
	}

	t.Run("given a release workflow signature for refs/tags/v1.2.3, it verifies", func(t *testing.T) {
		subject := "https://github.com/BrainerVirus/youtrack-cli/.github/workflows/release.yml@refs/tags/v1.2.3"
		if err := upgrade.VerifyBundle(s.Sign(checksums, subject, upgradetest.GitHubIssuer), checksums, trust); err != nil {
			t.Fatal(err)
		}
	})

	good := func() []byte { return s.Sign(checksums, upgradetest.ReleaseSubject, upgradetest.GitHubIssuer) }
	refused := map[string]struct {
		bundle   func() []byte
		artifact []byte
		trust    *upgrade.Trust
	}{
		"another repository's workflow": {
			bundle: func() []byte {
				return s.Sign(checksums, "https://github.com/evil/youtrack-cli/.github/workflows/release.yml@refs/tags/v1.0.0", upgradetest.GitHubIssuer)
			},
		},
		"another workflow of this repository": {
			bundle: func() []byte {
				return s.Sign(checksums, "https://github.com/BrainerVirus/youtrack-cli/.github/workflows/ci.yml@refs/heads/main", upgradetest.GitHubIssuer)
			},
		},
		"a run dispatched from main": {
			bundle: func() []byte {
				return s.Sign(checksums, "https://github.com/BrainerVirus/youtrack-cli/.github/workflows/release.yml@refs/heads/main", upgradetest.GitHubIssuer)
			},
		},
		"a feature branch": {
			bundle: func() []byte {
				return s.Sign(checksums, "https://github.com/BrainerVirus/youtrack-cli/.github/workflows/release.yml@refs/heads/evil", upgradetest.GitHubIssuer)
			},
		},
		"another OIDC issuer": {
			bundle: func() []byte { return s.Sign(checksums, upgradetest.ReleaseSubject, "https://accounts.example.com") },
		},
		"a changed file": {bundle: good, artifact: append([]byte("x"), checksums...)},
		"an untrusted Sigstore": {bundle: func() []byte {
			return upgradetest.NewSigstore(t).Sign(checksums, upgradetest.ReleaseSubject, upgradetest.GitHubIssuer)
		}},
		"the SCT check on, for certificates without SCTs": {
			bundle: good,
			trust:  &upgrade.Trust{Material: trust.Material, Identity: upgrade.ReleaseIdentity},
		},
		"a tampered log entry": {
			bundle: func() []byte {
				return editBundle(t, good(), func(b map[string]any) {
					e := b["verificationMaterial"].(map[string]any)["tlogEntries"].([]any)[0].(map[string]any)
					e["logIndex"] = "1"
				})
			},
		},
		"no log entry": {
			bundle: func() []byte {
				return editBundle(t, good(), func(b map[string]any) {
					delete(b["verificationMaterial"].(map[string]any), "tlogEntries")
				})
			},
		},
		"a swapped signature": {
			bundle: func() []byte {
				var o map[string]any
				_ = json.Unmarshal(s.Sign([]byte("other"), upgradetest.ReleaseSubject, upgradetest.GitHubIssuer), &o)
				return editBundle(t, good(), func(b map[string]any) {
					b["messageSignature"].(map[string]any)["signature"] = o["messageSignature"].(map[string]any)["signature"]
				})
			},
		},
		"garbage": {bundle: func() []byte { return []byte("not json") }},
	}
	for name, tc := range refused {
		t.Run("given "+name+", it refuses", func(t *testing.T) {
			artifact := tc.artifact
			if artifact == nil {
				artifact = checksums
			}
			tr := trust
			if tc.trust != nil {
				tr = *tc.trust
			}
			err := upgrade.VerifyBundle(tc.bundle(), artifact, tr)
			if err == nil {
				t.Fatal("verified, want an error")
			}
			t.Log(err)
		})
	}
}

func editBundle(t *testing.T, b []byte, edit func(map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestReleaseIdentityAcceptsOnlyReleaseRefs(t *testing.T) {
	const wf = "https://github.com/BrainerVirus/youtrack-cli/.github/workflows/release.yml@"
	for ref, want := range map[string]bool{
		"refs/tags/v1.2.3":       true,
		"refs/tags/v1.2.3-rc.1":  true,
		"refs/heads/main":        false,
		"refs/tags/latest":       false,
		"refs/heads/main-evil":   false,
		"refs/pull/1/merge":      false,
		"refs/tags/v1.2.3\nmore": false,
	} {
		if got := regexp.MustCompile(upgrade.ReleaseIdentity.SubjectRegexp).MatchString(wf + ref); got != want {
			t.Errorf("%s: match = %v, want %v", ref, got, want)
		}
	}
}

func TestChecksums(t *testing.T) {
	sums := []byte("" +
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  ytrack_1.0.0_darwin_arm64.tar.gz\n" +
		"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08  ytrack_1.0.0_linux_amd64.tar.gz\n")
	want, err := upgrade.ExpectedSHA256(sums, "ytrack_1.0.0_linux_amd64.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if err := upgrade.CheckSHA256([]byte("test"), want); err != nil {
		t.Errorf("given the listed file, it matches: %v", err)
	}
	if err := upgrade.CheckSHA256([]byte("tampered"), want); err == nil {
		t.Error("given a different file, it should not match")
	}
	if _, err := upgrade.ExpectedSHA256(sums, "ytrack_1.0.0_windows_amd64.zip"); err == nil {
		t.Error("given an unlisted file, it should fail")
	}
}

func TestDownloaderBinary(t *testing.T) {
	s := upgradetest.NewSigstore(t)
	newDownloader := func(srv *upgradetest.Server, goos, goarch string) *upgrade.Downloader {
		return &upgrade.Downloader{HTTP: srv.Client(), Trust: s.Trust(), GOOS: goos, GOARCH: goarch}
	}
	release := func(t *testing.T, srv *upgradetest.Server) *update.Release {
		t.Helper()
		rel, err := (&update.Client{HTTP: srv.Client(), APIURL: srv.URL}).Latest(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return rel
	}

	for _, p := range []string{"linux/arm64", "darwin/amd64", "windows/amd64"} {
		t.Run("given a signed release, it returns the "+p+" binary", func(t *testing.T) {
			rel := &upgradetest.Release{Version: "1.1.0"}
			srv := upgradetest.NewServer(t, s, rel)
			goos, goarch, _ := strings.Cut(p, "/")
			bin, err := newDownloader(srv, goos, goarch).Binary(context.Background(), release(t, srv))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(bin, rel.BinaryFor(p)) {
				t.Errorf("binary = %q", bin)
			}
		})
	}

	for name, rel := range map[string]*upgradetest.Release{
		"a wrong checksum":            {Version: "1.1.0", WrongChecksums: true},
		"a signature over other data": {Version: "1.1.0", SignOther: true},
		"a signature by another repo": {Version: "1.1.0", Subject: "https://github.com/evil/fork/.github/workflows/release.yml@refs/tags/v1.1.0"},
		"no signature":                {Version: "1.1.0", Unsigned: true},
	} {
		t.Run("given "+name+", it refuses", func(t *testing.T) {
			srv := upgradetest.NewServer(t, s, rel)
			_, err := newDownloader(srv, "linux", "amd64").Binary(context.Background(), release(t, srv))
			if !errors.Is(err, upgrade.ErrRefused) {
				t.Fatalf("err = %v, want ErrRefused", err)
			}
		})
	}

	t.Run("given a bad signature, it does not download the archive", func(t *testing.T) {
		srv := upgradetest.NewServer(t, s, &upgradetest.Release{Version: "1.1.0", SignOther: true})
		_, _ = newDownloader(srv, "linux", "amd64").Binary(context.Background(), release(t, srv))
		for _, p := range srv.Requests() {
			if strings.HasSuffix(p, ".tar.gz") {
				t.Errorf("downloaded %s before the signature check passed", p)
			}
		}
	})

	t.Run("given a platform without a build, it says so", func(t *testing.T) {
		srv := upgradetest.NewServer(t, s, &upgradetest.Release{Version: "1.1.0"})
		_, err := newDownloader(srv, "plan9", "386").Binary(context.Background(), release(t, srv))
		if err == nil || !strings.Contains(err.Error(), "plan9/386") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestReplaceExecutable(t *testing.T) {
	t.Run("given a binary, it is replaced and keeps its mode", func(t *testing.T) {
		exe := filepath.Join(t.TempDir(), "ytrack")
		if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := upgrade.ReplaceExecutable(exe, []byte("new")); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(exe); string(b) != "new" {
			t.Errorf("content = %q", b)
		}
		if runtime.GOOS != "windows" {
			if fi, _ := os.Stat(exe); fi.Mode().Perm() != 0o755 {
				t.Errorf("mode = %v", fi.Mode().Perm())
			}
		}
		entries, _ := os.ReadDir(filepath.Dir(exe))
		for _, e := range entries {
			if strings.Contains(e.Name(), ".new-") {
				t.Errorf("left a temp file %s", e.Name())
			}
		}
	})
	t.Run("given a missing binary, it fails", func(t *testing.T) {
		if err := upgrade.ReplaceExecutable(filepath.Join(t.TempDir(), "nope"), []byte("new")); err == nil {
			t.Fatal("want an error")
		}
	})
}
