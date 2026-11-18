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

	t.Run("given an absolute URL on another host, it sends no token", func(t *testing.T) {
		var auth string
		other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth = r.Header.Get("Authorization")
		}))
		defer other.Close()
		c := New(Options{BaseURL: "http://youtrack.invalid", Token: secret})
		if _, err := c.Download(context.Background(), other.URL+"/api/files/8-1", &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		if auth != "" {
			t.Errorf("the token went to another host: %q", auth)
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
