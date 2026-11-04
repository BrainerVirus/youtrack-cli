package update

import (
	"context"
	"os"
	"path/filepath"
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
// fails, so an offline machine asks once a day, not on every run) and
// returns a Notice when that release is newer than current. It returns nil
// without any network call when a check already happened within the interval.
func CheckDaily(ctx context.Context, c *Client, statePath, current string, now time.Time) (*Notice, error) {
	if !Due(statePath, now) {
		return nil, nil
	}
	rel, err := c.Latest(ctx)
	state := State{CheckedAt: now}
	if err == nil {
		state.LatestVersion, state.LatestURL = rel.Version(), rel.URL
	}
	// Best effort: a state directory that cannot be written only means the
	// next run checks again.
	_ = WriteState(statePath, state)
	if err != nil {
		return nil, err
	}
	if rel.Draft || rel.Prerelease || !Newer(rel.Version(), current) {
		return nil, nil
	}
	return &Notice{Current: current, Latest: rel.Version(), URL: rel.URL}, nil
}

// StatePath returns the path of the check state file in the state directory.
func StatePath() (string, error) {
	dir, err := config.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, StateFile), nil
}
