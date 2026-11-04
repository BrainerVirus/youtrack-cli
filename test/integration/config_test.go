package integration

import (
	"errors"
	"fmt"
	"strings"

	"github.com/BrainerVirus/youtrack-cli/internal/config"
	"github.com/BrainerVirus/youtrack-cli/internal/hosts"
)

// Environment variables the suite reads.
const (
	envEnable  = "YTRACK_IT"            // "1" runs the suite
	envProject = "YTRACK_IT_PROJECT"    // short name of the scratch project (required)
	envHost    = "YTRACK_IT_HOST"       // overrides the logged-in default host
	envToken   = "YTRACK_IT_TOKEN"      // overrides the stored token for that host
	envBin     = "YTRACK_IT_BIN"        // a prebuilt ytrack binary (make it sets it)
	envAllow   = "YTRACK_IT_ALLOW_HOST" // allows one host outside *.youtrack.cloud
)

// workInstanceMarker is part of every work YouTrack host name. The suite
// never runs against a host containing it.
const workInstanceMarker = "enghouse"

// publicTracker is JetBrains' public issue tracker. The suite never runs
// against it or any other jetbrains.com host, even when allowed.
const publicTracker = "youtrack.jetbrains.com"

// cloudSuffix marks a YouTrack Cloud instance, the only kind of host the
// suite runs against unless YTRACK_IT_ALLOW_HOST names another.
const cloudSuffix = ".youtrack.cloud"

// itConfig is what one run of the suite targets.
type itConfig struct {
	Host    hosts.Host
	Token   string // "" means the token ytrack auth login stored for Host
	Project string
	Bin     string // "" means build ytrack from this module
	// AllowHost is the key of the host YTRACK_IT_ALLOW_HOST names, or "".
	AllowHost string
}

// loadConfig resolves the suite's target from the environment. The host is
// YTRACK_IT_HOST, else the default host of ytrack's config (the logged-in
// session). It refuses a host checkHost rejects before anything is sent.
func loadConfig(getenv func(string) string, defaultHost func() (string, error)) (itConfig, error) {
	input := strings.TrimSpace(getenv(envHost))
	if input == "" {
		h, err := defaultHost()
		if err != nil {
			return itConfig{}, err
		}
		input = h
	}
	if input == "" {
		return itConfig{}, fmt.Errorf("no YouTrack host: run `ytrack auth login` against your personal instance or set %s", envHost)
	}
	if err := refuseAlways(input); err != nil {
		return itConfig{}, err
	}
	host, err := hosts.Parse(input)
	if err != nil {
		return itConfig{}, fmt.Errorf("YouTrack host: %w", err)
	}
	allow := ""
	if a := strings.TrimSpace(getenv(envAllow)); a != "" {
		ah, err := hosts.Parse(a)
		if err != nil {
			return itConfig{}, fmt.Errorf("%s: %w", envAllow, err)
		}
		allow = ah.Key
	}
	if err := checkHost(host, allow); err != nil {
		return itConfig{}, err
	}
	project := strings.TrimSpace(getenv(envProject))
	if project == "" {
		return itConfig{}, fmt.Errorf("set %s to the short name of a scratch project on %s", envProject, host.Key)
	}
	return itConfig{Host: host, Token: getenv(envToken), Project: project, Bin: getenv(envBin), AllowHost: allow}, nil
}

// checkHost decides whether the suite may write to host. It refuses the
// work instance and JetBrains' public tracker always, and otherwise runs
// only against a *.youtrack.cloud host or the host allowKey names.
func checkHost(host hosts.Host, allowKey string) error {
	if err := refuseAlways(host.Key); err != nil {
		return err
	}
	name := strings.ToLower(host.Key)
	if i := strings.IndexAny(name, ":/"); i >= 0 {
		name = name[:i]
	}
	if strings.HasSuffix(name, cloudSuffix) || (allowKey != "" && host.Key == allowKey) {
		return nil
	}
	return fmt.Errorf("refusing to run the real-instance suite against %q: it is not a personal YouTrack Cloud host (*%s); set %s=%s if it is a personal instance", host.Key, cloudSuffix, envAllow, host.Key)
}

// refuseAlways fails for the hosts no setting can allow: the work instance
// and jetbrains.com, whose YouTrack is JetBrains' public issue tracker.
func refuseAlways(host string) error {
	h := strings.ToLower(host)
	if strings.Contains(h, workInstanceMarker) {
		return fmt.Errorf("refusing to run the real-instance suite against %q: it is the work YouTrack instance (host contains %q); use a personal instance", host, workInstanceMarker)
	}
	name := strings.TrimPrefix(strings.TrimPrefix(h, "https://"), "http://")
	if i := strings.IndexAny(name, ":/"); i >= 0 {
		name = name[:i]
	}
	if name == "jetbrains.com" || strings.HasSuffix(name, ".jetbrains.com") {
		return fmt.Errorf("refusing to run the real-instance suite against %q: %s is JetBrains' public issue tracker, not a personal instance; log in to your own *%s instance", host, publicTracker, cloudSuffix)
	}
	return nil
}

// configDefaultHost returns the service URL of ytrack's default host.
func configDefaultHost() (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("reading ytrack config: %w", err)
	}
	key := cfg.DefaultHost()
	if key == "" {
		return "", nil
	}
	if e := cfg.Host(key); e != nil && e.URL != "" {
		return e.URL, nil
	}
	return "", errors.New("the default host in ytrack's hosts.yml has no URL; run `ytrack auth login` again")
}
