// Package download implements `ytrack issue download`.
package download

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmd/issue/shared"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

type options struct {
	ref       shared.Ref
	selectors []string
	dir       string
	comment   string
	clobber   bool
}

// NewCmdDownload returns the download command.
func NewCmdDownload(f *cmdutil.Factory) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "download {<id> | <url>} [<name> | <attachment-id>]...",
		Short: "Download the attachments of an issue",
		Long: `Download the files attached to an issue and to its comments, and print
each one as "<name><TAB><bytes>".

With no names, every attachment is downloaded; otherwise only those whose
name or attachment ID is given (see "ytrack issue attachments"). --comment
keeps the files of one comment. Files are saved in --dir (created when
missing, the current folder by default) under their attachment names. A
name that holds a path, such as ../x, is saved with its separators replaced
by "_"; so are characters, trailing dots and spaces that Windows refuses,
and a Windows device name such as NUL.txt gets a leading "_". A name used
twice gets its attachment ID added. stderr says when a name changes.

An existing file is never overwritten unless --clobber is given. When a
download fails, the others still run, and the command exits 1 naming the
files that failed. Ctrl-C removes the partly written file and exits 2.`,
		Example: `  $ ytrack issue download APP-123 -o out/
  $ ytrack issue download APP-123 trace.log 8-123
  $ ytrack issue download https://acme.youtrack.cloud/issue/APP-123 --comment 4-17 --clobber`,
		Args:              cmdutil.MinimumArgs(1, "expected an issue ID or URL, e.g. APP-123"),
		ValidArgsFunction: shared.CompleteIssues(f, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0])
			if err != nil {
				return err
			}
			opts.ref, opts.selectors = ref, args[1:]
			if err := shared.UseRefHost(f, ref, true); err != nil {
				return err
			}
			return f.WithIssueHost(cmd.Context(), ref.ID, func() error { return run(cmd.Context(), f, opts) })
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&opts.dir, "dir", "o", ".", "Save the files in this `directory`")
	fl.StringVar(&opts.comment, "comment", "", "Only the attachments of the comment with this `id`")
	fl.BoolVar(&opts.clobber, "clobber", false, "Overwrite existing files")
	return cmd
}

func run(ctx context.Context, f *cmdutil.Factory, opts *options) error {
	client, host, err := f.APIClient()
	if err != nil {
		return err
	}
	all, err := adapter.ListAttachments(ctx, client, opts.ref.ID)
	if err != nil {
		return shared.NotFound(err, opts.ref.ID, host)
	}
	picked, err := pick(all, opts)
	if err != nil {
		return err
	}
	ios := f.IOStreams
	if len(picked) == 0 {
		fmt.Fprintf(ios.ErrOut, "%s has no attachments\n", opts.ref.ID)
		return nil
	}
	if err := os.MkdirAll(opts.dir, 0o750); err != nil {
		return err
	}
	ctx, stop := interruptible(ctx)
	defer stop()
	var failed []string
	used := map[string]bool{}
	for _, a := range picked {
		name, why := fileName(a, used)
		if why != "" {
			fmt.Fprintf(ios.ErrOut, "saving %q as %q: %s\n", output.SanitizeCell(a.Name), output.SanitizeCell(name), why)
		}
		n, err := save(ctx, client, a, filepath.Join(opts.dir, name), opts.clobber)
		if err != nil && ctx.Err() != nil {
			fmt.Fprintf(ios.ErrOut, "%s: interrupted; the partial file was removed\n", output.SanitizeCell(name))
			return clierr.ErrCancel
		}
		if err != nil {
			fmt.Fprintf(ios.ErrOut, "%s: %s\n", output.SanitizeCell(name), err)
			failed = append(failed, output.SanitizeCell(name))
			continue
		}
		fmt.Fprintf(ios.Out, "%s\t%d\n", output.SanitizeCell(name), n)
	}
	if len(failed) > 0 {
		return fmt.Errorf("could not download %d of %d attachments: %s", len(failed), len(picked), strings.Join(failed, ", "))
	}
	return nil
}

