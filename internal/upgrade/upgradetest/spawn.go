package upgradetest

import (
	"sync"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/upgrade"
)

// Spawn is a background upgrade start that a Spawner recorded.
type Spawn struct {
	Exe     string
	Args    []string
	LogPath string
}

// Spawner records background upgrade starts instead of starting processes.
type Spawner struct {
	mu    sync.Mutex
	calls []Spawn
}

// Calls returns the starts recorded so far.
func (s *Spawner) Calls() []Spawn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Spawn(nil), s.calls...)
}

// FakeSpawn makes upgrade.Spawn record its calls, and upgrade.Executable
// return exe, until the test ends. Tests that use it must not run in
// parallel.
func FakeSpawn(t *testing.T, exe string) *Spawner {
	t.Helper()
	s := &Spawner{}
	oldSpawn, oldExe := upgrade.Spawn, upgrade.Executable
	upgrade.Spawn = func(exe string, args []string, logPath string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.calls = append(s.calls, Spawn{Exe: exe, Args: append([]string(nil), args...), LogPath: logPath})
		return nil
	}
	upgrade.Executable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { upgrade.Spawn, upgrade.Executable = oldSpawn, oldExe })
	return s
}
