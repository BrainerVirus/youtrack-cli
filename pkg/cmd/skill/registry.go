package skill

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/build"
	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/output"
	"github.com/BrainerVirus/youtrack-cli/internal/skill"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// openLocked opens the registry under its lock, saving any copies adopted on
// the way. unlock is never nil.
func openLocked() (reg *skill.Registry, unlock func(), err error) {
	unlock = func() {}
	if u, ok := skill.Lock(lockWait); ok {
		unlock = u
	}
	if reg, err = skill.OpenRegistry(); err != nil {
		unlock()
		return nil, nil, err
	}
	if reg.Dirty() {
		_ = reg.Save()
	}
	return reg, unlock, nil
}

func updatesOf(c skill.Copy) string {
	if c.Managed {
		return "auto"
	}
	return "manual"
}

func newCmdList(f *cmdutil.Factory) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the installed copies of the skill",
		Long: `List the skills directories ytrack installed the skill into, with the
version each copy holds and its state:
  current  the version of this ytrack
  stale    an older version
  newer    a newer ytrack's version; this ytrack leaves it alone
  missing  deleted, or replaced by something that is not the ytrack skill

UPDATES says how a copy is kept at ytrack's version: auto for a copy in your
home directory, which ytrack rewrites when a newer ytrack first runs (or now,
with ytrack skill sync); manual for a copy in a repository (a git work
tree), which changes only when you run ytrack skill install --dir there.

The first run also records copies installed before ytrack kept a list, in
the home-level skills directories of Claude Code, Codex, Cursor, OpenCode,
Pi and ~/.agents/skills.

JSON fields (--json), an array:
  dir      the skills directory, e.g. /home/me/.claude/skills
  path     the skill directory, dir/ytrack
  version  metadata.version of its SKILL.md; empty when missing
  state    current, stale, newer or missing
  managed  true when ytrack keeps it up to date (UPDATES auto)`,
		Example: `  $ ytrack skill list
  $ ytrack skill list --json`,
		Args: cmdutil.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			reg, unlock, err := openLocked()
			if err != nil {
				return err
			}
			unlock()
			copies := reg.Copies(build.Version)
			if jsonOut {
				type row struct {
					Dir     string `json:"dir"`
					Path    string `json:"path"`
					Version string `json:"version"`
					State   string `json:"state"`
					Managed bool   `json:"managed"`
				}
				rows := make([]row, 0, len(copies))
				for _, c := range copies {
					rows = append(rows, row{c.Dir, c.Path, c.Version, c.State, c.Managed})
				}
				return json.NewEncoder(f.IOStreams.Out).Encode(rows)
			}
			if len(copies) == 0 {
				fmt.Fprintln(f.IOStreams.ErrOut, "No installed copies of the ytrack skill are recorded; run `ytrack skill install`.")
				return nil
			}
			t := output.NewTable(f.IOStreams, "dir", "version", "state", "updates")
			for _, c := range copies {
				t.Row(c.Dir, c.Version, c.State, updatesOf(c))
			}
			if err := t.Render(); err != nil {
				return err
			}
			listHints(f, copies)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the copies as a JSON array")
	return cmd
}

// listHints tells how to bring stale and missing copies in line.
func listHints(f *cmdutil.Factory, copies []skill.Copy) {
	w := f.IOStreams.ErrOut
	staleAuto, missing, newer := false, false, ""
	for _, c := range copies {
		switch {
		case c.State == skill.StateMissing:
			missing = true
		case c.State == skill.StateNewer:
			newer = c.Version
		case c.State == skill.StateStale && !c.Managed:
			fmt.Fprintf(w, "%s is in a repository and holds the skill %s; update it with `ytrack skill install --dir %s`\n",
				output.SanitizeText(c.Dir), output.SanitizeText(c.Version), output.SanitizeText(c.Dir))
		case c.State == skill.StateStale:
			staleAuto = true
		}
	}
	if staleAuto {
		fmt.Fprintln(w, "Run `ytrack skill sync` to update the stale copies in your home directory now.")
	}
	if newer != "" {
		fmt.Fprintf(w, "A newer ytrack (%s) installed some copies; this ytrack (%s) leaves them alone. Upgrade with `ytrack upgrade`.\n",
			output.SanitizeText(newer), output.SanitizeText(build.Version))
	}
	if missing {
		fmt.Fprintln(w, "`ytrack skill sync` forgets the missing copies; `ytrack skill install --dir <dir>` puts one back.")
	}
}

