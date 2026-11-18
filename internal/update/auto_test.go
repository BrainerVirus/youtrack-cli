package update_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BrainerVirus/youtrack-cli/internal/update"
	"github.com/BrainerVirus/youtrack-cli/internal/upgrade/upgradetest"
)

func TestCheckDailyCachesThePublicationTime(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	published := now.Add(-30 * time.Hour)
	srv := upgradetest.NewServer(t, upgradetest.NewSigstore(t), &upgradetest.Release{Version: "1.1.0", PublishedAt: published})
	c := &update.Client{HTTP: srv.Client(), APIURL: srv.URL}
	state := filepath.Join(t.TempDir(), "state.yml")
	if _, err := update.CheckDaily(context.Background(), c, state, "1.0.0", now); err != nil {
		t.Fatal(err)
	}
	if s := update.ReadState(state); !s.LatestPublishedAt.Equal(published) {
		t.Errorf("latest_published_at = %v, want %v", s.LatestPublishedAt, published)
	}
}

func TestAutoDue(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	soaked := update.State{LatestVersion: "1.1.0", LatestPublishedAt: now.Add(-25 * time.Hour)}
	if !soaked.AutoDue("1.0.0", now) {
		t.Fatal("given a newer release published 25h ago, it is not due")
	}
	for name, tc := range map[string]struct {
		state   func(s *update.State)
		current string
	}{
		"no cached release":                         {state: func(s *update.State) { s.LatestVersion = "" }},
		"the cached release is installed":           {current: "1.1.0"},
		"a development build":                       {current: "dev"},
		"a release published 23h ago":               {state: func(s *update.State) { s.LatestPublishedAt = now.Add(-23 * time.Hour) }},
		"a release with no publication time":        {state: func(s *update.State) { s.LatestPublishedAt = time.Time{} }},
		"an active backoff":                         {state: func(s *update.State) { s.Auto.RetryAt = now.Add(time.Minute) }},
		"a background upgrade started 10 minutes ago": {state: func(s *update.State) { s.Auto.StartedAt = now.Add(-10 * time.Minute) }},
	} {
		t.Run("given "+name+", it is not due", func(t *testing.T) {
			s := soaked
			if tc.state != nil {
				tc.state(&s)
			}
			current := tc.current
			if current == "" {
				current = "1.0.0"
			}
			if s.AutoDue(current, now) {
				t.Errorf("AutoDue(%q) = true for %+v", current, s)
			}
		})
	}
	t.Run("given a backoff that ended and a start 2h ago, it is due", func(t *testing.T) {
		s := soaked
		s.Auto.RetryAt, s.Auto.StartedAt = now.Add(-time.Minute), now.Add(-2*time.Hour)
		if !s.AutoDue("1.0.0", now) {
			t.Error("not due")
		}
	})
}

func TestAutoFailureBacksOffExponentially(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	var a update.AutoState
	day := 24 * time.Hour
	for i, want := range []time.Duration{day, 2 * day, 4 * day, 8 * day, 8 * day} {
		a.RecordAutoFailure("1.1.0", errors.New("boom\nsecond line"), now)
		if got := a.RetryAt.Sub(now); got != want {
			t.Errorf("failure %d: retry after %v, want %v", i+1, got, want)
		}
	}
	if a.Error != "boom second line" || a.FailedVersion != "1.1.0" || a.Failures != 5 {
		t.Errorf("state = %+v", a)
	}
	a.RecordAutoFailure("1.1.0", errors.New(strings.Repeat("x", 2000)), now)
	if len(a.Error) > 600 {
		t.Errorf("error kept %d bytes, want it capped", len(a.Error))
	}

	a.RecordSuccess("1.0.0", "1.1.0", now, true)
	if a.Failures != 0 || a.Error != "" || a.BackingOff(now) {
		t.Errorf("a success did not reset the backoff: %+v", a)
	}
	if a.From != "1.0.0" || a.To != "1.1.0" || a.Announced {
		t.Errorf("background success = %+v", a)
	}
}
