package integration

import (
	"errors"
	"strings"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func noDefaultHost() (string, error) { return "", nil }

func defaultHost(url string) func() (string, error) {
	return func() (string, error) { return url, nil }
}

func TestLoadConfig(t *testing.T) {
	cases := []struct {
		name        string
		vars        map[string]string
		def         func() (string, error)
		wantErr     string // substring; "" means success
		wantHostKey string
	}{
		{
			name:        "given the logged-in default host is a youtrack.cloud instance, it targets it",
			vars:        map[string]string{envProject: "SCR"},
			def:         defaultHost("https://me.youtrack.cloud"),
			wantHostKey: "me.youtrack.cloud",
		},
		{
			name:        "given YTRACK_IT_HOST, it wins over the default host",
			vars:        map[string]string{envProject: "SCR", envHost: "other.youtrack.cloud"},
			def:         defaultHost("https://me.youtrack.cloud"),
			wantHostKey: "other.youtrack.cloud",
		},
		{
			name:    "given a default host containing enghouse, it refuses the work instance",
			vars:    map[string]string{envProject: "SCR"},
			def:     defaultHost("https://enghouseamg.youtrack.cloud"),
			wantErr: "it is the work YouTrack instance",
		},
		{
			name:    "given YTRACK_IT_HOST names the work instance in capitals, it refuses it",
			vars:    map[string]string{envProject: "SCR", envHost: "https://youtrack.EngHouse.com"},
			def:     noDefaultHost,
			wantErr: "it is the work YouTrack instance",
		},
		{
			name:    "given the work instance is also allowed explicitly, it still refuses it",
			vars:    map[string]string{envProject: "SCR", envHost: "yt.enghouseamg.com", envAllow: "yt.enghouseamg.com"},
			def:     noDefaultHost,
			wantErr: "it is the work YouTrack instance",
		},
		{
			name:    "given the default host is JetBrains' public tracker, it refuses it",
			vars:    map[string]string{envProject: "SCR"},
			def:     defaultHost("https://youtrack.jetbrains.com"),
			wantErr: "JetBrains' public issue tracker",
		},
		{
			name:    "given JetBrains' public tracker is allowed explicitly, it still refuses it",
			vars:    map[string]string{envProject: "SCR", envAllow: "youtrack.jetbrains.com"},
			def:     defaultHost("https://youtrack.jetbrains.com/"),
			wantErr: "JetBrains' public issue tracker",
		},
		{
			name:    "given a host outside youtrack.cloud that is not allowed, it refuses it and names YTRACK_IT_ALLOW_HOST",
			vars:    map[string]string{envProject: "SCR"},
			def:     defaultHost("https://yt.example.org"),
			wantErr: "YTRACK_IT_ALLOW_HOST=yt.example.org",
		},
		{
			name:    "given a look-alike youtrack.cloud host name, it refuses it",
			vars:    map[string]string{envProject: "SCR"},
			def:     defaultHost("https://youtrack.cloud.example.org"),
			wantErr: "YTRACK_IT_ALLOW_HOST",
		},
		{
			name:        "given YTRACK_IT_ALLOW_HOST names a self-hosted instance, it targets it",
			vars:        map[string]string{envProject: "SCR", envAllow: "https://yt.example.org/"},
			def:         defaultHost("https://yt.example.org"),
			wantHostKey: "yt.example.org",
		},
		{
			name:    "given YTRACK_IT_ALLOW_HOST names another host, it refuses the target",
			vars:    map[string]string{envProject: "SCR", envAllow: "yt.example.net"},
			def:     defaultHost("https://yt.example.org"),
			wantErr: "not a personal YouTrack Cloud host",
		},
		{
			name:    "given no project, it fails naming YTRACK_IT_PROJECT",
			vars:    map[string]string{},
			def:     defaultHost("https://me.youtrack.cloud"),
			wantErr: "set YTRACK_IT_PROJECT",
		},
		{
			name:    "given no logged-in host and no YTRACK_IT_HOST, it says how to log in",
			vars:    map[string]string{envProject: "SCR"},
			def:     noDefaultHost,
			wantErr: "ytrack auth login",
		},
		{
			name:    "given the config cannot be read, it reports that",
			vars:    map[string]string{envProject: "SCR"},
			def:     func() (string, error) { return "", errors.New("broken hosts.yml") },
			wantErr: "broken hosts.yml",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadConfig(env(tc.vars), tc.def)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Host.Key != tc.wantHostKey {
				t.Errorf("host %q, want %q", cfg.Host.Key, tc.wantHostKey)
			}
		})
	}
}

func TestSuiteRefusesBeforeRunningTheBinary(t *testing.T) {
	cases := map[string]string{
		"https://enghouseamg.youtrack.cloud": "work YouTrack instance",
		"https://youtrack.jetbrains.com":     "public issue tracker",
		"https://yt.example.org":             "not a personal YouTrack Cloud host",
	}
	for url, want := range cases {
		t.Run("given a suite built for "+url+", it refuses before running ytrack", func(t *testing.T) {
			// The binary does not exist: any attempt to run it would fail
			// with an exec error instead of the refusal.
			cfg := itConfig{Host: mustParse(t, url), Project: "SCR", Bin: "/nonexistent/ytrack"}
			var logged []string
			err := newSuite(cfg, nil, func(f string, _ ...any) { logged = append(logged, f) }).run()
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("error %v, want one containing %q", err, want)
			}
			if len(logged) != 0 {
				t.Errorf("the suite started work before refusing: %v", logged)
			}
		})
	}
}
