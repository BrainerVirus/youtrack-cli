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
}

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
	if msg == "" {
		return fmt.Sprintf("HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, msg)
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
