// Package skill implements `ytrack skill`: the agent skill embedded in the
// binary, printed or installed for a coding agent.
package skill

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/BrainerVirus/youtrack-cli/internal/build"
	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
	"github.com/BrainerVirus/youtrack-cli/internal/skill"
	"github.com/BrainerVirus/youtrack-cli/pkg/cmdutil"
)

// NewCmdSkill returns the skill command group.
func NewCmdSkill(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill <command>",
		Short: "Print or install the ytrack agent skill",
		Long: `Print, install and keep up to date the agent skill that teaches coding
agents (Claude Code, Codex, Cursor, OpenCode, Pi and others) how to use
ytrack. The skill is built into this binary and stamped with its version, so
it always matches the ytrack you run. ytrack remembers where it installed the
skill and rewrites the copies in your home directory when a newer ytrack
first runs. These commands work offline and without logging in.`,
		RunE: cmdutil.GroupRunE,
	}
	cmd.AddCommand(newCmdPrint(f))
	cmd.AddCommand(newCmdInstall(f))
	cmd.AddCommand(newCmdList(f))
	cmd.AddCommand(newCmdSync(f))
	cmd.AddCommand(newCmdUninstall(f))
	return cmd
}

func newCmdPrint(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "print",
		Short: "Print the skill's SKILL.md",
		Long: `Print the skill's SKILL.md, with metadata.version set to this ytrack's
version. The skill's reference files are written only by ytrack skill install.`,
		Example: `  $ ytrack skill print
  $ ytrack skill print > ~/skills/ytrack/SKILL.md`,
		Args: cmdutil.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			b, err := skill.Render(build.Version)
			if err != nil {
				return err
			}
			_, err = f.IOStreams.Out.Write(b)
			return err
		},
	}
}

func newCmdInstall(f *cmdutil.Factory) *cobra.Command {
	var (
		dir     string
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:   "install [--dir <skills-dir>]",
		Short: "Install the skill into agents' skills directories",
		Long: `Write the skill to <skills-dir>/ytrack: SKILL.md, stamped with this ytrack's
version, and its references/ directory. <skills-dir> is created when
missing. Without --dir, install into ~/.agents/skills (read by Codex,
Cursor, OpenCode, Pi and others) and ~/.claude/skills (Claude Code).

ytrack records every directory it installs into (see ytrack skill list) and
keeps the copies in your home directory at its own version: the first
command a newer ytrack runs rewrites them. A copy in a repository, such as
--dir .claude/skills inside a git work tree, is recorded but changes only
when you run this command again there.

An existing <skills-dir>/ytrack is replaced only when its SKILL.md has
"name: ytrack"; any other directory there is refused and left untouched. A
symlink there, as npx skills add makes, is followed. The new copy is written
next to the old one and renamed into place, so a failed install changes
nothing.

Skills directories by agent:
  Claude Code  ~/.claude/skills (or .claude/skills in a project)
  Codex        ~/.agents/skills (older Codex: ~/.codex/skills)
  Cursor       ~/.agents/skills or ~/.cursor/skills
  OpenCode     ~/.agents/skills or ~/.config/opencode/skills
  Pi           ~/.agents/skills or ~/.pi/agent/skills

Prints each skill directory, one per line. JSON fields (--json), one object
per line, one line per directory:
  path     the skill directory, e.g. /home/me/.claude/skills/ytrack
  version  the version stamped into SKILL.md`,
		Example: `  $ ytrack skill install
  $ ytrack skill install --dir ~/.cursor/skills
  $ ytrack skill install --dir .claude/skills --json`,
		Args: cmdutil.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var dirs []string
			if cmd.Flags().Changed("dir") {
				if dir == "" {
					return clierr.FlagErrorf("`--dir` needs a directory, e.g. --dir ~/.claude/skills")
				}
				dirs = []string{expandHome(dir)}
			} else {
				defaults, err := skill.DefaultDirs()
				if err != nil {
					return err
				}
				dirs = defaults
			}
			var installed []string
			var failed error
			for _, d := range dirs {
				path, err := skill.Install(d, build.Version)
				if foreign, ok := errors.AsType[*skill.ForeignError](err); ok {
					err = fmt.Errorf("%w; remove it or choose another --dir", foreign)
				} else if err != nil {
					err = fmt.Errorf("installing the skill into %s: %w", d, err)
				}
				if err != nil {
					failed = errors.Join(failed, err)
					continue
				}
				installed = append(installed, filepath.Dir(path))
				if jsonOut {
					if err := json.NewEncoder(f.IOStreams.Out).Encode(struct {
						Path    string `json:"path"`
						Version string `json:"version"`
					}{path, build.Version}); err != nil {
						return err
					}
				} else {
					fmt.Fprintln(f.IOStreams.Out, path)
				}
			}
			record(installed)
			if failed != nil {
				return failed
			}
			cs := f.IOStreams.ErrColorScheme()
			fmt.Fprintf(f.IOStreams.ErrOut, "%s Installed the ytrack skill %s\n", cs.SuccessIcon(), build.Version)
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&dir, "dir", "", "Agent skills `directory` to install into, e.g. ~/.claude/skills (default ~/.agents/skills and ~/.claude/skills)")
	fl.BoolVar(&jsonOut, "json", false, "Print {path, version} as JSON, one object per line")
	return cmd
}

// lockWait bounds how long a skill command waits for another ytrack that is
// syncing the copies; it then goes on without the lock.
const lockWait = 3 * time.Second

// record adds dirs to the registry. Best-effort: the skill is installed
// either way, and a registry that cannot be written (a read-only state
// directory) only means the copies are not kept up to date.
func record(dirs []string) {
	if len(dirs) == 0 {
		return
	}
	if unlock, ok := skill.Lock(lockWait); ok {
		defer unlock()
	}
	reg, err := skill.OpenRegistry()
	if err != nil {
		return
	}
	changed := reg.Dirty()
	for _, d := range dirs {
		changed = reg.Record(d) || changed
	}
	if changed {
		_ = reg.Save()
	}
}

// expandHome expands a leading "~" (as in --dir=~/.claude/skills, which most
// shells leave alone) to the home directory.
func expandHome(dir string) string {
	rest, ok := strings.CutPrefix(dir, "~")
	if !ok || (rest != "" && rest[0] != '/' && rest[0] != filepath.Separator) {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return dir
	}
	return filepath.Join(home, rest)
}
