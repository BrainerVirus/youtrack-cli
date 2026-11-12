// Package paths implements `ytrack auth paths`.
package paths

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/config"
	"github.com/BrainerVirus/youtrack-cli/internal/hosts"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// Fields lists the --json fields of auth paths.
var Fields = []string{"host", "paths"}

type hostPaths struct {
	Host  string
	Paths []string
}

func (h hostPaths) ExportData(fields []string) map[string]any {
	paths := h.Paths
	if paths == nil {
		paths = []string{}
	}
	all := map[string]any{"host": h.Host, "paths": paths}
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		out[f] = all[f]
	}
	return out
}

type options struct {
	add      []string
	remove   []string
	exporter *output.Exporter
}

// NewCmdPaths returns the paths command.
func NewCmdPaths(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "paths",
		Short: "Pick a host by folder",
		Long: `List or change the folders whose commands talk to a host.

A command run inside a folder that matches one of a host's paths talks to
that host when neither --host, an issue URL nor YTRACK_HOST names one; the
default host comes last. When paths of several hosts match, the one with the
longest text before its first wildcard wins.

A path is absolute or starts with ~/ and may use * (within one folder name)
and ** (any number of folders): /home/me/work/** matches /home/me/work and
every folder under it.

Without --add or --remove, it lists the paths of every logged-in host, or of
the --host host. With them, it changes the paths of the --host host (which
may be left out when only one host is logged in).

JSON fields:
  host   host key
  paths  path globs`,
		Example: `  $ ytrack auth paths
  $ ytrack auth paths --host acme.youtrack.cloud --add '~/work/**'
  $ ytrack auth paths --host acme.youtrack.cloud --remove '~/work/**'
  $ ytrack auth status    # shows which host the current folder uses and why`,
		Args: cmdutil.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := f.Config()
			if err != nil {
				return err
			}
			if len(opts.add) == 0 && len(opts.remove) == 0 {
				return list(f, cfg, opts)
			}
			return edit(f, cfg, opts)
		},
	}
	fl := cmd.Flags()
	fl.StringArrayVar(&opts.add, "add", nil, "Add a folder `glob` to the host's paths (repeatable)")
	fl.StringArrayVar(&opts.remove, "remove", nil, "Remove a folder `glob` from the host's paths (repeatable)")
	cmdutil.AddJSONFlags(cmd, &opts.exporter, Fields)
	return cmd
}

// flagHost returns the key of the logged-in host named by --host, or "".
func flagHost(f *cmdutil.Factory, cfg *config.Config) (string, error) {
	if f.HostFlag == "" {
		return "", nil
	}
	h, err := hosts.Parse(f.HostFlag)
	if err != nil {
		return "", clierr.FlagErrorWrap(err)
	}
	if cfg.Host(h.Key) == nil {
		return "", clierr.FlagErrorf("not logged in to %s; known hosts: %v", h.Key, cfg.HostKeys())
	}
	return h.Key, nil
}

func list(f *cmdutil.Factory, cfg *config.Config, opts *options) error {
	key, err := flagHost(f, cfg)
	if err != nil {
		return err
	}
	keys := cfg.HostKeys()
	if key != "" {
		keys = []string{key}
	}
	if len(keys) == 0 {
		return clierr.AuthErrorf("not logged in to any YouTrack host; run `ytrack auth login`")
	}
	rows := make([]hostPaths, len(keys))
	for i, k := range keys {
		rows[i] = hostPaths{Host: k, Paths: cfg.HostPaths(k)}
	}
	return write(f, opts, rows)
}

func write(f *cmdutil.Factory, opts *options, rows []hostPaths) error {
	if opts.exporter != nil {
		return opts.exporter.Write(f.IOStreams, rows)
	}
	w := f.IOStreams.Out
	for _, r := range rows {
		fmt.Fprintln(w, output.SanitizeCell(r.Host))
		if len(r.Paths) == 0 {
			fmt.Fprintln(w, "  (no paths)")
		}
		for _, p := range r.Paths {
			fmt.Fprintf(w, "  %s\n", output.SanitizeCell(p))
		}
	}
	return nil
}

func edit(f *cmdutil.Factory, cfg *config.Config, opts *options) error {
	key, err := flagHost(f, cfg)
	if err != nil {
		return err
	}
	if key == "" {
		keys := cfg.HostKeys()
		switch len(keys) {
		case 0:
			return clierr.AuthErrorf("not logged in to any YouTrack host; run `ytrack auth login`")
		case 1:
			key = keys[0]
		default:
			return clierr.FlagErrorf("--host is required to change paths when more than one host is logged in; known hosts: %v", keys)
		}
	}
	for _, g := range opts.add {
		if err := config.ValidatePathGlob(g); err != nil {
			return clierr.FlagErrorWrap(err)
		}
	}
	errOut := f.IOStreams.ErrOut
	for _, g := range opts.remove {
		if !cfg.RemoveHostPath(key, g) {
			fmt.Fprintf(errOut, "! %s was not a path of %s\n", output.SanitizeCell(g), key)
		}
	}
	for _, g := range opts.add {
		if _, err := cfg.AddHostPath(key, g); err != nil {
			return clierr.FlagErrorWrap(err)
		}
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	if opts.exporter != nil {
		return write(f, opts, []hostPaths{{Host: key, Paths: cfg.HostPaths(key)}})
	}
	ok := f.IOStreams.ErrColorScheme().SuccessIcon()
	paths := "(none)"
	if ps := cfg.HostPaths(key); len(ps) > 0 {
		paths = output.SanitizeCell(strings.Join(ps, ", "))
	}
	fmt.Fprintf(errOut, "%s Paths of %s: %s\n", ok, key, paths)
	return nil
}
