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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/BrainerVirus/youtrack-cli/internal/update"
)

const (
	maxSmallFile = 1 << 20
	maxArchive   = 300 << 20
)

// ErrRefused wraps every verification failure.
var ErrRefused = errors.New("refusing to upgrade")

// retryDelays are the waits between attempts at one URL: 3 attempts, about
// 3 s of waiting. Only transport errors, HTTP 5xx and HTTP 429 are retried.
var retryDelays = []time.Duration{time.Second, 2 * time.Second}

// Downloader fetches and verifies release binaries.
//
// Each file is fetched from its release download URL; a transient failure
// (transport error, HTTP 5xx or 429) is retried with backoff. If that URL
// still fails, or answers another HTTP error, the file is fetched once more
// through the GitHub REST API (the asset's API URL with Accept:
// application/octet-stream), with the same retries. GitHub's download CDN can
// serve an edge-cached error for one asset while the API serves it fine. Both
// paths keep the same guards (https only, no redirect off https, size limit,
// User-Agent), and the signature and checksum checks run on the bytes either
// way.
type Downloader struct {
	HTTP      *http.Client
	Trust     Trust
	GOOS      string
	GOARCH    string
	UserAgent string
	// APIURL is the GitHub REST API root used for the fallback;
	// update.DefaultAPIURL when empty.
	APIURL string
	// Sleep waits between retries; nil waits on a timer, honouring ctx.
	Sleep func(ctx context.Context, d time.Duration) error
	// Notice, when set, is told each time a file came through the API
	// fallback.
	Notice func(msg string)

	apiAssets map[string]string // asset name → API URL, loaded on first fallback
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

	sums, err := d.fetch(ctx, rel.Tag, checksums, maxSmallFile)
	if err != nil {
		return nil, err
	}
	sig, err := d.fetch(ctx, rel.Tag, bundle, maxSmallFile)
	if err != nil {
		return nil, err
	}
	if err := VerifyBundle(sig, sums, d.Trust); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrRefused, checksums.Name, err)
	}
	want, err := ExpectedSHA256(sums, archiveName)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRefused, err)
	}
	data, err := d.fetch(ctx, rel.Tag, archive, maxArchive)
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

// httpFailure is a download that failed at the transport or HTTP level, as
// opposed to a refusal or an oversized file.
type httpFailure struct {
	msg   string
	retry bool // transport error, HTTP 5xx or 429
}

func (e *httpFailure) Error() string { return e.msg }

// fetch returns asset a of release tag, falling back to the GitHub API when
// the download URL fails (see Downloader).
func (d *Downloader) fetch(ctx context.Context, tag string, a *update.Asset, limit int64) ([]byte, error) {
	b, err := d.getRetrying(ctx, a.Name, a.URL, "", limit)
	var hf *httpFailure
	if err == nil || !errors.As(err, &hf) || ctx.Err() != nil {
		return b, err
	}
	apiURL, lerr := d.assetAPIURL(ctx, tag, a.Name)
	if lerr != nil {
		return nil, fmt.Errorf("%w; GitHub API fallback: %w", err, lerr)
	}
	b, ferr := d.getRetrying(ctx, a.Name, apiURL, "application/octet-stream", limit)
	if ferr != nil {
		return nil, fmt.Errorf("%w; GitHub API fallback: %w", err, ferr)
	}
	if d.Notice != nil {
		d.Notice(fmt.Sprintf("%v; downloaded %s through the GitHub API instead", err, a.Name))
	}
	return b, nil
}

// assetAPIURL returns the REST API URL of asset name in release tag, read
// from the release JSON (its "url" field).
func (d *Downloader) assetAPIURL(ctx context.Context, tag, name string) (string, error) {
	if d.apiAssets == nil {
		base := d.APIURL
		if base == "" {
			base = update.DefaultAPIURL
		}
		u := strings.TrimSuffix(base, "/") + "/repos/" + update.Repo + "/releases/tags/" + url.PathEscape(tag)
		body, err := d.getRetrying(ctx, "release "+tag, u, "application/vnd.github+json", maxSmallFile)
		if err != nil {
			return "", err
		}
		var rel struct {
			Assets []struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			} `json:"assets"`
		}
		if err := json.Unmarshal(body, &rel); err != nil {
			return "", fmt.Errorf("reading release %s: %w", tag, err)
		}
		d.apiAssets = map[string]string{}
		for _, a := range rel.Assets {
			d.apiAssets[a.Name] = a.URL
		}
	}
	u := d.apiAssets[name]
	if u == "" {
		return "", fmt.Errorf("release %s lists no API URL for %s", tag, name)
	}
	return u, nil
}

// getRetrying gets rawURL, retrying transient failures after retryDelays.
func (d *Downloader) getRetrying(ctx context.Context, name, rawURL, accept string, limit int64) ([]byte, error) {
	for attempt := 1; ; attempt++ {
		b, err := d.get(ctx, name, rawURL, accept, limit)
		var hf *httpFailure
		if err == nil || !errors.As(err, &hf) || !hf.retry {
			return b, err
		}
		if attempt > len(retryDelays) {
			return nil, &httpFailure{msg: fmt.Sprintf("%s (%d attempts)", hf.msg, attempt)}
		}
		if serr := d.sleep(ctx, retryDelays[attempt-1]); serr != nil {
			return nil, fmt.Errorf("%w (retry canceled: %w)", err, serr)
		}
	}
}

func (d *Downloader) sleep(ctx context.Context, dur time.Duration) error {
	if d.Sleep != nil {
		return d.Sleep(ctx, dur)
	}
	t := time.NewTimer(dur)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// get makes one request for rawURL. Failures that a retry or the fallback
// may cure are *httpFailure; refusals and oversized files are not.
func (d *Downloader) get(ctx context.Context, name, rawURL, accept string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	if req.URL.Scheme != "https" {
		return nil, fmt.Errorf("%w: %s is not served over https", ErrRefused, name)
	}
	if d.UserAgent != "" {
		req.Header.Set("User-Agent", d.UserAgent)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	hc := d.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("downloading %s: %w", name, err)
		}
		return nil, &httpFailure{msg: fmt.Sprintf("downloading %s: %v", name, err), retry: true}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.Request != nil && resp.Request.URL.Scheme != "https" {
		return nil, fmt.Errorf("%w: %s was redirected off https", ErrRefused, name)
	}
	if resp.StatusCode != http.StatusOK {
		retry := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
		return nil, &httpFailure{msg: fmt.Sprintf("downloading %s from %s: HTTP %d", name, rawURL, resp.StatusCode), retry: retry}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("downloading %s: %w", name, err)
		}
		return nil, &httpFailure{msg: fmt.Sprintf("downloading %s: %v", name, err), retry: true}
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("downloading %s: larger than %d bytes", name, limit)
	}
	return b, nil
}
