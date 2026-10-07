// Package factory builds the production cmdutil.Factory.
package factory

import (
	"os"

	"github.com/BrainerVirus/youtrack-cli/internal/browser"
	"github.com/BrainerVirus/youtrack-cli/internal/config"
	"github.com/BrainerVirus/youtrack-cli/internal/iostreams"
	"github.com/BrainerVirus/youtrack-cli/internal/prompter"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// New returns a Factory wired to the real terminal, config dir and browser.
func New(appVersion string, ios *iostreams.IOStreams) *cmdutil.Factory {
	f := &cmdutil.Factory{
		IOStreams:  ios,
		AppVersion: appVersion,
		Prompter:   prompter.New(ios.In, ios.ErrOut),
	}
	var cfg *config.Config
	f.Config = func() (*config.Config, error) {
		if cfg != nil {
			return cfg, nil
		}
		c, err := config.Load()
		if err != nil {
			return nil, err
		}
		cfg = c
		return cfg, nil
	}
	f.Browser = &lazyBrowser{f: f}
	return f
}

// lazyBrowser defers reading config until a URL is actually opened.
type lazyBrowser struct{ f *cmdutil.Factory }

func (b *lazyBrowser) Browse(url string) error {
	launcher := os.Getenv("YTRACK_BROWSER")
	if launcher == "" {
		if cfg, err := b.f.Config(); err == nil {
			launcher = cfg.Browser()
		}
	}
	if launcher == "" {
		launcher = os.Getenv("BROWSER")
	}
	return browser.New(launcher, b.f.IOStreams.ErrOut, b.f.IOStreams.ErrOut).Browse(url)
}
