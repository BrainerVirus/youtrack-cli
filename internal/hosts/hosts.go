// Package hosts normalizes YouTrack service URLs.
//
// A host is identified by its key: the service URL without scheme or trailing
// slash, e.g. "acme.youtrack.cloud" or "tools.acme.com/youtrack" for a Server
// install under a path prefix. The key names config entries and keyring items.
package hosts

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"golang.org/x/net/idna"
)

// Host is a normalized YouTrack service location.
type Host struct {
	// URL is the canonical service URL, e.g. https://tools.acme.com/youtrack.
	URL string
	// Key is URL without the scheme, e.g. tools.acme.com/youtrack.
	Key string
}

// IsHTTPS reports whether the service URL uses https.
func (h Host) IsHTTPS() bool { return strings.HasPrefix(h.URL, "https://") }

// IsLoopback reports whether the host is localhost or a loopback address.
func (h Host) IsLoopback() bool {
	u, err := url.Parse(h.URL)
	if err != nil {
		return false
	}
	name := u.Hostname()
	if name == "localhost" || strings.HasSuffix(name, ".localhost") {
		return true
	}
	ip := net.ParseIP(name)
	return ip != nil && ip.IsLoopback()
}

// PlainHTTP reports whether credentials would cross the network unencrypted:
// plain http to a host that is not loopback.
func (h Host) PlainHTTP() bool { return !h.IsHTTPS() && !h.IsLoopback() }

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

	// Internationalized names are stored as punycode so one host has one key.
	name := u.Hostname()
	if ip := net.ParseIP(name); ip != nil {
		name = ip.String()
		if ip.To4() == nil {
			name = "[" + name + "]"
		}
	} else {
		name, err = idna.Lookup.ToASCII(name)
		if err != nil {
			return Host{}, fmt.Errorf("invalid host %q: %w", input, err)
		}
	}
	hostport := strings.ToLower(name)
	defaultPort := map[string]string{"https": "443", "http": "80"}[scheme]
	if port := u.Port(); port != "" && port != defaultPort {
		hostport += ":" + port
	}

	return Host{
		URL: scheme + "://" + hostport + path,
		Key: hostport + path,
	}, nil
}
