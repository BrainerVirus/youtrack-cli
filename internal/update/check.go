package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/BrainerVirus/youtrack-cli/internal/config"
)

// CheckInterval is how long a release check is reused before asking again.
const CheckInterval = 24 * time.Hour

// StateFile is the file in the state directory that records the last check.
const StateFile = "state.yml"

// State is what the daily check remembers between runs.
type State struct {
	CheckedAt     time.Time `yaml:"checked_at"`
	LatestVersion string    `yaml:"latest_version,omitempty"`
	LatestURL     string    `yaml:"latest_url,omitempty"`
	// NotifiedAt and NotifiedVersion record the last notice printed, so a
	// cached newer release is announced once per CheckInterval.
	NotifiedAt      time.Time `yaml:"notified_at,omitempty"`
	NotifiedVersion string    `yaml:"notified_version,omitempty"`
}

// stateMu serialises read-modify-write updates of the state file within the
// process: the background check and the notice both write it.
var stateMu sync.Mutex

// updateState applies fn to the state at path and saves it (best effort).
func updateState(path string, fn func(*State)) {
	stateMu.Lock()
	defer stateMu.Unlock()
	s := ReadState(path)
	fn(&s)
	_ = WriteState(path, s)
}

// ReadState loads the check state from path. A missing or unreadable file
// is an empty state.
func ReadState(path string) State {
	var s State
	b, err := os.ReadFile(path)
	if err != nil {
		return State{}
	}
	if yaml.Unmarshal(b, &s) != nil {
		return State{}
	}
	return s
}

// WriteState saves the check state to path.
func WriteState(path string, s State) error {
	b, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(path, b)
}

// Due reports whether the last check recorded at statePath is older than
// CheckInterval, so a new one may go to the network.
func Due(statePath string, now time.Time) bool {
	s := ReadState(statePath)
	return s.CheckedAt.IsZero() || now.Sub(s.CheckedAt) >= CheckInterval || s.CheckedAt.After(now)
}

// Notice is a newer release worth telling the user about.
type Notice struct {
	Current, Latest, URL string
}

// CheckDaily asks for the latest release when the last check recorded at
// statePath is older than CheckInterval, records this check (also when it
// fails, so an offline machine asks once a day, not on every run; a failed
// check keeps the release cached before it) and returns a Notice when that
// release is newer than current. It returns nil without any network call
// when a check already happened within the interval. A check cancelled
// through ctx (abandoned by the caller) records nothing, so the next run
// asks again.
func CheckDaily(ctx context.Context, c *Client, statePath, current string, now time.Time) (*Notice, error) {
	if !Due(statePath, now) {
		return nil, nil
	}
	rel, err := c.Latest(ctx)
	if err != nil && errors.Is(ctx.Err(), context.Canceled) {
		return nil, err
	}
	stable := err == nil && !rel.Draft && !rel.Prerelease
	// Best effort: a state directory that cannot be written only means the
	// next run checks again.
	updateState(statePath, func(s *State) {
		s.CheckedAt = now
		if stable {
			s.LatestVersion, s.LatestURL = rel.Version(), rel.URL
		}
	})
	if err != nil {
		return nil, err
	}
	if !stable || !Newer(rel.Version(), current) {
		return nil, nil
	}
	return &Notice{Current: current, Latest: rel.Version(), URL: rel.URL}, nil
}

// Pending returns the cached release at statePath as a Notice when it is
// newer than current and was not announced within CheckInterval. It reads
// only the state file, never the network.
func Pending(statePath, current string, now time.Time) *Notice {
	s := ReadState(statePath)
	if s.LatestVersion == "" || !Newer(s.LatestVersion, current) {
		return nil
	}
	if s.NotifiedVersion == s.LatestVersion && !s.NotifiedAt.IsZero() &&
		now.Sub(s.NotifiedAt) < CheckInterval && !s.NotifiedAt.After(now) {
		return nil
	}
	return &Notice{Current: current, Latest: s.LatestVersion, URL: s.LatestURL}
}

// MarkNotified records that the notice for version was printed at now.
func MarkNotified(statePath, version string, now time.Time) {
	updateState(statePath, func(s *State) { s.NotifiedAt, s.NotifiedVersion = now, version })
}

// StatePath returns the path of the check state file in the state directory.
func StatePath() (string, error) {
	dir, err := config.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, StateFile), nil
}
