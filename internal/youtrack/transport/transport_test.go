package transport

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
