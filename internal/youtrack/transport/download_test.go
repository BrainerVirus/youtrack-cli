package transport

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDownload(t *testing.T) {
	t.Run("given a signed path from the server root, it fetches it on the service host with the token and copies the body", func(t *testing.T) {
		var got *http.Request
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.Clone(context.Background())
			_, _ = w.Write([]byte("file-bytes"))
		}))
		defer srv.Close()
		c := New(Options{BaseURL: srv.URL + "/youtrack", Token: secret})
		var buf bytes.Buffer
		n, err := c.Download(context.Background(), "/youtrack/api/files/8-1?sign=abc&updated=1", &buf)
		if err != nil {
			t.Fatal(err)
		}
		if n != 10 || buf.String() != "file-bytes" {
			t.Errorf("wrote %d bytes: %q", n, buf.String())
		}
		if got.URL.Path != "/youtrack/api/files/8-1" || got.URL.RawQuery != "sign=abc&updated=1" ||
			got.Header.Get("Authorization") != "Bearer "+secret || got.Header.Get("Accept") != "*/*" {
			t.Errorf("request %s?%s %v", got.URL.Path, got.URL.RawQuery, got.Header)
		}
	})

	t.Run("given an absolute URL with another origin, it refuses it without contacting either host", func(t *testing.T) {
		var hits int
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
		defer srv.Close()
		// Same server under another host name, then another scheme, then another port.
		otherHost := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
		otherScheme := strings.Replace(srv.URL, "http://", "https://", 1)
		for _, ref := range []string{otherHost + "/api/files/8-1", otherScheme + "/api/files/8-1", "http://127.0.0.1:1/api/files/8-1"} {
			c := New(Options{BaseURL: srv.URL + "/youtrack", Token: secret})
			_, err := c.Download(context.Background(), ref, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "refusing to download from") || !strings.Contains(err.Error(), "outside "+srv.URL+"/youtrack") {
				t.Errorf("%s: err = %v, want a refused origin", ref, err)
			}
		}
		if hits != 0 {
			t.Errorf("the server was contacted %d times", hits)
		}
	})

	t.Run("given an absolute URL on the service origin, it fetches it", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}))
		defer srv.Close()
		var buf bytes.Buffer
		if _, err := New(Options{BaseURL: srv.URL, Token: secret}).Download(context.Background(), srv.URL+"/api/files/8-1", &buf); err != nil || buf.String() != "ok" {
			t.Errorf("err = %v, body %q", err, buf.String())
		}
	})

	t.Run("given a URL with user info, it refuses it and never prints the password", func(t *testing.T) {
		var hits int
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
		defer srv.Close()
		ref := strings.Replace(srv.URL, "http://", "http://mallory:hunter2@", 1) + "/api/files/8-1"
		_, err := New(Options{BaseURL: srv.URL, Token: secret}).Download(context.Background(), ref, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "the URL holds credentials") || strings.Contains(err.Error(), "hunter2") {
			t.Errorf("err = %v", err)
		}
		if hits != 0 {
			t.Errorf("the server was contacted %d times", hits)
		}
	})

	t.Run("given a 302 to another host, it refuses the redirect and the token never leaves", func(t *testing.T) {
		var gotAuth []string
		other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = append(gotAuth, r.Header.Get("Authorization"))
			_, _ = w.Write([]byte("stolen"))
		}))
		defer other.Close()
		otherURL := strings.Replace(other.URL, "127.0.0.1", "localhost", 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, otherURL+"/steal", http.StatusFound)
		}))
		defer srv.Close()
		var buf bytes.Buffer
		_, err := New(Options{BaseURL: srv.URL, Token: secret}).Download(context.Background(), "/api/files/8-1", &buf)
		if err == nil || !strings.Contains(err.Error(), "refusing redirect") || buf.Len() != 0 {
			t.Errorf("err = %v, wrote %q", err, buf.String())
		}
		if len(gotAuth) != 0 {
			t.Errorf("the other host was contacted: %q", gotAuth)
		}
	})

	t.Run("given an https file that redirects to http, it refuses the downgrade", func(t *testing.T) {
		var plainHits int
		plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { plainHits++ }))
		defer plain.Close()
		tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusFound)
		}))
		defer tlsSrv.Close()
		for _, token := range []string{secret, ""} {
			c := New(Options{BaseURL: tlsSrv.URL, Token: token, RoundTripper: tlsSrv.Client().Transport})
			_, err := c.Download(context.Background(), "/api/files/8-1", &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "refusing redirect from https") {
				t.Errorf("token %q: err = %v, want a refused downgrade", token, err)
			}
		}
		if plainHits != 0 {
			t.Errorf("the plain-http server was contacted %d times", plainHits)
		}
	})

	t.Run("given an error status, it returns the APIError and writes nothing", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"Not Found","error_description":"File with id 8-1 not found"}`))
		}))
		defer srv.Close()
		var buf bytes.Buffer
		_, err := New(Options{BaseURL: srv.URL}).Download(context.Background(), "/api/files/8-1", &buf)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound || buf.Len() != 0 {
			t.Errorf("err = %v, wrote %q", err, buf.String())
		}
	})

	t.Run("given a body slower than the timeout overall but never paused that long, it completes; a stalled one fails", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pause := 100 * time.Millisecond
			if r.URL.Path == "/stall" {
				pause = 2 * time.Second
			}
			for range 5 {
				_, _ = w.Write([]byte("x"))
				w.(http.Flusher).Flush()
				select {
				case <-time.After(pause):
				case <-r.Context().Done():
					return
				}
			}
		}))
		defer srv.Close()
		c := New(Options{BaseURL: srv.URL, Timeout: 400 * time.Millisecond})
		var buf bytes.Buffer
		if n, err := c.Download(context.Background(), "/slow", &buf); err != nil || n != 5 {
			t.Errorf("slow download: %d bytes, %v", n, err)
		}
		if _, err := c.Download(context.Background(), "/stall", &bytes.Buffer{}); !errors.Is(err, ErrStalled) {
			t.Errorf("stalled download: %v", err)
		}
	})

	t.Run("given a non-http URL, it refuses it", func(t *testing.T) {
		_, err := New(Options{BaseURL: "https://youtrack.invalid"}).Download(context.Background(), "file:///etc/passwd", &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "not http(s)") {
			t.Errorf("err = %v", err)
		}
	})
}
