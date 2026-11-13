package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const secret = "perm-c2VjcmV0.dG9rZW4=.VerySecretValue"

func TestClient(t *testing.T) {
	t.Run("it sends the bearer token, JSON Accept and the user agent", func(t *testing.T) {
		var got http.Header
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.Header.Clone()
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()
		c := New(Options{BaseURL: srv.URL + "/", Token: secret, UserAgent: "ytrack/1.0"})
		if err := c.GetJSON(context.Background(), "/api/x", &map[string]any{}); err != nil {
			t.Fatal(err)
		}
		if got.Get("Authorization") != "Bearer "+secret || got.Get("Accept") != "application/json" || got.Get("User-Agent") != "ytrack/1.0" {
			t.Errorf("headers = %v", got)
		}
	})
}

func TestDebugLog(t *testing.T) {
	t.Run("it logs the request and response with every copy of the token redacted", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Set-Cookie", "session=abc")
			// A server echoing the token in a header must not leak it either.
			w.Header().Set("X-Echo", secret)
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()
		var log bytes.Buffer
		c := New(Options{BaseURL: srv.URL, Token: secret, DebugLog: &log})
		req, _ := http.NewRequest(http.MethodGet, c.URL("/api/users/me?fields=login"), nil)
		req.Header.Set("Cookie", "YTJSESSIONID=xyz")
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()

		out := log.String()
		for _, want := range []string{"> GET " + srv.URL + "/api/users/me?fields=login", "> Authorization: Bearer [REDACTED]", "> Cookie: [REDACTED]", "< HTTP/1.1 200 OK", "< Set-Cookie: [REDACTED]", "< X-Echo: [REDACTED]"} {
			if !strings.Contains(out, want) {
				t.Errorf("log lacks %q:\n%s", want, out)
			}
		}
		for _, leak := range []string{secret, "VerySecretValue", "xyz", "session=abc"} {
			if strings.Contains(out, leak) {
				t.Errorf("log contains %q:\n%s", leak, out)
			}
		}
	})
}

func TestAPIError(t *testing.T) {
	tests := []struct {
		name, body string
		status     int
		want       string
	}{
		{"YouTrack error and description", `{"error":"Not Found","error_description":"Entity with id X-1 not found"}`, 404, "HTTP 404: Entity with id X-1 not found (Not Found)"},
		{"only an error code", `{"error":"invalid_query"}`, 400, "HTTP 400: invalid_query"},
		{"a non-JSON body", `<html>gateway</html>`, 502, "HTTP 502: Bad Gateway"},
	}
	for _, tt := range tests {
		t.Run("given "+tt.name+", the message is "+tt.want, func(t *testing.T) {
			e := ParseError(tt.status, http.StatusText(tt.status), strings.NewReader(tt.body))
			if e.Error() != tt.want || e.HTTPStatus() != tt.status {
				t.Errorf("got %q (%d)", e.Error(), e.HTTPStatus())
			}
		})
	}
}

