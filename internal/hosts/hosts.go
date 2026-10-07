// Package hosts normalizes YouTrack service URLs.
//
// A host is identified by its key: the service URL without scheme or trailing
// slash, e.g. "acme.youtrack.cloud" or "tools.acme.com/youtrack" for a Server
// install under a path prefix. The key names config entries and keyring items.
package hosts

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Host is a normalized YouTrack service location.
type Host struct {
	// URL is the canonical service URL, e.g. https://tools.acme.com/youtrack.
	URL string
	// Key is URL without the scheme, e.g. tools.acme.com/youtrack.
	Key string
}

// APIURL returns the REST API root, e.g. https://acme.youtrack.cloud/api.
func (h Host) APIURL() string { return h.URL + "/api" }

// Parse normalizes user input such as "acme.youtrack.cloud",
// "https://acme.youtrack.cloud/", or "https://tools.acme.com/youtrack/api".
// Input without a scheme gets https.
func Parse(input string) (Host, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return Host{}, errors.New("host is empty")
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return Host{}, fmt.Errorf("invalid host %q: %w", input, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return Host{}, fmt.Errorf("invalid host %q: scheme must be http or https", input)
	}
	if u.Host == "" {
		return Host{}, fmt.Errorf("invalid host %q: missing hostname", input)
	}
	if u.User != nil {
		return Host{}, fmt.Errorf("invalid host %q: credentials in the URL are not allowed", input)
	}

	path := strings.TrimRight(u.Path, "/")
	// Users often paste the REST root; the service URL is what we store.
	path = strings.TrimSuffix(path, "/api")
	path = strings.TrimRight(path, "/")

	hostport := strings.ToLower(u.Host)
	if scheme == "https" {
		hostport = strings.TrimSuffix(hostport, ":443")
	} else {
		hostport = strings.TrimSuffix(hostport, ":80")
	}

	return Host{
		URL: scheme + "://" + hostport + path,
		Key: hostport + path,
	}, nil
}
