// Package transport is ytrack's HTTP layer for the YouTrack REST API: it
// authenticates requests, identifies the client, logs redacted debug traces
// and turns error responses into *APIError.
package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const redacted = "[REDACTED]"

// Options configure a Client.
type Options struct {
	// BaseURL is the service URL, e.g. https://tools.acme.com/youtrack.
	BaseURL string
	Token   string
	// UserAgent defaults to "ytrack".
	UserAgent string
	// DebugLog, when non-nil, receives a trace of every request and response
	// with credentials redacted.
	DebugLog io.Writer
	// Timeout bounds each request. Zero means 60s.
	Timeout time.Duration
	// RoundTripper overrides the underlying transport (tests).
	RoundTripper http.RoundTripper
}

// Client sends authenticated requests to one YouTrack host.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a Client for opts.BaseURL.
func New(opts Options) *Client {
	rt := opts.RoundTripper
	if rt == nil {
		rt = http.DefaultTransport
	}
	ua := opts.UserAgent
	if ua == "" {
		ua = "ytrack"
	}
	baseURL := strings.TrimRight(opts.BaseURL, "/")
	base, _ := url.Parse(baseURL)
	auth := &authTransport{next: rt, token: opts.Token, userAgent: ua, base: base}
	rt = auth
	if opts.DebugLog != nil {
		rt = &debugTransport{next: rt, w: opts.DebugLog, secret: opts.Token}
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return &Client{
		baseURL: baseURL,
		http: &http.Client{
			Transport:     rt,
			Timeout:       timeout,
			CheckRedirect: auth.checkRedirect,
		},
	}
}

// URL joins the service URL with a path that already starts with /api.
func (c *Client) URL(path string) string { return c.baseURL + path }

// Do sends req as is. Callers inspect the status code themselves; use
// CheckResponse to convert error statuses.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	return c.http.Do(req)
}

// GetJSON sends GET path (relative to the service URL, starting with /api)
// and decodes a successful JSON response into v.
func (c *Client) GetJSON(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL(path), nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := CheckResponse(resp); err != nil {
		return err
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("decoding %s response: %w", path, err)
	}
	return nil
}

// SendJSON sends method path with body encoded as JSON and decodes a
// successful JSON response into v (when v is non-nil).
func (c *Client) SendJSON(ctx context.Context, method, path string, body, v any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encoding %s request: %w", path, err)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.URL(path), bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := CheckResponse(resp); err != nil {
		return err
	}
	if v == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("decoding %s response: %w", path, err)
	}
	return nil
}

// authTransport adds the token only to requests inside the service URL: same
// scheme, same host and port, and under its path prefix. Anything else, such
// as a redirect target elsewhere, goes out without credentials.
type authTransport struct {
	next      http.RoundTripper
	token     string
	userAgent string
	base      *url.URL
}

func (t *authTransport) inScope(u *url.URL) bool {
	if t.base == nil {
		return false
	}
	if !strings.EqualFold(u.Scheme, t.base.Scheme) || !strings.EqualFold(u.Host, t.base.Host) {
		return false
	}
	prefix := strings.TrimRight(t.base.Path, "/")
	return prefix == "" || u.Path == prefix || strings.HasPrefix(u.Path, prefix+"/")
}

// sendsToken reports whether a request to u would carry the token.
func (t *authTransport) sendsToken(u *url.URL) bool {
	return t.token != "" && t.inScope(u)
}

// checkRedirect refuses https-to-http downgrades and, when the client holds a
// token, redirects that leave the service URL.
func (t *authTransport) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	prev := via[len(via)-1].URL
	if strings.EqualFold(prev.Scheme, "https") && !strings.EqualFold(req.URL.Scheme, "https") {
		return fmt.Errorf("refusing redirect from https to %s", redactURL(req.URL))
	}
	if t.token != "" && !t.inScope(req.URL) {
		return fmt.Errorf("refusing redirect to %s, outside %s", redactURL(req.URL), t.base.Redacted())
	}
	return nil
}

func redactURL(u *url.URL) string {
	c := *u
	c.RawQuery = ""
	return c.Redacted()
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Del("Authorization")
	if t.sendsToken(req.URL) {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("User-Agent", t.userAgent)
	return t.next.RoundTrip(req)
}

// debugTransport logs requests and responses. It never logs bodies, and it
// scrubs the token from everything it writes, not only the Authorization
// header, so a token that leaks into a URL or header is still hidden.
type debugTransport struct {
	next   http.RoundTripper
	w      io.Writer
	secret string
}

var sensitiveHeaders = map[string]bool{
	"Authorization":       true,
	"Cookie":              true,
	"Set-Cookie":          true,
	"Proxy-Authorization": true,
}

func (t *debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "* Request at %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(&b, "> %s %s\n", req.Method, req.URL.String())
	// The Authorization header is added by the inner transport, so show it here
	// as it will be sent: redacted.
	headers := req.Header.Clone()
	if inner, ok := t.next.(*authTransport); ok && inner.sendsToken(req.URL) {
		headers.Set("Authorization", "Bearer "+inner.token)
	}
	writeHeaders(&b, ">", headers)
	t.write(b.String())

	start := time.Now()
	resp, err := t.next.RoundTrip(req)
	b.Reset()
	if err != nil {
		fmt.Fprintf(&b, "* Request failed after %s: %v\n", time.Since(start).Round(time.Millisecond), err)
		t.write(b.String())
		return resp, err
	}
	fmt.Fprintf(&b, "< %s %s (%s)\n", resp.Proto, resp.Status, time.Since(start).Round(time.Millisecond))
	writeHeaders(&b, "<", resp.Header)
	b.WriteString("\n")
	t.write(b.String())
	return resp, nil
}

func (t *debugTransport) write(s string) {
	if t.secret != "" {
		s = strings.ReplaceAll(s, t.secret, redacted)
	}
	_, _ = io.WriteString(t.w, s)
}

func writeHeaders(b *strings.Builder, prefix string, h http.Header) {
	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := strings.Join(h[name], ", ")
		if sensitiveHeaders[http.CanonicalHeaderKey(name)] {
			value = redactValue(value)
		}
		fmt.Fprintf(b, "%s %s: %s\n", prefix, name, value)
	}
}

// redactValue keeps an auth scheme such as "Bearer" and hides the rest.
func redactValue(v string) string {
	if scheme, _, ok := strings.Cut(v, " "); ok && !strings.ContainsAny(scheme, "=;") {
		return scheme + " " + redacted
	}
	return redacted
}
