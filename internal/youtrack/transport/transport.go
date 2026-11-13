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
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
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
	return c.send(ctx, method, path, "application/json", bytes.NewReader(b), v)
}

// FilePart is one file of a streamed multipart upload.
type FilePart struct {
	Name        string
	ContentType string
	// Size is the file's length in bytes. The upload fails if Open yields
	// more or fewer bytes, such as a file that changed during the upload.
	Size int64
	// Open returns the file's content from its start. It is called once per
	// attempt: once for the request and again for each redirect that resends
	// the body.
	Open func() (io.ReadCloser, error)
}

// SizeChangedError reports a file whose length changed during an upload.
type SizeChangedError struct {
	Name       string
	Want, Got  int64
	MoreToRead bool
}

func (e *SizeChangedError) Error() string {
	if e.MoreToRead {
		return fmt.Sprintf("%s grew during the upload: it was %d bytes", e.Name, e.Want)
	}
	return fmt.Sprintf("%s changed during the upload: it was %d bytes, %d were read", e.Name, e.Want, e.Got)
}

// SendMultipart sends POST path with files as multipart/form-data, one part
// per file named files[0], files[1], ..., and decodes a successful JSON
// response into v (when v is non-nil). It streams each file instead of
// holding it in memory: every file is opened before the request goes out, so
// one that cannot be opened sends nothing, and the body has an exact
// Content-Length. A 307 or 308 redirect reopens the files to resend it.
func (c *Client) SendMultipart(ctx context.Context, path string, files []FilePart, v any) error {
	mp, err := newMultipartBody(files)
	if err != nil {
		return err
	}
	body, err := mp.open()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL(path), body)
	if err != nil {
		_ = body.Close()
		return err
	}
	req.ContentLength = mp.length
	req.GetBody = mp.open
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+mp.boundary)
	err = c.do(req, path, v)
	if sc := (*SizeChangedError)(nil); errors.As(err, &sc) {
		return sc
	}
	return err
}

// multipartBody describes a multipart/form-data body of files with a fixed
// boundary, so its length is known before any file is read.
type multipartBody struct {
	files    []FilePart
	headers  []textproto.MIMEHeader
	boundary string
	length   int64
}

func newMultipartBody(files []FilePart) (*multipartBody, error) {
	mp := &multipartBody{files: files, headers: make([]textproto.MIMEHeader, len(files))}
	for i, fp := range files {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
			"name": fmt.Sprintf("files[%d]", i), "filename": fp.Name,
		}))
		ct := fp.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		h.Set("Content-Type", ct)
		mp.headers[i] = h
	}
	// The framing (boundaries and part headers) does not depend on the file
	// content, so writing it without content counts every byte but the files'.
	var framing countingWriter
	mw := multipart.NewWriter(&framing)
	mp.boundary = mw.Boundary()
	for i, fp := range files {
		if _, err := mw.CreatePart(mp.headers[i]); err != nil {
			return nil, err
		}
		mp.length += fp.Size
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	mp.length += framing.n
	return mp, nil
}

// open opens every file, then returns a body that streams them. If one fails
// to open, the files already open are closed and no body is returned.
func (mp *multipartBody) open() (io.ReadCloser, error) {
	readers := make([]io.ReadCloser, 0, len(mp.files))
	for _, fp := range mp.files {
		r, err := fp.Open()
		if err != nil {
			closeAll(readers)
			return nil, err
		}
		readers = append(readers, r)
	}
	pr, pw := io.Pipe()
	go func() {
		defer closeAll(readers)
		_ = pw.CloseWithError(mp.write(pw, readers))
	}()
	return pr, nil
}

func (mp *multipartBody) write(w io.Writer, readers []io.ReadCloser) error {
	mw := multipart.NewWriter(w)
	if err := mw.SetBoundary(mp.boundary); err != nil {
		return err
	}
	for i, r := range readers {
		pw, err := mw.CreatePart(mp.headers[i])
		if err != nil {
			return err
		}
		if err := copyExact(pw, r, mp.files[i]); err != nil {
			return err
		}
	}
	return mw.Close()
}

// copyExact copies exactly fp.Size bytes of r to w, failing if r has fewer
// or more, so a file that changed size never sends a body that disagrees with
// its Content-Length.
func copyExact(w io.Writer, r io.Reader, fp FilePart) error {
	n, err := io.Copy(w, io.LimitReader(r, fp.Size))
	if err != nil {
		return fmt.Errorf("reading %s: %w", fp.Name, err)
	}
	if n < fp.Size {
		return &SizeChangedError{Name: fp.Name, Want: fp.Size, Got: n}
	}
	var extra [1]byte
	if k, _ := io.ReadFull(r, extra[:]); k > 0 {
		return &SizeChangedError{Name: fp.Name, Want: fp.Size, Got: n + 1, MoreToRead: true}
	}
	return nil
}

func closeAll(rs []io.ReadCloser) {
	for _, r := range rs {
		_ = r.Close()
	}
}

type countingWriter struct{ n int64 }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
}

func (c *Client) send(ctx context.Context, method, path, contentType string, body io.Reader, v any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.URL(path), body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	return c.do(req, path, v)
}

// do sends req and decodes a successful JSON response into v (when v is
// non-nil).
func (c *Client) do(req *http.Request, path string, v any) error {
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

// Delete sends DELETE path and discards a successful response body.
func (c *Client) Delete(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.URL(path), http.NoBody)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return CheckResponse(resp)
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
