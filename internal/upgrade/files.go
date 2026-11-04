package upgrade

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// maxBinary caps the size of the binary extracted from an archive.
const maxBinary = 200 << 20

// ArchiveName returns the release archive for version (no "v") on goos/goarch,
// as .goreleaser.yaml names it.
func ArchiveName(version, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("ytrack_%s_%s_%s%s", version, goos, goarch, ext)
}

// ChecksumsName returns the checksums file of a release.
func ChecksumsName(version string) string {
	return fmt.Sprintf("ytrack_%s_checksums.txt", version)
}

// BundleName returns the Sigstore bundle that signs the checksums file.
func BundleName(version string) string { return ChecksumsName(version) + ".sigstore.json" }

// ExpectedSHA256 returns the hex digest listed for name in a checksums file
// ("<hex>  <name>" lines, as sha256sum writes them).
func ExpectedSHA256(checksums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(checksums))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			if _, err := hex.DecodeString(fields[0]); err != nil || len(fields[0]) != 64 {
				return "", fmt.Errorf("checksums file has an invalid digest for %s", name)
			}
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("checksums file does not list %s", name)
}

// CheckSHA256 reports an error unless data hashes to the hex digest want.
func CheckSHA256(data []byte, want string) error {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(got), []byte(strings.ToLower(want))) != 1 {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, want)
	}
	return nil
}

// ExtractBinary returns the ytrack executable from a release archive
// (tar.gz, or zip for Windows).
func ExtractBinary(archive []byte, name string) ([]byte, error) {
	bin := "ytrack"
	if strings.HasSuffix(name, ".zip") {
		bin = "ytrack.exe"
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		for _, f := range zr.File {
			if path.Base(f.Name) != bin || f.FileInfo().IsDir() {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer func() { _ = rc.Close() }()
			return readLimited(rc)
		}
		return nil, fmt.Errorf("%s does not contain %s", name, bin)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s does not contain %s", name, bin)
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		if hdr.Typeflag == tar.TypeReg && path.Base(hdr.Name) == bin {
			return readLimited(tr)
		}
	}
}

func readLimited(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxBinary+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxBinary {
		return nil, errors.New("binary in the archive is too large")
	}
	return b, nil
}

// OldSuffix names the previous binary that a Windows upgrade moves aside.
const OldSuffix = ".old"

// ReplaceExecutable atomically replaces the file at exe with data, keeping
// its permissions. The new file is written next to exe and renamed over it,
// so exe is either the old or the new binary, never a partial one. Windows
// cannot overwrite a running executable but can rename it, so there the old
// binary is first moved to exe+OldSuffix (removed by the next upgrade).
func ReplaceExecutable(exe string, data []byte) error {
	info, err := os.Stat(exe)
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(exe)+".new-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, info.Mode().Perm()|0o100); err != nil && runtime.GOOS != "windows" {
		return err
	}
	if runtime.GOOS != "windows" {
		return os.Rename(tmpName, exe)
	}
	old := exe + OldSuffix
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("moving the running binary aside: %w", err)
	}
	if err := os.Rename(tmpName, exe); err != nil {
		if rerr := os.Rename(old, exe); rerr != nil {
			return fmt.Errorf("installing the new binary: %w (the previous one is at %s)", err, old)
		}
		return fmt.Errorf("installing the new binary: %w", err)
	}
	return nil
}

// RemoveOld deletes the binary a previous Windows upgrade moved aside.
func RemoveOld(exe string) { _ = os.Remove(exe + OldSuffix) }
