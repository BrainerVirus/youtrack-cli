package upgrade_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/root"

	"github.com/BrainerVirus/youtrack-cli/internal/upgrade"
)

// The v0.2.0 release's real checksums file and the Sigstore bundle the release
// workflow made for it, verified offline against a snapshot of Sigstore's
// public-good trusted root. This pins the production path (real Fulcio chain,
// SCT, Rekor entry and the tag-only workflow identity) that the fake-Sigstore
// tests cannot reach.
func realRelease(t *testing.T) (bundle, checksums []byte, trust upgrade.Trust) {
	t.Helper()
	dir := filepath.Join("testdata", "v0.2.0")
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	material, err := root.NewTrustedRootFromPath(filepath.Join(dir, "trusted_root.json"))
	if err != nil {
		t.Fatal(err)
	}
	return read("ytrack_0.2.0_checksums.txt.sigstore.json"), read("ytrack_0.2.0_checksums.txt"),
		upgrade.Trust{Material: material, Identity: upgrade.ReleaseIdentity}
}

func TestRealReleaseBundle(t *testing.T) {
	t.Run("given the v0.2.0 release bundle, it verifies against the release identity", func(t *testing.T) {
		bundle, checksums, trust := realRelease(t)
		if err := upgrade.VerifyBundle(bundle, checksums, trust); err != nil {
			t.Fatalf("real release bundle did not verify: %v", err)
		}
	})

	t.Run("given a changed checksums file, it refuses", func(t *testing.T) {
		bundle, checksums, trust := realRelease(t)
		tampered := bytes.Replace(checksums, []byte("linux_amd64"), []byte("linux_arm64"), 1)
		if bytes.Equal(tampered, checksums) {
			t.Fatal("fixture has no linux_amd64 line to tamper with")
		}
		if err := upgrade.VerifyBundle(bundle, tampered, trust); err == nil {
			t.Fatal("a tampered checksums file verified")
		}
	})

	t.Run("given another workflow identity, it refuses", func(t *testing.T) {
		bundle, checksums, trust := realRelease(t)
		trust.Identity.SubjectRegexp = `^https://github\.com/BrainerVirus/youtrack-cli/\.github/workflows/release\.yml@refs/heads/main$`
		if err := upgrade.VerifyBundle(bundle, checksums, trust); err == nil {
			t.Fatal("a branch-run identity accepted the tag-signed bundle")
		}
	})
}
