// Package auth stores and resolves YouTrack tokens.
//
// Tokens live in the OS keyring under service "ytrack:<host key>". When no
// keyring is usable, storing fails unless the caller opts into insecure
// storage, which writes credentials.yml (mode 0600) in the config directory.
// Nothing silently downgrades from the keyring to the file.
package auth

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/zalando/go-keyring"
	"gopkg.in/yaml.v3"

	"github.com/BrainerVirus/youtrack-cli/internal/config"
)

const (
	keyringUser    = "token"
	credentialFile = "credentials.yml"
	// D-Bus Secret Service can hang when no agent answers; never block forever.
	keyringTimeout = 5 * time.Second
)

// Token sources, as reported by Resolve.
const (
	SourceEnv     = "YTRACK_TOKEN"
	SourceKeyring = config.StorageKeyring
	SourceFile    = config.StorageFile
)

// ErrNoKeyring means the OS keyring could not store the token.
var ErrNoKeyring = errors.New("no usable OS keyring")

// KeyringService returns the keyring service name for a host key.
func KeyringService(hostKey string) string { return "ytrack:" + hostKey }

// Store reads and writes tokens for hosts.
type Store struct {
	// Dir is the config directory holding the insecure credentials file.
	Dir string
}

// Set stores token for hostKey and returns where it went (keyring or file).
// With insecure=false it uses only the keyring and returns an error wrapping
// ErrNoKeyring when that fails.
func (s Store) Set(hostKey, token string, insecure bool) (string, error) {
	if insecure {
		if err := s.setFile(hostKey, token); err != nil {
			return "", err
		}
		_ = withTimeout(func() error { return keyring.Delete(KeyringService(hostKey), keyringUser) })
		return config.StorageFile, nil
	}
	err := withTimeout(func() error { return keyring.Set(KeyringService(hostKey), keyringUser, token) })
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrNoKeyring, err)
	}
	// A previous --insecure-storage login must not leave a plaintext copy behind.
	if err := s.deleteFile(hostKey); err != nil {
		return "", err
	}
	return config.StorageKeyring, nil
}

// Get returns the stored token for hostKey and its source, or "" if none.
func (s Store) Get(hostKey string) (token, source string, err error) {
	kerr := withTimeout(func() error {
		var e error
		token, e = keyring.Get(KeyringService(hostKey), keyringUser)
		return e
	})
	if kerr == nil && token != "" {
		return token, SourceKeyring, nil
	}
	creds, err := s.readFile()
	if err != nil {
		return "", "", err
	}
	if t := creds[hostKey]; t != "" {
		return t, SourceFile, nil
	}
	return "", "", nil
}

// Delete removes hostKey's token from the keyring and the credentials file.
func (s Store) Delete(hostKey string) error {
	kerr := withTimeout(func() error { return keyring.Delete(KeyringService(hostKey), keyringUser) })
	if kerr != nil && !errors.Is(kerr, keyring.ErrNotFound) {
		// Still remove any file copy; report the keyring failure afterwards.
		if err := s.deleteFile(hostKey); err != nil {
			return err
		}
		return fmt.Errorf("could not remove the token from the keyring: %w", kerr)
	}
	return s.deleteFile(hostKey)
}

func (s Store) path() string { return filepath.Join(s.Dir, credentialFile) }

func (s Store) readFile() (map[string]string, error) {
	creds := map[string]string{}
	b, err := os.ReadFile(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return creds, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, &creds); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", s.path(), err)
	}
	return creds, nil
}

func (s Store) writeFile(creds map[string]string) error {
	if len(creds) == 0 {
		err := os.Remove(s.path())
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	b, err := yaml.Marshal(creds)
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(s.path(), b)
}

func (s Store) setFile(hostKey, token string) error {
	creds, err := s.readFile()
	if err != nil {
		return err
	}
	creds[hostKey] = token
	return s.writeFile(creds)
}

func (s Store) deleteFile(hostKey string) error {
	creds, err := s.readFile()
	if err != nil {
		return err
	}
	if _, ok := creds[hostKey]; !ok {
		return nil
	}
	delete(creds, hostKey)
	return s.writeFile(creds)
}

func withTimeout(fn func() error) error {
	ch := make(chan error, 1)
	go func() { ch <- fn() }()
	select {
	case err := <-ch:
		return err
	case <-time.After(keyringTimeout):
		return errors.New("timed out waiting for the OS keyring")
	}
}
