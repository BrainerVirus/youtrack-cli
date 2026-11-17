package transport

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// APIError is a non-2xx response from YouTrack.
type APIError struct {
	StatusCode int
	Status     string
	// Code and Description are YouTrack's "error" and "error_description".
	Code        string
	Description string
	// Origin, when set, names the host that answered and why it was picked;
	// ytrack sets it when more than one host is logged in.
	Origin *Origin
}

// Origin says which host answered a request, why ytrack picked it and which
// other logged-in hosts the user could try instead.
type Origin struct {
	// Host is the host key, e.g. acme.youtrack.cloud.
	Host string
	// Why completes "chosen ...", e.g. "by --host" or "as the default host".
	Why string
	// Others are the other logged-in host keys.
	Others []string
}

// Hint is "; <host> chosen <why>; try --host <other>", or "" for a nil Origin.
func (o *Origin) Hint() string {
	if o == nil {
		return ""
	}
	return fmt.Sprintf("; %s chosen %s", o.Host, o.Why) + o.try()
}

// try is "; try --host <a> or --host <b>", or "" with no other hosts.
func (o *Origin) try() string {
	if len(o.Others) == 0 {
		return ""
	}
	tries := make([]string, len(o.Others))
	for i, h := range o.Others {
		tries[i] = "--host " + h
	}
	return "; try " + strings.Join(tries, " or ")
}

// HostHint is the Origin's Hint, for messages that already name the host.
func (e *APIError) HostHint() string { return e.Origin.Hint() }

func (e *APIError) Error() string {
	var msg string
	switch {
	case e.Description != "" && e.Code != "" && e.Code != e.Description:
		msg = fmt.Sprintf("%s (%s)", e.Description, e.Code)
	case e.Description != "":
		msg = e.Description
	case e.Code != "":
		msg = e.Code
	default:
		msg = strings.TrimSpace(strings.TrimPrefix(e.Status, fmt.Sprint(e.StatusCode)))
	}
	head := fmt.Sprintf("HTTP %d", e.StatusCode)
	if e.Origin != nil {
		head += fmt.Sprintf(" from %s (chosen %s)", e.Origin.Host, e.Origin.Why)
	}
	if msg != "" {
		head += ": " + msg
	}
	if e.Origin != nil {
		head += e.Origin.try()
	}
	return head
}

// HTTPStatus implements clierr.StatusCoder.
func (e *APIError) HTTPStatus() int { return e.StatusCode }

// CheckResponse returns nil for 2xx responses and an *APIError otherwise. It
// consumes the body of an error response.
func CheckResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return ParseError(resp.StatusCode, resp.Status, resp.Body)
}

// ParseError builds an *APIError from an error response body.
func ParseError(statusCode int, status string, body io.Reader) *APIError {
	e := &APIError{StatusCode: statusCode, Status: status}
	b, _ := io.ReadAll(io.LimitReader(body, 1<<20))
	var payload struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if json.Unmarshal(b, &payload) == nil {
		e.Code = payload.Error
		e.Description = payload.Description
	}
	return e
}