func TestRedirects(t *testing.T) {
	t.Run("given a redirect to another host, the token never reaches it", func(t *testing.T) {
		var gotAuth []string
		other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = append(gotAuth, r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{}`))
		}))
		defer other.Close()
		// Same port space, different host name: 127.0.0.1 redirects to localhost.
		otherURL := strings.Replace(other.URL, "127.0.0.1", "localhost", 1)
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, otherURL+"/steal", http.StatusFound)
		}))
		defer origin.Close()

		c := New(Options{BaseURL: origin.URL, Token: secret})
		err := c.GetJSON(context.Background(), "/api/users/me", &map[string]any{})

		for _, a := range gotAuth {
			if a != "" {
				t.Fatalf("the other host received Authorization %q", a)
			}
		}
		if err == nil || !strings.Contains(err.Error(), "refusing redirect") {
			t.Errorf("err = %v, want a refused redirect", err)
		}
		if strings.Contains(fmt.Sprint(err), secret) {
			t.Error("the error message contains the token")
		}
	})

	t.Run("given a redirect within the service URL, it follows it with the token", func(t *testing.T) {
		var final string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/yt/api/old" {
				http.Redirect(w, r, "/yt/api/new", http.StatusMovedPermanently)
				return
			}
			final = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()
		c := New(Options{BaseURL: srv.URL + "/yt", Token: secret})
		if err := c.GetJSON(context.Background(), "/api/old", &map[string]any{}); err != nil {
			t.Fatal(err)
		}
		if final != "Bearer "+secret {
			t.Errorf("Authorization after redirect = %q", final)
		}
	})

	t.Run("given a request outside the service path prefix, it is sent without the token", func(t *testing.T) {
		var got string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.Header.Get("Authorization")
		}))
		defer srv.Close()
		c := New(Options{BaseURL: srv.URL + "/youtrack", Token: secret})
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/youtrack-other/api/x", nil)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if got != "" {
			t.Errorf("Authorization = %q, want none", got)
		}
	})

	t.Run("given an https to http redirect, it is refused even without a token", func(t *testing.T) {
		var plainHits int
		plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { plainHits++ }))
		defer plain.Close()
		tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusFound)
		}))
		defer tlsSrv.Close()

		for _, token := range []string{secret, ""} {
			c := New(Options{BaseURL: tlsSrv.URL, Token: token, RoundTripper: tlsSrv.Client().Transport})
			err := c.GetJSON(context.Background(), "/api/users/me", &map[string]any{})
			if err == nil || !strings.Contains(err.Error(), "refusing redirect from https") {
				t.Errorf("token %q: err = %v, want a refused downgrade", token, err)
			}
		}
		if plainHits != 0 {
			t.Errorf("the plain-http server was contacted %d times", plainHits)
		}
	})
}

// tracker counts the Open and Close calls of in-memory FileParts, so a test
// can see every opened file closed.
type tracker struct {
	mu            sync.Mutex
	opens, closes int
}

type trackedReader struct {
	io.Reader
	t *tracker
}

func (r trackedReader) Close() error {
	r.t.mu.Lock()
	defer r.t.mu.Unlock()
	r.t.closes++
	return nil
}

func (t *tracker) part(name, ct string, content []byte) FilePart {
	return FilePart{Name: name, ContentType: ct, Size: int64(len(content)), Open: func() (io.ReadCloser, error) {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.opens++
		return trackedReader{bytes.NewReader(content), t}, nil
	}}
}

// waitClosed waits for the body goroutine to close every opened file.
func (t *tracker) waitClosed(tb testing.TB) {
	tb.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		t.mu.Lock()
		opens, closes := t.opens, t.closes
		t.mu.Unlock()
		if opens == closes {
			return
		}
		if time.Now().After(deadline) {
			tb.Fatalf("opened %d files, closed %d", opens, closes)
		}
		time.Sleep(time.Millisecond)
	}
}

type receivedPart struct {
	field, name, contentType string
	data                     []byte
}

// readUpload reads a multipart request, failing unless its Content-Length
// was sent and matches the body.
func readUpload(t *testing.T, r *http.Request) []receivedPart {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Errorf("reading the body: %v", err)
		return nil
	}
	if len(r.TransferEncoding) != 0 || r.ContentLength != int64(len(raw)) {
		t.Errorf("Transfer-Encoding %v, Content-Length %d, body %d bytes", r.TransferEncoding, r.ContentLength, len(raw))
	}
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		t.Errorf("Content-Type %q: %v", r.Header.Get("Content-Type"), err)
		return nil
	}
	mr := multipart.NewReader(bytes.NewReader(raw), params["boundary"])
	var parts []receivedPart
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			return parts
		}
		if err != nil {
			t.Errorf("next part: %v", err)
			return parts
		}
		data, _ := io.ReadAll(p)
		parts = append(parts, receivedPart{p.FormName(), p.FileName(), p.Header.Get("Content-Type"), data})
	}
}

func TestSendMultipart(t *testing.T) {
	t.Run("given two files, it sends both parts with an exact Content-Length and closes them", func(t *testing.T) {
		var got []receivedPart
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = readUpload(t, r)
			_, _ = w.Write([]byte(`[]`))
		}))
		defer srv.Close()
		var tr tracker
		c := New(Options{BaseURL: srv.URL, Token: secret})
		err := c.SendMultipart(context.Background(), "/api/up", []FilePart{
			tr.part("a.sql", "application/sql", []byte("select 1;")),
			tr.part("b.bin", "", []byte{0, 1, 2}),
		}, &[]any{})
		if err != nil {
			t.Fatal(err)
		}
		want := []receivedPart{{"files[0]", "a.sql", "application/sql", []byte("select 1;")}, {"files[1]", "b.bin", "application/octet-stream", []byte{0, 1, 2}}}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("parts = %v, want %v", got, want)
		}
		tr.waitClosed(t)
	})

	t.Run("given a file that cannot be opened, it sends nothing and closes the ones it opened", func(t *testing.T) {
		hits := 0
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
		defer srv.Close()
		var tr tracker
		bad := FilePart{Name: "gone.log", Size: 1, Open: func() (io.ReadCloser, error) { return nil, errors.New("cannot attach gone.log: no such file") }}
		err := New(Options{BaseURL: srv.URL}).SendMultipart(context.Background(), "/api/up",
			[]FilePart{tr.part("a", "", []byte("a")), bad, tr.part("c", "", []byte("c"))}, nil)
		if err == nil || !strings.Contains(err.Error(), "gone.log") || hits != 0 {
			t.Errorf("err = %v, hits = %d", err, hits)
		}
		tr.waitClosed(t)
	})

	t.Run("given a 307 within the service, it resends the whole upload", func(t *testing.T) {
		var got [][]receivedPart
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			parts := readUpload(t, r)
			if r.URL.Path == "/yt/api/old" {
				http.Redirect(w, r, "/yt/api/new", http.StatusTemporaryRedirect)
				return
			}
			got = append(got, parts)
			_, _ = w.Write([]byte(`[]`))
		}))
		defer srv.Close()
		var tr tracker
		content := bytes.Repeat([]byte("x"), 100_000)
		err := New(Options{BaseURL: srv.URL + "/yt", Token: secret}).SendMultipart(context.Background(), "/api/old",
			[]FilePart{tr.part("big.txt", "text/plain", content)}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || len(got[0]) != 1 || !bytes.Equal(got[0][0].data, content) {
			t.Errorf("the redirected request received %d uploads", len(got))
		}
		tr.waitClosed(t)
		if tr.opens != 2 {
			t.Errorf("opened the file %d times, want 2", tr.opens)
		}
	})

	t.Run("given a 307 outside the service, it is refused", func(t *testing.T) {
		otherHits := 0
		other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { otherHits++ }))
		defer other.Close()
		otherURL := strings.Replace(other.URL, "127.0.0.1", "localhost", 1)
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			http.Redirect(w, r, otherURL+"/steal", http.StatusTemporaryRedirect)
		}))
		defer origin.Close()
		var tr tracker
		err := New(Options{BaseURL: origin.URL, Token: secret}).SendMultipart(context.Background(), "/api/up",
			[]FilePart{tr.part("a", "", []byte("a"))}, nil)
		if err == nil || !strings.Contains(err.Error(), "refusing redirect") || otherHits != 0 {
			t.Errorf("err = %v, other host hits = %d", err, otherHits)
		}
		tr.waitClosed(t)
	})

	t.Run("given a file larger than 64 MiB, it streams it without holding it in memory", func(t *testing.T) {
		const size = 64<<20 + 12345
		p := filepath.Join(t.TempDir(), "sparse.bin")
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(size); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		var received int64
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mr, err := r.MultipartReader()
			if err != nil {
				t.Error(err)
				return
			}
			part, err := mr.NextPart()
			if err != nil {
				t.Error(err)
				return
			}
			received, _ = io.Copy(io.Discard, part)
		}))
		defer srv.Close()
		open := func() (io.ReadCloser, error) { return os.Open(p) }

		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		err = New(Options{BaseURL: srv.URL}).SendMultipart(context.Background(), "/api/up",
			[]FilePart{{Name: "sparse.bin", Size: size, Open: open}}, nil)
		runtime.ReadMemStats(&after)
		if err != nil {
			t.Fatal(err)
		}
		if received != size {
			t.Fatalf("the server received %d bytes, want %d", received, size)
		}
		// TotalAlloc counts every byte allocated during the upload, client and
		// server, so it bounds the peak heap; buffering the file would exceed it.
		if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 16<<20 {
			t.Errorf("allocated %d MiB uploading a %d MiB file", allocated>>20, size>>20)
		}
	})

	t.Run("given a file truncated during the upload, it fails naming the file", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "growing.log")
		if err := os.WriteFile(p, bytes.Repeat([]byte("l"), 1<<20), 0o600); err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
		}))
		defer srv.Close()
		// The test hook: the file is cut short once its first bytes are read.
		open := func() (io.ReadCloser, error) {
			f, err := os.Open(p)
			if err != nil {
				return nil, err
			}
			return &truncateAfterRead{File: f, path: p}, nil
		}
		err := New(Options{BaseURL: srv.URL}).SendMultipart(context.Background(), "/api/up",
			[]FilePart{{Name: "growing.log", Size: 1 << 20, Open: open}}, nil)
		var sc *SizeChangedError
		if !errors.As(err, &sc) || !strings.Contains(err.Error(), "growing.log changed during the upload: it was 1048576 bytes") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("given a file that grew during the upload, it fails naming the file", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
		}))
		defer srv.Close()
		var tr tracker
		part := tr.part("grew.log", "", []byte("12345"))
		part.Size = 3
		err := New(Options{BaseURL: srv.URL}).SendMultipart(context.Background(), "/api/up", []FilePart{part}, nil)
		if err == nil || err.Error() != "grew.log grew during the upload: it was 3 bytes" {
			t.Errorf("err = %v", err)
		}
		tr.waitClosed(t)
	})
}

type truncateAfterRead struct {
	*os.File
	path string
	done bool
}

func (f *truncateAfterRead) Read(p []byte) (int, error) {
	n, err := f.File.Read(p[:min(len(p), 4096)])
	if !f.done {
		f.done = true
		if terr := os.Truncate(f.path, 8192); terr != nil {
			return n, terr
		}
	}
	return n, err
}
