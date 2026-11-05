package upgrade_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BrainerVirus/youtrack-cli/internal/update"
	"github.com/BrainerVirus/youtrack-cli/internal/upgrade"
	"github.com/BrainerVirus/youtrack-cli/internal/upgrade/upgradetest"
)

// downloadFixture serves release 1.1.0 and records the downloader's waits
// and notices, so retries run instantly.
type downloadFixture struct {
	srv     *upgradetest.Server
	rel     *upgradetest.Release
	d       *upgrade.Downloader
	sleeps  []time.Duration
	notices []string
}

const tag = "v1.1.0"

var (
	bundleName    = upgrade.BundleName("1.1.0")
	checksumsName = upgrade.ChecksumsName("1.1.0")
)

func newDownloadFixture(t *testing.T) *downloadFixture {
	t.Helper()
	s := upgradetest.NewSigstore(t)
	fx := &downloadFixture{rel: &upgradetest.Release{Version: "1.1.0"}}
	fx.srv = upgradetest.NewServer(t, s, fx.rel)
	fx.d = &upgrade.Downloader{
		HTTP: fx.srv.Client(), Trust: s.Trust(), GOOS: "linux", GOARCH: "amd64",
		APIURL: fx.srv.URL,
		Sleep: func(_ context.Context, d time.Duration) error {
			fx.sleeps = append(fx.sleeps, d)
			return nil
		},
		Notice: func(msg string) { fx.notices = append(fx.notices, msg) },
	}
	return fx
}

func (fx *downloadFixture) binary(t *testing.T) ([]byte, error) {
	t.Helper()
	rel, err := (&update.Client{HTTP: fx.srv.Client(), APIURL: fx.srv.URL}).Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return fx.d.Binary(context.Background(), rel)
}

// count returns how many requests matched.
func (fx *downloadFixture) count(match func(path string) bool) int {
	n := 0
	for _, p := range fx.srv.Requests() {
		if match(p) {
			n++
		}
	}
	return n
}

func is(path string) func(string) bool { return func(p string) bool { return p == path } }

// failing answers status on path.
func failing(path string, status int) func(http.ResponseWriter, *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != path {
			return false
		}
		w.WriteHeader(status)
		return true
	}
}