// pick keeps the attachments of --comment and, when names or IDs are given,
// those matching one; a name or ID that matches nothing is an error.
func pick(all []adapter.IssueAttachment, opts *options) ([]adapter.IssueAttachment, error) {
	var out []adapter.IssueAttachment
	for _, a := range all {
		if opts.comment != "" && a.CommentID != opts.comment {
			continue
		}
		if len(opts.selectors) > 0 && !slices.Contains(opts.selectors, a.Name) && !slices.Contains(opts.selectors, a.ID) {
			continue
		}
		out = append(out, a)
	}
	var missing []string
	for _, s := range opts.selectors {
		if !slices.ContainsFunc(out, func(a adapter.IssueAttachment) bool { return a.Name == s || a.ID == s }) {
			missing = append(missing, fmt.Sprintf("%q", s))
		}
	}
	where := opts.ref.ID
	if opts.comment != "" {
		where = "comment " + opts.comment + " on " + opts.ref.ID
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("no attachment named or with ID %s on %s; see `ytrack issue attachments %s`", strings.Join(missing, ", "), where, opts.ref.ID)
	}
	if len(out) == 0 && opts.comment != "" {
		return nil, fmt.Errorf("%s has no attachments", where)
	}
	return out, nil
}

// fileName is the name to save a under, and why it differs from a.Name
// ("" when it does not). used holds the names taken so far, compared without
// case for case-insensitive file systems.
func fileName(a adapter.IssueAttachment, used map[string]bool) (name, why string) {
	name = safeName(a.Name)
	switch {
	case name == a.Name:
	case strings.ContainsAny(a.Name, `/\`):
		why = "attachment names cannot hold a path"
	default:
		why = "the name is not a valid file name on every system"
	}
	if used[strings.ToLower(name)] {
		ext := filepath.Ext(name)
		name = strings.TrimSuffix(name, ext) + "-" + safeName(a.ID) + ext
		why = "another attachment has the same name"
	}
	used[strings.ToLower(name)] = true
	return name, why
}

// safeName makes an attachment name a plain file name that is valid on
// every system, so the file always lands in --dir under that name: path
// separators, drive colons, the other characters Windows refuses (<>"|?*)
// and control characters become "_"; so do trailing dots and spaces, which
// Windows drops ("." and ".." become "_" and "__", an empty name "_"); and a
// Windows device name such as NUL or com1.txt gets a leading "_".
func safeName(name string) string {
	s := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:<>"|?*`, r) || r < 0x20 || r == 0x7f {
			return '_'
		}
		return r
	}, name)
	kept := strings.TrimRight(s, ". ")
	s = kept + strings.Repeat("_", len(s)-len(kept))
	if s == "" {
		s = "_"
	}
	if isDeviceName(s) {
		s = "_" + s
	}
	return s
}

// windowsDevices are the names Windows reserves for devices in every folder,
// with or without an extension.
var windowsDevices = []string{
	"CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$",
	"COM0", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "COM¹", "COM²", "COM³",
	"LPT0", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9", "LPT¹", "LPT²", "LPT³",
}

// isDeviceName reports whether Windows reads name as a device: its part
// before the first dot, without trailing spaces, is a reserved name in any
// case (NUL, nul.txt, "Com1 .log").
func isDeviceName(name string) bool {
	stem, _, _ := strings.Cut(name, ".")
	stem = strings.TrimRight(stem, " ")
	return slices.ContainsFunc(windowsDevices, func(d string) bool { return strings.EqualFold(stem, d) })
}

// interruptible returns ctx cancelled on Ctrl-C (os.Interrupt) until stop is
// called, so an interrupted download fails like any other and save removes
// what it wrote instead of the process dying with a partial file on disk.
func interruptible(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, os.Interrupt)
}

// save downloads a to path. Without clobber the file is created only if it
// does not exist; with clobber the download goes to a temporary file that
// then replaces path, so a failed download leaves the old file in place.
// A failed download removes what it wrote. Files are private to the user
// (0600), like the temporary file, since attachments can hold anything.
func save(ctx context.Context, client *transport.Client, a adapter.IssueAttachment, path string, clobber bool) (int64, error) {
	var (
		file *os.File
		err  error
	)
	if clobber {
		file, err = os.CreateTemp(filepath.Dir(path), ".ytrack-download-*")
	} else {
		file, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			return 0, fmt.Errorf("%s already exists; pass --clobber to overwrite it", path)
		}
	}
	if err != nil {
		return 0, err
	}
	n, err := adapter.DownloadAttachment(ctx, client, a, file)
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err == nil && clobber {
		err = os.Rename(file.Name(), path)
	}
	if err != nil {
		_ = os.Remove(file.Name())
		return 0, err
	}
	return n, nil
}
