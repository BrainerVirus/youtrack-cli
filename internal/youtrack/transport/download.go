package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrStalled is the cause of a download that received nothing for the
// client's timeout.
var ErrStalled = errors.New("download stalled")

// Download sends GET ref and copies a successful response body to w,
// returning the number of bytes written. ref is a URL the server returned:
// absolute, or a path from the server root such as an attachment's signed
// url, which already includes the service path (/youtrack/api/files/...).
// An absolute ref must have the service URL's origin (scheme, host and
// port) and hold no user info: a file URL elsewhere is refused, not fetched.
//
// The client's timeout bounds the wait for the response and each pause in
// the body, not the whole transfer, so a large file on a slow link
// completes while a stalled one fails.
func (c *Client) Download(ctx context.Context, ref string, w io.Writer) (int64, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return 0, err
	}
	u, err := base.Parse(ref)
	if err != nil {
		return 0, fmt.Errorf("invalid file URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return 0, fmt.Errorf("refusing to download from %s: not http(s)", redactURL(u))
	}
	if u.User != nil {
		return 0, fmt.Errorf("refusing to download from %s: the URL holds credentials", redactURL(u))
	}
	if !strings.EqualFold(u.Scheme, base.Scheme) || !strings.EqualFold(u.Host, base.Host) {
		return 0, fmt.Errorf("refusing to download from %s: outside %s", redactURL(u), base.Redacted())
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stall := time.AfterFunc(c.timeout, func() { cancel(ErrStalled) })
	defer stall.Stop()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "*/*")
	hc := *c.http
	hc.Timeout = 0 // the stall timer bounds the transfer instead
	resp, err := hc.Do(req)
	if err != nil {
		return 0, stalled(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := c.CheckResponse(resp); err != nil {
		return 0, err
	}
	n, err := io.Copy(w, &progressReader{r: resp.Body, onRead: func() { stall.Reset(c.timeout) }})
	return n, stalled(ctx, err)
}

// stalled replaces err with ErrStalled when the stall timer cancelled ctx.
func stalled(ctx context.Context, err error) error {
	if err != nil && errors.Is(context.Cause(ctx), ErrStalled) {
		return ErrStalled
	}
	return err
}

// progressReader calls onRead after every read that returned data.
type progressReader struct {
	r      io.Reader
	onRead func()
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.onRead()
	}
	return n, err
}
