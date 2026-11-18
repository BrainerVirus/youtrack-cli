package skill

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

// lockFileName, next to the registry, is held while a process syncs copies
// or changes the registry.
const lockFileName = "skills.lock"

// lockStale is how old a lock must be before it is taken for the leftover of
// a killed process. A sync takes milliseconds.
const lockStale = time.Minute

// TryLock takes the registry lock without waiting. It reports false when
// another process holds it or the lock cannot be made (a read-only state
// directory); the caller then skips its work.
func TryLock() (unlock func(), ok bool) {
	path, err := registryLockPath()
	if err != nil {
		return nil, false
	}
	unlock, _ = tryLockFile(path)
	return unlock, unlock != nil
}

// Lock takes the registry lock, waiting up to wait for another process to
// release it. It reports false at once when the lock cannot be made at all.
func Lock(wait time.Duration) (unlock func(), ok bool) {
	path, err := registryLockPath()
	if err != nil {
		return nil, false
	}
	return lockFile(path, wait)
}

func registryLockPath() (string, error) {
	path, err := RegistryPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), lockFileName), nil
}

// lockFile takes the lock file at path, waiting up to wait for another
// holder to release it. It reports false at once when the lock cannot be
// made at all (a read-only directory).
func lockFile(path string, wait time.Duration) (unlock func(), ok bool) {
	deadline := time.Now().Add(wait)
	for {
		unlock, busy := tryLockFile(path)
		if unlock != nil {
			return unlock, true
		}
		if !busy || time.Now().After(deadline) {
			return nil, false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// tryLockFile creates the lock file at path, or reports whether another
// holder has it. A lock older than lockStale is a killed holder's and is
// taken over.
func tryLockFile(path string) (unlock func(), busy bool) {
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
		denied := errors.Is(err, fs.ErrPermission)
		if !errors.Is(err, fs.ErrExist) && !denied {
			return nil, false
		}
		fi, err := os.Stat(path)
		switch {
		case denied && errors.Is(err, fs.ErrNotExist):
			// A read-only directory; on Windows, also a lock file its holder
			// is deleting, which is free a moment later.
			return nil, runtime.GOOS == "windows"
		case errors.Is(err, fs.ErrNotExist):
			continue // released meanwhile
		case err != nil:
			return nil, true // Windows: being deleted by its holder
		case time.Since(fi.ModTime()) < lockStale:
			return nil, true
		}
		_ = os.Remove(path) // left by a killed process
	}
	return nil, true
}
