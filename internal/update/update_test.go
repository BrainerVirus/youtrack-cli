package update_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/BrainerVirus/youtrack-cli/internal/update"
	"github.com/BrainerVirus/youtrack-cli/internal/upgrade/upgradetest"
)

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"1.2.0", "1.1.9", true},
		{"v1.10.0", "1.9.0", true},
		{"1.2.0", "1.2.0", false},
		{"1.2.0", "1.2.0-rc.1", true},
		{"1.2.0-rc.2", "1.2.0-rc.1", true},
		{"1.2.0-rc.1", "1.2.0", false},
		{"1.2.0-rc.10", "1.2.0-rc.9", true},
		{"1.1.0", "1.2.0", false},
		{"1.2.0", "dev", false},
		{"1.2.0", "test", false},
		{"1.2", "1.1.0", false},
	} {
		if got := update.Newer(tc.a, tc.b); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestDetectManager(t *testing.T) {
	for path, want := range map[string]string{
		"/opt/homebrew/Caskroom/ytrack/1.0.0/ytrack":                                         "brew upgrade brainervirus/tap/ytrack",
		"/usr/local/Cellar/ytrack/1.0.0/bin/ytrack":                                          "brew upgrade brainervirus/tap/ytrack",
		"/home/linuxbrew/.linuxbrew/bin/ytrack":                                              "brew upgrade brainervirus/tap/ytrack",
		`C:\Users\jd\scoop\apps\ytrack\current\ytrack.exe`:                                   "scoop update ytrack",
		`C:\Users\jd\AppData\Local\Microsoft\WinGet\Packages\BrainerVirus.ytrack\ytrack.exe`: "winget upgrade ytrack",
		`C:\Users\jd\AppData\Local\Microsoft\WinGet\Links\ytrack.exe`:                        "winget upgrade ytrack",
		"/home/jd/.local/bin/ytrack":                                                         "",
		`C:\Users\jd\AppData\Local\Programs\ytrack\ytrack.exe`:                               "",
	} {
		got := ""
		if m := update.DetectManager(path); m != nil {
			got = m.Command
		}
		if got != want {
			t.Errorf("%s: %q, want %q", path, got, want)
		}
	}
	t.Run("given a symlink into the Cellar, the target decides", func(t *testing.T) {
		if m := update.DetectManager("/usr/local/bin/ytrack", "/usr/local/Cellar/ytrack/1.0.0/bin/ytrack"); m == nil || m.Name != "Homebrew" {
			t.Errorf("got %+v", m)
		}
	})
}

func TestCheckDaily(t *testing.T) {
	s := upgradetest.NewSigstore(t)
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

	t.Run("given a newer release, it returns a notice once per 24h", func(t *testing.T) {
		srv := upgradetest.NewServer(t, s, &upgradetest.Release{Version: "1.0.0"}, &upgradetest.Release{Version: "1.1.0"})
		c := &update.Client{HTTP: srv.Client(), APIURL: srv.URL}
		state := filepath.Join(t.TempDir(), "state.yml")

		n, err := update.CheckDaily(context.Background(), c, state, "1.0.0", now)
		if err != nil || n == nil || n.Latest != "1.1.0" || n.Current != "1.0.0" {
			t.Fatalf("notice = %+v, err = %v", n, err)
		}
		n, err = update.CheckDaily(context.Background(), c, state, "1.0.0", now.Add(23*time.Hour))
		if err != nil || n != nil {
			t.Fatalf("within 24h: notice = %+v, err = %v", n, err)
		}
		if got := len(srv.Requests()); got != 1 {
			t.Errorf("requests = %d, want 1 (no network within 24h)", got)
		}
		n, _ = update.CheckDaily(context.Background(), c, state, "1.0.0", now.Add(25*time.Hour))
		if n == nil {
			t.Error("after 24h: no notice")
		}
	})

	t.Run("given the latest is a prerelease, it is ignored", func(t *testing.T) {
		srv := upgradetest.NewServer(t, s, &upgradetest.Release{Version: "1.0.0"}, &upgradetest.Release{Version: "1.1.0-rc.1", Prerelease: true})
		c := &update.Client{HTTP: srv.Client(), APIURL: srv.URL}
		n, err := update.CheckDaily(context.Background(), c, filepath.Join(t.TempDir(), "state.yml"), "1.0.0", now)
		if err != nil || n != nil {
			t.Fatalf("notice = %+v, err = %v", n, err)
		}
	})

	t.Run("given the current version is the latest, there is no notice", func(t *testing.T) {
		srv := upgradetest.NewServer(t, s, &upgradetest.Release{Version: "1.0.0"})
		c := &update.Client{HTTP: srv.Client(), APIURL: srv.URL}
		n, err := update.CheckDaily(context.Background(), c, filepath.Join(t.TempDir(), "state.yml"), "1.0.0", now)
		if err != nil || n != nil {
			t.Fatalf("notice = %+v, err = %v", n, err)
		}
	})

	t.Run("given a failed check, it waits 24h before asking again", func(t *testing.T) {
		srv := upgradetest.NewServer(t, s) // no releases: latest is 404
		c := &update.Client{HTTP: srv.Client(), APIURL: srv.URL}
		state := filepath.Join(t.TempDir(), "state.yml")
		if _, err := update.CheckDaily(context.Background(), c, state, "1.0.0", now); err == nil {
			t.Fatal("want an error")
		}
		_, _ = update.CheckDaily(context.Background(), c, state, "1.0.0", now.Add(time.Hour))
		if got := len(srv.Requests()); got != 1 {
			t.Errorf("requests = %d, want 1", got)
		}
	})
}
