package update

import (
	"strings"
	"time"
)

// SoakPeriod is how old a release must be before the background upgrade
// installs it, so a broken release can be pulled before it spreads. A manual
// `ytrack upgrade` does not wait.
const SoakPeriod = 24 * time.Hour

// SpawnInterval is the least time between two background upgrade starts, so
// a child that dies before it records anything is not restarted on every run.
const SpawnInterval = time.Hour

// maxBackoff caps the wait after repeated background upgrade failures.
const maxBackoff = 8 * 24 * time.Hour

// maxErrorLen caps the failure message kept in the state file.
const maxErrorLen = 500

// AutoState is what the background upgrade remembers between runs.
type AutoState struct {
	// StartedAt is when a run last started a background upgrade.
	StartedAt time.Time `yaml:"started_at,omitempty"`

	// From, To and UpgradedAt record the last successful background upgrade;
	// Announced is set once a terminal was told about it.
	From       string    `yaml:"from,omitempty"`
	To         string    `yaml:"to,omitempty"`
	UpgradedAt time.Time `yaml:"upgraded_at,omitempty"`
	Announced  bool      `yaml:"announced,omitempty"`

	// The last failed attempt since the last success: the release it tried,
	// when, why, how many failures in a row, and when it may try again.
	FailedVersion string    `yaml:"failed_version,omitempty"`
	FailedAt      time.Time `yaml:"failed_at,omitempty"`
	Error         string    `yaml:"error,omitempty"`
	Failures      int       `yaml:"failures,omitempty"`
	RetryAt       time.Time `yaml:"retry_at,omitempty"`
}

// Backoff is the wait after the nth background upgrade failure in a row:
// 1 day, 2, 4, then 8 days at most.
func Backoff(n int) time.Duration {
	d := 24 * time.Hour
	for i := 1; i < n && d < maxBackoff; i++ {
		d *= 2
	}
	return min(d, maxBackoff)
}

// BackingOff reports whether a failed background upgrade still blocks a
// new attempt at now.
func (a *AutoState) BackingOff(now time.Time) bool {
	return !a.RetryAt.IsZero() && now.Before(a.RetryAt)
}

// Soaked reports whether a release published at published is at least
// SoakPeriod old at now. An unknown publication time is not soaked.
func Soaked(published, now time.Time) bool {
	return !published.IsZero() && now.Sub(published) >= SoakPeriod
}

// AutoDue reports whether the cached state calls for a background upgrade
// of current at now: a newer stable release is known and soaked, no failure
// backoff is active, and none was started within SpawnInterval.
func (s *State) AutoDue(current string, now time.Time) bool {
	if s.LatestVersion == "" || !Newer(s.LatestVersion, current) || !Soaked(s.LatestPublishedAt, now) {
		return false
	}
	if s.Auto.BackingOff(now) {
		return false
	}
	started := s.Auto.StartedAt
	return started.IsZero() || started.After(now) || now.Sub(started) >= SpawnInterval
}

// RecordAutoFailure records a failed background upgrade to version and
// schedules the next attempt with Backoff.
func (a *AutoState) RecordAutoFailure(version string, err error, now time.Time) {
	msg := strings.Join(strings.Fields(err.Error()), " ")
	if len(msg) > maxErrorLen {
		msg = msg[:maxErrorLen] + "…"
	}
	a.Failures++
	a.FailedVersion, a.FailedAt, a.Error = version, now, msg
	a.RetryAt = now.Add(Backoff(a.Failures))
}

// RecordSuccess records an upgrade from one version to another and clears
// any failure and backoff. background marks it for a one-time announcement.
func (a *AutoState) RecordSuccess(from, to string, now time.Time, background bool) {
	*a = AutoState{StartedAt: a.StartedAt}
	if background {
		a.From, a.To, a.UpgradedAt = from, to, now
	}
}
