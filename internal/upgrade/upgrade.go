// Package upgrade downloads a ytrack release, verifies it and replaces the
// running binary with it.
//
// Trust chain: the release's checksums file must carry a Sigstore keyless
// signature by this repository's release workflow (ReleaseIdentity); the
// archive must match its SHA-256 in that file. Only then is the binary
// extracted and swapped in. Any failed check refuses the upgrade and leaves
// the installed binary untouched.
package upgrade

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/BrainerVirus/youtrack-cli/internal/update"
)

const (
	maxSmallFile = 1 << 20
	maxArchive   = 300 << 20
)

// ErrRefused wraps every verification failure.
var ErrRefused = errors.New("refusing to upgrade")

// Downloader fetches and verifies release binaries.
type Downloader struct {
	HTTP      *http.Client
	Root      *TrustedRoot
	Identity  Identity
	GOOS      string
	GOARCH    string
	UserAgent string
}

// Binary downloads the release archive for d.GOOS/d.GOARCH and returns the
// ytrack executable inside it, after checking the signature of the checksums
// file and the archive's checksum.
func (d *Downloader) Binary(ctx context.Context, rel *update.Release) ([]byte, error) {
	ver := rel.Version()
	archiveName := ArchiveName(ver, d.GOOS, d.GOARCH)
	archive, checksums, bundle := rel.Asset(archiveName), rel.Asset(ChecksumsName(ver)), rel.Asset(BundleName(ver))
	switch {
	case archive == nil:
		return nil, fmt.Errorf("release %s has no build for %s/%s (%s)", rel.Tag, d.GOOS, d.GOARCH, archiveName)
	case checksums == nil:
		return nil, fmt.Errorf("%w: release %s has no checksums file", ErrRefused, rel.Tag)
	case bundle == nil:
		return nil, fmt.Errorf("%w: release %s has no signature (%s)", ErrRefused, rel.Tag, BundleName(ver))
	}

	sums, err := d.fetch(ctx, checksums, maxSmallFile)
	if err != nil {
		return nil, err
	}
	sig, err := d.fetch(ctx, bundle, maxSmallFile)
	if err != nil {
		return nil, err
	}
	if err := VerifyBundle(sig, sums, d.Root, d.Identity); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrRefused, checksums.Name, err)
	}
	want, err := ExpectedSHA256(sums, archiveName)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRefused, err)
	}
	data, err := d.fetch(ctx, archive, maxArchive)
	if err != nil {
		return nil, err
	}
	if err := CheckSHA256(data, want); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrRefused, archiveName, err)
	}
	bin, err := ExtractBinary(data, archiveName)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRefused, err)
	}
	return bin, nil
}

func (d *Downloader) fetch(ctx context.Context, a *update.Asset, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, http.NoBody)
	if err != nil {
		return nil, err
	}
	if req.URL.Scheme != "https" {
		return nil, fmt.Errorf("%w: %s is not served over https", ErrRefused, a.Name)
	}
	if d.UserAgent != "" {
		req.Header.Set("User-Agent", d.UserAgent)
	}
	hc := d.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", a.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.Request != nil && resp.Request.URL.Scheme != "https" {
		return nil, fmt.Errorf("%w: %s was redirected off https", ErrRefused, a.Name)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: HTTP %d", a.Name, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", a.Name, err)
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("downloading %s: larger than %d bytes", a.Name, limit)
	}
	return b, nil
}
