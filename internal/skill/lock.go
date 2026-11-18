package skill

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// lockFile, next to the registry, is held while a process syncs copies or
// changes the registry.
const lockFile = "skills.lock"

// lockStale is how old a lock must be before it is taken for the leftover of
// a killed process. A sync takes milliseconds.
const lockStale = time.Minute

// TryLock takes the registry lock without waiting. It reports false when
// another process holds it or the lock cannot be made (a read-only state
// directory); the caller then skips its work.
func TryLock() (unlock func(), ok bool) {
	unlock, _ = tryLock()
	return unlock, unlock != nil
}

// Lock takes the registry lock, waiting up to wait for another process to
// release it. It reports false at once when the lock cannot be made at all.
func Lock(wait time.Duration) (unlock func(), ok bool) {
	deadline := time.Now().Add(wait)
	for {
		unlock, busy := tryLock()
		if unlock != nil {
			return unlock, true
		}
		if !busy || time.Now().After(deadline) {
			return nil, false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// tryLock takes the lock, or reports whether another process holds it.
func tryLock() (unlock func(), busy bool) {
	path, err := RegistryPath()
	if err != nil {
		return nil, false
	}
	path = filepath.Join(filepath.Dir(path), lockFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false
	}
	for range 2 {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, _ = f.WriteString(strconv.Itoa(os.Getpid()) + "\n")
			_ = f.Close()
			return func() { _ = os.Remove(path) }, false
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, false
		}
		fi, err := os.Stat(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue // released meanwhile
		case err != nil:
			return nil, false
		case time.Since(fi.ModTime()) < lockStale:
			return nil, true
		}
		_ = os.Remove(path) // left by a killed process
	}
	return nil, true
}
