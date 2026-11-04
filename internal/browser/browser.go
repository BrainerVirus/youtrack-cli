// Package browser opens URLs in the user's web browser.
package browser

import (
	"io"
	"os"

	clibrowser "github.com/cli/browser"
	ghbrowser "github.com/cli/go-gh/v2/pkg/browser"
)

// Browser opens a URL.
type Browser interface {
	Browse(url string) error
}

// New returns a Browser. A non-empty launcher (from $YTRACK_BROWSER, the
// config "browser" key or $BROWSER) is run with the URL as its argument;
// otherwise the platform default opener is used.
func New(launcher string, stdout, stderr io.Writer) Browser {
	if launcher != "" {
		return ghbrowser.New(launcher, stdout, stderr)
	}
	clibrowser.Stdout = stdout
	clibrowser.Stderr = stderr
	return systemBrowser{}
}

// Quiet returns a Browser that discards the browser's own output. A browser
// launched from a terminal prints its diagnostics there (Chrome's "Sandbox:
// CanCreateUserNamespace() clone() failure", GTK warnings), and during `auth
// login` that lands in the middle of the token prompt. Failure to launch is
// still returned as an error.
//
// The sink is a real file (os.DevNull), never io.Discard: for a non-file
// writer exec copies through a pipe and waits for every holder of it to exit,
// and the browser inherits that pipe and keeps running.
func Quiet(launcher string) Browser { return quietBrowser{launcher: launcher} }

type quietBrowser struct{ launcher string }

func (q quietBrowser) Browse(url string) error {
	sink, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return New(q.launcher, os.Stderr, os.Stderr).Browse(url)
	}
	defer sink.Close()
	return New(q.launcher, sink, sink).Browse(url)
}

type systemBrowser struct{}

func (systemBrowser) Browse(url string) error { return clibrowser.OpenURL(url) }

// Stub records URLs instead of opening them. For tests.
type Stub struct{ URLs []string }

func (s *Stub) Browse(url string) error {
	s.URLs = append(s.URLs, url)
	return nil
}
