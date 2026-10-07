package hosts

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantURL string
		wantKey string
	}{
		{"a bare cloud hostname gets https", "acme.youtrack.cloud", "https://acme.youtrack.cloud", "acme.youtrack.cloud"},
		{"a trailing slash is dropped", "https://acme.youtrack.cloud/", "https://acme.youtrack.cloud", "acme.youtrack.cloud"},
		{"surrounding whitespace is ignored", "  acme.youtrack.cloud \n", "https://acme.youtrack.cloud", "acme.youtrack.cloud"},
		{"the hostname is lowercased", "HTTPS://Acme.YouTrack.Cloud", "https://acme.youtrack.cloud", "acme.youtrack.cloud"},
		{"a server path prefix is kept", "https://tools.acme.com/youtrack/", "https://tools.acme.com/youtrack", "tools.acme.com/youtrack"},
		{"a pasted REST root is reduced to the service URL", "https://tools.acme.com/youtrack/api/", "https://tools.acme.com/youtrack", "tools.acme.com/youtrack"},
		{"query and fragment are discarded", "https://acme.youtrack.cloud/?tab=x#y", "https://acme.youtrack.cloud", "acme.youtrack.cloud"},
		{"the default https port is dropped", "https://acme.youtrack.cloud:443", "https://acme.youtrack.cloud", "acme.youtrack.cloud"},
		{"a custom port is kept in the key", "http://127.0.0.1:8080", "http://127.0.0.1:8080", "127.0.0.1:8080"},
		{"an internationalized name becomes punycode", "https://bücher.example/youtrack", "https://xn--bcher-kva.example/youtrack", "xn--bcher-kva.example/youtrack"},
		{"an upper-case internationalized name gets the same key", "BÜCHER.example", "https://xn--bcher-kva.example", "xn--bcher-kva.example"},
		{"an IPv6 literal keeps its brackets", "http://[::1]:8080", "http://[::1]:8080", "[::1]:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.input, err)
			}
			if h.URL != tt.wantURL || h.Key != tt.wantKey {
				t.Errorf("Parse(%q) = {%q %q}, want {%q %q}", tt.input, h.URL, h.Key, tt.wantURL, tt.wantKey)
			}
			if h.APIURL() != tt.wantURL+"/api" {
				t.Errorf("APIURL() = %q", h.APIURL())
			}
		})
	}
}

func TestParseRejects(t *testing.T) {
	for name, input := range map[string]string{
		"an empty string":        "  ",
		"a non-http scheme":      "ftp://acme.youtrack.cloud",
		"a URL with credentials": "https://user:pass@acme.youtrack.cloud",
		"a scheme without host":  "https://",
	} {
		t.Run("given "+name+", it returns an error", func(t *testing.T) {
			if _, err := Parse(input); err == nil {
				t.Errorf("Parse(%q) succeeded, want error", input)
			}
		})
	}
}

func TestPlainHTTP(t *testing.T) {
	for input, want := range map[string]bool{
		"https://acme.youtrack.cloud": false,
		"http://acme.youtrack.cloud":  true,
		"http://127.0.0.1:8080":       false,
		"http://localhost:8080":       false,
		"http://[::1]:8080":           false,
		"http://10.0.0.5/youtrack":    true,
	} {
		h, err := Parse(input)
		if err != nil {
			t.Fatal(err)
		}
		if h.PlainHTTP() != want {
			t.Errorf("%s: PlainHTTP() = %t, want %t", input, h.PlainHTTP(), want)
		}
	}
}