func TestDownloadRetryAndFallback(t *testing.T) {
	t.Run("given a download URL answering 504 and a working API URL, it returns the API bytes after bounded retries and says so", func(t *testing.T) {
		fx := newDownloadFixture(t)
		fx.srv.Handle(failing(upgradetest.DownloadPath(tag, bundleName), http.StatusGatewayTimeout))
		bin, err := fx.binary(t)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bin, fx.rel.BinaryFor("linux/amd64")) {
			t.Errorf("binary = %q", bin)
		}
		if n := fx.count(is(upgradetest.DownloadPath(tag, bundleName))); n != 3 {
			t.Errorf("download URL attempts = %d, want 3", n)
		}
		if n := fx.count(upgradetest.IsAssetAPIPath); n != 1 {
			t.Errorf("asset API requests = %d, want 1", n)
		}
		if n := fx.count(is(upgradetest.ObjectPath(tag, bundleName))); n != 1 {
			t.Errorf("redirected object requests = %d, want 1", n)
		}
		if want := []time.Duration{time.Second, 2 * time.Second}; !slices.Equal(fx.sleeps, want) {
			t.Errorf("waits = %v, want %v", fx.sleeps, want)
		}
		if len(fx.notices) != 1 || !strings.Contains(fx.notices[0], "through the GitHub API") ||
			!strings.Contains(fx.notices[0], bundleName) || !strings.Contains(fx.notices[0], "HTTP 504 (3 attempts)") {
			t.Errorf("notices = %q", fx.notices)
		}
	})

	t.Run("given a download URL answering 429 then 504 then 200, it returns the bytes without the fallback", func(t *testing.T) {
		fx := newDownloadFixture(t)
		var calls atomic.Int32
		statuses := []int{http.StatusTooManyRequests, http.StatusGatewayTimeout}
		fx.srv.Handle(func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Path != upgradetest.DownloadPath(tag, bundleName) {
				return false
			}
			if n := int(calls.Add(1)); n <= len(statuses) {
				w.WriteHeader(statuses[n-1])
				return true
			}
			return false
		})
		if _, err := fx.binary(t); err != nil {
			t.Fatal(err)
		}
		if n := fx.count(is(upgradetest.DownloadPath(tag, bundleName))); n != 3 {
			t.Errorf("download URL attempts = %d, want 3", n)
		}
		if n := fx.count(upgradetest.IsAssetAPIPath); n != 0 || len(fx.notices) != 0 {
			t.Errorf("used the fallback: %d API requests, notices %q", n, fx.notices)
		}
		if len(fx.sleeps) != 2 {
			t.Errorf("waits = %v, want 2", fx.sleeps)
		}
	})

	t.Run("given a dropped connection, it retries it", func(t *testing.T) {
		fx := newDownloadFixture(t)
		var dropped atomic.Bool
		fx.srv.Handle(func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Path != upgradetest.DownloadPath(tag, bundleName) || dropped.Swap(true) {
				return false
			}
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return true
			}
			_ = conn.Close()
			return true
		})
		if _, err := fx.binary(t); err != nil {
			t.Fatal(err)
		}
		if len(fx.notices) != 0 {
			t.Errorf("used the fallback: %q", fx.notices)
		}
	})

	t.Run("given both URLs failing, it names both after bounded retries", func(t *testing.T) {
		fx := newDownloadFixture(t)
		download := failing(upgradetest.DownloadPath(tag, bundleName), http.StatusGatewayTimeout)
		object := failing(upgradetest.ObjectPath(tag, bundleName), http.StatusServiceUnavailable)
		fx.srv.Handle(func(w http.ResponseWriter, r *http.Request) bool { return download(w, r) || object(w, r) })
		_, err := fx.binary(t)
		if err == nil {
			t.Fatal("downloaded, want an error")
		}
		msg := err.Error()
		for _, want := range []string{upgradetest.DownloadPath(tag, bundleName) + ": HTTP 504 (3 attempts)", "/releases/assets/", "HTTP 503 (3 attempts)"} {
			if !strings.Contains(msg, want) {
				t.Errorf("err = %q, want it to contain %q", msg, want)
			}
		}
		if errors.Is(err, upgrade.ErrRefused) {
			t.Errorf("err = %v, a download failure is not a refusal", err)
		}
		if a, o := fx.count(is(upgradetest.DownloadPath(tag, bundleName))), fx.count(is(upgradetest.ObjectPath(tag, bundleName))); a != 3 || o != 3 {
			t.Errorf("attempts: download URL %d, API %d; want 3 each", a, o)
		}
		if len(fx.sleeps) != 4 {
			t.Errorf("waits = %v, want 4", fx.sleeps)
		}
	})

	t.Run("given a 404 on both URLs, it tries each once and never retries", func(t *testing.T) {
		fx := newDownloadFixture(t)
		download := failing(upgradetest.DownloadPath(tag, bundleName), http.StatusNotFound)
		fx.srv.Handle(func(w http.ResponseWriter, r *http.Request) bool {
			if upgradetest.IsAssetAPIPath(r.URL.Path) {
				http.NotFound(w, r)
				return true
			}
			return download(w, r)
		})
		_, err := fx.binary(t)
		if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
			t.Fatalf("err = %v", err)
		}
		if a, o := fx.count(is(upgradetest.DownloadPath(tag, bundleName))), fx.count(upgradetest.IsAssetAPIPath); a != 1 || o != 1 {
			t.Errorf("attempts: download URL %d, API %d; want 1 each", a, o)
		}
		if len(fx.sleeps) != 0 {
			t.Errorf("waited %v", fx.sleeps)
		}
	})

	t.Run("given a fallback redirected off https, it refuses without retrying", func(t *testing.T) {
		var plainHits atomic.Int32
		plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			plainHits.Add(1)
			_, _ = w.Write([]byte("served over http"))
		}))
		t.Cleanup(plain.Close)
		fx := newDownloadFixture(t)
		download := failing(upgradetest.DownloadPath(tag, bundleName), http.StatusGatewayTimeout)
		fx.srv.Handle(func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Path == upgradetest.ObjectPath(tag, bundleName) {
				http.Redirect(w, r, plain.URL+"/bundle", http.StatusFound)
				return true
			}
			return download(w, r)
		})
		_, err := fx.binary(t)
		if !errors.Is(err, upgrade.ErrRefused) || !strings.Contains(err.Error(), "redirected off https") {
			t.Fatalf("err = %v, want a refusal", err)
		}
		// The hop is refused before it is followed: the http server is never asked.
		if n := plainHits.Load(); n != 0 {
			t.Errorf("http requests = %d, want 0", n)
		}
	})

	t.Run("given a redirect chain through http back to https, it refuses the http hop", func(t *testing.T) {
		var plainHits atomic.Int32
		fx := newDownloadFixture(t)
		back := fx.srv.URL + upgradetest.ObjectPath(tag, bundleName) + "?via=http"
		plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			plainHits.Add(1)
			http.Redirect(w, r, back, http.StatusFound)
		}))
		t.Cleanup(plain.Close)
		download := failing(upgradetest.DownloadPath(tag, bundleName), http.StatusGatewayTimeout)
		fx.srv.Handle(func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Path == upgradetest.ObjectPath(tag, bundleName) && r.URL.Query().Get("via") == "" {
				http.Redirect(w, r, plain.URL+"/hop", http.StatusFound)
				return true
			}
			return download(w, r)
		})
		_, err := fx.binary(t)
		if !errors.Is(err, upgrade.ErrRefused) || !strings.Contains(err.Error(), "redirected off https") {
			t.Fatalf("err = %v, want a refusal", err)
		}
		if n := plainHits.Load(); n != 0 {
			t.Errorf("http requests = %d, want 0", n)
		}
	})

	t.Run("given an oversized fallback answer, the size limit refuses it", func(t *testing.T) {
		fx := newDownloadFixture(t)
		download := failing(upgradetest.DownloadPath(tag, checksumsName), http.StatusGatewayTimeout)
		fx.srv.Handle(func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Path == upgradetest.ObjectPath(tag, checksumsName) {
				_, _ = w.Write(make([]byte, 1<<20+1))
				return true
			}
			return download(w, r)
		})
		_, err := fx.binary(t)
		if err == nil || !strings.Contains(err.Error(), "larger than") {
			t.Fatalf("err = %v", err)
		}
		if n := fx.count(is(upgradetest.ObjectPath(tag, checksumsName))); n != 1 {
			t.Errorf("oversized answer fetched %d times, want 1", n)
		}
	})

	t.Run("given a canceled context while waiting, it stops", func(t *testing.T) {
		fx := newDownloadFixture(t)
		fx.srv.Handle(failing(upgradetest.DownloadPath(tag, bundleName), http.StatusGatewayTimeout))
		fx.d.Sleep = nil
		rel, err := (&update.Client{HTTP: fx.srv.Client(), APIURL: fx.srv.URL}).Latest(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		fx.d.HTTP = &http.Client{Transport: cancelAfter{fx.srv.Client().Transport, bundleName, cancel}}
		_, err = fx.d.Binary(ctx, rel)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if n := fx.count(upgradetest.IsAssetAPIPath); n != 0 {
			t.Errorf("fell back after cancellation: %d API requests", n)
		}
	})
}

// cancelAfter cancels its context once a request for name has been answered,
// so the downloader's real wait sees the cancellation at once.
type cancelAfter struct {
	rt     http.RoundTripper
	name   string
	cancel context.CancelFunc
}

func (c cancelAfter) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := c.rt.RoundTrip(r)
	if strings.HasSuffix(r.URL.Path, "/"+c.name) {
		c.cancel()
	}
	return resp, err
}
