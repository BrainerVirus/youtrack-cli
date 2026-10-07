package cmdtest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Request is a request the fake server received.
type Request struct {
	Method   string
	Path     string
	RawQuery string
	Header   http.Header
	Body     string
}

// FakeYouTrack is a minimal YouTrack REST API:
//
//	GET  /api/users/me          the token's user
//	GET  /api/issues            Issues, paged with $skip/$top (default $top 42)
//	POST /api/issues            echoes the JSON body back
//	GET  /api/issues/NOPE-1     404 with a YouTrack error body
//	GET  /api/plain             a text/plain body
//	GET  /users/me              the web token page (no auth; 404 if HubTokenPage)
//
// Every /api route requires "Authorization: Bearer <Token>".
type FakeYouTrack struct {
	Server   *httptest.Server
	Prefix   string
	Token    string
	Login    string
	FullName string
	Issues   []map[string]any
	// MaxTop caps $top like a server limit would (0 means no cap).
	MaxTop int
	// HubTokenPage makes the instance's token page 404, as on a Server
	// install with an external Hub.
	HubTokenPage bool

	mu       sync.Mutex
	requests []Request
}

// NewFakeYouTrack starts a fake server mounted at prefix ("" or e.g. "/youtrack").
func NewFakeYouTrack(t *testing.T, prefix string) *FakeYouTrack {
	t.Helper()
	yt := &FakeYouTrack{Prefix: prefix, Token: "perm-am9obg==.dGVzdA==.s3cr3tT0k3nValue", Login: "jdoe", FullName: "John Doe"}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users/me", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]any{"login": yt.Login, "fullName": yt.FullName, "$type": "Me"})
	})
	mux.HandleFunc("GET /api/issues", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		skip, _ := strconv.Atoi(q.Get("$skip"))
		top := 42
		if v := q.Get("$top"); v != "" {
			top, _ = strconv.Atoi(v)
		}
		if yt.MaxTop > 0 {
			top = min(top, yt.MaxTop)
		}
		items := []map[string]any{}
		for i := skip; i < len(yt.Issues) && i < skip+top; i++ {
			items = append(items, yt.Issues[i])
		}
		writeJSON(w, 200, items)
	})
	mux.HandleFunc("POST /api/issues", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.Copy(w, r.Body)
	})
	mux.HandleFunc("GET /api/issues/NOPE-1", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 404, map[string]any{"error": "Not Found", "error_description": "Entity with id NOPE-1 not found"})
	})
	mux.HandleFunc("GET /api/plain", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "plain text")
	})

	authed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/me" { // the web UI token page, no API auth
			if yt.HubTokenPage {
				http.NotFound(w, r)
				return
			}
			_, _ = io.WriteString(w, "<html>Account security</html>")
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+yt.Token {
			writeJSON(w, 401, map[string]any{"error": "Unauthorized", "error_description": "You are not logged in."})
			return
		}
		mux.ServeHTTP(w, r)
	})
	inner := http.NewServeMux()
	if prefix != "" {
		inner.Handle(prefix+"/", http.StripPrefix(prefix, authed))
	} else {
		inner.Handle("/", authed)
	}
	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		yt.mu.Lock()
		yt.requests = append(yt.requests, Request{Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery, Header: r.Header.Clone(), Body: string(body)})
		yt.mu.Unlock()
		inner.ServeHTTP(w, r)
	})
	yt.Server = httptest.NewServer(root)
	t.Cleanup(yt.Server.Close)
	return yt
}

// URL is the service URL, including the prefix.
func (yt *FakeYouTrack) URL() string { return yt.Server.URL + yt.Prefix }

// Key is the host key ytrack derives from URL.
func (yt *FakeYouTrack) Key() string { return strings.TrimPrefix(yt.URL(), "http://") }

// Requests returns the requests received so far.
func (yt *FakeYouTrack) Requests() []Request {
	yt.mu.Lock()
	defer yt.mu.Unlock()
	return append([]Request(nil), yt.requests...)
}

// LastRequest returns the most recent request.
func (yt *FakeYouTrack) LastRequest(t *testing.T) Request {
	t.Helper()
	reqs := yt.Requests()
	if len(reqs) == 0 {
		t.Fatal("the fake YouTrack received no requests")
	}
	return reqs[len(reqs)-1]
}

// SeedIssues fills Issues with n issues APP-1..APP-n.
func (yt *FakeYouTrack) SeedIssues(n int) {
	yt.Issues = nil
	for i := 1; i <= n; i++ {
		yt.Issues = append(yt.Issues, map[string]any{"idReadable": fmt.Sprintf("APP-%d", i), "summary": fmt.Sprintf("Issue %d", i)})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
