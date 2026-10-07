// Package browser opens URLs in the user's web browser.
package browser

import (
	"io"

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

type systemBrowser struct{}

func (systemBrowser) Browse(url string) error { return clibrowser.OpenURL(url) }

// Stub records URLs instead of opening them. For tests.
type Stub struct{ URLs []string }

func (s *Stub) Browse(url string) error {
	s.URLs = append(s.URLs, url)
	return nil
}