func newCmdSync(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Update every installed copy of the skill now",
		Long: `Rewrite every copy of the skill in your home directory (UPDATES auto in
ytrack skill list) at this ytrack's version, and forget every copy that is
missing; a missing copy is not re-created. A copy in a repository is left
for ytrack skill install --dir.

ytrack does this by itself the first time a newer release runs; run it after
installing ytrack by other means, or to bring copies back to this version.

Prints one line per recorded directory: the action (updated, dropped,
manual or failed), the skill directory and the version it holds. Exits 1
when a copy could not be rewritten.`,
		Example: `  $ ytrack skill sync`,
		Args:    cmdutil.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			reg, unlock, err := openLocked()
			if err != nil {
				return err
			}
			defer unlock()
			if len(reg.Dirs) == 0 {
				fmt.Fprintln(f.IOStreams.ErrOut, "No installed copies of the ytrack skill are recorded; run `ytrack skill install`.")
				return nil
			}
			before := reg.Clone()
			results, _ := reg.Sync(build.Version)
			if !reg.Equal(before) {
				if err := reg.Save(); err != nil {
					fmt.Fprintf(f.IOStreams.ErrOut, "warning: could not save the list of skill copies: %s\n", output.SanitizeText(err.Error()))
				}
			}
			var failed error
			for _, r := range results {
				version := r.Version
				switch r.Action {
				case skill.SyncDropped:
					version = "(missing)"
				case skill.SyncFailed:
					failed = errors.Join(failed, fmt.Errorf("updating %s: %w", r.Path, r.Err))
				}
				fmt.Fprintf(f.IOStreams.Out, "%s\t%s\t%s\n", r.Action, output.SanitizeCell(r.Path), output.SanitizeCell(version))
			}
			return failed
		},
	}
}

func newCmdUninstall(f *cmdutil.Factory) *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "uninstall --dir <skills-dir>",
		Short: "Remove the skill from a skills directory",
		Long: `Remove <skills-dir>/ytrack and stop keeping it up to date. Only the ytrack
skill is removed: a directory there whose SKILL.md does not say
"name: ytrack" is refused and left untouched. A symlink there is removed,
not its target.`,
		Example: `  $ ytrack skill uninstall --dir ~/.cursor/skills`,
		Args:    cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("dir") || dir == "" {
				return clierr.FlagErrorf("`--dir` is required, e.g. --dir ~/.cursor/skills")
			}
			root, err := filepath.Abs(expandHome(dir))
			if err != nil {
				return err
			}
			removed, err := skill.Uninstall(root)
			if foreign, ok := errors.AsType[*skill.ForeignError](err); ok {
				return foreign
			}
			if err != nil {
				return fmt.Errorf("removing the skill: %w", err)
			}
			reg, unlock, err := openLocked()
			if err != nil {
				return err
			}
			defer unlock()
			if reg.Forget(root) {
				if err := reg.Save(); err != nil {
					return fmt.Errorf("forgetting %s: %w", root, err)
				}
			}
			cs := f.IOStreams.ErrColorScheme()
			if removed {
				fmt.Fprintf(f.IOStreams.ErrOut, "%s Removed %s\n", cs.SuccessIcon(), filepath.Join(root, skill.Name))
			} else {
				fmt.Fprintf(f.IOStreams.ErrOut, "No ytrack skill in %s\n", root)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "Agent skills `directory` to remove the skill from")
	return cmd
}
